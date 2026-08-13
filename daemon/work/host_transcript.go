package work

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

// HostTranscriptIdentity is the stable provider conversation binding for a
// Brain Host Executor Session. SessionID/Path/DataRoot are provider-native
// (Codex rollout, Grok session dir, Claude jsonl, OpenCode ses_*, and so on).
// A previous executor's identity is not a valid binding for a new host.
type HostTranscriptIdentity struct {
	Provider  string
	SessionID string
	Path      string
	DataRoot  string
}

// Bound reports whether the identity names a provider conversation.
func (identity HostTranscriptIdentity) Bound() bool {
	return strings.TrimSpace(identity.SessionID) != "" || strings.TrimSpace(identity.Path) != ""
}

// Codex converts the host identity into the Codex-specific rollout shape.
func (identity HostTranscriptIdentity) Codex() CodexTranscriptIdentity {
	return CodexTranscriptIdentity{
		SessionID: strings.TrimSpace(identity.SessionID),
		Path:      strings.TrimSpace(identity.Path),
		DataRoot:  strings.TrimSpace(identity.DataRoot),
	}
}

func hostTranscriptFromCodex(provider string, identity CodexTranscriptIdentity) HostTranscriptIdentity {
	return HostTranscriptIdentity{
		Provider:  strings.ToLower(strings.TrimSpace(provider)),
		SessionID: strings.TrimSpace(identity.SessionID),
		Path:      strings.TrimSpace(identity.Path),
		DataRoot:  strings.TrimSpace(identity.DataRoot),
	}
}

// ResolveHostTranscriptIdentityForAgent binds the live host process to its
// current provider conversation. An existing identity is kept only when it
// still belongs to that provider; a previous executor's transcript cannot win.
func ResolveHostTranscriptIdentityForAgent(
	agent classifier.Agent,
	existing HostTranscriptIdentity,
	provider string,
) HostTranscriptIdentity {
	provider = normalizeHostTranscriptProvider(provider, agent)
	existing = normalizeHostTranscriptIdentity(existing)
	if !hostIdentityUsable(existing, provider) {
		existing = HostTranscriptIdentity{}
	}
	if hostIdentityUsable(existing, provider) {
		existing.Provider = provider
		return existing
	}

	switch provider {
	case AgentProviderCodex, "":
		resolved := ResolveCodexTranscriptIdentityForAgent(agent, existing.Codex())
		out := hostTranscriptFromCodex(AgentProviderCodex, resolved)
		if provider != "" {
			out.Provider = provider
		}
		return out
	default:
		conversation, err := NewProviderConversationReader().Load(agent, provider, time.Now().UTC())
		if err != nil || !conversation.Available {
			return HostTranscriptIdentity{Provider: provider}
		}
		sessionID := strings.TrimSpace(conversation.SessionID)
		path := strings.TrimSpace(conversation.Path)
		if sessionID == "" && path == "" {
			return HostTranscriptIdentity{Provider: provider}
		}
		return HostTranscriptIdentity{
			Provider:  provider,
			SessionID: sessionID,
			Path:      path,
			DataRoot:  hostDataRootForPath(path, provider),
		}
	}
}

// LoadHostConversationByIdentity loads the bound host conversation through the
// existing provider readers. It never invents a second parser and never falls
// back to a different provider's transcript.
func LoadHostConversationByIdentity(identity HostTranscriptIdentity) (CodexConversation, error) {
	identity = normalizeHostTranscriptIdentity(identity)
	if !identity.Bound() {
		return CodexConversation{
			Available: false,
			Reason:    "host_transcript_unbound",
			Events:    []CodexConversationEvent{},
		}, nil
	}
	return NewProviderConversationReader().LoadByIdentity(identity)
}

