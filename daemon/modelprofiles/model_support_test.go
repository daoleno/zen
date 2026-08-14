package modelprofiles

import (
	"strings"
	"testing"
)

// The exact reported regression: the UI-selected gpt-5.6-sol must reach the
// Codex launch configuration unchanged (route binding UpstreamModel), never a
// fabricated gpt-5 preset default.
func TestLaunchCarriesSelectedModelGpt56SolEndToEnd(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "cf-api-fan", Name: "gateway.example", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: "https://gateway.example/v1",
		Advanced: true,
	}, "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	owner.discovery = newModelDiscoveryCache()
	owner.discovery.put("cf-api-fan", []string{"gpt-5.6-sol", "gpt-5.6-terra", "codex-auto-review"}, nil)
	owner.mu.Unlock()

	// The client selects gpt-5.6-sol from the support allowlist.
	proj, err = owner.SetProviderDefault(ClientCodex, "cf-api-fan", "gpt-5.6-sol", proj.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if got := proj.Defaults[ClientCodex].ModelID; got != "gpt-5.6-sol" {
		t.Fatalf("client-selected model=%q", got)
	}

	// New Session launch (create_session carries the selection): the upstream
	// model in the compiled launch/binding must be gpt-5.6-sol unchanged.
	plan, err := owner.PrepareLaunchModel(ExecutorCodex, "cf-api-fan", "gpt-5.6-sol", "codex")
	if err != nil {
		t.Fatalf("launch with selected model: %v", err)
	}
	if plan.State.Binding.UpstreamModel != "gpt-5.6-sol" {
		t.Fatalf("binding upstream_model=%q want gpt-5.6-sol", plan.State.Binding.UpstreamModel)
	}
	if plan.Wire.ModelID != "gpt-5.6-sol" {
		t.Fatalf("wire model_id=%q want gpt-5.6-sol", plan.Wire.ModelID)
	}
	if !strings.Contains(plan.Command, "gpt-5") {
		t.Fatalf("launch command must carry the client contract model: %q", plan.Command)
	}
	// The CLI contract model may differ from the upstream model; the route
	// binding owns the real upstream model.
	if plan.State.Binding.ClientModel == plan.State.Binding.UpstreamModel {
		t.Fatalf("client contract must stay distinct from upstream: %q", plan.State.Binding.UpstreamModel)
	}
	// A stale preset default (gpt-5) must never be fabricated into the launch.
	if plan.State.Binding.UpstreamModel == "gpt-5" {
		t.Fatal("fabricated preset default gpt-5 reached the binding")
	}
}

// The support allowlist is client-owned: disabling a model is durable across
// rediscovery, genuinely new models default enabled, and /v1/models exposes
// only supported models.
func TestModelSupportAllowlistSurvivesRefresh(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "gw", Name: "gateway.example", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: "https://gateway.example/v1",
		Advanced: true,
	}, "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	owner.discovery = newModelDiscoveryCache()
	owner.discovery.put("gw", []string{"gpt-5.6-sol", "gpt-5.6-terra", "codex-auto-review"}, nil)
	owner.mu.Unlock()

	// Fresh discovery: every discovered model is supported by default.
	proj, err = owner.ProjectProviders()
	if err != nil {
		t.Fatal(err)
	}
	all := map[string]bool{}
	for _, m := range proj.Models["gw"] {
		all[m.ID] = m.Available
	}
	if !all["gpt-5.6-sol"] || !all["gpt-5.6-terra"] || !all["codex-auto-review"] {
		t.Fatalf("fresh catalog must enable every discovered model: %#v", proj.Models["gw"])
	}

	// The client explicitly disables gpt-5.6-sol; everything else stays on.
	proj, _, err = owner.SetProviderModelSupport("gw", []string{"gpt-5.6-terra", "codex-auto-review"})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]bool{}
	for _, m := range proj.Models["gw"] {
		byID[m.ID] = m.Available
	}
	if byID["gpt-5.6-sol"] {
		t.Fatal("explicitly disabled model must not be supported")
	}
	if !byID["gpt-5.6-terra"] || !byID["codex-auto-review"] {
		t.Fatalf("enabled models must stay supported: %#v", byID)
	}

	// Refresh (rediscovery) keeps the explicit disable and enables genuinely
	// new models.
	owner.mu.Lock()
	owner.discovery.put("gw", []string{"gpt-5.6-sol", "gpt-5.6-terra", "codex-auto-review", "gpt-6-new"}, nil)
	owner.mu.Unlock()
	proj, err = owner.ProjectProviders()
	if err != nil {
		t.Fatal(err)
	}
	byID = map[string]bool{}
	for _, m := range proj.Models["gw"] {
		byID[m.ID] = m.Available
	}
	if byID["gpt-5.6-sol"] {
		t.Fatal("disabled model silently re-enabled after refresh")
	}
	if !byID["gpt-6-new"] {
		t.Fatal("newly discovered model must default enabled")
	}
	if !byID["gpt-5.6-terra"] || !byID["codex-auto-review"] {
		t.Fatalf("enabled models lost support after refresh: %#v", byID)
	}

	// Route-scoped /v1/models exposes only supported models.
	profile, err := owner.GetProfile("gw")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := owner.modelsForRoute(profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, e := range entries {
		if e.Available {
			ids = append(ids, e.ID)
		}
	}
	if strings.Join(ids, ",") != "gpt-5.6-terra,codex-auto-review,gpt-6-new" {
		t.Fatalf("route /v1/models ids=%v want supported only", ids)
	}
}

