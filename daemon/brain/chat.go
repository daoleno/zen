package brain

import (
	"fmt"
	"strings"
	"time"
)

const defaultChatMessageLimit = 120

func (s *Service) ChatThreadID() (string, error) {
	if s == nil || s.store == nil {
		return "", nil
	}
	return s.store.ChatThreadID()
}

func (s *Service) ChatMessages(threadID string) ([]ChatMessage, error) {
	if s == nil || s.store == nil {
		return []ChatMessage{}, nil
	}
	if strings.TrimSpace(threadID) == "" {
		threadID, _ = s.store.ChatThreadID()
	}
	messages, err := s.store.ChatMessages(threadID, defaultChatMessageLimit)
	if err != nil {
		return nil, err
	}
	return cleanBrainChatMessages(messages), nil
}

func (s *Service) NewChat() (Snapshot, error) {
	if s == nil || s.store == nil || s.watcher == nil {
		return Snapshot{}, fmt.Errorf("brain service is not configured")
	}
	adapter := s.hostAdapter()
	if hostSession, err := s.store.HostSession(); err == nil {
		if id := strings.TrimSpace(hostSession.ID); id != "" && s.watcher.HasSession(id) {
			if err := s.watcher.KillSession(id); err != nil {
				return Snapshot{}, err
			}
		}
	} else {
		return Snapshot{}, err
	}
	if err := s.store.SetHostSession("", adapter.ID); err != nil {
		return Snapshot{}, err
	}
	host, err := s.ensureHostAgent(adapter)
	if err != nil {
		return Snapshot{}, err
	}
	threadID := newChatThreadID()
	if err := s.store.SetChatState(ChatState{
		ThreadID:       threadID,
		SessionIDs:     []string{host.ID},
		LastTranscript: "",
		UpdatedAt:      s.nowUTC(),
	}); err != nil {
		return Snapshot{}, err
	}
	return s.Snapshot()
}

func (s *Service) RecordUserMessage(threadID, hostSessionID, body, transcriptBefore string) ([]ChatMessage, error) {
	if s == nil || s.store == nil {
		return []ChatMessage{}, fmt.Errorf("brain store is not configured")
	}
	hostSessionID = strings.TrimSpace(hostSessionID)
	body = strings.TrimSpace(body)
	if hostSessionID == "" {
		return []ChatMessage{}, fmt.Errorf("brain session id required")
	}
	if strings.TrimSpace(threadID) == "" {
		threadID, _ = s.store.ChatThreadID()
	}
	if body == "" {
		return s.ChatMessages(threadID)
	}
	now := s.nowUTC()
	state, err := s.store.ChatState(threadID)
	if err != nil {
		return []ChatMessage{}, err
	}
	state.ThreadID = firstNonEmpty(state.ThreadID, threadID)
	state.LastTranscript = trimTranscriptForChat(transcriptBefore)
	state.UpdatedAt = now
	if appendUniqueString(&state.SessionIDs, hostSessionID) {
		state.UpdatedAt = now
	}
	if err := s.store.SetChatState(state); err != nil {
		return []ChatMessage{}, err
	}
	if _, err := s.store.AppendChatMessage(ChatMessage{
		ID:        chatMessageID("user", now),
		ThreadID:  state.ThreadID,
		SessionID: hostSessionID,
		AdapterID: s.hostAdapter().ID,
		Role:      "user",
		Body:      body,
		CreatedAt: now,
	}); err != nil {
		return []ChatMessage{}, err
	}
	return s.ChatMessages(state.ThreadID)
}

func (s *Service) SyncTerminalTranscript(threadID, hostSessionID, transcript string) ([]ChatMessage, error) {
	if s == nil || s.store == nil {
		return []ChatMessage{}, nil
	}
	hostSessionID = strings.TrimSpace(hostSessionID)
	if hostSessionID == "" {
		return []ChatMessage{}, nil
	}
	if strings.TrimSpace(threadID) == "" {
		threadID, _ = s.store.ChatThreadID()
	}
	current := trimTranscriptForChat(transcript)
	state, err := s.store.ChatState(threadID)
	if err != nil {
		return nil, err
	}
	state.ThreadID = firstNonEmpty(state.ThreadID, threadID)
	changed := appendUniqueString(&state.SessionIDs, hostSessionID)
	if strings.TrimSpace(state.LastTranscript) == "" {
		state.LastTranscript = current
		state.UpdatedAt = s.nowUTC()
		if err := s.store.SetChatState(state); err != nil {
			return nil, err
		}
		return s.ChatMessages(state.ThreadID)
	}

	delta := transcriptDeltaForChat(state.LastTranscript, current)
	if delta != "" {
		delta = stripRecentUserEcho(delta, s.lastUserMessageBody(state.ThreadID))
	}
	if delta != "" {
		delta = cleanBrainAssistantTranscript(delta)
	}
	if delta != "" {
		now := s.nowUTC()
		if _, err := s.store.AppendChatMessage(ChatMessage{
			ID:        chatMessageID("assistant", now),
			ThreadID:  state.ThreadID,
			SessionID: hostSessionID,
			AdapterID: s.hostAdapter().ID,
			Role:      "assistant",
			Body:      delta,
			CreatedAt: now,
		}); err != nil {
			return nil, err
		}
	}
	if current != strings.TrimSpace(state.LastTranscript) || changed {
		state.LastTranscript = current
		state.UpdatedAt = s.nowUTC()
		if err := s.store.SetChatState(state); err != nil {
			return nil, err
		}
	}
	return s.ChatMessages(state.ThreadID)
}

