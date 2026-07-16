package server

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/chatthread"
	"github.com/daoleno/zen/daemon/codexshadow"
	"github.com/daoleno/zen/daemon/work"
)

type fakeCodexShadowObserver struct {
	mu           sync.Mutex
	enabled      bool
	err          error
	observations []codexshadow.Observation
	done         chan struct{}
}

type blockingCodexShadowObserver struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (*blockingCodexShadowObserver) Enabled(string) bool { return true }

func (observer *blockingCodexShadowObserver) ObserveRollout(
	context.Context,
	codexshadow.Observation,
) (chatthread.ShadowSnapshot, error) {
	observer.calls.Add(1)
	select {
	case observer.started <- struct{}{}:
	default:
	}
	<-observer.release
	return chatthread.ShadowSnapshot{}, nil
}

func (observer *fakeCodexShadowObserver) Enabled(string) bool {
	return observer != nil && observer.enabled
}

func (observer *fakeCodexShadowObserver) ObserveRollout(
	_ context.Context,
	observation codexshadow.Observation,
) (chatthread.ShadowSnapshot, error) {
	observer.mu.Lock()
	observer.observations = append(observer.observations, observation)
	observer.mu.Unlock()
	if observer.done != nil {
		select {
		case observer.done <- struct{}{}:
		default:
		}
	}
	return chatthread.ShadowSnapshot{}, observer.err
}

func (observer *fakeCodexShadowObserver) wait(t *testing.T) []codexshadow.Observation {
	t.Helper()
	if observer.done != nil {
		select {
		case <-observer.done:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for shadow observation")
		}
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	return append([]codexshadow.Observation{}, observer.observations...)
}

func TestCodexShadowObservationCannotMutateLegacyProjectionOrDispatch(t *testing.T) {
	now := time.Date(2026, 7, 16, 3, 3, 45, 411000000, time.UTC)
	conversation := work.CodexConversation{
		Available: true,
		Source:    "codex_rollout",
		Path:      "/private/rollout.jsonl",
		SessionID: "provider-session",
		Turn: &work.CodexConversationTurn{
			ID:        "public-turn-1",
			Status:    work.CodexConversationTurnRunning,
			StartedAt: now.Add(-time.Minute).Format(time.RFC3339Nano),
		},
		QueuedTurns: []work.CodexConversationTurn{{
			ID:        "public-turn-2",
			Status:    work.CodexConversationTurnQueued,
			StartedAt: now.Format(time.RFC3339Nano),
		}},
		Events: []work.CodexConversationEvent{{
			ID:   "event-1",
			Kind: "user_message",
			Body: "must remain only in v1",
		}},
	}
	before, err := json.Marshal(conversation)
	if err != nil {
		t.Fatal(err)
	}
	observer := &fakeCodexShadowObserver{
		enabled: true,
		err:     chatthread.ErrShadowCorrupt,
		done:    make(chan struct{}, 1),
	}
	server := &Server{
		structuredTurns: newStructuredTurnRegistry(),
		codexShadow:     observer,
		structuredSnapshotLoader: func(string) (work.CodexConversation, error) {
			return work.CodexConversation{Available: true, Events: []work.CodexConversationEvent{}}, nil
		},
	}

	providerEffects := 0
	raw := clientMessage{AgentID: "fixture-agent", TurnID: "public-turn-1"}
	accepted, err := server.acceptStructuredInput(raw, func() error {
		providerEffects++
		return nil
	})
	if err != nil {
		t.Fatalf("legacy acceptance: %v", err)
	}
	if accepted.TurnID != raw.TurnID || providerEffects != 1 {
		t.Fatalf("legacy acceptance/effects = %#v / %d", accepted, providerEffects)
	}

	server.observeCodexConversationShadow(
		context.Background(),
		raw,
		resolvedCodexConversationAgent{
			targetID:    raw.AgentID,
			provider:    "codex",
			fromWatcher: true,
		},
		conversation,
	)
	observations := observer.wait(t)
	after, err := json.Marshal(conversation)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("shadow observation changed legacy wire projection\nbefore: %s\nafter:  %s", before, after)
	}
	if providerEffects != 1 {
		t.Fatalf("shadow crossed provider boundary: effects = %d, want 1", providerEffects)
	}
	if len(observations) != 1 {
		t.Fatalf("shadow observations = %d", len(observations))
	}
	observed := observations[0]
	if len(observed.Legacy.OrderedTurns) != 2 || observed.Legacy.Current == nil ||
		len(observed.Legacy.Queued) != 1 || observed.Legacy.TerminalState != "" {
		t.Fatalf("sanitized legacy comparison input = %#v", observed.Legacy)
	}

	// Exact v1 retry remains registry-owned and does not dispatch again. The
	// failing shadow observer cannot alter its disposition.
	replayed, err := server.acceptStructuredInput(raw, func() error {
		providerEffects++
		return nil
	})
	if err != nil || !replayed.Duplicate || providerEffects != 1 {
		t.Fatalf("legacy replay = %#v, err %v, effects %d", replayed, err, providerEffects)
	}
}

