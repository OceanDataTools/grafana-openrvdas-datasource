// Package cdssim is a small stand-in for an OpenRVDAS CachedDataServer (CDS)
// with simulated ship data. It exists so that `docker compose up` works from
// a fresh checkout, for CI e2e tests and Grafana plugin reviewers, without a
// real OpenRVDAS install. It is a test tool, not part of the plugin.
//
// It speaks the subset of the CDS WebSocket protocol the plugin uses (see
// pkg/cds/client.go). With an API key set it also mimics the OpenRVDAS API
// proxy's handshake check: X-API-Key must match, and ?api_key= is refused.
package cdssim

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// APIKeyHeader is the handshake header the OpenRVDAS API proxy checks.
const APIKeyHeader = "X-API-Key"

// tick is how often fields are sampled; it bounds the fastest field rate.
const tick = 100 * time.Millisecond

// Field is one simulated field.
type Field struct {
	Name        string
	Units       string
	Description string
	// Period between samples; a multiple of tick.
	Period time.Duration
	// Value returns the field's value at t (seconds since the epoch).
	Value func(t float64) any
}

type point struct {
	t float64
	v any
}

// Sim holds the simulated cache and serves the CDS protocol over WebSocket.
type Sim struct {
	// APIKey, if set, is required in the X-API-Key handshake header.
	APIKey string
	// BackSeconds is how much history each field keeps.
	BackSeconds float64
	Logger      *log.Logger

	fields []Field
	byName map[string]Field

	mu    sync.Mutex
	cache map[string][]point
	next  map[string]time.Time
}

// New returns a simulator for fields. Call Run to start generating data.
func New(fields []Field) *Sim {
	s := &Sim{
		BackSeconds: 600,
		fields:      fields,
		byName:      make(map[string]Field, len(fields)),
		cache:       make(map[string][]point, len(fields)),
		next:        make(map[string]time.Time, len(fields)),
	}
	for _, f := range fields {
		s.byName[f.Name] = f
	}
	return s
}

// Run samples every field on its period until ctx is done.
func (s *Sim) Run(ctx context.Context) {
	s.Sample(time.Now())
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.Sample(now)
		}
	}
}

// Sample records a value for every field that is due at now.
func (s *Sim) Sample(now time.Time) {
	ts := float64(now.UnixNano()) / 1e9
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.fields {
		if n, ok := s.next[f.Name]; ok && now.Before(n) {
			continue
		}
		s.next[f.Name] = now.Add(f.Period - tick/2)
		pts := append(s.cache[f.Name], point{ts, f.Value(ts)})
		cut := 0
		for cut < len(pts) && pts[cut].t < ts-s.BackSeconds {
			cut++
		}
		s.cache[f.Name] = pts[cut:]
	}
}

func (s *Sim) logf(format string, args ...any) {
	if s.Logger != nil {
		s.Logger.Printf(format, args...)
	}
}

// ServeHTTP accepts a CDS WebSocket connection.
func (s *Sim) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.APIKey != "" && (r.Header.Get(APIKeyHeader) != s.APIKey || r.URL.Query().Has("api_key")) {
		s.logf("%s: refused, missing or wrong %s", r.RemoteAddr, APIKeyHeader)
		w.WriteHeader(http.StatusForbidden)
		return
	}
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = ws.CloseNow() }()
	s.logf("%s: connected", r.RemoteAddr)
	err = s.serve(r.Context(), ws)
	s.logf("%s: disconnected: %v", r.RemoteAddr, err)
}

type request struct {
	Type     string          `json:"type"`
	Interval float64         `json:"interval"`
	Fields   json.RawMessage `json:"fields"`
}

func (s *Sim) serve(ctx context.Context, ws *websocket.Conn) error {
	// sent is the timestamp of the last point sent per subscribed field.
	var sent map[string]float64
	interval := time.Second
	for {
		var req request
		if err := wsjson.Read(ctx, ws, &req); err != nil {
			return err
		}
		var resp map[string]any
		switch req.Type {
		case "fields":
			resp = map[string]any{"type": "fields", "status": 200, "data": s.names()}
		case "describe":
			var names []string
			_ = json.Unmarshal(req.Fields, &names)
			resp = map[string]any{"type": "describe", "status": 200, "data": s.describe(names)}
		case "subscribe":
			var specs map[string]struct{ Seconds float64 }
			if err := json.Unmarshal(req.Fields, &specs); err != nil {
				resp = map[string]any{"type": "subscribe", "status": 400, "error": "bad subscribe request: " + err.Error()}
				break
			}
			interval = time.Duration(req.Interval * float64(time.Second))
			sent = s.subscribe(specs)
			resp = map[string]any{"type": "subscribe", "status": 200}
		case "ready":
			start := time.Now()
			if len(sent) == 0 {
				resp = map[string]any{"type": "ready", "status": 400, "error": "client ready, but no matching fields found (yet)"}
			} else {
				resp = map[string]any{"type": "data", "status": 200, "data": s.since(sent)}
			}
			if err := wsjson.Write(ctx, ws, resp); err != nil {
				return err
			}
			// Like the CDS, wait out the rest of the interval before
			// reading the next request.
			wait := time.Until(start.Add(interval))
			if len(sent) == 0 {
				wait = max(wait, time.Second)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
			continue
		default:
			resp = map[string]any{"type": req.Type, "status": 400, "error": "unrecognized request type"}
		}
		if err := wsjson.Write(ctx, ws, resp); err != nil {
			return err
		}
	}
}

func (s *Sim) names() []string {
	out := make([]string, 0, len(s.fields))
	for _, f := range s.fields {
		out = append(out, f.Name)
	}
	sort.Strings(out)
	return out
}

func (s *Sim) describe(names []string) map[string]map[string]string {
	if len(names) == 0 {
		names = s.names()
	}
	out := map[string]map[string]string{}
	for _, n := range names {
		if f, ok := s.byName[n]; ok {
			out[n] = map[string]string{"units": f.Units, "description": f.Description}
		}
	}
	return out
}

// subscribe returns the starting "last sent" timestamp for each known field:
// seconds -1 sends the latest value, 0 only new values, N the last N seconds.
// Unknown fields are ignored; every simulated field exists from the start.
func (s *Sim) subscribe(specs map[string]struct{ Seconds float64 }) map[string]float64 {
	now := float64(time.Now().UnixNano()) / 1e9
	s.mu.Lock()
	defer s.mu.Unlock()
	sent := map[string]float64{}
	for name, spec := range specs {
		if _, ok := s.byName[name]; !ok {
			continue
		}
		pts := s.cache[name]
		switch {
		case spec.Seconds < 0 && len(pts) >= 2:
			sent[name] = pts[len(pts)-2].t
		case spec.Seconds < 0:
			sent[name] = math.Inf(-1)
		case spec.Seconds == 0 && len(pts) > 0:
			sent[name] = pts[len(pts)-1].t
		case spec.Seconds == 0:
			sent[name] = now
		default:
			sent[name] = now - spec.Seconds
		}
	}
	return sent
}

// since returns each field's points newer than sent and advances sent.
func (s *Sim) since(sent map[string]float64) map[string][][2]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string][][2]any{}
	for name, last := range sent {
		for _, p := range s.cache[name] {
			if p.t > last {
				out[name] = append(out[name], [2]any{p.t, p.v})
				sent[name] = p.t
			}
		}
	}
	return out
}
