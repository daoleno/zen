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
	"regexp"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// Claude Code transcript assumptions (filesystem format, not a public API):
//   - Sessions live at ~/.claude/projects/<cwd-with-/replaced-by->/<sessionId>.jsonl
//   - Filename stem matches sessionId
//   - JSONL records use type user|assistant|system|attachment|... with message.content
//     blocks: text, thinking, tool_use; tool_result appears on subsequent user records
//   - Stable record identity uses uuid; tool_use blocks expose id for call pairing
const (
	claudeConversationSource = "claude_code_transcript"
	maxClaudeConversationAge = 72 * time.Hour
)

type claudeTranscriptCandidate struct {
	ID        string
	CWD       string
	Path      string
	CreatedAt time.Time
	Updated   time.Time
}

func (r *ProviderConversationReader) loadClaudeConversationForWorker(worker classifier.Worker, now time.Time) (CodexConversation, error) {
	if strings.TrimSpace(worker.Cwd) == "" {
		r.resetSource()
		return CodexConversation{
			Available: false,
			Reason:    "missing_cwd",
			Events:    []CodexConversationEvent{},
		}, nil
	}

	candidate, ok, err := findClaudeTranscript(worker, now)
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

	conversation, err := r.loadClaudeConversation(candidate.Path)
	if err != nil {
		return CodexConversation{
			Available: false,
			Reason:    "transcript_malformed",
			Path:      candidate.Path,
			SessionID: candidate.ID,
			CWD:       candidate.CWD,
			Updated:   &candidate.Updated,
			Events:    []CodexConversationEvent{},
		}, nil
	}
	conversation.Available = true
	conversation.Source = claudeConversationSource
	conversation.Path = candidate.Path
	conversation.SessionID = firstNonEmpty(conversation.SessionID, candidate.ID)
	conversation.CWD = firstNonEmpty(conversation.CWD, candidate.CWD)
	conversation.Updated = &candidate.Updated
	if conversation.Events == nil {
		conversation.Events = []CodexConversationEvent{}
	}
	return conversation, nil
}

func (r *ProviderConversationReader) loadClaudeConversation(path string) (CodexConversation, error) {
	return r.loadFileConversation(WorkerProviderClaude, path, parseClaudeConversation)
}

func findClaudeTranscript(worker classifier.Worker, now time.Time) (claudeTranscriptCandidate, bool, error) {
	cwd := strings.TrimSpace(worker.Cwd)
	if cwd == "" {
		return claudeTranscriptCandidate{}, false, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return claudeTranscriptCandidate{}, false, err
	}
	resumeID := claudeResumeSessionID(worker.Command)
	configDir := firstProcessTreeEnvironValue(worker.ProcessID, "CLAUDE_CONFIG_DIR")
	if configDir == "" && worker.ProcessID > 0 {
		if processHome := firstProcessTreeEnvironValue(worker.ProcessID, "HOME"); processHome != "" {
			home = processHome
		}
	}
	if configDir == "" {
		configDir = filepath.Join(home, ".claude")
	}
	if !filepath.IsAbs(configDir) {
		// A process-local relative config directory cannot be resolved safely
		// from the daemon's cwd. Never bind another account's HOME transcript.
		return claudeTranscriptCandidate{}, false, nil
	}
	if candidate, owned, found := claudeProcessOwnedTranscript(worker, configDir); owned {
		return candidate, found, nil
	}

	var candidates []claudeTranscriptCandidate
	for _, candidateCWD := range transcriptCWDCandidates(cwd) {
		projectDir := claudeProjectDir(configDir, candidateCWD)
		entries, err := os.ReadDir(projectDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return claudeTranscriptCandidate{}, false, err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
				continue
			}
			path := filepath.Join(projectDir, entry.Name())
			info, err := entry.Info()
			if err != nil {
				continue
			}
			updated := info.ModTime()
			if resumeID == "" && !isClaudeTranscriptFresh(updated, now) {
				continue
			}
			meta, err := readClaudeMeta(path)
			if err != nil {
				continue
			}
			sessionCWD := firstNonEmpty(meta.CWD, candidateCWD)
			if meta.CWD != "" && !pathsEquivalent(meta.CWD, candidateCWD) && !pathsEquivalent(meta.CWD, cwd) {
				continue
			}
			sessionID := firstNonEmpty(meta.SessionID, strings.TrimSuffix(entry.Name(), ".jsonl"))
			createdAt := claudeTranscriptCreatedAt(path, updated)
			candidates = append(candidates, claudeTranscriptCandidate{
				ID:        sessionID,
				CWD:       sessionCWD,
				Path:      path,
				CreatedAt: createdAt,
				Updated:   updated,
			})
		}
	}
	if len(candidates) == 0 {
		return claudeTranscriptCandidate{}, false, nil
	}

	if resumeID != "" {
		matched, ok := matchClaudeTranscriptID(candidates, resumeID)
		return matched, ok, nil
	}

	freshCandidates := freshClaudeTranscriptCandidates(candidates, now)
	if len(freshCandidates) == 0 {
		return claudeTranscriptCandidate{}, false, nil
	}
	// Prefer an unambiguous bind. Never fall back to "newest file in cwd" when
	// multiple sessions exist — that can surface an unrelated conversation.
	if matched, ok := matchClaudeTranscriptToWorkerStart(freshCandidates, worker.StartedAt); ok {
		return matched, true, nil
	}
	if matched, ok := matchClaudeTranscriptToActiveSession(freshCandidates, worker.StartedAt); ok {
		return matched, true, nil
	}
	if worker.StartedAt.IsZero() && len(freshCandidates) == 1 {
		return freshCandidates[0], true, nil
	}
	return claudeTranscriptCandidate{}, false, nil
}

