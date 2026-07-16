package server

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/work"
)

func TestCanonicalCodexProjectionOneActivityConsumesFiveSubmissions(t *testing.T) {
	projection := newCanonicalCodexProjection()
	scope := "agent:codex-fixture"
	startedAt := time.Date(2026, 7, 16, 2, 50, 45, 176000000, time.UTC)
	activityID := "activity-one"

	projection.project(scope, work.CodexConversation{
		Available: true,
		SessionID: "session-one",
		Turn: &work.CodexConversationTurn{
			ID: activityID, Status: work.CodexConversationTurnRunning,
			StartedAt: startedAt.Format(time.RFC3339Nano),
		},
		Events: []work.CodexConversationEvent{},
	})

	for index := 1; index <= 5; index++ {
		id := fmt.Sprintf("submission-%d", index)
		body := "same body"
		if index > 3 {
			body = fmt.Sprintf("body-%d", index)
		}
		accepted, err := projection.acceptWithDispatch(
			scope,
			canonicalCodexSubmission{
				ID: id, Body: body,
				StartedAt: startedAt.Add(time.Duration(index) * time.Millisecond),
				AttemptID: fmt.Sprintf("attempt-%d", index),
			},
			func() (structuredInputAcceptance, error) {
				return structuredInputAcceptance{TurnID: id, Queued: index > 1}, nil
			},
		)
		if err != nil {
			t.Fatalf("accept %s: %v", id, err)
		}
		if accepted.Position == 0 || accepted.Revision == 0 {
			t.Fatalf("acceptance lacks canonical identity: %#v", accepted)
		}
	}

	settledAt := startedAt.Add(13 * time.Minute)
	events := make([]work.CodexConversationEvent, 0, 6)
	for index := 1; index <= 5; index++ {
		body := "same body"
		if index > 3 {
			body = fmt.Sprintf("body-%d", index)
		}
		events = append(events, work.CodexConversationEvent{
			ID:        fmt.Sprintf("provider-user-%d", index),
			Seq:       index,
			Timestamp: startedAt.Add(time.Duration(index) * time.Second).Format(time.RFC3339Nano),
			Kind:      "user_message", Role: "user", Body: body,
			ActivityID: activityID,
		})
	}
	events = append(events, work.CodexConversationEvent{
		ID: "assistant-final", Seq: 6,
		Timestamp: settledAt.Add(-time.Second).Format(time.RFC3339Nano),
		Kind:      "assistant_message", Role: "assistant", Body: "done",
		ActivityID: activityID,
	})

	snapshot := projection.project(scope, work.CodexConversation{
		Available: true,
		SessionID: "session-one",
		Turn: &work.CodexConversationTurn{
			ID: activityID, Status: work.CodexConversationTurnCompleted,
			StartedAt: startedAt.Format(time.RFC3339Nano),
			SettledAt: settledAt.Format(time.RFC3339Nano),
		},
		ProviderTurns: []work.CodexConversationTurn{{
			ID: activityID, Status: work.CodexConversationTurnCompleted,
			StartedAt: startedAt.Format(time.RFC3339Nano),
			SettledAt: settledAt.Format(time.RFC3339Nano),
		}},
		Events: events,
	})

	if snapshot.Conversation.Activity == nil ||
		snapshot.Conversation.Activity.Status != work.CodexConversationTurnCompleted ||
		snapshot.Conversation.Active == nil || *snapshot.Conversation.Active {
		t.Fatalf("terminal Activity still owns Working: %#v", snapshot.Conversation)
	}
	if len(snapshot.Conversation.QueuedTurns) != 0 {
		t.Fatalf("Submissions were projected as queued Activities: %#v", snapshot.Conversation.QueuedTurns)
	}
	var userIDs []string
	var previousPosition uint64
	for _, event := range snapshot.Conversation.Events {
		if event.Position <= previousPosition {
			t.Fatalf("non-monotonic position %d after %d", event.Position, previousPosition)
		}
		previousPosition = event.Position
		if event.Kind == "user_message" {
			userIDs = append(userIDs, event.ID)
			if event.SubmissionState != "delivered" {
				t.Fatalf("submission %s state = %q", event.ID, event.SubmissionState)
			}
		}
	}
	if got := fmt.Sprint(userIDs); got != "[submission-1 submission-2 submission-3 submission-4 submission-5]" {
		t.Fatalf("stable Submission IDs/order = %s", got)
	}
}

func TestCanonicalCodexProjectionReconnectPreservesHistoryButExplicitEmptyReplaces(t *testing.T) {
	projection := newCanonicalCodexProjection()
	scope := "agent:reconnect"
	initial := projection.project(scope, work.CodexConversation{
		Available: true,
		SessionID: "session-one",
		Events: []work.CodexConversationEvent{{
			ID: "history", Seq: 1, Kind: "assistant_message", Body: "kept",
		}},
	})
	if len(initial.Conversation.Events) != 1 {
		t.Fatalf("initial snapshot = %#v", initial)
	}

	transient := projection.project(scope, work.CodexConversation{
		Available: false,
		Reason:    "transcript_not_found",
		SessionID: "session-one",
		Events:    []work.CodexConversationEvent{},
	})
	if len(transient.Conversation.Events) != 1 || transient.Replace {
		t.Fatalf("transient reconnect erased canonical history: %#v", transient)
	}

	empty := projection.project(scope, work.CodexConversation{
		Available: true,
		SessionID: "session-one",
		Events:    []work.CodexConversationEvent{},
	})
	if len(empty.Conversation.Events) != 0 || !empty.Replace || empty.Revision <= transient.Revision {
		t.Fatalf("explicit empty snapshot did not replace: %#v", empty)
	}
}

