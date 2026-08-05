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
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

const (
	piConversationSource = "pi_session_jsonl"
	maxPiConversationAge = 72 * time.Hour
)

type piTranscriptCandidate struct {
	ID        string
	CWD       string
	Path      string
	CreatedAt time.Time
	Updated   time.Time
}

func (r *ProviderConversationReader) loadPiConversationForAgent(agent classifier.Agent, now time.Time) (CodexConversation, error) {
	if strings.TrimSpace(agent.Cwd) == "" {
		r.resetSource()
		return CodexConversation{
			Available: false,
			Reason:    "missing_cwd",
			Events:    []CodexConversationEvent{},
		}, nil
	}

	candidate, ok, err := findPiTranscript(agent, now)
	if err != nil {
		r.resetSource()
		return CodexConversation{}, err
	}
	if !ok {
		r.resetSource()
		// Missing transcript is expected until the first assistant flush while
		// the process is still alive. Callers must not treat this as idle or
		// delivery failure by itself.
		return CodexConversation{
			Available: false,
			Reason:    "transcript_not_found",
			Events:    []CodexConversationEvent{},
		}, nil
	}

	conversation, err := r.loadPiConversation(candidate.Path)
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
	conversation.Source = piConversationSource
	conversation.Path = candidate.Path
	conversation.SessionID = firstNonEmpty(conversation.SessionID, candidate.ID)
	conversation.CWD = firstNonEmpty(conversation.CWD, candidate.CWD)
	conversation.Updated = &candidate.Updated
	if conversation.Events == nil {
		conversation.Events = []CodexConversationEvent{}
	}
	return conversation, nil
}

func (r *ProviderConversationReader) loadPiConversation(path string) (CodexConversation, error) {
	return r.loadFileConversation(AgentProviderPi, path, parsePiConversation)
}

func findPiTranscript(agent classifier.Agent, now time.Time) (piTranscriptCandidate, bool, error) {
	if path := PiOwnedSessionPath(agent.Command); path != "" {
		candidate, ok, err := readPiOwnedSessionCandidate(path, agent.Cwd, now)
		if err != nil || ok {
			return candidate, ok, err
		}
		// Owned path not yet flushed: honest missing transcript.
		return piTranscriptCandidate{}, false, nil
	}
	if dir := PiOwnedSessionDir(agent.Command); dir != "" {
		return findPiExclusiveSessionDir(dir, agent, now)
	}
	// Never auto-bind from Pi's shared per-CWD directory or newest mtime.
	return piTranscriptCandidate{}, false, nil
}

func readPiOwnedSessionCandidate(path, agentCWD string, now time.Time) (piTranscriptCandidate, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return piTranscriptCandidate{}, false, nil
		}
		return piTranscriptCandidate{}, false, err
	}
	if info.IsDir() {
		return piTranscriptCandidate{}, false, nil
	}
	meta, err := readPiSessionHeader(path)
	if err != nil {
		return piTranscriptCandidate{}, false, nil
	}
	if !piHeaderMatchesAgent(meta, agentCWD) {
		return piTranscriptCandidate{}, false, nil
	}
	updated := info.ModTime()
	if !isPiTranscriptFresh(updated, now) {
		return piTranscriptCandidate{}, false, nil
	}
	return piTranscriptCandidate{
		ID:        firstNonEmpty(meta.ID, strings.TrimSuffix(filepath.Base(path), ".jsonl")),
		CWD:       firstNonEmpty(meta.CWD, agentCWD),
		Path:      path,
		CreatedAt: meta.CreatedAt,
		Updated:   updated,
	}, true, nil
}

func findPiExclusiveSessionDir(dir string, agent classifier.Agent, now time.Time) (piTranscriptCandidate, bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return piTranscriptCandidate{}, false, nil
		}
		return piTranscriptCandidate{}, false, err
	}
	var candidates []piTranscriptCandidate
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		candidate, ok, err := readPiOwnedSessionCandidate(path, agent.Cwd, now)
		if err != nil {
			return piTranscriptCandidate{}, false, err
		}
		if !ok {
			continue
		}
		if !agent.StartedAt.IsZero() {
			minCreated := agent.StartedAt.UTC().Add(-5 * time.Second)
			maxCreated := agent.StartedAt.UTC().Add(2 * time.Minute)
			created := candidate.CreatedAt.UTC()
			if created.IsZero() || created.Before(minCreated) || created.After(maxCreated) {
				continue
			}
		}
		candidates = append(candidates, candidate)
	}
	if len(candidates) != 1 {
		return piTranscriptCandidate{}, false, nil
	}
	return candidates[0], true, nil
}

