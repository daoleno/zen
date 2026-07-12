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

const (
	cursorConversationSource = "cursor_agent_transcript"
	cursorProjectDirPrefix   = ".cursor/projects"
	cursorTranscriptDir      = "agent-transcripts"
	cursorTranscriptAge      = 72 * time.Hour
)

type cachedCursorConversation struct {
	size         int64
	modTime      time.Time
	conversation CodexConversation
}

var cursorConversationCache = struct {
	sync.Mutex
	byPath map[string]cachedCursorConversation
}{
	byPath: map[string]cachedCursorConversation{},
}

type cursorTranscriptCandidate struct {
	ID        string
	CWD       string
	Path      string
	CreatedAt time.Time
	Updated   time.Time
}

func loadCursorConversationForAgent(agent classifier.Agent, now time.Time) (CodexConversation, error) {
	if strings.TrimSpace(agent.Cwd) == "" {
		return CodexConversation{
			Available: false,
			Reason:    "missing_cwd",
			Events:    []CodexConversationEvent{},
		}, nil
	}

	candidate, ok, err := findCursorTranscript(agent, now)
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

	conversation, err := loadCachedCursorConversation(candidate.Path)
	if err != nil {
		return CodexConversation{}, err
	}
	conversation.Available = true
	conversation.Source = cursorConversationSource
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

func loadCachedCursorConversation(path string) (CodexConversation, error) {
	info, err := os.Stat(path)
	if err != nil {
		return CodexConversation{}, err
	}

	cursorConversationCache.Lock()
	if cached, ok := cursorConversationCache.byPath[path]; ok &&
		cached.size == info.Size() &&
		cached.modTime.Equal(info.ModTime()) {
		conversation := cached.conversation
		cursorConversationCache.Unlock()
		return conversation, nil
	}
	cursorConversationCache.Unlock()

	conversation, err := parseCursorConversation(path)
	if err != nil {
		return CodexConversation{}, err
	}

	cursorConversationCache.Lock()
	cursorConversationCache.byPath[path] = cachedCursorConversation{
		size:         info.Size(),
		modTime:      info.ModTime(),
		conversation: conversation,
	}
	cursorConversationCache.Unlock()
	return conversation, nil
}

func findCursorTranscript(agent classifier.Agent, now time.Time) (cursorTranscriptCandidate, bool, error) {
	cwd := strings.TrimSpace(agent.Cwd)
	if cwd == "" {
		return cursorTranscriptCandidate{}, false, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return cursorTranscriptCandidate{}, false, err
	}

	var candidates []cursorTranscriptCandidate
	for _, candidateCWD := range transcriptCWDCandidates(cwd) {
		projectDir := filepath.Join(home, cursorProjectDirPrefix, encodeCursorProjectDir(candidateCWD))
		transcriptRoot := filepath.Join(projectDir, cursorTranscriptDir)
		entries, err := os.ReadDir(transcriptRoot)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return cursorTranscriptCandidate{}, false, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			sessionID := entry.Name()
			path := filepath.Join(transcriptRoot, sessionID, sessionID+".jsonl")
			info, err := os.Stat(path)
			if err != nil || info.IsDir() {
				continue
			}
			createdAt := cursorTranscriptCreatedAt(path)
			updated := info.ModTime()
			if !isCursorTranscriptFresh(updated, now) {
				continue
			}
			candidates = append(candidates, cursorTranscriptCandidate{
				ID:        sessionID,
				CWD:       candidateCWD,
				Path:      path,
				CreatedAt: createdAt,
				Updated:   updated,
			})
		}
	}
	if len(candidates) == 0 {
		return cursorTranscriptCandidate{}, false, nil
	}

	if sessionID := cursorResumeSessionID(agent.Command); sessionID != "" {
		if matched, ok := matchCursorTranscriptID(candidates, sessionID); ok {
			return matched, true, nil
		}
	}

	freshCandidates := freshCursorTranscriptCandidates(candidates, now)
	if len(freshCandidates) == 0 {
		return cursorTranscriptCandidate{}, false, nil
	}
	if matched, ok := matchCursorTranscriptToAgentStart(freshCandidates, agent.StartedAt); ok {
		return matched, true, nil
	}
	if matched, ok := matchCursorTranscriptToActiveSession(freshCandidates, agent.StartedAt); ok {
		return matched, true, nil
	}
	return latestUpdatedCursorTranscript(freshCandidates), true, nil
}

