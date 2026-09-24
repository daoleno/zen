package modelprofiles

import "testing"

func TestClaudeProviderSelectionOwnsConnectionButNotDefaultModel(t *testing.T) {
	owner, _ := newDurableOwner(t, t.TempDir())
	t.Cleanup(func() { _ = owner.Close() })
	projection, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		Name: "Anthropic", Client: ClientClaude, PresetID: ProviderPresetAnthropic,
	}, "fixture-key", owner.Catalog().Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	connectionID := projection.Connections[0].ID
	projection, err = owner.SetProviderDefault(ClientClaude, connectionID, "legacy-model", projection.Revision)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := projection.Defaults[ClientClaude]
	if !ok || got.ConnectionID != connectionID || got.ModelID != "" {
		t.Fatalf("Claude Provider projection owns an unexpected model: %#v", projection.Defaults)
	}
}
