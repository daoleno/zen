package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWorkspaceTreeListsDefaultMarkdownFiles(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	tree, err := store.WorkspaceTree()
	if err != nil {
		t.Fatalf("WorkspaceTree() error = %v", err)
	}

	if tree.Workspace != store.WorkspacePath() {
		t.Fatalf("WorkspaceTree().Workspace = %q, want %q", tree.Workspace, store.WorkspacePath())
	}
	if !workspaceTreeHasFile(tree.Entries, "memory.md") {
		t.Fatal("WorkspaceTree() missing memory.md")
	}
	if !workspaceTreeHasFile(tree.Entries, "profile.md") {
		t.Fatal("WorkspaceTree() missing profile.md")
	}
	if !workspaceTreeHasFile(tree.Entries, "current.md") {
		t.Fatal("WorkspaceTree() missing current.md")
	}
	if !workspaceTreeHasDirectory(tree.Entries, "policies") {
		t.Fatal("WorkspaceTree() missing policies directory")
	}
	policies, err := store.WorkspaceTree("policies")
	if err != nil {
		t.Fatalf("WorkspaceTree(policies) error = %v", err)
	}
	if policies.Path != "policies" {
		t.Fatalf("WorkspaceTree(policies).Path = %q, want policies", policies.Path)
	}
	if !workspaceTreeHasFile(policies.Entries, "policies/delegation.md") {
		t.Fatal("WorkspaceTree() missing policies/delegation.md")
	}
	if !workspaceTreeHasFile(policies.Entries, "policies/engine.md") {
		t.Fatal("WorkspaceTree() missing policies/engine.md")
	}
	if !workspaceTreeHasFile(policies.Entries, "policies/handoff.md") {
		t.Fatal("WorkspaceTree() missing policies/handoff.md")
	}
	if !workspaceTreeHasDirectory(tree.Entries, "worklog") {
		t.Fatal("WorkspaceTree() missing worklog directory")
	}
	worklog, err := store.WorkspaceTree("worklog")
	if err != nil {
		t.Fatalf("WorkspaceTree(worklog) error = %v", err)
	}
	if !workspaceTreeHasFile(worklog.Entries, "worklog/README.md") {
		t.Fatal("WorkspaceTree() missing worklog/README.md")
	}
	if !workspaceTreeHasDirectory(tree.Entries, "playbooks") {
		t.Fatal("WorkspaceTree() missing playbooks directory")
	}
	playbooks, err := store.WorkspaceTree("playbooks")
	if err != nil {
		t.Fatalf("WorkspaceTree(playbooks) error = %v", err)
	}
	if !workspaceTreeHasFile(playbooks.Entries, "playbooks/brain-flows.md") {
		t.Fatal("WorkspaceTree() missing playbooks/brain-flows.md")
	}
}

func TestWorkspaceTreeDoesNotRecursivelyLoadLargeFolders(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	crowded := filepath.Join(store.WorkspacePath(), "crowded")
	if err := os.MkdirAll(crowded, 0o700); err != nil {
		t.Fatalf("create crowded directory: %v", err)
	}
	for index := 0; index <= maxWorkspaceEntries; index++ {
		name := filepath.Join(crowded, fmt.Sprintf("file-%04d.md", index))
		if err := os.WriteFile(name, nil, 0o600); err != nil {
			t.Fatalf("write crowded file %d: %v", index, err)
		}
	}

	tree, err := store.WorkspaceTree()
	if err != nil {
		t.Fatalf("WorkspaceTree() recursively traversed crowded folder: %v", err)
	}
	if !workspaceTreeHasDirectory(tree.Entries, "crowded") {
		t.Fatal("WorkspaceTree() missing crowded directory")
	}
}

func TestNewStoreEnsuresProfileNotesWithVoiceAndAntiSlop(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	raw, err := os.ReadFile(store.profileNotesPath())
	if err != nil {
		t.Fatalf("read profile.md: %v", err)
	}
	content := string(raw)
	for _, marker := range currentProfileNotesMarkers {
		if !strings.Contains(content, marker) {
			t.Fatalf("profile.md missing %q:\n%s", marker, content)
		}
	}
}

