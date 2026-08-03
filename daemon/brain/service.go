package brain

import (
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
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

var (
	ErrExecutorNotConfigured = errors.New("brain host executor is not configured")
	ErrExecutorLockedByEnv   = errors.New("brain host executor is locked by environment override")
)

const codexFullAuthorizationFlag = work.CodexFullAuthorizationFlag

const workEventWakeCue = work.WorkEventWakeCue

type Watcher interface {
	Agents() []*classifier.Agent
	GetAgent(id string) *classifier.Agent
	HasSession(target string) bool
	CreateSession(preferredTarget string, opts watcher.CreateSessionOptions) (string, error)
	SendInput(sessionID, text string) error
	SendInputWhenReady(sessionID, command, text string) error
	SendInputWithReceipt(sessionID, text, receipt string) error
	KillSession(sessionID string) error
}

type Service struct {
	store   *Store
	watcher Watcher
	execs   *work.ExecutorConfig
	now     func() time.Time

	dispatchMu      sync.Mutex
	foregroundInput bool

	reconcileMu                sync.Mutex
	authoritativeInventorySeen bool
	delegatedInventory         map[string]struct{}
	unmanagedInventory         map[string]struct{}
}

func NewService(store *Store, watcher Watcher, execs *work.ExecutorConfig) *Service {
	return &Service{
		store:   store,
		watcher: watcher,
		execs:   execs,
		now:     time.Now,
	}
}

func (s *Service) Snapshot() (Snapshot, error) {
	snapshot, err := s.store.Snapshot()
	if err != nil {
		return Snapshot{}, err
	}
	activeWork, err := s.store.ActiveWork()
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
	snapshot.ActiveWork = activeWork
	return snapshot, nil
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
		ActiveWork:        snapshot.ActiveWork,
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
func (s *Service) RouteSessionEvent(event watcher.SessionEvent) (bool, error) {
	if s == nil || s.store == nil || event.Agent == nil {
		return false, nil
	}
	agent := event.Agent
	if !agent.Delegated || agent.Hidden || strings.TrimSpace(agent.ID) == "" {
		return false, nil
	}
	if event.Type == "agent_removed" {
		s.reconcileMu.Lock()
		if s.unmanagedInventory == nil {
			s.unmanagedInventory = make(map[string]struct{})
		}
		s.unmanagedInventory[agent.ID] = struct{}{}
		delete(s.delegatedInventory, agent.ID)
		s.reconcileMu.Unlock()
	}
	item, found, err := s.store.WorkByOwnerSession(agent.ID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}

	kind, actionable, update, ok := sessionEventProjection(event)
	if !ok {
		return false, nil
	}
	if actionable && calendarOwnsTerminalResult(item, event) {
		actionable = false
	}
	if workUpdateChanges(item, update) {
		item, err = s.store.UpdateWork(item.ID, update)
		if err != nil {
			return false, err
		}
	}
	dedupeKey, err := s.sessionEventDedupeKey(item.ID, event, kind)
	if err != nil {
		return false, err
	}
	_, created, err := s.store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       kind,
		DedupeKey:  dedupeKey,
		PayloadRef: "session:" + agent.ID,
		Actionable: actionable,
	})
	if err != nil || !created || !actionable {
		return false, err
	}
	return s.DispatchPendingEvent()
}

func calendarOwnsTerminalResult(item Work, event watcher.SessionEvent) bool {
	if !strings.HasPrefix(strings.TrimSpace(item.ContextRef), "calendar:") ||
		event.Agent == nil ||
		(event.Type != "agent_state_change" && event.Type != "agent_removed") {
		return false
	}
	state := classifier.AgentState(firstNonEmpty(event.NewState, string(event.Agent.State)))
	return state == classifier.StateDone ||
		state == classifier.StateFailed ||
		state == classifier.StateRemoved
}

