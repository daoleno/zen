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
	"strings"
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

const (
	maxGrokConversationEvents = 240
	maxGrokToolEventsInChat   = 48
	maxGrokSessionAge         = 72 * time.Hour
	grokChatHistoryFile       = "chat_history.jsonl"
	grokUpdatesFile           = "updates.jsonl"
	grokSummaryFile           = "summary.json"
)

type cachedGrokConversation struct {
	stamp        grokConversationStamp
	conversation CodexConversation
	updates      *grokUpdateTracker
}

type grokUpdateTracker struct {
	builder           *grokConversationBuilder
	identities        []CodexConversationEvent
	providerTurns     []CodexConversationTurn
	terminalTools     map[string]CodexConversationEvent
	terminalToolOrder []string
	offset            int64
	line              int
}

type grokConversationStamp struct {
	historySize    int64
	historyModTime time.Time
	updatesSize    int64
	updatesModTime time.Time
}

var grokConversationCache = struct {
	sync.Mutex
	byPath map[string]cachedGrokConversation
}{
	byPath: map[string]cachedGrokConversation{},
}

type grokSessionCandidate struct {
	ID        string
	CWD       string
	Dir       string
	CreatedAt time.Time
	Updated   time.Time
	Active    bool
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
	if conversation.Events == nil {
		conversation.Events = []CodexConversationEvent{}
	}
	return conversation, nil
}

func loadCachedGrokConversation(sessionDir string) (CodexConversation, error) {
	stamp, err := readGrokConversationStamp(sessionDir)
	if err != nil {
		return CodexConversation{}, err
	}

	grokConversationCache.Lock()
	defer grokConversationCache.Unlock()
	if cached, ok := grokConversationCache.byPath[sessionDir]; ok &&
		cached.stamp == stamp {
		return cached.conversation, nil
	}

	cached := grokConversationCache.byPath[sessionDir]
	tracker := cached.updates
	if tracker == nil || stamp.updatesSize < tracker.offset {
		tracker, err = newGrokUpdateTracker(sessionDir)
	} else if stamp.updatesSize > tracker.offset {
		err = tracker.consumeFrom(filepath.Join(sessionDir, grokUpdatesFile), stamp.updatesSize)
	}
	if err != nil {
		return CodexConversation{}, err
	}
	conversation, err := buildGrokConversation(sessionDir, tracker)
	if err != nil {
		return CodexConversation{}, err
	}

	grokConversationCache.byPath[sessionDir] = cachedGrokConversation{
		stamp:        stamp,
		conversation: conversation,
		updates:      tracker,
	}
	return conversation, nil
}

func readGrokConversationStamp(sessionDir string) (grokConversationStamp, error) {
	var stamp grokConversationStamp
	history, err := os.Stat(filepath.Join(sessionDir, grokChatHistoryFile))
	if err != nil {
		return stamp, err
	}
	stamp.historySize = history.Size()
	stamp.historyModTime = history.ModTime()
	if updates, err := os.Stat(filepath.Join(sessionDir, grokUpdatesFile)); err == nil {
		stamp.updatesSize = updates.Size()
		stamp.updatesModTime = updates.ModTime()
	} else if !os.IsNotExist(err) {
		return stamp, err
	}
	return stamp, nil
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
			createdAt := grokSessionCreatedAt(summary, sessionDir)
			updated := grokSessionUpdatedAt(summary, sessionDir)
			if now.Sub(updated) > maxGrokSessionAge {
				continue
			}
			candidates = append(candidates, grokSessionCandidate{
				ID:        firstNonEmpty(summary.Info.ID, entry.Name()),
				CWD:       sessionCWD,
				Dir:       sessionDir,
				CreatedAt: createdAt,
				Updated:   updated,
			})
		}
	}
	if len(candidates) == 0 {
		return grokSessionCandidate{}, false, nil
	}

	if sessionID := grokResumeSessionID(agent.Command); sessionID != "" {
		if matched, ok := matchGrokSessionID(candidates, sessionID); ok {
			return matched, true, nil
		}
	}

	freshCandidates := freshGrokSessionCandidates(candidates, now)
	if len(freshCandidates) == 0 {
		return grokSessionCandidate{}, false, nil
	}
	if matched, ok := matchGrokSessionToAgentStart(freshCandidates, agent.StartedAt); ok {
		return matched, true, nil
	}
	if matched, ok := matchGrokSessionToActiveSession(freshCandidates, agent.StartedAt); ok {
		return matched, true, nil
	}
	return grokSessionCandidate{}, false, nil
}

func matchGrokSessionID(candidates []grokSessionCandidate, sessionID string) (grokSessionCandidate, bool) {
	sessionID = strings.TrimSpace(strings.ToLower(sessionID))
	if sessionID == "" {
		return grokSessionCandidate{}, false
	}
	for _, candidate := range candidates {
		if strings.ToLower(strings.TrimSpace(candidate.ID)) == sessionID ||
			strings.ToLower(filepath.Base(candidate.Dir)) == sessionID {
			return candidate, true
		}
	}
	return grokSessionCandidate{}, false
}

func freshGrokSessionCandidates(candidates []grokSessionCandidate, now time.Time) []grokSessionCandidate {
	if len(candidates) == 0 {
		return nil
	}
	fresh := make([]grokSessionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if isTranscriptFresh(candidate.Updated, now) {
			fresh = append(fresh, candidate)
		}
	}
	return fresh
}

