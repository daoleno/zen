package watcher

import (
	"context"
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
)

const tmuxSendInputChunkBytes = 1024

const initialInputReadyTimeout = 8 * time.Second
const codexInputStartupStallTimeout = 30 * time.Second
const cursorInputReadyTimeout = 25 * time.Second
const claudeInputReadyTimeout = 12 * time.Second
const grokInputReadyTimeout = 15 * time.Second

var codexInputPromptRe = regexp.MustCompile(`(?m)^›\s`)
var codexModelLoadingRe = regexp.MustCompile(`(?im)\bmodel:\s+loading\b`)
var codexStartupContinueRe = regexp.MustCompile(`(?im)\bpress\s+enter\s+to\s+continue\b`)
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

// SessionEvent represents a state change or output update for an agent.
type SessionEvent struct {
	Type     string              `json:"type"`
	AgentID  string              `json:"agent_id"`
	Agent    *classifier.Agent   `json:"agent,omitempty"`
	Agents   []*classifier.Agent `json:"agents,omitempty"`
	Lines    []string            `json:"lines,omitempty"`
	OldState string              `json:"old,omitempty"`
	NewState string              `json:"new,omitempty"`
}

// Watcher monitors tmux windows and classifies agent states.
type Watcher struct {
	pollInterval   time.Duration
	agents         map[string]*classifier.Agent
	agentOrder     []string
	prevContent    map[string]string
	hidden         map[string]bool
	delegated      map[string]bool
	activityProbe  classifier.ActivityProbe
	pollGeneration int64
	agentEpoch     map[string]int64 // per-agent generation for lock-free probe apply
	mu             sync.RWMutex
	events         chan SessionEvent
	resources      delegatedResourceManager
}

