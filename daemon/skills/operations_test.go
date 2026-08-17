package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func deleteRequest(copy InstalledSkill) MutationRequest {
	return MutationRequest{
		Operation: OperationDelete, CopyID: copy.ID, SkillName: copy.Name,
		RootPath: copy.RootPath, CanonicalPath: copy.CanonicalPath,
		AllowedRoot: copy.AllowedRoot,
	}
}

func buildDelete(t *testing.T, options InventoryOptions, copy InstalledSkill) MutationCommand {
	t.Helper()
	command, err := BuildMutationCommand(options, deleteRequest(copy))
	if err != nil {
		t.Fatal(err)
	}
	return command
}

func executeDelete(t *testing.T, options InventoryOptions, command MutationCommand) {
	t.Helper()
	execution, err := ExecuteMutationCommand(context.Background(), command, MutationExecutionOptions{InventoryOptions: options})
	if err != nil {
		t.Fatal(err)
	}
	if !execution.Success || execution.ExitCode != 0 || !strings.Contains(execution.Output, command.SkillName) {
		t.Fatalf("execution = %+v", execution)
	}
}

func TestExactCopyDeleteRemovesOnlySelectedDirectoryAndReconciles(t *testing.T) {
	f := newFixture(t)
	root := f.agentGlobalDir(AgentCodex)
	selected := f.writeSkill(root, "selected", "delete me")
	neighbor := f.writeSkill(root, "neighbor", "keep me")
	neighborFile := filepath.Join(neighbor, "keep.txt")
	if err := os.WriteFile(neighborFile, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	inventory, err := DiscoverInventory(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	copy := findCopy(t, inventory, "selected", selected)
	command := buildDelete(t, f.options(""), copy)
	executeDelete(t, f.options(""), command)
	if _, err := os.Lstat(selected); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("selected root remains: %v", err)
	}
	data, err := os.ReadFile(neighborFile)
	if err != nil || string(data) != "unchanged" {
		t.Fatalf("neighbor changed: %q, %v", data, err)
	}
	after, err := DiscoverInventory(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range after.Skills {
		if current.Name == "selected" {
			t.Fatalf("deleted copy remained in inventory: %+v", current)
		}
	}
	findCopy(t, after, "neighbor", neighbor)
}

func TestDeletingOneOfMultipleCopiesPreservesOthersThenLastDisappears(t *testing.T) {
	f := newFixture(t)
	aPath := f.writeSkill(f.agentGlobalDir(AgentCodex), "multi", "codex")
	bPath := f.writeSkill(f.agentGlobalDir(AgentPi), "multi", "pi")
	inventory, _ := DiscoverInventory(f.options(""))
	a := findCopy(t, inventory, "multi", aPath)
	b := findCopy(t, inventory, "multi", bPath)
	executeDelete(t, f.options(""), buildDelete(t, f.options(""), a))
	afterOne, _ := DiscoverInventory(f.options(""))
	remaining := findCopy(t, afterOne, "multi", bPath)
	if remaining.ID != b.ID {
		t.Fatalf("remaining identity changed: %+v", remaining)
	}
	executeDelete(t, f.options(""), buildDelete(t, f.options(""), remaining))
	afterLast, _ := DiscoverInventory(f.options(""))
	for _, copy := range afterLast.Skills {
		if copy.Name == "multi" {
			t.Fatalf("last logical copy remains: %+v", copy)
		}
	}
}

func TestDeleteRejectsStaleAndMismatchedIdentityFields(t *testing.T) {
	f := newFixture(t)
	path := f.writeSkill(f.agentGlobalDir(AgentClaudeCode), "identity", "identity")
	inventory, _ := DiscoverInventory(f.options(""))
	copy := findCopy(t, inventory, "identity", path)
	scenarios := []struct {
		name string
		edit func(*MutationRequest)
	}{
		{"copy id", func(request *MutationRequest) { request.CopyID = strings.Repeat("f", 24) }},
		{"name", func(request *MutationRequest) { request.SkillName = "other" }},
		{"root", func(request *MutationRequest) { request.RootPath = filepath.Join(copy.AllowedRoot, "other") }},
		{"canonical", func(request *MutationRequest) { request.CanonicalPath = filepath.Join(copy.AllowedRoot, "other") }},
		{"allowed root", func(request *MutationRequest) { request.AllowedRoot = filepath.Dir(copy.AllowedRoot) }},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			request := deleteRequest(copy)
			scenario.edit(&request)
			if _, err := BuildMutationCommand(f.options(""), request); err == nil {
				t.Fatal("mismatched identity was accepted")
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("identity rejection changed source: %v", err)
			}
		})
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildMutationCommand(f.options(""), deleteRequest(copy)); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale identity error = %v", err)
	}
}

func TestExecuteRediscoversAndRejectsStaleCommand(t *testing.T) {
	f := newFixture(t)
	path := f.writeSkill(f.agentGlobalDir(AgentCursor), "stale", "stale")
	inventory, _ := DiscoverInventory(f.options(""))
	command := buildDelete(t, f.options(""), findCopy(t, inventory, "stale", path))
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteMutationCommand(context.Background(), command, MutationExecutionOptions{InventoryOptions: f.options("")}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale execute error = %v", err)
	}
}

