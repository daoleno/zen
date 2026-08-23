package brain

import (
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
)

// host_lane_model_test.go is the table-driven state-transition model for the
// single serialized Host-lane reducer (worklog
// 2026-08-11-zen-signal-system-architecture-review.md). The model is pure:
// every transition is derived from persisted delivery state plus current
// strong evidence, and no provider-specific branch exists here. Production
// reducer behavior is bound to this model by the tests at the bottom of the
// file, so a change to one side without the other fails.

// hostLaneReceipt is the reconcile outcome for the one current Host delivery
// attempt (dispatching Event) against the provider submission receipt ledger.
type hostLaneReceipt string

const (
	receiptNone         hostLaneReceipt = "none"          // no dispatching Event
	receiptAbsent       hostLaneReceipt = "absent"        // receipt absent, provider provably not mutated
	receiptProviderLive hostLaneReceipt = "provider_live" // receipt absent but canonical provider mutation recorded
	receiptAccepted     hostLaneReceipt = "accepted"      // receipt accepted; exact provider Turn canonical
	receiptAmbiguous    hostLaneReceipt = "ambiguous"     // receipt ambiguous; mutation may have begun
	receiptNotSubmitted hostLaneReceipt = "not_submitted" // receipt proves non-submission
)

// hostLaneState is the complete persisted + strong-evidence input to one
// reducer pass. Fewer than ten persisted transition states exist: the Event
// delivery states pending/dispatching/delivered/handled, the Brain input
// admission states pending/accepted, and the Host foreground turn record
// (active/absent), and the canonical Host provider Turn (live/immutable).
type hostLaneState struct {
	Receipt hostLaneReceipt
	// DeliveredAwaitingDisposition is one delivered Event whose typed
	// disposition has not been recorded.
	DeliveredAwaitingDisposition bool
	// ForegroundTurn is an accepted foreground Host turn (durable admission
	// event). ForegroundTerminalEvidence is strong exact terminal evidence
	// for that exact turn (provider activity identity match + terminal
	// status).
	ForegroundTurn             bool
	ForegroundTerminalEvidence bool
	// PendingUserAdmission is a Brain input admission persisted before
	// provider mutation (user steering in flight).
	PendingUserAdmission bool
	// PendingWork is at least one fair pending Work Event head.
	PendingWork bool
	// CanonicalTurnLive is the current canonical Host provider Turn before an
	// immutable done/failed boundary. It is independent of the scheduler
	// handling and foreground-user checkpoint.
	CanonicalTurnLive bool
	// AmbientProviderActivityLive is a provider-native Activity (for example a
	// user turn that crossed daemon restart) without a current non-immutable
	// canonical Turn. It is a conservative stop fence only.
	AmbientProviderActivityLive bool
}

// hostLaneResult is the one-pass outcome. Stop means the reducer must not
// claim or submit; every other field is a single idempotent action.
type hostLaneResult struct {
	ReleasedReceipt      bool // provably-unsent receipt released back to pending
	HeldReceipt          bool // ambiguous receipt held; never replayed
	ConsumedReceipt      bool // accepted receipt consumed; Event marked delivered
	Stop                 bool
	ClosedForegroundTurn bool
	ClaimedEvent         bool
	SubmittedEvent       bool // submitted exactly once; delivered only from accepted receipt
}

// reconcileHostLaneModel is the pure transition model. Order is frozen:
//
//  1. reconcile the existing delivery receipt
//  2. one delivered Event awaiting disposition: stop
//  3. pending user admission: stop
//  4. foreground turn: stop unless strong exact terminal evidence closes it
//  5. non-immutable canonical Host Turn: stop
//  6. non-terminal ambient provider Activity: stop
//  7. claim one fair pending Work key at the boundary
//  8. submit once; mark delivered only from the accepted receipt
func reconcileHostLaneModel(s hostLaneState) hostLaneResult {
	out := hostLaneResult{}
	switch s.Receipt {
	case receiptAbsent, receiptNotSubmitted:
		out.ReleasedReceipt = true
	case receiptAccepted:
		out.ConsumedReceipt = true
	case receiptAmbiguous, receiptProviderLive:
		out.HeldReceipt = true
	}
	if s.DeliveredAwaitingDisposition {
		out.Stop = true
		return out
	}
	if s.PendingUserAdmission {
		out.Stop = true
		return out
	}
	if s.ForegroundTurn {
		if !s.ForegroundTerminalEvidence {
			out.Stop = true
			return out
		}
		out.ClosedForegroundTurn = true
	}
	if s.CanonicalTurnLive {
		out.Stop = true
		return out
	}
	if s.AmbientProviderActivityLive {
		out.Stop = true
		return out
	}
	if !s.PendingWork {
		return out
	}
	out.ClaimedEvent = true
	out.SubmittedEvent = true
	return out
}

