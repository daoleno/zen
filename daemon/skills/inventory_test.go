package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func findCopy(t *testing.T, inventory Inventory, name, root string) InstalledSkill {
	t.Helper()
	for _, copy := range inventory.Skills {
		if copy.Name == name && (root == "" || copy.RootPath == root) {
			return copy
		}
	}
	t.Fatalf("copy %q at %q not found: %+v", name, root, inventory.Skills)
	return InstalledSkill{}
}

func TestEverySupportedAgentGlobalRootIsDiscoveredAndDeletable(t *testing.T) {
	f := newFixture(t)
	for _, agent := range supportedAgents {
		name := strings.ReplaceAll(string(agent), "-", "_") + "_global"
		root := f.agentGlobalDir(agent)
		path := f.writeSkill(root, name, "global")
		inventory, err := DiscoverInventory(f.options(""))
		if err != nil {
			t.Fatal(err)
		}
		copy := findCopy(t, inventory, name, path)
		if !copy.Capability.CanDelete || !slices.Contains(copy.Agents, agent) || copy.AllowedRoot != root || copy.Scope != ScopeGlobal {
			t.Fatalf("%s global root contract = %+v", agent, copy)
		}
	}
}

func TestEverySupportedAgentProjectRootIsDiscovered(t *testing.T) {
	f := newFixture(t)
	for _, agent := range supportedAgents {
		name := strings.ReplaceAll(string(agent), "-", "_") + "_project"
		root := f.agentProjectDir(agent, f.Project)
		path := f.writeSkill(root, name, "project")
		inventory, err := DiscoverInventory(f.options(f.Project))
		if err != nil {
			t.Fatal(err)
		}
		copy := findCopy(t, inventory, name, path)
		if !copy.Capability.CanDelete || !slices.Contains(copy.Agents, agent) || copy.Scope != ScopeProject {
			t.Fatalf("%s project root contract = %+v", agent, copy)
		}
	}
}

func TestSharedRootIsOneCopyAvailableToCodexAndPi(t *testing.T) {
	f := newFixture(t)
	root := filepath.Join(f.Home, ".agents", "skills")
	path := f.writeSkill(root, "shared", "shared")
	inventory, err := DiscoverInventory(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	copy := findCopy(t, inventory, "shared", path)
	if len(copy.Agents) != 2 || copy.Agents[0] != AgentCodex || copy.Agents[1] != AgentPi {
		t.Fatalf("shared Agents = %v", copy.Agents)
	}
	count := 0
	for _, current := range inventory.Skills {
		if current.Name == "shared" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("shared physical entry emitted %d copies", count)
	}
}

func TestDuplicateNamesRemainSeparatePhysicalCopies(t *testing.T) {
	f := newFixture(t)
	codex := f.writeSkill(f.agentGlobalDir(AgentCodex), "same", "codex")
	pi := f.writeSkill(f.agentGlobalDir(AgentPi), "same", "pi")
	inventory, err := DiscoverInventory(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	a := findCopy(t, inventory, "same", codex)
	b := findCopy(t, inventory, "same", pi)
	if a.ID == b.ID || a.AllowedRoot == b.AllowedRoot {
		t.Fatalf("copies did not retain exact identity: %+v / %+v", a, b)
	}
}

func TestBuiltinAndPluginSkillsAreReadableButNotDeletable(t *testing.T) {
	f := newFixture(t)
	builtin := f.writeSkill(filepath.Join(f.Home, ".codex", "skills", ".system"), "builtin", "builtin")
	plugin := f.writeSkill(filepath.Join(f.Home, ".codex", "plugins", "cache", "market", "demo", "1", "skills"), "hosted", "plugin")
	inventory, err := DiscoverInventory(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []struct{ name, path, provider string }{
		{"builtin", builtin, "Codex"}, {"hosted", plugin, "plugin"},
	} {
		copy := findCopy(t, inventory, expected.name, expected.path)
		if copy.Capability.CanDelete || !strings.Contains(copy.Capability.Reason, expected.provider) || strings.TrimSpace(copy.Location) == "" {
			t.Fatalf("readonly capability = %+v", copy)
		}
	}
}

func TestLegacyManagedStoreIsAnOrdinaryDiscoverableRoot(t *testing.T) {
	f := newFixture(t)
	root := filepath.Join(f.StateDir, "skills", "store")
	path := f.writeSkill(root, "legacy", "legacy")
	inventory, err := DiscoverInventory(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	copy := findCopy(t, inventory, "legacy", path)
	if !copy.Capability.CanDelete || copy.AllowedRoot != root || copy.Location != "Local Skills storage" || len(copy.Agents) != 0 {
		t.Fatalf("legacy store copy = %+v", copy)
	}
}

func TestDirectoryMetadataMismatchDisablesDelete(t *testing.T) {
	f := newFixture(t)
	root := f.agentGlobalDir(AgentClaudeCode)
	path := filepath.Join(root, "directory-name")
	writeTestSkill(t, path, "different-name", "mismatch")
	inventory, err := DiscoverInventory(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	copy := findCopy(t, inventory, "directory-name", path)
	if copy.Capability.CanDelete || !strings.Contains(copy.Capability.Reason, "metadata") {
		t.Fatalf("mismatch capability = %+v", copy.Capability)
	}
}

func TestSymlinkEntriesKeepDistinctEntryIdentity(t *testing.T) {
	f := newFixture(t)
	target := f.writeSkill(filepath.Join(f.Home, "source"), "linked", "target")
	for _, root := range []string{f.agentGlobalDir(AgentCodex), f.agentGlobalDir(AgentPi)} {
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(root, "linked")); err != nil {
			t.Fatal(err)
		}
	}
	inventory, err := DiscoverInventory(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	a := findCopy(t, inventory, "linked", filepath.Join(f.agentGlobalDir(AgentCodex), "linked"))
	b := findCopy(t, inventory, "linked", filepath.Join(f.agentGlobalDir(AgentPi), "linked"))
	if a.ID == b.ID || a.CanonicalPath != target || b.CanonicalPath != target {
		t.Fatalf("symlink identities = %+v / %+v", a, b)
	}
}

func TestIncompleteInventoryDisablesAllDeleteAuthority(t *testing.T) {
	f := newFixture(t)
	f.writeSkill(f.agentGlobalDir(AgentCodex), "bounded", "bounded")
	options := f.options("")
	options.MaxWork = 1
	inventory, err := DiscoverInventory(options)
	if err != nil {
		t.Fatal(err)
	}
	for _, copy := range inventory.Skills {
		if copy.Capability.CanDelete {
			t.Fatalf("incomplete inventory retained delete: %+v", copy)
		}
	}
}

func TestCanceledInventoryReturnsContextError(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	options := f.options("")
	options.Context = ctx
	if _, err := DiscoverInventory(options); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled discovery error = %v", err)
	}
}

func TestInventoryAdvertisesDeleteOnly(t *testing.T) {
	f := newFixture(t)
	inventory, err := DiscoverInventory(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(inventory.MutationOperations, []MutationOperation{OperationDelete}) {
		t.Fatalf("mutation operations = %v", inventory.MutationOperations)
	}
}
