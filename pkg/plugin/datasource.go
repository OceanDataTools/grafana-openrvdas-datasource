package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/grafana/grafana-plugin-sdk-go/live"

	"github.com/OceanDataTools/grafana-openrvdas-datasource/pkg/cds"
	"github.com/OceanDataTools/grafana-openrvdas-datasource/pkg/models"
)

var (
	_ backend.QueryDataHandler      = (*Datasource)(nil)
	_ backend.CheckHealthHandler    = (*Datasource)(nil)
	_ backend.StreamHandler         = (*Datasource)(nil)
	_ backend.CallResourceHandler   = (*Datasource)(nil)
	_ instancemgmt.InstanceDisposer = (*Datasource)(nil)
)

const (
	requestTimeout = 10 * time.Second
	fieldsCacheTTL = 10 * time.Second
	metaCacheTTL   = 5 * time.Minute
)

// NewDatasource creates a data source instance. Grafana creates a new
// instance (and disposes the old one) whenever the settings change.
func NewDatasource(_ context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	cfg, err := models.LoadPluginSettings(settings)
	if err != nil {
		return nil, err
	}
	opts := cds.Options{URL: cfg.URL, TLSSkipVerify: cfg.TLSSkipVerify, APIKey: cfg.EffectiveAPIKey()}
	return &Datasource{
		uid:      settings.UID,
		settings: cfg,
		opts:     opts,
		mgr:      cds.NewManager(opts, cfg.PollInterval(), log.DefaultLogger),
		meta:     map[string]*cds.Metadata{},
	}, nil
}

// Datasource streams OpenRVDAS CachedDataServer fields into Grafana.
type Datasource struct {
	uid      string
	settings *models.PluginSettings
	opts     cds.Options
	mgr      *cds.Manager

	fieldsMu sync.Mutex
	fields   []string
	fieldsAt time.Time

	metaMu sync.Mutex
	meta   map[string]*cds.Metadata // nil value = known to have no metadata
	metaAt time.Time
}

// Dispose closes the shared CDS connection.
func (d *Datasource) Dispose() {
	d.mgr.Close()
}

type queryModel struct {
	// Fields are CDS field names or '*' patterns.
	Fields []string `json:"fields"`
	// MinIntervalMs, if > 0, limits the stream to one (the most recent)
	// value per interval.
	MinIntervalMs int64 `json:"minIntervalMs"`
	// Stream defaults to true. When false, only the latest values are
	// returned (useful for Explore and alerting).
	Stream *bool `json:"stream"`
}

func (q queryModel) streaming() bool { return q.Stream == nil || *q.Stream }

// lazyConn opens a CDS connection on first use and is shared by all queries
// of one QueryData request.
type lazyConn struct {
	opts     cds.Options
	settings *models.PluginSettings
	conn     *cds.Conn
}

func (l *lazyConn) get(ctx context.Context) (*cds.Conn, error) {
	if l.conn == nil {
		if err := l.settings.Validate(); err != nil {
			return nil, err
		}
		c, err := cds.Dial(ctx, l.opts)
		if err != nil {
			return nil, errors.New(connectErrorMessage(err, l.settings))
		}
		l.conn = c
	}
	return l.conn, nil
}

func (l *lazyConn) close() {
	if l.conn != nil {
		_ = l.conn.Close()
	}
}

// QueryData returns, for each requested field, a frame holding the field's
// most recent value. When streaming, each frame is tagged with a Grafana
// Live channel; DataSourceWithBackend in the frontend subscribes to it and
// uses this frame as the stream's initial data.
func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	conn := &lazyConn{opts: d.opts, settings: d.settings}
	defer conn.close()

	for _, q := range req.Queries {
		response.Responses[q.RefID] = d.query(ctx, conn, q)
	}
	return response, nil
}