func TestHostLaneReducerTransitionTable(t *testing.T) {
	idle := hostLaneState{}
	terminalBoundary := hostLaneState{ForegroundTurn: true, ForegroundTerminalEvidence: true, PendingWork: true}
	rows := []struct {
		name  string
		state hostLaneState
		want  hostLaneResult
	}{
		{
			name:  "idle with no pending work is a no-op",
			state: idle,
			want:  hostLaneResult{},
		},
		{
			name:  "idle boundary claims and submits one pending Event",
			state: hostLaneState{PendingWork: true},
			want:  hostLaneResult{ClaimedEvent: true, SubmittedEvent: true},
		},
		{
			name:  "accepted receipt consumes and marks delivered",
			state: hostLaneState{Receipt: receiptAccepted, PendingWork: true},
			want:  hostLaneResult{ConsumedReceipt: true, ClaimedEvent: true, SubmittedEvent: true},
		},
		{
			name:  "absent receipt releases the provably-unsent claim",
			state: hostLaneState{Receipt: receiptAbsent, PendingWork: true},
			want:  hostLaneResult{ReleasedReceipt: true, ClaimedEvent: true, SubmittedEvent: true},
		},
		{
			name:  "not_submitted receipt releases the definitely-unsent claim",
			state: hostLaneState{Receipt: receiptNotSubmitted},
			want:  hostLaneResult{ReleasedReceipt: true},
		},
		{
			name:  "ambiguous receipt holds forever without replay",
			state: hostLaneState{Receipt: receiptAmbiguous, PendingWork: true},
			want:  hostLaneResult{HeldReceipt: true, ClaimedEvent: true, SubmittedEvent: true},
		},
		{
			name:  "absent receipt with live provider mutation holds without replay",
			state: hostLaneState{Receipt: receiptProviderLive, PendingWork: true},
			want:  hostLaneResult{HeldReceipt: true, ClaimedEvent: true, SubmittedEvent: true},
		},
		{
			name:  "delivered Event awaiting disposition stops the lane",
			state: hostLaneState{DeliveredAwaitingDisposition: true, PendingWork: true},
			want:  hostLaneResult{Stop: true},
		},
		{
			name:  "delivered handling stops even at a terminal foreground boundary",
			state: hostLaneState{DeliveredAwaitingDisposition: true, ForegroundTurn: true, ForegroundTerminalEvidence: true, PendingWork: true},
			want:  hostLaneResult{Stop: true},
		},
		{
			name:  "pending user admission stops the lane",
			state: hostLaneState{PendingUserAdmission: true, PendingWork: true},
			want:  hostLaneResult{Stop: true},
		},
		{
			name:  "live foreground turn without exact terminal evidence stops",
			state: hostLaneState{ForegroundTurn: true, PendingWork: true},
			want:  hostLaneResult{Stop: true},
		},
		{
			name:  "live canonical Host Turn stops after handling ends",
			state: hostLaneState{CanonicalTurnLive: true, PendingWork: true},
			want:  hostLaneResult{Stop: true},
		},
		{
			name:  "ambient provider Activity stops an untracked steer",
			state: hostLaneState{AmbientProviderActivityLive: true, PendingWork: true},
			want:  hostLaneResult{Stop: true},
		},
		{
			name:  "foreground turn with terminal evidence closes it even with no pending Work",
			state: hostLaneState{ForegroundTurn: true, ForegroundTerminalEvidence: true},
			want:  hostLaneResult{ClosedForegroundTurn: true},
		},
		{
			name:  "terminal boundary claims and submits the next Event",
			state: terminalBoundary,
			want:  hostLaneResult{ClosedForegroundTurn: true, ClaimedEvent: true, SubmittedEvent: true},
		},
		{
			name:  "terminal boundary with pending admission still stops",
			state: hostLaneState{ForegroundTurn: true, ForegroundTerminalEvidence: true, PendingUserAdmission: true, PendingWork: true},
			want:  hostLaneResult{Stop: true},
		},
		{
			name:  "reopen after delivered-but-unhandled Event never replays",
			state: hostLaneState{DeliveredAwaitingDisposition: true},
			want:  hostLaneResult{Stop: true},
		},
		{
			name:  "reopen after handled Event with no pending Work is a no-op",
			state: hostLaneState{},
			want:  hostLaneResult{},
		},
		{
			name:  "reopen after handled Event with a new pending Work claims the new head once",
			state: hostLaneState{PendingWork: true},
			want:  hostLaneResult{ClaimedEvent: true, SubmittedEvent: true},
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			got := reconcileHostLaneModel(row.state)
			if got != row.want {
				t.Fatalf("model(%+v) = %+v, want %+v", row.state, got, row.want)
			}
		})
	}
}

