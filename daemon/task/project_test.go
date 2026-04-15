package task

import "testing"

func TestProjectStoreCreateUpdateAndReload(t *testing.T) {
	dir := t.TempDir()
	store, err := NewProjectStore(dir)
	if err != nil {
		t.Fatalf("NewProjectStore: %v", err)
	}

	project, err := store.Create(
		"Zen",
		"",
		"/workspace/zen",
		"/workspace/.zen-worktrees/zen",
		"main",
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if project.RepoRoot != "/workspace/zen" {
		t.Fatalf("repo root = %q, want /workspace/zen", project.RepoRoot)
	}

	updated, err := store.Update(project.ID, func(current *Project) {
		current.WorktreeRoot = "/workspace/.zen-worktrees/zen-v2"
		current.BaseBranch = "develop"
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if updated.WorktreeRoot != "/workspace/.zen-worktrees/zen-v2" {
		t.Fatalf("worktree root = %q, want updated value", updated.WorktreeRoot)
	}
	if updated.BaseBranch != "develop" {
		t.Fatalf("base branch = %q, want develop", updated.BaseBranch)
	}

	reloaded, err := NewProjectStore(dir)
	if err != nil {
		t.Fatalf("NewProjectStore reload: %v", err)
	}

	got := reloaded.Get(project.ID)
	if got == nil {
		t.Fatal("expected persisted project")
	}
	if got.WorktreeRoot != "/workspace/.zen-worktrees/zen-v2" {
		t.Fatalf("reloaded worktree root = %q, want updated value", got.WorktreeRoot)
	}
	if got.BaseBranch != "develop" {
		t.Fatalf("reloaded base branch = %q, want develop", got.BaseBranch)
	}
}
