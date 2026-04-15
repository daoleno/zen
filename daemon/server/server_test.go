package server

import (
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
		Comments: []task.TaskComment{
			{
				ID:          "root",
				Body:        "We need replies and agent routing in one thread.",
				AuthorLabel: "Alice",
				CreatedAt:   now,
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

	attachedMessage := s.buildAttachedSessionMessage(
		currentTask,
		"reply-1",
		"Take this over and send back a concise plan.",
	)
	if !strings.Contains(attachedMessage, "Issue: Polish issue detail UX") {
		t.Fatalf("attached message missing issue title: %q", attachedMessage)
	}
	if !strings.Contains(attachedMessage, "Context:\nKeep the issue detail page calm, editable, and agent-native.") {
		t.Fatalf("attached message missing issue description: %q", attachedMessage)
	}
	if !strings.Contains(attachedMessage, "Relevant discussion:\n- Alice: We need replies and agent routing in one thread.\n- Bob: Make sure replies carry context, not only the last line.") {
		t.Fatalf("attached message missing discussion chain: %q", attachedMessage)
	}
	if !strings.Contains(attachedMessage, "User message:\nTake this over and send back a concise plan.") {
		t.Fatalf("attached message missing user message: %q", attachedMessage)
	}
}

func TestBuildTaskPromptRequiresExplicitAgentCommand(t *testing.T) {
	dir := t.TempDir()
	guidanceStore, err := task.NewGuidanceStore(dir)
	if err != nil {
		t.Fatalf("NewGuidanceStore: %v", err)
	}
	skillStore, err := task.NewSkillStore(dir)
	if err != nil {
		t.Fatalf("NewSkillStore: %v", err)
	}

	s := &Server{
		guidance: guidanceStore,
		skills:   skillStore,
	}

	_, _, err = s.buildTaskPrompt(&task.Task{
		ID:          "task-1",
		Title:       "Investigate command selection",
		Description: "Make agent startup explicit.",
	}, "", "")
	if err == nil {
		t.Fatal("expected missing agent command error")
	}
	if !strings.Contains(err.Error(), "agent command required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildTaskPromptUsesSkillAgentCommandWhenPresent(t *testing.T) {
	dir := t.TempDir()
	guidanceStore, err := task.NewGuidanceStore(dir)
	if err != nil {
		t.Fatalf("NewGuidanceStore: %v", err)
	}
	skillStore, err := task.NewSkillStore(dir)
	if err != nil {
		t.Fatalf("NewSkillStore: %v", err)
	}

	s := &Server{
		guidance: guidanceStore,
		skills:   skillStore,
	}

	_, cmd, err := s.buildTaskPrompt(&task.Task{
		ID:      "task-1",
		Title:   "Review the diff",
		SkillID: "builtin-review",
	}, "", "")
	if err != nil {
		t.Fatalf("buildTaskPrompt: %v", err)
	}
	if cmd != task.DefaultClaudeAgentCmd {
		t.Fatalf("command = %q, want %q", cmd, task.DefaultClaudeAgentCmd)
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
		"",
		"",
		0,
		nil,
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
