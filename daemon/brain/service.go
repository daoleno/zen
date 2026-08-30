package brain

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/calendar"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/modelprofiles"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

var (
	ErrExecutorNotConfigured = errors.New("brain host executor is not configured")
	ErrExecutorLockedByEnv   = errors.New("brain host executor is locked by environment override")
	// ErrRouteTransferNotDurable means TransferSession applied in memory but
	// route-bindings.json durability was not proven. Host bind/success audit
	// must not proceed.
	ErrRouteTransferNotDurable = errors.New("brain host route transfer applied but not durable")
)

const codexFullAuthorizationFlag = work.CodexFullAuthorizationFlag

type Watcher interface {
	Agents() []*classifier.Agent
	GetAgent(id string) *classifier.Agent
	HasSession(target string) bool
	ProbeSession(target string) (watcher.SessionPresence, error)
	CreateSession(preferredTarget string, opts watcher.CreateSessionOptions) (string, error)
	SendInput(sessionID, text string) error
	SendInputWhenReady(sessionID, command, text string) error
	SendInputWithReceiptResult(sessionID, text, receipt string) (watcher.InputResult, error)
	SubmitBrainHostInput(sessionID, payload, claimToken, workID, providerTurnID string, acceptedAt time.Time) (watcher.InputResult, error)
	InputReceiptResult(sessionID, receipt string) (watcher.InputResult, bool, error)
	KillSession(sessionID string) error
	// ProbeProviderEvidence returns the current provider-native observation
	// for a session; the Host foreground gate and delegated admission window
	// consume it.
	ProbeProviderEvidence(sessionID string) (watcher.ProviderActivityObservation, bool, error)
	ResolveOwnedGeneration(sessionID string) (watcher.OwnedGeneration, error)
	ResolveBrainHostGeneration(sessionID string) (watcher.OwnedGeneration, error)
}

type Service struct {
	store   *Store
	watcher Watcher
	execs   *work.ExecutorConfig
	now     func() time.Time

	// sessionConversationHook overrides the provider conversation reader for
	// the Session projection surface. Tests inject sanitized fixtures; nil
	// keeps the real reader. It carries no routing or input authority.
	sessionConversationHook func(agent *classifier.Agent, provider string, now time.Time) (work.CodexConversation, error)

	dispatchMu sync.Mutex
	// inFlightHostInputs protects only the live Prepare -> provider mutation ->
	// Admit/Abort critical section. It is deliberately process-local: durable
	// BrainInputAdmission + watcher receipt state remain the restart authority.
	inFlightHostInputs map[string]struct{}

	reconcileMu sync.Mutex

	routeMu sync.Mutex
	routes  SessionRouteLifecycle
}

// SessionRouteLifecycle remaps, resumes, prepares, or releases Model Profile
// routes across host Session identity changes. New Brain-host launches use
// Prepare/Commit; missing-tmux resume reuses an immutable existing binding via
// ResumeLaunch+Transfer and must not re-resolve the executor default.
type SessionRouteLifecycle interface {
	TransferSession(fromID, toID string) (modelprofiles.PersistResult, error)
	ResumeLaunch(sessionID, baseCommand string) (command string, env map[string]string, found bool, err error)
	ReleaseSession(sessionID string) (modelprofiles.PersistResult, error)
	PrepareLaunch(executorID, profileID, baseCommand string) (modelprofiles.SessionLaunchPlan, error)
	CommitLaunch(provisionalID, sessionID string) (modelprofiles.SessionRouteState, modelprofiles.WireSessionSnapshot, modelprofiles.PersistResult, error)
	AbortLaunch(provisionalID string) (modelprofiles.PersistResult, error)
	// CodexControlSocket returns the Session's live-control app-server socket
	// (empty when the Session has none).
	CodexControlSocket(sessionID string) string
}

// SetSessionRouteLifecycle installs optional Model Profiles resume support.
func (s *Service) SetSessionRouteLifecycle(routes SessionRouteLifecycle) {
	if s == nil {
		return
	}
	s.routeMu.Lock()
	defer s.routeMu.Unlock()
	s.routes = routes
}

func (s *Service) sessionRoutes() SessionRouteLifecycle {
	if s == nil {
		return nil
	}
	s.routeMu.Lock()
	defer s.routeMu.Unlock()
	return s.routes
}

// teardownHostSession kills a Brain host window and releases its Model Profile
// route only when the window is confirmed gone. Surfaces joined kill/release
// errors; preserves the route when kill fails and the Session is still live.
func (s *Service) teardownHostSession(sessionID string) error {
	return s.teardownOwnedSession(sessionID)
}

func (s *Service) teardownOwnedSession(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || s == nil || s.watcher == nil {
		return nil
	}
	var release func(string) (modelprofiles.PersistResult, error)
	controlSocket := ""
	if routes := s.sessionRoutes(); routes != nil {
		release = routes.ReleaseSession
		controlSocket = routes.CodexControlSocket(sessionID)
	}
	result := modelprofiles.TeardownSession(sessionID, s.watcher.KillSession, s.sessionLivenessProbe, release)
	if result.Err == nil && controlSocket != "" {
		// Session confirmed dead: kill any orphaned Codex app-server and
		// remove daemon-owned socket/pid/log artifacts.
		if cleanupErr := modelprofiles.CleanupCodexControlArtifacts(controlSocket); cleanupErr != nil {
			log.Printf("cleanup codex control artifacts for %s: %v", sessionID, cleanupErr)
		}
	}
	return result.Err
}

// ResolveWorkReview commits Brain's typed disposition. Lifecycle releases the
// canonical owner in the same transition; transport teardown is owned by the
// explicit Session close path, not a parallel finalization lifecycle.
func (s *Service) ResolveWorkReview(request WorkReviewDispositionRequest) (WorkEvent, Work, error) {
	if s == nil || s.store == nil {
		return WorkEvent{}, Work{}, fmt.Errorf("brain store is not configured")
	}
	event, item, err := s.store.ResolveWorkReview(request)
	if err != nil {
		return event, item, err
	}
	return event, item, nil
}

// CloseWork applies the explicit actor/revision-gated terminal transition,
// then drives the same exact Session finalization path used by a Host
// disposition. The Store refuses this path while any Host or provider
// submission authority is still in flight.
func (s *Service) CloseWork(request WorkCloseRequest) (Work, error) {
	if s == nil || s.store == nil {
		return Work{}, fmt.Errorf("brain store is not configured")
	}
	item, err := s.store.CloseWork(request)
	if err != nil {
		return item, err
	}
	return item, nil
}

// ReconcileSignalSystemStartup is the one bounded LISTEN-then-snapshot pass:
// watchers are already wired, every persisted live Host handling is reconciled
// by its original Session and exact provider Turn (not the current binding),
// and only newly pending terminal finalizations are attempted.
func (s *Service) ReconcileSignalSystemStartup(agents []*classifier.Agent, limit int) (bool, error) {
	if s == nil || s.store == nil {
		return true, nil
	}
	admissions, admissionMore, err := s.store.UnprojectedBrainInputAdmissions(limit)
	if err != nil {
		return false, err
	}
	for _, admission := range admissions {
		if err := s.store.ProjectBrainInputAdmission(admission); err != nil {
			return false, err
		}
	}
	byID := make(map[string]*classifier.Agent, len(agents))
	for _, agent := range agents {
		if agent != nil {
			byID[agent.ID] = agent
		}
	}
	handlings, handlingMore, err := s.store.LiveReviewHandlings(limit)
	if err != nil {
		return false, err
	}
	for _, handling := range handlings {
		agent := byID[handling.DeliveryHostSessionID]
		turn, hasTurn, turnErr := s.store.TurnByID(handling.DeliveryHostSessionID, handling.ProviderTurnID)
		if turnErr != nil {
			return false, turnErr
		}
		currentTurn, hasCurrentTurn, currentTurnErr := s.store.Turn(handling.DeliveryHostSessionID)
		if currentTurnErr != nil {
			return false, currentTurnErr
		}
		live := agent != nil && (agent.State == classifier.StateRunning || agent.State == classifier.StateBlocked) &&
			hasTurn && !watcher.TurnImmutable(turn.Status) && hasCurrentTurn && currentTurn.TurnID == handling.ProviderTurnID
		if !live {
			// The delivered lease ends without a disposition; the same
			// unresolved action becomes re-claimable (row 16).
			if _, _, err := s.store.EndReviewDelivery(
				handling.WorkID, handling.HandlingID, handling.ProviderTurnID,
			); err != nil {
				return false, err
			}
		}
	}
	_, dispatchErr := s.ReconcileHostLane()
	return !admissionMore && !handlingMore, dispatchErr
}

func (s *Service) sessionLivenessProbe(sessionID string) (modelprofiles.SessionLiveness, error) {
	if s == nil || s.watcher == nil {
		return modelprofiles.SessionLivenessUnknown, fmt.Errorf("watcher unavailable")
	}
	presence, err := s.watcher.ProbeSession(sessionID)
	if err != nil {
		return modelprofiles.SessionLivenessUnknown, err
	}
	switch presence {
	case watcher.SessionPresencePresent:
		return modelprofiles.SessionLivenessPresent, nil
	case watcher.SessionPresenceAbsent:
		return modelprofiles.SessionLivenessAbsent, nil
	default:
		return modelprofiles.SessionLivenessUnknown, nil
	}
}

func NewService(store *Store, watcher Watcher, execs *work.ExecutorConfig) *Service {
	return &Service{
		store:              store,
		watcher:            watcher,
		execs:              execs,
		now:                time.Now,
		inFlightHostInputs: map[string]struct{}{},
	}
}

// RunLifecycleScheduler owns durable retry and claim-expiry timing for the
// daemon. It waits on the exact next_attempt_at/deadline or a lifecycle commit;
// no Brain turn is kept alive and no Session is polled while waiting.
func (s *Service) RunLifecycleScheduler(ctx context.Context) {
	if s == nil || s.store == nil || s.store.FSM() == nil {
		return
	}
	engine := s.store.FSM()
	for {
		var timer *time.Timer
		var timerC <-chan time.Time
		if next, ok := engine.NextWakeAt(); ok {
			delay := time.Until(next)
			if delay < 0 {
				delay = 0
			}
			timer = time.NewTimer(delay)
			timerC = timer.C
		}
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-engine.Wakeups():
			if timer != nil {
				timer.Stop()
			}
			continue
		case <-timerC:
		}

		s.reconcileMu.Lock()
		err := s.store.SweepLifecycle()
		s.reconcileMu.Unlock()
		if err != nil {
			log.Printf("brain lifecycle timer: %v", err)
		}
		if _, err := s.ReconcileHostLane(); err != nil {
			log.Printf("brain lifecycle event delivery: %v", err)
		}
	}
}

// Turn returns the canonical ledger snapshot for the session. It implements
// watcher.TurnLedger so the watcher reads the same canonical owner.
func (s *Service) Turn(sessionID string) (watcher.TurnSnapshot, bool, error) {
	if s == nil || s.store == nil {
		return watcher.TurnSnapshot{}, false, nil
	}
	return s.store.Turn(sessionID)
}

// ApplyTurnFact applies one observation through the single canonical reducer.
// It implements watcher.TurnLedger; the store persists turn + derived Work +
// presentation event atomically, so this method never dispatches directly; the
// resulting Session event / reconcile loop re-drives the Host lane.
func (s *Service) ApplyTurnFact(fact watcher.TurnFact) (watcher.TurnSnapshot, bool, error) {
	if s == nil || s.store == nil {
		return watcher.TurnSnapshot{}, false, fmt.Errorf("brain store is not configured")
	}
	return s.store.ApplyTurnFact(fact)
}

func (s *Service) ApplyDelegatedTurnProgress(fact watcher.TurnFact) (watcher.TurnProgressResult, error) {
	if s == nil || s.store == nil {
		return watcher.TurnProgressResult{}, fmt.Errorf("brain store is not configured")
	}
	return s.store.ApplyDelegatedTurnProgress(fact)
}

func (s *Service) PrepareInputAdmission(submission watcher.InputAdmission) (watcher.InputAdmission, bool, error) {
	if s == nil || s.store == nil {
		return watcher.InputAdmission{}, false, fmt.Errorf("brain store is not configured")
	}
	return s.store.PrepareInputAdmission(submission)
}

func (s *Service) InputAdmission(sessionID, proposedTurnID string) (watcher.InputAdmission, bool, error) {
	if s == nil || s.store == nil {
		return watcher.InputAdmission{}, false, nil
	}
	return s.store.InputAdmission(sessionID, proposedTurnID)
}

func (s *Service) PendingInputAdmissions(sessionID string) ([]watcher.InputAdmission, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	return s.store.PendingInputAdmissions(sessionID)
}

func (s *Service) ResolveInputAdmission(resolution watcher.InputAdmissionResolution) (watcher.InputAdmission, error) {
	if s == nil || s.store == nil {
		return watcher.InputAdmission{}, fmt.Errorf("brain store is not configured")
	}
	return s.store.ResolveInputAdmission(resolution)
}

func (s *Service) MarkInputAdmissionAmbiguous(sessionID, proposedTurnID, reason string) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("brain service is not configured")
	}
	return s.store.MarkInputAdmissionAmbiguous(sessionID, proposedTurnID, reason)
}

func (s *Service) AbortInputAdmission(sessionID, proposedTurnID, receipt, payloadSHA256 string) (watcher.InputAdmission, error) {
	if s == nil || s.store == nil {
		return watcher.InputAdmission{}, fmt.Errorf("brain store is not configured")
	}
	submission, abortErr := s.store.AbortInputAdmission(sessionID, proposedTurnID, receipt, payloadSHA256)
	// The transport abort is evidence only. Re-drive the canonical lane in case
	// another aggregate already has review work ready for the Host.
	_, dispatchErr := s.ReconcileHostLane()
	return submission, errors.Join(abortErr, dispatchErr)
}

func (s *Service) Snapshot() (Snapshot, error) {
	snapshot, err := s.store.Snapshot()
	if err != nil {
		return Snapshot{}, err
	}
	chatThreadID, err := s.store.ChatThreadID()
	if err != nil {
		return Snapshot{}, err
	}
	hostExecutor := s.hostExecutor()
	host, err := s.ensureHostAgent(hostExecutor)
	if err != nil {
		return Snapshot{}, err
	}
	delegatedExecutor := s.brainDelegatedExecutor()
	if host.ID != "" {
		snapshot.HostAgent = &host
	}
	hostExecutor.Host = true
	if hostExecutor.ID == delegatedExecutor.ID {
		hostExecutor.Delegated = true
	}
	snapshot.HostExecutor = &hostExecutor
	delegatedExecutor.Delegated = true
	if delegatedExecutor.ID == hostExecutor.ID {
		delegatedExecutor.Host = true
	}
	snapshot.DelegatedExecutor = &delegatedExecutor
	snapshot.Executors = s.agentExecutors(hostExecutor.ID, delegatedExecutor.ID)
	snapshot.ChatThreadID = chatThreadID
	snapshot.Agents = s.agentRefs(host.ID)
	inventory, err := s.store.ProjectWorkInventory(presentDelegatedSessions(snapshot.Agents))
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.CurrentWork = inventory.Current
	snapshot.WorkBacklog = inventory.Backlog
	return snapshot, nil
}

// ProjectionSnapshot builds a brain_snapshot for wire projection without
// ensureHostAgent. Hidden-host discovery/removal refreshes use this so
// capability convergence never creates, resumes, rebinds, transfers routes,
// or rewrites host binding. Continuity remains owned by Snapshot, NewChat,
// and other intentional lifecycle entry points under tri-state/route-transfer
// rules.
func (s *Service) ProjectionSnapshot() (Snapshot, error) {
	if s == nil || s.store == nil {
		return Snapshot{}, fmt.Errorf("brain service is not configured")
	}
	snapshot, err := s.store.Snapshot()
	if err != nil {
		return Snapshot{}, err
	}
	chatThreadID, err := s.store.ChatThreadID()
	if err != nil {
		return Snapshot{}, err
	}
	hostExecutor := s.hostExecutor()
	host, err := s.projectedHostAgent(hostExecutor)
	if err != nil {
		return Snapshot{}, err
	}
	delegatedExecutor := s.brainDelegatedExecutor()
	if host.ID != "" {
		snapshot.HostAgent = &host
	}
	hostExecutor.Host = true
	if hostExecutor.ID == delegatedExecutor.ID {
		hostExecutor.Delegated = true
	}
	snapshot.HostExecutor = &hostExecutor
	delegatedExecutor.Delegated = true
	if delegatedExecutor.ID == hostExecutor.ID {
		delegatedExecutor.Host = true
	}
	snapshot.DelegatedExecutor = &delegatedExecutor
	snapshot.Executors = s.agentExecutors(hostExecutor.ID, delegatedExecutor.ID)
	snapshot.ChatThreadID = chatThreadID
	snapshot.Agents = s.agentRefs(host.ID)
	inventory, err := s.store.ProjectWorkInventory(presentDelegatedSessions(snapshot.Agents))
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.CurrentWork = inventory.Current
	snapshot.WorkBacklog = inventory.Backlog
	return snapshot, nil
}

// projectedHostAgent returns the recorded host for wire projection only.
// It never probes for replacement and never mutates store/route/tmux state.
func (s *Service) projectedHostAgent(executor work.AgentExecutor) (AgentRef, error) {
	if s == nil || s.store == nil {
		return AgentRef{}, nil
	}
	hostSession, err := s.store.HostSession()
	if err != nil {
		return AgentRef{}, err
	}
	id := strings.TrimSpace(hostSession.ID)
	if id == "" {
		return AgentRef{}, nil
	}
	if s.watcher != nil {
		if agent := s.watcher.GetAgent(id); agent != nil {
			return agentRefFromClassifier(agent), nil
		}
	}
	command := ""
	if cmd, cmdErr := s.hostCommand(executor); cmdErr == nil {
		command = cmd
	}
	return AgentRef{
		ID:      id,
		Name:    "Brain",
		Status:  string(classifier.StateUnknown),
		Summary: "Session not observed",
		Cwd:     s.brainWorkspace(),
		Command: command,
		Updated: firstNonZeroTime(hostSession.UpdatedAt, s.now().UTC()),
		Hidden:  true,
	}, nil
}

