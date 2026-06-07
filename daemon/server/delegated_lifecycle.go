package server

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/classifier"
)

const (
	defaultDelegatedDoneCloseAfter = 45 * time.Second
	defaultDelegatedIdleCloseAfter = 5 * time.Minute
)

type delegatedLifecycleManager struct {
	mu        sync.Mutex
	entries   map[string]delegatedLifecycleEntry
	now       func() time.Time
	wakeBrain func(brain.HeartbeatEvent) (bool, error)
	close     func(string) error

	doneCloseAfter time.Duration
	idleCloseAfter time.Duration
}

type delegatedLifecycleEntry struct {
	signature      string
	candidateSince time.Time
	woken          bool
}

type delegatedLifecycleCandidate struct {
	reason     string
	status     string
	summary    string
	signature  string
	closeAfter time.Duration
	wake       bool
}

func newDelegatedLifecycleManager(wakeBrain func(brain.HeartbeatEvent) (bool, error), close func(string) error) *delegatedLifecycleManager {
	return &delegatedLifecycleManager{
		entries:        map[string]delegatedLifecycleEntry{},
		now:            time.Now,
		wakeBrain:      wakeBrain,
		close:          close,
		doneCloseAfter: defaultDelegatedDoneCloseAfter,
		idleCloseAfter: defaultDelegatedIdleCloseAfter,
	}
}

func (m *delegatedLifecycleManager) Forget(agentID string) {
	if m == nil {
		return
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return
	}
	m.mu.Lock()
	delete(m.entries, agentID)
	m.mu.Unlock()
}

func (m *delegatedLifecycleManager) Observe(agent *classifier.Agent, alreadyWokeBrain bool) {
	if m == nil || agent == nil {
		return
	}
	candidate, ok := m.candidate(agent)
	if !ok {
		m.Forget(agent.ID)
		return
	}

	now := m.clock()
	var wakeEvent *brain.HeartbeatEvent
	var closeID string

	m.mu.Lock()
	entry, exists := m.entries[agent.ID]
	if !exists || entry.signature != candidate.signature {
		entry = delegatedLifecycleEntry{
			signature:      candidate.signature,
			candidateSince: now,
			woken:          alreadyWokeBrain,
		}
	}
	if alreadyWokeBrain {
		entry.woken = true
	}
	if candidate.wake && !entry.woken {
		event := brain.HeartbeatEvent{
			Reason:   candidate.reason,
			AgentID:  agent.ID,
			Name:     agent.Name,
			Status:   candidate.status,
			Summary:  candidate.summary,
			Cwd:      agent.Cwd,
			OldState: string(agent.State),
			NewState: string(agent.State),
		}
		wakeEvent = &event
	}
	if entry.woken && candidate.closeAfter > 0 && !entry.candidateSince.IsZero() && !now.Before(entry.candidateSince.Add(candidate.closeAfter)) {
		closeID = agent.ID
	}
	if closeID == "" {
		m.entries[agent.ID] = entry
	} else {
		delete(m.entries, agent.ID)
	}
	m.mu.Unlock()

	if wakeEvent != nil && m.wakeBrain != nil {
		woke, err := m.wakeBrain(*wakeEvent)
		if err != nil {
			log.Printf("delegated lifecycle wake failed for %s: %v", agent.ID, err)
		} else if woke {
			m.markWoken(agent.ID, candidate.signature, now)
			log.Printf("delegated lifecycle wake sent for %s (%s)", agent.ID, candidate.reason)
		}
	}
	if closeID != "" && m.close != nil {
		if err := m.close(closeID); err != nil {
			log.Printf("delegated lifecycle close failed for %s: %v", closeID, err)
		} else {
			log.Printf("delegated lifecycle closed %s", closeID)
		}
	}
}

func (m *delegatedLifecycleManager) markWoken(agentID, signature string, notifiedAt time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[agentID]
	if !ok || entry.signature != signature {
		return
	}
	entry.woken = true
	entry.candidateSince = notifiedAt
	m.entries[agentID] = entry
}

func (m *delegatedLifecycleManager) candidate(agent *classifier.Agent) (delegatedLifecycleCandidate, bool) {
	if agent == nil || !agent.Delegated || agent.Hidden || strings.TrimSpace(agent.ID) == "" {
		return delegatedLifecycleCandidate{}, false
	}
	switch agent.State {
	case classifier.StateDone:
		return delegatedLifecycleCandidate{
			reason:     "delegated_agent_done",
			status:     string(classifier.StateDone),
			summary:    fallbackLifecycleSummary(agent.Summary, agent.LastLines),
			signature:  lifecycleSignature(agent),
			closeAfter: m.durationOrDefault(m.doneCloseAfter, defaultDelegatedDoneCloseAfter),
			wake:       true,
		}, true
	case classifier.StateUnknown:
		if agent.StaleCount < 30 || !looksLikeCodexIdleAfterAssistantResponse(agent.LastLines) {
			return delegatedLifecycleCandidate{}, false
		}
		return delegatedLifecycleCandidate{
			reason:     "delegated_agent_idle_completion",
			status:     string(classifier.StateUnknown),
			summary:    fallbackLifecycleSummary(lastCodexAssistantLine(agent.LastLines), agent.LastLines),
			signature:  lifecycleSignature(agent),
			closeAfter: m.durationOrDefault(m.idleCloseAfter, defaultDelegatedIdleCloseAfter),
			wake:       true,
		}, true
	default:
		return delegatedLifecycleCandidate{}, false
	}
}

func (m *delegatedLifecycleManager) clock() time.Time {
	if m != nil && m.now != nil {
		return m.now()
	}
	return time.Now()
}

func (m *delegatedLifecycleManager) durationOrDefault(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

func looksLikeCodexIdleAfterAssistantResponse(lines []string) bool {
	meaningful := nonEmptyLifecycleLines(lines)
	if len(meaningful) < 2 {
		return false
	}
	lastPrompt := -1
	for i := len(meaningful) - 1; i >= 0; i-- {
		if isCodexInputPromptLine(meaningful[i]) {
			lastPrompt = i
			break
		}
	}
	if lastPrompt <= 0 {
		return false
	}
	for _, line := range meaningful[:lastPrompt] {
		if isCodexAssistantLine(line) {
			return true
		}
	}
	return false
}

func lifecycleSignature(agent *classifier.Agent) string {
	if agent == nil {
		return ""
	}
	lines := nonEmptyLifecycleLines(agent.LastLines)
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}
	return string(agent.State) + "\n" + strings.Join(lines, "\n")
}

func fallbackLifecycleSummary(primary string, lines []string) string {
	if summary := truncateLifecycleText(strings.TrimSpace(primary), 160); summary != "" {
		return summary
	}
	meaningful := nonEmptyLifecycleLines(lines)
	if len(meaningful) == 0 {
		return ""
	}
	return truncateLifecycleText(meaningful[len(meaningful)-1], 160)
}

func lastCodexAssistantLine(lines []string) string {
	meaningful := nonEmptyLifecycleLines(lines)
	for i := len(meaningful) - 1; i >= 0; i-- {
		line := strings.TrimSpace(meaningful[i])
		if isCodexAssistantLine(line) {
			line = strings.TrimSpace(strings.TrimPrefix(line, "•"))
			return truncateLifecycleText(line, 160)
		}
	}
	return ""
}

func nonEmptyLifecycleLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func isCodexInputPromptLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "›")
}

func isCodexAssistantLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "•")
}

func truncateLifecycleText(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maxLen {
		return value
	}
	if maxLen <= 3 {
		return value[:maxLen]
	}
	return value[:maxLen-3] + "..."
}
