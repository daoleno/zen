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
		if existing := st.AdmissionByToken(in.TurnToken); existing != nil {
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
		if previous := st.Admission; previous != nil && in.Purpose != "" &&
			previous.Purpose == in.Purpose && previous.PurposeID == in.PurposeID && previous.Status != AdmissionAborted {
			return nil, fmt.Errorf("%w: admission purpose already belongs to %s", ErrAttemptActive, previous.TurnToken)
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
		a := st.AdmissionByToken(token)
		if a == nil {
			return nil, fmt.Errorf("%w: admission is not prepared", ErrInvalidCommand)
		}
		if a.Status == AdmissionAccepted {
			if a.ActivityID != "" {
				return nil, nil
			}
			if in.ActivityID == "" {
				return nil, nil
			}
			if a.SessionID != in.SessionID || a.Receipt != in.Receipt || a.PayloadSHA256 != in.PayloadSHA256 ||
				in.AdmissionID == "" || in.AdmissionSHA256 != a.PayloadSHA256 {
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
			in.AdmissionID == "" || in.AdmissionSHA256 != a.PayloadSHA256 ||
			(in.ActivityID == "" && (a.ClaimToken == "" || a.BaselineActivityID == "")) ||
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
		a := st.AdmissionByToken(token)
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
		a := st.AdmissionByToken(token)
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
		a := st.AdmissionByToken(token)
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
		if existing, found := e.admittedAttemptLocked(id, in.TurnToken); found {
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

func (e *Engine) admittedAttemptLocked(id WorkID, token TurnToken) (AttemptIdentity, bool) {
	for _, event := range e.events {
		if event.WorkID != id || event.Kind != KTurnAdmitted || event.TurnToken != token {
			continue
		}
		payload, ok := event.Payload.(AdmittedPayload)
		if !ok {
			return AttemptIdentity{}, false
		}
		return AttemptIdentity{SessionID: payload.SessionID, TurnToken: token, Fence: event.Fence}, true
	}
	return AttemptIdentity{}, false
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
		if !currentAttempt(st, attempt) {
			historical, found := e.admittedAttemptLocked(id, attempt.TurnToken)
			if !found || historical != attempt {
				return nil, ErrStaleInput
			}
			if st.SeenSources["done:"+string(attempt.TurnToken)] {
				return nil, nil
			}
			if st.Attempt != nil || terminal(st) || st.Review == nil ||
				st.Review.Ref != string(attempt.TurnToken) ||
				(st.Review.Reason != "turn_lost" && st.Review.Reason != "lease_expired") {
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
		if st.SeenSources["lost:"+string(attempt.TurnToken)] {
			return nil, nil // already settled: idempotent no-op
		}
		if !currentAttempt(st, attempt) {
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
func (e *Engine) ClaimReview(id WorkID, hostSessionID, handlerID string, handlerToken TurnToken) (*State, error) {
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
		if handlerID == "" {
			return nil, fmt.Errorf("%w: handler identity required", ErrInvalidCommand)
		}
		if hostSessionID == "" {
			return nil, fmt.Errorf("%w: Host Session identity required", ErrInvalidCommand)
		}
		events := []Event{{
			WorkID: id, Kind: KReviewClaimed, SourceID: "claim:" + e.newID(), At: now,
			Payload: ReviewClaimedPayload{
				EventID: st.Review.EventID, HostSessionID: hostSessionID, HandlerID: handlerID,
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
		return []Event{{
			WorkID: id, Kind: KReviewReleased, SourceID: "release:" + e.newID(), At: now,
			Payload: ReviewReleasedPayload{EventID: st.Review.EventID, HandlerToken: handlerToken},
		}}, nil
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
		admission := st.AdmissionByToken(nextToken)
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

// ResolveReview applies the disposition. Resolving an already-closed Event is an
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
// Event. The supplied Event ID is preserved through review, delivery,
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
				return []Event{{
					WorkID: id, Kind: KReviewReleased,
					SourceID: "claim-expired:" + current.Review.EventID, At: at,
					Payload: ReviewReleasedPayload{
						EventID: current.Review.EventID, HandlerToken: current.Review.Handler.HandlerToken,
					},
				}}, nil
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
		card.Summary = st.LastSummary
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
