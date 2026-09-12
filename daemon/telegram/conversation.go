package telegram

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
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

func topicURL(state durableState, threadID int64) string {
	if !state.TopicsAvailable || state.BotUsername == "" || threadID <= generalTopicThreadID {
		return ""
	}
	id := strconv.FormatInt(threadID, 10)
	link := url.URL{Scheme: "https", Host: "t.me", Path: "/" + state.BotUsername + "/" + id,
		RawQuery: url.Values{"thread": {id}}.Encode()}
	return link.String()
}

func navigationKeyboard(state durableState, threadID int64) *InlineKeyboardMarkup {
	button := InlineKeyboardButton{Text: "Brain", CallbackData: "brain"}
	if link := topicURL(state, state.BrainTopicID); link != "" {
		button.URL, button.CallbackData = link, ""
	}
	rows := [][]InlineKeyboardButton{{button, {Text: "Sessions", CallbackData: "sessions"}}}
	_, sessionTopic := topicMappingByThread(state, threadID)
	if !sessionTopic && (isGeneralThread(threadID) || slices.Contains(state.BrainTopics, threadID)) {
		rows = append(rows, []InlineKeyboardButton{{Text: "New Chat", CallbackData: "new"}})
	}
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (m *Manager) returnToBrain(id string, sourceThread, replyID int64) {
	if err := m.store.mutate(func(state *durableState) error {
		removePendingFallbackRows(state, state.FallbackSessionID)
		state.FallbackSessionID = ""
		state.FallbackStartedAt = time.Time{}
		state.BrainReplyTopicID = 0
		return nil
	}); err != nil {
		m.enqueueTopicText(id, "Brain could not be selected. Try again.", sourceThread, replyID)
		return
	}
	text := "Recipient: Brain."
	if m.store.snapshot().TopicsAvailable {
		text = "Brain"
	}
	m.enqueueTopicText(id, text, sourceThread, replyID, navigationKeyboard(m.store.snapshot(), sourceThread).InlineKeyboard...)
}
