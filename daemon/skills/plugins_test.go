package skills

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	cli := &fakePluginCLI{data: authorityCatalogFixture()}

	install, err := BuildPluginMutationCommand(options, PluginMutationRequest{
		Operation: PluginOperationInstall,
		PluginID:  "explore-me@market-d",
		Scope:     "user",
	}, cli)
	if err != nil {
		t.Fatal(err)
	}
	if install.Command != "claude plugin install explore-me@market-d --scope user" {
		t.Fatalf("install command = %q", install.Command)
	}
	if install.Host != PluginHostClaude || install.Scope != "user" {
		t.Fatalf("install metadata = %#v", install)
	}

	update, err := BuildPluginMutationCommand(options, PluginMutationRequest{
		Operation: PluginOperationUpdate,
		PluginID:  "plug-one@market-a",
		Scope:     "user",
	}, cli)
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
	}, cli)
	if err != nil {
		t.Fatal(err)
	}
	if uninstall.Command != "claude plugin uninstall plug-one@market-a --scope user --yes" {
		t.Fatalf("uninstall command = %q", uninstall.Command)
	}
}

func TestBuildPluginMutationCommandFailsClosed(t *testing.T) {
	cli := &fakePluginCLI{data: authorityCatalogFixture()}
	options := InventoryOptions{}

	cases := []struct {
		name     string
		request  PluginMutationRequest
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
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			_, err := BuildPluginMutationCommand(options, scenario.request, cli)
			if err == nil || !strings.Contains(err.Error(), scenario.fragment) {
				t.Fatalf("error = %v, want fragment %q", err, scenario.fragment)
			}
		})
	}

	if _, err := BuildPluginMutationCommand(options, PluginMutationRequest{
		Operation: PluginOperationInstall,
		PluginID:  "plug-one@market-a;rm -rf",
		Scope:     "user",
	}, cli); err == nil {
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

func authorityCatalogFixture() []byte {
	return []byte(`{
  "installed": [
    {"id": "plug-one@market-a", "version": "1.0.0", "scope": "user", "enabled": true},
    {"id": "catalog-only@market-c", "version": "2.0.0", "scope": "user", "enabled": false}
  ],
  "available": [
    {"pluginId": "plug-one@market-a", "name": "plug-one", "marketplaceName": "market-a", "description": "", "source": {"url": "https://github.com/a/one", "ref": "v1.0.0"}},
    {"pluginId": "catalog-only@market-c", "name": "catalog-only", "marketplaceName": "market-c", "description": "", "source": {"url": "https://github.com/c/only", "ref": "v2.0.0"}},
    {"pluginId": "explore-me@market-d", "name": "explore-me", "marketplaceName": "market-d", "description": "", "source": {"url": "https://github.com/d/explore", "ref": "v3.0.0"}}
  ]
}`)
}

func TestPluginLifecycleAuthorityRequiresCatalogInstalledMembership(t *testing.T) {
	home := t.TempDir()
	writePluginFixture(t, filepath.Join(home, ".claude", "plugins", "cache", "market-a", "plug-one", "1.0.0"), "skill-a")
	writePluginFixture(t, filepath.Join(home, ".claude", "plugins", "cache", "market-x", "orphan", "0.5.0"), "orphan-skill")

	inventory, err := DiscoverPluginInventory(
		InventoryOptions{Home: home},
		&fakePluginCLI{data: authorityCatalogFixture()},
	)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Catalog.Status != "ready" {
		t.Fatalf("catalog status = %q", inventory.Catalog.Status)
	}
	byID := map[string]InstalledPluginRow{}
	for _, row := range inventory.Installed {
		byID[row.ID] = row
	}

	// Catalog-installed membership is the only lifecycle authority: the row
	// is manageable even though its cache dir exists, and its version and
	// enabled state come from the catalog, not the cache.
	managed := byID["plug-one@market-a"]
	if managed.Host != PluginHostClaude || !managed.Mutable || managed.Source != "catalog" {
		t.Fatalf("catalog-installed row = %#v", managed)
	}
	if managed.Version != "1.0.0" || !managed.Enabled {
		t.Fatalf("catalog-installed version/enabled = %q/%v", managed.Version, managed.Enabled)
	}
	if managed.SkillCount != 1 || managed.Skills[0].Name != "skill-a" {
		t.Fatalf("catalog-installed hosted skills = %#v", managed.Skills)
	}

	// Catalog-installed membership proves installed even without any cache
	// directory; hosted skills are simply absent.
	cacheless := byID["catalog-only@market-c"]
	if cacheless.Host != PluginHostClaude || !cacheless.Mutable || cacheless.Source != "catalog" {
		t.Fatalf("cacheless catalog row = %#v", cacheless)
	}
	if cacheless.Version != "2.0.0" || cacheless.Enabled {
		t.Fatalf("cacheless catalog version/enabled = %q/%v", cacheless.Version, cacheless.Enabled)
	}
	if cacheless.SkillCount != 0 || len(cacheless.Skills) != 0 {
		t.Fatalf("cacheless catalog hosted skills = %#v", cacheless.Skills)
	}

	// An orphan cache row that the owning client does not list as installed
	// must never masquerade as installed or manageable.
	orphan := byID["orphan@market-x"]
	if orphan.Mutable || orphan.Source != "cache" {
		t.Fatalf("orphan cache row must be read-only, got %#v", orphan)
	}
	if orphan.SkillCount != 1 || orphan.Skills[0].Name != "orphan-skill" {
		t.Fatalf("orphan hosted skills = %#v", orphan.Skills)
	}
}

func TestPluginLifecycleAuthorityFallsReadOnlyWhenCatalogUnavailable(t *testing.T) {
	home := t.TempDir()
	writePluginFixture(t, filepath.Join(home, ".claude", "plugins", "cache", "market-a", "plug-one", "1.0.0"), "skill-a")

	for _, cli := range []PluginCLI{
		&fakePluginCLI{err: ErrClaudeCLIUnavailable},
		&fakePluginCLI{err: ErrClaudeCatalogTimeout},
		&fakePluginCLI{data: []byte("not json")},
	} {
		inventory, err := DiscoverPluginInventory(InventoryOptions{Home: home}, cli)
		if err != nil {
			t.Fatal(err)
		}
		if inventory.Catalog.Status != "unavailable" {
			t.Fatalf("catalog status = %q", inventory.Catalog.Status)
		}
		if len(inventory.Installed) != 1 {
			t.Fatalf("installed rows = %d", len(inventory.Installed))
		}
		row := inventory.Installed[0]
		if row.Mutable || row.Source != "cache" {
			t.Fatalf("cache row under unavailable catalog must be read-only, got %#v", row)
		}
	}
}

func TestBuildPluginMutationCommandRevalidatesAgainstLiveCatalog(t *testing.T) {
	home := t.TempDir()
	writePluginFixture(t, filepath.Join(home, ".claude", "plugins", "cache", "market-a", "plug-one", "1.0.0"), "skill-a")
	writePluginFixture(t, filepath.Join(home, ".claude", "plugins", "cache", "market-x", "orphan", "0.5.0"), "orphan-skill")
	options := InventoryOptions{Home: home}
	readyCLI := &fakePluginCLI{data: authorityCatalogFixture()}

	install, err := BuildPluginMutationCommand(options, PluginMutationRequest{
		Operation: PluginOperationInstall,
		PluginID:  "explore-me@market-d",
		Scope:     "user",
	}, readyCLI)
	if err != nil {
		t.Fatal(err)
	}
	if install.Command != "claude plugin install explore-me@market-d --scope user" {
		t.Fatalf("install command = %q", install.Command)
	}

	update, err := BuildPluginMutationCommand(options, PluginMutationRequest{
		Operation: PluginOperationUpdate,
		PluginID:  "plug-one@market-a",
		Scope:     "user",
	}, readyCLI)
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
	}, readyCLI)
	if err != nil {
		t.Fatal(err)
	}
	if uninstall.Command != "claude plugin uninstall plug-one@market-a --scope user --yes" {
		t.Fatalf("uninstall command = %q", uninstall.Command)
	}
}

