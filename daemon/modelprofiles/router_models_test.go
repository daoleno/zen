package modelprofiles

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Codex's native /model switch reads GET /v1/models against the loopback base
// URL. The router must answer with the LOCAL synced catalog (discovery cache)
// of the route's connection — never forwarded upstream, never triggering live
// discovery — in the standard OpenAI list payload shape.
func TestRouterServesLocalModelsList(t *testing.T) {
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("GET /v1/models must never reach upstream, got %s %s", r.Method, r.URL.Path)
	})
	defer upstream.Close()

	table := NewRouteTable()
	profile := routedCodex(upstream.URL, "gpt-5", "upstream-model-v2")
	state, err := table.BindLaunch("s1", profile, 1, verifiedAuth(profile))
	if err != nil {
		t.Fatal(err)
	}
	var gotProfileID string
	router := NewRouter(table, WithRouterModelCatalog(func(profileID string) ([]ProviderModelEntry, error) {
		gotProfileID = profileID
		return []ProviderModelEntry{
			{ID: "gpt-5.6-sol", Available: true, Source: ModelSourceDiscovered},
			{ID: "gpt-5.4-mini", Available: true, Source: ModelSourceDiscovered},
			// Unavailable (e.g. last-known-good without live proof) must be
			// filtered out: the /model list is models the proxy supports now.
			{ID: "retired-model", Available: false, Source: ModelSourceLKG},
		}, nil
	}))
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	base, err := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/models", "/models?limit=20"} {
		resp, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, resp.StatusCode, raw)
		}
		if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
			t.Fatalf("Content-Type=%q", got)
		}
		var list struct {
			Models []struct {
				Slug string `json:"slug"`
			} `json:"models"`
		}
		if err := json.Unmarshal(raw, &list); err != nil {
			t.Fatalf("invalid payload: %v", err)
		}
		ids := make([]string, 0, len(list.Models))
		for _, entry := range list.Models {
			ids = append(ids, entry.Slug)
		}
		if strings.Join(ids, ",") != "gpt-5.6-sol,gpt-5.4-mini,upstream-model-v2" {
			t.Fatalf("ids=%v want available choices plus exact running slug", ids)
		}
		if !strings.Contains(string(raw), `"slug"`) || strings.Contains(string(raw), `"data"`) {
			t.Fatalf("payload must be the Codex ModelsResponse shape: %s", raw)
		}
	}
	if gotProfileID != state.Binding.ProfileID {
		t.Fatalf("catalog resolved profile=%q want %q", gotProfileID, state.Binding.ProfileID)
	}
}

// The catalog surface is GET-only; every other endpoint stays POST-only.
func TestRouterModelsMethodGate(t *testing.T) {
	table := NewRouteTable()
	profile := routedCodex("https://gateway.example/v1", "gpt-5.6-sol", "gpt-5.6-sol")
	state, err := table.BindLaunch("s", profile, 1, verifiedAuth(profile))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table, WithRouterModelCatalog(func(string) ([]ProviderModelEntry, error) {
		return nil, nil
	}))
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	base, _ := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)

	// POST to the catalog endpoint is not a request path.
	resp, err := http.Post(base+"/models", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /models status=%d body=%s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), "route_method_mismatch") {
		t.Fatalf("error code missing: %s", raw)
	}

	// GET on a POST-only endpoint stays rejected.
	resp, err = http.Get(base + "/responses")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /responses status=%d body=%s", resp.StatusCode, raw)
	}
}

// No synced models yet is a valid empty catalog, not an error.
func TestRouterModelsEmptyCatalog(t *testing.T) {
	table := NewRouteTable()
	profile := routedCodex("https://gateway.example/v1", "gpt-5.6-sol", "gpt-5.6-sol")
	state, err := table.BindLaunch("s", profile, 1, verifiedAuth(profile))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table, WithRouterModelCatalog(func(string) ([]ProviderModelEntry, error) {
		return nil, nil
	}))
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	base, _ := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)

	resp, err := http.Get(base + "/models")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}
	var list CodexModelsResponse
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Models) != 1 || list.Models[0].Slug != "gpt-5.6-sol" {
		t.Fatalf("payload=%s want only the running identity", raw)
	}
}

// A failing catalog resolver is a server-side condition, not a body error.
func TestRouterModelsCatalogError(t *testing.T) {
	table := NewRouteTable()
	profile := routedCodex("https://gateway.example/v1", "gpt-5.6-sol", "gpt-5.6-sol")
	state, err := table.BindLaunch("s", profile, 1, verifiedAuth(profile))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table, WithRouterModelCatalog(func(string) ([]ProviderModelEntry, error) {
		return nil, ErrNotFound
	}))
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	base, _ := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)

	resp, err := http.Get(base + "/models")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), "route_not_found") {
		t.Fatalf("error code missing: %s", raw)
	}
}

// Owner-level end-to-end: GET /v1/models on a launched route serves the synced
// discovery catalog of that route's connection and never touches upstream.
func TestOwnerRouterServesSyncedModels(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("models request must be served locally, got %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(upstream.Close)

	owner := startTestOwner(t, readyLookup("x"))
	proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "conn-a", Name: "gateway-a.example", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: upstream.URL + "/v1",
		ModelID: "gpt-5.6-sol", Advanced: true,
	}, "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "conn-b", Name: "gateway-b.example", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: upstream.URL + "/v1",
		ModelID: "gpt-5.6-sol", Advanced: true,
	}, "", proj.Revision, true); err != nil {
		t.Fatal(err)
	}

	// Two connections, two discovery catalogs: each route answers with its own.
	// Both connections carry an explicit launch model so PrepareLaunch does not
	// fail closed; the catalog surface is independent of the bound model.
	owner.mu.Lock()
	owner.discovery = newModelDiscoveryCache()
	owner.discovery.put("conn-a", []string{"gpt-5.6-sol", "gpt-5.4-mini"}, nil)
	owner.discovery.put("conn-b", []string{"gpt-5.6-sol", "gpt-5.5"}, nil)
	owner.mu.Unlock()

	planA, err := owner.PrepareLaunch(ExecutorCodex, "conn-a", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(planA.ProvisionalID, "s1"); err != nil {
		t.Fatal(err)
	}
	planB, err := owner.PrepareLaunch(ExecutorCodex, "conn-b", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(planB.ProvisionalID, "s2"); err != nil {
		t.Fatal(err)
	}

	stateA, _ := owner.Table().Get("s1")
	stateB, _ := owner.Table().Get("s2")
	baseA, err := LoopbackCodexBaseURL(owner.ListenAddr(), stateA.Binding.RouteID)
	if err != nil {
		t.Fatal(err)
	}
	baseB, err := LoopbackCodexBaseURL(owner.ListenAddr(), stateB.Binding.RouteID)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		baseA + "/models": "gpt-5.6-sol,gpt-5.4-mini",
		baseB + "/models": "gpt-5.6-sol,gpt-5.5",
	}
	for url, wantIDs := range want {
		resp, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", url, resp.StatusCode, raw)
		}
		var list CodexModelsResponse
		if err := json.Unmarshal(raw, &list); err != nil {
			t.Fatal(err)
		}
		ids := make([]string, 0, len(list.Models))
		for _, entry := range list.Models {
			ids = append(ids, entry.Slug)
		}
		if strings.Join(ids, ",") != wantIDs {
			t.Fatalf("GET %s ids=%v want %q", url, ids, wantIDs)
		}
	}
}
