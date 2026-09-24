package modelprofiles

import (
	"strings"
	"testing"
)

func TestClaudeProviderSelectionOwnsConnectionWithoutProviderModel(t *testing.T) {
	owner, _ := newDurableOwner(t, t.TempDir())
	t.Cleanup(func() { _ = owner.Close() })
	projection, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		Name: "Anthropic", Client: ClientClaude, PresetID: ProviderPresetAnthropic,
	}, "fixture-key", owner.Catalog().Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	connectionID := projection.Connections[0].ID
	projection, err = owner.SetProviderConnection(ClientClaude, connectionID, projection.Revision)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := projection.Defaults[ClientClaude]
	if !ok || got.ConnectionID != connectionID {
		t.Fatalf("Claude Provider projection owns an unexpected model: %#v", projection.Defaults)
	}
	plan, err := owner.PrepareLaunch(ExecutorClaude, "", "claude")
	if err != nil {
		t.Fatalf("selected connection without a Provider model must launch: %v", err)
	}
	if strings.Contains(plan.Command, "--model") || plan.State.Binding.UpstreamModel != "" {
		t.Fatalf("connection selection must preserve local Agent model flow: command=%q binding=%#v", plan.Command, plan.State.Binding)
	}
}
