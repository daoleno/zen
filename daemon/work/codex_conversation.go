package work

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

const (
	maxCodexConversationEvents   = 240
	maxCodexConversationBody     = 8000
	maxCodexConversationRead     = 4 << 20
	codexMessageDedupeLineWindow = 12
	codexMessageDedupeTimeWindow = 15 * time.Second
)

type CodexConversation struct {
	Available bool                     `json:"available"`
	Reason    string                   `json:"reason,omitempty"`
	Source    string                   `json:"source,omitempty"`
	Path      string                   `json:"path,omitempty"`
	SessionID string                   `json:"session_id,omitempty"`
	CWD       string                   `json:"cwd,omitempty"`
	Updated   *time.Time               `json:"updated_at,omitempty"`
	Activity  *ProviderActivity        `json:"activity,omitempty"`
	Events    []CodexConversationEvent `json:"events"`
	// ProviderActivities is parser-internal lifecycle history used only to
	// reconcile an exact previously recorded ActivityID after a reusable
	// provider session has advanced to a later native turn. It is never a UI
	// event surface and is not serialized.
	ProviderActivities []ProviderActivity `json:"-"`
}

type ProviderActivityStatus string

const (
	ProviderActivityRunning     ProviderActivityStatus = "running"
	ProviderActivityCompleted   ProviderActivityStatus = "completed"
	ProviderActivityFailed      ProviderActivityStatus = "failed"
	ProviderActivityInterrupted ProviderActivityStatus = "interrupted"
	ProviderActivityCancelled   ProviderActivityStatus = "cancelled"
)

// ProviderActivity is the provider/executor's current lifecycle fact.
// Transcript rendering and local send acknowledgements cannot create it.
type ProviderActivity struct {
	ID        string                 `json:"id"`
	Status    ProviderActivityStatus `json:"status"`
	StartedAt string                 `json:"started_at"`
	SettledAt string                 `json:"settled_at,omitempty"`
}

// providerActivityLifecycle keeps only the provider's current Activity. A
// repeated start preserves its clock, a distinct start cannot replace a running
// Activity, and only a distinct start after settlement advances the lifecycle.
type providerActivityLifecycle struct {
	activity *ProviderActivity
}

func (l *providerActivityLifecycle) start(id, startedAt string) {
	id = strings.TrimSpace(id)
	startedAt = normalizeProviderActivityTimestamp(startedAt)
	if id == "" || startedAt == "" {
		return
	}
	if l.activity != nil {
		if l.activity.Status == ProviderActivityRunning {
			return
		}
		if l.activity.ID == id {
			return
		}
	}
	l.activity = &ProviderActivity{
		ID:        id,
		Status:    ProviderActivityRunning,
		StartedAt: startedAt,
	}
}

func (l *providerActivityLifecycle) settle(id string, status ProviderActivityStatus, settledAt string) {
	id = strings.TrimSpace(id)
	status = normalizedProviderActivityTerminalStatus(status)
	settledAt = normalizeProviderActivityTimestamp(settledAt)
	if status == "" {
		return
	}
	if l.activity == nil {
		if id == "" || settledAt == "" {
			return
		}
		l.activity = &ProviderActivity{
			ID:        id,
			Status:    ProviderActivityRunning,
			StartedAt: settledAt,
		}
	}
	if id != "" && l.activity.ID != id {
		return
	}
	if l.activity.Status != ProviderActivityRunning {
		return
	}
	l.activity.Status = status
	l.activity.SettledAt = settledAt
}

func normalizeProviderActivityTimestamp(value string) string {
	value = normalizeCodexTimestamp(value)
	if parseNormalizedCodexTimestamp(value).IsZero() {
		return ""
	}
	return value
}

func (l *providerActivityLifecycle) running() bool {
	return l != nil && l.activity != nil && l.activity.Status == ProviderActivityRunning
}

func (l *providerActivityLifecycle) snapshot() *ProviderActivity {
	if l == nil || l.activity == nil {
		return nil
	}
	activity := *l.activity
	return &activity
}

func (l *providerActivityLifecycle) adopt(other *providerActivityLifecycle) {
	if other == nil {
		l.activity = nil
		return
	}
	l.activity = other.snapshot()
}

func normalizedProviderActivityTerminalStatus(status ProviderActivityStatus) ProviderActivityStatus {
	switch status {
	case ProviderActivityCompleted:
		return ProviderActivityCompleted
	case ProviderActivityFailed:
		return ProviderActivityFailed
	case ProviderActivityInterrupted:
		return ProviderActivityInterrupted
	case ProviderActivityCancelled:
		return ProviderActivityCancelled
	default:
		return ""
	}
}

func providerActivityID(sourceID, providerID string, lineNumber int) string {
	sourceID = firstNonEmpty(strings.TrimSpace(sourceID), "conversation")
	providerID = strings.TrimSpace(providerID)
	if providerID != "" {
		return fmt.Sprintf("%s:activity:%s", sourceID, providerID)
	}
	if lineNumber < 1 {
		lineNumber = 1
	}
	return fmt.Sprintf("%s:activity:line-%d", sourceID, lineNumber)
}

func conversationWithActivity(conversation CodexConversation, lifecycle *providerActivityLifecycle) CodexConversation {
	conversation.Activity = lifecycle.snapshot()
	return conversation
}