type piSessionHeader struct {
	Type      string
	Version   int
	ID        string
	CWD       string
	CreatedAt time.Time
}

func readPiSessionHeader(path string) (piSessionHeader, error) {
	file, err := os.Open(path)
	if err != nil {
		return piSessionHeader{}, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadBytes('\n')
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			var header struct {
				Type      string `json:"type"`
				Version   int    `json:"version"`
				ID        string `json:"id"`
				CWD       string `json:"cwd"`
				Timestamp string `json:"timestamp"`
			}
			if json.Unmarshal(trimmed, &header) != nil {
				return piSessionHeader{}, fmt.Errorf("invalid pi session header")
			}
			if strings.ToLower(strings.TrimSpace(header.Type)) != "session" {
				return piSessionHeader{}, fmt.Errorf("missing pi session header")
			}
			if header.Version != 0 && header.Version < 2 {
				return piSessionHeader{}, fmt.Errorf("unsupported pi session version %d", header.Version)
			}
			createdAt := parseNormalizedCodexTimestamp(normalizeCodexTimestamp(header.Timestamp))
			return piSessionHeader{
				Type:      header.Type,
				Version:   header.Version,
				ID:        strings.TrimSpace(header.ID),
				CWD:       strings.TrimSpace(header.CWD),
				CreatedAt: createdAt,
			}, nil
		}
		if err != nil {
			if err == io.EOF {
				return piSessionHeader{}, fmt.Errorf("empty pi session")
			}
			return piSessionHeader{}, err
		}
	}
}

func piHeaderMatchesAgent(meta piSessionHeader, agentCWD string) bool {
	agentCWD = strings.TrimSpace(agentCWD)
	if agentCWD == "" || meta.CWD == "" {
		return meta.CWD == "" && agentCWD == ""
	}
	for _, candidate := range transcriptCWDCandidates(agentCWD) {
		if pathsEquivalent(meta.CWD, candidate) || pathsEquivalent(meta.CWD, agentCWD) {
			return true
		}
	}
	return false
}

func isPiTranscriptFresh(updated, now time.Time) bool {
	if updated.IsZero() || now.IsZero() {
		return true
	}
	if updated.After(now.Add(10 * time.Minute)) {
		return true
	}
	return now.Sub(updated) <= maxPiConversationAge
}

func parsePiConversation(path string) (CodexConversation, error) {
	builder := newPiConversationBuilder(strings.TrimSuffix(filepath.Base(path), ".jsonl"))
	err := scanJSONLLines(path, builder.consumeLine)
	if err != nil {
		return CodexConversation{}, err
	}
	return builder.result(), nil
}

type piConversationBuilder struct {
	sourceID          string
	sessionID         string
	cwd               string
	entries           map[string]piSessionEntry
	order             []string
	leafID            string
	activityLifecycle providerActivityLifecycle
}

type piSessionEntry struct {
	ID        string
	ParentID  string
	Timestamp string
	Type      string
	Raw       json.RawMessage
}

func newPiConversationBuilder(sourceID string) *piConversationBuilder {
	return &piConversationBuilder{
		sourceID: strings.TrimSpace(sourceID),
		entries:  map[string]piSessionEntry{},
	}
}

func (b *piConversationBuilder) consumeLine(lineNumber int, line []byte) {
	var base struct {
		Type      string          `json:"type"`
		ID        string          `json:"id"`
		ParentID  *string         `json:"parentId"`
		Timestamp string          `json:"timestamp"`
		Version   int             `json:"version"`
		CWD       string          `json:"cwd"`
		Message   json.RawMessage `json:"message"`
	}
	if json.Unmarshal(line, &base) != nil {
		return
	}
	entryType := strings.ToLower(strings.TrimSpace(base.Type))
	if entryType == "session" {
		if b.sessionID == "" {
			b.sessionID = strings.TrimSpace(base.ID)
		}
		if b.cwd == "" {
			b.cwd = strings.TrimSpace(base.CWD)
		}
		return
	}
	id := strings.TrimSpace(base.ID)
	if id == "" {
		id = fmt.Sprintf("line-%d", lineNumber)
	}
	parentID := ""
	if base.ParentID != nil {
		parentID = strings.TrimSpace(*base.ParentID)
	}
	b.entries[id] = piSessionEntry{
		ID:        id,
		ParentID:  parentID,
		Timestamp: normalizeCodexTimestamp(base.Timestamp),
		Type:      entryType,
		Raw:       append(json.RawMessage(nil), line...),
	}
	b.order = append(b.order, id)
	b.leafID = id
}

