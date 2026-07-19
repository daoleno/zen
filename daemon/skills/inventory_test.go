package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiscoverInventoryDeduplicatesRealPathsAndAggregatesSupportedAgents(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "project")
	shared := filepath.Join(project, ".agents", "skills", "shared-skill")
	writeTestSkill(t, shared, "shared-skill", "Shared project guidance")
	claudeLink := filepath.Join(project, ".claude", "skills", "shared-skill")
	if err := os.MkdirAll(filepath.Dir(claudeLink), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, claudeLink); err != nil {
		t.Fatal(err)
	}
	writeTestLock(t, filepath.Join(project, "skills-lock.json"), 1, map[string]lockEntry{
		"shared-skill": {
			Source:       "acme/skills",
			SourceType:   "github",
			SourceURL:    "https://github.com/acme/skills",
			SkillPath:    "skills/shared-skill/SKILL.md",
			ComputedHash: "abc",
		},
	})

	inventory, err := DiscoverInventory(InventoryOptions{
		CWD:        project,
		Home:       home,
		CodexHome:  filepath.Join(home, ".codex"),
		ClaudeHome: filepath.Join(home, ".claude"),
		Now:        func() time.Time { return time.Unix(42, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Skills) != 1 {
		t.Fatalf("skills = %#v, want one real-path row", inventory.Skills)
	}
	got := inventory.Skills[0]
	if got.CanonicalPath != shared {
		t.Fatalf("canonical path = %q, want %q", got.CanonicalPath, shared)
	}
	wantAgents := []Agent{AgentCodex, AgentClaudeCode, AgentCursor}
	if !sameAgentSet(got.Agents, wantAgents) {
		t.Fatalf("agents = %v, want %v", got.Agents, wantAgents)
	}
	if got.Manager != ManagerSkillsCLI || !got.Capability.CanRemove {
		t.Fatalf("management = %#v/%#v, want exact CLI removal", got.Manager, got.Capability)
	}
	if len(got.Bindings) != 2 {
		t.Fatalf("bindings = %#v, want both supported paths", got.Bindings)
	}
	if got.Source != "acme/skills" || got.Scope != ScopeProject {
		t.Fatalf("source/scope = %q/%q", got.Source, got.Scope)
	}
	if inventory.GeneratedAt.Unix() != 42 {
		t.Fatalf("generated_at = %s", inventory.GeneratedAt)
	}
	if len(inventory.Agents) != 4 || inventory.Agents[3].Agent != AgentGrok || inventory.Agents[3].Supported {
		t.Fatalf("agent support = %#v", inventory.Agents)
	}
}

func TestDiscoverInventoryKeepsUnknownBuiltinAndPluginRowsUnmanaged(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestSkill(t, filepath.Join(home, ".agents", "skills", "unknown-skill"), "unknown-skill", "Unknown owner")
	writeTestSkill(t, filepath.Join(home, ".codex", "skills", ".system", "builtin-skill"), "builtin-skill", "Builtin")
	writeTestSkill(t, filepath.Join(home, ".codex", "plugins", "cache", "vendor", "sample-plugin", "1.0.0", "skills", "plugin-skill"), "plugin-skill", "Plugin")

	inventory, err := DiscoverInventory(InventoryOptions{
		CWD:        project,
		Home:       home,
		CodexHome:  filepath.Join(home, ".codex"),
		ClaudeHome: filepath.Join(home, ".claude"),
	})
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]InstalledSkill)
	for _, skill := range inventory.Skills {
		byName[skill.Name] = skill
	}
	for name, want := range map[string]struct {
		scope   Scope
		manager Manager
	}{
		"unknown-skill": {ScopeGlobal, ManagerUnknown},
		"builtin-skill": {ScopeBuiltin, ManagerBuiltin},
		"plugin-skill":  {ScopePlugin, ManagerPlugin},
	} {
		got, ok := byName[name]
		if !ok {
			t.Fatalf("missing %s from %#v", name, inventory.Skills)
		}
		if got.Scope != want.scope || got.Manager != want.manager || got.Capability.CanRemove {
			t.Fatalf("%s = %#v, want unmanaged %s/%s", name, got, want.scope, want.manager)
		}
	}
	if got := byName["plugin-skill"].Plugin; got != "sample-plugin" {
		t.Fatalf("plugin = %q, want sample-plugin", got)
	}
}