// LoadByIdentity reads a previously bound provider conversation. Source
// selection is the identity; cwd matching is not authority.
func (r *ProviderConversationReader) LoadByIdentity(identity HostTranscriptIdentity) (CodexConversation, error) {
	if r == nil {
		r = NewProviderConversationReader()
	}
	identity = normalizeHostTranscriptIdentity(identity)
	if !identity.Bound() {
		r.resetSource()
		return CodexConversation{
			Available: false,
			Reason:    "host_transcript_unbound",
			Events:    []CodexConversationEvent{},
		}, nil
	}

	switch identity.Provider {
	case AgentProviderCodex, "":
		return LoadCodexConversationByIdentity(identity.Codex())
	case AgentProviderGrok:
		return r.loadBoundGrokConversation(identity)
	case AgentProviderClaude:
		return r.loadBoundFileConversation(identity, claudeConversationSource, r.loadClaudeConversation)
	case AgentProviderCursor:
		return r.loadBoundFileConversation(identity, cursorConversationSource, r.loadCursorConversation)
	case AgentProviderPi:
		return r.loadBoundFileConversation(identity, piConversationSource, r.loadPiConversation)
	case AgentProviderOpenCode:
		return r.loadBoundOpenCodeConversation(identity)
	default:
		r.resetSource()
		return CodexConversation{
			Available: false,
			Reason:    "not_structured_agent",
			Events:    []CodexConversationEvent{},
		}, nil
	}
}

func (r *ProviderConversationReader) loadBoundGrokConversation(identity HostTranscriptIdentity) (CodexConversation, error) {
	path := strings.TrimSpace(identity.Path)
	if path == "" || !hostIdentityUsable(identity, AgentProviderGrok) {
		r.resetSource()
		return CodexConversation{
			Available: false,
			Reason:    "transcript_not_found",
			Events:    []CodexConversationEvent{},
		}, nil
	}
	conversation, err := r.loadGrokConversation(path)
	if err != nil {
		return CodexConversation{}, err
	}
	conversation.Available = true
	conversation.Source = "grok_session"
	conversation.Path = path
	conversation.SessionID = firstNonEmpty(conversation.SessionID, identity.SessionID, filepath.Base(path))
	if conversation.Events == nil {
		conversation.Events = []CodexConversationEvent{}
	}
	return conversation, nil
}

func (r *ProviderConversationReader) loadBoundFileConversation(
	identity HostTranscriptIdentity,
	source string,
	load func(string) (CodexConversation, error),
) (CodexConversation, error) {
	path := strings.TrimSpace(identity.Path)
	if path == "" {
		r.resetSource()
		return CodexConversation{
			Available: false,
			Reason:    "transcript_not_found",
			Events:    []CodexConversationEvent{},
		}, nil
	}
	if _, err := os.Stat(path); err != nil {
		r.resetSource()
		return CodexConversation{
			Available: false,
			Reason:    "transcript_not_found",
			Events:    []CodexConversationEvent{},
		}, nil
	}
	conversation, err := load(path)
	if err != nil {
		return CodexConversation{}, err
	}
	conversation.Available = true
	conversation.Source = firstNonEmpty(conversation.Source, source)
	conversation.Path = path
	conversation.SessionID = firstNonEmpty(conversation.SessionID, identity.SessionID, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if conversation.Events == nil {
		conversation.Events = []CodexConversationEvent{}
	}
	return conversation, nil
}

func (r *ProviderConversationReader) loadBoundOpenCodeConversation(identity HostTranscriptIdentity) (CodexConversation, error) {
	sessionID := strings.TrimSpace(identity.SessionID)
	if sessionID == "" {
		r.resetSource()
		return CodexConversation{
			Available: false,
			Reason:    "transcript_not_found",
			Events:    []CodexConversationEvent{},
		}, nil
	}
	dbPath := strings.TrimSpace(identity.Path)
	if dbPath == "" {
		resolved, err := openCodeDBPath()
		if err != nil || resolved == "" {
			r.resetSource()
			return CodexConversation{
				Available: false,
				Reason:    "db_unavailable",
				Events:    []CodexConversationEvent{},
			}, nil
		}
		dbPath = resolved
	}
	conversation, err := r.loadOpenCodeConversation(dbPath, sessionID)
	if err != nil {
		return CodexConversation{}, err
	}
	conversation.Available = true
	conversation.SessionID = firstNonEmpty(conversation.SessionID, sessionID)
	if conversation.Events == nil {
		conversation.Events = []CodexConversationEvent{}
	}
	return conversation, nil
}

func normalizeHostTranscriptProvider(provider string, agent classifier.Agent) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" || provider == AgentProviderCustom {
		provider = InferAgentProvider(agent.Command, agent.Name)
	}
	return provider
}

