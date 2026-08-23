package lifecycle

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Engine is the single serial authority for Work lifecycle state. Every
// mutation is a command: validate against reduced state, append events to the
// canonical log, apply them. Modules never write state directly (I1).
type Engine struct {
	root string

	mu      sync.Mutex
	works   map[WorkID]*workActor
	events  []Event
	nextSeq uint64

	nowMu sync.Mutex
	now   func() time.Time
	wake  chan struct{}
}

// SetNow replaces the engine clock. Callers bind a shared clock so supervisor
// decisions and event timestamps stay consistent with the embedding process.
func (e *Engine) SetNow(fn func() time.Time) {
	if fn == nil {
		return
	}
	e.nowMu.Lock()
	e.now = fn
	e.nowMu.Unlock()
}

func (e *Engine) nowUTC() time.Time {
	e.nowMu.Lock()
	fn := e.now
	e.nowMu.Unlock()
	if fn == nil {
		return time.Now()
	}
	return fn()
}

// workActor owns one aggregate's lock and reduced state.
type workActor struct {
	st *State
}

// Open loads the one canonical transaction image. There is no legacy
// directory, snapshot fallback, migration, or second log to replay.
func Open(root string) (*Engine, error) {
	database, err := readLifecycleDatabase(root + "/state.json")
	if err != nil {
		return nil, err
	}
	e := &Engine{
		root: root, works: map[WorkID]*workActor{}, events: database.Events,
		nextSeq: database.NextSeq, now: time.Now, wake: make(chan struct{}, 1),
	}
	for id, state := range database.Works {
		if state == nil || state.ID != WorkID(id) {
			return nil, fmt.Errorf("lifecycle: Work row %q has mismatched identity", id)
		}
		e.works[WorkID(id)] = &workActor{st: state}
	}
	return e, nil
}

// Close is retained for Store lifecycle symmetry. Every command is already
// durable before it returns.
func (e *Engine) Close() error {
	return nil
}

func (e *Engine) databaseLocked(states map[WorkID]*State, events []Event, nextSeq uint64) lifecycleDatabase {
	database := lifecycleDatabase{
		Schema: lifecycleStoreSchema, NextSeq: nextSeq,
		Works: make(map[string]*State, len(states)), Events: events,
	}
	for id, state := range states {
		database.Works[string(id)] = state
	}
	return database
}

func (e *Engine) statesLocked(replaceID WorkID, replacement *State) map[WorkID]*State {
	states := make(map[WorkID]*State, len(e.works)+1)
	for id, actor := range e.works {
		if id == replaceID {
			states[id] = replacement
		} else {
			states[id] = actor.st
		}
	}
	if _, exists := states[replaceID]; !exists && replacement != nil {
		states[replaceID] = replacement
	}
	return states
}

func (e *Engine) newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("lifecycle: id generation failed: %v", err))
	}
	return hex.EncodeToString(b[:])
}

func (e *Engine) dispatch(id WorkID, build func(st *State, now time.Time) ([]Event, error)) (*State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	a := e.works[id]
	if a == nil {
		return nil, ErrUnknownWork
	}

	events, err := build(a.st, e.nowUTC())
	if err != nil {
		return a.st.Clone(), err
	}
	events = unseenSourceEvents(a.st, events)
	if len(events) == 0 {
		return a.st.Clone(), nil
	}
	next := a.st.Clone()
	nextEvents := append([]Event(nil), e.events...)
	nextSeq := e.nextSeq
	for i := range events {
		events[i].Seq = nextSeq
		nextSeq++
		next = Reduce(next, events[i])
		nextEvents = append(nextEvents, events[i])
	}
	states := e.statesLocked(id, next)
	if err := writeLifecycleDatabase(e.root+"/state.json", e.databaseLocked(states, nextEvents, nextSeq)); err != nil {
		return a.st.Clone(), err
	}
	a.st, e.events, e.nextSeq = next, nextEvents, nextSeq
	e.signalWake()
	return a.st.Clone(), nil
}

// unseenSourceEvents is the canonical source-admission boundary. Commands are
// serialized under Engine.mu, so rejecting sources already committed to the
// current row (or admitted earlier in the same command) makes sequence
// allocation, event append, reduction, and revision advancement exactly-once.
func unseenSourceEvents(st *State, events []Event) []Event {
	if len(events) == 0 {
		return nil
	}
	admitted := make([]Event, 0, len(events))
	batchSources := make(map[string]bool, len(events))
	for _, event := range events {
		if event.SourceID != "" {
			if st.SeenSources[event.SourceID] || batchSources[event.SourceID] {
				continue
			}
			batchSources[event.SourceID] = true
		}
		admitted = append(admitted, event)
	}
	return admitted
}

func (e *Engine) signalWake() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// Wakeups is notified after every committed lifecycle transaction. Consumers
// recalculate NextWakeAt; notifications are deliberately coalesced.
func (e *Engine) Wakeups() <-chan struct{} { return e.wake }

// ---- commands ----

// DefineWorkInput seeds a new aggregate.
type DefineWorkInput struct {
	Title           string
	Objective       string
	Policy          Policy
	DoneCriteriaRef string
	SourceThreadID  string
}

// DefineWork creates a Work. The caller may pin an ID when an upstream durable
// identity already names the aggregate.
func (e *Engine) DefineWork(id WorkID, in DefineWorkInput) (*State, error) {
	if id == "" {
		id = WorkID("w" + e.newID())
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	_, exists := e.works[id]
	if exists {
		return nil, ErrWorkExists
	}
	events := []Event{{
		WorkID: id, Kind: KWorkDefined, SourceID: "defined",
		At: e.nowUTC(), Payload: DefinedPayload{
			Title: in.Title, Objective: in.Objective, Policy: in.Policy,
			DoneCriteriaRef: in.DoneCriteriaRef, SourceThreadID: in.SourceThreadID,
		}},
	}
	events[0].Seq = e.nextSeq
	next := Reduce(nil, events[0])
	nextEvents := append(append([]Event(nil), e.events...), events[0])
	states := e.statesLocked(id, next)
	if err := writeLifecycleDatabase(e.root+"/state.json", e.databaseLocked(states, nextEvents, e.nextSeq+1)); err != nil {
		return nil, err
	}
	e.works[id] = &workActor{st: next}
	e.events, e.nextSeq = nextEvents, e.nextSeq+1
	e.signalWake()
	return next.Clone(), nil
}

// AdmitTurnInput admits one provider turn as the active Attempt.
type AdmitTurnInput struct {
	SessionID  string
	Delegated  bool
	TurnToken  TurnToken
	FollowUpOf TurnToken
}

// PrepareAdmissionInput binds one exact payload to one Work, Session, provider
// process/pane generation, and proposed Turn before provider mutation begins.
type PrepareAdmissionInput struct {
	SessionID          string
	TurnToken          TurnToken
	Receipt            string
	ClaimToken         string
	PayloadSHA256      string
	ProcessIdentity    string
	PaneGeneration     string
	Mode               AdmissionMode
	ExistingTurnToken  TurnToken
	BaselineActivityID string
	SignalProtocol     bool
	AttemptedAt        time.Time
	TranscriptProvider string
	TranscriptFlag     string
	TranscriptPath     string
	Purpose            AdmissionPurpose
	PurposeID          string
}

// PrepareTerminalFollowUpInput authorizes an ordinary fresh follow-up after
// exact provider terminal evidence for the current Attempt. It is deliberately
// one command so no state can expose a settled predecessor without the new
// mutation admission, or vice versa.
type PrepareTerminalFollowUpInput struct {
	AttemptSessionID   string
	AttemptToken       TurnToken
	AttemptFence       uint64
	TerminalEvidenceID string
	Admission          PrepareAdmissionInput
}

func (e *Engine) PrepareTerminalFollowUp(id WorkID, in PrepareTerminalFollowUpInput) (*State, error) {
	a := in.Admission
	if in.AttemptSessionID == "" || in.AttemptToken == "" || in.AttemptFence == 0 || in.TerminalEvidenceID == "" ||
		a.SessionID == "" || a.TurnToken == "" || a.Receipt == "" || a.PayloadSHA256 == "" ||
		a.ProcessIdentity == "" || a.PaneGeneration == "" || a.AttemptedAt.IsZero() || a.Mode != AdmissionFresh ||
		a.ClaimToken != "" || a.Purpose != "" || a.PurposeID != "" {
		return nil, fmt.Errorf("%w: complete terminal follow-up identity required", ErrInvalidCommand)
	}
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		if terminal(st) {
			return nil, ErrTerminal
		}
		identity := AttemptIdentity{SessionID: in.AttemptSessionID, TurnToken: in.AttemptToken, Fence: in.AttemptFence}
		if !currentAttempt(st, identity) || in.AttemptSessionID != a.SessionID {
			return nil, ErrStaleInput
		}
		if st.ActiveAdmission() != nil || st.Admission(a.TurnToken) != nil {
			return nil, ErrAttemptActive
		}
		return []Event{{
			WorkID: id, Kind: KTurnRelinquished, TurnToken: in.AttemptToken, Fence: in.AttemptFence,
			SourceID: "terminal-follow-up-settle:" + in.TerminalEvidenceID, At: now,
			Payload: RelinquishedPayload{Reason: "exact terminal evidence before same-session follow-up"},
		}, {
			WorkID: id, Kind: KAdmissionPrepared, TurnToken: a.TurnToken,
			SourceID: "admission-prepare:" + string(a.TurnToken), At: now,
			Payload: AdmissionPreparedPayload{
				SessionID: a.SessionID, Receipt: a.Receipt, PayloadSHA256: a.PayloadSHA256,
				ProcessIdentity: a.ProcessIdentity, PaneGeneration: a.PaneGeneration, Mode: AdmissionFresh,
				ExistingTurnToken: in.AttemptToken, SignalProtocol: a.SignalProtocol, AttemptedAt: a.AttemptedAt,
				TranscriptProvider: a.TranscriptProvider, TranscriptFlag: a.TranscriptFlag, TranscriptPath: a.TranscriptPath,
			},
		}}, nil
	})
}

