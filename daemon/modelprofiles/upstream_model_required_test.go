package modelprofiles

import (
	"errors"
	"testing"
)

// Defect regression: Custom/Advanced connections created without a model must
// never fabricate the ClientModel contract id (e.g. gpt-5) into a RouteBinding.
// The route binding fails closed until a model is selected from discovery or
// set explicitly; probe compiles (discovery, test connection) keep working.

func TestCustomConnectionWithoutModelCannotBind(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "cf-api-fan", Name: "gateway.example", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: "https://gateway.example/v1",
		Advanced: true,
	}, "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := owner.GetProfile("cf-api-fan")
	if err != nil {
		t.Fatal(err)
	}
	if conn.Model != "" {
		t.Fatalf("durable connection must not own a model: %#v", conn)
	}

	// Probe compile (discovery / test-connection) still succeeds with the
	// placeholder — it never creates a binding.
	target, err := CompileConnectionTarget(conn, ClientCodex, "", "")
	if err != nil {
		t.Fatalf("probe compile: %v", err)
	}
	if !target.ModelPlaceholder || target.Model != "gpt-5" {
		t.Fatalf("probe must be marked placeholder: model=%q placeholder=%v", target.Model, target.ModelPlaceholder)
	}

	// Binding creation must fail closed with a clear error.
	_, err = owner.PrepareLaunch(ExecutorCodex, conn.ID, "codex")
	if !errors.Is(err, ErrUpstreamModelRequired) {
		t.Fatalf("launch without model: want ErrUpstreamModelRequired got %v", err)
	}
	if got := ControlErrorCode(err); got != CodeUpstreamModelRequired {
		t.Fatalf("control code=%q want %q", got, CodeUpstreamModelRequired)
	}

	// The failed launch must not leave a binding behind.
	if _, ok := owner.Table().Get("cf-api-fan"); ok {
		t.Fatal("failed launch left a route binding")
	}
	if owner.Catalog().Revision != proj.Revision {
		t.Fatalf("failed launch mutated catalog revision %d -> %d", proj.Revision, owner.Catalog().Revision)
	}
}

func TestSetProviderDefaultPreservesCompleteSeedAndRejectsIncompleteSwitch(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "custom-gw", Name: "gateway.example", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: "https://gateway.example/v1",
		Advanced: true,
	}, "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	proj, err = owner.SetProviderDefault(ClientCodex, "custom-gw", "gpt-5", proj.Revision)
	if err != nil {
		t.Fatal(err)
	}
	proj, err = owner.SetProviderDefault(ClientCodex, "custom-gw", "", proj.Revision)
	if err != nil {
		t.Fatalf("same connection must preserve its complete seed: %v", err)
	}
	if got := proj.Defaults[ClientCodex]; got.ConnectionID != "custom-gw" || got.ModelID != "gpt-5" {
		t.Fatalf("preserved default=%#v", got)
	}
	proj, err = owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "other-gw", Name: "other.example", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: "https://other.example/v1",
		Advanced: true,
	}, "", proj.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.SetProviderDefault(ClientCodex, "other-gw", "", proj.Revision); !errors.Is(err, ErrUpstreamModelRequired) {
		t.Fatalf("want ErrUpstreamModelRequired got %v", err)
	}
	current, projectErr := owner.ProjectProviders()
	if projectErr != nil {
		t.Fatal(projectErr)
	}
	if got := current.Defaults[ClientCodex]; got.ConnectionID != "custom-gw" || got.ModelID != "gpt-5" {
		t.Fatalf("rejected switch mutated default=%#v", got)
	}
}

func TestLaunchRejectsExplicitDefaultThatIsNoLongerSupported(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "cf-api-fan", Name: "gateway.example", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: "https://gateway.example/v1",
		Advanced: true,
	}, "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	// Upstream /v1/models already discovered: cf.api.fan codex group catalog.
	owner.mu.Lock()
	owner.discovery = newModelDiscoveryCache()
	owner.discovery.put("cf-api-fan", []string{
		"codex-auto-review", "gpt-5.4", "gpt-5.6-sol", "gpt-5.6-terra",
	}, nil)
	owner.mu.Unlock()

	// No client-selected model: the connection can still become the default
	// (connection selection is not a model choice), and launch falls back to
	// the first supported model deterministically.
	proj, err = owner.SetProviderDefault(ClientCodex, "cf-api-fan", "gpt-5.6-sol", proj.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if got := proj.Defaults[ClientCodex].ModelID; got != "gpt-5.6-sol" {
		t.Fatalf("client-selected model=%q", got)
	}

	plan, err := owner.PrepareLaunch(ExecutorCodex, "", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if plan.State.Binding.UpstreamModel != "gpt-5.6-sol" {
		t.Fatalf("binding upstream_model=%q want gpt-5.6-sol", plan.State.Binding.UpstreamModel)
	}
	if plan.Wire.ModelID != "gpt-5.6-sol" {
		t.Fatalf("wire model_id=%q", plan.Wire.ModelID)
	}

	// An unsupported model cannot replace the complete default seed.
	if _, err = owner.SetProviderDefault(ClientCodex, "cf-api-fan", "retired-model", proj.Revision); !errors.Is(err, ErrUpstreamModelRequired) {
		t.Fatalf("retired explicit default must be refused: %v", err)
	}
	plan, err = owner.PrepareLaunch(ExecutorCodex, "", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Wire.ModelID != "gpt-5.6-sol" {
		t.Fatalf("refused update changed default model: %q", plan.Wire.ModelID)
	}
}

func TestCustomConnectionExplicitModelBinds(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	if _, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "cf-api-fan", Name: "gateway.example", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: "https://gateway.example/v1",
		ModelID: "gpt-5.6-sol", Advanced: true,
	}, "", 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, "cf-api-fan", "codex")
	if err != nil {
		t.Fatalf("explicit model must bind: %v", err)
	}
	if plan.State.Binding.UpstreamModel != "gpt-5.6-sol" {
		t.Fatalf("binding upstream_model=%q want gpt-5.6-sol", plan.State.Binding.UpstreamModel)
	}
}

func TestSetThreadRuntimeWithoutModelFailsClosed(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	a := codexResponsesProfile("a", "gpt-5", "up-a")
	if _, err := owner.UpsertProfile(a, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "custom-gw", Name: "gateway.example", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: "https://gateway.example/v1",
		Advanced: true,
	}, "", 1, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, a.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s1"); err != nil {
		t.Fatal(err)
	}
	// Session activation with no model must not fabricate gpt-5 into the route.
	_, _, _, err = owner.SetThreadRuntime("s1", ThreadRuntimeChoice{ConnectionID: "custom-gw", ModelID: "", Effect: ""})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("want ErrInvalid got %v", err)
	}
	// The session keeps its original binding untouched.
	state, ok := owner.Table().Get("s1")
	if !ok || state.Binding.UpstreamModel != "up-a" {
		t.Fatalf("session binding changed after failed activate: %#v", state)
	}
	// Activation with an explicit discovered model succeeds.
	state, _, persist, err := owner.SetThreadRuntime("s1", ThreadRuntimeChoice{ConnectionID: "custom-gw", ModelID: "gpt-5.6-sol", Effect: ""})
	if err != nil || !persist.Applied {
		t.Fatalf("activate with model err=%v persist=%#v", err, persist)
	}
	if state.Binding.UpstreamModel != "gpt-5.6-sol" {
		t.Fatalf("binding upstream_model=%q", state.Binding.UpstreamModel)
	}
}