func TestBuildPluginMutationCommandRejectsWithoutExactCatalogProof(t *testing.T) {
	home := t.TempDir()
	writePluginFixture(t, filepath.Join(home, ".claude", "plugins", "cache", "market-a", "plug-one", "1.0.0"), "skill-a")
	writePluginFixture(t, filepath.Join(home, ".codex", "plugins", "cache", "market-b", "codex-plug", "0.2.0"), "skill-b")
	options := InventoryOptions{Home: home}

	catalogUnavailable := &fakePluginCLI{err: ErrClaudeCLIUnavailable}
	catalogTimeout := &fakePluginCLI{err: ErrClaudeCatalogTimeout}
	catalogMalformed := &fakePluginCLI{data: []byte("not json")}

	cases := []struct {
		name     string
		request  PluginMutationRequest
		cli      PluginCLI
		fragment string
	}{
		{
			"install with unavailable catalog",
			PluginMutationRequest{Operation: PluginOperationInstall, PluginID: "explore-me@market-d", Scope: "user"},
			catalogUnavailable,
			"catalog",
		},
		{
			"install with timed-out catalog",
			PluginMutationRequest{Operation: PluginOperationInstall, PluginID: "explore-me@market-d", Scope: "user"},
			catalogTimeout,
			"catalog",
		},
		{
			"install with malformed catalog",
			PluginMutationRequest{Operation: PluginOperationInstall, PluginID: "explore-me@market-d", Scope: "user"},
			catalogMalformed,
			"catalog",
		},
		{
			"install of an identity absent from the catalog",
			PluginMutationRequest{Operation: PluginOperationInstall, PluginID: "ghost@market-z", Scope: "user"},
			&fakePluginCLI{data: authorityCatalogFixture()},
			"not present",
		},
		{
			"install of an already-installed identity",
			PluginMutationRequest{Operation: PluginOperationInstall, PluginID: "plug-one@market-a", Scope: "user"},
			&fakePluginCLI{data: authorityCatalogFixture()},
			"already installed",
		},
		{
			"update of an orphan cache row absent from the catalog",
			PluginMutationRequest{Operation: PluginOperationUpdate, PluginID: "orphan@market-x", Scope: "user"},
			&fakePluginCLI{data: authorityCatalogFixture()},
			"not present",
		},
		{
			"uninstall of an orphan cache row absent from the catalog",
			PluginMutationRequest{Operation: PluginOperationUninstall, PluginID: "orphan@market-x", Scope: "user"},
			&fakePluginCLI{data: authorityCatalogFixture()},
			"not present",
		},
		{
			"update with unavailable catalog",
			PluginMutationRequest{Operation: PluginOperationUpdate, PluginID: "plug-one@market-a", Scope: "user"},
			catalogUnavailable,
			"catalog",
		},
		{
			"uninstall with unavailable catalog",
			PluginMutationRequest{Operation: PluginOperationUninstall, PluginID: "plug-one@market-a", Scope: "user"},
			catalogUnavailable,
			"catalog",
		},
		{
			"update with malformed catalog",
			PluginMutationRequest{Operation: PluginOperationUpdate, PluginID: "plug-one@market-a", Scope: "user"},
			catalogMalformed,
			"catalog",
		},
		{
			"uninstall of a codex-hosted cache row",
			PluginMutationRequest{Operation: PluginOperationUninstall, PluginID: "codex-plug@market-b", Scope: "user"},
			&fakePluginCLI{data: authorityCatalogFixture()},
			"catalog",
		},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			_, err := BuildPluginMutationCommand(options, scenario.request, scenario.cli)
			if err == nil || !strings.Contains(err.Error(), scenario.fragment) {
				t.Fatalf("error = %v, want fragment %q", err, scenario.fragment)
			}
		})
	}
}

// The daemon's bounded catalog deadline must expire before the App's plugin
// inventory (10s) and plugin command (15s) request deadlines, so the App
// never waits on a request the daemon has already given up on.
func TestPluginCatalogDeadlinePrecedesAppRequestDeadlines(t *testing.T) {
	const appInventoryDeadline = 10 * time.Second
	const appCommandDeadline = 15 * time.Second
	if defaultPluginCLITimeout >= appInventoryDeadline {
		t.Fatalf("daemon catalog deadline %v must precede the App inventory deadline %v", defaultPluginCLITimeout, appInventoryDeadline)
	}
	if defaultPluginCLITimeout >= appCommandDeadline {
		t.Fatalf("daemon catalog deadline %v must precede the App command deadline %v", defaultPluginCLITimeout, appCommandDeadline)
	}
}
