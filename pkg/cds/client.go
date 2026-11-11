// Package cds is a client for the OpenRVDAS CachedDataServer (CDS) WebSocket
// API. See openrvdas/server/cached_data_server.py for the server side.
//
// Protocol summary (all messages are JSON objects):
//
//	-> {"type":"fields"}
//	<- {"type":"fields","status":200,"data":["f1","f2",...]}
//	-> {"type":"describe","fields":["f1",...]}
//	<- {"type":"describe","status":200,"data":{"f1":{"units":"...","description":"..."},...}}
//	-> {"type":"subscribe","interval":0.1,"fields":{"f1":{"seconds":-1},...}}
//	<- {"type":"subscribe","status":200}
//	-> {"type":"ready"}
//	<- {"type":"data","status":200,"data":{"f1":[[ts,value],...],...}}
//
// After answering a "ready", the server sleeps for whatever is left of the
// subscription interval before reading the client's next message, so a
// client that sends "ready" immediately after each data message receives
// batches at roughly the requested interval.
package cds

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// readLimit caps a single CDS message. status:* fields can carry large JSON
// blobs and a backlog of many fields can be large, so this is generous.
const readLimit = 16 << 20

// Point is one timestamped value from the CDS cache. Value is the decoded
// JSON value: float64, string, bool, nil, map[string]any or []any.
type Point struct {
	Time  float64
	Value any
}

// UnmarshalJSON decodes the CDS [timestamp, value] pair encoding.
func (p *Point) UnmarshalJSON(b []byte) error {
	var pair []json.RawMessage
	if err := json.Unmarshal(b, &pair); err != nil {
		return err
	}
	if len(pair) != 2 {
		return fmt.Errorf("expected [timestamp, value] pair, got %d elements", len(pair))
	}
	if err := json.Unmarshal(pair[0], &p.Time); err != nil {
		return fmt.Errorf("bad timestamp: %w", err)
	}
	return json.Unmarshal(pair[1], &p.Value)
}

// Timestamp returns the point's time as a time.Time.
func (p Point) Timestamp() time.Time {
	sec := int64(p.Time)
	nsec := int64((p.Time - float64(sec)) * 1e9)
	return time.Unix(sec, nsec).UTC()
}

// Metadata is the per-field metadata returned by a "describe" request.
// Only the keys the plugin uses are typed; everything else is in Extra.
type Metadata struct {
	Units       string
	Description string
	Extra       map[string]any
}

// Message is a raw message from the server.
type Message struct {
	Type   string          `json:"type"`
	Status int             `json:"status"`
	Error  string          `json:"error"`
	Data   json.RawMessage `json:"data"`
}

// FieldSpec is the per-field part of a subscribe request.
type FieldSpec struct {
	// Seconds of back data: 0 = only new data, -1 = most recent value then
	// new data, N = N seconds of back data then new data.
	Seconds float64 `json:"seconds"`
}

// Options configures a connection to a CDS.
type Options struct {
	// URL is the CDS WebSocket URL, e.g. ws://host:8766.
	URL string
	// TLSSkipVerify disables certificate verification for wss:// URLs.
	TLSSkipVerify bool
}

// Conn is a single WebSocket connection to a CDS. It is not safe for
// concurrent use; callers serialize access.
type Conn struct {
	ws *websocket.Conn
}

// NormalizeURL accepts host, host:port, http(s):// or ws(s):// forms and
// returns a ws(s):// URL. The CDS default port 8766 is not assumed; a bare
// host gets no port added.
func NormalizeURL(raw string) (string, error) {
	u := strings.TrimSpace(raw)
	if u == "" {
		return "", errors.New("CDS URL is not configured")
	}
	switch {
	case strings.HasPrefix(u, "ws://"), strings.HasPrefix(u, "wss://"):
	case strings.HasPrefix(u, "http://"):
		u = "ws://" + strings.TrimPrefix(u, "http://")
	case strings.HasPrefix(u, "https://"):
		u = "wss://" + strings.TrimPrefix(u, "https://")
	case strings.Contains(u, "://"):
		return "", fmt.Errorf("unsupported URL scheme in %q (use ws:// or wss://)", raw)
	default:
		u = "ws://" + u
	}
	return u, nil
}