func TestSymlinkCopyDeleteUnlinksEntryWithoutFollowingTarget(t *testing.T) {
	f := newFixture(t)
	target := f.writeSkill(filepath.Join(f.Home, "outside"), "linked", "target")
	targetFile := filepath.Join(target, "neighbor.txt")
	if err := os.WriteFile(targetFile, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := f.agentGlobalDir(AgentPi)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	inventory, _ := DiscoverInventory(f.options(""))
	copy := findCopy(t, inventory, "linked", link)
	executeDelete(t, f.options(""), buildDelete(t, f.options(""), copy))
	if _, err := os.Lstat(link); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("link remains: %v", err)
	}
	if data, err := os.ReadFile(targetFile); err != nil || string(data) != "preserve" {
		t.Fatalf("symlink target changed: %q %v", data, err)
	}
}

func TestInternalSymlinkIsNeverTraversedDuringDelete(t *testing.T) {
	f := newFixture(t)
	outside := filepath.Join(f.Home, "outside.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := f.writeSkill(f.agentGlobalDir(AgentGrok), "internal-link", "body")
	if err := os.Symlink(outside, filepath.Join(path, "escape")); err != nil {
		t.Fatal(err)
	}
	inventory, _ := DiscoverInventory(f.options(""))
	copy := findCopy(t, inventory, "internal-link", path)
	executeDelete(t, f.options(""), buildDelete(t, f.options(""), copy))
	if data, err := os.ReadFile(outside); err != nil || string(data) != "keep" {
		t.Fatalf("internal symlink target changed: %q %v", data, err)
	}
}

func TestDeleteRejectsUnsafeTrashSymlink(t *testing.T) {
	f := newFixture(t)
	root := f.agentGlobalDir(AgentOpenCode)
	path := f.writeSkill(root, "unsafe-trash", "body")
	outside := filepath.Join(f.Home, "outside-trash")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, deleteTrashDir)); err != nil {
		t.Fatal(err)
	}
	inventory, _ := DiscoverInventory(f.options(""))
	command := buildDelete(t, f.options(""), findCopy(t, inventory, "unsafe-trash", path))
	if _, err := ExecuteMutationCommand(context.Background(), command, MutationExecutionOptions{InventoryOptions: f.options("")}); err == nil || !strings.Contains(err.Error(), "safe directory") {
		t.Fatalf("trash symlink error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("source changed after rejection: %v", err)
	}
}

func TestDeleteRefusesInventoryRootAndTraversal(t *testing.T) {
	f := newFixture(t)
	root := f.agentGlobalDir(AgentCodex)
	copy := InstalledSkill{ID: strings.Repeat("a", 24), Name: "skills", RootPath: root, CanonicalPath: root, AllowedRoot: root}
	if err := validateDeleteIdentity(copy); err == nil || !strings.Contains(err.Error(), "inventory root") {
		t.Fatalf("root deletion error = %v", err)
	}
	copy.RootPath = filepath.Join(root, "..", "escape")
	copy.RootPath = filepath.Clean(copy.RootPath)
	if err := validateDeleteIdentity(copy); err == nil || !strings.Contains(err.Error(), "escaped") {
		t.Fatalf("traversal error = %v", err)
	}
}

func TestReadonlyCopiesNeverBuildDeleteCommand(t *testing.T) {
	f := newFixture(t)
	path := f.writeSkill(filepath.Join(f.Home, ".codex", "skills", ".system"), "readonly", "body")
	inventory, _ := DiscoverInventory(f.options(""))
	copy := findCopy(t, inventory, "readonly", path)
	if copy.Capability.CanDelete {
		t.Fatal("builtin unexpectedly deletable")
	}
	if _, err := BuildMutationCommand(f.options(""), deleteRequest(copy)); err == nil || !strings.Contains(err.Error(), "Codex") {
		t.Fatalf("readonly delete error = %v", err)
	}
}

func TestDeleteFailuresAreTruthfulAndRestoreOriginal(t *testing.T) {
	scenarios := []struct {
		name  string
		hooks *deleteTestHooks
	}{
		{"before rename", &deleteTestHooks{beforeRename: func(InstalledSkill) error { return errors.New("injected before rename") }}},
		{"after rename", &deleteTestHooks{afterRename: func(InstalledSkill) error { return errors.New("injected after rename") }}},
		{"remove", &deleteTestHooks{removeAll: func(*os.Root, string) error { return errors.New("injected remove failure") }}},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			f := newFixture(t)
			path := f.writeSkill(f.agentGlobalDir(AgentClaudeCode), "rollback", "body")
			options := f.options("")
			inventory, _ := DiscoverInventory(options)
			command := buildDelete(t, options, findCopy(t, inventory, "rollback", path))
			options.deleteHooks = scenario.hooks
			if _, err := ExecuteMutationCommand(context.Background(), command, MutationExecutionOptions{InventoryOptions: options}); err == nil || !strings.Contains(err.Error(), "injected") {
				t.Fatalf("partial failure = %v", err)
			}
			if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err != nil {
				t.Fatalf("original was not restored: %v", err)
			}
		})
	}
}

func TestOnlyDeleteOperationIsAccepted(t *testing.T) {
	f := newFixture(t)
	for _, operation := range []MutationOperation{"migrate", "adopt", "bind", "unbind", "uninstall", "forget", "update", "import"} {
		if _, err := BuildMutationCommand(f.options(""), MutationRequest{Operation: operation}); err == nil {
			t.Fatalf("obsolete operation %q was accepted", operation)
		}
	}
}