type CodexConversationEvent struct {
	ID        string `json:"id"`
	Seq       int    `json:"seq"`
	Timestamp string `json:"timestamp,omitempty"`
	Kind      string `json:"kind"`
	Role      string `json:"role,omitempty"`
	Title     string `json:"title,omitempty"`
	Body      string `json:"body,omitempty"`
	Command   string `json:"command,omitempty"`
	ToolName  string `json:"tool_name,omitempty"`
	Input     string `json:"input,omitempty"`
	Output    string `json:"output,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	Status    string `json:"status,omitempty"`
	// Partial means the provider may update this same logical event ID again.
	// It does not imply or permit client-side splitting of completed text.
	Partial bool `json:"partial,omitempty"`
	// Transient means a provider projection may be absent from a later provider
	// snapshot and is therefore safe to delete during reconciliation.
	Transient   bool                          `json:"transient,omitempty"`
	Files       []string                      `json:"files,omitempty"`
	FileChanges []CodexConversationFileChange `json:"file_changes,omitempty"`
	Explanation string                        `json:"explanation,omitempty"`
	Plan        []CodexPlanStep               `json:"plan,omitempty"`
	Source      string                        `json:"source,omitempty"`
	// Work-card fields are only set for Brain timeline Source=work_result items.
	WorkID      string `json:"work_id,omitempty"`
	WorkSession string `json:"work_session_id,omitempty"`
	SessionName string `json:"session_name,omitempty"`
	Unread      bool   `json:"unread,omitempty"`
	// Work result lifecycle is separate from the immutable result fact in
	// Status: queued attention, admitted review, handled disposition, and
	// Session finalization are different authorities.
	WorkReviewState   string `json:"work_review_state,omitempty"`
	WorkSessionState  string `json:"work_session_state,omitempty"`
	WorkResultCurrent bool   `json:"work_result_current"`
	WorkPhase         string `json:"work_phase,omitempty"`
	WorkAttention     string `json:"work_attention,omitempty"`
	WorkEventKind     string `json:"work_event_kind,omitempty"`
	WorkDetailsJSON   string `json:"work_details_json,omitempty"`
	WorkNextAction    string `json:"work_next_action,omitempty"`
	WorkWaitFor       string `json:"work_wait_for,omitempty"`
	// AdmissionSHA256 is the exact provider-native user input digest when the
	// source preserves those bytes separately from its display projection.
	AdmissionSHA256 string `json:"-"`
}

// CodexConversationFileChange is the provider-neutral, display-only summary
// of one file mutation. Optional line counts distinguish unknown statistics
// from a known zero without making the raw patch a second state owner.
type CodexConversationFileChange struct {
	Path      string `json:"path"`
	MovePath  string `json:"move_path,omitempty"`
	Operation string `json:"operation"`
	Additions *int   `json:"additions,omitempty"`
	Deletions *int   `json:"deletions,omitempty"`
}

type CodexPlanStep struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}

func (r *ProviderConversationReader) loadCodexConversationForAgent(agent classifier.Agent, now time.Time) (CodexConversation, error) {
	if strings.TrimSpace(agent.Cwd) == "" {
		r.resetSource()
		return CodexConversation{
			Available: false,
			Reason:    "missing_cwd",
			Events:    []CodexConversationEvent{},
		}, nil
	}

	candidate, ok, err := findCodexTranscript(agent, now)
	if err != nil {
		r.resetSource()
		return CodexConversation{}, err
	}
	if !ok {
		r.resetSource()
		return CodexConversation{
			Available: false,
			Reason:    "transcript_not_found",
			Events:    []CodexConversationEvent{},
		}, nil
	}

	conversation, err := r.loadCodexConversation(candidate.Path)
	if err != nil {
		return CodexConversation{}, err
	}
	conversation.Available = true
	conversation.Source = "codex_rollout"
	conversation.Path = candidate.Path
	conversation.SessionID = firstNonEmpty(conversation.SessionID, candidate.Meta.ID, candidate.Row.ID)
	conversation.CWD = firstNonEmpty(conversation.CWD, candidate.Meta.CWD)
	conversation.Updated = &candidate.Updated
	if conversation.Events == nil {
		conversation.Events = []CodexConversationEvent{}
	}
	return conversation, nil
}

func (r *ProviderConversationReader) loadCodexConversation(path string) (CodexConversation, error) {
	return r.loadFileConversation(AgentProviderCodex, path, parseCodexConversation)
}

func parseCodexConversation(path string) (CodexConversation, error) {
	file, err := os.Open(path)
	if err != nil {
		return CodexConversation{}, err
	}
	defer file.Close()

	// The bounded parser may seek past session_meta on large rollouts. Read
	// that immutable header from this same open file first so every lifecycle
	// row — including a lone task_complete in the retained tail — derives the
	// same session-scoped Activity identity that was published when
	// task_started was still in the parse window. Failure stays backward-
	// compatible: malformed/legacy files continue through the ordinary tail
	// parser and use the path-scoped id.
	meta, _ := readCodexMetaFromReader(file)

	if err := seekCodexConversationTail(file); err != nil {
		return CodexConversation{}, err
	}
	lineOffset, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return CodexConversation{}, err
	}
	builder := newCodexConversationBuilder(filepath.Base(path))
	builder.sessionID = strings.TrimSpace(meta.ID)
	builder.cwd = strings.TrimSpace(meta.CWD)
	reader := bufio.NewReader(file)
	for {
		currentLineOffset := lineOffset
		line, err := reader.ReadBytes('\n')
		lineOffset += int64(len(line))
		if len(bytes.TrimSpace(line)) > 0 {
			builder.consumeLine(codexConversationLineMarker(currentLineOffset), line)
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return CodexConversation{}, err
		}
	}
	return builder.conversation(), nil
}

func codexConversationLineMarker(offset int64) int {
	if offset < 0 {
		return 1
	}
	return int(offset) + 1
}

func seekCodexConversationTail(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() <= maxCodexConversationRead {
		_, err = file.Seek(0, io.SeekStart)
		return err
	}
	if _, err := file.Seek(info.Size()-maxCodexConversationRead, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReader(file)
	if _, err := reader.ReadBytes('\n'); err != nil && err != io.EOF {
		return err
	}
	offset, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if _, err := file.Seek(offset-int64(reader.Buffered()), io.SeekStart); err != nil {
		return err
	}
	return nil
}

type codexConversationBuilder struct {
	sourceID             string
	sessionID            string
	cwd                  string
	activityLifecycle    providerActivityLifecycle
	slashCommandActivity bool
	activityProviderID   string
	providerActivities   []ProviderActivity
	events               []CodexConversationEvent
	commandByCall        map[string]string
	commandCallBySession map[string]string
	eventByCall          map[string]int
	sessionByCall        map[string]string
	recentMessageByKey   map[string]recentCodexMessageFingerprint
	seenStatusKeys       map[string]struct{}
	pendingReasoningID   string
	patchEventSeen       bool
	pendingUserEcho      *codexPendingUserEcho
	unpairedAdmissions   int
	nextAdmissionPaired  bool
}

// response_item/message/user is a rendering echo in current Codex rollouts;
// event_msg/user_message is the admission boundary. Defer the response form by
// one record so an adjacent admission can replace it without body/time dedupe,
// while response-only legacy rollouts retain a safe fallback row.
type codexPendingUserEcho struct {
	lineNumber int
	timestamp  string
	text       string
}

type recentCodexMessageFingerprint struct {
	lineNumber int
	timestamp  time.Time
}

func newCodexConversationBuilder(sourceID string) *codexConversationBuilder {
	return &codexConversationBuilder{
		sourceID:             sourceID,
		commandByCall:        map[string]string{},
		commandCallBySession: map[string]string{},
		eventByCall:          map[string]int{},
		sessionByCall:        map[string]string{},
		recentMessageByKey:   map[string]recentCodexMessageFingerprint{},
		seenStatusKeys:       map[string]struct{}{},
	}
}

func (b *codexConversationBuilder) startActivity(providerID, timestamp string, lineNumber int) {
	providerID = strings.TrimSpace(providerID)
	if providerID != "" && b.activityLifecycle.running() &&
		b.activityProviderID != "" && providerID != b.activityProviderID {
		// A newer native turn is authoritative evidence that an older turn with
		// no terminal row is no longer current. This occurs after an interrupted
		// host/executor. Let the new turn own Activity instead of pinning the UI
		// to the old running clock forever.
		b.activityLifecycle = providerActivityLifecycle{}
		b.activityProviderID = ""
	}
	if b.activityLifecycle.activity != nil && !b.activityLifecycle.running() {
		b.retainProviderActivity(b.activityLifecycle.activity)
	}
	previousActivityID := ""
	if b.activityLifecycle.activity != nil {
		previousActivityID = b.activityLifecycle.activity.ID
	}
	id := ""
	if providerID != "" {
		if b.activityLifecycle.running() && b.activityProviderID == "" {
			// Some Codex rollouts write the user row before the provider turn ID.
			// Correlate that later fact without changing the already-published ID.
			b.activityProviderID = providerID
			id = b.activityLifecycle.activity.ID
		} else {
			id = providerActivityID(firstNonEmpty(b.sessionID, b.sourceID), providerID, lineNumber)
		}
	} else if b.activityLifecycle.running() {
		id = b.activityLifecycle.activity.ID
	} else {
		id = providerActivityID(firstNonEmpty(b.sessionID, b.sourceID), "", lineNumber)
	}
	b.activityLifecycle.start(id, timestamp)
	if b.activityLifecycle.activity != nil && b.activityLifecycle.activity.ID != previousActivityID {
		b.activityProviderID = providerID
	}
}

// maxCodexProviderActivities is a recovery-only resource ceiling, not a
// correctness window: it bounds the probe payload to 64 fixed-size lifecycle
// records already encountered in the 4 MiB conversation tail, without a
// second history owner or full-file scan. Correctness never depends on finding
// an older record. A desired ActivityID displaced by the 65th terminal is
// deliberately unavailable, so watcher binding fails closed and requires
// explicit offline reconciliation.
const maxCodexProviderActivities = 64

func (b *codexConversationBuilder) retainProviderActivity(activity *ProviderActivity) {
	if activity == nil || strings.TrimSpace(activity.ID) == "" ||
		normalizedProviderActivityTerminalStatus(activity.Status) == "" {
		return
	}
	for index := range b.providerActivities {
		if b.providerActivities[index].ID == activity.ID {
			b.providerActivities[index] = *activity
			return
		}
	}
	b.providerActivities = append(b.providerActivities, *activity)
	if len(b.providerActivities) > maxCodexProviderActivities {
		b.providerActivities = append(
			[]ProviderActivity(nil),
			b.providerActivities[len(b.providerActivities)-maxCodexProviderActivities:]...,
		)
	}
}

func (b *codexConversationBuilder) providerActivitySnapshots() []ProviderActivity {
	activities := append([]ProviderActivity(nil), b.providerActivities...)
	if current := b.activityLifecycle.snapshot(); current != nil {
		replaced := false
		for index := range activities {
			if activities[index].ID != current.ID {
				continue
			}
			activities[index] = *current
			replaced = true
			break
		}
		if !replaced {
			activities = append(activities, *current)
		}
	}
	if len(activities) > maxCodexProviderActivities {
		activities = activities[len(activities)-maxCodexProviderActivities:]
	}
	return activities
}

func (b *codexConversationBuilder) settleActivity(providerID string, status ProviderActivityStatus, timestamp string, lineNumber int) {
	providerID = strings.TrimSpace(providerID)
	id := ""
	if b.activityLifecycle.running() {
		if providerID != "" && b.activityProviderID != "" && providerID != b.activityProviderID {
			return
		}
		if providerID != "" && b.activityProviderID == "" {
			b.activityProviderID = providerID
		}
		id = b.activityLifecycle.activity.ID
	} else if providerID != "" {
		id = providerActivityID(firstNonEmpty(b.sessionID, b.sourceID), providerID, lineNumber)
	} else if b.activityLifecycle.activity == nil {
		id = providerActivityID(firstNonEmpty(b.sessionID, b.sourceID), "", lineNumber)
	}
	b.activityLifecycle.settle(id, status, timestamp)
}

func codexAbortedActivityStatus(reason string) ProviderActivityStatus {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if strings.Contains(reason, "cancel") {
		return ProviderActivityCancelled
	}
	return ProviderActivityInterrupted
}

func (b *codexConversationBuilder) consumeLine(lineNumber int, line []byte) {
	var envelope struct {
		Type      string          `json:"type"`
		Timestamp string          `json:"timestamp"`
		Payload   json.RawMessage `json:"payload"`
	}
	if json.Unmarshal(line, &envelope) != nil {
		return
	}
	isAdmission := envelope.Type == "event_msg" && codexEventPayloadType(envelope.Payload) == "user_message"
	b.nextAdmissionPaired = false
	if b.pendingUserEcho != nil {
		if isAdmission {
			b.pendingUserEcho = nil
			b.nextAdmissionPaired = true
		} else {
			b.flushPendingUserEcho()
		}
	}

	timestamp := normalizeCodexTimestamp(envelope.Timestamp)
	switch envelope.Type {
	case "session_meta":
		b.consumeSessionMeta(envelope.Payload)
	case "event_msg":
		b.consumeEvent(lineNumber, timestamp, envelope.Payload)
	case "response_item":
		b.consumeResponseItem(lineNumber, timestamp, envelope.Payload)
	}
}

func codexEventPayloadType(raw json.RawMessage) string {
	var payload struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	return strings.TrimSpace(payload.Type)
}

func (b *codexConversationBuilder) flushPendingUserEcho() {
	pending := b.pendingUserEcho
	if pending == nil {
		return
	}
	b.pendingUserEcho = nil
	b.startActivity("", pending.timestamp, pending.lineNumber)
	b.slashCommandActivity = isCodexSlashCommandInvocation(pending.text)
	b.addMessage(pending.lineNumber, pending.timestamp, "user", pending.text)
	// A response_item-only user row is the provider-native admitted payload,
	// not merely a display echo. Preserve its exact digest just like the paired
	// event_msg shape so restart recovery can correlate a pending transaction
	// without cwd/latest-session guessing or input replay.
	b.markAdmissionDigest(pending.lineNumber, pending.text)
}

func (b *codexConversationBuilder) consumeSessionMeta(raw json.RawMessage) {
	var payload struct {
		ID  string `json:"id"`
		CWD string `json:"cwd"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return
	}
	if id := strings.TrimSpace(payload.ID); id != "" {
		b.sessionID = id
	}
	if cwd := strings.TrimSpace(payload.CWD); cwd != "" {
		b.cwd = cwd
	}
}