func (s *Service) Context() (BrainContext, error) {
	if s == nil || s.store == nil {
		return BrainContext{}, fmt.Errorf("brain service is not configured")
	}
	snapshot, err := s.Snapshot()
	if err != nil {
		return BrainContext{}, err
	}
	playbooks, err := s.store.PlaybookCatalog()
	if err != nil {
		return BrainContext{}, err
	}
	return BrainContext{
		ThreadID:          snapshot.ChatThreadID,
		Workspace:         snapshot.Workspace,
		Current:           snapshot.Current,
		Memory:            snapshot.Memory,
		Profile:           snapshot.Profile,
		Personality:       snapshot.Personality,
		CurrentWork:       snapshot.CurrentWork,
		WorkBacklog:       snapshot.WorkBacklog,
		Playbooks:         playbooks.Playbooks,
		HostAgent:         snapshot.HostAgent,
		HostExecutor:      snapshot.HostExecutor,
		DelegatedExecutor: snapshot.DelegatedExecutor,
		Executors:         snapshot.Executors,
		Agents:            snapshot.Agents,
		GeneratedAt:       s.nowUTC(),
	}, nil
}

func (s *Service) Housekeeping() (HousekeepingReport, error) {
	if s == nil || s.store == nil {
		return HousekeepingReport{}, fmt.Errorf("brain service is not configured")
	}
	before, err := workspaceContentIdentity(s.store)
	if err != nil {
		return HousekeepingReport{}, err
	}
	if err := s.store.ensureFiles(); err != nil {
		return HousekeepingReport{}, err
	}
	after, err := workspaceContentIdentity(s.store)
	if err != nil {
		return HousekeepingReport{}, err
	}
	changedPaths := changedWorkspacePaths(before, after)
	context, err := s.Context()
	if err != nil {
		return HousekeepingReport{}, err
	}
	delegated := []AgentRef{}
	for _, agent := range context.Agents {
		if agent.Delegated {
			delegated = append(delegated, agent)
		}
	}
	steps := []string{}
	if strings.TrimSpace(context.Current) == "" || strings.Contains(context.Current, "None recorded yet.") {
		steps = append(steps, "Update current.md with the active objective, decisions, open threads, and next step.")
	}
	if len(delegated) > 0 {
		steps = append(steps, "Inspect open delegated agents and close only those whose larger task is complete and reported.")
	}
	return HousekeepingReport{
		Workspace:            s.store.WorkspacePath(),
		CurrentPath:          "current.md",
		SoulPath:             "soul.md",
		PolicyPaths:          []string{"policies/delegation.md", "policies/engine.md", "policies/handoff.md"},
		PlaybookPaths:        seedPlaybookPaths(),
		WorklogPath:          worklogDirName,
		OpenDelegatedAgents:  delegated,
		ChangedPaths:         changedPaths,
		RecommendedNextSteps: steps,
		GeneratedAt:          s.nowUTC(),
	}, nil
}

type workspaceFileIdentity struct {
	exists bool
	digest [sha256.Size]byte
}

func workspaceContentIdentity(store *Store) (map[string]workspaceFileIdentity, error) {
	identities := make(map[string]workspaceFileIdentity)
	for _, relativePath := range standardWorkspaceRelativePaths() {
		path := filepath.Join(store.WorkspacePath(), filepath.FromSlash(relativePath))
		raw, exists, err := readOptionalFile(path)
		if err != nil {
			return nil, fmt.Errorf("read Brain workspace identity %s: %w", relativePath, err)
		}
		identity := workspaceFileIdentity{exists: exists}
		if exists {
			identity.digest = sha256.Sum256(raw)
		}
		identities[relativePath] = identity
	}
	return identities, nil
}

