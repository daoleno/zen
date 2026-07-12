package work

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
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

type cachedClaudeConversation struct {
	size         int64
	modTime      time.Time
	conversation CodexConversation
}

var claudeConversationCache = struct {
	sync.Mutex
	byPath map[string]cachedClaudeConversation
}{
	byPath: map[string]cachedClaudeConversation{},
}

type claudeTranscriptCandidate struct {
	ID        string
	CWD       string
	Path      string
	CreatedAt time.Time
	Updated   time.Time
}

func loadClaudeConversationForAgent(agent classifier.Agent, now time.Time) (CodexConversation, error) {
	if strings.TrimSpace(agent.Cwd) == "" {
		return CodexConversation{
			Available: false,
			Reason:    "missing_cwd",
			Events:    []CodexConversationEvent{},
		}, nil
	}

	candidate, ok, err := findClaudeTranscript(agent, now)
	if err != nil {
		return CodexConversation{}, err
	}
	if !ok {
		return CodexConversation{
			Available: false,
			Reason:    "transcript_not_found",
			Events:    []CodexConversationEvent{},
		}, nil
	}

	conversation, err := loadCachedClaudeConversation(candidate.Path)
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
	active := conversationHasActiveTurn(conversation.Events)
	conversation.Active = &active
	return conversation, nil
}

func loadCachedClaudeConversation(path string) (CodexConversation, error) {
	info, err := os.Stat(path)
	if err != nil {
		return CodexConversation{}, err
	}

	claudeConversationCache.Lock()
	if cached, ok := claudeConversationCache.byPath[path]; ok &&
		cached.size == info.Size() &&
		cached.modTime.Equal(info.ModTime()) {
		conversation := cached.conversation
		claudeConversationCache.Unlock()
		return conversation, nil
	}
	claudeConversationCache.Unlock()

	conversation, err := parseClaudeConversation(path)
	if err != nil {
		return CodexConversation{}, err
	}

	claudeConversationCache.Lock()
	claudeConversationCache.byPath[path] = cachedClaudeConversation{
		size:         info.Size(),
		modTime:      info.ModTime(),
		conversation: conversation,
	}
	claudeConversationCache.Unlock()
	return conversation, nil
}

func findClaudeTranscript(agent classifier.Agent, now time.Time) (claudeTranscriptCandidate, bool, error) {
	cwd := strings.TrimSpace(agent.Cwd)
	if cwd == "" {
		return claudeTranscriptCandidate{}, false, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return claudeTranscriptCandidate{}, false, err
	}

	var candidates []claudeTranscriptCandidate
	for _, candidateCWD := range transcriptCWDCandidates(cwd) {
		projectDir := filepath.Join(home, ".claude", "projects", encodeClaudeProjectDir(candidateCWD))
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
			if !isClaudeTranscriptFresh(updated, now) {
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

	if sessionID := claudeResumeSessionID(agent.Command); sessionID != "" {
		if matched, ok := matchClaudeTranscriptID(candidates, sessionID); ok {
			return matched, true, nil
		}
	}

	freshCandidates := freshClaudeTranscriptCandidates(candidates, now)
	if len(freshCandidates) == 0 {
		return claudeTranscriptCandidate{}, false, nil
	}
	// Prefer an unambiguous bind. Never fall back to "newest file in cwd" when
	// multiple sessions exist — that can surface an unrelated conversation.
	if matched, ok := matchClaudeTranscriptToAgentStart(freshCandidates, agent.StartedAt); ok {
		return matched, true, nil
	}
	if matched, ok := matchClaudeTranscriptToActiveSession(freshCandidates, agent.StartedAt); ok {
		return matched, true, nil
	}
	if agent.StartedAt.IsZero() && len(freshCandidates) == 1 {
		return freshCandidates[0], true, nil
	}
	return claudeTranscriptCandidate{}, false, nil
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
		case lower == "resume" || lower == "--resume" || lower == "-r":
			nextIndex := index + 2
			if nextIndex < len(fields) {
				sessionID := strings.Trim(fields[nextIndex], `"'`)
				if sessionID != "" && !strings.HasPrefix(sessionID, "-") {
					return sessionID
				}
			}
		case strings.HasPrefix(lower, "--resume="):
			if idx := strings.Index(trimmed, "="); idx >= 0 {
				return strings.Trim(trimmed[idx+1:], `"'`)
			}
		}
	}
	return ""
}

func matchClaudeTranscriptToAgentStart(candidates []claudeTranscriptCandidate, startedAt time.Time) (claudeTranscriptCandidate, bool) {
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
	sourceID         string
	sessionID        string
	cwd              string
	supportedRecords int
	events           []CodexConversationEvent
	eventByCall      map[string]int
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
		Message     struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
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
		b.consumeUserContent(lineNumber, recordID, timestamp, envelope.Message.Content)
	case "assistant":
		b.consumeAssistantContent(lineNumber, recordID, timestamp, envelope.Message.Content)
	default:
		// Skip system/attachment/permission-mode/file-history-snapshot and other
		// provider-internal records from the shared conversation surface.
	}
}

func (b *claudeConversationBuilder) consumeUserContent(lineNumber int, recordID, timestamp string, raw json.RawMessage) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return
	}
	if raw[0] == '"' {
		var text string
		if json.Unmarshal(raw, &text) == nil {
			b.addMessage(lineNumber, recordID, 0, timestamp, "user", text)
		}
		return
	}
	var items []claudeContentBlock
	if json.Unmarshal(raw, &items) != nil {
		return
	}
	textIndex := 0
	for index, item := range items {
		switch strings.ToLower(strings.TrimSpace(item.Type)) {
		case "text":
			textIndex++
			b.addMessage(lineNumber, recordID, textIndex, timestamp, "user", item.Text)
		case "tool_result":
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
	text = CleanCodexDisplayText(text)
	if text == "" || isTranscriptBoilerplate(text) {
		return
	}
	kind := "assistant_message"
	if role == "user" {
		kind = "user_message"
	}
	b.addEvent(CodexConversationEvent{
		ID:        b.messageEventID(recordID, role, index),
		Seq:       claudeEventSeq(lineNumber, index),
		Timestamp: timestamp,
		Kind:      kind,
		Role:      role,
		Body:      text,
		Source:    claudeConversationSource,
	})
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
	if b.addEvent(event) && callID != "" {
		b.eventByCall[callID] = len(b.events) - 1
	}
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
	event.Body = truncateConversationBody(event.Body)
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
	return CodexConversation{
		Available: true,
		Source:    claudeConversationSource,
		SessionID: firstNonEmpty(b.sessionID, b.sourceID),
		CWD:       b.cwd,
		Events:    b.events,
	}
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
