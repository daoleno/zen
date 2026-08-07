package work

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

const (
	opencodeConversationSource = "opencode_db"
	maxOpenCodeConversationAge = 72 * time.Hour
)

type openCodeSessionCandidate struct {
	ID        string
	CWD       string
	ParentID  string
	CreatedAt time.Time
	Updated   time.Time
}

func (r *ProviderConversationReader) loadOpenCodeConversationForAgent(agent classifier.Agent, now time.Time) (CodexConversation, error) {
	if strings.TrimSpace(agent.Cwd) == "" {
		r.resetSource()
		return CodexConversation{
			Available: false,
			Reason:    "missing_cwd",
			Events:    []CodexConversationEvent{},
		}, nil
	}

	candidate, ok, err := r.findOpenCodeSession(agent, now)
	if err != nil {
		r.resetSource()
		return CodexConversation{}, err
	}
	if !ok {
		r.resetSource()
		return CodexConversation{
			Available: false,
			Reason:    "session_not_found",
			Events:    []CodexConversationEvent{},
		}, nil
	}

	dbPath, err := openCodeDBPath()
	if err != nil || dbPath == "" {
		r.resetSource()
		return CodexConversation{
			Available: false,
			Reason:    "db_unavailable",
			Events:    []CodexConversationEvent{},
		}, nil
	}

	conversation, err := r.loadOpenCodeConversation(dbPath, candidate.ID)
	if err != nil {
		return CodexConversation{}, err
	}
	conversation.Available = true
	conversation.Source = opencodeConversationSource
	conversation.Path = dbPath
	conversation.SessionID = firstNonEmpty(conversation.SessionID, candidate.ID)
	conversation.CWD = firstNonEmpty(conversation.CWD, candidate.CWD)
	conversation.Updated = &candidate.Updated
	if conversation.Events == nil {
		conversation.Events = []CodexConversationEvent{}
	}
	return conversation, nil
}

func (r *ProviderConversationReader) findOpenCodeSession(agent classifier.Agent, now time.Time) (openCodeSessionCandidate, bool, error) {
	if owned := strings.TrimSpace(r.openCodeOwnedSessionID); owned != "" {
		if candidate, ok := r.revalidateOpenCodeOwnedSession(owned, agent.Cwd); ok {
			return candidate, true, nil
		}
		r.openCodeOwnedSessionID = ""
	}
	if owned := OpenCodeOwnedSessionID(agent.Command); owned != "" {
		if candidate, ok := r.revalidateOpenCodeOwnedSession(owned, agent.Cwd); ok {
			r.openCodeOwnedSessionID = candidate.ID
			return candidate, true, nil
		}
		return openCodeSessionCandidate{}, false, nil
	}

	dbPath, err := openCodeDBPath()
	if err != nil || dbPath == "" {
		return openCodeSessionCandidate{}, false, err
	}
	candidates, err := queryOpenCodeSessions(dbPath, agent.Cwd)
	if err != nil {
		return openCodeSessionCandidate{}, false, err
	}
	// Child sessions (session.parent_id set) are Task/sub-agent activity, not
	// the Zen agent's own transcript thread. Only root sessions can bind.
	roots := rootOpenCodeCandidates(candidates)
	fresh := freshOpenCodeSessionCandidates(roots, now)
	if len(openCodeWindowCandidates(fresh, agent.StartedAt)) > 0 {
		// A StartedAt window exists: unique min-delta bind, else refuse.
		if matched, ok := matchOpenCodeSessionToAgentStart(fresh, agent.StartedAt); ok {
			r.openCodeOwnedSessionID = matched.ID
			return matched, true, nil
		}
		return openCodeSessionCandidate{}, false, nil
	}
	// No window candidate (startedAt zero or drifted): bind the freshest root
	// so a live session is never reported as session_not_found and the
	// Interface does not collapse an active transcript to Working-only.
	if matched, ok := freshestOpenCodeRoot(fresh); ok {
		r.openCodeOwnedSessionID = matched.ID
		return matched, true, nil
	}
	return openCodeSessionCandidate{}, false, nil
}

