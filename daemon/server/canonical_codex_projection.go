package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/chatthread"
	"github.com/daoleno/zen/daemon/work"
)

// canonicalCodexProjection is the sole owner of user-visible Codex ordering
// and lifecycle. The v1 structured-turn registry remains a dispatch/control
// adapter, but its queue promotion is deliberately absent from this model.
type canonicalCodexProjection struct {
	mu     sync.Mutex
	scopes map[string]*canonicalCodexScope
}

type canonicalCodexScope struct {
	identity string
	thread   *chatthread.Projector

	source             work.CodexConversation
	providerBindings   map[string]chatthread.SubmissionID
	providerUserEvents map[chatthread.SubmissionID]work.CodexConversationEvent
	eventCauses        map[string]chatthread.SubmissionID
	hiddenSubmissions  map[chatthread.SubmissionID]struct{}
	appSubmissions     map[chatthread.SubmissionID]canonicalCodexSubmissionMetadata

	revision         uint64
	lastFingerprint  string
	lastConversation work.CodexConversation
	hadProviderState bool
}

type canonicalCodexSubmission struct {
	ID        string
	Body      string
	StartedAt time.Time
	AttemptID string
}

type canonicalCodexSubmissionMetadata struct {
	startedAt   time.Time
	queued      bool
	rejected    bool
	dispatchErr error
	acceptance  structuredInputAcceptance
}

type canonicalCodexProjectionSnapshot struct {
	Conversation work.CodexConversation
	Revision     uint64
	Replace      bool
}

func newCanonicalCodexProjection() *canonicalCodexProjection {
	return &canonicalCodexProjection{scopes: make(map[string]*canonicalCodexScope)}
}

func newCanonicalCodexScope(scope, identity string) *canonicalCodexScope {
	threadID := strings.TrimSpace(scope)
	if threadID == "" {
		threadID = "codex-conversation"
	}
	if identity != "" {
		threadID += ":" + identity
	}
	projector, err := chatthread.NewProjector(chatthread.ThreadID(threadID))
	if err != nil {
		// threadID is constructed from a nonempty constant/scope, so this is an
		// invariant failure rather than a transport fallback.
		panic(err)
	}
	return &canonicalCodexScope{
		identity:           identity,
		thread:             projector,
		providerBindings:   make(map[string]chatthread.SubmissionID),
		providerUserEvents: make(map[chatthread.SubmissionID]work.CodexConversationEvent),
		eventCauses:        make(map[string]chatthread.SubmissionID),
		hiddenSubmissions:  make(map[chatthread.SubmissionID]struct{}),
		appSubmissions:     make(map[chatthread.SubmissionID]canonicalCodexSubmissionMetadata),
	}
}

func (projection *canonicalCodexProjection) scopeLocked(scope, identity string) (*canonicalCodexScope, bool) {
	if projection.scopes == nil {
		projection.scopes = make(map[string]*canonicalCodexScope)
	}
	key := strings.TrimSpace(scope)
	if key == "" {
		key = "codex-conversation"
	}
	state := projection.scopes[key]
	if state == nil {
		state = newCanonicalCodexScope(key, identity)
		projection.scopes[key] = state
		return state, true
	}
	if identity != "" && state.identity != "" && identity != state.identity {
		revision := state.revision
		state = newCanonicalCodexScope(key, identity)
		state.revision = revision
		projection.scopes[key] = state
		return state, true
	}
	if state.identity == "" && identity != "" {
		state.identity = identity
	}
	return state, false
}

