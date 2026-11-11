//go:build integration

// Integration tests against a real CachedDataServer. Run with:
//
//	CDS_URL=ws://host:8766 go test -tags integration -v ./pkg/cds/
package cds

import (
	"context"
	"os"
	"testing"
	"time"
)

func cdsURL(t *testing.T) string {
	u := os.Getenv("CDS_URL")
	if u == "" {
		t.Skip("CDS_URL not set")
	}
	return u
}

func TestIntegrationOneShots(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Dial(ctx, Options{URL: cdsURL(t)})
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
	m := NewManager(Options{URL: cdsURL(t)}, 100*time.Millisecond, nopLogger{})
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