func (b *codexConversationBuilder) consumeEvent(lineNumber int, timestamp string, raw json.RawMessage) {
	var payload struct {
		Type              string          `json:"type"`
		Message           string          `json:"message"`
		Phase             string          `json:"phase"`
		CallID            string          `json:"call_id"`
		ExitCode          *int            `json:"exit_code"`
		Status            string          `json:"status"`
		Command           []string        `json:"command"`
		AggregatedOutput  string          `json:"aggregated_output"`
		Explanation       string          `json:"explanation"`
		Plan              []CodexPlanStep `json:"plan"`
		Text              string          `json:"text"`
		Query             string          `json:"query"`
		Action            json.RawMessage `json:"action"`
		AdditionalDetails string          `json:"additional_details"`
		Reason            string          `json:"reason"`
		TurnID            string          `json:"turn_id"`
		CodexErrorInfo    json.RawMessage `json:"codex_error_info"`
		NumTurns          *int            `json:"num_turns"`
		RateLimits        json.RawMessage `json:"rate_limits"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return
	}

	if shouldFinishPendingReasoningForEvent(payload.Type) {
		b.finishPendingReasoning()
	}

	switch payload.Type {
	case "task_started", "turn_started", "item_started", "item_completed":
		// Codex can emit enough tool/item traffic for task_started to fall
		// outside the bounded tail while the turn is still active. Native item
		// rows carry the same turn_id and therefore recover the exact running
		// Activity identity; only a later turn terminal settles it.
		b.startActivity(payload.TurnID, timestamp, lineNumber)
		if payload.Type == "task_started" || payload.Type == "turn_started" {
			b.addStatus(lineNumber, timestamp, "Task started", "")
		}
	case "task_complete", "turn_complete":
		b.settleActivity(payload.TurnID, ProviderActivityCompleted, timestamp, lineNumber)
	case "turn_aborted":
		b.settleActivity(payload.TurnID, codexAbortedActivityStatus(payload.Reason), timestamp, lineNumber)
		if reason := cleanConversationText(payload.Reason); reason != "" {
			b.addStatusWithState(lineNumber, timestamp, "Turn aborted", reason, "failed")
		}
	case "error":
		if codexErrorAffectsTurnStatus(payload.CodexErrorInfo) {
			b.settleActivity(payload.TurnID, ProviderActivityFailed, timestamp, lineNumber)
			b.finishPendingReasoning()
		}
		message := cleanConversationText(payload.Message)
		if message == "" {
			message = "Codex reported an error."
		}
		b.addStatusWithState(lineNumber, timestamp, "Codex error", message, "failed")
	case "stream_error":
		b.startActivity(payload.TurnID, timestamp, lineNumber)
		title := cleanConversationText(payload.Message)
		if title == "" {
			title = "Stream interrupted"
		}
		b.addStatusWithState(lineNumber, timestamp, title, payload.AdditionalDetails, "running")
	case "warning", "guardian_warning":
		title := "Codex warning"
		if payload.Type == "guardian_warning" {
			title = "Guardian warning"
		}
		b.addStatusWithState(lineNumber, timestamp, title, payload.Message, "warning")
	case "context_compacted":
		body := ""
		if payload.NumTurns != nil && *payload.NumTurns > 0 {
			body = fmt.Sprintf("%d user turn", *payload.NumTurns)
			if *payload.NumTurns != 1 {
				body += "s"
			}
			body += " summarized"
		}
		b.addStatusWithState(lineNumber, timestamp, "Context compacted", body, "done")
	case "token_count":
		if title, body, ok := codexRateLimitStatus(payload.RateLimits); ok {
			b.addStatusWithState(lineNumber, timestamp, title, body, "failed")
		}
	case "user_message":
		b.startActivity("", timestamp, lineNumber)
		b.slashCommandActivity = isCodexSlashCommandInvocation(payload.Message)
		b.addMessage(lineNumber, timestamp, "user", payload.Message)
		b.markAdmissionDigest(lineNumber, payload.Message)
		if len(b.events) > 0 && b.events[len(b.events)-1].ID == b.eventID(lineNumber) && !b.nextAdmissionPaired {
			b.unpairedAdmissions++
		}
		b.nextAdmissionPaired = false
	case "history_entry":
		b.addHistoryEntry(lineNumber, timestamp, payload.Message)
		if b.slashCommandActivity {
			b.settleActivity("", ProviderActivityCompleted, timestamp, lineNumber)
			b.slashCommandActivity = false
		}
	case "agent_message":
		title := ""
		if strings.TrimSpace(payload.Phase) != "" {
			title = strings.TrimSpace(payload.Phase)
		}
		b.addMessageWithTitle(lineNumber, timestamp, "assistant", payload.Message, title)
	case "agent_reasoning":
		b.upsertReasoning(lineNumber, timestamp, payload.Text, false)
	case "web_search_begin":
		b.upsertWebSearchBegin(lineNumber, timestamp, payload.CallID)
	case "web_search_end":
		b.upsertWebSearchEnd(lineNumber, timestamp, payload.CallID, payload.Query, payload.Action, "done")
	case "exec_command_end":
		command := shellCommandLabel(payload.Command)
		if command == "" {
			command = b.commandByCall[payload.CallID]
		}
		if command == "" {
			command = "command"
		}
		exitCode := 0
		if payload.ExitCode != nil {
			exitCode = *payload.ExitCode
		}
		b.upsertCommandEnd(lineNumber, timestamp, payload.CallID, command, exitCode, payload.AggregatedOutput)
	case "patch_apply_end":
		status := strings.TrimSpace(payload.Status)
		if (status == "" || status == "success") && b.patchEventSeen {
			return
		}
		if status == "" || status == "success" {
			b.addStatus(lineNumber, timestamp, "Patch applied", "")
		} else {
			b.addStatus(lineNumber, timestamp, "Patch "+status, "")
		}
	case "plan_update":
		b.addPlanUpdate(lineNumber, timestamp, "", payload.Explanation, payload.Plan)
	}
}

func (b *codexConversationBuilder) consumeResponseItem(lineNumber int, timestamp string, raw json.RawMessage) {
	var payload struct {
		Type      string          `json:"type"`
		ID        string          `json:"id"`
		Role      string          `json:"role"`
		Content   json.RawMessage `json:"content"`
		Name      string          `json:"name"`
		Arguments string          `json:"arguments"`
		CallID    string          `json:"call_id"`
		Status    string          `json:"status"`
		Input     string          `json:"input"`
		Output    json.RawMessage `json:"output"`
		Summary   json.RawMessage `json:"summary"`
		Action    json.RawMessage `json:"action"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return
	}

	switch payload.Type {
	case "message":
		b.finishPendingReasoning()
		text := codexConversationContentText(payload.Content)
		switch payload.Role {
		case "user":
			if b.unpairedAdmissions > 0 {
				b.unpairedAdmissions--
				return
			}
			b.pendingUserEcho = &codexPendingUserEcho{
				lineNumber: lineNumber,
				timestamp:  timestamp,
				text:       text,
			}
		case "assistant":
			b.addMessage(lineNumber, timestamp, payload.Role, text)
		}
	case "web_search_call":
		callID := firstNonEmpty(payload.CallID, payload.ID)
		b.upsertWebSearchEnd(lineNumber, timestamp, callID, "", payload.Action, codexWebSearchStatus(payload.Status))
	case "function_call":
		if isCodexPlanTool(payload.Name) {
			explanation, plan := codexPlanToolArguments(payload.Arguments)
			b.addPlanUpdate(lineNumber, timestamp, payload.CallID, explanation, plan)
		} else if isCodexExecWrapperTool(payload.Name) {
			b.addExecWrapperCall(lineNumber, timestamp, payload.CallID, payload.Arguments, "running")
		} else if isCodexCommandTool(payload.Name) {
			command := codexExecCommand(payload.Arguments)
			if command != "" {
				b.commandByCall[payload.CallID] = command
				b.addCommandStart(lineNumber, timestamp, payload.CallID, command)
			}
		} else {
			callID := strings.TrimSpace(payload.CallID)
			command := ""
			if sessionID := codexToolSessionID(payload.Arguments); sessionID != "" && callID != "" {
				b.sessionByCall[callID] = sessionID
				if commandCallID := b.commandCallBySession[sessionID]; commandCallID != "" {
					command = b.commandByCall[commandCallID]
				}
			}
			b.addToolStart(lineNumber, timestamp, payload.CallID, payload.Name, payload.Arguments, "running", command)
		}
	case "function_call_output":
		output := codexFunctionOutputText(payload.Output, payload.Content)
		if sessionID := b.sessionByCall[strings.TrimSpace(payload.CallID)]; sessionID != "" {
			b.updateSessionCommandOutput(lineNumber, timestamp, sessionID, output)
		}
		b.updateCallOutput(lineNumber, timestamp, payload.CallID, output)
	case "custom_tool_call":
		if payload.Name == "apply_patch" {
			b.addPatchEvent(lineNumber, timestamp, payload.CallID, payload.Input)
		} else if isCodexExecWrapperTool(payload.Name) {
			b.addExecWrapperCall(lineNumber, timestamp, payload.CallID, payload.Input, "done")
		} else {
			b.addToolStart(lineNumber, timestamp, payload.CallID, payload.Name, payload.Input, "done", "")
		}
	case "custom_tool_call_output":
		output := codexFunctionOutputText(payload.Output, payload.Content)
		if sessionID := b.sessionByCall[strings.TrimSpace(payload.CallID)]; sessionID != "" {
			b.updateSessionCommandOutput(lineNumber, timestamp, sessionID, output)
		}
		b.updateCallOutput(lineNumber, timestamp, payload.CallID, output)
	case "reasoning":
		b.upsertReasoning(lineNumber, timestamp, codexConversationContentText(payload.Summary), true)
	}
}