func TestCanonicalCodexProjectionExactRetryDispatchesOnlyAfterKnownRejection(t *testing.T) {
	projection := newCanonicalCodexProjection()
	scope := "agent:retry"
	submission := canonicalCodexSubmission{
		ID: "submission-stable", Body: "same immutable body",
		StartedAt: time.Date(2026, 7, 16, 4, 0, 0, 0, time.UTC),
		AttemptID: "attempt-one",
	}
	dispatches := 0
	_, err := projection.acceptWithDispatch(scope, submission, func() (structuredInputAcceptance, error) {
		dispatches++
		return structuredInputAcceptance{}, &structuredInputRejectedError{
			cause: fmt.Errorf("known zero-effect rejection"), code: "not_dispatched", retryable: true,
		}
	})
	if err == nil || dispatches != 1 {
		t.Fatalf("first rejection: dispatches=%d err=%v", dispatches, err)
	}

	accepted, err := projection.acceptWithDispatch(scope, submission, func() (structuredInputAcceptance, error) {
		dispatches++
		return structuredInputAcceptance{TurnID: submission.ID}, nil
	})
	if err != nil || dispatches != 2 || accepted.Position == 0 {
		t.Fatalf("retry: acceptance=%#v dispatches=%d err=%v", accepted, dispatches, err)
	}

	duplicate, err := projection.acceptWithDispatch(scope, submission, func() (structuredInputAcceptance, error) {
		dispatches++
		return structuredInputAcceptance{}, nil
	})
	if err != nil || dispatches != 2 || !duplicate.Duplicate || duplicate.Position != accepted.Position {
		t.Fatalf("accepted replay duplicated effect: acceptance=%#v dispatches=%d err=%v", duplicate, dispatches, err)
	}
}

func TestCanonicalCodexProjectionUnconfirmedReplayNeverRepeatsDispatch(t *testing.T) {
	projection := newCanonicalCodexProjection()
	submission := canonicalCodexSubmission{
		ID: "submission-uncertain", Body: "possibly delivered",
		StartedAt: time.Date(2026, 7, 16, 4, 30, 0, 0, time.UTC),
		AttemptID: "attempt-uncertain",
	}
	dispatches := 0
	dispatchErr := &structuredInputDeliveryUnconfirmedError{
		cause: fmt.Errorf("executor reply lost after write"),
	}
	_, firstErr := projection.acceptWithDispatch(
		"agent:uncertain",
		submission,
		func() (structuredInputAcceptance, error) {
			dispatches++
			return structuredInputAcceptance{}, dispatchErr
		},
	)
	if !errors.Is(firstErr, dispatchErr) || dispatches != 1 {
		t.Fatalf("first uncertain dispatch: err=%v dispatches=%d", firstErr, dispatches)
	}

	_, replayErr := projection.acceptWithDispatch(
		"agent:uncertain",
		submission,
		func() (structuredInputAcceptance, error) {
			dispatches++
			return structuredInputAcceptance{}, nil
		},
	)
	if !errors.Is(replayErr, dispatchErr) || dispatches != 1 {
		t.Fatalf("uncertain replay repeated effect: err=%v dispatches=%d", replayErr, dispatches)
	}
}

func TestCanonicalCodexProjectionKnownZeroEffectRejectionCannotStealSuccessorAdmission(t *testing.T) {
	projection := newCanonicalCodexProjection()
	scope := "agent:rejection-binding"
	started := time.Date(2026, 7, 16, 4, 45, 0, 0, time.UTC)

	_, rejectedErr := projection.acceptWithDispatch(
		scope,
		canonicalCodexSubmission{
			ID: "submission-rejected", Body: "never crossed",
			StartedAt: started, AttemptID: "attempt-rejected",
		},
		func() (structuredInputAcceptance, error) {
			return structuredInputAcceptance{}, &structuredInputRejectedError{
				cause: fmt.Errorf("known zero-effect rejection"), code: "not_dispatched", retryable: true,
			}
		},
	)
	if rejectedErr == nil {
		t.Fatal("known zero-effect dispatch was not rejected")
	}

	_, err := projection.acceptWithDispatch(
		scope,
		canonicalCodexSubmission{
			ID: "submission-success", Body: "did cross",
			StartedAt: started.Add(time.Second), AttemptID: "attempt-success",
		},
		func() (structuredInputAcceptance, error) {
			return structuredInputAcceptance{TurnID: "submission-success"}, nil
		},
	)
	if err != nil {
		t.Fatalf("accept successor: %v", err)
	}

	snapshot := projection.project(scope, work.CodexConversation{
		Available: true,
		SessionID: "rejection-binding-session",
		Turn: &work.CodexConversationTurn{
			ID: "activity-one", Status: work.CodexConversationTurnRunning,
			StartedAt: started.Format(time.RFC3339Nano),
		},
		Events: []work.CodexConversationEvent{{
			ID: "provider-success", Seq: 1,
			Kind: "user_message", Role: "user", Body: "did cross",
			ActivityID: "activity-one",
		}},
	})

	states := make(map[string]string)
	for _, event := range snapshot.Conversation.Events {
		if event.Kind == "user_message" {
			states[event.ID] = event.SubmissionState
		}
	}
	if states["submission-rejected"] != "rejected" {
		t.Fatalf("known rejection state = %q, all states = %#v", states["submission-rejected"], states)
	}
	if states["submission-success"] != "delivered" {
		t.Fatalf("successor admission was stolen: states = %#v", states)
	}
}

