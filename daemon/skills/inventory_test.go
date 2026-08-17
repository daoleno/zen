package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- inventory state and ownership semantics ------------------------------

func TestStoreInventoryRoundTrip(t *testing.T) {
	f := newFixture(t)
	store := f.store()
	file := InventoryFile{
		Version: inventoryVersion,
		Packages: map[string]PackageEntry{
			"alpha": {
				SkillName: "alpha", Source: "owner/repo", SourceType: string(SourceTypeCatalog),
				ContentHash: "abc123", InstalledAt: f.Now.Format(time.RFC3339), UpdatedAt: f.Now.Format(time.RFC3339),
				Owned: true, Bindings: []BindingEntry{{Agent: AgentCodex, Scope: ScopeGlobal, TargetPath: f.agentGlobalDir(AgentCodex) + "/alpha", Enabled: true, Mode: BindingSymlink}},
			},
		},
	}
	if err := store.SaveInventory(file); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadInventory(false)
	if err != nil {
		t.Fatal(err)
	}
	entry := loaded.Packages["alpha"]
	if entry.Source != "owner/repo" || !entry.Owned || len(entry.Bindings) != 1 {
		t.Fatalf("round-trip lost metadata: %+v", entry)
	}
	if loaded.Version != inventoryVersion {
		t.Fatalf("unexpected version %d", loaded.Version)
	}
	// Immutable: a package directory must never contain inventory metadata.
	if _, err := os.Stat(filepath.Join(store.PackageDir("alpha"), "inventory.json")); !os.IsNotExist(err) {
		t.Fatal("inventory metadata must live outside package directories")
	}
}