func shouldFinishPendingReasoningForEvent(eventType string) bool {
	switch eventType {
	case "task_started",
		"turn_started",
		"item_started",
		"item_completed",
		"task_complete",
		"turn_complete",
		"turn_aborted",
		"user_message",
		"history_entry",
		"agent_message",
		"exec_command_end",
		"patch_apply_end",
		"plan_update":
		return true
	default:
		return false
	}
}

func (b *codexConversationBuilder) addMessage(lineNumber int, timestamp, role, text string) {
	b.addMessageWithTitle(lineNumber, timestamp, role, text, "")
}

func (b *codexConversationBuilder) markAdmissionDigest(lineNumber int, exact string) {
	id := b.eventID(lineNumber)
	for index := len(b.events) - 1; index >= 0; index-- {
		if b.events[index].ID != id || b.events[index].Kind != "user_message" {
			continue
		}
		b.events[index].AdmissionSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(exact)))
		return
	}
}

func (b *codexConversationBuilder) addHistoryEntry(lineNumber int, timestamp, text string) {
	text = CleanCodexDisplayText(text)
	if text == "" || isTranscriptBoilerplate(text) {
		return
	}
	b.addEvent(CodexConversationEvent{
		ID:        b.eventID(lineNumber),
		Timestamp: timestamp,
		Kind:      "status",
		Title:     "Codex",
		Body:      truncateConversationBody(text),
		Source:    "codex_rollout",
	})
}

func (b *codexConversationBuilder) addMessageWithTitle(lineNumber int, timestamp, role, text, title string) {
	text = CleanCodexDisplayText(text)
	if text == "" || isTranscriptBoilerplate(text) {
		return
	}
	if role != "user" {
		key := role + ":" + text
		currentTimestamp := parseNormalizedCodexTimestamp(timestamp)
		if previous, exists := b.recentMessageByKey[key]; exists && shouldDedupeCodexMessage(previous, lineNumber, currentTimestamp) {
			b.recentMessageByKey[key] = recentCodexMessageFingerprint{
				lineNumber: lineNumber,
				timestamp:  latestNonZeroTime(previous.timestamp, currentTimestamp),
			}
			return
		}
		b.recentMessageByKey[key] = recentCodexMessageFingerprint{
			lineNumber: lineNumber,
			timestamp:  currentTimestamp,
		}
	}

	kind := "assistant_message"
	if role == "user" {
		kind = "user_message"
	}
	b.addEvent(CodexConversationEvent{
		ID:        b.eventID(lineNumber),
		Timestamp: timestamp,
		Kind:      kind,
		Role:      role,
		Title:     cleanConversationText(title),
		Body:      text,
		Source:    "codex_rollout",
	})
}

func shouldDedupeCodexMessage(
	previous recentCodexMessageFingerprint,
	lineNumber int,
	timestamp time.Time,
) bool {
	if lineNumber-previous.lineNumber <= codexMessageDedupeLineWindow {
		return true
	}
	if previous.timestamp.IsZero() || timestamp.IsZero() {
		return false
	}
	delta := timestamp.Sub(previous.timestamp)
	if delta < 0 {
		delta = -delta
	}
	return delta <= codexMessageDedupeTimeWindow
}

func latestNonZeroTime(left, right time.Time) time.Time {
	if left.IsZero() {
		return right
	}
	if right.IsZero() {
		return left
	}
	if right.After(left) {
		return right
	}
	return left
}

