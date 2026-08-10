package brain

import (
	"bufio"
	"crypto/sha256"
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

// AdmissionTimelineItemID returns the durable timeline identity for a successful
// Brain input receipt. The accepted request_id is the canonical user-row id.
func AdmissionTimelineItemID(receipt string) string {
	return strings.TrimSpace(receipt)
}

// AdmissionDigest returns the stable SHA-256 hex digest of exact admitted UTF-8.
func AdmissionDigest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return fmt.Sprintf("%x", sum)
}

// AdmitUserMessage durably records the exact user payload for a successful
// Brain input admission/receipt. Idempotent by request_id.
func (s *Store) AdmitUserMessage(threadID, sessionID, receipt, body string) (TimelineItem, error) {
	threadID = strings.TrimSpace(threadID)
	sessionID = strings.TrimSpace(sessionID)
	receipt = strings.TrimSpace(receipt)
	body = strings.TrimSpace(body)
	id := AdmissionTimelineItemID(receipt)
	if threadID == "" || sessionID == "" || id == "" || body == "" {
		return TimelineItem{}, fmt.Errorf("admit user message requires thread, session, receipt, and body")
	}
	return s.AppendTimelineItem(TimelineItem{
		ID:              id,
		ThreadID:        threadID,
		SessionID:       sessionID,
		Role:            "user",
		Body:            body,
		Kind:            timelineKindUserMessage,
		BrainAdmission:  true,
		AdmissionSHA256: AdmissionDigest(body),
	})
}

func timelineItemMatchesBrainInputAdmission(item TimelineItem, admission BrainInputAdmission) bool {
	return strings.TrimSpace(item.ID) == AdmissionTimelineItemID(admission.RequestID) &&
		strings.TrimSpace(item.ThreadID) == admission.ThreadID &&
		strings.TrimSpace(item.SessionID) == admission.SessionID &&
		strings.TrimSpace(item.Role) == "user" && strings.TrimSpace(item.Body) == admission.DisplayBody &&
		strings.TrimSpace(item.Kind) == timelineKindUserMessage && item.BrainAdmission &&
		strings.TrimSpace(item.AdmissionSHA256) == admission.BodySHA256
}

func brainInputAdmissionTimelineItem(admission BrainInputAdmission) TimelineItem {
	return TimelineItem{
		ID:              AdmissionTimelineItemID(admission.RequestID),
		ThreadID:        admission.ThreadID,
		SessionID:       admission.SessionID,
		Role:            "user",
		Body:            admission.DisplayBody,
		CreatedAt:       admission.AcceptedAt.UTC(),
		Kind:            timelineKindUserMessage,
		BrainAdmission:  true,
		AdmissionSHA256: admission.BodySHA256,
	}
}

func providerRowMatchesAdmissionWindow(item TimelineItem, admission BrainInputAdmission) bool {
	if item.BrainAdmission || strings.TrimSpace(item.Kind) != timelineKindUserMessage ||
		!strings.EqualFold(strings.TrimSpace(item.Role), "user") {
		return false
	}
	return providerEchoMatchesAdmission(
		admission,
		item.ThreadID,
		item.SessionID,
		item.Body,
		item.CreatedAt,
	)
}

func brainInputAdmissionProjectionState(
	allItems []TimelineItem,
	admission BrainInputAdmission,
) (int, []int, error) {
	canonicalIndex := -1
	canonicalID := AdmissionTimelineItemID(admission.RequestID)
	for index, existing := range allItems {
		if strings.TrimSpace(existing.ID) != canonicalID {
			continue
		}
		if !timelineItemMatchesBrainInputAdmission(existing, admission) {
			return -1, nil, fmt.Errorf(
				"timeline identity %q belongs to different Brain input admission",
				existing.ID,
			)
		}
		canonicalIndex = index
		break
	}

	claimedProviderIDs := make(map[string]bool, len(allItems))
	for _, existing := range allItems {
		if !IsBrainInputAdmission(existing) {
			continue
		}
		if echoID := strings.TrimSpace(existing.AdmissionEchoEventID); echoID != "" {
			claimedProviderIDs[echoID] = true
		}
	}
	candidates := make([]int, 0, 1)
	for index, existing := range allItems {
		if claimedProviderIDs[strings.TrimSpace(existing.ID)] {
			continue
		}
		if providerRowMatchesAdmissionWindow(existing, admission) {
			candidates = append(candidates, index)
		}
	}
	return canonicalIndex, candidates, nil
}

