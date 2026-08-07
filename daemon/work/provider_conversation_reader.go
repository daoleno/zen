package work

import (
	"os"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

// ProviderConversationReader owns the parsed provider source used by one live
// conversation subscription. Its zero value is ready to use, but callers
// normally allocate one at the subscription boundary.
type ProviderConversationReader struct {
	bound   bool
	binding providerConversationBinding
	source  providerConversationSource

	// cursorProjectRoots caches only the CWD→project-root resolution for this
	// subscription. Transcript files are still enumerated on every poll.
	cursorProjectRootsCWD string
	cursorProjectRoots    []cursorProjectRoot

	// openCodeOwnedSessionID retains the unambiguously bound OpenCode ses_* for
	// this subscription so later polls never cross-bind another same-CWD row.
	openCodeOwnedSessionID string

	// piPinnedSessionPath retains the auto-bound shared-directory Pi transcript
	// for this subscription so a newer same-CWD session never leaks into the
	// active agent's Interface mid-turn.
	piPinnedSessionPath string
}

type providerConversationBinding struct {
	provider  string
	agentID   string
	agentName string
	cwd       string
	command   string
	startedAt time.Time
	processID int
	hidden    bool
}

type providerConversationSource struct {
	provider     string
	path         string
	sessionID    string
	size         int64
	modTime      time.Time
	fileInfo     os.FileInfo
	conversation CodexConversation

	grokStamp       grokConversationStamp
	grokUpdatesInfo os.FileInfo
	grokUpdates     *grokUpdateTracker
}

func NewProviderConversationReader() *ProviderConversationReader {
	return &ProviderConversationReader{}
}

// Load selects and reads the configured provider's current native source.
// Source selection intentionally runs on every call; only parsing of the
// selected unchanged source is reused.
func (r *ProviderConversationReader) Load(
	agent classifier.Agent,
	provider string,
	now time.Time,
) (CodexConversation, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	r.bind(agent, provider)

	switch provider {
	case AgentProviderCursor:
		return r.loadCursorConversationForAgent(agent, now)
	case AgentProviderGrok:
		return r.loadGrokConversationForAgent(agent, now)
	case AgentProviderClaude:
		return r.loadClaudeConversationForAgent(agent, now)
	case AgentProviderCodex:
		return r.loadCodexConversationForAgent(agent, now)
	case AgentProviderPi:
		return r.loadPiConversationForAgent(agent, now)
	case AgentProviderOpenCode:
		return r.loadOpenCodeConversationForAgent(agent, now)
	default:
		r.resetSource()
		return CodexConversation{
			Available: false,
			Reason:    "not_structured_agent",
			Events:    []CodexConversationEvent{},
		}, nil
	}
}

func (r *ProviderConversationReader) bind(agent classifier.Agent, provider string) {
	next := providerConversationBinding{
		provider:  provider,
		agentID:   strings.TrimSpace(agent.ID),
		agentName: strings.TrimSpace(agent.Name),
		cwd:       strings.TrimSpace(agent.Cwd),
		command:   strings.TrimSpace(agent.Command),
		startedAt: agent.StartedAt,
		processID: agent.ProcessID,
		hidden:    agent.Hidden,
	}
	if r.bound && r.binding.equal(next) {
		return
	}
	if !r.bound || r.binding.cwd != next.cwd {
		r.cursorProjectRootsCWD = ""
		r.cursorProjectRoots = nil
	}
	if !r.bound || r.binding.provider != next.provider || r.binding.agentID != next.agentID ||
		r.binding.command != next.command || r.binding.cwd != next.cwd {
		r.openCodeOwnedSessionID = ""
		r.piPinnedSessionPath = ""
	}
	r.bound = true
	r.binding = next
	r.resetSource()
}

func (b providerConversationBinding) equal(other providerConversationBinding) bool {
	return b.provider == other.provider &&
		b.agentID == other.agentID &&
		b.agentName == other.agentName &&
		b.cwd == other.cwd &&
		b.command == other.command &&
		b.startedAt.Equal(other.startedAt) &&
		b.processID == other.processID &&
		b.hidden == other.hidden
}

func (r *ProviderConversationReader) resetSource() {
	r.source = providerConversationSource{}
}

func (r *ProviderConversationReader) loadFileConversation(
	provider string,
	path string,
	parse func(string) (CodexConversation, error),
) (CodexConversation, error) {
	info, err := os.Stat(path)
	if err != nil {
		r.resetSource()
		return CodexConversation{}, err
	}
	if r.source.provider == provider &&
		r.source.path == path &&
		r.source.size == info.Size() &&
		r.source.modTime.Equal(info.ModTime()) &&
		sameProviderSourceFile(r.source.fileInfo, info) {
		return r.source.conversation, nil
	}

	// A changed or newly-selected source cannot retain the prior parsed value.
	r.resetSource()
	conversation, err := parse(path)
	if err != nil {
		return CodexConversation{}, err
	}
	after, err := os.Stat(path)
	if err != nil {
		return CodexConversation{}, err
	}
	if !sameProviderSourceFile(info, after) ||
		info.Size() != after.Size() ||
		!info.ModTime().Equal(after.ModTime()) {
		// The parser opened a coherent file, but its source changed during the
		// read. Return this poll's value without retaining it; the next poll
		// selects and parses the current source again.
		return conversation, nil
	}
	r.source = providerConversationSource{
		provider:     provider,
		path:         path,
		size:         after.Size(),
		modTime:      after.ModTime(),
		fileInfo:     after,
		conversation: conversation,
	}
	return conversation, nil
}

func sameProviderSourceFile(left, right os.FileInfo) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return os.SameFile(left, right)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func truncateRunes(value string, max int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= max {
		return string(runes)
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return strings.TrimSpace(string(runes[:max-3])) + "..."
}
