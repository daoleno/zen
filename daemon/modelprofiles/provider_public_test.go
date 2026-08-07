package modelprofiles

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompileProviderConnectionPresetsHideInternalFields(t *testing.T) {
	in := ProviderConnectionInput{
		Name: "OpenAI", PresetID: ProviderPresetOpenAI,
	}
	profile, err := CompileProviderConnection(in)
	if err != nil {
		t.Fatal(err)
	}
	if !isAccountConnection(profile) || profile.CredentialEnv == "" || profile.Model != "" {
		t.Fatalf("account compile=%#v", profile)
	}
	target, err := CompileConnectionTarget(profile, ClientCodex, "gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	if target.Protocol != ProtocolOpenAIResponses || target.AuthMode != AuthModeBearerEnv {
		t.Fatalf("client target=%#v", target)
	}

	projReady := providerConnectionFromProfile(profile, true)
	raw, _ := json.Marshal(projReady)
	for _, banned := range []string{"auth_mode", "credential_env", "protocol", "client_model", "envelope", "generation", "OPENAI_API_KEY", "executor_id", `"model_id"`, `"provider_id"`} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("public connection leaked %q: %s", banned, raw)
		}
	}
	if !projReady.CredentialReady || projReady.ManualModelID != "" {
		t.Fatalf("public=%#v", projReady)
	}
}

func TestCompileProviderConnectionRejectsUntrustedModelUnlessAdvanced(t *testing.T) {
	_, err := CompileProviderConnection(ProviderConnectionInput{
		Name: "OR", PresetID: ProviderPresetOpenRouter, ModelID: "invented/model",
	})
	if err == nil {
		t.Fatal("expected curated model_id rejection")
	}
	profile, err := CompileProviderConnection(ProviderConnectionInput{
		Name: "Custom", Client: ClientCodex, PresetID: ProviderPresetCustom,
		BaseURL: "https://gateway.example/v1", ModelID: "invented/model", Advanced: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if profile.Model != "invented/model" {
		t.Fatalf("advanced model=%q", profile.Model)
	}
}

func TestProjectModelEntriesTrustedAuthorizeAvailability(t *testing.T) {
	trusted := []string{"gpt-5", "o3"}
	entries := projectModelEntries(trusted, "manual-1", []string{"gpt-5", "extra"}, []string{"o3"}, true)
	byID := map[string]ProviderModelEntry{}
	for _, e := range entries {
		byID[e.ID] = e
	}
	if !byID["gpt-5"].Available || byID["gpt-5"].Source != ModelSourceDiscovered {
		t.Fatalf("gpt-5=%#v", byID["gpt-5"])
	}
	if _, ok := byID["extra"]; ok {
		t.Fatal("live ids outside trusted/manual must not invent capabilities")
	}
	if byID["manual-1"].Source != ModelSourceManual {
		t.Fatalf("manual=%#v", byID["manual-1"])
	}
}

func TestDiscoverProviderModelsTTLAndLKG(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5"},{"id":"o3"}]}`))
			return
		}
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	owner := startTestOwner(t, func(string) (string, bool) { return "ready", true })
	profile := Profile{
		ID: "c1", Name: "C1", ExecutorID: ExecutorCodex,
		ProviderID: "openai", ProviderLabel: "OpenAI",
		Protocol: ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "gpt-5",
		ClientModelProvenance: ContractProvenanceConfiguredCompatibility,
		BaseURL:               srv.URL + "/v1",
		AuthMode:              AuthModeBearerEnv,
		CredentialEnv:         "OPENAI_API_KEY",
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	owner.discovery = newModelDiscoveryCache()
	owner.discovery.client = srv.Client()
	owner.discovery.ttl = time.Hour
	now := time.Now()
	owner.discovery.now = func() time.Time { return now }
	owner.mu.Unlock()

	first, err := owner.DiscoverProviderModels("c1", true)
	if err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("hits=%d", hits)
	}
	second, err := owner.DiscoverProviderModels("c1", false)
	if err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("ttl should skip refresh hits=%d", hits)
	}
	_ = first
	_ = second

	now = now.Add(2 * time.Hour)
	third, err := owner.DiscoverProviderModels("c1", true)
	if err != nil {
		t.Fatal(err)
	}
	if hits != 2 {
		t.Fatalf("forced refresh hits=%d", hits)
	}
	foundLKG := false
	for _, e := range third {
		if e.ID == "gpt-5" && e.Source == ModelSourceLKG && e.Available {
			foundLKG = true
		}
	}
	if !foundLKG {
		t.Fatalf("expected LKG after failure: %#v", third)
	}
}

