package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/brain"
)

// generalTopicThreadID is the non-deletable General topic id=1
// (core.telegram.org/api/forum). General messages arrive without a thread id,
// but Zen also routes thread id 1 to the Brain route defensively.
const generalTopicThreadID = 1

// isGeneralThread reports whether a message belongs to the Brain route.
func isGeneralThread(threadID int64) bool {
	return threadID == 0 || threadID == generalTopicThreadID
}

func topicLabel(session brain.WorkerRef) string {
	name := shortWorkerLabel(session.Name)
	if name == "" {
		name = strings.TrimSpace(session.ID)
	}
	runes := []rune(name)
	if len(runes) > maxTopicNameRunes {
		name = string(runes[:maxTopicNameRunes])
	}
	return name
}

// shortWorkerLabel matches the Session list presentation contract: the
// parenthesized canonical agent identity is routing metadata, not display text.
func shortWorkerLabel(value string) string {
	trimmed := strings.TrimSpace(value)
	if !strings.HasSuffix(trimmed, ")") {
		return trimmed
	}
	if index := strings.LastIndex(trimmed, " ("); index > 0 {
		return strings.TrimSpace(trimmed[:index])
	}
	return trimmed
}

func topicMappingByThread(state durableState, threadID int64) (topicMapping, bool) {
	for _, mapping := range state.Topics {
		if mapping.MessageThreadID == threadID && mapping.ChatID == state.ChatID {
			return mapping, true
		}
	}
	return topicMapping{}, false
}

// errTopicMappingLimit is the persistent degraded state when the durable
// Topic mapping budget is exhausted. It must not abort projection of the
// other mapped Sessions; it is surfaced as a connection health problem.
var errTopicMappingLimit = errors.New("Telegram Session topic mapping limit reached")

// projectSessionTopics is the single Session-topic reconciliation entry point.
// It creates/renames Topics for user-visible delegated Sessions, retires
// disappeared Sessions, and projects each mapping's user-visible assistant
// output and lifecycle state into its own Topic. It never delivers anything
// itself: output is enqueued into the shared outbox and Topic operations into
// the durable topic op queue.
func (m *Manager) projectSessionTopics(ctx context.Context, token string) error {
	state := m.store.snapshot()
	if state.OwnerID == 0 || state.ChatID == 0 || m.brain == nil {
		return nil
	}
	sessions, err := m.brain.DelegatedSessions()
	if err != nil {
		return err
	}
	present := make(map[string]brain.WorkerRef, len(sessions))
	for _, session := range sessions {
		present[session.ID] = session
	}
	now := m.now().UTC()
	limitReached := false

	if state.TopicsAvailable {
		for _, session := range sessions {
			if err := m.ensureSessionTopic(session, now); err != nil {
				if errors.Is(err, errTopicMappingLimit) {
					limitReached = true
					continue
				}
				return err
			}
		}
		if limitReached {
			m.recordError("Telegram Session topic mapping limit reached; no new Session topics are created.")
		}
	}

	// A missing Session fails closed for input; no presentation cleanup may
	// delete its Telegram history or terminate the local task.
	// A stale mapping whose exact Session identity is user-visible again is
	// revived (same durable session id, same surface), so a still-viable
	// Session always owns an exact reopen path.
	for _, mapping := range state.Topics {
		if _, ok := present[mapping.SessionID]; ok {
			if mapping.State == topicStateStale {
				if err := m.reviveTopicMapping(mapping, now); err != nil {
					return err
				}
			}
			continue
		}
		projection, err := m.brain.SessionProjection(mapping.SessionID)
		if err != nil {
			return err
		}
		if !projection.AbsenceConfirmed {
			continue
		}
		if err := m.retireTopicMapping(mapping, now); err != nil {
			return err
		}
	}

	// Projection only for mapped Topics; stale mappings are presentation-dead.
	for _, mapping := range m.store.snapshot().Topics {
		if mapping.State == topicStateStale {
			continue
		}
		if !state.TopicsAvailable && state.FallbackSessionID == mapping.SessionID {
			continue
		}
		projection, err := m.brain.SessionProjection(mapping.SessionID)
		if err != nil {
			return err
		}
		if !projection.Present {
			continue
		}
		if err := m.projectSessionOutput(mapping, projection, now); err != nil {
			return err
		}
		if err := m.projectSessionLifecycle(mapping, projection, now); err != nil {
			return err
		}
	}
	return nil
}