func parseCursorConversation(path string) (CodexConversation, error) {
	builder := newCursorConversationBuilder(cursorTranscriptIDFromPath(path))
	if err := consumeCursorJSONL(path, builder.consumeLine); err != nil {
		return CodexConversation{}, err
	}
	return builder.conversation(), nil
}

func consumeCursorJSONL(path string, consume func(int, []byte)) error {
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

type cursorConversationBuilder struct {
	sourceID string
	events   []CodexConversationEvent
}

func newCursorConversationBuilder(sourceID string) *cursorConversationBuilder {
	return &cursorConversationBuilder{sourceID: strings.TrimSpace(sourceID)}
}

func (b *cursorConversationBuilder) consumeLine(lineNumber int, line []byte) {
	var record struct {
		Role    string `json:"role"`
		Message struct {
			Content []cursorContentBlock `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &record) != nil {
		return
	}

	role := strings.ToLower(strings.TrimSpace(record.Role))
	if role != "user" && role != "assistant" {
		return
	}
	body := cursorMessageText(record.Message.Content)
	body = cursorVisibleMessageText(role, body)
	if body != "" {
		kind := "assistant_message"
		if role == "user" {
			kind = "user_message"
		}
		b.addEvent(CodexConversationEvent{
			ID:     b.eventID(lineNumber, "message", 0),
			Seq:    cursorEventSeq(lineNumber, 0),
			Kind:   kind,
			Role:   role,
			Body:   body,
			Source: cursorConversationSource,
		})
	}

	if role != "assistant" {
		return
	}
	toolIndex := 0
	for _, block := range record.Message.Content {
		if block.Type != "tool_use" {
			continue
		}
		toolIndex++
		b.addCursorToolEvent(lineNumber, toolIndex, block)
	}
}

func (b *cursorConversationBuilder) addCursorToolEvent(lineNumber int, toolIndex int, block cursorContentBlock) {
	name := cleanToolName(block.Name)
	if strings.EqualFold(name, "Shell") {
		command := cursorToolInputString(block.Input, "command")
		description := cursorToolInputString(block.Input, "description")
		body := description
		if body == "" {
			body = command
		}
		b.addEvent(CodexConversationEvent{
			ID:      b.eventID(lineNumber, "command", toolIndex),
			Seq:     cursorEventSeq(lineNumber, toolIndex),
			Kind:    "command",
			Command: command,
			Body:    body,
			Status:  "done",
			Source:  cursorConversationSource,
		})
		return
	}

	input := ""
	if len(block.Input) > 0 {
		input = string(block.Input)
	}
	b.addEvent(CodexConversationEvent{
		ID:       b.eventID(lineNumber, "tool", toolIndex),
		Seq:      cursorEventSeq(lineNumber, toolIndex),
		Kind:     "tool",
		ToolName: name,
		Input:    input,
		Status:   "done",
		Source:   cursorConversationSource,
	})
}

func (b *cursorConversationBuilder) addEvent(event CodexConversationEvent) bool {
	event.Body = truncateConversationBody(event.Body)
	event.ToolName = truncateRunes(cleanToolName(event.ToolName), 120)
	event.Input = truncateConversationBody(event.Input)
	event.Output = truncateConversationBody(event.Output)
	if event.Kind == "" || (event.Body == "" && event.Title == "" && event.Command == "" && event.ToolName == "" && event.Input == "" && event.Output == "") {
		return false
	}
	if event.ID == "" {
		event.ID = b.eventID(len(b.events)+1, "event", 0)
	}
	b.events = append(b.events, event)
	return true
}

func (b *cursorConversationBuilder) eventID(lineNumber int, kind string, index int) string {
	sourceID := firstNonEmpty(b.sourceID, "cursor")
	if index > 0 {
		return fmt.Sprintf("%s:%d:%s:%d", sourceID, lineNumber, kind, index)
	}
	return fmt.Sprintf("%s:%d:%s", sourceID, lineNumber, kind)
}

func (b *cursorConversationBuilder) conversation() CodexConversation {
	if b.events == nil {
		b.events = []CodexConversationEvent{}
	}
	if len(b.events) > maxCodexConversationEvents {
		b.events = b.events[len(b.events)-maxCodexConversationEvents:]
	}
	for index := range b.events {
		if b.events[index].Seq <= 0 {
			b.events[index].Seq = cursorEventSeq(index+1, 0)
		}
	}
	return CodexConversation{
		Available: true,
		Source:    cursorConversationSource,
		SessionID: b.sourceID,
		Events:    b.events,
	}
}

type cursorContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

func cursorMessageText(blocks []cursorContentBlock) string {
	var parts []string
	for _, block := range blocks {
		if block.Type != "text" {
			continue
		}
		if text := cleanCursorRedactedText(CleanCodexDisplayText(block.Text)); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func cursorVisibleMessageText(role string, text string) string {
	text = cleanCursorRedactedText(CleanCodexDisplayText(text))
	if role != "user" {
		return text
	}
	text = stripMarkedSection(text, "<timestamp>", "</timestamp>")
	if query := markedSectionText(text, "<user_query>", "</user_query>"); query != "" {
		return cleanCursorRedactedText(query)
	}
	return cleanCursorRedactedText(CleanCodexDisplayText(text))
}

func cleanCursorRedactedText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	out := make([]string, 0, len(lines))
	blankRun := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "[REDACTED]" {
			continue
		}
		if trimmed == "" {
			blankRun++
			if blankRun <= 1 {
				out = append(out, "")
			}
			continue
		}
		blankRun = 0
		out = append(out, strings.TrimRight(line, " \t"))
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func cursorToolInputString(raw json.RawMessage, key string) string {
	if len(raw) == 0 {
		return ""
	}
	var input map[string]any
	if err := json.Unmarshal(raw, &input); err != nil {
		return ""
	}
	value, ok := input[key].(string)
	if !ok {
		return ""
	}
	return CleanCodexDisplayText(value)
}

func encodeCursorProjectDir(cwd string) string {
	cwd = filepath.Clean(strings.TrimSpace(cwd))
	if cwd == "" || cwd == "." {
		return ""
	}
	return strings.Trim(strings.ReplaceAll(cwd, string(filepath.Separator), "-"), "-")
}

func cursorResumeSessionID(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return ""
	}
	base := strings.ToLower(filepath.Base(strings.Trim(fields[0], `"'`)))
	if base != "cursor-agent" && base != "agent" {
		return ""
	}
	for index := 1; index < len(fields); index++ {
		field := strings.Trim(fields[index], `"'`)
		switch {
		case field == "--resume":
			if index+1 < len(fields) {
				sessionID := strings.Trim(fields[index+1], `"'`)
				if sessionID != "" && !strings.HasPrefix(sessionID, "-") {
					return sessionID
				}
			}
		case strings.HasPrefix(field, "--resume="):
			return strings.Trim(strings.TrimPrefix(field, "--resume="), `"'`)
		}
	}
	return ""
}

func matchCursorTranscriptID(candidates []cursorTranscriptCandidate, sessionID string) (cursorTranscriptCandidate, bool) {
	sessionID = strings.ToLower(strings.TrimSpace(sessionID))
	if sessionID == "" {
		return cursorTranscriptCandidate{}, false
	}
	for _, candidate := range candidates {
		if strings.ToLower(candidate.ID) == sessionID || strings.ToLower(cursorTranscriptIDFromPath(candidate.Path)) == sessionID {
			return candidate, true
		}
	}
	return cursorTranscriptCandidate{}, false
}

func freshCursorTranscriptCandidates(candidates []cursorTranscriptCandidate, now time.Time) []cursorTranscriptCandidate {
	if len(candidates) == 0 {
		return nil
	}
	fresh := make([]cursorTranscriptCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if isCursorTranscriptFresh(candidate.Updated, now) {
			fresh = append(fresh, candidate)
		}
	}
	return fresh
}

func isCursorTranscriptFresh(updated, now time.Time) bool {
	if updated.IsZero() || now.IsZero() {
		return true
	}
	if updated.After(now.Add(10 * time.Minute)) {
		return true
	}
	return now.Sub(updated) <= cursorTranscriptAge
}

func matchCursorTranscriptToAgentStart(candidates []cursorTranscriptCandidate, startedAt time.Time) (cursorTranscriptCandidate, bool) {
	if startedAt.IsZero() {
		return cursorTranscriptCandidate{}, false
	}
	startedAt = startedAt.UTC()
	minCreatedAt := startedAt.Add(-5 * time.Second)
	bestIndex := -1
	var bestDelta time.Duration
	for index, candidate := range candidates {
		createdAt := candidate.CreatedAt
		if createdAt.IsZero() || createdAt.Before(minCreatedAt) || candidate.Updated.Before(startedAt) {
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
		return cursorTranscriptCandidate{}, false
	}
	return candidates[bestIndex], true
}

func matchCursorTranscriptToActiveSession(candidates []cursorTranscriptCandidate, startedAt time.Time) (cursorTranscriptCandidate, bool) {
	if len(candidates) == 0 || startedAt.IsZero() {
		return cursorTranscriptCandidate{}, false
	}
	startedAt = startedAt.UTC()
	minCreatedAt := startedAt.Add(-maxCodexActiveTranscriptStartBackdate)
	var eligible []cursorTranscriptCandidate
	for _, candidate := range candidates {
		if candidate.Updated.IsZero() || candidate.Updated.Before(startedAt) {
			continue
		}
		if !candidate.CreatedAt.IsZero() && candidate.CreatedAt.Before(minCreatedAt) {
			continue
		}
		eligible = append(eligible, candidate)
	}
	if len(eligible) == 0 {
		return cursorTranscriptCandidate{}, false
	}
	return latestUpdatedCursorTranscript(eligible), true
}

func latestUpdatedCursorTranscript(candidates []cursorTranscriptCandidate) cursorTranscriptCandidate {
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.Updated.After(best.Updated) {
			best = candidate
		}
	}
	return best
}

func cursorTranscriptCreatedAt(path string) time.Time {
	file, err := os.Open(path)
	if err != nil {
		return time.Time{}
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			if timestamp := cursorTimestampFromLine(line); !timestamp.IsZero() {
				return timestamp
			}
		}
		if err != nil {
			return time.Time{}
		}
	}
}

func cursorTimestampFromLine(line []byte) time.Time {
	var record struct {
		Message struct {
			Content []cursorContentBlock `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &record) != nil {
		return time.Time{}
	}
	for _, block := range record.Message.Content {
		if block.Type != "text" {
			continue
		}
		if timestamp := markedSectionText(block.Text, "<timestamp>", "</timestamp>"); timestamp != "" {
			return parseCursorTimestamp(timestamp)
		}
	}
	return time.Time{}
}

func parseCursorTimestamp(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		"Monday, Jan 2, 2006, 3:04 PM (UTC-7)",
		"Monday, Jan 2, 2006, 3:04 PM (UTC-07)",
		"Monday, Jan 2, 2006, 3:04 PM (UTC+7)",
		"Monday, Jan 2, 2006, 3:04 PM (UTC+07)",
		time.RFC3339Nano,
		time.RFC3339,
	} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func cursorTranscriptIDFromPath(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return strings.TrimSpace(base)
}

func cursorEventSeq(lineNumber int, index int) int {
	if lineNumber < 1 {
		lineNumber = 1
	}
	if index < 0 {
		index = 0
	}
	return lineNumber*100 + index
}

func markedSectionText(value, open, close string) string {
	start := strings.Index(value, open)
	if start < 0 {
		return ""
	}
	start += len(open)
	end := strings.Index(value[start:], close)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(value[start : start+end])
}

func stripMarkedSection(value, open, close string) string {
	for {
		start := strings.Index(value, open)
		if start < 0 {
			return strings.TrimSpace(value)
		}
		end := strings.Index(value[start+len(open):], close)
		if end < 0 {
			return strings.TrimSpace(value)
		}
		end = start + len(open) + end + len(close)
		value = strings.TrimSpace(value[:start] + "\n" + value[end:])
	}
}
