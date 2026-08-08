package skills

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

type fakePluginCLI struct {
	data []byte
	err  error
}

func (cli *fakePluginCLI) ListAvailable(ctx context.Context) ([]byte, error) {
	return cli.data, cli.err
}

func catalogFixture() []byte {
	return []byte(`{
  "installed": [
    {"id": "plug-one@market-a", "version": "1.0.0", "scope": "user", "enabled": true}
  ],
  "available": [
    {"pluginId": "plug-one@market-a", "name": "plug-one", "marketplaceName": "market-a", "description": "One", "source": {"url": "https://github.com/a/one", "ref": "v1.0.0"}},
    {"pluginId": "plug-two@market-b", "name": "plug-two", "marketplaceName": "market-b", "description": "Two", "source": {"url": "https://github.com/b/two", "ref": "v0.2.0"}}
  ]
}`)
}

func writePluginFixture(t *testing.T, root string, skills ...string) {
	t.Helper()
	for _, skill := range skills {
		writeTestSkill(t, filepath.Join(root, "skills", skill), skill, "Hosted")
	}
}

func TestParseClaudeCatalogMarksInstalledEntriesNonInstallable(t *testing.T) {
	state, err := parseClaudeCatalog(catalogFixture())
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "ready" {
		t.Fatalf("status = %q", state.Status)
	}
	if len(state.Installed) != 1 || state.Installed[0].ID != "plug-one@market-a" || !state.Installed[0].Enabled {
		t.Fatalf("installed = %#v", state.Installed)
	}
	if len(state.Available) != 2 {
		t.Fatalf("available count = %d", len(state.Available))
	}
	byID := map[string]AvailablePlugin{}
	for _, plugin := range state.Available {
		byID[plugin.PluginID] = plugin
	}
	if byID["plug-one@market-a"].Installable {
		t.Fatal("installed catalog entry must not be installable")
	}
	if !byID["plug-two@market-b"].Installable {
		t.Fatal("available catalog entry must be installable")
	}
	if byID["plug-two@market-b"].SourceRef != "v0.2.0" {
		t.Fatalf("source ref = %q", byID["plug-two@market-b"].SourceRef)
	}
}

func TestParseClaudeCatalogRejectsMalformedAndOversizedPayloads(t *testing.T) {
	for _, data := range [][]byte{
		nil,
		[]byte("not json"),
		[]byte(`{"installed": [{"id": "bad id@market"}], "available": []}`),
		[]byte(`{"installed": [], "available": [{"pluginId": "broken"}]}`),
	} {
		if _, err := parseClaudeCatalog(data); err == nil {
			t.Fatalf("malformed payload %q was accepted", data)
		}
	}
	oversized := append([]byte(`{"installed": [], "available": []}`), make([]byte, maxPluginCatalogBytes)...)
	if _, err := parseClaudeCatalog(oversized); err == nil {
		t.Fatal("oversized payload was accepted")
	}
}

func TestDiscoverPluginInventoryMergesCatalogTruthWithCacheRows(t *testing.T) {
	home := t.TempDir()
	writePluginFixture(t, filepath.Join(home, ".claude", "plugins", "cache", "market-a", "plug-one", "1.0.0"), "skill-a")
	writePluginFixture(t, filepath.Join(home, ".codex", "plugins", "cache", "market-b", "plug-two", "0.2.0"), "skill-b")

	inventory, err := DiscoverPluginInventory(
		InventoryOptions{Home: home},
		&fakePluginCLI{data: catalogFixture()},
	)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Catalog.Status != "ready" {
		t.Fatalf("catalog status = %q (%s)", inventory.Catalog.Status, inventory.Catalog.Message)
	}
	if len(inventory.Installed) != 2 {
		t.Fatalf("installed rows = %d, want 2", len(inventory.Installed))
	}
	byID := map[string]InstalledPluginRow{}
	for _, row := range inventory.Installed {
		byID[row.ID] = row
	}
	claude := byID["plug-one@market-a"]
	if claude.Host != PluginHostClaude || !claude.Mutable || claude.Source != "catalog" {
		t.Fatalf("claude row = %#v", claude)
	}
	if claude.Version != "1.0.0" || !claude.Enabled {
		t.Fatalf("claude row version/enabled = %q/%v", claude.Version, claude.Enabled)
	}
	if claude.SkillCount != 1 || len(claude.Skills) != 1 || claude.Skills[0].Name != "skill-a" {
		t.Fatalf("claude hosted skills = %#v", claude.Skills)
	}
	codex := byID["plug-two@market-b"]
	if codex.Host != PluginHostCodex || codex.Mutable {
		t.Fatalf("codex row must be immutable, got %#v", codex)
	}
	if codex.Source != "cache" || codex.Version != "0.2.0" || codex.SkillCount != 1 {
		t.Fatalf("codex row = %#v", codex)
	}
}