// claudeProcessOwnedTranscript binds the transcript named by Claude's process
// registry (<config>/sessions/<pid>.json) for the Worker's live process. The
// start-time heuristic below cannot separate Sessions launched within seconds
// in one cwd; the registry is exact. owned reports a live record for this
// process: its transcript is then the only candidate, even before Claude's
// first write creates it. Registry failures (missing, stale, malformed or
// other-cwd records) are not evidence loss; they leave the heuristic in charge.
func claudeProcessOwnedTranscript(worker classifier.Worker, configDir string) (candidate claudeTranscriptCandidate, owned, found bool) {
	if worker.ProcessID <= 0 {
		return claudeTranscriptCandidate{}, false, false
	}
	record, live, err := claudeProcessRecord(configDir, worker.ProcessID)
	if err != nil || !live || !pathsEquivalent(record.Cwd, worker.Cwd) {
		return claudeTranscriptCandidate{}, false, false
	}
	path := filepath.Join(claudeProjectDir(configDir, record.Cwd), record.SessionID+".jsonl")
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return claudeTranscriptCandidate{}, true, false
	}
	updated := info.ModTime()
	return claudeTranscriptCandidate{
		ID:        record.SessionID,
		CWD:       record.Cwd,
		Path:      path,
		CreatedAt: claudeTranscriptCreatedAt(path, updated),
		Updated:   updated,
	}, true, true
}

func isClaudeTranscriptFresh(updated, now time.Time) bool {
	if updated.IsZero() || now.IsZero() {
		return true
	}
	if updated.After(now.Add(10 * time.Minute)) {
		return true
	}
	return now.Sub(updated) <= maxClaudeConversationAge
}

func freshClaudeTranscriptCandidates(candidates []claudeTranscriptCandidate, now time.Time) []claudeTranscriptCandidate {
	if len(candidates) == 0 {
		return nil
	}
	fresh := make([]claudeTranscriptCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if isClaudeTranscriptFresh(candidate.Updated, now) {
			fresh = append(fresh, candidate)
		}
	}
	return fresh
}

func matchClaudeTranscriptID(candidates []claudeTranscriptCandidate, sessionID string) (claudeTranscriptCandidate, bool) {
	sessionID = strings.TrimSpace(strings.ToLower(sessionID))
	if sessionID == "" {
		return claudeTranscriptCandidate{}, false
	}
	for _, candidate := range candidates {
		if strings.ToLower(strings.TrimSpace(candidate.ID)) == sessionID ||
			strings.ToLower(strings.TrimSuffix(filepath.Base(candidate.Path), ".jsonl")) == sessionID {
			return candidate, true
		}
	}
	return claudeTranscriptCandidate{}, false
}

func claudeResumeSessionID(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return ""
	}
	base := strings.ToLower(filepath.Base(strings.Trim(fields[0], `"'`)))
	base = strings.TrimSuffix(base, ".exe")
	if base != "claude" && base != "cc" && !strings.Contains(base, "claude") {
		return ""
	}
	for index, field := range fields[1:] {
		trimmed := strings.Trim(field, `"'`)
		lower := strings.ToLower(trimmed)
		switch {
		case lower == "resume" || lower == "--resume" || lower == "-r" || lower == "--session-id":
			nextIndex := index + 2
			if nextIndex < len(fields) {
				sessionID := strings.Trim(fields[nextIndex], `"'`)
				if sessionID != "" && !strings.HasPrefix(sessionID, "-") {
					return sessionID
				}
			}
		case strings.HasPrefix(lower, "--resume="), strings.HasPrefix(lower, "--session-id="):
			if idx := strings.Index(trimmed, "="); idx >= 0 {
				return strings.Trim(trimmed[idx+1:], `"'`)
			}
		}
	}
	return ""
}

