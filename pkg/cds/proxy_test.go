package cds

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// fakeProxy mimics the OpenRVDAS API proxy: it rejects the handshake with
// 403 unless X-API-Key matches, then relays to the fake CDS.
func fakeProxy(key string, cds http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ws/data-server" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get(APIKeyHeader) != key || r.URL.Query().Get("api_key") != "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		cds.ServeHTTP(w, r)
	})
}

func TestDialSendsAPIKeyHeader(t *testing.T) {
	fake := newFakeCDS()
	fake.push("A", 1, 1.0)
	srv := httptest.NewServer(fakeProxy("secret", fake))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c, err := Dial(ctx, Options{URL: wsURL(srv) + "/api/v1/ws/data-server", APIKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if fields, err := c.Fields(ctx); err != nil || len(fields) != 1 {
		t.Fatalf("Fields() = %v, %v", fields, err)
	}

	// The manager uses the same dial path.
	m := NewManager(Options{URL: wsURL(srv) + "/api/v1/ws/data-server", APIKey: "secret"}, 5*time.Millisecond, nopLogger{})
	defer m.Close()
	s, _ := m.Subscribe("A")
	recvPoints(t, s, 1)
}

func TestDialHandshakeErrors(t *testing.T) {
	srv := httptest.NewServer(fakeProxy("secret", newFakeCDS()))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	for _, tc := range []struct {
		name, path, key string
		status          int
		auth            bool
	}{
		{"bad key", "/api/v1/ws/data-server", "wrong", http.StatusForbidden, true},
		{"no key", "/api/v1/ws/data-server", "", http.StatusForbidden, true},
		{"wrong path", "/nope", "secret", http.StatusNotFound, false},
	} {
		_, err := Dial(ctx, Options{URL: wsURL(srv) + tc.path, APIKey: tc.key})
		var hs *HandshakeError
		if !errors.As(err, &hs) {
			t.Fatalf("%s: expected HandshakeError, got %v", tc.name, err)
		}
		if hs.StatusCode != tc.status || hs.IsAuthFailure() != tc.auth {
			t.Errorf("%s: status %d auth %v", tc.name, hs.StatusCode, hs.IsAuthFailure())
		}
	}

	// A refused TCP connection is not a handshake error.
	dead := httptest.NewServer(http.NotFoundHandler())
	u := wsURL(dead)
	dead.Close()
	_, err := Dial(ctx, Options{URL: u})
	var hs *HandshakeError
	if err == nil || errors.As(err, &hs) {
		t.Fatalf("expected a plain connection error, got %v", err)
	}
}

// The proxy reports an unreachable CDS as
// {"type":"error","status":503,"message":...} and closes.
func TestProxyErrorMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		var req map[string]any
		_ = wsjson.Read(r.Context(), ws, &req)
		_ = wsjson.Write(r.Context(), ws, map[string]any{
			"type": "error", "status": 503, "message": "CachedDataServer unavailable",
		})
		_ = ws.Close(websocket.StatusNormalClosure, "")
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := Dial(ctx, Options{URL: wsURL(srv)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, err = c.Fields(ctx)
	if err == nil || !strings.Contains(err.Error(), "CachedDataServer unavailable") {
		t.Fatalf("expected the proxy's message, got %v", err)
	}
}
