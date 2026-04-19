package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/daoleno/zen/daemon/task"
)

func TestReadTaskStateSnapshotParsesStructuredSections(t *testing.T) {
	dir := t.TempDir()
	taskStore, err := task.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	workspaceDir := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(filepath.Join(workspaceDir, ".zen"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	currentTask, err := taskStore.Create(
		"Surface task note in UI",
		"Expose the live task note to mobile users.",
		nil,
		workspaceDir,
		0,
		nil,
		"",
		"",
		"ZEN",
	)
	if err != nil {
		t.Fatalf("Create task: %v", err)
	}

	content := `# ZEN-1 Surface task note in UI

## Goal
Expose the live task note to mobile users.

## Machine status
<!-- ZEN:STATUS START -->
- Updated: 2026-04-19T10:20:30Z
- Task status: in_progress
- Run status: blocked
- Run attempt: 2
- Workspace: /tmp/workspace
- Session: agent-1
- Summary: Waiting for product feedback.
<!-- ZEN:STATUS END -->

## Completed
- Parsed the file on the daemon.
- Wired the response into the issue screen.

## Known pitfalls / blockers
- Users cannot see raw markdown yet.

## Next step
- Add a refresh affordance in the UI.
`

	if err := os.WriteFile(taskStateFilePath(workspaceDir), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s := &Server{tasks: taskStore}
	snapshot, err := s.readTaskStateSnapshot(currentTask.ID)
	if err != nil {
		t.Fatalf("readTaskStateSnapshot: %v", err)
	}

	if !snapshot.Available {
		t.Fatal("expected task state snapshot to be available")
	}
	if snapshot.Title != "ZEN-1 Surface task note in UI" {
		t.Fatalf("title = %q", snapshot.Title)
	}
	if snapshot.Path != taskStateFilePath(workspaceDir) {
		t.Fatalf("path = %q, want %q", snapshot.Path, taskStateFilePath(workspaceDir))
	}
	if snapshot.Goal.Body != "Expose the live task note to mobile users." {
		t.Fatalf("goal = %q", snapshot.Goal.Body)
	}
	if snapshot.MachineStatus.RunStatus != "blocked" {
		t.Fatalf("run status = %q", snapshot.MachineStatus.RunStatus)
	}
	if snapshot.MachineStatus.RunAttempt != 2 {
		t.Fatalf("run attempt = %d, want 2", snapshot.MachineStatus.RunAttempt)
	}
	if snapshot.MachineStatus.Summary != "Waiting for product feedback." {
		t.Fatalf("summary = %q", snapshot.MachineStatus.Summary)
	}
	if len(snapshot.Completed.Items) != 2 {
		t.Fatalf("completed items = %d, want 2", len(snapshot.Completed.Items))
	}
	if len(snapshot.Blockers.Items) != 1 || snapshot.Blockers.Items[0] != "Users cannot see raw markdown yet." {
		t.Fatalf("blockers = %#v", snapshot.Blockers.Items)
	}
	if len(snapshot.NextStep.Items) != 1 || snapshot.NextStep.Items[0] != "Add a refresh affordance in the UI." {
		t.Fatalf("next step = %#v", snapshot.NextStep.Items)
	}
}

func TestReadTaskStateSnapshotReturnsUnavailableWhenFileMissing(t *testing.T) {
	dir := t.TempDir()
	taskStore, err := task.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	workspaceDir := filepath.Join(dir, "workspace")
	currentTask, err := taskStore.Create(
		"Missing state file",
		"",
		nil,
		workspaceDir,
		0,
		nil,
		"",
		"",
		"ZEN",
	)
	if err != nil {
		t.Fatalf("Create task: %v", err)
	}

	s := &Server{tasks: taskStore}
	snapshot, err := s.readTaskStateSnapshot(currentTask.ID)
	if err != nil {
		t.Fatalf("readTaskStateSnapshot: %v", err)
	}

	if snapshot.Available {
		t.Fatal("expected missing task state file to report unavailable")
	}
	if snapshot.Path != taskStateFilePath(workspaceDir) {
		t.Fatalf("path = %q, want %q", snapshot.Path, taskStateFilePath(workspaceDir))
	}
}