func grokResumeSessionID(command string) string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(command)))
	if len(fields) == 0 {
		return ""
	}
	if filepath.Base(strings.Trim(fields[0], `"'`)) != "grok" {
		return ""
	}
	for index, field := range fields[1:] {
		field = strings.Trim(field, `"'`)
		switch {
		case field == "resume" || field == "--resume":
			nextIndex := index + 2
			if nextIndex < len(fields) {
				sessionID := strings.Trim(fields[nextIndex], `"'`)
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

func matchGrokSessionToAgentStart(candidates []grokSessionCandidate, startedAt time.Time) (grokSessionCandidate, bool) {
	if startedAt.IsZero() {
		return grokSessionCandidate{}, false
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
		return grokSessionCandidate{}, false
	}
	return candidates[bestIndex], true
}

func matchGrokSessionToActiveSession(candidates []grokSessionCandidate, startedAt time.Time) (grokSessionCandidate, bool) {
	if len(candidates) == 0 || startedAt.IsZero() {
		return grokSessionCandidate{}, false
	}
	startedAt = startedAt.UTC()
	minCreatedAt := startedAt.Add(-maxCodexActiveTranscriptStartBackdate)
	var eligible []grokSessionCandidate
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
		return grokSessionCandidate{}, false
	}
	return latestUpdatedGrokSession(eligible), true
}

func latestUpdatedGrokSession(candidates []grokSessionCandidate) grokSessionCandidate {
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.Updated.After(best.Updated) {
			best = candidate
		}
	}
	return best
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

func grokSessionCreatedAt(summary grokSummary, sessionDir string) time.Time {
	return parseGrokSummaryTimestamp(summary.CreatedAt, sessionDir, false)
}

func grokSessionUpdatedAt(summary grokSummary, sessionDir string) time.Time {
	updated := parseGrokSummaryTimestamp(summary.UpdatedAt, sessionDir, true)
	if !updated.IsZero() {
		return updated
	}
	return grokSessionCreatedAt(summary, sessionDir)
}

func parseGrokSummaryTimestamp(value string, sessionDir string, allowFileFallback bool) time.Time {
	for _, candidate := range []string{value} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if parsed, err := time.Parse(layout, candidate); err == nil {
				return parsed
			}
		}
	}
	if !allowFileFallback {
		return time.Time{}
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
	tracker, err := newGrokUpdateTracker(sessionDir)
	if err != nil {
		return CodexConversation{}, err
	}
	return buildGrokConversation(sessionDir, tracker)
}

func buildGrokConversation(sessionDir string, tracker *grokUpdateTracker) (CodexConversation, error) {
	builder := newGrokConversationBuilder(filepath.Base(sessionDir))
	if summary, err := readGrokSummary(sessionDir); err == nil {
		builder.sessionID = strings.TrimSpace(summary.Info.ID)
		builder.cwd = strings.TrimSpace(summary.Info.CWD)
	}

	historyPath := filepath.Join(sessionDir, grokChatHistoryFile)
	if err := consumeGrokJSONL(historyPath, builder.consumeChatHistoryLine); err != nil && !os.IsNotExist(err) {
		return CodexConversation{}, err
	}
	builder.markCanonicalEvents()
	if tracker != nil && tracker.builder != nil {
		builder.adoptStreamEventIDs(tracker.events())
		builder.markCanonicalEvents()
		builder.mergeLiveEvents(tracker.terminalToolEvents())
		builder.mergeLiveEvents(tracker.builder.events)
		builder.lifecycleSeen = tracker.builder.lifecycleSeen
		builder.taskActive = tracker.builder.taskActive
		builder.turnLifecycle.adopt(&tracker.builder.turnLifecycle)
	}

	conversation := builder.conversation()
	if tracker != nil {
		conversation.ProviderTurns = mergeGrokProviderTurns(
			tracker.providerTurns,
			conversation.ProviderTurns,
		)
	}
	return conversation, nil
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

func newGrokUpdateTracker(sessionDir string) (*grokUpdateTracker, error) {
	builder := newGrokConversationBuilder(filepath.Base(sessionDir))
	if summary, err := readGrokSummary(sessionDir); err == nil {
		builder.sessionID = strings.TrimSpace(summary.Info.ID)
		builder.cwd = strings.TrimSpace(summary.Info.CWD)
	}
	tracker := &grokUpdateTracker{
		builder:       builder,
		terminalTools: map[string]CodexConversationEvent{},
	}
	updatesPath := filepath.Join(sessionDir, grokUpdatesFile)
	offset, err := findLatestGrokTurnOffset(updatesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return tracker, nil
		}
		return nil, err
	}
	tracker.offset = offset
	info, err := os.Stat(updatesPath)
	if err != nil {
		return nil, err
	}
	if err := tracker.consumeFrom(updatesPath, info.Size()); err != nil {
		return nil, err
	}
	return tracker, nil
}

func (t *grokUpdateTracker) consumeFrom(path string, limit int64) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()
	if _, err := file.Seek(t.offset, io.SeekStart); err != nil {
		return err
	}
	if limit <= t.offset {
		return nil
	}
	reader := bufio.NewReader(io.LimitReader(file, limit-t.offset))
	for {
		lineStart := t.offset
		line, readErr := reader.ReadBytes('\n')
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			if readErr == io.EOF && !json.Valid(trimmed) {
				// Do not consume a partially-written JSONL record. The next cache
				// refresh will retry it after the provider appends the remainder.
				t.offset = lineStart
				return nil
			}
			if grokSessionUpdateKind(trimmed) == "user_message_chunk" &&
				t.builder.lifecycleSeen && !t.builder.taskActive {
				t.rememberIdentities(t.builder.events)
				t.rememberProviderTurns(t.builder.turnLifecycle.snapshots())
				sourceID := t.builder.sourceID
				sessionID := t.builder.sessionID
				cwd := t.builder.cwd
				backgroundCallByTask := t.builder.backgroundCallByTask
				t.builder = newGrokConversationBuilder(sourceID)
				t.builder.sessionID = sessionID
				t.builder.cwd = cwd
				// A Grok background task can complete after a later user turn has
				// started. Preserve its native task -> tool identity across the
				// current-turn tracker reset so completion updates the same tool.
				t.builder.backgroundCallByTask = backgroundCallByTask
				// Retain only the bounded terminal tool set. This prevents delayed
				// in_progress projections in a later turn from regressing terminal
				// status or output while avoiding an unbounded per-builder history.
				for _, event := range t.terminalTools {
					if callID := strings.TrimSpace(event.CallID); callID != "" {
						t.builder.terminalToolCalls[callID] = struct{}{}
					}
				}
				t.line = 0
			}
			t.line++
			t.builder.consumeUpdatesLine(t.line, trimmed)
		}
		t.offset += int64(len(line))
		if readErr != nil {
			if readErr == io.EOF {
				return nil
			}
			return readErr
		}
	}
}