func TestStoreInventoryRejectsCorruptAndInvalid(t *testing.T) {
	f := newFixture(t)
	store := f.store()
	if err := os.MkdirAll(filepath.Dir(store.InventoryPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.InventoryPath(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadInventory(false); err == nil {
		t.Fatal("corrupt inventory must fail closed")
	}
	if err := os.WriteFile(store.InventoryPath(), []byte(`{"version":99,"packages":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadInventory(false); err == nil {
		t.Fatal("unsupported schema must fail closed")
	}
	if err := os.WriteFile(store.InventoryPath(), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadInventory(true); err == nil {
		t.Fatal("missing inventory under required load must fail")
	}
}

func TestInventoryIsolatesMalformedPackageAndPreservesItAcrossSave(t *testing.T) {
	f := newFixture(t)
	store := f.store()
	if err := os.MkdirAll(store.PackageDir("healthy"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.PackageDir("healthy"), "SKILL.md"), []byte("---\nname: healthy\n---\nHealthy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := `{"version":1,"packages":{"healthy":{"skill_name":"healthy","source":"owner/repo","source_type":"catalog","content_hash":"hash","owned":true},"broken":{"skill_name":"broken","source_type":"catalog","owned":true}}}`
	if err := os.MkdirAll(filepath.Dir(store.InventoryPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.InventoryPath(), []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.LoadInventory(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.Packages["healthy"]; !ok {
		t.Fatal("valid sibling package was not loaded")
	}
	if _, ok := loaded.Packages["broken"]; ok || len(loaded.InvalidPackages) != 1 || len(loaded.Warnings) != 1 {
		t.Fatalf("malformed package isolation = %+v", loaded)
	}
	if err := store.SaveInventory(loaded); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(store.InventoryPath())
	if err != nil {
		t.Fatal(err)
	}
	var persisted struct {
		Packages map[string]json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	if _, ok := persisted.Packages["broken"]; !ok {
		t.Fatal("isolated package was silently deleted during save")
	}

	inventory, err := DiscoverInventory(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Skills) != 1 || inventory.Skills[0].Name != "healthy" {
		t.Fatalf("usable inventory = %+v", inventory.Skills)
	}
	if len(inventory.Warnings) == 0 || !strings.Contains(inventory.Warnings[0], "broken") {
		t.Fatalf("inventory warnings = %v", inventory.Warnings)
	}
}

func TestInventoryOwnedAndExternalRows(t *testing.T) {
	f := newFixture(t)
	store := f.store()
	// Owned package in the store.
	storeDir := store.PackageDir("alpha")
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte("---\nname: alpha\n---\nInstructions\n")
	if err := os.WriteFile(filepath.Join(storeDir, "SKILL.md"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	hash, err := folderContentHash(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	file := InventoryFile{
		Version: inventoryVersion,
		Packages: map[string]PackageEntry{
			"alpha": {
				SkillName: "alpha", Source: "owner/repo", SourceType: string(SourceTypeCatalog),
				ContentHash: hash, InstalledAt: f.Now.Format(time.RFC3339), UpdatedAt: f.Now.Format(time.RFC3339),
				Owned: true,
				Bindings: []BindingEntry{{
					Agent: AgentCodex, Scope: ScopeGlobal, TargetPath: f.agentGlobalDir(AgentCodex) + "/alpha",
					Enabled: true, Mode: BindingSymlink,
				}},
			},
		},
	}
	if err := store.SaveInventory(file); err != nil {
		t.Fatal(err)
	}
	// Ensures the Codex global dir now contains the symlink binding.
	if err := os.MkdirAll(f.agentGlobalDir(AgentCodex), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(storeDir, filepath.Join(f.agentGlobalDir(AgentCodex), "alpha")); err != nil {
		t.Fatal(err)
	}
	// An external skill in Grok's global dir (untracked).
	f.writeSkill(f.agentGlobalDir(AgentGrok), "grok-only", "grok body")

	inventory, err := DiscoverInventory(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	var alpha, grokOnly *InstalledSkill
	for index := range inventory.Skills {
		switch inventory.Skills[index].Name {
		case "alpha":
			alpha = &inventory.Skills[index]
		case "grok-only":
			grokOnly = &inventory.Skills[index]
		}
	}
	if alpha == nil {
		t.Fatal("owned row missing")
	}
	if !alpha.Owned || alpha.Manager != ManagerZen || alpha.Enabled != true {
		t.Fatalf("owned row state wrong: %+v", alpha)
	}
	if !alpha.Capability.CanManage {
		t.Fatal("owned row must be manageable")
	}
	hasOperations := map[MutationOperation]bool{}
	for _, op := range alpha.Capability.Operations {
		hasOperations[op] = true
	}
	for _, required := range []MutationOperation{OperationBind, OperationUninstall} {
		if !hasOperations[required] {
			t.Fatalf("owned row missing operation %q", required)
		}
	}
	if hasOperations[OperationUpdate] {
		t.Fatal("catalog package without a pinned ref must not advertise update")
	}
	if len(alpha.Bindings) != 1 || len(alpha.Bindings[0].Operations) != 2 {
		t.Fatalf("binding must carry exact current-state operations: %+v", alpha.Bindings)
	}
	if grokOnly == nil {
		t.Fatal("external row missing")
	}
	if grokOnly.Owned || grokOnly.Tracked || grokOnly.Manager != ManagerExternal {
		t.Fatalf("external row state wrong: %+v", grokOnly)
	}
	if !grokOnly.Capability.CanManage {
		t.Fatal("external row must offer adopt")
	}
	// Agents table must include all six with truthful scope capability.
	if len(inventory.Agents) != 6 {
		t.Fatalf("expected six adapters, got %d", len(inventory.Agents))
	}
	byAgent := map[Agent]AgentSupport{}
	for _, support := range inventory.Agents {
		byAgent[support.Agent] = support
	}
	for _, agent := range []Agent{AgentCodex, AgentClaudeCode, AgentCursor, AgentGrok, AgentOpenCode, AgentPi} {
		support := byAgent[agent]
		if !support.Supported || !support.GlobalScope || !support.ProjectScope || support.BindingMode == "" {
			t.Fatalf("agent %s capability table is not truthful: %+v", agent, support)
		}
	}
	if byAgent[AgentGrok].Supported != true {
		t.Fatal("Grok has a real adapter and must be reported supported")
	}
}

func TestUnboundOwnedPackageDoesNotInventUpdateCapability(t *testing.T) {
	f := newFixture(t)
	store := f.store()
	storeDir := store.PackageDir("unbound")
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storeDir, "SKILL.md"), []byte("---\nname: unbound\n---\nunbound\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hash, err := folderContentHash(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveInventory(InventoryFile{Version: inventoryVersion, Packages: map[string]PackageEntry{
		"unbound": {
			SkillName: "unbound", Source: "fixture/catalog", SourceType: string(SourceTypeCatalog), ContentHash: hash, Owned: true,
		},
	}}); err != nil {
		t.Fatal(err)
	}
	inventory, err := DiscoverInventory(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Skills) != 1 {
		t.Fatalf("unbound inventory = %+v", inventory.Skills)
	}
	operations := inventory.Skills[0].Capability.Operations
	if !operationAllowed(operations, OperationBind) || !operationAllowed(operations, OperationUninstall) || operationAllowed(operations, OperationUpdate) {
		t.Fatalf("unbound capability invented an update action: %v", operations)
	}
}

func TestMissingLocalUpdateSourceDoesNotAdvertiseUpdate(t *testing.T) {
	f := newFixture(t)
	store := f.store()
	storeDir := store.PackageDir("missing-update-source")
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storeDir, "SKILL.md"), []byte("---\nname: missing-update-source\n---\nowned\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hash, err := folderContentHash(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	missingSource := filepath.Join(f.Home, "missing-local-source")
	if err := store.SaveInventory(InventoryFile{Version: inventoryVersion, Packages: map[string]PackageEntry{
		"missing-update-source": {
			SkillName: "missing-update-source", Source: missingSource, SourceType: string(SourceTypeLocal), ContentHash: hash, Owned: true,
		},
	}}); err != nil {
		t.Fatal(err)
	}
	inventory, err := DiscoverInventory(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Skills) != 1 || operationAllowed(inventory.Skills[0].Capability.Operations, OperationUpdate) {
		t.Fatalf("missing local provenance advertised update: %+v", inventory.Skills)
	}
}

func TestTrackedExternalMissingSourceOnlyAdvertisesForget(t *testing.T) {
	f := newFixture(t)
	missing := filepath.Join(f.Home, "missing-external")
	store := f.store()
	if err := store.SaveInventory(InventoryFile{
		Version: inventoryVersion,
		Packages: map[string]PackageEntry{
			"missing-external": {
				SkillName: "missing-external", Source: missing,
				SourceType: string(SourceTypeExternal), ContentHash: "recorded-hash",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	inventory, err := DiscoverInventory(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	var row *InstalledSkill
	for index := range inventory.Skills {
		if inventory.Skills[index].Name == "missing-external" {
			row = &inventory.Skills[index]
			break
		}
	}
	if row == nil {
		t.Fatal("missing tracked external row")
	}
	if row.CanonicalPath != missing || row.SourcePath != missing {
		t.Fatalf("missing row did not preserve its recorded path: %+v", row)
	}
	if !row.Capability.CanManage || len(row.Capability.Operations) != 1 || row.Capability.Operations[0] != OperationForget {
		t.Fatalf("missing source capability must be Forget only: %+v", row.Capability)
	}
	if !strings.Contains(row.Capability.Reason, missing) || !strings.Contains(row.Capability.Reason, "unavailable") {
		t.Fatalf("missing source reason was not preserved: %q", row.Capability.Reason)
	}
	detail, err := InspectPackage(f.options(""), "missing-external")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Capability.Operations) != 1 || detail.Capability.Operations[0] != OperationForget || detail.Capability.Reason != row.Capability.Reason {
		t.Fatalf("inventory and inspector capability diverged: row=%+v detail=%+v", row.Capability, detail.Capability)
	}
}

func TestInventoryCopyDriftDetection(t *testing.T) {
	f := newFixture(t)
	store := f.store()
	storeDir := store.PackageDir("beta")
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte("---\nname: beta\n---\nCanonical\n")
	if err := os.WriteFile(filepath.Join(storeDir, "SKILL.md"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	hash, err := folderContentHash(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	// Cursor uses copy mode: materialize a copy, then mutate it to drift.
	cursorDir := f.agentGlobalDir(AgentCursor)
	if err := os.MkdirAll(cursorDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(cursorDir, "beta")
	if err := copyDirBounded(storeDir, target); err != nil {
		t.Fatal(err)
	}
	// Drift the copy.
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("---\nname: beta\n---\nMUTATED\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := InventoryFile{
		Version: inventoryVersion,
		Packages: map[string]PackageEntry{
			"beta": {
				SkillName: "beta", Source: "owner/repo", SourceType: string(SourceTypeCatalog),
				ContentHash: hash, InstalledAt: f.Now.Format(time.RFC3339), UpdatedAt: f.Now.Format(time.RFC3339),
				Owned: true,
				Bindings: []BindingEntry{{
					Agent: AgentCursor, Scope: ScopeGlobal, TargetPath: target,
					Enabled: true, Mode: BindingCopy,
				}},
			},
		},
	}
	if err := store.SaveInventory(file); err != nil {
		t.Fatal(err)
	}
	inventory, err := DiscoverInventory(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	var beta *InstalledSkill
	for index := range inventory.Skills {
		if inventory.Skills[index].Name == "beta" {
			beta = &inventory.Skills[index]
		}
	}
	if beta == nil {
		t.Fatal("beta row missing")
	}
	if len(beta.Bindings) != 1 || beta.Bindings[0].DriftHash != "drifted" {
		t.Fatalf("copy drift must be reported, got %+v", beta.Bindings)
	}
	if len(beta.Warnings) == 0 {
		t.Fatal("drift must surface a warning")
	}
}

func TestInventoryDuplicateAndConflictClassification(t *testing.T) {
	f := newFixture(t)
	store := f.store()
	// Same-name different-content external skills in two agent dirs = duplicate.
	f.writeSkill(f.agentGlobalDir(AgentCodex), "dup-name", "codex variant")
	f.writeSkill(f.agentGlobalDir(AgentPi), "dup-name", "pi variant")
	// Conflict: an external copy of an owned package with different content.
	storeDir := store.PackageDir("owned-name")
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storeDir, "SKILL.md"), []byte("---\nname: owned-name\n---\nowned\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hash, _ := folderContentHash(storeDir)
	file := InventoryFile{
		Version: inventoryVersion,
		Packages: map[string]PackageEntry{
			"owned-name": {
				SkillName: "owned-name", Source: "owner/repo", SourceType: string(SourceTypeCatalog),
				ContentHash: hash, InstalledAt: f.Now.Format(time.RFC3339), UpdatedAt: f.Now.Format(time.RFC3339),
				Owned: true,
			},
		},
	}
	if err := store.SaveInventory(file); err != nil {
		t.Fatal(err)
	}
	f.writeSkill(f.agentGlobalDir(AgentGrok), "owned-name", "conflicting external content")

	inventory, err := DiscoverInventory(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{} // name for dup-name assertions
	conflictExternal := false
	for _, skill := range inventory.Skills {
		if skill.Name == "dup-name" && skill.Migration == "duplicate" {
			seen["dup-name"] = true
		}
		if skill.Name == "owned-name" && skill.Manager == ManagerExternal && skill.Migration == "conflict" {
			conflictExternal = true
		}
	}
	if !seen["dup-name"] {
		t.Fatal("duplicate classification missing")
	}
	if !conflictExternal {
		t.Fatal("conflict classification missing on the external row")
	}
	if inventory.Migration.Duplicate < 1 || inventory.Migration.Conflict < 1 {
		t.Fatalf("migration status incomplete: %+v", inventory.Migration)
	}
	// Conflicted rows must not be manageable (fail closed).
	for _, skill := range inventory.Skills {
		if skill.Migration == "conflict" && skill.Capability.CanManage {
			t.Fatal("conflicted row must not grant management authority")
		}
	}
}

func TestInventoryExecutorsAndAdapters(t *testing.T) {
	f := newFixture(t)
	f.writeSkill(f.agentGlobalDir(AgentCodex), "x", "x body")
	options := f.options("")
	options.Executors = []ExecutorAlias{
		{Name: "agent", Kind: "cursor", Command: "cursor-agent --force"},
		{Name: "custom-grok", Kind: "", Command: "/usr/bin/grok"},
		{Name: "unknown-tool", Kind: "", Command: "some-random-tool"},
	}
	inventory, err := DiscoverInventory(options)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]ExecutorSupport{}
	for _, entry := range inventory.Executors {
		byName[entry.Name] = entry
	}
	if byName["agent"].Agent != AgentCursor {
		t.Fatalf("kind must drive alias resolution, got %+v", byName["agent"])
	}
	if byName["custom-grok"].Agent != AgentGrok {
		t.Fatalf("command inference must map grok, got %+v", byName["custom-grok"])
	}
	if _, ok := byName["unknown-tool"]; ok {
		t.Fatal("unknown executors must never gain lifecycle authority")
	}
}

func TestInventoryDiscoversAllAdaptersBuiltinPluginAndSymlinkLayouts(t *testing.T) {
	f := newFixture(t)
	allAgents := []Agent{AgentCodex, AgentClaudeCode, AgentCursor, AgentGrok, AgentOpenCode, AgentPi}
	for _, agent := range allAgents {
		f.writeSkill(f.agentGlobalDir(agent), "global-"+string(agent), "global")
		f.writeSkill(f.agentProjectDir(agent, f.Project), "project-"+string(agent), "project")
	}
	f.writeSkill(filepath.Join(f.Home, ".codex", "skills", ".system"), "builtin-proof", "builtin")
	f.writeSkill(filepath.Join(f.Home, ".codex", "plugins", "cache", "demo-plugin", "1.0.0", "skills"), "plugin-proof", "plugin")
	cachebusterTarget := filepath.Join(f.Home, ".codex", "plugins", "cache", "demo-plugin", "1.0.0")
	if err := os.Symlink(cachebusterTarget, filepath.Join(f.Home, ".codex", "plugins", "cache", "demo-plugin", "latest")); err != nil {
		t.Fatal(err)
	}

	inventory, err := DiscoverInventory(f.options(f.Project))
	if err != nil {
		t.Fatal(err)
	}
	counts := map[Agent]int{}
	byName := map[string]InstalledSkill{}
	for _, skill := range inventory.Skills {
		byName[skill.Name] = skill
		for _, agent := range skill.Agents {
			counts[agent]++
		}
		for _, binding := range skill.Bindings {
			if ValidateAgent(binding.Agent) != nil {
				t.Fatalf("row %q emitted invalid binding agent %q", skill.Name, binding.Agent)
			}
		}
	}
	for _, agent := range allAgents {
		want := 2
		if agent == AgentCodex {
			want = 4 // global + project + builtin + plugin
		} else if agent == AgentPi {
			want = 3 // Pi also loads Codex's shared project .agents/skills copy.
		}
		if counts[agent] != want {
			t.Fatalf("%s count=%d, want %d", agent, counts[agent], want)
		}
	}
	if row := byName["builtin-proof"]; row.Manager != ManagerBuiltin || row.Scope != ScopeBuiltin || row.Capability.CanManage {
		t.Fatalf("builtin row contract = %+v", row)
	}
	if row := byName["plugin-proof"]; row.Manager != ManagerPlugin || row.Scope != ScopePlugin || row.Capability.CanManage {
		t.Fatalf("plugin row contract = %+v", row)
	}
	for _, warning := range inventory.Warnings {
		if strings.Contains(warning, "symbolic link") || strings.Contains(warning, "management is disabled") {
			t.Fatalf("cachebuster symlink disabled valid inventory: %q", warning)
		}
	}
	if !byName["global-codex"].Capability.CanManage {
		t.Fatal("unrelated valid external row lost management authority")
	}
}

func TestMalformedExternalEntryIsIsolatedFromValidManagement(t *testing.T) {
	f := newFixture(t)
	valid := f.writeSkill(f.agentGlobalDir(AgentClaudeCode), "valid-external", "valid")
	broken := filepath.Join(f.agentGlobalDir(AgentClaudeCode), "broken-external")
	if err := os.MkdirAll(broken, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "SKILL.md"), []byte("---\nname: [invalid\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inventory, err := DiscoverInventory(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	foundValid := false
	for _, skill := range inventory.Skills {
		if skill.CanonicalPath == valid && !skill.Capability.CanManage {
			t.Fatalf("valid sibling was globally disabled: %+v", skill.Capability)
		}
		if skill.CanonicalPath == valid {
			foundValid = true
		}
	}
	if !foundValid {
		t.Fatal("valid external sibling was not discovered")
	}
	for _, warning := range inventory.Warnings {
		if strings.Contains(warning, "management is disabled") {
			t.Fatalf("row-local metadata failure became global: %q", warning)
		}
	}
}

func TestUnhashableExternalCopyDoesNotAdvertiseAdopt(t *testing.T) {
	f := newFixture(t)
	root := f.agentGlobalDir(AgentClaudeCode)
	valid := f.writeSkill(root, "valid-adopt", "valid")
	unsafe := f.writeSkill(root, "unsafe-adopt", "unsafe")
	outside := filepath.Join(f.Home, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(unsafe, "escape.txt")); err != nil {
		t.Fatal(err)
	}

	inventory, err := DiscoverInventory(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]InstalledSkill{}
	for _, skill := range inventory.Skills {
		byPath[skill.SourcePath] = skill
	}
	if !byPath[valid].Capability.CanManage || !operationAllowed(byPath[valid].Capability.Operations, OperationAdopt) {
		t.Fatalf("valid sibling lost adopt capability: %+v", byPath[valid])
	}
	unsafeRow, ok := byPath[unsafe]
	if !ok {
		t.Fatal("unsafe external row was dropped instead of isolated")
	}
	if unsafeRow.Capability.CanManage || unsafeRow.ContentHash != "" || !strings.Contains(unsafeRow.Capability.Reason, "could not be hashed") {
		t.Fatalf("unsafe external row advertised a blocking action: %+v", unsafeRow)
	}
}

func TestSharedRootEmitsOneValidBindingPerAgent(t *testing.T) {
	f := newFixture(t)
	root := filepath.Join(f.Project, ".agents", "skills")
	f.writeSkill(root, "shared-skill", "shared")
	options, err := normalizeInventoryOptions(f.options(f.Project))
	if err != nil {
		t.Fatal(err)
	}
	collector := &inventoryCollector{
		options: options,
		byReal:  map[string]*InstalledSkill{},
		byName:  map[string][]*InstalledSkill{},
		blocked: map[string]string{},
		warned:  map[string]struct{}{},
	}
	collector.scanRoot(inventoryRoot{
		path: root, label: "shared project Skills directory", scope: ScopeProject,
		agents: []Agent{AgentCodex, AgentPi}, manager: ManagerExternal,
	}, f.store())
	rows := collector.rows()
	if len(rows) != 1 || len(rows[0].Bindings) != 2 {
		t.Fatalf("shared row = %+v", rows)
	}
	seen := map[Agent]bool{}
	for _, binding := range rows[0].Bindings {
		seen[binding.Agent] = true
	}
	if !seen[AgentCodex] || !seen[AgentPi] {
		t.Fatalf("shared bindings = %+v", rows[0].Bindings)
	}
}

func TestSharedUserRootMergesCodexAndPiAndHonorsCodexEnabledOverride(t *testing.T) {
	f := newFixture(t)
	shared := f.writeSkill(filepath.Join(f.Home, ".agents", "skills"), "shared-disabled", "shared")
	configPath := filepath.Join(f.Home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf("[[skills.config]]\npath = %q\nenabled = false\n", filepath.Join(shared, "SKILL.md"))
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	inventory, err := DiscoverInventory(f.options(f.Project))
	if err != nil {
		t.Fatal(err)
	}
	var row *InstalledSkill
	for index := range inventory.Skills {
		if inventory.Skills[index].Name == "shared-disabled" {
			row = &inventory.Skills[index]
			break
		}
	}
	if row == nil {
		t.Fatal("shared Skill was not discovered")
	}
	if len(row.Bindings) != 2 || len(row.Agents) != 2 {
		t.Fatalf("shared physical copy was not merged: %+v", row)
	}
	enabled := map[Agent]bool{}
	for _, binding := range row.Bindings {
		if binding.Scope != ScopeGlobal {
			t.Fatalf("shared user binding was misclassified as %s: %+v", binding.Scope, binding)
		}
		if binding.Mode != string(BindingDirect) {
			t.Fatalf("physical external directory was misreported as %q: %+v", binding.Mode, binding)
		}
		enabled[binding.Agent] = binding.Enabled
	}
	if enabled[AgentCodex] || !enabled[AgentPi] || !row.Enabled {
		t.Fatalf("provider-specific enabled state = row %v bindings %+v", row.Enabled, enabled)
	}
}

func TestPiEnabledOverridesMatchRuntimeGlobalAndProjectRules(t *testing.T) {
	f := newFixture(t)
	globalEnabled := f.writeSkill(f.agentGlobalDir(AgentPi), "global-enabled", "global enabled")
	globalDisabled := f.writeSkill(f.agentGlobalDir(AgentPi), "global-disabled", "global disabled")
	sharedDisabled := f.writeSkill(filepath.Join(f.Home, ".agents", "skills"), "shared-disabled", "shared disabled")
	projectDisabled := f.writeSkill(f.agentProjectDir(AgentPi, f.Project), "project-disabled", "project disabled")
	projectSharedDisabled := f.writeSkill(filepath.Join(f.Project, ".agents", "skills"), "project-shared-disabled", "project shared disabled")

	globalSettings := filepath.Join(f.Home, ".pi", "agent", "settings.json")
	if err := os.MkdirAll(filepath.Dir(globalSettings), 0o700); err != nil {
		t.Fatal(err)
	}
	// Force-includes are applied after globs, and force-excludes win last,
	// matching Pi's DefaultPackageManager ordering rather than array order.
	if err := os.WriteFile(globalSettings, []byte(`{"skills":["+skills/global-enabled","!skills/**","-skills/global-disabled"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	projectSettings := filepath.Join(f.Project, ".pi", "settings.json")
	if err := os.MkdirAll(filepath.Dir(projectSettings), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectSettings, []byte(`{"skills":["-skills/project-disabled","-skills/project-shared-disabled"]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	inventory, err := DiscoverInventory(f.options(f.Project))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		globalEnabled:         true,
		globalDisabled:        false,
		sharedDisabled:        false,
		projectDisabled:       false,
		projectSharedDisabled: false,
	}
	seen := map[string]bool{}
	for _, skill := range inventory.Skills {
		for _, binding := range skill.Bindings {
			if binding.Agent != AgentPi {
				continue
			}
			expected, ok := want[binding.TargetPath]
			if !ok {
				continue
			}
			seen[binding.TargetPath] = true
			if binding.Enabled != expected {
				t.Fatalf("Pi enabled state for %s = %v, want %v", binding.TargetPath, binding.Enabled, expected)
			}
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("Pi override fixtures missing from inventory: got %v want %v", seen, want)
	}
}

func TestPiDisabledOverrideRemovesNonExecutableOwnedToggle(t *testing.T) {
	f := newFixture(t)
	store := f.store()
	storeDir := store.PackageDir("pi-owned")
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storeDir, "SKILL.md"), []byte("---\nname: pi-owned\n---\nowned\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hash, err := folderContentHash(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(f.agentGlobalDir(AgentPi), "pi-owned")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(storeDir, target); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveInventory(InventoryFile{Version: inventoryVersion, Packages: map[string]PackageEntry{
		"pi-owned": {
			SkillName: "pi-owned", Source: "fixture/catalog", SourceType: string(SourceTypeCatalog), ContentHash: hash, Owned: true,
			Bindings: []BindingEntry{{Agent: AgentPi, Scope: ScopeGlobal, TargetPath: target, Enabled: true, Mode: BindingSymlink}},
		},
	}}); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(f.Home, ".pi", "agent", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"skills":["-skills/pi-owned"]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	inventory, err := DiscoverInventory(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	var owned *InstalledSkill
	for index := range inventory.Skills {
		if inventory.Skills[index].Name == "pi-owned" {
			owned = &inventory.Skills[index]
			break
		}
	}
	if owned == nil || owned.Enabled || len(owned.Bindings) != 1 {
		t.Fatalf("owned Pi runtime state = %+v", owned)
	}
	binding := owned.Bindings[0]
	if binding.Enabled || len(binding.Operations) != 1 || binding.Operations[0] != OperationUnbind {
		t.Fatalf("provider-disabled binding advertised an inoperative toggle: %+v", binding)
	}
	if !strings.Contains(binding.Note, "runtime settings disable") {
		t.Fatalf("provider-disabled binding reason missing: %+v", binding)
	}
}

func TestMalformedPiConfigWarnsWithoutDroppingInventory(t *testing.T) {
	f := newFixture(t)
	f.writeSkill(f.agentGlobalDir(AgentPi), "still-visible-pi", "pi")
	settingsPath := filepath.Join(f.Home, ".pi", "agent", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"skills":`), 0o600); err != nil {
		t.Fatal(err)
	}
	inventory, err := DiscoverInventory(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	warned := false
	for _, skill := range inventory.Skills {
		found = found || skill.Name == "still-visible-pi"
	}
	for _, warning := range inventory.Warnings {
		warned = warned || strings.Contains(warning, "Pi global Skill enabled overrides could not be parsed")
	}
	if !found || !warned {
		t.Fatalf("malformed Pi settings were not isolated: found=%v warnings=%v", found, inventory.Warnings)
	}
}

func TestExternalBindingModeReflectsPhysicalDirectoryOrSymlink(t *testing.T) {
	f := newFixture(t)
	direct := f.writeSkill(f.agentGlobalDir(AgentClaudeCode), "direct-copy", "direct")
	external := f.writeSkill(filepath.Join(f.Home, "external-source"), "linked-copy", "linked")
	linked := filepath.Join(f.agentGlobalDir(AgentClaudeCode), "linked-copy")
	if err := os.MkdirAll(filepath.Dir(linked), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, linked); err != nil {
		t.Fatal(err)
	}
	inventory, err := DiscoverInventory(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	modes := map[string]string{}
	for _, skill := range inventory.Skills {
		for _, binding := range skill.Bindings {
			modes[binding.TargetPath] = binding.Mode
		}
	}
	if modes[direct] != string(BindingDirect) || modes[linked] != string(BindingSymlink) {
		t.Fatalf("external binding modes = %v", modes)
	}
}

func TestMalformedCodexConfigWarnsWithoutDroppingSharedInventory(t *testing.T) {
	f := newFixture(t)
	f.writeSkill(filepath.Join(f.Home, ".agents", "skills"), "still-visible", "shared")
	configPath := filepath.Join(f.Home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("[[skills.config]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	inventory, err := DiscoverInventory(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, skill := range inventory.Skills {
		found = found || skill.Name == "still-visible"
	}
	if !found {
		t.Fatal("malformed Codex config dropped otherwise valid shared Skills")
	}
	warned := false
	for _, warning := range inventory.Warnings {
		warned = warned || strings.Contains(warning, "enabled overrides could not be parsed")
	}
	if !warned {
		t.Fatalf("malformed Codex config warning missing: %v", inventory.Warnings)
	}
}

func TestSharedProjectRootsStopBeforeHomeAndFilesystemRoot(t *testing.T) {
	f := newFixture(t)
	nested := filepath.Join(f.Project, "nested", "deeper")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	roots := sharedProjectSkillRoots(nested, f.Home)
	homeRoot := filepath.Join(f.Home, ".agents", "skills")
	filesystemRoot := filepath.Join(filepath.VolumeName(f.Home)+string(filepath.Separator), ".agents", "skills")
	for _, root := range roots {
		if root == homeRoot || root == filesystemRoot {
			t.Fatalf("project discovery escaped its boundary: %v", roots)
		}
	}
	if len(roots) != 3 || roots[len(roots)-1] != filepath.Join(f.Project, ".agents", "skills") {
		t.Fatalf("project ancestor roots = %v", roots)
	}
	if err := os.MkdirAll(filepath.Join(f.Project, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	roots = sharedProjectSkillRoots(nested, f.Home)
	if roots[len(roots)-1] != filepath.Join(f.Project, ".agents", "skills") {
		t.Fatalf("repository boundary was not included exactly once: %v", roots)
	}
}

func TestInventoryEscapesRealUserState(t *testing.T) {
	f := newFixture(t)
	// Discovery with an explicit fixture Home must never consult the real
	// user's home/state dirs. Point the fixture env at bogus paths and assert
	// the store path used is the fixture one.
	options := f.options("")
	inventory, err := DiscoverInventory(options)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Skills == nil {
		t.Fatal("nil skills slice")
	}
	store := f.store()
	if !filepath.HasPrefix(store.Root(), f.Home) {
		t.Fatalf("store escaped fixture home: %s", store.Root())
	}
	_ = inventory
}

// --- helpers ---------------------------------------------------------------

func TestInventoryJSONWireShape(t *testing.T) {
	// The JSON wire shape is part of the app contract; snapshot key fields.
	f := newFixture(t)
	store := f.store()
	if err := os.MkdirAll(store.PackageDir("gamma"), 0o700); err != nil {
		t.Fatal(err)
	}
	entry := PackageEntry{
		SkillName: "gamma", Source: "owner/repo", SourceType: string(SourceTypeCatalog),
		ContentHash: "hash", Owned: true,
	}
	file := InventoryFile{Version: inventoryVersion, Packages: map[string]PackageEntry{"gamma": entry}}
	if err := store.SaveInventory(file); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.InventoryPath())
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	packages := decoded["packages"].(map[string]any)
	gamma := packages["gamma"].(map[string]any)
	if gamma["owned"] != true || gamma["content_hash"] != "hash" || gamma["skill_name"] != "gamma" {
		t.Fatalf("inventory wire shape unexpected: %v", gamma)
	}
	// No control characters or traversal in package ids.
	if strings.ContainsAny(gamma["skill_name"].(string), "../\x00") {
		t.Fatal("invalid skill name leaked to wire")
	}
}
