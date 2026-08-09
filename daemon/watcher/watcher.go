package watcher

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/google/uuid"
)

const tmuxSendInputChunkBytes = 1024

const initialInputReadyTimeout = 8 * time.Second
const codexInputStartupStallTimeout = 30 * time.Second
const cursorInputReadyTimeout = 25 * time.Second
const claudeInputReadyTimeout = 12 * time.Second
const grokInputReadyTimeout = 15 * time.Second
const piInputReadyTimeout = 15 * time.Second
const openCodeInputReadyTimeout = 15 * time.Second

var cursorInputReadyRe = regexp.MustCompile(`(?im)\b(run\s+everything|composer\s+[0-9][^\n]*\n\s*~?[/\w.-].*)\b`)
var cursorWorkspaceTrustRe = regexp.MustCompile(`(?im)\bworkspace\s+trust\s+required\b`)

// Claude Code TUI ready (observed on 2.1.214 / probe @224): numeric version
// header, empty composer line (❯), plus one permission/mode footer.
// Distinguishes ready state from startup/loading/safety screens. Version
// matcher is intentionally not pinned to major version 2.
//
// Empty composers are often "❯" + U+00A0. Go regexp \s does not match NBSP, so
// claudeComposerRe lists U+00A0 explicitly and still rejects nonempty drafts.
var claudeHeaderRe = regexp.MustCompile(`Claude Code v?\d+\.\d+\.\d+`)
var claudeComposerRe = regexp.MustCompile(`(?m)^[\t \x{00A0}]*❯[\t \x{00A0}]*$`)
var claudeModeFooterRe = regexp.MustCompile(`(?i)(bypass permissions|manual mode).*(shift\+tab|shortcuts|\?)`)

// Grok TUI ready: model/footer chrome plus the empty/ready composer prompt glyph.
var grokChromeReadyRe = regexp.MustCompile(`(?im)(\bgrok\s+[0-9]|always-approve|enter\s*:\s*send|shift\+tab:mode)`)
var grokPromptReadyRe = regexp.MustCompile(`(?m)[│┃]\s*❯|^\s*❯`)

// Pi TUI ready (captured 0.73.1): version header, paired empty editor rules, and
// a footer with cwd/usage/model. Overlays and changelog floods are not ready.
var piVersionRe = regexp.MustCompile(`(?im)\bpi\s+v\d+\.\d+\.\d+\b`)
var piEditorBorderRe = regexp.MustCompile(`(?m)^─{16,}$`)
var piChromeRe = regexp.MustCompile(`(?im)(escape interrupt|/ commands|! bash)`)

// OpenCode TUI ready: empty composer placeholder, agent/model line, and idle
// footer chrome. Two footer shapes are accepted, both anchored to a filesystem
// path at line start so arbitrary pane text cannot pass:
//
//   - 1.18.13 legacy: cwd/path left and semver right.
//   - 1.18.15 current (captured live 2026-08-08): cwd/path left and
//     "ctrl+p commands" right; the semver was dropped from the idle footer.
//     The busy footer ("esc interrupt ... ctrl+p commands") does not start
//     with a path, so it is never accepted as ready.
//
// openCodeFooterPathPrefix admits the home-directory abbreviation "~" as well
// as ~/, absolute, relative, and drive paths: the real Calendar cwd
// /home/daoleno renders as a bare "~" in the 1.18.15 idle footer
// (captured live 2026-08-08 occurrence d9ff47a4), e.g.
//
//	~                                                                    1.18.15
//
// Model overlays are not ready. Do not treat arbitrary pane semver (tool
// output, deps) as OpenCode's version footer.
var openCodeComposerPlaceholderRe = regexp.MustCompile(`(?im)Ask anything\.\.\.`)
var openCodeAgentLineRe = regexp.MustCompile(`(?im)\b(Build|Plan|Ask)\b[^\n]*[·•]`)
var openCodeFooterPathPrefix = `(?:~|/|\.{1,2}/|[A-Za-z]:\\)`
var openCodeVersionFooterRe = regexp.MustCompile(`(?m)^\s*` + openCodeFooterPathPrefix + `\S*(?:\s+\S+)*?\s{2,}\d+\.\d+\.\d+\s*$`)
var openCodeIdleFooterRe = regexp.MustCompile(`(?m)^\s*` + openCodeFooterPathPrefix + `\S*(?:\s+\S+)*?\s{2,}ctrl\+p\s+commands\s*$`)
var openCodeBlockedOverlayRe = regexp.MustCompile(`(?im)(connect provider|sign in|permission required|select a model|choose a model|trust this)`)

type targetProcessIdentity struct {
	Command         string
	PanePID         int
	PaneStart       int64
	ForegroundID    int
	ForegroundStart int64
	ProcessID       int
	ProcessStart    int64
}

func (identity targetProcessIdentity) valid() bool {
	return strings.TrimSpace(identity.Command) != "" &&
		identity.PanePID > 0 &&
		identity.PaneStart > 0 &&
		identity.ForegroundID > 0 &&
		identity.ForegroundStart > 0 &&
		identity.ProcessID > 0 &&
		identity.ProcessStart > 0
}

func (identity targetProcessIdentity) equal(other targetProcessIdentity) bool {
	return identity.valid() && other.valid() && identity == other
}

// Poll observation seams for deterministic tests. Production rebinds them to
// watcher-bound closures at New() so inventory and pane captures resolve each
// target's tmux server; tests in this package swap them before invoking poll.
var listTmuxWindowsFunc = func() ([]tmuxWindow, error) { return listTmuxWindowsOn("") }
var capturePaneContentFunc = func(target string) (string, bool, int) { return capturePaneContentOn("", target) }
var snapshotProcessesFunc = snapshotProcesses
var tmuxSubmitSleep = time.Sleep

func guardTargetIdentity(
	resolver func(string) (targetProcessIdentity, bool),
	target string,
	expected targetProcessIdentity,
) error {
	current, ok := resolver(target)
	if !ok || !current.equal(expected) {
		return fmt.Errorf("target provider process identity changed; terminal mutation was not sent")
	}
	return nil
}

func resolveTargetIdentityWhenReady(
	resolver func(string) (targetProcessIdentity, bool),
	target string,
	commandHint string,
) (targetProcessIdentity, bool) {
	return resolveTargetIdentityWhenReadyTimeout(
		resolver,
		target,
		commandHint,
		inputReadyTimeout(strings.TrimSpace(commandHint)),
	)
}

func resolveTargetIdentityWhenReadyTimeout(
	resolver func(string) (targetProcessIdentity, bool),
	target string,
	commandHint string,
	timeout time.Duration,
) (targetProcessIdentity, bool) {
	deadline := time.Now().Add(timeout)
	expectedExecutable := commandExecutableBase(strings.TrimSpace(commandHint))
	var previous targetProcessIdentity
	stable := 0
	for {
		if identity, ok := resolver(target); ok {
			resolvedExecutable := commandExecutableBase(identity.Command)
			if expectedExecutable != "" &&
				resolvedExecutable != expectedExecutable &&
				isTransientLaunchShell(resolvedExecutable) {
				previous = targetProcessIdentity{}
				stable = 0
				if !time.Now().Before(deadline) {
					return targetProcessIdentity{}, false
				}
				time.Sleep(25 * time.Millisecond)
				continue
			}
			if identity.equal(previous) {
				stable++
			} else {
				previous = identity
				stable = 1
			}
			if stable >= 2 {
				return identity, true
			}
		} else {
			previous = targetProcessIdentity{}
			stable = 0
		}
		if !time.Now().Before(deadline) {
			return targetProcessIdentity{}, false
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func isTransientLaunchShell(command string) bool {
	switch normalizeCommand(command) {
	case "sh", "bash", "dash", "fish", "zsh":
		return true
	default:
		return false
	}
}

// SessionEvent represents a state change or output update for an agent.
type SessionEvent struct {
	Type     string              `json:"type"`
	AgentID  string              `json:"agent_id"`
	Agent    *classifier.Agent   `json:"agent,omitempty"`
	Agents   []*classifier.Agent `json:"agents,omitempty"`
	Lines    []string            `json:"lines,omitempty"`
	OldState string              `json:"old,omitempty"`
	NewState string              `json:"new,omitempty"`
	TurnID   string              `json:"-"`
}

// Watcher monitors tmux windows and classifies agent states.
type Watcher struct {
	pollInterval          time.Duration
	agents                map[string]*classifier.Agent
	agentOrder            []string
	prevContent           map[string]string
	hidden                map[string]bool
	delegated             map[string]bool
	activityProbe         classifier.ActivityProbe
	providerActivityProbe ProviderActivityProbe
	pollGeneration        int64
	agentEpoch            map[string]int64 // per-agent generation for lock-free probe apply
	turnLedger            TurnLedger
	pollSources           *PollSources              // test-only seam (SetPollSources); production is nil
	ledgerTurns           map[string]TurnSnapshot   // projection cache of the canonical ledger, never a truth owner
	appliedFactIDs        map[string]string         // session -> last applied provider FactID (skip identical applies)
	ledgerTurnReadAt      map[string]time.Time      // TTL for authoritative ledger re-reads
	probeLossSince        map[string]probeLossState // session -> current turn loss streak
	// daemonSocketPath is the daemon-namespaced tmux server that hosts all
	// Zen-owned Brain and delegated Sessions; empty keeps the legacy default
	// server (tests). daemonScratchDir is the TMUX_TMPDIR for hidden host
	// sessions without a per-agent resource scratch.
	daemonSocketPath string
	daemonScratchDir string
	// targetSockets maps every inventoried target to its tmux server
	// ownership. known distinguishes a target this watcher has actually seen
	// or created from a genuinely unknown target; socket is the server path
	// ("" = the user's default server). Ownership is never mixed.
	targetSockets         map[string]targetSocket
	mu                    sync.RWMutex
	events                chan SessionEvent
	resources             delegatedResourceManager
	sessionInput          *sessionInputOwner
	targetProcessResolver func(string) (targetProcessIdentity, bool)
	targetCommandResolver func(string) (string, bool)
	admissionNow          func() time.Time
	admissionSleep        func(time.Duration)
	admissionTimeout      func(string) time.Duration
	pollNow               func() time.Time
}

// New creates a Watcher that polls tmux windows at the given interval.
func New(pollInterval time.Duration) *Watcher {
	w := &Watcher{
		pollInterval:     pollInterval,
		agents:           make(map[string]*classifier.Agent),
		prevContent:      make(map[string]string),
		hidden:           make(map[string]bool),
		delegated:        make(map[string]bool),
		agentEpoch:       make(map[string]int64),
		ledgerTurns:      make(map[string]TurnSnapshot),
		appliedFactIDs:   make(map[string]string),
		ledgerTurnReadAt: make(map[string]time.Time),
		probeLossSince:   make(map[string]probeLossState),
		targetSockets:    make(map[string]targetSocket),
		events:           make(chan SessionEvent, 100),
		resources:        noopDelegatedResourceManager{},
	}
	// The production session input IO and poll sources are bound to this
	// watcher so every tmux invocation resolves each target's server. Test
	// seams replace the package-level function pointers and restore these
	// closures.
	w.sessionInput = newSessionInputOwner(realSessionInputIO{socketFor: w.socketPathFor})
	listTmuxWindowsFunc = w.listTmuxWindows
	capturePaneContentFunc = w.capturePaneContent
	return w
}

// targetSocket is the tmux server ownership of one target. known=true means
// the target was inventoried or created by this watcher; socket is the server
// path, where "" means the user's default server. A missing entry (known =
// false) is a genuinely unknown target — distinct from a known user-server
// target.
type targetSocket struct {
	known  bool
	socket string
}

type probeLossState struct {
	turnID string
	since  time.Time
}

// SetDaemonSocket installs the daemon-namespaced tmux server that hosts all
// Zen-owned Brain and delegated Sessions, plus the TMUX_TMPDIR scratch for
// hidden host sessions. Sessions already inventoried keep their recorded
// socket; new sessions are created on the daemon socket.
func (w *Watcher) SetDaemonSocket(socketPath, scratchDir string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.daemonSocketPath = strings.TrimSpace(socketPath)
	w.daemonScratchDir = strings.TrimSpace(scratchDir)
	w.mu.Unlock()
}

// SocketPathFor returns the tmux server socket path that hosts a KNOWN
// target: the daemon-namespaced socket for Zen-owned Sessions, or "" (the
// user's default server) for known user/manual Sessions. Genuinely unknown
// targets resolve to "" so user-visible surfaces never leak the daemon
// socket and never assume ownership.
func (w *Watcher) SocketPathFor(target string) string {
	if w == nil {
		return ""
	}
	target = strings.TrimSpace(target)
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.targetSockets[target].socket
}

// socketPathFor resolves the tmux server for one target for internal
// operations. A KNOWN user-server target resolves to "" (its own server); a
// known daemon target resolves to the daemon socket; only a genuinely
// UNKNOWN target falls back to the daemon socket (fresh Zen creates and the
// Brain host before its first inventory). Same-name targets across servers
// are resolved deterministically by the inventory (daemon shadows user) and
// by the create path (the socket actually used for the create is recorded).
func (w *Watcher) socketPathFor(target string) string {
	if w == nil {
		return ""
	}
	target = strings.TrimSpace(target)
	w.mu.RLock()
	ownership := w.targetSockets[target]
	daemon := w.daemonSocketPath
	w.mu.RUnlock()
	if ownership.known {
		return ownership.socket
	}
	return daemon
}

// SetTurnLedger installs the canonical per-turn ledger. The watcher applies
// provider/control/liveness facts through it and projects Session state from
// its snapshots; it holds no competing lifecycle state machine.
func (w *Watcher) SetTurnLedger(ledger TurnLedger) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.turnLedger = ledger
	if w.sessionInput != nil {
		w.sessionInput.ledger = ledger
	}
}

func (w *Watcher) sessionInputOwner() *sessionInputOwner {
	if w == nil {
		return defaultSessionInputOwner
	}
	w.mu.RLock()
	owner := w.sessionInput
	w.mu.RUnlock()
	if owner == nil {
		return defaultSessionInputOwner
	}
	return owner
}

func targetIdentityResolverFromCommandResolver(
	resolver func(string) (string, bool),
) func(string) (targetProcessIdentity, bool) {
	return func(target string) (targetProcessIdentity, bool) {
		command, ok := resolver(target)
		command = strings.TrimSpace(command)
		if !ok || command == "" {
			return targetProcessIdentity{}, false
		}
		// Tests and embedders can replace the authoritative resolver without
		// requiring a live process table. Production leaves it nil and receives
		// the real pane/process identity from resolveTargetProcessIdentity.
		return targetProcessIdentity{
			Command:         command,
			PanePID:         1,
			PaneStart:       1,
			ForegroundID:    1,
			ForegroundStart: 1,
			ProcessID:       1,
			ProcessStart:    1,
		}, true
	}
}

func (w *Watcher) targetForSession(sessionID string) (targetProcessIdentity, bool) {
	if w == nil {
		return targetProcessIdentity{}, false
	}
	w.mu.RLock()
	identityResolver := w.targetProcessResolver
	resolver := w.targetCommandResolver
	w.mu.RUnlock()
	if identityResolver != nil {
		return identityResolver(sessionID)
	}
	if resolver == nil {
		return w.resolveTargetProcessIdentity(sessionID)
	}
	return targetIdentityResolverFromCommandResolver(resolver)(sessionID)
}

// resolveTargetProcessIdentity resolves the exact pane/process identity on
// the target's own tmux server (daemon-namespaced for Zen-owned Sessions,
// user default for known user/manual Sessions), so every production
// admission/input path proves identity against the server that actually owns
// the pane.
func (w *Watcher) resolveTargetProcessIdentity(target string) (targetProcessIdentity, bool) {
	target = strings.TrimSpace(target)
	if target == "" {
		return targetProcessIdentity{}, false
	}
	out, err := tmuxCommand(w.socketPathFor(target), "display-message", "-p", "-t", target, "#{pane_dead}\t#{pane_pid}").Output()
	if err != nil {
		return targetProcessIdentity{}, false
	}
	fields := strings.Split(strings.TrimSuffix(string(out), "\n"), "\t")
	if len(fields) != 2 || fields[0] == "1" {
		return targetProcessIdentity{}, false
	}
	panePID, err := strconv.Atoi(strings.TrimSpace(fields[1]))
	if err != nil || panePID <= 0 {
		return targetProcessIdentity{}, false
	}
	processes := snapshotProcesses()
	if len(processes) == 0 {
		return targetProcessIdentity{}, false
	}
	paneProcess, ok := processes[panePID]
	if !ok || paneProcess.startedAt.IsZero() {
		return targetProcessIdentity{}, false
	}
	authority, ok := resolveForegroundTargetProcess(panePID, processes)
	authority.command = strings.TrimSpace(authority.command)
	if !ok || authority.command == "" {
		return targetProcessIdentity{}, false
	}
	identity := targetProcessIdentity{
		Command:         authority.command,
		PanePID:         panePID,
		PaneStart:       paneProcess.startedAt.UnixNano(),
		ForegroundID:    authority.foreground.pid,
		ForegroundStart: authority.foreground.startedAt.UnixNano(),
		ProcessID:       authority.provider.pid,
		ProcessStart:    authority.provider.startedAt.UnixNano(),
	}
	return identity, identity.valid()
}

// ConfigureDelegatedResources enables the platform resource backend for
// Brain-owned delegated sessions. owner must be the durable daemon identity;
// it namespaces every resource unit so one daemon can never stop another
// daemon's sessions. Call this before Run or CreateSession.
func (w *Watcher) ConfigureDelegatedResources(owner string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.resources = newDelegatedResourceManager(owner)
	w.mu.Unlock()
}

func (w *Watcher) resourceManager() delegatedResourceManager {
	if w == nil {
		return noopDelegatedResourceManager{}
	}
	w.mu.RLock()
	manager := w.resources
	w.mu.RUnlock()
	if manager == nil {
		return noopDelegatedResourceManager{}
	}
	return manager
}

// SessionResourceSnapshot returns one on-demand read-only resource projection
// for agentID from the daemon-owned shared resource manager.
func (w *Watcher) SessionResourceSnapshot(agentID string) SessionResourceSnapshot {
	return w.resourceManager().Snapshot(strings.TrimSpace(agentID))
}

func (w *Watcher) delegatedSessionCount() int {
	if w == nil {
		return 0
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	count := 0
	for _, delegated := range w.delegated {
		if delegated {
			count++
		}
	}
	return count
}

// SetActivityProbe injects the provider-neutral activity probe used after
// progress/classification merge. Wire classifier.DefaultActivityProbe() (or a
// custom MultiActivityProbe) from daemon main — no package init registration.
func (w *Watcher) SetActivityProbe(probe classifier.ActivityProbe) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.activityProbe = probe
}

// SetProviderActivityProbe injects daemon/work's native Activity reader.
// Watcher correlates those facts to accepted Session input receipts; it does
// not parse provider lifecycle sources or maintain a second lifecycle truth.
func (w *Watcher) SetProviderActivityProbe(probe ProviderActivityProbe) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.providerActivityProbe = probe
}

// Events returns the channel on which state changes and output updates are sent.
func (w *Watcher) Events() <-chan SessionEvent {
	return w.events
}

// Agents returns a snapshot of all current agents.
func (w *Watcher) Agents() []*classifier.Agent {
	w.mu.RLock()
	defer w.mu.RUnlock()
	result := make([]*classifier.Agent, 0, len(w.agents))
	for _, id := range w.agentOrder {
		a, ok := w.agents[id]
		if !ok {
			continue
		}
		copy := *a
		result = append(result, &copy)
	}
	return result
}

// SnapshotReady reports whether Watcher has completed at least one full poll.
// It gates one-way migration from pre-Work delegated Session ownership.
func (w *Watcher) SnapshotReady() bool {
	if w == nil {
		return false
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.pollGeneration > 0
}

// GetAgent returns a snapshot of a single agent, or nil if not found.
func (w *Watcher) GetAgent(id string) *classifier.Agent {
	w.mu.RLock()
	defer w.mu.RUnlock()
	a, ok := w.agents[id]
	if !ok {
		return nil
	}
	copy := *a
	return &copy
}

// UpdateAgentProgress applies a control-plane lifecycle progress update to a
// known agent and emits the same state/metadata events used by watcher polling.
//
// For canonical-turn sessions the progress is a Control-class fact applied
// through the single reducer: running/attention renew or block the turn,
// done/failed are provisional hints that never change canonical status, and
// the lease fields refresh the projection only. For markerless sessions the
// legacy projection behavior is preserved.
func (w *Watcher) UpdateAgentProgress(id string, progress classifier.AgentProgress) (*classifier.Agent, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("missing agent id")
	}
	progress, err := classifier.ValidateProgress(progress)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	// A progress heartbeat can race the post-confirmation fresh-Turn commit.
	// Classify it from an authoritative read, refreshing the two-second poll
	// cache before deriving its TurnID, so the first heartbeat can never target
	// the superseded Turn.
	turn, hasCurrentTurn, turnErr := w.ledgerTurnAuthoritative(id, now)
	if turnErr != nil {
		return nil, turnErr
	}
	_, hasPendingSubmission, pendingErr := w.pendingTurnSubmission(id)
	if pendingErr != nil {
		return nil, pendingErr
	}
	appliedFact := false
	if hasCurrentTurn && !hasPendingSubmission && w.turnLedger != nil {
		if fact := controlFactFromProgress(id, turn.TurnID, progress, now); fact != nil {
			snapshot, changed, applyErr := w.turnLedger.ApplyTurnFact(*fact)
			if applyErr != nil {
				return nil, applyErr
			}
			appliedFact = changed
			if changed {
				turn = snapshot
			}
		}
	}
	var event SessionEvent
	var snapshot *classifier.Agent

	w.mu.Lock()
	agent, ok := w.agents[id]
	if !ok {
		w.mu.Unlock()
		return nil, fmt.Errorf("agent session not found")
	}
	oldState := agent.State
	classifier.ApplyProgress(agent, progress, now)
	if hasPendingSubmission && (!hasCurrentTurn || TurnTerminal(turn.Status)) {
		// Pending is canonical transaction state, not a Turn. It suppresses raw
		// Control terminal projection until provider admission resolves it, but
		// cannot mutate or replace the previous terminal Turn.
		clearStaleAttemptMetadata(agent)
		agent.State = classifier.StateRunning
		agent.Summary = "Delegated input is awaiting provider admission"
	} else if hasCurrentTurn {
		// The canonical turn owns the Session projection: status, attention,
		// and summary come from the ledger, and stale terminal-attempt
		// metadata (attention/phase/lease) never survives.
		w.ledgerTurns[id] = turn
		w.ledgerTurnReadAt[id] = now
		clearStaleAttemptMetadata(agent)
		newState, summary := projectDelegatedTurn(agent, turn)
		agent.State = newState
		agent.Summary = summary
		_ = appliedFact
	}
	snapshot = cloneAgent(agent)
	event = SessionEvent{
		Type:    "agent_metadata_change",
		AgentID: id,
		Agent:   snapshot,
		TurnID: func() string {
			if hasPendingSubmission && (!hasCurrentTurn || TurnTerminal(turn.Status)) {
				return ""
			}
			return turn.TurnID
		}(),
	}
	if oldState != agent.State {
		event.Type = "agent_state_change"
		event.OldState = string(oldState)
		event.NewState = string(agent.State)
	}
	w.mu.Unlock()

	w.events <- event
	return snapshot, nil
}

// controlFactFromProgress derives the Control-class fact for one progress
// submission. The caller-minted progress_event_id is the stable logical event
// identity (C.3.1): identical later heartbeats with distinct IDs are distinct
// facts that each renew the lease, while a transport retry reusing the same ID
// dedupes. The payload hash is audit metadata only, never identity.
func controlFactFromProgress(id, turnID string, progress classifier.AgentProgress, now time.Time) *TurnFact {
	progressEventID := strings.TrimSpace(progress.ProgressEventID)
	if progressEventID == "" {
		progressEventID = uuid.NewString()
	}
	base := TurnFact{
		SessionID:    id,
		TurnID:       turnID,
		Class:        EvidenceControl,
		At:           now,
		Summary:      strings.TrimSpace(progress.Summary),
		LeaseSeconds: progress.LeaseSeconds,
	}
	progressState := classifier.ProgressState(progress)
	switch progressState {
	case classifier.StateRunning:
		base.Kind = "running"
		base.SourceID = "control\x00" + progressEventID
		return &base
	case classifier.StateBlocked:
		base.Kind = "attention"
		base.SourceID = "control\x00" + progressEventID
		return &base
	case classifier.StateDone:
		base.Kind = "done"
		base.SourceID = "control\x00" + progressEventID
		return &base
	case classifier.StateFailed:
		base.Kind = "failed"
		base.SourceID = "control\x00" + progressEventID
		return &base
	default:
		return nil
	}
}

// RebindDelegatedTurnProjection re-reads the canonical ledger turn and rebinds
// the Session projection (list/capture) to it, clearing stale terminal
// metadata from a previous turn. The control app calls it immediately after a
// delegated dispatch returns — accepted or ambiguous — so a reused Session
// never inherits the previous turn's done projection while its new provider
// turn is live or admitted.
func (w *Watcher) RebindDelegatedTurnProjection(id string) (*classifier.Agent, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("missing agent id")
	}
	now := time.Now().UTC()
	// Rebind is the synchronous post-input boundary. It must observe the Turn
	// written by that transaction immediately, not the two-second poll cache.
	turn, hasTurn, err := w.ledgerTurnAuthoritative(id, now)
	if err != nil {
		return nil, err
	}
	_, hasPendingSubmission, pendingErr := w.pendingTurnSubmission(id)
	if pendingErr != nil {
		return nil, pendingErr
	}
	if !hasTurn && !hasPendingSubmission {
		return nil, fmt.Errorf("delegated Session %s has no canonical turn", id)
	}
	var event SessionEvent
	var snapshot *classifier.Agent
	w.mu.Lock()
	agent, ok := w.agents[id]
	if !ok {
		w.mu.Unlock()
		return nil, fmt.Errorf("agent session not found")
	}
	oldState := agent.State
	if hasTurn {
		w.ledgerTurns[id] = turn
		w.ledgerTurnReadAt[id] = now
	}
	clearStaleAttemptMetadata(agent)
	newState, summary := classifier.StateRunning, "Delegated input is awaiting provider admission"
	if hasTurn && (!hasPendingSubmission || !TurnTerminal(turn.Status)) {
		newState, summary = projectDelegatedTurn(agent, turn)
	}
	agent.State = newState
	agent.Summary = summary
	agent.UpdatedAt = now
	snapshot = cloneAgent(agent)
	event = SessionEvent{
		Type:    "agent_metadata_change",
		AgentID: id,
		Agent:   snapshot,
		TurnID: func() string {
			if hasPendingSubmission && (!hasTurn || TurnTerminal(turn.Status)) {
				return ""
			}
			return turn.TurnID
		}(),
	}
	if oldState != agent.State {
		event.Type = "agent_state_change"
		event.OldState = string(oldState)
		event.NewState = string(agent.State)
	}
	w.mu.Unlock()

	w.events <- event
	return snapshot, nil
}