func changedWorkspacePaths(before, after map[string]workspaceFileIdentity) []string {
	changed := []string{}
	for path, afterIdentity := range after {
		if beforeIdentity, ok := before[path]; !ok || beforeIdentity != afterIdentity {
			changed = append(changed, path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return changed
}

func (s *Service) WorkspaceTree(paths ...string) (WorkspaceTree, error) {
	if s == nil || s.store == nil {
		return WorkspaceTree{}, fmt.Errorf("brain store is not configured")
	}
	return s.store.WorkspaceTree(paths...)
}

func (s *Service) PlaybookCatalog() (PlaybookCatalog, error) {
	if s == nil || s.store == nil {
		return PlaybookCatalog{}, fmt.Errorf("brain store is not configured")
	}
	return s.store.PlaybookCatalog()
}

func (s *Service) ReadWorkspaceFile(path string) (WorkspaceFile, error) {
	if s == nil || s.store == nil {
		return WorkspaceFile{}, fmt.Errorf("brain store is not configured")
	}
	return s.store.ReadWorkspaceFile(path)
}

func (s *Service) SetHostExecutor(executorID string) (Snapshot, error) {
	if s == nil || s.store == nil {
		return Snapshot{}, fmt.Errorf("brain store is not configured")
	}
	executorID = strings.TrimSpace(executorID)
	if executorID == "" {
		return Snapshot{}, fmt.Errorf("brain host executor is required")
	}
	if locked := brainHostExecutorOverride(); locked != "" && locked != executorID {
		return Snapshot{}, ErrExecutorLockedByEnv
	}
	if s.execs == nil {
		return Snapshot{}, ErrExecutorNotConfigured
	}
	executor, ok := s.execs.AgentExecutor(executorID)
	if !ok {
		return Snapshot{}, fmt.Errorf("%w: %s", ErrExecutorNotConfigured, executorID)
	}
	var previousHost HostSession
	var currentContext string
	chatThreadID, _ := s.store.ChatThreadID()
	if host, err := s.store.HostSession(); err == nil {
		previousHost = host
	}
	if snapshot, err := s.store.Snapshot(); err == nil {
		currentContext = snapshot.Current
	}
	if err := s.store.SetHostExecutorID(executor.ID); err != nil {
		return Snapshot{}, err
	}
	snapshot, err := s.Snapshot()
	if err != nil {
		return Snapshot{}, err
	}
	if snapshot.HostAgent != nil && strings.TrimSpace(previousHost.ID) != "" && strings.TrimSpace(snapshot.HostAgent.ID) != "" && snapshot.HostAgent.ID != strings.TrimSpace(previousHost.ID) {
		_ = s.handoffHostSession(chatThreadID, previousHost.ExecutorID, executor.ID, snapshot.HostAgent.ID, currentContext, snapshot.Agents)
	}
	return snapshot, nil
}

// RouteSessionEvent records the executor fact against its owning Work before
// attempting a wake. Provider transcript state is never scheduler authority.
//
// Sessions with a canonical ledger turn are already owned by the single
// reducer: Work status and presentation events were derived at fact-apply
// time (watcher poll facts, control-plane facts, liveness facts). This route
// only re-drives delivery for those sessions. Markerless/projection sessions
// remain non-actionable without exact Turn identity.
func (s *Service) RouteSessionEvent(event watcher.SessionEvent) (bool, error) {
	if s == nil || s.store == nil || event.Agent == nil {
		return false, nil
	}
	agent := event.Agent
	if !agent.Delegated || agent.Hidden || strings.TrimSpace(agent.ID) == "" {
		return false, nil
	}
	if _, hasTurn, turnErr := s.store.Turn(agent.ID); turnErr != nil {
		return false, turnErr
	} else if hasTurn {
		// Canonical-turn path: the ledger already derived Work + Events; this
		// route only re-drives delivery of newly actionable rows. Liveness on
		// agent_removed was applied by the watcher before the removal event;
		// ownership stays attached until Brain resolves session.uncertain.
		return s.ReconcileHostLane()
	}
	// No canonical current TurnID: no delegated lifecycle event exists. The
	// legacy raw-state projection (sessionEventProjection), occurrence
	// counting, and terminal-kind string scanning are deleted; a markerless
	// accepted input is unrepresentable.
	return false, nil
}

func terminalSessionWorkUpdate(kind string) WorkUpdate {
	status := WorkWaiting
	next := "Review the delegated Session result."
	if kind == "session.failed" {
		next = "Inspect the delegated Session failure."
	}
	empty := ""
	var noWake *WorkWake
	return WorkUpdate{
		Status:     &status,
		NextAction: &next,
		WaitFor:    &empty,
		Wake:       &noWake,
	}
}

func sessionTurnEventDedupeKey(sessionID, turnID, kind string) string {
	return fmt.Sprintf(
		"session:%s:turn:%s:%s",
		strings.TrimSpace(sessionID),
		strings.TrimSpace(turnID),
		strings.TrimSpace(kind),
	)
}

func isSessionLifecycleKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "session.running", "session.waiting", "session.needs_input",
		"session.done", "session.failed", "session.stale", "session.uncertain":
		return true
	default:
		return false
	}
}

// isTurnScopedSessionDedupeKey reports whether a delegated lifecycle dedupe
// key carries the canonical TurnID shape
// (session:<sessionID>:turn:<turnID>:<kind>). Occurrence-counting and
// bare-session keys are unrepresentable for lifecycle events. The turn scope
// marker alone is the discriminator: legacy unscoped keys never contain it,
// and the TurnID itself may embed the Session ID (turnID =
// sessionID+":turn:N"), so no further structure is validated.
func isTurnScopedSessionDedupeKey(dedupeKey string) bool {
	return strings.HasPrefix(strings.TrimSpace(dedupeKey), "session:") &&
		strings.Contains(dedupeKey, ":turn:")
}

// isCanonicalSessionWakeDedupeKey is the durable shape produced only by
// wakeWaitingWorkLocked after the exact Session Turn terminalizes in the same
// transaction. Generic Event append rejects unscoped lifecycle keys, while
// unscoped rows never have this typed wake prefix.
func isCanonicalSessionWakeDedupeKey(dedupeKey string) bool {
	dedupeKey = strings.TrimSpace(dedupeKey)
	return strings.HasPrefix(dedupeKey, "wake:session_terminal:session:") &&
		strings.Contains(dedupeKey, ":turn:")
}

func workUpdateChanges(item Work, update WorkUpdate) bool {
	return update.Title != nil && strings.TrimSpace(*update.Title) != item.Title ||
		update.Objective != nil && strings.TrimSpace(*update.Objective) != item.Objective ||
		update.Status != nil && *update.Status != item.Status ||
		update.AttemptSessionID != nil && strings.TrimSpace(*update.AttemptSessionID) != item.AttemptSessionID ||
		update.CompletionPolicy != nil && *update.CompletionPolicy != item.CompletionPolicy ||
		update.DoneCriteriaRef != nil && strings.TrimSpace(*update.DoneCriteriaRef) != item.DoneCriteriaRef ||
		update.NextAction != nil && strings.TrimSpace(*update.NextAction) != item.NextAction ||
		update.WaitFor != nil && strings.TrimSpace(*update.WaitFor) != item.WaitFor ||
		update.Wake != nil && !workWakeEqual(*update.Wake, item.Wake) ||
		update.ContextRef != nil && strings.TrimSpace(*update.ContextRef) != item.ContextRef
}

func (s *Service) providerTerminalFact(
	sessionID, turnID string,
	observation watcher.ProviderActivityObservation,
	kind string,
) watcher.TurnFact {
	return watcher.TurnFact{
		SessionID:  sessionID,
		TurnID:     turnID,
		Class:      watcher.EvidenceProvider,
		Kind:       kind,
		SourceID:   providerFactSourceID(sessionID, observation),
		Cursor:     observation.AdmissionCursor,
		Admission:  admissionFromObservation(observation),
		ActivityID: strings.TrimSpace(observation.ID),
		StartedAt:  observation.StartedAt,
		SettledAt:  observation.SettledAt,
		At:         s.nowUTC(),
		Summary:    "Delegated provider completed the turn",
	}
}

func providerFactSourceID(sessionID string, observation watcher.ProviderActivityObservation) string {
	return fmt.Sprintf("provider\x00%s\x00%s\x00%s\x00%d",
		sessionID,
		firstNonEmpty(observation.AdmissionStream, "stream"),
		firstNonEmpty(observation.ID, observation.AdmissionID),
		observation.AdmissionCursor,
	)
}

func admissionFromObservation(observation watcher.ProviderActivityObservation) watcher.TurnAdmission {
	return watcher.TurnAdmission{
		Stream: strings.TrimSpace(observation.AdmissionStream),
		ID:     strings.TrimSpace(observation.AdmissionID),
		Cursor: observation.AdmissionCursor,
		SHA256: strings.TrimSpace(observation.InputSHA256),
		At:     observation.AdmissionAt.UTC(),
	}
}

// ReconcileHostLane is the single serialized Host-lane reducer. Every
// trigger — Work Event append, Host provider state change, startup
// reconciliation, and new Brain user input — enters this same reducer under
// one mutex; trigger identity never changes semantics. It derives the next
// action entirely from persisted state and current strong evidence:
//
//  1. reconcile every existing delivery receipt (exact-once Lifecycle admission state)
//     and every prepared Brain input against its exact request receipt
//  2. reconcile a Host foreground turn from strong exact terminal evidence,
//     without making that turn or ambient provider Activity an admission gate
//  3. one delivered Event awaiting its typed disposition: stop
//  4. pending Brain user admission: stop (durable user-steering gate)
//  5. select one fair pending Work key
//  6. atomically claim its current Event head
//  7. submit once with the existing receipt ledger
//  8. mark delivered only from the accepted receipt
func (s *Service) ReconcileHostLane() (bool, error) {
	if s == nil || s.store == nil || s.watcher == nil {
		return false, nil
	}
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()
	return s.reconcileHostLaneLocked()
}

// reconcileHostLaneLocked is the reducer body; the caller holds dispatchMu.
// Review lease recovery is four-state with no time-based release (C.2.7):
// a provably absent receipt releases the lease immediately; an accepted
// receipt consumes it (delivered); an ambiguous receipt or an inaccessible
// host quarantines the lease in Work state and surfaces a deduped delivery
// diagnostic while unrelated events keep dispatching. A lease whose Host is
// gone is recovered from the durable Lifecycle admission state: no exact submission
// (or an Aborted one) proves the action was never sent, so the same
// unresolved action is re-claimable; a Pending/Resolved exact submission
// stays the quarantine boundary. Held leases close only via explicit
// ResolveReviewLease (mark_delivered/discard/replay), a receipt-state change,
// or that evidence-based recovery — never by elapsed time. The exact held
// lease is the quarantine boundary: it is never replayed, while unrelated
// Work continues through the ordinary serialized admission gates instead of
// inheriting a Session-wide fence.
func (s *Service) reconcileHostLaneLocked() (bool, error) {
	if s == nil || s.store == nil || s.watcher == nil {
		return false, nil
	}
	if err := s.reconcileReviewLeasesLocked(); err != nil {
		return false, err
	}
	hostSession, err := s.store.HostSession()
	if err != nil {
		return false, err
	}
	hostID := strings.TrimSpace(hostSession.ID)
	if err := s.reconcileBrainInputAdmissionsLocked(hostID); err != nil {
		return false, err
	}
	active, err := s.store.CurrentHostForegroundTurn()
	if err != nil {
		return false, err
	}
	if hostID == "" {
		if active != nil {
			if err := s.retireHostForegroundLocked(*active, hostID, "", "host_binding_removed"); err != nil {
				return false, err
			}
		}
		return false, nil
	}
	hostPresence, presenceErr := s.watcher.ProbeSession(hostID)
	if presenceErr != nil || hostPresence == watcher.SessionPresenceUnknown {
		if presenceErr == nil {
			presenceErr = fmt.Errorf("Brain Host Session %s liveness is unknown", hostID)
		}
		return false, presenceErr
	}
	if hostPresence == watcher.SessionPresenceAbsent {
		if active != nil {
			if err := s.retireHostForegroundLocked(*active, hostID, "", "host_session_absent"); err != nil {
				return false, err
			}
		}
		return false, nil
	}
	if active != nil {
		switch {
		case active.HostSessionID != hostID:
			if err := s.retireHostForegroundLocked(*active, hostID, "", "host_binding_replaced"); err != nil {
				return false, err
			}
			active = nil
		default:
			currentGeneration, generationErr := s.hostOwnedGeneration(hostID)
			if generationErr != nil {
				return false, generationErr
			}
			if currentGeneration != active.HostGeneration {
				if err := s.retireHostForegroundLocked(
					*active, hostID, currentGeneration, "host_generation_replaced",
				); err != nil {
					return false, err
				}
				active = nil
			}
		}
	}
	// Step 2: reconcile the accepted foreground Host turn without using it as
	// an admission boundary. Only the exact bound provider activity's terminal
	// status (or the current observation's terminal status for an unbound turn)
	// closes it. A running observation binds the durable activity identity once.
	// A queued Review admission never adopts, replaces, or closes this row.
	if active != nil && active.HostSessionID == hostID {
		generation := active.HostGeneration
		activityID := ""
		bindActivity := ""
		exactTerminal := false
		var probeErr error
		var observation watcher.ProviderActivityObservation
		var found bool
		observation, found, probeErr = s.watcher.ProbeProviderEvidence(hostID)
		if probeErr == nil && found {
			observedID := strings.TrimSpace(observation.ID)
			bound := strings.TrimSpace(active.ProviderActivityID)
			if bound != "" {
				// Terminal evidence is exact only when it names the durable
				// turn's bound Activity — either as the current observation
				// or from the same source's bounded terminal history. A
				// delayed terminal observation for a replaced Activity is
				// never adopted and never closes this turn.
				activityID, exactTerminal = hostForegroundTerminalEvidence(observation, bound)
			} else {
				status := strings.TrimSpace(observation.Status)
				if providerStatusRunning(status) {
					bindActivity = observedID
				} else if providerStatusTerminal(status) &&
					!observation.StartedAt.IsZero() && !observation.StartedAt.Before(active.StartedAt) {
					// The turn's response already ended (for example while
					// the daemon was down) before any running observation
					// could bind it. The current activity ending is adopted
					// only when it began inside this turn's admission
					// window: a stale terminal that began before the durable
					// Prepare boundary is never a boundary for the new turn.
					activityID = observedID
					exactTerminal = true
				}
			}
		} else if probeErr == nil && strings.TrimSpace(active.ProviderActivityID) == "" {
			agent := s.watcher.GetAgent(hostID)
			if agent != nil && (agent.State == classifier.StateDone || agent.State == classifier.StateFailed || agent.State == classifier.StateUnknown) {
				exactTerminal = true
			}
		}
		if bindActivity != "" {
			if bindErr := s.store.BindHostForegroundActivity(hostID, generation, active.HostTurnID, bindActivity); bindErr != nil {
				return false, bindErr
			}
		}
		if exactTerminal {
			if closeErr := s.store.CloseHostForegroundTurn(hostID, generation, active.HostTurnID, activityID); closeErr != nil {
				return false, closeErr
			}
		}
	}
	// Step 3: one delivered review awaits its typed disposition. The Host is
	// mid-review; no new admission may overtake it.
	if delivered, err := s.store.HasLiveDeliveredReview(); err != nil {
		return false, err
	} else if delivered {
		return false, nil
	}
	// Step 4: a pending Brain user admission is the durable user-steering
	// gate. Pending is persisted before provider mutation, so while it exists
	// the lane must not admit an internal Event ahead of the user's message.
	if pending, err := s.store.PendingHostInputAdmission(hostID); err != nil {
		return false, err
	} else if pending {
		return false, nil
	}
	// Steps 5-8: select one fair review-required Work, claim
	// its current action atomically, submit once through the receipt ledger,
	// and mark delivered only from the accepted receipt. Claims are leases:
	// Host replacement/death re-delivers the same unresolved action and never
	// creates a separate queue item.
	action, claimed, err := s.store.ClaimNextReviewAction(hostID)
	if err != nil || !claimed {
		return false, err
	}
	return s.deliverClaimedReviewLocked(action)
}

func (s *Service) retireHostForegroundLocked(
	active HostForegroundTurn,
	currentHostID, currentGeneration, reason string,
) error {
	retired, err := s.store.RetireHostForegroundTurn(active)
	if err != nil || !retired {
		return err
	}
	s.recordHostReplacement(HostReplacementEvent{
		Reason: reason,
		FromID: active.HostSessionID,
		ToID:   strings.TrimSpace(currentHostID),
		Detail: fmt.Sprintf(
			"retired_foreground_turn=%q old_generation=%q current_generation=%q provider_activity=%q",
			active.HostTurnID,
			active.HostGeneration,
			strings.TrimSpace(currentGeneration),
			active.ProviderActivityID,
		),
	})
	return nil
}

// reconcileBrainInputAdmissionsLocked terminalizes every prepared user input
// from exact receipt authority before the lane consults its steering gate. The
// caller holds dispatchMu; no Store mutex is held across watcher probes.
func (s *Service) reconcileBrainInputAdmissionsLocked(currentHostID string) error {
	pending, err := s.store.PendingBrainInputAdmissions()
	if err != nil {
		return err
	}
	for _, admission := range pending {
		if _, active := s.inFlightHostInputs[hostInputAttemptKey(admission.RequestID, admission.ThreadID)]; active {
			continue
		}
		state := BrainInputAdmissionUncertain
		createForeground := false
		admissionHostID := strings.TrimSpace(admission.HostSessionID)
		if admissionHostID != "" && s.watcher.HasSession(admissionHostID) {
			owned, ownershipErr := s.watcher.ResolveBrainHostGeneration(admissionHostID)
			identityPreserved := ownershipErr == nil &&
				strings.TrimSpace(owned.Generation) != "" &&
				strings.TrimSpace(owned.Generation) == strings.TrimSpace(admission.HostGeneration)
			if identityPreserved {
				result, found, receiptErr := s.watcher.InputReceiptResult(admissionHostID, admission.RequestID)
				switch {
				case receiptErr != nil:
					state = BrainInputAdmissionUncertain
				case found && result.Outcome == watcher.InputAccepted:
					state = BrainInputAdmissionAccepted
					createForeground = admissionHostID == strings.TrimSpace(currentHostID)
				case found && result.Outcome == watcher.InputNotSubmitted:
					state = BrainInputAdmissionNotSubmitted
				case found:
					state = BrainInputAdmissionUncertain
				default:
					// The request ledger is written before provider mutation.
					// Absence is proof only while the exact pane generation is
					// still preserved.
					state = BrainInputAdmissionNotSubmitted
				}
			}
		}
		settled, changed, err := s.store.SettleBrainInputAdmission(
			admission.RequestID,
			admission.ThreadID,
			state,
			createForeground,
		)
		if err != nil {
			return err
		}
		if changed && settled.State == BrainInputAdmissionAccepted {
			if err := s.store.ProjectBrainInputAdmission(settled); err != nil {
				// Projection is independently retryable and is not scheduler
				// authority. Never re-block the lane after exact settlement.
				log.Printf("brain recovered input projection failed for %s: %v", settled.RequestID, err)
			}
		}
	}
	return nil
}

// reconcileReviewLeasesLocked is reducer step 1: every review lease's provider
// submission receipt is reconciled against the exact lease capability. An
// ambiguous exact transaction remains a durable lease quarantine with an
// audited delivery.ambiguous note; that quarantine is replay-proof but is not
// a lane-wide stop condition for unrelated Work. A lease whose Host Session is
// gone is recovered from the durable Lifecycle admission state: absent/Aborted exact
// submission proves the action was never delivered, so the lease is dropped
// and the same unresolved action is re-claimable by the current Host (I8); a
// Pending/Resolved exact submission quarantines the lease in Work state (I7).
func (s *Service) reconcileReviewLeasesLocked() error {
	leases, err := s.store.LeasedReviewActions()
	if err != nil {
		return err
	}
	for _, claimed := range leases {
		// Delivered leases await the typed disposition (lane stop gate) and
		// ended leases await re-claim; neither is receipt-reconciled here.
		if claimed.DeliveredAt != nil || claimed.HandlingEndedAt != nil {
			continue
		}
		hostID := claimed.DeliveryHostSessionID
		if !s.watcher.HasSession(hostID) {
			// The claiming Host Session is gone. Recover from the durable
			// Lifecycle admission state: no exact submission (or an Aborted one)
			// proves the Event was never sent, so the lease is dropped and the
			// same unresolved action is re-claimable; Pending/Resolved exact
			// submission means mutation may have begun and the lease is
			// quarantined in Work state for explicit actor resolution.
			if _, err := s.store.RecoverReviewLease(
				claimed.WorkID, claimed.HandlingID, claimed.ProviderTurnID,
			); err != nil {
				return err
			}
			continue
		}
		result, found, receiptErr := s.watcher.InputReceiptResult(hostID, claimed.ProviderTurnID)
		if receiptErr != nil || !found {
			if receiptErr != nil {
				// Transient receipt-ledger read failure: retry on the next
				// wake; never release by elapsed time.
				continue
			}
			if _, providerMutated, turnErr := s.store.TurnByID(hostID, claimed.ProviderTurnID); turnErr != nil {
				return turnErr
			} else if providerMutated {
				// Canonical provider admission dominates an absent transport
				// receipt. Mutation began, so hold both authorities without
				// replay and let ordinary admission/receipt recovery converge.
				continue
			}
			// Receipt absent: host receipts are written before the host
			// mutates, so the mutation provably never began. Release.
			if releaseErr := s.store.ReleaseReviewLease(
				claimed.WorkID, claimed.HandlingID, claimed.ProviderTurnID,
			); releaseErr != nil {
				return fmt.Errorf("release provably-unsent Work review %s: %w", claimed.WorkID, releaseErr)
			}
			continue
		}
		// Canonical provider admission dominates every transport receipt state,
		// including an ambiguous pre-mutation marker left by a process crash.
		// Consume through the exact lease capability; the Store independently
		// verifies the matching resolved submission.
		if _, providerMutated, turnErr := s.store.TurnByID(hostID, claimed.ProviderTurnID); turnErr != nil {
			return turnErr
		} else if providerMutated {
			if _, _, consumeErr := s.store.ConsumeReviewDelivery(
				claimed.WorkID, claimed.HandlingID, claimed.ProviderTurnID,
			); consumeErr != nil {
				return fmt.Errorf("finalize admitted Work review %s: %w", claimed.WorkID, consumeErr)
			}
			continue
		}
		switch result.Outcome {
		case watcher.InputAccepted:
			if _, found, turnErr := s.store.TurnByID(hostID, claimed.ProviderTurnID); turnErr != nil {
				return turnErr
			} else if !found {
				// The transport receipt alone is not a provider Turn. Hold the
				// lease without replay until canonical admission is recoverable.
				continue
			}
			if _, _, err := s.store.ConsumeReviewDelivery(
				claimed.WorkID, claimed.HandlingID, claimed.ProviderTurnID,
			); err != nil {
				return fmt.Errorf("finalize accepted Work review %s: %w", claimed.WorkID, err)
			}
		case watcher.InputAmbiguous:
			// Retry only the exact pending capability. The watcher recognizes its
			// canonical transaction before any provider mutation and may resolve
			// it from provider admission/transcript evidence; it never replays the
			// payload. The original ambiguous receipt remains dominant, so a
			// failed recovery never releases the lease.
			recovered, _, recoveryErr := s.recoverAmbiguousReviewLocked(claimed)
			if recoveryErr == nil && recovered {
				continue
			}
			if _, _, noteErr := s.store.AppendDeliveryNote(
				claimed.WorkID,
				claimed.EventID,
				"delivery.ambiguous",
				"delivery:"+claimed.EventID+":ambiguous",
				"Delivery of Work review "+claimed.EventID+" is quarantined because its exact provider outcome remains ambiguous. It will not be replayed automatically; wait for exact provider evidence or resolve the held lease explicitly (mark_delivered, discard, or replay).",
				false,
			); noteErr != nil {
				return fmt.Errorf("persist ambiguous delivery quarantine for Work review %s: %w", claimed.WorkID, noteErr)
			}
		default:
			// InputNotSubmitted: the receipt exists and proves non-submission.
			if releaseErr := s.store.ReleaseReviewLease(
				claimed.WorkID, claimed.HandlingID, claimed.ProviderTurnID,
			); releaseErr != nil {
				return fmt.Errorf("release definitely unsent Work review %s: %w", claimed.WorkID, releaseErr)
			}
		}
	}
	return nil
}

// recoverAmbiguousReviewLocked re-enters only an already-persisted exact
// pending transaction. Reconstructed bytes must match its durable digest
// before the watcher is called. The watcher duplicate path may resolve from
// authoritative provider evidence but cannot enqueue input again. A failed
// retry never releases the original ambiguous authority.
func (s *Service) recoverAmbiguousReviewLocked(claimed WorkReviewAction) (bool, bool, error) {
	hostID := strings.TrimSpace(claimed.DeliveryHostSessionID)
	submission, found, err := s.store.InputAdmission(hostID, claimed.ProviderTurnID)
	if err != nil || !found || submission.State != watcher.InputAdmissionPending {
		return false, false, err
	}
	if submission.Receipt != claimed.ProviderTurnID || submission.ClaimToken != claimed.HandlingID ||
		submission.WorkID != claimed.WorkID || submission.SessionID != hostID ||
		submission.ProposedTurnID != claimed.ProviderTurnID {
		return false, false, fmt.Errorf("ambiguous Work review %s lacks its exact pending submission", claimed.WorkID)
	}
	item, err := s.store.Work(claimed.WorkID)
	if err != nil {
		return false, true, err
	}
	payload, err := marshalDirectWorkEventInput(claimed, item)
	if err != nil {
		return false, true, err
	}
	if AdmissionDigest(payload) != submission.PayloadSHA256 {
		return false, true, fmt.Errorf("ambiguous Work review %s payload no longer matches its pending submission", claimed.WorkID)
	}
	if claimed.ClaimedAt == nil {
		return false, true, fmt.Errorf("ambiguous Work review %s has no claim timestamp", claimed.WorkID)
	}
	if recovered, matched, err := s.recoverPendingFromBoundHostConversation(submission); err != nil {
		if matched {
			return false, true, err
		}
	} else if recovered {
		if err := s.consumeRecoveredReview(claimed); err != nil {
			return false, true, err
		}
		return true, true, nil
	}
	acceptedAt := claimed.ClaimedAt.UTC()
	result, submitErr := s.watcher.SubmitBrainHostInput(
		hostID, payload, claimed.HandlingID, claimed.WorkID, claimed.ProviderTurnID, acceptedAt,
	)
	if submitErr != nil {
		return false, true, submitErr
	}
	if result.Outcome != watcher.InputAccepted || result.TurnID != claimed.ProviderTurnID {
		return false, true, fmt.Errorf("ambiguous Work review %s recovery returned outcome=%q turn=%q", claimed.WorkID, result.Outcome, result.TurnID)
	}
	if _, found, err := s.store.TurnByID(hostID, claimed.ProviderTurnID); err != nil || !found {
		if err == nil {
			err = fmt.Errorf("canonical provider Turn is missing")
		}
		return false, true, err
	}
	if err := s.consumeRecoveredReview(claimed); err != nil {
		return false, true, err
	}
	return true, true, nil
}

func (s *Service) consumeRecoveredReview(claimed WorkReviewAction) error {
	_, _, err := s.store.ConsumeReviewDelivery(
		claimed.WorkID, claimed.HandlingID, claimed.ProviderTurnID,
	)
	return err
}

// recoverPendingFromBoundHostConversation uses the Host Session's persisted
// provider transcript identity, never cwd/latest-session guessing. The latest
// public provider-native user row must carry the exact pending digest (native
// admission hash or visible body) and occur after admission; Grok chat_history
// rows may omit timestamps, in which case the enclosing Activity clock is the
// admission time. The current provider Activity must be a valid lifecycle
// enclosing that row. Only then may the pending transaction become a canonical
// Turn. matched=true means exact evidence was found and any resolution error is
// a consistency failure rather than a reason to fall back to ambient probing.
func (s *Service) recoverPendingFromBoundHostConversation(
	submission watcher.InputAdmission,
) (recovered bool, matched bool, err error) {
	host, err := s.store.HostSession()
	if err != nil || strings.TrimSpace(host.ID) != strings.TrimSpace(submission.SessionID) ||
		(strings.TrimSpace(host.ProviderSessionID) == "" && strings.TrimSpace(host.TranscriptPath) == "") {
		return false, false, err
	}
	conversation, err := s.HostBoundProviderConversation()
	if err != nil {
		return false, false, err
	}
	resolution, observation, matched := boundHostConversationSubmissionResolution(conversation, submission, s.now().UTC())
	if !matched {
		return false, false, nil
	}
	if _, err := s.store.ResolveInputAdmission(resolution); err != nil {
		return false, true, err
	}
	switch observation.Status {
	case "completed":
		if _, _, err := s.store.ApplyTurnFact(s.providerTerminalFact(
			submission.SessionID, submission.ProposedTurnID, observation, "done",
		)); err != nil {
			return false, true, err
		}
	case "failed", "interrupted", "cancelled":
		if _, _, err := s.store.ApplyTurnFact(s.providerTerminalFact(
			submission.SessionID, submission.ProposedTurnID, observation, "failed",
		)); err != nil {
			return false, true, err
		}
	}
	return true, true, nil
}

func boundHostConversationSubmissionResolution(
	conversation work.CodexConversation,
	submission watcher.InputAdmission,
	resolvedAt time.Time,
) (watcher.InputAdmissionResolution, watcher.ProviderActivityObservation, bool) {
	if !conversation.Available || conversation.Activity == nil ||
		strings.TrimSpace(conversation.Source) == "" || strings.TrimSpace(conversation.SessionID) == "" ||
		strings.TrimSpace(conversation.Path) == "" {
		return watcher.InputAdmissionResolution{}, watcher.ProviderActivityObservation{}, false
	}
	wantDigest := strings.TrimSpace(submission.PayloadSHA256)
	var userEvent *work.CodexConversationEvent
	for index := len(conversation.Events) - 1; index >= 0; index-- {
		event := &conversation.Events[index]
		if event.Kind != "user_message" {
			continue
		}
		if work.IsPrivateHostPrompt(event.Body) {
			continue
		}
		userEvent = event
		break
	}
	if userEvent == nil || strings.TrimSpace(userEvent.ID) == "" || userEvent.Seq <= 0 ||
		!eventMatchesPendingPayloadDigest(*userEvent, wantDigest) {
		return watcher.InputAdmissionResolution{}, watcher.ProviderActivityObservation{}, false
	}
	activity := conversation.Activity
	activityID := strings.TrimSpace(activity.ID)
	activityStartedAt, activityOK := parseBoundHostAdmissionTime(activity.StartedAt)
	if activityID == "" || !activityOK || activityStartedAt.IsZero() {
		return watcher.InputAdmissionResolution{}, watcher.ProviderActivityObservation{}, false
	}
	userAt, userOK := parseBoundHostAdmissionTime(userEvent.Timestamp)
	if !userOK {
		// Grok chat_history user rows have no timestamps. The enclosing
		// provider Activity clock is the native admission time.
		userAt = activityStartedAt
	}
	if userAt.Before(submission.AcceptedAt.UTC()) {
		return watcher.InputAdmissionResolution{}, watcher.ProviderActivityObservation{}, false
	}
	switch activity.Status {
	case work.ProviderActivityRunning, work.ProviderActivityCompleted, work.ProviderActivityFailed,
		work.ProviderActivityInterrupted, work.ProviderActivityCancelled:
	default:
		return watcher.InputAdmissionResolution{}, watcher.ProviderActivityObservation{}, false
	}
	settledAt := time.Time{}
	if strings.TrimSpace(activity.SettledAt) != "" {
		var settledOK bool
		settledAt, settledOK = parseBoundHostAdmissionTime(activity.SettledAt)
		if !settledOK || settledAt.Before(userAt) {
			return watcher.InputAdmissionResolution{}, watcher.ProviderActivityObservation{}, false
		}
	}
	if resolvedAt.Before(userAt) {
		resolvedAt = userAt
	}
	stream := strings.Join([]string{
		strings.TrimSpace(conversation.Source),
		strings.TrimSpace(conversation.SessionID),
		strings.TrimSpace(conversation.Path),
	}, "\x00")
	observation := watcher.ProviderActivityObservation{
		ID: activityID, Status: string(activity.Status),
		StartedAt: activityStartedAt, SettledAt: settledAt,
		Structured:      true,
		AdmissionStream: stream, AdmissionID: strings.TrimSpace(userEvent.ID),
		AdmissionCursor: uint64(userEvent.Seq), AdmissionAt: userAt,
		InputSHA256: wantDigest,
	}
	return watcher.InputAdmissionResolution{
		SessionID: submission.SessionID, ProposedTurnID: submission.ProposedTurnID,
		Receipt: submission.Receipt, PayloadSHA256: submission.PayloadSHA256,
		ActivityID: activityID,
		Admission: watcher.TurnAdmission{
			Stream: stream, ID: strings.TrimSpace(userEvent.ID), Cursor: uint64(userEvent.Seq),
			SHA256: wantDigest, At: userAt,
		},
		ResolvedAt: resolvedAt.UTC(),
	}, observation, true
}

func eventMatchesPendingPayloadDigest(event work.CodexConversationEvent, wantDigest string) bool {
	wantDigest = strings.TrimSpace(wantDigest)
	if wantDigest == "" {
		return false
	}
	if strings.TrimSpace(event.AdmissionSHA256) == wantDigest {
		return true
	}
	body := strings.TrimSpace(event.Body)
	if body == "" {
		return false
	}
	if AdmissionDigest(body) == wantDigest {
		return true
	}
	if parsed, ok := work.ParseCanonicalDirectWorkEventInput(body); ok {
		return AdmissionDigest(work.FormatDirectWorkEventInput(parsed)) == wantDigest
	}
	return false
}

func parseBoundHostAdmissionTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

// hostForegroundTerminalEvidence resolves strong exact terminal evidence for
// the foreground turn's bound provider activity. The current observation
// counts when it names the bound activity with a terminal status; otherwise
// the bounded terminal history from the same durable source is consulted (a
// reusable provider session may have advanced to a newer activity).
func hostForegroundTerminalEvidence(observation watcher.ProviderActivityObservation, boundActivityID string) (string, bool) {
	observedID := strings.TrimSpace(observation.ID)
	status := strings.TrimSpace(observation.Status)
	if observedID == boundActivityID && providerStatusTerminal(status) {
		return observedID, true
	}
	for index := len(observation.TerminalActivities) - 1; index >= 0; index-- {
		terminal := observation.TerminalActivities[index]
		if strings.TrimSpace(terminal.ID) == boundActivityID && providerStatusTerminal(strings.TrimSpace(terminal.Status)) {
			return boundActivityID, true
		}
	}
	return "", false
}

func providerStatusRunning(status string) bool {
	return strings.TrimSpace(status) == "running"
}

func providerStatusTerminal(status string) bool {
	switch strings.TrimSpace(status) {
	case "completed", "failed", "interrupted", "cancelled":
		return true
	default:
		return false
	}
}

func (s *Service) deliverClaimedReviewLocked(action WorkReviewAction) (bool, error) {
	hostID := strings.TrimSpace(action.DeliveryHostSessionID)
	item, err := s.store.Work(action.WorkID)
	if err != nil {
		if releaseErr := s.store.ReleaseReviewLease(
			action.WorkID, action.HandlingID, action.ProviderTurnID,
		); releaseErr != nil {
			return false, fmt.Errorf("release undeliverable Work review %s: %w", action.WorkID, releaseErr)
		}
		return false, err
	}
	payload, err := marshalDirectWorkEventInput(action, item)
	if err != nil {
		if releaseErr := s.store.ReleaseReviewLease(
			action.WorkID, action.HandlingID, action.ProviderTurnID,
		); releaseErr != nil {
			return false, fmt.Errorf("release invalid Work review input %s: %w", action.WorkID, releaseErr)
		}
		return false, err
	}
	acceptedAt := s.now().UTC()
	if action.ClaimedAt != nil {
		acceptedAt = action.ClaimedAt.UTC()
	}
	result, sendErr := s.watcher.SubmitBrainHostInput(
		hostID, payload, action.HandlingID, action.WorkID, action.ProviderTurnID, acceptedAt,
	)
	if sendErr != nil {
		if result.Outcome == watcher.InputNotSubmitted {
			if releaseErr := s.store.ReleaseReviewLease(
				action.WorkID, action.HandlingID, action.ProviderTurnID,
			); releaseErr != nil {
				return false, fmt.Errorf("release definitely unsent Work review %s: %w", action.WorkID, releaseErr)
			}
		}
		if result.Outcome == watcher.InputAmbiguous {
			if recovered, _, recoveryErr := s.recoverAmbiguousReviewLocked(action); recoveryErr == nil && recovered {
				return true, nil
			}
		}
		return false, sendErr
	}
	if result.Outcome != watcher.InputAccepted {
		if result.Outcome == watcher.InputAmbiguous {
			if recovered, _, recoveryErr := s.recoverAmbiguousReviewLocked(action); recoveryErr == nil && recovered {
				return true, nil
			}
		}
		return false, fmt.Errorf("Work review %s Session Input returned non-accepted outcome %q", action.WorkID, result.Outcome)
	}
	if result.TurnID != action.ProviderTurnID {
		return false, fmt.Errorf("Work review %s admitted provider Turn %q, want %q", action.WorkID, result.TurnID, action.ProviderTurnID)
	}
	if _, found, err := s.store.TurnByID(hostID, result.TurnID); err != nil {
		return false, fmt.Errorf("read accepted Host provider Turn %s: %w", result.TurnID, err)
	} else if !found {
		return false, fmt.Errorf("accepted Host provider Turn %s lacks canonical prepare/resolve evidence", result.TurnID)
	}
	if _, _, err := s.store.ConsumeReviewDelivery(
		action.WorkID, action.HandlingID, action.ProviderTurnID,
	); err != nil {
		return false, fmt.Errorf("consume accepted Work review %s: %w", action.WorkID, err)
	}
	return true, nil
}

// NoteUserSteering recognizes the Host agent and enters the lane for
// reconciliation. It never sets process-local scheduling state: the durable
// user-steering gate is the pending Brain input admission persisted by
// PrepareHostUserInput before provider mutation. Reconciling here first means
// an internal Event admitted at the serialized input boundary is delivered before this
// message can overtake it.
//
// A background lane error (review delivery failure, liveness probe error) is
// never authority over a foreground user send: the failed background work
// keeps its own durable quarantine while the send proceeds through the
// per-Session input owner and the admission gates below.
func (s *Service) NoteUserSteering(agentID string) (bool, error) {
	if s == nil || s.store == nil {
		return false, nil
	}
	host, err := s.store.HostSession()
	if err != nil || strings.TrimSpace(host.ID) == "" || strings.TrimSpace(host.ID) != strings.TrimSpace(agentID) {
		return false, err
	}
	s.dispatchMu.Lock()
	_, reconcileErr := s.reconcileHostLaneLocked()
	s.dispatchMu.Unlock()
	if reconcileErr != nil {
		log.Printf("brain user steering lane reconciliation failed (foreground send proceeds): %v", reconcileErr)
	}
	return true, nil
}

// CancelUserSteering is an idempotent lane trigger used when a prepared user
// input was proved not submitted. Nothing process-local is cleared: the
// pending admission is removed by AbortHostUserInput, and this method merely
// re-runs reconciliation at the freed boundary.
func (s *Service) CancelUserSteering(agentID string) {
	if s == nil || s.store == nil {
		return
	}
	host, err := s.store.HostSession()
	if err != nil || strings.TrimSpace(host.ID) != strings.TrimSpace(agentID) {
		return
	}
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()
	_, _ = s.reconcileHostLaneLocked()
}

// CurrentHostSessionID returns the recorded Brain host Session id, or empty
// when unset/unavailable. Used by the server to bound Hidden-host snapshot
// refreshes to the current Host only.
func (s *Service) CurrentHostSessionID() string {
	if s == nil || s.store == nil {
		return ""
	}
	host, err := s.store.HostSession()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(host.ID)
}

// ObserveHostSessionEvent treats every current Hidden Host watcher change as
// a lane trigger. Persistent provider Sessions do not necessarily change
// process-level Agent state between turns, but their ordinary output/metadata
// changes still provide the boundary at which the reducer can probe exact
// provider evidence. The watcher Event is never completion authority itself.
//
// Exact terminal state changes additionally close a matching in-flight review
// handling. Missing disposition never replays the delivered input: the Work
// key is durably reconciled once at the FIFO tail before dispatch resumes.
func (s *Service) ObserveHostSessionEvent(event watcher.SessionEvent) (bool, error) {
	if s == nil || s.store == nil || event.Agent == nil || !event.Agent.Hidden {
		return false, nil
	}
	agentID := firstNonEmpty(strings.TrimSpace(event.Agent.ID), strings.TrimSpace(event.AgentID))
	requeued := false
	stateChanged := event.Type == "agent_state_change" && strings.TrimSpace(event.TurnID) != "" &&
		strings.TrimSpace(event.OldState) != strings.TrimSpace(event.NewState)
	if stateChanged {
		state := classifier.AgentState(strings.TrimSpace(event.NewState))
		terminal := state == classifier.StateDone || state == classifier.StateUnknown || state == classifier.StateFailed
		if terminal {
			handlings, _, err := s.store.LiveReviewHandlings(2)
			if err != nil {
				return false, err
			}
			for _, handling := range handlings {
				if handling.DeliveryHostSessionID == agentID &&
					handling.ProviderTurnID == strings.TrimSpace(event.TurnID) {
					_, requeued, err = s.store.EndReviewDelivery(
						handling.WorkID, handling.HandlingID, handling.ProviderTurnID,
					)
					if err != nil {
						return false, err
					}
					break
				}
			}
		}
	}
	// Host terminal boundary: this event is a trigger only. The reducer
	// probes the exact foreground turn and closes it exclusively on strong
	// exact terminal evidence; ambient Agent state can never clear the
	// durable turn or fabricate a boundary.
	if host, hostErr := s.store.HostSession(); hostErr == nil && strings.TrimSpace(host.ID) == agentID {
		s.dispatchMu.Lock()
		defer s.dispatchMu.Unlock()
		woke, reconcileErr := s.reconcileHostLaneLocked()
		return requeued || woke, reconcileErr
	}
	if requeued {
		return s.ReconcileHostLane()
	}
	return false, nil
}

// ReconcileDelegatedSessions handles first-inventory missing owners, expired
// delegated leases, and interrupted Event delivery. Healthy leases and
// unleased idle panes remain waiting; no Event means no Brain turn.
func (s *Service) ReconcileDelegatedSessions(agents []*classifier.Agent) {
	if s == nil || s.store == nil {
		return
	}
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	byID := make(map[string]*classifier.Agent, len(agents))
	for _, agent := range agents {
		if agent != nil {
			byID[agent.ID] = agent
		}
	}
	items, err := s.store.ListWork()
	if err != nil {
		log.Printf("brain Work reconciliation list failed: %v", err)
		return
	}
	now := s.nowUTC()
	for _, item := range items {
		if item.Status == WorkDone || item.Status == WorkCancelled || strings.TrimSpace(item.AttemptSessionID) == "" {
			continue
		}
		agent := byID[item.AttemptSessionID]
		turn, hasTurn, turnErr := s.store.Turn(item.AttemptSessionID)
		if turnErr != nil {
			log.Printf("brain Session canonical turn read failed for %s: %v", item.AttemptSessionID, turnErr)
			continue
		}
		if agent == nil {
			if !item.AttemptDelegated && !hasTurn {
				// A bare non-delegated relationship is not a Zen-managed Session
				// authority. It is excluded from CurrentWork projection, but this
				// inventory pass does not mutate foreign lifecycle state.
				continue
			}
			if hasTurn && !watcher.TurnImmutable(turn.Status) {
				// Preserve outcome uncertainty as an append-only Turn fact before
				// retiring the stale Work relationship. Absence never fabricates
				// done/failed and never replays pending Session input.
				_, _, _ = s.store.ApplyTurnFact(watcher.TurnFact{
					SessionID:   item.AttemptSessionID,
					TurnID:      turn.TurnID,
					Class:       watcher.EvidenceLiveness,
					Kind:        "uncertain",
					ProcessDead: true,
					SourceID:    "liveness\x00" + turn.ProcessIdentity + "\x00process-dead",
					At:          now,
					Summary:     "Delegated Session is absent after restart; outcome is unknown",
				})
			}
			_, changed, reconcileErr := s.store.ReconcileAbsentWorkAttempt(item.ID, item.AttemptSessionID)
			if reconcileErr != nil {
				log.Printf("brain absent Work owner reconciliation failed for %s: %v", item.ID, reconcileErr)
			} else if changed {
				// Owner absence is an actionable Work Event producer. Drive the
				// canonical lane immediately after its atomic ledger write.
				if _, dispatchErr := s.ReconcileHostLane(); dispatchErr != nil {
					log.Printf("brain absent Work owner dispatch failed for %s: %v", item.ID, dispatchErr)
				}
			}
			continue
		}
		if !hasTurn {
			// A present markerless Session still has no canonical lifecycle;
			// raw classifier state cannot fabricate a Turn fact.
			continue
		}
		// Canonical-turn path: the ledger owns Work + Events. Immutable
		// terminal turns (Done/Failed) are final; Unknown is still probed
		// for a later bound Provider terminal. Only the lease-expiry stale
		// wake and the restart-absent recovery are re-derived here, both
		// from the current ledger record.
		if watcher.TurnImmutable(turn.Status) {
			continue
		}
		// session.stale reads the current turn's OWN expected-next-check
		// time only: the agent's lease fields are a cross-turn projection
		// and cannot stale a newer turn (the false-stale incident). A dead
		// pane is owned by watcher liveness (end-of-identity Unknown),
		// never by the clock.
		if now.Before(turn.LeaseDeadline.UTC()) {
			continue
		}
		if !agent.PaneAlive && agent.ProcessID <= 0 {
			continue
		}
		// Progress lease time and live execution ownership are orthogonal. An
		// exact provider-native running Activity keeps this Turn/Work owned even
		// when the Agent missed its expected progress check; stale Attention must
		// not make a demonstrably live Session uncontrollable.
		if s.watcher != nil {
			observation, found, probeErr := s.watcher.ProbeProviderEvidence(item.AttemptSessionID)
			if probeErr == nil && found {
				switch strings.TrimSpace(observation.Status) {
				case "running":
					snapshot, _, applyErr := s.store.ApplyTurnFact(watcher.TurnFact{
						SessionID: item.AttemptSessionID, TurnID: turn.TurnID,
						Class: watcher.EvidenceProvider, Kind: "running",
						SourceID: providerFactSourceID(item.AttemptSessionID, observation),
						Cursor:   observation.AdmissionCursor, Admission: admissionFromObservation(observation),
						ActivityID: strings.TrimSpace(observation.ID), StartedAt: observation.StartedAt,
						At: now, Summary: "Delegated turn running",
					})
					if applyErr == nil && providerObservationOwnsTurn(snapshot, observation) {
						if _, _, ownerErr := s.store.ReassertLiveTurnOwnership(item.ID, item.AttemptSessionID, turn.TurnID); ownerErr != nil {
							log.Printf("brain live Work ownership repair failed for %s: %v", item.ID, ownerErr)
						}
						continue
					}
				case "completed":
					snapshot, _, applyErr := s.store.ApplyTurnFact(s.providerTerminalFact(item.AttemptSessionID, turn.TurnID, observation, "done"))
					if applyErr == nil && watcher.TurnTerminal(snapshot.Status) {
						continue
					}
				case "failed", "interrupted", "cancelled":
					snapshot, _, applyErr := s.store.ApplyTurnFact(s.providerTerminalFact(item.AttemptSessionID, turn.TurnID, observation, "failed"))
					if applyErr == nil && watcher.TurnTerminal(snapshot.Status) {
						continue
					}
				}
			}
		}
		if agent.State != classifier.StateRunning && agent.State != classifier.StateUnknown {
			continue
		}
	}
	// Lease expiry, claim expiry, and durable due retries run once for the
	// entire Work inventory. Sweeps only record lifecycle facts; they never
	// create Sessions or infer a next delegated task.
	if sweepErr := s.store.SweepLifecycle(); sweepErr != nil {
		log.Printf("brain Work supervisor sweep failed: %v", sweepErr)
	}
	_, _ = s.ReconcileHostLane()
}

func providerObservationOwnsTurn(turn watcher.TurnSnapshot, observation watcher.ProviderActivityObservation) bool {
	activityID := strings.TrimSpace(observation.ID)
	if strings.TrimSpace(turn.ActivityID) != "" && activityID == strings.TrimSpace(turn.ActivityID) {
		return true
	}
	admission := admissionFromObservation(observation)
	if turn.HasAdmission && !admission.Empty() && admission.Stream == turn.Admission.Stream &&
		admission.ID != "" && admission.Cursor >= turn.Admission.Cursor &&
		(strings.TrimSpace(turn.Admission.SHA256) == "" || turn.Admission.SHA256 == admission.SHA256) {
		return true
	}
	return !turn.HasAdmission && strings.TrimSpace(turn.ActivityID) == "" && activityID != "" &&
		!observation.StartedAt.IsZero() && !observation.StartedAt.Before(turn.AcceptedAt)
}

func (s *Service) SubscribeWork() (int, <-chan WorkChange) {
	if s == nil || s.store == nil {
		ch := make(chan WorkChange)
		close(ch)
		return 0, ch
	}
	return s.store.SubscribeWork()
}

func (s *Service) UnsubscribeWork(id int) {
	if s != nil && s.store != nil {
		s.store.UnsubscribeWork(id)
	}
}

func (s *Service) MarkWorkRead(workID string) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("brain service is not configured")
	}
	if err := s.store.MarkWorkRead(workID); err != nil {
		return err
	}
	return s.store.MarkTimelineWorkCardsRead(workID)
}

