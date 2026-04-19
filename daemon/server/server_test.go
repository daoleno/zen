package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/task"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestCommentAncestorsReturnsOrderedReplyChain(t *testing.T) {
	s := &Server{}
	now := time.Now().UTC()
	currentTask := &task.Task{
		ID:    "task-1",
		Title: "Investigate issue detail routing",
		Comments: []task.TaskComment{
			{
				ID:          "root",
				Body:        "Original issue context",
				AuthorLabel: "Alice",
				CreatedAt:   now,
			},
			{
				ID:          "reply-1",
				Body:        "Need more detail from the agent",
				AuthorLabel: "Bob",
				ParentID:    "root",
				CreatedAt:   now.Add(time.Minute),
			},
			{
				ID:          "reply-2",
				Body:        "Please send the next update into the current run",
				AuthorLabel: "You",
				ParentID:    "reply-1",
				CreatedAt:   now.Add(2 * time.Minute),
			},
		},
	}

	chain := s.commentAncestors(currentTask, "reply-2")
	if len(chain) != 3 {
		t.Fatalf("commentAncestors len = %d, want 3", len(chain))
	}

	ids := []string{chain[0].ID, chain[1].ID, chain[2].ID}
	want := []string{"root", "reply-1", "reply-2"}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("commentAncestors[%d] = %q, want %q", i, ids[i], want[i])
		}
	}
}

func TestBuildReplyMessagesIncludeRelevantDiscussionContext(t *testing.T) {
	s := &Server{}
	now := time.Now().UTC()
	currentTask := &task.Task{
		ID:          "task-1",
		Title:       "Polish issue detail UX",
		Description: "Keep the issue detail page calm, editable, and agent-native.",
		Attachments: []task.Attachment{
			{Name: "issue-spec.md", Path: "/tmp/issue-spec.md"},
		},
		Comments: []task.TaskComment{
			{
				ID:          "root",
				Body:        "We need replies and agent routing in one thread.",
				AuthorLabel: "Alice",
				Attachments: []task.Attachment{
					{Name: "context.txt", Path: "/tmp/context.txt"},
				},
				CreatedAt: now,
			},
			{
				ID:          "reply-1",
				Body:        "Make sure replies carry context, not only the last line.",
				AuthorLabel: "Bob",
				ParentID:    "root",
				CreatedAt:   now.Add(time.Minute),
			},
		},
	}

	currentRunMessage := s.buildCurrentRunReplyMessage(
		currentTask,
		"reply-1",
		"Please continue from the existing thread.",
		[]task.Attachment{{Name: "screenshot.png", Path: "/tmp/screenshot.png"}},
	)
	if !strings.Contains(currentRunMessage, "Reply context:") {
		t.Fatalf("current run message missing reply context: %q", currentRunMessage)
	}
	if !strings.Contains(currentRunMessage, "- Alice: We need replies and agent routing in one thread.") {
		t.Fatalf("current run message missing root comment: %q", currentRunMessage)
	}
	if !strings.Contains(currentRunMessage, "- Bob: Make sure replies carry context, not only the last line.") {
		t.Fatalf("current run message missing parent comment: %q", currentRunMessage)
	}
	if !strings.Contains(currentRunMessage, "New reply:\nPlease continue from the existing thread.") {
		t.Fatalf("current run message missing new reply body: %q", currentRunMessage)
	}
	if !strings.Contains(currentRunMessage, "Attached files:\n- /tmp/screenshot.png (screenshot.png)") {
		t.Fatalf("current run message missing attachment block: %q", currentRunMessage)
	}
	if !strings.Contains(currentRunMessage, "Attachments: /tmp/context.txt") {
		t.Fatalf("current run message missing reply chain attachment line: %q", currentRunMessage)
	}

	attachedMessage := s.buildAttachedSessionMessage(
		currentTask,
		"reply-1",
		"Take this over and send back a concise plan.",
		[]task.Attachment{{Name: "trace.log", Path: "/tmp/trace.log"}},
	)
	if !strings.Contains(attachedMessage, "Issue: Polish issue detail UX") {
		t.Fatalf("attached message missing issue title: %q", attachedMessage)
	}
	if !strings.Contains(attachedMessage, "Context:\nKeep the issue detail page calm, editable, and agent-native.") {
		t.Fatalf("attached message missing issue description: %q", attachedMessage)
	}
	if !strings.Contains(attachedMessage, "Issue attachments:\n- /tmp/issue-spec.md (issue-spec.md)") {
		t.Fatalf("attached message missing issue attachments: %q", attachedMessage)
	}
	if !strings.Contains(attachedMessage, "Relevant discussion:\n- Alice: We need replies and agent routing in one thread.\n  Attachments: /tmp/context.txt\n- Bob: Make sure replies carry context, not only the last line.") {
		t.Fatalf("attached message missing discussion chain: %q", attachedMessage)
	}
	if !strings.Contains(attachedMessage, "User message:\nTake this over and send back a concise plan.") {
		t.Fatalf("attached message missing user message: %q", attachedMessage)
	}
	if !strings.Contains(attachedMessage, "Comment attachments:\n- /tmp/trace.log (trace.log)") {
		t.Fatalf("attached message missing comment attachments: %q", attachedMessage)
	}
}