func isCodexSlashCommandInvocation(value string) bool {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "/") {
		return false
	}
	if strings.Contains(trimmed, "\n") {
		firstLine, _, _ := strings.Cut(trimmed, "\n")
		trimmed = strings.TrimSpace(firstLine)
	}
	if len(trimmed) < 2 {
		return false
	}
	for index, r := range trimmed[1:] {
		if r == ' ' || r == '\t' {
			return index > 0
		}
		if r == '-' {
			continue
		}
		if r >= 'a' && r <= 'z' {
			continue
		}
		if index > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func (b *codexConversationBuilder) addCommandStart(lineNumber int, timestamp, callID, command string) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		b.addEvent(CodexConversationEvent{
			ID:        b.eventID(lineNumber),
			Timestamp: timestamp,
			Kind:      "command",
			Title:     "Command",
			Command:   cleanConversationText(command),
			Status:    "running",
			Source:    "codex_rollout",
		})
		return
	}
	if _, exists := b.eventByCall[callID]; exists {
		return
	}
	event := CodexConversationEvent{
		ID:        b.eventID(lineNumber),
		Timestamp: timestamp,
		Kind:      "command",
		Title:     "Command",
		Command:   cleanConversationText(command),
		CallID:    callID,
		Status:    "running",
		Source:    "codex_rollout",
	}
	if b.addEvent(event) {
		b.eventByCall[callID] = len(b.events) - 1
	}
}

func (b *codexConversationBuilder) upsertCommandEnd(lineNumber int, timestamp, callID, command string, exitCode int, output string) {
	status := "done"
	title := "Command finished"
	if exitCode != 0 {
		status = "failed"
		title = "Command failed"
	}
	body := codexCommandOutputBody(output)
	if body != "" {
		body = truncateConversationBody(body)
	}
	if index, exists := b.eventByCall[strings.TrimSpace(callID)]; exists && index >= 0 && index < len(b.events) {
		if b.events[index].Kind != "command" {
			return
		}
		b.events[index].Title = title
		b.events[index].Command = cleanConversationText(command)
		b.events[index].ExitCode = &exitCode
		b.events[index].Status = status
		b.events[index].Body = body
		if timestamp != "" {
			b.events[index].Timestamp = timestamp
		}
		return
	}
	b.addEvent(CodexConversationEvent{
		ID:        b.eventID(lineNumber),
		Timestamp: timestamp,
		Kind:      "command",
		Title:     title,
		Command:   cleanConversationText(command),
		CallID:    strings.TrimSpace(callID),
		ExitCode:  &exitCode,
		Status:    status,
		Body:      body,
		Source:    "codex_rollout",
	})
}

func (b *codexConversationBuilder) updateCommandOutput(lineNumber int, timestamp, callID, output string) {
	callID = strings.TrimSpace(callID)
	if index, exists := b.eventByCall[callID]; exists && index >= 0 && index < len(b.events) {
		if b.events[index].Kind != "command" {
			return
		}
		body := cleanConversationText(output)
		if sessionID := codexProcessSessionID(body); sessionID != "" {
			b.commandCallBySession[sessionID] = callID
			b.events[index].Status = "running"
			b.events[index].Title = "Command"
			body = codexCommandOutputBody(body)
		} else if exitCode := codexFunctionOutputExitCode(body); exitCode != nil {
			b.events[index].ExitCode = exitCode
			b.events[index].Status = "done"
			b.events[index].Title = "Command finished"
			if *exitCode != 0 {
				b.events[index].Status = "failed"
				b.events[index].Title = "Command failed"
			}
			body = codexCommandOutputBody(body)
			if timestamp != "" {
				b.events[index].Timestamp = timestamp
			}
		} else if body != "" {
			status := codexToolOutputStatus(body)
			b.events[index].Status = status
			if status == "failed" {
				b.events[index].Title = "Command failed"
			} else {
				b.events[index].Title = "Command finished"
			}
			if timestamp != "" {
				b.events[index].Timestamp = timestamp
			}
		}
		b.events[index].Body = truncateConversationBody(body)
	}
}

func (b *codexConversationBuilder) updateSessionCommandOutput(lineNumber int, timestamp, sessionID, output string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	callID := b.commandCallBySession[sessionID]
	if callID == "" {
		return
	}
	b.updateCommandOutput(lineNumber, timestamp, callID, output)
}

func (b *codexConversationBuilder) upsertWebSearchBegin(lineNumber int, timestamp, callID string) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return
	}
	if index, exists := b.eventByCall[callID]; exists && index >= 0 && index < len(b.events) {
		if b.events[index].Kind == "web_search" {
			b.events[index].Status = "running"
			if timestamp != "" {
				b.events[index].Timestamp = timestamp
			}
		}
		return
	}
	if b.addEvent(CodexConversationEvent{
		ID:        b.eventID(lineNumber),
		Timestamp: timestamp,
		Kind:      "web_search",
		Title:     "Web Search",
		CallID:    callID,
		Status:    "running",
		Source:    "codex_rollout",
	}) && callID != "" {
		b.eventByCall[callID] = len(b.events) - 1
	}
}

func (b *codexConversationBuilder) upsertWebSearchEnd(lineNumber int, timestamp, callID, query string, action json.RawMessage, status string) {
	callID = strings.TrimSpace(callID)
	body := codexWebSearchDetail(query, action)
	input := codexWebSearchActionText(action)
	status = codexWebSearchStatus(status)
	if callID != "" {
		if index, exists := b.eventByCall[callID]; exists && index >= 0 && index < len(b.events) {
			if b.events[index].Kind == "web_search" {
				b.events[index].Body = truncateConversationBody(body)
				b.events[index].Input = truncateConversationBody(input)
				b.events[index].Status = status
				if timestamp != "" {
					b.events[index].Timestamp = timestamp
				}
				return
			}
		}
	}
	if callID == "" && b.isDuplicateRecentWebSearch(timestamp, body, input) {
		return
	}
	if b.addEvent(CodexConversationEvent{
		ID:        b.eventID(lineNumber),
		Timestamp: timestamp,
		Kind:      "web_search",
		Title:     "Web Search",
		Body:      body,
		Input:     input,
		CallID:    callID,
		Status:    status,
		Source:    "codex_rollout",
	}) && callID != "" {
		b.eventByCall[callID] = len(b.events) - 1
	}
}

func (b *codexConversationBuilder) isDuplicateRecentWebSearch(timestamp, body, input string) bool {
	if len(b.events) == 0 {
		return false
	}
	previous := b.events[len(b.events)-1]
	if previous.Kind != "web_search" {
		return false
	}
	if strings.TrimSpace(previous.CallID) == "" {
		return false
	}
	if cleanConversationText(previous.Body) != cleanConversationText(body) ||
		cleanConversationText(previous.Input) != cleanConversationText(input) {
		return false
	}
	previousTimestamp := parseNormalizedCodexTimestamp(previous.Timestamp)
	currentTimestamp := parseNormalizedCodexTimestamp(timestamp)
	if previousTimestamp.IsZero() || currentTimestamp.IsZero() {
		return true
	}
	delta := currentTimestamp.Sub(previousTimestamp)
	if delta < 0 {
		delta = -delta
	}
	return delta <= 2*time.Second
}

func (b *codexConversationBuilder) addPatchEvent(lineNumber int, timestamp, callID, input string) {
	callID = strings.TrimSpace(callID)
	fileChanges := patchFileChanges(input)
	files := patchSurfacesFromChanges(fileChanges)
	title := "Patch"
	if len(files) > 0 {
		title = fmt.Sprintf("Patch %d file", len(files))
		if len(files) > 1 {
			title += "s"
		}
	}
	event := CodexConversationEvent{
		ID:          b.eventID(lineNumber),
		Timestamp:   timestamp,
		Kind:        "patch",
		Title:       title,
		Body:        truncateConversationBody(input),
		Files:       files,
		FileChanges: fileChanges,
		CallID:      callID,
		Source:      "codex_rollout",
	}
	if callID != "" {
		if index, exists := b.eventByCall[callID]; exists && index >= 0 && index < len(b.events) {
			existingID := b.events[index].ID
			b.events[index] = event
			b.events[index].ID = existingID
			b.patchEventSeen = true
			return
		}
	}
	if b.addEvent(event) && callID != "" {
		b.eventByCall[callID] = len(b.events) - 1
	}
	b.patchEventSeen = true
}

