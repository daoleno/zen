package modelprofiles

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func startBuiltinVerifierOwner(t *testing.T) *Owner {
	t.Helper()
	root := t.TempDir()
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath: filepath.Join(root, "model-profiles.toml"),
		RoutesPath:   filepath.Join(root, "route-bindings.json"),
		ListenerPath: filepath.Join(root, "route-listener.json"),
		Lookup:       readyLookup("x"),
		Verifier:     BuiltinEnvelopeVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	return owner
}

func TestUnknownCodexModelLaunchAndCatalogPassThrough(t *testing.T) {
	const model = "vendor/private-alpha"
	owner := startBuiltinVerifierOwner(t)
	projection, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "opaque-gateway", Name: "Opaque gateway", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: "https://gateway.example/v1",
		Advanced: true,
	}, "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	owner.discovery = newModelDiscoveryCache()
	owner.discovery.put("opaque-gateway", []string{"gpt-5.6-luna", "gpt-5.6-sol"}, nil)
	owner.mu.Unlock()

	projection, err = owner.SetProviderDefault(ClientCodex, "opaque-gateway", model, projection.Revision)
	if err != nil {
		t.Fatalf("set opaque default: %v", err)
	}
	plan, err := owner.PrepareLaunchModel(ExecutorCodex, "opaque-gateway", model, "codex")
	if err != nil {
		t.Fatalf("launch opaque model: %v", err)
	}
	if plan.State.Binding.ClientModel != model || plan.State.Binding.UpstreamModel != model {
		t.Fatalf("model identity changed: %#v", plan.State.Binding)
	}
	if plan.State.Binding.ClientModelProvenance != ContractProvenanceOpaquePassthrough {
		t.Fatalf("provenance=%q", plan.State.Binding.ClientModelProvenance)
	}
	if !strings.Contains(plan.Command, "--model "+model) {
		t.Fatalf("launch command=%q", plan.Command)
	}

	raw, err := json.Marshal(CodexModelsResponseForModels([]string{model}))
	if err != nil {
		t.Fatal(err)
	}
	var response CodexModelsResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatalf("generic catalog entry must parse: %v", err)
	}
	if len(response.Models) != 1 || response.Models[0].Slug != model || response.Models[0].DisplayName != model {
		t.Fatalf("catalog response=%#v", response)
	}
	if response.Models[0].SupportedReasoningLevels == nil {
		t.Fatal("generic catalog reasoning levels must be an explicit sequence")
	}
}

func TestUnknownCodexModelRuntimeAndEffectPassThrough(t *testing.T) {
	owner := startBuiltinVerifierOwner(t)
	projection, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "opaque-gateway", Name: "Opaque gateway", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: "https://gateway.example/v1",
		Advanced: true,
	}, "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	projection, err = owner.SetProviderDefault(ClientCodex, "opaque-gateway", "gpt-5.4", projection.Revision)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, "opaque-gateway", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "opaque-runtime"); err != nil {
		t.Fatal(err)
	}

	state, runtime, persist, err := owner.SetThreadRuntime("opaque-runtime", ThreadRuntimeChoice{
		ConnectionID: "opaque-gateway",
		ModelID:      "gpt-5.6",
		Effect:       ReasoningEffortMax,
	})
	if err != nil || !persist.Applied {
		t.Fatalf("runtime passthrough persist=%#v err=%v", persist, err)
	}
	if state.Binding.ClientModel != "gpt-5.6" || state.Binding.UpstreamModel != "gpt-5.6" || state.Binding.ReasoningEffort != ReasoningEffortMax {
		t.Fatalf("binding=%#v", state.Binding)
	}
	if runtime.Current == nil || runtime.Current.ModelID != "gpt-5.6" || runtime.Current.ReasoningEffort != ReasoningEffortMax {
		t.Fatalf("runtime=%#v", runtime.Current)
	}
	if _, _, _, err := owner.SetThreadRuntime("opaque-runtime", ThreadRuntimeChoice{
		ConnectionID: "opaque-gateway",
		ModelID:      "gpt-5.6",
		Effect:       "turbo",
	}); err == nil {
		t.Fatal("invalid effect vocabulary was admitted")
	}
}

func TestAccountCodexProjectionAddsKnownEffectMetadataWithoutGatingUnknownModels(t *testing.T) {
	codexHome := t.TempDir()
	volatileCache, err := json.Marshal(CodexModelsResponse{Models: []CodexModelCatalogWireEntry{
		{
			Slug:                  "gpt-5.6-sol",
			DisplayName:           "volatile host label",
			DefaultReasoningLevel: ReasoningEffortLow,
			SupportedReasoningLevels: []CodexReasoningEffortPreset{
				{Effort: ReasoningEffortLow},
				{Effort: ReasoningEffortMedium},
			},
			ContextWindow: 123456,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "models_cache.json"), volatileCache, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)

	owner := startBuiltinVerifierOwner(t)
	if _, err = owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "metadata-gateway", Name: "Metadata gateway", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: "https://gateway.example/v1",
		Advanced: true,
	}, "", 0, true); err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	owner.discovery = newModelDiscoveryCache()
	owner.discovery.put("metadata-gateway", []string{"gpt-5.6-sol", "vendor/private-alpha"}, nil)
	owner.mu.Unlock()

	projection, err := owner.ProjectProviders()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ProviderModelEntry{}
	for _, entry := range projection.Models["metadata-gateway"] {
		byID[entry.ID] = entry
	}
	known := byID["gpt-5.6-sol"]
	if !known.Known || known.DisplayName != "GPT-5.6-Sol" ||
		known.ReasoningEffortDefault != ReasoningEffortMedium ||
		len(known.ReasoningEfforts) != 5 {
		t.Fatalf("known metadata missing: %#v", known)
	}
	unknown := byID["vendor/private-alpha"]
	if unknown.Known || !unknown.Available || len(unknown.ReasoningEfforts) != 0 {
		t.Fatalf("unknown model must remain available without invented metadata: %#v", unknown)
	}
}
