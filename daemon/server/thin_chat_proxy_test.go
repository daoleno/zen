package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
	"github.com/gorilla/websocket"
)

type thinProxyResponse struct {
	Type       string `json:"type"`
	RequestID  string `json:"request_id"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	FieldCount int    `json:"-"`
}

type thinProxyInputCall struct {
	agentID string
	text    string
}

func definitelyNotSubmittedTestError(receipt string) error {
	return &watcher.InputSubmissionError{
		Result: watcher.InputResult{Outcome: watcher.InputNotSubmitted, Receipt: receipt},
		Cause:  errors.New("target pane generation could not be proven"),
	}
}

func TestInboundSendInputCallsProviderOnceAndAcknowledges(t *testing.T) {
	calls := make(chan thinProxyInputCall, 3)
	srv := &Server{
		sendInputOverride: func(agentID, text string) error {
			calls <- thinProxyInputCall{agentID: agentID, text: text}
			return nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)
	request := clientMessage{
		Type:      "send_input",
		RequestID: "request-1",
		AgentID:   "agent-1",
		Text:      "hello",
	}

	first := sendThinProxyRequest(t, conn, request)
	if first.Type != "input_sent" || first.RequestID != request.RequestID ||
		first.FieldCount != 2 {
		t.Fatalf("first response = %#v", first)
	}
	if call := <-calls; call != (thinProxyInputCall{agentID: "agent-1", text: "hello"}) {
		t.Fatalf("first provider call = %#v", call)
	}
	select {
	case extra := <-calls:
		t.Fatalf("one inbound request caused an extra provider call: %#v", extra)
	default:
	}

	// request_id correlates only this response; it is not a dedupe key.
	second := sendThinProxyRequest(t, conn, request)
	if second.Type != "input_sent" || second.RequestID != request.RequestID {
		t.Fatalf("repeated response = %#v", second)
	}
	if call := <-calls; call != (thinProxyInputCall{agentID: "agent-1", text: "hello"}) {
		t.Fatalf("repeated provider call = %#v", call)
	}
	select {
	case extra := <-calls:
		t.Fatalf("two inbound requests caused an extra provider call: %#v", extra)
	default:
	}
}

func TestInboundSendInputDoesNotReadOrGateOnBusyTranscript(t *testing.T) {
	var transcriptReads atomic.Int32
	var providerCalls atomic.Int32
	srv := &Server{
		providerConversationLoader: func(*work.ProviderConversationReader, string) (work.CodexConversation, error) {
			transcriptReads.Add(1)
			return work.CodexConversation{
				Available: true,
				Activity: &work.ProviderActivity{
					ID:        "provider-active",
					Status:    work.ProviderActivityRunning,
					StartedAt: "2026-07-16T06:00:00Z",
				},
			}, nil
		},
		sendInputOverride: func(string, string) error {
			providerCalls.Add(1)
			return nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)

	for _, requestID := range []string{"busy-request-1", "busy-request-2"} {
		response := sendThinProxyRequest(t, conn, clientMessage{
			Type:      "send_input",
			RequestID: requestID,
			AgentID:   "busy-agent",
			Text:      "provider owns native queuing",
		})
		if response.Type != "input_sent" || response.RequestID != requestID {
			t.Fatalf("busy response = %#v", response)
		}
	}
	if got := providerCalls.Load(); got != 2 {
		t.Fatalf("provider calls = %d, want 2", got)
	}
	if got := transcriptReads.Load(); got != 0 {
		t.Fatalf("send_input read provider transcript %d times, want 0", got)
	}
}

func TestInboundSendInputFailureReturnsRejectionWithoutAck(t *testing.T) {
	wantErr := errors.New("executor rejected input")
	var providerCalls atomic.Int32
	srv := &Server{
		sendInputOverride: func(string, string) error {
			providerCalls.Add(1)
			return wantErr
		},
	}
	conn := openThinProxyTestSocket(t, srv)

	response := sendThinProxyRequest(t, conn, clientMessage{
		Type:      "send_input",
		RequestID: "failed-request",
		AgentID:   "agent-1",
		Text:      "fail",
	})
	if response.Type == "input_sent" {
		t.Fatalf("failed provider call was acknowledged: %#v", response)
	}
	if response.Type != "input_failed" || response.Code != "send_input_failed" ||
		response.Message != wantErr.Error() || response.RequestID != "failed-request" ||
		response.FieldCount != 4 {
		t.Fatalf("failure response = %#v", response)
	}
	if got := providerCalls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
}

func TestInboundSendInputProjectsAmbiguousOutcomeAsPending(t *testing.T) {
	var calls atomic.Int32
	var gotReceipt string
	srv := &Server{
		sendInputWithReceiptOverride: func(_ string, _ string, receipt string) error {
			calls.Add(1)
			gotReceipt = receipt
			return &watcher.InputSubmissionError{
				Result: watcher.InputResult{Outcome: watcher.InputAmbiguous},
				Cause:  errors.New("tmux queue outcome unknown"),
			}
		},
	}
	conn := openThinProxyTestSocket(t, srv)

	response := sendThinProxyRequest(t, conn, clientMessage{
		Type:      "send_input",
		RequestID: "request-durable-pending",
		AgentID:   "agent-pending",
		Text:      "preserve and resume me",
	})
	if response.Type != "input_pending" ||
		response.RequestID != "request-durable-pending" ||
		response.Code != "input_uncertain" ||
		response.FieldCount != 4 {
		t.Fatalf("pending response = %#v", response)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
	if gotReceipt != "request-durable-pending" {
		t.Fatalf("receipt = %q, want request ID", gotReceipt)
	}
}

func TestBrainAdmissionIntentFailurePreventsProviderMutation(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-host:@admit-fail"
	if err := store.SetChatState(brain.ChatState{ThreadID: "known-thread"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	var providerCalls atomic.Int32
	srv := &Server{
		brain: brain.NewService(store, nil, nil),
		sendInputWithReceiptOverride: func(agentID, text, receipt string) error {
			providerCalls.Add(1)
			if agentID != hostID || text != "persist me" || receipt != "request-admit-fail" {
				t.Fatalf("provider call = %q %q %q", agentID, text, receipt)
			}
			return nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)

	response := sendThinProxyRequest(t, conn, clientMessage{
		Type:                 "send_input",
		RequestID:            "request-admit-fail",
		AgentID:              hostID,
		Text:                 "persist me",
		DisplayBody:          "persist me",
		ConversationScopeKey: "brain-thread:unknown-thread",
	})
	if response.Type != "input_failed" ||
		response.RequestID != "request-admit-fail" ||
		response.Code != "send_input_failed" || response.FieldCount != 4 {
		t.Fatalf("intent failure response = %#v", response)
	}
	if got := providerCalls.Load(); got != 0 {
		t.Fatalf("provider calls = %d, want 0 before durable intent", got)
	}
	items, err := store.ThreadTimeline("known-thread", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("failed admission must not invent durable rows: %#v", items)
	}
}

func TestBrainAdmissionProvedNonSubmissionAbortsIntent(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-host:@not-submitted"
	threadID := "thread-not-submitted"
	requestID := "request-not-submitted"
	if err := store.SetChatState(brain.ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	var providerCalls atomic.Int32
	srv := &Server{
		brain: brain.NewService(store, nil, nil),
		sendInputWithReceiptOverride: func(_, _, receipt string) error {
			providerCalls.Add(1)
			return &watcher.InputSubmissionError{
				Result: watcher.InputResult{Outcome: watcher.InputNotSubmitted, Receipt: receipt},
				Cause:  errors.New("provider rejected before mutation"),
			}
		},
	}
	response := sendThinProxyRequest(t, openThinProxyTestSocket(t, srv), clientMessage{
		Type: "send_input", RequestID: requestID, AgentID: hostID, Text: "do not submit",
		ConversationScopeKey: "brain-thread:" + threadID,
	})
	if response.Type != "input_failed" || response.Code != "input_not_submitted" {
		t.Fatalf("not-submitted response=%#v", response)
	}
	if providerCalls.Load() != 1 {
		t.Fatalf("provider calls=%d want 1", providerCalls.Load())
	}
	if admission, found, err := store.BrainInputAdmission(requestID, threadID); err != nil || !found ||
		admission.State != brain.BrainInputAdmissionNotSubmitted || admission.SettledAt == nil {
		t.Fatalf("proved not-submitted terminal identity found=%v admission=%+v err=%v", found, admission, err)
	}
}

func TestBrainAcceptedInputReservesQueuedAttentionDespiteProjectionFailure(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-host:@projection-reservation"
	threadID := "thread-projection-reservation"
	requestID := "request-projection-reservation"
	if err := store.SetChatState(brain.ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(brain.Work{
		Title: "Queued review", Objective: "Reserve this review behind the accepted foreground response.",
		Status: brain.WorkWaiting, CompletionPolicy: brain.CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	event, created, err := store.AppendWorkEvent(brain.WorkEvent{
		WorkID: item.ID, Kind: "review.ready", DedupeKey: "review:projection-reservation",
		Summary: "A queued result is ready.", Actionable: true,
	})
	if err != nil || !created {
		t.Fatalf("queued Event=%+v created=%v err=%v", event, created, err)
	}
	// Occupy the deterministic presentation identity with incompatible bytes.
	// Lifecycle acceptance must remain authoritative and reserve Attention;
	// the existing startup projection retry continues to own this conflict.
	if _, err := store.AdmitUserMessage(threadID, hostID, requestID, "conflicting presentation body"); err != nil {
		t.Fatal(err)
	}
	watcherFixture := &brainServiceTestWatcher{
		sessions: map[string]*classifier.Agent{
			hostID: {ID: hostID, Hidden: true, State: classifier.StateRunning, PaneAlive: true},
		},
		turnStore: store,
	}
	service := brain.NewService(store, watcherFixture, nil)
	var providerCalls atomic.Int32
	srv := &Server{
		brain: service,
		sendInputWithReceiptOverride: func(agentID, text, receipt string) error {
			providerCalls.Add(1)
			if agentID != hostID || text != "continue" || receipt != requestID {
				t.Fatalf("provider call=%q %q %q", agentID, text, receipt)
			}
			return nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)
	request := clientMessage{
		Type: "send_input", RequestID: requestID, AgentID: hostID, Text: "continue",
		DisplayBody: "continue", ConversationScopeKey: "brain-thread:" + threadID,
	}
	// The queued internal Event is admitted at the idle boundary BEFORE the
	// user's message is prepared: steering can never overtake it. The
	// projection failure then keeps every retry pending without replay.
	for attempt := 0; attempt < 2; attempt++ {
		response := sendThinProxyRequest(t, conn, request)
		if response.Type != "input_pending" || response.RequestID != requestID {
			t.Fatalf("attempt %d response=%#v", attempt+1, response)
		}
	}
	if providerCalls.Load() != 1 {
		t.Fatalf("projection retry replayed provider input: calls=%d", providerCalls.Load())
	}
	admission, found, err := store.BrainInputAdmission(requestID, threadID)
	if err != nil || !found || admission.State != brain.BrainInputAdmissionAccepted {
		t.Fatalf("accepted admission found=%v row=%+v err=%v", found, admission, err)
	}
	active, err := store.CurrentHostForegroundTurn()
	if err != nil || active == nil || active.HostGeneration != "test-owned-generation" ||
		active.HostTurnID == "" {
		t.Fatalf("foreground turn active=%+v err=%v", active, err)
	}
	claims, err := store.LeasedReviewActions()
	if err != nil {
		t.Fatal(err)
	}
	for _, claim := range claims {
		if claim.DeliveredAt == nil {
			t.Fatalf("idle-boundary Event left an undelivered claim: %+v", claim)
		}
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("idle-boundary Event history: %+v err=%v", events, err)
	}
	if item2, err := store.Work(item.ID); err != nil || item2.Review == nil || item2.Review.Lease == nil || item2.Review.Lease.DeliveredAt == nil {
		t.Fatalf("idle-boundary Event was not delivered exactly once: %+v err=%v", item2, err)
	}
}

type changingProxyGenerationWatcher struct {
	*brainServiceTestWatcher
	generation string
}

func (w *changingProxyGenerationWatcher) ResolveOwnedGeneration(sessionID string) (watcher.OwnedGeneration, error) {
	if w.GetAgent(sessionID) == nil {
		return watcher.OwnedGeneration{}, fmt.Errorf("Session %s is unavailable", sessionID)
	}
	return watcher.OwnedGeneration{SessionID: sessionID, Generation: w.generation}, nil
}

func (w *changingProxyGenerationWatcher) ResolveBrainHostGeneration(sessionID string) (watcher.OwnedGeneration, error) {
	return w.ResolveOwnedGeneration(sessionID)
}

func (w *changingProxyGenerationWatcher) ProbeProviderEvidence(string) (watcher.ProviderActivityObservation, bool, error) {
	if w.generation != "host-generation-g2" {
		return watcher.ProviderActivityObservation{}, false, nil
	}
	return watcher.ProviderActivityObservation{
		ID: "host-generation-g2-activity", Status: "running", StartedAt: time.Now().UTC(),
	}, true, nil
}

func TestBrainAcceptedInputUsesPreparedGenerationWhenProviderChangesBeforeAdmit(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-host:@prepared-generation"
	threadID := "thread-prepared-generation"
	requestID := "request-prepared-generation"
	if err := store.SetChatState(brain.ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(brain.Work{
		Title: "Prepared generation review", Objective: "Reserve against the provider generation that accepted input.",
		Status: brain.WorkWaiting, CompletionPolicy: brain.CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	event, created, err := store.AppendWorkEvent(brain.WorkEvent{
		WorkID: item.ID, Kind: "review.ready", DedupeKey: "review:prepared-generation",
		Summary: "Review remains queued behind the accepted foreground turn.", Actionable: true,
	})
	if err != nil || !created {
		t.Fatalf("queued Event=%+v created=%v err=%v", event, created, err)
	}
	watcherFixture := &changingProxyGenerationWatcher{
		brainServiceTestWatcher: &brainServiceTestWatcher{
			sessions: map[string]*classifier.Agent{
				hostID: {ID: hostID, Hidden: true, State: classifier.StateRunning, PaneAlive: true},
			},
			turnStore: store,
		},
		generation: "host-generation-g1",
	}
	service := brain.NewService(store, watcherFixture, nil)
	var providerCalls atomic.Int32
	srv := &Server{
		brain: service,
		sendInputWithReceiptOverride: func(agentID, text, receipt string) error {
			providerCalls.Add(1)
			if agentID != hostID || text != "continue" || receipt != requestID {
				t.Fatalf("provider call=%q %q %q", agentID, text, receipt)
			}
			// Provider mutation succeeded on G1; the ambient pane/process
			// generation changes before Brain commits that accepted admission.
			watcherFixture.generation = "host-generation-g2"
			return nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)
	request := clientMessage{
		Type: "send_input", RequestID: requestID, AgentID: hostID, Text: "continue",
		DisplayBody: "continue", ConversationScopeKey: "brain-thread:" + threadID,
	}
	// The queued internal Event is delivered at the idle boundary before the
	// user message. The accepted G1 admission remains exact history, while the
	// reducer retires its foreground gate once G2 is observed. Both attempts
	// acknowledge without ever adopting an ambient G2 Activity for G1.
	first := sendThinProxyRequest(t, conn, request)
	if first.Type != "input_sent" || first.RequestID != requestID {
		t.Fatalf("first response=%#v", first)
	}
	second := sendThinProxyRequest(t, conn, request)
	if second.Type != "input_sent" || second.RequestID != requestID {
		t.Fatalf("idempotent accepted retry response=%#v", second)
	}
	if providerCalls.Load() != 1 {
		t.Fatalf("provider submissions=%d want exactly one", providerCalls.Load())
	}
	admission, found, err := store.BrainInputAdmission(requestID, threadID)
	if err != nil || !found || admission.State != brain.BrainInputAdmissionAccepted ||
		admission.HostGeneration != "host-generation-g1" {
		t.Fatalf("accepted prepared admission found=%v row=%+v err=%v", found, admission, err)
	}
	active, err := store.CurrentHostForegroundTurn()
	if err != nil || active != nil {
		t.Fatalf("superseded prepared generation remained active=%+v err=%v", active, err)
	}
	claims, err := store.LeasedReviewActions()
	if err != nil {
		t.Fatal(err)
	}
	for _, claim := range claims {
		if claim.DeliveredAt == nil {
			t.Fatalf("idle-boundary Event left an undelivered claim: %+v", claim)
		}
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("idle-boundary Event history: %+v err=%v", events, err)
	}
	if item2, err := store.Work(item.ID); err != nil || item2.Review == nil || item2.Review.Lease == nil || item2.Review.Lease.DeliveredAt == nil {
		t.Fatalf("idle-boundary Event was not delivered exactly once: %+v err=%v", item2, err)
	}
}

func TestBrainAdmissionAmbiguousAndAcceptedDuplicatesNeverReplayProviderInput(t *testing.T) {
	for _, test := range []struct {
		name         string
		providerErr  error
		wantResponse string
		wantState    brain.BrainInputAdmissionState
	}{
		{
			name: "ambiguous terminal", wantResponse: "input_pending", wantState: brain.BrainInputAdmissionUncertain,
			providerErr: &watcher.InputSubmissionError{
				Result: watcher.InputResult{Outcome: watcher.InputAmbiguous},
				Cause:  errors.New("provider acceptance unknown"),
			},
		},
		{name: "accepted", wantResponse: "input_sent", wantState: brain.BrainInputAdmissionAccepted},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := brain.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			hostID := "brain-host:@duplicate"
			threadID := "thread-duplicate"
			requestID := "request-duplicate"
			if err := store.SetChatState(brain.ChatState{ThreadID: threadID}); err != nil {
				t.Fatal(err)
			}
			if err := store.SetHostSession(hostID, "codex"); err != nil {
				t.Fatal(err)
			}
			var providerCalls atomic.Int32
			srv := &Server{
				brain: brain.NewService(store, nil, nil),
				sendInputWithReceiptOverride: func(_, _, _ string) error {
					providerCalls.Add(1)
					return test.providerErr
				},
			}
			conn := openThinProxyTestSocket(t, srv)
			request := clientMessage{
				Type: "send_input", RequestID: requestID, AgentID: hostID, Text: "submit exactly once",
				ConversationScopeKey: "brain-thread:" + threadID,
			}
			for attempt := 0; attempt < 2; attempt++ {
				response := sendThinProxyRequest(t, conn, request)
				if response.Type != test.wantResponse {
					t.Fatalf("attempt %d response=%#v want=%s", attempt+1, response, test.wantResponse)
				}
			}
			if providerCalls.Load() != 1 {
				t.Fatalf("duplicate provider calls=%d want 1", providerCalls.Load())
			}
			admission, found, err := store.BrainInputAdmission(requestID, threadID)
			if err != nil || !found || admission.State != test.wantState {
				t.Fatalf("admission found=%v row=%+v err=%v want state=%s", found, admission, err, test.wantState)
			}
		})
	}
}

func TestNonBrainSendInputStillAcknowledgesAfterProviderAccept(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(brain.ChatState{ThreadID: "brain-thread"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession("brain-host:@1", "codex"); err != nil {
		t.Fatal(err)
	}
	var providerCalls atomic.Int32
	srv := &Server{
		brain: brain.NewService(store, nil, nil),
		sendInputWithReceiptOverride: func(agentID, text, receipt string) error {
			providerCalls.Add(1)
			if agentID != "ordinary-session" || receipt != "request-non-brain" {
				t.Fatalf("provider call = %q %q", agentID, receipt)
			}
			return nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)

	response := sendThinProxyRequest(t, conn, clientMessage{
		Type:      "send_input",
		RequestID: "request-non-brain",
		AgentID:   "ordinary-session",
		Text:      "hello ordinary",
	})
	if response.Type != "input_sent" ||
		response.RequestID != "request-non-brain" ||
		response.FieldCount != 2 {
		t.Fatalf("non-Brain response = %#v", response)
	}
	if got := providerCalls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
}

func TestServerSendInputWithReceiptForwardsExactRawText(t *testing.T) {
	var gotAgent, gotText, gotReceipt string
	srv := &Server{sendInputWithReceiptOverride: func(agentID, text, receipt string) error {
		gotAgent, gotText, gotReceipt = agentID, text, receipt
		return nil
	}}
	conn := openThinProxyTestSocket(t, srv)
	response := sendThinProxyRequest(t, conn, clientMessage{
		Type:      "send_input",
		RequestID: "request-raw-text",
		AgentID:   "owned-session:@1",
		Text:      "hello\nraw text",
	})
	if response.Type != "input_sent" || response.RequestID != "request-raw-text" {
		t.Fatalf("WebSocket send_input response = %#v", response)
	}
	if gotAgent != "owned-session:@1" || gotText != "hello\nraw text" || gotReceipt != "request-raw-text" {
		t.Fatalf("forwarded input = agent %q text %q receipt %q", gotAgent, gotText, gotReceipt)
	}
}

func TestPauseActsDirectlyOnCurrentExecutorState(t *testing.T) {
	var calls atomic.Int32
	actions := make(chan thinProxyInputCall, 1)
	srv := &Server{
		sendActionOverride: func(agentID, action string) error {
			calls.Add(1)
			actions <- thinProxyInputCall{agentID: agentID, text: action}
			return nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)

	response := sendThinProxyRequest(t, conn, clientMessage{
		Type:      "send_action",
		RequestID: "stop-request",
		AgentID:   "agent-1",
		Action:    "pause",
	})
	if response.Type != "action_sent" || response.RequestID != "stop-request" ||
		response.FieldCount != 2 {
		t.Fatalf("pause response = %#v", response)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("pause provider calls = %d, want 1", got)
	}
	if call := <-actions; call != (thinProxyInputCall{agentID: "agent-1", text: "pause"}) {
		t.Fatalf("pause provider call = %#v", call)
	}
}

func TestPauseFailureReturnsCorrelatedFailureWithoutSentAck(t *testing.T) {
	wantErr := errors.New("executor could not stop")
	var calls atomic.Int32
	srv := &Server{
		sendActionOverride: func(string, string) error {
			calls.Add(1)
			return wantErr
		},
	}
	conn := openThinProxyTestSocket(t, srv)

	response := sendThinProxyRequest(t, conn, clientMessage{
		Type:      "send_action",
		RequestID: "failed-stop",
		AgentID:   "agent-1",
		Action:    "pause",
	})
	if response.Type == "action_sent" {
		t.Fatalf("failed provider action was acknowledged: %#v", response)
	}
	if response.Type != "action_failed" || response.RequestID != "failed-stop" ||
		response.Code != "send_action_failed" || response.Message != wantErr.Error() ||
		response.FieldCount != 4 {
		t.Fatalf("failure response = %#v", response)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
}

func openThinProxyTestSocket(t *testing.T, srv *Server) *websocket.Conn {
	t.Helper()
	registered := make(chan struct{})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		srv.mu.Lock()
		if srv.writes == nil {
			srv.writes = make(map[*websocket.Conn]*sync.Mutex)
		}
		if srv.codexSubs == nil {
			srv.codexSubs = make(map[*websocket.Conn]map[string]codexConversationSubscription)
		}
		srv.writes[conn] = &sync.Mutex{}
		srv.mu.Unlock()
		close(registered)
		defer func() {
			srv.mu.Lock()
			for _, subscription := range srv.codexSubs[conn] {
				subscription.cancel()
			}
			delete(srv.codexSubs, conn)
			delete(srv.writes, conn)
			srv.mu.Unlock()
			_ = conn.Close()
		}()
		for {
			_, message, readErr := conn.ReadMessage()
			if readErr != nil {
				return
			}
			srv.handleClientMessage(conn, message)
		}
	}))
	t.Cleanup(httpServer.Close)

	url := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial test WebSocket: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	select {
	case <-registered:
	case <-time.After(2 * time.Second):
		t.Fatal("test WebSocket was not registered")
	}
	return conn
}

func sendThinProxyRequest(t *testing.T, conn *websocket.Conn, request clientMessage) thinProxyResponse {
	t.Helper()
	if err := conn.WriteJSON(request); err != nil {
		t.Fatalf("write WebSocket request: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read WebSocket response: %v", err)
	}
	var response thinProxyResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatalf("decode WebSocket response: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatalf("decode WebSocket response fields: %v", err)
	}
	response.FieldCount = len(fields)
	return response
}

func TestSendInputStaleReceiptMismatchReturnsTypedCodeAndFreesTheAdmission(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-host:@stale-receipt"
	threadID := "thread-stale-receipt"
	requestID := "request-stale-receipt"
	if err := store.SetChatState(brain.ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	watcherFixture := &brainServiceTestWatcher{
		sessions: map[string]*classifier.Agent{
			hostID: {ID: hostID, Hidden: true, State: classifier.StateRunning, PaneAlive: true},
		},
		turnStore: store,
	}
	service := brain.NewService(store, watcherFixture, nil)
	var providerCalls atomic.Int32
	srv := &Server{
		brain: service,
		sendInputWithReceiptOverride: func(_, _, receipt string) error {
			providerCalls.Add(1)
			if receipt == requestID {
				// The durable receipt ledger already binds this receipt to a
				// different payload (legacy/stale binding): the watcher fails
				// closed strictly before provider mutation.
				return &watcher.InputSubmissionError{
					Result: watcher.InputResult{Outcome: watcher.InputNotSubmitted, Receipt: receipt},
					Cause:  fmt.Errorf("%w", watcher.ErrReceiptBelongsToDifferentInput),
				}
			}
			return nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)
	request := clientMessage{
		Type: "send_input", RequestID: requestID, AgentID: hostID, Text: "edited message",
		DisplayBody: "edited message", ConversationScopeKey: "brain-thread:" + threadID,
	}
	response := sendThinProxyRequest(t, conn, request)
	if response.Type != "input_failed" || response.Code != "stale_receipt_invalidated" ||
		response.RequestID != requestID {
		t.Fatalf("stale mismatch response=%#v", response)
	}
	admission, found, err := store.BrainInputAdmission(requestID, threadID)
	if err != nil || !found || admission.State != brain.BrainInputAdmissionNotSubmitted {
		t.Fatalf("stale mismatch admission found=%v row=%+v err=%v", found, admission, err)
	}
	if providerCalls.Load() != 1 {
		t.Fatalf("provider calls=%d want exactly one fail-closed attempt", providerCalls.Load())
	}
	// The app discards the stale identity and resubmits the same logical input
	// with a fresh identity: a new admission is prepared and the provider
	// mutation boundary is crossed exactly once more.
	freshRequestID := "request-stale-receipt-fresh"
	fresh := sendThinProxyRequest(t, conn, clientMessage{
		Type: "send_input", RequestID: freshRequestID, AgentID: hostID, Text: "edited message",
		DisplayBody: "edited message", ConversationScopeKey: "brain-thread:" + threadID,
	})
	if fresh.Type != "input_sent" || fresh.RequestID != freshRequestID {
		t.Fatalf("fresh-identity retry response=%#v", fresh)
	}
	if providerCalls.Load() != 2 {
		t.Fatalf("fresh-identity retry provider calls=%d want 2", providerCalls.Load())
	}
	freshAdmission, found, err := store.BrainInputAdmission(freshRequestID, threadID)
	if err != nil || !found || freshAdmission.State != brain.BrainInputAdmissionAccepted {
		t.Fatalf("fresh-identity admission found=%v row=%+v err=%v", found, freshAdmission, err)
	}
	// The stale identity stays terminal at the payload-binding guard: reusing
	// it re-arms the never-mutated admission but still fails closed before any
	// provider mutation. The app-side auto-retry bound is what stops the loop.
	staleAgain := sendThinProxyRequest(t, conn, request)
	if staleAgain.Type != "input_failed" || staleAgain.Code != "stale_receipt_invalidated" {
		t.Fatalf("stale identity reuse response=%#v", staleAgain)
	}
}

func TestSendInputLaneErrorNeverBlocksForegroundSend(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-host:@lane-error"
	threadID := "thread-lane-error"
	requestID := "request-lane-error"
	if err := store.SetChatState(brain.ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	// The lane reducer fails hard (host liveness probe error) before the
	// foreground send. Background reconciliation errors must never block an
	// unrelated foreground user message.
	watcherFixture := &failingLaneWatcher{
		brainServiceTestWatcher: &brainServiceTestWatcher{
			sessions: map[string]*classifier.Agent{
				hostID: {ID: hostID, Hidden: true, State: classifier.StateRunning, PaneAlive: true},
			},
			turnStore: store,
		},
	}
	service := brain.NewService(store, watcherFixture, nil)
	var providerCalls atomic.Int32
	srv := &Server{
		brain: service,
		sendInputWithReceiptOverride: func(agentID, text, receipt string) error {
			providerCalls.Add(1)
			if agentID != hostID || text != "user message" || receipt != requestID {
				t.Fatalf("provider call=%q %q %q", agentID, text, receipt)
			}
			return nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)
	response := sendThinProxyRequest(t, conn, clientMessage{
		Type: "send_input", RequestID: requestID, AgentID: hostID, Text: "user message",
		DisplayBody: "user message", ConversationScopeKey: "brain-thread:" + threadID,
	})
	if response.Type != "input_sent" || response.RequestID != requestID {
		t.Fatalf("foreground send behind lane error response=%#v", response)
	}
	if providerCalls.Load() != 1 {
		t.Fatalf("provider calls=%d want 1", providerCalls.Load())
	}
	admission, found, err := store.BrainInputAdmission(requestID, threadID)
	if err != nil || !found || admission.State != brain.BrainInputAdmissionAccepted {
		t.Fatalf("admission found=%v row=%+v err=%v", found, admission, err)
	}
}

func TestSendInputSameIdentityRetryAfterNotSubmittedRearmsAndSubmits(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-host:@retry-identity"
	threadID := "thread-retry-identity"
	requestID := "request-retry-identity"
	if err := store.SetChatState(brain.ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	watcherFixture := &brainServiceTestWatcher{
		sessions: map[string]*classifier.Agent{
			hostID: {ID: hostID, Hidden: true, State: classifier.StateRunning, PaneAlive: true},
		},
		turnStore: store,
	}
	service := brain.NewService(store, watcherFixture, nil)
	failFirst := atomic.Bool{}
	failFirst.Store(true)
	var providerCalls atomic.Int32
	srv := &Server{
		brain: service,
		sendInputWithReceiptOverride: func(agentID, text, receipt string) error {
			providerCalls.Add(1)
			if agentID != hostID || text != "same message" || receipt != requestID {
				t.Fatalf("provider call=%q %q %q", agentID, text, receipt)
			}
			if failFirst.CompareAndSwap(true, false) {
				return definitelyNotSubmittedTestError(receipt)
			}
			return nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)
	request := clientMessage{
		Type: "send_input", RequestID: requestID, AgentID: hostID, Text: "same message",
		DisplayBody: "same message", ConversationScopeKey: "brain-thread:" + threadID,
	}
	first := sendThinProxyRequest(t, conn, request)
	if first.Type != "input_failed" || first.Code != "input_not_submitted" {
		t.Fatalf("first response=%#v", first)
	}
	admission, found, err := store.BrainInputAdmission(requestID, threadID)
	if err != nil || !found || admission.State != brain.BrainInputAdmissionNotSubmitted {
		t.Fatalf("first admission found=%v row=%+v err=%v", found, admission, err)
	}
	// Retrying the exact logical input reuses its identity: the daemon re-arms
	// the never-mutated admission and submits exactly once more.
	retry := sendThinProxyRequest(t, conn, request)
	if retry.Type != "input_sent" || retry.RequestID != requestID {
		t.Fatalf("retry response=%#v", retry)
	}
	if providerCalls.Load() != 2 {
		t.Fatalf("provider calls=%d want exactly two (one per mutation boundary)", providerCalls.Load())
	}
	admission, found, err = store.BrainInputAdmission(requestID, threadID)
	if err != nil || !found || admission.State != brain.BrainInputAdmissionAccepted {
		t.Fatalf("retry admission found=%v row=%+v err=%v", found, admission, err)
	}
}

type failingLaneWatcher struct {
	*brainServiceTestWatcher
}

func (w *failingLaneWatcher) ProbeSession(string) (watcher.SessionPresence, error) {
	return watcher.SessionPresenceUnknown, errors.New("host liveness probe failed")
}