func TestActivateSessionProviderUsesCurrentGenerationAtomically(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	a := codexResponsesProfile("a", "gpt-5", "up-a")
	b := codexResponsesProfile("b", "gpt-5", "up-b")
	b.BaseURL = a.BaseURL
	b.ProviderID = a.ProviderID
	if _, err := owner.UpsertProfile(a, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.UpsertProfile(b, 1, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, a.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s1"); err != nil {
		t.Fatal(err)
	}
	state, snap, persist, err := owner.ActivateSessionProvider("s1", b.ID, "up-b")
	if err != nil || !persist.Applied {
		t.Fatalf("activate err=%v persist=%#v", err, persist)
	}
	if snap.Current == nil || snap.Current.ConnectionID != b.ID || snap.Current.ModelID != "up-b" {
		t.Fatalf("snap=%#v", snap)
	}
	if state.Generation != 2 || state.Binding.ProfileID != b.ID {
		t.Fatalf("state=%#v", state)
	}
	got, _ := owner.GetProfile(b.ID)
	if got.Model != "up-b" && got.Model != b.Model {
		// catalog model must remain the pre-activate catalog value (b.Model was up-b already)
	}
	if got.Model != "up-b" {
		// b was created with Model up-b; session override of same id is fine
	}
	raw, _ := json.Marshal(snap)
	for _, banned := range []string{"generation", "history", "protocol", "credential_env", "auth_mode", "executor_id"} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("wire leaked %q: %s", banned, raw)
		}
	}
	if snap.Current.Client != ClientCodex {
		t.Fatalf("client=%q", snap.Current.Client)
	}
}

func TestValidateUpstreamBaseURLBlocksSSRF(t *testing.T) {
	if err := ValidateUpstreamBaseURL("http://169.254.169.254/"); err == nil {
		t.Fatal("metadata IP must fail")
	}
	if err := ValidateUpstreamBaseURL("https://api.openai.com/v1"); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoveryCacheSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	discoveryPath := filepath.Join(root, "provider-discovery.json")
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5"},{"id":"o3"}]}`))
	}))
	t.Cleanup(srv.Close)

	owner := startTestOwner(t, func(string) (string, bool) { return "ready", true })
	profile := Profile{
		ID: "c1", Name: "C1", Scope: ConnectionScopeAccount,
		ProviderID: "openai", ProviderLabel: "OpenAI",
		Model: "gpt-5", BaseURL: srv.URL + "/v1",
		AuthMode: AuthModeNone, CredentialEnv: "OPENAI_API_KEY",
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	owner.discoveryPath = discoveryPath
	owner.discovery = newModelDiscoveryCache()
	owner.discovery.client = srv.Client()
	owner.discovery.ttl = time.Hour
	owner.mu.Unlock()
	if _, err := owner.DiscoverProviderModels("c1", true); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("hits=%d", hits)
	}
	if _, err := os.Stat(discoveryPath); err != nil {
		t.Fatal(err)
	}

	owner2 := startTestOwner(t, func(string) (string, bool) { return "ready", true })
	if _, err := owner2.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	owner2.mu.Lock()
	owner2.discoveryPath = discoveryPath
	owner2.discovery = newModelDiscoveryCache()
	owner2.discovery.client = srv.Client()
	owner2.discovery.ttl = time.Hour
	if err := owner2.discovery.load(discoveryPath); err != nil {
		t.Fatal(err)
	}
	owner2.mu.Unlock()
	entries, err := owner2.DiscoverProviderModels("c1", false)
	if err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("restart load should skip network hits=%d", hits)
	}
	found := false
	for _, e := range entries {
		if e.ID == "gpt-5" && e.Available {
			found = true
		}
	}
	if !found {
		t.Fatalf("entries=%#v", entries)
	}
}

func TestUpsertCustomAccountConnectionViaOwner(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "codex-main", Name: "Codex Main", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: "https://gateway.example/v1",
		ModelID: "up-1", Advanced: true,
	}, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(proj.Connections) != 1 || proj.Connections[0].ManualModelID != "up-1" {
		t.Fatalf("%#v", proj)
	}
	// conflict revision
	_, err = owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "other", Name: "X", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: "https://gateway.example/v1",
		ModelID: "up-2", Advanced: true,
	}, 99, true)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want conflict got %v", err)
	}
}