func sessionEventProjection(event watcher.SessionEvent) (string, bool, WorkUpdate, bool) {
	agent := event.Agent
	if agent == nil {
		return "", false, WorkUpdate{}, false
	}
	sessionWait := "Session " + agent.ID
	switch event.Type {
	case "agent_metadata_change":
		if !agent.NeedsAttention {
			return "session.progress", false, WorkUpdate{}, true
		}
		switch strings.TrimSpace(agent.Attention) {
		case "user_input", "blocked":
			status := WorkNeedsInput
			next := "Resolve the delegated Session request."
			return "session.needs_input", true, WorkUpdate{
				Status:     &status,
				NextAction: &next,
				WaitFor:    &sessionWait,
			}, true
		case "failed":
			status := WorkWaiting
			next := "Inspect the delegated Session failure."
			return "session.failed", true, WorkUpdate{
				Status:     &status,
				NextAction: &next,
				WaitFor:    &sessionWait,
			}, true
		default:
			return "session.progress", false, WorkUpdate{}, true
		}
	case "agent_state_change", "agent_removed":
		state := firstNonEmpty(event.NewState, string(agent.State))
		switch classifier.AgentState(state) {
		case classifier.StateRunning:
			status := WorkRunning
			next := "Wait for the delegated Session."
			return "session.running", false, WorkUpdate{
				Status:     &status,
				NextAction: &next,
				WaitFor:    &sessionWait,
			}, true
		case classifier.StateUnknown:
			status := WorkWaiting
			next := "Wait for a durable Session lifecycle fact."
			return "session.waiting", false, WorkUpdate{
				Status:     &status,
				NextAction: &next,
				WaitFor:    &sessionWait,
			}, true
		case classifier.StateBlocked:
			status := WorkNeedsInput
			next := "Resolve the delegated Session blocker."
			return "session.needs_input", true, WorkUpdate{
				Status:     &status,
				NextAction: &next,
				WaitFor:    &sessionWait,
			}, true
		case classifier.StateDone:
			status := WorkWaiting
			next := "Review the delegated Session result."
			empty := ""
			return "session.done", true, WorkUpdate{
				Status:     &status,
				NextAction: &next,
				WaitFor:    &empty,
			}, true
		case classifier.StateFailed, classifier.StateRemoved:
			status := WorkWaiting
			next := "Inspect the delegated Session failure."
			empty := ""
			return "session.failed", true, WorkUpdate{
				Status:     &status,
				NextAction: &next,
				WaitFor:    &empty,
			}, true
		}
	}
	return "", false, WorkUpdate{}, false
}

func (s *Service) sessionEventDedupeKey(workID string, event watcher.SessionEvent, kind string) (string, error) {
	agent := event.Agent
	if agent == nil {
		return "session:" + strings.TrimSpace(event.AgentID) + ":" + kind + ":1", nil
	}
	events, err := s.store.ListWorkEvents(workID)
	if err != nil {
		return "", err
	}
	occurrence := 1
	lastLifecycleKind := ""
	lastDedupeKey := ""
	payloadRef := "session:" + agent.ID
	for _, current := range events {
		if current.PayloadRef != payloadRef || !isSessionLifecycleKind(current.Kind) {
			continue
		}
		lastLifecycleKind = current.Kind
		lastDedupeKey = current.DedupeKey
		if current.Kind == kind {
			occurrence++
		}
	}
	if lastLifecycleKind == kind && lastDedupeKey != "" {
		return lastDedupeKey, nil
	}
	return fmt.Sprintf("session:%s:%s:%d", agent.ID, kind, occurrence), nil
}

func isSessionLifecycleKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "session.running", "session.waiting", "session.needs_input",
		"session.done", "session.failed", "session.stale":
		return true
	default:
		return false
	}
}

func workUpdateChanges(item Work, update WorkUpdate) bool {
	return update.Title != nil && strings.TrimSpace(*update.Title) != item.Title ||
		update.Objective != nil && strings.TrimSpace(*update.Objective) != item.Objective ||
		update.Status != nil && *update.Status != item.Status ||
		update.OwnerSessionID != nil && strings.TrimSpace(*update.OwnerSessionID) != item.OwnerSessionID ||
		update.CompletionPolicy != nil && *update.CompletionPolicy != item.CompletionPolicy ||
		update.DoneCriteriaRef != nil && strings.TrimSpace(*update.DoneCriteriaRef) != item.DoneCriteriaRef ||
		update.NextAction != nil && strings.TrimSpace(*update.NextAction) != item.NextAction ||
		update.WaitFor != nil && strings.TrimSpace(*update.WaitFor) != item.WaitFor ||
		update.ContextRef != nil && strings.TrimSpace(*update.ContextRef) != item.ContextRef
}

