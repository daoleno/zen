package telegram

import (
	"fmt"
	"slices"
	"strings"

	"github.com/daoleno/zen/daemon/attachment"
)

// mediaRecipient captures the same exact destination for an attachment and
// for a prompt waiting on it. Topic identity outranks fallback selection.
func (m *Manager) mediaRecipient(state durableState, message Message, updateID int64) (mediaInput, error) {
	input := mediaInput{ID: fmt.Sprintf("update:%d", updateID), Receipt: fmt.Sprintf("telegram:update:%d:%d", state.BotID, updateID),
		MessageThreadID: message.MessageThreadID, OwnerID: state.OwnerID, ChatID: state.ChatID, BotID: state.BotID,
		State: "collecting", CreatedAt: m.now().UTC(), Reply: replyContext(message.ReplyToMessage)}
	if mapping, ok := topicMappingByThread(state, message.MessageThreadID); ok {
		input.SessionID = mapping.SessionID
	} else if !isGeneralThread(message.MessageThreadID) && !slices.Contains(state.BrainTopics, message.MessageThreadID) {
		return input, fmt.Errorf("This topic is not mapped to Zen.")
	} else if message.ReplyToMessage != nil && message.ReplyToMessage.From != nil && message.ReplyToMessage.From.ID == state.BotID && state.ReplySessions[message.ReplyToMessage.MessageID] != "" {
		input.SessionID = state.ReplySessions[message.ReplyToMessage.MessageID]
	} else if !state.TopicsAvailable {
		input.SessionID = state.FallbackSessionID
	}
	if input.SessionID == "" {
		var err error
		input.BrainThreadID, err = m.brain.ChatThreadID()
		if err != nil || input.BrainThreadID == "" {
			return input, fmt.Errorf("Brain is unavailable.")
		}
	}
	return input, nil
}

func sameMediaRecipient(a, b mediaInput) bool {
	return a.BotID == b.BotID && a.OwnerID == b.OwnerID && a.ChatID == b.ChatID &&
		a.SessionID == b.SessionID && a.BrainThreadID == b.BrainThreadID
}

func readyMediaFollowup(state durableState) *mediaInput {
	var next *mediaInput
	for _, input := range state.MediaInputs {
		if !input.TextOnly || mediaTerminal(input.State) {
			continue
		}
		ready := true
		for _, id := range input.DependsOn {
			dependency, found := state.MediaInputs[id]
			if !found || (mediaTerminal(dependency.State) && dependency.State != "accepted") {
				ready = true
				break
			}
			if !mediaTerminal(dependency.State) {
				ready = false
			}
		}
		if (ready || input.State == "admitting") && (next == nil || input.Parts[0].UpdateID < next.Parts[0].UpdateID) {
			copy := input
			next = &copy
		}
	}
	return next
}

// Only text that follows unadmitted input to this recipient enters the media
// journal. Controls and other recipients keep the ordinary immediate path.
func (m *Manager) deferTextAfterMedia(message Message, updateID int64) (string, error) {
	command, _ := parseCommand(message.Text)
	if slices.Contains([]string{"/help", "/start", "/status", "/new", "/brain", "/sessions", "/use", "/session"}, command) || m.brain == nil {
		return "", nil
	}
	body := strings.TrimSpace(message.Text)
	if body == "" {
		body = strings.TrimSpace(message.Caption)
	}
	if body == "" {
		return "", nil
	}
	state := m.store.snapshot()
	input, err := m.mediaRecipient(state, message, updateID)
	if err != nil {
		// The ordinary route owns unavailable-topic feedback.
		return "", nil
	}
	if existing, found := state.MediaInputs[input.ID]; found && existing.TextOnly {
		return "deferred_queued", nil
	}
	for id, earlier := range state.MediaInputs {
		if !mediaTerminal(earlier.State) && sameMediaRecipient(earlier, input) && earlier.Parts[0].UpdateID < updateID {
			input.DependsOn = append(input.DependsOn, id)
		}
	}
	if len(input.DependsOn) == 0 {
		return "", nil
	}
	slices.Sort(input.DependsOn)
	if len(message.Entities) > 0 {
		caption := attachment.Caption{Text: message.Text, Entities: message.Entities}
		if err := validateCaption(caption); err != nil {
			m.enqueueTopicText(fmt.Sprintf("entities:%d", updateID), err.Error(), message.MessageThreadID, message.MessageID)
			return "invalid_entities", nil
		}
		body = attachment.Input(body, nil, []attachment.Caption{caption})
	}
	if input.Reply != "" {
		body = "Replying to: " + input.Reply + "\n\n" + body
	}
	input.TextOnly, input.Body = true, body
	input.Parts = []mediaPart{{UpdateID: updateID, MessageID: message.MessageID}}
	err = m.store.mutate(func(current *durableState) error {
		if !current.Enabled || current.BotID != input.BotID || current.OwnerID != input.OwnerID || current.ChatID != input.ChatID {
			return fmt.Errorf("Telegram binding changed before staging follow-up")
		}
		if len(current.MediaInputs) >= maxMediaInputs {
			if !enqueue(current, outboxRecord{ID: "deferred-capacity:" + input.ID, Kind: "send", Text: "The attachment input queue is full. This follow-up was not submitted; resend the file and prompt together later.", MessageThreadID: input.MessageThreadID, ReplyMessageID: message.MessageID, CreatedAt: m.now().UTC()}) {
				return fmt.Errorf("follow-up feedback queue is full")
			}
			return nil
		}
		current.MediaInputs[input.ID] = input
		return nil
	})
	if err != nil {
		return "", err
	}
	if _, found := m.store.snapshot().MediaInputs[input.ID]; !found {
		return "deferred_not_submitted", nil
	}
	return "deferred_queued", nil
}