func (b *codexConversationBuilder) addExecWrapperCall(lineNumber int, timestamp, callID, input, status string) {
	calls := parseCodexExecWrapper(input)
	if len(calls) == 0 {
		b.addToolStart(lineNumber, timestamp, callID, "tool", input, status, "")
		return
	}
	if len(calls) == 1 {
		b.addNestedCodexToolCall(lineNumber, timestamp, callID, calls[0], input, status)
		return
	}
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		names = append(names, call.Name)
	}
	b.addToolStart(lineNumber, timestamp, callID, "multi:"+strings.Join(names, ","), input, status, "")
}

func (b *codexConversationBuilder) addNestedCodexToolCall(
	lineNumber int,
	timestamp, callID string,
	call nestedCodexToolCall,
	rawInput, status string,
) {
	switch call.Name {
	case "exec_command", "shell_command":
		command := nestedCallCommand(call)
		if command == "" {
			b.addToolStart(lineNumber, timestamp, callID, call.Name, rawInput, status, "")
			return
		}
		b.commandByCall[strings.TrimSpace(callID)] = command
		b.addCommandStart(lineNumber, timestamp, callID, command)
		if status != "" && status != "running" {
			if index, exists := b.eventByCall[strings.TrimSpace(callID)]; exists && index >= 0 && index < len(b.events) {
				b.events[index].Status = status
			}
		}
	case "apply_patch":
		patch := nestedCallPatchText(call)
		if patch == "" {
			b.addToolStart(lineNumber, timestamp, callID, call.Name, rawInput, status, "")
			return
		}
		b.addPatchEvent(lineNumber, timestamp, callID, patch)
	case "update_plan":
		explanation, plan := nestedCallPlan(call)
		if len(plan) == 0 && explanation == "" {
			b.addToolStart(lineNumber, timestamp, callID, call.Name, rawInput, status, "")
			return
		}
		b.addPlanUpdate(lineNumber, timestamp, callID, explanation, plan)
	case "view_image":
		path := nestedCallViewPath(call)
		input := rawInput
		if path != "" {
			encoded, err := json.Marshal(map[string]string{"path": path})
			if err == nil {
				input = string(encoded)
			}
		}
		b.addToolStart(lineNumber, timestamp, callID, call.Name, input, status, "")
	default:
		b.addToolStart(lineNumber, timestamp, callID, call.Name, rawInput, status, "")
	}
}

func (b *codexConversationBuilder) addToolStart(lineNumber int, timestamp, callID, name, input, status, command string) {
	callID = strings.TrimSpace(callID)
	name = cleanToolName(name)
	if name == "" {
		name = "tool"
	}
	input = codexToolPayloadText(input)
	status = cleanConversationText(status)
	if status == "" {
		status = "running"
	}
	if input == "" && command == "" && status == "running" && isConversationToolDisplayOptional(name) {
		return
	}
	event := CodexConversationEvent{
		ID:        b.eventID(lineNumber),
		Timestamp: timestamp,
		Kind:      "tool",
		Title:     "Tool",
		ToolName:  name,
		Input:     input,
		Command:   cleanConversationText(command),
		CallID:    callID,
		Status:    status,
		Source:    "codex_rollout",
	}
	if callID != "" {
		if index, exists := b.eventByCall[callID]; exists && index >= 0 && index < len(b.events) {
			b.events[index].Title = event.Title
			b.events[index].ToolName = event.ToolName
			b.events[index].Input = event.Input
			if event.Command != "" {
				b.events[index].Command = event.Command
			}
			b.events[index].Status = event.Status
			if timestamp != "" {
				b.events[index].Timestamp = timestamp
			}
			return
		}
	}
	if b.addEvent(event) && callID != "" {
		b.eventByCall[callID] = len(b.events) - 1
	}
}

func isConversationToolDisplayOptional(name string) bool {
	switch strings.TrimSpace(strings.TrimPrefix(name, "functions.")) {
	case "read_file", "read", "list_files", "list", "glob", "search", "grep":
		return true
	default:
		return false
	}
}

func (b *codexConversationBuilder) updateCallOutput(lineNumber int, timestamp, callID, output string) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return
	}
	if index, exists := b.eventByCall[callID]; exists && index >= 0 && index < len(b.events) {
		switch b.events[index].Kind {
		case "command":
			b.updateCommandOutput(lineNumber, timestamp, callID, output)
		case "tool":
			b.events[index].Output = codexToolPayloadText(output)
			b.events[index].Status = codexToolOutputStatus(output)
			if timestamp != "" {
				b.events[index].Timestamp = timestamp
			}
		case "patch":
			// Codex renders apply_patch as a file-change cell. The paired
			// custom_tool_call_output is only protocol acknowledgement.
			if timestamp != "" {
				b.events[index].Timestamp = timestamp
			}
		}
		return
	}
	output = codexToolPayloadText(output)
	if output == "" {
		return
	}
	b.addEvent(CodexConversationEvent{
		ID:        b.eventID(lineNumber),
		Timestamp: timestamp,
		Kind:      "tool",
		Title:     "Tool output",
		ToolName:  "tool",
		Output:    output,
		CallID:    callID,
		Status:    codexToolOutputStatus(output),
		Source:    "codex_rollout",
	})
}

func (b *codexConversationBuilder) addPlanUpdate(lineNumber int, timestamp, callID, explanation string, plan []CodexPlanStep) {
	explanation = cleanConversationText(explanation)
	steps := make([]CodexPlanStep, 0, len(plan))
	for _, item := range plan {
		step := cleanConversationText(item.Step)
		if step == "" {
			continue
		}
		steps = append(steps, CodexPlanStep{
			Step:   step,
			Status: normalizePlanStepStatus(item.Status),
		})
	}
	if explanation == "" && len(steps) == 0 {
		return
	}

	b.addEvent(CodexConversationEvent{
		ID:          b.eventID(lineNumber),
		Timestamp:   timestamp,
		Kind:        "plan",
		Title:       "Updated Plan",
		Body:        explanation,
		Explanation: explanation,
		Plan:        steps,
		CallID:      strings.TrimSpace(callID),
		Status:      "done",
		Source:      "codex_rollout",
	})
}

func (b *codexConversationBuilder) addStatus(lineNumber int, timestamp, title, body string) {
	b.addStatusWithState(lineNumber, timestamp, title, body, "")
}

func (b *codexConversationBuilder) addStatusWithState(lineNumber int, timestamp, title, body, status string) {
	title = cleanConversationText(title)
	body = cleanConversationText(body)
	status = cleanConversationText(status)
	if isLowSignalCodexStatus(title, body) {
		return
	}
	key := title + "\x00" + body
	if _, exists := b.seenStatusKeys[key]; exists {
		return
	}
	b.seenStatusKeys[key] = struct{}{}
	b.addEvent(CodexConversationEvent{
		ID:        b.eventID(lineNumber),
		Timestamp: timestamp,
		Kind:      "status",
		Title:     title,
		Body:      body,
		Status:    status,
		Source:    "codex_rollout",
	})
}

func (b *codexConversationBuilder) upsertReasoning(lineNumber int, timestamp, text string, finalize bool) {
	text = CleanCodexDisplayText(text)
	if text == "" || isTranscriptBoilerplate(text) {
		if finalize {
			b.finishPendingReasoning()
		}
		return
	}
	if index := b.pendingReasoningEventIndex(); index >= 0 {
		event := &b.events[index]
		if event.Kind == "commentary" {
			event.Title = "Reasoning"
			if finalize {
				event.Body = text
			} else {
				event.Body = mergeCodexReasoningText(event.Body, text)
			}
			if event.Timestamp == "" {
				event.Timestamp = timestamp
			}
			if finalize {
				event.Status = "done"
				event.Partial = false
				b.pendingReasoningID = ""
			} else {
				event.Status = "running"
				event.Partial = true
			}
			return
		}
	}

	event := CodexConversationEvent{
		ID:        b.eventID(lineNumber),
		Timestamp: timestamp,
		Kind:      "commentary",
		Title:     "Reasoning",
		Body:      text,
		Status:    "running",
		Partial:   !finalize,
		Source:    "codex_rollout",
	}
	if finalize {
		event.Status = "done"
	}
	if b.addEvent(event) && !finalize {
		b.pendingReasoningID = event.ID
	}
	if finalize {
		b.pendingReasoningID = ""
	}
}