// ProjectBrainInputAdmission idempotently materializes the presentation row
// from accepted orchestration authority. Failure never rolls back acceptance
// or Attention; the existing bounded startup pass retries this projection.
func (s *Store) ProjectBrainInputAdmission(admission BrainInputAdmission) error {
	persisted, found, err := s.BrainInputAdmission(admission.RequestID, admission.ThreadID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("Brain input admission is not durable")
	}
	if !samePersistedBrainInputAdmission(persisted, admission) {
		return fmt.Errorf("Brain input admission projection identity differs from durable authority")
	}
	admission = persisted
	if admission.State != BrainInputAdmissionAccepted || admission.AcceptedAt == nil {
		return fmt.Errorf("only accepted Brain input admission can be projected")
	}
	if err := validateBrainInputAdmissions([]BrainInputAdmission{admission}); err != nil {
		return err
	}
	if s.projectBrainInputAdmission != nil {
		return s.projectBrainInputAdmission(admission)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureAdmissionProvenanceLocked(); err != nil {
		return formatAdmissionProvenanceError(err)
	}
	allItems, err := s.readAllTimelineItemsLocked()
	if err != nil {
		return err
	}
	item := brainInputAdmissionTimelineItem(admission)
	canonicalIndex, candidates, err := brainInputAdmissionProjectionState(allItems, admission)
	if err != nil {
		return err
	}
	if canonicalIndex >= 0 {
		item = allItems[canonicalIndex]
	}
	if canonicalIndex >= 0 && strings.TrimSpace(item.AdmissionEchoEventID) != "" {
		return nil
	}

	// A provider transcript snapshot can race between immutable admission
	// preparation and acceptance. Reconcile only the exact provider-native row
	// from that closed interval. This is deliberately narrower than ordinary
	// echo suppression: exact thread, provider Session, digest, and timestamp
	// are all required, and one admission may consume only one provider row.
	if len(candidates) > 1 {
		return fmt.Errorf(
			"Brain input admission %q has %d provider echoes in its acceptance window",
			admission.RequestID,
			len(candidates),
		)
	}
	if len(candidates) == 0 {
		if canonicalIndex >= 0 {
			return nil
		}
		_, err := s.appendTimelineItemLocked(item)
		return err
	}

	candidateIndex := candidates[0]
	item.AdmissionEchoEventID = strings.TrimSpace(allItems[candidateIndex].ID)
	reconciled := make([]TimelineItem, 0, len(allItems))
	for index, existing := range allItems {
		switch index {
		case candidateIndex:
			continue
		case canonicalIndex:
			reconciled = append(reconciled, item)
		default:
			reconciled = append(reconciled, existing)
		}
	}
	if canonicalIndex < 0 {
		reconciled = append(reconciled, item)
	}
	return s.rewriteTimelineLocked(reconciled)
}

// TimelineItem is one durable Brain-thread timeline row. The append-only
// messages.jsonl ledger is the sole presentation truth for Brain chat history
// and Work cards. Work Events remain the scheduler audit log and are never
// reprojected into the UI on every snapshot.
//
// Cards are immutable messages: every newly materialized presentable event
// remains chronologically visible. There is no current-per-Work collapse.
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

	// BrainAdmission marks rows written by AdmitUserMessage (Interface
	// send_input). Provider materialization never sets this. Digests alone are
	// not provenance.
	BrainAdmission bool `json:"brain_admission,omitempty"`

	// AdmissionSHA256 is correlation data for BrainAdmission rows only: the
	// exact UTF-8 digest used to match one provider echo. It is never message
	// identity and must not appear on provider-native durable rows.
	AdmissionSHA256 string `json:"admission_sha256,omitempty"`

	// AdmissionEchoEventID records the provider event id that consumed this
	// admission's single echo credit. Empty means unmatched.
	AdmissionEchoEventID string `json:"admission_echo_event_id,omitempty"`

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
	item.AdmissionSHA256 = strings.TrimSpace(item.AdmissionSHA256)
	item.AdmissionEchoEventID = strings.TrimSpace(item.AdmissionEchoEventID)
	item.WorkID = strings.TrimSpace(item.WorkID)
	item.EventKind = strings.TrimSpace(item.EventKind)
	item.Summary = strings.TrimSpace(item.Summary)
	item.SessionName = strings.TrimSpace(item.SessionName)
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
	if err := s.ensureAdmissionProvenanceLocked(); err != nil {
		return nil, formatAdmissionProvenanceError(err)
	}
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
// events stay live-only until they finalize. Provider user echoes reconcile
// one-to-one against Brain input admissions and never create rows.
func (s *Store) MaterializeProviderConversation(threadID string, conversation work.CodexConversation) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureAdmissionProvenanceLocked(); err != nil {
		return formatAdmissionProvenanceError(err)
	}
	allItems, err := s.readAllTimelineItemsLocked()
	if err != nil {
		return err
	}
	database, err := s.loadOrchestrationLocked()
	if err != nil {
		return err
	}
	items := make([]TimelineItem, 0, len(allItems))
	for _, item := range allItems {
		if item.ThreadID != threadID {
			continue
		}
		normalizeLegacyTimelineKind(&item)
		items = append(items, item)
	}
	sortTimelineItems(items)
	sessionID := strings.TrimSpace(conversation.SessionID)
	if sessionID == "" {
		sessionID = "provider"
	}
	// Assign echo credits before appending so restart/replay stays idempotent.
	suppress, updatedItems, dirty := claimProviderUserEchoes(
		items,
		conversation.Events,
		database.BrainInputAdmissions,
		threadID,
		sessionID,
	)
	if dirty {
		byID := make(map[string]TimelineItem, len(updatedItems))
		for _, item := range updatedItems {
			byID[item.ID] = item
		}
		for index := range allItems {
			if next, ok := byID[allItems[index].ID]; ok {
				allItems[index] = next
			}
		}
		if err := s.rewriteTimelineLocked(allItems); err != nil {
			return err
		}
		items = updatedItems
	}
	known := make(map[string]bool, len(items))
	for _, item := range items {
		known[item.ID] = true
	}
	for _, event := range conversation.Events {
		if !providerEventMaterializable(event) {
			continue
		}
		id := strings.TrimSpace(event.ID)
		if id == "" || known[id] {
			continue
		}
		if strings.TrimSpace(event.Kind) == timelineKindUserMessage && suppress[id] {
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
	case timelineKindUserMessage:
		if work.IsCanonicalDirectWorkEventInput(event.Body) {
			return false
		}
		_, hasExactTimestamp := exactProviderEventTimestamp(event)
		return strings.TrimSpace(event.Body) != "" && hasExactTimestamp
	case timelineKindAssistantMessage:
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
		// Provider-native rows never receive Brain admission provenance or
		// correlation digests. Echo matching uses live provider event digests
		// against BrainAdmission rows only.
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
// item exactly once by event ID into the Work's frozen SourceThreadID.
func (s *Store) MaterializeWorkCard(workItem Work, event WorkEvent) (TimelineItem, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.materializeWorkCardLocked(workItem, event)
}

func (s *Store) materializeWorkCardLocked(workItem Work, event WorkEvent) (TimelineItem, bool, error) {
	threadID := strings.TrimSpace(workItem.SourceThreadID)
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
	})
	if err != nil {
		return TimelineItem{}, false, err
	}
	return item, true, nil
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

func timelineItemsToConversationEvents(items []TimelineItem) []work.CodexConversationEvent {
	out := make([]work.CodexConversationEvent, 0, len(items))
	for index, item := range items {
		event, ok := timelineItemToConversationEvent(item, index+1)
		if !ok {
			continue
		}
		out = append(out, event)
	}
	return out
}

// TimelineItemsToConversationEvents projects durable timeline rows into the
// shared conversation wire contract. Every materialized presentable card stays
// chronologically visible.
func TimelineItemsToConversationEvents(items []TimelineItem) []work.CodexConversationEvent {
	return timelineItemsToConversationEvents(items)
}

func timelineItemToConversationEvent(item TimelineItem, seq int) (work.CodexConversationEvent, bool) {
	switch item.Kind {
	case timelineKindUserMessage:
		if work.IsCanonicalDirectWorkEventInput(item.Body) {
			// Transport-only Session Input must never become a visible user row.
			// Presentable Work outcomes own the work_card / work_result path.
			return work.CodexConversationEvent{}, false
		}
		event := work.CodexConversationEvent{
			ID:        item.ID,
			Seq:       seq,
			Timestamp: item.CreatedAt.Format(time.RFC3339Nano),
			Kind:      timelineKindUserMessage,
			Role:      "user",
			Body:      item.Body,
			Source:    "brain_timeline",
		}
		if IsBrainInputAdmission(item) {
			event.AdmissionSHA256 = admissionCorrelationDigest(item)
		}
		return event, true
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
