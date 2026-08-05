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
	readers map[string]*work.ProviderConversationReader
}

func newWorkProviderActivityProbe() *workProviderActivityProbe {
	return &workProviderActivityProbe{
		readers: make(map[string]*work.ProviderConversationReader),
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
		reader = work.NewProviderConversationReader()
		p.readers[agent.ID] = reader
	}
	p.mu.Unlock()

	conversation, err := reader.Load(agent, provider, now)
	if err != nil {
		return watcher.ProviderActivityObservation{
			Structured:      true,
			FallbackAllowed: true,
		}
	}
	if conversation.Activity == nil {
		return watcher.ProviderActivityObservation{
			Structured:      true,
			FallbackAllowed: true,
		}
	}
	return watcher.ProviderActivityObservation{
		ID:              strings.TrimSpace(conversation.Activity.ID),
		Status:          string(conversation.Activity.Status),
		StartedAt:       parseProviderActivityTime(conversation.Activity.StartedAt),
		SettledAt:       parseProviderActivityTime(conversation.Activity.SettledAt),
		Structured:      true,
		FallbackAllowed: true,
	}
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