func (r *ProviderConversationReader) revalidateOpenCodeOwnedSession(sessionID, agentCWD string) (openCodeSessionCandidate, bool) {
	dbPath, err := openCodeDBPath()
	if err != nil || dbPath == "" {
		return openCodeSessionCandidate{}, false
	}
	candidate, ok, err := queryOpenCodeSessionByID(dbPath, sessionID)
	if err != nil || !ok {
		return openCodeSessionCandidate{}, false
	}
	// A child (subtask) session must never anchor a Zen agent transcript even
	// when previously pinned by an older reader binding.
	if strings.TrimSpace(candidate.ParentID) != "" {
		return openCodeSessionCandidate{}, false
	}
	if !openCodeDirectoryMatches(candidate.CWD, agentCWD) {
		return openCodeSessionCandidate{}, false
	}
	return candidate, true
}

func (r *ProviderConversationReader) loadOpenCodeConversation(dbPath, sessionID string) (CodexConversation, error) {
	stamp, err := openCodeConversationStamp(dbPath, sessionID)
	if err != nil {
		r.resetSource()
		return CodexConversation{}, err
	}
	previous := r.source
	if previous.provider == AgentProviderOpenCode &&
		previous.sessionID == sessionID &&
		previous.path == dbPath &&
		previous.size == stamp.size &&
		previous.modTime.Equal(stamp.modTime) &&
		previous.walSize == stamp.walSize &&
		previous.walModTime.Equal(stamp.walModTime) {
		return previous.conversation, nil
	}
	r.resetSource()
	conversation, err := parseOpenCodeConversation(dbPath, sessionID)
	if err != nil {
		return CodexConversation{}, err
	}
	after, err := openCodeConversationStamp(dbPath, sessionID)
	if err != nil {
		return CodexConversation{}, err
	}
	if after != stamp {
		return conversation, nil
	}
	r.source = providerConversationSource{
		provider:     AgentProviderOpenCode,
		path:         dbPath,
		sessionID:    sessionID,
		size:         after.size,
		modTime:      after.modTime,
		walSize:      after.walSize,
		walModTime:   after.walModTime,
		conversation: conversation,
	}
	return conversation, nil
}

type openCodeStamp struct {
	size       int64
	modTime    time.Time
	walSize    int64
	walModTime time.Time
}

func openCodeConversationStamp(dbPath, sessionID string) (openCodeStamp, error) {
	info, err := os.Stat(dbPath)
	if err != nil {
		return openCodeStamp{}, err
	}
	stamp := openCodeStamp{size: info.Size(), modTime: info.ModTime()}
	// SQLite WAL mode keeps new rows in the -wal file; the main file's stat
	// changes only at checkpoint. Without the WAL in the stamp the cache
	// returns a stale conversation (the previous completed Activity) while a
	// newly accepted turn is already in the WAL.
	if wal, err := os.Stat(dbPath + "-wal"); err == nil {
		stamp.walSize = wal.Size()
		stamp.walModTime = wal.ModTime()
	}
	return stamp, nil
}

func openCodeDBPath() (string, error) {
	if override := strings.TrimSpace(os.Getenv("ZEN_OPENCODE_DB")); override != "" {
		return override, nil
	}
	sqlitePath, err := lookPathOpenCodeDB()
	if err == nil && sqlitePath != "" {
		return sqlitePath, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	fallback := filepath.Join(home, ".local", "share", "opencode", "opencode.db")
	if _, err := os.Stat(fallback); err != nil {
		return "", nil
	}
	return fallback, nil
}

func lookPathOpenCodeDB() (string, error) {
	binary, err := exec.LookPath("opencode")
	if err != nil {
		return "", err
	}
	out, err := exec.Command(binary, "db", "path").CombinedOutput()
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", fmt.Errorf("empty opencode db path")
	}
	return path, nil
}

