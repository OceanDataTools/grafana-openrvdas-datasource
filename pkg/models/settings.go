package models

import (
	"encoding/json"
	"fmt"
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

// PluginSettings is the data source instance configuration (jsonData).
type PluginSettings struct {
	// URL of the CDS WebSocket, e.g. ws://openrvdas.local:8766.
	URL string `json:"url"`
	// PollIntervalMs is the CDS subscription interval in milliseconds.
	PollIntervalMs int64 `json:"pollIntervalMs"`
	// TLSSkipVerify disables certificate checks for wss:// URLs.
	TLSSkipVerify bool `json:"tlsSkipVerify"`
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
	return &settings, nil
}