func legacySessionWorkID(sessionID string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(sessionID)))
	return fmt.Sprintf("session-%x", digest[:12])
}

func (s *Service) MigrateDelegatedSessionsV1(agents []*classifier.Agent) (bool, error) {
	if s == nil || s.store == nil {
		return false, nil
	}
	candidates := []Work{}
	for _, agent := range agents {
		if agent == nil || !agent.Delegated || agent.Hidden || strings.TrimSpace(agent.ID) == "" {
			continue
		}
		status := WorkWaiting
		if agent.State == classifier.StateRunning {
			status = WorkRunning
		}
		candidates = append(candidates, Work{
			Title:            firstNonEmpty(agent.Name, "Delegated work"),
			Objective:        firstNonEmpty(agent.Summary, "Complete the delegated Session."),
			Status:           status,
			OwnerSessionID:   agent.ID,
			CompletionPolicy: CompletionBounded,
			NextAction:       "Wait for the delegated Session.",
			WaitFor:          "Session " + agent.ID,
			ContextRef:       "session:" + agent.ID,
		})
	}
	return s.store.MigrateDelegatedSessionsV1(candidates)
}

// DispatchPendingEvent is the complete automatic scheduler: claim one durable
// actionable Event, send one constant cue, then stop. The Host reads and
// consumes the identity-bound Event through Zen control. A claimed Event is
// never replayed after an ambiguous send.
func (s *Service) DispatchPendingEvent() (bool, error) {
	if s == nil || s.store == nil || s.watcher == nil {
		return false, nil
	}
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()
	claimedEvents, err := s.store.ClaimedActionableEvents()
	if err != nil {
		return false, err
	}
	if len(claimedEvents) != 0 {
		return false, nil
	}
	hostSession, err := s.store.HostSession()
	if err != nil {
		return false, err
	}
	hostID := strings.TrimSpace(hostSession.ID)
	if hostID == "" || !s.watcher.HasSession(hostID) {
		return false, nil
	}
	if s.foregroundInput {
		return false, nil
	}
	host := s.watcher.GetAgent(hostID)
	if host != nil && (host.State == classifier.StateRunning || host.State == classifier.StateBlocked) {
		return false, nil
	}
	event, claimed, err := s.store.ClaimNextActionableEvent(hostID)
	if err != nil || !claimed {
		return false, err
	}
	if err := s.watcher.SendInputWithReceipt(
		hostID,
		workEventWakeCue,
		event.ID,
	); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) ConsumeHostWorkEvent(hostSessionID string) (WorkEvent, Work, bool, error) {
	if s == nil || s.store == nil {
		return WorkEvent{}, Work{}, false, fmt.Errorf("brain service is not configured")
	}
	host, err := s.store.HostSession()
	if err != nil {
		return WorkEvent{}, Work{}, false, err
	}
	hostSessionID = strings.TrimSpace(hostSessionID)
	if hostSessionID == "" || hostSessionID != strings.TrimSpace(host.ID) {
		return WorkEvent{}, Work{}, false, ErrEventClaim
	}
	return s.store.ConsumeClaimedWorkEvent(hostSessionID)
}

func (s *Service) NoteUserSteering(agentID string) bool {
	if s == nil || s.store == nil {
		return false
	}
	host, err := s.store.HostSession()
	if err != nil || strings.TrimSpace(host.ID) == "" || strings.TrimSpace(host.ID) != strings.TrimSpace(agentID) {
		return false
	}
	s.dispatchMu.Lock()
	s.foregroundInput = true
	s.dispatchMu.Unlock()
	return true
}

func (s *Service) CancelUserSteering(agentID string) {
	if s == nil || s.store == nil {
		return
	}
	host, err := s.store.HostSession()
	if err != nil || strings.TrimSpace(host.ID) != strings.TrimSpace(agentID) {
		return
	}
	s.dispatchMu.Lock()
	s.foregroundInput = false
	s.dispatchMu.Unlock()
	_, _ = s.DispatchPendingEvent()
}

