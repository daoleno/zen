package brain

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

// SessionAssistantItem is one user-visible assistant message of a delegated
// Session. It contains only display text and the provider event identity; no
// tool payload, prompt, or internal envelope is exposed.
type SessionAssistantItem struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	Partial   bool      `json:"partial,omitempty"`
}

// SessionProjection is the read-only, user-visible projection of one delegated
// Session for channel adapters (Telegram). It carries no policy authority: it
// cannot admit input, close a turn, or resolve Work. Session lifecycle state
// mirrors the canonical Turn ledger and the owning Work; `Present` reports
// whether the watcher currently sees the Session.
type SessionProjection struct {
	SessionID   string                 `json:"session_id"`
	Present     bool                   `json:"present"`
	Label       string                 `json:"label,omitempty"`
	Status      string                 `json:"status,omitempty"`
	TurnID      string                 `json:"turn_id,omitempty"`
	TurnStatus  string                 `json:"turn_status,omitempty"`
	TurnSummary string                 `json:"turn_summary,omitempty"`
	WorkID      string                 `json:"work_id,omitempty"`
	WorkStatus  string                 `json:"work_status,omitempty"`
	WorkTitle   string                 `json:"work_title,omitempty"`
	ThreadID    string                 `json:"thread_id,omitempty"`
	Assistant   []SessionAssistantItem `json:"assistant,omitempty"`
}

// DelegatedSessions returns the current user-visible delegated Sessions in
// projection order (newest activity first). It never launches, rebinds, hides,
// or retires a Session: this is the read-only inventory used by presentation
// adapters.
func (s *Service) DelegatedSessions() ([]AgentRef, error) {
	if s == nil || s.watcher == nil {
		return []AgentRef{}, nil
	}
	host, err := s.store.HostSession()
	if err != nil {
		return nil, err
	}
	hostID := strings.TrimSpace(host.ID)
	agents := s.watcher.Agents()
	out := make([]AgentRef, 0, len(agents))
	for _, agent := range agents {
		if agent == nil || agent.Hidden || !agent.Delegated || strings.TrimSpace(agent.ID) == "" {
			continue
		}
		if hostID != "" && agent.ID == hostID {
			continue
		}
		out = append(out, agentRefFromClassifier(agent))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Updated.After(out[j].Updated)
	})
	return out, nil
}

// WorkForSession returns the durable Work owned by a delegated Session when
// present: the newest non-terminal Work wins; otherwise the newest terminal
// Work is returned so a channel adapter can record "Work ID when present".
func (s *Service) WorkForSession(sessionID string) (Work, bool, error) {
	if s == nil || s.store == nil {
		return Work{}, false, nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Work{}, false, nil
	}
	items, err := s.store.ListWork()
	if err != nil {
		return Work{}, false, err
	}
	var best, bestTerminal *Work
	for index := range items {
		item := items[index]
		if strings.TrimSpace(item.AttemptSessionID) != sessionID {
			continue
		}
		if item.Status != WorkDone && item.Status != WorkCancelled {
			if best == nil || item.CreatedAt.After(best.CreatedAt) {
				copy := item
				best = &copy
			}
			continue
		}
		if bestTerminal == nil || item.CreatedAt.After(bestTerminal.CreatedAt) {
			copy := item
			bestTerminal = &copy
		}
	}
	if best != nil {
		return *best, true, nil
	}
	if bestTerminal != nil {
		return *bestTerminal, true, nil
	}
	return Work{}, false, nil
}

// SubmitExternalSessionInput submits one channel-owned receipt to the exact
// delegated Session through the watcher's provider input owner. It uses the
// same receipt semantics as the mobile direct Session input path: the watcher
// may retry a definite pre-mutation failure under the same durable identity,
// but an ambiguous provider admission is never replayed. It never wraps the
// text as a Brain message and never synthesizes Work Events or lifecycle
// control turns.
func (s *Service) SubmitExternalSessionInput(sessionID, receipt, body string) (ExternalInputDisposition, error) {
	sessionID = strings.TrimSpace(sessionID)
	receipt = strings.TrimSpace(receipt)
	body = strings.TrimSpace(body)
	if s == nil || s.store == nil || s.watcher == nil {
		return ExternalInputNotSubmitted, fmt.Errorf("brain service is not configured")
	}
	if receipt == "" || body == "" {
		return ExternalInputNotSubmitted, fmt.Errorf("external Session input requires receipt and body")
	}
	agent := s.watcher.GetAgent(sessionID)
	if agent == nil || agent.Hidden || !agent.Delegated {
		return ExternalInputNotSubmitted, fmt.Errorf("delegated Session is unavailable")
	}
	result, err := s.watcher.SendInputWithReceiptResult(sessionID, body, receipt)
	if err != nil {
		if result.Outcome == watcher.InputAmbiguous {
			return ExternalInputUncertain, err
		}
		return ExternalInputNotSubmitted, err
	}
	return ExternalInputAccepted, nil
}

