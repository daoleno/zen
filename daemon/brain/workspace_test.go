package brain

import (
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
	if !workspaceTreeHasDirectory(tree.Entries, "worklog") {
		t.Fatal("WorkspaceTree() missing worklog directory")
	}
	if !workspaceTreeHasFile(tree.Entries, "worklog/README.md") {
		t.Fatal("WorkspaceTree() missing worklog/README.md")
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