func TestDiscoverInventoryRejectsMalformedLockProvenance(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "project")
	writeTestSkill(t, filepath.Join(project, ".agents", "skills", "safe-skill"), "safe-skill", "Visible")
	writeTestLock(t, filepath.Join(project, "skills-lock.json"), 1, map[string]lockEntry{
		"safe-skill": {
			Source:     "acme/skills;touch-pwned",
			SourceType: "github",
			SkillPath:  "../outside/SKILL.md",
		},
	})

	inventory, err := DiscoverInventory(InventoryOptions{CWD: project, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Skills) != 1 || inventory.Skills[0].Manager != ManagerUnknown || inventory.Skills[0].Capability.CanRemove {
		t.Fatalf("malformed lock granted management: %#v", inventory.Skills)
	}
}

func TestDiscoverInventoryRequiresLinkedGlobalAgentsAndCurrentLockProvenance(t *testing.T) {
	home := t.TempDir()
	canonical := filepath.Join(home, ".agents", "skills", "global-skill")
	writeTestSkill(t, canonical, "global-skill", "Global")
	writeTestLock(t, filepath.Join(home, ".agents", ".skill-lock.json"), 2, map[string]lockEntry{
		"global-skill": {
			Source:       "acme/skills",
			SourceType:   "github",
			SourceURL:    "https://github.com/acme/skills",
			SkillPath:    "skills/global-skill/SKILL.md",
			ComputedHash: "abc",
		},
	})

	inventory, err := DiscoverInventory(InventoryOptions{Home: home})
	if err != nil || len(inventory.Skills) != 1 {
		t.Fatalf("inventory = %#v, error = %v", inventory, err)
	}
	if inventory.Skills[0].Manager != ManagerUnknown || len(inventory.Skills[0].Agents) != 0 {
		t.Fatalf("old canonical lock granted ownership or targets: %#v", inventory.Skills[0])
	}

	writeTestLock(t, filepath.Join(home, ".agents", ".skill-lock.json"), 3, map[string]lockEntry{
		"global-skill": {
			Source:          "acme/skills",
			SourceType:      "github",
			SourceURL:       "https://github.com/acme/skills",
			SkillPath:       "skills/global-skill/SKILL.md",
			SkillFolderHash: "abc",
		},
	})
	codexLink := filepath.Join(home, ".codex", "skills", "global-skill")
	if err := os.MkdirAll(filepath.Dir(codexLink), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(canonical, codexLink); err != nil {
		t.Fatal(err)
	}

	inventory, err = DiscoverInventory(InventoryOptions{Home: home})
	if err != nil || len(inventory.Skills) != 1 {
		t.Fatalf("linked inventory = %#v, error = %v", inventory, err)
	}
	got := inventory.Skills[0]
	if got.Manager != ManagerSkillsCLI || !sameAgentSet(got.Agents, []Agent{AgentCodex}) || !got.Capability.CanRemove {
		t.Fatalf("linked global ownership = %#v", got)
	}
}

func TestDiscoverInventoryReadsFrontmatterWithoutReadingOrBoundingSkillBody(t *testing.T) {
	home := t.TempDir()
	directory := filepath.Join(home, ".agents", "skills", "metadata-only")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	metadata := "---\nname: metadata-only\ndescription: Safe summary\n---\n" + strings.Repeat("private body\n", 100_000)
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(metadata), 0o644); err != nil {
		t.Fatal(err)
	}

	inventory, err := DiscoverInventory(InventoryOptions{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Skills) != 1 || inventory.Skills[0].Name != "metadata-only" || inventory.Skills[0].Description != "Safe summary" {
		t.Fatalf("metadata-only inventory = %#v", inventory.Skills)
	}
}

func TestDiscoverInventoryHonorsItsUniqueResultLimit(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"alpha", "bravo", "charlie"} {
		writeTestSkill(t, filepath.Join(home, ".codex", "skills", name), name, "Bounded")
	}

	inventory, err := DiscoverInventory(InventoryOptions{Home: home, MaxSkills: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Skills) != 2 {
		t.Fatalf("skills = %d, want bounded result of 2", len(inventory.Skills))
	}
	if !inventory.incomplete || !warningsContain(inventory.Warnings, "bounded result limit") {
		t.Fatalf("warnings = %#v", inventory.Warnings)
	}
}

func TestDiscoverInventoryRemovalAuthorityCannotBeBorrowed(t *testing.T) {
	t.Run("frontmatter alias", func(t *testing.T) {
		home := t.TempDir()
		project := filepath.Join(home, "project")
		writeTestSkill(t, filepath.Join(project, ".agents", "skills", "decoy"), "victim", "Decoy")
		writeTestLock(t, filepath.Join(project, "skills-lock.json"), 1, map[string]lockEntry{
			"victim": {Source: "acme/skills", SourceType: "github"},
		})
		inventory, err := DiscoverInventory(InventoryOptions{CWD: project, Home: home})
		if err != nil || len(inventory.Skills) != 1 {
			t.Fatalf("inventory = %#v, error = %v", inventory, err)
		}
		got := inventory.Skills[0]
		if got.Name != "decoy" || got.Manager != ManagerUnknown || got.Capability.CanRemove {
			t.Fatalf("frontmatter borrowed lock authority: %#v", got)
		}
	})

	t.Run("stale lock key", func(t *testing.T) {
		home := t.TempDir()
		project := filepath.Join(home, "project")
		writeTestSkill(t, filepath.Join(project, ".agents", "skills", "current"), "current", "Current")
		writeTestLock(t, filepath.Join(project, "skills-lock.json"), 1, map[string]lockEntry{
			"previous": {Source: "acme/skills", SourceType: "github"},
		})
		inventory, err := DiscoverInventory(InventoryOptions{CWD: project, Home: home})
		if err != nil || inventory.Skills[0].Manager != ManagerUnknown || inventory.Skills[0].Capability.CanRemove {
			t.Fatalf("stale lock granted removal: %#v, error = %v", inventory.Skills, err)
		}
	})

	t.Run("mismatched lock source path", func(t *testing.T) {
		home := t.TempDir()
		project := filepath.Join(home, "project")
		writeTestSkill(t, filepath.Join(project, ".agents", "skills", "current"), "current", "Current")
		writeTestLock(t, filepath.Join(project, "skills-lock.json"), 1, map[string]lockEntry{
			"current": {Source: "acme/skills", SourceType: "github", SkillPath: "skills/other/SKILL.md"},
		})
		inventory, err := DiscoverInventory(InventoryOptions{CWD: project, Home: home})
		if err != nil || inventory.Skills[0].Manager != ManagerUnknown || inventory.Skills[0].Capability.CanRemove {
			t.Fatalf("mismatched source granted removal: %#v, error = %v", inventory.Skills, err)
		}
	})

	t.Run("duplicate installed identity", func(t *testing.T) {
		home := t.TempDir()
		project := filepath.Join(home, "project")
		writeTestSkill(t, filepath.Join(project, ".agents", "skills", "duplicate"), "duplicate", "One")
		writeTestSkill(t, filepath.Join(project, ".claude", "skills", "duplicate"), "duplicate", "Two")
		writeTestLock(t, filepath.Join(project, "skills-lock.json"), 1, map[string]lockEntry{
			"duplicate": {Source: "acme/skills", SourceType: "github"},
		})
		inventory, err := DiscoverInventory(InventoryOptions{CWD: project, Home: home})
		if err != nil || len(inventory.Skills) != 2 {
			t.Fatalf("inventory = %#v, error = %v", inventory, err)
		}
		for _, got := range inventory.Skills {
			if got.Manager != ManagerUnknown || got.Capability.CanRemove || !strings.Contains(got.Capability.Reason, "share this Skill identity") {
				t.Fatalf("duplicate identity remained removable: %#v", got)
			}
		}
	})
}

func TestDiscoverInventoryRepresentsMixedScopeBindingsOnceAndDisablesRemoval(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "project")
	shared := filepath.Join(project, ".agents", "skills", "shared")
	writeTestSkill(t, shared, "shared", "Shared")
	globalLink := filepath.Join(home, ".codex", "skills", "shared")
	if err := os.MkdirAll(filepath.Dir(globalLink), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, globalLink); err != nil {
		t.Fatal(err)
	}
	writeTestLock(t, filepath.Join(project, "skills-lock.json"), 1, map[string]lockEntry{
		"shared": {Source: "acme/skills", SourceType: "github"},
	})
	writeTestLock(t, filepath.Join(home, ".agents", ".skill-lock.json"), 3, map[string]lockEntry{
		"shared": {Source: "acme/skills", SourceType: "github"},
	})
	inventory, err := DiscoverInventory(InventoryOptions{CWD: project, Home: home})
	if err != nil || len(inventory.Skills) != 1 {
		t.Fatalf("inventory = %#v, error = %v", inventory, err)
	}
	got := inventory.Skills[0]
	if got.Scope != ScopeMixed || got.Capability.CanRemove || len(got.Bindings) != 2 {
		t.Fatalf("mixed binding = %#v", got)
	}
}