// ensureSessionTopic guarantees at most one durable create op per user-visible
// delegated Session and enqueues an opportunistic rename op when the display
// label changed. A durable op in any state (pending/ambiguous) prevents a
// second create; ambiguous is never retried automatically.
func (m *Manager) ensureSessionTopic(session brain.WorkerRef, now time.Time) error {
	label := topicLabel(session)
	return m.store.mutate(func(state *durableState) error {
		for index := range state.Topics {
			mapping := &state.Topics[index]
			if mapping.SessionID != session.ID {
				continue
			}
			if mapping.State != topicStateStale && mapping.Label != label {
				opID := fmt.Sprintf("topic:rename:%s:%s", session.ID, digestText(label))
				if !enqueueTopicOp(state, topicOpRecord{ID: opID, Kind: topicOpRename, SessionID: session.ID,
					MessageThreadID: mapping.MessageThreadID, Label: label, CreatedAt: now}) {
					return fmt.Errorf("Telegram topic operation queue is full")
				}
			}
			return nil
		}
		for _, op := range state.TopicOps {
			if op.Kind == topicOpCreate && op.SessionID == session.ID {
				return nil
			}
		}
		if len(state.Topics) >= maxTopicMappings {
			return errTopicMappingLimit
		}
		workItem, _, workErr := m.brain.WorkForSession(session.ID)
		if workErr != nil {
			return workErr
		}
		threadID := workItem.SourceThreadID
		if threadID == "" {
			chatThread, chatErr := m.brain.ChatThreadID()
			if chatErr != nil {
				return chatErr
			}
			threadID = chatThread
		}
		opID := "topic:create:" + session.ID
		if !enqueueTopicOp(state, topicOpRecord{ID: opID, Kind: topicOpCreate, SessionID: session.ID,
			Label: label, ThreadID: threadID, WorkID: workItem.ID, CreatedAt: now}) {
			return fmt.Errorf("Telegram topic operation queue is full")
		}
		return nil
	})
}

// reviveTopicMapping restores a stale mapping when its exact Session identity
// is user-visible again, and enqueues a one-shot revival marker.
func (m *Manager) reviveTopicMapping(mapping topicMapping, now time.Time) error {
	return m.store.mutate(func(state *durableState) error {
		ops := state.TopicOps[:0]
		for _, op := range state.TopicOps {
			if op.Kind == topicOpDelete && op.SessionID == mapping.SessionID &&
				op.MessageThreadID == mapping.MessageThreadID && op.State == "pending" {
				continue
			}
			ops = append(ops, op)
		}
		state.TopicOps = ops
		for index := range state.Topics {
			current := &state.Topics[index]
			if current.SessionID != mapping.SessionID || current.MessageThreadID != mapping.MessageThreadID {
				continue
			}
			if current.State != topicStateStale {
				return nil
			}
			current.State = topicStateActive
			current.UpdatedAt = now
			enqueueTopicTextLocked(state, fmt.Sprintf("topic:life:%s:revive", mapping.SessionID), mapping.MessageThreadID, revivedTopicText(mapping.Label), now)
			return nil
		}
		return nil
	})
}

// Private topics cannot be closed through the Bot API. Keep a tombstone and
// rename our exact topic; deleteForumTopic would erase the user's history.
func (m *Manager) retireTopicMapping(mapping topicMapping, now time.Time) error {
	return m.store.mutate(func(state *durableState) error {
		for i := range state.TopicOps {
			if state.TopicOps[i].SessionID == mapping.SessionID && state.TopicOps[i].Kind == topicOpDelete && state.TopicOps[i].State == "pending" {
				state.TopicOps[i].State = "cancelled"
			}
		}
		rows := state.Outbox[:0]
		for _, row := range state.Outbox {
			if row.State == "pending" && row.MessageThreadID == mapping.MessageThreadID && row.TopicKey != "" {
				continue
			}
			rows = append(rows, row)
		}
		state.Outbox = rows
		label := topicLabel(brain.WorkerRef{Name: "Closed - " + strings.TrimPrefix(mapping.Label, "Closed - ")})
		opID := fmt.Sprintf("topic:retire:%s:%d", mapping.SessionID, mapping.MessageThreadID)
		if !enqueueTopicOp(state, topicOpRecord{ID: opID, Kind: topicOpRename, SessionID: mapping.SessionID,
			MessageThreadID: mapping.MessageThreadID, Label: label, CreatedAt: now}) {
			return fmt.Errorf("Telegram topic operation queue is full")
		}
		for index := range state.Topics {
			current := &state.Topics[index]
			if current.SessionID == mapping.SessionID && current.MessageThreadID == mapping.MessageThreadID {
				if current.State != topicStateStale {
					current.State = topicStateStale
					current.UpdatedAt = now
				}
				break
			}
		}
		return nil
	})
}