// New creates a Watcher that polls tmux windows at the given interval.
func New(pollInterval time.Duration) *Watcher {
	return &Watcher{
		pollInterval: pollInterval,
		agents:       make(map[string]*classifier.Agent),
		prevContent:  make(map[string]string),
		hidden:       make(map[string]bool),
		delegated:    make(map[string]bool),
		agentEpoch:   make(map[string]int64),
		events:       make(chan SessionEvent, 100),
		resources:    noopDelegatedResourceManager{},
	}
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
	snapshot = cloneAgent(agent)
	event = SessionEvent{
		Type:    "agent_metadata_change",
		AgentID: id,
		Agent:   snapshot,
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

// SettleAgentInputAccepted clears an older terminal handoff projection after
// the provider has consumed the composer. It updates the canonical Agent in
// place rather than keeping a parallel launch-status shadow. Lifecycle progress
// reported after this handoff began wins and is never overwritten.
func (w *Watcher) SettleAgentInputAccepted(id string, handoffStartedAt time.Time, phase, summary string) (*classifier.Agent, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("missing agent id")
	}
	if handoffStartedAt.IsZero() {
		return nil, fmt.Errorf("missing handoff start time")
	}

	var event SessionEvent
	var snapshot *classifier.Agent
	w.mu.Lock()
	agent, ok := w.agents[id]
	if !ok {
		w.mu.Unlock()
		return nil, fmt.Errorf("agent session not found")
	}
	if agent.LastProgressAt != nil && !agent.LastProgressAt.Before(handoffStartedAt) {
		snapshot = cloneAgent(agent)
		w.mu.Unlock()
		return snapshot, nil
	}

	oldState := agent.State
	agent.State = classifier.StateRunning
	agent.Summary = strings.TrimSpace(summary)
	agent.Phase = strings.TrimSpace(phase)
	agent.Attention = "none"
	agent.NeedsAttention = false
	agent.TaskClass = ""
	agent.EventKind = ""
	agent.DetailsJSON = ""
	agent.LastProgressAt = nil
	agent.ExpectedNextCheckAt = nil
	agent.LeaseSeconds = 0
	agent.UpdatedAt = time.Now().UTC()
	snapshot = cloneAgent(agent)
	event = SessionEvent{
		Type:    "agent_metadata_change",
		AgentID: id,
		Agent:   snapshot,
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

// HasSession reports whether tmux still has a session matching the target.
func (w *Watcher) HasSession(target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	sessionName := baseSessionName(target)
	if strings.Contains(target, ":") {
		return exec.Command("tmux", "has-session", "-t", target).Run() == nil
	}
	if sessionName == "" {
		sessionName = target
	}
	return exec.Command("tmux", "has-session", "-t", sessionName).Run() == nil
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
	windows, err := listTmuxWindows()
	if err != nil {
		if isNoTmuxServerError(err) {
			w.resourceManager().Reconcile(nil)
		}
		return
	}
	w.resourceManager().Reconcile(windows)
	processes := snapshotProcesses()
	processSnapshotAt := time.Now()

	type paneObs struct {
		win     tmuxWindow
		content string
		alive   bool
		lines   []string
	}
	observations := make([]paneObs, 0, len(windows))
	for _, win := range windows {
		content, alive := capturePaneContent(win.target)
		observations = append(observations, paneObs{
			win:     win,
			content: content,
			alive:   alive,
			lines:   strings.Split(content, "\n"),
		})
	}

	type preparedAgent struct {
		id                string
		epoch             int64
		agentSnap         classifier.Agent
		content           string
		lines             []string
		alive             bool
		panePID           int
		classified        classifier.AgentState
		classifiedSummary string
		oldState          classifier.AgentState
		contentChanged    bool
		existed           bool
		exists            bool
		prev              string
		previousMetadata  agentMetadataSnapshot
		now               time.Time
	}

	w.mu.Lock()
	w.pollGeneration++
	generation := w.pollGeneration
	probe := w.activityProbe
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
		agent.Command, agent.StartedAt, agent.ProcessID = detectAgentProcess(win.command, win.panePID, processes, processSnapshotAt)
		if w.hidden[win.target] {
			agent.Hidden = true
		}
		if win.delegated {
			w.delegated[win.target] = true
		}
		agent.Delegated = (w.delegated[win.target] || win.delegated) && !agent.Hidden

		agent.PaneAlive = obs.alive
		agent.LastLines = lastN(obs.lines, 120)
		now := time.Now()
		agent.UpdatedAt = now

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
			panePID:           win.panePID,
			classified:        classified,
			classifiedSummary: classifiedSummary,
			oldState:          oldState,
			contentChanged:    contentChanged,
			existed:           existed,
			exists:            exists,
			prev:              prev,
			previousMetadata:  previousMetadata,
			now:               now,
		})
	}
	w.mu.Unlock()

	type probedAgent struct {
		preparedAgent
		activity classifier.ActivitySignal
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
		results = append(results, probedAgent{preparedAgent: item, activity: activity})
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
		if r.oldState == classifier.StateRunning && newState == classifier.StateUnknown && agent.LastProgressAt != nil {
			if agent.Attention == "" || agent.Attention == "none" {
				agent.Attention = "stale"
			}
			agent.NeedsAttention = true
		}
		agent.State = newState
		agent.Summary = summary

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
			delete(w.agents, id)
			delete(w.prevContent, id)
			delete(w.hidden, id)
			delete(w.delegated, id)
			delete(w.agentEpoch, id)
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
	target       string // "session:window_id" — stable tmux target usable as -t
	name         string // window name (e.g. "claude", "node")
	cwd          string // active pane cwd
	command      string // active pane command
	panePID      int
	hidden       bool
	delegated    bool
	resourceUnit string
}

// listTmuxWindows returns all windows across all tmux sessions.
func listTmuxWindows() ([]tmuxWindow, error) {
	cmd := exec.Command("tmux", "list-windows", "-a", "-F", "#{session_name}:#{window_id}\t#{window_name}\t#{pane_current_path}\t#{pane_current_command}\t#{pane_pid}\t#{@zen_agent_hidden}\t#{@zen_agent_delegated}\t#{@zen_agent_resource_unit}")
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
		parts := strings.SplitN(line, "\t", 8)
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
		windows = append(windows, tmuxWindow{target: target, name: name, cwd: cwd, command: command, panePID: panePID, hidden: hidden, delegated: delegated, resourceUnit: resourceUnit})
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

// capturePaneContent captures the visible content of a tmux window's active pane.
func capturePaneContent(target string) (string, bool) {
	cmd := exec.Command("tmux", "capture-pane", "-t", target, "-p", "-S", "-200")
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}

	cmdAlive := exec.Command("tmux", "list-panes", "-t", target, "-F", "#{pane_dead}")
	aliveOut, err := cmdAlive.Output()
	alive := true
	if err == nil && strings.TrimSpace(string(aliveOut)) == "1" {
		alive = false
	}

	return string(out), alive
}

// CapturePaneContent returns a plain-text snapshot of a tmux window's active pane.
func (w *Watcher) CapturePaneContent(sessionID string) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", fmt.Errorf("missing session id")
	}
	out, err := exec.Command("tmux", "capture-pane", "-t", sessionID, "-p", "-S", "-200").Output()
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
	return exec.Command("tmux", "send-keys", "-t", sessionID, key).Run()
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
	return SendInput(sessionID, text)
}