type openCodeSessionRow struct {
	ID          string `json:"id"`
	Directory   string `json:"directory"`
	ParentID    string `json:"parent_id"`
	TimeCreated int64  `json:"time_created"`
	TimeUpdated int64  `json:"time_updated"`
}

func queryOpenCodeSessions(dbPath, cwd string) ([]openCodeSessionCandidate, error) {
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		return nil, nil
	}
	var candidates []openCodeSessionCandidate
	seen := map[string]struct{}{}
	for _, candidateCWD := range transcriptCWDCandidates(cwd) {
		query := fmt.Sprintf(
			`SELECT id, directory, parent_id, time_created, time_updated FROM session WHERE directory = %s ORDER BY time_created DESC;`,
			sqliteStringLiteral(candidateCWD),
		)
		rows, err := queryOpenCodeSessionRows(sqlite3, dbPath, query)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if _, ok := seen[row.ID]; ok {
				continue
			}
			seen[row.ID] = struct{}{}
			candidates = append(candidates, openCodeSessionCandidate{
				ID:        row.ID,
				CWD:       row.Directory,
				ParentID:  strings.TrimSpace(row.ParentID),
				CreatedAt: time.UnixMilli(row.TimeCreated).UTC(),
				Updated:   time.UnixMilli(row.TimeUpdated).UTC(),
			})
		}
	}
	return candidates, nil
}

func queryOpenCodeSessionByID(dbPath, sessionID string) (openCodeSessionCandidate, bool, error) {
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		return openCodeSessionCandidate{}, false, nil
	}
	query := fmt.Sprintf(
		`SELECT id, directory, parent_id, time_created, time_updated FROM session WHERE id = %s LIMIT 1;`,
		sqliteStringLiteral(sessionID),
	)
	rows, err := queryOpenCodeSessionRows(sqlite3, dbPath, query)
	if err != nil || len(rows) == 0 {
		return openCodeSessionCandidate{}, false, err
	}
	row := rows[0]
	return openCodeSessionCandidate{
		ID:        row.ID,
		CWD:       row.Directory,
		ParentID:  strings.TrimSpace(row.ParentID),
		CreatedAt: time.UnixMilli(row.TimeCreated).UTC(),
		Updated:   time.UnixMilli(row.TimeUpdated).UTC(),
	}, true, nil
}

