package brain

import (
	"os"
	"path/filepath"
	"runtime"
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
