package watcher

import (
	"errors"
	"testing"
	"time"
)

func TestBrainHostIdleAdmissionDoesNotConsultHistoricalTurn(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	identity := testSessionInputIdentity("codex")
	owner := newLedgerSessionInputOwner(io, ledger)
	acceptedAt := time.Date(2026, 8, 23, 1, 0, 0, 0, time.UTC)
	ledger.seed("brain-host:@1", TurnSnapshot{
		SessionID: "brain-host:@1", TurnID: "turn-lost", Status: TurnUnknown,
		AcceptedAt: acceptedAt, ProcessIdentity: delegatedTurnIdentity(identity),
		PaneGeneration: io.paneValue.generation,
	})
	draft := delegatedTurnDraft{
		WorkID: "work-complete", ID: "turn-fresh", Receipt: "event-canonical",
		AcceptedAt: acceptedAt.Add(time.Minute), ProcessIdentity: delegatedTurnIdentity(identity),
	}
	confirmer := scriptedActivityTransitionAdmission(
		"canonical event payload", ProviderActivityObservation{}, "activity-fresh",
	)
	result, err := owner.submitHost(
		"brain-host:@1", identity, fixedSessionInputResolver(identity), identity.Command,
		"canonical event payload", draft, confirmer,
	)
	if err != nil || result.Outcome != InputAccepted || result.TurnID != draft.ID || len(io.queues) != 1 {
		t.Fatalf("Host recovery submit=(%+v,%v) queues=%d", result, err, len(io.queues))
	}
	for _, fact := range ledger.applied {
		if fact.TurnID == "turn-lost" {
			t.Fatalf("historical lost Turn was replayed or rewritten: %+v", fact)
		}
	}

	delegatedIO := newFakeSessionInputIO()
	delegatedLedger := newFakeTurnLedger()
	delegatedLedger.seed("agent:@1", TurnSnapshot{
		SessionID: "agent:@1", TurnID: "delegated-lost", Status: TurnUnknown,
		ControlState: TurnControlOwnershipLost, AcceptedAt: acceptedAt,
	})
	_, delegatedErr := newLedgerSessionInputOwner(delegatedIO, delegatedLedger).submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity), identity.Command, "follow-up",
		delegatedTurnDraft{ID: "delegated-next", AcceptedAt: acceptedAt.Add(time.Minute), ProcessIdentity: delegatedTurnIdentity(identity)},
		scriptedActivityTransitionAdmission("follow-up", ProviderActivityObservation{}, "delegated-fresh"),
	)
	if delegatedErr == nil || len(delegatedIO.queues) != 0 {
		t.Fatalf("delegated ownership loss did not remain fail-closed: err=%v queues=%d", delegatedErr, len(delegatedIO.queues))
	}
}

func TestBrainHostAdmissionFailsClosedOnCurrentLiveProviderActivity(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	identity := testSessionInputIdentity("codex")
	owner := newLedgerSessionInputOwner(io, ledger)
	acceptedAt := time.Date(2026, 8, 23, 1, 30, 0, 0, time.UTC)
	ledger.seed("brain-host:@live", TurnSnapshot{
		SessionID: "brain-host:@live", TurnID: "turn-historical", Status: TurnUnknown,
		AcceptedAt: acceptedAt, ProcessIdentity: delegatedTurnIdentity(identity),
		PaneGeneration: io.paneValue.generation,
	})
	draft := delegatedTurnDraft{
		WorkID: "work-pending", ID: "turn-fresh", Receipt: "event-pending",
		AcceptedAt: acceptedAt.Add(time.Minute), ProcessIdentity: delegatedTurnIdentity(identity),
	}
	result, err := owner.submitHost(
		"brain-host:@live", identity, fixedSessionInputResolver(identity), identity.Command,
		"must remain queued", draft,
		scriptedActivityTransitionAdmission(
			"must remain queued",
			ProviderActivityObservation{ID: "activity-current", Status: "running", StartedAt: acceptedAt},
			"activity-fresh",
		),
	)
	if err == nil || result.Outcome != InputNotSubmitted || len(io.queues) != 0 || len(io.submissions) != 0 {
		t.Fatalf("live current Activity result=%+v err=%v queues=%d submissions=%d",
			result, err, len(io.queues), len(io.submissions))
	}
	turn, found, readErr := ledger.Turn("brain-host:@live")
	if readErr != nil || !found || turn.TurnID != "turn-historical" || turn.Status != TurnUnknown {
		t.Fatalf("live Activity rewrote historical Turn: found=%v turn=%+v err=%v", found, turn, readErr)
	}
}