func TestBuildTaskPromptRequiresExplicitAgentCommand(t *testing.T) {
	dir := t.TempDir()
	guidanceStore, err := task.NewGuidanceStore(dir)
	if err != nil {
		t.Fatalf("NewGuidanceStore: %v", err)
	}

	s := &Server{
		guidance: guidanceStore,
	}

	_, _, err = s.buildTaskPrompt(&task.Task{
		ID:          "task-1",
		Number:      12,
		Title:       "Investigate command selection",
		Description: "Make agent startup explicit.",
	}, "/tmp/zen-worktree", "", "")
	if err == nil {
		t.Fatal("expected missing agent command error")
	}
	if !strings.Contains(err.Error(), "agent command required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildTaskPromptIncludesStructuredIssueContext(t *testing.T) {
	dir := t.TempDir()
	guidanceStore, err := task.NewGuidanceStore(dir)
	if err != nil {
		t.Fatalf("NewGuidanceStore: %v", err)
	}
	projectStore, err := task.NewProjectStore(dir)
	if err != nil {
		t.Fatalf("NewProjectStore: %v", err)
	}
	project, err := projectStore.Create("wooo-cli", "", "", "/repo/root", "", "main")
	if err != nil {
		t.Fatalf("Create project: %v", err)
	}

	s := &Server{
		guidance: guidanceStore,
		projects: projectStore,
	}

	prompt, cmd, err := s.buildTaskPrompt(&task.Task{
		ID:               "task-1",
		IdentifierPrefix: project.Key,
		Number:           7,
		Title:            "Upgrade polymarket v2",
		Description:      "Follow the migration guide and update the CLI client.",
		Status:           task.StatusTodo,
		Priority:         2,
		DueDate:          "2026-04-18",
		Labels:           []string{"migration", "api"},
		ProjectID:        project.ID,
		Attachments:      []task.Attachment{{Name: "guide.md", Path: "/tmp/guide.md"}},
		Comments: []task.TaskComment{
			{ID: "c1", Body: "Please keep the old commands working.", AuthorLabel: "Alice"},
			{ID: "c2", Body: "Double-check auth edge cases.", AuthorLabel: "Bob"},
		},
	}, "/tmp/zen-worktree", "", "codex --dangerously-bypass-approvals-and-sandbox")
	if err != nil {
		t.Fatalf("buildTaskPrompt: %v", err)
	}
	if cmd != "codex --dangerously-bypass-approvals-and-sandbox" {
		t.Fatalf("command = %q, want %q", cmd, "codex --dangerously-bypass-approvals-and-sandbox")
	}
	for _, snippet := range []string{
		"Issue:\n- ID: WOO-7",
		"- Title: Upgrade polymarket v2",
		"- Project: wooo-cli",
		"- Repo root: /repo/root",
		"- Base branch: main",
		"- Workspace: /tmp/zen-worktree",
		"Goal:\nFollow the migration guide and update the CLI client.",
		"Attached files:\n- /tmp/guide.md (guide.md)",
		"Recent discussion:\n- Alice: Please keep the old commands working.\n- Bob: Double-check auth edge cases.",
		"Working rules:",
	} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("prompt missing %q:\n%s", snippet, prompt)
		}
	}
}

