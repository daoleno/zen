package task

import (
	"sync"
	"testing"
)

func TestStoreCRUD(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Create
	task, err := s.Create("Fix bug", "Fix the login bug", "", "/home/user/project", 2, []string{"bug"}, "", "2026-04-20")
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
	if task.Priority != 2 {
		t.Errorf("Priority = %d, want 2", task.Priority)
	}
	if len(task.Labels) != 1 || task.Labels[0] != "bug" {
		t.Errorf("Labels = %v, want [bug]", task.Labels)
	}
	if task.DueDate != "2026-04-20" {
		t.Errorf("DueDate = %q, want %q", task.DueDate, "2026-04-20")
	}

	// Get
	got := s.Get(task.ID)
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Title != "Fix bug" {
		t.Errorf("Get Title = %q, want %q", got.Title, "Fix bug")
	}

	// Second create should get number 2
	task2, err := s.Create("Add tests", "", "", "", 0, nil, "", "")
	if err != nil {
		t.Fatalf("Create #2: %v", err)
	}
	if task2.Number != 2 {
		t.Errorf("Number = %d, want 2", task2.Number)
	}

	// List
	list := s.List()
	if len(list) != 2 {
		t.Fatalf("List len = %d, want 2", len(list))
	}

	// Update
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

	// FindByCurrentRunID
	found := s.FindByCurrentRunID("run-1")
	if found == nil {
		t.Fatal("FindByCurrentRunID returned nil")
	}
	if found.ID != task.ID {
		t.Errorf("FindByCurrentRunID ID = %q, want %q", found.ID, task.ID)
	}

	// Delete
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

	task, err := s1.Create("Persistent task", "Should survive reload", "", "", 0, nil, "", "2026-05-01")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Create a new store from the same directory
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

	// Issue number should continue from where it left off
	task2, err := s2.Create("Next task", "", "", "", 0, nil, "", "")
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

	task, err := s.Create("Commented task", "", "", "", 0, nil, "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := s.AddComment(task.ID, TaskComment{
		ID:             "comment-1",
		Body:           "Please take a look at this.",
		AuthorKind:     "user",
		AuthorLabel:    "You",
		ParentID:       "comment-root",
		DeliveryMode:   "note",
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
			_, _ = s.Create("Task", "", "", "", 0, nil, "", "")
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

	task, err := s.Create("Event task", "", "", "", 0, nil, "", "")
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

	projectTask, err := s.Create("Project task", "", "", "", 0, nil, "project-1", "")
	if err != nil {
		t.Fatalf("Create project task: %v", err)
	}
	otherTask, err := s.Create("Other task", "", "", "", 0, nil, "project-2", "")
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

	unchanged := s.Get(otherTask.ID)
	if unchanged == nil {
		t.Fatal("expected unchanged task")
	}
	if unchanged.ProjectID != "project-2" {
		t.Fatalf("other task project id = %q, want %q", unchanged.ProjectID, "project-2")
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
}