// acceptWithDispatch serializes canonical acceptance with the one legacy
// dispatch callback. Exact retries never invoke the callback again, and body
// text never participates in row lookup.
func (projection *canonicalCodexProjection) acceptWithDispatch(
	scope string,
	submission canonicalCodexSubmission,
	dispatch func() (structuredInputAcceptance, error),
) (structuredInputAcceptance, error) {
	projection.mu.Lock()
	defer projection.mu.Unlock()

	state, _ := projection.scopeLocked(scope, "")
	submissionID := chatthread.SubmissionID(strings.TrimSpace(submission.ID))
	if submissionID == "" {
		return structuredInputAcceptance{}, fmt.Errorf("canonical Submission ID is empty")
	}

	if existing, ok := state.appSubmissions[submissionID]; ok {
		threadSubmission := canonicalSubmissionByID(state.thread.Snapshot(), submissionID)
		if threadSubmission == nil || threadSubmission.Payload.Body != submission.Body {
			return structuredInputAcceptance{}, fmt.Errorf("canonical Submission %q conflicts with immutable payload", submissionID)
		}
		if existing.rejected {
			acceptance, err := dispatch()
			existing.acceptance = acceptance
			existing.rejected = canonicalDispatchKnownRejected(err)
			existing.dispatchErr = err
			existing.queued = err == nil && acceptance.Queued
			state.appSubmissions[submissionID] = existing
			_, revision := state.refreshVisibleLocked(false)
			acceptance.Position = uint64(threadSubmission.Position)
			acceptance.ConversationRevision = revision
			if acceptance.Revision == 0 {
				acceptance.Revision = int64(revision)
			}
			existing.acceptance = acceptance
			state.appSubmissions[submissionID] = existing
			return acceptance, err
		}
		acceptance := existing.acceptance
		if existing.dispatchErr != nil {
			// Dispatch may already have crossed the executor boundary. Preserve the
			// same uncertain outcome for this exact ID without invoking it again.
			return acceptance, existing.dispatchErr
		}
		acceptance.Duplicate = true
		acceptance.Position = uint64(threadSubmission.Position)
		acceptance.ConversationRevision = state.revision
		if acceptance.Revision == 0 {
			acceptance.Revision = int64(state.revision)
		}
		return acceptance, nil
	}

	if _, err := state.thread.Accept(chatthread.AcceptSubmissionCommand{
		SubmissionID: submissionID,
		Origin:       chatthread.OriginApp,
		Payload: chatthread.SubmissionPayload{
			Body: submission.Body,
		},
	}); err != nil {
		return structuredInputAcceptance{}, err
	}
	attemptID := strings.TrimSpace(submission.AttemptID)
	if attemptID == "" {
		attemptID = "dispatch:" + string(submissionID)
	}
	if _, err := state.thread.BeginDelivery(chatthread.BeginDeliveryCommand{
		SubmissionID: submissionID,
		AttemptID:    chatthread.DispatchAttemptID(attemptID),
	}); err != nil {
		return structuredInputAcceptance{}, err
	}

	acceptance, err := dispatch()
	metadata := canonicalCodexSubmissionMetadata{
		startedAt:   submission.StartedAt,
		acceptance:  acceptance,
		rejected:    canonicalDispatchKnownRejected(err),
		dispatchErr: err,
	}
	if metadata.startedAt.IsZero() {
		metadata.startedAt = time.Now()
	}
	if err == nil {
		metadata.queued = acceptance.Queued
	}
	state.appSubmissions[submissionID] = metadata
	conversation, revision := state.refreshVisibleLocked(false)
	_ = conversation

	threadSubmission := canonicalSubmissionByID(state.thread.Snapshot(), submissionID)
	if threadSubmission != nil {
		acceptance.Position = uint64(threadSubmission.Position)
	}
	acceptance.ConversationRevision = revision
	if acceptance.Revision == 0 {
		acceptance.Revision = int64(revision)
	}
	metadata.acceptance = acceptance
	state.appSubmissions[submissionID] = metadata
	return acceptance, err
}

func canonicalDispatchKnownRejected(err error) bool {
	if err == nil {
		return false
	}
	var rejected *structuredInputRejectedError
	return errors.As(err, &rejected)
}

func (projection *canonicalCodexProjection) project(
	scope string,
	conversation work.CodexConversation,
) canonicalCodexProjectionSnapshot {
	projection.mu.Lock()
	defer projection.mu.Unlock()

	identity := structuredConversationThreadIdentity(conversation)
	if strings.HasPrefix(strings.TrimSpace(scope), "scope:") {
		// A scoped Brain/thread owner survives hidden-host or provider session
		// replacement. Agent-scoped Work still follows provider thread identity.
		identity = strings.TrimSpace(scope)
	}
	state, replaced := projection.scopeLocked(scope, identity)
	if canonicalCodexTransientAbsence(conversation) && state.lastFingerprint != "" {
		visible := state.lastConversation
		visible.Available = false
		visible.Reason = conversation.Reason
		return canonicalCodexProjectionSnapshot{
			Conversation: visible,
			Revision:     state.revision,
			Replace:      false,
		}
	}

	explicitEmpty := conversation.Available && conversation.Turn == nil &&
		len(conversation.ProviderTurns) == 0 && len(conversation.Events) == 0
	if explicitEmpty && state.hadProviderState {
		revision := state.revision
		key := strings.TrimSpace(scope)
		if key == "" {
			key = "codex-conversation"
		}
		state = newCanonicalCodexScope(key, identity)
		state.revision = revision
		projection.scopes[key] = state
		replaced = true
	}

	state.source = cloneCodexConversation(conversation)
	if !explicitEmpty {
		state.projectProviderConversationLocked(conversation)
	}
	if conversation.Turn != nil || len(conversation.ProviderTurns) > 0 || len(conversation.Events) > 0 {
		state.hadProviderState = true
	}
	visible, revision := state.refreshVisibleLocked(replaced)
	return canonicalCodexProjectionSnapshot{
		Conversation: visible,
		Revision:     revision,
		Replace:      replaced,
	}
}