// PrepareTerminalReviewFollowUp is the pre-mutation half of a same-Session
// needs_input follow-up over an observed terminal provider phase. One batch
// settles the exact Attempt, resolves the exact actionable review capability,
// and prepares the next Attempt token. Exact acceptance later admits that token;
// ambiguous transport can never replay it.
type PrepareTerminalReviewFollowUpInput struct {
	AttemptSessionID   string
	AttemptToken       TurnToken
	AttemptFence       uint64
	TerminalEvidenceID string
	EventID            string
	HandlerID          string
	HandlerToken       TurnToken
	Admission          PrepareAdmissionInput
}

func (e *Engine) PrepareTerminalReviewFollowUp(id WorkID, in PrepareTerminalReviewFollowUpInput) (*State, error) {
	a := in.Admission
	if in.AttemptSessionID == "" || in.AttemptToken == "" || in.AttemptFence == 0 || in.TerminalEvidenceID == "" || in.EventID == "" ||
		in.HandlerID == "" || in.HandlerToken == "" || a.SessionID == "" ||
		a.TurnToken == "" || a.Receipt == "" || a.PayloadSHA256 == "" || a.ProcessIdentity == "" ||
		a.PaneGeneration == "" || a.AttemptedAt.IsZero() || a.Mode != AdmissionFresh || a.ClaimToken != "" {
		return nil, fmt.Errorf("%w: complete terminal review follow-up identity required", ErrInvalidCommand)
	}
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		if existing := st.Admission(a.TurnToken); existing != nil &&
			st.SeenSources["terminal-review-settle:"+in.TerminalEvidenceID] {
			if !admissionMatchesPrepare(existing, a, in.AttemptToken) {
				return nil, fmt.Errorf("%w: admission token belongs to different terminal follow-up", ErrInvalidCommand)
			}
			return nil, nil
		}
		identity := AttemptIdentity{SessionID: in.AttemptSessionID, TurnToken: in.AttemptToken, Fence: in.AttemptFence}
		if !currentAttempt(st, identity) || in.AttemptSessionID != a.SessionID {
			return nil, ErrStaleInput
		}
		if st.Review == nil || st.Review.EventID != in.EventID ||
			st.Review.Handler == nil || st.Review.Handler.HandlerID != in.HandlerID ||
			st.Review.Handler.HandlerToken != in.HandlerToken {
			return nil, ErrReviewLease
		}
		events := make([]Event, 0, 4)
		if active := st.ActiveAdmission(); active != nil {
			if active.TurnToken != in.HandlerToken || active.ClaimToken != in.HandlerID || active.Receipt != in.EventID {
				return nil, ErrAttemptActive
			}
			events = append(events, Event{
				WorkID: id, Kind: KAdmissionAborted, TurnToken: active.TurnToken,
				SourceID: "admission-abort:review-consumed:" + in.EventID, At: now,
				Payload: AdmissionAbortedPayload{Reason: "exact review handler consumed by terminal same-session follow-up"},
			})
		}
		if st.Admission(a.TurnToken) != nil {
			return nil, ErrAttemptActive
		}
		events = append(events, Event{
			WorkID: id, Kind: KTurnRelinquished, TurnToken: in.AttemptToken, Fence: in.AttemptFence,
			SourceID: "terminal-review-settle:" + in.TerminalEvidenceID, At: now,
			Payload: RelinquishedPayload{Reason: "exact terminal evidence before reviewed same-session follow-up"},
		}, Event{
			WorkID: id, Kind: KReviewResolved, SourceID: "resolve:" + in.EventID, At: now,
			Payload: ReviewResolvedPayload{EventID: in.EventID, Disposition: DispositionContinue, Actor: "session_input"},
		}, Event{
			WorkID: id, Kind: KAdmissionPrepared, TurnToken: a.TurnToken,
			SourceID: "admission-prepare:" + string(a.TurnToken), At: now,
			Payload: AdmissionPreparedPayload{
				SessionID: a.SessionID, Receipt: a.Receipt, PayloadSHA256: a.PayloadSHA256,
				ProcessIdentity: a.ProcessIdentity, PaneGeneration: a.PaneGeneration, Mode: AdmissionFresh,
				ExistingTurnToken: in.AttemptToken, SignalProtocol: a.SignalProtocol, AttemptedAt: a.AttemptedAt,
				TranscriptProvider: a.TranscriptProvider, TranscriptFlag: a.TranscriptFlag, TranscriptPath: a.TranscriptPath,
			},
		})
		return events, nil
	})
}

func admissionMatchesPrepare(existing *AdmissionState, in PrepareAdmissionInput, predecessor TurnToken) bool {
	return existing != nil && existing.SessionID == in.SessionID && existing.Receipt == in.Receipt &&
		existing.PayloadSHA256 == in.PayloadSHA256 && existing.ProcessIdentity == in.ProcessIdentity &&
		existing.PaneGeneration == in.PaneGeneration && existing.Mode == AdmissionFresh &&
		existing.ExistingTurnToken == predecessor && existing.SignalProtocol == in.SignalProtocol &&
		existing.TranscriptProvider == in.TranscriptProvider && existing.TranscriptFlag == in.TranscriptFlag &&
		existing.TranscriptPath == in.TranscriptPath && existing.Purpose == in.Purpose && existing.PurposeID == in.PurposeID
}