const brainInputReceiptOption = "zen_brain_input_receipt"

// SendInputWithReceipt makes one Brain input idempotent at the receiving
// Session boundary. A matching durable Session receipt suppresses re-enqueue;
// a new receipt is recorded only after tmux accepts the input.
func (w *Watcher) SendInputWithReceipt(sessionID, text, receipt string) error {
	receipt = strings.TrimSpace(receipt)
	if receipt == "" {
		return fmt.Errorf("input receipt is required")
	}
	accepted, err := w.HasInputReceipt(sessionID, receipt)
	if err != nil {
		return err
	}
	if accepted {
		return nil
	}
	if err := w.SendInput(sessionID, text); err != nil {
		return err
	}
	return setTmuxWindowUserOption(sessionID, brainInputReceiptOption, receipt)
}

// HasInputReceipt reads durable receiver-side acceptance from Session metadata.
func (w *Watcher) HasInputReceipt(sessionID, receipt string) (bool, error) {
	receipt = strings.TrimSpace(receipt)
	if receipt == "" {
		return false, fmt.Errorf("input receipt is required")
	}
	value, err := tmuxWindowUserOption(sessionID, brainInputReceiptOption)
	if err != nil {
		return false, err
	}
	return value == receipt, nil
}

// SendInputWhenReady waits for a newly started agent UI to be ready, then sends
// text. Unknown executors are treated as ready immediately. Known Codex, Cursor,
// Claude, and Grok UIs must reach an input prompt so Zen does not paste a task
// into a startup screen before the composer can accept Enter-to-send.
func (w *Watcher) SendInputWhenReady(sessionID, command, text string) error {
	if isCodexCommand(command) {
		body, submit := splitTmuxInput(text)
		if submit && body != "" {
			return submitCodexInput(realCodexInputIO{}, sessionID, body, defaultCodexSubmitConfig())
		}
	}
	if !WaitForInputReady(sessionID, command, inputReadyTimeout(command)) && needsInputReadinessWait(command, "") {
		return fmt.Errorf("agent input not ready for %q", command)
	}
	return SendInputForCommand(sessionID, command, text)
}

// SendInput sends text to a tmux window and treats trailing newlines as submit.
func SendInput(sessionID, text string) error {
	return SendInputForCommand(sessionID, "", text)
}

// SendInputForCommand sends text to a tmux window and applies executor-specific
// submit timing where terminal UIs need a short paste-settle delay.
func SendInputForCommand(sessionID, command, text string) error {
	if isCodexCommand(command) {
		body, submit := splitTmuxInput(text)
		if submit && body != "" {
			return submitCodexInput(realCodexInputIO{}, sessionID, body, defaultCodexSubmitConfig())
		}
	}
	body, submit := splitTmuxInput(text)
	if body != "" {
		if err := sendLiteralTmuxInput(sessionID, body); err != nil {
			return err
		}
	}
	if submit {
		if body != "" {
			time.Sleep(tmuxSubmitDelay(command))
		}
		return exec.Command("tmux", "send-keys", "-t", sessionID, "Enter").Run()
	}
	return nil
}