func (d *Datasource) query(ctx context.Context, conn *lazyConn, query backend.DataQuery) backend.DataResponse {
	var qm queryModel
	if err := json.Unmarshal(query.JSON, &qm); err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("json unmarshal: %v", err))
	}

	var available []string
	if hasWildcard(qm.Fields) {
		var err error
		if available, err = d.availableFields(ctx, conn); err != nil {
			return backend.ErrDataResponse(backend.StatusBadGateway, err.Error())
		}
	}
	fields, err := expandFields(qm.Fields, available)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, err.Error())
	}
	if len(fields) == 0 {
		return backend.DataResponse{}
	}

	meta, err := d.metadata(ctx, conn, fields)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadGateway, err.Error())
	}

	// Fields already streaming have their latest point in the manager;
	// fetch the rest from the CDS.
	latest := map[string]cds.Point{}
	var missing []string
	for _, f := range fields {
		if p, ok := d.mgr.Latest(f); ok {
			latest[f] = p
		} else {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		c, err := conn.get(ctx)
		if err != nil {
			return backend.ErrDataResponse(backend.StatusBadGateway, err.Error())
		}
		got, err := c.Latest(ctx, missing)
		if err != nil {
			return backend.ErrDataResponse(backend.StatusBadGateway, err.Error())
		}
		for f, p := range got {
			latest[f] = p
		}
	}

	var resp backend.DataResponse
	var absent []string
	for _, f := range fields {
		p, ok := latest[f]
		kind := kindNumber
		if ok {
			kind = inferKind(p.Value)
		}
		frame := newFieldFrame(f, kind, meta[f])
		if ok {
			appendPoint(frame, kind, p)
		} else {
			absent = append(absent, f)
		}
		if qm.streaming() {
			ch := live.Channel{
				Scope:     live.ScopeDatasource,
				Namespace: d.uid,
				Path:      streamPath(f, qm.MinIntervalMs),
			}
			frame.SetMeta(&data.FrameMeta{Channel: ch.String()})
		}
		resp.Frames = append(resp.Frames, frame)
	}
	if len(absent) > 0 && len(resp.Frames) > 0 {
		resp.Frames[0].AppendNotices(data.Notice{
			Severity: data.NoticeSeverityWarning,
			Text:     "No cached value yet for: " + strings.Join(absent, ", "),
		})
	}
	return resp
}

// availableFields returns the CDS field list, cached briefly.
func (d *Datasource) availableFields(ctx context.Context, conn *lazyConn) ([]string, error) {
	d.fieldsMu.Lock()
	defer d.fieldsMu.Unlock()
	if d.fields != nil && time.Since(d.fieldsAt) < fieldsCacheTTL {
		return d.fields, nil
	}
	c, err := conn.get(ctx)
	if err != nil {
		return nil, err
	}
	fields, err := c.Fields(ctx)
	if err != nil {
		return nil, err
	}
	sort.Strings(fields)
	d.fields, d.fieldsAt = fields, time.Now()
	return fields, nil
}

// metadata returns describe() metadata for fields, cached.
func (d *Datasource) metadata(ctx context.Context, conn *lazyConn, fields []string) (map[string]*cds.Metadata, error) {
	d.metaMu.Lock()
	defer d.metaMu.Unlock()
	if time.Since(d.metaAt) > metaCacheTTL {
		d.meta = map[string]*cds.Metadata{}
		d.metaAt = time.Now()
	}
	var unknown []string
	for _, f := range fields {
		if _, ok := d.meta[f]; !ok {
			unknown = append(unknown, f)
		}
	}
	if len(unknown) > 0 {
		c, err := conn.get(ctx)
		if err != nil {
			return nil, err
		}
		got, err := c.Describe(ctx, unknown)
		if err != nil {
			return nil, err
		}
		for _, f := range unknown {
			if m, ok := got[f]; ok {
				d.meta[f] = &m
			} else {
				d.meta[f] = nil
			}
		}
	}
	out := make(map[string]*cds.Metadata, len(fields))
	for _, f := range fields {
		out[f] = d.meta[f]
	}
	return out, nil
}

// SubscribeStream authorizes a subscription to a field stream. Initial
// data comes from the QueryData frame the frontend already holds.
func (d *Datasource) SubscribeStream(_ context.Context, req *backend.SubscribeStreamRequest) (*backend.SubscribeStreamResponse, error) {
	if _, _, err := parseStreamPath(req.Path); err != nil {
		return &backend.SubscribeStreamResponse{Status: backend.SubscribeStreamStatusNotFound}, nil
	}
	return &backend.SubscribeStreamResponse{Status: backend.SubscribeStreamStatusOK}, nil
}

// PublishStream is not supported: the plugin is read-only.
func (d *Datasource) PublishStream(_ context.Context, _ *backend.PublishStreamRequest) (*backend.PublishStreamResponse, error) {
	return &backend.PublishStreamResponse{Status: backend.PublishStreamStatusPermissionDenied}, nil
}

