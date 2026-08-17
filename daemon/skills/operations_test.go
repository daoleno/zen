package skills

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runMutation builds and executes one lifecycle request end to end, exactly
// like the server does: build a reviewed plan, then execute it.
func runMutation(t *testing.T, f *fixture, request MutationRequest) (MutationCommand, MutationExecution, error) {
	t.Helper()
	ctx := context.Background()
	options := f.options(request.CWD)
	command, err := BuildMutationCommand(options, request)
	if err != nil {
		return MutationCommand{}, MutationExecution{}, err
	}
	execution, err := ExecuteMutationCommand(ctx, command, MutationExecutionOptions{
		CWD:              request.CWD,
		InventoryOptions: options,
	})
	return command, execution, err
}

func mustRunMutation(t *testing.T, f *fixture, request MutationRequest) MutationCommand {
	t.Helper()
	command, execution, err := runMutation(t, f, request)
	if err != nil {
		t.Fatalf("%s failed: %v", request.Operation, err)
	}
	if !execution.Success {
		t.Fatalf("%s reported failure: %+v", request.Operation, execution)
	}
	return command
}

func importRequest(name, source string, scope Scope, agents ...Agent) MutationRequest {
	return MutationRequest{
		Operation: OperationImport, SkillName: name, Source: source, Scope: scope, Agents: agents,
	}
}

func TestLifecycleImportBindDisableUnbindEnable(t *testing.T) {
	f := newFixture(t)
	// Import a local skill into the store, bound globally to Codex (symlink)
	// and Cursor (copy).
	source := f.writeSkill(f.Home, "focus", "focus body")
	request := MutationRequest{
		Operation: OperationImport, SkillName: "focus", InfoPath: source, Scope: ScopeGlobal,
		Agents: []Agent{AgentCodex, AgentCursor},
	}
	command := mustRunMutation(t, f, request)
	if command.Operation != OperationImport || len(command.Agents) != 2 {
		t.Fatalf("plan mismatch: %+v", command)
	}
	codexTarget := filepath.Join(f.agentGlobalDir(AgentCodex), "focus")
	cursorTarget := filepath.Join(f.agentGlobalDir(AgentCursor), "focus")
	if !commandDestructiveContains(command, "symlink", codexTarget) {
		t.Fatalf("plan must declare the codex symlink: %+v", command.Changes)
	}
	if !commandDestructiveContains(command, "copy_file", cursorTarget) {
		t.Fatalf("plan must declare the cursor copy: %+v", command.Changes)
	}
	// Symlink materialization for codex; copy for cursor.
	info, err := os.Lstat(codexTarget)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("codex binding must be a symlink, got %v %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(cursorTarget, "SKILL.md")); err != nil {
		t.Fatalf("cursor copy binding missing: %v", err)
	}
	store := f.store()
	file, err := store.LoadInventory(false)
	if err != nil {
		t.Fatal(err)
	}
	entry := file.Packages["focus"]
	if !entry.Owned || entry.ContentHash == "" || len(entry.Bindings) != 2 {
		t.Fatalf("inventory entry wrong: %+v", entry)
	}
	// Inventory metadata never lives inside the store package dir.
	if _, err := os.Stat(filepath.Join(store.PackageDir("focus"), "inventory.json")); !os.IsNotExist(err) {
		t.Fatal("metadata must not leak into package dirs")
	}

	// Disable the codex binding: materialization is removed, package content
	// and inventory entry remain.
	mustRunMutation(t, f, MutationRequest{
		Operation: OperationDisable, SkillName: "focus", CWD: "", Scope: ScopeGlobal, Agents: []Agent{AgentCodex},
	})
	if _, err := os.Lstat(codexTarget); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disable must remove the symlink materialization: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.PackageDir("focus"), "SKILL.md")); err != nil {
		t.Fatalf("disable must never delete package content: %v", err)
	}
	file, _ = store.LoadInventory(false)
	if file.Packages["focus"].Owned != true || len(file.Packages["focus"].Bindings) != 2 {
		t.Fatal("disable must keep the binding record and ownership")
	}
	codexBinding := file.Packages["focus"].Bindings[0]
	cursorBinding := file.Packages["focus"].Bindings[1]
	if codexBinding.Agent != AgentCodex || codexBinding.Enabled {
		t.Fatalf("disable must flip the requested binding off: %+v", codexBinding)
	}
	if cursorBinding.Agent != AgentCursor || !cursorBinding.Enabled {
		t.Fatalf("disable must leave other bindings on: %+v", cursorBinding)
	}
	// Disabling again is idempotence-rejected (already disabled).
	if _, _, err := runMutation(t, f, MutationRequest{Operation: OperationDisable, SkillName: "focus", Scope: ScopeGlobal, Agents: []Agent{AgentCodex}}); err == nil {
		t.Fatal("double disable must be rejected")
	}

	// Enable restores the symlink.
	mustRunMutation(t, f, MutationRequest{Operation: OperationEnable, SkillName: "focus", Scope: ScopeGlobal, Agents: []Agent{AgentCodex}})
	if _, err := os.Lstat(codexTarget); err != nil {
		t.Fatalf("enable must restore the symlink: %v", err)
	}

	// Unbind removes the record + materialization; content stays.
	mustRunMutation(t, f, MutationRequest{Operation: OperationUnbind, SkillName: "focus", Scope: ScopeGlobal, Agents: []Agent{AgentCodex}})
	if _, err := os.Lstat(codexTarget); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unbind must remove the materialization: %v", err)
	}
	file, _ = store.LoadInventory(false)
	if len(file.Packages["focus"].Bindings) != 1 || file.Packages["focus"].Bindings[0].Agent != AgentCursor {
		t.Fatalf("unbind must remove only the requested binding: %+v", file.Packages["focus"].Bindings)
	}
	// Unbind when no binding exists is rejected.
	if _, _, err := runMutation(t, f, MutationRequest{Operation: OperationUnbind, SkillName: "focus", Scope: ScopeGlobal, Agents: []Agent{AgentCodex}}); err == nil {
		t.Fatal("unbind of a missing binding must be rejected")
	}
}

