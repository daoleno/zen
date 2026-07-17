package calendar

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const maxScheduledFailureRunes = 240

// ScheduledResult is a read-only presentation of one canonical terminal Run.
// It is rebuilt from Calendar state and is never persisted independently.
type ScheduledResult struct {
	ID             string    `json:"id"`
	ThreadID       string    `json:"thread_id"`
	Body           string    `json:"body"`
	CreatedAt      time.Time `json:"created_at"`
	Status         Status    `json:"status"`
	Title          string    `json:"title"`
	CalendarItemID string    `json:"calendar_item_id"`
	CalendarRunID  string    `json:"calendar_run_id"`
	ScheduledFor   time.Time `json:"scheduled_for"`
}

// ScheduledResults returns canonical terminal Run projections in ascending
// finished-time/identity order. A positive limit selects the newest results.
func (s *Store) ScheduledResults(threadID string, limit int) []ScheduledResult {
	if s == nil {
		return []ScheduledResult{}
	}
	targetThreadID := strings.TrimSpace(threadID)
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := []ScheduledResult{}
	for _, item := range s.items {
		if item.Kind != KindScheduledAction {
			continue
		}
		for _, run := range item.Runs {
			result, ok := scheduledResultFromRun(item.ID, run)
			if !ok || targetThreadID != "" && result.ThreadID != targetThreadID {
				continue
			}
			results = append(results, result)
		}
	}
	sort.SliceStable(results, func(left, right int) bool {
		if !results[left].CreatedAt.Equal(results[right].CreatedAt) {
			return results[left].CreatedAt.Before(results[right].CreatedAt)
		}
		return results[left].ID < results[right].ID
	})
	if limit > 0 && len(results) > limit {
		results = results[len(results)-limit:]
	}
	return results
}

func scheduledResultFromRun(itemID string, run Run) (ScheduledResult, bool) {
	title := strings.TrimSpace(run.Title)
	threadID := strings.TrimSpace(run.SourceThreadID)
	if strings.TrimSpace(itemID) == "" || strings.TrimSpace(run.ID) == "" ||
		title == "" || threadID == "" || run.ScheduledFor.IsZero() ||
		run.FinishedAt == nil || run.FinishedAt.IsZero() {
		return ScheduledResult{}, false
	}
	switch run.Status {
	case StatusCompleted:
		result, err := normalizeScheduledResult(run.Result)
		if err != nil || result != run.Result || run.FailureReason != "" {
			return ScheduledResult{}, false
		}
	case StatusFailed:
		failure, err := normalizeScheduledFailure(run.FailureReason)
		if err != nil || failure != run.FailureReason || run.Result != "" {
			return ScheduledResult{}, false
		}
	default:
		return ScheduledResult{}, false
	}
	return ScheduledResult{
		ID:             "calendar_result:" + strings.TrimSpace(itemID) + ":" + strings.TrimSpace(run.ID),
		ThreadID:       threadID,
		Body:           scheduledResultBody(run),
		CreatedAt:      run.FinishedAt.UTC(),
		Status:         run.Status,
		Title:          title,
		CalendarItemID: strings.TrimSpace(itemID),
		CalendarRunID:  strings.TrimSpace(run.ID),
		ScheduledFor:   run.ScheduledFor,
	}, true
}

func scheduledResultBody(run Run) string {
	title := strings.TrimSpace(run.Title)
	if run.Status == StatusFailed {
		return fmt.Sprintf("**%s failed**\n\n%s", title, run.FailureReason)
	}
	return fmt.Sprintf("**%s completed**\n\n%s", title, run.Result)
}

func compactFailure(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	runes := []rune(value)
	if len(runes) > maxScheduledFailureRunes {
		return string(runes[:maxScheduledFailureRunes-3]) + "..."
	}
	return value
}

func normalizeScheduledFailure(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("failure must be valid UTF-8")
	}
	value = compactFailure(value)
	if value == "" {
		return "", fmt.Errorf("failure must be nonempty")
	}
	return value, nil
}
