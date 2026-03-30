package terminal

import "context"

// EventType classifies terminal session events.
type EventType string

const (
	EventHistory EventType = "history"
	EventOutput  EventType = "output"
	EventScroll  EventType = "scroll"
	EventExit    EventType = "exit"
	EventError   EventType = "error"
)

type ScrollState struct {
	AtBottom   bool
	InCopyMode bool
	Position   int
}

// Event is emitted by a terminal session.
type Event struct {
	Type        EventType
	Data        string
	ExitCode    int
	Err         error
	ScrollState ScrollState
}

// OpenOptions configures a terminal session.
type OpenOptions struct {
	Cols int
	Rows int
}

// Size reports the current terminal dimensions.
type Size struct {
	Cols int
	Rows int
}

// Session represents a live terminal session.
type Session interface {
	ID() string
	Start(ctx context.Context) error
	Events() <-chan Event
	Write(data string) error
	Resize(cols, rows int) error
	Close() error
	Size() Size
}

// Scroller is optionally implemented by sessions that support tmux copy-mode scrolling.
type Scroller interface {
	Scroll(lines int) error
	CancelScroll() error
}

// Backend opens terminal sessions for a given target.
type Backend interface {
	Name() string
	Open(targetID string, opts OpenOptions) (Session, error)
}