func TestLegacyShadowProjectionReportsOnlyLifecycleMetadata(t *testing.T) {
	terminal := work.CodexConversation{
		Turn: &work.CodexConversationTurn{
			ID:     "terminal-public-id",
			Status: work.CodexConversationTurnCompleted,
		},
		QueuedTurns: []work.CodexConversationTurn{},
		Events: []work.CodexConversationEvent{{
			ID:      "sensitive-event-id",
			Kind:    "tool",
			Body:    "sensitive body",
			Command: "sensitive command",
			Output:  "sensitive output",
			Files:   []string{"/sensitive/path"},
		}},
	}
	projection := legacyShadowProjection(terminal)
	raw, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"sensitive body", "sensitive command", "sensitive output", "/sensitive/path"} {
		if reflect.ValueOf(raw).Len() > 0 && containsBytes(raw, forbidden) {
			t.Fatalf("legacy diagnostic projection contains %q: %s", forbidden, raw)
		}
	}
	if projection.Current != nil || projection.TerminalState != work.CodexConversationTurnCompleted ||
		len(projection.OrderedTurns) != 1 {
		t.Fatalf("terminal legacy projection = %#v", projection)
	}
}

func containsBytes(raw []byte, value string) bool {
	for index := 0; index+len(value) <= len(raw); index++ {
		if string(raw[index:index+len(value)]) == value {
			return true
		}
	}
	return false
}

func TestCodexShadowObserverErrorsAreDiagnosticOnly(t *testing.T) {
	observer := &fakeCodexShadowObserver{
		enabled: true,
		err:     errors.New("provider path must not be returned"),
		done:    make(chan struct{}, 1),
	}
	server := &Server{codexShadow: observer}
	conversation := work.CodexConversation{
		Path:        "/sensitive/source",
		SessionID:   "session",
		QueuedTurns: []work.CodexConversationTurn{},
		Events:      []work.CodexConversationEvent{},
	}
	server.observeCodexConversationShadow(
		context.Background(),
		clientMessage{AgentID: "agent"},
		resolvedCodexConversationAgent{targetID: "agent", provider: "codex", fromWatcher: true},
		conversation,
	)
	if observations := observer.wait(t); len(observations) != 1 {
		t.Fatalf("observer was not invoked")
	}
}

func TestCodexShadowObservationIsBoundedAndDoesNotBlockV1Subscription(t *testing.T) {
	observer := &blockingCodexShadowObserver{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	server := &Server{codexShadow: observer}
	conversation := work.CodexConversation{
		Path:        "/transient/source",
		SessionID:   "session",
		QueuedTurns: []work.CodexConversationTurn{},
		Events:      []work.CodexConversationEvent{},
	}
	invoke := func() {
		server.observeCodexConversationShadow(
			context.Background(),
			clientMessage{AgentID: "agent"},
			resolvedCodexConversationAgent{targetID: "agent", provider: "codex", fromWatcher: true},
			conversation,
		)
	}
	returned := make(chan struct{})
	go func() {
		invoke()
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("shadow observation blocked the v1 subscription path")
	}
	select {
	case <-observer.started:
	case <-time.After(2 * time.Second):
		t.Fatal("background shadow observation did not start")
	}

	invoke()
	if got := observer.calls.Load(); got != 1 {
		t.Fatalf("single-flight shadow calls = %d, want 1", got)
	}
	close(observer.release)
}