func mergeCodexReasoningText(current, incoming string) string {
	current = CleanCodexDisplayText(current)
	incoming = CleanCodexDisplayText(incoming)
	if current == "" || strings.HasPrefix(incoming, current) {
		return incoming
	}
	if incoming == "" || strings.HasPrefix(current, incoming) {
		return current
	}
	return current + "\n\n" + incoming
}

func (b *codexConversationBuilder) finishPendingReasoning() {
	index := b.pendingReasoningEventIndex()
	if index < 0 {
		b.pendingReasoningID = ""
		return
	}
	event := &b.events[index]
	if event.Kind == "commentary" && event.Status == "running" {
		event.Status = "done"
		event.Partial = false
	}
	b.pendingReasoningID = ""
}

func (b *codexConversationBuilder) pendingReasoningEventIndex() int {
	if strings.TrimSpace(b.pendingReasoningID) == "" {
		return -1
	}
	for index := len(b.events) - 1; index >= 0; index-- {
		if b.events[index].ID == b.pendingReasoningID {
			return index
		}
	}
	return -1
}

func (b *codexConversationBuilder) addEvent(event CodexConversationEvent) bool {
	event.Body = normalizeConversationEventBody(event.Kind, event.Body)
	event.Command = truncateRunes(cleanConversationText(event.Command), 800)
	event.ToolName = truncateRunes(cleanToolName(event.ToolName), 120)
	event.Input = truncateConversationBody(event.Input)
	event.Output = truncateConversationBody(event.Output)
	event.Explanation = truncateConversationBody(event.Explanation)
	if isTranscriptBoilerplate(event.Body) {
		event.Body = ""
	}
	if isTranscriptBoilerplate(event.Command) {
		event.Command = ""
	}
	if isTranscriptBoilerplate(event.ToolName) {
		event.ToolName = ""
	}
	if isTranscriptBoilerplate(event.Input) {
		event.Input = ""
	}
	if isTranscriptBoilerplate(event.Output) {
		event.Output = ""
	}
	if isTranscriptBoilerplate(event.Explanation) {
		event.Explanation = ""
	}
	for index := range event.Plan {
		event.Plan[index].Step = truncateRunes(cleanConversationText(event.Plan[index].Step), 240)
		event.Plan[index].Status = normalizePlanStepStatus(event.Plan[index].Status)
	}
	event.Plan = filterVisibleCodexPlanSteps(event.Plan)
	if event.Kind == "" || (event.Body == "" && event.Title == "" && event.Command == "" && event.ToolName == "" && event.Input == "" && event.Output == "" && len(event.Files) == 0 && len(event.FileChanges) == 0 && event.Explanation == "" && len(event.Plan) == 0) {
		return false
	}
	if event.ID == "" {
		event.ID = b.eventID(len(b.events) + 1)
	}
	b.events = append(b.events, event)
	if len(b.events) > maxCodexConversationEvents {
		copy(b.events, b.events[len(b.events)-maxCodexConversationEvents:])
		b.events = b.events[:maxCodexConversationEvents]
	}
	b.reindexEvents()
	return true
}

func filterVisibleCodexPlanSteps(steps []CodexPlanStep) []CodexPlanStep {
	if len(steps) == 0 {
		return steps
	}
	out := steps[:0]
	for _, step := range steps {
		if step.Step == "" || isTranscriptBoilerplate(step.Step) {
			continue
		}
		out = append(out, step)
	}
	return out
}

func (b *codexConversationBuilder) reindexEvents() {
	b.eventByCall = map[string]int{}
	for index := range b.events {
		if b.events[index].Seq <= 0 {
			b.events[index].Seq = stableCodexEventSeq(b.events[index].ID, index)
		}
		if callID := strings.TrimSpace(b.events[index].CallID); callID != "" {
			b.eventByCall[callID] = index
		}
	}
}

func stableCodexEventSeq(eventID string, fallbackIndex int) int {
	trimmed := strings.TrimSpace(eventID)
	if trimmed != "" {
		if separator := strings.LastIndex(trimmed, ":"); separator >= 0 && separator+1 < len(trimmed) {
			if value, err := strconv.Atoi(trimmed[separator+1:]); err == nil && value > 0 {
				return value
			}
		}
	}
	return fallbackIndex + 1
}

func (b *codexConversationBuilder) eventID(lineNumber int) string {
	if b.sessionID != "" {
		return fmt.Sprintf("%s:%d", b.sessionID, lineNumber)
	}
	return fmt.Sprintf("%s:%d", b.sourceID, lineNumber)
}

func (b *codexConversationBuilder) conversation() CodexConversation {
	b.flushPendingUserEcho()
	if b.events == nil {
		b.events = []CodexConversationEvent{}
	}
	if b.activityLifecycle.activity != nil && !b.activityLifecycle.running() {
		b.finishPendingReasoning()
	}
	b.reindexEvents()
	return conversationWithActivity(CodexConversation{
		Available:          true,
		Source:             "codex_rollout",
		SessionID:          b.sessionID,
		CWD:                b.cwd,
		Events:             b.events,
		ProviderActivities: b.providerActivitySnapshots(),
	}, &b.activityLifecycle)
}

func codexConversationContentText(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	if raw[0] == '"' {
		return CleanCodexDisplayText(jsonString(raw))
	}
	var items []map[string]json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return ""
	}
	var parts []string
	for _, item := range items {
		itemType := jsonString(item["type"])
		switch itemType {
		case "input_text", "output_text", "text", "summary_text":
			if text := CleanCodexDisplayText(jsonString(item["text"])); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func codexFunctionOutputText(rawOutput, rawContent json.RawMessage) string {
	if text := codexConversationContentText(rawOutput); text != "" {
		return text
	}
	if text := codexConversationContentText(rawContent); text != "" {
		return text
	}
	if text := codexJSONPayloadText(rawOutput); text != "" {
		return text
	}
	if text := codexJSONPayloadText(rawContent); text != "" {
		return text
	}
	return ""
}

func codexJSONPayloadText(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	if raw[0] == '"' {
		return cleanConversationText(jsonString(raw))
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, bytes.TrimSpace(raw), "", "  ") == nil {
		return cleanConversationText(pretty.String())
	}
	return cleanConversationText(string(raw))
}

func codexToolPayloadText(value string) string {
	value = cleanConversationText(value)
	if value == "" {
		return ""
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, []byte(value), "", "  ") == nil {
		return cleanConversationText(pretty.String())
	}
	return value
}

func codexWebSearchStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "error":
		return "failed"
	case "running", "in_progress", "in-progress", "inprogress":
		return "running"
	default:
		return "done"
	}
}

func codexErrorAffectsTurnStatus(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return true
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return true
	}
	return !codexErrorInfoContainsNonTurnFatal(value)
}

func codexErrorInfoContainsNonTurnFatal(value any) bool {
	switch typed := value.(type) {
	case string:
		return typed == "thread_rollback_failed" || typed == "active_turn_not_steerable"
	case map[string]any:
		for key, nested := range typed {
			if key == "thread_rollback_failed" || key == "active_turn_not_steerable" {
				return true
			}
			if codexErrorInfoContainsNonTurnFatal(nested) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if codexErrorInfoContainsNonTurnFatal(item) {
				return true
			}
		}
	}
	return false
}