func (s *Service) AppendWorkEvent(event WorkEvent) (WorkEvent, bool, error) {
	if s == nil || s.store == nil {
		return WorkEvent{}, false, fmt.Errorf("brain service is not configured")
	}
	recorded, created, err := s.store.AppendWorkEvent(event)
	if err != nil {
		return recorded, created, err
	}
	if created && isProjectedWorkResultEvent(recorded.Kind) {
		// One projected card per Work lineage, replaced in place from
		// canonical state: historical facts never materialize parallel cards.
		if _, _, cardErr := s.store.SyncWorkCard(recorded.WorkID, &recorded); cardErr != nil {
			log.Printf("brain work card projection failed for %s: %v", recorded.WorkID, cardErr)
		}
	}
	if !created || !recorded.Actionable {
		return recorded, created, nil
	}
	_, dispatchErr := s.ReconcileHostLane()
	return recorded, created, dispatchErr
}

func (s *Service) WorkspacePath() string {
	if s == nil || s.store == nil {
		return ""
	}
	return s.store.WorkspacePath()
}

func (s *Service) MaterializeProviderConversation(threadID string, conversation work.CodexConversation) error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.MaterializeProviderConversation(threadID, conversation)
}

// AnnotateWorkResultEvents adds current lifecycle truth to immutable result
// cards. The fact label remains Status; these fields do not rewrite history.
func (s *Service) AnnotateWorkResultEvents(events []work.CodexConversationEvent) error {
	if s == nil || s.store == nil || len(events) == 0 {
		return nil
	}
	workIDs := make([]string, 0, len(events))
	for _, event := range events {
		if event.Source == workResultConversationSource && strings.TrimSpace(event.WorkID) != "" {
			workIDs = append(workIDs, event.WorkID)
		}
	}
	lifecycles, err := s.store.WorkResultLifecycles(workIDs)
	if err != nil {
		return err
	}
	for index := range events {
		lifecycle, found := lifecycles[events[index].WorkID]
		if !found {
			continue
		}
		events[index].WorkReviewState = string(lifecycle.ReviewState)
		events[index].WorkSessionState = string(lifecycle.SessionState)
		events[index].WorkResultCurrent = true
	}
	return nil
}

