package cds

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}
func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

// fakeCDS mimics the parts of the CachedDataServer protocol the plugin
// uses. A subscribe with seconds > 0 resends a field's entire history,
// which exercises the manager's de-duplication.
type fakeCDS struct {
	mu          sync.Mutex
	cache       map[string][]Point
	connects    int
	subscribes  int
	open        int
	interval    time.Duration
	lastSubSpec map[string]FieldSpec
}

func newFakeCDS() *fakeCDS {
	return &fakeCDS{cache: map[string][]Point{}, interval: 5 * time.Millisecond}
}

func (f *fakeCDS) push(field string, t float64, v any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cache[field] = append(f.cache[field], Point{Time: t, Value: v})
}

func (f *fakeCDS) stats() (connects, subscribes, open int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connects, f.subscribes, f.open
}

func (f *fakeCDS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = ws.CloseNow() }()
	f.mu.Lock()
	f.connects++
	f.open++
	f.mu.Unlock()
	defer func() { f.mu.Lock(); f.open--; f.mu.Unlock() }()

	ctx := r.Context()
	sent := map[string]float64{}
	for {
		var req map[string]any
		if err := wsjson.Read(ctx, ws, &req); err != nil {
			return
		}
		switch req["type"] {
		case "fields":
			f.mu.Lock()
			var names []string
			for k := range f.cache {
				names = append(names, k)
			}
			f.mu.Unlock()
			_ = wsjson.Write(ctx, ws, map[string]any{"type": "fields", "status": 200, "data": names})
		case "subscribe":
			raw, _ := json.Marshal(req["fields"])
			var specs map[string]FieldSpec
			_ = json.Unmarshal(raw, &specs)
			f.mu.Lock()
			f.subscribes++
			f.lastSubSpec = specs
			sent = map[string]float64{}
			for name, s := range specs {
				pts := f.cache[name]
				switch {
				case s.Seconds < 0 && len(pts) >= 2:
					sent[name] = pts[len(pts)-2].Time
				case s.Seconds < 0:
					sent[name] = 0
				case s.Seconds == 0 && len(pts) > 0:
					sent[name] = pts[len(pts)-1].Time
				default:
					sent[name] = 0
				}
			}
			f.mu.Unlock()
			_ = wsjson.Write(ctx, ws, map[string]any{"type": "subscribe", "status": 200})
		case "ready":
			out := map[string][][2]any{}
			f.mu.Lock()
			for name, last := range sent {
				for _, p := range f.cache[name] {
					if p.Time > last {
						out[name] = append(out[name], [2]any{p.Time, p.Value})
						sent[name] = p.Time
					}
				}
			}
			f.mu.Unlock()
			_ = wsjson.Write(ctx, ws, map[string]any{"type": "data", "status": 200, "data": out})
			time.Sleep(f.interval)
		}
	}
}

func wsURL(s *httptest.Server) string { return "ws" + s.URL[len("http"):] }

func recvPoints(t *testing.T, s *Subscription, n int) []Point {
	t.Helper()
	var got []Point
	deadline := time.After(2 * time.Second)
	for len(got) < n {
		select {
		case pts, ok := <-s.C:
			if !ok {
				t.Fatalf("subscription closed after %d points", len(got))
			}
			got = append(got, pts...)
		case <-deadline:
			t.Fatalf("timed out with %d/%d points: %v", len(got), n, got)
		}
	}
	return got
}