func TestNewStoreUpgradesExistingProfileNotesWithoutOverwriting(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	customProfile := "# Brain Profile\n\nKeep my custom note.\n"
	if err := os.WriteFile(filepath.Join(workspace, "profile.md"), []byte(customProfile), 0o600); err != nil {
		t.Fatalf("write profile.md: %v", err)
	}

	if _, err := NewStore(root); err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(workspace, "profile.md"))
	if err != nil {
		t.Fatalf("read profile.md: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "Keep my custom note.") {
		t.Fatalf("profile.md lost custom content:\n%s", content)
	}
	for _, marker := range currentProfileNotesMarkers {
		if !strings.Contains(content, marker) {
			t.Fatalf("profile.md missing %q:\n%s", marker, content)
		}
	}
}

func TestNewStoreEnsuresWorkspaceCommunicationRules(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	raw, err := os.ReadFile(store.workspaceInstructionsPath())
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	content := string(raw)
	for _, marker := range []string{
		"## Brain Communication Rules",
		"Avoid AI slop",
		"Answer first",
		"Do not be sycophantic",
		"## Brain Orchestration Rules",
		"## Executor Rules",
		"## Zen CLI",
	} {
		if !strings.Contains(content, marker) {
			t.Fatalf("AGENTS.md missing %q:\n%s", marker, content)
		}
	}
}

func TestNewStoreEnsuresCurrentAndPolicyDocs(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	current, err := os.ReadFile(store.currentPath())
	if err != nil {
		t.Fatalf("read current.md: %v", err)
	}
	for _, marker := range []string{
		"# Current Brain Context",
		"## Active Objective",
		"## Decisions",
		"## Open Threads",
		"## Next",
	} {
		if !strings.Contains(string(current), marker) {
			t.Fatalf("current.md missing %q:\n%s", marker, current)
		}
	}

	for _, policy := range []struct {
		path   string
		marker string
	}{
		{store.policyPath("delegation.md"), "Reduce user decision load"},
		{store.policyPath("delegation.md"), "Final synthesis should be concise and judgmental"},
		{store.policyPath("engine.md"), "Delegated agents use the configured Delegated Executor unless the user explicitly asks for a different executor for that session."},
		{store.policyPath("handoff.md"), "Host executor switching preserves the visible Brain chat."},
	} {
		raw, err := os.ReadFile(policy.path)
		if err != nil {
			t.Fatalf("read policy %s: %v", policy.path, err)
		}
		if !strings.Contains(string(raw), policy.marker) {
			t.Fatalf("policy %s missing %q:\n%s", policy.path, policy.marker, raw)
		}
	}
}

func TestNewStoreUpgradesExistingDelegationPolicyWithoutOverwriting(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	policies := filepath.Join(workspace, "policies")
	if err := os.MkdirAll(policies, 0o700); err != nil {
		t.Fatalf("create policies: %v", err)
	}
	customDelegation := "# Custom Delegation\n\nKeep my local rule.\n"
	if err := os.WriteFile(filepath.Join(policies, "delegation.md"), []byte(customDelegation), 0o600); err != nil {
		t.Fatalf("write delegation policy: %v", err)
	}

	if _, err := NewStore(root); err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	readDelegation, err := os.ReadFile(filepath.Join(policies, "delegation.md"))
	if err != nil {
		t.Fatalf("read delegation policy: %v", err)
	}
	content := string(readDelegation)
	for _, want := range []string{
		"Keep my local rule.",
		"Reduce user decision load",
		"## Orchestrator / Delegation Model",
		"Brain owns decomposition, ordering, judgment, result review, and final synthesis",
		"Delegated agents are scoped execution sessions",
		"Do not ask a delegated agent to invent the plan",
		"Review delegated output before integrating it",
		"Final synthesis should be concise and judgmental",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("delegation policy missing %q:\n%s", want, content)
		}
	}
}

