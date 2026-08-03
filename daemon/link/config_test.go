package link

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConnectorConfigUsesExplicitEnvironmentAndRelativeCA(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("TEST_ZEN_LINK_TOKEN", "0123456789abcdef0123456789abcdef")
	path := filepath.Join(directory, "link.json")
	raw := []byte(`{
  "version": 1,
  "connector_token_env": "TEST_ZEN_LINK_TOKEN",
  "max_streams": 9,
  "relays": [{
    "name": "region-a",
    "control_address": "control.test:8443",
    "control_server_name": "control.test",
    "control_ca_file": "control-ca.pem",
    "client_domain": "link.test",
    "client_port": 443
  }]
}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConnectorConfig(path)
	if err != nil {
		t.Fatalf("LoadConnectorConfig: %v", err)
	}
	if config.ConnectorToken != "0123456789abcdef0123456789abcdef" ||
		config.MaxStreams != 9 ||
		len(config.Candidates) != 1 ||
		config.Candidates[0].ControlCAFile != filepath.Join(directory, "control-ca.pem") {
		t.Fatalf("unexpected connector config: %#v", config)
	}
}

func TestLoadConnectorConfigDoesNotFallbackWhenTokenSourceIsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "link.json")
	if err := os.WriteFile(path, []byte(`{
  "version": 1,
  "connector_token_env": "MISSING_ZEN_LINK_TOKEN",
  "relays": [{
    "control_address": "control.test:8443",
    "control_server_name": "control.test",
    "client_domain": "link.test"
  }]
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MISSING_ZEN_LINK_TOKEN", "")
	if _, err := LoadConnectorConfig(path); err == nil {
		t.Fatal("missing connector token environment was accepted")
	}
}

func TestConnectorConfigBoundsRelayCandidates(t *testing.T) {
	config := ConnectorConfig{
		ConnectorToken: "0123456789abcdef0123456789abcdef",
	}
	for index := 0; index < 17; index++ {
		config.Candidates = append(config.Candidates, RelayCandidate{
			Name:              "region-" + strings.Repeat("x", index),
			ControlAddress:    "control.test:8443",
			ControlServerName: "control.test",
			ClientDomain:      "link.test",
		})
	}
	if _, err := normalizeConnectorConfig(config); err == nil ||
		!strings.Contains(err.Error(), "at most 16") {
		t.Fatalf("candidate bound error=%v", err)
	}
}