// A launch whose selected model was explicitly disabled fails closed instead of
// routing a model the client turned off.
func TestLaunchFailsClosedWhenAllModelsDisabled(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	if _, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "gw", Name: "gateway.example", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: "https://gateway.example/v1",
		Advanced: true,
	}, "", 0, true); err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	owner.discovery = newModelDiscoveryCache()
	owner.discovery.put("gw", []string{"gpt-5.6-sol"}, nil)
	owner.mu.Unlock()
	proj, _, err := owner.SetProviderModelSupport("gw", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetProviderDefault(ClientCodex, "gw", "gpt-5.6-sol", proj.Revision); err != nil {
		t.Fatal(err)
	}
	_, err = owner.PrepareLaunch(ExecutorCodex, "gw", "codex")
	if err == nil {
		t.Fatal("launch with every model disabled must fail closed")
	}
}

// The effective Codex route decides whether official subscription usage is
// meaningful: a routed Provider/API-key connection suppresses it even when the
// host still has an official ChatGPT login cached.
func TestCodexRoutedDefaultAuthoritativeForStats(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	if owner.CodexRoutedDefault() {
		t.Fatal("direct official login must not report a routed codex default")
	}
	if _, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "gw", Name: "gateway.example", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: "https://gateway.example/v1",
		Advanced: true,
	}, "", 0, true); err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	owner.discovery = newModelDiscoveryCache()
	owner.discovery.put("gw", []string{"gpt-5.6-sol"}, nil)
	owner.mu.Unlock()
	proj, err := owner.ProjectProviders()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetProviderDefault(ClientCodex, "gw", "gpt-5.6-sol", proj.Revision); err != nil {
		t.Fatal(err)
	}
	if !owner.CodexRoutedDefault() {
		t.Fatal("routed codex default must be reported for stats suppression")
	}
	// Switching back to the direct official login clears the route.
	proj, err = owner.ProjectProviders()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetProviderDefault(ClientCodex, "", "", proj.Revision); err != nil {
		t.Fatal(err)
	}
	if owner.CodexRoutedDefault() {
		t.Fatal("direct official login after clearing default must not be routed")
	}
}

// SetProviderDefault preserves the client-selected model when the same
// connection remains the default, and never fabricates a preset default.
func TestSetProviderDefaultPreservesSelectedModel(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "gw", Name: "gateway.example", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: "https://gateway.example/v1",
		Advanced: true,
	}, "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	owner.discovery = newModelDiscoveryCache()
	owner.discovery.put("gw", []string{"gpt-5.6-sol"}, nil)
	owner.mu.Unlock()
	proj, err = owner.SetProviderDefault(ClientCodex, "gw", "gpt-5.6-sol", proj.Revision)
	if err != nil {
		t.Fatal(err)
	}
	// Re-selecting the same connection with an empty model must not reset the
	// client's selection to a fabricated preset default.
	proj, err = owner.SetProviderDefault(ClientCodex, "gw", "", proj.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if got := proj.Defaults[ClientCodex].ModelID; got != "gpt-5.6-sol" {
		t.Fatalf("selected model lost after re-selecting connection: %q", got)
	}
}