// SessionPresence is the tri-state result of ProbeSession.
type SessionPresence int

const (
	// SessionPresenceUnknown means the probe failed; callers must not treat this
	// as proof of absence.
	SessionPresenceUnknown SessionPresence = iota
	SessionPresencePresent
	SessionPresenceAbsent
)

// ErrDelegatedResourceRelease means the tmux window is gone (or was already
// missing) but delegated resource cleanup failed and remains retryable.
var ErrDelegatedResourceRelease = errors.New("delegated resource release failed")

// HasSession reports whether tmux still has a session matching the target.
// Probe failures collapse to false for backward-compatible callers; prefer
// ProbeSession when absence must be proven.
func (w *Watcher) HasSession(target string) bool {
	presence, err := w.ProbeSession(target)
	return err == nil && presence == SessionPresencePresent
}

// ProbeSession reports whether tmux still has the target. Transport/probe
// failures return SessionPresenceUnknown with a non-nil error — never Absent.
func (w *Watcher) ProbeSession(target string) (SessionPresence, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return SessionPresenceAbsent, nil
	}
	probeTarget := target
	if !strings.Contains(target, ":") {
		if name := baseSessionName(target); name != "" {
			probeTarget = name
		}
	}
	// Known targets probe their recorded server (including a known user-server
	// target, which probes the user default ONLY); genuinely unknown targets
	// probe the daemon-namespaced server first (Zen-owned), then the user's
	// default server. A hard error on any probed server fails closed to
	// Unknown.
	sockets := []string{""}
	if w != nil {
		w.mu.RLock()
		ownership := w.targetSockets[target]
		daemon := w.daemonSocketPath
		w.mu.RUnlock()
		if ownership.known {
			sockets = []string{ownership.socket}
		} else {
			sockets = []string{daemon, ""}
		}
	}
	for _, socket := range sockets {
		out, err := tmuxCommand(socket, "has-session", "-t", probeTarget).CombinedOutput()
		if err == nil {
			return SessionPresencePresent, nil
		}
		if isTmuxTargetMissing(err, string(out)) || isNoTmuxServerError(err) || isNoTmuxServerError(fmt.Errorf("%s", out)) {
			continue
		}
		return SessionPresenceUnknown, fmt.Errorf("tmux has-session %s: %w: %s", probeTarget, err, strings.TrimSpace(string(out)))
	}
	return SessionPresenceAbsent, nil
}

// LegacyDelegatedTurnMarkers reads the raw pre-protocol @zen_delegated_turn
// options from the current tmux inventory for the one-shot ledger migration.
func (w *Watcher) LegacyDelegatedTurnMarkers() []LegacyDelegatedTurnMarker {
	windows, err := listTmuxWindowsFunc()
	if err != nil {
		return nil
	}
	markers := []LegacyDelegatedTurnMarker{}
	for _, win := range windows {
		if strings.TrimSpace(win.delegatedTurnRaw) != "" {
			markers = append(markers, LegacyDelegatedTurnMarker{
				Target: win.target,
				Raw:    win.delegatedTurnRaw,
			})
		}
	}
	return markers
}

// ClearDelegatedTurnMarkers unsets the migrated @zen_delegated_turn options.
// All later lifecycle writes go to the canonical ledger.
func (w *Watcher) ClearDelegatedTurnMarkers(targets []string) {
	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		_ = tmuxCommand(
			w.socketPathFor(target), "set-option", "-w", "-u", "-t", target,
			"@"+delegatedTurnOption,
		).Run()
	}
}

// ProbeProviderEvidence returns the current provider-native observation for a
// session. The brain service uses it during the legacy-marker reconciliation
// sweep (migration Phase 1b).
func (w *Watcher) ProbeProviderEvidence(sessionID string) (ProviderActivityObservation, bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ProviderActivityObservation{}, false, fmt.Errorf("missing session id")
	}
	agent := w.GetAgent(sessionID)
	if agent == nil {
		return ProviderActivityObservation{}, false, nil
	}
	w.mu.RLock()
	probe := w.providerActivityProbe
	w.mu.RUnlock()
	if probe == nil {
		return ProviderActivityObservation{}, false, nil
	}
	observation := probe.ObserveProviderActivity(*agent, time.Now().UTC())
	return observation, providerFactRelevant(observation), nil
}

// Run starts the polling loop. Blocks until context is cancelled.
func (w *Watcher) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			w.poll()
		}
	}
}

