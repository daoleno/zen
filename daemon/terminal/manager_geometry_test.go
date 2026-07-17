package terminal

import (
	"context"
	"sync"
	"testing"
)

type phoneGeometryBackend struct {
	opened OpenOptions
	events chan Event
}

func (b *phoneGeometryBackend) Name() string { return "phone-geometry" }

func (b *phoneGeometryBackend) Open(_ string, options OpenOptions) (Session, error) {
	b.opened = options
	b.events = make(chan Event)
	return &phoneGeometrySession{
		id:     "phone-session",
		size:   Size{Cols: options.Cols, Rows: options.Rows},
		events: b.events,
	}, nil
}

type phoneGeometrySession struct {
	mu     sync.Mutex
	id     string
	size   Size
	events chan Event
	writes []string
}

func (s *phoneGeometrySession) ID() string                  { return s.id }
func (s *phoneGeometrySession) Start(context.Context) error { return nil }
func (s *phoneGeometrySession) Events() <-chan Event        { return s.events }
func (s *phoneGeometrySession) Close() error                { return nil }

func (s *phoneGeometrySession) Write(data string) error {
	s.mu.Lock()
	s.writes = append(s.writes, data)
	s.mu.Unlock()
	return nil
}

func (s *phoneGeometrySession) Resize(cols, rows int) error {
	s.mu.Lock()
	s.size = Size{Cols: cols, Rows: rows}
	s.mu.Unlock()
	return nil
}

func (s *phoneGeometrySession) Size() Size {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.size
}

func TestManagerPublishesAndResizesTheExactPhoneGrid(t *testing.T) {
	backend := &phoneGeometryBackend{}
	manager := NewManager(backend)
	var messages []map[string]any

	opened, err := manager.Open(
		"owner-a",
		backend.Name(),
		"target-a",
		OpenOptions{Cols: 44, Rows: 18},
		func(value any) {
			messages = append(messages, value.(map[string]any))
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { close(backend.events) })
	if backend.opened != (OpenOptions{Cols: 44, Rows: 18}) {
		t.Fatalf("backend open grid = %+v, want 44x18", backend.opened)
	}
	if got := opened.Size(); got != (Size{Cols: 44, Rows: 18}) {
		t.Fatalf("opened session grid = %+v, want 44x18", got)
	}
	if len(messages) != 1 ||
		messages[0]["type"] != "terminal_opened" ||
		messages[0]["cols"] != 44 ||
		messages[0]["rows"] != 18 {
		t.Fatalf("initial blank-session protocol messages = %#v, want one 44x18 terminal_opened", messages)
	}

	if err := manager.Input("owner-a", opened.ID(), "x"); err != nil {
		t.Fatalf("blank opened session should be immediately usable: %v", err)
	}
	if got := opened.(*phoneGeometrySession).writes; len(got) != 1 || got[0] != "x" {
		t.Fatalf("blank-session input writes = %v, want [x] exactly once", got)
	}

	if err := manager.Resize("owner-a", opened.ID(), 72, 20); err != nil {
		t.Fatal(err)
	}
	if got := opened.Size(); got != (Size{Cols: 72, Rows: 20}) {
		t.Fatalf("resized session grid = %+v, want 72x20", got)
	}
}