func matchClaudeTranscriptToWorkerStart(candidates []claudeTranscriptCandidate, startedAt time.Time) (claudeTranscriptCandidate, bool) {
	if startedAt.IsZero() {
		return claudeTranscriptCandidate{}, false
	}
	startedAt = startedAt.UTC()
	minCreatedAt := startedAt.Add(-5 * time.Second)
	maxCreatedAt := startedAt.Add(2 * time.Minute)
	bestIndex := -1
	var bestDelta time.Duration
	for index, candidate := range candidates {
		createdAt := candidate.CreatedAt.UTC()
		if createdAt.IsZero() || createdAt.Before(minCreatedAt) || createdAt.After(maxCreatedAt) {
			continue
		}
		if candidate.Updated.Before(startedAt) {
			continue
		}
		delta := createdAt.Sub(startedAt)
		if delta < 0 {
			delta = -delta
		}
		if bestIndex == -1 || delta < bestDelta ||
			(delta == bestDelta && candidate.Updated.After(candidates[bestIndex].Updated)) {
			bestIndex = index
			bestDelta = delta
		}
	}
	if bestIndex == -1 {
		return claudeTranscriptCandidate{}, false
	}
	// Reject near-ties: two sessions created equally close to StartedAt are ambiguous.
	for index, candidate := range candidates {
		if index == bestIndex {
			continue
		}
		createdAt := candidate.CreatedAt.UTC()
		if createdAt.IsZero() || createdAt.Before(minCreatedAt) || createdAt.After(maxCreatedAt) {
			continue
		}
		delta := createdAt.Sub(startedAt)
		if delta < 0 {
			delta = -delta
		}
		if delta == bestDelta || (delta <= 2*time.Second && bestDelta <= 2*time.Second) {
			return claudeTranscriptCandidate{}, false
		}
	}
	return candidates[bestIndex], true
}

func matchClaudeTranscriptToActiveSession(candidates []claudeTranscriptCandidate, startedAt time.Time) (claudeTranscriptCandidate, bool) {
	if len(candidates) == 0 || startedAt.IsZero() {
		return claudeTranscriptCandidate{}, false
	}
	startedAt = startedAt.UTC()
	minCreatedAt := startedAt.Add(-maxCodexActiveTranscriptStartBackdate)
	maxCreatedAt := startedAt.Add(2 * time.Minute)
	var eligible []claudeTranscriptCandidate
	for _, candidate := range candidates {
		if candidate.Updated.IsZero() || candidate.Updated.Before(startedAt) {
			continue
		}
		createdAt := candidate.CreatedAt.UTC()
		if createdAt.IsZero() || createdAt.Before(minCreatedAt) || createdAt.After(maxCreatedAt) {
			continue
		}
		eligible = append(eligible, candidate)
	}
	if len(eligible) != 1 {
		return claudeTranscriptCandidate{}, false
	}
	return eligible[0], true
}

func claudeTranscriptCreatedAt(path string, fallback time.Time) time.Time {
	file, err := os.Open(path)
	if err != nil {
		return fallback
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	for lineCount := 0; lineCount < 40; lineCount++ {
		line, err := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var envelope struct {
				Timestamp string `json:"timestamp"`
			}
			if json.Unmarshal(line, &envelope) == nil {
				if parsed := parseNormalizedCodexTimestamp(envelope.Timestamp); !parsed.IsZero() {
					return parsed
				}
			}
		}
		if err != nil {
			break
		}
	}
	return fallback
}

func parseClaudeConversation(path string) (CodexConversation, error) {
	builder := newClaudeConversationBuilder(strings.TrimSuffix(filepath.Base(path), ".jsonl"))
	if err := consumeClaudeJSONL(path, builder.consumeLine); err != nil {
		return CodexConversation{}, err
	}
	if builder.supportedRecords == 0 {
		return CodexConversation{}, fmt.Errorf("claude transcript has no supported records")
	}
	return builder.conversation(), nil
}