func (w *Watcher) poll() {
	windows, err := listTmuxWindowsFunc()
	if err != nil {
		if isNoTmuxServerError(err) {
			w.resourceManager().Reconcile(nil)
		}
		return
	}
	w.resourceManager().Reconcile(windows)
	processes := snapshotProcessesFunc()
	processSnapshotAt := time.Now()

	type paneObs struct {
		win        tmuxWindow
		content    string
		alive      bool
		deadStatus int
		lines      []string
	}
	observations := make([]paneObs, 0, len(windows))
	for _, win := range windows {
		// Tag ownership BEFORE the first capture of this poll: pane/readiness
		// reads resolve the target's own server (daemon-namespaced vs user
		// default), so the first observation already uses the right socket.
		w.mu.Lock()
		w.targetSockets[win.target] = targetSocket{known: true, socket: win.socket}
		w.mu.Unlock()
		content, alive, deadStatus := capturePaneContentFunc(win.target)
		observations = append(observations, paneObs{
			win:        win,
			content:    content,
			alive:      alive,
			deadStatus: deadStatus,
			lines:      strings.Split(content, "\n"),
		})
	}

	type preparedAgent struct {
		id                string
		epoch             int64
		agentSnap         classifier.Agent
		content           string
		lines             []string
		alive             bool
		deadStatus        int
		panePID           int
		classified        classifier.AgentState
		classifiedSummary string
		oldState          classifier.AgentState
		contentChanged    bool
		existed           bool
		exists            bool
		prev              string
		previousMetadata  agentMetadataSnapshot
		delegatedTurnRaw  string
		now               time.Time
	}

	w.mu.Lock()
	w.pollGeneration++
	generation := w.pollGeneration
	probe := w.activityProbe
	providerProbe := w.providerActivityProbe
	prepared := make([]preparedAgent, 0, len(observations))
	seen := make(map[string]bool, len(observations))

	for _, obs := range observations {
		win := obs.win
		seen[win.target] = true

		prev, existed := w.prevContent[win.target]
		contentChanged := obs.content != prev
		w.prevContent[win.target] = obs.content

		agent, exists := w.agents[win.target]
		if !exists {
			agent = &classifier.Agent{
				ID:   win.target,
				Name: formatAgentName(win.name, win.target),
			}
			w.agents[win.target] = agent
			w.agentOrder = append(w.agentOrder, win.target)
			// Rediscovered window: restore the durable Pi ownership binding
			// (@zen_agent_pi_session) recorded at session create. After a
			// daemon restart the provider process may rewrite its argv
			// (node-based Pi), so the tmux option is the only recoverable
			// record of an owned --session path; mergeAgentCommandOwnership
			// then preserves it exactly as it would for a never-restarted
			// daemon, and a provider switch still clears it.
			if flag, path, ok := DecodePiSessionBinding(win.piSessionBinding); ok {
				agent.Command = "pi " + flag + " " + shellQuoteForLaunch(path)
			}
		}
		previousMetadata := agentMetadataSnapshotFor(agent)
		if nextName := formatAgentName(win.name, win.target); nextName != "" {
			agent.Name = nextName
		}
		if w.hidden[win.target] || win.hidden || isBrainHostWindow(win.target, win.name) {
			w.hidden[win.target] = true
			agent.Hidden = true
		}
		agent.Cwd = win.cwd
		agent.Project = projectNameFromPath(win.cwd)
		detectedCommand, detectedStartedAt, detectedPID := detectAgentProcess(win.command, win.panePID, processes, processSnapshotAt)
		// Sub-second provider start evidence: ps lstart is whole-second
		// precision, so instance-ownership arms would compare against a
		// rounded start. The platform's precise derivation (Linux
		// /proc/<pid>/stat starttime, boot-relative clock ticks) refines the
		// detected provider start when it is consistent with the observation;
		// the guarded fallback keeps the observed value everywhere else.
		detectedStartedAt = refineProcessStartedAt(detectedStartedAt, detectedPID)
		agent.Command = mergeAgentCommandOwnership(agent.Command, detectedCommand)
		agent.StartedAt = detectedStartedAt
		agent.ProcessID = detectedPID
		if w.hidden[win.target] {
			agent.Hidden = true
		}
		if win.delegated {
			w.delegated[win.target] = true
		}
		agent.Delegated = (w.delegated[win.target] || win.delegated) && !agent.Hidden

		agent.PaneAlive = obs.alive
		agent.LastLines = lastN(obs.lines, 120)
		now := w.pollNowValue()
		agent.LastSeenAt = now

		oldState := agent.State
		classified, classifiedSummary := classifyPaneAndApplyProgressInvalidation(agent, obs.alive, obs.lines, now)

		w.agentEpoch[win.target] = generation
		prepared = append(prepared, preparedAgent{
			id:                win.target,
			epoch:             generation,
			agentSnap:         *agent,
			content:           obs.content,
			lines:             obs.lines,
			alive:             obs.alive,
			deadStatus:        obs.deadStatus,
			panePID:           win.panePID,
			classified:        classified,
			classifiedSummary: classifiedSummary,
			oldState:          oldState,
			contentChanged:    contentChanged,
			existed:           existed,
			exists:            exists,
			prev:              prev,
			previousMetadata:  previousMetadata,
			delegatedTurnRaw:  win.delegatedTurnRaw,
			now:               now,
		})
	}
	w.mu.Unlock()

	type probedAgent struct {
		preparedAgent
		activity classifier.ActivitySignal
		provider ProviderActivityObservation
		turn     TurnSnapshot
		hasTurn  bool
		turnErr  error
	}
	results := make([]probedAgent, 0, len(prepared))
	for _, item := range prepared {
		activity := classifier.ActivitySignal{}
		if probe != nil {
			toolChild := false
			if isCursorAgentCommand(item.agentSnap.Command) {
				toolChild = cursorToolChildActive(item.panePID, processes)
			}
			activity = probe.Infer(classifier.ActivityInput{
				Agent:           item.agentSnap,
				PaneContent:     item.content,
				ToolChildActive: toolChild,
			})
		}
		// Canonical-turn path: read the ledger snapshot and apply provider +
		// liveness facts through the single reducer. Pane/classifier activity
		// never terminalizes and never sets attention for turn-tracked
		// sessions; it only refreshes the projection. Unknown turns are still
		// probed so a later turn-bound Provider terminal can upgrade them.
		// applyPollFacts runs for every mutable ledger-tracked turn even with
		// a nil Provider probe: only the Provider observation is gated, so
		// liveness facts (abnormal exit, end-of-identity) always apply.
		turn, hasTurn, turnErr := w.ledgerTurnFor(item.id, item.now)
		pending, hasPending, pendingErr := w.pendingTurnSubmission(item.id)
		if pendingErr != nil {
			turnErr = pendingErr
		}
		provider := ProviderActivityObservation{}
		if providerProbe != nil && pendingErr == nil && (hasPending || (hasTurn && turnErr == nil && !TurnImmutable(turn.Status))) {
			provider = providerProbe.ObserveProviderActivity(item.agentSnap, item.now)
		}
		if hasPending && providerFactRelevant(provider) {
			if _, resolved := w.resolvePendingProviderAdmission(pending, provider, item.now); resolved {
				turn, hasTurn, turnErr = w.ledgerTurnAuthoritative(item.id, item.now)
			}
		}
		if hasTurn && turnErr == nil && !TurnImmutable(turn.Status) {
			turn = w.applyPollFacts(item.id, item.alive, item.deadStatus, item.now, turn, provider)
		}
		if providerProbe != nil && !hasPending && (!hasTurn || turnErr != nil || TurnImmutable(turn.Status)) {
			providerProbe.ForgetProviderActivity(item.id)
		}
		results = append(results, probedAgent{
			preparedAgent: item,
			activity:      activity,
			provider:      provider,
			turn:          turn,
			hasTurn:       hasTurn,
			turnErr:       turnErr,
		})
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	for _, r := range results {
		agent := w.agents[r.id]
		if agent == nil {
			continue
		}
		if w.agentEpoch[r.id] != r.epoch {
			continue
		}
		activity := r.activity
		if agent.Cwd != r.agentSnap.Cwd || agent.Command != r.agentSnap.Command {
			// Identity drifted while unlocked; drop provider signal rather than
			// applying pane/process evidence to the wrong session.
			activity = classifier.ActivitySignal{}
		}

		newState, summary := classifier.ResolveSessionStatus(agent, r.classified, r.classifiedSummary, r.now, activity)
		previousTurn, hadPreviousTurn := w.ledgerTurns[r.id]
		if r.hasTurn && r.turnErr == nil {
			// Ledger-recorded transcript binding is the truth for provider
			// evidence recovery; the tmux option is only an advisory cache.
			w.restoreTurnTranscriptBindingLocked(agent, r.turn)
		}
		if r.turnErr != nil {
			// Ledger read failure is transient; keep the last projection
			// instead of fabricating a terminal state.
		} else if r.hasTurn {
			w.ledgerTurns[r.id] = r.turn
			clearStaleAttemptMetadata(agent)
			newState, summary = projectDelegatedTurn(agent, r.turn)
		}
		agent.State = newState
		agent.Summary = summary
		if !r.exists {
			// First observation (fresh watcher, daemon restart, or a brand-new
			// pane): seed the activity time from provable evidence, never the
			// observation clock. Pre-existing Sessions rediscovered by one poll
			// must keep their real activity times, or they would all display
			// the same discovery instant.
			if seeded := sessionDiscoveryActivityTime(agent, r.turn, r.hasTurn, r.provider); !seeded.IsZero() {
				agent.UpdatedAt = seeded
			}
		} else if sessionActivityAdvanced(
			r.contentChanged && r.existed,
			r.oldState,
			newState,
			previousTurn,
			hadPreviousTurn,
			r.turn,
			r.hasTurn,
		) {
			agent.UpdatedAt = r.now
		}

		if !r.exists {
			w.events <- SessionEvent{
				Type:    "agent_discovered",
				AgentID: r.id,
				Agent:   cloneAgent(agent),
			}
		}

		if r.contentChanged && r.existed {
			w.events <- SessionEvent{
				Type:    "agent_output",
				AgentID: r.id,
				Agent:   cloneAgent(agent),
				Lines:   changedPaneLines(r.prev, r.content),
			}
		}

		if r.oldState != newState && r.existed {
			w.events <- SessionEvent{
				Type:     "agent_state_change",
				AgentID:  r.id,
				Agent:    cloneAgent(agent),
				OldState: string(r.oldState),
				NewState: string(newState),
				TurnID:   r.turn.TurnID,
			}
		}

		if r.exists && r.oldState == newState && agentMetadataChanged(r.previousMetadata, agent) {
			w.events <- SessionEvent{
				Type:    "agent_metadata_change",
				AgentID: r.id,
				Agent:   cloneAgent(agent),
			}
		}
	}

	for id := range w.agents {
		if !seen[id] {
			old := w.agents[id]
			turn, hasTurn := w.ledgerTurns[id]
			if hasTurn && !TurnImmutable(turn.Status) {
				// Positive identity disappearance (CR.3): the inventory
				// succeeded and the target is absent from it, so the recorded
				// pane/process identity is gone. End is not outcome: without a
				// readable bound Provider terminal this resolves to Unknown +
				// session.uncertain, never Failed. A bound terminal readable
				// at death decides first (C.2.4).
				w.resolveRemovedTurnFacts(id, *old, turn, providerProbe)
			}
			if providerProbe != nil {
				providerProbe.ForgetProviderActivity(id)
			}
			delete(w.agents, id)
			delete(w.prevContent, id)
			delete(w.hidden, id)
			delete(w.delegated, id)
			delete(w.agentEpoch, id)
			delete(w.ledgerTurns, id)
			delete(w.appliedFactIDs, id)
			delete(w.ledgerTurnReadAt, id)
			delete(w.probeLossSince, id)
			delete(w.targetSockets, id)
			archived := cloneAgent(old)
			if archived != nil {
				archived.State = classifier.StateRemoved
			}
			w.events <- SessionEvent{
				Type:     "agent_removed",
				AgentID:  id,
				Agent:    archived,
				OldState: string(old.State),
				NewState: string(classifier.StateRemoved),
				TurnID:   turn.TurnID,
			}
		}
	}
	w.compactAgentOrderLocked()
}

func (w *Watcher) compactAgentOrderLocked() {
	next := w.agentOrder[:0]
	for _, id := range w.agentOrder {
		if _, ok := w.agents[id]; ok {
			next = append(next, id)
		}
	}
	w.agentOrder = next
}

// restoreTurnTranscriptBindingLocked restores the ledger-recorded provider
// transcript binding onto the Session command (Pi owned --session/--session-dir
// path) and backfills a missing binding from the advisory tmux option / launch
// command, idempotently. The ledger is the durable truth; the tmux option is
// only an advisory cache for sessions without a ledger record.
func (w *Watcher) restoreTurnTranscriptBindingLocked(agent *classifier.Agent, turn TurnSnapshot) {
	if agent == nil || strings.TrimSpace(agent.ID) == "" {
		return
	}
	binding := turn.TranscriptBinding
	if binding.Empty() {
		// Backfill: the current launch command carries a Pi owned binding the
		// ledger lacks (turns admitted before the binding existed, or a
		// tmux-option recovery). Idempotent: the store only persists when the
		// binding is missing.
		if flag, path := piOwnedLaunchFlag(agent.Command); flag != "" && path != "" && w.turnLedger != nil {
			if backfiller, ok := w.turnLedger.(interface {
				BackfillTurnTranscriptBinding(string, TranscriptBinding) (bool, error)
			}); ok {
				_, _ = backfiller.BackfillTurnTranscriptBinding(agent.ID, TranscriptBinding{
					Provider: "pi", PiFlag: flag, PiPath: path,
				})
			}
		}
		return
	}
	if strings.TrimSpace(binding.Provider) != "pi" ||
		commandExecutableBase(agent.Command) != "pi" {
		return
	}
	owned := commandExecutableBase(agent.Command) + " " + binding.PiFlag + " " + shellQuoteForLaunch(binding.PiPath)
	if strings.TrimSpace(agent.Command) != owned {
		agent.Command = owned
	}
}

// projectDelegatedTurn projects the canonical ledger turn onto the Session
// (list/capture/close/Work all read this same canonical owner). Hints are
// attached notes only: they never change the status text.
func projectDelegatedTurn(agent *classifier.Agent, turn TurnSnapshot) (classifier.AgentState, string) {
	var state classifier.AgentState
	summary := strings.TrimSpace(turn.Summary)
	switch turn.Status {
	case TurnAdmitted:
		state = classifier.StateRunning
		summary = "Delegated input outcome pending; observing provider activity"
	case TurnAccepted:
		state = classifier.StateRunning
		if summary == "" {
			summary = "Delegated turn started"
		}
	case TurnRunning:
		state = classifier.StateRunning
		if summary == "" {
			summary = "Delegated turn running"
		}
	case TurnBlocked:
		state = classifier.StateBlocked
		if summary == "" {
			summary = "Delegated Session needs input"
		}
	case TurnDone:
		state = classifier.StateDone
		if summary == "" {
			summary = "Delegated turn completed"
		}
	case TurnFailed:
		state = classifier.StateFailed
		if summary == "" {
			summary = "Delegated turn failed"
		}
	case TurnUnknown:
		state = classifier.StateUnknown
		if summary == "" {
			summary = "Delegated Session outcome is unknown; inspect and reconcile"
		}
	default:
		state = classifier.StateUnknown
	}
	if agent != nil && turn.Status == TurnBlocked {
		agent.Attention = "user_input"
		agent.NeedsAttention = true
	} else if agent != nil {
		agent.Attention = "none"
		agent.NeedsAttention = false
	}
	return state, summary
}

// clearStaleAttemptMetadata removes terminal-attempt projection residue (a
// sticky done/failed attention, phase, event kind, or lease from a
// non-canonical Control report) while a canonical turn owns the Session.
// Control terminal reports are hints only; leaving their metadata attached to
// an Admitted/Accepted/Running turn would expose the impossible combination
// event_kind=done with canonical status=running. A live running/blocked lease
// is never cleared: it drives session.stale.
func clearStaleAttemptMetadata(agent *classifier.Agent) {
	if agent == nil {
		return
	}
	terminalAttempt := agent.Attention == "done" || agent.Attention == "failed" ||
		agent.State == classifier.StateDone || agent.State == classifier.StateFailed ||
		agent.EventKind == "done"
	if !terminalAttempt {
		return
	}
	agent.Attention = "none"
	agent.NeedsAttention = false
	agent.Phase = ""
	agent.TaskClass = ""
	agent.EventKind = ""
	agent.DetailsJSON = ""
	agent.LastProgressAt = nil
	agent.ExpectedNextCheckAt = nil
	agent.LeaseSeconds = 0
}

// ledgerTurnFor returns the canonical ledger snapshot for the session, re-
// reading the durable ledger at most every two seconds. The cache is a pure
// projection: every ApplyTurnFact result refreshes it, and every re-read
// refreshes from the authoritative record.
func (w *Watcher) ledgerTurnFor(sessionID string, now time.Time) (TurnSnapshot, bool, error) {
	if w == nil || w.turnLedger == nil {
		return TurnSnapshot{}, false, nil
	}
	w.mu.RLock()
	cached, hasCached := w.ledgerTurns[sessionID]
	readAt := w.ledgerTurnReadAt[sessionID]
	w.mu.RUnlock()
	if hasCached && now.Sub(readAt) < 2*time.Second {
		return cached, true, nil
	}
	turn, hasTurn, err := w.turnLedger.Turn(sessionID)
	if err != nil {
		return turn, hasTurn, err
	}
	if hasTurn {
		w.mu.Lock()
		w.ledgerTurns[sessionID] = turn
		w.ledgerTurnReadAt[sessionID] = now
		w.mu.Unlock()
	}
	return turn, hasTurn, nil
}

// ledgerTurnAuthoritative bypasses and refreshes the short poll projection
// cache. Transaction boundaries such as post-input rebind use this path so a
// freshly admitted/reused Turn is visible immediately.
func (w *Watcher) ledgerTurnAuthoritative(
	sessionID string,
	now time.Time,
) (TurnSnapshot, bool, error) {
	if w == nil || w.turnLedger == nil {
		return TurnSnapshot{}, false, nil
	}
	turn, hasTurn, err := w.turnLedger.Turn(sessionID)
	if err != nil {
		return turn, hasTurn, err
	}
	w.mu.Lock()
	if hasTurn {
		w.ledgerTurns[sessionID] = turn
		w.ledgerTurnReadAt[sessionID] = now
	} else {
		delete(w.ledgerTurns, sessionID)
		delete(w.ledgerTurnReadAt, sessionID)
	}
	w.mu.Unlock()
	return turn, hasTurn, nil
}

func (w *Watcher) pendingTurnSubmission(sessionID string) (TurnSubmission, bool, error) {
	if w == nil || w.turnLedger == nil {
		return TurnSubmission{}, false, nil
	}
	ledger, ok := w.turnLedger.(TurnSubmissionLedger)
	if !ok {
		return TurnSubmission{}, false, nil
	}
	submission, found, err := ledger.PendingTurnSubmission(sessionID)
	if err != nil {
		return TurnSubmission{}, false, err
	}
	return submission, found, nil
}

// resolvePendingProviderAdmission is the restart/crash recovery path for the
// same canonical submission transaction. It consumes only an exact provider
// admission tuple whose digest equals the pending payload; tmux receipt state
// is irrelevant to canonical ownership and input is never replayed here.
func (w *Watcher) resolvePendingProviderAdmission(
	submission TurnSubmission,
	provider ProviderActivityObservation,
	now time.Time,
) (TurnSubmission, bool) {
	if w == nil || w.turnLedger == nil {
		return TurnSubmission{}, false
	}
	ledger, ok := w.turnLedger.(TurnSubmissionLedger)
	if !ok {
		return TurnSubmission{}, false
	}
	identity, known := w.targetForSession(submission.SessionID)
	if !known || delegatedTurnIdentity(identity) != submission.ProcessIdentity ||
		w.currentPaneGeneration(submission.SessionID) != submission.PaneGeneration {
		return TurnSubmission{}, false
	}
	switch strings.TrimSpace(provider.Status) {
	case "running", "completed", "failed", "interrupted", "cancelled":
	default:
		return TurnSubmission{}, false
	}
	admission := admissionFromObservation(provider)
	if strings.TrimSpace(provider.ID) == "" || admission.Empty() ||
		strings.TrimSpace(admission.SHA256) != submission.PayloadSHA256 {
		return TurnSubmission{}, false
	}
	resolved, err := ledger.ResolveTurnSubmission(TurnSubmissionResolution{
		SessionID: submission.SessionID, ProposedTurnID: submission.ProposedTurnID,
		Receipt: submission.Receipt, PayloadSHA256: submission.PayloadSHA256,
		ActivityID: strings.TrimSpace(provider.ID), Admission: admission,
		ResolvedAt: now.UTC(),
	})
	return resolved, err == nil
}

// providerEvidenceLossWindow bounds a consecutive provider-evidence loss
// (transcript unlocatable/unreadable) before the watcher emits exactly one
// canonical session.uncertain for the current turn. It is deliberately
// generous so a healthy provider's pre-first-flush window (Pi session files
// appear at first write; OpenCode session rows commit with the first message)
// never fabricates uncertainty, while a genuinely lost source stays bounded.
const providerEvidenceLossWindow = 90 * time.Second

// applyPollFacts applies one poll's provider + liveness observations through
// the single canonical reducer and returns the latest snapshot. It runs for
// every mutable ledger-tracked turn regardless of whether a Provider probe
// is installed: only the Provider observation is gated, so liveness facts
// always reach the reducer.
//
// Liveness-derived terminal attribution is deliberately absent (Round 4):
// production tmux primitives cannot prove that a dead pane's exit status
// belongs to the exact recorded process lifetime — wrapper/shell panes can
// propagate a replaced child's status, nil/empty/unreadable process
// snapshots prove nothing, and dead-pane identity reads fail closed. A dead
// pane therefore always resolves end-of-identity Unknown + session.uncertain
// exactly once, never Failed; only a bound Provider terminal fact may decide
// Failed.
func (w *Watcher) applyPollFacts(
	id string,
	alive bool,
	deadStatus int,
	now time.Time,
	turn TurnSnapshot,
	provider ProviderActivityObservation,
) TurnSnapshot {
	ledger := w.turnLedger
	if ledger == nil || TurnImmutable(turn.Status) {
		return turn
	}
	provider = providerObservationForTurn(provider, turn)
	facts := []TurnFact{}
	if providerFactRelevant(provider) {
		if fact := admissionFactFromObservation(id, turn, provider); fact != nil {
			facts = append(facts, *fact)
		}
		if fact := activityFactFromObservation(id, turn, provider); fact != nil {
			facts = append(facts, *fact)
		}
	}
	// Bounded provider-evidence loss (transcript unlocatable/unreadable): a
	// successful read with no new fact is never a loss; only a provably lost
	// source drives the canonical session.uncertain, exactly once per turn
	// (deterministic FactID). A healthy recovery before the window resets the
	// streak. The reducer ignores the fact for non-current turns and upgrades
	// Unknown monotonically when a bound terminal becomes readable (C.2.4).
	w.mu.Lock()
	if provider.ProbeState.Loss() {
		state, ok := w.probeLossSince[id]
		if !ok || state.turnID != turn.TurnID || state.since.IsZero() {
			// A new current turn starts a new evidence-loss streak; the
			// predecessor's loss can never make this turn immediately uncertain.
			state = probeLossState{turnID: turn.TurnID, since: now}
			w.probeLossSince[id] = state
		} else if now.Sub(state.since) >= providerEvidenceLossWindow {
			facts = append(facts, TurnFact{
				SessionID: id,
				TurnID:    turn.TurnID,
				Class:     EvidenceProvider,
				Kind:      "uncertain",
				SourceID:  "provider-loss\x00" + turn.TurnID,
				At:        now,
				Summary:   "Delegated Session provider evidence is unreadable; outcome is unknown",
			})
		}
	} else {
		delete(w.probeLossSince, id)
	}
	w.mu.Unlock()
	if !alive {
		if deadStatus >= 0 {
			// A dead pane with a readable exit status still cannot attribute
			// that status to the exact recorded process lifetime: the pane
			// root may be a wrapper that propagated a replaced child's exit,
			// the process snapshot may be nil/empty/unreadable (a missing PID
			// proves nothing), and dead-pane identity reads fail closed in
			// production. The liveness-derived Failed path is removed
			// entirely: end-of-identity resolves Unknown + session.uncertain,
			// exactly once; only a bound Provider terminal may decide Failed.
			facts = append(facts, TurnFact{
				SessionID:   id,
				TurnID:      turn.TurnID,
				Class:       EvidenceLiveness,
				Kind:        "uncertain",
				ProcessDead: true,
				SourceID:    "liveness\x00" + turn.ProcessIdentity + "\x00process-dead",
				At:          now,
				Summary:     "Delegated provider process exited; outcome is unknown",
			})
		}
		// PaneAbsent (no dead status readable): transient absence never
		// terminalizes (CR.3).
	} else if generation := w.currentPaneGeneration(id); generation != "" &&
		strings.TrimSpace(turn.PaneGeneration) != "" && generation != turn.PaneGeneration {
		// A different live pane identity owns the target: the recorded
		// process is provably gone (CR.3 SessionReplaced).
		facts = append(facts, TurnFact{
			SessionID:       id,
			TurnID:          turn.TurnID,
			Class:           EvidenceLiveness,
			Kind:            "uncertain",
			SessionReplaced: true,
			SourceID:        "liveness\x00" + turn.ProcessIdentity + "\x00session-replaced",
			At:              now,
			Summary:         "Delegated Session was replaced; outcome is unknown",
		})
	}
	for _, fact := range facts {
		factID := fact.TurnFactIDFor()
		w.mu.RLock()
		applied := w.appliedFactIDs[id] == factID
		w.mu.RUnlock()
		if applied {
			continue
		}
		snapshot, changed, err := ledger.ApplyTurnFact(fact)
		if err != nil {
			continue
		}
		w.mu.Lock()
		w.appliedFactIDs[id] = factID
		if changed {
			w.ledgerTurns[id] = snapshot
			w.ledgerTurnReadAt[id] = now
			turn = snapshot
		}
		w.mu.Unlock()
	}
	return turn
}

// resolveRemovedTurnFacts applies a removed session's provider facts (a bound
// terminal readable at death decides first) and then the end-of-identity
// liveness fact, per the same canonical reducer. The recorded identity is
// considered gone because the target is absent from a successful inventory.
// Unknown turns are still probed so a readable bound terminal upgrades them.
func (w *Watcher) resolveRemovedTurnFacts(
	id string,
	agent classifier.Agent,
	turn TurnSnapshot,
	probe ProviderActivityProbe,
) {
	if w == nil || w.turnLedger == nil || TurnImmutable(turn.Status) {
		return
	}
	now := time.Now().UTC()
	provider := ProviderActivityObservation{}
	if probe != nil {
		provider = probe.ObserveProviderActivity(agent, now)
	}
	provider = providerObservationForTurn(provider, turn)
	if providerFactRelevant(provider) {
		if fact := admissionFactFromObservation(id, turn, provider); fact != nil {
			_, _, _ = w.turnLedger.ApplyTurnFact(*fact)
		}
		if fact := activityFactFromObservation(id, turn, provider); fact != nil {
			_, _, _ = w.turnLedger.ApplyTurnFact(*fact)
		}
	}
	// ProcessDead, no bound terminal readable: Unknown + session.uncertain.
	_, _, _ = w.turnLedger.ApplyTurnFact(TurnFact{
		SessionID:   id,
		TurnID:      turn.TurnID,
		Class:       EvidenceLiveness,
		Kind:        "uncertain",
		ProcessDead: true,
		SourceID:    "liveness\x00" + turn.ProcessIdentity + "\x00process-dead",
		At:          now,
		Summary:     "Delegated Session disappeared; outcome is unknown",
	})
}

func providerFactRelevant(provider ProviderActivityObservation) bool {
	return strings.TrimSpace(provider.ID) != "" ||
		strings.TrimSpace(provider.AdmissionID) != "" ||
		!provider.StartedAt.IsZero()
}

// providerObservationForTurn selects the exact activity identity already
// recorded by the canonical turn. Reusable provider sessions expose only
// their latest Activity as the ordinary projection, but the bounded terminal
// metadata from the same authoritative source can safely settle an older
// stuck turn when — and only when — its full ActivityID matches. Admission
// fields are cleared for a historical selection because they describe the
// source's latest input, not the selected older activity.
func providerObservationForTurn(
	provider ProviderActivityObservation,
	turn TurnSnapshot,
) ProviderActivityObservation {
	activityID := strings.TrimSpace(turn.ActivityID)
	if activityID == "" || strings.TrimSpace(provider.ID) == activityID {
		return provider
	}
	for index := len(provider.TerminalActivities) - 1; index >= 0; index-- {
		terminal := provider.TerminalActivities[index]
		if strings.TrimSpace(terminal.ID) != activityID {
			continue
		}
		provider.ID = strings.TrimSpace(terminal.ID)
		provider.Status = strings.TrimSpace(terminal.Status)
		provider.StartedAt = terminal.StartedAt.UTC()
		provider.SettledAt = terminal.SettledAt.UTC()
		provider.AdmissionStream = ""
		provider.AdmissionID = ""
		provider.AdmissionCursor = 0
		provider.AdmissionAt = time.Time{}
		provider.InputSHA256 = ""
		return provider
	}
	return provider
}

// admissionFactFromObservation derives the admission-correlated fact for an
// Admitted/Accepted turn with no recorded provider identity. The admission
// tuple must start inside the turn's admission window (C.6).
func admissionFactFromObservation(sessionID string, turn TurnSnapshot, provider ProviderActivityObservation) *TurnFact {
	if turn.Status != TurnAdmitted && turn.Status != TurnAccepted {
		return nil
	}
	if turn.HasAdmission || strings.TrimSpace(provider.AdmissionID) == "" {
		return nil
	}
	if !provider.StartedAt.IsZero() && provider.StartedAt.Before(turn.AcceptedAt) {
		return nil
	}
	return &TurnFact{
		SessionID:  sessionID,
		TurnID:     turn.TurnID,
		Class:      EvidenceProvider,
		Kind:       "admission",
		SourceID:   fmt.Sprintf("provider\x00%s\x00%s\x00%s\x00%d", sessionID, strings.TrimSpace(provider.AdmissionStream), strings.TrimSpace(provider.AdmissionID), provider.AdmissionCursor),
		Cursor:     provider.AdmissionCursor,
		Admission:  admissionFromObservation(provider),
		ActivityID: strings.TrimSpace(provider.ID),
		StartedAt:  provider.StartedAt,
		At:         time.Now().UTC(),
		Summary:    "Provider admitted the delegated input",
	}
}

// activityFactFromObservation derives the provider activity fact (running /
// done / failed). The source identity is the adapter's native durable
// activity identity plus its monotone cursor, so the deterministic FactID
// dedupes across restart and reorder.
func activityFactFromObservation(sessionID string, turn TurnSnapshot, provider ProviderActivityObservation) *TurnFact {
	kind := ""
	switch strings.TrimSpace(provider.Status) {
	case "running":
		kind = "running"
	case "completed":
		kind = "done"
	case "failed", "interrupted", "cancelled":
		kind = "failed"
	default:
		return nil
	}
	return &TurnFact{
		SessionID:  sessionID,
		TurnID:     turn.TurnID,
		Class:      EvidenceProvider,
		Kind:       kind,
		SourceID:   providerFactSourceID(sessionID, provider),
		Cursor:     provider.AdmissionCursor,
		Admission:  admissionFromObservation(provider),
		ActivityID: strings.TrimSpace(provider.ID),
		StartedAt:  provider.StartedAt,
		SettledAt:  provider.SettledAt,
		At:         time.Now().UTC(),
		Summary:    "Delegated provider activity " + strings.TrimSpace(provider.Status),
	}
}

func (w *Watcher) currentPaneGeneration(sessionID string) string {
	// The test-only poll source seam can supply the pane generation directly
	// so end-to-end tests exercise the identity gate without tmux.
	w.mu.RLock()
	sources := w.pollSources
	w.mu.RUnlock()
	if sources != nil && sources.PaneGeneration != nil {
		return sources.PaneGeneration(sessionID)
	}
	owner := w.sessionInputOwner()
	if owner == nil {
		return ""
	}
	socket := owner.ioSocket(sessionID)
	return owner.io.pane(socket, sessionID).generation
}

// sessionActivityAdvanced reports whether a poll apply advanced meaningful
// Session activity: pane content change, status transition, or a delegated
// turn appearing / changing identity / settling. Repeated no-op observations
// (same content, same state, same turn) never advance it. First observations
// are handled by sessionDiscoveryActivityTime instead.
func sessionActivityAdvanced(
	contentChanged bool,
	oldState, newState classifier.AgentState,
	previousTurn TurnSnapshot,
	hadPreviousTurn bool,
	turn TurnSnapshot,
	hasTurn bool,
) bool {
	if contentChanged || oldState != newState {
		return true
	}
	return hasTurn && (!hadPreviousTurn ||
		previousTurn.TurnID != turn.TurnID ||
		previousTurn.Status != turn.Status)
}

// sessionDiscoveryActivityTime returns the most trustworthy real activity
// time available when a Session is first observed (fresh watcher, daemon
// restart, or a brand-new pane). It never returns observation/poll time.
// Sources, latest wins: delegated turn settlement, turn acceptance, last
// structured progress, authoritative provider activity, process start time.
func sessionDiscoveryActivityTime(
	agent *classifier.Agent,
	turn TurnSnapshot,
	hasTurn bool,
	provider ProviderActivityObservation,
) time.Time {
	var latest time.Time
	if agent != nil {
		if agent.LastProgressAt != nil && agent.LastProgressAt.After(latest) {
			latest = *agent.LastProgressAt
		}
		if agent.StartedAt.After(latest) {
			latest = agent.StartedAt
		}
	}
	if hasTurn {
		if turn.SettledAt != nil && turn.SettledAt.After(latest) {
			latest = *turn.SettledAt
		}
		if turn.AcceptedAt.After(latest) {
			latest = turn.AcceptedAt
		}
	}
	if provider.StartedAt.After(latest) {
		latest = provider.StartedAt
	}
	if provider.SettledAt.After(latest) {
		latest = provider.SettledAt
	}
	if latest.IsZero() {
		return time.Time{}
	}
	return latest.UTC()
}

func (w *Watcher) pollNowValue() time.Time {
	if w != nil && w.pollNow != nil {
		return w.pollNow().UTC()
	}
	return time.Now().UTC()
}

func changedPaneLines(previous, next string) []string {
	previousLines := strings.Split(previous, "\n")
	nextLines := strings.Split(next, "\n")
	if len(nextLines) > len(previousLines) && linesEqual(previousLines, nextLines[:len(previousLines)]) {
		return nextLines[len(previousLines):]
	}
	return lastN(nextLines, 12)
}

func linesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func cloneAgent(agent *classifier.Agent) *classifier.Agent {
	if agent == nil {
		return nil
	}
	cp := *agent
	if agent.LastLines != nil {
		cp.LastLines = append([]string(nil), agent.LastLines...)
	}
	return &cp
}

type agentMetadataSnapshot struct {
	name                string
	project             string
	cwd                 string
	command             string
	processID           int
	hidden              bool
	phase               string
	attention           string
	taskClass           string
	eventKind           string
	detailsJSON         string
	needsAttention      bool
	lastProgressAt      int64
	expectedNextCheckAt int64
	leaseSeconds        int
	delegated           bool
}

func agentMetadataSnapshotFor(agent *classifier.Agent) agentMetadataSnapshot {
	if agent == nil {
		return agentMetadataSnapshot{}
	}
	return agentMetadataSnapshot{
		name:                agent.Name,
		project:             agent.Project,
		cwd:                 agent.Cwd,
		command:             agent.Command,
		processID:           agent.ProcessID,
		hidden:              agent.Hidden,
		phase:               agent.Phase,
		attention:           agent.Attention,
		taskClass:           agent.TaskClass,
		eventKind:           agent.EventKind,
		detailsJSON:         agent.DetailsJSON,
		needsAttention:      agent.NeedsAttention,
		leaseSeconds:        agent.LeaseSeconds,
		delegated:           agent.Delegated,
		lastProgressAt:      unixNanoOrZero(agent.LastProgressAt),
		expectedNextCheckAt: unixNanoOrZero(agent.ExpectedNextCheckAt),
	}
}

func agentMetadataChanged(previous agentMetadataSnapshot, agent *classifier.Agent) bool {
	return previous != agentMetadataSnapshotFor(agent)
}

func unixNanoOrZero(value *time.Time) int64 {
	if value == nil {
		return 0
	}
	return value.UnixNano()
}

// classifyPaneAndApplyProgressInvalidation is the poll classify step: classify
// pane text, then clear progress metadata only for authoritative terminal
// outcomes (blocked always; failed unless explicit progress protects).
func classifyPaneAndApplyProgressInvalidation(agent *classifier.Agent, alive bool, lines []string, now time.Time) (classifier.AgentState, string) {
	agent.PaneAlive = alive
	classified, summary := classifier.Classify(alive, lines, agent.Command)
	if classified == classifier.StateBlocked ||
		(classified == classifier.StateFailed && !classifier.ExplicitProgressProtectsAgainstPaneFailed(agent, now)) {
		agent.LastProgressAt = nil
		agent.ExpectedNextCheckAt = nil
		agent.LeaseSeconds = 0
	}
	return classified, summary
}

func isBrainHostWindow(target, windowName string) bool {
	sessionName, _, ok := strings.Cut(strings.TrimSpace(target), ":")
	if !ok {
		return false
	}
	return strings.HasPrefix(sessionName, "brain-agent-brain-") && strings.TrimSpace(windowName) == "Brain"
}

// tmuxWindow represents a single tmux window target.
type tmuxWindow struct {
	target           string // "session:window_id" — stable tmux target usable as -t
	name             string // window name (e.g. "claude", "node")
	cwd              string // active pane cwd
	command          string // active pane command
	piSessionBinding string // durable Pi ownership binding (@zen_agent_pi_session)
	panePID          int
	hidden           bool
	delegated        bool
	resourceUnit     string
	delegatedTurnRaw string
	socket           string // tmux server socket path ("" = user default server)
}

// listTmuxWindows inventories the daemon-namespaced server (Zen-owned Brain
// and delegated Sessions) and the user's default server (manual Terminal
// Sessions) without mixing ownership: every window is tagged with the socket
// it lives on. A missing server on either socket is an empty inventory for
// that socket, never an error — removal reconciliation owns vanished
// sessions.
func (w *Watcher) listTmuxWindows() ([]tmuxWindow, error) {
	if w == nil {
		return listTmuxWindowsOn("")
	}
	w.mu.RLock()
	daemon := w.daemonSocketPath
	w.mu.RUnlock()
	windows := []tmuxWindow{}
	var hardErr error
	// Daemon server first, then the user default server. Same-name targets
	// across servers are resolved deterministically: the daemon-namespaced
	// (Zen-owned) entry shadows the user entry, so a user session that
	// collides with a Zen-owned name is never misrouted to the user server.
	seen := make(map[string]bool)
	for _, socket := range []string{daemon, ""} {
		onSocket, err := listTmuxWindowsOn(socket)
		if err != nil {
			if isNoTmuxServerError(err) {
				continue
			}
			if hardErr == nil {
				hardErr = err
			}
			continue
		}
		for _, win := range onSocket {
			if seen[win.target] {
				continue
			}
			seen[win.target] = true
			windows = append(windows, win)
		}
	}
	return windows, hardErr
}

func listTmuxWindowsOn(socket string) ([]tmuxWindow, error) {
	cmd := tmuxCommand(socket, "list-windows", "-a", "-F", "#{session_name}:#{window_id}\t#{window_name}\t#{pane_current_path}\t#{pane_current_command}\t#{pane_pid}\t#{@zen_agent_hidden}\t#{@zen_agent_delegated}\t#{@zen_agent_resource_unit}\t#{@zen_delegated_turn}\t#{@zen_agent_pi_session}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("tmux list-windows: %w: %s", err, strings.TrimSpace(string(out)))
	}
	var windows []tmuxWindow
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 10)
		target := parts[0]
		// Skip grouped sessions created by the terminal backend (zen-<pid>-<counter>).
		sessionName := strings.SplitN(target, ":", 2)[0]
		if strings.HasPrefix(sessionName, "zen-") {
			continue
		}
		name := target
		if len(parts) >= 2 {
			name = parts[1]
		}
		cwd := ""
		if len(parts) >= 3 {
			cwd = strings.TrimSpace(parts[2])
		}
		command := ""
		if len(parts) >= 4 {
			command = strings.TrimSpace(parts[3])
		}
		panePID := 0
		if len(parts) >= 5 {
			panePID, _ = strconv.Atoi(strings.TrimSpace(parts[4]))
		}
		hidden := false
		if len(parts) >= 6 {
			hidden = tmuxBoolOption(parts[5])
		}
		delegated := false
		if len(parts) >= 7 {
			delegated = tmuxBoolOption(parts[6])
		}
		resourceUnit := ""
		if len(parts) >= 8 {
			resourceUnit = strings.TrimSpace(parts[7])
		}
		delegatedTurnRaw := ""
		if len(parts) >= 9 {
			delegatedTurnRaw = strings.TrimSpace(parts[8])
		}
		piSessionBinding := ""
		if len(parts) >= 10 {
			piSessionBinding = strings.TrimSpace(parts[9])
		}
		windows = append(windows, tmuxWindow{
			target: target, name: name, cwd: cwd, command: command,
			piSessionBinding: piSessionBinding,
			panePID:          panePID, hidden: hidden, delegated: delegated,
			resourceUnit: resourceUnit, delegatedTurnRaw: delegatedTurnRaw,
			socket: socket,
		})
	}
	return windows, nil
}