func (s *Service) lastUserMessageBody(threadID string) string {
	messages, err := s.store.ChatMessages(threadID, defaultChatMessageLimit)
	if err != nil {
		return ""
	}
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "user" {
			return messages[index].Body
		}
	}
	return ""
}

func (s *Service) nowUTC() time.Time {
	if s != nil && s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

func chatMessageID(role string, t time.Time) string {
	return fmt.Sprintf("%s_%d", role, t.UnixNano())
}

func trimTranscriptForChat(value string) string {
	lines := strings.Split(strings.ReplaceAll(value, "\r", ""), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

func transcriptDeltaForChat(before, after string) string {
	cleanBefore := trimTranscriptForChat(before)
	cleanAfter := trimTranscriptForChat(after)
	if cleanAfter == "" {
		return ""
	}
	if cleanBefore == "" {
		return cleanAfter
	}
	if strings.HasPrefix(cleanAfter, cleanBefore) {
		return trimTranscriptForChat(strings.TrimPrefix(cleanAfter, cleanBefore))
	}
	if anchor := strings.Join(lastMeaningfulTranscriptLines(cleanBefore, 4), "\n"); anchor != "" {
		if index := strings.LastIndex(cleanAfter, anchor); index >= 0 {
			return trimTranscriptForChat(cleanAfter[index+len(anchor):])
		}
	}
	if prefix := commonTranscriptPrefix(cleanBefore, cleanAfter); prefix > 120 {
		return trimTranscriptForChat(cleanAfter[prefix:])
	}
	return cleanAfter
}

func stripRecentUserEcho(value, sentText string) string {
	body := trimTranscriptForChat(value)
	sent := trimTranscriptForChat(sentText)
	if body == "" || sent == "" {
		return body
	}
	lines := strings.Split(body, "\n")
	removed := false
	out := make([]string, 0, len(lines))
	for index, line := range lines {
		if !removed && index <= 6 {
			normalized := strings.TrimSpace(line)
			if normalized == sent || strings.HasSuffix(normalized, sent) {
				removed = true
				continue
			}
		}
		out = append(out, line)
	}
	return trimTranscriptForChat(strings.Join(out, "\n"))
}

func cleanBrainChatMessages(messages []ChatMessage) []ChatMessage {
	out := make([]ChatMessage, 0, len(messages))
	for _, message := range messages {
		if message.Role == "assistant" {
			message.Body = cleanBrainAssistantTranscript(message.Body)
			if strings.TrimSpace(message.Body) == "" {
				continue
			}
		}
		out = append(out, message)
	}
	return out
}

func cleanBrainAssistantTranscript(value string) string {
	value = stripANSI(value)
	lines := strings.Split(trimTranscriptForChat(value), "\n")
	out := make([]string, 0, len(lines))
	skipNextInterrupt := false
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
				out = append(out, "")
			}
			continue
		}
		normalized := strings.ToLower(strings.TrimSpace(strings.Trim(trimmed, "─━═-_—>│┃┆┊┌┐└┘╭╮╰╯┬┴┼╞╡╪ ")))
		if skipNextInterrupt && normalized == "interrupt" {
			skipNextInterrupt = false
			continue
		}
		skipNextInterrupt = false
		if isBrainTerminalChromeLine(trimmed) {
			if strings.Contains(normalized, "esc to") {
				skipNextInterrupt = true
			}
			continue
		}
		out = append(out, line)
	}
	return trimTranscriptForChat(strings.Join(compactBlankLines(out), "\n"))
}

func isBrainTerminalChromeLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return true
	}
	normalized := strings.ToLower(strings.TrimSpace(strings.Trim(trimmed, "─━═-_—>│┃┆┊┌┐└┘╭╮╰╯┬┴┼╞╡╪ ")))
	if normalized == "" {
		return true
	}
	switch normalized {
	case "interrupt", "esc to interrupt", "? for shortcuts", "? for help":
		return true
	}
	if strings.Contains(normalized, "esc to") || strings.Contains(normalized, "? for") {
		return true
	}
	if strings.Contains(normalized, "transfiguring") || strings.Contains(normalized, "cogitated for") {
		return true
	}
	if strings.Contains(normalized, "thinking") && strings.Contains(normalized, "…") {
		return true
	}
	if isMostlyBoxDrawing(trimmed) {
		return true
	}
	return false
}

func isMostlyBoxDrawing(value string) bool {
	total := 0
	chrome := 0
	for _, r := range value {
		if r == ' ' || r == '\t' {
			continue
		}
		total++
		switch r {
		case '─', '━', '═', '-', '_', '—', '>', '<', '│', '┃', '┆', '┊', '┌', '┐', '└', '┘', '╭', '╮', '╰', '╯', '┬', '┴', '┼', '╞', '╡', '╪':
			chrome++
		}
	}
	return total > 0 && chrome*100/total >= 80
}

func stripANSI(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == 0x1b && i+1 < len(value) && value[i+1] == '[' {
			i += 2
			for i < len(value) {
				b := value[i]
				if b >= 0x40 && b <= 0x7e {
					break
				}
				i++
			}
			continue
		}
		out.WriteByte(value[i])
	}
	return out.String()
}

func compactBlankLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if blank || len(out) == 0 {
				continue
			}
			blank = true
			out = append(out, "")
			continue
		}
		blank = false
		out = append(out, line)
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return out
}

func lastMeaningfulTranscriptLines(value string, count int) []string {
	lines := []string{}
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if count > 0 && len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return lines
}

func commonTranscriptPrefix(left, right string) int {
	max := len(left)
	if len(right) < max {
		max = len(right)
	}
	index := 0
	for index < max && left[index] == right[index] {
		index++
	}
	return index
}