func consumeClaudeJSONL(path string, consume func(int, []byte)) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	lineNumber := 0
	for {
		line, err := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			lineNumber++
			consume(lineNumber, line)
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

type claudeConversationBuilder struct {
	sourceID          string
	sessionID         string
	cwd               string
	supportedRecords  int
	events            []CodexConversationEvent
	eventByCall       map[string]int
	activityLifecycle providerActivityLifecycle
	// pendingChoiceCalls are AskUserQuestion calls without a result yet.
	pendingChoiceCalls []string
	// pendingTaskTurn holds the activity a task notification would open. It
	// starts only once the provider actually answers with an assistant record.
	pendingTaskTurn claudePendingTaskTurn
	// seenTaskNotifications drops the rare second copy when Claude records one
	// notification both as a mid-turn attachment and as a later prompt.
	seenTaskNotifications map[string]bool
}

type claudePendingTaskTurn struct {
	id        string
	startedAt string
}

func newClaudeConversationBuilder(sourceID string) *claudeConversationBuilder {
	return &claudeConversationBuilder{
		sourceID:    strings.TrimSpace(sourceID),
		eventByCall: map[string]int{},
	}
}

func isSupportedClaudeRecordType(recordType string) bool {
	switch strings.ToLower(strings.TrimSpace(recordType)) {
	case "user", "assistant", "system", "attachment", "permission-mode",
		"file-history-snapshot", "last-prompt", "mode", "progress", "queue-operation":
		return true
	default:
		return false
	}
}

func (b *claudeConversationBuilder) consumeLine(lineNumber int, line []byte) {
	var envelope struct {
		Type        string `json:"type"`
		UUID        string `json:"uuid"`
		Timestamp   string `json:"timestamp"`
		SessionID   string `json:"sessionId"`
		SessionID2  string `json:"session_id"`
		CWD         string `json:"cwd"`
		IsMeta      bool   `json:"isMeta"`
		IsSidechain bool   `json:"isSidechain"`
		TurnOrigin  string `json:"turnOrigin"`
		Origin      struct {
			Kind string `json:"kind"`
		} `json:"origin"`
		Attachment    json.RawMessage `json:"attachment"`
		ToolUseResult json.RawMessage `json:"toolUseResult"`
		Message       struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			StopReason string          `json:"stop_reason"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &envelope) != nil {
		return
	}
	if !isSupportedClaudeRecordType(envelope.Type) {
		return
	}
	b.supportedRecords++
	if b.sessionID == "" {
		b.sessionID = firstNonEmpty(envelope.SessionID, envelope.SessionID2, b.sourceID)
	}
	if b.cwd == "" {
		b.cwd = strings.TrimSpace(envelope.CWD)
	}
	// Keep provider-internal linkage/metadata out of the shared conversation:
	// parentUuid, toolUseResult, attachments, permission mode, sidechains, etc.
	if envelope.IsSidechain {
		return
	}
	timestamp := normalizeCodexTimestamp(envelope.Timestamp)
	recordID := firstNonEmpty(strings.TrimSpace(envelope.UUID), fmt.Sprintf("line-%d", lineNumber))

	switch envelope.Type {
	case "user":
		if envelope.IsMeta {
			return
		}
		activityID := providerActivityID(firstNonEmpty(b.sessionID, b.sourceID), recordID, lineNumber)
		if b.consumeTaskNotification(lineNumber, recordID, timestamp, envelope.Origin.Kind, envelope.TurnOrigin, envelope.Message.Content) {
			if !b.activityLifecycle.running() {
				b.pendingTaskTurn = claudePendingTaskTurn{id: activityID, startedAt: timestamp}
			}
			return
		}
		if b.consumeUserContent(lineNumber, recordID, timestamp, envelope.Message.Content, envelope.ToolUseResult) &&
			!b.activityLifecycle.running() {
			b.pendingTaskTurn = claudePendingTaskTurn{}
			b.activityLifecycle.start(activityID, timestamp)
		}
	case "assistant":
		if pending := b.pendingTaskTurn; pending.id != "" {
			b.pendingTaskTurn = claudePendingTaskTurn{}
			if !b.activityLifecycle.running() {
				b.activityLifecycle.start(pending.id, pending.startedAt)
			}
		}
		b.consumeAssistantContent(lineNumber, recordID, timestamp, envelope.Message.Content)
		if !claudeAssistantRecordContinuesTurn(
			envelope.Message.StopReason,
			envelope.Message.Content,
		) {
			b.activityLifecycle.settle("", ProviderActivityCompleted, timestamp)
		}
	case "attachment":
		// A notification delivered mid-turn rides along with the running turn;
		// it never opens activity of its own.
		b.consumeTaskNotificationAttachment(lineNumber, recordID, timestamp, envelope.Attachment)
	default:
		// Skip system/permission-mode/file-history-snapshot and other
		// provider-internal records from the shared conversation surface.
	}
}

func claudeAssistantRecordContinuesTurn(stopReason string, raw json.RawMessage) bool {
	stopReason = strings.ToLower(strings.TrimSpace(stopReason))
	if stopReason == "max_tokens" || stopReason == "stop_sequence" {
		// Unlike Claude's progressive end_turn projection, these stop reasons are
		// authoritative terminal facts even when the final record only contains
		// thinking. Waiting for a later text row would leave Working stale.
		return false
	}
	if stopReason != "end_turn" {
		return true
	}
	var items []claudeContentBlock
	if json.Unmarshal(raw, &items) != nil {
		return false
	}
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.Type), "text") && strings.TrimSpace(item.Text) != "" {
			return false
		}
	}
	// Claude writes thinking and text blocks as separate records. A terminal
	// stop reason on a thinking-only record still has visible content to come.
	return true
}

func (b *claudeConversationBuilder) consumeUserContent(lineNumber int, recordID, timestamp string, raw, toolUseResult json.RawMessage) bool {
	if len(bytes.TrimSpace(raw)) == 0 {
		return false
	}
	if raw[0] == '"' {
		var text string
		if json.Unmarshal(raw, &text) == nil {
			b.addMessage(lineNumber, recordID, 0, timestamp, "user", text)
			return claudeVisibleUserText(text)
		}
		return false
	}
	var items []claudeContentBlock
	if json.Unmarshal(raw, &items) != nil {
		return false
	}
	hasUserText := false
	textIndex := 0
	toolUseResults := claudeToolUseResults(raw, toolUseResult)
	for index, item := range items {
		switch strings.ToLower(strings.TrimSpace(item.Type)) {
		case "text":
			textIndex++
			b.addMessage(lineNumber, recordID, textIndex, timestamp, "user", item.Text)
			hasUserText = hasUserText || claudeVisibleUserText(item.Text)
		case "tool_result":
			b.settleChoice(item.ToolUseID, toolUseResults[strings.TrimSpace(item.ToolUseID)], item.IsError)
			output := claudeContentText(item.Content)
			if item.IsError {
				if output == "" {
					output = "Tool failed"
				}
				b.updateToolResult(lineNumber, recordID, index, timestamp, item.ToolUseID, output, true)
			} else {
				b.updateToolResult(lineNumber, recordID, index, timestamp, item.ToolUseID, output, false)
			}
		}
	}
	return hasUserText
}

// consumeTaskNotification projects a provider background-task notification as
// a system card. Claude marks these records with origin.kind
// "task-notification"; records without origin fall back to the strict block
// parser. Human-origin input (including a pasted notification) stays a user
// message.
func (b *claudeConversationBuilder) consumeTaskNotification(lineNumber int, recordID, timestamp, originKind, turnOrigin string, raw json.RawMessage) bool {
	originKind = strings.ToLower(strings.TrimSpace(originKind))
	if originKind == "human" {
		return false
	}
	text, ok := claudeUserPlainText(raw)
	if !ok {
		return false
	}
	notifications, parsed := ParseTaskNotifications(text)
	if !parsed {
		if originKind != "task-notification" && strings.TrimSpace(turnOrigin) != "task_notification" {
			return false
		}
		// A structurally marked notification in an unknown shape still is not
		// user input; show its cleaned text on a neutral card.
		body := CleanCodexDisplayText(text)
		if body == "" {
			return true
		}
		notifications = []TaskNotification{{Body: body}}
	}
	baseID := b.messageEventID(recordID, "user", 0)
	for index, notification := range notifications {
		if notification.TaskID != "" {
			key := notification.TaskID + "\x00" + notification.Summary + "\x00" + notification.Body
			if b.seenTaskNotifications[key] {
				continue
			}
			if b.seenTaskNotifications == nil {
				b.seenTaskNotifications = map[string]bool{}
			}
			b.seenTaskNotifications[key] = true
		}
		b.addEvent(notification.ConversationEvent(
			TaskNotificationEventID(baseID, index),
			claudeEventSeq(lineNumber, index),
			timestamp,
		))
	}
	return true
}

// consumeTaskNotificationAttachment projects queued task notifications that
// Claude delivered into an already running turn. Other queued commands (for
// example queued human prompts) are left to their existing handling.
func (b *claudeConversationBuilder) consumeTaskNotificationAttachment(lineNumber int, recordID, timestamp string, raw json.RawMessage) {
	var attachment struct {
		Type        string          `json:"type"`
		CommandMode string          `json:"commandMode"`
		Prompt      json.RawMessage `json:"prompt"`
		Origin      struct {
			Kind string `json:"kind"`
		} `json:"origin"`
	}
	if json.Unmarshal(raw, &attachment) != nil || attachment.Type != "queued_command" {
		return
	}
	if attachment.CommandMode != "task-notification" && attachment.Origin.Kind != "task-notification" {
		return
	}
	b.consumeTaskNotification(lineNumber, recordID, timestamp, "task-notification", "", attachment.Prompt)
}

// claudeUserPlainText returns the text of a user record made only of text
// content (a string, or text blocks). Tool results disqualify the record.
func claudeUserPlainText(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", false
	}
	if raw[0] == '"' {
		var text string
		return text, json.Unmarshal(raw, &text) == nil
	}
	var items []claudeContentBlock
	if json.Unmarshal(raw, &items) != nil || len(items) == 0 {
		return "", false
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if !strings.EqualFold(strings.TrimSpace(item.Type), "text") {
			return "", false
		}
		parts = append(parts, item.Text)
	}
	return strings.Join(parts, "\n"), true
}

func claudeVisibleUserText(text string) bool {
	text = CleanCodexDisplayText(text)
	return text != "" && !isTranscriptBoilerplate(text)
}

func (b *claudeConversationBuilder) consumeAssistantContent(lineNumber int, recordID, timestamp string, raw json.RawMessage) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return
	}
	if raw[0] == '"' {
		var text string
		if json.Unmarshal(raw, &text) == nil {
			b.addMessage(lineNumber, recordID, 0, timestamp, "assistant", text)
		}
		return
	}
	var items []claudeContentBlock
	if json.Unmarshal(raw, &items) != nil {
		return
	}
	textIndex := 0
	thinkingIndex := 0
	toolIndex := 0
	for _, item := range items {
		switch strings.ToLower(strings.TrimSpace(item.Type)) {
		case "thinking":
			thinkingIndex++
			b.addThinking(lineNumber, recordID, thinkingIndex, timestamp, item.Thinking)
		case "text":
			textIndex++
			b.addMessage(lineNumber, recordID, textIndex, timestamp, "assistant", item.Text)
		case "tool_use":
			toolIndex++
			b.addToolUse(lineNumber, recordID, toolIndex, timestamp, item)
		}
	}
}

type claudeContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Name      string          `json:"name"`
	ID        string          `json:"id"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
}

func (b *claudeConversationBuilder) addMessage(lineNumber int, recordID string, index int, timestamp, role, text string) {
	exact := text
	text = CleanCodexDisplayText(text)
	if text == "" || isTranscriptBoilerplate(text) {
		return
	}
	inner, pasted := "", false
	if role == "user" {
		inner, pasted = claudePastedInput(exact)
		// Visibility is decided on the native wrapped bytes above; the paste
		// envelope itself is transport, so show only what the user submitted.
		if display := cleanConversationText(inner); pasted && display != "" {
			text = display
		} else if stripped := stripClaudeEmbeddedPasteEnvelopes(text); !pasted && stripped != "" {
			text = stripped
		}
	}
	kind := "assistant_message"
	if role == "user" {
		kind = "user_message"
	}
	event := CodexConversationEvent{
		ID:        b.messageEventID(recordID, role, index),
		Seq:       claudeEventSeq(lineNumber, index),
		Timestamp: timestamp,
		Kind:      kind,
		Role:      role,
		Body:      text,
		Source:    claudeConversationSource,
	}
	if role == "user" {
		// Retain literal native bytes as well as the strict transport-envelope
		// alternative. A user can also submit literal wrapper-like text.
		event.AdmissionSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(exact)))
		if pasted {
			event.AdmissionUnwrappedSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(inner)))
		}
	}
	if b.addEvent(event) {
		b.abandonPendingChoices()
	}
}