// projectSessionOutput projects assistant output produced after the Topic was
// created. Checkpoints are per event+chunk; partial events edit their existing
// message, completed ones stay stable. A pending row coalesces in place; an
// in-flight or ambiguous row blocks later revisions, matching the Work-card
// no-replay contract.
func (m *Manager) projectSessionOutput(mapping topicMapping, projection brain.SessionProjection, now time.Time) error {
	boundary := m.store.snapshot().DeliveryStartedAt
	if boundary.After(mapping.CreatedAt) {
		mapping.CreatedAt = boundary
	}
	candidates := make([]sessionOutputCandidate, 0, len(projection.Assistant)*2)
	for _, item := range projection.Assistant {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		if !mapping.CreatedAt.IsZero() && !item.CreatedAt.After(mapping.CreatedAt) {
			continue
		}
		rendered := renderMarkdown(item.Body)
		if mapping.MessageThreadID == 0 && strings.TrimSpace(mapping.Label) != "" {
			rendered = renderMarkdown("[" + strings.TrimSpace(mapping.Label) + "]\n\n" + item.Body)
		}
		for index, chunk := range chunkRichText(rendered, maxMessageText) {
			prefix := "topic:msg:"
			if mapping.MessageThreadID == 0 {
				prefix = "private:msg:"
			}
			key := fmt.Sprintf("%s%s:%s:%d", prefix, mapping.SessionID, item.ID, index)
			candidates = append(candidates, sessionOutputCandidate{
				Key: key, CanonicalID: "session:" + mapping.SessionID + ":" + item.ID,
				Content: chunk, Digest: digestRichText(chunk),
			})
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	return m.store.mutate(func(state *durableState) error {
		for _, candidate := range candidates {
			if state.TopicProjection[candidate.Key] == candidate.Digest {
				continue
			}
			if coalesceTopicRow(state, candidate, mapping.MessageThreadID, now) {
				continue
			}
			messageID := state.TopicMessages[candidate.Key]
			kind := "send"
			if messageID != 0 {
				kind = "edit"
			}
			rowID := candidate.Key
			if kind == "edit" {
				rowID = candidate.Key + ":" + candidate.Digest
			}
			if enqueue(state, outboxRecord{
				ID: rowID, Kind: kind, SessionID: mapping.SessionID, CanonicalID: candidate.CanonicalID, TopicKey: candidate.Key,
				Text: candidate.Content.Text, PlainText: candidate.Content.Text, Entities: candidate.Content.Entities,
				Variant: variantFor(candidate.Content), MessageThreadID: mapping.MessageThreadID, MessageID: messageID,
				CreatedAt: now,
			}) {
				state.TopicProjection[candidate.Key] = candidate.Digest
			}
		}
		return nil
	})
}

type sessionOutputCandidate struct {
	Key         string
	CanonicalID string
	Content     richText
	Digest      string
}

// coalesceTopicRow keeps one unsent logical row per topic message checkpoint.
// An in-flight (dispatching) or indeterminate (ambiguous) row blocks later
// revisions because no local state can prove whether Telegram committed it.
func coalesceTopicRow(state *durableState, candidate sessionOutputCandidate, threadID int64, now time.Time) bool {
	for index := range state.Outbox {
		row := &state.Outbox[index]
		if row.TopicKey != candidate.Key {
			continue
		}
		switch row.State {
		case "failed":
			if !invalidTelegramLink(row.Entities) || candidate.Digest == state.TopicProjection[candidate.Key] {
				return true
			}
			// A definite API rejection with a now-corrected invalid URL is safe
			// to retry once under the same row. Unknown deliveries never enter here.
			row.State = "pending"
			fallthrough
		case "pending":
			row.Text = candidate.Content.Text
			row.PlainText = candidate.Content.Text
			row.Entities = candidate.Content.Entities
			row.Variant = variantFor(candidate.Content)
			row.MessageThreadID = threadID
			row.CreatedAt = now
			if row.Kind == "send" {
				row.ID = candidate.Key
			} else {
				row.ID = candidate.Key + ":" + candidate.Digest
			}
			state.TopicProjection[candidate.Key] = candidate.Digest
			return true
		case "dispatching", "ambiguous":
			return true
		}
	}
	return false
}

// projectSessionLifecycle emits turn/Work lifecycle markers on transition and
// moves the mapping between active and completed. Completed still admits new
// direct input while the Session is present: the reopen policy is exact input
// starting a new turn. Terminal Work/turn markers are checkpointed per turn
// and per Work status, so completion is marked once and never replayed.
func (m *Manager) projectSessionLifecycle(mapping topicMapping, projection brain.SessionProjection, now time.Time) error {
	markers := make([]string, 0, 3)
	if projection.TurnID != "" && projection.TurnStatus != "" {
		if marker, checkpoint := turnLifecycleMarker(projection); marker != "" {
			markers = append(markers, checkpoint+"\x00"+marker)
		}
	}
	if projection.WorkID != "" && projection.WorkStatus != "" {
		if marker, checkpoint := workLifecycleMarker(projection); marker != "" {
			markers = append(markers, checkpoint+"\x00"+marker)
		}
	}
	return m.store.mutate(func(state *durableState) error {
		for _, entry := range markers {
			parts := strings.SplitN(entry, "\x00", 2)
			checkpoint := "topic:mark:" + mapping.SessionID + ":" + parts[0]
			marker := parts[1]
			if state.TopicProjection[checkpoint] == digestText(marker) {
				continue
			}
			if enqueueTopicTextLocked(state, checkpoint, mapping.MessageThreadID, marker, m.now().UTC()) {
				state.TopicProjection[checkpoint] = digestText(marker)
			}
		}
		for index := range state.Topics {
			current := &state.Topics[index]
			if current.SessionID != mapping.SessionID || current.MessageThreadID != mapping.MessageThreadID {
				continue
			}
			next := current.State
			if sessionTurnActive(projection.TurnStatus) {
				next = topicStateActive
			} else if sessionTurnTerminal(projection.TurnStatus) || sessionWorkTerminal(projection.WorkStatus) {
				next = topicStateCompleted
			}
			if next != current.State {
				current.State = next
				current.UpdatedAt = now
			}
		}
		return nil
	})
}

func turnLifecycleMarker(projection brain.SessionProjection) (string, string) {
	checkpoint := projection.TurnID + ":" + projection.TurnStatus
	switch projection.TurnStatus {
	case string(turnStatusAccepted), string(turnStatusRunning), string(turnStatusBlocked), string(turnStatusAdmitted):
		return "Session is working.", checkpoint
	case string(turnStatusDone):
		return "Session completed. " + strings.TrimSpace(projection.TurnSummary), checkpoint
	case string(turnStatusFailed):
		return "Session failed. " + strings.TrimSpace(projection.TurnSummary), checkpoint
	default:
		return "", ""
	}
}

func workLifecycleMarker(projection brain.SessionProjection) (string, string) {
	checkpoint := projection.WorkID + ":" + projection.WorkStatus
	switch projection.WorkStatus {
	case string(workStatusDone):
		return "Work done: " + strings.TrimSpace(projection.WorkTitle), checkpoint
	case string(workStatusCancelled):
		return "Work cancelled: " + strings.TrimSpace(projection.WorkTitle), checkpoint
	default:
		return "", ""
	}
}

const (
	turnStatusAdmitted = "admitted"
	turnStatusAccepted = "accepted"
	turnStatusRunning  = "running"
	turnStatusBlocked  = "blocked"
	turnStatusDone     = "done"
	turnStatusFailed   = "failed"

	workStatusDone      = "done"
	workStatusCancelled = "cancelled"
)

func sessionTurnTerminal(status string) bool {
	return status == turnStatusDone || status == turnStatusFailed
}

func sessionWorkTerminal(status string) bool {
	return status == workStatusDone || status == workStatusCancelled
}

func sessionTurnActive(status string) bool {
	return status == turnStatusAccepted || status == turnStatusRunning || status == turnStatusBlocked || status == turnStatusAdmitted
}

func staleTopicText(label string) string {
	return "This Session" + sessionLabelSuffix(label) + " is no longer available. Open Brain or choose another Session with /sessions. This message was not forwarded."
}

func revivedTopicText(label string) string {
	return "This Session" + sessionLabelSuffix(label) + " is available again."
}

func sessionLabelSuffix(label string) string {
	if strings.TrimSpace(label) == "" {
		return ""
	}
	return " (" + label + ")"
}

func sessionStatusText(projection brain.SessionProjection) string {
	label := strings.TrimSpace(projection.Label)
	if label == "" {
		label = strings.TrimSpace(projection.SessionID)
	}
	parts := []string{"Session: " + label}
	if status := strings.TrimSpace(projection.Status); status != "" {
		parts = append(parts, "State: "+status)
	}
	if status := strings.TrimSpace(projection.TurnStatus); status != "" {
		parts = append(parts, "Turn: "+status)
	}
	if workID := strings.TrimSpace(projection.WorkID); workID != "" {
		workParts := "Work: " + strings.TrimSpace(projection.WorkStatus)
		if title := strings.TrimSpace(projection.WorkTitle); title != "" {
			workParts += " — " + title
		}
		parts = append(parts, workParts)
	}
	if summary := strings.TrimSpace(projection.TurnSummary); summary != "" {
		parts = append(parts, summary)
	}
	return strings.Join(parts, "\n")
}

// enqueueTopicText enqueues a topic-scoped plain text message split into safe
// chunks. The same row identity is shared across re-enqueues.
func (m *Manager) enqueueTopicText(id, text string, threadID, reply int64, buttons ...InlineKeyboardButton) {
	_ = m.store.mutate(func(state *durableState) error {
		enqueueTopicTextLocked(state, id, threadID, text, m.now().UTC())
		for i := range state.Outbox {
			if state.Outbox[i].ID == id+":0" {
				state.Outbox[i].ReplyMessageID = reply
				state.Outbox[i].ReplyMarkup = navigationKeyboard(*state, threadID)
				if len(buttons) > 0 {
					keyboard := state.Outbox[i].ReplyMarkup
					keyboard.InlineKeyboard = append([][]InlineKeyboardButton{buttons}, keyboard.InlineKeyboard...)
				}
				if mapping, ok := topicMappingByThread(*state, threadID); ok {
					state.Outbox[i].SessionID = mapping.SessionID
				}
			}
		}
		return nil
	})
}

func enqueueTopicTextLocked(state *durableState, id string, threadID int64, text string, now time.Time) bool {
	all := true
	for index, chunk := range chunkRichText(richText{Text: strings.TrimSpace(text)}, maxMessageText) {
		rowID := fmt.Sprintf("%s:%d", id, index)
		key := ""
		if strings.HasPrefix(id, "topic:mark:") || strings.HasPrefix(id, "topic:life:") {
			key = id
		}
		if !enqueue(state, outboxRecord{ID: rowID, Kind: "send", Text: chunk.Text,
			TopicKey: key, MessageThreadID: threadID, CreatedAt: now}) {
			all = false
		}
	}
	return all
}

// handleSessionTopicMessage routes one owner message in a mapped Session topic
// directly to that exact Session, or fails closed with a concise actionable
// reply in the same Topic. It never falls back to Brain or another Session.
func (m *Manager) handleSessionTopicMessage(ctx context.Context, token string, message Message, updateID int64) string {
	state := m.store.snapshot()
	command, _ := parseCommand(message.Text)
	if command == "/brain" {
		m.returnToBrain(fmt.Sprintf("command:%d", updateID), message.MessageThreadID, message.MessageID)
		return "command"
	}
	if command == "/sessions" {
		m.enqueueSessionList(updateID, message.MessageID, message.MessageThreadID)
		return "command"
	}
	if m.brain == nil {
		return "not_submitted"
	}
	mapping, found := topicMappingByThread(state, message.MessageThreadID)
	if !found {
		m.enqueueTopicText(fmt.Sprintf("topic-ack:%d:unknown", updateID), unknownTopicText, message.MessageThreadID, message.MessageID)
		return "topic_unknown"
	}
	if mapping.State == topicStateStale {
		projection, projectionErr := m.brain.SessionProjection(mapping.SessionID)
		if projectionErr != nil || !projection.Present {
			m.enqueueTopicText(fmt.Sprintf("topic-ack:%d:stale", updateID), staleTopicText(mapping.Label), message.MessageThreadID, message.MessageID)
			return "topic_stale"
		}
		// The exact Session identity is viable again: the reopen policy admits
		// direct input, and the next reconcile revives the mapping state.
	}
	projection, err := m.brain.SessionProjection(mapping.SessionID)
	if err != nil {
		m.enqueueTopicText(fmt.Sprintf("topic-ack:%d:unavailable", updateID), "Zen could not resolve this Session right now. Try again shortly.", message.MessageThreadID, message.MessageID)
		return "topic_unavailable"
	}
	if !projection.Present {
		m.enqueueTopicText(fmt.Sprintf("topic-ack:%d:stale", updateID), staleTopicText(mapping.Label), message.MessageThreadID, message.MessageID)
		return "topic_stale"
	}
	switch command {
	case "/help", "/start":
		m.enqueueTopicText(fmt.Sprintf("topic-command:%d", updateID), sessionHelpText, message.MessageThreadID, message.MessageID)
		return "command"
	case "/status":
		m.enqueueTopicText(fmt.Sprintf("topic-command:%d", updateID), sessionStatusText(projection), message.MessageThreadID, message.MessageID)
		return "command"
	case "/new":
		// /new is the Brain new-chat operation; it is route-local and never
		// executes from a Session topic.
		m.enqueueTopicText(fmt.Sprintf("topic-command:%d", updateID), sessionNewResponseText, message.MessageThreadID, message.MessageID)
		return "command"
	}
	if message.hasUnsupportedMedia() {
		m.enqueueTopicText(fmt.Sprintf("unsupported:%d", updateID), unsupportedMediaText, message.MessageThreadID, message.MessageID)
		return "unsupported_media"
	}
	body := strings.TrimSpace(message.Text)
	if body == "" {
		body = strings.TrimSpace(message.Caption)
	}
	if body == "" {
		return "ignored"
	}
	if reply := replyContext(message.ReplyToMessage); reply != "" {
		body = "Replying to: " + reply + "\n\n" + body
	}
	receipt := fmt.Sprintf("telegram:update:%d:%d", m.store.snapshot().BotID, updateID)
	result, err := m.brain.SubmitExternalSessionInput(mapping.SessionID, receipt, body)
	if err != nil && result == brain.ExternalInputPending {
		return "session_pending"
	}
	switch result {
	case brain.ExternalInputAccepted:
		m.startSessionTyping(ctx, token, mapping.SessionID, mapping.MessageThreadID)
		return "session_accepted"
	case brain.ExternalInputUncertain:
		m.enqueueTopicText(fmt.Sprintf("ack:%d", updateID), sessionUncertainText(mapping.Label), message.MessageThreadID, message.MessageID)
		return "session_uncertain"
	case brain.ExternalInputPending:
		return "session_pending"
	default:
		m.enqueueTopicText(fmt.Sprintf("ack:%d", updateID), sessionNotSubmittedText(mapping.Label), message.MessageThreadID, message.MessageID)
		return "session_not_submitted"
	}
}

const (
	unknownTopicText       = "This topic is not mapped to Zen. Open Brain or choose /sessions. This message was not forwarded."
	sessionHelpText        = "Session conversation. /status shows its state. /brain returns to Brain; /sessions opens the Session list."
	sessionNewResponseText = "New Chat belongs to Brain. Open /brain first; this Session was not changed."
	unsupportedMediaText   = "This connection supports text and captions only. Send the content as text."
)

func sessionUncertainText(label string) string {
	return "Zen could not prove whether " + sessionDisplayLabelText(label) + " received this message. It was not replayed."
}

func sessionNotSubmittedText(label string) string {
	return "Zen did not submit this message to " + sessionDisplayLabelText(label) + ". Send it again when the Session is available."
}

func sessionDisplayLabelText(label string) string {
	if strings.TrimSpace(label) == "" {
		return "the Session"
	}
	return "the Session (" + label + ")"
}

type sessionTypingIdentity struct {
	SessionID  string
	ThreadID   int64
	TurnID     string
	TurnStatus string
	Status     string
	Started    time.Time
}

// startSessionTyping begins native typing in a mapped Session topic after
// exact accepted direct input. It is bounded, cancellable, best-effort, and
// creates no durable row.
func (m *Manager) startSessionTyping(parent context.Context, token string, sessionID string, threadID int64) {
	if m.brain == nil {
		return
	}
	projection, err := m.brain.SessionProjection(sessionID)
	if err != nil || !projection.Present {
		return
	}
	state := m.store.snapshot()
	if !state.Enabled || state.OwnerID == 0 || state.ChatID == 0 {
		return
	}
	m.stopTyping()
	ctx, cancel := context.WithCancel(parent)
	m.typingMu.Lock()
	m.typingCancel = cancel
	m.typingMu.Unlock()
	go m.runSessionTyping(ctx, token, state.ChatID, sessionTypingIdentity{
		SessionID: sessionID, ThreadID: threadID, TurnID: projection.TurnID,
		TurnStatus: projection.TurnStatus, Status: projection.Status, Started: m.now(),
	})
}

func (m *Manager) runSessionTyping(ctx context.Context, token string, chatID int64, expected sessionTypingIdentity) {
	deadline := time.NewTimer(m.typingDeadline)
	defer deadline.Stop()
	for {
		if !m.sessionTypingActive(expected) {
			return
		}
		m.outboundMu.Lock()
		_ = m.api.SendChatAction(ctx, token, ChatActionRequest{ChatID: chatID, MessageThreadID: expected.ThreadID, Action: "typing"})
		m.outboundMu.Unlock()

		timer := time.NewTimer(m.typingInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-deadline.C:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (m *Manager) sessionTypingActive(expected sessionTypingIdentity) bool {
	state := m.store.snapshot()
	if !state.Enabled || state.OwnerID == 0 || state.ChatID == 0 {
		return false
	}
	projection, err := m.brain.SessionProjection(expected.SessionID)
	if err != nil || !projection.Present {
		return false
	}
	if projection.TurnStatus != "" {
		// Canonical-turn state is authoritative while it exists: terminal
		// states stop typing; accepted/running/blocked keep it active.
		switch projection.TurnStatus {
		case turnStatusAccepted, turnStatusRunning, turnStatusBlocked, turnStatusAdmitted:
			return true
		default:
			return false
		}
	}
	// Without a canonical turn, the accepted direct input is queued in the
	// provider: follow the classifier's working signals, with a bounded grace
	// window after acceptance while the provider starts up.
	switch projection.Status {
	case "running", "blocked":
		return true
	case "unknown":
		return m.now().Sub(expected.Started) < 2*m.typingInterval
	default:
		return false
	}
}

// topicOp delivery -----------------------------------------------------------

func enqueueTopicOp(state *durableState, op topicOpRecord) bool {
	for _, existing := range state.TopicOps {
		if existing.ID == op.ID {
			return true
		}
	}
	compactTopicOps(state)
	if len(state.TopicOps) >= maxTopicOps {
		return false
	}
	op.State = "pending"
	state.TopicOps = append(state.TopicOps, op)
	return true
}

func compactTopicOps(state *durableState) {
	if len(state.TopicOps) < maxTopicOps {
		return
	}
	compacted := state.TopicOps[:0]
	for _, op := range state.TopicOps {
		if op.State != "sent" && op.State != "failed" {
			compacted = append(compacted, op)
		}
	}
	state.TopicOps = compacted
}

func (m *Manager) hasDeliverableTopicOp() bool {
	state := m.store.snapshot()
	return m.deliverableTopicOpIndex(state) >= 0
}

func (m *Manager) deliverableTopicOpIndex(state durableState) int {
	if !state.Enabled || m.now().Before(state.RetryAt) {
		return -1
	}
	for index, op := range state.TopicOps {
		if op.State == "pending" && (op.AttemptAt.IsZero() || !m.now().Before(op.AttemptAt)) {
			return index
		}
	}
	return -1
}

func (m *Manager) deliverTopicOps(ctx context.Context, token string, limit int) error {
	for delivered := 0; delivered < limit; delivered++ {
		if !m.hasDeliverableTopicOp() {
			return nil
		}
		if err := m.deliverTopicOpOne(ctx, token); err != nil {
			return err
		}
		if !m.hasDeliverableTopicOp() {
			return nil
		}
		// Topic creation/rename is a per-chat message under Telegram's
		// one-message-per-second guidance; share the conservative schedule.
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

// deliverTopicOpOne performs one durable Topic operation. A definite create
// rejection returns to pending with capped backoff (safe: no Topic exists);
// rename/close/reopen/delete failures are terminal and opportunistic; any
// transport-indeterminate outcome becomes durable ambiguous and is never
// retried automatically.
func (m *Manager) deliverTopicOpOne(ctx context.Context, token string) error {
	m.outboundMu.Lock()
	defer m.outboundMu.Unlock()
	state := m.store.snapshot()
	if !state.Enabled || state.ChatID == 0 {
		return nil
	}
	currentToken, tokenErr := m.store.readToken()
	if tokenErr != nil || currentToken == "" {
		return fmt.Errorf("Telegram credential is unavailable")
	}
	token = currentToken
	index := m.deliverableTopicOpIndex(state)
	if index < 0 {
		return nil
	}
	op := state.TopicOps[index]
	if op.Kind == topicOpCreate && op.SessionID != "" && m.brain != nil {
		projection, err := m.brain.SessionProjection(op.SessionID)
		if err != nil {
			return err
		}
		if !projection.Present {
			return m.store.mutate(func(current *durableState) error {
				for i := range current.TopicOps {
					if current.TopicOps[i].ID == op.ID {
						current.TopicOps[i].State = "cancelled"
					}
				}
				return nil
			})
		}
	}
	if op.Kind == topicOpDelete || op.Kind == topicOpClose || op.Kind == topicOpReopen {
		return m.store.mutate(func(current *durableState) error {
			for i := range current.TopicOps {
				if current.TopicOps[i].ID == op.ID {
					current.TopicOps[i].State = "cancelled"
				}
			}
			return nil
		})
	}
	if err := m.store.mutate(func(current *durableState) error {
		for i := range current.TopicOps {
			if current.TopicOps[i].ID == op.ID && current.TopicOps[i].State == "pending" {
				current.TopicOps[i].State = "dispatching"
				return nil
			}
		}
		return fmt.Errorf("topic op unavailable")
	}); err != nil {
		return err
	}

	var err error
	switch op.Kind {
	case topicOpCreate:
		var topic ForumTopic
		topic, err = m.api.CreateForumTopic(ctx, token, CreateForumTopicRequest{ChatID: state.ChatID, Name: op.Label})
		if err == nil {
			err = m.store.mutate(func(current *durableState) error {
				return m.applyCreateTopicResult(current, op, topic, m.now().UTC())
			})
		}
	case topicOpRename:
		err = m.api.EditForumTopic(ctx, token, EditForumTopicRequest{ChatID: state.ChatID, MessageThreadID: op.MessageThreadID, Name: op.Label})
		if err == nil {
			err = m.store.mutate(func(current *durableState) error {
				for i := range current.TopicOps {
					if current.TopicOps[i].ID == op.ID {
						current.TopicOps[i].State = "sent"
						current.TopicOps[i].AttemptAt = time.Time{}
					}
				}
				for i := range current.Topics {
					if current.Topics[i].SessionID == op.SessionID && current.Topics[i].MessageThreadID == op.MessageThreadID {
						current.Topics[i].Label = op.Label
						current.Topics[i].UpdatedAt = m.now().UTC()
					}
				}
				return nil
			})
		}
	case topicOpClose:
		err = m.api.CloseForumTopic(ctx, token, ForumTopicIDRequest{ChatID: state.ChatID, MessageThreadID: op.MessageThreadID})
	case topicOpReopen:
		err = m.api.ReopenForumTopic(ctx, token, ForumTopicIDRequest{ChatID: state.ChatID, MessageThreadID: op.MessageThreadID})
	case topicOpDelete:
		err = m.api.DeleteForumTopic(ctx, token, ForumTopicIDRequest{ChatID: state.ChatID, MessageThreadID: op.MessageThreadID})
	default:
		err = fmt.Errorf("unknown topic operation")
	}
	if err == nil {
		if op.Kind != topicOpCreate {
			return m.store.mutate(func(current *durableState) error {
				if op.Kind == topicOpDelete {
					removeDeletedTopicState(current, op)
					return nil
				}
				for i := range current.TopicOps {
					if current.TopicOps[i].ID == op.ID {
						current.TopicOps[i].State = "sent"
					}
				}
				return nil
			})
		}
		return nil
	}
	var apiErr *APIError
	definite := errors.As(err, &apiErr)
	switch {
	case definite && !apiErr.Retryable && op.Kind == topicOpCreate:
		delay := retryDelay(err)
		if delay <= 0 {
			delay = m.topicCreateRetryDelay(op)
		}
		return m.store.mutate(func(current *durableState) error {
			for i := range current.TopicOps {
				if current.TopicOps[i].ID == op.ID {
					current.TopicOps[i].State = "pending"
					current.TopicOps[i].AttemptAt = m.now().Add(delay)
					current.TopicOps[i].Attempts++
				}
			}
			return nil
		})
	case definite && apiErr.Retryable:
		delay := retryDelay(err)
		if delay <= 0 {
			delay = m.backoff
		}
		return m.store.mutate(func(current *durableState) error {
			current.RetryAt = m.now().Add(delay)
			for i := range current.TopicOps {
				if current.TopicOps[i].ID == op.ID {
					current.TopicOps[i].State = "pending"
					current.TopicOps[i].AttemptAt = m.now().Add(delay)
				}
			}
			return nil
		})
	default:
		// A transport/decoding error may have created/renamed the Topic
		// remotely: no-replay ambiguous, never retried automatically.
		terminal := "ambiguous"
		if definite {
			terminal = "failed"
		}
		_ = m.store.mutate(func(current *durableState) error {
			for i := range current.TopicOps {
				if current.TopicOps[i].ID == op.ID {
					current.TopicOps[i].State = terminal
				}
			}
			return nil
		})
		return err
	}
}

func removeDeletedTopicState(state *durableState, op topicOpRecord) {
	topics := state.Topics[:0]
	for _, mapping := range state.Topics {
		if mapping.SessionID != op.SessionID || mapping.MessageThreadID != op.MessageThreadID {
			topics = append(topics, mapping)
		}
	}
	state.Topics = topics

	ops := state.TopicOps[:0]
	for _, candidate := range state.TopicOps {
		if candidate.SessionID != op.SessionID {
			ops = append(ops, candidate)
		}
	}
	state.TopicOps = ops

	outbox := state.Outbox[:0]
	for _, row := range state.Outbox {
		if row.MessageThreadID != op.MessageThreadID {
			outbox = append(outbox, row)
		}
	}
	state.Outbox = outbox

	messagePrefix := "topic:msg:" + op.SessionID + ":"
	markerPrefix := "topic:mark:" + op.SessionID + ":"
	for key := range state.TopicProjection {
		if strings.HasPrefix(key, messagePrefix) || strings.HasPrefix(key, markerPrefix) {
			delete(state.TopicProjection, key)
		}
	}
	for key := range state.TopicMessages {
		if strings.HasPrefix(key, messagePrefix) {
			delete(state.TopicMessages, key)
		}
	}
}

func (m *Manager) topicCreateRetryDelay(op topicOpRecord) time.Duration {
	delay := m.backoff
	for attempt := 0; attempt < op.Attempts && attempt < 12; attempt++ {
		delay *= 2
		if delay >= topicCreateBackoff {
			return topicCreateBackoff
		}
	}
	if delay > topicCreateBackoff {
		return topicCreateBackoff
	}
	return m.jitteredDelay(delay)
}

// applyCreateTopicResult atomically records the successful create: the op
// becomes sent and the durable mapping is materialized with the exact
// message_thread_id. Both happen in one mutation, so a mapped message always
// routes to the same Session or fails closed.
func (m *Manager) applyCreateTopicResult(state *durableState, op topicOpRecord, topic ForumTopic, now time.Time) error {
	if topic.MessageThreadID == 0 {
		return fmt.Errorf("Telegram topic create returned no thread id")
	}
	for i := range state.TopicOps {
		if state.TopicOps[i].ID == op.ID {
			state.TopicOps[i].State = "sent"
			state.TopicOps[i].AttemptAt = time.Time{}
		}
	}
	if op.ID == "topic:brain" {
		if state.BrainTopicID == 0 {
			state.BrainTopicID = topic.MessageThreadID
		}
		state.BrainTopics = append(state.BrainTopics, topic.MessageThreadID)
		enqueue(state, outboxRecord{ID: "topic:brain:welcome", Kind: "send", MessageThreadID: topic.MessageThreadID, Text: "Brain", ReplyMarkup: navigationKeyboard(*state, topic.MessageThreadID), CreatedAt: now})
		return nil
	}
	for index := range state.Topics {
		if state.Topics[index].SessionID == op.SessionID {
			// No duplicate mapping is ever created; a topic id mismatch is
			// ambiguous fail-closed data and is not overwritten.
			return nil
		}
	}
	state.Topics = append(state.Topics, topicMapping{
		SessionID: op.SessionID, ThreadID: op.ThreadID, WorkID: op.WorkID,
		ChatID: state.ChatID, MessageThreadID: topic.MessageThreadID, Label: op.Label,
		State: topicStateActive, CreatedAt: now, UpdatedAt: now,
	})
	return nil
}

func (m *Manager) jitteredDelay(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 0
	}
	// 75%-125% jitter to avoid synchronized retries without shortening an
	// explicit Telegram retry_after interval.
	percent := 75 + int(m.now().UnixNano()%51)
	return time.Duration(int64(delay) * int64(percent) / 100)
}
