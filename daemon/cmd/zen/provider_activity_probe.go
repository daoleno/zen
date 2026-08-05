package main

import (
	"strings"
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

// workProviderActivityProbe is a thin bridge to daemon/work's authoritative
// provider-native Activity parsers. Watcher calls it only after finding a
// nonterminal accepted delegated-turn marker.
type workProviderActivityProbe struct {
	mu      sync.Mutex
	readers map[string]*providerActivityReader
}

type providerActivityReader struct {
	mu     sync.Mutex
	reader *work.ProviderConversationReader
}

func newWorkProviderActivityProbe() *workProviderActivityProbe {
	return &workProviderActivityProbe{
		readers: make(map[string]*providerActivityReader),
	}
}

func (p *workProviderActivityProbe) ObserveProviderActivity(
	agent classifier.Agent,
	now time.Time,
) watcher.ProviderActivityObservation {
	provider := work.InferAgentProvider(agent.Command, agent.Name)
	if provider == "" {
		return watcher.ProviderActivityObservation{FallbackAllowed: true}
	}

	p.mu.Lock()
	reader := p.readers[agent.ID]
	if reader == nil {
		reader = &providerActivityReader{reader: work.NewProviderConversationReader()}
		p.readers[agent.ID] = reader
	}
	p.mu.Unlock()

	reader.mu.Lock()
	defer reader.mu.Unlock()
	conversation, err := reader.reader.Load(agent, provider, now)
	if err != nil {
		return watcher.ProviderActivityObservation{
			Structured:      true,
			FallbackAllowed: true,
		}
	}
	observation := watcher.ProviderActivityObservation{
		Structured:      true,
		FallbackAllowed: true,
	}
	if conversation.Activity != nil {
		observation.ID = strings.TrimSpace(conversation.Activity.ID)
		observation.Status = string(conversation.Activity.Status)
		observation.StartedAt = parseProviderActivityTime(conversation.Activity.StartedAt)
		observation.SettledAt = parseProviderActivityTime(conversation.Activity.SettledAt)
	}
	for index := len(conversation.Events) - 1; index >= 0; index-- {
		event := conversation.Events[index]
		if event.Kind != "user_message" || strings.TrimSpace(event.Body) == "" {
			continue
		}
		observation.AdmissionStream = strings.Join([]string{
			strings.TrimSpace(conversation.Source),
			strings.TrimSpace(conversation.SessionID),
			strings.TrimSpace(conversation.Path),
		}, "\x00")
		observation.AdmissionID = strings.TrimSpace(event.ID)
		if event.Seq > 0 {
			observation.AdmissionCursor = uint64(event.Seq)
		}
		if provider != work.AgentProviderCursor {
			observation.AdmissionAt = parseProviderActivityTime(event.Timestamp)
		}
		observation.InputSHA256 = strings.TrimSpace(event.AdmissionSHA256)
		break
	}
	return observation
}

func (p *workProviderActivityProbe) ForgetProviderActivity(agentID string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	delete(p.readers, strings.TrimSpace(agentID))
	p.mu.Unlock()
}

func parseProviderActivityTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	return parsed.UTC()
}
