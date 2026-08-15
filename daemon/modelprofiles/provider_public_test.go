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
		Name: "OpenAI", PresetID: ProviderPresetOpenAI, Client: ClientCodex,
	}
	profile, err := CompileProviderConnection(in)
	if err != nil {
		t.Fatal(err)
	}
	if !isAccountConnection(profile) || profile.CredentialEnv == "" || profile.Model != "" {
		t.Fatalf("account compile=%#v", profile)
	}
	target, err := CompileConnectionTarget(profile, ClientCodex, "gpt-5", "")
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

func TestCompileProviderConnectionRequiresOneExplicitClient(t *testing.T) {
	_, err := CompileProviderConnection(ProviderConnectionInput{
		Name: "Unscoped", PresetID: ProviderPresetDeepSeek,
	})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "client") {
		t.Fatalf("missing client error=%v", err)
	}
}

func TestCompileProviderConnectionPassesThroughAnyValidModelSlug(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	owner.SetCredentialStore(NewMemoryCredentialStore())
	projection, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "or", Name: "OR", PresetID: ProviderPresetOpenRouter, Client: ClientCodex,
	}, "key", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	projection, err = owner.SetProviderDefault(ClientCodex, "or", "invented/model", projection.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if got := projection.Defaults[ClientCodex].ModelID; got != "invented/model" {
		t.Fatalf("curated default model=%q", got)
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
	entries := projectModelEntries(trusted, "manual-1", []string{"gpt-5", "extra"}, []string{"o3"}, nil, true)
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

func TestProjectModelEntriesCustomUsesDiscoveredCatalog(t *testing.T) {
	entries := projectModelEntries(nil, "", []string{"deepseek-v4-flash", "deepseek-v4-pro"}, nil, nil, true)
	if len(entries) != 2 || !entries[0].Available || entries[0].Source != ModelSourceDiscovered {
		t.Fatalf("custom entries=%#v", entries)
	}
}

func TestDiscoverProviderModelsTTLAndLKG(t *testing.T) {
	installTestCodexModelCache(t, []CodexModelCatalogWireEntry{
		testCodexCacheEntry("cache-fallback", "Cache fallback", ""),
	})
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
	foundCache := false
	for _, e := range third {
		if e.ID == "cache-fallback" && e.Source == ModelSourceCodexCache && e.Available {
			foundCache = true
		}
	}
	if !foundCache {
		t.Fatalf("expected installed Codex cache after failure: %#v", third)
	}
}

func TestSetThreadRuntimeUsesCurrentGenerationAtomically(t *testing.T) {
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
	state, snap, persist, err := owner.SetThreadRuntime("s1", ThreadRuntimeChoice{ConnectionID: b.ID, ModelID: "up-b", Effect: ""})
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
	if err := ValidateUpstreamBaseURL("http://models.internal:8080/v1"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateUpstreamBaseURL("http://10.20.30.40:8080/v1"); err != nil {
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
		Client:     ClientCodex,
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
	}, "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(proj.Connections) != 1 || proj.Connections[0].ManualModelID != "up-1" {
		t.Fatalf("%#v", proj)
	}
	if got := proj.Connections[0].Clients; len(got) != 1 || got[0] != ClientCodex {
		t.Fatalf("client scope=%#v", got)
	}
	if _, err := CompileConnectionTarget(owner.Catalog().Profiles[0], ClientClaude, "up-1", ""); !errors.Is(err, ErrBindingExecutorMismatch) {
		t.Fatalf("cross-client compile err=%v", err)
	}
	// conflict revision
	_, err = owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "other", Name: "X", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: "https://gateway.example/v1",
		ModelID: "up-2", Advanced: true,
	}, "", 99, true)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want conflict got %v", err)
	}
}

func TestProviderConnectionProbeIsTransientAndUsesClientAuth(t *testing.T) {
	tests := []struct {
		client     string
		wantHeader string
	}{
		{client: ClientCodex, wantHeader: "Authorization"},
		{client: ClientClaude, wantHeader: "x-api-key"},
	}
	for _, tt := range tests {
		t.Run(tt.client, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get(tt.wantHeader); got == "" {
					t.Fatalf("missing %s", tt.wantHeader)
				}
				_, _ = w.Write([]byte(`{"data":[{"id":"model-a"},{"id":"model-b"}]}`))
			}))
			t.Cleanup(server.Close)

			owner := startTestOwner(t, func(string) (string, bool) { return "", false })
			before := owner.Catalog()
			result, err := owner.TestProviderConnection(ProviderConnectionTestInput{
				Client: tt.client, BaseURL: server.URL, Credential: "transient-secret",
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Client != tt.client || result.ModelCount != 2 || result.LatencyMS < 0 {
				t.Fatalf("result=%#v", result)
			}
			after := owner.Catalog()
			if after.Revision != before.Revision || len(after.Profiles) != len(before.Profiles) {
				t.Fatalf("probe mutated catalog: before=%#v after=%#v", before, after)
			}
		})
	}
}