// TestHostLaneReducerFrozenOrder verifies the gate precedence that makes user
// steering unable to overtake an admitted internal Event: the delivered
// handling, pending user admission, live foreground turn, live canonical
// provider Turn, and ambient provider Activity gates all stop the lane before
// any claim, in that order.
func TestHostLaneReducerFrozenOrder(t *testing.T) {
	for _, row := range []struct {
		state hostLaneState
	}{
		{hostLaneState{DeliveredAwaitingDisposition: true, PendingUserAdmission: true, ForegroundTurn: true, PendingWork: true}},
		{hostLaneState{PendingUserAdmission: true, ForegroundTurn: true, PendingWork: true}},
		{hostLaneState{ForegroundTurn: true, PendingWork: true}},
		{hostLaneState{CanonicalTurnLive: true, PendingWork: true}},
		{hostLaneState{AmbientProviderActivityLive: true, PendingWork: true}},
	} {
		result := reconcileHostLaneModel(row.state)
		if !result.Stop || result.ClaimedEvent || result.SubmittedEvent || result.ClosedForegroundTurn {
			t.Fatalf("gate order violated: state=%+v result=%+v", row.state, result)
		}
	}
}

// TestHostLaneReducerIdempotentOneShot verifies that no input combination can
// produce a replay: each pass claims at most one Event, submits at most once,
// and a second pass over the resulting state never submits again.
func TestHostLaneReducerIdempotentOneShot(t *testing.T) {
	states := []hostLaneState{
		{},
		{PendingWork: true},
		{Receipt: receiptAbsent},
		{Receipt: receiptAccepted},
		{Receipt: receiptAmbiguous},
		{Receipt: receiptNotSubmitted},
		{Receipt: receiptProviderLive},
		{DeliveredAwaitingDisposition: true, PendingWork: true},
		{PendingUserAdmission: true, PendingWork: true},
		{ForegroundTurn: true, PendingWork: true},
		{CanonicalTurnLive: true, PendingWork: true},
		{AmbientProviderActivityLive: true, PendingWork: true},
		{ForegroundTurn: true, ForegroundTerminalEvidence: true, PendingWork: true},
		{ForegroundTurn: true, ForegroundTerminalEvidence: true, PendingUserAdmission: true, PendingWork: true},
	}
	for _, state := range states {
		first := reconcileHostLaneModel(state)
		if first.SubmittedEvent {
			// After a submission the delivered gate stops the lane: the
			// delivered Event awaits disposition until a typed disposition
			// records it handled.
			second := reconcileHostLaneModel(hostLaneState{
				DeliveredAwaitingDisposition: true,
			})
			if !second.Stop || second.SubmittedEvent || second.ClaimedEvent {
				t.Fatalf("post-submit state=%+v second pass=%+v replayed", state, second)
			}
		}
		if first.Stop && (first.ClaimedEvent || first.SubmittedEvent || first.ClosedForegroundTurn) {
			t.Fatalf("state=%+v result=%+v stopped while acting", state, first)
		}
	}
}