func TestCanonicalCodexProjectionRetainsSequentialActivityHistory(t *testing.T) {
	projection := newCanonicalCodexProjection()
	started := time.Date(2026, 7, 16, 5, 0, 0, 0, time.UTC)
	turns := []work.CodexConversationTurn{
		{ID: "activity-a", Status: work.CodexConversationTurnCompleted, StartedAt: started.Format(time.RFC3339Nano), SettledAt: started.Add(time.Minute).Format(time.RFC3339Nano)},
		{ID: "activity-b", Status: work.CodexConversationTurnCompleted, StartedAt: started.Add(2 * time.Minute).Format(time.RFC3339Nano), SettledAt: started.Add(3 * time.Minute).Format(time.RFC3339Nano)},
	}
	events := []work.CodexConversationEvent{
		{ID: "input-a", Seq: 1, Kind: "user_message", Role: "user", Body: "first", ActivityID: "activity-a"},
		{ID: "output-a", Seq: 2, Kind: "assistant_message", Role: "assistant", Body: "first done", ActivityID: "activity-a"},
		{ID: "input-b", Seq: 3, Kind: "user_message", Role: "user", Body: "second", ActivityID: "activity-b"},
		{ID: "output-b", Seq: 4, Kind: "assistant_message", Role: "assistant", Body: "second done", ActivityID: "activity-b"},
	}
	snapshot := projection.project("agent:history", work.CodexConversation{
		Available: true, SessionID: "history-session",
		Turn: &turns[1], ProviderTurns: turns, Events: events,
	})
	ids := make([]string, 0, len(snapshot.Conversation.Events))
	for _, event := range snapshot.Conversation.Events {
		ids = append(ids, event.ID)
	}
	if got := fmt.Sprint(ids); got != "[input-a output-a input-b output-b]" {
		t.Fatalf("sequential Activity history = %s", got)
	}
}

func TestCanonicalCodexProjectionZeroEffectActivityCannotBlockNextActivity(t *testing.T) {
	projection := newCanonicalCodexProjection()
	scope := "agent:zero-effect"
	started := time.Date(2026, 7, 16, 5, 30, 0, 0, time.UTC)

	terminal := projection.project(scope, work.CodexConversation{
		Available: true, SessionID: "zero-effect-session",
		Turn: &work.CodexConversationTurn{
			ID: "activity-empty", Status: work.CodexConversationTurnCancelled,
			StartedAt: started.Format(time.RFC3339Nano),
			SettledAt: started.Add(time.Second).Format(time.RFC3339Nano),
		},
		Events: []work.CodexConversationEvent{},
	})
	if terminal.Conversation.Activity == nil ||
		terminal.Conversation.Activity.Status != work.CodexConversationTurnCancelled {
		t.Fatalf("zero-effect terminal Activity = %#v", terminal.Conversation.Activity)
	}

	next := projection.project(scope, work.CodexConversation{
		Available: true, SessionID: "zero-effect-session",
		Turn: &work.CodexConversationTurn{
			ID: "activity-next", Status: work.CodexConversationTurnRunning,
			StartedAt: started.Add(2 * time.Second).Format(time.RFC3339Nano),
		},
		Events: []work.CodexConversationEvent{
			{ID: "next-user", Seq: 1, Kind: "user_message", Role: "user", Body: "next", ActivityID: "activity-next"},
			{ID: "next-output", Seq: 2, Kind: "assistant_message", Role: "assistant", Body: "working", ActivityID: "activity-next"},
		},
	})
	if next.Conversation.Activity == nil ||
		next.Conversation.Activity.ID != "activity-next" ||
		next.Conversation.Activity.Status != work.CodexConversationTurnRunning {
		t.Fatalf("next Activity was blocked: %#v", next.Conversation.Activity)
	}
	if len(next.Conversation.Events) != 2 {
		t.Fatalf("next Activity event count = %d, events=%#v", len(next.Conversation.Events), next.Conversation.Events)
	}
	if got := fmt.Sprint([]string{
		next.Conversation.Events[0].ID,
		next.Conversation.Events[1].ID,
	}); got != "[next-user next-output]" {
		t.Fatalf("next Activity events = %s", got)
	}
}