func (b *claudeConversationBuilder) addThinking(lineNumber int, recordID string, index int, timestamp, text string) {
	text = CleanCodexDisplayText(text)
	if text == "" || isTranscriptBoilerplate(text) {
		return
	}
	b.addEvent(CodexConversationEvent{
		ID:        b.eventID(recordID, "thinking", index),
		Seq:       claudeEventSeq(lineNumber, index),
		Timestamp: timestamp,
		Kind:      "commentary",
		Title:     "Reasoning",
		Body:      text,
		Status:    "done",
		Source:    claudeConversationSource,
	})
}

func (b *claudeConversationBuilder) addToolUse(lineNumber int, recordID string, index int, timestamp string, item claudeContentBlock) {
	name := cleanToolName(item.Name)
	if name == "" {
		name = "tool"
	}
	callID := strings.TrimSpace(item.ID)
	inputText := claudeToolInputJSON(item.Input)
	if strings.EqualFold(name, "Bash") {
		command := claudeToolInputString(item.Input, "command")
		description := claudeToolInputString(item.Input, "description")
		body := description
		if body == "" {
			body = command
		}
		event := CodexConversationEvent{
			ID:        b.toolEventID(callID, recordID, index),
			Seq:       claudeEventSeq(lineNumber, index),
			Timestamp: timestamp,
			Kind:      "command",
			Command:   command,
			Body:      body,
			CallID:    callID,
			Status:    "running",
			Source:    claudeConversationSource,
		}
		if b.addEvent(event) && callID != "" {
			b.eventByCall[callID] = len(b.events) - 1
		}
		return
	}

	event := CodexConversationEvent{
		ID:        b.toolEventID(callID, recordID, index),
		Seq:       claudeEventSeq(lineNumber, index),
		Timestamp: timestamp,
		Kind:      "tool",
		Title:     "Tool",
		ToolName:  name,
		Input:     inputText,
		CallID:    callID,
		Status:    "running",
		Source:    claudeConversationSource,
	}
	if surface := claudeToolSurface(name, item.Input); surface != "" {
		event.Files = []string{surface}
	}
	if name == ClaudeAskUserQuestionTool {
		event.Choice = parseClaudeAskUserQuestion(item.Input)
		if event.Choice != nil && callID != "" {
			b.pendingChoiceCalls = append(b.pendingChoiceCalls, callID)
		}
	}
	if b.addEvent(event) && callID != "" {
		b.eventByCall[callID] = len(b.events) - 1
	}
}

