package plugin

import (
	"fmt"
	"strconv"
	"strings"
)

// Grafana Live channel paths may only contain [A-z0-9_\-/=.], but CDS field
// names can contain other characters (e.g. "status:logger_status"). Field
// names are therefore escaped: safe characters pass through, anything else
// becomes "=XX" (uppercase hex of each byte). '=' itself is escaped, so the
// encoding is reversible and plain names like "S330HeadingTrue" stay
// readable in Grafana's Live debugging views.
//
// A stream path is "f/<minIntervalMs>/<escaped field>". Every panel asking
// for the same field at the same decimation interval shares one stream.

const streamPathPrefix = "f/"

func isSafeChannelByte(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') ||
		b == '_' || b == '-' || b == '.'
}

func escapeField(field string) string {
	var sb strings.Builder
	for i := 0; i < len(field); i++ {
		b := field[i]
		if isSafeChannelByte(b) {
			sb.WriteByte(b)
		} else {
			fmt.Fprintf(&sb, "=%02X", b)
		}
	}
	return sb.String()
}

func unescapeField(s string) (string, error) {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '=' {
			sb.WriteByte(s[i])
			continue
		}
		if i+2 >= len(s) {
			return "", fmt.Errorf("truncated escape in %q", s)
		}
		v, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
		if err != nil {
			return "", fmt.Errorf("bad escape in %q: %w", s, err)
		}
		sb.WriteByte(byte(v))
		i += 2
	}
	return sb.String(), nil
}

// streamPath builds the channel path for a field stream.
func streamPath(field string, minIntervalMs int64) string {
	if minIntervalMs < 0 {
		minIntervalMs = 0
	}
	return fmt.Sprintf("%s%d/%s", streamPathPrefix, minIntervalMs, escapeField(field))
}

// parseStreamPath is the inverse of streamPath.
func parseStreamPath(path string) (field string, minIntervalMs int64, err error) {
	rest, ok := strings.CutPrefix(path, streamPathPrefix)
	if !ok {
		return "", 0, fmt.Errorf("unknown stream path %q", path)
	}
	ms, enc, ok := strings.Cut(rest, "/")
	if !ok || enc == "" {
		return "", 0, fmt.Errorf("malformed stream path %q", path)
	}
	minIntervalMs, err = strconv.ParseInt(ms, 10, 64)
	if err != nil || minIntervalMs < 0 {
		return "", 0, fmt.Errorf("malformed interval in stream path %q", path)
	}
	field, err = unescapeField(enc)
	if err != nil {
		return "", 0, err
	}
	return field, minIntervalMs, nil
}