// PrepareAdmission makes the provider mutation transaction durable in the
// Work aggregate. A Work can have only one active admission. Exact repeats are
// idempotent; a token or receipt rebound to different bytes fails closed.
func (e *Engine) PrepareAdmission(id WorkID, in PrepareAdmissionInput) (bool, *State, error) {
	if in.SessionID == "" || in.TurnToken == "" || in.Receipt == "" || in.PayloadSHA256 == "" ||
		in.ProcessIdentity == "" || in.PaneGeneration == "" || in.AttemptedAt.IsZero() ||
		(in.Mode != AdmissionFresh && in.Mode != AdmissionConditionalSteer) {
		return false, nil, fmt.Errorf("%w: complete admission identity is required", ErrInvalidCommand)
	}
	if (in.Purpose == "") != (in.PurposeID == "") ||
		(in.Purpose != "" && in.Purpose != AdmissionPurposeReview) {
		return false, nil, fmt.Errorf("%w: invalid admission purpose", ErrInvalidCommand)
	}
	before, err := e.State(id)
	if err != nil {
		return false, nil, err
	}
	st, err := e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		if terminal(st) {
			return nil, ErrTerminal
		}
		if existing := st.Admission(in.TurnToken); existing != nil {
			if existing.SessionID != in.SessionID || existing.Receipt != in.Receipt ||
				existing.PayloadSHA256 != in.PayloadSHA256 || existing.ProcessIdentity != in.ProcessIdentity ||
				existing.PaneGeneration != in.PaneGeneration || existing.Mode != in.Mode ||
				existing.ExistingTurnToken != in.ExistingTurnToken || existing.BaselineActivityID != in.BaselineActivityID ||
				existing.ClaimToken != in.ClaimToken || existing.SignalProtocol != in.SignalProtocol ||
				existing.Purpose != in.Purpose || existing.PurposeID != in.PurposeID ||
				existing.TranscriptProvider != in.TranscriptProvider || existing.TranscriptFlag != in.TranscriptFlag ||
				existing.TranscriptPath != in.TranscriptPath {
				return nil, fmt.Errorf("%w: admission token belongs to different input", ErrInvalidCommand)
			}
			if existing.Status != AdmissionAborted {
				return nil, nil
			}
			if st.ActiveAdmission() != nil {
				return nil, fmt.Errorf("%w: another admission is still active", ErrAttemptActive)
			}
			if in.Purpose == AdmissionPurposeReview &&
				(st.Review == nil || st.Review.Handler == nil || st.Review.Handler.HandlerID != in.PurposeID) {
				return nil, ErrReviewLease
			}
			return []Event{{
				WorkID: id, Kind: KAdmissionRearmed, TurnToken: in.TurnToken,
				SourceID: "admission-rearm:" + string(in.TurnToken) + ":" + fmt.Sprint(st.Revision), At: now,
				Payload: AdmissionPreparedPayload{
					SessionID: in.SessionID, Receipt: in.Receipt, ClaimToken: in.ClaimToken,
					PayloadSHA256: in.PayloadSHA256, ProcessIdentity: in.ProcessIdentity,
					PaneGeneration: in.PaneGeneration, Mode: in.Mode,
					ExistingTurnToken: in.ExistingTurnToken, BaselineActivityID: in.BaselineActivityID,
					SignalProtocol: in.SignalProtocol, AttemptedAt: in.AttemptedAt,
					TranscriptProvider: in.TranscriptProvider, TranscriptFlag: in.TranscriptFlag,
					TranscriptPath: in.TranscriptPath, Purpose: in.Purpose, PurposeID: in.PurposeID,
				},
			}}, nil
		}
		if active := st.ActiveAdmission(); active != nil {
			return nil, fmt.Errorf("%w: admission %s is still %s", ErrAttemptActive, active.TurnToken, active.Status)
		}
		for _, admission := range st.Admissions {
			if in.Purpose != "" && admission.Purpose == in.Purpose && admission.PurposeID == in.PurposeID &&
				admission.Status != AdmissionAborted {
				return nil, fmt.Errorf("%w: admission purpose already belongs to %s", ErrAttemptActive, admission.TurnToken)
			}
		}
		switch in.Purpose {
		case AdmissionPurposeReview:
			if in.Mode != AdmissionFresh || st.Review == nil || st.Review.Handler == nil ||
				st.Review.Handler.HandlerID != in.PurposeID {
				return nil, ErrReviewLease
			}
		}
		if in.ClaimToken == "" {
			switch in.Mode {
			case AdmissionConditionalSteer:
				if st.Attempt == nil || st.Attempt.SessionID != in.SessionID ||
					st.Attempt.TurnToken != in.ExistingTurnToken || in.BaselineActivityID == "" {
					return nil, fmt.Errorf("%w: steering requires the exact active Attempt", ErrStaleInput)
				}
			case AdmissionFresh:
				if st.Attempt != nil {
					boundReview := st.Review != nil && st.Review.Handler != nil &&
						in.Purpose == AdmissionPurposeReview && st.Review.Handler.HandlerID == in.PurposeID
					if !boundReview {
						return nil, ErrAttemptActive
					}
				}
			}
		}
		events := []Event{{
			WorkID: id, Kind: KAdmissionPrepared, TurnToken: in.TurnToken,
			SourceID: "admission-prepare:" + string(in.TurnToken), At: now,
			Payload: AdmissionPreparedPayload{
				SessionID: in.SessionID, Receipt: in.Receipt, ClaimToken: in.ClaimToken,
				PayloadSHA256: in.PayloadSHA256, ProcessIdentity: in.ProcessIdentity,
				PaneGeneration: in.PaneGeneration, Mode: in.Mode,
				ExistingTurnToken: in.ExistingTurnToken, BaselineActivityID: in.BaselineActivityID,
				SignalProtocol: in.SignalProtocol, AttemptedAt: in.AttemptedAt, TranscriptProvider: in.TranscriptProvider,
				TranscriptFlag: in.TranscriptFlag, TranscriptPath: in.TranscriptPath,
				Purpose: in.Purpose, PurposeID: in.PurposeID,
			},
		}}
		return events, nil
	})
	if err != nil {
		return false, st, err
	}
	return st.Revision > before.Revision, st, nil
}

type AcceptAdmissionInput struct {
	SessionID       string
	Receipt         string
	PayloadSHA256   string
	ActivityID      string
	AdmissionStream string
	AdmissionID     string
	AdmissionCursor uint64
	AdmissionSHA256 string
	AdmissionAt     time.Time
}

// AcceptAdmission records exact provider evidence. Fresh initial input commits
// admission.accepted and turn.admitted in one batch. A review-purpose admission
// is accepted but remains non-owning until AcceptReviewFollowUp atomically
// settles the review. A signal-protocol steer is also a new lifecycle Turn:
// the provider may reuse its Activity, but the exact prompt token atomically
// replaces the predecessor as the active Attempt.
func (e *Engine) AcceptAdmission(id WorkID, token TurnToken, in AcceptAdmissionInput) (*State, error) {
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		a := st.Admission(token)
		if a == nil {
			return nil, fmt.Errorf("%w: admission is not prepared", ErrInvalidCommand)
		}
		if a.Status == AdmissionAccepted {
			if a.ActivityID != "" {
				return nil, nil
			}
			if a.SessionID != in.SessionID || a.Receipt != in.Receipt || a.PayloadSHA256 != in.PayloadSHA256 ||
				in.ActivityID == "" || in.AdmissionID == "" || in.AdmissionSHA256 != a.PayloadSHA256 {
				return nil, fmt.Errorf("%w: provider evidence does not match signal-accepted admission", ErrInvalidCommand)
			}
			return []Event{{
				WorkID: id, Kind: KAdmissionAccepted, TurnToken: token,
				SourceID: "admission-evidence:" + string(token), At: now,
				Payload: AdmissionAcceptedPayload{ActivityID: in.ActivityID, AdmissionStream: in.AdmissionStream,
					AdmissionID: in.AdmissionID, AdmissionCursor: in.AdmissionCursor,
					AdmissionSHA256: in.AdmissionSHA256, AdmissionAt: in.AdmissionAt,
					ResultTurnToken: a.ResultTurnToken},
			}}, nil
		}
		if a.Status == AdmissionAborted {
			return nil, fmt.Errorf("%w: aborted admission cannot be accepted", ErrInvalidCommand)
		}
		if a.SessionID != in.SessionID || a.Receipt != in.Receipt || a.PayloadSHA256 != in.PayloadSHA256 ||
			in.ActivityID == "" || in.AdmissionID == "" || in.AdmissionSHA256 != a.PayloadSHA256 ||
			(!in.AdmissionAt.IsZero() && in.AdmissionAt.Before(a.AttemptedAt)) {
			return nil, fmt.Errorf("%w: provider evidence does not match prepared admission", ErrInvalidCommand)
		}
		resultToken := token
		if a.Mode == AdmissionConditionalSteer {
			if st.Attempt == nil || st.Attempt.SessionID != a.SessionID || st.Attempt.TurnToken != a.ExistingTurnToken ||
				in.ActivityID != a.BaselineActivityID {
				return nil, ErrStaleInput
			}
			if !a.SignalProtocol {
				resultToken = a.ExistingTurnToken
			}
		}
		events := []Event{{
			WorkID: id, Kind: KAdmissionAccepted, TurnToken: token,
			SourceID: "admission-accept:" + string(token), At: now,
			Payload: AdmissionAcceptedPayload{
				ActivityID: in.ActivityID, AdmissionStream: in.AdmissionStream, AdmissionID: in.AdmissionID,
				AdmissionCursor: in.AdmissionCursor, AdmissionSHA256: in.AdmissionSHA256,
				AdmissionAt: in.AdmissionAt, ResultTurnToken: resultToken,
			},
		}}
		if a.ClaimToken != "" {
			return events, nil
		}
		if a.Mode == AdmissionConditionalSteer && !a.SignalProtocol {
			events = append(events, Event{
				WorkID: id, Kind: KTurnHeartbeat, TurnToken: st.Attempt.TurnToken, Fence: st.Attempt.Generation,
				SourceID: "admission-steer:" + string(token), At: now,
				Payload: HeartbeatPayload{LeaseSeconds: int(LeaseGrace.Seconds())},
			})
			return events, nil
		}
		if a.Mode == AdmissionConditionalSteer {
			predecessor, predecessorFence := st.Attempt.TurnToken, st.Attempt.Generation
			events = append(events,
				Event{WorkID: id, Kind: KTurnRelinquished, TurnToken: predecessor, Fence: predecessorFence,
					SourceID: "signal-steer-relinquish:" + string(token), At: now,
					Payload: RelinquishedPayload{Reason: "superseded by exact delegated prompt"}},
				Event{WorkID: id, Kind: KTurnAdmitted, TurnToken: token, Fence: st.Fence + 2,
					SourceID: "admit:" + string(token), At: now,
					Payload: AdmittedPayload{SessionID: a.SessionID, Delegated: true, FollowUpOf: predecessor}},
			)
			return events, nil
		}
		if st.Review != nil && st.Review.Handler != nil && a.Purpose == AdmissionPurposeReview &&
			a.PurposeID == st.Review.Handler.HandlerID {
			return events, nil
		}
		if st.Attempt != nil {
			return nil, ErrAttemptActive
		}
		events = append(events, Event{
			WorkID: id, Kind: KTurnAdmitted, TurnToken: token, Fence: st.Fence + 1,
			SourceID: "admit:" + string(token), At: now,
			Payload: AdmittedPayload{SessionID: a.SessionID, Delegated: true, FollowUpOf: a.ExistingTurnToken},
		})
		return events, nil
	})
}

