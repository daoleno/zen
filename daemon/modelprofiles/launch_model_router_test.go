package modelprofiles

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Regression at the launch boundary: the per-connection Codex model catalog
// file written before every routed Codex launch must carry evidence-based
// context windows (installed Codex cache first, daemon-owned pinned catalog
// second) and must never contain the fabricated 1-token window that drives
// constant "Context compacted", skill-budget warnings, and false tool
// failures.
func TestLaunchCodexModelCatalogFileNeverFabricatesContextWindowOne(t *testing.T) {
	installTestCodexModelCache(t, []CodexModelCatalogWireEntry{
		testCodexCacheEntry("gpt-5.6-sol", "GPT-5.6-Sol", ReasoningEffortMedium, ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh),
	})
	owner := startBuiltinVerifierOwner(t)
	proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "xcode", Name: "Xcode gateway", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: "https://gateway.example/v1",
		Advanced: true,
	}, "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	owner.discovery = newModelDiscoveryCache()
	owner.discovery.put("xcode", []string{"gpt-5.6-sol", "vendor/private-alpha"}, nil)
	owner.mu.Unlock()
	proj, err = owner.SetProviderDefault(ClientCodex, "xcode", "gpt-5.6-sol", proj.Revision)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, "xcode", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "xcode-session"); err != nil {
		t.Fatal(err)
	}

	path := owner.CodexModelCatalogPath("xcode")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read conn catalog %s: %v", path, err)
	}
	var resp CodexModelsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode conn catalog: %v", err)
	}
	windows := map[string]int64{}
	for _, entry := range resp.Models {
		windows[entry.Slug] = entry.ContextWindow
	}
	// Installed cache wins for the routed model (fixture 123456) over the
	// daemon-owned 272000; never the fabricated 1.
	if windows["gpt-5.6-sol"] != 123456 {
		t.Fatalf("gpt-5.6-sol context_window=%d want installed cache 123456: %s", windows["gpt-5.6-sol"], raw)
	}
	// Unknown models are never projected with a fabricated window.
	if windows["vendor/private-alpha"] != 0 {
		t.Fatalf("unknown model context_window=%d want 0 (omitted, native fallback)", windows["vendor/private-alpha"])
	}
	for slug, window := range windows {
		if window == 1 {
			t.Fatalf("entry %s must never carry the fabricated 1-token window: %s", slug, raw)
		}
	}
}

// Controlled integration proof for the unified identity invariant: a new
// Session launched with the UI-selected gpt-5.6-sol runs that EXACT model as
// the Codex session model (launch argv), the routed upstream model, and the
// model the CLI sends in request bodies — no hidden compatibility model.
func TestRoutedLaunchCarriesGpt56SolToUpstreamUnchanged(t *testing.T) {
	var upstreamModel string
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var obj map[string]any
		_ = json.Unmarshal(body, &obj)
		upstreamModel, _ = obj["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"r_1","object":"response"}`)
	})
	defer upstream.Close()

	owner := startTestOwner(t, readyLookup("x"))
	proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "cf-api-fan", Name: "gateway.example", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: upstream.URL + "/v1",
		Advanced: true,
	}, "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	owner.discovery = newModelDiscoveryCache()
	owner.discovery.put("cf-api-fan", []string{"gpt-5.6-sol", "gpt-5.6-terra"}, nil)
	owner.mu.Unlock()
	proj, err = owner.SetProviderDefault(ClientCodex, "cf-api-fan", "gpt-5.6-sol", proj.Revision)
	if err != nil {
		t.Fatal(err)
	}

	// New Session launch carrying the client-selected model.
	plan, err := owner.PrepareLaunchModel(ExecutorCodex, "cf-api-fan", "gpt-5.6-sol", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if plan.State.Binding.UpstreamModel != "gpt-5.6-sol" || plan.State.Binding.ClientModel != "gpt-5.6-sol" {
		t.Fatalf("binding model identity=%q/%q want gpt-5.6-sol/gpt-5.6-sol", plan.State.Binding.ClientModel, plan.State.Binding.UpstreamModel)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s1"); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(owner.router.Handler())
	defer srv.Close()
	base, err := LoopbackCodexBaseURL(srv.Listener.Addr().String(), plan.State.Binding.RouteID)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, base+"/responses",
		bytes.NewReader([]byte(`{"model":"gpt-5.6-sol","input":"hello"}`)))
	// The Codex CLI authenticates to the loopback with the placeholder; the
	// router strips it and injects the real upstream credential (AuthModeNone
	// here, so no auth header is forwarded upstream).
	req.Header.Set("Authorization", "Bearer "+LoopbackAuthPlaceholder)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if upstreamModel != "gpt-5.6-sol" {
		t.Fatalf("upstream received model=%q want gpt-5.6-sol (unified identity)", upstreamModel)
	}
	// The launch command carries the exact selected model as the Codex session
	// model, plus the deterministic per-route model catalog (no gpt-5 contract
	// anywhere).
	if !strings.Contains(plan.Command, "--model gpt-5.6-sol") {
		t.Fatalf("launch command must run the selected model: %q", plan.Command)
	}
	if !strings.Contains(plan.Command, "model_catalog_json=") {
		t.Fatalf("launch command must reference the per-route model catalog: %q", plan.Command)
	}
	if strings.Contains(plan.Command, "--model gpt-5 ") {
		t.Fatalf("launch command must not masquerade under gpt-5: %q", plan.Command)
	}
}