func (s *Service) hostUserInputAdmission(agentID, receipt, displayBody, conversationScopeKey string) (BrainInputAdmission, bool, error) {
	if s == nil || s.store == nil {
		return BrainInputAdmission{}, false, nil
	}
	host, err := s.store.HostSession()
	if err != nil {
		return BrainInputAdmission{}, false, err
	}
	if strings.TrimSpace(host.ID) == "" || strings.TrimSpace(host.ID) != strings.TrimSpace(agentID) {
		return BrainInputAdmission{}, false, nil
	}
	threadID := threadIDFromConversationScopeKey(conversationScopeKey)
	if threadID == "" {
		threadID, err = s.store.ChatThreadID()
		if err != nil {
			return BrainInputAdmission{}, false, err
		}
	}
	known, err := s.store.HasChatThread(threadID)
	if err != nil {
		return BrainInputAdmission{}, false, err
	}
	if !known {
		return BrainInputAdmission{}, false, fmt.Errorf("brain thread %q is unknown", threadID)
	}
	sessionID := strings.TrimSpace(host.ProviderSessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(host.ID)
	}
	generation := ""
	if s.watcher != nil {
		generation, err = s.hostOwnedGeneration(strings.TrimSpace(host.ID))
		if err != nil {
			return BrainInputAdmission{}, false, err
		}
	}
	return BrainInputAdmission{
		RequestID: strings.TrimSpace(receipt), ThreadID: threadID,
		HostSessionID: strings.TrimSpace(host.ID), SessionID: sessionID,
		HostGeneration: generation,
		DisplayBody:    strings.TrimSpace(displayBody),
	}, true, nil
}

func (s *Service) hostOwnedGeneration(hostSessionID string) (string, error) {
	if s == nil || s.watcher == nil {
		return "", fmt.Errorf("Brain Host watcher is unavailable")
	}
	owned, err := s.watcher.ResolveBrainHostGeneration(hostSessionID)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(owned.Generation), nil
}

// PrepareHostUserInput persists the one exact no-replay intent before Session
// Input may mutate the provider. created=false means this request/thread was
// already pending or accepted and must not be submitted again; a durably
// NotSubmitted row (provider provably never mutated) is the same logical input
// retried and is re-armed in place by PrepareBrainInputAdmission.
func (s *Service) PrepareHostUserInput(agentID, receipt, displayBody, conversationScopeKey string) (BrainInputAdmission, bool, error) {
	if s == nil || s.store == nil {
		return BrainInputAdmission{}, false, nil
	}
	threadID := threadIDFromConversationScopeKey(conversationScopeKey)
	var err error
	if threadID == "" {
		threadID, err = s.store.ChatThreadID()
		if err != nil {
			return BrainInputAdmission{}, false, err
		}
	}
	if existing, found, lookupErr := s.store.BrainInputAdmission(receipt, threadID); lookupErr != nil {
		return BrainInputAdmission{}, false, lookupErr
	} else if found {
		if existing.HostSessionID != strings.TrimSpace(agentID) ||
			existing.DisplayBody != strings.TrimSpace(displayBody) {
			return BrainInputAdmission{}, false, fmt.Errorf("Brain input admission identity belongs to different input")
		}
		if existing.State != BrainInputAdmissionNotSubmitted {
			// Pending/Accepted/Uncertain stay terminal for this identity.
			return existing, false, nil
		}
		// NotSubmitted is the same logical input retried after a proven
		// non-mutation: fall through so the store re-arms the exact row with
		// the caller's current host generation.
	}
	admission, hostInput, err := s.hostUserInputAdmission(agentID, receipt, displayBody, conversationScopeKey)
	if err != nil || !hostInput {
		return admission, false, err
	}
	// Enter the lane mutex before this message may mutate the provider:
	// reconciliation runs first (an internal Event admitted at an idle
	// boundary cannot be overtaken), then the pending admission is persisted
	// as the durable user-steering gate that blocks further lane admissions
	// while the user's input is in flight. A background lane error is logged
	// and never blocks the foreground send: the admission itself is the
	// durable gate, and the per-Session input owner serializes provider
	// mutation.
	s.dispatchMu.Lock()
	if _, reconcileErr := s.reconcileHostLaneLocked(); reconcileErr != nil {
		log.Printf("brain user input lane reconciliation failed (admission proceeds): %v", reconcileErr)
	}
	prepared, created, err := s.store.PrepareBrainInputAdmission(admission)
	s.dispatchMu.Unlock()
	if err == nil && created {
		s.inFlightHostInputs[hostInputAttemptKey(prepared.RequestID, prepared.ThreadID)] = struct{}{}
	}
	return prepared, created, err
}

// AbortHostUserInput terminalizes only a pending intent after Session Input
// proves no provider mutation began, then re-drives the lane. A background
// lane error is logged, never returned: the terminalization is the authority
// the server needs to acknowledge the definite non-submission.
func (s *Service) AbortHostUserInput(requestID, threadID string) error {
	if s == nil || s.store == nil {
		return nil
	}
	s.dispatchMu.Lock()
	delete(s.inFlightHostInputs, hostInputAttemptKey(requestID, threadID))
	if err := s.store.AbortBrainInputAdmission(requestID, threadID); err != nil {
		s.dispatchMu.Unlock()
		return err
	}
	if _, err := s.reconcileHostLaneLocked(); err != nil {
		log.Printf("brain post-abort lane reconciliation failed (non-submission stands): %v", err)
	}
	s.dispatchMu.Unlock()
	return nil
}

// ReleaseHostUserInputAttempt ends the live mutation critical section when the
// provider outcome is ambiguous. The reducer reads the exact durable receipt,
// records uncertain without replay, and frees unrelated lane work. A background
// lane error is logged, never returned: the ambiguous settlement is the
// authority the server needs to keep the outcome no-replay.
func (s *Service) ReleaseHostUserInputAttempt(requestID, threadID string) error {
	if s == nil || s.store == nil {
		return nil
	}
	s.dispatchMu.Lock()
	delete(s.inFlightHostInputs, hostInputAttemptKey(requestID, threadID))
	if _, _, err := s.store.SettleBrainInputAdmission(
		requestID, threadID, BrainInputAdmissionUncertain, false,
	); err != nil {
		s.dispatchMu.Unlock()
		return err
	}
	if _, err := s.reconcileHostLaneLocked(); err != nil {
		log.Printf("brain post-uncertain lane reconciliation failed (uncertain stands): %v", err)
	}
	s.dispatchMu.Unlock()
	return nil
}

// AdmitHostUserInput makes provider acceptance and every matching user_input
// Attention authoritative from the exact admission persisted before provider
// mutation. It never reconstructs immutable generation/Session identity from
// ambient state after acceptance. It then reserves any queued future Attention
// behind that foreground turn and attempts the independently retryable
// messages.jsonl projection.
func (s *Service) AdmitHostUserInput(prepared BrainInputAdmission) error {
	if s == nil || s.store == nil {
		return nil
	}
	s.dispatchMu.Lock()
	delete(s.inFlightHostInputs, hostInputAttemptKey(prepared.RequestID, prepared.ThreadID))
	persisted, found, err := s.store.BrainInputAdmission(prepared.RequestID, prepared.ThreadID)
	if err != nil {
		s.dispatchMu.Unlock()
		return err
	}
	if !found {
		s.dispatchMu.Unlock()
		return fmt.Errorf("Brain input admission must be prepared before provider acceptance")
	}
	if !samePreparedBrainInputAdmission(persisted, prepared) {
		s.dispatchMu.Unlock()
		return fmt.Errorf("Brain input admission identity changed after provider acceptance")
	}
	// Provider acceptance is durable before the lane runs; the accepted
	// admission creates the foreground lifecycle owner. Review admission may
	// queue behind it, while the pre-mutation pending admission remains the
	// ordering gate that prevents an internal Review from overtaking user input.
	accepted, _, _, err := s.store.AcceptBrainInputAdmission(persisted)
	if err != nil {
		s.dispatchMu.Unlock()
		return err
	}
	if _, dispatchErr := s.reconcileHostLaneLocked(); dispatchErr != nil {
		// Acceptance is already durable; the lane re-drive is background work
		// and must not turn a successful user send into a pending outcome.
		log.Printf("brain post-accept lane reconciliation failed (accepted input stands): %v", dispatchErr)
	}
	s.dispatchMu.Unlock()
	projectionErr := s.store.ProjectBrainInputAdmission(accepted)
	return projectionErr
}

// SubmitExternalUserInput submits one channel-owned receipt through the same
// Prepare -> provider mutation -> Admit/Abort/Uncertain transaction used by
// the mobile Brain surface. The current Brain host and thread are resolved by
// their canonical owners; callers cannot select a provider transcript or
// manufacture a Session.
func (s *Service) SubmitExternalUserInput(receipt, body string) (ExternalInputDisposition, error) {
	receipt = strings.TrimSpace(receipt)
	body = strings.TrimSpace(body)
	if s == nil || s.store == nil || s.watcher == nil {
		return ExternalInputNotSubmitted, fmt.Errorf("brain service is not configured")
	}
	if receipt == "" || body == "" {
		return ExternalInputNotSubmitted, fmt.Errorf("external Brain input requires receipt and body")
	}
	host, err := s.store.HostSession()
	if err != nil {
		return ExternalInputNotSubmitted, err
	}
	hostID := strings.TrimSpace(host.ID)
	if hostID == "" {
		return ExternalInputNotSubmitted, fmt.Errorf("brain host is unavailable")
	}
	steering, err := s.NoteUserSteering(hostID)
	if err != nil || !steering {
		return ExternalInputNotSubmitted, firstNonNil(err, fmt.Errorf("brain host is unavailable"))
	}
	prepared, created, err := s.PrepareHostUserInput(hostID, receipt, body, "")
	if err != nil {
		s.CancelUserSteering(hostID)
		return ExternalInputNotSubmitted, err
	}
	if !created {
		s.CancelUserSteering(hostID)
		switch prepared.State {
		case BrainInputAdmissionAccepted:
			if err := s.AdmitHostUserInput(prepared); err != nil {
				return ExternalInputPending, err
			}
			return ExternalInputAccepted, nil
		case BrainInputAdmissionNotSubmitted:
			return ExternalInputNotSubmitted, nil
		case BrainInputAdmissionUncertain:
			return ExternalInputUncertain, nil
		default:
			return ExternalInputPending, nil
		}
	}

	_, sendErr := s.watcher.SendInputWithReceiptResult(hostID, body, receipt)
	if sendErr == nil {
		if err := s.AdmitHostUserInput(prepared); err != nil {
			return ExternalInputPending, err
		}
		return ExternalInputAccepted, nil
	}
	if watcher.InputOutcomeFromError(sendErr) == watcher.InputAmbiguous {
		if err := s.ReleaseHostUserInputAttempt(prepared.RequestID, prepared.ThreadID); err != nil {
			return ExternalInputPending, err
		}
		return ExternalInputUncertain, nil
	}
	if err := s.AbortHostUserInput(prepared.RequestID, prepared.ThreadID); err != nil {
		return ExternalInputPending, err
	}
	s.CancelUserSteering(hostID)
	return ExternalInputNotSubmitted, nil
}

// CurrentHostForegroundTurn exposes the canonical durable foreground identity
// to read-only presentation adapters. It grants no authority to close or alter
// the turn.
func (s *Service) CurrentHostForegroundTurn() (*HostForegroundTurn, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	return s.store.CurrentHostForegroundTurn()
}

func firstNonNil(values ...error) error {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func hostInputAttemptKey(requestID, threadID string) string {
	return strings.TrimSpace(requestID) + "\x00" + strings.TrimSpace(threadID)
}

func threadIDFromConversationScopeKey(scopeKey string) string {
	const prefix = "brain-thread:"
	scopeKey = strings.TrimSpace(scopeKey)
	if !strings.HasPrefix(scopeKey, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(scopeKey, prefix))
}

// BindHostProviderTranscript resolves and persists the Host Executor Session's
// provider transcript identity from the live host process when needed.
func (s *Service) BindHostProviderTranscript() (work.HostTranscriptIdentity, error) {
	if s == nil || s.store == nil {
		return work.HostTranscriptIdentity{}, nil
	}
	host, err := s.store.HostSession()
	if err != nil {
		return work.HostTranscriptIdentity{}, err
	}
	var agent *classifier.Agent
	if strings.TrimSpace(host.ID) != "" && s.watcher != nil {
		agent = s.watcher.GetAgent(host.ID)
	}
	provider := s.hostTranscriptProvider(host, agent)
	existing := work.HostTranscriptIdentity{
		Provider:  provider,
		SessionID: host.ProviderSessionID,
		Path:      host.TranscriptPath,
		DataRoot:  host.ProviderDataRoot,
	}
	if strings.TrimSpace(host.ID) == "" || s.watcher == nil || agent == nil {
		return existing, nil
	}
	resolved := work.ResolveHostTranscriptIdentityForAgent(*agent, existing, provider)
	if strings.TrimSpace(resolved.SessionID) == strings.TrimSpace(existing.SessionID) &&
		strings.TrimSpace(resolved.Path) == strings.TrimSpace(existing.Path) &&
		strings.TrimSpace(resolved.DataRoot) == strings.TrimSpace(existing.DataRoot) &&
		strings.TrimSpace(resolved.Provider) == strings.TrimSpace(existing.Provider) {
		return resolved, nil
	}
	if !resolved.Bound() {
		return work.HostTranscriptIdentity{Provider: provider}, nil
	}
	if err := s.store.SetHostProviderTranscript(resolved.SessionID, resolved.Path, resolved.DataRoot); err != nil {
		return resolved, err
	}
	return resolved, nil
}

func (s *Service) hostTranscriptProvider(host HostSession, agent *classifier.Agent) string {
	if agent != nil {
		if provider := work.InferAgentProvider(agent.Command, agent.Name); provider != "" {
			return provider
		}
	}
	if provider := work.InferAgentProvider(host.ExecutorID); provider != "" && provider != work.AgentProviderCustom {
		return provider
	}
	executor := s.hostExecutor()
	if provider := strings.TrimSpace(executor.Provider); provider != "" && provider != work.AgentProviderCustom {
		return provider
	}
	return work.InferAgentProvider(host.ExecutorID, executor.ID, executor.Command)
}

// HostBoundProviderConversation loads assistant/final transcript rows from the
// stable Host Executor Session identity rather than cwd matching.
func (s *Service) HostBoundProviderConversation() (work.CodexConversation, error) {
	if s == nil || s.store == nil {
		return work.CodexConversation{Available: false, Events: []work.CodexConversationEvent{}}, nil
	}
	identity, err := s.BindHostProviderTranscript()
	if err != nil {
		return work.CodexConversation{}, err
	}
	if !identity.Bound() {
		host, hostErr := s.store.HostSession()
		if hostErr != nil {
			return work.CodexConversation{}, hostErr
		}
		var agent *classifier.Agent
		if strings.TrimSpace(host.ID) != "" && s.watcher != nil {
			agent = s.watcher.GetAgent(host.ID)
		}
		identity = work.HostTranscriptIdentity{
			Provider:  s.hostTranscriptProvider(host, agent),
			SessionID: host.ProviderSessionID,
			Path:      host.TranscriptPath,
			DataRoot:  host.ProviderDataRoot,
		}
	}
	if !identity.Bound() {
		return work.CodexConversation{
			Available: false,
			Reason:    "host_transcript_unbound",
			Events:    []work.CodexConversationEvent{},
		}, nil
	}
	conversation, err := work.LoadHostConversationByIdentity(identity)
	if err != nil {
		return work.CodexConversation{}, err
	}
	conversation.Events = work.SuppressPrivateHostTurns(conversation.Events)
	return conversation, nil
}

func (s *Service) ThreadTimeline(threadID string, limit int) ([]TimelineItem, error) {
	if s == nil || s.store == nil {
		return []TimelineItem{}, nil
	}
	return s.store.ThreadTimeline(threadID, limit)
}

// RouteCalendarEvent projects the existing idempotent Calendar occurrence into
// Work/Event without taking ownership of Calendar execution or result delivery.
func (s *Service) RouteCalendarEvent(event calendar.Event) (bool, error) {
	if s == nil || s.store == nil || event.Item.Kind != calendar.KindScheduledAction {
		return false, nil
	}
	run, ok := latestCalendarRun(event.Item)
	if !ok {
		return false, nil
	}
	contextRef := "calendar:" + event.Item.ID + ":" + run.ID
	sourceThreadID := strings.TrimSpace(run.SourceThreadID)
	if sourceThreadID == "" {
		sourceThreadID = strings.TrimSpace(event.Item.SourceThreadID)
	}
	candidate := Work{
		ID:               calendarWorkID(event.Item.ID, run.ID),
		Title:            firstNonEmpty(run.Title, event.Item.Title),
		Objective:        strings.TrimSpace(event.Item.ActionInstruction),
		Status:           WorkRunning,
		AttemptSessionID: strings.TrimSpace(run.AgentSession),
		AttemptDelegated: strings.TrimSpace(run.AgentSession) != "",
		SourceThreadID:   sourceThreadID,
		CompletionPolicy: CompletionBounded,
		NextAction:       "Wait for the scheduled action.",
		WaitFor:          calendarWaitCondition(run),
		Wake:             calendarRunWake(run, contextRef),
		ContextRef:       contextRef,
	}

	kind := "calendar.due"
	actionable := false
	terminal := false
	payloadRef := contextRef
	update := WorkUpdate{}
	switch run.Status {
	case calendar.StatusRunning:
		status := WorkRunning
		next := "Wait for the scheduled action."
		wait := calendarWaitCondition(run)
		owner := strings.TrimSpace(run.AgentSession)
		wake := calendarRunWake(run, contextRef)
		update = WorkUpdate{
			Status:           &status,
			AttemptSessionID: &owner,
			NextAction:       &next,
			WaitFor:          &wait,
			Wake:             &wake,
		}
		if owner != "" {
			kind = "calendar.launched"
		}
	case calendar.StatusCompleted:
		status := WorkDone
		empty := ""
		var noWake *WorkWake
		update = WorkUpdate{Status: &status, NextAction: &empty, WaitFor: &empty, Wake: &noWake}
		kind = "calendar.result"
		actionable = true
		terminal = true
		if event.ScheduledResult != nil {
			payloadRef = event.ScheduledResult.ID
		}
	case calendar.StatusFailed:
		status := WorkNeedsInput
		next := "Inspect the scheduled action failure."
		empty := ""
		var noWake *WorkWake
		update = WorkUpdate{Status: &status, NextAction: &next, WaitFor: &empty, Wake: &noWake}
		kind = "calendar.failure"
		actionable = true
		terminal = true
		if event.ScheduledResult != nil {
			payloadRef = event.ScheduledResult.ID
		}
	default:
		return false, nil
	}
	producerEvent := WorkEvent{
		WorkID:     candidate.ID,
		Kind:       kind,
		DedupeKey:  fmt.Sprintf("calendar:%s:%s:%s", event.Item.ID, run.ID, kind),
		PayloadRef: payloadRef,
		SourceName: contextRef,
		Actionable: actionable,
	}
	var err error
	var recorded WorkEvent
	var item Work
	created := false
	producerWoke := false
	if terminal {
		wakes := []WorkEvent{}
		item, recorded, created, wakes, err = s.store.ApplyProducerTransition(
			&candidate,
			&update,
			producerEvent,
			&WorkWake{Kind: WorkWakeCalendarResult, Ref: contextRef},
			event.Item.ID+":"+run.ID+":"+kind,
		)
		if err != nil {
			return false, err
		}
		producerWoke = len(wakes) > 0
	} else {
		item, _, err = s.store.EnsureWork(candidate)
		if err != nil {
			return false, err
		}
		if workUpdateChanges(item, update) {
			item, err = s.store.UpdateWork(item.ID, update)
			if err != nil {
				return false, err
			}
		}
		recorded, created, err = s.store.AppendWorkEvent(producerEvent)
		if err != nil {
			return false, err
		}
	}
	if !created && !producerWoke {
		return false, nil
	}
	woke, dispatchErr := s.ReconcileHostLane()
	return woke || producerWoke || recorded.Actionable, dispatchErr
}

func calendarRunWake(run calendar.Run, contextRef string) *WorkWake {
	if strings.TrimSpace(run.AgentSession) != "" {
		return nil
	}
	return &WorkWake{Kind: WorkWakeCalendarResult, Ref: strings.TrimSpace(contextRef)}
}

func latestCalendarRun(item calendar.Item) (calendar.Run, bool) {
	if len(item.Runs) == 0 {
		return calendar.Run{}, false
	}
	return item.Runs[len(item.Runs)-1], true
}

func calendarWaitCondition(run calendar.Run) string {
	if sessionID := strings.TrimSpace(run.AgentSession); sessionID != "" {
		return "Session " + sessionID
	}
	return "Calendar occurrence " + run.ID
}

func calendarWorkID(itemID, runID string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(itemID) + "\x00" + strings.TrimSpace(runID)))
	return fmt.Sprintf("calendar-%x", digest[:12])
}

