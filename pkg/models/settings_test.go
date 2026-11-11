package models

import (
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func load(t *testing.T, jsonData string, secure map[string]string) *PluginSettings {
	t.Helper()
	s, err := LoadPluginSettings(backend.DataSourceInstanceSettings{
		JSONData:                []byte(jsonData),
		DecryptedSecureJSONData: secure,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestModeDefaultsToDirect(t *testing.T) {
	// Data sources created before connectionMode existed have none.
	s := load(t, `{"url":"ws://h:8766"}`, map[string]string{"apiKey": "k"})
	if s.Mode() != ConnectionDirect {
		t.Fatalf("mode = %q", s.Mode())
	}
	if s.EffectiveAPIKey() != "" {
		t.Fatal("the key must not be sent on direct connections")
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestProxyMode(t *testing.T) {
	s := load(t, `{"connectionMode":"proxy","url":"wss://h/api/v1/ws/data-server"}`, map[string]string{"apiKey": " k \n"})
	if s.Mode() != ConnectionProxy || s.EffectiveAPIKey() != "k" {
		t.Fatalf("mode %q key %q", s.Mode(), s.EffectiveAPIKey())
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	s = load(t, `{"connectionMode":"proxy","url":"wss://h"}`, nil)
	if err := s.Validate(); err == nil {
		t.Fatal("proxy mode without a key should be invalid")
	}
}

func TestValidateURLAndInterval(t *testing.T) {
	s := load(t, `{}`, nil)
	if err := s.Validate(); err == nil {
		t.Fatal("missing URL should be invalid")
	}
	if s.PollInterval() != 100*time.Millisecond {
		t.Fatalf("default interval %v", s.PollInterval())
	}
	s = load(t, `{"pollIntervalMs":5}`, nil)
	if s.PollInterval() != 20*time.Millisecond {
		t.Fatalf("clamped interval %v", s.PollInterval())
	}
}
