package main

import (
	"errors"
	"os"
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
		// Channel health: a failed read is a bounded evidence loss — the
		// transcript is provably unlocatable (missing file) or unreadable
		// (stat/open/parse/sqlite failure). "Read successfully, no new
		// fact" is never a loss.
		return watcher.ProviderActivityObservation{
			Structured:      true,
			FallbackAllowed: true,
			ProbeState:      probeStateForConversation(work.CodexConversation{}, err),
		}
	}
	if !conversation.Available {
		// The reader succeeded but the source is not available: distinguish
		// the declared non-structured shapes (healthy no-fact) from a
		// provably unlocatable/unreadable transcript. The Pi reader fails
		// closed on a malformed header at the exact owned --session path; the
		// file existing proves the source is unreadable, not unlocatable.
		state := probeStateForConversation(conversation, nil)
		if state == watcher.ProbeStateUnlocatable && provider == work.AgentProviderPi {
			if ownedPath := work.PiOwnedSessionPath(agent.Command); ownedPath != "" {
				if info, statErr := os.Stat(ownedPath); statErr == nil && !info.IsDir() {
					state = watcher.ProbeStateUnreadable
				}
			}
		}
		return watcher.ProviderActivityObservation{
			Structured:      true,
			FallbackAllowed: true,
			ProbeState:      state,
		}
	}
	observation := watcher.ProviderActivityObservation{
		Structured:      true,
		FallbackAllowed: true,
		ProbeState:      watcher.ProbeStateOK,
	}
	if conversation.Activity != nil {
		observation.ID = strings.TrimSpace(conversation.Activity.ID)
		observation.Status = string(conversation.Activity.Status)
		observation.StartedAt = parseProviderActivityTime(conversation.Activity.StartedAt)
		observation.SettledAt = parseProviderActivityTime(conversation.Activity.SettledAt)
	}
	for _, activity := range conversation.ProviderActivities {
		status := strings.TrimSpace(string(activity.Status))
		switch status {
		case string(work.ProviderActivityCompleted),
			string(work.ProviderActivityFailed),
			string(work.ProviderActivityInterrupted),
			string(work.ProviderActivityCancelled):
			observation.TerminalActivities = append(
				observation.TerminalActivities,
				watcher.ProviderTerminalActivity{
					ID:        strings.TrimSpace(activity.ID),
					Status:    status,
					StartedAt: parseProviderActivityTime(activity.StartedAt),
					SettledAt: parseProviderActivityTime(activity.SettledAt),
				},
			)
		}
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

// probeStateForConversation classifies the provider-channel health of one
// read: a successful read with no new fact is ProbeStateOK; a provably lost
// source is unlocatable (missing) or unreadable (present but unreadable/
// malformed). Only loss states may drive the bounded session.uncertain.
func probeStateForConversation(conversation work.CodexConversation, err error) watcher.ProviderProbeState {
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return watcher.ProbeStateUnlocatable
		}
		return watcher.ProbeStateUnreadable
	}
	switch strings.TrimSpace(conversation.Reason) {
	case "", "not_structured_agent", "missing_cwd":
		return watcher.ProbeStateOK
	case "transcript_malformed":
		return watcher.ProbeStateUnreadable
	default:
		// transcript_not_found, session_not_found, db_unavailable, ...
		return watcher.ProbeStateUnlocatable
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