// Host replacement reasons are durable audit tags written to host_replacements.jsonl.
// They answer: why did ensureHostAgent create a new Brain host instead of reusing one?
const (
	hostReplaceReasonMissingTmux               = "missing_tmux"
	hostReplaceReasonMissingTmuxResumeLaunched = "missing_tmux_resume_launched"
	hostReplaceReasonMissingTmuxUnrecoverable  = "missing_tmux_unrecoverable"
	hostReplaceReasonProviderMismatch          = "provider_mismatch"
	hostReplaceReasonNoRecordedHost            = "no_recorded_host"
	hostReplaceReasonRecoveredAlive            = "recovered_alive_host"
)

func (s *Service) ensureHostAgent(executor work.AgentExecutor) (AgentRef, error) {
	if s == nil || s.store == nil || s.watcher == nil {
		return AgentRef{}, nil
	}
	hostSession, err := s.store.HostSession()
	if err != nil {
		return AgentRef{}, err
	}
	command, err := s.hostCommand(executor)
	if err != nil {
		return AgentRef{}, err
	}
	id := strings.TrimSpace(hostSession.ID)
	replaceReason := ""
	replaceDetail := ""

	if id != "" {
		presence, probeErr := s.watcher.ProbeSession(id)
		switch {
		case probeErr != nil || presence == watcher.SessionPresenceUnknown:
			if probeErr == nil {
				probeErr = fmt.Errorf("tmux probe returned unknown for %q", id)
			}
			return AgentRef{}, fmt.Errorf("brain host recorded session liveness unknown: %w", probeErr)
		case presence == watcher.SessionPresencePresent:
			if agent := s.watcher.GetAgent(id); agent != nil {
				if s.hostAgentMatches(agent, executor) {
					if strings.TrimSpace(hostSession.ExecutorID) != executor.ID {
						if err := s.store.SetHostSession(id, executor.ID); err != nil {
							return AgentRef{}, err
						}
					}
					_, _ = s.BindHostProviderTranscript()
					return agentRefFromClassifier(agent), nil
				}
				// Explicit provider/executor mismatch (e.g. user switched host executor).
				replaceReason = hostReplaceReasonProviderMismatch
				replaceDetail = fmt.Sprintf(
					"recorded_executor=%q resolved_executor=%q agent_command=%q agent_provider=%q",
					hostSession.ExecutorID,
					executor.ID,
					strings.TrimSpace(agent.Command),
					work.InferAgentProvider(agent.Command),
				)
				s.recordHostReplacement(HostReplacementEvent{
					Reason:           replaceReason,
					FromID:           id,
					FromExecutorID:   hostSession.ExecutorID,
					FromCommand:      agent.Command,
					ResolvedExecutor: executor.ID,
					Detail:           replaceDetail,
				})
				if err := s.teardownHostSession(id); err != nil {
					return AgentRef{}, fmt.Errorf("brain host provider replacement teardown: %w", err)
				}
			} else {
				// Tmux target still exists but watcher has not observed it yet (common right
				// after daemon restart). Do not replace; return a bootstrap stub.
				if strings.TrimSpace(hostSession.ExecutorID) != executor.ID {
					if err := s.store.SetHostSession(id, executor.ID); err != nil {
						return AgentRef{}, err
					}
				}
				return AgentRef{
					ID:      id,
					Name:    "Brain",
					Status:  string(classifier.StateRunning),
					Summary: "Session starting",
					Cwd:     s.brainWorkspace(),
					Command: command,
					Updated: firstNonZeroTime(hostSession.UpdatedAt, s.now().UTC()),
					Hidden:  true,
				}, nil
			}
		default:
			// Proven Absent only — prefer rebinding a live matching Brain host.
			recovered, recoverErr := s.recoverMatchingHost(executor, hostSession)
			if recoverErr != nil {
				return AgentRef{}, recoverErr
			}
			if recovered != nil {
				if err := s.rebindRecoveredHost(id, recovered, executor, hostSession); err != nil {
					return AgentRef{}, err
				}
				return agentRefFromClassifier(recovered), nil
			}
			replaceReason = hostReplaceReasonMissingTmux
			replaceDetail = fmt.Sprintf("probe=absent id=%q", id)
			s.recordHostReplacement(HostReplacementEvent{
				Reason:           replaceReason,
				FromID:           id,
				FromExecutorID:   hostSession.ExecutorID,
				ResolvedExecutor: executor.ID,
				Detail:           replaceDetail,
			})
		}
	} else {
		replaceReason = hostReplaceReasonNoRecordedHost
	}

	resumeToken := ""
	if replaceReason == hostReplaceReasonMissingTmux {
		token, known, resumable := s.hostProviderResumeToken(hostSession, executor)
		if known {
			if !resumable {
				s.recordHostReplacement(HostReplacementEvent{
					Reason:           hostReplaceReasonMissingTmuxUnrecoverable,
					FromID:           id,
					FromExecutorID:   hostSession.ExecutorID,
					ResolvedExecutor: executor.ID,
					Detail: fmt.Sprintf(
						"has_session=false id=%q provider_session=%q executor=%q: no native resume shape",
						id, token, executor.ID,
					),
				})
				return AgentRef{}, fmt.Errorf(
					"brain host refusing blank replacement: recorded provider session %q cannot be natively resumed for executor %q",
					token, executor.ID,
				)
			}
			resumeCommand, err := s.hostLaunchCommand(executor, token)
			if err != nil {
				s.recordHostReplacement(HostReplacementEvent{
					Reason:           hostReplaceReasonMissingTmuxUnrecoverable,
					FromID:           id,
					FromExecutorID:   hostSession.ExecutorID,
					ResolvedExecutor: executor.ID,
					Detail: fmt.Sprintf(
						"has_session=false id=%q provider_session=%q: %v",
						id, token, err,
					),
				})
				return AgentRef{}, fmt.Errorf(
					"brain host refusing blank replacement: recorded provider session %q cannot be natively resumed for executor %q: %w",
					token, executor.ID, err,
				)
			}
			command = resumeCommand
			resumeToken = token
			replaceDetail = fmt.Sprintf("has_session=false id=%q provider_session_id=%q", id, token)
		}
	}

	sessionEnv := brainSessionEnvironment()
	routes := s.sessionRoutes()
	provisionalID := ""
	// Missing-tmux resume reuses the immutable existing route binding so the
	// thread keeps its exact identity. New host launches (initial, NewChat,
	// provider/executor mismatch) and resumes whose binding no longer exists
	// (dropped or never routed) resolve the selected executor default through
	// PrepareLaunch — a live thread must never stay stuck on a dead route.
	resumeBindingFound := false
	if routes != nil && strings.TrimSpace(id) != "" && resumeToken != "" {
		routeCommand, routeEnv, found, routeErr := routes.ResumeLaunch(id, command)
		if routeErr != nil {
			s.recordHostReplacement(HostReplacementEvent{
				Reason:           hostReplaceReasonMissingTmuxUnrecoverable,
				FromID:           id,
				FromExecutorID:   hostSession.ExecutorID,
				ResolvedExecutor: executor.ID,
				Detail: fmt.Sprintf(
					"has_session=false id=%q provider_session=%q route_resume_failed=%v",
					id, resumeToken, routeErr,
				),
			})
			return AgentRef{}, fmt.Errorf(
				"brain host refusing blank replacement: recorded route for session %q cannot be resumed: %w",
				id, routeErr,
			)
		}
		resumeBindingFound = found
		if found {
			if strings.TrimSpace(routeCommand) != "" {
				command = routeCommand
			}
			sessionEnv = mergeStringMaps(sessionEnv, routeEnv)
		}
	}
	if routes != nil && !resumeBindingFound {
		clientHint := work.ProfileClientExecutor(executor.Provider, executor.Command, executor.ID)
		plan, planErr := routes.PrepareLaunch(clientHint, "", command)
		if planErr != nil && !plan.Persist.Applied && !plan.Bypass {
			return AgentRef{}, fmt.Errorf("brain host profile prepare: %w", planErr)
		}
		if plan.Applied && !plan.Bypass {
			if strings.TrimSpace(plan.Command) != "" {
				command = plan.Command
			}
			sessionEnv = mergeStringMaps(sessionEnv, plan.Env)
			provisionalID = plan.ProvisionalID
			// Prepare Applied+!Durable may proceed: CommitLaunch is the
			// durability barrier for the exact final Session-owned route.
			// Brain Snapshot/NewChat has no persistence-warning wire, so a
			// later Applied+!Durable or errored Commit must fail closed
			// (unlike control/App keep-with-warning).
			if planErr != nil || !plan.Persist.Durable {
				if planErr == nil {
					planErr = modelprofiles.ErrPersistDirSync
				}
				log.Printf("brain host profile prepare applied; Commit is durability barrier (prepare uncertain: %v)", planErr)
			}
		}
	}

	agentID, err := s.watcher.CreateSession("", watcher.CreateSessionOptions{
		Cwd:         s.brainWorkspace(),
		Command:     command,
		Name:        "Brain",
		Detached:    true,
		Hidden:      true,
		ProgressEnv: true,
		Env:         sessionEnv,
	})
	if err != nil {
		if provisionalID != "" && routes != nil {
			abortPersist, abortErr := routes.AbortLaunch(provisionalID)
			err = errors.Join(err, abortErr)
			if abortErr != nil || !abortPersist.Applied {
				err = errors.Join(err, modelprofiles.ErrLaunchCleanupIncomplete)
			}
		}
		if resumeToken != "" {
			s.recordHostReplacement(HostReplacementEvent{
				Reason:           hostReplaceReasonMissingTmuxUnrecoverable,
				FromID:           id,
				FromExecutorID:   hostSession.ExecutorID,
				ResolvedExecutor: executor.ID,
				Detail: fmt.Sprintf(
					"has_session=false id=%q provider_session=%q create_failed=%v",
					id, resumeToken, err,
				),
			})
		}
		return AgentRef{}, err
	}
	if provisionalID != "" && routes != nil {
		_, _, persist, commitErr := routes.CommitLaunch(provisionalID, agentID)
		if !persist.Applied {
			cleanup := modelprofiles.CleanupFailedLaunch(routes, provisionalID, agentID, s.watcher.KillSession, s.sessionLivenessProbe)
			return AgentRef{}, errors.Join(commitErr, cleanup.Err)
		}
		if commitErr != nil || !persist.Durable {
			// Fail closed: Brain has no persistence-warning wire. Tear down the
			// committed route (kill first; preserve route if kill/resource
			// cleanup fails). Do not SetHostSession or audit successful replacement.
			durabilityErr := commitErr
			if durabilityErr == nil {
				durabilityErr = modelprofiles.ErrPersistDirSync
			}
			cleanup := modelprofiles.CleanupFailedLaunch(routes, "", agentID, s.watcher.KillSession, s.sessionLivenessProbe)
			return AgentRef{}, fmt.Errorf(
				"brain host profile commit not durable: %w",
				errors.Join(durabilityErr, cleanup.Err),
			)
		}
		provisionalID = ""
	} else if routes != nil && strings.TrimSpace(id) != "" && resumeToken != "" && id != agentID {
		persist, transferErr := routes.TransferSession(id, agentID)
		if settleErr := s.settleHostIdentityRouteTransfer(routes, id, agentID, persist, transferErr, true); settleErr != nil {
			s.recordHostReplacement(HostReplacementEvent{
				Reason:           hostReplaceReasonMissingTmuxUnrecoverable,
				FromID:           id,
				FromExecutorID:   hostSession.ExecutorID,
				ResolvedExecutor: executor.ID,
				Detail: fmt.Sprintf(
					"has_session=false id=%q provider_session=%q route_transfer=%v",
					id, resumeToken, settleErr,
				),
			})
			return AgentRef{}, fmt.Errorf(
				"brain host refusing blank replacement: %w",
				settleErr,
			)
		}
	}
	if resumeToken != "" {
		// CreateSession only proves tmux launch. Persist binding atomically with the
		// true resume token; do not clear-then-reseal.
		if err := s.store.ReplaceHostSessionBinding(
			agentID,
			executor.ID,
			resumeToken,
			hostSession.TranscriptPath,
			hostSession.ProviderDataRoot,
		); err != nil {
			// Roll route ownership back to the old Session id so resume remains possible.
			// Kill the new Session only after a durable rollback; otherwise preserve
			// the live possible route owner and surface recoverable failure.
			if routes != nil && strings.TrimSpace(id) != "" && id != agentID {
				persist, rollbackErr := routes.TransferSession(agentID, id)
				if !(persist.Applied && persist.Durable && rollbackErr == nil) {
					return AgentRef{}, fmt.Errorf(
						"brain host store bind failed and route rollback %q <- %q did not durably restore (live session retained): %w",
						id, agentID, errors.Join(err, rollbackErr, nondurableOrIncomplete(persist, rollbackErr)),
					)
				}
			}
			killErr := s.watcher.KillSession(agentID)
			return AgentRef{}, errors.Join(err, killErr)
		}
	} else if err := s.store.SetHostSession(agentID, executor.ID); err != nil {
		if routes != nil {
			cleanup := modelprofiles.CleanupFailedLaunch(routes, provisionalID, agentID, s.watcher.KillSession, s.sessionLivenessProbe)
			return AgentRef{}, errors.Join(err, cleanup.Err)
		}
		killErr := s.watcher.KillSession(agentID)
		return AgentRef{}, errors.Join(err, killErr)
	}
	if replaceReason == hostReplaceReasonMissingTmux || replaceReason == hostReplaceReasonProviderMismatch {
		createdReason := replaceReason + "_created"
		if resumeToken != "" {
			// Honest: tmux launched a resume command; provider acceptance is later.
			createdReason = hostReplaceReasonMissingTmuxResumeLaunched
		}
		s.recordHostReplacement(HostReplacementEvent{
			Reason:           createdReason,
			FromID:           id,
			ToID:             agentID,
			FromExecutorID:   hostSession.ExecutorID,
			ResolvedExecutor: executor.ID,
			Detail:           replaceDetail,
		})
	}
	if resumeToken == "" {
		if prompt := s.hostBootstrapPrompt(executor); prompt != "" {
			_ = s.watcher.SendInputWhenReady(agentID, command, prompt+"\n")
		}
	}
	if agent := s.watcher.GetAgent(agentID); agent != nil {
		return agentRefFromClassifier(agent), nil
	}
	return AgentRef{
		ID:      agentID,
		Name:    "Brain",
		Status:  string(classifier.StateRunning),
		Summary: "Session starting",
		Cwd:     s.brainWorkspace(),
		Command: command,
		Updated: s.now().UTC(),
		Hidden:  true,
	}, nil
}

