package work

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

const (
	maxGrokConversationEvents = 240
	maxGrokSessionAge         = 72 * time.Hour
	grokChatHistoryFile       = "chat_history.jsonl"
	grokUpdatesFile           = "updates.jsonl"
	grokSummaryFile           = "summary.json"
)

type cachedGrokConversation struct {
	size         int64
	modTime      time.Time
	conversation CodexConversation
}

var grokConversationCache = struct {
	sync.Mutex
	byPath map[string]cachedGrokConversation
}{
	byPath: map[string]cachedGrokConversation{},
}

type grokSessionCandidate struct {
	ID      string
	CWD     string
	Dir     string
	Updated time.Time
	Active  bool
}

func loadGrokConversationForAgent(agent classifier.Agent, now time.Time) (CodexConversation, error) {
	if strings.TrimSpace(agent.Cwd) == "" {
		return CodexConversation{
			Available: false,
			Reason:    "missing_cwd",
			Events:    []CodexConversationEvent{},
		}, nil
	}

	candidate, ok, err := findGrokSession(agent, now)
	if err != nil {
		return CodexConversation{}, err
	}
	if !ok {
		return CodexConversation{
			Available: false,
			Reason:    "session_not_found",
			Events:    []CodexConversationEvent{},
		}, nil
	}

	conversation, err := loadCachedGrokConversation(candidate.Dir)
	if err != nil {
		return CodexConversation{}, err
	}
	conversation.Available = true
	conversation.Source = "grok_session"
	conversation.Path = candidate.Dir
	conversation.SessionID = firstNonEmpty(conversation.SessionID, candidate.ID)
	conversation.CWD = firstNonEmpty(conversation.CWD, candidate.CWD)
	conversation.Updated = &candidate.Updated
	if candidate.Active {
		active := true
		conversation.Active = &active
	} else if conversation.Active == nil && !candidate.Updated.IsZero() {
		active := false
		conversation.Active = &active
	}
	if conversation.Events == nil {
		conversation.Events = []CodexConversationEvent{}
	}
	return conversation, nil
}

func loadCachedGrokConversation(sessionDir string) (CodexConversation, error) {
	historyPath := filepath.Join(sessionDir, grokChatHistoryFile)
	info, err := os.Stat(historyPath)
	if err != nil {
		return CodexConversation{}, err
	}

	grokConversationCache.Lock()
	if cached, ok := grokConversationCache.byPath[sessionDir]; ok &&
		cached.size == info.Size() &&
		cached.modTime.Equal(info.ModTime()) {
		conversation := cached.conversation
		grokConversationCache.Unlock()
		return conversation, nil
	}
	grokConversationCache.Unlock()

	conversation, err := parseGrokConversation(sessionDir)
	if err != nil {
		return CodexConversation{}, err
	}

	grokConversationCache.Lock()
	grokConversationCache.byPath[sessionDir] = cachedGrokConversation{
		size:         info.Size(),
		modTime:      info.ModTime(),
		conversation: conversation,
	}
	grokConversationCache.Unlock()
	return conversation, nil
}

func findGrokSession(agent classifier.Agent, now time.Time) (grokSessionCandidate, bool, error) {
	cwd := strings.TrimSpace(agent.Cwd)
	if cwd == "" {
		return grokSessionCandidate{}, false, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return grokSessionCandidate{}, false, err
	}

	var candidates []grokSessionCandidate
	for _, candidateCWD := range transcriptCWDCandidates(cwd) {
		baseDir := filepath.Join(home, ".grok", "sessions", encodeGrokSessionCWD(candidateCWD))
		entries, err := os.ReadDir(baseDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return grokSessionCandidate{}, false, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			sessionDir := filepath.Join(baseDir, entry.Name())
			summary, err := readGrokSummary(sessionDir)
			if err != nil {
				continue
			}
			sessionCWD := firstNonEmpty(summary.Info.CWD, candidateCWD)
			if !pathsEquivalent(sessionCWD, candidateCWD) && !pathsEquivalent(sessionCWD, cwd) {
				continue
			}
			updated := grokSessionUpdatedAt(summary, sessionDir)
			if now.Sub(updated) > maxGrokSessionAge {
				continue
			}
			candidates = append(candidates, grokSessionCandidate{
				ID:      firstNonEmpty(summary.Info.ID, entry.Name()),
				CWD:     sessionCWD,
				Dir:     sessionDir,
				Updated: updated,
			})
		}
	}
	if len(candidates) == 0 {
		return grokSessionCandidate{}, false, nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Updated.After(candidates[j].Updated)
	})

	if !agent.StartedAt.IsZero() {
		startedAt := agent.StartedAt
		for _, candidate := range candidates {
			if candidate.Updated.After(startedAt.Add(-5 * time.Minute)) {
				return candidate, true, nil
			}
		}
	}
	return candidates[0], true, nil
}