func TestDiscoverInventoryBoundsAllDirectoryEntryWorkAndHonorsCancellation(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".agents", "skills")
	for index := 0; index < 40; index++ {
		if err := os.MkdirAll(filepath.Join(root, fmt.Sprintf("unrelated-%03d", index)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	inventory, err := DiscoverInventory(InventoryOptions{Home: home, MaxWork: 5})
	if err != nil || len(inventory.Skills) != 0 || !inventory.incomplete || !warningsContain(inventory.Warnings, "work limit") {
		t.Fatalf("bounded inventory = %#v, error = %v", inventory, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DiscoverInventory(InventoryOptions{Context: ctx, Home: home}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled inventory error = %v", err)
	}
}

func TestDiscoverInventoryCountsDuplicateCanonicalBindingsAgainstWorkBudget(t *testing.T) {
	home := t.TempDir()
	canonical := filepath.Join(home, ".agents", "skills", "shared")
	writeTestSkill(t, canonical, "shared", "Shared")
	for _, path := range []string{
		filepath.Join(home, ".codex", "skills", "shared"),
		filepath.Join(home, ".claude", "skills", "shared"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(canonical, path); err != nil {
			t.Fatal(err)
		}
	}
	inventory, err := DiscoverInventory(InventoryOptions{Home: home, MaxWork: 6})
	if err != nil || len(inventory.Skills) != 1 || len(inventory.Warnings) == 0 {
		t.Fatalf("duplicate-path work budget = %#v, error = %v", inventory, err)
	}
}

func TestDiscoverInventorySurfacesBoundedSanitizedReadWarnings(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".agents", "skills", "broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".agents", "skills", "broken", "SKILL.md"), []byte("---\nname: [\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".agents", ".skill-lock.json"), []byte("not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".cursor", "skills"), []byte("not-a-directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	inventory, err := DiscoverInventory(InventoryOptions{Home: home})
	if err != nil || len(inventory.Warnings) < 3 || len(inventory.Warnings) > maxInventoryWarnings {
		t.Fatalf("warnings = %#v, error = %v", inventory.Warnings, err)
	}
	for _, warning := range inventory.Warnings {
		if strings.Contains(warning, home) {
			t.Fatalf("warning exposed private root: %q", warning)
		}
	}
}

func TestIncompleteInventoryDisablesEarlierRemovalAuthorityAndCommandConstruction(t *testing.T) {
	t.Run("cancellation preserves visible rows without authority", func(t *testing.T) {
		home := t.TempDir()
		project := filepath.Join(home, "project")
		for _, name := range []string{"alpha", "later"} {
			writeTestSkill(t, filepath.Join(project, ".agents", "skills", name), name, "Managed")
		}
		writeTestLock(t, filepath.Join(project, "skills-lock.json"), 1, map[string]lockEntry{
			"alpha": {Source: "acme/skills", SourceType: "github"},
			"later": {Source: "acme/skills", SourceType: "github"},
		})
		cancelContext := &cancelAfterErrChecksContext{Context: context.Background(), cancelAfter: 8}
		inventory, err := DiscoverInventory(InventoryOptions{Context: cancelContext, CWD: project, Home: home})
		if !errors.Is(err, context.Canceled) || len(inventory.Skills) != 1 {
			t.Fatalf("canceled inventory = %#v, error = %v", inventory, err)
		}
		assertIncompleteUnmanagedRow(t, inventory, inventory.Skills[0].Name)
		if !warningsContain(inventory.Warnings, "canceled") {
			t.Fatalf("canceled warnings = %#v", inventory.Warnings)
		}
	})

	t.Run("result limit hides later duplicate", func(t *testing.T) {
		home := t.TempDir()
		project := filepath.Join(home, "project")
		first := filepath.Join(project, ".agents", "skills", "shared")
		writeTestSkill(t, first, "shared", "First")
		writeTestSkill(t, filepath.Join(project, ".claude", "skills", "shared"), "shared", "Later duplicate")
		writeTestLock(t, filepath.Join(project, "skills-lock.json"), 1, map[string]lockEntry{
			"shared": {Source: "acme/skills", SourceType: "github"},
		})
		options := InventoryOptions{CWD: project, Home: home, MaxSkills: 1}
		inventory, err := DiscoverInventory(options)
		if err != nil || len(inventory.Skills) != 1 {
			t.Fatalf("inventory = %#v, error = %v", inventory, err)
		}
		assertIncompleteUnmanagedRow(t, inventory, "shared")
		_, err = BuildMutationCommand(options, MutationRequest{
			Operation: OperationRemove,
			CWD:       project,
			SkillID:   installedSkillID(first),
			Scope:     ScopeProject,
			Agents:    []Agent{AgentCodex, AgentCursor},
		})
		if err == nil || !strings.Contains(err.Error(), "inventory is incomplete") {
			t.Fatalf("incomplete result-limit removal error = %v", err)
		}
	})

	t.Run("work limit hides later mixed-scope binding", func(t *testing.T) {
		home := t.TempDir()
		project := filepath.Join(home, "project")
		first := filepath.Join(project, ".agents", "skills", "shared")
		writeTestSkill(t, first, "shared", "Project")
		globalLink := filepath.Join(home, ".codex", "skills", "shared")
		if err := os.MkdirAll(filepath.Dir(globalLink), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(first, globalLink); err != nil {
			t.Fatal(err)
		}
		writeTestLock(t, filepath.Join(project, "skills-lock.json"), 1, map[string]lockEntry{
			"shared": {Source: "acme/skills", SourceType: "github"},
		})
		writeTestLock(t, filepath.Join(home, ".agents", ".skill-lock.json"), 3, map[string]lockEntry{
			"shared": {Source: "acme/skills", SourceType: "github"},
		})
		options := InventoryOptions{CWD: project, Home: home, MaxWork: 8}
		inventory, err := DiscoverInventory(options)
		if err != nil || len(inventory.Skills) != 1 {
			t.Fatalf("inventory = %#v, error = %v", inventory, err)
		}
		assertIncompleteUnmanagedRow(t, inventory, "shared")
		_, err = BuildMutationCommand(options, MutationRequest{
			Operation: OperationRemove,
			CWD:       project,
			SkillID:   installedSkillID(first),
			Scope:     ScopeProject,
			Agents:    []Agent{AgentCodex, AgentCursor},
		})
		if err == nil || !strings.Contains(err.Error(), "inventory is incomplete") {
			t.Fatalf("incomplete work-limit removal error = %v", err)
		}
	})
}

func TestPluginTraversalStopsOnCancellationAndSharedWorkExhaustion(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "cache")
	writeTestSkill(t, filepath.Join(cache, "vendor", "plugin", "1.0.0", "skills", "plugin-skill"), "plugin-skill", "Plugin")

	complete := newPluginTestCollector(context.Background(), 100)
	complete.scanPluginCache(cache, AgentCodex, "Codex plugin")
	if complete.incomplete || complete.count != 1 {
		t.Fatalf("complete plugin traversal = count %d, work %d, warnings %#v", complete.count, complete.work, complete.warnings)
	}

	cancelContext := &cancelAfterErrChecksContext{Context: context.Background(), cancelAfter: 12}
	canceled := newPluginTestCollector(cancelContext, 100)
	canceled.scanPluginCache(cache, AgentCodex, "Codex plugin")
	if !canceled.incomplete || !canceled.stopped || canceled.work < 2 || canceled.work >= complete.work || canceled.count != 0 {
		t.Fatalf("canceled plugin traversal = count %d, work %d/%d, warnings %#v", canceled.count, canceled.work, complete.work, canceled.warnings)
	}
	if !errors.Is(cancelContext.Err(), context.Canceled) || !warningsContain(canceled.warnings, "canceled") {
		t.Fatalf("cancellation state = %v, warnings %#v", cancelContext.Err(), canceled.warnings)
	}

	workLimited := newPluginTestCollector(context.Background(), 8)
	workLimited.scanPluginCache(cache, AgentCodex, "Codex plugin")
	if !workLimited.incomplete || !workLimited.stopped || workLimited.work != 8 || workLimited.count != 0 || !warningsContain(workLimited.warnings, "total-work limit") {
		t.Fatalf("work-limited plugin traversal = count %d, work %d, warnings %#v", workLimited.count, workLimited.work, workLimited.warnings)
	}
}

func assertIncompleteUnmanagedRow(t *testing.T, inventory Inventory, name string) {
	t.Helper()
	if !inventory.incomplete || !warningsContain(inventory.Warnings, "incomplete") {
		t.Fatalf("inventory did not report incompleteness: %#v", inventory)
	}
	got := inventory.Skills[0]
	if got.Name != name || got.Manager != ManagerUnknown || got.Capability.CanRemove || got.Source != "" || got.SourceType != "" || got.Provenance == "official skills-cli lock" {
		t.Fatalf("incomplete row retained mutation authority: %#v", got)
	}
}

func newPluginTestCollector(ctx context.Context, maxWork int) *inventoryCollector {
	return &inventoryCollector{
		options: InventoryOptions{Context: ctx, MaxSkills: defaultMaxInstalledSkills, MaxWork: maxWork},
		byReal:  make(map[string]*InstalledSkill),
		blocked: make(map[string]string),
		warned:  make(map[string]struct{}),
	}
}

type cancelAfterErrChecksContext struct {
	context.Context
	checks      int
	cancelAfter int
}

func (ctx *cancelAfterErrChecksContext) Err() error {
	ctx.checks++
	if ctx.checks >= ctx.cancelAfter {
		return context.Canceled
	}
	return nil
}

func warningsContain(warnings []string, fragment string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, fragment) {
			return true
		}
	}
	return false
}

func writeTestSkill(t *testing.T, directory, name, description string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	metadata := "---\nname: " + name + "\ndescription: " + description + "\n---\n"
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(metadata), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeTestLock(t *testing.T, path string, version int, entries map[string]lockEntry) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(lockFile{Version: version, Skills: entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