func brainSessionEnvironment() map[string]string {
	env := map[string]string{}
	if root, err := work.DefaultWorktreeRoot(); err == nil {
		env["ZEN_WORKTREE_ROOT"] = root
	}
	return env
}

func mergeStringMaps(base, overlay map[string]string) map[string]string {
	if len(overlay) == 0 {
		return base
	}
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		out[k] = v
	}
	return out
}

// recoverMatchingHost finds a live Brain-owned host that still represents the
// recorded provider session. It must not rebind an unrelated hidden session
// (for example main:@0 "codex resume") and pretend that is continuity.
// Candidate ProbeSession Unknown fails closed — never falls through to spawn.
func (s *Service) recoverMatchingHost(executor work.AgentExecutor, hostSession HostSession) (*classifier.Agent, error) {
	if s == nil || s.watcher == nil {
		return nil, nil
	}
	workspace := s.brainWorkspace()
	wantSession := strings.TrimSpace(hostSession.ProviderSessionID)
	wantPath := strings.TrimSpace(hostSession.TranscriptPath)
	provider := strings.TrimSpace(executor.Provider)
	if provider == "" || provider == work.AgentProviderCustom {
		provider = work.InferAgentProvider(executor.Command, executor.ID)
	}
	wantDerived := ""
	if provider == work.AgentProviderCodex && wantPath != "" {
		wantDerived = work.CodexSessionIDFromRolloutPath(wantPath)
	}
	hasWant := wantSession != "" || wantPath != "" || wantDerived != ""
	var fallback *classifier.Agent
	for _, agent := range s.watcher.Agents() {
		if agent == nil || !agent.Hidden {
			continue
		}
		if !s.hostAgentMatches(agent, executor) {
			continue
		}
		if !isBrainOwnedHostAgent(agent, workspace) {
			continue
		}
		presence, probeErr := s.watcher.ProbeSession(agent.ID)
		switch {
		case probeErr != nil || presence == watcher.SessionPresenceUnknown:
			if probeErr == nil {
				probeErr = fmt.Errorf("tmux probe returned unknown for %q", agent.ID)
			}
			return nil, fmt.Errorf("brain host candidate liveness unknown: %w", probeErr)
		case presence != watcher.SessionPresencePresent:
			continue
		}
		cp := *agent
		if hasWant {
			token, present, err := work.ProviderResumeToken(provider, agent.Command)
			if err != nil || !present {
				continue
			}
			if (wantSession != "" && token == wantSession) ||
				(wantPath != "" && token == wantPath) ||
				(wantDerived != "" && token == wantDerived) {
				return &cp, nil
			}
			continue
		}
		if fallback == nil {
			fallback = &cp
		}
	}
	return fallback, nil
}

// rebindRecoveredHost transfers any Model Profile route oldID→recovered.ID before
// replacing the host binding. Failed/not-applied/applied-nondurable transfer
// (except no-route) preserves the old binding and live host with no
// recovered_alive audit. host_session.json is never treated as a durability
// barrier for route-bindings.json.
func (s *Service) rebindRecoveredHost(oldID string, recovered *classifier.Agent, executor work.AgentExecutor, hostSession HostSession) error {
	if s == nil || s.store == nil || recovered == nil {
		return fmt.Errorf("brain host recover: missing service or recovered agent")
	}
	newID := strings.TrimSpace(recovered.ID)
	if newID == "" {
		return fmt.Errorf("brain host recover: empty recovered id")
	}
	oldID = strings.TrimSpace(oldID)

	routes := s.sessionRoutes()
	routeTransferred := false
	if routes != nil && oldID != "" && oldID != newID {
		persist, transferErr := routes.TransferSession(oldID, newID)
		if settleErr := s.settleHostIdentityRouteTransfer(routes, oldID, newID, persist, transferErr, false); settleErr != nil {
			return settleErr
		}
		// No-route is a successful settle with nothing transferred.
		if persist.Applied {
			routeTransferred = true
		}
	}

	providerToken := strings.TrimSpace(hostSession.ProviderSessionID)
	transcriptPath := strings.TrimSpace(hostSession.TranscriptPath)
	providerRoot := strings.TrimSpace(hostSession.ProviderDataRoot)
	if providerToken == "" && transcriptPath != "" {
		provider := strings.TrimSpace(executor.Provider)
		if provider == "" || provider == work.AgentProviderCustom {
			provider = work.InferAgentProvider(executor.Command, executor.ID)
		}
		if provider == work.AgentProviderCodex {
			if derived := work.CodexSessionIDFromRolloutPath(transcriptPath); derived != "" {
				providerToken = derived
			}
		}
	}
	if err := s.store.ReplaceHostSessionBinding(
		newID,
		executor.ID,
		providerToken,
		transcriptPath,
		providerRoot,
	); err != nil {
		if routeTransferred && routes != nil {
			persist, rollbackErr := routes.TransferSession(newID, oldID)
			if !(persist.Applied && persist.Durable && rollbackErr == nil) {
				return fmt.Errorf(
					"brain host recover bind failed and route rollback %q <- %q did not durably restore (live host retained): %w",
					oldID, newID, errors.Join(err, rollbackErr, nondurableOrIncomplete(persist, rollbackErr)),
				)
			}
		}
		// Keep old binding; do not audit recovered_alive; do not kill live host.
		return err
	}
	s.recordHostReplacement(HostReplacementEvent{
		Reason:           hostReplaceReasonRecoveredAlive,
		FromID:           oldID,
		ToID:             newID,
		FromExecutorID:   hostSession.ExecutorID,
		FromCommand:      recovered.Command,
		ResolvedExecutor: executor.ID,
		Detail:           "recorded host missing; rebound matching live Brain host",
	})
	return nil
}

// settleHostIdentityRouteTransfer enforces one route/host convergence invariant:
// Applied+!Durable TransferSession must not become a successful host binding or
// recovered/resume audit. host_session.json is not a durability barrier for
// route-bindings.json. On nondurable apply, compensate toward fromID; only a
// durable compensation may authorize killing toID (resume spawn). Recovered
// live hosts never kill (killOnCompensated=false).
func (s *Service) settleHostIdentityRouteTransfer(
	routes SessionRouteLifecycle,
	fromID, toID string,
	persist modelprofiles.PersistResult,
	transferErr error,
	killOnCompensated bool,
) error {
	fromID = strings.TrimSpace(fromID)
	toID = strings.TrimSpace(toID)
	if routes == nil || fromID == "" || toID == "" || fromID == toID {
		return nil
	}
	if errors.Is(transferErr, modelprofiles.ErrBindingNotFound) && !persist.Applied {
		// No route for the recorded Session — host bind only.
		return nil
	}
	if !persist.Applied {
		var killErr error
		if killOnCompensated && s != nil && s.watcher != nil {
			killErr = s.watcher.KillSession(toID)
		}
		return errors.Join(fmt.Errorf(
			"route transfer %q -> %q failed: %w",
			fromID, toID, transferErr,
		), killErr)
	}
	if persist.Durable && transferErr == nil {
		return nil
	}
	durabilityErr := transferErr
	if durabilityErr == nil {
		durabilityErr = modelprofiles.ErrPersistDirSync
	}
	durabilityErr = errors.Join(ErrRouteTransferNotDurable, durabilityErr)

	rollbackPersist, rollbackErr := routes.TransferSession(toID, fromID)
	if rollbackPersist.Applied && rollbackPersist.Durable && rollbackErr == nil {
		var killErr error
		if killOnCompensated && s != nil && s.watcher != nil {
			killErr = s.watcher.KillSession(toID)
		}
		return errors.Join(fmt.Errorf(
			"route transfer %q -> %q applied but not durable; compensated to %q: %w",
			fromID, toID, fromID, durabilityErr,
		), killErr)
	}
	// Compensation failed or remains nondurable: preserve the live possible owner.
	return fmt.Errorf(
		"route transfer %q -> %q not durable and compensation did not durably restore %q (live owner %q retained): %w",
		fromID, toID, fromID, toID,
		errors.Join(durabilityErr, rollbackErr, nondurableOrIncomplete(rollbackPersist, rollbackErr)),
	)
}

func nondurableOrIncomplete(persist modelprofiles.PersistResult, err error) error {
	if persist.Applied && persist.Durable && err == nil {
		return nil
	}
	if !persist.Applied {
		if err != nil {
			return err
		}
		return modelprofiles.ErrLaunchCleanupIncomplete
	}
	if err != nil {
		return err
	}
	return modelprofiles.ErrPersistDirSync
}

func isBrainOwnedHostAgent(agent *classifier.Agent, workspace string) bool {
	if agent == nil || !agent.Hidden {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(agent.Name))
	if !strings.HasPrefix(name, "brain") {
		return false
	}
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return true
	}
	return strings.TrimSpace(agent.Cwd) == workspace
}

// hostProviderResumeToken resolves the native resume source for a recorded
// Brain host binding. Known+!resumable must fail closed (never blank-launch).
func (s *Service) hostProviderResumeToken(host HostSession, executor work.AgentExecutor) (token string, tokenKnown bool, tokenResumable bool) {
	command := strings.TrimSpace(executor.Command)
	if command == "" {
		command = strings.TrimSpace(executor.ID)
	}
	provider := strings.TrimSpace(executor.Provider)
	if provider == "" || provider == work.AgentProviderCustom {
		provider = work.InferAgentProvider(command, executor.ID)
	}
	id := strings.TrimSpace(host.ProviderSessionID)
	path := strings.TrimSpace(host.TranscriptPath)
	switch provider {
	case work.AgentProviderCodex:
		if id != "" {
			return id, true, true
		}
		if path == "" {
			return "", false, false
		}
		if sid := work.CodexSessionIDFromRolloutPath(path); sid != "" {
			return sid, true, true
		}
		resolved := work.ResolveCodexTranscriptIdentityForAgent(classifier.Agent{}, work.CodexTranscriptIdentity{
			Path:     path,
			DataRoot: strings.TrimSpace(host.ProviderDataRoot),
		})
		if sid := strings.TrimSpace(resolved.SessionID); sid != "" {
			return sid, true, true
		}
		return path, true, false
	case work.AgentProviderClaude, work.AgentProviderGrok, work.AgentProviderCursor:
		if id != "" {
			return id, true, true
		}
		if path != "" {
			return path, true, false
		}
		return "", false, false
	case work.AgentProviderOpenCode:
		if strings.HasPrefix(id, "ses_") {
			return id, true, true
		}
		if id != "" || path != "" {
			return firstNonEmpty(id, path), true, false
		}
		return "", false, false
	case work.AgentProviderPi:
		if filepath.IsAbs(id) {
			return id, true, true
		}
		if filepath.IsAbs(path) {
			return path, true, true
		}
		if id != "" || path != "" {
			return firstNonEmpty(id, path), true, false
		}
		return "", false, false
	default:
		if id != "" || path != "" {
			return firstNonEmpty(id, path), true, false
		}
		return "", false, false
	}
}

func (s *Service) recordHostReplacement(event HostReplacementEvent) {
	if strings.TrimSpace(event.Reason) == "" {
		return
	}
	if event.At.IsZero() {
		if s != nil && s.now != nil {
			event.At = s.now().UTC()
		} else {
			event.At = time.Now().UTC()
		}
	}
	log.Printf(
		"brain host replace reason=%s from=%q to=%q executor=%q detail=%s",
		event.Reason,
		strings.TrimSpace(event.FromID),
		strings.TrimSpace(event.ToID),
		strings.TrimSpace(event.ResolvedExecutor),
		strings.TrimSpace(event.Detail),
	)
	if s != nil && s.store != nil {
		if err := s.store.AppendHostReplacement(event); err != nil {
			log.Printf("brain host replace audit write failed: %v", err)
		}
	}
}

func (s *Service) hostExecutor() work.AgentExecutor {
	preferred := brainHostExecutorOverride()
	var hostSession HostSession
	if preferred == "" && s != nil && s.store != nil {
		if session, err := s.store.HostSession(); err == nil {
			hostSession = session
			preferred = strings.TrimSpace(session.ExecutorID)
		}
	}
	// When host_session.executor_id is empty, prefer the live host's provider over
	// the codex default. Defaulting to codex while a grok/claude Brain host is still
	// alive causes provider_mismatch kill+replace on every Snapshot/reconnect.
	// ensureHostAgent persists executor_id once the live host is matched.
	if preferred == "" && s != nil && s.watcher != nil {
		if id := strings.TrimSpace(hostSession.ID); id != "" {
			if agent := s.watcher.GetAgent(id); agent != nil && agent.Hidden {
				if provider := work.InferAgentProvider(agent.Command); provider != "" {
					preferred = provider
				}
			}
		}
	}
	if s != nil && s.execs != nil {
		if preferred != "" {
			if executor, ok := s.execs.AgentExecutor(preferred); ok {
				return executor
			}
		}
		if executor, ok := s.execs.AgentExecutor("codex"); ok {
			return executor
		}
	}
	return work.NewAgentExecutor("codex", work.Executor{Name: "codex", Command: "codex", Kind: "codex", Runtime: work.AgentRuntimeTmux})
}

func (s *Service) brainDelegatedExecutor() work.AgentExecutor {
	// Effective delegated selection (including startup env lock) is owned only
	// by ExecutorConfig — no parallel env readers on Brain paths.
	if s != nil && s.execs != nil {
		if executor, ok := s.execs.DelegatedAgentExecutor(); ok {
			return executor
		}
	}
	return s.hostExecutor()
}

func brainHostExecutorOverride() string {
	return strings.TrimSpace(os.Getenv("ZEN_BRAIN_HOST_EXECUTOR"))
}

func (s *Service) agentExecutors(hostExecutorID, delegatedExecutorID string) []work.AgentExecutor {
	if s == nil || s.execs == nil {
		if hostExecutorID == "" {
			hostExecutorID = "codex"
		}
		executor := work.NewAgentExecutor(hostExecutorID, work.Executor{Name: hostExecutorID, Command: hostExecutorID})
		executor.Host = true
		executor.Delegated = delegatedExecutorID == "" || executor.ID == delegatedExecutorID
		return []work.AgentExecutor{executor}
	}
	executors := s.execs.AgentExecutors()
	if len(executors) == 0 {
		if hostExecutorID == "" {
			hostExecutorID = "codex"
		}
		executor := work.NewAgentExecutor(hostExecutorID, work.Executor{Name: hostExecutorID, Command: hostExecutorID})
		executor.Host = true
		executor.Delegated = delegatedExecutorID == "" || executor.ID == delegatedExecutorID
		return []work.AgentExecutor{executor}
	}
	for i := range executors {
		executors[i].Host = executors[i].ID == hostExecutorID
		executors[i].Delegated = executors[i].ID == delegatedExecutorID
	}
	sort.Slice(executors, func(i, j int) bool {
		if executors[i].Host != executors[j].Host {
			return executors[i].Host
		}
		if executors[i].Delegated != executors[j].Delegated {
			return executors[i].Delegated
		}
		return executors[i].ID < executors[j].ID
	})
	return executors
}

func (s *Service) hostAgentMatches(agent *classifier.Agent, executor work.AgentExecutor) bool {
	if agent == nil || !agent.Hidden {
		return false
	}
	expectedProvider := strings.TrimSpace(executor.Provider)
	if expectedProvider != "" && expectedProvider != work.AgentProviderCustom {
		if work.InferAgentProvider(agent.Command) != expectedProvider {
			return false
		}
		return true
	}
	return commandBase(agent.Command) == commandBase(executor.Command)
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func (s *Service) hostCommand(executor work.AgentExecutor) (string, error) {
	return s.hostLaunchCommand(executor, "")
}

func (s *Service) hostLaunchCommand(executor work.AgentExecutor, resumeSessionID string) (string, error) {
	command := strings.TrimSpace(executor.Command)
	if command == "" {
		command = strings.TrimSpace(executor.ID)
	}
	if command == "" {
		command = "codex"
	}
	provider := strings.TrimSpace(executor.Provider)
	if provider == "" || provider == work.AgentProviderCustom {
		provider = work.InferAgentProvider(command, executor.ID)
	}
	workspace := s.brainWorkspace()
	resumeSessionID = strings.TrimSpace(resumeSessionID)

	switch provider {
	case work.AgentProviderCodex:
		if !codexCommandHasFullAuthorization(command) {
			command = strings.TrimSpace(command + " " + codexFullAuthorizationFlag)
		}
		if resumeSessionID != "" {
			var err error
			command, err = work.WithProviderResumeToken(provider, command, resumeSessionID)
			if err != nil {
				return "", err
			}
		}
		if !strings.Contains(command, "--no-alt-screen") {
			command = strings.TrimSpace(command + " --no-alt-screen")
		}
		if workspace != "" && !strings.Contains(command, " -C ") && !strings.Contains(command, " --cd ") {
			command = strings.TrimSpace(command + " -C " + shellQuote(workspace))
		}
		return withZenCLIOnPath(command), nil
	case work.AgentProviderClaude:
		command = work.HardenClaudeCommand(command)
		if resumeSessionID != "" {
			var err error
			command, err = work.WithProviderResumeToken(provider, command, resumeSessionID)
			if err != nil {
				return "", err
			}
		}
		if workspace != "" && !strings.Contains(command, " --add-dir ") {
			command = strings.TrimSpace(command + " --add-dir " + shellQuote(workspace))
		}
		return withZenCLIOnPath(command), nil
	case work.AgentProviderGrok, work.AgentProviderCursor:
		if resumeSessionID != "" {
			var err error
			command, err = work.WithProviderResumeToken(provider, command, resumeSessionID)
			if err != nil {
				return "", err
			}
		}
		return withZenCLIOnPath(command), nil
	case work.AgentProviderOpenCode:
		hardened, err := work.HardenOpenCodeDelegatedCommand(command)
		if err != nil {
			return "", err
		}
		if resumeSessionID != "" {
			hardened, err = work.WithProviderResumeToken(provider, hardened, resumeSessionID)
			if err != nil {
				return "", err
			}
		}
		return withZenCLIOnPath(hardened), nil
	case work.AgentProviderPi:
		if resumeSessionID != "" {
			command, err := work.WithProviderResumeToken(provider, command, resumeSessionID)
			if err != nil {
				return "", err
			}
			return withZenCLIOnPath(command), nil
		}
		command, err := work.EnsurePiSessionLaunchCommand(command)
		if err != nil {
			return "", err
		}
		return withZenCLIOnPath(command), nil
	default:
		if resumeSessionID != "" {
			return "", fmt.Errorf("executor %q has no native resume launch shape", firstNonEmpty(executor.ID, provider))
		}
		return withZenCLIOnPath(command), nil
	}
}

func withZenCLIOnPath(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return command
	}
	dir := zenExecutableDir()
	if dir == "" || pathContainsDir(os.Getenv("PATH"), dir) {
		return command
	}
	return "env PATH=" + shellQuote(dir) + ":$PATH " + command
}

func zenExecutableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	exe = strings.TrimSpace(exe)
	if exe == "" {
		return ""
	}
	base := strings.ToLower(commandBase(exe))
	if base != "zen" && base != "zen.exe" {
		return ""
	}
	return strings.TrimSpace(filepath.Dir(exe))
}

func pathContainsDir(pathValue, dir string) bool {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return false
	}
	for _, entry := range filepath.SplitList(pathValue) {
		if strings.TrimSpace(entry) == dir {
			return true
		}
	}
	return false
}

func codexCommandHasFullAuthorization(command string) bool {
	return strings.Contains(command, codexFullAuthorizationFlag)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func commandBase(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return ""
	}
	base := strings.ToLower(fields[0])
	if slash := strings.LastIndex(base, "/"); slash >= 0 {
		base = base[slash+1:]
	}
	return base
}

func (s *Service) hostBootstrapPrompt(executor work.AgentExecutor) string {
	snapshot, err := s.store.Snapshot()
	if err != nil {
		return ""
	}
	delegatedExecutor := s.brainDelegatedExecutor()
	worktreeRoot, _ := work.DefaultWorktreeRoot()
	return strings.TrimSpace(fmt.Sprintf(`
You are Brain inside zen, the user's private second brain and agent orchestrator.

Work as a warm, direct, capable chat assistant. Reply in the user's language unless they ask otherwise.

Brain workspace: %s
Managed worktree root: %s
Host executor: %s (%s via %s)
Delegated executor: %s (%s via %s)
Host executor capabilities: %s

Durable state rules:
- At the start of this Brain Host Session, read soul.md once before the first response or work. Follow its stable expression and judgment principles for this Session. Re-read it only if the file changes. Its private contents are not included in this bootstrap.
- Keep long-term memory in memory.md; read it only when durable memory is relevant to the user's current request.
- Keep user background, preferences, and profile notes in profile.md; read it when preferences or user background matter.
- Keep a human-readable handoff projection in current.md. Work/Event database state is authoritative.
- Use policies/delegation.md, policies/engine.md, and policies/handoff.md for stable lifecycle rules.
- Use playbooks/ for provider-neutral operating playbooks. Discover them with zen brain playbooks --json; read playbook files on demand (progressive disclosure — do not assume full bodies are in bootstrap).
- Use files in this workspace for plans, inbox notes, reminders, and follow-up state.
- Do not use arbitrary project repositories as Brain's default workspace.
- Treat this bootstrap as a map, not the full context. Prefer current.md and zen brain context --json for restoration. The Session-start rule above owns soul.md loading; read memory.md/profile.md on demand instead of assuming their contents are in the prompt.

Agent lifecycle rules:
- You are running in a real tmux agent session.
- This Brain host is launched with the most permissive available non-interactive authorization mode for its executor.
- The zen app sends user messages directly into this session.
- Treat the executor as replaceable; do not make Brain's plans depend on Codex-only or Claude-only behavior unless the user asks for that executor specifically.
- Host Executor runs Brain chat, planning, delegation, review, and final synthesis. Delegated Executor runs delegated agents and ordinary non-Brain sessions unless the user explicitly asks for a different executor for that session, such as @codex, @grok, or @claude. Do not switch executors based on private task-type judgment.
- Brain is the sole master orchestrator and scheduler above delegated Sessions. Given the user's goal and boundaries, independently decompose, order, choose or reuse scoped Sessions, review results, and advance the next runnable Work without asking the user to babysit the queue or type continue.
- Brain is the user's scheduler: reduce decision load.
- Brain's operating goal is to understand the task, decompose it into executable concerns, delegate progress to Workers, review the evidence, and close the loop. For repository and tool-backed work, normally create or reuse a visible delegated agent session; use judgment when direct execution is clearly the better route. Stay in Brain for chat, decomposition, judgment, review, memory, synthesis, reminders, and final decisions. A sustained coherent debugging thread normally belongs to one Worker while Brain remains the acceptance owner.
- Create Work only for a commitment that must survive the current turn. Ordinary questions and discussion create no Work.
- Work and append-only Events are the sole durable Brain scheduler state. current.md and provider state are projections or execution details, not alternate owners.
- Only an atomically claimed actionable Work Event may start an automatic Brain turn. Active or waiting Work without an Event stays idle.
- until_done changes when Work may be marked done; it never creates a wake or polling loop.
- Do not use a provider Goal as Brain scheduler state. Provider Goal support may remain local to an individual executor Session.
- Brain is the orchestrator, not the execution pool: keep decomposition, ordering, judgment, result review, and final synthesis in Brain. Use delegated agents for scoped execution.
- Delegate a subtask only when it can be named clearly. A delegated-agent brief should contain one concern, the workspace, enough context to avoid re-exploring the whole repo, acceptance criteria, safety constraints, feasible verification, and a short expected report.
- Run independent delegated subtasks in parallel when that reduces elapsed time. Do not parallelize work that shares fragile state or depends on unresolved product judgment. For a coherent debugging thread, prefer one Worker with the whole scoped concern while Brain retains decision and review ownership.
- Delegated agents should not invent the overall plan. If a delegated result is incomplete or off-target, inspect it, rewrite the brief or send a focused follow-up, and only patch over it directly when the fix is trivial.
- Review delegated results before integrating them: capture the session, compare the result with the acceptance criteria, run or inspect verification, and then decide whether to merge, follow up, or ask the user.
- For a single larger task, prefer reusing the same delegated agent session across stages. Send follow-up instructions to that session until the task is genuinely complete. Open a separate delegated session only when the work is meaningfully independent, benefits from parallelism, needs a different repository/context, or the current session is blocked or unusable.
- Use the repository supplied by the user as the default workspace, even when it is dirty; preserve unrelated changes. Delegation and parallelism do not themselves justify a worktree.
- Create a worktree only for genuine concurrent-write isolation or when the user explicitly requests one. Reuse it for the larger task and place it under $ZEN_WORKTREE_ROOT (%s), never on OS temporary or memory-backed storage.
- Use TMPDIR/TMP/TEMP for Agent-owned scratch and audit state, and $ZEN_BUILD_TMPDIR for large disposable builds when supported. Never hard-code OS-global temp paths; bounded tool-internal temp is allowed. Remove owned artifacts before reporting done.
- Use the zen binary to spawn, send to, and inspect delegated agents. When delegating, write a short note with workspace, objective, context, acceptance criteria, safety constraints, and expected report.
- Zen CLI quick reference:
  - %s brain context --json returns structured Brain context: current.md, host executor, and delegated agents.
  - %s brain playbooks --json returns the playbook catalog (name, description, path) without full playbook bodies.
  - %s brain gc --json repairs product-owned standard Brain workspace blocks and missing files while preserving user-authored content, then reports open delegated sessions.
  - zen brain work list --json lists durable Work. zen brain work create/update changes only the named commitment.
  - %s agent list --json lists visible sessions; only sessions with delegated=true are Brain-owned.
  - %s agent spawn -name "<name>" -cwd <workspace> -prompt "<task>" creates a visible delegated agent with Brain's delegated executor routing.
  - A visible delegated spawn creates bounded Work automatically. Use -work to attach an existing Work; use -completion until_done with -done-criteria only for an explicit verified-completion requirement.
  - %s agent spawn -name "<name>" -executor <executor> -cwd <workspace> -prompt "<task>" creates a visible delegated agent with an explicit user-requested executor override.
  - %s agent capture -id <agent_id> --json inspects a delegated agent.
  - %s agent send -id <agent_id> -text "<message>" --submit=true continues a delegated agent.
  - %s agent close -id <agent_id> closes a delegated agent after the larger task is complete and its result is recorded or reported.
  - Use %s calendar list/get/create/update/cancel/run for explicit time intent. event, reminder, and deadline are passive Calendar records; scheduled_action launches delegated execution.
  - Before creating a scheduled_action, obtain the current Brain thread_id from %s brain context --json and pass that exact value as -source-thread (source_thread_id). Never invent, omit, or silently retarget this thread. The canonical full result, or a concise failure, returns idempotently to that captured Brain thread; unread state and notifications are projections. A recurring series continues after a failed occurrence.
  - Calendar create uses a local YYYY-MM-DD date, HH:MM wall time, and IANA timezone. If the local time occurs twice at DST fall-back, ask the user to choose -occurrence first or second; never guess. After create, update, or run, repeat the resolved local date, time, timezone, recurrence/effect, and result destination from the command confirmation. Do not infer Calendar items from unrelated messages.
- Delegated agent lifecycle: keep ownership from spawn through inspection, follow-up, result consolidation, and close. Do not close a delegated session merely because a small stage finished; close it when the larger task is complete or you have intentionally moved the remaining work elsewhere.
- Never close, kill, rename, repurpose, or otherwise manage sessions whose agent list entry does not have delegated=true. Those belong to the user or another tool.
- Keep lifecycle principles in Markdown, prompts, and agent instructions. Product code should provide tools, context, persistence, visibility, and safety boundaries rather than rigid workflow gates.
- Treat a direct Work Event input as one claimed actionable delta; use its compact facts and inspect only its referenced change, then act, summarize, or wait.
- Every direct Work Event has resolution_required=true and an exact resolve_command. Before the provider Turn ends, run that command with one typed disposition; keep event_id, handling_id, provider_turn_id, and revision unchanged.
- Every completed, failed, blocked, or needs-input delegated Attempt must end with one typed disposition and a durable next action. Admit the next useful Attempt, establish a specific wait, complete/cancel the Work, or consolidate the one decision that genuinely requires the user; never leave an ordinary terminal Event as an unattended card.
- Use a source-specific producer wake or due_retry with next_attempt_at for discoverable external conditions. Never use generic user_input as a polling clock, infer Calendar work, sleep in Brain, or hold a Turn open.
- After handling an Event, re-anchor to the foreground Work, verify its current status and durable next action, and take the next useful lifecycle step before waiting.
- Continue low-risk next steps autonomously. Research discoverable environment facts with tools or delegated agents. Interrupt the user only for a material values choice, a new permission or credential, irreversible or high-impact action outside existing approval, or a blocker with no safe default. Put every currently independent required decision in one small numbered round with a recommended default. Let unresolved research block only dependent decisions, and proceed when remaining unknowns have safe defaults and completion is checkable; when blocked, consolidate options and a recommendation.

Current personality:
%s

Reference files:
- current.md: active objective, decisions, open threads, next step
- memory.md: durable long-term memory
- profile.md: user profile and preferences
- policies/delegation.md
- policies/engine.md
- policies/handoff.md
- playbooks/ (catalog via zen brain playbooks --json)
`, snapshot.Workspace, worktreeRoot, executor.ID, executor.Provider, executor.Runtime, delegatedExecutor.ID, delegatedExecutor.Provider, delegatedExecutor.Runtime, executorCapabilitiesSummary(executor.Capabilities), worktreeRoot, zenCLICommand(), zenCLICommand(), zenCLICommand(), zenCLICommand(), zenCLICommand(), zenCLICommand(), zenCLICommand(), zenCLICommand(), zenCLICommand(), zenCLICommand(), zenCLICommand(), strings.TrimSpace(snapshot.Personality)))
}

func (s *Service) handoffHostSession(threadID, previousExecutorID, nextExecutorID, nextHostID, currentContext string, agents []AgentRef) error {
	if s == nil || s.store == nil || s.watcher == nil {
		return nil
	}
	threadID = strings.TrimSpace(threadID)
	nextHostID = strings.TrimSpace(nextHostID)
	if threadID == "" || nextHostID == "" {
		return nil
	}
	delegatedExecutor := s.brainDelegatedExecutor()
	prompt := formatHostHandoffPrompt(threadID, previousExecutorID, nextExecutorID, delegatedExecutor.ID, currentContext, agents)
	if prompt != "" {
		hostCmd, err := s.hostCommand(s.hostExecutor())
		if err != nil {
			return err
		}
		if err := s.watcher.SendInputWhenReady(nextHostID, hostCmd, prompt+"\n"); err != nil {
			return err
		}
	}
	return nil
}

func formatHostHandoffPrompt(threadID, previousExecutorID, nextExecutorID, delegatedExecutorID, currentContext string, agents []AgentRef) string {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return ""
	}
	lines := []string{
		"Brain host executor handoff:",
		"The user switched Brain host executors. This is the same visible Brain chat, not a new conversation.",
		"Continue naturally in the user's current language. Do not mention this handoff unless the user asks.",
		"Current thread id: " + threadID,
	}
	if strings.TrimSpace(previousExecutorID) != "" {
		lines = append(lines, "Previous host executor: "+strings.TrimSpace(previousExecutorID))
	}
	if strings.TrimSpace(nextExecutorID) != "" {
		lines = append(lines, "Current host executor: "+strings.TrimSpace(nextExecutorID))
	}
	if strings.TrimSpace(delegatedExecutorID) != "" {
		lines = append(lines, "Delegated executor: "+strings.TrimSpace(delegatedExecutorID))
	}
	lines = append(lines,
		"",
		"Primary persisted context:",
		"Read current.md in the Brain workspace before continuing. Its current contents are included below when available.",
	)
	if strings.TrimSpace(currentContext) != "" {
		lines = append(lines, "", "current.md:", strings.TrimSpace(currentContext))
	}
	lines = append(lines,
		"",
		"Executor policy:",
		"- Host Executor runs Brain chat, planning, delegation, review, and final synthesis.",
		"- Delegated Executor runs delegated agents and ordinary non-Brain sessions unless the user explicitly asks for a different executor for that session.",
		"- Use a different executor only when the user explicitly mentions or asks for it, such as @codex, @grok, or @claude.",
		"",
		"Lifecycle policy:",
		"- Brain keeps decomposition, ordering, judgment, result review, and final synthesis.",
		"- Delegated agents are scoped execution sessions: give each one concern, enough context, acceptance criteria, verification, safety constraints, and a short expected report.",
		"- Run independent subtasks in parallel when useful. Do not parallelize shared fragile state or unresolved product judgment; for a coherent debugging thread, prefer one Worker with the whole scoped concern while Brain retains decision and review ownership.",
		"- Inspect delegated results before integrating them. If a result is off-target, rewrite the brief or send a focused follow-up instead of silently absorbing the mistake.",
	)
	delegated := []string{}
	for _, agent := range agents {
		if !agent.Delegated {
			continue
		}
		entry := strings.TrimSpace(agent.Name)
		if entry == "" {
			entry = strings.TrimSpace(agent.ID)
		}
		if status := strings.TrimSpace(agent.Status); status != "" {
			entry += " [" + status + "]"
		}
		if summary := strings.TrimSpace(agent.Summary); summary != "" {
			entry += ": " + summary
		}
		if entry != "" {
			delegated = append(delegated, entry)
		}
	}
	if len(delegated) > 0 {
		lines = append(lines, "", "Open delegated agents:")
		for _, entry := range delegated {
			lines = append(lines, "- "+entry)
		}
	}
	lines = append(lines, "", "Wait for the next user message or direct Work Event input.")
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func zenCLICommand() string {
	exe, err := os.Executable()
	if err != nil || strings.TrimSpace(exe) == "" {
		return "zen"
	}
	if strings.ContainsAny(exe, " \t'") {
		return shellQuote(exe)
	}
	return exe
}

func executorCapabilitiesSummary(caps work.AgentCapabilities) string {
	parts := []string{}
	if caps.InteractiveTTY {
		parts = append(parts, "interactive_tty")
	}
	if caps.StructuredEvents {
		parts = append(parts, "structured_events")
	}
	if len(parts) == 0 {
		return "none declared"
	}
	return strings.Join(parts, ", ")
}

func (s *Service) agentRefs(hostID string) []AgentRef {
	if s == nil || s.watcher == nil {
		return []AgentRef{}
	}
	agents := s.watcher.Agents()
	out := make([]AgentRef, 0, len(agents))
	for _, agent := range agents {
		if agent == nil {
			continue
		}
		if agent.Hidden || (hostID != "" && agent.ID == hostID) {
			continue
		}
		out = append(out, agentRefFromClassifier(agent))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Updated.After(out[j].Updated)
	})
	return out
}

func presentDelegatedSessions(agents []AgentRef) map[string]bool {
	present := make(map[string]bool, len(agents))
	for _, agent := range agents {
		if agent.Delegated && strings.TrimSpace(agent.ID) != "" {
			present[strings.TrimSpace(agent.ID)] = true
		}
	}
	return present
}

func agentRefFromClassifier(agent *classifier.Agent) AgentRef {
	if agent == nil {
		return AgentRef{}
	}
	var startedAt *time.Time
	if !agent.StartedAt.IsZero() {
		value := agent.StartedAt
		startedAt = &value
	}
	return AgentRef{
		ID:        agent.ID,
		Name:      agent.Name,
		Status:    string(agent.State),
		Summary:   agent.Summary,
		Cwd:       agent.Cwd,
		Command:   agent.Command,
		StartedAt: startedAt,
		ProcessID: agent.ProcessID,
		Updated:   agent.UpdatedAt,
		Hidden:    agent.Hidden,
		Delegated: agent.Delegated,
	}
}

func (s *Service) brainWorkspace() string {
	if s != nil && s.store != nil {
		return s.store.WorkspacePath()
	}
	return ""
}
