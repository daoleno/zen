package server

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/modelprofiles"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestProvidersCatalogPayloadIncludesGatewayStatus(t *testing.T) {
	proj := modelprofiles.ProviderCatalogProjection{
		Revision: 9,
		Defaults: map[string]modelprofiles.ProviderDefault{},
		Presets:  []modelprofiles.ProviderPreset{},
		Models:   map[string][]modelprofiles.ProviderModelEntry{},
		Gateway: modelprofiles.GatewayStatus{
			Running:    true,
			Address:    "127.0.0.1:4318",
			Endpoint:   "http://127.0.0.1:4318/v1",
			Protocols:  []string{"openai", "anthropic"},
			ModelCount: 3,
		},
	}
	payload := (&Server{}).providersCatalogPayload(
		"req-gateway",
		proj,
		modelprofiles.PersistResult{Applied: true, Durable: true},
		nil,
	)
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	gateway, ok := decoded["gateway"].(map[string]any)
	if !ok {
		t.Fatalf("gateway payload=%#v", decoded["gateway"])
	}
	if gateway["running"] != true || gateway["address"] != "127.0.0.1:4318" || gateway["endpoint"] != "http://127.0.0.1:4318/v1" {
		t.Fatalf("gateway identity=%#v", gateway)
	}
	if gateway["model_count"] != float64(3) {
		t.Fatalf("gateway model_count=%#v", gateway["model_count"])
	}
	protocols, ok := gateway["protocols"].([]any)
	if !ok || len(protocols) != 2 || protocols[0] != "openai" || protocols[1] != "anthropic" {
		t.Fatalf("gateway protocols=%#v", gateway["protocols"])
	}
}

func TestCreateSessionWithProfilesCompilesAndBypasses(t *testing.T) {
	root := t.TempDir()
	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: filepath.Join(root, "model-profiles.toml"),
		RoutesPath:   filepath.Join(root, "route-bindings.json"),
		ListenerPath: filepath.Join(root, "route-listener.json"),
		Lookup:       func(string) (string, bool) { return "ready", true },
		Verifier:     wsProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })

	profile := modelprofiles.Profile{
		ID: "codex-main", Name: "Codex Main", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "acme", ProviderLabel: "Acme",
		Protocol:              modelprofiles.ProtocolOpenAIResponses,
		ClientModel:           "gpt-5",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		Model:                 "org/up",
		BaseURL:               "https://gateway.example/v1",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ACME_KEY",
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetDefault(modelprofiles.ExecutorCodex, profile.ID, 1); err != nil {
		t.Fatal(err)
	}

	w := watcher.New(time.Second)
	srv := New(nil, w, nil, nil, nil, nil, nil)
	srv.SetModelProfiles(owner)

	plan, err := owner.PrepareLaunch(modelprofiles.ExecutorCodex, "", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Applied || !strings.Contains(plan.Command, "openai_base_url=") {
		t.Fatalf("plan=%#v", plan)
	}
	_, _ = owner.AbortLaunch(plan.ProvisionalID)

	bypass, err := owner.PrepareLaunch("", "", "zsh")
	if err != nil {
		t.Fatal(err)
	}
	if !bypass.Bypass {
		t.Fatalf("expected bypass %#v", bypass)
	}

	_, _, _, err = owner.ActivateSession("missing", profile.ID, 1)
	if !errors.Is(err, modelprofiles.ErrBindingNotFound) {
		t.Fatalf("activate err=%v", err)
	}
	if modelprofiles.ControlErrorCode(err) != modelprofiles.CodeBindingNotFound {
		t.Fatalf("code=%s", modelprofiles.ControlErrorCode(err))
	}
	_ = srv
}