func tmuxBoolOption(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// capturePaneContent captures the visible content of a tmux window's active
// pane on the target's server socket. The second result reports pane
// liveness; the third is the recorded pane exit status (#{pane_dead_status})
// when the pane is dead, or -1 when unknown. Exit status is authoritative
// abnormal-exit evidence only for the recorded pane identity; absence of the
// pane is never death by itself.
func (w *Watcher) capturePaneContent(target string) (string, bool, int) {
	if w == nil {
		return "", false, -1
	}
	return capturePaneContentOn(w.socketPathFor(target), target)
}

func capturePaneContentOn(socket, target string) (string, bool, int) {
	cmd := tmuxCommand(socket, "capture-pane", "-t", target, "-p", "-S", "-200")
	out, err := cmd.Output()
	if err != nil {
		return "", false, -1
	}

	cmdAlive := tmuxCommand(socket, "list-panes", "-t", target, "-F", "#{pane_dead}\t#{pane_dead_status}")
	aliveOut, err := cmdAlive.Output()
	alive := true
	deadStatus := -1
	if err == nil {
		fields := strings.Split(strings.TrimSpace(string(aliveOut)), "\t")
		if len(fields) >= 1 && fields[0] == "1" {
			alive = false
			if len(fields) >= 2 && strings.TrimSpace(fields[1]) != "" {
				if status, parseErr := strconv.Atoi(strings.TrimSpace(fields[1])); parseErr == nil {
					deadStatus = status
				}
			}
		}
	}

	return string(out), alive, deadStatus
}

// CapturePaneContent returns a plain-text snapshot of a tmux window's active pane.
func (w *Watcher) CapturePaneContent(sessionID string) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", fmt.Errorf("missing session id")
	}
	out, err := tmuxCommand(w.socketPathFor(sessionID), "capture-pane", "-t", sessionID, "-p", "-S", "-200").Output()
	if err != nil {
		return "", fmt.Errorf("capture pane: %w", err)
	}
	text := string(out)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text, nil
}

// SendKey sends a single tmux key to a window.
func (w *Watcher) SendKey(sessionID, key string) error {
	sessionID = strings.TrimSpace(sessionID)
	key = strings.TrimSpace(key)
	if sessionID == "" {
		return fmt.Errorf("missing session id")
	}
	if key == "" {
		return fmt.Errorf("missing key")
	}
	if !allowedTmuxKey(key) {
		return fmt.Errorf("unsupported key %q", key)
	}
	identity, known := w.targetForSession(sessionID)
	if !known {
		return fmt.Errorf("target provider could not be proven; key was not sent")
	}
	resolver := w.targetForSession
	action := func() error {
		if err := guardTargetIdentity(resolver, sessionID, identity); err != nil {
			return err
		}
		return tmuxCommand(w.socketPathFor(sessionID), "send-keys", "-t", sessionID, key).Run()
	}
	return w.sessionInputOwner().serialized(sessionID, action)
}

func allowedTmuxKey(key string) bool {
	if len(key) == 1 && allowedLiteralKeyByte(key[0]) {
		return true
	}
	switch key {
	case "Enter", "Escape", "Up", "Down", "Left", "Right", "Tab", "BTab", "Space":
		return true
	default:
		return false
	}
}

func allowedLiteralKeyByte(key byte) bool {
	return (key >= '1' && key <= '9') ||
		(key >= 'a' && key <= 'z') ||
		(key >= 'A' && key <= 'Z')
}

// SendInput sends text to a tmux window and treats trailing newlines as submit.
func (w *Watcher) SendInput(sessionID, text string) error {
	identity, known := w.targetForSession(sessionID)
	if !known {
		return definitelyNotSubmitted("", fmt.Errorf("target provider could not be proven"))
	}
	resolver := w.targetForSession
	body, submit := splitSubmitInput(text)
	if submit && body != "" {
		_, err := w.sessionInputOwner().submit(
			sessionID,
			identity,
			resolver,
			identity.Command,
			body,
			"",
		)
		return err
	}
	return w.sessionInputOwner().serialized(sessionID, func() error {
		if err := guardTargetIdentity(resolver, sessionID, identity); err != nil {
			return definitelyNotSubmitted("", err)
		}
		return sendDraftInputLocked(w.socketPathFor(sessionID), sessionID, text, tmuxSubmitDelay(identity.Command), nil)
	})
}

// SendInputWithReceipt submits through the shared Session Input owner and
// records the stable receiver receipt after the provider submit key.
func (w *Watcher) SendInputWithReceipt(sessionID, text, receipt string) error {
	_, err := w.SendInputWithReceiptResult(sessionID, text, receipt)
	return err
}