func commandDestructiveContains(command MutationCommand, kind, path string) bool {
	for _, change := range command.Changes {
		if change.Kind == kind && (change.Path == path || change.Destination == path) {
			return true
		}
	}
	return false
}

func TestUninstallVsForgetAreDistinct(t *testing.T) {
	f := newFixture(t)
	// Owned package: uninstall removes bindings, store, inventory.
	imported := f.writeSkill(f.Home, "uninstall-me", "body")
	mustRunMutation(t, f, MutationRequest{Operation: OperationImport, SkillName: "uninstall-me", InfoPath: imported, Scope: ScopeGlobal, Agents: []Agent{AgentPi}})
	command := mustRunMutation(t, f, MutationRequest{Operation: OperationUninstall, SkillName: "uninstall-me"})
	if !command.Destructive {
		t.Fatal("uninstall must be marked destructive")
	}
	store := f.store()
	if _, err := os.Stat(store.PackageDir("uninstall-me")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("uninstall must remove store content")
	}
	if _, err := os.Stat(filepath.Join(f.agentGlobalDir(AgentPi), "uninstall-me")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("uninstall must remove bindings")
	}
	if _, err := store.LoadInventory(false); err != nil {
		t.Fatal(err)
	}
	file, _ := store.LoadInventory(false)
	if _, ok := file.Packages["uninstall-me"]; ok {
		t.Fatal("uninstall must remove the inventory entry")
	}

	// Tracked external: forget removes only the inventory entry; the external
	// directory on disk is preserved byte-for-byte.
	externalDir := f.writeSkill(f.agentGlobalDir(AgentGrok), "external-thing", "external content")
	mustRunMutation(t, f, MutationRequest{Operation: OperationMigrate})
	// external-thing is now tracked (unowned): uninstall is refused because
	// external rows are forgotten, never uninstalled.
	if _, _, err := runMutation(t, f, MutationRequest{Operation: OperationUninstall, SkillName: "external-thing"}); err == nil || !strings.Contains(err.Error(), "forgotten") {
		t.Fatalf("uninstall of a tracked external must be refused, got %v", err)
	}
	command = mustRunMutation(t, f, MutationRequest{Operation: OperationForget, SkillName: "external-thing"})
	if command.Destructive {
		t.Fatal("forget is not destructive")
	}
	file, _ = store.LoadInventory(false)
	if _, ok := file.Packages["external-thing"]; ok {
		t.Fatal("forget must remove the inventory entry")
	}
	content, err := os.ReadFile(filepath.Join(externalDir, "SKILL.md"))
	if err != nil || !strings.Contains(string(content), "external content") {
		t.Fatal("forget must never delete or edit external files")
	}
	// Forget of an owned package is rejected: the operations never blur.
	f.writeSkill(f.Home, "second", "again")
	mustRunMutation(t, f, MutationRequest{Operation: OperationImport, SkillName: "second", InfoPath: filepath.Join(f.Home, "second"), Scope: ScopeGlobal, Agents: []Agent{AgentCodex}})
	if _, _, err := runMutation(t, f, MutationRequest{Operation: OperationForget, SkillName: "second"}); err == nil {
		t.Fatal("forget on an owned package must be rejected")
	}
}