func TestNewStoreEnsuresWorklogReadme(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	raw, err := os.ReadFile(store.worklogReadmePath())
	if err != nil {
		t.Fatalf("read worklog README: %v", err)
	}
	content := string(raw)
	for _, marker := range []string{
		"# Brain Worklog",
		"YYYY-MM-DD-short-title.md",
		"- Status:",
		"- Date:",
		"## Context",
		"## Goal",
		"## Todo",
		"## Progress",
		"## Verification",
		"## Result",
		"## Follow-up",
	} {
		if !strings.Contains(content, marker) {
			t.Fatalf("worklog README missing %q:\n%s", marker, content)
		}
	}

	if _, err := NewStore(root); err != nil {
		t.Fatalf("second NewStore() error = %v", err)
	}
	secondRaw, err := os.ReadFile(store.worklogReadmePath())
	if err != nil {
		t.Fatalf("read worklog README after second NewStore: %v", err)
	}
	if string(secondRaw) != content {
		t.Fatal("second NewStore() rewrote worklog README")
	}
}

func TestNewStoreBackfillsMissingWorklogForExistingWorkspace(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatalf("create existing workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "memory.md"), []byte("# Existing Memory\n\n"), 0o600); err != nil {
		t.Fatalf("write existing memory: %v", err)
	}

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if info, err := os.Stat(store.worklogPath()); err != nil {
		t.Fatalf("stat worklog directory: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("worklog path is not a directory: %s", store.worklogPath())
	}
	if _, err := os.Stat(store.worklogReadmePath()); err != nil {
		t.Fatalf("stat worklog README: %v", err)
	}
}

func TestNewStoreDoesNotOverwriteExistingWorklogFiles(t *testing.T) {
	root := t.TempDir()
	worklog := filepath.Join(root, "workspace", "worklog")
	if err := os.MkdirAll(worklog, 0o700); err != nil {
		t.Fatalf("create existing worklog: %v", err)
	}
	customReadme := "# Custom Worklog\n\nKeep my structure.\n"
	taskRecord := "# Existing Task\n\n## Result\n\nDone.\n"
	if err := os.WriteFile(filepath.Join(worklog, "README.md"), []byte(customReadme), 0o600); err != nil {
		t.Fatalf("write custom README: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worklog, "2026-06-09-existing-task.md"), []byte(taskRecord), 0o600); err != nil {
		t.Fatalf("write task record: %v", err)
	}

	if _, err := NewStore(root); err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	readme, err := os.ReadFile(filepath.Join(worklog, "README.md"))
	if err != nil {
		t.Fatalf("read custom README: %v", err)
	}
	if string(readme) != customReadme {
		t.Fatalf("custom README was overwritten:\n%s", string(readme))
	}
	task, err := os.ReadFile(filepath.Join(worklog, "2026-06-09-existing-task.md"))
	if err != nil {
		t.Fatalf("read task record: %v", err)
	}
	if string(task) != taskRecord {
		t.Fatalf("task record was overwritten:\n%s", string(task))
	}
}

func TestNewStorePreservesCurrentAndUpgradesExistingPolicyDocs(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	policies := filepath.Join(workspace, "policies")
	if err := os.MkdirAll(policies, 0o700); err != nil {
		t.Fatalf("create policies: %v", err)
	}
	customCurrent := "# Custom Current\n\nKeep this.\n"
	customEngine := "# Custom Engine Policy\n\nKeep this too.\n"
	customHandoff := "# Custom Handoff Policy\n\nKeep this handoff rule.\n"
	if err := os.WriteFile(filepath.Join(workspace, "current.md"), []byte(customCurrent), 0o600); err != nil {
		t.Fatalf("write current.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(policies, "engine.md"), []byte(customEngine), 0o600); err != nil {
		t.Fatalf("write engine policy: %v", err)
	}
	if err := os.WriteFile(filepath.Join(policies, "handoff.md"), []byte(customHandoff), 0o600); err != nil {
		t.Fatalf("write handoff policy: %v", err)
	}

	if _, err := NewStore(root); err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	readCurrent, err := os.ReadFile(filepath.Join(workspace, "current.md"))
	if err != nil {
		t.Fatalf("read current.md: %v", err)
	}
	if string(readCurrent) != customCurrent {
		t.Fatalf("current.md was overwritten:\n%s", readCurrent)
	}
	readEngine, err := os.ReadFile(filepath.Join(policies, "engine.md"))
	if err != nil {
		t.Fatalf("read engine policy: %v", err)
	}
	engineContent := string(readEngine)
	for _, want := range []string{
		"Keep this too.",
		"## Current Executor Rules",
		"Delegated agents use the configured Delegated Executor unless the user explicitly asks for a different executor for that session.",
		"Do not switch executors based on private task-type judgment.",
	} {
		if !strings.Contains(engineContent, want) {
			t.Fatalf("engine policy missing %q:\n%s", want, engineContent)
		}
	}
	readHandoff, err := os.ReadFile(filepath.Join(policies, "handoff.md"))
	if err != nil {
		t.Fatalf("read handoff policy: %v", err)
	}
	handoffContent := string(readHandoff)
	for _, want := range []string{
		"Keep this handoff rule.",
		"## Current Handoff Rules",
		"Treat a host executor switch as a host replacement, not a new conversation.",
		"Keep handoff prompts private",
	} {
		if !strings.Contains(handoffContent, want) {
			t.Fatalf("handoff policy missing %q:\n%s", want, handoffContent)
		}
	}
}

func TestReadWorkspaceFileReadsMarkdown(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	file, err := store.ReadWorkspaceFile("memory.md")
	if err != nil {
		t.Fatalf("ReadWorkspaceFile() error = %v", err)
	}
	if file.Path != "memory.md" {
		t.Fatalf("Path = %q, want memory.md", file.Path)
	}
	if file.Language != "markdown" {
		t.Fatalf("Language = %q, want markdown", file.Language)
	}
	if file.Content == "" {
		t.Fatal("Content is empty")
	}

	worklogReadme, err := store.ReadWorkspaceFile("worklog/README.md")
	if err != nil {
		t.Fatalf("ReadWorkspaceFile(worklog/README.md) error = %v", err)
	}
	if worklogReadme.Language != "markdown" {
		t.Fatalf("Worklog README language = %q, want markdown", worklogReadme.Language)
	}
	if !strings.Contains(worklogReadme.Content, "# Brain Worklog") {
		t.Fatalf("Worklog README content missing title:\n%s", worklogReadme.Content)
	}
}

func TestReadWorkspaceFileRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(filepath.Join(root, "brain"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside.md"), []byte("# Outside\n"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	if _, err := store.ReadWorkspaceFile("../outside.md"); err == nil {
		t.Fatal("ReadWorkspaceFile() accepted parent traversal")
	}
	if _, err := store.ReadWorkspaceFile(filepath.Join(root, "outside.md")); err == nil {
		t.Fatal("ReadWorkspaceFile() accepted absolute path")
	}
}

func TestReadWorkspaceFileRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on windows")
	}

	root := t.TempDir()
	store, err := NewStore(filepath.Join(root, "brain"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	outside := filepath.Join(root, "outside.md")
	if err := os.WriteFile(outside, []byte("# Outside\n"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(store.WorkspacePath(), "outside.md")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	if _, err := store.ReadWorkspaceFile("outside.md"); err == nil {
		t.Fatal("ReadWorkspaceFile() accepted symlink")
	}
}

func TestReadWorkspaceFileRejectsSymlinkDirectoryTraversal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on windows")
	}

	root := t.TempDir()
	store, err := NewStore(filepath.Join(root, "brain"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	outsideDir := filepath.Join(root, "outside")
	if err := os.MkdirAll(outsideDir, 0o700); err != nil {
		t.Fatalf("create outside dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outsideDir, "secret.md"), []byte("# Outside\n"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(store.WorkspacePath(), "linked")); err != nil {
		t.Fatalf("create symlink dir: %v", err)
	}

	if _, err := store.ReadWorkspaceFile("linked/secret.md"); err == nil {
		t.Fatal("ReadWorkspaceFile() accepted symlink directory traversal")
	}
}

func workspaceTreeHasFile(entries []WorkspaceEntry, path string) bool {
	for _, entry := range entries {
		if entry.Kind == "file" && entry.Path == path {
			return true
		}
		if workspaceTreeHasFile(entry.Children, path) {
			return true
		}
	}
	return false
}

func workspaceTreeHasDirectory(entries []WorkspaceEntry, path string) bool {
	for _, entry := range entries {
		if entry.Kind == "directory" && entry.Path == path {
			return true
		}
		if workspaceTreeHasDirectory(entry.Children, path) {
			return true
		}
	}
	return false
}
