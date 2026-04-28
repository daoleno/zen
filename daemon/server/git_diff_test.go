package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildGitDiffPatchIncludesStagedAndUnstagedSections(t *testing.T) {
	repoRoot := initGitDiffTestRepo(t)
	writeGitDiffTestFile(t, repoRoot, "tracked.txt", "one\n")
	runGitDiffTestGit(t, repoRoot, "add", "tracked.txt")
	runGitDiffTestGit(t, repoRoot, "commit", "-m", "initial")

	writeGitDiffTestFile(t, repoRoot, "tracked.txt", "two\n")
	runGitDiffTestGit(t, repoRoot, "add", "tracked.txt")
	writeGitDiffTestFile(t, repoRoot, "tracked.txt", "three\n")

	payload, err := (&Server{}).buildGitDiffPatch("", repoRoot, "tracked.txt")
	if err != nil {
		t.Fatalf("buildGitDiffPatch returned error: %v", err)
	}
	if payload.Path != "tracked.txt" {
		t.Fatalf("payload path = %q, want tracked.txt", payload.Path)
	}
	if len(payload.Sections) != 2 {
		t.Fatalf("section count = %d, want 2", len(payload.Sections))
	}
	if payload.Sections[0].Scope != "staged" {
		t.Fatalf("first section scope = %q, want staged", payload.Sections[0].Scope)
	}
	if payload.Sections[1].Scope != "unstaged" {
		t.Fatalf("second section scope = %q, want unstaged", payload.Sections[1].Scope)
	}
	if !strings.Contains(payload.Sections[0].Patch, "-one") || !strings.Contains(payload.Sections[0].Patch, "+two") {
		t.Fatalf("staged patch did not include expected one -> two hunk:\n%s", payload.Sections[0].Patch)
	}
	if !strings.Contains(payload.Sections[1].Patch, "-two") || !strings.Contains(payload.Sections[1].Patch, "+three") {
		t.Fatalf("unstaged patch did not include expected two -> three hunk:\n%s", payload.Sections[1].Patch)
	}
}

func TestBuildGitDiffPatchIncludesUntrackedSection(t *testing.T) {
	repoRoot := initGitDiffTestRepo(t)
	writeGitDiffTestFile(t, repoRoot, "tracked.txt", "tracked\n")
	runGitDiffTestGit(t, repoRoot, "add", "tracked.txt")
	runGitDiffTestGit(t, repoRoot, "commit", "-m", "initial")
	writeGitDiffTestFile(t, repoRoot, "new.txt", "new\n")

	payload, err := (&Server{}).buildGitDiffPatch("", repoRoot, "new.txt")
	if err != nil {
		t.Fatalf("buildGitDiffPatch returned error: %v", err)
	}
	if len(payload.Sections) != 1 {
		t.Fatalf("section count = %d, want 1", len(payload.Sections))
	}
	if payload.Sections[0].Scope != "untracked" {
		t.Fatalf("section scope = %q, want untracked", payload.Sections[0].Scope)
	}
	if !strings.Contains(payload.Sections[0].Patch, "+new") {
		t.Fatalf("untracked patch did not include expected content:\n%s", payload.Sections[0].Patch)
	}
}

func initGitDiffTestRepo(t *testing.T) string {
	t.Helper()

	repoRoot := t.TempDir()
	runGitDiffTestGit(t, repoRoot, "init")
	runGitDiffTestGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGitDiffTestGit(t, repoRoot, "config", "user.name", "Test User")
	return repoRoot
}

func writeGitDiffTestFile(t *testing.T, repoRoot, relativePath, content string) {
	t.Helper()

	path := filepath.Join(repoRoot, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent directory for %s: %v", relativePath, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", relativePath, err)
	}
}

func runGitDiffTestGit(t *testing.T, repoRoot string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}
