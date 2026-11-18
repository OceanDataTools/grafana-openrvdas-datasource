package plugin

import (
	"reflect"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/OceanDataTools/grafana-openrvdas-datasource/pkg/cds"
)

func TestStreamPathRoundTrip(t *testing.T) {
	cases := []struct {
		field string
		ms    int64
		path  string
	}{
		{"S330HeadingTrue", 0, "f/0/S330HeadingTrue"},
		{"status:logger_status", 250, "f/250/status=3Alogger_status"},
		{"weird/field=name with space", 0, "f/0/weird=2Ffield=3Dname=20with=20space"},
	}
	for _, c := range cases {
		got := streamPath(c.field, c.ms)
		if got != c.path {
			t.Errorf("streamPath(%q, %d) = %q, want %q", c.field, c.ms, got, c.path)
		}
		f, ms, err := parseStreamPath(got)
		if err != nil || f != c.field || ms != c.ms {
			t.Errorf("parseStreamPath(%q) = %q, %d, %v", got, f, ms, err)
		}
	}
	for _, bad := range []string{"x/0/a", "f/abc/a", "f/0/", "f/0/a=4", "f/-1/a"} {
		if _, _, err := parseStreamPath(bad); err == nil {
			t.Errorf("parseStreamPath(%q) should fail", bad)
		}
	}
}

func TestExpandFields(t *testing.T) {
	avail := []string{"S330Roll", "S330Pitch", "MwxAirTemp", "AvgMwxAirTemp", "status:logger_status"}
	got, err := expandFields([]string{"S330*", "MwxAirTemp", "S330Roll", "Missing", " "}, avail)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"S330Pitch", "S330Roll", "MwxAirTemp", "Missing"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// Patterns are anchored globs, not CDS's unanchored regex: "Mwx*" must
	// not match "AvgMwxAirTemp", and '.' is literal.
	got, _ = expandFields([]string{"Mwx*", "status:*"}, avail)
	want = []string{"MwxAirTemp", "status:logger_status"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestInferKindAndConvert(t *testing.T) {
	if inferKind(1.5) != kindNumber || inferKind(nil) != kindNumber {
		t.Error("numbers and unknown should be numeric")
	}
	if inferKind("W") != kindString || inferKind(map[string]any{}) != kindString || inferKind([]any{}) != kindString {
		t.Error("strings, objects and lists should be strings")
	}
	if inferKind(true) != kindBool {
		t.Error("bools should be bool")
	}
	if v := toNumber("12.5"); v == nil || *v != 12.5 {
		t.Error("numeric string should convert")
	}
	if toNumber("N") != nil {
		t.Error("non-numeric string should be null")
	}
	if v := toString(map[string]any{"a": 1.0}); v == nil || *v != `{"a":1}` {
		t.Errorf("object should be JSON-encoded, got %v", v)
	}
}

func TestFieldFrame(t *testing.T) {
	meta := &cds.Metadata{Units: "degrees", Description: "Heading"}
	f := newFieldFrame("S330HeadingTrue", kindNumber, meta)
	appendPoint(f, kindNumber, cds.Point{Time: 1794389196.5, Value: 217.85})
	appendPoint(f, kindNumber, cds.Point{Time: 1794389197.5, Value: nil})

	if f.Name != "S330HeadingTrue" || len(f.Fields) != 2 {
		t.Fatalf("unexpected frame %v", f)
	}
	v := f.Fields[1]
	if v.Name != "S330HeadingTrue" {
		t.Errorf("value column must be named after the CDS field, got %q", v.Name)
	}
	if v.Config == nil || v.Config.Unit != "degree" || v.Config.Description != "Heading" {
		t.Errorf("unexpected config %+v", v.Config)
	}
	if v.Type() != data.FieldTypeNullableFloat64 || v.Len() != 2 {
		t.Errorf("unexpected value field type %v len %d", v.Type(), v.Len())
	}
	if ts := f.Fields[0].At(0); ts.(interface{ UnixMilli() int64 }).UnixMilli() != 1794389196500 {
		t.Errorf("unexpected timestamp %v", ts)
	}
}

func TestGrafanaUnit(t *testing.T) {
	for in, want := range map[string]string{
		"degrees": "degree", "C": "celsius", "knots": "velocityknot", "W/m^2": "suffix: W/m^2", "": "",
	} {
		if got := grafanaUnit(in); got != want {
			t.Errorf("grafanaUnit(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStaleNotice(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	from := now.Add(-5 * time.Minute)
	at := func(d time.Duration) cds.Point {
		return cds.Point{Time: float64(now.Add(-d).UnixNano()) / 1e9, Value: 1.0}
	}

	t.Run("fresh values: no notice", func(t *testing.T) {
		latest := map[string]cds.Point{"A": at(10 * time.Second)}
		if text, ok := staleNotice([]string{"A"}, latest, from, now); ok {
			t.Fatalf("unexpected notice %q", text)
		}
	})

	t.Run("one stale field", func(t *testing.T) {
		latest := map[string]cds.Point{
			"AvgPortTrueWindSpeed": at(23 * time.Hour),
			"Fresh":                at(time.Second),
		}
		text, ok := staleNotice([]string{"AvgPortTrueWindSpeed", "Fresh"}, latest, from, now)
		want := "Latest value is older than the selected time range for: AvgPortTrueWindSpeed (23 h old). Is its logger running?"
		if !ok || text != want {
			t.Fatalf("got %q, %v; want %q", text, ok, want)
		}
	})

	t.Run("several stale fields keep query order", func(t *testing.T) {
		latest := map[string]cds.Point{
			"B": at(3 * 24 * time.Hour),
			"A": at(12 * time.Minute),
		}
		text, ok := staleNotice([]string{"A", "B", "Absent"}, latest, from, now)
		want := "Latest value is older than the selected time range for: A (12 min old), B (3 d old). Are their loggers running?"
		if !ok || text != want {
			t.Fatalf("got %q, %v; want %q", text, ok, want)
		}
	})

	t.Run("value exactly at the range start is in range", func(t *testing.T) {
		latest := map[string]cds.Point{"A": at(5 * time.Minute)}
		if text, ok := staleNotice([]string{"A"}, latest, from, now); ok {
			t.Fatalf("unexpected notice %q", text)
		}
	})

	t.Run("no time range: no notice", func(t *testing.T) {
		latest := map[string]cds.Point{"A": at(48 * time.Hour)}
		if text, ok := staleNotice([]string{"A"}, latest, time.Time{}, now); ok {
			t.Fatalf("unexpected notice %q", text)
		}
	})
}

func TestFormatAge(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{45 * time.Second, "45 s"},
		{59*time.Minute + 59*time.Second, "59 min"},
		{time.Hour, "1 h"},
		{47 * time.Hour, "47 h"},
		{48 * time.Hour, "2 d"},
		{10*24*time.Hour + 5*time.Hour, "10 d"},
	} {
		if got := formatAge(tc.d); got != tc.want {
			t.Errorf("formatAge(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
