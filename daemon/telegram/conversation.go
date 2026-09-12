package telegram

import (
	"fmt"
	"slices"
	"time"
)

// Reconnect starts future delivery, not a replay of the disabled interval.
// Receipts, sent messages and ambiguous outcomes are never discarded here.
func resetDeliveryBoundary(state *durableState, now time.Time) {
	state.DeliveryStartedAt = now
	rows := state.Outbox[:0]
	for _, row := range state.Outbox {
		if row.State == "pending" && (row.CanonicalID != "" || row.TopicKey != "") {
			continue
		}
		rows = append(rows, row)
	}
	state.Outbox = rows
}

func (m *Manager) ensureBrainTopic() error {
	state := m.store.snapshot()
	if !state.Enabled || !state.TopicsAvailable || state.OwnerID == 0 || state.BrainTopicID != 0 {
		return nil
	}
	return m.store.mutate(func(current *durableState) error {
		if !enqueueTopicOp(current, topicOpRecord{ID: "topic:brain", Kind: topicOpCreate, Label: "Brain", CreatedAt: m.now().UTC()}) {
			return fmt.Errorf("Telegram topic operation queue is full")
		}
		return nil
	})
}

// Telegram clients with user topic creation enabled turn a General send into
// a native topic. That is a UI destination, never a canonical Brain NewChat.
func (m *Manager) bindBrainTopic(threadID int64) error {
	return m.store.mutate(func(state *durableState) error {
		if _, found := topicMappingByThread(*state, threadID); found {
			return fmt.Errorf("topic belongs to a Session")
		}
		if !slices.Contains(state.BrainTopics, threadID) {
			if len(state.BrainTopics) >= maxTopicMappings {
				return fmt.Errorf("Brain topic routing limit reached")
			}
			state.BrainTopics = append(state.BrainTopics, threadID)
		}
		state.BrainReplyTopicID = threadID
		return nil
	})
}

func brainDestination(state durableState) int64 {
	if state.TopicsAvailable {
		if state.BrainReplyTopicID != 0 {
			return state.BrainReplyTopicID
		}
		return state.BrainTopicID
	}
	return 0
}

func navigationKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "Brain", CallbackData: "brain"}, {Text: "Sessions", CallbackData: "sessions"}},
		{{Text: "New Chat", CallbackData: "new"}},
	}}
}
