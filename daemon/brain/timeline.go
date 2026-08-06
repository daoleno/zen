package brain

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/work"
)

const (
	defaultThreadTimelineLimit = 240

	timelineKindUserMessage      = "user_message"
	timelineKindAssistantMessage = "assistant_message"
	timelineKindWorkCard         = "work_card"
	timelineKindCalendarResult   = "calendar_result"

	workResultConversationSource = "work_result"
)

// TimelineItem is one durable Brain-thread timeline row. The append-only
// messages.jsonl ledger is the sole presentation truth for Brain chat history
// and Work cards. Work Events remain the scheduler audit log and are never
// reprojected into the UI on every snapshot.
type TimelineItem struct {
	ID         string    `json:"id"`
	ThreadID   string    `json:"thread_id,omitempty"`
	SessionID  string    `json:"session_id"`
	ExecutorID string    `json:"executor_id,omitempty"`
	Role       string    `json:"role"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
	Kind       string    `json:"kind,omitempty"`
	Status     string    `json:"status,omitempty"`
	Title      string    `json:"title,omitempty"`

	// Legacy calendar rows retained for orphan-message migration compatibility.
	CalendarItemID string     `json:"calendar_item_id,omitempty"`
	CalendarRunID  string     `json:"calendar_run_id,omitempty"`
	ScheduledFor   *time.Time `json:"scheduled_for,omitempty"`

	// Work-card presentation fields. Identity is ID == Work Event ID.
	WorkID      string `json:"work_id,omitempty"`
	EventKind   string `json:"event_kind,omitempty"`
	Summary     string `json:"summary,omitempty"`
	SessionName string `json:"session_name,omitempty"`
	Unread      bool   `json:"unread,omitempty"`
	// Supersedes names the prior current work_card ID for the same Work.
	// Presentation collapses superseded cards; audit facts stay in Work Events.
	Supersedes string `json:"supersedes,omitempty"`
}

func (s *Store) messagesPath() string {
	return filepath.Join(s.statePath(), "messages.jsonl")
}

func (s *Store) AppendTimelineItem(item TimelineItem) (TimelineItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appendTimelineItemLocked(item)
}

func (s *Store) appendTimelineItemLocked(item TimelineItem) (TimelineItem, error) {
	item.ID = strings.TrimSpace(item.ID)
	if item.ID == "" {
		item.ID = fmt.Sprintf("timeline_%d", time.Now().UTC().UnixNano())
	}
	item.ThreadID = strings.TrimSpace(item.ThreadID)
	item.SessionID = strings.TrimSpace(item.SessionID)
	item.ExecutorID = strings.TrimSpace(item.ExecutorID)
	item.Role = strings.TrimSpace(item.Role)
	item.Body = strings.TrimSpace(item.Body)
	item.Kind = strings.TrimSpace(item.Kind)
	item.Status = strings.TrimSpace(item.Status)
	item.Title = strings.TrimSpace(item.Title)
	item.WorkID = strings.TrimSpace(item.WorkID)
	item.EventKind = strings.TrimSpace(item.EventKind)
	item.Summary = strings.TrimSpace(item.Summary)
	item.SessionName = strings.TrimSpace(item.SessionName)
	item.Supersedes = strings.TrimSpace(item.Supersedes)
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	} else {
		item.CreatedAt = item.CreatedAt.UTC()
	}
	if item.ThreadID == "" {
		return TimelineItem{}, fmt.Errorf("timeline thread id required")
	}
	if item.SessionID == "" {
		return TimelineItem{}, fmt.Errorf("timeline session id required")
	}
	if item.Role == "" {
		return TimelineItem{}, fmt.Errorf("timeline role required")
	}
	if item.Kind == timelineKindWorkCard {
		if item.Body == "" {
			item.Body = firstNonEmpty(item.Summary, item.Title, item.EventKind, "Work update")
		}
		if item.EventKind == "" {
			return TimelineItem{}, fmt.Errorf("work card event kind required")
		}
		if item.WorkID == "" {
			return TimelineItem{}, fmt.Errorf("work card work id required")
		}
	} else if item.Body == "" && item.Kind != timelineKindCalendarResult {
		return TimelineItem{}, fmt.Errorf("timeline body required")
	}
	if existing, ok, err := s.timelineItemByIDLocked(item.ID); err != nil {
		return TimelineItem{}, err
	} else if ok {
		return existing, nil
	}
	raw, err := json.Marshal(item)
	if err != nil {
		return TimelineItem{}, err
	}
	if err := os.MkdirAll(filepath.Dir(s.messagesPath()), 0o700); err != nil {
		return TimelineItem{}, err
	}
	file, err := os.OpenFile(s.messagesPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return TimelineItem{}, err
	}
	defer file.Close()
	if _, err := file.Write(append(raw, '\n')); err != nil {
		return TimelineItem{}, err
	}
	if err := file.Sync(); err != nil {
		return TimelineItem{}, err
	}
	return item, nil
}

func (s *Store) timelineItemByIDLocked(id string) (TimelineItem, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return TimelineItem{}, false, nil
	}
	file, err := os.Open(s.messagesPath())
	if errors.Is(err, os.ErrNotExist) {
		return TimelineItem{}, false, nil
	}
	if err != nil {
		return TimelineItem{}, false, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var item TimelineItem
		if json.Unmarshal(scanner.Bytes(), &item) == nil && strings.TrimSpace(item.ID) == id {
			return item, true, nil
		}
	}
	return TimelineItem{}, false, scanner.Err()
}

func (s *Store) ThreadTimeline(threadID string, limit int) ([]TimelineItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.threadTimelineLocked(threadID, limit)
}

func (s *Store) threadTimelineLocked(threadID string, limit int) ([]TimelineItem, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return []TimelineItem{}, nil
	}
	if limit <= 0 {
		limit = defaultThreadTimelineLimit
	}
	file, err := os.Open(s.messagesPath())
	if errors.Is(err, os.ErrNotExist) {
		return []TimelineItem{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	out := []TimelineItem{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item TimelineItem
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			continue
		}
		item.ID = strings.TrimSpace(item.ID)
		item.ThreadID = strings.TrimSpace(item.ThreadID)
		if item.ID == "" || item.ThreadID != threadID {
			continue
		}
		normalizeLegacyTimelineKind(&item)
		out = append(out, item)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sortTimelineItems(out)
	if len(out) > limit {
		out = append([]TimelineItem(nil), out[len(out)-limit:]...)
	}
	return out, nil
}

func normalizeLegacyTimelineKind(item *TimelineItem) {
	if item == nil {
		return
	}
	kind := strings.TrimSpace(item.Kind)
	switch kind {
	case timelineKindUserMessage, timelineKindAssistantMessage, timelineKindWorkCard, timelineKindCalendarResult:
		item.Kind = kind
		return
	case "input":
		item.Kind = timelineKindUserMessage
		if item.Role == "" {
			item.Role = "user"
		}
	case "chat", "memory", "":
		if strings.EqualFold(item.Role, "user") {
			item.Kind = timelineKindUserMessage
		} else {
			item.Kind = timelineKindAssistantMessage
			if item.Role == "" {
				item.Role = "assistant"
			}
		}
	default:
		if kind == "calendar_result" {
			item.Kind = timelineKindCalendarResult
		}
	}
}

func sortTimelineItems(items []TimelineItem) {
	sort.SliceStable(items, func(left, right int) bool {
		if !items[left].CreatedAt.Equal(items[right].CreatedAt) {
			return items[left].CreatedAt.Before(items[right].CreatedAt)
		}
		return items[left].ID < items[right].ID
	})
}

// MaterializeProviderConversation appends durable provider transcript events
// into the Brain thread timeline exactly once by event ID. Partial/transient
// events stay live-only until they finalize.
func (s *Store) MaterializeProviderConversation(threadID string, conversation work.CodexConversation) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	known, err := s.timelineIDsLocked(threadID)
	if err != nil {
		return err
	}
	sessionID := strings.TrimSpace(conversation.SessionID)
	if sessionID == "" {
		sessionID = "provider"
	}
	for _, event := range conversation.Events {
		if !providerEventMaterializable(event) {
			continue
		}
		id := strings.TrimSpace(event.ID)
		if id == "" || known[id] {
			continue
		}
		item, ok := timelineItemFromProviderEvent(threadID, sessionID, event)
		if !ok {
			continue
		}
		if _, err := s.appendTimelineItemLocked(item); err != nil {
			return err
		}
		known[id] = true
	}
	return nil
}

func providerEventMaterializable(event work.CodexConversationEvent) bool {
	if event.Partial || event.Transient {
		return false
	}
	switch strings.TrimSpace(event.Kind) {
	case timelineKindUserMessage, timelineKindAssistantMessage:
		return strings.TrimSpace(event.Body) != ""
	default:
		return false
	}
}

func timelineItemFromProviderEvent(threadID, sessionID string, event work.CodexConversationEvent) (TimelineItem, bool) {
	id := strings.TrimSpace(event.ID)
	body := strings.TrimSpace(event.Body)
	if id == "" || body == "" {
		return TimelineItem{}, false
	}
	role := strings.TrimSpace(event.Role)
	kind := strings.TrimSpace(event.Kind)
	switch kind {
	case timelineKindUserMessage:
		if role == "" {
			role = "user"
		}
	case timelineKindAssistantMessage:
		if role == "" {
			role = "assistant"
		}
	default:
		return TimelineItem{}, false
	}
	createdAt := time.Now().UTC()
	if ts := strings.TrimSpace(event.Timestamp); ts != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			createdAt = parsed.UTC()
		} else if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
			createdAt = parsed.UTC()
		}
	}
	return TimelineItem{
		ID:        id,
		ThreadID:  threadID,
		SessionID: sessionID,
		Role:      role,
		Body:      body,
		CreatedAt: createdAt,
		Kind:      kind,
		Title:     strings.TrimSpace(event.Title),
		Status:    strings.TrimSpace(event.Status),
	}, true
}

func (s *Store) timelineIDsLocked(threadID string) (map[string]bool, error) {
	items, err := s.threadTimelineLocked(threadID, 0)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(items))
	for _, item := range items {
		out[item.ID] = true
	}
	return out, nil
}

// MaterializeWorkCard records a presentable Work Event as a typed timeline
// item exactly once. A newer card for the same Work supersedes the prior
// current card without deleting Work Event audit facts.
func (s *Store) MaterializeWorkCard(threadID string, workItem Work, event WorkEvent) (TimelineItem, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.materializeWorkCardLocked(threadID, workItem, event)
}

func (s *Store) materializeWorkCardLocked(threadID string, workItem Work, event WorkEvent) (TimelineItem, bool, error) {
	threadID = strings.TrimSpace(threadID)
	event.ID = strings.TrimSpace(event.ID)
	if threadID == "" || event.ID == "" || !isProjectedWorkResultEvent(event.Kind) {
		return TimelineItem{}, false, nil
	}
	if existing, ok, err := s.timelineItemByIDLocked(event.ID); err != nil {
		return TimelineItem{}, false, err
	} else if ok {
		return existing, false, nil
	}

	sessionID := strings.TrimSpace(workItem.OwnerSessionID)
	if payloadSessionID := strings.TrimPrefix(event.PayloadRef, "session:"); payloadSessionID != event.PayloadRef {
		sessionID = strings.TrimSpace(payloadSessionID)
	}
	if sessionID == "" {
		sessionID = "work"
	}
	summary := strings.TrimSpace(event.Summary)
	if summary == "" {
		summary = strings.TrimSpace(workItem.Objective)
		if nextAction := strings.TrimSpace(workItem.NextAction); nextAction != "" {
			summary = nextAction
		}
	}
	summary = compactWorkResultText(summary)
	title := strings.TrimSpace(workItem.Title)
	body := firstNonEmpty(summary, title, event.Kind)

	supersedes := ""
	items, err := s.threadTimelineLocked(threadID, 0)
	if err != nil {
		return TimelineItem{}, false, err
	}
	superseded := supersededWorkCardIDs(items)
	var latest *TimelineItem
	for index := range items {
		item := &items[index]
		if item.Kind != timelineKindWorkCard || item.WorkID != workItem.ID {
			continue
		}
		if superseded[item.ID] {
			continue
		}
		if latest == nil || item.CreatedAt.After(latest.CreatedAt) ||
			(item.CreatedAt.Equal(latest.CreatedAt) && item.ID > latest.ID) {
			latest = item
		}
	}
	if latest != nil && latest.ID != event.ID {
		supersedes = latest.ID
	}

	item, err := s.appendTimelineItemLocked(TimelineItem{
		ID:          event.ID,
		ThreadID:    threadID,
		SessionID:   sessionID,
		Role:        "assistant",
		Body:        body,
		CreatedAt:   event.CreatedAt.UTC(),
		Kind:        timelineKindWorkCard,
		Status:      event.Kind,
		Title:       title,
		WorkID:      workItem.ID,
		EventKind:   event.Kind,
		Summary:     summary,
		SessionName: strings.TrimSpace(event.SourceName),
		Unread:      event.ReadAt == nil,
		Supersedes:  supersedes,
	})
	if err != nil {
		return TimelineItem{}, false, err
	}
	return item, true, nil
}

func supersededWorkCardIDs(items []TimelineItem) map[string]bool {
	out := make(map[string]bool)
	for _, item := range items {
		if item.Kind != timelineKindWorkCard {
			continue
		}
		if target := strings.TrimSpace(item.Supersedes); target != "" {
			out[target] = true
		}
	}
	return out
}

// MarkTimelineWorkCardsRead clears unread emphasis on materialized work cards
// for one Work. Work Event ReadAt remains the scheduler ack owner.
func (s *Store) MarkTimelineWorkCardsRead(workID string) error {
	workID = strings.TrimSpace(workID)
	if workID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.readAllTimelineItemsLocked()
	if err != nil {
		return err
	}
	changed := false
	for index := range items {
		item := &items[index]
		if item.Kind != timelineKindWorkCard || item.WorkID != workID || !item.Unread {
			continue
		}
		item.Unread = false
		changed = true
	}
	if !changed {
		return nil
	}
	return s.rewriteTimelineLocked(items)
}

func (s *Store) readAllTimelineItemsLocked() ([]TimelineItem, error) {
	file, err := os.Open(s.messagesPath())
	if errors.Is(err, os.ErrNotExist) {
		return []TimelineItem{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	out := []TimelineItem{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item TimelineItem
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			continue
		}
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		normalizeLegacyTimelineKind(&item)
		out = append(out, item)
	}
	return out, scanner.Err()
}

func (s *Store) rewriteTimelineLocked(items []TimelineItem) error {
	if err := os.MkdirAll(filepath.Dir(s.messagesPath()), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.messagesPath()), "messages-*.jsonl")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()
	for _, item := range items {
		raw, err := json.Marshal(item)
		if err != nil {
			_ = tmp.Close()
			return err
		}
		if _, err := tmp.Write(append(raw, '\n')); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.messagesPath()); err != nil {
		return err
	}
	return nil
}

// BackfillCurrentWorkCardsOnce materializes only the current presentable
// outcome per Work into the current thread. It never floods historical
// superseded Events from the global audit log.
func (s *Store) BackfillCurrentWorkCardsOnce(threadID string) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	markerPath := filepath.Join(s.statePath(), "timeline_work_card_backfill_v1")
	if _, err := os.Stat(markerPath); err == nil {
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return err
	}
	projected := projectCurrentWorkResultEvents(database, recentWorkResultEventLimit)
	workByID := make(map[string]Work, len(database.BrainWork))
	for _, item := range database.BrainWork {
		workByID[item.ID] = item
	}
	eventByID := make(map[string]WorkEvent, len(database.BrainWorkEvents))
	for _, event := range database.BrainWorkEvents {
		eventByID[event.ID] = event
	}
	for _, card := range projected {
		workItem, ok := workByID[card.WorkID]
		if !ok {
			continue
		}
		event, ok := eventByID[card.EventID]
		if !ok {
			continue
		}
		event.Summary = card.Summary
		event.SourceName = card.SessionName
		event.ReadAt = nil
		if !card.Unread {
			now := s.nowUTC()
			event.ReadAt = &now
		}
		if _, _, err := s.materializeWorkCardLocked(threadID, workItem, event); err != nil {
			return err
		}
	}
	return os.WriteFile(markerPath, []byte(time.Now().UTC().Format(time.RFC3339Nano)+"\n"), 0o600)
}

func timelineItemsToConversationEvents(items []TimelineItem, includeSuperseded bool) []work.CodexConversationEvent {
	superseded := supersededWorkCardIDs(items)
	out := make([]work.CodexConversationEvent, 0, len(items))
	for index, item := range items {
		if item.Kind == timelineKindWorkCard && !includeSuperseded && superseded[item.ID] {
			continue
		}
		event, ok := timelineItemToConversationEvent(item, index+1)
		if !ok {
			continue
		}
		out = append(out, event)
	}
	return out
}

// TimelineItemsToConversationEvents projects durable timeline rows into the
// shared conversation wire contract.
func TimelineItemsToConversationEvents(items []TimelineItem, includeSuperseded bool) []work.CodexConversationEvent {
	return timelineItemsToConversationEvents(items, includeSuperseded)
}

func timelineItemToConversationEvent(item TimelineItem, seq int) (work.CodexConversationEvent, bool) {
	switch item.Kind {
	case timelineKindUserMessage:
		return work.CodexConversationEvent{
			ID:        item.ID,
			Seq:       seq,
			Timestamp: item.CreatedAt.Format(time.RFC3339Nano),
			Kind:      timelineKindUserMessage,
			Role:      "user",
			Body:      item.Body,
			Source:    "brain_timeline",
		}, true
	case timelineKindAssistantMessage:
		return work.CodexConversationEvent{
			ID:        item.ID,
			Seq:       seq,
			Timestamp: item.CreatedAt.Format(time.RFC3339Nano),
			Kind:      timelineKindAssistantMessage,
			Role:      "assistant",
			Body:      item.Body,
			Title:     item.Title,
			Source:    "brain_timeline",
		}, true
	case timelineKindWorkCard:
		return work.CodexConversationEvent{
			ID:          item.ID,
			Seq:         seq,
			Timestamp:   item.CreatedAt.Format(time.RFC3339Nano),
			Kind:        "status",
			Role:        "assistant",
			Title:       item.Title,
			Body:        firstNonEmpty(item.Summary, item.Body),
			Status:      firstNonEmpty(item.EventKind, item.Status),
			Source:      workResultConversationSource,
			WorkID:      item.WorkID,
			WorkSession: item.SessionID,
			SessionName: item.SessionName,
			Unread:      item.Unread,
			Supersedes:  item.Supersedes,
		}, true
	case timelineKindCalendarResult:
		return work.CodexConversationEvent{
			ID:        item.ID,
			Seq:       seq,
			Timestamp: item.CreatedAt.Format(time.RFC3339Nano),
			Kind:      "status",
			Role:      "assistant",
			Title:     item.Title,
			Body:      item.Body,
			Status:    item.Status,
			Source:    "calendar_result",
		}, true
	default:
		return work.CodexConversationEvent{}, false
	}
}
