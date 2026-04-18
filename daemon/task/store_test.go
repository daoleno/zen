package task

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestStoreCRUD(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	task, err := s.Create(
		"Fix bug",
		"Fix the login bug",
		[]Attachment{{Name: "screenshot.png", Path: "/tmp/screenshot.png"}},
		"/home/user/project",
		2,
		[]string{"bug"},
		"",
		"2026-04-20",
		"",
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if task.Title != "Fix bug" {
		t.Errorf("Title = %q, want %q", task.Title, "Fix bug")
	}
	if task.Status != StatusBacklog {
		t.Errorf("Status = %q, want %q", task.Status, StatusBacklog)
	}
	if task.Number != 1 {
		t.Errorf("Number = %d, want 1", task.Number)
	}
	if task.IdentifierPrefix != DefaultIdentifierPrefix {
		t.Errorf("IdentifierPrefix = %q, want %q", task.IdentifierPrefix, DefaultIdentifierPrefix)
	}
	if task.Priority != 2 {
		t.Errorf("Priority = %d, want 2", task.Priority)
	}
	if len(task.Labels) != 1 || task.Labels[0] != "bug" {
		t.Errorf("Labels = %v, want [bug]", task.Labels)
	}
	if task.DueDate != "2026-04-20" {
		t.Errorf("DueDate = %q, want %q", task.DueDate, "2026-04-20")
	}
	if len(task.Attachments) != 1 || task.Attachments[0].Path != "/tmp/screenshot.png" {
		t.Errorf("Attachments = %#v, want persisted attachment", task.Attachments)
	}

	got := s.Get(task.ID)
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Title != "Fix bug" {
		t.Errorf("Get Title = %q, want %q", got.Title, "Fix bug")
	}

	task2, err := s.Create("Add tests", "", nil, "", 0, nil, "", "", "")
	if err != nil {
		t.Fatalf("Create #2: %v", err)
	}
	if task2.Number != 2 {
		t.Errorf("Number = %d, want 2", task2.Number)
	}

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("List len = %d, want 2", len(list))
	}

	updated, err := s.Update(task.ID, func(t *Task) {
		t.Status = StatusInProgress
		t.CurrentRunID = "run-1"
		t.LastRunStatus = "running"
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Status != StatusInProgress {
		t.Errorf("Status = %q, want %q", updated.Status, StatusInProgress)
	}

	found := s.FindByCurrentRunID("run-1")
	if found == nil {
		t.Fatal("FindByCurrentRunID returned nil")
	}
	if found.ID != task.ID {
		t.Errorf("FindByCurrentRunID ID = %q, want %q", found.ID, task.ID)
	}

	if err := s.Delete(task.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if s.Get(task.ID) != nil {
		t.Error("Get after Delete returned non-nil")
	}
}

func TestStorePersistence(t *testing.T) {
	dir := t.TempDir()
	s1, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	task, err := s1.Create("Persistent task", "Should survive reload", nil, "", 0, nil, "", "2026-05-01", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	s2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore reload: %v", err)
	}

	got := s2.Get(task.ID)
	if got == nil {
		t.Fatal("Task not found after reload")
	}
	if got.Title != "Persistent task" {
		t.Errorf("Title after reload = %q, want %q", got.Title, "Persistent task")
	}
	if got.DueDate != "2026-05-01" {
		t.Errorf("DueDate after reload = %q, want %q", got.DueDate, "2026-05-01")
	}

	task2, err := s2.Create("Next task", "", nil, "", 0, nil, "", "", "")
	if err != nil {
		t.Fatalf("Create after reload: %v", err)
	}
	if task2.Number != 2 {
		t.Errorf("Number after reload = %d, want 2", task2.Number)
	}
}

func TestStoreCommentsPersist(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	task, err := s.Create("Commented task", "", nil, "", 0, nil, "", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := s.AddComment(task.ID, TaskComment{
		ID:             "comment-1",
		Body:           "Please take a look at this.",
		Attachments:    []Attachment{{Name: "trace.log", Path: "/tmp/trace.log"}},
		AuthorKind:     "user",
		AuthorLabel:    "You",
		ParentID:       "comment-root",
		DeliveryMode:   "comment",
		AgentSessionID: "session-1",
		TargetLabel:    "repo",
	})
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	if len(updated.Comments) != 1 {
		t.Fatalf("Comments len = %d, want 1", len(updated.Comments))
	}
	if updated.Comments[0].Body != "Please take a look at this." {
		t.Fatalf("Comment body = %q, want persisted body", updated.Comments[0].Body)
	}

	reloaded, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore reload: %v", err)
	}

	got := reloaded.Get(task.ID)
	if got == nil {
		t.Fatal("Get after reload returned nil")
	}
	if len(got.Comments) != 1 {
		t.Fatalf("Reloaded comments len = %d, want 1", len(got.Comments))
	}
	if got.Comments[0].TargetLabel != "repo" {
		t.Fatalf("TargetLabel = %q, want repo", got.Comments[0].TargetLabel)
	}
	if got.Comments[0].ParentID != "comment-root" {
		t.Fatalf("ParentID = %q, want comment-root", got.Comments[0].ParentID)
	}
	if len(got.Comments[0].Attachments) != 1 || got.Comments[0].Attachments[0].Path != "/tmp/trace.log" {
		t.Fatalf("Attachments = %#v, want persisted comment attachment", got.Comments[0].Attachments)
	}
}

func TestStoreConcurrency(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.Create("Task", "", nil, "", 0, nil, "", "", "")
		}()
	}
	wg.Wait()

	list := s.List()
	if len(list) != 20 {
		t.Errorf("List len = %d, want 20", len(list))
	}
}

func TestStoreEvents(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	task, err := s.Create("Event task", "", nil, "", 0, nil, "", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	select {
	case e := <-s.Events():
		if e.Type != "task_created" {
			t.Errorf("event type = %q, want task_created", e.Type)
		}
		if e.TaskID != task.ID {
			t.Errorf("event task_id = %q, want %q", e.TaskID, task.ID)
		}
	default:
		t.Error("no create event received")
	}

	_, _ = s.Update(task.ID, func(t *Task) { t.Status = StatusInProgress })
	select {
	case e := <-s.Events():
		if e.Type != "task_updated" {
			t.Errorf("event type = %q, want task_updated", e.Type)
		}
	default:
		t.Error("no update event received")
	}

	_ = s.Delete(task.ID)
	select {
	case e := <-s.Events():
		if e.Type != "task_deleted" {
			t.Errorf("event type = %q, want task_deleted", e.Type)
		}
	default:
		t.Error("no delete event received")
	}
}

func TestStoreClearProjectRemovesProjectReferenceAndEmitsUpdates(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	projectTask, err := s.Create("Project task", "", nil, "", 0, nil, "project-1", "", "PRO")
	if err != nil {
		t.Fatalf("Create project task: %v", err)
	}
	otherTask, err := s.Create("Other task", "", nil, "", 0, nil, "project-2", "", "OTH")
	if err != nil {
		t.Fatalf("Create other task: %v", err)
	}

	select {
	case <-s.Events():
	default:
	}
	select {
	case <-s.Events():
	default:
	}

	updated, err := s.ClearProject("project-1")
	if err != nil {
		t.Fatalf("ClearProject: %v", err)
	}
	if len(updated) != 1 {
		t.Fatalf("ClearProject updated %d tasks, want 1", len(updated))
	}
	if updated[0].ID != projectTask.ID {
		t.Fatalf("updated task id = %q, want %q", updated[0].ID, projectTask.ID)
	}
	if updated[0].ProjectID != "" {
		t.Fatalf("updated project id = %q, want empty", updated[0].ProjectID)
	}

	got := s.Get(projectTask.ID)
	if got == nil {
		t.Fatal("expected updated task")
	}
	if got.ProjectID != "" {
		t.Fatalf("task project id = %q, want empty", got.ProjectID)
	}
	if got.IdentifierPrefix != "PRO" {
		t.Fatalf("task prefix = %q, want PRO", got.IdentifierPrefix)
	}

	unchanged := s.Get(otherTask.ID)
	if unchanged == nil {
		t.Fatal("expected unchanged task")
	}
	if unchanged.ProjectID != "project-2" {
		t.Fatalf("other task project id = %q, want %q", unchanged.ProjectID, "project-2")
	}
	if unchanged.IdentifierPrefix != "OTH" {
		t.Fatalf("other task prefix = %q, want OTH", unchanged.IdentifierPrefix)
	}

	select {
	case event := <-s.Events():
		if event.Type != "task_updated" {
			t.Fatalf("event type = %q, want task_updated", event.Type)
		}
		if event.TaskID != projectTask.ID {
			t.Fatalf("event task id = %q, want %q", event.TaskID, projectTask.ID)
		}
		if event.Task == nil || event.Task.ProjectID != "" {
			t.Fatalf("event task project id = %#v, want empty", event.Task)
		}
	default:
		t.Fatal("expected task_updated event after ClearProject")
	}

	reloaded, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore reload: %v", err)
	}
	reloadedTask := reloaded.Get(projectTask.ID)
	if reloadedTask == nil {
		t.Fatal("expected task after reload")
	}
	if reloadedTask.ProjectID != "" {
		t.Fatalf("reloaded project id = %q, want empty", reloadedTask.ProjectID)
	}
	if reloadedTask.IdentifierPrefix != "PRO" {
		t.Fatalf("reloaded prefix = %q, want PRO", reloadedTask.IdentifierPrefix)
	}
}

func TestStoreNumbersAreScopedByIdentifierPrefix(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	backlog, err := s.Create("General issue", "", nil, "", 0, nil, "", "", "")
	if err != nil {
		t.Fatalf("Create backlog: %v", err)
	}
	projectOne, err := s.Create("Project issue 1", "", nil, "", 0, nil, "project-1", "", "WOO")
	if err != nil {
		t.Fatalf("Create project issue 1: %v", err)
	}
	projectTwo, err := s.Create("Project issue 2", "", nil, "", 0, nil, "project-1", "", "WOO")
	if err != nil {
		t.Fatalf("Create project issue 2: %v", err)
	}
	otherProject, err := s.Create("Project issue A", "", nil, "", 0, nil, "project-2", "", "ABC")
	if err != nil {
		t.Fatalf("Create project issue A: %v", err)
	}

	if backlog.Number != 1 || backlog.IdentifierPrefix != DefaultIdentifierPrefix {
		t.Fatalf("backlog = %s, want %s", DisplayID(backlog), FormatDisplayID(DefaultIdentifierPrefix, 1))
	}
	if projectOne.Number != 1 || projectOne.IdentifierPrefix != "WOO" {
		t.Fatalf("project one = %s, want %s", DisplayID(projectOne), FormatDisplayID("WOO", 1))
	}
	if projectTwo.Number != 2 || projectTwo.IdentifierPrefix != "WOO" {
		t.Fatalf("project two = %s, want %s", DisplayID(projectTwo), FormatDisplayID("WOO", 2))
	}
	if otherProject.Number != 1 || otherProject.IdentifierPrefix != "ABC" {
		t.Fatalf("other project = %s, want %s", DisplayID(otherProject), FormatDisplayID("ABC", 1))
	}
}

func TestStoreLoadBackfillsLegacyIdentifierPrefixAndCounter(t *testing.T) {
	dir := t.TempDir()

	legacyTasks := []*Task{
		{ID: "task-1", Number: 1, Title: "Legacy 1"},
		{ID: "task-2", Number: 3, Title: "Legacy 3"},
	}
	tasksData, err := json.MarshalIndent(legacyTasks, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent tasks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), tasksData, 0o600); err != nil {
		t.Fatalf("Write tasks.json: %v", err)
	}

	metaData, err := json.MarshalIndent(storeMeta{NextIssueNumber: 4}, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), metaData, 0o600); err != nil {
		t.Fatalf("Write meta.json: %v", err)
	}

	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	for _, id := range []string{"task-1", "task-2"} {
		current := s.Get(id)
		if current == nil {
			t.Fatalf("expected task %s", id)
		}
		if current.IdentifierPrefix != DefaultIdentifierPrefix {
			t.Fatalf("legacy prefix = %q, want %q", current.IdentifierPrefix, DefaultIdentifierPrefix)
		}
	}

	next, err := s.Create("Next legacy issue", "", nil, "", 0, nil, "", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if next.Number != 4 {
		t.Fatalf("Number = %d, want 4", next.Number)
	}
	if next.IdentifierPrefix != DefaultIdentifierPrefix {
		t.Fatalf("IdentifierPrefix = %q, want %q", next.IdentifierPrefix, DefaultIdentifierPrefix)
	}
}