func (w *Watcher) SendInputWithReceiptResult(sessionID, text, receipt string) (InputResult, error) {
	receipt = strings.TrimSpace(receipt)
	if receipt == "" {
		return InputResult{Outcome: InputNotSubmitted}, definitelyNotSubmitted("", fmt.Errorf("input receipt is required"))
	}
	identity, known := w.targetForSession(sessionID)
	if !known {
		err := definitelyNotSubmitted(receipt, fmt.Errorf("target provider could not be proven"))
		return InputResult{Outcome: InputNotSubmitted, Receipt: receipt}, err
	}
	if text == "" {
		err := definitelyNotSubmitted(receipt, fmt.Errorf("receipt input payload is empty"))
		return InputResult{Outcome: InputNotSubmitted, Receipt: receipt}, err
	}
	return w.sessionInputOwner().submit(
		sessionID,
		identity,
		w.targetForSession,
		identity.Command,
		text,
		receipt,
	)
}

// InputReceiptResult reads the existing Session Input receipt ledger without
// submitting or reconstructing the original payload. A missing receipt is not
// proof that a prior provider mutation was definitely unsent.
func (w *Watcher) InputReceiptResult(sessionID, receipt string) (InputResult, bool, error) {
	receipt = strings.TrimSpace(receipt)
	if receipt == "" {
		return InputResult{Outcome: InputNotSubmitted}, false, fmt.Errorf("input receipt is required")
	}
	identity, known := w.targetForSession(sessionID)
	if !known {
		return InputResult{Outcome: InputNotSubmitted, Receipt: receipt}, false, fmt.Errorf("target provider could not be proven")
	}
	return w.sessionInputOwner().receiptOutcome(
		sessionID,
		identity,
		w.targetForSession,
		receipt,
	)
}

// SendInputWhenReady waits for a newly started agent UI to be ready, then sends
// text. Unknown executors are treated as ready immediately. Known Codex, Cursor,
// Claude, Grok, Pi, and OpenCode UIs must reach an input prompt so Zen does not
// paste a task into a startup screen before the composer can accept Enter-to-send.
func (w *Watcher) SendInputWhenReady(sessionID, command, text string) error {
	return w.sendInputWhenReadyAttempt(sessionID, command, text, inputReadyTimeout(command))
}

// inputReadyRetryInterval is the pause between bounded readiness attempts. The
// attempts themselves poll the pane far more often; the pause only spaces out
// full per-attempt timeouts.
const inputReadyRetryInterval = 250 * time.Millisecond

// SendInputWhenReadyBudgeted bounds the initial delegated handoff for one
// scheduled occurrence. The exact spawned provider input surface must become
// attributable and ready within budget; a definitely-not-submitted attempt
// (ErrAgentInputNotReady) may retry within the same budget while the spawned
// identity stays attributable. Ambiguous admission or loss of the spawned
// identity (the session ended without notification) fails closed immediately
// and is never replayed blindly. A non-positive budget keeps the legacy
// single-attempt behavior.
func (w *Watcher) SendInputWhenReadyBudgeted(sessionID, command, text string, budget time.Duration) error {
	if budget <= 0 {
		return w.SendInputWhenReady(sessionID, command, text)
	}
	deadline := w.admissionNowValue().Add(budget)
	for {
		timeout := inputReadyTimeout(command)
		if remaining := deadline.Sub(w.admissionNowValue()); remaining < timeout {
			timeout = remaining
		}
		err := w.sendInputWhenReadyAttempt(sessionID, command, text, timeout)
		if err == nil || !errors.Is(err, ErrAgentInputNotReady) {
			return err
		}
		if !w.admissionNowValue().Before(deadline) {
			return err
		}
		w.admissionSleepValue(inputReadyRetryInterval)
	}
}

// sendInputWhenReadyAttempt is one bounded ready-and-submit attempt against the
// exact spawned target identity. Identity attribution is capped at the
// adapter's per-attempt window so a session that ended without notification
// fails fast instead of consuming the whole occurrence budget; the readiness
// wait then uses the remainder. Only a readiness timeout returns
// ErrAgentInputNotReady (retryable); an unprovable or replaced identity and any
// ambiguous submission are terminal for the occurrence.
func (w *Watcher) sendInputWhenReadyAttempt(
	sessionID, command, text string,
	timeout time.Duration,
) error {
	resolver := w.targetForSession
	identityTimeout := timeout
	if perAttempt := inputReadyTimeout(command); perAttempt < identityTimeout {
		identityTimeout = perAttempt
	}
	identity, known := resolveTargetIdentityWhenReadyTimeout(resolver, sessionID, command, identityTimeout)
	if !known {
		return definitelyNotSubmitted("", fmt.Errorf("target provider could not be proven"))
	}
	command = identity.Command
	guard := func() error {
		return guardTargetIdentity(resolver, sessionID, identity)
	}
	if !waitForInputReadyGuarded(w.socketPathFor(sessionID), sessionID, command, timeout, guard) &&
		needsInputReadinessWait(command, "") {
		return agentInputNotReady(command)
	}
	body, submit := splitSubmitInput(text)
	if submit && body != "" {
		_, err := w.sessionInputOwner().submit(sessionID, identity, resolver, command, body, "")
		return err
	}
	return w.sessionInputOwner().serialized(sessionID, func() error {
		if err := guard(); err != nil {
			return definitelyNotSubmitted("", err)
		}
		// The non-submit draft send is a server-local mutation: route it
		// through the target's own tmux server exactly like the submit path.
		return sendDraftInputLocked(w.socketPathFor(sessionID), sessionID, text, tmuxSubmitDelay(command), nil)
	})
}

// SubmitInputWhenReady submits payload as a structured action. Unlike the
// legacy terminal-text boundary, payload never contains or loses a transport
// delimiter; caller-owned final line endings remain payload bytes.
func (w *Watcher) SubmitInputWhenReady(sessionID, command, payload string) error {
	resolver := w.targetForSession
	identity, known := resolveTargetIdentityWhenReady(resolver, sessionID, command)
	if !known {
		return definitelyNotSubmitted("", fmt.Errorf("target provider could not be proven"))
	}
	if !waitForInputReadyGuarded(w.socketPathFor(sessionID), sessionID, identity.Command, inputReadyTimeout(identity.Command), func() error {
		return guardTargetIdentity(resolver, sessionID, identity)
	}) && needsInputReadinessWait(identity.Command, "") {
		return agentInputNotReady(identity.Command)
	}
	_, err := w.sessionInputOwner().submit(sessionID, identity, resolver, identity.Command, payload, "")
	return err
}

// SubmitDelegatedInputWhenReady submits an initial delegated turn and durably
// binds its identity to the same Session input boundary.
func (w *Watcher) SubmitDelegatedInputWhenReady(
	sessionID, command, payload, turnID string,
	acceptedAt time.Time,
) (InputResult, error) {
	resolver := w.targetForSession
	identity, known := resolveTargetIdentityWhenReady(resolver, sessionID, command)
	if !known {
		return InputResult{Outcome: InputNotSubmitted, Receipt: turnID},
			definitelyNotSubmitted(turnID, fmt.Errorf("target provider could not be proven"))
	}
	if !waitForInputReadyGuarded(w.socketPathFor(sessionID), sessionID, identity.Command, inputReadyTimeout(identity.Command), func() error {
		return guardTargetIdentity(resolver, sessionID, identity)
	}) && needsInputReadinessWait(identity.Command, "") {
		return InputResult{Outcome: InputNotSubmitted, Receipt: turnID},
			agentInputNotReady(identity.Command)
	}
	result, err := w.sessionInputOwner().submitDelegated(
		sessionID,
		identity,
		resolver,
		identity.Command,
		payload,
		delegatedTurnDraft{
			ID:                strings.TrimSpace(turnID),
			AcceptedAt:        acceptedAt.UTC(),
			ProcessIdentity:   delegatedTurnIdentity(identity),
			TranscriptBinding: transcriptBindingForCommand(identity.Command),
		},
		w.delegatedInputConfirmer(
			sessionID,
			identity.Command,
		),
	)
	_, _, _ = w.ledgerTurnAuthoritative(sessionID, time.Now().UTC())
	return result, err
}

// SubmitInput submits one exact payload without consulting rendered provider
// state. It is the ordinary Chat/follow-up boundary after initial launch.
func (w *Watcher) SubmitInput(sessionID, payload string) error {
	identity, known := w.targetForSession(sessionID)
	if !known {
		return definitelyNotSubmitted("", fmt.Errorf("target provider could not be proven"))
	}
	_, err := w.sessionInputOwner().submit(
		sessionID,
		identity,
		w.targetForSession,
		identity.Command,
		payload,
		"",
	)
	return err
}

// SubmitDelegatedInput submits a follow-up delegated turn through the same
// durable input owner used for the initial handoff.
func (w *Watcher) SubmitDelegatedInput(
	sessionID, payload, turnID string,
	acceptedAt time.Time,
) (InputResult, error) {
	identity, known := w.targetForSession(sessionID)
	if !known {
		return InputResult{Outcome: InputNotSubmitted, Receipt: turnID},
			definitelyNotSubmitted(turnID, fmt.Errorf("target provider could not be proven"))
	}
	result, err := w.sessionInputOwner().submitDelegated(
		sessionID,
		identity,
		w.targetForSession,
		identity.Command,
		payload,
		delegatedTurnDraft{
			ID:                strings.TrimSpace(turnID),
			AcceptedAt:        acceptedAt.UTC(),
			ProcessIdentity:   delegatedTurnIdentity(identity),
			TranscriptBinding: transcriptBindingForCommand(identity.Command),
		},
		w.delegatedInputConfirmer(
			sessionID,
			identity.Command,
		),
	)
	_, _, _ = w.ledgerTurnAuthoritative(sessionID, time.Now().UTC())
	return result, err
}

// SubmitBrainHostInput admits a direct Brain Event as a real canonical
// provider Turn while keeping the Event receipt, random handling token, and
// provider Turn identity distinct. It reuses the sole Session Input owner and
// provider-neutral admission confirmer; there is no second scheduler.
func (w *Watcher) SubmitBrainHostInput(
	sessionID, payload, eventID, providerTurnID string,
	acceptedAt time.Time,
) (InputResult, error) {
	identity, known := w.targetForSession(sessionID)
	if !known {
		return InputResult{Outcome: InputNotSubmitted, Receipt: eventID, TurnID: providerTurnID},
			definitelyNotSubmitted(eventID, fmt.Errorf("target provider could not be proven"))
	}
	result, err := w.sessionInputOwner().submitDelegated(
		sessionID,
		identity,
		w.targetForSession,
		identity.Command,
		payload,
		delegatedTurnDraft{
			ID:                strings.TrimSpace(providerTurnID),
			Receipt:           strings.TrimSpace(eventID),
			AcceptedAt:        acceptedAt.UTC(),
			ProcessIdentity:   delegatedTurnIdentity(identity),
			TranscriptBinding: transcriptBindingForCommand(identity.Command),
		},
		w.delegatedInputConfirmer(sessionID, identity.Command),
	)
	_, _, _ = w.ledgerTurnAuthoritative(sessionID, time.Now().UTC())
	return result, err
}

// transcriptBindingForCommand records the provider-native transcript identity
// known at admission. Only a Zen-owned Pi launch carries an admission-time
// binding (the owned --session/--session-dir path); other providers discover
// their transcript identity from live evidence and bind via provider facts.
func transcriptBindingForCommand(command string) TranscriptBinding {
	flag, path := piOwnedLaunchFlag(command)
	if flag == "" || path == "" {
		return TranscriptBinding{}
	}
	return TranscriptBinding{Provider: "pi", PiFlag: flag, PiPath: path}
}

func (w *Watcher) delegatedInputConfirmer(
	sessionID string,
	command string,
) delegatedInputConfirmer {
	observe := func() (ProviderActivityObservation, error) {
		agent := w.GetAgent(sessionID)
		if agent == nil {
			return ProviderActivityObservation{}, fmt.Errorf("provider Session disappeared")
		}
		w.mu.RLock()
		providerProbe := w.providerActivityProbe
		w.mu.RUnlock()
		if providerProbe == nil {
			return ProviderActivityObservation{}, fmt.Errorf("provider admission probe is unavailable")
		}
		return providerProbe.ObserveProviderActivity(*agent, w.admissionNowValue()), nil
	}
	return delegatedInputConfirmer{
		baseline: func() (delegatedInputBaseline, error) {
			observation, err := observe()
			if err != nil {
				return delegatedInputBaseline{}, err
			}
			return delegatedInputBaseline{
				Admission: delegatedAdmissionEvidenceFromObservation(observation),
				Provider:  observation,
			}, nil
		},
		confirm: func(
			baseline delegatedAdmissionEvidence,
			mutationBoundary time.Time,
			payloadSHA256 string,
		) (delegatedInputConfirmation, error) {
			deadline := w.admissionNowValue().Add(w.admissionTimeoutValue(command))
			for {
				observation, err := observe()
				if err != nil {
					return delegatedInputConfirmation{Outcome: InputAmbiguous}, err
				}
				evidence := delegatedAdmissionEvidenceFromObservation(observation)
				switch correlateDelegatedAdmission(
					baseline,
					evidence,
					mutationBoundary,
					payloadSHA256,
				) {
				case delegatedAdmissionAccepted:
					return delegatedInputConfirmation{
						Outcome:          InputAccepted,
						ProviderActivity: strings.TrimSpace(observation.ID),
						Admission:        evidence,
					}, nil
				case delegatedAdmissionMismatched:
					return delegatedInputConfirmation{Outcome: InputAmbiguous},
						fmt.Errorf("provider admitted input bytes that did not match the submitted UTF-8 payload")
				}
				if !w.admissionNowValue().Before(deadline) {
					return delegatedInputConfirmation{Outcome: InputAmbiguous},
						fmt.Errorf("provider submit may have mutated the composer, but no correlated provider admission was observed")
				}
				w.admissionSleepValue(50 * time.Millisecond)
			}
		},
	}
}

type delegatedAdmissionCorrelation uint8

const (
	delegatedAdmissionMissing delegatedAdmissionCorrelation = iota
	delegatedAdmissionMismatched
	delegatedAdmissionAccepted
)

func delegatedAdmissionEvidenceFromObservation(
	observation ProviderActivityObservation,
) delegatedAdmissionEvidence {
	return delegatedAdmissionEvidence{
		Stream:      strings.TrimSpace(observation.AdmissionStream),
		ID:          strings.TrimSpace(observation.AdmissionID),
		Cursor:      observation.AdmissionCursor,
		StartedAt:   observation.AdmissionAt.UTC(),
		InputSHA256: strings.TrimSpace(observation.InputSHA256),
	}
}

func correlateDelegatedAdmission(
	baseline delegatedAdmissionEvidence,
	current delegatedAdmissionEvidence,
	mutationBoundary time.Time,
	payloadSHA256 string,
) delegatedAdmissionCorrelation {
	// A provider admission identity is meaningful only as a complete tuple.
	// In particular, an event ID or cursor cannot be compared across streams.
	if current.Stream == "" || current.ID == "" ||
		current.Cursor == 0 || current.InputSHA256 == "" {
		return delegatedAdmissionMissing
	}
	if baseline.Stream != "" {
		if current.Stream != baseline.Stream ||
			current.Cursor <= baseline.Cursor ||
			(baseline.ID != "" && current.ID == baseline.ID) {
			return delegatedAdmissionMissing
		}
	}
	if !current.StartedAt.IsZero() &&
		current.StartedAt.Before(mutationBoundary.UTC()) {
		return delegatedAdmissionMissing
	}
	if current.InputSHA256 != strings.TrimSpace(payloadSHA256) {
		return delegatedAdmissionMismatched
	}
	return delegatedAdmissionAccepted
}

func (w *Watcher) admissionNowValue() time.Time {
	if w != nil && w.admissionNow != nil {
		return w.admissionNow().UTC()
	}
	return time.Now().UTC()
}

func (w *Watcher) admissionSleepValue(duration time.Duration) {
	if w != nil && w.admissionSleep != nil {
		w.admissionSleep(duration)
		return
	}
	time.Sleep(duration)
}