func queryOpenCodeSessionRows(sqlite3, dbPath, query string) ([]openCodeSessionRow, error) {
	uri := fmt.Sprintf("file:%s?mode=ro", dbPath)
	out, err := exec.Command(sqlite3, "-json", uri, query).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("opencode db query: %w: %s", err, strings.TrimSpace(string(out)))
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "[]" {
		return nil, nil
	}
	var rows []openCodeSessionRow
	if err := json.Unmarshal([]byte(trimmed), &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func freshOpenCodeSessionCandidates(candidates []openCodeSessionCandidate, now time.Time) []openCodeSessionCandidate {
	if len(candidates) == 0 {
		return nil
	}
	fresh := make([]openCodeSessionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if isOpenCodeSessionFresh(candidate.Updated, now) {
			fresh = append(fresh, candidate)
		}
	}
	return fresh
}

func isOpenCodeSessionFresh(updated, now time.Time) bool {
	if updated.IsZero() || now.IsZero() {
		return true
	}
	if updated.After(now.Add(10 * time.Minute)) {
		return true
	}
	return now.Sub(updated) <= maxOpenCodeConversationAge
}

func rootOpenCodeCandidates(candidates []openCodeSessionCandidate) []openCodeSessionCandidate {
	if len(candidates) == 0 {
		return nil
	}
	roots := make([]openCodeSessionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.ParentID) == "" {
			roots = append(roots, candidate)
		}
	}
	return roots
}

func openCodeWindowCandidates(candidates []openCodeSessionCandidate, startedAt time.Time) []openCodeSessionCandidate {
	if startedAt.IsZero() {
		return nil
	}
	startedAt = startedAt.UTC()
	minCreatedAt := startedAt.Add(-5 * time.Second)
	maxCreatedAt := startedAt.Add(2 * time.Minute)
	out := make([]openCodeSessionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		createdAt := candidate.CreatedAt.UTC()
		if createdAt.IsZero() || createdAt.Before(minCreatedAt) || createdAt.After(maxCreatedAt) {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func matchOpenCodeSessionToAgentStart(candidates []openCodeSessionCandidate, startedAt time.Time) (openCodeSessionCandidate, bool) {
	window := openCodeWindowCandidates(candidates, startedAt)
	if len(window) == 0 {
		return openCodeSessionCandidate{}, false
	}
	bestIndex := 0
	bestDelta := time.Duration(0)
	for index, candidate := range window {
		delta := candidate.CreatedAt.UTC().Sub(startedAt.UTC())
		if delta < 0 {
			delta = -delta
		}
		if index == 0 || delta < bestDelta {
			bestIndex = index
			bestDelta = delta
		}
	}
	for index, candidate := range window {
		if index == bestIndex {
			continue
		}
		delta := candidate.CreatedAt.UTC().Sub(startedAt.UTC())
		if delta < 0 {
			delta = -delta
		}
		if delta == bestDelta {
			return openCodeSessionCandidate{}, false
		}
	}
	return window[bestIndex], true
}

// freshestOpenCodeRoot binds the most recently updated fresh root session
// when the StartedAt window is unavailable (zero or drifted). Equal-updated
// candidates refuse as ambiguous rather than guessing.
func freshestOpenCodeRoot(candidates []openCodeSessionCandidate) (openCodeSessionCandidate, bool) {
	if len(candidates) == 0 {
		return openCodeSessionCandidate{}, false
	}
	bestIndex := 0
	for index, candidate := range candidates {
		if index == 0 || candidate.Updated.After(candidates[bestIndex].Updated) {
			bestIndex = index
		}
	}
	for index, candidate := range candidates {
		if index == bestIndex {
			continue
		}
		if candidate.Updated.Equal(candidates[bestIndex].Updated) {
			return openCodeSessionCandidate{}, false
		}
	}
	return candidates[bestIndex], true
}

func openCodeDirectoryMatches(sessionDir, agentCWD string) bool {
	sessionDir = strings.TrimSpace(sessionDir)
	agentCWD = strings.TrimSpace(agentCWD)
	if sessionDir == "" || agentCWD == "" {
		return false
	}
	for _, candidate := range transcriptCWDCandidates(agentCWD) {
		if pathsEquivalent(sessionDir, candidate) || pathsEquivalent(sessionDir, agentCWD) {
			return true
		}
	}
	return false
}

func parseOpenCodeConversation(dbPath, sessionID string) (CodexConversation, error) {
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		return CodexConversation{}, fmt.Errorf("sqlite3 required for opencode conversation")
	}
	messages, err := queryOpenCodeMessages(sqlite3, dbPath, sessionID)
	if err != nil {
		return CodexConversation{}, err
	}
	parts, err := queryOpenCodeParts(sqlite3, dbPath, sessionID)
	if err != nil {
		return CodexConversation{}, err
	}
	partsByMessage := map[string][]openCodePartRow{}
	for _, part := range parts {
		partsByMessage[part.MessageID] = append(partsByMessage[part.MessageID], part)
	}
	builder := newOpenCodeConversationBuilder(sessionID)
	for _, message := range messages {
		builder.consumeMessage(message, partsByMessage[message.ID])
	}
	return builder.result(), nil
}

type openCodeMessageRow struct {
	ID          string `json:"id"`
	TimeCreated int64  `json:"time_created"`
	TimeUpdated int64  `json:"time_updated"`
	Data        string `json:"data"`
}

type openCodePartRow struct {
	ID          string `json:"id"`
	MessageID   string `json:"message_id"`
	TimeCreated int64  `json:"time_created"`
	TimeUpdated int64  `json:"time_updated"`
	Data        string `json:"data"`
}

func queryOpenCodeMessages(sqlite3, dbPath, sessionID string) ([]openCodeMessageRow, error) {
	query := fmt.Sprintf(
		`SELECT id, time_created, time_updated, data FROM message WHERE session_id = %s ORDER BY time_created ASC, id ASC;`,
		sqliteStringLiteral(sessionID),
	)
	uri := fmt.Sprintf("file:%s?mode=ro", dbPath)
	out, err := exec.Command(sqlite3, "-json", uri, query).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("opencode message query: %w: %s", err, strings.TrimSpace(string(out)))
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "[]" {
		return nil, nil
	}
	var rows []openCodeMessageRow
	if err := json.Unmarshal([]byte(trimmed), &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func queryOpenCodeParts(sqlite3, dbPath, sessionID string) ([]openCodePartRow, error) {
	query := fmt.Sprintf(
		`SELECT id, message_id, time_created, time_updated, data FROM part WHERE session_id = %s ORDER BY time_created ASC, id ASC;`,
		sqliteStringLiteral(sessionID),
	)
	uri := fmt.Sprintf("file:%s?mode=ro", dbPath)
	out, err := exec.Command(sqlite3, "-json", uri, query).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("opencode part query: %w: %s", err, strings.TrimSpace(string(out)))
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "[]" {
		return nil, nil
	}
	var rows []openCodePartRow
	if err := json.Unmarshal([]byte(trimmed), &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func sqliteStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

type openCodeConversationBuilder struct {
	sessionID         string
	events            []CodexConversationEvent
	eventByCall       map[string]int
	subtaskIndexes    []int
	seq               int
	openSteps         int
	activityLifecycle providerActivityLifecycle
}

func newOpenCodeConversationBuilder(sessionID string) *openCodeConversationBuilder {
	return &openCodeConversationBuilder{
		sessionID:   strings.TrimSpace(sessionID),
		eventByCall: map[string]int{},
	}
}

func (b *openCodeConversationBuilder) consumeMessage(message openCodeMessageRow, parts []openCodePartRow) {
	var meta struct {
		Role string `json:"role"`
	}
	_ = json.Unmarshal([]byte(message.Data), &meta)
	role := strings.ToLower(strings.TrimSpace(meta.Role))
	timestamp := normalizeCodexTimestamp(time.UnixMilli(message.TimeCreated).UTC().Format(time.RFC3339Nano))
	switch role {
	case "user":
		exact := openCodeUserText(parts)
		if exact == "" {
			return
		}
		b.seq++
		b.events = append(b.events, CodexConversationEvent{
			ID:              firstNonEmpty(message.ID, fmt.Sprintf("%s:user:%d", b.sessionID, b.seq)),
			Seq:             b.seq,
			Timestamp:       timestamp,
			Kind:            "user_message",
			Role:            "user",
			Body:            exact,
			AdmissionSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(exact))),
		})
		if !b.activityLifecycle.running() {
			b.activityLifecycle.start(
				providerActivityID(b.sessionID, message.ID, b.seq),
				timestamp,
			)
		}
	case "assistant":
		b.projectAssistantParts(message.ID, timestamp, parts)
		b.settleFromAssistantMessage(message, timestamp)
	default:
		b.projectAssistantParts(message.ID, timestamp, parts)
		b.settleFromAssistantMessage(message, timestamp)
	}
}

// settleFromAssistantMessage uses OpenCode's authoritative message finish /
// time.completed markers: the assistant message row is the only turn-terminal
// fact. step-finish parts are per-step signals and must never settle the
// Activity (the first assistant message echo finishes with tool-calls while
// the provider is only starting real work, and treating it as completion
// pinned live Sessions to done).
func (b *openCodeConversationBuilder) settleFromAssistantMessage(message openCodeMessageRow, timestamp string) {
	var meta struct {
		Finish string `json:"finish"`
		Time   struct {
			Completed int64 `json:"completed"`
		} `json:"time"`
	}
	if json.Unmarshal([]byte(message.Data), &meta) != nil {
		return
	}
	finish := strings.ToLower(strings.TrimSpace(meta.Finish))
	if openCodeFinishContinuesTurn(finish) {
		return
	}
	if finish == "" && meta.Time.Completed <= 0 {
		return
	}
	// Message finish is authoritative for the turn. Refuse only while a tool
	// call is still marked running without a terminal finish reason.
	if finish == "" && b.hasRunningTools() {
		return
	}
	status := ProviderActivityCompleted
	switch finish {
	case "error", "failed":
		status = ProviderActivityFailed
	case "abort", "aborted", "interrupted", "cancel", "canceled", "cancelled":
		status = ProviderActivityInterrupted
	}
	settleAt := timestamp
	if meta.Time.Completed > 0 {
		settleAt = normalizeCodexTimestamp(time.UnixMilli(meta.Time.Completed).UTC().Format(time.RFC3339Nano))
	}
	b.openSteps = 0
	b.activityLifecycle.settle("", status, settleAt)
}

// openCodeFinishContinuesTurn reports whether an OpenCode message finish means
// the assistant yielded to a tool or is otherwise mid-turn. Such messages must
// never settle the turn Activity; only a terminal finish (or a finish-less
// message with a completed timestamp and no running tools) may.
func openCodeFinishContinuesTurn(finish string) bool {
	switch finish {
	case "tool-calls", "unknown", "running", "pending":
		return true
	default:
		return false
	}
}

func (b *openCodeConversationBuilder) hasRunningTools() bool {
	for _, event := range b.events {
		if event.Kind != "tool_call" {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(event.Status))
		if status == "running" || status == "pending" || event.Partial {
			return true
		}
	}
	return false
}

func (b *openCodeConversationBuilder) projectAssistantParts(messageID, timestamp string, parts []openCodePartRow) {
	for _, part := range parts {
		var payload struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			Tool        string `json:"tool"`
			CallID      string `json:"callID"`
			Reason      string `json:"reason"`
			Prompt      string `json:"prompt"`
			Description string `json:"description"`
			Agent       string `json:"agent"`
			Command     string `json:"command"`
			State       struct {
				Status string          `json:"status"`
				Input  json.RawMessage `json:"input"`
				Output string          `json:"output"`
				Error  string          `json:"error"`
			} `json:"state"`
		}
		if json.Unmarshal([]byte(part.Data), &payload) != nil {
			continue
		}
		partType := strings.ToLower(strings.TrimSpace(payload.Type))
		partTime := timestamp
		if part.TimeCreated > 0 {
			partTime = normalizeCodexTimestamp(time.UnixMilli(part.TimeCreated).UTC().Format(time.RFC3339Nano))
		}
		switch partType {
		case "step-start":
			b.openSteps++
			if !b.activityLifecycle.running() {
				b.activityLifecycle.start(
					providerActivityID(b.sessionID, part.ID, b.seq+1),
					partTime,
				)
			}
		case "step-finish":
			// A step-finish part closes one assistant step, never the turn.
			// OpenCode writes one step per assistant message and mirrors the
			// step outcome onto the message row (finish/time.completed), so the
			// step sequence goes 1 -> 0 on every step-finish while the turn is
			// still Thinking/Preparing edit/Build. Settling the whole Activity
			// here pinned live turns to done after every tool-call step.
			if b.openSteps > 0 {
				b.openSteps--
			}
		case "text":
			text := strings.TrimSpace(payload.Text)
			if text == "" {
				continue
			}
			b.seq++
			b.events = append(b.events, CodexConversationEvent{
				ID:        firstNonEmpty(part.ID, messageID+"-text"),
				Seq:       b.seq,
				Timestamp: partTime,
				Kind:      "assistant_message",
				Role:      "assistant",
				Body:      text,
				Partial:   b.openSteps > 0,
			})
		case "reasoning":
			text := strings.TrimSpace(payload.Text)
			if text == "" {
				continue
			}
			b.seq++
			b.events = append(b.events, CodexConversationEvent{
				ID:        firstNonEmpty(part.ID, messageID+"-reasoning"),
				Seq:       b.seq,
				Timestamp: partTime,
				Kind:      "reasoning",
				Body:      text,
				Partial:   b.openSteps > 0,
				Transient: true,
			})
		case "tool":
			b.seq++
			callID := firstNonEmpty(payload.CallID, part.ID)
			status := strings.ToLower(strings.TrimSpace(payload.State.Status))
			switch status {
			case "error":
				status = "failed"
			case "":
				status = "running"
			}
			input := strings.TrimSpace(string(payload.State.Input))
			output := strings.TrimSpace(payload.State.Output)
			if output == "" {
				// Upstream ToolStateError carries the failure text in state.error
				// (no state.output), so a failed card must not collapse to empty.
				output = strings.TrimSpace(payload.State.Error)
			}
			event := CodexConversationEvent{
				ID:        firstNonEmpty(part.ID, fmt.Sprintf("%s:tool:%d", b.sessionID, b.seq)),
				Seq:       b.seq,
				Timestamp: partTime,
				Kind:      "tool_call",
				ToolName:  strings.TrimSpace(payload.Tool),
				CallID:    callID,
				Input:     input,
				Output:    output,
				Status:    status,
				Partial:   status == "running" || status == "pending",
			}
			if index, ok := b.eventByCall[callID]; ok && index >= 0 && index < len(b.events) {
				event.Seq = b.events[index].Seq
				b.events[index] = event
			} else {
				b.events = append(b.events, event)
				b.eventByCall[callID] = len(b.events) - 1
			}
			if !b.activityLifecycle.running() && (status == "running" || status == "pending") {
				b.activityLifecycle.start(
					providerActivityID(b.sessionID, callID, b.seq),
					partTime,
				)
			}
		case "subtask":
			// OpenCode's Task/sub-agent launch part. Project it as a tool call
			// node so child activity is represented in the Interface instead of
			// silently dropping the parent transcript's Task activity.
			b.seq++
			callID := firstNonEmpty(part.ID, fmt.Sprintf("%s:subtask:%d", b.sessionID, b.seq))
			input, _ := json.Marshal(map[string]string{
				"prompt":      payload.Prompt,
				"description": payload.Description,
				"agent":       payload.Agent,
				"command":     payload.Command,
			})
			b.events = append(b.events, CodexConversationEvent{
				ID:        callID,
				Seq:       b.seq,
				Timestamp: partTime,
				Kind:      "tool_call",
				ToolName:  "subtask",
				CallID:    callID,
				Input:     string(input),
				Status:    "running",
				Partial:   true,
			})
			b.subtaskIndexes = append(b.subtaskIndexes, len(b.events)-1)
		case "file":
			// Optional file/ref parts are not projected into chat body yet.
		}
	}
}

// resolveSubtaskStates converges subtask part events to their final state at
// result time: a settled turn marks child activity completed, while a still
// running turn keeps it in-flight.
func (b *openCodeConversationBuilder) resolveSubtaskStates() {
	settled := !b.activityLifecycle.running()
	for _, index := range b.subtaskIndexes {
		if index < 0 || index >= len(b.events) {
			continue
		}
		event := &b.events[index]
		if settled {
			event.Status = "completed"
			event.Partial = false
		}
	}
}

func (b *openCodeConversationBuilder) result() CodexConversation {
	b.resolveSubtaskStates()
	// A settled turn means the DB rows are the final state: partial-to-final
	// replacement clears in-flight flags so the Interface renders the turn as
	// finished instead of perpetually streaming.
	if !b.activityLifecycle.running() {
		for i := range b.events {
			b.events[i].Partial = false
		}
	}
	return conversationWithActivity(CodexConversation{
		SessionID: b.sessionID,
		Events:    b.events,
	}, &b.activityLifecycle)
}

func openCodeUserText(parts []openCodePartRow) string {
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		var payload struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(part.Data), &payload) != nil {
			continue
		}
		if strings.ToLower(strings.TrimSpace(payload.Type)) != "text" {
			continue
		}
		if text := payload.Text; text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "")
}