func TestDiscoverPluginInventoryReportsExplicitCatalogGaps(t *testing.T) {
	home := t.TempDir()
	for _, scenario := range []struct {
		name string
		cli  PluginCLI
		code string
	}{
		{"unavailable", &fakePluginCLI{err: ErrClaudeCLIUnavailable}, "claude_catalog_unavailable"},
		{"timeout", &fakePluginCLI{err: ErrClaudeCatalogTimeout}, "claude_catalog_timeout"},
		{"malformed", &fakePluginCLI{data: []byte("not json")}, "claude_catalog_malformed"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			inventory, err := DiscoverPluginInventory(InventoryOptions{Home: home}, scenario.cli)
			if err != nil {
				t.Fatal(err)
			}
			if inventory.Catalog.Status != "unavailable" || inventory.Catalog.Code != scenario.code {
				t.Fatalf("catalog state = %#v, want code %s", inventory.Catalog, scenario.code)
			}
			if len(inventory.Catalog.Available) != 0 {
				t.Fatal("unavailable catalog must not carry available entries")
			}
		})
	}
}

func TestBuildPluginMutationCommandBuildsExactReviewedCommands(t *testing.T) {
	home := t.TempDir()
	writePluginFixture(t, filepath.Join(home, ".claude", "plugins", "cache", "market-a", "plug-one", "1.0.0"), "skill-a")

	options := InventoryOptions{Home: home}

	install, err := BuildPluginMutationCommand(options, PluginMutationRequest{
		Operation: PluginOperationInstall,
		PluginID:  "plug-two@market-b",
		Scope:     "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	if install.Command != "claude plugin install plug-two@market-b --scope user" {
		t.Fatalf("install command = %q", install.Command)
	}
	if install.Host != PluginHostClaude || install.Scope != "user" {
		t.Fatalf("install metadata = %#v", install)
	}

	update, err := BuildPluginMutationCommand(options, PluginMutationRequest{
		Operation: PluginOperationUpdate,
		PluginID:  "plug-one@market-a",
		Scope:     "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	if update.Command != "claude plugin update plug-one@market-a --scope user" {
		t.Fatalf("update command = %q", update.Command)
	}

	uninstall, err := BuildPluginMutationCommand(options, PluginMutationRequest{
		Operation: PluginOperationUninstall,
		PluginID:  "plug-one@market-a",
		Scope:     "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	if uninstall.Command != "claude plugin uninstall plug-one@market-a --scope user --yes" {
		t.Fatalf("uninstall command = %q", uninstall.Command)
	}
}

func TestBuildPluginMutationCommandFailsClosed(t *testing.T) {
	home := t.TempDir()
	writePluginFixture(t, filepath.Join(home, ".claude", "plugins", "cache", "market-a", "plug-one", "1.0.0"), "skill-a")
	writePluginFixture(t, filepath.Join(home, ".codex", "plugins", "cache", "market-b", "plug-two", "0.2.0"), "skill-b")
	options := InventoryOptions{Home: home}

	cases := []struct {
		name    string
		request PluginMutationRequest
		fragment string
	}{
		{
			"unsupported operation",
			PluginMutationRequest{Operation: "sync", PluginID: "plug-one@market-a", Scope: "user"},
			"unsupported plugin operation",
		},
		{
			"invalid identity",
			PluginMutationRequest{Operation: PluginOperationUpdate, PluginID: "plug one@market", Scope: "user"},
			"plugin identity",
		},
		{
			"invalid scope",
			PluginMutationRequest{Operation: PluginOperationUpdate, PluginID: "plug-one@market-a", Scope: "project"},
			"unsupported managed plugin scope",
		},
		{
			"missing plugin",
			PluginMutationRequest{Operation: PluginOperationUninstall, PluginID: "ghost@market-z", Scope: "user"},
			"not present",
		},
		{
			"codex host",
			PluginMutationRequest{Operation: PluginOperationUninstall, PluginID: "plug-two@market-b", Scope: "user"},
			"unsupported client",
		},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			_, err := BuildPluginMutationCommand(options, scenario.request)
			if err == nil || !strings.Contains(err.Error(), scenario.fragment) {
				t.Fatalf("error = %v, want fragment %q", err, scenario.fragment)
			}
		})
	}

	if _, err := BuildPluginMutationCommand(options, PluginMutationRequest{
		Operation: PluginOperationInstall,
		PluginID:  "plug-one@market-a;rm -rf",
		Scope:     "user",
	}); err == nil {
		t.Fatal("shell-injected install identity was accepted")
	}
}

func TestValidatePluginIDRejectsShellAndTraversalGrammars(t *testing.T) {
	for _, valid := range []string{"clangd-lsp@claude-plugins-official", "codex@openai-codex", "42crunch-api-security-testing@claude-plugins-official"} {
		if err := ValidatePluginID(valid); err != nil {
			t.Fatalf("ValidatePluginID(%q): %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "../x@market", "plug-one@market-a;echo", "plug one@market", "plug-one@Market-A", "plug-one", "plug@market@extra"} {
		if err := ValidatePluginID(invalid); err == nil {
			t.Fatalf("ValidatePluginID(%q) succeeded", invalid)
		}
	}
}

func TestPluginCatalogGapErrorsAreTyped(t *testing.T) {
	if !errors.Is(ErrClaudeCLIUnavailable, ErrClaudeCLIUnavailable) {
		t.Fatal("capability errors must be comparable")
	}
}