func (w *Watcher) admissionTimeoutValue(command string) time.Duration {
	if w != nil && w.admissionTimeout != nil {
		return w.admissionTimeout(command)
	}
	return inputReadyTimeout(command)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func sendDraftInputLocked(
	socket, sessionID string,
	text string,
	submitDelay time.Duration,
	guard func() error,
) error {
	body, submit := splitTmuxInput(text)
	if body != "" {
		if err := sendLiteralTmuxInputGuarded(socket, sessionID, body, guard); err != nil {
			return err
		}
	}
	if submit {
		if body != "" {
			tmuxSubmitSleep(submitDelay)
		}
		if guard != nil {
			if err := guard(); err != nil {
				return err
			}
		}
		return tmuxCommand(socket, "send-keys", "-t", sessionID, "Enter").Run()
	}
	return nil
}

func waitForInputReadyGuarded(
	socket, sessionID string,
	command string,
	timeout time.Duration,
	guard func() error,
) bool {
	if !needsInputReadinessWait(command, "") {
		return guard == nil || guard() == nil
	}
	deadline := time.Now().Add(timeout)
	advancedWorkspaceTrustPrompt := false
	for {
		if guard != nil && guard() != nil {
			return false
		}
		content, alive, _ := capturePaneContentFunc(sessionID)
		if !alive {
			return false
		}
		paneCWD := ""
		if isCodexCommand(command) &&
			strings.Contains(content, "Do you trust the contents of this directory?") {
			paneCWD = capturePaneWorkingDirectory(socket, sessionID)
		}
		var advanced, ok bool
		advancedWorkspaceTrustPrompt, advanced, ok = advanceStartupTrustPromptOnce(
			advancedWorkspaceTrustPrompt,
			command,
			content,
			paneCWD,
			guard,
			func(key string) error {
				return tmuxCommand(socket, "send-keys", "-t", sessionID, key).Run()
			},
		)
		if !ok {
			return false
		}
		if advanced {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if isAgentInputReady(command, content) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func advanceStartupTrustPromptOnce(
	alreadyAdvanced bool,
	command string,
	content string,
	paneCWD string,
	guard func() error,
	sendKey func(string) error,
) (bool, bool, bool) {
	if alreadyAdvanced {
		return true, false, true
	}
	key := ""
	switch {
	case isCursorWorkspaceTrustPrompt(command, content):
		key = "a"
	case isCodexWorkspaceTrustPrompt(command, content, paneCWD):
		key = "Enter"
	default:
		return false, false, true
	}
	if guard != nil && guard() != nil {
		return false, false, false
	}
	if sendKey == nil || sendKey(key) != nil {
		return false, false, false
	}
	return true, true, true
}

func capturePaneWorkingDirectory(socket, sessionID string) string {
	output, err := tmuxCommand(
		socket,
		"display-message",
		"-p",
		"-t",
		sessionID,
		"#{pane_current_path}",
	).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func isCodexWorkspaceTrustPrompt(command, content, paneCWD string) bool {
	if !isCodexCommand(command) || strings.TrimSpace(paneCWD) == "" {
		return false
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) > 80 {
		normalized = strings.Join(lines[len(lines)-80:], "\n")
	}
	currentPath := filepath.Clean(strings.TrimSpace(paneCWD))
	pathMatches := false
	for _, candidate := range codexWorkspaceTrustPathCandidates(normalized) {
		if filepath.Clean(candidate) == currentPath {
			pathMatches = true
			break
		}
	}
	if !pathMatches {
		return false
	}
	return strings.Contains(normalized, "Do you trust the contents of this directory?") &&
		strings.Contains(normalized, "1. Yes, continue") &&
		strings.Contains(normalized, "2. No, quit") &&
		strings.Contains(normalized, "Press enter to continue")
}

func codexWorkspaceTrustPathCandidates(content string) []string {
	lines := strings.Split(content, "\n")
	const prefix = "> You are in "
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		parts := []string{strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))}
		for next := index + 1; next < len(lines); next++ {
			segment := strings.TrimSpace(lines[next])
			if segment == "" {
				break
			}
			parts = append(parts, segment)
		}
		concatenated := strings.Join(parts, "")
		spaceJoined := strings.Join(parts, " ")
		if concatenated == spaceJoined {
			return []string{concatenated}
		}
		return []string{concatenated, spaceJoined}
	}
	return nil
}

func isAgentInputReady(command, content string) bool {
	if !needsInputReadinessWait(command, content) {
		return true
	}
	explicitCodex := isCodexCommand(command)
	if explicitCodex {
		return isCodexStartupReady(content)
	}
	if isCursorAgentCommand(command) || strings.Contains(strings.ToLower(content), "cursor agent") {
		current := latestCursorPaneContent(content)
		return strings.Contains(strings.ToLower(current), "cursor agent") &&
			cursorInputReadyRe.MatchString(current)
	}
	if isClaudeCommand(command) {
		return isClaudeInputReady(content)
	}
	if isGrokCommand(command) || looksLikeGrokPane(content) {
		return isGrokInputReady(content)
	}
	if isPiCommand(command) {
		return isPiInputReady(content)
	}
	if isOpenCodeCommand(command) || looksLikeOpenCodePane(content) {
		return isOpenCodeInputReady(content)
	}
	return strings.TrimSpace(content) != ""
}

func isCodexStartupReady(content string) bool {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lower := strings.ToLower(normalized)
	lastHeader := strings.LastIndex(lower, "openai codex")
	if lastHeader < 0 {
		lastHeader = strings.LastIndex(lower, ">_ codex")
	}
	if lastHeader < 0 {
		return false
	}
	normalized = normalized[lastHeader:]
	lower = strings.ToLower(normalized)
	for _, blocked := range []string{
		"select a model",
		"choose a model",
		"loading model",
		"starting codex",
		"trust this folder",
		"press enter to continue",
	} {
		if strings.Contains(lower, blocked) {
			return false
		}
	}
	for _, line := range strings.Split(normalized, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "›") {
			return true
		}
	}
	return false
}

func isGrokInputReady(content string) bool {
	current := latestGrokPaneContent(content)
	if strings.TrimSpace(current) == "" {
		return false
	}
	return grokChromeReadyRe.MatchString(current) && grokPromptReadyRe.MatchString(current)
}

func looksLikeGrokPane(content string) bool {
	lower := strings.ToLower(content)
	return strings.Contains(lower, "grok") &&
		(strings.Contains(lower, "always-approve") ||
			strings.Contains(lower, "xai") ||
			strings.Contains(lower, "enter:send") ||
			strings.Contains(lower, "shift+tab:mode") ||
			grokChromeReadyRe.MatchString(content))
}

func latestGrokPaneContent(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lower := strings.ToLower(normalized)
	if idx := strings.LastIndex(lower, "grok"); idx >= 0 {
		// Prefer the latest chrome block; keep preceding context so the composer
		// prompt above the Grok footer is still visible for readiness checks.
		start := idx - 400
		if start < 0 {
			start = 0
		}
		return normalized[start:]
	}
	lines := strings.Split(normalized, "\n")
	if len(lines) > 60 {
		return strings.Join(lines[len(lines)-60:], "\n")
	}
	return normalized
}

func isCursorWorkspaceTrustPrompt(command, content string) bool {
	if !isCursorAgentCommand(command) && !strings.Contains(strings.ToLower(content), "cursor agent") {
		return false
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lower := strings.ToLower(normalized)
	return cursorWorkspaceTrustRe.MatchString(normalized) &&
		strings.Contains(lower, "trust this workspace")
}

func needsInputReadinessWait(command, content string) bool {
	lowerContent := strings.ToLower(content)
	return isCodexCommand(command) ||
		isCursorAgentCommand(command) ||
		isClaudeCommand(command) ||
		isGrokCommand(command) ||
		isPiCommand(command) ||
		isOpenCodeCommand(command) ||
		strings.Contains(lowerContent, "openai codex") ||
		strings.Contains(lowerContent, "cursor agent") ||
		looksLikeGrokPane(content) ||
		looksLikeOpenCodePane(content)
}

func isCodexCommand(command string) bool {
	return commandExecutableBase(command) == "codex"
}

func isCursorAgentCommand(command string) bool {
	return commandExecutableBase(command) == "cursor-agent"
}

func isGrokCommand(command string) bool {
	base := commandExecutableBase(command)
	return base == "grok" || strings.HasPrefix(base, "grok-")
}

func isClaudeCommand(command string) bool {
	base := commandExecutableBase(command)
	return base == "claude" || base == "cc"
}

func isPiCommand(command string) bool {
	return commandExecutableBase(command) == "pi"
}

func isOpenCodeCommand(command string) bool {
	return commandExecutableBase(command) == "opencode"
}

func isPiInputReady(content string) bool {
	if strings.TrimSpace(content) == "" {
		return false
	}
	if !piVersionRe.MatchString(content) && !piChromeRe.MatchString(content) {
		return false
	}
	borders := piEditorBorderRe.FindAllStringIndex(content, -1)
	if len(borders) < 2 {
		return false
	}
	// Empty editor: two horizontal rules with only blank/whitespace between them.
	between := content[borders[len(borders)-2][1]:borders[len(borders)-1][0]]
	if strings.TrimSpace(between) != "" {
		return false
	}
	return true
}

func isOpenCodeInputReady(content string) bool {
	if strings.TrimSpace(content) == "" {
		return false
	}
	if openCodeBlockedOverlayRe.MatchString(content) {
		return false
	}
	return openCodeComposerPlaceholderRe.MatchString(content) &&
		openCodeAgentLineRe.MatchString(content) &&
		(openCodeVersionFooterRe.MatchString(content) || openCodeIdleFooterRe.MatchString(content))
}

func looksLikeOpenCodePane(content string) bool {
	lower := strings.ToLower(content)
	return strings.Contains(lower, "ask anything...") ||
		(strings.Contains(lower, "tab agents") && strings.Contains(lower, "ctrl+p commands"))
}

// commandExecutableBase returns filepath.Base of the launch executable.
// Direct commands use field 0. Zen Host PATH wrapping uses the shape:
//
//	env [NAME=value...] [--] executable [args...]
//
// Only that optional env prefix is recognized. Assignment values may be
// shell-quoted (as withZenCLIOnPath/shellQuote produce) and can contain
// spaces. Later arguments are never scanned for provider names, and env
// without an executable yields "".
func commandExecutableBase(command string) string {
	fields := splitZenLaunchFields(command)
	if len(fields) == 0 {
		return ""
	}
	index := 0
	if fields[0] == "env" {
		index = 1
		for index < len(fields) {
			if fields[index] == "--" {
				index++
				break
			}
			if !looksLikeEnvAssignment(fields[index]) {
				break
			}
			index++
		}
	}
	if index >= len(fields) {
		return ""
	}
	return filepath.Base(fields[index])
}

// splitZenLaunchFields splits a Zen launch command on whitespace while keeping
// single-quoted spans intact so shellQuote'd PATH values with spaces stay one
// assignment token. It is not a general shell parser.
func splitZenLaunchFields(command string) []string {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}
	fields := make([]string, 0, 8)
	var token strings.Builder
	inSingle := false
	flush := func() {
		if token.Len() == 0 {
			return
		}
		fields = append(fields, token.String())
		token.Reset()
	}
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if inSingle {
			token.WriteByte(ch)
			if ch == '\'' {
				inSingle = false
			}
			continue
		}
		switch ch {
		case '\'':
			token.WriteByte(ch)
			inSingle = true
		case ' ', '\t', '\n', '\r':
			flush()
		default:
			token.WriteByte(ch)
		}
	}
	flush()
	return fields
}

func looksLikeEnvAssignment(token string) bool {
	eq := strings.IndexByte(token, '=')
	if eq <= 0 {
		return false
	}
	for _, r := range token[:eq] {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
		default:
			return false
		}
	}
	return true
}

func isClaudeInputReady(content string) bool {
	if strings.TrimSpace(content) == "" {
		return false
	}
	// Require all three ready indicators to distinguish from startup/loading.
	// Header: numeric Claude Code version marker
	// Composer: empty input line with prompt glyph (spaces/tabs/NBSP only)
	// Footer: mode indication (bypass permissions or manual mode)
	return claudeHeaderRe.MatchString(content) &&
		claudeComposerRe.MatchString(content) &&
		claudeModeFooterRe.MatchString(content)
}

func (w *Watcher) activitySignal(agent classifier.Agent, paneContent string, panePID int, processes map[int]processInfo) classifier.ActivitySignal {
	w.mu.RLock()
	probe := w.activityProbe
	w.mu.RUnlock()
	if probe == nil {
		return classifier.ActivitySignal{}
	}
	toolChild := false
	if isCursorAgentCommand(agent.Command) {
		// Cursor-specific process hint until a shared process observer exists.
		toolChild = cursorToolChildActive(panePID, processes)
	}
	return probe.Infer(classifier.ActivityInput{
		Agent:           agent,
		PaneContent:     paneContent,
		ToolChildActive: toolChild,
	})
}

// cursorToolChildActive reports a non-MCP worker under the Cursor agent process.
// Long-lived MCP servers (playwright/context7/etc.) stay attached while idle, so
// they must not count as turn activity.
func cursorToolChildActive(panePID int, processes map[int]processInfo) bool {
	if panePID <= 0 || len(processes) == 0 {
		return false
	}
	scan := descendantProcesses(panePID, processes)
	if proc, ok := processes[panePID]; ok {
		scan = append([]processInfo{proc}, scan...)
	}
	cursorPID := 0
	for _, proc := range scan {
		if isCursorAgentProcess(proc) {
			cursorPID = proc.pid
			break
		}
	}
	if cursorPID == 0 {
		return false
	}
	for _, proc := range descendantProcesses(cursorPID, processes) {
		if isCursorMCPProcess(proc) {
			continue
		}
		if isCursorToolWorkerProcess(proc) {
			return true
		}
	}
	return false
}

func isCursorAgentProcess(proc processInfo) bool {
	lowerComm := strings.ToLower(normalizeCommand(proc.comm))
	lowerArgs := strings.ToLower(proc.args)
	if lowerComm == "cursor-agent" || strings.Contains(lowerArgs, "cursor-agent") {
		return true
	}
	return strings.Contains(lowerArgs, ".local/share/cursor-agent") && strings.Contains(lowerArgs, "index.js")
}

func isCursorMCPProcess(proc processInfo) bool {
	lower := strings.ToLower(proc.comm + " " + proc.args)
	if strings.Contains(lower, "mcp") {
		return true
	}
	if strings.Contains(lower, "code-mode-host") {
		return true
	}
	if strings.Contains(lower, "playwright") && strings.Contains(lower, "npx") {
		return true
	}
	return false
}

func isCursorToolWorkerProcess(proc processInfo) bool {
	comm := strings.ToLower(normalizeCommand(proc.comm))
	switch comm {
	case "zsh", "bash", "sh", "dash", "fish", "python", "python3", "node", "go", "ruby", "perl", "deno", "bun":
		return true
	}
	lowerArgs := strings.ToLower(proc.args)
	if strings.Contains(lowerArgs, "cursor sandbox") || strings.Contains(lowerArgs, "__cursor_sandbox") {
		return true
	}
	return false
}

func latestCursorPaneContent(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lower := strings.ToLower(normalized)
	if idx := strings.LastIndex(lower, "cursor agent"); idx >= 0 {
		return normalized[idx:]
	}
	lines := strings.Split(normalized, "\n")
	if len(lines) > 60 {
		return strings.Join(lines[len(lines)-60:], "\n")
	}
	return normalized
}

func tmuxSubmitDelay(command string) time.Duration {
	if isCursorAgentCommand(command) {
		// Large pastes become a composer attachment asynchronously. Enter sent
		// before that attachment is ready is ignored and leaves bytes sitting
		// in the composer.
		return 2 * time.Second
	}
	if isGrokCommand(command) {
		// Large spawn briefs need a settle window before Enter or Grok keeps the draft unsent.
		return 300 * time.Millisecond
	}
	if isClaudeCommand(command) {
		// Claude needs settle time for initial brief to be fully pasted before submit.
		return 250 * time.Millisecond
	}
	return 120 * time.Millisecond
}

func tmuxPrepareDelay(command string) time.Duration {
	if isCursorAgentCommand(command) {
		// Cursor applies composer edits asynchronously. Give its clear action
		// one render boundary before atomically pasting the new payload.
		return 400 * time.Millisecond
	}
	return 0
}

func inputReadyTimeout(command string) time.Duration {
	if isCodexCommand(command) {
		return codexInputStartupStallTimeout
	}
	if isCursorAgentCommand(command) {
		return cursorInputReadyTimeout
	}
	if isClaudeCommand(command) {
		return claudeInputReadyTimeout
	}
	if isGrokCommand(command) {
		return grokInputReadyTimeout
	}
	if isPiCommand(command) {
		return piInputReadyTimeout
	}
	if isOpenCodeCommand(command) {
		return openCodeInputReadyTimeout
	}
	return initialInputReadyTimeout
}

func sendLiteralTmuxInput(socket, sessionID, body string) error {
	return sendLiteralTmuxInputGuarded(socket, sessionID, body, nil)
}

func sendLiteralTmuxInputGuarded(socket, sessionID, body string, guard func() error) error {
	for _, chunk := range splitStringByMaxBytes(body, tmuxSendInputChunkBytes) {
		if chunk == "" {
			continue
		}
		if guard != nil {
			if err := guard(); err != nil {
				return err
			}
		}
		if out, err := tmuxCommand(socket, "send-keys", "-l", "-t", sessionID, "--", chunk).CombinedOutput(); err != nil {
			return fmt.Errorf("send literal tmux input: %w%s", err, commandOutputSuffix(out))
		}
	}
	return nil
}

func splitTmuxInput(text string) (body string, submit bool) {
	submit = strings.HasSuffix(text, "\n") || strings.HasSuffix(text, "\r")
	if submit {
		text = strings.TrimRight(text, "\r\n")
	}
	return text, submit
}

// splitSubmitInput treats exactly one final line ending as the transport
// submit delimiter. Any earlier trailing line endings remain payload bytes.
func splitSubmitInput(text string) (body string, submit bool) {
	switch {
	case strings.HasSuffix(text, "\r\n"):
		return strings.TrimSuffix(text, "\r\n"), true
	case strings.HasSuffix(text, "\n"):
		return strings.TrimSuffix(text, "\n"), true
	case strings.HasSuffix(text, "\r"):
		return strings.TrimSuffix(text, "\r"), true
	default:
		return text, false
	}
}

func splitStringByMaxBytes(value string, maxBytes int) []string {
	if value == "" {
		return nil
	}
	if maxBytes <= 0 || len(value) <= maxBytes {
		return []string{value}
	}
	chunks := make([]string, 0, (len(value)/maxBytes)+1)
	start := 0
	size := 0
	for index, r := range value {
		runeBytes := len(string(r))
		if size > 0 && size+runeBytes > maxBytes {
			chunks = append(chunks, value[start:index])
			start = index
			size = 0
		}
		size += runeBytes
	}
	if start < len(value) {
		chunks = append(chunks, value[start:])
	}
	return chunks
}

func commandOutputSuffix(output []byte) string {
	if len(output) == 0 {
		return ""
	}
	text := strings.TrimSpace(string(output))
	if text == "" {
		return ""
	}
	return ": " + text
}

// SendAction executes a predefined action on a tmux window.
func (w *Watcher) SendAction(sessionID, action string) error {
	var args []string
	switch action {
	case "approve":
		args = []string{"send-keys", "-t", sessionID, "y", "Enter"}
	case "reject":
		args = []string{"send-keys", "-t", sessionID, "n", "Enter"}
	case "pause":
		args = []string{"send-keys", "-t", sessionID, "C-c"}
	case "show_diff":
		args = []string{"send-keys", "-t", sessionID, "/diff", "Enter"}
	case "run_tests":
		args = []string{"send-keys", "-t", sessionID, "/test", "Enter"}
	case "git_status":
		args = []string{"send-keys", "-t", sessionID, "git status", "Enter"}
	default:
		return fmt.Errorf("unknown action: %s", action)
	}
	identity, known := w.targetForSession(sessionID)
	if !known {
		return fmt.Errorf("target provider could not be proven; action was not sent")
	}
	resolver := w.targetForSession
	send := func() error {
		if err := guardTargetIdentity(resolver, sessionID, identity); err != nil {
			return err
		}
		return tmuxCommand(w.socketPathFor(sessionID), args...).Run()
	}
	return w.sessionInputOwner().serialized(sessionID, send)
}

type CreateSessionOptions struct {
	Cwd         string
	Command     string
	Name        string
	Detached    bool
	Hidden      bool
	Env         map[string]string
	ProgressEnv bool
	Delegated   bool
	resource    *delegatedResourceSpec
}