// AcceptAdmissionBySignal uses an exact prompt-carried Control signal as
// authoritative proof that provider input was accepted. Later provider
// evidence may enrich this admission, but cannot admit another Turn.
func (e *Engine) AcceptAdmissionBySignal(id WorkID, token TurnToken, sessionID string) (*State, error) {
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		a := st.Admission(token)
		if a == nil || a.SessionID != sessionID || !a.SignalProtocol || a.ClaimToken != "" {
			return nil, fmt.Errorf("%w: no exact delegated signal admission", ErrInvalidCommand)
		}
		if a.Status == AdmissionAccepted {
			return nil, nil
		}
		if a.Status == AdmissionAborted {
			return nil, fmt.Errorf("%w: aborted admission cannot be accepted", ErrInvalidCommand)
		}
		if a.Mode == AdmissionConditionalSteer {
			if st.Attempt == nil || st.Attempt.SessionID != sessionID || st.Attempt.TurnToken != a.ExistingTurnToken {
				return nil, ErrStaleInput
			}
		}
		events := []Event{{WorkID: id, Kind: KAdmissionAccepted, TurnToken: token,
			SourceID: "admission-signal:" + string(token), At: now,
			Payload: AdmissionAcceptedPayload{ResultTurnToken: token}}}
		if a.Mode == AdmissionConditionalSteer {
			predecessor, predecessorFence := st.Attempt.TurnToken, st.Attempt.Generation
			events = append(events,
				Event{WorkID: id, Kind: KTurnRelinquished, TurnToken: predecessor, Fence: predecessorFence,
					SourceID: "signal-steer-relinquish:" + string(token), At: now,
					Payload: RelinquishedPayload{Reason: "superseded by exact delegated prompt"}},
				Event{WorkID: id, Kind: KTurnAdmitted, TurnToken: token, Fence: st.Fence + 2,
					SourceID: "admit:" + string(token), At: now,
					Payload: AdmittedPayload{SessionID: sessionID, Delegated: true, FollowUpOf: predecessor}},
			)
			return events, nil
		}
		if st.Review != nil && st.Review.Handler != nil && a.Purpose == AdmissionPurposeReview &&
			a.PurposeID == st.Review.Handler.HandlerID {
			return events, nil
		}
		if st.Attempt != nil {
			return nil, ErrAttemptActive
		}
		events = append(events, Event{WorkID: id, Kind: KTurnAdmitted, TurnToken: token, Fence: st.Fence + 1,
			SourceID: "admit:" + string(token), At: now,
			Payload: AdmittedPayload{SessionID: sessionID, Delegated: true, FollowUpOf: a.ExistingTurnToken}})
		return events, nil
	})
}

func (e *Engine) MarkAdmissionAmbiguous(id WorkID, token TurnToken, reason string) (*State, error) {
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		a := st.Admission(token)
		if a == nil {
			return nil, fmt.Errorf("%w: admission is not prepared", ErrInvalidCommand)
		}
		if a.Status == AdmissionAmbiguous || a.Status == AdmissionAccepted {
			return nil, nil
		}
		if a.Status != AdmissionPrepared {
			return nil, fmt.Errorf("%w: admission is %s", ErrInvalidCommand, a.Status)
		}
		return []Event{{WorkID: id, Kind: KAdmissionAmbiguous, TurnToken: token,
			SourceID: "admission-ambiguous:" + string(token), At: now,
			Payload: AdmissionAmbiguousPayload{Reason: reason}}}, nil
	})
}

func (e *Engine) AbortAdmission(id WorkID, token TurnToken, receipt, payloadSHA256, reason string) (*State, error) {
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		a := st.Admission(token)
		if a == nil {
			return nil, fmt.Errorf("%w: admission is not prepared", ErrInvalidCommand)
		}
		if a.Receipt != receipt || a.PayloadSHA256 != payloadSHA256 {
			return nil, fmt.Errorf("%w: admission identity mismatch", ErrInvalidCommand)
		}
		if a.Status == AdmissionAborted {
			return nil, nil
		}
		if a.Status == AdmissionAccepted {
			return nil, fmt.Errorf("%w: accepted admission cannot be aborted", ErrInvalidCommand)
		}
		return []Event{{WorkID: id, Kind: KAdmissionAborted, TurnToken: token,
			SourceID: "admission-abort:" + string(token), At: now,
			Payload: AdmissionAbortedPayload{Reason: reason}}}, nil
	})
}

// AdmitTurn is idempotent by TurnToken: re-admission of a known token is a
// no-op success. Admission while another Attempt is active is rejected (I2).
func (e *Engine) AdmitTurn(id WorkID, in AdmitTurnInput) (applied bool, st *State, err error) {
	if in.TurnToken == "" || in.SessionID == "" {
		return false, nil, fmt.Errorf("%w: session and turn token required", ErrInvalidCommand)
	}
	before, err := e.State(id)
	if err != nil {
		return false, nil, err
	}
	st, err = e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		if terminal(st) {
			return nil, ErrTerminal
		}
		if existing := st.turnByToken(in.TurnToken); existing != nil {
			if existing.SessionID != in.SessionID {
				return nil, ErrStaleInput
			}
			return nil, nil // exact duplicate admission: idempotent no-op
		}
		if st.Attempt != nil {
			return nil, ErrAttemptActive
		}
		return []Event{{
			WorkID: id, Kind: KTurnAdmitted, TurnToken: in.TurnToken,
			Fence: st.Fence + 1, SourceID: "admit:" + string(in.TurnToken), At: now,
			Payload: AdmittedPayload{SessionID: in.SessionID, Delegated: in.Delegated, FollowUpOf: in.FollowUpOf},
		}}, nil
	})
	if err != nil {
		return false, st, err
	}
	return st.Revision > before.Revision, st, nil
}

// Heartbeat extends the live turn's lease monotonically.
func (e *Engine) Heartbeat(id WorkID, attempt AttemptIdentity, leaseSeconds int) (*State, error) {
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		if !currentAttempt(st, attempt) {
			return nil, ErrStaleInput
		}
		deadline, due := coalescedLeaseRenewal(st.Attempt.LeaseDeadline, now, time.Duration(leaseSeconds)*time.Second)
		if !due {
			return nil, nil
		}
		return []Event{{
			WorkID: id, Kind: KTurnHeartbeat, TurnToken: attempt.TurnToken, Fence: attempt.Fence,
			SourceID: fmt.Sprintf("hb:%s:%d", attempt.TurnToken, deadline.UnixNano()), At: now,
			Payload: HeartbeatPayload{LeaseSeconds: leaseSeconds},
		}}, nil
	})
}