func (b *claudeConversationBuilder) settleChoice(callID string, toolUseResult json.RawMessage, isError bool) {
	eventIndex, exists := b.eventByCall[strings.TrimSpace(callID)]
	if !exists || eventIndex < 0 || eventIndex >= len(b.events) || b.events[eventIndex].Choice == nil {
		return
	}
	choice := *b.events[eventIndex].Choice
	settleClaudeChoice(&choice, toolUseResult, isError)
	b.events[eventIndex].Choice = &choice
}

func (b *claudeConversationBuilder) updateToolResult(lineNumber int, recordID string, index int, timestamp, callID, output string, isError bool) {
	callID = strings.TrimSpace(callID)
	output = truncateConversationBody(codexToolPayloadText(output))
	status := "done"
	if isError {
		status = "failed"
	} else if output != "" {
		status = codexToolOutputStatus(output)
	}
	if callID != "" {
		if eventIndex, exists := b.eventByCall[callID]; exists && eventIndex >= 0 && eventIndex < len(b.events) {
			b.events[eventIndex].Output = output
			b.events[eventIndex].Status = status
			if isError {
				code := 1
				b.events[eventIndex].ExitCode = &code
			} else if b.events[eventIndex].ExitCode == nil && b.events[eventIndex].Kind == "command" {
				code := 0
				b.events[eventIndex].ExitCode = &code
			}
			if timestamp != "" {
				b.events[eventIndex].Timestamp = timestamp
			}
			return
		}
	}
	if output == "" && !isError {
		return
	}
	event := CodexConversationEvent{
		ID:        b.toolEventID(callID, recordID, index),
		Seq:       claudeEventSeq(lineNumber, index),
		Timestamp: timestamp,
		Kind:      "tool",
		Title:     "Tool output",
		ToolName:  "tool",
		Output:    output,
		CallID:    callID,
		Status:    status,
		Source:    claudeConversationSource,
	}
	if isError {
		code := 1
		event.ExitCode = &code
	}
	b.addEvent(event)
}