// SessionProjection builds the read-only user-visible projection of one
// delegated Session. It is safe for presentation adapters and never mutates
// Session, Turn, Work, or provider state.
func (s *Service) SessionProjection(sessionID string) (SessionProjection, error) {
	projection := SessionProjection{SessionID: strings.TrimSpace(sessionID)}
	if s == nil || s.watcher == nil || s.store == nil {
		return projection, nil
	}
	agent := s.watcher.GetAgent(projection.SessionID)
	if agent != nil && !agent.Hidden && agent.Delegated {
		projection.Present = true
		projection.Label = sessionDisplayLabel(agent)
		projection.Status = string(agent.State)
		if turn, hasTurn, turnErr := s.store.Turn(projection.SessionID); turnErr == nil && hasTurn {
			projection.TurnID = turn.TurnID
			projection.TurnStatus = string(turn.Status)
			projection.TurnSummary = turn.Summary
		}
		if workItem, found, workErr := s.WorkForSession(projection.SessionID); workErr == nil && found {
			projection.WorkID = workItem.ID
			projection.WorkStatus = string(workItem.Status)
			projection.WorkTitle = workItem.Title
			projection.ThreadID = workItem.SourceThreadID
		}
		projection.Assistant = s.sessionAssistantItems(agent, s.nowUTC())
	} else {
		// The Session is absent/not visible: retain Turn/Work so an adapter can
		// mark completion/staleness without having to guess.
		if turn, hasTurn, turnErr := s.store.Turn(projection.SessionID); turnErr == nil && hasTurn {
			projection.TurnID = turn.TurnID
			projection.TurnStatus = string(turn.Status)
			projection.TurnSummary = turn.Summary
		}
		if workItem, found, workErr := s.WorkForSession(projection.SessionID); workErr == nil && found {
			projection.WorkID = workItem.ID
			projection.WorkStatus = string(workItem.Status)
			projection.WorkTitle = workItem.Title
			projection.ThreadID = workItem.SourceThreadID
		}
	}
	return projection, nil
}

func (s *Service) sessionAssistantItems(agent *classifier.Agent, now time.Time) []SessionAssistantItem {
	provider := work.InferAgentProvider(agent.Command, agent.Name)
	if provider == "" {
		return nil
	}
	conversation, err := s.loadSessionAssistantConversation(agent, provider, now)
	if err != nil {
		return nil
	}
	conversation = work.SanitizeConversationProjection(conversation)
	items := make([]SessionAssistantItem, 0, len(conversation.Events))
	for _, event := range conversation.Events {
		if event.Kind != "assistant_message" || strings.TrimSpace(event.Body) == "" {
			continue
		}
		createdAt := time.Time{}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if parsed, parseErr := time.Parse(layout, event.Timestamp); parseErr == nil {
				createdAt = parsed
				break
			}
		}
		items = append(items, SessionAssistantItem{
			ID:        strings.TrimSpace(event.ID),
			Body:      event.Body,
			CreatedAt: createdAt,
			Partial:   event.Partial,
		})
	}
	return items
}

func (s *Service) loadSessionAssistantConversation(agent *classifier.Agent, provider string, now time.Time) (work.CodexConversation, error) {
	if s != nil && s.sessionConversationHook != nil {
		return s.sessionConversationHook(agent, provider, now)
	}
	if agent == nil {
		return work.CodexConversation{}, nil
	}
	return work.NewProviderConversationReader().Load(*agent, provider, now)
}

func sessionDisplayLabel(agent *classifier.Agent) string {
	name := strings.TrimSpace(agent.Name)
	if name == "" {
		return strings.TrimSpace(agent.ID)
	}
	runes := []rune(name)
	if len(runes) > 128 {
		return string(runes[:128])
	}
	return name
}