// RunStream forwards one CDS field to a Grafana Live channel. Grafana runs a
// single RunStream per channel no matter how many panels are subscribed,
// and cancels ctx when the last one leaves.
func (d *Datasource) RunStream(ctx context.Context, req *backend.RunStreamRequest, sender *backend.StreamSender) error {
	field, minIntervalMs, err := parseStreamPath(req.Path)
	if err != nil {
		return err
	}
	sub, err := d.mgr.Subscribe(field)
	if err != nil {
		return err
	}
	defer sub.Close()

	var (
		kind      valueKind
		kindKnown bool
		pending   *cds.Point
		tick      <-chan time.Time
	)
	if minIntervalMs > 0 {
		t := time.NewTicker(time.Duration(minIntervalMs) * time.Millisecond)
		defer t.Stop()
		tick = t.C
	}

	send := func(pts []cds.Point) error {
		if !kindKnown {
			for _, p := range pts {
				if p.Value != nil {
					kind, kindKnown = inferKind(p.Value), true
					break
				}
			}
		}
		frame := newFieldFrame(field, kind, nil)
		for _, p := range pts {
			appendPoint(frame, kind, p)
		}
		return sender.SendFrame(frame, data.IncludeDataOnly)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case pts, ok := <-sub.C:
			if !ok {
				return errors.New("CDS stream closed")
			}
			if tick == nil {
				if err := send(pts); err != nil {
					return err
				}
			} else {
				last := pts[len(pts)-1]
				pending = &last
			}
		case <-tick:
			if pending != nil {
				if err := send([]cds.Point{*pending}); err != nil {
					return err
				}
				pending = nil
			}
		}
	}
}

// CallResource serves GET /fields for the query editor's field picker.
func (d *Datasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	reply := func(status int, body any) error {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		return sender.Send(&backend.CallResourceResponse{
			Status:  status,
			Headers: map[string][]string{"Content-Type": {"application/json"}},
			Body:    b,
		})
	}
	if req.Path != "fields" || req.Method != http.MethodGet {
		return reply(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	conn := &lazyConn{opts: d.opts, settings: d.settings}
	defer conn.close()
	fields, err := d.availableFields(ctx, conn)
	if err != nil {
		return reply(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return reply(http.StatusOK, map[string]any{"fields": fields})
}

// connectErrorMessage explains a failed connection in terms of the
// configured connection mode.
func connectErrorMessage(err error, s *models.PluginSettings) string {
	var hs *cds.HandshakeError
	if !errors.As(err, &hs) {
		return fmt.Sprintf("Could not connect to the %s: %v", s.ModeName(), err)
	}
	switch {
	case hs.IsAuthFailure() && s.Mode() == models.ConnectionProxy:
		return fmt.Sprintf("Authentication failed (HTTP %d): the API key is invalid, revoked or expired, "+
			"or lacks the WEBSOCKET permission on /api/v1/ws/data-server. Check the key on the "+
			"API Keys page of the OpenRVDAS web UI.", hs.StatusCode)
	case hs.IsAuthFailure():
		return fmt.Sprintf("%s refused the connection (HTTP %d). If this is the OpenRVDAS API, "+
			"set Connection to \"OpenRVDAS API\" and enter an API key.", hs.URL, hs.StatusCode)
	case hs.StatusCode == http.StatusNotFound && s.Mode() == models.ConnectionProxy:
		return fmt.Sprintf("No WebSocket endpoint at %s (HTTP 404). The URL should end in "+
			"/api/v1/ws/data-server; OpenRVDAS installations without the API proxy need a Direct connection.", hs.URL)
	default:
		return hs.Error()
	}
}

// CheckHealth connects and lists the available fields.
func (d *Datasource) CheckHealth(ctx context.Context, _ *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	fail := func(msg string) (*backend.CheckHealthResult, error) {
		return &backend.CheckHealthResult{Status: backend.HealthStatusError, Message: msg}, nil
	}
	if err := d.settings.Validate(); err != nil {
		return fail(err.Error())
	}
	u, err := cds.NormalizeURL(d.settings.URL)
	if err != nil {
		return fail(err.Error())
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	conn, err := cds.Dial(ctx, d.opts)
	if err != nil {
		return fail(connectErrorMessage(err, d.settings))
	}
	defer conn.Close()
	fields, err := conn.Fields(ctx)
	if err != nil {
		return fail(fmt.Sprintf("Connected to the %s at %s, but the fields request failed: %v",
			d.settings.ModeName(), u, err))
	}
	msg := fmt.Sprintf("Connected to the %s at %s: %d fields available (streaming every %s)",
		d.settings.ModeName(), u, len(fields), d.settings.PollInterval())
	if d.settings.Mode() == models.ConnectionDirect {
		msg += ". Direct connections are unauthenticated and are scheduled for removal in OpenRVDAS v3."
	}
	return &backend.CheckHealthResult{Status: backend.HealthStatusOk, Message: msg}, nil
}
