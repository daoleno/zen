package telegram

import (
	"fmt"
	"slices"
	"strings"
)

type messageSource struct {
	SessionID       string `json:"session_id,omitempty"`
	BrainThreadID   string `json:"brain_thread_id,omitempty"`
	MessageThreadID int64  `json:"message_thread_id,omitempty"`
}

type feedbackRecord struct {
	Source    messageSource  `json:"source"`
	Reactions []ReactionType `json:"reactions"`
	UpdateID  int64          `json:"update_id"`
	Native    bool           `json:"native"`
}

func recordMessageSource(state *durableState, id int64, source messageSource) {
	if id <= 0 || (source.SessionID == "" && source.BrainThreadID == "") {
		return
	}
	state.MessageSources[id] = source
	for len(state.MessageSources) > maxOutboxRows {
		var oldest int64
		for id := range state.MessageSources {
			if oldest == 0 || id < oldest {
				oldest = id
			}
		}
		delete(state.MessageSources, oldest)
		delete(state.Feedback, oldest)
	}
}

func (m *Manager) recordFeedback(id, updateID int64, reactions []ReactionType, native bool) (string, error) {
	state := m.store.snapshot()
	source, found := state.MessageSources[id]
	if !found {
		return "reaction_unavailable", nil
	}
	if previous, found := state.Feedback[id]; found && previous.UpdateID >= updateID {
		return "reaction_duplicate", nil
	}
	if len(reactions) > 3 {
		return "reaction_rejected", nil
	}
	for _, reaction := range reactions {
		if (reaction.Type != "emoji" && reaction.Type != "custom_emoji") || len(reaction.Emoji) > 128 || len(reaction.CustomEmojiID) > 128 {
			return "reaction_rejected", nil
		}
	}
	err := m.store.mutate(func(current *durableState) error {
		if current.OwnerID != state.OwnerID || current.BotID != state.BotID || current.ChatID != state.ChatID || current.MessageSources[id] != source {
			return fmt.Errorf("feedback message binding changed")
		}
		current.Feedback[id] = feedbackRecord{Source: source, Reactions: reactions, UpdateID: updateID, Native: native}
		return nil
	})
	return "reaction_recorded", err
}

// Private Bot API reaction delivery is not documented: Update requires chat
// administrator status. Defensive support here grants no provider authority
// and does not replace unavailable native reactions with inline buttons.
func (m *Manager) handleReaction(reaction MessageReactionUpdated, updateID int64) (string, error) {
	state := m.store.snapshot()
	if !state.Enabled || state.OwnerID == 0 || reaction.User == nil || reaction.User.IsBot || reaction.ActorChat != nil || reaction.Chat.Type != "private" || reaction.Chat.ID != state.ChatID || reaction.User.ID != state.OwnerID {
		return "reaction_rejected", nil
	}
	return m.recordFeedback(reaction.MessageID, updateID, reaction.NewReaction, true)
}

// Old pending rows can contain the former automatic keyboard. Match only that
// complete shape on known ordinary output; preserve explicit and unknown controls.
func stripOrdinaryReplyMarkup(state durableState, row *outboxRecord) {
	if row.ReplyMarkup == nil || (row.Kind != "send" && row.Kind != "edit") {
		return
	}
	ordinary := row.CanonicalID != "" || row.TopicKey != "" || row.ID == "binding:connected" || row.ID == "topic:brain:welcome"
	for _, prefix := range []string{"ack:", "entities:", "fallback:", "media-ack:", "media-error:", "media-late:", "topic-ack:", "topic-command:", "command:", "callback:"} {
		ordinary = ordinary || strings.HasPrefix(row.ID, prefix)
	}
	if !ordinary {
		return
	}
	if row.CanonicalID == "" && (strings.HasPrefix(row.ID, "command:") || strings.HasPrefix(row.ID, "callback:")) {
		// Legacy command rows have no intent field. These are the exact Brain
		// entry and chooser bodies; populated choosers also carry Session buttons.
		if row.Text == "Brain" || row.Text == "Recipient: Brain." || row.Text == sessionListText(nil, state.TopicsAvailable) || strings.HasPrefix(row.Text, "Delegated Sessions:\n") {
			return
		}
	}
	rows := row.ReplyMarkup.InlineKeyboard
	if len(rows) == 0 {
		return
	}
	nav := navigationKeyboard(state, row.MessageThreadID).InlineKeyboard[0]
	callbackNav := []InlineKeyboardButton{{Text: "Brain", CallbackData: "brain"}, {Text: "Sessions", CallbackData: "sessions"}}
	if !slices.Equal(rows[0], nav) && !slices.Equal(rows[0], callbackNav) {
		return
	}
	rows = rows[1:]
	if len(rows) > 0 && slices.Equal(rows[0], []InlineKeyboardButton{{Text: "New Chat", CallbackData: "new"}}) {
		rows = rows[1:]
	}
	if len(rows) > 0 && slices.Equal(rows[0], []InlineKeyboardButton{
		{Text: "\U0001f44d", CallbackData: "feedback:up"},
		{Text: "\U0001f44e", CallbackData: "feedback:down"},
		{Text: "Clear feedback", CallbackData: "feedback:clear"},
	}) {
		rows = rows[1:]
	}
	if len(rows) == 0 {
		row.ReplyMarkup = nil
	}
}

func (m *Manager) ensureBrainEntry() error {
	state := m.store.snapshot()
	if !state.Enabled || state.OwnerID == 0 || state.BrainEntryState != "" || (state.TopicsAvailable && state.BrainTopicID == 0) {
		return nil
	}
	return m.store.mutate(func(current *durableState) error {
		if current.BrainEntryState != "" {
			return nil
		}
		if !enqueue(current, outboxRecord{ID: "brain-entry:v1", Kind: "send", Text: "Brain", MessageThreadID: current.BrainTopicID,
			ReplyMarkup: navigationKeyboard(*current, current.BrainTopicID), CreatedAt: m.now().UTC()}) {
			return fmt.Errorf("Brain entry queue is full")
		}
		current.BrainEntryState = "queued"
		return nil
	})
}

func resetRichInteractionBinding(state *durableState) {
	state.MediaInputs = map[string]mediaInput{}
	state.MessageSources = map[int64]messageSource{}
	state.Feedback = map[int64]feedbackRecord{}
	state.BrainEntryState, state.BrainEntryMessageID = "", 0
}

func isFeedback(value string) bool {
	return slices.Contains([]string{"feedback:up", "feedback:down", "feedback:clear"}, value)
}