func (b *piConversationBuilder) result() CodexConversation {
	chain := b.activeParentChain()
	events := make([]CodexConversationEvent, 0, len(chain))
	eventByCall := map[string]int{}
	seq := 0
	for _, entry := range chain {
		switch entry.Type {
		case "message":
			seq = b.projectMessage(entry, &events, eventByCall, seq)
		case "custom_message":
			// Extension messages participate in LLM context; skip from chat body.
		default:
			// model_change / thinking_level_change / compaction / branch_summary /
			// session_info / label / custom stay out of the shared chat body.
		}
	}
	return conversationWithActivity(CodexConversation{
		SessionID: firstNonEmpty(b.sessionID, b.sourceID),
		CWD:       b.cwd,
		Events:    events,
	}, &b.activityLifecycle)
}

func (b *piConversationBuilder) activeParentChain() []piSessionEntry {
	if b.leafID == "" {
		return nil
	}
	var chain []piSessionEntry
	seen := map[string]struct{}{}
	current := b.leafID
	for current != "" {
		if _, dup := seen[current]; dup {
			break
		}
		seen[current] = struct{}{}
		entry, ok := b.entries[current]
		if !ok {
			break
		}
		chain = append(chain, entry)
		current = entry.ParentID
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

func (b *piConversationBuilder) projectMessage(
	entry piSessionEntry,
	events *[]CodexConversationEvent,
	eventByCall map[string]int,
	seq int,
) int {
	var envelope struct {
		Message struct {
			Role         string          `json:"role"`
			Content      json.RawMessage `json:"content"`
			StopReason   string          `json:"stopReason"`
			ErrorMessage string          `json:"errorMessage"`
			ToolCallID   string          `json:"toolCallId"`
			ToolName     string          `json:"toolName"`
			IsError      bool            `json:"isError"`
			Command      string          `json:"command"`
			Output       string          `json:"output"`
			ExitCode     *int            `json:"exitCode"`
			Cancelled    bool            `json:"cancelled"`
			Timestamp    int64           `json:"timestamp"`
		} `json:"message"`
	}
	if json.Unmarshal(entry.Raw, &envelope) != nil {
		return seq
	}
	role := strings.ToLower(strings.TrimSpace(envelope.Message.Role))
	timestamp := entry.Timestamp
	if timestamp == "" && envelope.Message.Timestamp > 0 {
		timestamp = normalizeCodexTimestamp(time.UnixMilli(envelope.Message.Timestamp).UTC().Format(time.RFC3339Nano))
	}
	switch role {
	case "user":
		exact := piUserText(envelope.Message.Content)
		if exact == "" {
			return seq
		}
		seq++
		event := CodexConversationEvent{
			ID:              firstNonEmpty(entry.ID, fmt.Sprintf("%s:user:%d", b.sourceID, seq)),
			Seq:             seq,
			Timestamp:       timestamp,
			Kind:            "user_message",
			Role:            "user",
			Body:            exact,
			AdmissionSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(exact))),
		}
		*events = append(*events, event)
		if !b.activityLifecycle.running() {
			b.activityLifecycle.start(
				providerActivityID(firstNonEmpty(b.sessionID, b.sourceID), entry.ID, seq),
				timestamp,
			)
		}
	case "assistant":
		seq = b.projectAssistant(entry, envelope.Message.Content, envelope.Message.StopReason, envelope.Message.ErrorMessage, timestamp, events, eventByCall, seq)
	case "toolresult":
		seq++
		callID := strings.TrimSpace(envelope.Message.ToolCallID)
		body := piContentText(envelope.Message.Content)
		status := "completed"
		if envelope.Message.IsError {
			status = "failed"
		}
		event := CodexConversationEvent{
			ID:        firstNonEmpty(entry.ID, fmt.Sprintf("%s:tool-result:%d", b.sourceID, seq)),
			Seq:       seq,
			Timestamp: timestamp,
			Kind:      "tool_result",
			ToolName:  strings.TrimSpace(envelope.Message.ToolName),
			CallID:    callID,
			Output:    body,
			Status:    status,
		}
		if index, ok := eventByCall[callID]; ok && index >= 0 && index < len(*events) {
			(*events)[index].Output = body
			(*events)[index].Status = status
			(*events)[index].Partial = false
		} else {
			*events = append(*events, event)
		}
	case "bashexecution":
		seq++
		status := "completed"
		if envelope.Message.Cancelled {
			status = "cancelled"
		}
		*events = append(*events, CodexConversationEvent{
			ID:        firstNonEmpty(entry.ID, fmt.Sprintf("%s:bash:%d", b.sourceID, seq)),
			Seq:       seq,
			Timestamp: timestamp,
			Kind:      "command_execution",
			Command:   strings.TrimSpace(envelope.Message.Command),
			Output:    envelope.Message.Output,
			ExitCode:  envelope.Message.ExitCode,
			Status:    status,
		})
	}
	return seq
}