// Progress renews a live turn from exact provider-running evidence. Detailed
// provider evidence lives in the Turn ledger; Lifecycle persists only
// coalesced lease extensions, so observation frequency cannot drive revision
// growth.
func (e *Engine) Progress(id WorkID, attempt AttemptIdentity, note string) (*State, error) {
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		if !currentAttempt(st, attempt) {
			return nil, ErrStaleInput
		}
		deadline, due := coalescedLeaseRenewal(st.Attempt.LeaseDeadline, now, LeaseGrace)
		if !due {
			return nil, nil
		}
		return []Event{{
			WorkID: id, Kind: KTurnProgress, TurnToken: attempt.TurnToken, Fence: attempt.Fence,
			SourceID: fmt.Sprintf("prog:%s:%d", attempt.TurnToken, deadline.UnixNano()), At: now,
			Payload: ProgressPayload{Note: note},
		}}, nil
	})
}

// coalescedLeaseRenewal admits at most two durable renewals per requested
// lease window. The target must advance the existing deadline by at least half
// a lease, making the result independent of how often an observation loop
// happens to report the same live provider phase.
func coalescedLeaseRenewal(current, observedAt time.Time, lease time.Duration) (time.Time, bool) {
	if lease <= 0 {
		return time.Time{}, false
	}
	target := observedAt.Add(lease).UTC()
	return target, !target.Before(current.Add(lease / 2))
}

// DoneInput settles the live turn.
type DoneInput struct {
	OK          bool
	Summary     string
	CriteriaMet bool
	// Final carries strong completion authority (see DonePayload).
	Final bool
}

// ReportTurnDone applies the completion rule. Stale tokens are rejected
// idempotently: late results can never touch a newer turn (C). One upgrade is
// allowed: a turn provisionally settled as lost (execution-evidence loss) may
// be upgraded by later authoritative terminal evidence while it is still the
// latest turn, no newer Attempt exists, and the Work is non-terminal (C.2.4).
func (e *Engine) ReportTurnDone(id WorkID, attempt AttemptIdentity, in DoneInput) (*State, error) {
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		historical := st.turnByToken(attempt.TurnToken)
		if historical != nil && historical.SettledAt != nil && historical.Outcome != "lost" {
			if historical.SessionID != attempt.SessionID || historical.Generation != attempt.Fence {
				return nil, ErrStaleInput
			}
			return nil, nil
		}
		if !currentAttempt(st, attempt) {
			t := historical
			latest := len(st.Attempts) > 0 && st.Attempts[len(st.Attempts)-1].Token == attempt.TurnToken
			if t == nil || t.Outcome != "lost" || !latest ||
				t.SessionID != attempt.SessionID || t.Generation != attempt.Fence || st.Attempt != nil || terminal(st) {
				return nil, ErrStaleInput
			}
		}
		return []Event{{
			WorkID: id, Kind: KTurnDone, TurnToken: attempt.TurnToken, Fence: attempt.Fence,
			SourceID: "done:" + string(attempt.TurnToken), At: now,
			Payload: DonePayload{OK: in.OK, Summary: in.Summary, CriteriaMet: in.CriteriaMet, Final: in.Final},
		}}, nil
	})
}

// ReportTurnLost releases an Attempt after evidence loss or lease escalation.
func (e *Engine) ReportTurnLost(id WorkID, attempt AttemptIdentity, reason string) (*State, error) {
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		t := st.turnByToken(attempt.TurnToken)
		if t == nil {
			return nil, ErrStaleInput
		}
		if t.SessionID != attempt.SessionID || t.Generation != attempt.Fence {
			return nil, ErrStaleInput
		}
		if t.SettledAt != nil {
			return nil, nil // already settled: idempotent no-op
		}
		if st.Attempt != nil && !currentAttempt(st, attempt) {
			return nil, ErrStaleInput
		}
		return []Event{{
			WorkID: id, Kind: KTurnLost, TurnToken: attempt.TurnToken, Fence: attempt.Fence,
			SourceID: "lost:" + string(attempt.TurnToken), At: now,
			Payload: LostPayload{Reason: reason},
		}}, nil
	})
}

// Amend patches mutable fields with an optional CAS revision pin.
func (e *Engine) Amend(id WorkID, expectedRevision uint64, title, objective, doneCriteriaRef, nextAction *string) (*State, error) {
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		if expectedRevision != 0 && expectedRevision != st.Revision {
			return nil, ErrRevisionConflict
		}
		if terminal(st) {
			return nil, ErrTerminal
		}
		return []Event{{
			WorkID: id, Kind: KWorkAmended, SourceID: "amend:" + e.newID(), At: now,
			Payload: AmendedPayload{Title: title, Objective: objective, DoneCriteriaRef: doneCriteriaRef, NextAction: nextAction},
		}}, nil
	})
}

// Cancel terminalizes from any non-terminal state.
func (e *Engine) Cancel(id WorkID, expectedRevision uint64, actor, reason string) (*State, error) {
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		if expectedRevision != 0 && expectedRevision != st.Revision {
			return nil, ErrRevisionConflict
		}
		if terminal(st) {
			return nil, ErrTerminal
		}
		fence := uint64(0)
		token := TurnToken("")
		if st.Attempt != nil {
			fence, token = st.Attempt.Generation, st.Attempt.TurnToken
		}
		return []Event{{
			WorkID: id, Kind: KWorkCancelled, TurnToken: token, Fence: fence,
			SourceID: "cancel:" + e.newID(), At: now,
			Payload: CancelledPayload{Actor: actor, Reason: reason},
		}}, nil
	})
}

// Complete terminalizes a Work as done by operator decision (CloseWork with
// done). It is the only completion path that bypasses the until_done rule,
// and it requires an explicit actor.
func (e *Engine) Complete(id WorkID, expectedRevision uint64, actor, reason string) (*State, error) {
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		if expectedRevision != 0 && expectedRevision != st.Revision {
			return nil, ErrRevisionConflict
		}
		if terminal(st) {
			return nil, ErrTerminal
		}
		fence := uint64(0)
		token := TurnToken("")
		if st.Attempt != nil {
			fence, token = st.Attempt.Generation, st.Attempt.TurnToken
		}
		return []Event{{
			WorkID: id, Kind: KWorkCompleted, TurnToken: token, Fence: fence,
			SourceID: "complete:" + e.newID(), At: now,
			Payload: CancelledPayload{Actor: actor, Reason: reason},
		}}, nil
	})
}

// SetWait parks a non-owned Work on a typed external producer.
func (e *Engine) SetWait(id WorkID, kind WakeKind, ref string) (*State, error) {
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		if terminal(st) {
			return nil, ErrTerminal
		}
		if st.Attempt != nil {
			return nil, ErrAttemptActive
		}
		return []Event{{
			WorkID: id, Kind: KWakeSet, SourceID: "wake:" + string(kind) + ":" + ref, At: now,
			Payload: WakePayload{WakeKind: kind, Ref: ref},
		}}, nil
	})
}

// ClearWait releases a matching wake; mismatched producers are no-ops.
func (e *Engine) ClearWait(id WorkID, kind WakeKind, ref, occurrence string) (*State, error) {
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		if st.Wake == nil || st.Wake.Kind != kind || st.Wake.Ref != ref {
			return nil, nil
		}
		return []Event{{
			WorkID: id, Kind: KWakeCleared, SourceID: "unwake:" + string(kind) + ":" + ref + ":" + occurrence, At: now,
			Payload: WakeClearedPayload{WakeKind: kind, Ref: ref, Occurrence: occurrence},
		}}, nil
	})
}