func normalizeHostTranscriptIdentity(identity HostTranscriptIdentity) HostTranscriptIdentity {
	identity.Provider = strings.ToLower(strings.TrimSpace(identity.Provider))
	identity.SessionID = strings.TrimSpace(identity.SessionID)
	identity.Path = strings.TrimSpace(identity.Path)
	identity.DataRoot = strings.TrimSpace(identity.DataRoot)
	return identity
}

func hostIdentityUsable(identity HostTranscriptIdentity, provider string) bool {
	identity = normalizeHostTranscriptIdentity(identity)
	provider = strings.ToLower(strings.TrimSpace(provider))
	if identity.Provider != "" && provider != "" && identity.Provider != provider {
		return false
	}
	if !identity.Bound() {
		return false
	}
	switch provider {
	case AgentProviderGrok:
		return grokSessionDirUsable(identity.Path)
	case AgentProviderCodex, "":
		if identity.Path != "" {
			info, err := os.Stat(identity.Path)
			return err == nil && !info.IsDir()
		}
		return identity.SessionID != ""
	case AgentProviderOpenCode:
		if identity.SessionID != "" {
			if identity.Path == "" {
				return true
			}
			_, err := os.Stat(identity.Path)
			return err == nil
		}
		return false
	default:
		if identity.Path == "" {
			return false
		}
		_, err := os.Stat(identity.Path)
		return err == nil
	}
}

func grokSessionDirUsable(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	if _, err := os.Stat(filepath.Join(path, grokChatHistoryFile)); err == nil {
		return true
	}
	_, err = os.Stat(filepath.Join(path, grokSummaryFile))
	return err == nil
}

func hostDataRootForPath(path, provider string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case AgentProviderCodex:
		return dataRootForPath(path, nil)
	case AgentProviderGrok:
		const marker = string(os.PathSeparator) + ".grok" + string(os.PathSeparator)
		if index := strings.Index(path, marker); index > 0 {
			return path[:index]
		}
	}
	return ""
}

// IsPrivateHostPrompt reports Brain bootstrap/handoff inputs that must never
// become visible Interface rows.
func IsPrivateHostPrompt(body string) bool {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return false
	}
	if strings.Contains(trimmed, "Brain host executor handoff:") {
		return true
	}
	if strings.Contains(trimmed, "You are Brain inside zen") {
		return true
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "treat this bootstrap as a map") {
		return true
	}
	return isGrokBootstrapUserMessage(trimmed)
}

// SuppressPrivateHostTurns drops bootstrap/handoff user rows and every host
// output that precedes the first public user turn. Later real assistant
// replies remain.
func SuppressPrivateHostTurns(events []CodexConversationEvent) []CodexConversationEvent {
	if len(events) == 0 {
		return events
	}
	publicUserSeen := false
	out := make([]CodexConversationEvent, 0, len(events))
	for _, event := range events {
		switch strings.TrimSpace(event.Kind) {
		case "user_message":
			if IsPrivateHostPrompt(event.Body) {
				continue
			}
			publicUserSeen = true
			out = append(out, event)
		default:
			if !publicUserSeen {
				continue
			}
			out = append(out, event)
		}
	}
	return out
}