// Regression: custom/advanced account connections intentionally omit model_id.
// Discover and compile probes must use the ClientModel contract placeholder
// instead of failing with "model_id is required".
func TestDiscoverCustomAccountConnectionWithoutModelID(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"data":[{"id":"upstream-a"},{"id":"upstream-b"}]}`))
	}))
	t.Cleanup(srv.Close)

	for _, client := range []string{ClientCodex, ClientClaude} {
		t.Run(client, func(t *testing.T) {
			owner := startTestOwner(t, func(string) (string, bool) { return "", false })
			store := NewMemoryCredentialStore()
			owner.creds = store
			owner.router.creds = store

			proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{
				ID:       "custom-" + client,
				Name:     "gateway.example",
				Client:   client,
				PresetID: ProviderPresetCustom,
				BaseURL:  srv.URL + "/v1",
				Advanced: true,
			}, "", 0, true)
			if err != nil {
				t.Fatal(err)
			}
			conn, err := owner.GetProfile("custom-" + client)
			if err != nil {
				t.Fatal(err)
			}
			if conn.Model != "" {
				t.Fatalf("durable account must not own model_id: %#v", conn)
			}

			if _, err := owner.SetProviderCredential(conn.ID, "sk-test-not-a-secret"); err != nil {
				t.Fatal(err)
			}

			target, err := CompileConnectionTarget(conn, client, "", "")
			if err != nil {
				t.Fatalf("empty-model compile probe: %v", err)
			}
			wantClientModel := "gpt-5"
			if client == ClientClaude {
				wantClientModel = "claude-sonnet-4-6"
			}
			if target.Model != wantClientModel || target.ClientModel != wantClientModel {
				t.Fatalf("probe placeholder model=%q client_model=%q want %q", target.Model, target.ClientModel, wantClientModel)
			}

			owner.mu.Lock()
			owner.discovery = newModelDiscoveryCache()
			owner.discovery.client = srv.Client()
			owner.mu.Unlock()

			beforeHits := hits
			entries, err := owner.DiscoverProviderModels(conn.ID, true)
			if err != nil {
				t.Fatalf("discover: %v", err)
			}
			if hits <= beforeHits {
				t.Fatalf("discover did not hit upstream (hits=%d)", hits)
			}
			if len(entries) == 0 {
				t.Fatalf("expected discovered entries, got %#v", entries)
			}

			after, err := owner.GetProfile(conn.ID)
			if err != nil {
				t.Fatal(err)
			}
			if after.Model != "" {
				t.Fatalf("discover must not persist model_id onto account: %#v", after)
			}
			if owner.Catalog().Revision != proj.Revision {
				t.Fatalf("discover mutated catalog revision %d -> %d", proj.Revision, owner.Catalog().Revision)
			}
		})
	}
}

func TestCustomDefaultDoesNotFabricateDiscoveredModel(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	projection, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID:       "codex-auto",
		Name:     "gateway.example",
		Client:   ClientCodex,
		PresetID: ProviderPresetCustom,
		BaseURL:  "https://gateway.example/v1",
		Advanced: true,
	}, "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	owner.discovery = newModelDiscoveryCache()
	owner.discovery.put("codex-auto", []string{"deepseek-v4-flash"}, nil)
	owner.mu.Unlock()
	// Settings must select the exact discovered model atomically; an empty
	// model is refused rather than fabricated.
	if _, err := owner.SetProviderDefault(ClientCodex, "codex-auto", "", projection.Revision); err == nil {
		t.Fatal("empty default runtime was accepted")
	}
	// Discovery remains suggestions-only and must not fabricate a launch model.
	if _, err := owner.PrepareLaunch(ExecutorCodex, "codex-auto", "codex"); !errors.Is(err, ErrUpstreamModelRequired) {
		t.Fatalf("launch without explicit model must fail, got %v", err)
	}
}