// ClaimReview leases the open actionable Event to one Brain handling.
func (e *Engine) ClaimReview(id WorkID, handlerID string, handlerToken TurnToken) (*State, error) {
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		if st.Review == nil {
			return nil, ErrNoOpenReview
		}
		if handlerToken == "" {
			return nil, fmt.Errorf("%w: handler turn token required", ErrInvalidCommand)
		}
		if st.Review.Handler != nil {
			return nil, ErrReviewLease
		}
		if entry := outboxFind(st, st.Review.OutboxID); entry != nil && entry.NextAttemptAt != nil && now.Before(*entry.NextAttemptAt) {
			return nil, ErrReviewLease
		}
		if handlerID == "" {
			return nil, fmt.Errorf("%w: handler identity required", ErrInvalidCommand)
		}
		events := []Event{{
			WorkID: id, Kind: KReviewClaimed, SourceID: "claim:" + e.newID(), At: now,
			Payload: ReviewClaimedPayload{
				EventID: st.Review.EventID, HandlerID: handlerID,
				HandlerToken: handlerToken, ExpiresAt: now.Add(EventClaimTTL),
			},
		}}
		return events, nil
	})
}

// MarkReviewDelivered records that the leased handling reached its target.
func (e *Engine) MarkReviewDelivered(id WorkID, handlerToken TurnToken) (*State, error) {
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		if st.Review == nil || st.Review.Handler == nil || st.Review.Handler.HandlerToken != handlerToken {
			return nil, ErrReviewLease
		}
		if st.Review.Handler.DeliveredAt != nil {
			return nil, nil
		}
		return []Event{{
			WorkID: id, Kind: KReviewDelivered, SourceID: "deliv:" + e.newID(), At: now,
			Payload: ReviewDeliveredPayload{EventID: st.Review.EventID, HandlerToken: handlerToken},
		}}, nil
	})
}

// ReleaseReview drops the handler lease so the Event can be reclaimed.
func (e *Engine) ReleaseReview(id WorkID, handlerToken TurnToken) (*State, error) {
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		if st.Review == nil || st.Review.Handler == nil || st.Review.Handler.HandlerToken != handlerToken {
			return nil, ErrReviewLease
		}
		events := []Event{{
			WorkID: id, Kind: KReviewReleased, SourceID: "release:" + e.newID(), At: now,
			Payload: ReviewReleasedPayload{EventID: st.Review.EventID, HandlerToken: handlerToken},
		}}
		if entry := outboxFind(st, st.Review.OutboxID); entry != nil {
			events = append(events, Event{
				WorkID: id, Kind: KOutboxDispatch, SourceID: "retry:" + st.Review.EventID + ":" + e.newID(), At: now,
				Payload: OutboxDispatchPayload{EntryID: entry.ID, Result: DispatchRetryable, Error: "notification delivery not confirmed"},
			})
		}
		return events, nil
	})
}

const (
	ReviewDeliveryMarkDelivered = "mark_delivered"
	ReviewDeliveryDiscard       = "discard"
	ReviewDeliveryReplay        = "replay"
)

// ResolveReviewDelivery records an explicit actor judgment for an ambiguous
// Host delivery. The append and state transition are one canonical commit.
// Retrying after that exact handler has already been released is a successful
// no-op, which makes the control command safe across lost responses/restarts.
func (e *Engine) ResolveReviewDelivery(id WorkID, action, actor, reason string) (*State, error) {
	if action != ReviewDeliveryMarkDelivered && action != ReviewDeliveryDiscard && action != ReviewDeliveryReplay {
		return nil, fmt.Errorf("%w: invalid review delivery resolution", ErrInvalidCommand)
	}
	if actor == "" || reason == "" {
		return nil, fmt.Errorf("%w: actor and reason required", ErrInvalidCommand)
	}
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		if st.Review == nil || st.Review.Handler == nil {
			return nil, nil
		}
		h := st.Review.Handler
		if h.DeliveredAt != nil {
			return nil, ErrReviewLease
		}
		return []Event{{
			WorkID: id, Kind: KReviewDeliveryResolved,
			SourceID: "delivery-resolution:" + st.Review.EventID + ":" + string(h.HandlerToken), At: now,
			Payload: ReviewDeliveryResolvedPayload{
				EventID: st.Review.EventID, HandlerToken: h.HandlerToken,
				Action: action, Actor: actor, Reason: reason,
			},
		}}, nil
	})
}

// ResolveReviewInput closes the open Event with a typed disposition.
type ResolveReviewInput struct {
	Disposition   Disposition
	Actor         string
	WakeKind      WakeKind
	WakeRef       string
	NextAttemptAt *time.Time
}

// ResolveTerminalReviewInput is the complete capability for recovering an
// actionable review whose provider Attempt is already terminal or closed. The
// exact Event, delivered handler, Attempt identity, and terminal evidence are
// required together; none is inferred from projection or process inventory.
type ResolveTerminalReviewInput struct {
	EventID            string
	HandlerID          string
	HandlerToken       TurnToken
	AttemptSessionID   string
	AttemptToken       TurnToken
	AttemptFence       uint64
	TerminalEvidenceID string
	Disposition        Disposition
	Actor              string
	WakeKind           WakeKind
	WakeRef            string
	NextAttemptAt      *time.Time
	NextSessionID      string
	NextTurnToken      TurnToken
}

// ResolveTerminalReview atomically relinquishes the exact old Attempt, resolves
// the exact actionable Event, and optionally admits the named next Attempt. A
// restart therefore observes either the entire recovery or none of it.
func (e *Engine) ResolveTerminalReview(id WorkID, in ResolveTerminalReviewInput) (*State, error) {
	if in.EventID == "" || in.HandlerID == "" || in.HandlerToken == "" ||
		in.AttemptSessionID == "" || in.AttemptToken == "" || in.AttemptFence == 0 || in.TerminalEvidenceID == "" {
		return nil, fmt.Errorf("%w: exact review, Attempt, and terminal evidence required", ErrInvalidCommand)
	}
	if in.Disposition == DispositionContinue {
		if in.NextSessionID == "" || in.NextTurnToken == "" {
			return nil, fmt.Errorf("%w: continue requires named next Attempt Session and token", ErrInvalidCommand)
		}
	} else if in.NextSessionID != "" || in.NextTurnToken != "" {
		return nil, fmt.Errorf("%w: next Attempt is valid only for continue", ErrInvalidCommand)
	}
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		if st.SeenSources["resolve:"+in.EventID] {
			return nil, nil
		}
		if err := validateWaitDisposition(in.Disposition, in.WakeKind, in.WakeRef, in.NextAttemptAt, now); err != nil {
			return nil, err
		}
		if st.Review == nil || st.Review.EventID != in.EventID ||
			st.Review.Handler == nil || st.Review.Handler.HandlerID != in.HandlerID ||
			st.Review.Handler.HandlerToken != in.HandlerToken || st.Review.Handler.DeliveredAt == nil {
			return nil, ErrReviewLease
		}
		identity := AttemptIdentity{SessionID: in.AttemptSessionID, TurnToken: in.AttemptToken, Fence: in.AttemptFence}
		if !currentAttempt(st, identity) {
			return nil, ErrStaleInput
		}
		events := []Event{{
			WorkID: id, Kind: KTurnRelinquished, TurnToken: in.AttemptToken, Fence: in.AttemptFence,
			SourceID: "terminal-evidence:" + in.TerminalEvidenceID, At: now,
			Payload: RelinquishedPayload{Reason: "exact terminal Attempt evidence"},
		}, {
			WorkID: id, Kind: KReviewResolved, SourceID: "resolve:" + in.EventID, At: now,
			Payload: ReviewResolvedPayload{EventID: in.EventID, Disposition: in.Disposition,
				Actor: in.Actor, WakeKind: in.WakeKind, WakeRef: in.WakeRef, NextAttemptAt: in.NextAttemptAt},
		}}
		if in.Disposition == DispositionContinue {
			events = append(events, Event{
				WorkID: id, Kind: KTurnAdmitted, TurnToken: in.NextTurnToken, Fence: in.AttemptFence + 1,
				SourceID: "terminal-follow-up:" + in.TerminalEvidenceID + ":" + string(in.NextTurnToken), At: now,
				Payload: AdmittedPayload{SessionID: in.NextSessionID, Delegated: true, FollowUpOf: in.AttemptToken},
			})
		}
		return events, nil
	})
}