func (b *claudeConversationBuilder) addEvent(event CodexConversationEvent) bool {
	event.Body = normalizeConversationEventBody(event.Kind, event.Body)
	event.ToolName = truncateRunes(cleanToolName(event.ToolName), 120)
	event.Input = truncateConversationBody(event.Input)
	event.Output = truncateConversationBody(event.Output)
	event.Command = truncateConversationBody(event.Command)
	if event.Kind == "" || (event.Body == "" && event.Title == "" && event.Command == "" && event.ToolName == "" && event.Input == "" && event.Output == "") {
		return false
	}
	if event.ID == "" {
		event.ID = b.eventID(fmt.Sprintf("event-%d", len(b.events)+1), "event", 0)
	}
	b.events = append(b.events, event)
	return true
}

func (b *claudeConversationBuilder) messageEventID(recordID, role string, index int) string {
	return b.eventID(recordID, role, index)
}

func (b *claudeConversationBuilder) toolEventID(callID, recordID string, index int) string {
	if callID = strings.TrimSpace(callID); callID != "" {
		return "claude-tool:" + callID
	}
	return b.eventID(recordID, "tool", index)
}

func (b *claudeConversationBuilder) eventID(recordID, kind string, index int) string {
	sourceID := firstNonEmpty(b.sessionID, b.sourceID, "claude")
	recordID = firstNonEmpty(strings.TrimSpace(recordID), "record")
	if index > 0 {
		return fmt.Sprintf("%s:%s:%s:%d", sourceID, recordID, kind, index)
	}
	return fmt.Sprintf("%s:%s:%s", sourceID, recordID, kind)
}