func codexRateLimitStatus(raw json.RawMessage) (string, string, bool) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", "", false
	}
	var payload struct {
		LimitID              string `json:"limit_id"`
		LimitName            string `json:"limit_name"`
		PlanType             string `json:"plan_type"`
		RateLimitReachedType string `json:"rate_limit_reached_type"`
		Primary              *struct {
			UsedPercent   float64 `json:"used_percent"`
			WindowMinutes *int64  `json:"window_minutes"`
		} `json:"primary"`
		Secondary *struct {
			UsedPercent   float64 `json:"used_percent"`
			WindowMinutes *int64  `json:"window_minutes"`
		} `json:"secondary"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return "", "", false
	}
	reachedType := strings.TrimSpace(payload.RateLimitReachedType)
	if reachedType == "" {
		return "", "", false
	}

	title := codexRateLimitTitle(reachedType)
	parts := []string{}
	if limit := firstNonEmpty(payload.LimitName, payload.LimitID); limit != "" {
		parts = append(parts, "Limit: "+cleanConversationText(limit))
	}
	if plan := cleanConversationText(payload.PlanType); plan != "" {
		parts = append(parts, "Plan: "+plan)
	}
	if payload.Primary != nil {
		parts = append(parts, codexRateLimitWindowText("Primary", payload.Primary.UsedPercent, payload.Primary.WindowMinutes))
	}
	if payload.Secondary != nil {
		parts = append(parts, codexRateLimitWindowText("Secondary", payload.Secondary.UsedPercent, payload.Secondary.WindowMinutes))
	}
	if len(parts) == 0 {
		parts = append(parts, strings.ReplaceAll(reachedType, "_", " "))
	}
	return title, strings.Join(parts, "\n"), true
}

func codexRateLimitTitle(reachedType string) string {
	switch strings.TrimSpace(reachedType) {
	case "workspace_owner_credits_depleted":
		return "Workspace credits depleted"
	case "workspace_member_credits_depleted":
		return "Member credits depleted"
	case "workspace_owner_usage_limit_reached":
		return "Workspace usage limit reached"
	case "workspace_member_usage_limit_reached":
		return "Member usage limit reached"
	default:
		return "Rate limit reached"
	}
}

func codexRateLimitWindowText(label string, usedPercent float64, windowMinutes *int64) string {
	value := fmt.Sprintf("%s window: %.0f%% used", label, usedPercent)
	if windowMinutes != nil && *windowMinutes > 0 {
		value += fmt.Sprintf(" over %dm", *windowMinutes)
	}
	return value
}

func codexWebSearchActionText(action json.RawMessage) string {
	if len(bytes.TrimSpace(action)) == 0 || bytes.Equal(bytes.TrimSpace(action), []byte("null")) {
		return ""
	}
	return codexJSONPayloadText(action)
}

func codexWebSearchDetail(query string, action json.RawMessage) string {
	query = cleanConversationText(query)
	var payload struct {
		Type    string   `json:"type"`
		Query   string   `json:"query"`
		Queries []string `json:"queries"`
		URL     string   `json:"url"`
		Pattern string   `json:"pattern"`
	}
	if len(bytes.TrimSpace(action)) > 0 && json.Unmarshal(action, &payload) == nil {
		switch payload.Type {
		case "search":
			if value := codexWebSearchQuery(payload.Query, payload.Queries); value != "" {
				return value
			}
		case "open_page":
			if url := cleanConversationText(payload.URL); url != "" {
				return url
			}
		case "find_in_page":
			pattern := cleanConversationText(payload.Pattern)
			url := cleanConversationText(payload.URL)
			switch {
			case pattern != "" && url != "":
				return fmt.Sprintf("'%s' in %s", pattern, url)
			case pattern != "":
				return fmt.Sprintf("'%s'", pattern)
			case url != "":
				return url
			}
		default:
			if value := firstNonEmpty(payload.Query, payload.URL, payload.Pattern); value != "" {
				return cleanConversationText(value)
			}
		}
	}
	return query
}

func codexWebSearchQuery(query string, queries []string) string {
	query = cleanConversationText(query)
	if query != "" {
		return query
	}
	first := ""
	for _, item := range queries {
		if first = cleanConversationText(item); first != "" {
			break
		}
	}
	if first == "" {
		return ""
	}
	if len(queries) > 1 {
		return first + " ..."
	}
	return first
}

func codexFunctionOutputExitCode(output string) *int {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var value string
		switch {
		case strings.HasPrefix(trimmed, "Exit code:"):
			value = strings.TrimSpace(strings.TrimPrefix(trimmed, "Exit code:"))
		case strings.HasPrefix(trimmed, "Process exited with code "):
			value = strings.TrimSpace(strings.TrimPrefix(trimmed, "Process exited with code "))
		default:
			continue
		}
		if fields := strings.Fields(value); len(fields) > 0 {
			value = fields[0]
		}
		exitCode, err := strconv.Atoi(value)
		if err != nil {
			return nil
		}
		return &exitCode
	}
	return nil
}

func codexProcessSessionID(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		const prefix = "Process running with session ID "
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		if fields := strings.Fields(value); len(fields) > 0 {
			return fields[0]
		}
	}
	return ""
}

func codexToolSessionID(arguments string) string {
	var payload map[string]json.RawMessage
	if json.Unmarshal([]byte(arguments), &payload) != nil {
		return ""
	}
	raw := bytes.TrimSpace(payload["session_id"])
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	if raw[0] == '"' {
		return strings.TrimSpace(jsonString(raw))
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return ""
	}
	switch typed := value.(type) {
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func codexCommandOutputBody(output string) string {
	lines := strings.Split(cleanConversationText(output), "\n")
	bodyLines := lines
	for index, line := range lines {
		if strings.TrimSpace(line) == "Output:" {
			bodyLines = lines[index+1:]
			break
		}
	}
	var kept []string
	for _, line := range bodyLines {
		if isCodexCommandMetadataLine(line) {
			continue
		}
		kept = append(kept, line)
	}
	return cleanConversationText(strings.Join(kept, "\n"))
}

func isCodexCommandMetadataLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "Chunk ID:") ||
		strings.HasPrefix(trimmed, "Wall time:") ||
		strings.HasPrefix(trimmed, "Process exited with code ") ||
		strings.HasPrefix(trimmed, "Process running with session ID ") ||
		strings.HasPrefix(trimmed, "Original token count:") ||
		strings.HasPrefix(trimmed, "Total output lines:")
}

func codexToolOutputStatus(output string) string {
	normalized := strings.ToLower(cleanConversationText(output))
	switch {
	case normalized == "":
		return "done"
	case strings.Contains(normalized, "failed to parse function arguments"):
		return "failed"
	case strings.HasPrefix(normalized, "error:"):
		return "failed"
	case strings.Contains(normalized, "\nerror:"):
		return "failed"
	case strings.Contains(normalized, "toolcallerror"):
		return "failed"
	default:
		return "done"
	}
}

func isCodexCommandTool(name string) bool {
	normalized := strings.TrimSpace(name)
	normalized = strings.TrimPrefix(normalized, "functions.")
	switch normalized {
	case "exec_command", "shell_command":
		return true
	default:
		return false
	}
}

func isCodexPlanTool(name string) bool {
	normalized := strings.TrimSpace(name)
	normalized = strings.TrimPrefix(normalized, "functions.")
	return normalized == "update_plan"
}

func codexPlanToolArguments(arguments string) (string, []CodexPlanStep) {
	var payload struct {
		Explanation string          `json:"explanation"`
		Plan        []CodexPlanStep `json:"plan"`
	}
	if json.Unmarshal([]byte(arguments), &payload) != nil {
		return "", nil
	}
	return payload.Explanation, payload.Plan
}

func normalizePlanStepStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "completed":
		return "completed"
	case "in_progress", "in-progress", "inprogress":
		return "in_progress"
	default:
		return "pending"
	}
}

func cleanToolName(name string) string {
	return truncateRunes(cleanConversationText(name), 120)
}

func normalizeCodexTimestamp(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Format(time.RFC3339Nano)
		}
	}
	return value
}

func parseNormalizedCodexTimestamp(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func cleanConversationText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	lines := strings.Split(value, "\n")
	out := make([]string, 0, len(lines))
	blankRun := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			blankRun++
			if blankRun <= 1 {
				out = append(out, "")
			}
			continue
		}
		blankRun = 0
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func isConversationMessageKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "user_message", "assistant_message":
		return true
	default:
		return false
	}
}

// normalizeConversationEventBody keeps full cleaned user/assistant prose and
// applies the shared payload bound only to non-message event bodies.
func normalizeConversationEventBody(kind, body string) string {
	if isConversationMessageKind(kind) {
		return cleanConversationText(body)
	}
	return truncateConversationBody(body)
}

func truncateConversationBody(value string) string {
	value = cleanConversationText(value)
	if value == "" {
		return ""
	}
	return truncateRunes(value, maxCodexConversationBody)
}

func isLowSignalCodexStatus(title, body string) bool {
	switch strings.TrimSpace(title) {
	case "Task started":
		return true
	}
	return false
}