// CreateSession creates a new tmux window and returns its target id.
// If preferredTarget is set, the new window is created in the same tmux
// session as that target. Otherwise the first non-zen tmux session is used.
func (w *Watcher) CreateSession(preferredTarget string, opts CreateSessionOptions) (string, error) {
	createdAt := time.Now().UTC()
	// Zen-owned sessions (Brain host, delegated spawns, launcher) live on the
	// daemon-namespaced tmux server; only an explicit preferred target on the
	// user server joins that server.
	createSocket := w.daemonSocketPath
	if preferredTarget != "" {
		createSocket = w.socketPathFor(preferredTarget)
	}
	sessionName := baseSessionName(preferredTarget)
	createDetachedSession := opts.Detached
	if sessionName == "" {
		if !createDetachedSession {
			sessions, err := listTmuxSessionsOn(createSocket)
			if err != nil {
				if !isNoTmuxServerError(err) {
					return "", err
				}
			}
			if len(sessions) > 0 {
				sessionName = sessions[0]
			} else {
				createDetachedSession = true
			}
		}
		if sessionName == "" {
			sessionName = newTmuxSessionName(opts)
		}
	}

	cwd := strings.TrimSpace(opts.Cwd)
	if cwd == "" && preferredTarget != "" {
		currentPath, err := currentPathForTarget(createSocket, preferredTarget)
		if err == nil {
			cwd = currentPath
		}
	}
	if cwd == "" {
		if workingDir, err := os.Getwd(); err == nil {
			cwd = workingDir
		}
	}
	// Daemon tmux isolation: every daemon-owned pane gets a private
	// TMUX_TMPDIR (the daemon host scratch, overridden by the per-agent
	// resource scratch once prepared) and an unset TMUX (in the launch shell),
	// so an unscoped nested `tmux kill-server` cannot reach the daemon server
	// or the user's default server.
	if opts.ProgressEnv {
		opts.Env = cloneEnvironment(opts.Env)
		if scratch := strings.TrimSpace(w.daemonScratchDir); scratch != "" {
			opts.Env["TMUX_TMPDIR"] = scratch
		}
	}
	manager := w.resourceManager()
	resourceCommitted := false
	if opts.Delegated && !opts.Hidden {
		if err := validateDelegatedWorkspace(cwd); err != nil {
			return "", err
		}
		opts.Env = cloneEnvironment(opts.Env)
		opts.Env[delegatedMarkerEnv] = "1"
		spec, err := manager.Prepare(w.delegatedSessionCount())
		if err != nil {
			return "", err
		}
		if spec != nil {
			opts.resource = spec
			opts.Env[delegatedResourceUnitEnv] = spec.Unit
			opts.Env[delegatedResourceOwnerEnv] = spec.Owner
			if strings.TrimSpace(spec.TempDir) != "" {
				opts.Env["TMPDIR"] = spec.TempDir
				opts.Env["TMP"] = spec.TempDir
				opts.Env["TEMP"] = spec.TempDir
				opts.Env["ZEN_BUILD_TMPDIR"] = spec.TempDir
				// Per-agent private tmux scratch: nested plain `tmux` commands
				// from this pane resolve under the agent's own directory.
				opts.Env["TMUX_TMPDIR"] = spec.TempDir
			}
			defer func() {
				if !resourceCommitted {
					_ = manager.Release("", spec.Unit)
				}
			}()
		}
	}

	if shellCommand, err := buildWindowCommand(opts); err != nil {
		return "", err
	} else if shellCommand != "" {
		var args []string
		if createDetachedSession {
			args = buildNewSessionArgs(sessionName, cwd, opts, shellCommand)
		} else {
			args = buildNewWindowArgs(sessionName, cwd, opts, shellCommand)
		}
		out, err := tmuxCommand(createSocket, args...).Output()
		if err != nil {
			return "", fmt.Errorf("create tmux window: %w", err)
		}

		target := strings.TrimSpace(string(out))
		if target == "" {
			return "", fmt.Errorf("tmux returned empty window target")
		}
		if err := markCreatedSession(createSocket, target, opts); err != nil {
			killOut, killErr := tmuxCommand(createSocket, "kill-window", "-t", target).CombinedOutput()
			if killErr != nil {
				return "", fmt.Errorf("mark owned tmux window: %v; remove unmarked window: %w: %s", err, killErr, strings.TrimSpace(string(killOut)))
			}
			return "", fmt.Errorf("mark owned tmux window: %w", err)
		}
		w.registerCreatedSession(createSocket, target, cwd, opts, createdAt)
		resourceCommitted = true
		return target, nil
	}

	var args []string
	if createDetachedSession {
		args = buildNewSessionArgs(sessionName, cwd, opts, "")
	} else {
		args = buildNewWindowArgs(sessionName, cwd, opts, "")
	}
	out, err := tmuxCommand(createSocket, args...).Output()
	if err != nil {
		return "", fmt.Errorf("create tmux window: %w", err)
	}

	target := strings.TrimSpace(string(out))
	if target == "" {
		return "", fmt.Errorf("tmux returned empty window target")
	}
	if err := markCreatedSession(createSocket, target, opts); err != nil {
		killOut, killErr := tmuxCommand(createSocket, "kill-window", "-t", target).CombinedOutput()
		if killErr != nil {
			return "", fmt.Errorf("mark owned tmux window: %v; remove unmarked window: %w: %s", err, killErr, strings.TrimSpace(string(killOut)))
		}
		return "", fmt.Errorf("mark owned tmux window: %w", err)
	}
	w.registerCreatedSession(createSocket, target, cwd, opts, createdAt)
	resourceCommitted = true
	return target, nil
}

func buildNewWindowArgs(sessionName, cwd string, opts CreateSessionOptions, shellCommand string) []string {
	args := []string{
		"new-window",
		"-P",
		"-F",
		"#{session_name}:#{window_id}",
		"-t",
		sessionName,
	}
	args = appendTmuxCreateOptions(args, cwd, opts)
	if shellCommand != "" {
		args = append(args, shellCommand)
	}
	return args
}

func buildNewSessionArgs(sessionName, cwd string, opts CreateSessionOptions, shellCommand string) []string {
	args := []string{
		"new-session",
		"-d",
		"-P",
		"-F",
		"#{session_name}:#{window_id}",
		"-s",
		sessionName,
	}
	args = appendTmuxCreateOptions(args, cwd, opts)
	if shellCommand != "" {
		args = append(args, shellCommand)
	}
	return args
}

func appendTmuxCreateOptions(args []string, cwd string, opts CreateSessionOptions) []string {
	baseEnv := os.Environ()
	if len(opts.Env) > 0 {
		keys := make([]string, 0, len(opts.Env))
		for key := range opts.Env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			baseEnv = append(baseEnv, key+"="+opts.Env[key])
		}
	}
	for _, envEntry := range tmuxWindowEnvironment(baseEnv) {
		args = append(args, "-e", envEntry)
	}
	if name := strings.TrimSpace(opts.Name); name != "" {
		args = append(args, "-n", name)
	}
	if cwd != "" {
		args = append(args, "-c", cwd)
	}
	return args
}

func newTmuxSessionName(opts CreateSessionOptions) string {
	base := strings.ToLower(createdSessionName(opts))
	base = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_':
			return r
		default:
			return '-'
		}
	}, base)
	base = strings.Trim(base, "-_")
	if base == "" {
		base = "agent"
	}
	return fmt.Sprintf("brain-agent-%s-%d", base, time.Now().UnixNano())
}

func (w *Watcher) registerCreatedSession(socket, target, cwd string, opts CreateSessionOptions, createdAt time.Time) {
	target = strings.TrimSpace(target)
	if target == "" {
		return
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	if opts.resource != nil {
		w.resourceManager().Bind(target, opts.resource.Unit)
	}
	w.mu.Lock()
	// Record the server actually used for the create/join (daemon-namespaced
	// for Zen-owned sessions, the preferred target's own server on join), so
	// first-poll and pre-inventory routing already target the right server.
	w.targetSockets[target] = targetSocket{known: true, socket: socket}
	w.mu.Unlock()

	agent := &classifier.Agent{
		ID:        target,
		Name:      formatAgentName(createdSessionName(opts), target),
		Project:   projectNameFromPath(cwd),
		Cwd:       strings.TrimSpace(cwd),
		Command:   strings.TrimSpace(opts.Command),
		State:     classifier.StateUnknown,
		Summary:   "Session starting",
		LastLines: []string{},
		StartedAt: createdAt,
		UpdatedAt: createdAt,
		PaneAlive: true,
		Hidden:    opts.Hidden,
		Delegated: opts.Delegated && !opts.Hidden,
	}

	w.mu.Lock()
	if opts.Hidden {
		w.hidden[target] = true
	}
	if agent.Delegated {
		w.delegated[target] = true
	} else {
		delete(w.delegated, target)
	}
	if _, exists := w.agents[target]; !exists {
		w.agentOrder = append(w.agentOrder, target)
	}
	w.agents[target] = agent
	w.agentEpoch[target] = 0 // invalidate any in-flight poll apply for this id
	if _, exists := w.prevContent[target]; !exists {
		w.prevContent[target] = ""
	}
	snapshot := cloneAgent(agent)
	w.mu.Unlock()

	w.events <- SessionEvent{
		Type:    "agent_discovered",
		AgentID: target,
		Agent:   snapshot,
	}
}

func markCreatedSession(socket, target string, opts CreateSessionOptions) error {
	if err := setTmuxWindowUserOption(socket, target, "zen_agent_created", "1"); err != nil {
		return err
	}
	// Durable Pi ownership binding only: the raw launch command is never
	// persisted (it may contain secrets or delimiter-breaking characters).
	// Only a validated Pi launch with an absolute owned --session/
	// --session-dir writes an option, encoded delimiter-safe; non-Pi commands
	// and Pi commands without an owned binding write nothing. The binding
	// outlives the daemon in the tmux server, because node-based Pi rewrites
	// its own argv and window re-discovery after a daemon restart cannot
	// recover the owned path from the process table.
	if commandExecutableBase(opts.Command) == "pi" {
		if flag, path := piOwnedLaunchFlag(opts.Command); flag != "" {
			if err := setTmuxWindowUserOption(socket, target, "zen_agent_pi_session", EncodePiSessionBinding(flag, path)); err != nil {
				return err
			}
		}
	}
	if opts.Hidden {
		if err := setTmuxWindowUserOption(socket, target, "zen_agent_hidden", "1"); err != nil {
			return err
		}
	}
	if opts.Delegated && !opts.Hidden {
		if err := setTmuxWindowUserOption(socket, target, "zen_agent_delegated", "1"); err != nil {
			return err
		}
		if opts.resource != nil {
			if err := setTmuxWindowUserOption(socket, target, "zen_agent_resource_unit", opts.resource.Unit); err != nil {
				return err
			}
			if err := setTmuxWindowUserOption(socket, target, "zen_agent_resource_owner", opts.resource.Owner); err != nil {
				return err
			}
		}
	}
	return nil
}

func setTmuxWindowUserOption(socket, target, key, value string) error {
	target = strings.TrimSpace(target)
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if target == "" || key == "" || value == "" {
		return fmt.Errorf("tmux window option target, key, and value are required")
	}
	out, err := tmuxCommand(socket, "set-option", "-w", "-t", target, "@"+key, value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("set @%s on %s: %w: %s", key, target, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func tmuxWindowUserOption(socket, target, key string) (string, error) {
	target = strings.TrimSpace(target)
	key = strings.TrimSpace(key)
	if target == "" || key == "" {
		return "", fmt.Errorf("tmux window option target and key are required")
	}
	out, err := tmuxCommand(
		socket,
		"show-options",
		"-w",
		"-qv",
		"-t",
		target,
		"@"+key,
	).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("show @%s on %s: %w: %s", key, target, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func createdSessionName(opts CreateSessionOptions) string {
	if name := strings.TrimSpace(opts.Name); name != "" {
		return name
	}
	fields := strings.Fields(strings.TrimSpace(opts.Command))
	if len(fields) == 0 {
		return ""
	}
	return filepath.Base(fields[0])
}

func buildWindowCommand(opts CreateSessionOptions) (string, error) {
	shellPath, err := currentLoginShell()
	if err != nil {
		return "", err
	}

	inner := buildWindowCommandForShellWithOptions(shellPath, strings.TrimSpace(opts.Command), opts.ProgressEnv)
	return wrapDelegatedResourceCommand(inner, opts.resource), nil
}

func buildWindowCommandForShell(shellPath, command string) string {
	return buildWindowCommandForShellWithOptions(shellPath, command, false)
}

func buildWindowCommandForShellWithOptions(shellPath, command string, progressEnv bool) string {
	quotedShell := shellQuote(shellPath)
	command = strings.TrimSpace(command)
	if progressEnv {
		prefix := agentProgressEnvScript()
		if command == "" {
			return "exec " + quotedShell + " -i -l -c " + shellQuote(prefix+"; exec "+quotedShell+" -i -l")
		}
		command = prefix + "; " + command
	}
	if command == "" {
		return "exec " + quotedShell + " -i -l"
	}
	return "exec " + quotedShell + " -i -l -c " + shellQuote(command)
}

func agentProgressEnvScript() string {
	// The progress script derives ZEN_AGENT_ID from the pane's own server
	// ($TMUX is set by tmux), then unsets TMUX: with TMUX_TMPDIR pointed at
	// the agent's private scratch, any later unscoped `tmux` invocation
	// (including kill-server) can reach neither the daemon server nor the
	// user's default server (Slice 3).
	return `if [ -z "${ZEN_AGENT_ID:-}" ] && [ -n "${TMUX_PANE:-}" ]; then ZEN_AGENT_ID="$(tmux display-message -p -t "$TMUX_PANE" "#{session_name}:#{window_id}" 2>/dev/null || true)"; export ZEN_AGENT_ID; fi; if [ -z "${ZEN_AGENT_PROGRESS_CMD:-}" ]; then ZEN_AGENT_PROGRESS_CMD=` + shellQuote(ZenExecutablePath()) + `; export ZEN_AGENT_PROGRESS_CMD; fi; unset TMUX`
}

// ZenExecutablePath returns the absolute path of the currently running zen
// daemon executable so delegated agents invoke the exact same binary (and
// therefore the same control socket / state dir) without relying on shell
// word splitting or PATH lookups. It trusts os.Executable() regardless of the
// binary's base name, so dev daemons launched as "zen-dev" (which rebuilds
// cmd/zen into tmp/zen-dev) resolve to that dev binary instead of a stale
// "zen" found elsewhere on PATH. It only falls back to "zen" when the current
// executable cannot be resolved or is empty (for example, in exotic test
// runners); the protocol always invokes the value as a quoted single token
// followed by the "agent progress" subcommand, which is safe under zsh/bash.
func ZenExecutablePath() string {
	exe, err := os.Executable()
	if err != nil {
		return "zen"
	}
	exe = strings.TrimSpace(exe)
	if exe == "" {
		return "zen"
	}
	return exe
}

func formatAgentName(windowName, target string) string {
	trimmedName := strings.TrimSpace(windowName)
	trimmedTarget := strings.TrimSpace(target)
	switch {
	case trimmedName != "" && trimmedTarget != "":
		return trimmedName + " (" + trimmedTarget + ")"
	case trimmedName != "":
		return trimmedName
	default:
		return trimmedTarget
	}
}

func tmuxWindowEnvironment(base []string) []string {
	skipKeys := map[string]bool{
		"":                     true,
		"_":                    true,
		"OLDPWD":               true,
		"PWD":                  true,
		"SHLVL":                true,
		"TERM":                 true,
		"TERM_PROGRAM":         true,
		"TERM_PROGRAM_VERSION": true,
		"TMUX":                 true,
		"TMUX_PANE":            true,
	}

	values := make(map[string]string, len(base))
	keys := make([]string, 0, len(base))
	for _, entry := range base {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if skipKeys[key] {
			continue
		}
		if _, exists := values[key]; !exists {
			keys = append(keys, key)
		}
		values[key] = value
	}

	sort.Strings(keys)

	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env
}

func currentLoginShell() (string, error) {
	if shell := loginShellFromPasswd(); shell != "" {
		return shell, nil
	}
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" {
		return shell, nil
	}
	return "/bin/sh", nil
}

func loginShellFromPasswd() string {
	currentUser, err := user.Current()
	if err != nil {
		return ""
	}

	passwd, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return ""
	}

	username := strings.TrimSpace(currentUser.Username)
	uid := strings.TrimSpace(currentUser.Uid)
	for _, line := range strings.Split(string(passwd), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) < 7 {
			continue
		}
		if fields[0] != username && fields[2] != uid {
			continue
		}
		shell := strings.TrimSpace(fields[6])
		if shell != "" {
			return shell
		}
	}
	return ""
}

// applyDaemonWindowEnvironment injects the daemon tmux isolation into a
// daemon-owned pane environment: TMUX_TMPDIR points at the per-agent resource
// scratch (or the daemon host scratch for hidden host sessions) so an
// unscoped nested `tmux kill-server` resolves into a private directory and
// can reach neither the daemon server nor the user's default server. The
// launch shell additionally unsets TMUX (agentProgressEnvScript). Idempotent.
func applyDaemonWindowEnvironment(opts *CreateSessionOptions, w *Watcher) {
	if opts == nil || w == nil || !opts.ProgressEnv {
		return
	}
	opts.Env = cloneEnvironment(opts.Env)
	if opts.resource != nil && strings.TrimSpace(opts.resource.TempDir) != "" {
		opts.Env["TMUX_TMPDIR"] = strings.TrimSpace(opts.resource.TempDir)
		return
	}
	if scratch := strings.TrimSpace(w.daemonScratchDir); scratch != "" {
		opts.Env["TMUX_TMPDIR"] = scratch
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

// KillSession terminates the tmux window backing a single agent.
// Agent IDs use the form session:window_id, so killing the window
// exits only that agent instead of the whole tmux session.
//
// A target that is already missing is an idempotent success for the kill
// itself. Delegated resource release still runs when a bound unit is known and
// must succeed before KillSession returns nil — a resource-release failure
// after a successful (or already-missing) kill is retryable and typed as
// ErrDelegatedResourceRelease.
func (w *Watcher) KillSession(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	manager := w.resourceManager()
	unit := manager.UnitForTarget(sessionID)
	delegated := unit != ""
	socket := w.socketPathFor(sessionID)
	if !delegated {
		delegated, unit = tmuxDelegatedResource(socket, sessionID)
	}
	out, killErr := tmuxCommand(socket, "kill-window", "-t", sessionID).CombinedOutput()
	outText := strings.TrimSpace(string(out))
	missing := killErr != nil && isTmuxTargetMissing(killErr, outText)
	if killErr != nil && !missing {
		// Non-missing kill failure: window may still be live. Do not release
		// delegated resources; surface the kill error for retry.
		return fmt.Errorf("kill tmux window: %w: %s", killErr, outText)
	}
	if delegated {
		if releaseErr := manager.Release(sessionID, unit); releaseErr != nil {
			return fmt.Errorf("%w: %v", ErrDelegatedResourceRelease, releaseErr)
		}
	}
	return nil
}

func isTmuxTargetMissing(err error, output string) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error() + " " + output))
	if isNoTmuxServerError(err) || isNoTmuxServerError(fmt.Errorf("%s", output)) {
		return true
	}
	return strings.Contains(text, "can't find window") ||
		strings.Contains(text, "couldn't find window") ||
		strings.Contains(text, "can't find session") ||
		strings.Contains(text, "couldn't find session") ||
		strings.Contains(text, "no such window") ||
		strings.Contains(text, "no such session") ||
		strings.Contains(text, "session not found") ||
		strings.Contains(text, "window not found")
}

func tmuxDelegatedResource(socket, target string) (bool, string) {
	target = strings.TrimSpace(target)
	if target == "" {
		return false, ""
	}
	out, err := tmuxCommand(
		socket,
		"display-message",
		"-p",
		"-t",
		target,
		"#{@zen_agent_delegated}\t#{@zen_agent_resource_unit}",
	).Output()
	if err != nil {
		return false, ""
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "\t", 2)
	if len(parts) != 2 || !tmuxBoolOption(parts[0]) {
		return false, ""
	}
	return true, strings.TrimSpace(parts[1])
}

func listTmuxSessionsOn(socket string) ([]string, error) {
	out, err := tmuxCommand(socket, "list-sessions", "-F", "#{session_name}").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("tmux list-sessions: %w: %s", err, strings.TrimSpace(string(out)))
	}

	sessions := make([]string, 0)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		sessionName := strings.TrimSpace(line)
		if sessionName == "" || strings.HasPrefix(sessionName, "zen-") {
			continue
		}
		sessions = append(sessions, sessionName)
	}
	return sessions, nil
}

func isNoTmuxServerError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "no server running") ||
		strings.Contains(text, "failed to connect to server") ||
		// An explicit -S socket with no server behind it reports the connect
		// failure as ENOENT (socket path absent) or ECONNREFUSED (stale
		// socket file). Both mean there is no server on that socket; a
		// permission failure is a real error and stays fail-closed.
		(strings.Contains(text, "error connecting to") &&
			(strings.Contains(text, "no such file") || strings.Contains(text, "connection refused")))
}

func baseSessionName(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}

	sessionName, _, ok := strings.Cut(target, ":")
	if !ok {
		return ""
	}
	if strings.HasPrefix(sessionName, "zen-") {
		return ""
	}
	return sessionName
}