func TestReviewAuthorizedReuseUnknownRetryNeverReplays(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	identity := testSessionInputIdentity("codex")
	owner := newLedgerSessionInputOwner(io, ledger)
	acceptedAt := time.Date(2026, 8, 23, 2, 0, 0, 0, time.UTC)
	settledAt := acceptedAt.Add(time.Minute)
	ledger.seed("agent:@500", TurnSnapshot{
		SessionID: "agent:@500", TurnID: "turn-stage-one", Status: TurnDone,
		AcceptedAt: acceptedAt, SettledAt: &settledAt, ActivityID: "activity-stage-one",
	})
	draft := delegatedTurnDraft{
		WorkID: "work-reviewed", ID: "turn:reviewed-next", AcceptedAt: settledAt.Add(time.Second),
		ProcessIdentity: delegatedTurnIdentity(identity), SignalProtocol: true,
		Purpose: "review", PurposeID: "handling-exact",
	}
	unknown := delegatedInputConfirmer{
		baseline: func() (delegatedInputBaseline, error) {
			return delegatedInputBaseline{Provider: ProviderActivityObservation{
				ID: "activity-stage-one", Status: "completed", StartedAt: acceptedAt, SettledAt: settledAt,
			}}, nil
		},
		confirm: func(delegatedAdmissionEvidence, time.Time, string) (delegatedInputConfirmation, error) {
			return delegatedInputConfirmation{Outcome: InputAmbiguous}, errors.New("provider admission outcome unknown")
		},
	}
	first, err := owner.submitDelegated(
		"agent:@500", identity, fixedSessionInputResolver(identity), identity.Command,
		"reviewed follow-up", draft, unknown,
	)
	if err == nil || first.Outcome != InputAmbiguous || len(io.queues) != 1 {
		t.Fatalf("first unknown result=(%+v,%v) queues=%d", first, err, len(io.queues))
	}
	second, retryErr := owner.submitDelegated(
		"agent:@500", identity, fixedSessionInputResolver(identity), identity.Command,
		"reviewed follow-up", draft, unknown,
	)
	if retryErr == nil || second.Outcome != InputAmbiguous || !second.Duplicate || len(io.queues) != 1 {
		t.Fatalf("unknown retry=(%+v,%v) queues=%d", second, retryErr, len(io.queues))
	}
}