func encodeGrokSessionCWD(cwd string) string {
	cwd = filepath.Clean(strings.TrimSpace(cwd))
	if cwd == "" || cwd == "." {
		return ""
	}
	if !strings.HasPrefix(cwd, "/") {
		cwd = "/" + cwd
	}
	return strings.ReplaceAll(url.PathEscape(cwd), "%2F", "%2F")
}

func pathsEquivalent(left, right string) bool {
	left = filepath.Clean(strings.TrimSpace(left))
	right = filepath.Clean(strings.TrimSpace(right))
	if left == "" || right == "" {
		return false
	}
	return left == right
}

type grokSummary struct {
	Info struct {
		ID  string `json:"id"`
		CWD string `json:"cwd"`
	} `json:"info"`
	UpdatedAt string `json:"updated_at"`
	CreatedAt string `json:"created_at"`
}

func readGrokSummary(sessionDir string) (grokSummary, error) {
	raw, err := os.ReadFile(filepath.Join(sessionDir, grokSummaryFile))
	if err != nil {
		return grokSummary{}, err
	}
	var summary grokSummary
	if err := json.Unmarshal(raw, &summary); err != nil {
		return grokSummary{}, err
	}
	return summary, nil
}

func grokSessionUpdatedAt(summary grokSummary, sessionDir string) time.Time {
	for _, value := range []string{summary.UpdatedAt, summary.CreatedAt} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if parsed, err := time.Parse(layout, value); err == nil {
				return parsed
			}
		}
	}
	if info, err := os.Stat(filepath.Join(sessionDir, grokChatHistoryFile)); err == nil {
		return info.ModTime()
	}
	if info, err := os.Stat(sessionDir); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

func parseGrokConversation(sessionDir string) (CodexConversation, error) {
	builder := newGrokConversationBuilder(filepath.Base(sessionDir))
	if summary, err := readGrokSummary(sessionDir); err == nil {
		builder.sessionID = strings.TrimSpace(summary.Info.ID)
		builder.cwd = strings.TrimSpace(summary.Info.CWD)
	}

	historyPath := filepath.Join(sessionDir, grokChatHistoryFile)
	if err := consumeGrokJSONL(historyPath, builder.consumeChatHistoryLine); err != nil && !os.IsNotExist(err) {
		return CodexConversation{}, err
	}

	updatesPath := filepath.Join(sessionDir, grokUpdatesFile)
	if err := consumeGrokJSONL(updatesPath, builder.consumeUpdatesLine); err != nil && !os.IsNotExist(err) {
		return CodexConversation{}, err
	}

	return builder.conversation(), nil
}