// AcceptReviewFollowUp atomically settles a still-live reviewed turn, closes
// the exact actionable Event, and admits the named accepted admission. The append is
// one log transaction, so restart can observe only the state before or after
// the Attempt transition.
func (e *Engine) AcceptReviewFollowUp(id WorkID, eventID, nextSession string, nextToken TurnToken) (*State, error) {
	if eventID == "" || nextSession == "" || nextToken == "" {
		return nil, fmt.Errorf("%w: Event, next Attempt Session, and next Attempt token required", ErrInvalidCommand)
	}
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		if st.Review == nil || st.Review.EventID != eventID {
			return nil, nil
		}
		admission := st.Admission(nextToken)
		if admission == nil || admission.Status != AdmissionAccepted || admission.SessionID != nextSession ||
			admission.Mode != AdmissionFresh || admission.ClaimToken != "" || st.Review.Handler == nil ||
			admission.Purpose != AdmissionPurposeReview || admission.PurposeID != st.Review.Handler.HandlerID {
			return nil, fmt.Errorf("%w: next Attempt provider input is not accepted", ErrInvalidCommand)
		}
		events := make([]Event, 0, 3)
		followUpOf := TurnToken("")
		if st.Attempt != nil {
			followUpOf = st.Attempt.TurnToken
			events = append(events, Event{
				WorkID: id, Kind: KTurnRelinquished, TurnToken: st.Attempt.TurnToken, Fence: st.Attempt.Generation,
				SourceID: "review-relinquish:" + eventID, At: now,
				Payload: RelinquishedPayload{Reason: "review accepted with named follow-up"},
			})
		}
		events = append(events,
			Event{WorkID: id, Kind: KReviewResolved, SourceID: "resolve:" + eventID, At: now,
				Payload: ReviewResolvedPayload{EventID: eventID, Disposition: DispositionContinue, Actor: "brain"}},
			Event{WorkID: id, Kind: KTurnAdmitted, TurnToken: nextToken, Fence: st.Fence + 1,
				SourceID: "admit:" + string(nextToken), At: now,
				Payload: AdmittedPayload{SessionID: nextSession, Delegated: true, FollowUpOf: followUpOf}},
		)
		return events, nil
	})
}

// ResolveReview applies the disposition. Resolving a superseded Event is an
// idempotent no-op.
func (e *Engine) ResolveReview(id WorkID, eventID string, in ResolveReviewInput) (*State, error) {
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		if st.Review == nil || st.Review.EventID != eventID {
			return nil, nil
		}
		if err := validateWaitDisposition(in.Disposition, in.WakeKind, in.WakeRef, in.NextAttemptAt, now); err != nil {
			return nil, err
		}
		return []Event{{
			WorkID: id, Kind: KReviewResolved, SourceID: "resolve:" + eventID, At: now,
			Payload: ReviewResolvedPayload{
				EventID: eventID, Disposition: in.Disposition, Actor: in.Actor,
				WakeKind: in.WakeKind, WakeRef: in.WakeRef, NextAttemptAt: in.NextAttemptAt,
			},
		}}, nil
	})
}

func validateWaitDisposition(disposition Disposition, kind WakeKind, ref string, nextAttemptAt *time.Time, now time.Time) error {
	if disposition != DispositionWait {
		return nil
	}
	if kind == "" || ref == "" {
		return fmt.Errorf("%w: wait requires a typed wake", ErrInvalidCommand)
	}
	if kind == WakeDueRetry {
		if nextAttemptAt == nil || !nextAttemptAt.After(now) {
			return fmt.Errorf("%w: due_retry requires a future next_attempt_at", ErrInvalidCommand)
		}
		return nil
	}
	if nextAttemptAt != nil {
		return fmt.Errorf("%w: next_attempt_at is valid only for due_retry", ErrInvalidCommand)
	}
	return nil
}

// OpenReview records an external judgment obligation (e.g. from an appended
// actionable Event). No-op while an Event is already open.
func (e *Engine) OpenReview(id WorkID, reason, ref string) (*State, error) {
	return e.openReview(id, reason, ref, "evt-"+e.newID())
}

// OpenReviewEvent opens judgment for an already-existing exact actionable
// Event. The supplied Event ID is preserved through review, outbox, delivery,
// card projection, and resolution.
func (e *Engine) OpenReviewEvent(id WorkID, reason, ref, eventID string) (*State, error) {
	if eventID == "" {
		return nil, fmt.Errorf("%w: actionable event identity required", ErrInvalidCommand)
	}
	return e.openReview(id, reason, ref, eventID)
}

func (e *Engine) openReview(id WorkID, reason, ref, eventID string) (*State, error) {
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		if st.Review != nil {
			return nil, nil
		}
		return []Event{{
			WorkID: id, Kind: KReviewOpened,
			SourceID: "fsmreview:" + reason + ":" + ref, At: now,
			Payload: ReviewOpenedPayload{EventID: eventID, Reason: reason, Ref: ref},
		}}, nil
	})
}

// AckNotification acknowledges the deterministic source-thread notification
// owned by one actionable event. Repeating the acknowledgement is idempotent.
func (e *Engine) AckNotification(id WorkID, eventID string) (*State, error) {
	if eventID == "" {
		return nil, fmt.Errorf("%w: event identity required", ErrInvalidCommand)
	}
	return e.AckOutbox(id, reviewOutboxID(id, eventID), DispatchSuccess, "")
}

// RecordNotificationOutcome applies the four-outcome side-effect policy to
// the exact event_id. In particular, unknown_side_effect is durable and never
// receives next_attempt_at.
func (e *Engine) RecordNotificationOutcome(id WorkID, eventID, outcome, errMsg string) (*State, error) {
	if eventID == "" {
		return nil, fmt.Errorf("%w: event identity required", ErrInvalidCommand)
	}
	return e.AckOutbox(id, reviewOutboxID(id, eventID), outcome, errMsg)
}

// AckNotificationIfPresent acknowledges only when the exact Event currently
// owns a canonical outbox entry. Projection-only historical cards return
// present=false; absence is observed explicitly rather than swallowed as an
// authority error.
func (e *Engine) AckNotificationIfPresent(id WorkID, eventID string) (present bool, st *State, err error) {
	if eventID == "" {
		return false, nil, fmt.Errorf("%w: event identity required", ErrInvalidCommand)
	}
	st, err = e.State(id)
	if err != nil {
		return false, nil, err
	}
	if outboxFind(st, reviewOutboxID(id, eventID)) == nil {
		return false, st, nil
	}
	st, err = e.AckNotification(id, eventID)
	return true, st, err
}

// AckOutbox records a dispatch attempt outcome for a continuation entry.
func (e *Engine) AckOutbox(id WorkID, entryID, result, errMsg string) (*State, error) {
	switch result {
	case DispatchSuccess, DispatchRetryable, DispatchUnknownSideEffect, DispatchTerminal:
	default:
		return nil, fmt.Errorf("%w: invalid external outcome %q", ErrInvalidCommand, result)
	}
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		entry := outboxFind(st, entryID)
		if entry == nil {
			return nil, ErrUnknownOutbox
		}
		switch entry.DispatchResult {
		case DispatchSuccess, DispatchUnknownSideEffect, DispatchTerminal:
			return nil, nil // resolved or quarantined outcomes stick
		}
		return []Event{{
			WorkID: id, Kind: KOutboxDispatch, SourceID: "ack:" + entryID + ":" + result + ":" + e.newID(), At: now,
			Payload: OutboxDispatchPayload{EntryID: entryID, Result: result, Error: errMsg},
		}}, nil
	})
}

// ---- supervisor ----

// ReportLeaseExpired records one idempotent lease-expiry fact for the live
// Attempt. It shares the sweep's source identity, so a supervisor probe
// and the periodic sweep dedupe to a single expiry per (turn, deadline).
func (e *Engine) ReportLeaseExpired(id WorkID, token TurnToken) (*State, error) {
	return e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
		if st == nil {
			return nil, ErrUnknownWork
		}
		if st.Attempt == nil || st.Attempt.TurnToken != token {
			return nil, nil
		}
		return []Event{{
			WorkID: id, Kind: KLeaseExpired, TurnToken: token, Fence: st.Attempt.Generation,
			SourceID: leaseExpiredSourceID(token, st.Attempt.LeaseDeadline), At: now,
			Payload: ExpiredPayload{Deadline: st.Attempt.LeaseDeadline},
		}}, nil
	})
}

func leaseExpiredSourceID(token TurnToken, deadline time.Time) string {
	return fmt.Sprintf("lease:%s:%d", token, deadline.UTC().Unix())
}

