package terminal

import (
	"context"
	"sync"
	"testing"
	"time"
)

type serializedScrollFixture struct {
	mu            sync.Mutex
	order         []string
	scrollLines   []int
	scrollEntered chan struct{}
	releaseScroll chan struct{}
	writeEntered  chan struct{}
	scrollOnce    sync.Once
	writeOnce     sync.Once
}

func newSerializedScrollFixture() *serializedScrollFixture {
	return &serializedScrollFixture{
		scrollEntered: make(chan struct{}),
		releaseScroll: make(chan struct{}),
		writeEntered:  make(chan struct{}),
	}
}

func (s *serializedScrollFixture) ID() string                  { return "session-a" }
func (s *serializedScrollFixture) Start(context.Context) error { return nil }
func (s *serializedScrollFixture) Events() <-chan Event        { return make(chan Event) }
func (s *serializedScrollFixture) Resize(int, int) error       { return nil }
func (s *serializedScrollFixture) Close() error                { return nil }
func (s *serializedScrollFixture) Size() Size                  { return Size{Cols: 44, Rows: 18} }

func (s *serializedScrollFixture) record(value string) {
	s.mu.Lock()
	s.order = append(s.order, value)
	s.mu.Unlock()
}

func (s *serializedScrollFixture) Scroll(lines int) error {
	s.mu.Lock()
	s.scrollLines = append(s.scrollLines, lines)
	s.mu.Unlock()
	s.record("scroll-start")
	s.scrollOnce.Do(func() { close(s.scrollEntered) })
	<-s.releaseScroll
	s.record("scroll-end")
	return nil
}

func (s *serializedScrollFixture) recordedScrollLines() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.scrollLines...)
}

func (s *serializedScrollFixture) CancelScroll() error {
	s.record("cancel")
	return nil
}

func (s *serializedScrollFixture) Write(data string) error {
	s.record("write:" + data)
	s.writeOnce.Do(func() { close(s.writeEntered) })
	return nil
}

func (s *serializedScrollFixture) recordedOrder() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.order...)
}

func managerWithScrollFixture(fixture Session) *Manager {
	manager := NewManager()
	manager.sessions[fixture.ID()] = &managedSession{
		owner:   "owner-a",
		target:  "target-a",
		session: fixture,
		cancel:  func() {},
	}
	return manager
}

func TestManagerSerializesScrollThenInputCancelAndWrite(t *testing.T) {
	fixture := newSerializedScrollFixture()
	manager := managerWithScrollFixture(fixture)
	scrollDone := make(chan error, 1)
	go func() { scrollDone <- manager.Scroll("owner-a", fixture.ID(), -2) }()
	<-fixture.scrollEntered

	inputDone := make(chan error, 1)
	go func() { inputDone <- manager.Input("owner-a", fixture.ID(), "x") }()

	select {
	case <-fixture.writeEntered:
		t.Fatal("input overtook the active serialized scroll command")
	case <-time.After(40 * time.Millisecond):
	}

	close(fixture.releaseScroll)
	if err := <-scrollDone; err != nil {
		t.Fatal(err)
	}
	if err := <-inputDone; err != nil {
		t.Fatal(err)
	}

	want := []string{"scroll-start", "scroll-end", "cancel", "write:x"}
	if got := fixture.recordedOrder(); !equalStrings(got, want) {
		t.Fatalf("serialized interaction order = %v, want %v", got, want)
	}
}

func TestManagerInputCancelsCopyModeThenWritesExactlyOnce(t *testing.T) {
	fixture := newSerializedScrollFixture()
	close(fixture.releaseScroll)
	manager := managerWithScrollFixture(fixture)

	if err := manager.Input("owner-a", fixture.ID(), "payload"); err != nil {
		t.Fatal(err)
	}
	want := []string{"cancel", "write:payload"}
	if got := fixture.recordedOrder(); !equalStrings(got, want) {
		t.Fatalf("input transition = %v, want %v", got, want)
	}
}

func TestManagerRejectsStaleSessionScrollAndInputWithoutReplay(t *testing.T) {
	fixture := newSerializedScrollFixture()
	manager := managerWithScrollFixture(fixture)

	if err := manager.Scroll("owner-a", "stale-session", -1); err == nil {
		t.Fatal("stale scroll unexpectedly succeeded")
	}
	if err := manager.Input("owner-a", "stale-session", "x"); err == nil {
		t.Fatal("stale input unexpectedly succeeded")
	}
	if got := fixture.recordedOrder(); len(got) != 0 {
		t.Fatalf("stale activity reached session: %v", got)
	}
}

func TestManagerBoundsEveryProtocolScrollBatch(t *testing.T) {
	fixture := newSerializedScrollFixture()
	close(fixture.releaseScroll)
	manager := managerWithScrollFixture(fixture)

	if err := manager.Scroll("owner-a", fixture.ID(), -999); err != nil {
		t.Fatal(err)
	}
	if got := fixture.recordedScrollLines(); len(got) != 1 || got[0] != -maxTerminalScrollBatchLines {
		t.Fatalf("bounded scroll lines = %v, want [%d]", got, -maxTerminalScrollBatchLines)
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
