package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

const (
	// DefaultPollIntervalMs is the CDS subscription interval used when none
	// is configured: 10 Hz.
	DefaultPollIntervalMs = 100
	// MinPollIntervalMs guards against configurations that would make the
	// CDS spin.
	MinPollIntervalMs = 20
)

// Connection modes.
const (
	// ConnectionProxy connects through the OpenRVDAS API's authenticated,
	// read-only CDS proxy (ws(s)://<host>/api/v1/ws/data-server) with an
	// API key.
	ConnectionProxy = "proxy"
	// ConnectionDirect connects straight to the CachedDataServer
	// (ws://<host>:8766 or nginx /cds-ws) without authentication. Slated
	// for removal in OpenRVDAS v3.
	ConnectionDirect = "direct"
)

// PluginSettings is the data source instance configuration.
type PluginSettings struct {
	// ConnectionMode is ConnectionProxy or ConnectionDirect. Empty means
	// direct, so data sources created before the setting existed keep
	// working; the config editor sets it explicitly for new ones.
	ConnectionMode string `json:"connectionMode"`
	// URL of the CDS WebSocket or the OpenRVDAS API proxy endpoint.
	URL string `json:"url"`
	// PollIntervalMs is the CDS subscription interval in milliseconds.
	PollIntervalMs int64 `json:"pollIntervalMs"`
	// TLSSkipVerify disables certificate checks for wss:// URLs.
	TLSSkipVerify bool `json:"tlsSkipVerify"`

	// APIKey comes from secureJsonData and is only used in proxy mode.
	APIKey string `json:"-"`
}

// Mode returns the effective connection mode.
func (s *PluginSettings) Mode() string {
	if s.ConnectionMode == ConnectionProxy {
		return ConnectionProxy
	}
	return ConnectionDirect
}

// ModeName is a human-readable name for the connection mode.
func (s *PluginSettings) ModeName() string {
	if s.Mode() == ConnectionProxy {
		return "OpenRVDAS API"
	}
	return "CachedDataServer"
}

// EffectiveAPIKey is the key to send: the configured key in proxy mode,
// nothing in direct mode.
func (s *PluginSettings) EffectiveAPIKey() string {
	if s.Mode() == ConnectionProxy {
		return s.APIKey
	}
	return ""
}

// Validate reports configuration problems that make connecting pointless.
func (s *PluginSettings) Validate() error {
	if strings.TrimSpace(s.URL) == "" {
		return errors.New("URL is not configured")
	}
	if s.Mode() == ConnectionProxy && s.APIKey == "" {
		return errors.New("an API key is required for OpenRVDAS API connections")
	}
	return nil
}

// PollInterval returns the configured interval, defaulted and clamped.
func (s *PluginSettings) PollInterval() time.Duration {
	ms := s.PollIntervalMs
	if ms <= 0 {
		ms = DefaultPollIntervalMs
	}
	if ms < MinPollIntervalMs {
		ms = MinPollIntervalMs
	}
	return time.Duration(ms) * time.Millisecond
}

// LoadPluginSettings parses the instance settings.
func LoadPluginSettings(source backend.DataSourceInstanceSettings) (*PluginSettings, error) {
	settings := PluginSettings{}
	if len(source.JSONData) > 0 {
		if err := json.Unmarshal(source.JSONData, &settings); err != nil {
			return nil, fmt.Errorf("could not unmarshal PluginSettings json: %w", err)
		}
	}
	settings.APIKey = strings.TrimSpace(source.DecryptedSecureJSONData["apiKey"])
	return &settings, nil
}