func consumeGrokJSONL(path string, consume func(int, []byte)) error {
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

type grokConversationBuilder struct {
	sourceID       string
	sessionID      string
	cwd            string
	events         []CodexConversationEvent
	eventByCall    map[string]int
	seenPlanKeys   map[string]struct{}
	pendingThought string
	taskActive     bool
	lifecycleSeen  bool
}

func newGrokConversationBuilder(sourceID string) *grokConversationBuilder {
	return &grokConversationBuilder{
		sourceID:     sourceID,
		eventByCall:  map[string]int{},
		seenPlanKeys: map[string]struct{}{},
	}
}

func (b *grokConversationBuilder) consumeChatHistoryLine(lineNumber int, line []byte) {
	var record struct {
		Type       string          `json:"type"`
		Content    json.RawMessage `json:"content"`
		Summary    json.RawMessage `json:"summary"`
		Status     string          `json:"status"`
		ToolCalls  json.RawMessage `json:"tool_calls"`
		ToolCallID string          `json:"tool_call_id"`
	}
	if json.Unmarshal(line, &record) != nil {
		return
	}

	switch record.Type {
	case "user":
		text := grokMessageText(record.Content)
		text = grokVisibleUserText(text)
		if text == "" || isGrokBootstrapUserMessage(text) {
			return
		}
		b.finishPendingThought()
		b.addMessage(lineNumber, "", "user", text)
	case "assistant":
		text := grokMessageText(record.Content)
		if text != "" && !isTranscriptBoilerplate(text) {
			b.finishPendingThought()
			b.addMessage(lineNumber, "", "assistant", text)
		}
		b.consumeAssistantToolCalls(lineNumber, "", record.ToolCalls)
	case "reasoning":
		text := grokReasoningText(record.Summary)
		if text == "" {
			return
		}
		b.upsertThought(lineNumber, "", text, strings.EqualFold(strings.TrimSpace(record.Status), "completed"))
	case "tool_result":
		callID := strings.TrimSpace(record.ToolCallID)
		output := grokMessageText(record.Content)
		if callID == "" || output == "" {
			return
		}
		b.updateToolOutput(lineNumber, "", callID, output)
	}
}

func (b *grokConversationBuilder) consumeUpdatesLine(lineNumber int, line []byte) {
	var envelope struct {
		Timestamp json.RawMessage `json:"timestamp"`
		Params    struct {
			SessionID string `json:"sessionId"`
			Update    struct {
				SessionUpdate string          `json:"sessionUpdate"`
				Content       json.RawMessage `json:"content"`
				ToolCallID    string          `json:"toolCallId"`
				Title         string          `json:"title"`
				RawInput      json.RawMessage `json:"rawInput"`
				Status        string          `json:"status"`
				Entries       []struct {
					Content  string `json:"content"`
					Status   string `json:"status"`
					Priority string `json:"priority"`
				} `json:"entries"`
			} `json:"update"`
		} `json:"params"`
	}
	if json.Unmarshal(line, &envelope) != nil {
		return
	}

	timestamp := grokUpdateTimestamp(envelope.Timestamp)
	if b.sessionID == "" {
		b.sessionID = strings.TrimSpace(envelope.Params.SessionID)
	}

	update := envelope.Params.Update
	switch update.SessionUpdate {
	case "plan":
		b.addPlanUpdate(lineNumber, timestamp, update.Entries)
	case "turn_completed":
		b.lifecycleSeen = true
		b.taskActive = false
		b.finishPendingThought()
	}
}

func (b *grokConversationBuilder) consumeAssistantToolCalls(lineNumber int, timestamp string, raw json.RawMessage) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return
	}
	var calls []struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	if json.Unmarshal(raw, &calls) != nil {
		return
	}
	for _, call := range calls {
		name := cleanConversationText(call.Name)
		if name == "" {
			name = "tool"
		}
		b.addToolStart(lineNumber, timestamp, call.ID, name, codexToolPayloadText(call.Arguments), "running")
	}
}

func (b *grokConversationBuilder) addMessage(lineNumber int, timestamp, role, text string) {
	text = CleanCodexDisplayText(text)
	if text == "" || isTranscriptBoilerplate(text) {
		return
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
		Body:      text,
		Source:    "grok_session",
	})
}

func (b *grokConversationBuilder) upsertThought(lineNumber int, timestamp, text string, finalize bool) {
	text = CleanCodexDisplayText(text)
	if text == "" || isTranscriptBoilerplate(text) {
		if finalize {
			b.finishPendingThought()
		}
		return
	}
	if index := b.pendingThoughtIndex(); index >= 0 {
		event := &b.events[index]
		event.Body = text
		if event.Timestamp == "" {
			event.Timestamp = timestamp
		}
		if finalize {
			event.Status = "done"
			b.pendingThought = ""
		} else {
			event.Status = "running"
		}
		return
	}
	event := CodexConversationEvent{
		ID:        b.eventID(lineNumber),
		Timestamp: timestamp,
		Kind:      "commentary",
		Title:     "Reasoning",
		Body:      text,
		Status:    "running",
		Source:    "grok_session",
	}
	if finalize {
		event.Status = "done"
	}
	if b.addEvent(event) && !finalize {
		b.pendingThought = event.ID
	}
	if finalize {
		b.pendingThought = ""
	}
}