func TestReviewAuthorizedReuseAcceptedDuplicateReturnsOriginalTurnWithoutReplay(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	identity := testSessionInputIdentity("codex")
	owner := newLedgerSessionInputOwner(io, ledger)
	acceptedAt := time.Date(2026, 8, 23, 2, 30, 0, 0, time.UTC)
	settledAt := acceptedAt.Add(time.Minute)
	ledger.seed("agent:@500", TurnSnapshot{
		SessionID: "agent:@500", TurnID: "turn-stage-one", Status: TurnDone,
		AcceptedAt: acceptedAt, SettledAt: &settledAt, ActivityID: "activity-stage-one",
	})
	draft := delegatedTurnDraft{
		WorkID: "work-reviewed", ID: "turn:reviewed-next", Receipt: "turn:reviewed-next",
		AcceptedAt: settledAt.Add(time.Second), ProcessIdentity: delegatedTurnIdentity(identity),
		SignalProtocol: true, Purpose: "review", PurposeID: "handling-exact",
	}
	confirmer := scriptedActivityTransitionAdmission(
		"reviewed follow-up", ProviderActivityObservation{
			ID: "activity-stage-one", Status: "completed", StartedAt: acceptedAt, SettledAt: settledAt,
		}, "activity-stage-two",
	)
	first, err := owner.submitDelegated(
		"agent:@500", identity, fixedSessionInputResolver(identity), identity.Command,
		"reviewed follow-up", draft, confirmer,
	)
	if err != nil || first.Outcome != InputAccepted || first.Duplicate || first.TurnID != draft.ID || len(io.queues) != 1 {
		t.Fatalf("first review submit=(%+v,%v) queues=%d", first, err, len(io.queues))
	}
	duplicate, err := owner.submitDelegated(
		"agent:@500", identity, fixedSessionInputResolver(identity), identity.Command,
		"reviewed follow-up", draft, confirmer,
	)
	if err != nil || duplicate.Outcome != InputAccepted || !duplicate.Duplicate ||
		duplicate.TurnID != draft.ID || len(io.queues) != 1 {
		t.Fatalf("duplicate review submit=(%+v,%v) queues=%d", duplicate, err, len(io.queues))
	}
}

func TestReviewAuthorizedReuseDefiniteAbortRearmsSameTurn(t *testing.T) {
	io := newFakeSessionInputIO()
	io.runErr = errors.New("tmux queue did not start")
	io.runStarted = false
	ledger := newFakeTurnLedger()
	identity := testSessionInputIdentity("codex")
	owner := newLedgerSessionInputOwner(io, ledger)
	acceptedAt := time.Date(2026, 8, 23, 3, 0, 0, 0, time.UTC)
	settledAt := acceptedAt.Add(time.Minute)
	ledger.seed("agent:@500", TurnSnapshot{
		SessionID: "agent:@500", TurnID: "turn-stage-one", Status: TurnDone,
		AcceptedAt: acceptedAt, SettledAt: &settledAt, ActivityID: "activity-stage-one",
	})
	draft := delegatedTurnDraft{
		WorkID: "work-reviewed", ID: "turn:reviewed-retry", AcceptedAt: settledAt.Add(time.Second),
		ProcessIdentity: delegatedTurnIdentity(identity), SignalProtocol: true,
		Purpose: "review", PurposeID: "handling-exact",
	}
	confirmer := scriptedActivityTransitionAdmission(
		"reviewed follow-up", ProviderActivityObservation{
			ID: "activity-stage-one", Status: "completed", StartedAt: acceptedAt, SettledAt: settledAt,
		}, "activity-stage-two",
	)
	first, err := owner.submitDelegated(
		"agent:@500", identity, fixedSessionInputResolver(identity), identity.Command,
		"reviewed follow-up", draft, confirmer,
	)
	if err == nil || first.Outcome != InputNotSubmitted || io.startedQueues != 0 {
		t.Fatalf("definite abort=(%+v,%v) started=%d", first, err, io.startedQueues)
	}
	if aborted, found, _ := ledger.InputAdmission("agent:@500", draft.ID); !found || aborted.State != InputAdmissionAborted {
		t.Fatalf("aborted admission found=%v admission=%+v", found, aborted)
	}
	io.runErr = nil
	second, retryErr := owner.submitDelegated(
		"agent:@500", identity, fixedSessionInputResolver(identity), identity.Command,
		"reviewed follow-up", draft, confirmer,
	)
	if retryErr != nil || second.Outcome != InputAccepted || second.TurnID != draft.ID || io.startedQueues != 1 {
		t.Fatalf("same-turn retry=(%+v,%v) started=%d", second, retryErr, io.startedQueues)
	}
	resolved, found, _ := ledger.InputAdmission("agent:@500", draft.ID)
	if !found || resolved.State != InputAdmissionResolved {
		t.Fatalf("resolved retry found=%v admission=%+v", found, resolved)
	}
}