func expectNoPoints(t *testing.T, s *Subscription, d time.Duration) {
	t.Helper()
	select {
	case pts := <-s.C:
		t.Fatalf("unexpected points %v", pts)
	case <-time.After(d):
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestManagerStreamsAndDedupes(t *testing.T) {
	fake := newFakeCDS()
	fake.push("A", 1, 1.0)
	fake.push("A", 2, 2.0)
	srv := httptest.NewServer(fake)
	defer srv.Close()

	m := NewManager(Options{URL: wsURL(srv)}, 5*time.Millisecond, nopLogger{})
	defer m.Close()

	a, err := m.Subscribe("A")
	if err != nil {
		t.Fatal(err)
	}
	// New field starts from its most recent value.
	got := recvPoints(t, a, 1)
	if got[0].Time != 2 {
		t.Fatalf("expected latest point first, got %v", got)
	}

	fake.push("A", 3, 3.0)
	if got = recvPoints(t, a, 1); got[0].Time != 3 {
		t.Fatalf("expected new point, got %v", got)
	}

	// Adding a field re-subscribes; A gets an overlap request and the
	// fake resends A's whole history, none of which may be redelivered.
	fake.push("B", 10, "x")
	b, err := m.Subscribe("B")
	if err != nil {
		t.Fatal(err)
	}
	if got = recvPoints(t, b, 1); got[0].Value != "x" {
		t.Fatalf("unexpected B points %v", got)
	}
	waitFor(t, func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return fake.lastSubSpec["A"].Seconds == resubscribeOverlap && fake.lastSubSpec["B"].Seconds == -1
	})
	expectNoPoints(t, a, 50*time.Millisecond)
	if p, ok := m.Latest("A"); !ok || p.Time != 3 {
		t.Fatalf("Latest(A) = %v, %v", p, ok)
	}

	// One connection serves both fields.
	if c, _, _ := fake.stats(); c != 1 {
		t.Fatalf("expected 1 connection, got %d", c)
	}

	// Closing all subscriptions closes the connection.
	a.Close()
	b.Close()
	if _, ok := <-a.C; ok {
		t.Fatal("closed subscription channel should be closed")
	}
	waitFor(t, func() bool { _, _, open := fake.stats(); return open == 0 })

	// A new subscription reconnects.
	c, err := m.Subscribe("A")
	if err != nil {
		t.Fatal(err)
	}
	recvPoints(t, c, 1)
	if n, _, _ := fake.stats(); n != 2 {
		t.Fatalf("expected reconnect, got %d connections", n)
	}
}

func TestManagerReconnects(t *testing.T) {
	fake := newFakeCDS()
	fake.push("A", 1, 1.0)
	srv := httptest.NewServer(fake)

	m := NewManager(Options{URL: wsURL(srv)}, 5*time.Millisecond, nopLogger{})
	defer m.Close()
	a, _ := m.Subscribe("A")
	recvPoints(t, a, 1)

	// Kill the server's connections; the manager must reconnect and
	// resume without redelivering point 1.
	srv.CloseClientConnections()
	fake.push("A", 2, 2.0)
	got := recvPoints(t, a, 1)
	if got[0].Time != 2 {
		t.Fatalf("expected only the new point after reconnect, got %v", got)
	}
	srv.Close()
}

func TestManagerCloseClosesSubscriptions(t *testing.T) {
	fake := newFakeCDS()
	srv := httptest.NewServer(fake)
	defer srv.Close()
	m := NewManager(Options{URL: wsURL(srv)}, 5*time.Millisecond, nopLogger{})
	a, _ := m.Subscribe("A")
	m.Close()
	select {
	case _, ok := <-a.C:
		if ok {
			t.Fatal("expected closed channel")
		}
	case <-time.After(time.Second):
		t.Fatal("subscription not closed")
	}
	a.Close() // must not panic
	if _, err := m.Subscribe("A"); err == nil {
		t.Fatal("subscribe after close should fail")
	}
}

func TestClientOneShots(t *testing.T) {
	fake := newFakeCDS()
	fake.push("A", 1, 1.0)
	fake.push("A", 2, 2.5)
	fake.push("S", 1, "W")
	srv := httptest.NewServer(fake)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := Dial(ctx, Options{URL: srv.URL}) // http:// is normalized to ws://
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	fields, err := c.Fields(ctx)
	if err != nil || len(fields) != 2 {
		t.Fatalf("Fields() = %v, %v", fields, err)
	}
	latest, err := c.Latest(ctx, []string{"A", "S", "Missing"})
	if err != nil {
		t.Fatal(err)
	}
	if latest["A"].Value != 2.5 || latest["S"].Value != "W" {
		t.Fatalf("unexpected latest %v", latest)
	}
	if _, ok := latest["Missing"]; ok {
		t.Fatal("missing field should be absent")
	}
}

func TestNormalizeURL(t *testing.T) {
	for in, want := range map[string]string{
		"host:8766":          "ws://host:8766",
		"ws://host:8766":     "ws://host:8766",
		"wss://host":         "wss://host",
		"http://host:8766/":  "ws://host:8766/",
		"https://host:8766/": "wss://host:8766/",
	} {
		if got, err := NormalizeURL(in); err != nil || got != want {
			t.Errorf("NormalizeURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "  ", "ftp://host"} {
		if _, err := NormalizeURL(bad); err == nil {
			t.Errorf("NormalizeURL(%q) should fail", bad)
		}
	}
}