func (b *grokConversationBuilder) finishPendingThought() {
	index := b.pendingThoughtIndex()
	if index < 0 {
		b.pendingThought = ""
		return
	}
	if b.events[index].Status == "running" {
		b.events[index].Status = "done"
	}
	b.pendingThought = ""
}

func (b *grokConversationBuilder) pendingThoughtIndex() int {
	if strings.TrimSpace(b.pendingThought) == "" {
		return -1
	}
	for index := len(b.events) - 1; index >= 0; index-- {
		if b.events[index].ID == b.pendingThought {
			return index
		}
	}
	return -1
}

func (b *grokConversationBuilder) addToolStart(lineNumber int, timestamp, callID, name, input, status string) {
	callID = strings.TrimSpace(callID)
	name = cleanToolName(name)
	if name == "" {
		name = "tool"
	}
	status = cleanConversationText(status)
	if status == "" {
		status = "running"
	}
	event := CodexConversationEvent{
		ID:       b.eventID(lineNumber),
		Timestamp: timestamp,
		Kind:     "tool",
		Title:    "Tool",
		ToolName: name,
		Input:    truncateConversationBody(input),
		CallID:   callID,
		Status:   status,
		Source:   "grok_session",
	}
	if callID != "" {
		if index, exists := b.eventByCall[callID]; exists && index >= 0 && index < len(b.events) {
			b.events[index].ToolName = event.ToolName
			if event.Input != "" {
				b.events[index].Input = event.Input
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

func (b *grokConversationBuilder) updateToolOutput(lineNumber int, timestamp, callID, output string) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return
	}
	output = truncateConversationBody(codexToolPayloadText(output))
	if index, exists := b.eventByCall[callID]; exists && index >= 0 && index < len(b.events) {
		b.events[index].Output = output
		b.events[index].Status = codexToolOutputStatus(output)
		if timestamp != "" {
			b.events[index].Timestamp = timestamp
		}
		return
	}
	if output == "" {
		return
	}
	b.addEvent(CodexConversationEvent{
		ID:       b.eventID(lineNumber),
		Timestamp: timestamp,
		Kind:     "tool",
		Title:    "Tool output",
		ToolName: "tool",
		Output:   output,
		CallID:   callID,
		Status:   codexToolOutputStatus(output),
		Source:   "grok_session",
	})
}

func (b *grokConversationBuilder) updateToolStatus(callID, status string) {
	callID = strings.TrimSpace(callID)
	status = grokToolStatus(status)
	if callID == "" || status == "" {
		return
	}
	if index, exists := b.eventByCall[callID]; exists && index >= 0 && index < len(b.events) {
		b.events[index].Status = status
	}
}

func (b *grokConversationBuilder) addPlanUpdate(lineNumber int, timestamp string, entries []struct {
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}) {
	steps := make([]CodexPlanStep, 0, len(entries))
	for _, entry := range entries {
		step := cleanConversationText(entry.Content)
		if step == "" {
			continue
		}
		steps = append(steps, CodexPlanStep{
			Step:   step,
			Status: normalizePlanStepStatus(entry.Status),
		})
	}
	if len(steps) == 0 {
		return
	}
	key := ""
	for _, step := range steps {
		key += step.Step + "\x00" + step.Status + "\x00"
	}
	if _, exists := b.seenPlanKeys[key]; exists {
		return
	}
	b.seenPlanKeys[key] = struct{}{}
	b.addEvent(CodexConversationEvent{
		ID:        b.eventID(lineNumber),
		Timestamp: timestamp,
		Kind:      "plan",
		Title:     "Updated Plan",
		Plan:      steps,
		Status:    "done",
		Source:    "grok_session",
	})
}

func (b *grokConversationBuilder) addEvent(event CodexConversationEvent) bool {
	event.Body = truncateConversationBody(event.Body)
	event.ToolName = truncateRunes(cleanToolName(event.ToolName), 120)
	event.Input = truncateConversationBody(event.Input)
	event.Output = truncateConversationBody(event.Output)
	event.Plan = filterVisibleCodexPlanSteps(event.Plan)
	if event.Kind == "" || (event.Body == "" && event.Title == "" && event.Command == "" && event.ToolName == "" && event.Input == "" && event.Output == "" && len(event.Plan) == 0) {
		return false
	}
	if event.ID == "" {
		event.ID = b.eventID(len(b.events) + 1)
	}
	b.events = append(b.events, event)
	if len(b.events) > maxGrokConversationEvents {
		copy(b.events, b.events[len(b.events)-maxGrokConversationEvents:])
		b.events = b.events[:maxGrokConversationEvents]
	}
	b.reindexEvents()
	return true
}

func (b *grokConversationBuilder) reindexEvents() {
	b.eventByCall = map[string]int{}
	for index := range b.events {
		b.events[index].Seq = index + 1
		if callID := strings.TrimSpace(b.events[index].CallID); callID != "" {
			b.eventByCall[callID] = index
		}
	}
}

func (b *grokConversationBuilder) eventID(lineNumber int) string {
	if b.sessionID != "" {
		return fmt.Sprintf("%s:%d", b.sessionID, lineNumber)
	}
	return fmt.Sprintf("%s:%d", b.sourceID, lineNumber)
}

func (b *grokConversationBuilder) conversation() CodexConversation {
	if b.events == nil {
		b.events = []CodexConversationEvent{}
	}
	if b.lifecycleSeen && b.taskActive {
		b.taskActive = false
	}
	b.finishPendingThought()
	b.reindexEvents()
	var active *bool
	if b.lifecycleSeen {
		current := b.taskActive
		active = &current
	}
	return CodexConversation{
		Available: true,
		Source:    "grok_session",
		SessionID: b.sessionID,
		CWD:       b.cwd,
		Active:    active,
		Events:    b.events,
	}
}

func grokMessageText(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	if raw[0] == '"' {
		return CleanCodexDisplayText(jsonString(raw))
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, block := range blocks {
			if text := CleanCodexDisplayText(block.Text); text != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n\n")
		}
	}
	return codexConversationContentText(raw)
}

func grokReasoningText(raw json.RawMessage) string {
	if text := codexConversationContentText(raw); text != "" {
		return text
	}
	return grokMessageText(raw)
}

func grokChunkText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var payload struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &payload) == nil {
		return CleanCodexDisplayText(payload.Text)
	}
	return grokMessageText(raw)
}

