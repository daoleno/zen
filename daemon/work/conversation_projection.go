package work

import "strings"

// SanitizeConversationProjection is the final user-facing API boundary. It
// drops typed provider Goal context and cleans every displayable text field,
// even when a provider adds a new structured envelope around legacy context.
func SanitizeConversationProjection(conversation CodexConversation) CodexConversation {
	events := make([]CodexConversationEvent, 0, len(conversation.Events))
	for _, event := range conversation.Events {
		if isGoalInternalContextEvent(event) {
			continue
		}
		event.Title = CleanCodexDisplayText(event.Title)
		event.Body = CleanCodexDisplayText(event.Body)
		event.Command = CleanCodexDisplayText(event.Command)
		event.Input = CleanCodexDisplayText(event.Input)
		event.Output = CleanCodexDisplayText(event.Output)
		event.Explanation = CleanCodexDisplayText(event.Explanation)
		for index := range event.Plan {
			event.Plan[index].Step = CleanCodexDisplayText(event.Plan[index].Step)
		}
		if event.Kind == "user_message" || event.Kind == "assistant_message" {
			if event.Body == "" {
				continue
			}
		}
		events = append(events, event)
	}
	conversation.Events = events
	return conversation
}

func isGoalInternalContextEvent(event CodexConversationEvent) bool {
	source := strings.ToLower(strings.TrimSpace(event.Source))
	title := strings.ToLower(strings.TrimSpace(event.Title))
	toolName := strings.ToLower(strings.TrimSpace(event.ToolName))
	return source == "goal" ||
		source == "codex_internal_context" ||
		source == "codex_internal_context:goal" ||
		title == "codex_internal_context" ||
		toolName == "codex_internal_context"
}