func (t *grokUpdateTracker) rememberProviderTurns(turns []CodexConversationTurn) {
	if t == nil || len(turns) == 0 {
		return
	}
	t.providerTurns = mergeGrokProviderTurns(t.providerTurns, turns)
	if len(t.providerTurns) > maxCodexConversationTurnHistory {
		t.providerTurns = append(
			[]CodexConversationTurn(nil),
			t.providerTurns[len(t.providerTurns)-maxCodexConversationTurnHistory:]...,
		)
	}
}

func mergeGrokProviderTurns(groups ...[]CodexConversationTurn) []CodexConversationTurn {
	turns := make([]CodexConversationTurn, 0)
	seen := make(map[string]struct{})
	for _, group := range groups {
		for _, turn := range group {
			key := strings.Join(
				[]string{turn.ID, turn.Status, turn.StartedAt, turn.SettledAt},
				"\x00",
			)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			turns = append(turns, turn)
		}
	}
	if len(turns) > maxCodexConversationTurnHistory {
		turns = append(
			[]CodexConversationTurn(nil),
			turns[len(turns)-maxCodexConversationTurnHistory:]...,
		)
	}
	return turns
}

func (t *grokUpdateTracker) rememberIdentities(events []CodexConversationEvent) {
	for _, event := range events {
		if event.Kind == "assistant_message" || event.Kind == "commentary" {
			t.identities = append(t.identities, event)
			continue
		}
		if event.Kind == "tool" && !event.Partial && event.Status != "running" && event.ID != "" {
			if t.terminalTools == nil {
				t.terminalTools = map[string]CodexConversationEvent{}
			}
			if _, exists := t.terminalTools[event.ID]; !exists {
				t.terminalToolOrder = append(t.terminalToolOrder, event.ID)
			}
			t.terminalTools[event.ID] = event
		}
	}
	if len(t.identities) > maxGrokConversationEvents*2 {
		t.identities = append([]CodexConversationEvent(nil), t.identities[len(t.identities)-maxGrokConversationEvents*2:]...)
	}
	for len(t.terminalToolOrder) > maxGrokConversationEvents*2 {
		oldest := t.terminalToolOrder[0]
		t.terminalToolOrder = t.terminalToolOrder[1:]
		delete(t.terminalTools, oldest)
	}
}

func (t *grokUpdateTracker) events() []CodexConversationEvent {
	events := make([]CodexConversationEvent, 0, len(t.identities)+len(t.builder.events))
	events = append(events, t.identities...)
	events = append(events, t.builder.events...)
	return events
}

func (t *grokUpdateTracker) terminalToolEvents() []CodexConversationEvent {
	events := make([]CodexConversationEvent, 0, len(t.terminalToolOrder))
	for _, id := range t.terminalToolOrder {
		if event, exists := t.terminalTools[id]; exists {
			events = append(events, event)
		}
	}
	return events
}

func grokSessionUpdateKind(line []byte) string {
	var envelope struct {
		Params struct {
			Update struct {
				SessionUpdate string `json:"sessionUpdate"`
			} `json:"update"`
		} `json:"params"`
	}
	if json.Unmarshal(line, &envelope) != nil {
		return ""
	}
	return strings.TrimSpace(envelope.Params.Update.SessionUpdate)
}

func findLatestGrokTurnOffset(path string) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	const blockSize int64 = 64 * 1024
	marker := []byte(`"sessionUpdate":"user_message_chunk"`)
	for end := info.Size(); end > 0; {
		start := end - blockSize
		if start < 0 {
			start = 0
		}
		readEnd := end + int64(len(marker))
		if readEnd > info.Size() {
			readEnd = info.Size()
		}
		buffer := make([]byte, readEnd-start)
		if _, err := file.ReadAt(buffer, start); err != nil && err != io.EOF {
			return 0, err
		}
		if index := bytes.LastIndex(buffer, marker); index >= 0 && int64(index) < end-start {
			return findGrokLineStart(file, start+int64(index))
		}
		end = start
	}
	return 0, nil
}

func findGrokLineStart(file *os.File, offset int64) (int64, error) {
	const blockSize int64 = 4096
	for offset > 0 {
		start := offset - blockSize
		if start < 0 {
			start = 0
		}
		buffer := make([]byte, offset-start)
		if _, err := file.ReadAt(buffer, start); err != nil && err != io.EOF {
			return 0, err
		}
		if index := bytes.LastIndexByte(buffer, '\n'); index >= 0 {
			return start + int64(index) + 1, nil
		}
		offset = start
	}
	return 0, nil
}

type grokConversationBuilder struct {
	sourceID             string
	sessionID            string
	cwd                  string
	events               []CodexConversationEvent
	eventByCall          map[string]int
	seenPlanKeys         map[string]struct{}
	pendingThought       string
	streamByKey          map[string]int
	streamRaw            map[string]string
	streamByGroup        map[string]string
	statusByKey          map[string]int
	canonicalIDs         map[string]struct{}
	backgroundCallByTask map[string]string
	terminalToolCalls    map[string]struct{}
	taskActive           bool
	lifecycleSeen        bool
	turnSettled          bool
	turnLifecycle        codexConversationTurnLifecycle
	nextEventID          int
}