func claudeEventSeq(lineNumber, index int) int {
	if lineNumber <= 0 {
		lineNumber = 1
	}
	if index < 0 {
		index = 0
	}
	return lineNumber*100 + index
}

func (b *claudeConversationBuilder) conversation() CodexConversation {
	if b.events == nil {
		b.events = []CodexConversationEvent{}
	}
	if len(b.events) > maxCodexConversationEvents {
		b.events = b.events[len(b.events)-maxCodexConversationEvents:]
	}
	for index := range b.events {
		if b.events[index].Seq <= 0 {
			b.events[index].Seq = index + 1
		}
	}
	return conversationWithActivity(CodexConversation{
		Available: true,
		Source:    claudeConversationSource,
		SessionID: firstNonEmpty(b.sessionID, b.sourceID),
		CWD:       b.cwd,
		Events:    b.events,
	}, &b.activityLifecycle)
}

func claudeToolInputJSON(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err == nil {
		return compact.String()
	}
	return string(raw)
}

func claudeToolInputString(raw json.RawMessage, key string) string {
	var input map[string]json.RawMessage
	if json.Unmarshal(raw, &input) != nil {
		return ""
	}
	return jsonString(input[key])
}

// Claude Code 2.1.285 wraps bracketed terminal pastes with these exact outer
// bytes. Decode one whole envelope only; do not trim, normalize, search for a
// payload substring, or recursively unwrap. Both IDs must match. Unknown or
// nested forms stay raw and therefore fail closed for an inner submission.
var claudePasteEnvelope = regexp.MustCompile(`(?s)\A\n\n<pasted_content id="([0-9a-f]{4})">\n(.*)\n</pasted_content id="([0-9a-f]{4})">\n\z`)

func claudePastedInput(raw string) (string, bool) {
	parts := claudePasteEnvelope.FindStringSubmatch(raw)
	if parts == nil || parts[1] != parts[3] || strings.Contains(parts[2], "<pasted_content") || strings.Contains(parts[2], "</pasted_content") {
		return "", false
	}
	return parts[2], true
}

// claudePasteDisplayEnvelope is the same envelope after display trimming, the
// form durable presentation rows kept before paste envelopes were unwrapped.
var claudePasteDisplayEnvelope = regexp.MustCompile(`(?s)\A<pasted_content id="([0-9a-f]{4})">\n(.*)\n</pasted_content id="([0-9a-f]{4})">\z`)

// UnwrapClaudePasteDisplay recovers the submitted text from a display-trimmed
// Claude paste envelope. It is a presentation repair for legacy rows only;
// admission evidence uses the exact native bytes (claudePastedInput).
func UnwrapClaudePasteDisplay(body string) (string, bool) {
	parts := claudePasteDisplayEnvelope.FindStringSubmatch(body)
	if parts == nil || parts[1] != parts[3] || strings.Contains(parts[2], "<pasted_content") || strings.Contains(parts[2], "</pasted_content") {
		return "", false
	}
	return parts[2], true
}

var claudeEmbeddedPasteOpen = regexp.MustCompile(`<pasted_content id="([0-9a-f]{4})">\n`)

// stripClaudeEmbeddedPasteEnvelopes removes paste envelopes that Claude puts
// around a paste inside typed text (for example "Execute: " followed by a
// pasted brief). Display only: each pair needs matching IDs and no nesting,
// and admission digests keep the native bytes.
func stripClaudeEmbeddedPasteEnvelopes(text string) string {
	if !strings.Contains(text, "<pasted_content") {
		return text
	}
	var out strings.Builder
	rest := text
	for {
		loc := claudeEmbeddedPasteOpen.FindStringSubmatchIndex(rest)
		if loc == nil {
			break
		}
		closeTag := "\n</pasted_content id=\"" + rest[loc[2]:loc[3]] + "\">"
		end := strings.Index(rest[loc[1]:], closeTag)
		if end < 0 {
			break
		}
		inner := rest[loc[1] : loc[1]+end]
		if strings.Contains(inner, "<pasted_content") || strings.Contains(inner, "</pasted_content") {
			break
		}
		out.WriteString(rest[:loc[0]])
		out.WriteString(inner)
		rest = rest[loc[1]+end+len(closeTag):]
	}
	out.WriteString(rest)
	return strings.TrimSpace(out.String())
}
