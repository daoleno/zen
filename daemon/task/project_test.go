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
		"/workspace/.zen/worktrees/zen",
		"main",
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if project.Key != "ZEN" {
		t.Fatalf("key = %q, want ZEN", project.Key)
	}
	if project.RepoRoot != "/workspace/zen" {
		t.Fatalf("repo root = %q, want /workspace/zen", project.RepoRoot)
	}

	updated, err := store.Update(project.ID, func(current *Project) {
		current.Name = "Zen Mobile"
		current.WorktreeRoot = "/workspace/.zen/worktrees/zen-v2"
		current.BaseBranch = "develop"
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if updated.Key != "ZEN" {
		t.Fatalf("updated key = %q, want stable ZEN", updated.Key)
	}
	if updated.WorktreeRoot != "/workspace/.zen/worktrees/zen-v2" {
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
	if got.Key != "ZEN" {
		t.Fatalf("reloaded key = %q, want ZEN", got.Key)
	}
	if got.WorktreeRoot != "/workspace/.zen/worktrees/zen-v2" {
		t.Fatalf("reloaded worktree root = %q, want updated value", got.WorktreeRoot)
	}
	if got.BaseBranch != "develop" {
		t.Fatalf("reloaded base branch = %q, want develop", got.BaseBranch)
	}
}

func TestProjectStoreDerivesUniqueKeys(t *testing.T) {
	dir := t.TempDir()
	store, err := NewProjectStore(dir)
	if err != nil {
		t.Fatalf("NewProjectStore: %v", err)
	}

	first, err := store.Create("wooo-cli", "", "", "", "")
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	second, err := store.Create("wooo mobile", "", "", "", "")
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}
	third, err := store.Create("0g-agent-market", "", "", "", "")
	if err != nil {
		t.Fatalf("Create third: %v", err)
	}

	if first.Key != "WOO" {
		t.Fatalf("first key = %q, want WOO", first.Key)
	}
	if second.Key != "WOO2" {
		t.Fatalf("second key = %q, want WOO2", second.Key)
	}
	if third.Key != "0GA" {
		t.Fatalf("third key = %q, want 0GA", third.Key)
	}
}

func TestProjectStoreLoadBackfillsMissingKeys(t *testing.T) {
	dir := t.TempDir()
	store, err := NewProjectStore(dir)
	if err != nil {
		t.Fatalf("NewProjectStore: %v", err)
	}

	first, err := store.Create("better-wallet", "", "", "", "")
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	second, err := store.Create("backend-api", "", "", "", "")
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}

	if _, err := store.Update(first.ID, func(current *Project) {
		current.Key = ""
	}); err != nil {
		t.Fatalf("Clear first key: %v", err)
	}
	if _, err := store.Update(second.ID, func(current *Project) {
		current.Key = ""
	}); err != nil {
		t.Fatalf("Clear second key: %v", err)
	}

	reloaded, err := NewProjectStore(dir)
	if err != nil {
		t.Fatalf("NewProjectStore reload: %v", err)
	}

	firstReloaded := reloaded.Get(first.ID)
	if firstReloaded == nil {
		t.Fatal("expected first project after reload")
	}
	if firstReloaded.Key != "BET" {
		t.Fatalf("first reloaded key = %q, want BET", firstReloaded.Key)
	}

	secondReloaded := reloaded.Get(second.ID)
	if secondReloaded == nil {
		t.Fatal("expected second project after reload")
	}
	if secondReloaded.Key != "BAC" {
		t.Fatalf("second reloaded key = %q, want BAC", secondReloaded.Key)
	}
}
