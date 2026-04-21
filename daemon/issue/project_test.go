package issue

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProject_Explicit(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "project.toml"), []byte(`
name = "zen"
cwd = "/home/x/code/zen"
executor = "codex"
`), 0o600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	project, err := LoadProject(dir)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if project.Name != "zen" || project.Cwd != "/home/x/code/zen" || project.Executor != "codex" {
		t.Fatalf("project = %+v", project)
	}
}

func TestLoadProject_MissingFileDefaultsToBasename(t *testing.T) {
	dir := t.TempDir()
	inboxDir := filepath.Join(dir, "inbox")
	if err := os.MkdirAll(inboxDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	project, err := LoadProject(inboxDir)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if project.Name != "inbox" {
		t.Fatalf("name = %q", project.Name)
	}
	if project.Cwd != "" {
		t.Fatalf("cwd = %q, want empty", project.Cwd)
	}
}

func TestLoadProject_MalformedTOMLError(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "project.toml"), []byte("this is not = toml ]]]"), 0o600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := LoadProject(dir); err == nil {
		t.Fatal("expected error for malformed TOML")
	}
}