// ObserveHostSessionEvent lets foreground user steering finish before a queued
// internal Event is claimed. The assistant turn ending is not itself persisted
// as an Event; it only makes the existing Event claimable.
func (s *Service) ObserveHostSessionEvent(event watcher.SessionEvent) (bool, error) {
	if s == nil || s.store == nil || event.Agent == nil || !event.Agent.Hidden {
		return false, nil
	}
	host, err := s.store.HostSession()
	if err != nil || strings.TrimSpace(host.ID) != strings.TrimSpace(event.Agent.ID) {
		return false, err
	}
	state := classifier.AgentState(firstNonEmpty(event.NewState, string(event.Agent.State)))
	if state == classifier.StateRunning {
		return false, nil
	}
	s.dispatchMu.Lock()
	wasForeground := s.foregroundInput
	s.foregroundInput = false
	s.dispatchMu.Unlock()
	if wasForeground || state == classifier.StateDone || state == classifier.StateUnknown {
		return s.DispatchPendingEvent()
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
	firstAuthoritativeInventory := !s.authoritativeInventorySeen
	byID := make(map[string]*classifier.Agent, len(agents))
	if s.delegatedInventory == nil {
		s.delegatedInventory = make(map[string]struct{})
	}
	if s.unmanagedInventory == nil {
		s.unmanagedInventory = make(map[string]struct{})
	}
	for _, agent := range agents {
		if agent != nil {
			byID[agent.ID] = agent
			if agent.Delegated && !agent.Hidden {
				s.delegatedInventory[agent.ID] = struct{}{}
				delete(s.unmanagedInventory, agent.ID)
			} else {
				s.unmanagedInventory[agent.ID] = struct{}{}
				delete(s.delegatedInventory, agent.ID)
			}
		}
	}
	items, err := s.store.ListWork()
	if err != nil {
		log.Printf("brain Work reconciliation list failed: %v", err)
		return
	}
	s.authoritativeInventorySeen = true
	now := s.nowUTC()
	for _, item := range items {
		if item.Status == WorkDone || item.Status == WorkCancelled || strings.TrimSpace(item.OwnerSessionID) == "" {
			continue
		}
		agent := byID[item.OwnerSessionID]
		if agent == nil {
			_, knownDelegated := s.delegatedInventory[item.OwnerSessionID]
			if _, knownUnmanaged := s.unmanagedInventory[item.OwnerSessionID]; knownUnmanaged {
				continue
			}
			if !firstAuthoritativeInventory && !knownDelegated {
				continue
			}
			s.reconcileAbsentDelegatedSession(item)
			continue
		}
		if !agent.Delegated || agent.Hidden {
			continue
		}
		if agent.State == classifier.StateDone || agent.State == classifier.StateFailed || agent.State == classifier.StateRemoved {
			_, _ = s.RouteSessionEvent(watcher.SessionEvent{
				Type:     "agent_state_change",
				AgentID:  agent.ID,
				Agent:    agent,
				OldState: string(agent.State),
				NewState: string(agent.State),
			})
			continue
		}
		if agent.State != classifier.StateRunning && agent.State != classifier.StateUnknown {
			continue
		}
		leaseActive := agent.ExpectedNextCheckAt != nil &&
			!now.After(agent.ExpectedNextCheckAt.UTC())
		if !agent.PaneAlive && agent.ProcessID <= 0 && !leaseActive {
			s.reconcileAbsentDelegatedSession(item)
			continue
		}
		if agent.ExpectedNextCheckAt == nil || agent.LastProgressAt == nil {
			continue
		}
		if now.Before(agent.ExpectedNextCheckAt.UTC()) {
			status := WorkWaiting
			wait := "Session " + agent.ID
			next := "Wait for the delegated Session lease."
			update := WorkUpdate{Status: &status, WaitFor: &wait, NextAction: &next}
			if workUpdateChanges(item, update) {
				_, _ = s.store.UpdateWork(item.ID, update)
			}
			continue
		}
		status := WorkWaiting
		wait := "Session " + agent.ID + " is live; progress lease overdue."
		next := "Wait for authoritative delegated Session state."
		update := WorkUpdate{Status: &status, WaitFor: &wait, NextAction: &next}
		if workUpdateChanges(item, update) {
			_, _ = s.store.UpdateWork(item.ID, update)
		}
	}
	_, _ = s.DispatchPendingEvent()
}

func (s *Service) reconcileAbsentDelegatedSession(item Work) {
	_, _, created, err := s.store.ReconcileMissingWorkOwner(item.ID, item.OwnerSessionID)
	if err != nil {
		log.Printf("brain absent Session reconciliation failed for %s: %v", item.OwnerSessionID, err)
	} else if created {
		_, _ = s.DispatchPendingEvent()
	}
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
	return s.store.MarkWorkRead(workID)
}

func (s *Service) AppendWorkEvent(event WorkEvent) (WorkEvent, bool, error) {
	if s == nil || s.store == nil {
		return WorkEvent{}, false, fmt.Errorf("brain service is not configured")
	}
	recorded, created, err := s.store.AppendWorkEvent(event)
	if err != nil || !created || !recorded.Actionable {
		return recorded, created, err
	}
	_, dispatchErr := s.DispatchPendingEvent()
	return recorded, created, dispatchErr
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
	item, _, err := s.store.EnsureWork(Work{
		ID:               calendarWorkID(event.Item.ID, run.ID),
		Title:            firstNonEmpty(run.Title, event.Item.Title),
		Objective:        strings.TrimSpace(event.Item.ActionInstruction),
		Status:           WorkRunning,
		OwnerSessionID:   strings.TrimSpace(run.AgentSession),
		CompletionPolicy: CompletionBounded,
		NextAction:       "Wait for the scheduled action.",
		WaitFor:          calendarWaitCondition(run),
		ContextRef:       contextRef,
	})
	if err != nil {
		return false, err
	}

	kind := "calendar.due"
	actionable := false
	payloadRef := contextRef
	update := WorkUpdate{}
	switch run.Status {
	case calendar.StatusRunning:
		status := WorkRunning
		next := "Wait for the scheduled action."
		wait := calendarWaitCondition(run)
		owner := strings.TrimSpace(run.AgentSession)
		update = WorkUpdate{
			Status:         &status,
			OwnerSessionID: &owner,
			NextAction:     &next,
			WaitFor:        &wait,
		}
		if owner != "" {
			kind = "calendar.launched"
		}
	case calendar.StatusCompleted:
		status := WorkDone
		empty := ""
		update = WorkUpdate{Status: &status, NextAction: &empty, WaitFor: &empty}
		kind = "calendar.result"
		actionable = true
		if event.ScheduledResult != nil {
			payloadRef = event.ScheduledResult.ID
		}
	case calendar.StatusFailed:
		status := WorkNeedsInput
		next := "Inspect the scheduled action failure."
		empty := ""
		update = WorkUpdate{Status: &status, NextAction: &next, WaitFor: &empty}
		kind = "calendar.failure"
		actionable = true
		if event.ScheduledResult != nil {
			payloadRef = event.ScheduledResult.ID
		}
	default:
		return false, nil
	}
	if workUpdateChanges(item, update) {
		item, err = s.store.UpdateWork(item.ID, update)
		if err != nil {
			return false, err
		}
	}
	recorded, created, err := s.store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       kind,
		DedupeKey:  fmt.Sprintf("calendar:%s:%s:%s", event.Item.ID, run.ID, kind),
		PayloadRef: payloadRef,
		Actionable: actionable,
	})
	if err != nil || !created || !recorded.Actionable {
		return false, err
	}
	return s.DispatchPendingEvent()
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
	hostReplaceReasonMissingTmux      = "missing_tmux"
	hostReplaceReasonProviderMismatch = "provider_mismatch"
	hostReplaceReasonNoRecordedHost   = "no_recorded_host"
	hostReplaceReasonRecoveredAlive   = "recovered_alive_host"
)