// Sweep reconciles durable timers across all Works: it expires claims, opens
// due-retry reviews, appends idempotent lease-expiry facts, and escalates
// persistently expired turns to lost. It never starts execution or creates a
// Session.
func (e *Engine) Sweep() error {
	e.mu.Lock()
	ids := make([]WorkID, 0, len(e.works))
	for id := range e.works {
		ids = append(ids, id)
	}
	e.mu.Unlock()
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	now := e.nowUTC()
	for _, id := range ids {
		st, err := e.State(id)
		if err != nil || st == nil {
			continue
		}
		if st.Review != nil && st.Review.Handler != nil && st.Review.Handler.DeliveredAt == nil &&
			!now.Before(st.Review.Handler.ClaimExpiresAt) {
			if _, err := e.dispatch(id, func(current *State, at time.Time) ([]Event, error) {
				if current.Review == nil || current.Review.Handler == nil || current.Review.Handler.DeliveredAt != nil ||
					at.Before(current.Review.Handler.ClaimExpiresAt) {
					return nil, nil
				}
				events := []Event{{
					WorkID: id, Kind: KReviewReleased,
					SourceID: "claim-expired:" + current.Review.EventID, At: at,
					Payload: ReviewReleasedPayload{
						EventID: current.Review.EventID, HandlerToken: current.Review.Handler.HandlerToken,
					},
				}}
				if entry := outboxFind(current, current.Review.OutboxID); entry != nil {
					events = append(events, Event{
						WorkID: id, Kind: KOutboxDispatch,
						SourceID: "claim-retry:" + current.Review.EventID + ":" + e.newID(), At: at,
						Payload: OutboxDispatchPayload{
							EntryID: entry.ID, Result: DispatchRetryable, Error: "event claim expired",
						},
					})
				}
				return events, nil
			}); err != nil {
				return err
			}
		}
		if st.Wake != nil && st.Wake.Kind == WakeDueRetry && st.Wake.NextAttemptAt != nil &&
			!now.Before(*st.Wake.NextAttemptAt) {
			if _, err := e.dispatch(id, func(current *State, at time.Time) ([]Event, error) {
				if current.Wake == nil || current.Wake.Kind != WakeDueRetry ||
					current.Wake.NextAttemptAt == nil || at.Before(*current.Wake.NextAttemptAt) {
					return nil, nil
				}
				wake := current.Wake
				dueKey := fmt.Sprintf("%s:%d", wake.Ref, wake.NextAttemptAt.UTC().UnixNano())
				eventID := stableID("due-retry-event", string(id), dueKey)
				return []Event{{
					WorkID: id, Kind: KWakeCleared,
					SourceID: "due-retry-clear:" + dueKey, At: at,
					Payload: WakeClearedPayload{WakeKind: wake.Kind, Ref: wake.Ref, Occurrence: dueKey},
				}, {
					WorkID: id, Kind: KReviewOpened,
					SourceID: "due-retry:" + dueKey, At: at,
					Payload: ReviewOpenedPayload{EventID: eventID, Reason: "retry_due", Ref: wake.Ref},
				}}, nil
			}); err != nil {
				return err
			}
		}
		attempt := st.Attempt
		if attempt == nil {
			continue
		}
		if now.After(attempt.LeaseDeadline) {
			if _, err := e.dispatch(id, func(st *State, now time.Time) ([]Event, error) {
				if st.Attempt == nil || st.Attempt.TurnToken != attempt.TurnToken {
					return nil, nil
				}
				events := []Event{{
					WorkID: id, Kind: KLeaseExpired, TurnToken: attempt.TurnToken, Fence: st.Attempt.Generation,
					SourceID: leaseExpiredSourceID(attempt.TurnToken, st.Attempt.LeaseDeadline), At: now,
					Payload: ExpiredPayload{Deadline: st.Attempt.LeaseDeadline},
				}}
				if now.After(st.Attempt.LeaseDeadline.Add(LostGrace)) {
					events = append(events, Event{
						WorkID: id, Kind: KTurnLost, TurnToken: attempt.TurnToken, Fence: st.Attempt.Generation,
						SourceID: "lost:" + string(attempt.TurnToken), At: now,
						Payload: LostPayload{Reason: "lease_expired_escalation"},
					})
				}
				return events, nil
			}); err != nil && err != ErrStaleInput {
				return err
			}
		}
	}
	return nil
}

// NextWakeAt returns the earliest durable retry, claim-expiry, or Attempt
// liveness deadline. The daemon can sleep on one timer without keeping a Brain
// turn alive or polling Sessions.
func (e *Engine) NextWakeAt() (time.Time, bool) {
	e.mu.Lock()
	ids := make([]WorkID, 0, len(e.works))
	for id := range e.works {
		ids = append(ids, id)
	}
	e.mu.Unlock()
	var next time.Time
	consider := func(at time.Time) {
		if at.IsZero() {
			return
		}
		if next.IsZero() || at.Before(next) {
			next = at
		}
	}
	for _, id := range ids {
		st, err := e.State(id)
		if err != nil || st == nil {
			continue
		}
		if st.Attempt != nil {
			consider(st.Attempt.LeaseDeadline)
		}
		if st.Wake != nil && st.Wake.Kind == WakeDueRetry && st.Wake.NextAttemptAt != nil {
			consider(*st.Wake.NextAttemptAt)
		}
		if st.Review != nil && st.Review.Handler != nil && st.Review.Handler.DeliveredAt == nil {
			consider(st.Review.Handler.ClaimExpiresAt)
		}
		if st.Status.Terminal() {
			continue
		}
		for _, entry := range st.Outbox {
			if entry.Attempts >= MaxDispatchAttempts {
				continue
			}
			if entry.Reason == "review" && st.Review != nil && st.Review.OutboxID == entry.ID && st.Review.Handler != nil {
				continue // claim expiry is the only timer while a handler owns the Event
			}
			switch entry.DispatchResult {
			case DispatchSuccess, DispatchUnknownSideEffect, DispatchTerminal:
				continue
			}
			if entry.NextAttemptAt != nil {
				consider(*entry.NextAttemptAt)
			} else {
				consider(entry.CreatedAt)
			}
		}
	}
	return next, !next.IsZero()
}

// ---- read models ----

// State returns a cloned view of one aggregate.
func (e *Engine) State(id WorkID) (*State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	a := e.works[id]
	if a == nil {
		return nil, ErrUnknownWork
	}
	return a.st.Clone(), nil
}

// ListStates returns cloned views of all aggregates sorted by ID.
func (e *Engine) ListStates() []*State {
	e.mu.Lock()
	states := make(map[WorkID]*State, len(e.works))
	for id, actor := range e.works {
		states[id] = actor.st.Clone()
	}
	e.mu.Unlock()
	ids := make([]WorkID, 0, len(states))
	for id := range states {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]*State, 0, len(ids))
	for _, id := range ids {
		out = append(out, states[id])
	}
	return out
}

// Card is the projected user-facing card. Exactly one exists per Work lineage;
// historical events never materialize additional cards (I6).
type Card struct {
	WorkID           WorkID    `json:"work_id"`
	Revision         uint64    `json:"revision"`
	Status           Status    `json:"status"`
	Title            string    `json:"title"`
	Actionable       bool      `json:"actionable"`
	Reason           string    `json:"reason,omitempty"`
	Summary          string    `json:"summary,omitempty"`
	AttemptSessionID string    `json:"attempt_session_id,omitempty"`
	Unread           bool      `json:"unread"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// ProjectCards derives the card projection from reduced state.
func ProjectCards(states []*State) []Card {
	cards := make([]Card, 0, len(states))
	for _, st := range states {
		card := Card{
			WorkID:    st.ID,
			Revision:  st.Revision,
			Status:    st.Status,
			Title:     st.Title,
			UpdatedAt: st.UpdatedAt,
		}
		if st.Attempt != nil {
			card.AttemptSessionID = st.Attempt.SessionID
		}
		switch {
		case st.Review != nil:
			card.Actionable = true
			card.Reason = st.Review.Reason
		case st.Status == StatusBlocked:
			card.Actionable = true
			card.Reason = "needs_input"
		}
		for i := len(st.Attempts) - 1; i >= 0; i-- {
			if st.Attempts[i].SettledAt != nil && st.Attempts[i].Summary != "" {
				card.Summary = st.Attempts[i].Summary
				break
			}
		}
		if card.Summary == "" {
			card.Summary = st.Objective
		}
		card.Unread = card.Actionable
		cards = append(cards, card)
	}
	return cards
}

// Cards returns the current projection.
func (e *Engine) Cards() []Card {
	return ProjectCards(e.ListStates())
}