func canonicalCodexTransientAbsence(conversation work.CodexConversation) bool {
	if conversation.Available {
		return false
	}
	switch strings.TrimSpace(conversation.Reason) {
	case "transcript_not_found", "terminal_snapshot_unavailable", "agent_not_found", "structured_lifecycle_syncing":
		return true
	default:
		return false
	}
}

func (state *canonicalCodexScope) projectProviderConversationLocked(conversation work.CodexConversation) {
	turns := append([]work.CodexConversationTurn{}, conversation.ProviderTurns...)
	if conversation.Turn != nil {
		found := false
		for index := range turns {
			if turns[index].ID == conversation.Turn.ID {
				turns[index] = *conversation.Turn
				found = true
				break
			}
		}
		if !found {
			turns = append(turns, *conversation.Turn)
		}
	}

	turnByID := make(map[string]work.CodexConversationTurn, len(turns))
	for _, turn := range turns {
		turnByID[strings.TrimSpace(turn.ID)] = turn
	}

	events := append([]work.CodexConversationEvent{}, conversation.Events...)
	sort.SliceStable(events, func(left, right int) bool {
		if events[left].Seq != events[right].Seq {
			return events[left].Seq < events[right].Seq
		}
		return events[left].ID < events[right].ID
	})
	currentActivityID := ""
	settleBeforeNext := func(activityID string) {
		if turn, ok := turnByID[activityID]; ok && isStructuredTurnTerminal(turn.Status) {
			state.settleActivityLocked(turn)
			return
		}
		if strings.HasPrefix(activityID, "provider-history:") {
			state.settleSyntheticActivityLocked(activityID)
			return
		}
		// Seeing a distinct later provider Activity is itself terminal evidence
		// for an older Activity whose explicit terminal record fell outside the
		// bounded parser tail.
		state.settleActivityLocked(work.CodexConversationTurn{
			ID: activityID, Status: work.CodexConversationTurnCompleted,
		})
	}
	for _, event := range events {
		activityID := strings.TrimSpace(event.ActivityID)
		if activityID == "" && conversation.Turn != nil {
			activityID = strings.TrimSpace(conversation.Turn.ID)
		}
		if activityID == "" {
			activityID = canonicalSyntheticActivityID(conversation)
		}
		if currentActivityID != "" && activityID != currentActivityID {
			settleBeforeNext(currentActivityID)
		}
		state.ensureActivityLocked(activityID)
		currentActivityID = activityID
		if event.Kind == "user_message" || event.Role == "user" {
			state.admitProviderUserLocked(activityID, event)
			continue
		}
		state.upsertProviderEventLocked(activityID, event)
	}

	if currentActivityID != "" {
		if turn, ok := turnByID[currentActivityID]; ok && isStructuredTurnTerminal(turn.Status) {
			state.settleActivityLocked(turn)
		} else if strings.HasPrefix(currentActivityID, "provider-history:") {
			state.settleSyntheticActivityLocked(currentActivityID)
		}
	}
	if conversation.Turn != nil {
		// A zero-effect provider Activity has no causal input or event to own in
		// the Thread. Starting it here would create an Activity that cannot be
		// terminally settled under the domain invariant and would block the next
		// real Activity. The raw provider Turn still drives visible lifecycle.
		activity := canonicalActivityByID(
			state.thread.Snapshot(),
			chatthread.ExecutionID(conversation.Turn.ID),
		)
		if activity != nil && isStructuredTurnTerminal(conversation.Turn.Status) {
			state.settleActivityLocked(*conversation.Turn)
		}
	}
}

func canonicalSyntheticActivityID(conversation work.CodexConversation) string {
	identity := structuredConversationThreadIdentity(conversation)
	if identity == "" {
		identity = "unknown"
	}
	return "provider-history:" + identity
}