func grokJSONPayloadText(raw json.RawMessage) string {
	if text := codexJSONPayloadText(raw); text != "" {
		return text
	}
	return ""
}

func grokToolUpdateOutput(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	var blocks []struct {
		Type    string          `json:"type"`
		Content json.RawMessage `json:"content"`
		Text    string          `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, block := range blocks {
			if text := CleanCodexDisplayText(block.Text); text != "" {
				parts = append(parts, text)
				continue
			}
			if text := grokMessageText(block.Content); text != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n\n")
		}
	}
	return grokMessageText(raw)
}

func grokUpdateTimestamp(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		return normalizeCodexTimestamp(jsonString(raw))
	}
	var value float64
	if json.Unmarshal(raw, &value) == nil && value > 0 {
		seconds := int64(value)
		if value > 1_000_000_000_000 {
			seconds = int64(value / 1000)
		}
		return time.Unix(seconds, 0).UTC().Format(time.RFC3339Nano)
	}
	return ""
}

func grokToolStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "failed", "error", "cancelled", "canceled":
		return "failed"
	case "running", "pending", "in_progress", "in-progress":
		return "running"
	case "completed", "complete", "success", "done":
		return "done"
	default:
		if strings.TrimSpace(value) == "" {
			return ""
		}
		return "done"
	}
}

func grokVisibleUserText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	const openTag = "<user_query>"
	const closeTag = "</user_query>"
	if start := strings.Index(text, openTag); start >= 0 {
		rest := text[start+len(openTag):]
		if end := strings.Index(rest, closeTag); end >= 0 {
			return strings.TrimSpace(rest[:end])
		}
		return strings.TrimSpace(rest)
	}
	return text
}

func isGrokBootstrapUserMessage(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return true
	}
	lower := strings.ToLower(trimmed)
	markers := []string{
		"<user_info>",
		"<git_status>",
		"<rules>",
		"<agent_skills>",
		"<mcp_file_system>",
		"you are working directly on this goal",
		"<system-reminder>",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	if len(trimmed) > 4000 && strings.Contains(lower, "always_applied_workspace_rules") {
		return true
	}
	return false
}