func currentPathForTarget(socket, target string) (string, error) {
	out, err := tmuxCommand(socket, "display-message", "-p", "-t", target, "#{pane_current_path}").Output()
	if err != nil {
		return "", fmt.Errorf("tmux current path: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

type processInfo struct {
	pid       int
	ppid      int
	pgid      int
	tpgid     int
	startedAt time.Time
	comm      string
	args      string
}

func snapshotProcesses() map[int]processInfo {
	command := exec.Command("ps", "-eo", "pid=,ppid=,pgid=,tpgid=,lstart=,comm=,args=")
	command.Env = append(command.Environ(), "LC_ALL=C")
	out, err := command.Output()
	if err != nil {
		return nil
	}
	return parseProcessSnapshot(out, time.Local)
}

func parseProcessSnapshot(out []byte, location *time.Location) map[int]processInfo {
	if location == nil {
		location = time.Local
	}
	processes := make(map[int]processInfo)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		pgid, err3 := strconv.Atoi(fields[2])
		tpgid, err4 := strconv.Atoi(fields[3])
		startedAt, err5 := time.ParseInLocation(
			"Mon Jan 2 15:04:05 2006",
			strings.Join(fields[4:9], " "),
			location,
		)
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil {
			continue
		}

		args := ""
		if len(fields) > 10 {
			args = strings.Join(fields[10:], " ")
		}

		processes[pid] = processInfo{
			pid:       pid,
			ppid:      ppid,
			pgid:      pgid,
			tpgid:     tpgid,
			startedAt: startedAt,
			comm:      fields[9],
			args:      args,
		}
	}
	return processes
}

type foregroundTargetAuthority struct {
	command    string
	foreground processInfo
	provider   processInfo
}

// resolveForegroundTargetProcess binds terminal mutation authority to both the
// kernel's foreground process-group leader and one coherent Provider process
// in that leader's same-PGID descendant lineage. Other process groups cannot
// influence the result; conflicting or indeterminate foreground Providers
// fail closed.
func resolveForegroundTargetProcess(panePID int, processes map[int]processInfo) (foregroundTargetAuthority, bool) {
	paneProcess, ok := processes[panePID]
	if !ok || paneProcess.startedAt.IsZero() || paneProcess.tpgid <= 0 {
		return foregroundTargetAuthority{}, false
	}
	foregroundPID := paneProcess.tpgid
	foreground, ok := processes[foregroundPID]
	if !ok ||
		foreground.pid != foregroundPID ||
		foreground.pgid != foregroundPID ||
		foreground.startedAt.IsZero() ||
		!processDescendsFrom(panePID, foregroundPID, processes) {
		return foregroundTargetAuthority{}, false
	}

	providerFamily := ""
	bestScore := -1
	bestCommand := ""
	bestProcess := processInfo{}
	bestTied := false
	resumeCommand := ""
	var providerLineage []processInfo
	for _, process := range processes {
		if process.pgid != foregroundPID {
			continue
		}
		detected := agentCommandFromProcess(process)
		if detected == "" {
			continue
		}
		if process.startedAt.IsZero() ||
			!processDescendsFrom(foregroundPID, process.pid, processes) {
			return foregroundTargetAuthority{}, false
		}
		family := agentProviderFamily(detected)
		if family == "" {
			return foregroundTargetAuthority{}, false
		}
		for _, previous := range providerLineage {
			if !processDescendsFrom(previous.pid, process.pid, processes) &&
				!processDescendsFrom(process.pid, previous.pid, processes) {
				return foregroundTargetAuthority{}, false
			}
		}
		providerLineage = append(providerLineage, process)
		if providerFamily == "" {
			providerFamily = family
		} else if providerFamily != family {
			return foregroundTargetAuthority{}, false
		}
		if resume, _ := commandResumeArg(detected); resume {
			if resumeCommand != "" && resumeCommand != detected {
				return foregroundTargetAuthority{}, false
			}
			resumeCommand = detected
		}
		score := agentProcessScore(process, detected)
		switch {
		case score > bestScore:
			bestScore = score
			bestCommand = detected
			bestProcess = process
			bestTied = false
		case score == bestScore && process.pid != bestProcess.pid:
			bestTied = true
		}
	}
	if providerFamily != "" {
		if bestTied || bestCommand == "" || bestProcess.pid <= 0 {
			return foregroundTargetAuthority{}, false
		}
		if resumeCommand != "" && agentProviderFamily(bestCommand) == providerFamily {
			bestCommand = resumeCommand
		}
		return foregroundTargetAuthority{
			command:    bestCommand,
			foreground: foreground,
			provider:   bestProcess,
		}, true
	}

	command := normalizeCommand(foreground.comm)
	if command == "" {
		return foregroundTargetAuthority{}, false
	}
	return foregroundTargetAuthority{
		command:    command,
		foreground: foreground,
		provider:   foreground,
	}, true
}

func foregroundTargetProcess(panePID int, processes map[int]processInfo) (string, time.Time, int, bool) {
	authority, ok := resolveForegroundTargetProcess(panePID, processes)
	if !ok {
		return "", time.Time{}, 0, false
	}
	return authority.command, authority.provider.startedAt, authority.provider.pid, true
}

func agentProviderFamily(command string) string {
	switch agentCommandName(command) {
	case "claude", "claude-code", "cc":
		return "claude"
	case "codex":
		return "codex"
	case "cursor-agent":
		return "cursor-agent"
	case "grok":
		return "grok"
	case "pi":
		return "pi"
	case "opencode":
		return "opencode"
	default:
		return ""
	}
}

// mergeAgentCommandOwnership preserves Zen-owned Pi launch metadata across
// polls. detectAgentProcess reports the provider identity observed in the
// process table, but node-based Pi rewrites its own argv, so the injected
// absolute --session path cannot be recovered from the process. The launch
// command bound at session create is the only authoritative structured source;
// the merge reconstructs the canonical `pi --session <path>` command while the
// observed process is the same provider family. A provider switch clears stale
// ownership, and commands without ownership keep the detected identity exactly
// as before. The canonical form keeps every downstream consumer (App provider
// classification, InferAgentProvider, input readiness) on the plain pi command
// shape.
func mergeAgentCommandOwnership(previous, detected string) string {
	previous = strings.TrimSpace(previous)
	detected = strings.TrimSpace(detected)
	if detected == "" {
		return detected
	}
	if commandExecutableBase(previous) != "pi" || commandExecutableBase(detected) != "pi" {
		return detected
	}
	flag, path := piOwnedLaunchFlag(previous)
	if flag == "" {
		return detected
	}
	return commandExecutableBase(detected) + " " + flag + " " + shellQuoteForLaunch(path)
}

// piOwnedLaunchPath returns the absolute --session or --session-dir value
// declared by a Pi launch command, or "" when the command carries no
// Zen-owned Pi session path. The env-assignment launch shape Zen emits is
// understood; quoting is preserved by splitZenLaunchFields.
func piOwnedLaunchPath(command string) string {
	flag, path := piOwnedLaunchFlag(command)
	if flag == "" {
		return ""
	}
	return path
}

// piOwnedLaunchFlag returns the owned Pi session flag ("--session" or
// "--session-dir") and its absolute value, or ("", "") when the command
// carries no Zen-owned Pi session path.
func piOwnedLaunchFlag(command string) (string, string) {
	fields := splitZenLaunchFields(command)
	if len(fields) == 0 {
		return "", ""
	}
	index := 0
	if fields[0] == "env" {
		index = 1
		for index < len(fields) {
			if fields[index] == "--" {
				index++
				break
			}
			if !looksLikeEnvAssignment(fields[index]) {
				break
			}
			index++
		}
	}
	for i := index; i < len(fields); i++ {
		flag := ""
		value := ""
		switch {
		case fields[i] == "--session" || fields[i] == "--session-dir":
			if i+1 >= len(fields) || strings.HasPrefix(fields[i+1], "-") {
				return "", ""
			}
			flag = fields[i]
			value = fields[i+1]
		case strings.HasPrefix(fields[i], "--session-dir="):
			flag = "--session-dir"
			value = strings.TrimPrefix(fields[i], "--session-dir=")
		case strings.HasPrefix(fields[i], "--session="):
			flag = "--session"
			value = strings.TrimPrefix(fields[i], "--session=")
		default:
			continue
		}
		value = strings.TrimSpace(value)
		value = unquoteLaunchValue(value)
		if value != "" && filepath.IsAbs(value) {
			return flag, value
		}
		return "", ""
	}
	return "", ""
}

// unquoteLaunchValue removes one layer of Zen launcher quoting from a launch
// token so the watcher and the work reader agree on the owned session path
// value. Zen wraps values containing shell metacharacters in single quotes
// with backslash-escaped apostrophes (work.shellQuoteForLaunch); a token whose
// first and last characters are both the wrapping quote is returned without
// them. Values with an embedded literal apostrophe cannot form one wrapped
// token in splitZenLaunchFields (the escape's first quote closes the span), so
// they fail closed exactly like the work parser, which decodes them
// differently but never binds a wrong transcript.

// piSessionBindingWire is the versioned durable shape stored under the
// @zen_agent_pi_session tmux window option. Only a validated Pi ownership
// binding (flag + absolute path) is ever written; the raw launch command is
// never persisted.
type piSessionBindingWire struct {
	Version int    `json:"v"`
	Flag    string `json:"flag"`
	Path    string `json:"path"`
}

// EncodePiSessionBinding encodes a validated Pi ownership binding into the
// delimiter-safe durable option value. base64url keeps the value a single
// token (no tabs, newlines, or quotes), so the tab-separated list-windows
// projection can never be corrupted by a path. Invalid flags or non-absolute
// paths return "".
func EncodePiSessionBinding(flag, path string) string {
	flag = strings.TrimSpace(flag)
	if flag != "--session" && flag != "--session-dir" {
		return ""
	}
	if path == "" || !filepath.IsAbs(path) {
		return ""
	}
	data, err := json.Marshal(piSessionBindingWire{Version: 1, Flag: flag, Path: path})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

// DecodePiSessionBinding decodes and validates a @zen_agent_pi_session option
// value. Any malformed, wrong-version, or non-absolute value fails closed
// (ok=false), so a corrupted option can never bind a transcript.
func DecodePiSessionBinding(value string) (flag, path string, ok bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", false
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", "", false
	}
	var wire piSessionBindingWire
	if json.Unmarshal(data, &wire) != nil {
		return "", "", false
	}
	if wire.Version != 1 {
		return "", "", false
	}
	flag = strings.TrimSpace(wire.Flag)
	if flag != "--session" && flag != "--session-dir" {
		return "", "", false
	}
	if wire.Path == "" || !filepath.IsAbs(wire.Path) {
		return "", "", false
	}
	return flag, wire.Path, true
}

func unquoteLaunchValue(value string) string {
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1]
	}
	return value
}

// shellQuoteForLaunch mirrors work.shellQuoteForLaunch so the merged command
// form is byte-identical to the injected launch command: values containing
// shell metacharacters are wrapped in single quotes with backslash-escaped
// apostrophes, clean values are unchanged.
func shellQuoteForLaunch(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	if !strings.ContainsAny(value, " \t\"'\\$`") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func processDescendsFrom(rootPID, processID int, processes map[int]processInfo) bool {
	if rootPID <= 0 || processID <= 0 {
		return false
	}
	visited := make(map[int]struct{})
	for current := processID; current > 0; {
		if current == rootPID {
			return true
		}
		if _, duplicate := visited[current]; duplicate {
			return false
		}
		visited[current] = struct{}{}
		process, ok := processes[current]
		if !ok || process.ppid == current {
			return false
		}
		current = process.ppid
	}
	return false
}

func detectAgentProcess(baseCommand string, panePID int, processes map[int]processInfo, fallbackAt time.Time) (string, time.Time, int) {
	command := normalizeCommand(baseCommand)
	baseStartedAt := time.Time{}
	basePID := 0
	if proc, ok := processes[panePID]; ok {
		baseStartedAt = proc.startedAt
		basePID = proc.pid
	}

	if panePID > 0 && len(processes) > 0 {
		scan := descendantProcesses(panePID, processes)
		if proc, ok := processes[panePID]; ok {
			scan = append([]processInfo{proc}, scan...)
		}
		codexResumeSeen := false
		grokResumeSeen := false
		codexResumeCommand := ""
		grokResumeCommand := ""
		bestScore := -1
		var bestCommand string
		var bestProcess processInfo
		for _, proc := range scan {
			if detected := agentCommandFromProcess(proc); detected != "" {
				if isCodexResumeCommandLine(detected) {
					codexResumeSeen = true
					codexResumeCommand = detected
				}
				if isGrokResumeCommandLine(detected) {
					grokResumeSeen = true
					grokResumeCommand = detected
				}
				score := agentProcessScore(proc, detected)
				if bestScore == -1 || score > bestScore {
					bestScore = score
					bestCommand = detected
					bestProcess = proc
				}
			}
		}
		if bestCommand != "" {
			if bestCommand == "codex" && codexResumeSeen {
				if codexResumeCommand != "" {
					bestCommand = codexResumeCommand
				} else {
					bestCommand = "codex resume"
				}
			}
			if bestCommand == "grok" && grokResumeSeen {
				if grokResumeCommand != "" {
					bestCommand = grokResumeCommand
				} else {
					bestCommand = "grok resume"
				}
			}
			return bestCommand, firstNonZeroTime(bestProcess.startedAt, fallbackAt), bestProcess.pid
		}
	}

	if isAgentCommand(command) {
		return command, fallbackAt, basePID
	}
	return command, baseStartedAt, basePID
}

func agentCommandFromProcess(proc processInfo) string {
	lowerComm := normalizeCommand(proc.comm)
	lowerArgs := strings.ToLower(proc.args)
	switch lowerComm {
	case "sh", "bash", "dash", "zsh", "fish":
		// A shell command line is launch intent, not proof that the provider
		// process exists. Bind only the actual child (or an exec-replaced pane).
		return ""
	}

	if lowerComm == "claude" || lowerComm == "claude-code" || lowerComm == "cc" {
		return lowerComm
	}
	if strings.Contains(lowerArgs, " claude") || strings.HasPrefix(lowerArgs, "claude ") {
		return "claude"
	}
	if lowerComm == "codex" || lowerArgs == "codex" || strings.Contains(lowerArgs, "/bin/codex") || strings.Contains(lowerArgs, " codex ") || strings.HasPrefix(lowerArgs, "codex ") {
		if resume, sessionID := commandResumeArg(lowerArgs); resume {
			if sessionID != "" {
				return "codex resume " + sessionID
			}
			return "codex resume"
		}
		return "codex"
	}
	if lowerComm == "cursor-agent" || strings.Contains(lowerArgs, "/bin/cursor-agent") || strings.Contains(lowerArgs, " cursor-agent ") || strings.HasPrefix(lowerArgs, "cursor-agent ") {
		return "cursor-agent"
	}
	if lowerComm == "grok" || strings.Contains(lowerArgs, "/bin/grok") || strings.Contains(lowerArgs, " grok ") || strings.HasPrefix(lowerArgs, "grok ") {
		if resume, sessionID := commandResumeArg(lowerArgs); resume {
			if sessionID != "" {
				return "grok --resume " + sessionID
			}
			return "grok resume"
		}
		return "grok"
	}
	// Exact basename only for pi: avoid substring false positives (pip, pixel).
	if lowerComm == "pi" || processArgsExecutableBase(lowerArgs) == "pi" {
		return "pi"
	}
	if lowerComm == "opencode" || processArgsExecutableBase(lowerArgs) == "opencode" ||
		strings.Contains(lowerArgs, "/bin/opencode") || strings.Contains(lowerArgs, " opencode ") ||
		strings.HasPrefix(lowerArgs, "opencode ") {
		return "opencode"
	}
	return ""
}

// processArgsExecutableBase returns filepath.Base of the first non-shell/env
// field in a process args string. Unlike substring path checks, this rejects
// near-misses such as /bin/pip for provider "pi".
func processArgsExecutableBase(args string) string {
	fields := strings.Fields(strings.TrimSpace(args))
	for _, field := range fields {
		base := normalizeCommand(field)
		switch base {
		case "", "env", "node", "nodejs", "bun", "deno", "python", "python3", "sh", "bash", "dash", "zsh", "fish":
			continue
		default:
			return base
		}
	}
	return ""
}

func agentProcessScore(proc processInfo, detected string) int {
	lowerComm := normalizeCommand(proc.comm)
	detectedName := agentCommandName(detected)
	switch {
	case detectedName == "codex" || detectedName == "grok" || detectedName == "pi" || detectedName == "opencode":
		score := 50
		if lowerComm == "codex" || lowerComm == "grok" || lowerComm == "pi" || lowerComm == "opencode" {
			score = 100
		}
		if resume, _ := commandResumeArg(detected); resume {
			score += 10
		}
		return score
	case detectedName == "cursor-agent":
		if lowerComm == "cursor-agent" {
			return 100
		}
		return 50
	case detected == "claude" || detected == "claude-code" || detected == "cc":
		if lowerComm == detected {
			return 100
		}
		return 50
	default:
		return 0
	}
}

func isCodexResumeCommandLine(command string) bool {
	resume, _ := commandResumeArg(command)
	return agentCommandName(command) == "codex" && resume
}

func isGrokResumeCommandLine(command string) bool {
	resume, _ := commandResumeArg(command)
	return agentCommandName(command) == "grok" && resume
}

func isAgentCommand(command string) bool {
	name := agentCommandName(command)
	return name == "claude" || name == "claude-code" || name == "codex" || name == "cursor-agent" || name == "grok" || name == "cc" || name == "pi" || name == "opencode"
}

func agentCommandName(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return normalizeCommand(command)
	}
	return normalizeCommand(fields[0])
}

// refineProcessStartedAt replaces an observed second-granularity process
// start with the platform's sub-second evidence for the same PID when that
// evidence is consistent with the observation. ps lstart truncates to whole
// seconds, so the true start of the same process lies in [observed,
// observed+1s). The guard accepts the wider [observed, observed+2s) as
// fail-safe tolerance for observation scheduling; any value outside it
// (foreign or recycled pid, fake test observation, stale snapshot) keeps the
// observed value. The function never fabricates or widens precision: without
// evidence, or with contradictory evidence, the observed value is returned
// unchanged.
func refineProcessStartedAt(observed time.Time, pid int) time.Time {
	if observed.IsZero() {
		return observed
	}
	precise, ok := processStartTimeFromProc(pid)
	if !ok {
		return observed
	}
	if precise.Before(observed) || !precise.Before(observed.Add(2*time.Second)) {
		return observed
	}
	return precise
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func descendantProcesses(rootPID int, processes map[int]processInfo) []processInfo {
	if rootPID <= 0 || len(processes) == 0 {
		return nil
	}

	children := make(map[int][]processInfo)
	for _, proc := range processes {
		children[proc.ppid] = append(children[proc.ppid], proc)
	}

	var result []processInfo
	queue := append([]processInfo(nil), children[rootPID]...)
	for len(queue) > 0 {
		proc := queue[0]
		queue = queue[1:]
		result = append(result, proc)
		queue = append(queue, children[proc.pid]...)
	}

	return result
}

func normalizeCommand(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "./")
	if idx := strings.LastIndex(value, "/"); idx >= 0 {
		value = value[idx+1:]
	}
	return value
}

func commandHasArg(command string, arg string) bool {
	for _, field := range strings.Fields(command) {
		if strings.Trim(field, `"'`) == arg {
			return true
		}
	}
	return false
}

func commandResumeArg(command string) (bool, string) {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(command)))
	for index, field := range fields {
		field = strings.Trim(field, `"'`)
		switch {
		case field == "resume" || field == "--resume":
			if index+1 < len(fields) {
				sessionID := strings.Trim(fields[index+1], `"'`)
				if sessionID != "" && !strings.HasPrefix(sessionID, "-") {
					return true, sessionID
				}
			}
			return true, ""
		case strings.HasPrefix(field, "--resume="):
			return true, strings.Trim(strings.TrimPrefix(field, "--resume="), `"'`)
		}
	}
	return false, ""
}

func projectNameFromPath(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ""
	}

	base := filepath.Base(cwd)
	if base == "." || base == string(filepath.Separator) {
		return cwd
	}
	return base
}

func lastN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
