package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// fakeServer answers "fields" after checking the API key the same way the
// OpenRVDAS API proxy does. An empty key accepts anything (direct CDS).
func fakeServer(t *testing.T, key string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if key != "" && r.Header.Get("X-API-Key") != key {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		for {
			var req map[string]any
			if err := wsjson.Read(r.Context(), ws, &req); err != nil {
				return
			}
			_ = wsjson.Write(r.Context(), ws, map[string]any{"type": "fields", "status": 200, "data": []string{"A", "B"}})
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func health(t *testing.T, jsonData string, apiKey string) *backend.CheckHealthResult {
	t.Helper()
	secure := map[string]string{}
	if apiKey != "" {
		secure["apiKey"] = apiKey
	}
	inst, err := NewDatasource(context.Background(), backend.DataSourceInstanceSettings{
		UID: "test", JSONData: []byte(jsonData), DecryptedSecureJSONData: secure,
	})
	if err != nil {
		t.Fatal(err)
	}
	ds := inst.(*Datasource)
	defer ds.Dispose()
	res, err := ds.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func expectHealth(t *testing.T, res *backend.CheckHealthResult, status backend.HealthStatus, contains string) {
	t.Helper()
	if res.Status != status || !strings.Contains(res.Message, contains) {
		t.Errorf("got %v %q; want %v containing %q", res.Status, res.Message, status, contains)
	}
}

func TestCheckHealth(t *testing.T) {
	proxy := "ws" + strings.TrimPrefix(fakeServer(t, "good").URL, "http")
	direct := "ws" + strings.TrimPrefix(fakeServer(t, "").URL, "http")

	expectHealth(t, health(t, `{"connectionMode":"proxy","url":"`+proxy+`"}`, "good"),
		backend.HealthStatusOk, "Connected to the OpenRVDAS API")
	expectHealth(t, health(t, `{"connectionMode":"proxy","url":"`+proxy+`"}`, "bad"),
		backend.HealthStatusError, "Authentication failed (HTTP 403)")
	expectHealth(t, health(t, `{"connectionMode":"proxy","url":"`+proxy+`"}`, ""),
		backend.HealthStatusError, "API key is required")
	expectHealth(t, health(t, `{"url":"`+direct+`"}`, ""),
		backend.HealthStatusOk, "Direct connections are unauthenticated")
	// Direct mode against the proxy: suggest switching modes.
	expectHealth(t, health(t, `{"connectionMode":"direct","url":"`+proxy+`"}`, "good"),
		backend.HealthStatusError, `set Connection to "OpenRVDAS API"`)
	expectHealth(t, health(t, `{}`, ""), backend.HealthStatusError, "URL is not configured")
}