func (state *canonicalCodexScope) ensureActivityLocked(activityID string) {
	activityID = strings.TrimSpace(activityID)
	if activityID == "" {
		return
	}
	thread := state.thread.Snapshot()
	for _, activity := range thread.ExecutionActivities {
		if string(activity.ID) == activityID {
			return
		}
	}
	if thread.CurrentExecutionID != "" {
		return
	}
	_, _ = state.thread.Apply(chatthread.ActivityStartedFact{
		Key:         chatthread.ProviderFactKey("activity:start:" + activityID),
		ExecutionID: chatthread.ExecutionID(activityID),
	})
}

func (state *canonicalCodexScope) admitProviderUserLocked(
	activityID string,
	event work.CodexConversationEvent,
) {
	providerID := strings.TrimSpace(event.ID)
	if providerID == "" {
		providerID = fmt.Sprintf("provider-user:%d", event.Seq)
	}
	if submissionID, exists := state.providerBindings[providerID]; exists {
		state.providerUserEvents[submissionID] = event
		return
	}

	thread := state.thread.Snapshot()
	var submissionID chatthread.SubmissionID
	for _, submission := range thread.Submissions {
		if submission.Origin != chatthread.OriginApp || submission.Delivery == chatthread.DeliveryDelivered {
			continue
		}
		if metadata, ok := state.appSubmissions[submission.ID]; ok && metadata.rejected {
			// A structuredInputRejectedError proves the effect never crossed the
			// provider boundary. It cannot consume a later provider admission; an
			// unknown dispatch outcome deliberately remains eligible here.
			continue
		}
		submissionID = submission.ID
		break
	}
	if submissionID == "" {
		submissionID = chatthread.SubmissionID(providerID)
		if canonicalSubmissionByID(thread, submissionID) != nil {
			submissionID = chatthread.SubmissionID("provider:" + providerID)
		}
		if _, err := state.thread.Accept(chatthread.AcceptSubmissionCommand{
			SubmissionID: submissionID,
			Origin:       chatthread.OriginProviderExternal,
			Payload: chatthread.SubmissionPayload{
				Body: event.Body,
			},
		}); err != nil {
			return
		}
		thread = state.thread.Snapshot()
	}

	activity := canonicalActivityByID(thread, chatthread.ExecutionID(activityID))
	if activity == nil {
		return
	}
	ordinal := activity.InputCount + 1
	if _, err := state.thread.Apply(chatthread.InputAdmittedFact{
		Key:          chatthread.ProviderFactKey("input:" + activityID + ":" + providerID),
		ExecutionID:  chatthread.ExecutionID(activityID),
		SubmissionID: submissionID,
		Ordinal:      ordinal,
	}); err != nil {
		return
	}
	state.providerBindings[providerID] = submissionID
	state.providerUserEvents[submissionID] = event
}

func (state *canonicalCodexScope) ensureCausalSubmissionLocked(activityID, eventID string) chatthread.SubmissionID {
	thread := state.thread.Snapshot()
	var latest chatthread.SubmissionID
	for _, submission := range thread.Submissions {
		if submission.Delivery == chatthread.DeliveryDelivered && string(submission.ExecutionID) == activityID {
			latest = submission.ID
		}
	}
	if latest != "" {
		return latest
	}

	submissionID := chatthread.SubmissionID("cause:" + activityID)
	if canonicalSubmissionByID(thread, submissionID) == nil {
		if _, err := state.thread.Accept(chatthread.AcceptSubmissionCommand{
			SubmissionID: submissionID,
			Origin:       chatthread.OriginProviderExternal,
			Payload:      chatthread.SubmissionPayload{},
		}); err != nil {
			return ""
		}
		state.hiddenSubmissions[submissionID] = struct{}{}
	}
	thread = state.thread.Snapshot()
	activity := canonicalActivityByID(thread, chatthread.ExecutionID(activityID))
	if activity == nil {
		return ""
	}
	if submission := canonicalSubmissionByID(thread, submissionID); submission != nil &&
		submission.Delivery != chatthread.DeliveryDelivered {
		if _, err := state.thread.Apply(chatthread.InputAdmittedFact{
			Key:          chatthread.ProviderFactKey("input:" + activityID + ":synthetic:" + eventID),
			ExecutionID:  chatthread.ExecutionID(activityID),
			SubmissionID: submissionID,
			Ordinal:      activity.InputCount + 1,
		}); err != nil {
			return ""
		}
	}
	return submissionID
}

