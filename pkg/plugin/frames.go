package plugin

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/OceanDataTools/grafana-openrvdas-datasource/pkg/cds"
)

// valueKind is the column type used for a field's values. It is chosen from
// the first known value and then kept fixed for the life of a stream, since
// streamed frames carry data only and must match the schema the panel got
// from QueryData.
type valueKind int

const (
	kindNumber valueKind = iota
	kindString
	kindBool
)

func inferKind(v any) valueKind {
	switch v.(type) {
	case string, map[string]any, []any:
		return kindString
	case bool:
		return kindBool
	default:
		// float64, or nil when nothing is known yet: numbers are by far the
		// common case for instrument data.
		return kindNumber
	}
}

func toNumber(v any) *float64 {
	switch t := v.(type) {
	case float64:
		return &t
	case bool:
		f := 0.0
		if t {
			f = 1
		}
		return &f
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil {
			return &f
		}
	}
	return nil
}

func toString(v any) *string {
	var s string
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		s = t
	case float64:
		s = strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		s = strconv.FormatBool(t)
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return nil
		}
		s = string(b)
	}
	return &s
}

func toBool(v any) *bool {
	if b, ok := v.(bool); ok {
		return &b
	}
	return nil
}

// grafanaUnits maps unit strings commonly found in OpenRVDAS device type
// definitions to Grafana unit IDs. Anything not listed is shown as a suffix.
var grafanaUnits = map[string]string{
	"degree":        "degree",
	"degrees":       "degree",
	"deg":           "degree",
	"knots":         "velocityknot",
	"knot":          "velocityknot",
	"kt":            "velocityknot",
	"kts":           "velocityknot",
	"m/s":           "velocityms",
	"meters/second": "velocityms",
	"km/h":          "velocitykmh",
	"km/hour":       "velocitykmh",
	"kph":           "velocitykmh",
	"degrees c":     "celsius",
	"k":             "kelvin",
	"mv":            "mvolt",
	"lbs/sq-in":     "pressurepsi",
	"psi":           "pressurepsi",
	"m":             "lengthm",
	"meter":         "lengthm",
	"meters":        "lengthm",
	"km":            "lengthkm",
	"ft":            "lengthft",
	"feet":          "lengthft",
	"c":             "celsius",
	"degc":          "celsius",
	"celsius":       "celsius",
	"f":             "fahrenheit",
	"degf":          "fahrenheit",
	"%":             "percent",
	"percent":       "percent",
	"hpa":           "pressurehpa",
	"mbar":          "pressurembar",
	"mb":            "pressurembar",
	"v":             "volt",
	"volts":         "volt",
	"a":             "amp",
	"amps":          "amp",
	"hz":            "hertz",
}

func grafanaUnit(units string) string {
	u := strings.TrimSpace(units)
	if u == "" {
		return ""
	}
	if id, ok := grafanaUnits[strings.ToLower(u)]; ok {
		return id
	}
	return "suffix: " + u
}

// newFieldFrame builds a frame for one CDS field: a time column and a value
// column named after the CDS field, which is what panels such as the
// OceanDataTools compass and attitude panels match on.
func newFieldFrame(field string, kind valueKind, meta *cds.Metadata) *data.Frame {
	var values *data.Field
	switch kind {
	case kindString:
		values = data.NewField(field, nil, []*string{})
	case kindBool:
		values = data.NewField(field, nil, []*bool{})
	default:
		values = data.NewField(field, nil, []*float64{})
	}
	if meta != nil && (meta.Units != "" || meta.Description != "") {
		values.Config = &data.FieldConfig{
			Unit:        grafanaUnit(meta.Units),
			Description: meta.Description,
		}
	}
	return data.NewFrame(field,
		data.NewField("time", nil, []time.Time{}),
		values,
	)
}

// appendPoint adds a point to a frame created by newFieldFrame.
func appendPoint(frame *data.Frame, kind valueKind, p cds.Point) {
	var v any
	switch kind {
	case kindString:
		v = toString(p.Value)
	case kindBool:
		v = toBool(p.Value)
	default:
		v = toNumber(p.Value)
	}
	frame.AppendRow(p.Timestamp(), v)
}

// globToRegexp converts a field pattern with '*' wildcards into an anchored
// regular expression. Every other character is literal.
func globToRegexp(pattern string) (*regexp.Regexp, error) {
	parts := strings.Split(pattern, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return regexp.Compile("^" + strings.Join(parts, ".*") + "$")
}

// expandFields resolves wildcard patterns against the list of available
// fields. Literal names are passed through as-is, even if not currently in
// the cache, so a panel can be set up before a logger starts. The result is
// de-duplicated and keeps the order of the requested patterns.
func expandFields(requested []string, available []string) ([]string, error) {
	sortedAvail := append([]string(nil), available...)
	sort.Strings(sortedAvail)
	seen := map[string]bool{}
	var out []string
	add := func(f string) {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	for _, r := range requested {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if !strings.Contains(r, "*") {
			add(r)
			continue
		}
		re, err := globToRegexp(r)
		if err != nil {
			return nil, err
		}
		for _, f := range sortedAvail {
			if re.MatchString(f) {
				add(f)
			}
		}
	}
	return out, nil
}

// hasWildcard reports whether any requested field is a pattern.
func hasWildcard(requested []string) bool {
	for _, r := range requested {
		if strings.Contains(r, "*") {
			return true
		}
	}
	return false
}