func newGrokConversationBuilder(sourceID string) *grokConversationBuilder {
	return &grokConversationBuilder{
		sourceID:             sourceID,
		eventByCall:          map[string]int{},
		seenPlanKeys:         map[string]struct{}{},
		streamByKey:          map[string]int{},
		streamRaw:            map[string]string{},
		streamByGroup:        map[string]string{},
		statusByKey:          map[string]int{},
		canonicalIDs:         map[string]struct{}{},
		backgroundCallByTask: map[string]string{},
		terminalToolCalls:    map[string]struct{}{},
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
		if text := grokReasoningText(record.Summary); text != "" {
			b.upsertThought(lineNumber, "", text, true)
		}
	case "tool_result":
		callID := strings.TrimSpace(record.ToolCallID)
		output := grokMessageText(record.Content)
		if callID == "" {
			return
		}
		if output != "" {
			b.updateToolOutput(lineNumber, "", callID, output)
		} else {
			b.updateToolStatus(callID, "done")
		}
	}
}

func (b *grokConversationBuilder) consumeUpdatesLine(lineNumber int, line []byte) {
	var envelope struct {
		Timestamp json.RawMessage `json:"timestamp"`
		Params    struct {
			SessionID string `json:"sessionId"`
			Meta      struct {
				PromptID      string          `json:"promptId"`
				StreamStartMS json.RawMessage `json:"streamStartMs"`
				UpdateParams  struct {
					Status string `json:"status"`
				} `json:"updateParams"`
			} `json:"_meta"`
			Update struct {
				SessionUpdate    string          `json:"sessionUpdate"`
				Type             string          `json:"type"`
				Content          json.RawMessage `json:"content"`
				ToolCallID       string          `json:"toolCallId"`
				Title            string          `json:"title"`
				RawInput         json.RawMessage `json:"rawInput"`
				RawOutput        json.RawMessage `json:"rawOutput"`
				Status           string          `json:"status"`
				Kind             string          `json:"kind"`
				Attempt          int             `json:"attempt"`
				MaxRetries       int             `json:"max_retries"`
				Reason           string          `json:"reason"`
				Message          string          `json:"message"`
				LegacyToolCallID string          `json:"tool_call_id"`
				TaskID           string          `json:"task_id"`
				Command          string          `json:"command"`
				Description      string          `json:"description"`
				TaskSnapshot     struct {
					TaskID           string          `json:"task_id"`
					Command          string          `json:"command"`
					Output           string          `json:"output"`
					OutputFile       string          `json:"output_file"`
					ExitCode         *int            `json:"exit_code"`
					Signal           json.RawMessage `json:"signal"`
					Completed        bool            `json:"completed"`
					Kind             string          `json:"kind"`
					ExplicitlyKilled bool            `json:"explicitly_killed"`
				} `json:"task_snapshot"`
				Entries []struct {
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

	if b.sessionID == "" {
		b.sessionID = strings.TrimSpace(envelope.Params.SessionID)
	}

	update := envelope.Params.Update
	timestamp := grokUpdateTimestamp(envelope.Timestamp)
	promptID := strings.TrimSpace(envelope.Params.Meta.PromptID)
	streamStart := grokStreamStartIdentity(envelope.Params.Meta.StreamStartMS)
	switch update.SessionUpdate {
	case "user_message_chunk":
		// This is a lifecycle signal only. The canonical user message comes from
		// chat_history; exposing this record would leak prompt wrappers/echoes.
		b.lifecycleSeen = true
		b.startTurn(promptID, streamStart, envelope.Params.Meta.StreamStartMS, timestamp, lineNumber)
		b.taskActive = b.turnLifecycle.running()
		b.turnSettled = !b.taskActive
	case "agent_message_chunk":
		b.upsertStreamText(promptID, streamStart, "assistant", timestamp, grokRawChunkText(update.Content))
	case "agent_thought_chunk":
		b.upsertStreamText(promptID, streamStart, "reasoning", timestamp, grokRawChunkText(update.Content))
	case "tool_call", "tool_call_update":
		b.lifecycleSeen = true
		// A completed tool is still inside the active turn. Only the provider's
		// turn_completed marker settles the composer state.
		if !b.turnSettled {
			b.taskActive = true
		}
		callID := strings.TrimSpace(update.ToolCallID)
		currentName := ""
		currentStatus := ""
		if index, exists := b.eventByCall[callID]; exists && index >= 0 && index < len(b.events) {
			currentName = b.events[index].ToolName
			currentStatus = b.events[index].Status
		}
		nativeStatus := grokToolStatus(firstNonEmpty(update.Status, envelope.Params.Meta.UpdateParams.Status))
		if _, terminal := b.terminalToolCalls[callID]; terminal && nativeStatus == "running" {
			// task_completed/turn_completed is authoritative for the complete
			// projection. Ignore delayed running title/input/output snapshots,
			// not merely their status bit.
			break
		}
		status := firstNonEmpty(
			nativeStatus,
			currentStatus,
			"running",
		)
		if b.turnSettled && status == "running" {
			status = firstNonEmpty(grokTerminalToolStatus(currentStatus), "done")
		}
		name := firstNonEmpty(cleanConversationText(update.Title), currentName, cleanConversationText(update.Kind), "tool")
		b.addToolStart(lineNumber, timestamp, callID, name, grokJSONPayloadText(update.RawInput), status)
		if output := firstNonEmpty(grokToolUpdateOutput(update.Content), grokToolUpdateOutput(update.RawOutput)); output != "" {
			b.updateToolOutputSnapshot(timestamp, update.ToolCallID, output)
		}
		b.updateToolStatus(update.ToolCallID, status)
		if index, exists := b.eventByCall[callID]; exists && index >= 0 && index < len(b.events) {
			_, canonical := b.canonicalIDs[b.events[index].ID]
			b.events[index].Partial = !canonical && !b.turnSettled && b.events[index].Status == "running"
			b.events[index].Transient = !canonical
		}
	case "task_backgrounded":
		b.lifecycleSeen = true
		taskID := strings.TrimSpace(update.TaskID)
		callID := strings.TrimSpace(update.LegacyToolCallID)
		if taskID != "" && callID != "" {
			b.backgroundCallByTask[taskID] = callID
		}
		if callID == "" {
			return
		}
		delete(b.terminalToolCalls, callID)
		name := "Background task"
		if index, exists := b.eventByCall[callID]; exists && index >= 0 && index < len(b.events) {
			name = firstNonEmpty(b.events[index].ToolName, name)
		}
		status := "running"
		partial := true
		if b.turnSettled {
			status = "done"
			partial = false
		} else {
			b.taskActive = true
		}
		b.addToolStart(lineNumber, timestamp, callID, name, sanitizeGrokToolOutput(update.Command), status)
		b.setToolProjection(callID, status, partial, false)
	case "task_completed":
		b.lifecycleSeen = true
		snapshot := update.TaskSnapshot
		taskID := strings.TrimSpace(snapshot.TaskID)
		callID := b.backgroundCallByTask[taskID]
		if callID == "" {
			callID = grokBackgroundCallID(taskID, snapshot.OutputFile, b.eventByCall)
		}
		if callID == "" {
			return
		}
		name := firstNonEmpty(cleanToolName(snapshot.Kind), "Background task")
		if index, exists := b.eventByCall[callID]; exists && index >= 0 && index < len(b.events) {
			name = firstNonEmpty(b.events[index].ToolName, name)
		}
		status := grokBackgroundTaskStatus(snapshot.ExitCode, snapshot.Signal, snapshot.ExplicitlyKilled)
		b.addToolStart(lineNumber, timestamp, callID, name, sanitizeGrokToolOutput(snapshot.Command), status)
		if output := sanitizeGrokToolOutput(snapshot.Output); output != "" {
			b.updateToolOutputSnapshot(timestamp, callID, output)
		}
		b.setToolProjection(callID, status, false, true)
		delete(b.backgroundCallByTask, taskID)
	case "plan":
		b.addPlanUpdate(lineNumber, timestamp, update.Entries)
	case "retry_state":
		b.lifecycleSeen = true
		failed := strings.EqualFold(strings.TrimSpace(update.Type), "failed")
		status := "running"
		title := "Retrying provider request"
		body := cleanConversationText(update.Reason)
		if failed {
			status = "failed"
			title = "Provider request failed"
			body = cleanConversationText(firstNonEmpty(update.Message, update.Reason))
			b.taskActive = false
			b.turnSettled = true
			b.settleTurn(CodexConversationTurnFailed, timestamp, lineNumber)
		} else {
			b.startTurn(promptID, streamStart, envelope.Params.Meta.StreamStartMS, timestamp, lineNumber)
			b.taskActive = b.turnLifecycle.running()
			b.turnSettled = !b.taskActive
			if update.Attempt > 0 && update.MaxRetries > 0 {
				title = fmt.Sprintf("Retrying provider request (%d/%d)", update.Attempt, update.MaxRetries)
			}
		}
		b.upsertStatus("retry", timestamp, title, body, status, !failed)
		if failed {
			b.finishAllStreams()
			b.finishTools("failed")
			b.finishStatuses()
		}
	case "turn_completed":
		b.lifecycleSeen = true
		b.taskActive = false
		b.turnSettled = true
		b.settleTurn(CodexConversationTurnCompleted, timestamp, lineNumber)
		b.finishAllStreams()
		b.finishTools("done")
		b.finishStatuses()
	}
}

func (b *grokConversationBuilder) startTurn(promptID, streamStart string, streamStartRaw json.RawMessage, timestamp string, lineNumber int) {
	providerTurnID := strings.Trim(strings.Join([]string{strings.TrimSpace(promptID), strings.TrimSpace(streamStart)}, ":"), ":")
	id := ""
	if providerTurnID != "" {
		id = conversationTurnID(firstNonEmpty(b.sessionID, b.sourceID), providerTurnID, lineNumber)
	} else if b.turnLifecycle.running() {
		id = b.turnLifecycle.turn.ID
	} else {
		id = conversationTurnID(firstNonEmpty(b.sessionID, b.sourceID), "", lineNumber)
	}
	b.turnLifecycle.start(id, grokTurnStartedAt(streamStartRaw, timestamp))
}

func (b *grokConversationBuilder) settleTurn(status, timestamp string, lineNumber int) {
	id := ""
	if b.turnLifecycle.turn == nil {
		id = conversationTurnID(firstNonEmpty(b.sessionID, b.sourceID), "", lineNumber)
	}
	b.turnLifecycle.settle(id, status, timestamp)
}

func (b *grokConversationBuilder) upsertStatus(key, timestamp, title, body, status string, partial bool) {
	if index, ok := b.statusByKey[key]; ok && index >= 0 && index < len(b.events) {
		event := &b.events[index]
		event.Title = title
		event.Body = body
		event.Status = status
		event.Partial = partial
		return
	}
	event := CodexConversationEvent{
		ID:        fmt.Sprintf("%s:status:%s", firstNonEmpty(b.sessionID, b.sourceID, "grok"), key),
		Timestamp: timestamp,
		Kind:      "status",
		Title:     title,
		Body:      body,
		Status:    status,
		Partial:   partial,
		Transient: true,
		Source:    "grok_session",
	}
	if b.addEvent(event) {
		b.statusByKey[key] = len(b.events) - 1
	}
}

func (b *grokConversationBuilder) finishStatuses() {
	for _, index := range b.statusByKey {
		if index < 0 || index >= len(b.events) {
			continue
		}
		event := &b.events[index]
		if event.Status == "running" {
			event.Status = "done"
		}
		event.Partial = false
	}
}

func (b *grokConversationBuilder) markCanonicalEvents() {
	for _, event := range b.events {
		if event.ID != "" {
			b.canonicalIDs[event.ID] = struct{}{}
		}
	}
}

func (b *grokConversationBuilder) upsertStreamText(promptID, streamStart, streamKind, timestamp, rawChunk string) {
	if !b.turnSettled && !b.turnLifecycle.running() {
		providerTurnID := strings.Trim(strings.Join([]string{strings.TrimSpace(promptID), strings.TrimSpace(streamStart)}, ":"), ":")
		b.turnLifecycle.start(
			conversationTurnID(firstNonEmpty(b.sessionID, b.sourceID), providerTurnID, 1),
			timestamp,
		)
	}
	group := firstNonEmpty(promptID, "turn") + ":" + streamKind
	streamStart = firstNonEmpty(streamStart, "legacy")
	key := group + ":" + streamStart
	if previous := b.streamByGroup[group]; previous != "" && previous != key {
		b.finishStreamKey(previous)
	}
	b.streamByGroup[group] = key
	b.streamRaw[key] += rawChunk
	body := CleanCodexDisplayText(b.streamRaw[key])
	if body == "" || isTranscriptBoilerplate(body) {
		return
	}
	b.lifecycleSeen = true
	partial := !b.turnSettled
	status := "done"
	if partial {
		b.taskActive = true
		status = "running"
	}
	if index, ok := b.streamByKey[key]; ok && index >= 0 && index < len(b.events) {
		b.events[index].Body = truncateConversationBody(body)
		b.events[index].Partial = partial
		b.events[index].Status = status
		return
	}
	kind := "assistant_message"
	title := ""
	role := "assistant"
	if streamKind == "reasoning" {
		kind = "commentary"
		title = "Reasoning"
		role = ""
	}
	event := CodexConversationEvent{
		ID:        fmt.Sprintf("%s:stream:%s:%s:%s", firstNonEmpty(b.sessionID, b.sourceID, "grok"), firstNonEmpty(promptID, "turn"), streamKind, streamStart),
		Timestamp: timestamp,
		Kind:      kind,
		Role:      role,
		Title:     title,
		Body:      body,
		Status:    status,
		Partial:   partial,
		Transient: true,
		Source:    "grok_session",
	}
	if b.addEvent(event) {
		b.streamByKey[key] = len(b.events) - 1
	}
}

func grokRawChunkText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var payload struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &payload) == nil && payload.Text != "" {
		return payload.Text
	}
	return grokChunkText(raw)
}

func (b *grokConversationBuilder) finishStreamKey(key string) {
	index, ok := b.streamByKey[key]
	if !ok || index < 0 || index >= len(b.events) {
		return
	}
	b.events[index].Status = "done"
	b.events[index].Partial = false
}

func (b *grokConversationBuilder) finishAllStreams() {
	for _, key := range b.streamByGroup {
		b.finishStreamKey(key)
	}
	b.streamByGroup = map[string]string{}
}

func (b *grokConversationBuilder) finishTools(status string) {
	status = grokToolStatus(status)
	if status == "" || status == "running" {
		status = "done"
	}
	for callID, index := range b.eventByCall {
		if index < 0 || index >= len(b.events) {
			continue
		}
		event := &b.events[index]
		if event.Status == "running" || event.Partial {
			event.Status = status
		}
		event.Partial = false
		b.terminalToolCalls[callID] = struct{}{}
	}
}

func (b *grokConversationBuilder) setToolProjection(callID, status string, partial, terminal bool) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return
	}
	index, exists := b.eventByCall[callID]
	if !exists || index < 0 || index >= len(b.events) {
		return
	}
	event := &b.events[index]
	event.Status = firstNonEmpty(grokToolStatus(status), event.Status, "done")
	event.Partial = partial
	if _, canonical := b.canonicalIDs[event.ID]; !canonical {
		event.Transient = true
	}
	if terminal {
		b.terminalToolCalls[callID] = struct{}{}
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
	if _, terminal := b.terminalToolCalls[callID]; terminal && status == "running" {
		status = "done"
		if index, exists := b.eventByCall[callID]; exists && index >= 0 && index < len(b.events) {
			status = firstNonEmpty(b.events[index].Status, status)
		}
	}
	event := CodexConversationEvent{
		ID:        b.eventID(lineNumber),
		Timestamp: timestamp,
		Kind:      "tool",
		Title:     "Tool",
		ToolName:  name,
		Input:     truncateConversationBody(input),
		CallID:    callID,
		Status:    status,
		Source:    "grok_session",
	}
	if callID != "" {
		event.ID = fmt.Sprintf("%s:tool:%s", firstNonEmpty(b.sessionID, b.sourceID, "grok"), callID)
	}
	if callID != "" {
		if index, exists := b.eventByCall[callID]; exists && index >= 0 && index < len(b.events) {
			b.events[index].ToolName = event.ToolName
			if event.Input != "" {
				b.events[index].Input = event.Input
			}
			b.events[index].Status = event.Status
			if b.events[index].Timestamp == "" && timestamp != "" {
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
		if b.events[index].Timestamp == "" && timestamp != "" {
			b.events[index].Timestamp = timestamp
		}
		return
	}
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
		Source:    "grok_session",
	})
}

func (b *grokConversationBuilder) updateToolOutputSnapshot(timestamp, callID, output string) {
	callID = strings.TrimSpace(callID)
	output = truncateConversationBody(output)
	if callID == "" || output == "" {
		return
	}
	if index, exists := b.eventByCall[callID]; exists && index >= 0 && index < len(b.events) {
		b.events[index].Output = output
		if b.events[index].Timestamp == "" && timestamp != "" {
			b.events[index].Timestamp = timestamp
		}
	}
}

func (b *grokConversationBuilder) updateToolStatus(callID, status string) {
	callID = strings.TrimSpace(callID)
	status = grokToolStatus(status)
	if callID == "" || status == "" {
		return
	}
	if _, terminal := b.terminalToolCalls[callID]; terminal && status == "running" {
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

func (b *grokConversationBuilder) eventID(_ int) string {
	b.nextEventID++
	if b.sessionID != "" {
		return fmt.Sprintf("%s:%d", b.sessionID, b.nextEventID)
	}
	return fmt.Sprintf("%s:%d", b.sourceID, b.nextEventID)
}

func (b *grokConversationBuilder) conversation() CodexConversation {
	if b.events == nil {
		b.events = []CodexConversationEvent{}
	}
	b.events = pruneGrokEventsForChat(b.events, maxGrokConversationEvents)
	if !b.taskActive {
		b.events = trimTrailingGrokTools(b.events)
	}
	b.reindexEvents()
	return conversationWithTurn(CodexConversation{
		Available: true,
		Source:    "grok_session",
		SessionID: b.sessionID,
		CWD:       b.cwd,
		Events:    b.events,
	}, &b.turnLifecycle)
}

func (b *grokConversationBuilder) adoptStreamEventIDs(streamEvents []CodexConversationEvent) {
	used := make(map[int]struct{})
	for historyIndex := len(b.events) - 1; historyIndex >= 0; historyIndex-- {
		history := &b.events[historyIndex]
		if history.Kind != "assistant_message" && history.Kind != "commentary" {
			continue
		}
		for streamIndex := len(streamEvents) - 1; streamIndex >= 0; streamIndex-- {
			stream := streamEvents[streamIndex]
			if _, alreadyUsed := used[streamIndex]; alreadyUsed ||
				!strings.Contains(stream.ID, ":stream:") ||
				stream.Kind != history.Kind ||
				CleanCodexDisplayText(stream.Body) != CleanCodexDisplayText(history.Body) {
				continue
			}
			history.ID = stream.ID
			history.Partial = false
			history.Transient = true
			used[streamIndex] = struct{}{}
			break
		}
	}
}

func (b *grokConversationBuilder) mergeLiveEvents(liveEvents []CodexConversationEvent) {
	byID := make(map[string]int, len(b.events))
	for index, event := range b.events {
		byID[event.ID] = index
	}
	for _, live := range liveEvents {
		if index, canonical := byID[live.ID]; canonical {
			current := &b.events[index]
			switch live.Kind {
			case "assistant_message", "commentary":
				current.Body = live.Body
				current.Status = "done"
				current.Partial = false
				current.Transient = live.Transient
			case "tool":
				current.Title = firstNonEmpty(live.Title, current.Title)
				current.ToolName = firstNonEmpty(live.ToolName, current.ToolName)
				current.Input = firstNonEmpty(live.Input, current.Input)
				current.Output = firstNonEmpty(live.Output, current.Output)
				canonicalStatus := grokToolStatus(current.Status)
				liveStatus := grokToolStatus(live.Status)
				switch {
				case liveStatus == "failed":
					// Native task completion carries the authoritative exit/signal
					// result and may supersede an earlier canonical launch ack.
					current.Status = "failed"
					current.Partial = false
				case canonicalStatus == "failed":
					// A delayed done/running projection cannot erase a failure.
					current.Status = "failed"
					current.Partial = false
				case liveStatus == "running" && live.Partial:
					// task_backgrounded is authoritative over the canonical launch
					// acknowledgement while the native task is genuinely active.
					current.Status = "running"
					current.Partial = true
				case canonicalStatus == "" || canonicalStatus == "running":
					current.Status = firstNonEmpty(live.Status, current.Status)
					current.Partial = live.Partial
				default:
					// chat_history is the canonical terminal state. A delayed live
					// in_progress projection must never reopen the same tool.
					current.Partial = false
				}
			}
			if live.Kind == "tool" {
				current.Transient = false
			}
			continue
		}
		live.Transient = true
		if b.addEvent(live) {
			byID[live.ID] = len(b.events) - 1
		}
	}
}

func pruneGrokEventsForChat(events []CodexConversationEvent, max int) []CodexConversationEvent {
	if len(events) == 0 {
		return events
	}
	kept := make([]CodexConversationEvent, 0, len(events))
	toolIndexes := make([]int, 0, len(events))
	for _, event := range events {
		switch event.Kind {
		case "user_message", "assistant_message", "status", "command", "patch", "web_search":
			kept = append(kept, event)
		case "commentary":
			kept = append(kept, event)
		case "tool":
			if !grokToolEventIsVisible(event) {
				continue
			}
			toolIndexes = append(toolIndexes, len(kept))
			kept = append(kept, event)
		default:
			continue
		}
	}
	for len(kept) > max && len(toolIndexes) > 0 {
		dropAt := toolIndexes[0]
		toolIndexes = toolIndexes[1:]
		kept = append(kept[:dropAt], kept[dropAt+1:]...)
		for index := range toolIndexes {
			if toolIndexes[index] > dropAt {
				toolIndexes[index]--
			}
		}
	}
	if len(toolIndexes) > maxGrokToolEventsInChat {
		dropCount := len(toolIndexes) - maxGrokToolEventsInChat
		for dropCount > 0 && len(toolIndexes) > 0 {
			dropAt := toolIndexes[0]
			toolIndexes = toolIndexes[1:]
			kept = append(kept[:dropAt], kept[dropAt+1:]...)
			for index := range toolIndexes {
				if toolIndexes[index] > dropAt {
					toolIndexes[index]--
				}
			}
			dropCount--
		}
	}
	return kept
}

func trimTrailingGrokTools(events []CodexConversationEvent) []CodexConversationEvent {
	lastMessageIndex := -1
	for index, event := range events {
		if event.Kind != "user_message" && event.Kind != "assistant_message" {
			continue
		}
		if strings.TrimSpace(event.Body) == "" {
			continue
		}
		lastMessageIndex = index
	}
	if lastMessageIndex < 0 {
		return events
	}
	kept := append([]CodexConversationEvent(nil), events[:lastMessageIndex+1]...)
	for _, event := range events[lastMessageIndex+1:] {
		if event.Kind != "tool" {
			kept = append(kept, event)
		}
	}
	return kept
}

func grokToolEventIsVisible(event CodexConversationEvent) bool {
	if strings.TrimSpace(event.Output) != "" {
		return true
	}
	status := strings.TrimSpace(strings.ToLower(event.Status))
	if status == "running" || status == "failed" {
		return true
	}
	return false
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
		Path    string          `json:"path"`
		OldText string          `json:"oldText"`
		NewText string          `json:"newText"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, block := range blocks {
			if text := sanitizeGrokToolOutput(block.Text); text != "" {
				parts = append(parts, text)
				continue
			}
			if text := grokStructuredContentText(block.Content); text != "" {
				parts = append(parts, text)
				continue
			}
			if strings.EqualFold(block.Type, "diff") {
				if text := sanitizeGrokToolOutput(firstNonEmpty(block.NewText, block.OldText)); text != "" {
					parts = append(parts, firstNonEmpty(cleanConversationText(block.Path), "diff")+"\n"+text)
				}
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n\n")
		}
	}
	return grokStructuredContentText(raw)
}

func grokStructuredContentText(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	if raw[0] == '"' {
		return sanitizeGrokToolOutput(jsonString(raw))
	}
	var content struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &content) == nil && strings.EqualFold(content.Type, "text") {
		return sanitizeGrokToolOutput(content.Text)
	}
	return ""
}

func sanitizeGrokToolOutput(value string) string {
	value = stripGrokANSI(value)
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r >= 0x20 {
			return r
		}
		return -1
	}, value)
	return CleanCodexDisplayText(value)
}

func stripGrokANSI(value string) string {
	var out strings.Builder
	for index := 0; index < len(value); {
		if value[index] != 0x1b {
			out.WriteByte(value[index])
			index++
			continue
		}
		index++
		if index >= len(value) {
			break
		}
		switch value[index] {
		case '[':
			index++
			for index < len(value) {
				final := value[index]
				index++
				if final >= 0x40 && final <= 0x7e {
					break
				}
			}
		case ']':
			index++
			for index < len(value) {
				if value[index] == 0x07 {
					index++
					break
				}
				if value[index] == 0x1b && index+1 < len(value) && value[index+1] == '\\' {
					index += 2
					break
				}
				index++
			}
		default:
			// Drop the single-character escape and its introducer.
			index++
		}
	}
	return out.String()
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

func grokTurnStartedAt(streamStartRaw json.RawMessage, fallback string) string {
	raw := bytes.TrimSpace(streamStartRaw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return normalizeCodexTimestamp(fallback)
	}
	if raw[0] == '"' {
		value := strings.TrimSpace(jsonString(raw))
		if parsed := parseNormalizedCodexTimestamp(value); !parsed.IsZero() {
			return parsed.UTC().Format(time.RFC3339Nano)
		}
		raw = []byte(value)
	}
	var value float64
	if json.Unmarshal(raw, &value) == nil {
		switch {
		case value >= 1_000_000_000_000:
			return time.UnixMilli(int64(value)).UTC().Format(time.RFC3339Nano)
		case value >= 1_000_000_000:
			return time.Unix(int64(value), 0).UTC().Format(time.RFC3339Nano)
		}
	}
	return normalizeCodexTimestamp(fallback)
}

func grokStreamStartIdentity(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	if raw[0] == '"' {
		return strings.TrimSpace(jsonString(raw))
	}
	return string(raw)
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

func grokTerminalToolStatus(value string) string {
	status := grokToolStatus(value)
	if status == "done" || status == "failed" {
		return status
	}
	return ""
}

func grokBackgroundCallID(taskID, outputFile string, eventByCall map[string]int) string {
	taskID = strings.TrimSpace(taskID)
	if taskID != "" {
		if _, exists := eventByCall[taskID]; exists {
			return taskID
		}
	}
	base := strings.TrimSuffix(filepath.Base(strings.TrimSpace(outputFile)), filepath.Ext(outputFile))
	if base == "" || base == "." {
		return ""
	}
	if _, exists := eventByCall[base]; exists || strings.HasPrefix(base, "call-") || strings.HasPrefix(base, "call_") {
		return base
	}
	return ""
}

func grokBackgroundTaskStatus(exitCode *int, signal json.RawMessage, explicitlyKilled bool) string {
	if explicitlyKilled || (exitCode != nil && *exitCode != 0) || grokTaskSignalPresent(signal) {
		return "failed"
	}
	return "done"
}

func grokTaskSignalPresent(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || bytes.Equal(raw, []byte(`""`)) || bytes.Equal(raw, []byte("0")) || bytes.Equal(raw, []byte("false")) {
		return false
	}
	return true
}

func grokVisibleUserText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if idx := strings.Index(text, "<zen_attachments>"); idx >= 0 {
		text = strings.TrimSpace(text[:idx])
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