// Dial opens a connection to the CDS.
func Dial(ctx context.Context, opts Options) (*Conn, error) {
	u, err := NormalizeURL(opts.URL)
	if err != nil {
		return nil, err
	}
	dialOpts := &websocket.DialOptions{}
	if opts.TLSSkipVerify {
		dialOpts.HTTPClient = &http.Client{
			Transport: &http.Transport{
				//nolint:gosec // explicitly requested by the data source configuration
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
	}
	ws, _, err := websocket.Dial(ctx, u, dialOpts)
	if err != nil {
		return nil, fmt.Errorf("connect to CDS at %s: %w", u, err)
	}
	ws.SetReadLimit(readLimit)
	return &Conn{ws: ws}, nil
}

// Close closes the connection.
func (c *Conn) Close() error {
	return c.ws.Close(websocket.StatusNormalClosure, "")
}

// Send writes one request.
func (c *Conn) Send(ctx context.Context, req any) error {
	return wsjson.Write(ctx, c.ws, req)
}

// Recv reads one message.
func (c *Conn) Recv(ctx context.Context) (*Message, error) {
	var m Message
	if err := wsjson.Read(ctx, c.ws, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// request sends req and returns the next message whose type matches
// wantType, skipping anything else (e.g. stray data messages). Errors come
// back either untyped or typed with the request's own type (a failed
// "ready" is answered with {"type":"ready","status":400}), so those are
// accepted too.
func (c *Conn) request(ctx context.Context, req map[string]any, wantType string) (*Message, error) {
	if err := c.Send(ctx, req); err != nil {
		return nil, err
	}
	reqType, _ := req["type"].(string)
	for {
		m, err := c.Recv(ctx)
		if err != nil {
			return nil, err
		}
		if m.Type != wantType && m.Type != reqType && m.Type != "" {
			continue
		}
		if m.Status != 200 {
			return nil, fmt.Errorf("CDS %s request failed (status %d): %s", wantType, m.Status, m.Error)
		}
		return m, nil
	}
}

// Fields returns the names of all fields currently in the CDS cache.
func (c *Conn) Fields(ctx context.Context) ([]string, error) {
	m, err := c.request(ctx, map[string]any{"type": "fields"}, "fields")
	if err != nil {
		return nil, err
	}
	var fields []string
	if err := json.Unmarshal(m.Data, &fields); err != nil {
		return nil, fmt.Errorf("decode fields response: %w", err)
	}
	return fields, nil
}

// Describe returns metadata for the given fields. Fields without metadata
// are omitted from the result.
func (c *Conn) Describe(ctx context.Context, fields []string) (map[string]Metadata, error) {
	m, err := c.request(ctx, map[string]any{"type": "describe", "fields": fields}, "describe")
	if err != nil {
		return nil, err
	}
	var raw map[string]map[string]any
	if err := json.Unmarshal(m.Data, &raw); err != nil {
		return nil, fmt.Errorf("decode describe response: %w", err)
	}
	out := make(map[string]Metadata, len(raw))
	for name, md := range raw {
		if len(md) == 0 {
			continue
		}
		meta := Metadata{Extra: md}
		if s, ok := md["units"].(string); ok {
			meta.Units = s
		}
		if s, ok := md["description"].(string); ok {
			meta.Description = s
		}
		out[name] = meta
	}
	return out, nil
}

// Subscribe sends a subscribe request and waits for its acknowledgement.
func (c *Conn) Subscribe(ctx context.Context, interval time.Duration, fields map[string]FieldSpec) error {
	req := map[string]any{
		"type":     "subscribe",
		"interval": interval.Seconds(),
		"fields":   fields,
	}
	_, err := c.request(ctx, req, "subscribe")
	return err
}

// DecodeData decodes the payload of a "data" message in the default
// field_dict format.
func DecodeData(raw json.RawMessage) (map[string][]Point, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var out map[string][]Point
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode data message: %w", err)
	}
	return out, nil
}

// Latest fetches the most recent cached value of each field with a one-shot
// subscription. Fields with no cached value are absent from the result.
func (c *Conn) Latest(ctx context.Context, fields []string) (map[string]Point, error) {
	specs := make(map[string]FieldSpec, len(fields))
	for _, f := range fields {
		specs[f] = FieldSpec{Seconds: -1}
	}
	if err := c.Subscribe(ctx, 0, specs); err != nil {
		return nil, err
	}
	m, err := c.request(ctx, map[string]any{"type": "ready"}, "data")
	if err != nil {
		// A "ready" answered with status 400 means none of the fields exist.
		if strings.Contains(err.Error(), "no matching fields") {
			return map[string]Point{}, nil
		}
		return nil, err
	}
	data, err := DecodeData(m.Data)
	if err != nil {
		return nil, err
	}
	out := make(map[string]Point, len(data))
	for name, pts := range data {
		if len(pts) > 0 {
			out[name] = pts[len(pts)-1]
		}
	}
	return out, nil
}