func (b *piConversationBuilder) projectAssistant(
	entry piSessionEntry,
	raw json.RawMessage,
	stopReason, errorMessage, timestamp string,
	events *[]CodexConversationEvent,
	eventByCall map[string]int,
	seq int,
) int {
	blocks := piContentBlocks(raw)
	for _, block := range blocks {
		switch block.Type {
		case "thinking":
			text := strings.TrimSpace(block.Thinking)
			if text == "" {
				continue
			}
			seq++
			*events = append(*events, CodexConversationEvent{
				ID:        firstNonEmpty(entry.ID+"-thinking", fmt.Sprintf("%s:thinking:%d", b.sourceID, seq)),
				Seq:       seq,
				Timestamp: timestamp,
				Kind:      "reasoning",
				Body:      text,
				Partial:   false,
				Transient: true,
			})
		case "text":
			text := strings.TrimSpace(block.Text)
			if text == "" {
				continue
			}
			seq++
			*events = append(*events, CodexConversationEvent{
				ID:        firstNonEmpty(entry.ID+"-text", fmt.Sprintf("%s:assistant:%d", b.sourceID, seq)),
				Seq:       seq,
				Timestamp: timestamp,
				Kind:      "assistant_message",
				Role:      "assistant",
				Body:      text,
				Partial:   false,
			})
		case "toolcall":
			seq++
			callID := firstNonEmpty(block.ID, fmt.Sprintf("call-%d", seq))
			input, _ := json.Marshal(block.Arguments)
			event := CodexConversationEvent{
				ID:        firstNonEmpty(entry.ID+"-"+callID, fmt.Sprintf("%s:tool:%d", b.sourceID, seq)),
				Seq:       seq,
				Timestamp: timestamp,
				Kind:      "tool_call",
				ToolName:  strings.TrimSpace(block.Name),
				CallID:    callID,
				Input:     string(input),
				Status:    "running",
				Partial:   true,
			}
			*events = append(*events, event)
			eventByCall[callID] = len(*events) - 1
		}
	}
	stopReason = strings.ToLower(strings.TrimSpace(stopReason))
	switch stopReason {
	case "tooluse":
		if !b.activityLifecycle.running() {
			b.activityLifecycle.start(
				providerActivityID(firstNonEmpty(b.sessionID, b.sourceID), entry.ID, seq),
				timestamp,
			)
		}
	case "error":
		if strings.TrimSpace(errorMessage) != "" {
			seq++
			*events = append(*events, CodexConversationEvent{
				ID:        firstNonEmpty(entry.ID+"-error", fmt.Sprintf("%s:error:%d", b.sourceID, seq)),
				Seq:       seq,
				Timestamp: timestamp,
				Kind:      "status",
				Title:     "error",
				Body:      strings.TrimSpace(errorMessage),
				Status:    "failed",
			})
		}
		b.activityLifecycle.settle("", ProviderActivityFailed, timestamp)
	case "aborted":
		b.activityLifecycle.settle("", ProviderActivityInterrupted, timestamp)
	case "stop", "length":
		b.activityLifecycle.settle("", ProviderActivityCompleted, timestamp)
	}
	return seq
}

type piContentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text"`
	Thinking  string         `json:"thinking"`
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func piUserText(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var text string
		if json.Unmarshal(raw, &text) == nil {
			return text
		}
	}
	return piContentText(raw)
}

func piContentText(raw json.RawMessage) string {
	blocks := piContentBlocks(raw)
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == "text" || block.Type == "" {
			if text := strings.TrimSpace(block.Text); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func piContentBlocks(raw json.RawMessage) []piContentBlock {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil
	}
	if raw[0] == '"' {
		var text string
		if json.Unmarshal(raw, &text) == nil {
			return []piContentBlock{{Type: "text", Text: text}}
		}
	}
	var blocks []piContentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		return blocks
	}
	return nil
}

func scanJSONLLines(path string, consume func(lineNumber int, line []byte)) error {
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