func TestImportRejectsDuplicatesAndConflicts(t *testing.T) {
	f := newFixture(t)
	source := f.writeSkill(f.Home, "once", "body")
	mustRunMutation(t, f, MutationRequest{Operation: OperationImport, SkillName: "once", InfoPath: source, Scope: ScopeGlobal, Agents: []Agent{AgentCodex}})
	if _, _, err := runMutation(t, f, MutationRequest{Operation: OperationImport, SkillName: "once", InfoPath: source, Scope: ScopeGlobal, Agents: []Agent{AgentCodex}}); err == nil {
		t.Fatal("importing an already-owned name must be rejected")
	}
	// A tracked external of the same name blocks import until forgotten.
	f.writeSkill(f.agentGlobalDir(AgentGrok), "blocked", "external")
	mustRunMutation(t, f, MutationRequest{Operation: OperationMigrate})
	if _, _, err := runMutation(t, f, MutationRequest{Operation: OperationImport, SkillName: "blocked", InfoPath: filepath.Join(f.Home, "blocked"), Scope: ScopeGlobal, Agents: []Agent{AgentCodex}}); err == nil {
		t.Fatal("import over a tracked external must be rejected")
	}
}

func TestImportFromArchiveAndSafety(t *testing.T) {
	f := newFixture(t)
	// Make a safe zip with a wrapper folder.
	zipPath := filepath.Join(f.Home, "safe.zip")
	archive := mustCreateZip(t, zipPath, map[string]string{
		"wrapper/SKILL.md":     "---\nname: from-zip\n---\nzip body\n",
		"wrapper/reference.md": "# ref\n",
	})
	_ = archive
	mustRunMutation(t, f, MutationRequest{Operation: OperationImport, SkillName: "from-zip", InfoPath: zipPath, Scope: ScopeGlobal, Agents: []Agent{AgentGrok}})
	store := f.store()
	if _, err := os.Stat(filepath.Join(store.PackageDir("from-zip"), "reference.md")); err != nil {
		t.Fatalf("archive files must materialize: %v", err)
	}

	// Unsafe archives must be rejected before any content is trusted.
	unsafePath := filepath.Join(f.Home, "unsafe.zip")
	mustCreateZip(t, unsafePath, map[string]string{"../escape/SKILL.md": "---\nname: evil\n---\n"})
	if _, _, err := runMutation(t, f, MutationRequest{Operation: OperationImport, SkillName: "evil", InfoPath: unsafePath, Scope: ScopeGlobal, Agents: []Agent{AgentCodex}}); !errors.Is(err, ErrUnsafeArchiveEntry) {
		t.Fatalf("traversal must be rejected, got %v", err)
	}
	absolutePath := filepath.Join(f.Home, "absolute.zip")
	mustCreateZip(t, absolutePath, map[string]string{"/tmp/evil/SKILL.md": "---\nname: evil2\n---\n"})
	if _, _, err := runMutation(t, f, MutationRequest{Operation: OperationImport, SkillName: "evil2", InfoPath: absolutePath, Scope: ScopeGlobal, Agents: []Agent{AgentCodex}}); !errors.Is(err, ErrUnsafeArchiveEntry) {
		t.Fatalf("absolute member must be rejected, got %v", err)
	}
	if _, _, err := runMutation(t, f, MutationRequest{Operation: OperationImport, SkillName: "noskill", InfoPath: filepath.Join(f.Home, "noskill.zip"), Scope: ScopeGlobal, Agents: []Agent{AgentCodex}}); err == nil {
		t.Fatal("missing archive file must fail")
	}
	dirWithoutSkill := filepath.Join(f.Home, "no-skill-dir")
	if err := os.MkdirAll(dirWithoutSkill, 0o700); err != nil {
		t.Fatal(err)
	}
	noSkillZip := filepath.Join(f.Home, "noskill2.zip")
	mustCreateZip(t, noSkillZip, map[string]string{"readme.txt": "no skill here"})
	if _, _, err := runMutation(t, f, MutationRequest{Operation: OperationImport, SkillName: "noskill2", InfoPath: noSkillZip, Scope: ScopeGlobal, Agents: []Agent{AgentCodex}}); err == nil {
		t.Fatal("archive without SKILL.md must fail")
	}
}

