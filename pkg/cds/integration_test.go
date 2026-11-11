//go:build integration

// Integration tests against a real CachedDataServer, directly or through
// the OpenRVDAS API proxy. Run with:
//
//	CDS_URL=ws://host:8766 go test -tags integration -v ./pkg/cds/
//	CDS_URL=wss://host/api/v1/ws/data-server OPENRVDAS_API_KEY=... go test -tags integration -v ./pkg/cds/
//
// TestIntegrationProxyRejectsBadKey only needs OPENRVDAS_PROXY_URL.
package cds

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func cdsOptions(t *testing.T) Options {
	u := os.Getenv("CDS_URL")
	if u == "" {
		t.Skip("CDS_URL not set")
	}
	return Options{URL: u, APIKey: os.Getenv("OPENRVDAS_API_KEY")}
}

func TestIntegrationProxyRejectsBadKey(t *testing.T) {
	u := os.Getenv("OPENRVDAS_PROXY_URL")
	if u == "" {
		t.Skip("OPENRVDAS_PROXY_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, key := range []string{"", "not-a-real-key"} {
		_, err := Dial(ctx, Options{URL: u, APIKey: key})
		var hs *HandshakeError
		if !errors.As(err, &hs) || !hs.IsAuthFailure() {
			t.Fatalf("key %q: expected an auth failure, got %v", key, err)
		}
		t.Logf("key %q rejected: %v", key, hs)
	}
}

func TestIntegrationOneShots(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Dial(ctx, cdsOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fields, err := c.Fields(ctx)
	if err != nil || len(fields) == 0 {
		t.Fatalf("Fields() = %d fields, %v", len(fields), err)
	}
	t.Logf("%d fields", len(fields))
	md, err := c.Describe(ctx, fields[:3])
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("describe: %+v", md)
	latest, err := c.Latest(ctx, fields)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("latest values for %d/%d fields", len(latest), len(fields))
}

func TestIntegrationStreamRate(t *testing.T) {
	field := os.Getenv("CDS_FIELD")
	if field == "" {
		field = "S330HeadingTrue"
	}
	m := NewManager(cdsOptions(t), 100*time.Millisecond, nopLogger{})
	defer m.Close()
	s, err := m.Subscribe(field)
	if err != nil {
		t.Fatal(err)
	}
	var batches, points int
	var lastTs float64
	deadline := time.After(5 * time.Second)
	for done := false; !done; {
		select {
		case pts := <-s.C:
			batches++
			for _, p := range pts {
				if p.Time <= lastTs {
					t.Fatalf("non-increasing timestamp %v after %v", p.Time, lastTs)
				}
				lastTs = p.Time
			}
			points += len(pts)
		case <-deadline:
			done = true
		}
	}
	if points == 0 {
		t.Fatalf("no data for %s in 5s", field)
	}
	t.Logf("%s: %d points in %d batches over 5s (%.1f points/s)", field, points, batches, float64(points)/5)
}