func TestDefaultWorktreeRootUsesGlobalZenDir(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	got := defaultWorktreeRoot("/workspace/zen")
	want := "/home/tester/.zen/worktrees/zen"
	if got != want {
		t.Fatalf("defaultWorktreeRoot = %q, want %q", got, want)
	}
}

func TestWriteTaskStateFileUsesRepoLocalZenDir(t *testing.T) {
	dir := t.TempDir()
	currentTask := &task.Task{
		ID:               "task-1",
		IdentifierPrefix: "ZEN",
		Number:           12,
		Title:            "Investigate workspace layout",
		Description:      "Keep task state in the repo-local .zen directory.",
		Cwd:              dir,
	}
	currentRun := &task.Run{
		ID:           "run-1",
		TaskID:       currentTask.ID,
		Status:       task.RunStatusRunning,
		ExecutorKind: "codex",
	}

	s := &Server{}
	if err := s.writeTaskStateFile(currentTask, currentRun); err != nil {
		t.Fatalf("writeTaskStateFile: %v", err)
	}

	path := filepath.Join(dir, ".zen", "task.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	content := string(data)
	if !strings.Contains(content, "# ZEN-12 Investigate workspace layout") {
		t.Fatalf("task state file missing heading: %q", content)
	}
	if !strings.Contains(content, "## Machine status") {
		t.Fatalf("task state file missing machine status block: %q", content)
	}

	legacyPath := filepath.Join(dir, ".zen-task.md")
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy task state path should not exist, stat err = %v", err)
	}
}

func TestSyncRunAndTaskForSessionEventKeepsTaskInProgressOnRunCompletion(t *testing.T) {
	dir := t.TempDir()
	taskStore, err := task.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	runStore, err := task.NewRunStore(dir)
	if err != nil {
		t.Fatalf("NewRunStore: %v", err)
	}

	currentTask, err := taskStore.Create(
		"Refactor issue lifecycle",
		"Separate run status from task status",
		nil,
		"",
		0,
		nil,
		"",
		"",
		"",
	)
	if err != nil {
		t.Fatalf("Create task: %v", err)
	}

	currentRun, err := runStore.Create(task.CreateRunOptions{
		TaskID:         currentTask.ID,
		Status:         task.RunStatusRunning,
		ExecutionMode:  "spawn_new_session",
		ExecutorKind:   "claude",
		AgentSessionID: "session-1",
		Summary:        "Running",
	})
	if err != nil {
		t.Fatalf("Create run: %v", err)
	}

	if _, err := taskStore.Update(currentTask.ID, func(next *task.Task) {
		next.Status = task.StatusInProgress
		next.CurrentRunID = currentRun.ID
		next.LastRunStatus = string(currentRun.Status)
	}); err != nil {
		t.Fatalf("Update task: %v", err)
	}

	s := &Server{
		tasks: taskStore,
		runs:  runStore,
	}

	s.syncRunAndTaskForSessionEvent(watcher.SessionEvent{
		Type:    "agent_state_change",
		AgentID: "session-1",
		Agent: &classifier.Agent{
			ID:      "session-1",
			State:   classifier.StateDone,
			Summary: "Implementation finished",
		},
		NewState: "done",
	})

	updatedTask := taskStore.Get(currentTask.ID)
	if updatedTask == nil {
		t.Fatal("expected updated task")
	}
	if updatedTask.Status != task.StatusInProgress {
		t.Fatalf("task status = %q, want %q", updatedTask.Status, task.StatusInProgress)
	}
	if updatedTask.LastRunStatus != string(task.RunStatusDone) {
		t.Fatalf("last run status = %q, want %q", updatedTask.LastRunStatus, task.RunStatusDone)
	}

	updatedRun := runStore.Get(currentRun.ID)
	if updatedRun == nil {
		t.Fatal("expected updated run")
	}
	if updatedRun.Status != task.RunStatusDone {
		t.Fatalf("run status = %q, want %q", updatedRun.Status, task.RunStatusDone)
	}
	if updatedRun.Summary != "Implementation finished" {
		t.Fatalf("run summary = %q, want %q", updatedRun.Summary, "Implementation finished")
	}
}
