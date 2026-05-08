package work

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeWorkItem(t *testing.T, path, id string) {
	t.Helper()

	content := `---
id: ` + id + `
created: 2026-04-21T00:00:00Z
---
# Item ` + id + `

Body.
`
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestStore_Scan(t *testing.T) {
	root := t.TempDir()
	writeWorkItem(t, filepath.Join(root, "zen", "a.md"), "A")
	writeWorkItem(t, filepath.Join(root, "zen", "b.md"), "B")
	writeWorkItem(t, filepath.Join(root, "inbox", "c.md"), "C")

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	all := store.List()
	if len(all) != 3 {
		t.Fatalf("len = %d, want 3", len(all))
	}
}

func TestStore_GetByID(t *testing.T) {
	root := t.TempDir()
	writeWorkItem(t, filepath.Join(root, "zen", "a.md"), "A")

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	iss, ok := store.GetByID("A")
	if !ok {
		t.Fatal("work item A not found")
	}
	if iss.Project != "zen" {
		t.Fatalf("project = %q", iss.Project)
	}
}

func TestStore_WriteAndRead(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "zen"), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	iss := &Item{
		Path: filepath.Join(root, "zen", "new.md"),
		Body: "# New\n\nBody.\n",
		Frontmatter: Frontmatter{
			ID:      "NEW",
			Created: time.Now().UTC(),
		},
	}
	written, err := store.Write(iss, time.Time{})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if written.Title != "New" {
		t.Fatalf("title = %q", written.Title)
	}

	got, ok := store.GetByID("NEW")
	if !ok {
		t.Fatal("work item NEW not found")
	}
	if got.Title != "New" {
		t.Fatalf("title = %q", got.Title)
	}
}

func TestStore_WatchNotifiesOnChange(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "zen"), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	if err := store.StartWatcher(); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}
	_, ch := store.Subscribe()

	go writeWorkItem(t, filepath.Join(root, "zen", "live.md"), "LIVE")

	select {
	case ev := <-ch:
		if ev.Type != EventChanged {
			t.Fatalf("type = %q", ev.Type)
		}
		if ev.Item == nil || ev.Item.ID != "LIVE" {
			t.Fatalf("item = %#v", ev.Item)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for change event")
	}
}

func TestStore_WatchDebouncesMultipleWrites(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "zen"), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	if err := store.StartWatcher(); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}
	_, ch := store.Subscribe()

	path := filepath.Join(root, "zen", "hot.md")
	writeWorkItem(t, path, "HOT")
	for range 3 {
		time.Sleep(50 * time.Millisecond)
		writeWorkItem(t, path, "HOT")
	}

	count := 0
	timeout := time.After(600 * time.Millisecond)
loop:
	for {
		select {
		case <-ch:
			count++
		case <-timeout:
			break loop
		}
	}

	if count == 0 {
		t.Fatal("expected at least one event")
	}
	if count > 2 {
		t.Fatalf("count = %d, want <= 2", count)
	}
}