func (state *canonicalCodexScope) upsertProviderEventLocked(
	activityID string,
	event work.CodexConversationEvent,
) {
	eventID := strings.TrimSpace(event.ID)
	if eventID == "" {
		eventID = fmt.Sprintf("provider-event:%d", event.Seq)
	}
	cause := state.eventCauses[eventID]
	if cause == "" {
		cause = state.ensureCausalSubmissionLocked(activityID, eventID)
		if cause == "" {
			return
		}
		state.eventCauses[eventID] = cause
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	fingerprint := sha256.Sum256(payload)
	factKey := "event:" + eventID + ":" + hex.EncodeToString(fingerprint[:8])
	if _, err := state.thread.Apply(chatthread.EventUpsertFact{
		Key:                chatthread.ProviderFactKey(factKey),
		EventID:            chatthread.EventID(eventID),
		ExecutionID:        chatthread.ExecutionID(activityID),
		CausalSubmissionID: cause,
		Kind:               canonicalThreadEventKind(event),
		Final:              false,
		Payload:            string(payload),
	}); err != nil {
		return
	}
}

func canonicalThreadEventKind(event work.CodexConversationEvent) chatthread.EventKind {
	switch event.Kind {
	case "assistant_message":
		return chatthread.EventAssistant
	case "plan":
		return chatthread.EventPlan
	case "command", "tool", "patch", "web_search":
		return chatthread.EventTool
	default:
		return chatthread.EventStatus
	}
}

func (state *canonicalCodexScope) settleActivityLocked(turn work.CodexConversationTurn) {
	activityID := strings.TrimSpace(turn.ID)
	if activityID == "" {
		return
	}
	state.ensureActivityLocked(activityID)
	thread := state.thread.Snapshot()
	activity := canonicalActivityByID(thread, chatthread.ExecutionID(activityID))
	if activity == nil || activity.State != chatthread.ActivityRunning || activity.InputCount == 0 {
		return
	}
	_, _ = state.thread.Apply(chatthread.ActivityTerminalFact{
		Key:           chatthread.ProviderFactKey("activity:terminal:" + activityID + ":" + turn.Status),
		ExecutionID:   chatthread.ExecutionID(activityID),
		TerminalState: canonicalTerminalActivityState(turn.Status),
		Reason:        turn.Status,
	})
}

func (state *canonicalCodexScope) settleSyntheticActivityLocked(activityID string) {
	state.settleActivityLocked(work.CodexConversationTurn{
		ID:     activityID,
		Status: work.CodexConversationTurnCompleted,
	})
}

func canonicalTerminalActivityState(status string) chatthread.ActivityState {
	switch status {
	case work.CodexConversationTurnFailed:
		return chatthread.ActivityFailed
	case work.CodexConversationTurnInterrupted:
		return chatthread.ActivityInterrupted
	case work.CodexConversationTurnCancelled:
		return chatthread.ActivityCancelled
	default:
		return chatthread.ActivityCompleted
	}
}

func (state *canonicalCodexScope) refreshVisibleLocked(replace bool) (work.CodexConversation, uint64) {
	conversation := cloneCodexConversation(state.source)
	thread := state.thread.Snapshot()
	conversation.Events = make([]work.CodexConversationEvent, 0, len(thread.Submissions)+len(thread.Events))
	conversation.QueuedTurns = []work.CodexConversationTurn{}

	for _, submission := range thread.Submissions {
		if _, hidden := state.hiddenSubmissions[submission.ID]; hidden {
			continue
		}
		event := work.CodexConversationEvent{
			ID:              string(submission.ID),
			Seq:             int(submission.Position),
			Kind:            "user_message",
			Role:            "user",
			Body:            submission.Payload.Body,
			Source:          firstNonEmptyString(conversation.Source, "codex_rollout"),
			Position:        uint64(submission.Position),
			EventRevision:   uint64(submission.AcceptedRevision),
			ActivityID:      string(submission.ExecutionID),
			SubmissionID:    string(submission.ID),
			SubmissionState: canonicalSubmissionState(submission, state.appSubmissions[submission.ID]),
		}
		if providerEvent, ok := state.providerUserEvents[submission.ID]; ok {
			event.Timestamp = providerEvent.Timestamp
			event.Title = providerEvent.Title
			if event.Body == "" {
				event.Body = providerEvent.Body
			}
		} else if metadata, ok := state.appSubmissions[submission.ID]; ok && !metadata.startedAt.IsZero() {
			event.Timestamp = metadata.startedAt.Format(time.RFC3339Nano)
		}
		conversation.Events = append(conversation.Events, event)
	}
	for _, canonicalEvent := range thread.Events {
		var event work.CodexConversationEvent
		if json.Unmarshal([]byte(canonicalEvent.Payload), &event) != nil {
			event = work.CodexConversationEvent{Body: canonicalEvent.Payload}
		}
		event.ID = string(canonicalEvent.ID)
		event.Seq = int(canonicalEvent.Position)
		event.Position = uint64(canonicalEvent.Position)
		event.EventRevision = uint64(canonicalEvent.Revision)
		event.ActivityID = string(canonicalEvent.ExecutionID)
		event.SubmissionID = string(canonicalEvent.CausalSubmissionID)
		conversation.Events = append(conversation.Events, event)
	}
	sort.SliceStable(conversation.Events, func(left, right int) bool {
		if conversation.Events[left].Position != conversation.Events[right].Position {
			return conversation.Events[left].Position < conversation.Events[right].Position
		}
		return conversation.Events[left].ID < conversation.Events[right].ID
	})

	conversation.Activity = cloneCodexConversationTurn(conversation.Turn)
	active := conversation.Activity != nil && conversation.Activity.Status == work.CodexConversationTurnRunning
	conversation.Active = &active

	fingerprint := canonicalCodexConversationFingerprint(conversation)
	if replace || fingerprint != state.lastFingerprint {
		state.revision++
		if state.revision == 0 {
			state.revision = 1
		}
		state.lastFingerprint = fingerprint
		state.lastConversation = cloneCodexConversation(conversation)
	} else if state.lastFingerprint == "" {
		state.revision = 1
		state.lastFingerprint = fingerprint
		state.lastConversation = cloneCodexConversation(conversation)
	}
	return conversation, state.revision
}

func canonicalSubmissionState(
	submission chatthread.Submission,
	metadata canonicalCodexSubmissionMetadata,
) string {
	if metadata.rejected {
		return "rejected"
	}
	switch submission.Delivery {
	case chatthread.DeliveryDelivered:
		return "delivered"
	case chatthread.DeliveryAmbiguous:
		return "unconfirmed"
	case chatthread.DeliveryQueued:
		return "queued"
	default:
		if metadata.queued {
			return "queued"
		}
		return "accepted"
	}
}

func canonicalSubmissionByID(thread chatthread.Thread, id chatthread.SubmissionID) *chatthread.Submission {
	for index := range thread.Submissions {
		if thread.Submissions[index].ID == id {
			return &thread.Submissions[index]
		}
	}
	return nil
}

func canonicalActivityByID(thread chatthread.Thread, id chatthread.ExecutionID) *chatthread.ExecutionActivity {
	for index := range thread.ExecutionActivities {
		if thread.ExecutionActivities[index].ID == id {
			return &thread.ExecutionActivities[index]
		}
	}
	return nil
}

func canonicalCodexConversationFingerprint(conversation work.CodexConversation) string {
	raw, _ := json.Marshal(conversation)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func cloneCodexConversation(conversation work.CodexConversation) work.CodexConversation {
	clone := conversation
	clone.Turn = cloneCodexConversationTurn(conversation.Turn)
	clone.Activity = cloneCodexConversationTurn(conversation.Activity)
	clone.QueuedTurns = append([]work.CodexConversationTurn{}, conversation.QueuedTurns...)
	clone.ProviderTurns = append([]work.CodexConversationTurn{}, conversation.ProviderTurns...)
	clone.Events = append([]work.CodexConversationEvent{}, conversation.Events...)
	for index := range clone.Events {
		clone.Events[index].Files = append([]string{}, conversation.Events[index].Files...)
		clone.Events[index].Plan = append([]work.CodexPlanStep{}, conversation.Events[index].Plan...)
	}
	if conversation.Active != nil {
		active := *conversation.Active
		clone.Active = &active
	}
	if conversation.Updated != nil {
		updated := *conversation.Updated
		clone.Updated = &updated
	}
	return clone
}

func cloneCodexConversationTurn(turn *work.CodexConversationTurn) *work.CodexConversationTurn {
	if turn == nil {
		return nil
	}
	clone := *turn
	return &clone
}