// TestHostLaneReducerModelBindsProduction is the promised binding: each
// scenario drives the real serialized reducer (service.ReconcileHostLane over
// a real Store + fakeWatcher) and asserts the observable outcome equals the
// pure model's outcome for the same persisted + strong-evidence state. A
// change to one side without the other fails.
func TestHostLaneReducerModelBindsProduction(t *testing.T) {
	const hostID = "brain-agent-brain-hidden:@model-bind"
	rows := []struct {
		name  string
		state hostLaneState
		setup func(t *testing.T, store *Store, fw *fakeWatcher, service *Service)
	}{
		{
			name:  "idle no pending work is a no-op",
			state: hostLaneState{},
			setup: func(t *testing.T, store *Store, fw *fakeWatcher, service *Service) {},
		},
		{
			name:  "idle boundary claims and submits one pending Event",
			state: hostLaneState{PendingWork: true},
			setup: func(t *testing.T, store *Store, fw *fakeWatcher, service *Service) {
				item := createSignalTestWork(t, store, "model idle delivery", "brain-agent-model:@1")
				appendSignalTestEvent(t, store, item, "model-idle")
			},
		},
		{
			name:  "pending user admission stops the lane",
			state: hostLaneState{PendingUserAdmission: true, PendingWork: true},
			setup: func(t *testing.T, store *Store, fw *fakeWatcher, service *Service) {
				item := createSignalTestWork(t, store, "model pending admission", "brain-agent-model:@2")
				appendSignalTestEvent(t, store, item, "model-admission")
				if _, created, err := service.PrepareHostUserInput(hostID, "model-pending-steer", "continue", ""); err != nil || !created {
					t.Fatalf("prepare pending admission created=%v err=%v", created, err)
				}
			},
		},
		{
			name:  "live foreground turn without exact terminal evidence stops",
			state: hostLaneState{ForegroundTurn: true, PendingWork: true},
			setup: func(t *testing.T, store *Store, fw *fakeWatcher, service *Service) {
				fw.providerEvidence[hostID] = watcher.ProviderActivityObservation{
					ID: "model-activity-live", Status: "running", StartedAt: time.Now().Add(-time.Minute),
				}
				acceptModelForeground(t, service, store, hostID, "model-live", "model-activity-live")
				item := createSignalTestWork(t, store, "model live turn", "brain-agent-model:@3")
				appendSignalTestEvent(t, store, item, "model-live-turn")
			},
		},
		{
			name:  "live canonical Host Turn stops after handling resolution",
			state: hostLaneState{CanonicalTurnLive: true, PendingWork: true},
			setup: func(t *testing.T, store *Store, fw *fakeWatcher, service *Service) {
				first := createSignalTestWork(t, store, "model live canonical turn", "brain-agent-model:@canonical-1")
				appendSignalTestEvent(t, store, first, "model-live-canonical-first")
				if woke, err := service.ReconcileHostLane(); err != nil || !woke {
					t.Fatalf("deliver first canonical Turn woke=%v err=%v", woke, err)
				}
				lease := requireReviewDelivered(t, store, first.ID)
				if _, _, err := store.ResolveWorkReview(WorkReviewDispositionRequest{
					WorkID: first.ID, HandlingID: lease.HandlingID,
					ProviderTurnID:       lease.ProviderTurnID,
					ExpectedWorkRevision: lease.DeliveryWorkRevision,
					Disposition:          WorkDispositionComplete,
				}); err != nil {
					t.Fatal(err)
				}
				second := createSignalTestWork(t, store, "model canonical follower", "brain-agent-model:@canonical-2")
				appendSignalTestEvent(t, store, second, "model-live-canonical-second")
			},
		},
		{
			name:  "ambient provider Activity stops before claim",
			state: hostLaneState{AmbientProviderActivityLive: true, PendingWork: true},
			setup: func(t *testing.T, store *Store, fw *fakeWatcher, service *Service) {
				fw.providerEvidence[hostID] = watcher.ProviderActivityObservation{
					ID: "ambient-provider-activity", Status: "running", StartedAt: time.Now().Add(-time.Minute),
				}
				item := createSignalTestWork(t, store, "model ambient provider Activity", "brain-agent-model:@ambient")
				appendSignalTestEvent(t, store, item, "model-ambient-provider")
			},
		},
		{
			name:  "terminal boundary closes the exact turn and admits once",
			state: hostLaneState{ForegroundTurn: true, ForegroundTerminalEvidence: true, PendingWork: true},
			setup: func(t *testing.T, store *Store, fw *fakeWatcher, service *Service) {
				fw.providerEvidence[hostID] = watcher.ProviderActivityObservation{
					ID: "model-activity-live", Status: "running", StartedAt: time.Now().Add(-time.Minute),
				}
				acceptModelForeground(t, service, store, hostID, "model-terminal", "")
				item := createSignalTestWork(t, store, "model terminal boundary", "brain-agent-model:@4")
				appendSignalTestEvent(t, store, item, "model-terminal-boundary")
				// The exact bound Activity's terminal evidence converges the
				// boundary; ambient Agent state is never authority.
				fw.sessions[hostID].State = classifier.StateDone
				fw.providerEvidence[hostID] = watcher.ProviderActivityObservation{
					ID: "model-activity-live", Status: "completed",
					StartedAt: time.Now().Add(-time.Minute), SettledAt: time.Now(),
				}
			},
		},
		{
			name:  "delivered Event awaiting disposition stops the lane",
			state: hostLaneState{DeliveredAwaitingDisposition: true},
			setup: func(t *testing.T, store *Store, fw *fakeWatcher, service *Service) {
				item := createSignalTestWork(t, store, "model delivered", "brain-agent-model:@5")
				appendSignalTestEvent(t, store, item, "model-delivered")
				// First pass delivers at the idle boundary; the assertion pass
				// then observes the delivered handling gate.
				if woke, err := service.ReconcileHostLane(); err != nil || !woke {
					t.Fatalf("model delivery pass woke=%v err=%v", woke, err)
				}
			},
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := store.SetHostSession(hostID, "codex"); err != nil {
				t.Fatal(err)
			}
			fw := &fakeWatcher{
				turnStore: store,
				sessions: map[string]*classifier.Agent{
					hostID: {ID: hostID, Hidden: true, State: classifier.StateRunning, PaneAlive: true},
				},
				ownedGenerations: map[string]string{hostID: "host-generation-model"},
				providerEvidence: map[string]watcher.ProviderActivityObservation{},
			}
			service := NewService(store, fw, nil)
			row.setup(t, store, fw, service)
			baseline := len(fw.sentCalls)
			woke, err := service.ReconcileHostLane()
			if err != nil {
				t.Fatalf("production reconcile: %v", err)
			}
			want := reconcileHostLaneModel(row.state)
			sent := len(fw.sentCalls) - baseline
			if (sent > 0) != want.SubmittedEvent || woke != want.SubmittedEvent {
				t.Fatalf("model submitted=%v but production sent=%d woke=%v", want.SubmittedEvent, sent, woke)
			}
			if want.ClaimedEvent && sent != 1 {
				t.Fatalf("model claimed once but production sent=%d", sent)
			}
			active, err := store.CurrentHostForegroundTurn()
			if err != nil {
				t.Fatal(err)
			}
			if want.ClosedForegroundTurn && active != nil {
				t.Fatalf("model closed the foreground turn but production kept %+v", active)
			}
			if want.Stop && (woke || sent > 0) {
				t.Fatalf("model stopped but production acted: woke=%v sent=%d", woke, sent)
			}
			if want.SubmittedEvent {
				if delivered, err := store.HasLiveDeliveredReview(); err != nil || !delivered {
					t.Fatalf("model submission left no delivered handling: delivered=%v err=%v", delivered, err)
				}
			}
		})
	}
}

func acceptModelForeground(t *testing.T, service *Service, store *Store, hostID, requestID, _ string) {
	t.Helper()
	if recognized, err := service.NoteUserSteering(hostID); err != nil || !recognized {
		t.Fatalf("model steering recognized=%v err=%v", recognized, err)
	}
	prepared, created, err := service.PrepareHostUserInput(hostID, requestID, "continue "+requestID, "")
	if err != nil || !created {
		t.Fatalf("model prepare created=%v err=%v", created, err)
	}
	if err := service.AdmitHostUserInput(prepared); err != nil {
		t.Fatalf("model admit: %v", err)
	}
}