// SendInputWhenReady is the package-level form used by executor shims that do
// not hold a Watcher instance.
func SendInputWhenReady(sessionID, command, text string) error {
	if isCodexCommand(command) {
		body, submit := splitTmuxInput(text)
		if submit && body != "" {
			return submitCodexInput(realCodexInputIO{}, sessionID, body, defaultCodexSubmitConfig())
		}
	}
	if !WaitForInputReady(sessionID, command, inputReadyTimeout(command)) && needsInputReadinessWait(command, "") {
		return fmt.Errorf("agent input not ready for %q", command)
	}
	return SendInputForCommand(sessionID, command, text)
}

// WaitForInputReady reports whether a known agent UI reached an input prompt.
// Unknown commands return immediately.
func WaitForInputReady(sessionID, command string, timeout time.Duration) bool {
	if !needsInputReadinessWait(command, "") {
		return true
	}
	deadline := time.Now().Add(timeout)
	advancedStartupPrompt := false
	advancedCursorTrustPrompt := false
	for {
		content, alive := capturePaneContent(sessionID)
		if !alive {
			return false
		}
		if !advancedStartupPrompt && isCodexStartupContinuePrompt(command, content) {
			_ = exec.Command("tmux", "send-keys", "-t", sessionID, "Enter").Run()
			advancedStartupPrompt = true
			time.Sleep(250 * time.Millisecond)
			continue
		}
		if !advancedCursorTrustPrompt && isCursorWorkspaceTrustPrompt(command, content) {
			_ = exec.Command("tmux", "send-keys", "-t", sessionID, "a").Run()
			advancedCursorTrustPrompt = true
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

func isAgentInputReady(command, content string) bool {
	if !needsInputReadinessWait(command, content) {
		return true
	}
	explicitCodex := isCodexCommand(command)
	if explicitCodex || strings.Contains(strings.ToLower(content), "openai codex") {
		current := latestCodexPaneContent(content)
		return (explicitCodex || strings.Contains(current, "OpenAI Codex")) &&
			!codexModelLoadingRe.MatchString(current) &&
			!codexStartupContinueRe.MatchString(current) &&
			codexInputPromptRe.MatchString(current)
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
	return strings.TrimSpace(content) != ""
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

func isCodexStartupContinuePrompt(command, content string) bool {
	if !isCodexCommand(command) && !strings.Contains(strings.ToLower(content), "openai codex") {
		return false
	}
	return codexStartupContinueRe.MatchString(latestCodexPaneContent(content))
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

func latestCodexPaneContent(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lower := strings.ToLower(normalized)
	if idx := strings.LastIndex(lower, "openai codex"); idx >= 0 {
		return normalized[idx:]
	}
	lines := strings.Split(normalized, "\n")
	if len(lines) > 60 {
		return strings.Join(lines[len(lines)-60:], "\n")
	}
	return normalized
}

func needsInputReadinessWait(command, content string) bool {
	lowerContent := strings.ToLower(content)
	return isCodexCommand(command) ||
		isCursorAgentCommand(command) ||
		isClaudeCommand(command) ||
		isGrokCommand(command) ||
		strings.Contains(lowerContent, "openai codex") ||
		strings.Contains(lowerContent, "cursor agent") ||
		looksLikeGrokPane(content)
}

func isCodexCommand(command string) bool {
	return commandExecutableBase(command) == "codex"
}

// IsCodexCommand reports whether command launches the interactive Codex TUI.
// Control-plane callers use this to opt only Codex follow-ups into the
// confirmed submission transaction without changing other providers.
func IsCodexCommand(command string) bool {
	return isCodexCommand(command)
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
		return 400 * time.Millisecond
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
	return initialInputReadyTimeout
}

func sendLiteralTmuxInput(sessionID, body string) error {
	for _, chunk := range splitStringByMaxBytes(body, tmuxSendInputChunkBytes) {
		if chunk == "" {
			continue
		}
		if out, err := exec.Command("tmux", "send-keys", "-l", "-t", sessionID, "--", chunk).CombinedOutput(); err != nil {
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
	return exec.Command("tmux", args...).Run()
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
	sessionName := baseSessionName(preferredTarget)
	createDetachedSession := opts.Detached
	if sessionName == "" {
		if !createDetachedSession {
			sessions, err := listTmuxSessions()
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
		currentPath, err := currentPathForTarget(preferredTarget)
		if err == nil {
			cwd = currentPath
		}
	}
	if cwd == "" {
		if workingDir, err := os.Getwd(); err == nil {
			cwd = workingDir
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
		out, err := exec.Command("tmux", args...).Output()
		if err != nil {
			return "", fmt.Errorf("create tmux window: %w", err)
		}

		target := strings.TrimSpace(string(out))
		if target == "" {
			return "", fmt.Errorf("tmux returned empty window target")
		}
		if err := markCreatedSession(target, opts); err != nil {
			killOut, killErr := exec.Command("tmux", "kill-window", "-t", target).CombinedOutput()
			if killErr != nil {
				return "", fmt.Errorf("mark owned tmux window: %v; remove unmarked window: %w: %s", err, killErr, strings.TrimSpace(string(killOut)))
			}
			return "", fmt.Errorf("mark owned tmux window: %w", err)
		}
		w.registerCreatedSession(target, cwd, opts, createdAt)
		resourceCommitted = true
		return target, nil
	}

	var args []string
	if createDetachedSession {
		args = buildNewSessionArgs(sessionName, cwd, opts, "")
	} else {
		args = buildNewWindowArgs(sessionName, cwd, opts, "")
	}
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return "", fmt.Errorf("create tmux window: %w", err)
	}

	target := strings.TrimSpace(string(out))
	if target == "" {
		return "", fmt.Errorf("tmux returned empty window target")
	}
	if err := markCreatedSession(target, opts); err != nil {
		killOut, killErr := exec.Command("tmux", "kill-window", "-t", target).CombinedOutput()
		if killErr != nil {
			return "", fmt.Errorf("mark owned tmux window: %v; remove unmarked window: %w: %s", err, killErr, strings.TrimSpace(string(killOut)))
		}
		return "", fmt.Errorf("mark owned tmux window: %w", err)
	}
	w.registerCreatedSession(target, cwd, opts, createdAt)
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

func (w *Watcher) registerCreatedSession(target, cwd string, opts CreateSessionOptions, createdAt time.Time) {
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

func markCreatedSession(target string, opts CreateSessionOptions) error {
	if err := setTmuxWindowUserOption(target, "zen_agent_created", "1"); err != nil {
		return err
	}
	if opts.Hidden {
		if err := setTmuxWindowUserOption(target, "zen_agent_hidden", "1"); err != nil {
			return err
		}
	}
	if opts.Delegated && !opts.Hidden {
		if err := setTmuxWindowUserOption(target, "zen_agent_delegated", "1"); err != nil {
			return err
		}
		if opts.resource != nil {
			if err := setTmuxWindowUserOption(target, "zen_agent_resource_unit", opts.resource.Unit); err != nil {
				return err
			}
			if err := setTmuxWindowUserOption(target, "zen_agent_resource_owner", opts.resource.Owner); err != nil {
				return err
			}
		}
	}
	return nil
}

func setTmuxWindowUserOption(target, key, value string) error {
	target = strings.TrimSpace(target)
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if target == "" || key == "" || value == "" {
		return fmt.Errorf("tmux window option target, key, and value are required")
	}
	out, err := exec.Command("tmux", "set-option", "-w", "-t", target, "@"+key, value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("set @%s on %s: %w: %s", key, target, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func tmuxWindowUserOption(target, key string) (string, error) {
	target = strings.TrimSpace(target)
	key = strings.TrimSpace(key)
	if target == "" || key == "" {
		return "", fmt.Errorf("tmux window option target and key are required")
	}
	out, err := exec.Command(
		"tmux",
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
	return `if [ -z "${ZEN_AGENT_ID:-}" ] && [ -n "${TMUX_PANE:-}" ]; then ZEN_AGENT_ID="$(tmux display-message -p -t "$TMUX_PANE" "#{session_name}:#{window_id}" 2>/dev/null || true)"; export ZEN_AGENT_ID; fi; if [ -z "${ZEN_AGENT_PROGRESS_CMD:-}" ]; then ZEN_AGENT_PROGRESS_CMD=` + shellQuote(ZenExecutablePath()) + `; export ZEN_AGENT_PROGRESS_CMD; fi`
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

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

// KillSession terminates the tmux window backing a single agent.
// Agent IDs use the form session:window_id, so killing the window
// exits only that agent instead of the whole tmux session.
func (w *Watcher) KillSession(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	manager := w.resourceManager()
	unit := manager.UnitForTarget(sessionID)
	delegated := unit != ""
	if !delegated {
		delegated, unit = tmuxDelegatedResource(sessionID)
	}
	out, killErr := exec.Command("tmux", "kill-window", "-t", sessionID).CombinedOutput()
	var releaseErr error
	if delegated {
		releaseErr = manager.Release(sessionID, unit)
	}
	if killErr != nil && releaseErr != nil {
		return fmt.Errorf("kill tmux window: %w: %s; release delegated resources: %v", killErr, strings.TrimSpace(string(out)), releaseErr)
	}
	if killErr != nil {
		return fmt.Errorf("kill tmux window: %w: %s", killErr, strings.TrimSpace(string(out)))
	}
	if releaseErr != nil {
		return fmt.Errorf("release delegated resources: %w", releaseErr)
	}
	return nil
}

func tmuxDelegatedResource(target string) (bool, string) {
	target = strings.TrimSpace(target)
	if target == "" {
		return false, ""
	}
	out, err := exec.Command(
		"tmux",
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

func listTmuxSessions() ([]string, error) {
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").CombinedOutput()
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
		strings.Contains(text, "failed to connect to server")
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

func currentPathForTarget(target string) (string, error) {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", target, "#{pane_current_path}").Output()
	if err != nil {
		return "", fmt.Errorf("tmux current path: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

type processInfo struct {
	pid       int
	ppid      int
	startedAt time.Time
	comm      string
	args      string
}

func snapshotProcesses() map[int]processInfo {
	command := exec.Command("ps", "-eo", "pid=,ppid=,lstart=,comm=,args=")
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
		if len(fields) < 8 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		startedAt, err3 := time.ParseInLocation(
			"Mon Jan 2 15:04:05 2006",
			strings.Join(fields[2:7], " "),
			location,
		)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}

		args := ""
		if len(fields) > 8 {
			args = strings.Join(fields[8:], " ")
		}

		processes[pid] = processInfo{
			pid:       pid,
			ppid:      ppid,
			startedAt: startedAt,
			comm:      fields[7],
			args:      args,
		}
	}
	return processes
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

	if lowerComm == "claude" || lowerComm == "claude-code" || lowerComm == "cc" {
		return lowerComm
	}
	if strings.Contains(lowerArgs, " claude") || strings.HasPrefix(lowerArgs, "claude ") {
		return "claude"
	}
	if lowerComm == "codex" || strings.Contains(lowerArgs, "/bin/codex") || strings.Contains(lowerArgs, " codex ") || strings.HasPrefix(lowerArgs, "codex ") {
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
	return ""
}

func agentProcessScore(proc processInfo, detected string) int {
	lowerComm := normalizeCommand(proc.comm)
	detectedName := agentCommandName(detected)
	switch {
	case detectedName == "codex" || detectedName == "grok":
		score := 50
		if lowerComm == "codex" || lowerComm == "grok" {
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
	return name == "claude" || name == "claude-code" || name == "codex" || name == "cursor-agent" || name == "grok" || name == "cc"
}

func agentCommandName(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return normalizeCommand(command)
	}
	return normalizeCommand(fields[0])
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
