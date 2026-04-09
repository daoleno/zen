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
	task, err := s.Create("Fix bug", "Fix the login bug", "", "/home/user/project", 2, []string{"bug"}, "")
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

	// Get
	got := s.Get(task.ID)
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Title != "Fix bug" {
		t.Errorf("Get Title = %q, want %q", got.Title, "Fix bug")
	}

	// Second create should get number 2
	task2, err := s.Create("Add tests", "", "", "", 0, nil, "")
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
		t.AgentID = "main:3"
		t.AgentStatus = "running"
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Status != StatusInProgress {
		t.Errorf("Status = %q, want %q", updated.Status, StatusInProgress)
	}

	// FindByAgentID
	found := s.FindByAgentID("main:3")
	if found == nil {
		t.Fatal("FindByAgentID returned nil")
	}
	if found.ID != task.ID {
		t.Errorf("FindByAgentID ID = %q, want %q", found.ID, task.ID)
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

	task, err := s1.Create("Persistent task", "Should survive reload", "", "", 0, nil, "")
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

	// Issue number should continue from where it left off
	task2, err := s2.Create("Next task", "", "", "", 0, nil, "")
	if err != nil {
		t.Fatalf("Create after reload: %v", err)
	}
	if task2.Number != 2 {
		t.Errorf("Number after reload = %d, want 2", task2.Number)
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
			_, _ = s.Create("Task", "", "", "", 0, nil, "")
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

	task, err := s.Create("Event task", "", "", "", 0, nil, "")
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

