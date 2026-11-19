package cdssim

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/OceanDataTools/grafana-openrvdas-datasource/pkg/cds"
)

type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}
func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

// start runs a simulator behind an httptest server and returns its ws:// URL.
func start(t *testing.T, apiKey string) string {
	t.Helper()
	sim := New(ShipFields())
	sim.APIKey = apiKey
	ctx, cancel := context.WithCancel(context.Background())
	go sim.Run(ctx)
	srv := httptest.NewServer(sim)
	t.Cleanup(func() { cancel(); srv.Close() })
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func dial(t *testing.T, opts cds.Options) *cds.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := cds.Dial(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// The plugin's own client must be able to list, describe and read fields.
func TestClientProtocol(t *testing.T) {
	c := dial(t, cds.Options{URL: start(t, "")})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fields, err := c.Fields(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The e2e field-picker test expects AvgMwxAirTemp first.
	if len(fields) != len(ShipFields()) || fields[0] != "AvgMwxAirTemp" {
		t.Fatalf("Fields() = %v", fields)
	}

	meta, err := c.Describe(ctx, []string{"S330HeadingTrue", "NoSuchField"})
	if err != nil {
		t.Fatal(err)
	}
	if len(meta) != 1 || meta["S330HeadingTrue"].Units != "degrees" {
		t.Fatalf("Describe() = %+v", meta)
	}

	latest, err := c.Latest(ctx, []string{"S330HeadingTrue", "AvgMwxAirTemp", "NoSuchField"})
	if err != nil {
		t.Fatal(err)
	}
	if len(latest) != 2 {
		t.Fatalf("Latest() = %+v", latest)
	}
	if v, ok := latest["S330HeadingTrue"].Value.(float64); !ok || v < 0 || v >= 360 {
		t.Fatalf("S330HeadingTrue = %v", latest["S330HeadingTrue"].Value)
	}
	if age := time.Since(latest["S330HeadingTrue"].Timestamp()); age > time.Second {
		t.Fatalf("latest value is %s old", age)
	}
}

func TestLatestNoMatchingFields(t *testing.T) {
	c := dial(t, cds.Options{URL: start(t, "")})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	latest, err := c.Latest(ctx, []string{"NoSuchField"})
	if err != nil || len(latest) != 0 {
		t.Fatalf("Latest() = %v, %v", latest, err)
	}
}

func TestAPIKey(t *testing.T) {
	url := start(t, "secret")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	for name, opts := range map[string]cds.Options{
		"missing key":   {URL: url},
		"wrong key":     {URL: url, APIKey: "nope"},
		"query api_key": {URL: url + "/?api_key=secret", APIKey: "secret"},
	} {
		_, err := cds.Dial(ctx, opts)
		var he *cds.HandshakeError
		if !errors.As(err, &he) || he.StatusCode != 403 {
			t.Errorf("%s: Dial() error = %v, want HTTP 403", name, err)
		}
	}

	c := dial(t, cds.Options{URL: url + "/api/v1/ws/data-server", APIKey: "secret"})
	if _, err := c.Fields(ctx); err != nil {
		t.Fatal(err)
	}
}

// Streaming through the plugin's Manager delivers 10 Hz attitude data in
// order without duplicates.
func TestManagerStreams10Hz(t *testing.T) {
	m := cds.NewManager(cds.Options{URL: start(t, "")}, 100*time.Millisecond, nopLogger{})
	defer m.Close()
	sub, err := m.Subscribe("S330Pitch")
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()

	var got []cds.Point
	deadline := time.After(3 * time.Second)
	for len(got) < 15 {
		select {
		case pts := <-sub.C:
			got = append(got, pts...)
		case <-deadline:
			t.Fatalf("got %d points in 3 s, want 15", len(got))
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i].Time <= got[i-1].Time {
			t.Fatalf("points out of order or duplicated at %d: %v", i, got)
		}
	}
	if span := got[len(got)-1].Time - got[1].Time; span > 2.5 {
		t.Fatalf("%d points took %.1f s; want about 10 Hz", len(got)-1, span)
	}
}