func (s *Service) ensureHostAgent(executor work.AgentExecutor) (AgentRef, error) {
	if s == nil || s.store == nil || s.watcher == nil {
		return AgentRef{}, nil
	}
	hostSession, err := s.store.HostSession()
	if err != nil {
		return AgentRef{}, err
	}
	command := s.hostCommand(executor)
	id := strings.TrimSpace(hostSession.ID)
	replaceReason := ""
	replaceDetail := ""

	if id != "" && s.watcher.HasSession(id) {
		if agent := s.watcher.GetAgent(id); agent != nil {
			if s.hostAgentMatches(agent, executor) {
				if strings.TrimSpace(hostSession.ExecutorID) != executor.ID {
					if err := s.store.SetHostSession(id, executor.ID); err != nil {
						return AgentRef{}, err
					}
				}
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
			_ = s.watcher.KillSession(id)
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
	} else if id != "" {
		// Recorded host id is gone from tmux. Prefer re-binding an already-running
		// hidden Brain host for this executor over spawning a blank session.
		if recovered := s.recoverMatchingHost(executor); recovered != nil {
			if err := s.store.SetHostSession(recovered.ID, executor.ID); err != nil {
				return AgentRef{}, err
			}
			s.recordHostReplacement(HostReplacementEvent{
				Reason:           hostReplaceReasonRecoveredAlive,
				FromID:           id,
				ToID:             recovered.ID,
				FromExecutorID:   hostSession.ExecutorID,
				FromCommand:      recovered.Command,
				ResolvedExecutor: executor.ID,
				Detail:           "recorded host missing; rebound matching live Brain host",
			})
			return agentRefFromClassifier(recovered), nil
		}
		replaceReason = hostReplaceReasonMissingTmux
		replaceDetail = fmt.Sprintf("has_session=false id=%q", id)
		s.recordHostReplacement(HostReplacementEvent{
			Reason:           replaceReason,
			FromID:           id,
			FromExecutorID:   hostSession.ExecutorID,
			ResolvedExecutor: executor.ID,
			Detail:           replaceDetail,
		})
	} else {
		replaceReason = hostReplaceReasonNoRecordedHost
	}

	agentID, err := s.watcher.CreateSession("", watcher.CreateSessionOptions{
		Cwd:         s.brainWorkspace(),
		Command:     command,
		Name:        "Brain",
		Detached:    true,
		Hidden:      true,
		ProgressEnv: true,
		Env:         brainSessionEnvironment(),
	})
	if err != nil {
		return AgentRef{}, err
	}
	if err := s.store.SetHostSession(agentID, executor.ID); err != nil {
		return AgentRef{}, err
	}
	if replaceReason == hostReplaceReasonMissingTmux || replaceReason == hostReplaceReasonProviderMismatch {
		// Record the newly created target as the replacement destination.
		s.recordHostReplacement(HostReplacementEvent{
			Reason:           replaceReason + "_created",
			FromID:           id,
			ToID:             agentID,
			FromExecutorID:   hostSession.ExecutorID,
			ResolvedExecutor: executor.ID,
			Detail:           replaceDetail,
		})
	}
	if prompt := s.hostBootstrapPrompt(executor); prompt != "" {
		_ = s.watcher.SendInputWhenReady(agentID, command, prompt+"\n")
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

// recoverMatchingHost finds a live hidden Brain host that matches the resolved
// executor. Used when host_session.json points at a dead tmux target so Snapshot
// can rebind instead of always spawning a fresh host.
func (s *Service) recoverMatchingHost(executor work.AgentExecutor) *classifier.Agent {
	if s == nil || s.watcher == nil {
		return nil
	}
	for _, agent := range s.watcher.Agents() {
		if agent == nil || !agent.Hidden {
			continue
		}
		if !s.hostAgentMatches(agent, executor) {
			continue
		}
		if !s.watcher.HasSession(agent.ID) {
			continue
		}
		cp := *agent
		return &cp
	}
	return nil
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

func (s *Service) hostCommand(executor work.AgentExecutor) string {
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
	switch provider {
	case "codex":
		args := []string{command}
		if !codexCommandHasFullAuthorization(command) {
			args = append(args, codexFullAuthorizationFlag)
		}
		if !strings.Contains(command, "--no-alt-screen") {
			args = append(args, "--no-alt-screen")
		}
		if workspace != "" && !strings.Contains(command, " -C ") && !strings.Contains(command, " --cd ") {
			args = append(args, "-C", shellQuote(workspace))
		}
		return withZenCLIOnPath(strings.Join(args, " "))
	case "claude":
		command = work.HardenClaudeCommand(command)
		if workspace != "" && !strings.Contains(command, " --add-dir ") {
			return withZenCLIOnPath(strings.TrimSpace(command + " --add-dir " + shellQuote(workspace)))
		}
		return withZenCLIOnPath(command)
	default:
		return withZenCLIOnPath(command)
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
- Keep long-term memory in memory.md; read it only when durable memory is relevant to the user's current request.
- Keep personality, preferences, and profile notes in profile.md; read it when preferences or user background matter.
- Keep a human-readable handoff projection in current.md. Work/Event database state is authoritative.
- Use policies/delegation.md, policies/engine.md, and policies/handoff.md for stable orchestration rules.
- Use playbooks/ for provider-neutral operating playbooks. Discover them with zen brain playbooks --json; read playbook files on demand (progressive disclosure — do not assume full bodies are in bootstrap).
- Use files in this workspace for plans, inbox notes, reminders, and follow-up state.
- Do not use arbitrary project repositories as Brain's default workspace.
- Treat this bootstrap as a map, not the full context. Prefer current.md and zen brain context --json for restoration; read memory.md/profile.md on demand instead of assuming they are in the prompt.

Agent orchestration rules:
- You are running in a real tmux agent session.
- This Brain host is launched with the most permissive available non-interactive authorization mode for its executor.
- The zen app sends user messages directly into this session.
- Treat the executor as replaceable; do not make Brain's plans depend on Codex-only or Claude-only behavior unless the user asks for that executor specifically.
- Host Executor runs Brain chat, planning, delegation, review, and final synthesis. Delegated Executor runs delegated agents and ordinary non-Brain sessions unless the user explicitly asks for a different executor for that session, such as @codex, @grok, or @claude. Do not switch executors based on private task-type judgment.
- Brain is the user's scheduler: reduce decision load. For concrete work that needs repository/tool execution, independent progress, parallelism, or follow-up, proactively create or reuse a visible delegated agent session; stay in Brain for chat, memory, synthesis, reminders, and decisions that fit the current context.
- Create Work only for a commitment that must survive the current turn. Ordinary questions and discussion create no Work.
- Work and append-only Events are the sole durable Brain scheduler state. current.md and provider state are projections or execution details, not alternate owners.
- Only an atomically claimed actionable Work Event may start an automatic Brain turn. Active or waiting Work without an Event stays idle.
- until_done changes when Work may be marked done; it never creates a wake or polling loop.
- Do not use a provider Goal as Brain scheduler state. Provider Goal support may remain local to an individual executor Session.
- Brain is the orchestrator, not the execution pool: keep decomposition, ordering, judgment, result review, and final synthesis in Brain. Use delegated agents for scoped execution.
- Delegate a subtask only when it can be named clearly. A delegated-agent brief should contain one concern, the workspace, enough context to avoid re-exploring the whole repo, acceptance criteria, safety constraints, feasible verification, and a short expected report.
- Run independent delegated subtasks in parallel when that reduces elapsed time. Do not parallelize work that shares fragile state, needs one coherent debugging thread, or depends on unresolved product judgment.
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
- Keep orchestration principles in Markdown, prompts, and agent instructions. Product code should provide tools, context, persistence, visibility, and safety boundaries rather than rigid workflow gates.
- Treat an Active work event message as one claimed actionable delta; inspect only its referenced change, then act, summarize, or wait.
- Continue low-risk next steps autonomously. Ask only when critical context is missing, an action is high-risk or irreversible, credentials/permissions are needed, or the decision depends on the user's values; when blocked, consolidate options and a recommendation.

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
		if err := s.watcher.SendInputWhenReady(nextHostID, s.hostCommand(s.hostExecutor()), prompt+"\n"); err != nil {
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
		"Orchestration policy:",
		"- Brain keeps decomposition, ordering, judgment, result review, and final synthesis.",
		"- Delegated agents are scoped execution sessions: give each one concern, enough context, acceptance criteria, verification, safety constraints, and a short expected report.",
		"- Run independent subtasks in parallel when useful; keep coupled design decisions and gnarly single-thread debugging in Brain.",
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
	lines = append(lines, "", "Wait for the next user message unless an unconsumed actionable Work event is already present.")
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