func mustCreateZip(t *testing.T, path string, entries map[string]string) string {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, content := range entries {
		part, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImportCatalogViaFetcherAndProjectScope(t *testing.T) {
	f := newFixture(t)
	// Hermetic catalog fetch: the injected fetcher writes a repo layout.
	options := f.options(f.Project)
	options.Fetcher = func(ctx context.Context, request MutationRequest, stageDir string) error {
		if request.Source != "owner/repo" || request.SkillName != "from-catalog" {
			return errors.New("unexpected catalog request")
		}
		dir := filepath.Join(stageDir, "skills", "from-catalog")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, "SKILL.md"),
			[]byte("---\nname: from-catalog\n---\ncatalog body\n"), 0o600)
	}
	request := MutationRequest{
		Operation: OperationImport, SkillName: "from-catalog", Source: "owner/repo",
		SkillID: "owner/repo/from-catalog", Scope: ScopeGlobal, Agents: []Agent{AgentCodex},
	}
	command, err := BuildMutationCommand(options, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteMutationCommand(context.Background(), command, MutationExecutionOptions{InventoryOptions: options}); err != nil {
		t.Fatal(err)
	}
	// Project scope bind for Claude Code.
	projectRequest := MutationRequest{
		Operation: OperationBind, SkillName: "from-catalog", Scope: ScopeProject,
		CWD: f.Project, Agents: []Agent{AgentClaudeCode},
	}
	command, err = BuildMutationCommand(options, projectRequest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteMutationCommand(context.Background(), command, MutationExecutionOptions{InventoryOptions: options, CWD: f.Project}); err != nil {
		t.Fatal(err)
	}
	projectTarget := filepath.Join(f.Project, ".claude", "skills", "from-catalog")
	if _, err := os.Stat(projectTarget); err != nil {
		t.Fatalf("project binding missing: %v", err)
	}
	// Project bind without a cwd is rejected.
	if _, err := BuildMutationCommand(options, MutationRequest{Operation: OperationBind, SkillName: "from-catalog", Scope: ScopeProject, Agents: []Agent{AgentCodex}}); err == nil {
		t.Fatal("project scope without cwd must be rejected")
	}
}

func TestUpdateAtomicRollbackAndPin(t *testing.T) {
	f := newFixture(t)
	source := f.writeSkill(f.Home, "evolving", "v1")
	mustRunMutation(t, f, MutationRequest{Operation: OperationImport, SkillName: "evolving", InfoPath: source, Scope: ScopeGlobal, Agents: []Agent{AgentCodex, AgentCursor}})
	store := f.store()
	before, _ := store.LoadInventory(false)
	beforeHash := before.Packages["evolving"].ContentHash

	// Local update re-reads the same source dir; mutate it.
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("---\nname: evolving\n---\nv2 content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := mustRunMutation(t, f, MutationRequest{Operation: OperationUpdate, SkillName: "evolving"})
	after, _ := store.LoadInventory(false)
	if after.Packages["evolving"].ContentHash == beforeHash {
		t.Fatal("update must detect changed content")
	}
	if after.Packages["evolving"].PreviousHash != beforeHash {
		t.Fatal("update must retain the previous hash for rollback")
	}
	cursorCopy, err := os.ReadFile(filepath.Join(f.agentGlobalDir(AgentCursor), "evolving", "SKILL.md"))
	if err != nil || !strings.Contains(string(cursorCopy), "v2 content") {
		t.Fatalf("update must advance enabled copy bindings: %v, %q", err, cursorCopy)
	}
	if command.Changes == nil {
		t.Fatal("update plan must describe changes")
	}
	// No-op update when content is unchanged.
	if _, execution, err := runMutation(t, f, MutationRequest{Operation: OperationUpdate, SkillName: "evolving"}); err == nil {
		if !strings.Contains(execution.Output, "already up to date") {
			t.Fatalf("unchanged update should report up-to-date, got %q", execution.Output)
		}
	}

	// Failure during a catalog fetch must leave the previous content intact.
	catalogOptions := f.options("")
	catalogOptions.Fetcher = func(ctx context.Context, request MutationRequest, stageDir string) error {
		dir := filepath.Join(stageDir, "skills", "versioned")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: versioned\n---\ninitial\n"), 0o600)
	}
	catalogCommand, err := BuildMutationCommand(catalogOptions, MutationRequest{
		Operation: OperationImport, SkillName: "versioned", Source: "owner/versioned",
		SkillID: "owner/versioned/versioned", Ref: "deadbeef", Scope: ScopeGlobal, Agents: []Agent{AgentCodex},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteMutationCommand(context.Background(), catalogCommand, MutationExecutionOptions{InventoryOptions: catalogOptions}); err != nil {
		t.Fatal(err)
	}
	catalogOptions.Fetcher = func(ctx context.Context, request MutationRequest, stageDir string) error {
		dir := filepath.Join(stageDir, "skills", "versioned")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: versioned\n---\nupdated\n"), 0o600)
	}
	catalogUpdate, err := BuildMutationCommand(catalogOptions, MutationRequest{Operation: OperationUpdate, SkillName: "versioned"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteMutationCommand(context.Background(), catalogUpdate, MutationExecutionOptions{InventoryOptions: catalogOptions}); err != nil {
		t.Fatalf("pinned catalog/git update failed: %v", err)
	}
	updatedCatalog, err := os.ReadFile(filepath.Join(store.PackageDir("versioned"), "SKILL.md"))
	if err != nil || !strings.Contains(string(updatedCatalog), "updated") {
		t.Fatalf("pinned catalog/git update did not replace content: %v, %q", err, updatedCatalog)
	}
	failing := f.options("")
	failing.Fetcher = func(context.Context, MutationRequest, string) error {
		return errors.New("fetch exploded")
	}
	command, err = BuildMutationCommand(failing, MutationRequest{Operation: OperationUpdate, SkillName: "versioned"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteMutationCommand(context.Background(), command, MutationExecutionOptions{InventoryOptions: failing}); err == nil {
		t.Fatal("fetch failure must surface")
	}
	file, _ := store.LoadInventory(false)
	if file.Packages["versioned"].ContentHash == "" {
		t.Fatal("failed update must keep the entry")
	}
	if _, err := os.Stat(filepath.Join(store.PackageDir("versioned"), "SKILL.md")); err != nil {
		t.Fatal("failed update must keep old content")
	}
	// Rollback: the update plan requires a pinned ref for catalog sources.
	if _, err := BuildMutationCommand(f.options(""), MutationRequest{Operation: OperationUpdate, SkillName: "from-catalog"}); err == nil {
		t.Fatal("update of unknown entry must be rejected at plan time")
	}
}

func TestArchiveUpdateUsesPinnedSource(t *testing.T) {
	f := newFixture(t)
	archivePath := filepath.Join(f.Home, "archive-update.zip")
	mustCreateZip(t, archivePath, map[string]string{
		"skill/SKILL.md": "---\nname: archive-update\n---\nv1\n",
	})
	mustRunMutation(t, f, MutationRequest{
		Operation: OperationImport, SkillName: "archive-update", InfoPath: archivePath,
		Scope: ScopeGlobal, Agents: []Agent{AgentGrok},
	})
	mustCreateZip(t, archivePath, map[string]string{
		"skill/SKILL.md": "---\nname: archive-update\n---\nv2\n",
	})
	mustRunMutation(t, f, MutationRequest{Operation: OperationUpdate, SkillName: "archive-update"})
	content, err := os.ReadFile(filepath.Join(f.store().PackageDir("archive-update"), "SKILL.md"))
	if err != nil || !strings.Contains(string(content), "v2") {
		t.Fatalf("archive update did not use its pinned source: %v, %q", err, content)
	}
	copyContent, err := os.ReadFile(filepath.Join(f.agentGlobalDir(AgentGrok), "archive-update", "SKILL.md"))
	if err != nil || !strings.Contains(string(copyContent), "v2") {
		t.Fatalf("archive update did not advance the Grok copy: %v, %q", err, copyContent)
	}
}

func TestAllAdaptersGlobalAndProjectBindingLifecycle(t *testing.T) {
	f := newFixture(t)
	agents := []Agent{AgentCodex, AgentClaudeCode, AgentCursor, AgentGrok, AgentOpenCode, AgentPi}
	source := f.writeSkill(f.Home, "all-adapters", "adapter body")
	mustRunMutation(t, f, MutationRequest{
		Operation: OperationImport, SkillName: "all-adapters", InfoPath: source,
		Scope: ScopeGlobal, Agents: agents,
	})
	mustRunMutation(t, f, MutationRequest{
		Operation: OperationBind, SkillName: "all-adapters", CWD: f.Project,
		Scope: ScopeProject, Agents: agents,
	})

	assertMaterialized := func(scope Scope) {
		t.Helper()
		for _, agent := range agents {
			adapter := Adapters[agent]
			dir := globalSkillsDir(adapter, f.Home, envResolverFor(f.options(f.Project)))
			if scope == ScopeProject {
				dir = projectSkillsDir(adapter, f.Project)
			}
			target := filepath.Join(dir, "all-adapters")
			info, err := os.Lstat(target)
			if err != nil {
				t.Fatalf("%s %s binding missing: %v", agent, scope, err)
			}
			if adapter.Mode == BindingSymlink && info.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("%s %s binding must be a symlink", agent, scope)
			}
			if adapter.Mode == BindingCopy && info.Mode()&os.ModeSymlink != 0 {
				t.Fatalf("%s %s binding must be a copy", agent, scope)
			}
		}
	}
	assertMaterialized(ScopeGlobal)
	assertMaterialized(ScopeProject)

	for _, operation := range []MutationOperation{OperationDisable, OperationEnable, OperationUnbind} {
		mustRunMutation(t, f, MutationRequest{
			Operation: operation, SkillName: "all-adapters", CWD: f.Project,
			Scope: ScopeProject, Agents: agents,
		})
	}
	file, err := f.store().LoadInventory(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range file.Packages["all-adapters"].Bindings {
		if binding.Scope == ScopeProject {
			t.Fatalf("project binding survived unbind: %+v", binding)
		}
	}
}

func TestAdoptExternalPreservesWithoutTakeover(t *testing.T) {
	f := newFixture(t)
	externalDir := f.writeSkill(f.agentGlobalDir(AgentClaudeCode), "legacy", "legacy body")
	mustRunMutation(t, f, MutationRequest{Operation: OperationMigrate})
	store := f.store()
	file, _ := store.LoadInventory(false)
	if !file.Packages["legacy"].Owned {
		// adopt with discovered agents default
		mustRunMutation(t, f, MutationRequest{Operation: OperationAdopt, SkillName: "legacy", Agents: []Agent{AgentClaudeCode}})
		file, _ = store.LoadInventory(false)
		if !file.Packages["legacy"].Owned {
			t.Fatal("adopt must create an owned entry")
		}
		if len(file.Packages["legacy"].Bindings) != 0 {
			t.Fatal("adopt must require an explicit later bind")
		}
		if _, err := os.Stat(filepath.Join(store.PackageDir("legacy"), "SKILL.md")); err != nil {
			t.Fatalf("adopt must copy content into the store: %v", err)
		}
		content, err := os.ReadFile(filepath.Join(externalDir, "SKILL.md"))
		if err != nil || !strings.Contains(string(content), "legacy body") {
			t.Fatal("adopt must preserve external content on disk")
		}
		if info, err := os.Lstat(externalDir); err != nil || info.Mode()&os.ModeSymlink != 0 {
			t.Fatal("adopt must not replace the external source with a managed binding")
		}
		inventory, err := DiscoverInventory(f.options(""))
		if err != nil {
			t.Fatal(err)
		}
		matches := 0
		for _, skill := range inventory.Skills {
			if skill.Name == "legacy" {
				matches++
			}
		}
		if matches != 1 {
			t.Fatalf("adopted origin must remain provenance, not a duplicate row: %d", matches)
		}
	}
}

func TestCopyAwareAdoptRejectsStaleMismatchAndPostReviewChanges(t *testing.T) {
	f := newFixture(t)
	selectedDir := f.writeSkill(f.agentGlobalDir(AgentClaudeCode), "selected", "selected v1")
	otherDir := f.writeSkill(f.agentGlobalDir(AgentPi), "other", "other")
	decoyDir := f.writeSkill(f.Home, "decoy", "decoy")
	options := f.options("")
	inventory, err := DiscoverInventory(options)
	if err != nil {
		t.Fatal(err)
	}
	copyID := func(name, source string) string {
		t.Helper()
		for _, skill := range inventory.Skills {
			if skill.Name == name && skill.SourcePath == source {
				return skill.ID
			}
		}
		t.Fatalf("copy %q at %q missing from inventory", name, source)
		return ""
	}
	selectedID := copyID("selected", selectedDir)
	otherID := copyID("other", otherDir)
	if _, err := BuildMutationCommand(options, MutationRequest{
		Operation: OperationAdopt, SkillID: otherID, SkillName: "selected", Scope: ScopeGlobal,
	}); err == nil {
		t.Fatal("copy ID from a different Skill name was accepted")
	}
	if _, err := BuildMutationCommand(options, MutationRequest{
		Operation: OperationAdopt, SkillID: strings.Repeat("f", 24), SkillName: "selected", Scope: ScopeGlobal,
	}); err == nil {
		t.Fatal("stale copy ID was accepted")
	}

	command, err := BuildMutationCommand(options, MutationRequest{
		Operation: OperationAdopt, SkillID: selectedID, SkillName: "selected",
		InfoPath: decoyDir, Scope: ScopeGlobal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if command.CopyID != selectedID || command.InfoPath != selectedDir {
		t.Fatalf("request path overrode daemon copy resolution: %+v", command)
	}
	if err := os.WriteFile(
		filepath.Join(selectedDir, "SKILL.md"),
		[]byte("---\nname: selected\ndescription: changed\n---\nselected v2\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteMutationCommand(context.Background(), command, MutationExecutionOptions{InventoryOptions: options}); err == nil || !strings.Contains(err.Error(), "changed after it was reviewed") {
		t.Fatalf("post-review source change was not rejected: %v", err)
	}
	if _, err := os.Stat(f.store().PackageDir("selected")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed adopt left managed content behind: %v", err)
	}

	currentBytes, err := os.ReadFile(filepath.Join(selectedDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	command, err = BuildMutationCommand(options, MutationRequest{
		Operation: OperationAdopt, SkillID: selectedID, SkillName: "selected", Scope: ScopeGlobal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteMutationCommand(context.Background(), command, MutationExecutionOptions{InventoryOptions: options}); err != nil {
		t.Fatal(err)
	}
	afterBytes, err := os.ReadFile(filepath.Join(selectedDir, "SKILL.md"))
	if err != nil || string(afterBytes) != string(currentBytes) {
		t.Fatal("Manage with Zen changed the external source")
	}
	if info, err := os.Lstat(selectedDir); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("Manage with Zen replaced the external source: %v, %v", info, err)
	}
}

func TestCopyAwareAdoptPreservesExternalSymlinkIdentity(t *testing.T) {
	f := newFixture(t)
	realDir := filepath.Join(f.Home, "sources", "physical-directory")
	if err := os.MkdirAll(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "SKILL.md"), []byte("---\nname: linked-skill\n---\nlinked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkedDir := filepath.Join(f.agentGlobalDir(AgentClaudeCode), "linked-skill")
	if err := os.MkdirAll(filepath.Dir(linkedDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, linkedDir); err != nil {
		t.Fatal(err)
	}
	options := f.options("")
	inventory, err := DiscoverInventory(options)
	if err != nil {
		t.Fatal(err)
	}
	var copy *InstalledSkill
	for index := range inventory.Skills {
		if inventory.Skills[index].Name == "linked-skill" {
			copy = &inventory.Skills[index]
			break
		}
	}
	if copy == nil || !operationAllowed(copy.Capability.Operations, OperationAdopt) {
		t.Fatalf("symlink copy did not advertise executable adopt: %+v", copy)
	}
	command, err := BuildMutationCommand(options, MutationRequest{
		Operation: OperationAdopt, SkillID: copy.ID, SkillName: copy.Name, Scope: ScopeGlobal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if command.InfoPath != linkedDir || command.Source != linkedDir {
		t.Fatalf("reviewed command lost the daemon-discovered symlink identity: %+v", command)
	}
	if _, err := ExecuteMutationCommand(context.Background(), command, MutationExecutionOptions{InventoryOptions: options}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(linkedDir); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("Manage with Zen replaced the external symlink: %v, %v", info, err)
	}
	after, err := DiscoverInventory(options)
	if err != nil {
		t.Fatal(err)
	}
	matches := 0
	for _, skill := range after.Skills {
		if skill.Name == "linked-skill" {
			matches++
		}
	}
	if matches != 1 {
		t.Fatalf("adopted symlink origin became a duplicate row: %d", matches)
	}
}

func TestBindingPlanValidation(t *testing.T) {
	f := newFixture(t)
	_ = f.writeSkill(f.Home, "val", "body")
	mustRunMutation(t, f, MutationRequest{Operation: OperationImport, SkillName: "val", InfoPath: filepath.Join(f.Home, "val"), Scope: ScopeGlobal, Agents: []Agent{AgentCodex}})
	// Reject unknown agents everywhere.
	if _, err := BuildMutationCommand(f.options(""), MutationRequest{Operation: OperationBind, SkillName: "val", Scope: ScopeGlobal, Agents: []Agent{"bogus"}}); err == nil {
		t.Fatal("unknown agent must be rejected")
	}
	// Reject invalid skill names.
	if _, err := BuildMutationCommand(f.options(""), MutationRequest{Operation: OperationBind, SkillName: "../evil", Scope: ScopeGlobal, Agents: []Agent{AgentCodex}}); err == nil {
		t.Fatal("traversal name must be rejected")
	}
	// Import without agents is meaningless.
	_, _, err := runMutation(t, f, MutationRequest{Operation: OperationImport, SkillName: "noagents", InfoPath: filepath.Join(f.Home, "noagents"), Scope: ScopeGlobal})
	if err == nil {
		t.Fatal("agents are required for import")
	}
}

func TestUpdateRejectsUnpinnedCatalogProvenance(t *testing.T) {
	f := newFixture(t)
	source := f.writeSkill(f.Home, "unpinned", "body")
	mustRunMutation(t, f, MutationRequest{Operation: OperationImport, SkillName: "unpinned", InfoPath: source, Scope: ScopeGlobal, Agents: []Agent{AgentCodex}})
	store := f.store()
	file, err := store.LoadInventory(false)
	if err != nil {
		t.Fatal(err)
	}
	entry := file.Packages["unpinned"]
	entry.Source = "owner/repo"
	entry.SourceType = string(SourceTypeCatalog)
	entry.Ref = ""
	file.Packages["unpinned"] = entry
	if err := store.SaveInventory(file); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildMutationCommand(f.options(""), MutationRequest{Operation: OperationUpdate, SkillName: "unpinned"}); err == nil {
		t.Fatal("unpinned catalog provenance must not reach update execution")
	}
}
