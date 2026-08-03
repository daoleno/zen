package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/calendar"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

type controlWatcher interface {
	Agents() []*classifier.Agent
	GetAgent(id string) *classifier.Agent
	HasSession(target string) bool
	CreateSession(preferredTarget string, opts watcher.CreateSessionOptions) (string, error)
	UpdateAgentProgress(id string, progress classifier.AgentProgress) (*classifier.Agent, error)
	SettleAgentInputAccepted(id string, handoffStartedAt time.Time, phase, summary string) (*classifier.Agent, error)
	SendInput(sessionID, text string) error
	SendInputWithReceipt(sessionID, text, receipt string) error
	HasInputReceipt(sessionID, receipt string) (bool, error)
	SendInputWhenReady(sessionID, command, text string) error
	SubmitInputWhenReady(sessionID, command, payload string) error
	KillSession(sessionID string) error
	CapturePaneContent(sessionID string) (string, error)
}

type controlApp struct {
	auth              *auth.Manager
	watcher           controlWatcher
	execs             *work.ExecutorConfig
	brainStore        *brain.Store
	brainService      *brain.Service
	calendarStore     *calendar.Store
	calendarScheduler *calendar.Scheduler
	stateDir          string
}

func (a *controlApp) HandleControlRequest(req control.Request) control.Response {
	switch strings.TrimSpace(req.Type) {
	case "agent_list":
		return a.handleAgentList()
	case "agent_spawn":
		return a.handleAgentSpawn(req)
	case "agent_send":
		return a.handleAgentSend(req)
	case "agent_capture":
		return a.handleAgentCapture(req)
	case "agent_status":
		return a.handleAgentStatus(req)
	case "agent_progress":
		return a.handleAgentProgress(req)
	case "agent_event":
		return a.handleAgentEvent(req)
	case "agent_close", "agent_kill":
		return a.handleAgentClose(req)
	case "brain_executors":
		return a.handleBrainExecutors()
	case "brain_context":
		return a.handleBrainContext()
	case "brain_playbooks":
		return a.handleBrainPlaybooks()
	case "brain_gc":
		return a.handleBrainGC()
	case "brain_work_list":
		return a.handleBrainWorkList(req)
	case "brain_work_create":
		return a.handleBrainWorkCreate(req)
	case "brain_work_update":
		return a.handleBrainWorkUpdate(req)
	case "brain_work_event":
		return a.handleBrainWorkEvent(req)
	case "brain_set_executor":
		return a.handleBrainSetExecutor(req)
	case "set_delegated_executor":
		return a.handleSetDelegatedExecutor(req)
	case "brain_workspace":
		if a == nil || a.brainStore == nil {
			return control.ErrorResponse("brain_unavailable", "Brain workspace is not configured.")
		}
		return control.Response{OK: true, Workspace: a.brainStore.WorkspacePath()}
	case "calendar_list":
		if a == nil || a.calendarStore == nil {
			return control.ErrorResponse("calendar_unavailable", "Calendar is not configured.")
		}
		return control.Response{OK: true, CalendarItems: a.calendarStore.List()}
	case "calendar_get":
		return a.handleCalendarGet(req)
	case "calendar_create":
		return a.handleCalendarCreate(req)
	case "calendar_update":
		return a.handleCalendarUpdate(req)
	case "calendar_cancel":
		return a.handleCalendarCancel(req)
	case "calendar_run":
		return a.handleCalendarRun(req)
	case "device_list":
		return a.handleDeviceList()
	case "device_revoke":
		return a.handleDeviceRevoke(req)
	case "pair":
		return a.handlePair()
	default:
		return control.ErrorResponse("unknown_request", fmt.Sprintf("Unknown control request: %s", req.Type))
	}
}

func (a *controlApp) handleDeviceList() control.Response {
	if a == nil || a.auth == nil {
		return control.ErrorResponse("auth_unavailable", "Device authentication is not configured.")
	}
	return control.Response{
		OK:      true,
		Devices: a.auth.ListDevices(),
	}
}

func (a *controlApp) handleDeviceRevoke(req control.Request) control.Response {
	if a == nil || a.auth == nil {
		return control.ErrorResponse("auth_unavailable", "Device authentication is not configured.")
	}
	deviceID := strings.TrimSpace(req.ID)
	if deviceID == "" {
		return control.ErrorResponse("invalid_device", "A device ID is required.")
	}
	return revokeDeviceControlResponse(a.auth, deviceID)
}

func revokeDeviceControlResponse(
	manager *auth.Manager,
	deviceID string,
) control.Response {
	persistence, err := manager.RevokeDevice(deviceID)
	return deviceRevokeControlResponseFromResult(
		deviceID,
		persistence,
		err,
	)
}

func deviceRevokeControlResponseFromResult(
	deviceID string,
	persistence auth.PersistenceResult,
	err error,
) control.Response {
	if !persistence.Applied {
		if errors.Is(err, auth.ErrUnknownDevice) {
			durable := false
			response := control.ErrorResponse(
				"device_not_found",
				"The paired device was not found.",
			)
			response.PersistenceOutcome =
				control.PersistenceVerifiedAbsent
			response.PersistenceDurable = &durable
			return response
		}
		if err == nil {
			err = errors.New("trusted-device persistence did not apply")
		}
		return control.ErrorResponse("device_revoke_failed", err.Error())
	}
	durable := persistence.Durable
	confirmation := "Revoked device " + deviceID + "."
	if err != nil {
		log.Printf(
			"revoked device %q but directory durability is uncertain: %v",
			deviceID,
			err,
		)
		confirmation = "Revoked device " + deviceID +
			"; persistence was applied but directory durability is uncertain."
	}
	return control.Response{
		OK:                 true,
		PersistenceOutcome: control.PersistenceApplied,
		PersistenceDurable: &durable,
		Confirmation:       confirmation,
	}
}

func (a *controlApp) handlePair() control.Response {
	if a == nil || a.auth == nil {
		return control.ErrorResponse("auth_unavailable", "Device authentication is not configured.")
	}
	return issuePairingControlResponse(a.auth)
}

func issuePairingControlResponse(manager *auth.Manager) control.Response {
	pairing, err := manager.IssuePairingToken(auth.DefaultPairingTTL)
	if err != nil {
		return control.ErrorResponse("pair_failed", err.Error())
	}
	return control.Response{
		OK: true,
		Pairing: &control.PairingInfo{
			Token:           pairing.Value,
			ExpiresAt:       pairing.ExpiresAt,
			DaemonID:        manager.DaemonID(),
			DaemonPublicKey: manager.PublicKeyHex(),
		},
	}
}

func (a *controlApp) handleCalendarGet(req control.Request) control.Response {
	if a == nil || a.calendarStore == nil {
		return control.ErrorResponse("calendar_unavailable", "Calendar is not configured.")
	}
	item, err := a.calendarStore.Get(strings.TrimSpace(req.ID))
	if err != nil {
		return calendarControlError(err)
	}
	return calendarControlResponse(item, "Found")
}
func (a *controlApp) handleCalendarCreate(req control.Request) control.Response {
	if a == nil || a.calendarStore == nil || req.CalendarItem == nil {
		return control.ErrorResponse("invalid_calendar_item", "A calendar item is required.")
	}
	item, err := a.calendarStore.Create(*req.CalendarItem)
	if err != nil {
		return calendarControlError(err)
	}
	return calendarControlResponse(item, "Created")
}
func (a *controlApp) handleCalendarUpdate(req control.Request) control.Response {
	if a == nil || a.calendarStore == nil || req.CalendarItem == nil {
		return control.ErrorResponse("invalid_calendar_item", "A calendar item is required.")
	}
	item, err := a.calendarStore.Update(*req.CalendarItem, req.Revision)
	if err != nil {
		return calendarControlError(err)
	}
	return calendarControlResponse(item, "Updated")
}
func (a *controlApp) handleCalendarCancel(req control.Request) control.Response {
	if a == nil || a.calendarStore == nil {
		return control.ErrorResponse("calendar_unavailable", "Calendar is not configured.")
	}
	item, err := a.calendarStore.Cancel(strings.TrimSpace(req.ID), req.Revision)
	if err != nil {
		return calendarControlError(err)
	}
	return calendarControlResponse(item, "Cancelled")
}
func (a *controlApp) handleCalendarRun(req control.Request) control.Response {
	if a == nil || a.calendarScheduler == nil {
		return control.ErrorResponse("calendar_unavailable", "Calendar scheduler is not configured.")
	}
	item, err := a.calendarScheduler.RunNow(context.Background(), strings.TrimSpace(req.ID))
	if err != nil {
		return calendarControlError(err)
	}
	return calendarRunControlResponse(item)
}
func calendarControlResponse(item calendar.Item, verb string) control.Response {
	loc, _ := time.LoadLocation(item.Timezone)
	local := item.TriggerAt().In(loc).Format("2006-01-02 15:04:05 MST")
	action := "Zen will show it in Calendar"
	if verb == "Cancelled" {
		action = "Zen will keep it visible as cancelled and will not act on it"
	} else {
		switch item.Kind {
		case calendar.KindReminder:
			action = "Zen will notify you"
		case calendar.KindDeadline:
			action = "Zen will keep the deadline visible"
		case calendar.KindScheduledAction:
			action = "Zen will launch visible Work when the daemon is online"
		case calendar.KindEvent:
			action = "Zen will reserve the start/end time"
		}
	}
	confirmation := fmt.Sprintf("%s %s for %s (%s). %s.", verb, item.Kind, local, item.Timezone, action)
	return control.Response{OK: true, CalendarItem: &item, Confirmation: confirmation}
}
func calendarRunControlResponse(item calendar.Item) control.Response {
	startedAt := time.Now()
	if len(item.Runs) > 0 {
		startedAt = item.Runs[len(item.Runs)-1].StartedAt
	}
	loc, _ := time.LoadLocation(item.Timezone)
	local := startedAt.In(loc).Format("2006-01-02 15:04:05 MST")
	confirmation := fmt.Sprintf("Started scheduled_action at %s (%s). Zen launched visible Work and will reconcile it to completed or failed.", local, item.Timezone)
	if item.Status == calendar.StatusFailed {
		confirmation = fmt.Sprintf("Could not start scheduled_action at %s (%s). No execution is running: %s", local, item.Timezone, item.FailureReason)
	}
	return control.Response{OK: true, CalendarItem: &item, Confirmation: confirmation}
}
func calendarControlError(err error) control.Response {
	code := "calendar_request_failed"
	switch {
	case errors.Is(err, calendar.ErrNotFound):
		code = "calendar_not_found"
	case errors.Is(err, calendar.ErrConflict):
		code = "conflict"
	case errors.Is(err, calendar.ErrClaimed):
		code = "already_running"
	}
	return control.ErrorResponse(code, err.Error())
}

func (a *controlApp) handleAgentList() control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Agent watcher is not running.")
	}
	agents := visibleControlAgents(a.watcher.Agents())
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].UpdatedAt.After(agents[j].UpdatedAt)
	})
	return control.Response{OK: true, Agents: agents}
}

func (a *controlApp) handleAgentSpawn(req control.Request) control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Agent watcher is not running.")
	}

	command, err := a.resolveSpawnCommand(req)
	if err != nil {
		return control.ErrorResponse("invalid_executor", err.Error())
	}

	cwd := strings.TrimSpace(req.Cwd)
	if cwd == "" {
		return control.ErrorResponse("missing_cwd", "Agent spawn requires an explicit working directory.")
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = defaultAgentName(req.Executor, command)
	}

	prompt, err := spawnPrompt(req)
	if err != nil {
		return control.ErrorResponse("prompt_failed", err.Error())
	}

	var ownedWork brain.Work
	if !req.Hidden {
		ownedWork, err = a.prepareSpawnWork(req, name, prompt)
		if err != nil {
			return brainWorkControlError(err)
		}
	}

	agentID, err := a.watcher.CreateSession("", watcher.CreateSessionOptions{
		Cwd:         cwd,
		Command:     command,
		Name:        name,
		Detached:    true,
		Hidden:      req.Hidden,
		ProgressEnv: true,
		Delegated:   !req.Hidden,
		Env:         progressEnvForStateDir(a.stateDir),
	})
	if err != nil {
		a.recordSpawnWorkFailure(ownedWork, err)
		return control.ErrorResponse("spawn_failed", err.Error())
	}
	if ownedWork.ID != "" {
		ownedWork, err = a.brainStore.AttachWorkOwner(ownedWork.ID, agentID)
		if err != nil {
			_ = a.watcher.KillSession(agentID)
			return brainWorkControlError(err)
		}
	}

	if prompt != "" {
		var sendErr error
		if watcher.IsCodexCommand(command) {
			sendErr = a.submitCodexHandoff(agentID, command, prompt, true)
		} else {
			sendErr = a.watcher.SendInputWhenReady(agentID, command, ensureTrailingNewline(prompt))
			if sendErr != nil {
				a.recordSubmissionFailure(agentID, "Initial delegated prompt was not submitted: "+sendErr.Error())
			}
		}
		if sendErr != nil {
			a.recordSpawnWorkFailure(ownedWork, sendErr)
			return control.ErrorResponse("send_prompt_failed", sendErr.Error())
		}
	}

	agent := a.watcher.GetAgent(agentID)
	if agent == nil {
		response := control.Response{
			OK: true,
			Agent: &control.Agent{
				ID:        agentID,
				Name:      name,
				Status:    string(classifier.StateRunning),
				Cwd:       cwd,
				Command:   command,
				Hidden:    req.Hidden,
				Delegated: !req.Hidden,
			},
		}
		if ownedWork.ID != "" {
			response.BrainWork = &ownedWork
		}
		return response
	}
	out := controlAgent(agent)
	response := control.Response{OK: true, Agent: &out}
	if ownedWork.ID != "" {
		response.BrainWork = &ownedWork
	}
	return response
}

func (a *controlApp) prepareSpawnWork(req control.Request, name, prompt string) (brain.Work, error) {
	if a == nil || a.brainStore == nil {
		return brain.Work{}, fmt.Errorf("Brain Work store is not configured")
	}
	if workID := strings.TrimSpace(req.WorkID); workID != "" {
		item, err := a.brainStore.Work(workID)
		if err != nil {
			return brain.Work{}, err
		}
		if item.Status == brain.WorkDone || item.Status == brain.WorkCancelled {
			return brain.Work{}, fmt.Errorf("Brain Work %s is already %s", item.ID, item.Status)
		}
		if strings.TrimSpace(item.OwnerSessionID) != "" {
			return brain.Work{}, fmt.Errorf(
				"%w: Work %s is owned by %s",
				brain.ErrWorkOwnerConflict,
				item.ID,
				item.OwnerSessionID,
			)
		}
		return item, nil
	}
	policy := brain.CompletionBounded
	doneCriteria := ""
	contextRef := ""
	if req.BrainWork != nil {
		if req.BrainWork.CompletionPolicy != "" {
			policy = req.BrainWork.CompletionPolicy
		}
		doneCriteria = req.BrainWork.DoneCriteriaRef
		contextRef = req.BrainWork.ContextRef
	}
	objective := strings.TrimSpace(prompt)
	if objective == "" {
		objective = "Complete " + strings.TrimSpace(name) + "."
	}
	return a.brainStore.CreateWork(brain.Work{
		Title:            name,
		Objective:        objective,
		Status:           brain.WorkOpen,
		CompletionPolicy: policy,
		DoneCriteriaRef:  doneCriteria,
		NextAction:       "Start the delegated Session.",
		ContextRef:       contextRef,
	})
}

func (a *controlApp) recordSpawnWorkFailure(item brain.Work, spawnErr error) {
	if a == nil || a.brainStore == nil || item.ID == "" {
		return
	}
	status := brain.WorkNeedsInput
	next := "Resolve the delegated Session launch failure."
	wait := strings.TrimSpace(spawnErr.Error())
	_, _ = a.brainStore.UpdateWork(item.ID, brain.WorkUpdate{
		Status:     &status,
		NextAction: &next,
		WaitFor:    &wait,
	})
}

func (a *controlApp) handleBrainWorkList(req control.Request) control.Response {
	if a == nil || a.brainStore == nil {
		return control.ErrorResponse("brain_unavailable", "Brain Work is not configured.")
	}
	if workID := strings.TrimSpace(req.WorkID); workID != "" {
		item, err := a.brainStore.Work(workID)
		if err != nil {
			return brainWorkControlError(err)
		}
		events, err := a.brainStore.ListWorkEvents(workID)
		if err != nil {
			return brainWorkControlError(err)
		}
		return control.Response{OK: true, BrainWork: &item, BrainWorkEvents: events}
	}
	items, err := a.brainStore.ListWork()
	if err != nil {
		return brainWorkControlError(err)
	}
	return control.Response{OK: true, BrainWorks: items}
}

func (a *controlApp) handleBrainWorkCreate(req control.Request) control.Response {
	if a == nil || a.brainStore == nil || req.BrainWork == nil {
		return control.ErrorResponse("invalid_brain_work", "Brain Work is required.")
	}
	item, err := a.brainStore.CreateWork(*req.BrainWork)
	if err != nil {
		return brainWorkControlError(err)
	}
	return control.Response{OK: true, BrainWork: &item}
}

func (a *controlApp) handleBrainWorkUpdate(req control.Request) control.Response {
	if a == nil || a.brainStore == nil || req.BrainWork == nil {
		return control.ErrorResponse("invalid_brain_work", "Brain Work update is required.")
	}
	source := req.BrainWork
	update := brain.WorkUpdate{}
	for _, field := range req.WorkFields {
		switch strings.TrimSpace(field) {
		case "title":
			update.Title = &source.Title
		case "objective":
			update.Objective = &source.Objective
		case "status":
			update.Status = &source.Status
		case "owner_session_id":
			update.OwnerSessionID = &source.OwnerSessionID
		case "completion_policy":
			update.CompletionPolicy = &source.CompletionPolicy
		case "done_criteria_ref":
			update.DoneCriteriaRef = &source.DoneCriteriaRef
		case "next_action":
			update.NextAction = &source.NextAction
		case "wait_for":
			update.WaitFor = &source.WaitFor
		case "context_ref":
			update.ContextRef = &source.ContextRef
		default:
			return control.ErrorResponse("invalid_brain_work", "Unknown Brain Work field: "+field)
		}
	}
	if len(req.WorkFields) == 0 {
		return control.ErrorResponse("invalid_brain_work", "At least one Brain Work field is required.")
	}
	item, err := a.brainStore.UpdateWork(strings.TrimSpace(req.WorkID), update)
	if err != nil {
		return brainWorkControlError(err)
	}
	return control.Response{OK: true, BrainWork: &item}
}

func (a *controlApp) handleBrainWorkEvent(req control.Request) control.Response {
	if a == nil || a.brainService == nil || req.BrainWorkEvent == nil {
		return control.ErrorResponse("brain_unavailable", "Brain Work event routing is not configured.")
	}
	event, created, err := a.brainService.AppendWorkEvent(*req.BrainWorkEvent)
	if err != nil {
		return brainWorkControlError(err)
	}
	response := control.Response{OK: true, BrainWorkEvent: &event}
	if !created {
		response.Confirmation = "Duplicate event already recorded."
	}
	return response
}

func (a *controlApp) handleAgentEvent(req control.Request) control.Response {
	if a == nil || a.brainService == nil {
		return control.ErrorResponse("brain_unavailable", "Brain Work event routing is not configured.")
	}
	event, item, found, err := a.brainService.ConsumeHostWorkEvent(strings.TrimSpace(req.AgentID))
	if err != nil {
		return control.ErrorResponse("event_identity_mismatch", err.Error())
	}
	if !found {
		return control.Response{OK: true, Confirmation: "No Work Event is currently assigned."}
	}
	return control.Response{OK: true, BrainWork: &item, BrainWorkEvent: &event}
}

func brainWorkControlError(err error) control.Response {
	code := "brain_work_failed"
	switch {
	case errors.Is(err, brain.ErrWorkNotFound):
		code = "brain_work_not_found"
	case errors.Is(err, brain.ErrWorkConflict):
		code = "conflict"
	case errors.Is(err, brain.ErrWorkOwnerConflict):
		code = "conflict"
	}
	return control.ErrorResponse(code, err.Error())
}

func (a *controlApp) handleAgentSend(req control.Request) control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Agent watcher is not running.")
	}
	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		return control.ErrorResponse("missing_agent_id", "Agent id is required.")
	}
	agent := a.watcher.GetAgent(agentID)
	if agent != nil && !agent.Delegated && !agent.Hidden && !req.Force {
		return control.ErrorResponse("agent_not_delegated", "Refusing to send input to a session that was not created as a Brain delegated agent. Use --force only when you intentionally want to control this external session.")
	}
	payload := req.Text
	if strings.TrimSpace(payload) == "" && !req.Submit {
		return control.ErrorResponse("missing_text", "Text is required.")
	}
	if agent != nil && !a.watcher.HasSession(agentID) {
		return control.ErrorResponse("agent_session_unavailable", "Agent is listed but the tmux target is no longer available. Refresh the agent list and spawn a new session if needed.")
	}
	var sendErr error
	if req.Submit && agent != nil && watcher.IsCodexCommand(agent.Command) {
		sendErr = a.submitCodexHandoff(agentID, agent.Command, payload, false)
	} else {
		if req.Submit {
			payload = ensureTrailingNewline(payload)
		}
		sendErr = a.watcher.SendInput(agentID, payload)
	}
	if sendErr != nil {
		return control.ErrorResponse("send_failed", sendErr.Error())
	}
	agent = a.watcher.GetAgent(agentID)
	if agent == nil {
		return control.Response{OK: true}
	}
	out := controlAgent(agent)
	return control.Response{OK: true, Agent: &out}
}

// submitCodexHandoff is the single control-plane owner for both the initial
// delegated prompt and confirmed Codex follow-ups. The watcher owns the
// paste-once/Enter-once provider transaction; this owner settles the canonical
// Agent projection from that same result and never replays an ambiguous send.
func (a *controlApp) submitCodexHandoff(agentID, command, payload string, initial bool) error {
	handoffStartedAt := time.Now().UTC()
	err := a.watcher.SubmitInputWhenReady(agentID, command, payload)
	if err != nil {
		if initial {
			a.recordSubmissionFailure(agentID, "Initial delegated prompt was not submitted: "+err.Error())
		}
		return err
	}
	a.recordCodexHandoffAccepted(agentID, handoffStartedAt, initial)
	return nil
}

func (a *controlApp) recordCodexHandoffAccepted(agentID string, handoffStartedAt time.Time, initial bool) {
	if a == nil || a.watcher == nil {
		return
	}
	phase := "working"
	summary := "Delegated Codex input accepted"
	if initial {
		phase = "starting"
		summary = "Initial delegated prompt accepted by Codex"
	}
	_, _ = a.watcher.SettleAgentInputAccepted(agentID, handoffStartedAt, phase, summary)
}

func (a *controlApp) recordSubmissionFailure(agentID, summary string) {
	if a == nil || a.watcher == nil {
		return
	}
	_, _ = a.watcher.UpdateAgentProgress(agentID, classifier.AgentProgress{
		Status:    "failed",
		Phase:     "starting",
		Attention: "failed",
		Summary:   summary,
		TaskClass: "lasting_design",
		EventKind: "risk",
	})
}

func (a *controlApp) handleAgentCapture(req control.Request) control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Agent watcher is not running.")
	}
	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		return control.ErrorResponse("missing_agent_id", "Agent id is required.")
	}
	if agent := a.watcher.GetAgent(agentID); agent != nil && !a.watcher.HasSession(agentID) {
		return control.ErrorResponse("agent_session_unavailable", "Agent is listed but the tmux target is no longer available. Refresh the agent list and spawn a new session if needed.")
	}
	text, err := a.watcher.CapturePaneContent(agentID)
	if err != nil {
		return control.ErrorResponse("capture_failed", err.Error())
	}
	text = work.CleanCodexDisplayText(text)
	agent := a.watcher.GetAgent(agentID)
	if agent == nil {
		return control.Response{OK: true, Text: text}
	}
	out := controlAgent(agent)
	return control.Response{OK: true, Text: text, Agent: &out}
}

func (a *controlApp) handleAgentStatus(req control.Request) control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Agent watcher is not running.")
	}
	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		return control.ErrorResponse("missing_agent_id", "Agent id is required.")
	}
	agent := a.watcher.GetAgent(agentID)
	if agent == nil {
		return control.ErrorResponse("agent_not_found", "Agent session was not found.")
	}
	out := controlAgent(agent)
	return control.Response{OK: true, Agent: &out}
}

func (a *controlApp) handleAgentProgress(req control.Request) control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Agent watcher is not running.")
	}
	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		return control.ErrorResponse("missing_agent_id", "Agent id is required.")
	}
	if agent := a.watcher.GetAgent(agentID); agent == nil {
		return control.ErrorResponse("agent_not_found", "Agent session was not found.")
	}
	progress, err := classifier.ValidateProgress(classifier.AgentProgress{
		Status:       req.Status,
		Phase:        req.Phase,
		Attention:    req.Attention,
		Summary:      req.Summary,
		TaskClass:    req.TaskClass,
		EventKind:    req.EventKind,
		DetailsJSON:  req.DetailsJSON,
		LeaseSeconds: req.LeaseSeconds,
	})
	if err != nil {
		return control.ErrorResponse("invalid_progress", err.Error())
	}
	agent, err := a.watcher.UpdateAgentProgress(agentID, progress)
	if err != nil {
		return control.ErrorResponse("progress_failed", err.Error())
	}
	out := controlAgent(agent)
	return control.Response{OK: true, Agent: &out}
}

func (a *controlApp) handleAgentClose(req control.Request) control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Agent watcher is not running.")
	}
	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		return control.ErrorResponse("missing_agent_id", "Agent id is required.")
	}
	agent := a.watcher.GetAgent(agentID)
	if agent != nil && !agent.Delegated && !agent.Hidden && !req.Force {
		return control.ErrorResponse("agent_not_delegated", "Refusing to close a session that was not created as a Brain delegated agent. Use --force only when you intentionally want to close this external session.")
	}
	if agent != nil && !req.Force && closeRequiresForce(agent) {
		return control.ErrorResponse("agent_running_requires_force", "Agent is still running or unresolved. Send it a cancellation request first, wait for done/failed/blocked, or close with force.")
	}
	if err := a.watcher.KillSession(agentID); err != nil {
		return control.ErrorResponse("close_failed", err.Error())
	}
	if agent == nil {
		return control.Response{OK: true}
	}
	out := controlAgent(agent)
	out.Status = string(classifier.StateRemoved)
	return control.Response{OK: true, Agent: &out}
}

func (a *controlApp) handleBrainExecutors() control.Response {
	executor, delegatedExecutor, executors, resp := a.brainExecutorSnapshot()
	if !resp.OK || resp.Error != nil {
		return resp
	}
	return control.Response{
		OK:                true,
		Executor:          executor,
		DelegatedExecutor: delegatedExecutor,
		Executors:         executors,
	}
}

func (a *controlApp) handleBrainContext() control.Response {
	if a == nil || a.brainStore == nil {
		return control.ErrorResponse("brain_unavailable", "Brain workspace is not configured.")
	}
	service := brain.NewService(a.brainStore, a.watcher, a.execs)
	context, err := service.Context()
	if err != nil {
		return control.ErrorResponse("brain_context_failed", err.Error())
	}
	return control.Response{
		OK:      true,
		Context: context,
	}
}

func (a *controlApp) handleBrainPlaybooks() control.Response {
	if a == nil || a.brainStore == nil {
		return control.ErrorResponse("brain_unavailable", "Brain workspace is not configured.")
	}
	service := brain.NewService(a.brainStore, a.watcher, a.execs)
	catalog, err := service.PlaybookCatalog()
	if err != nil {
		return control.ErrorResponse("brain_playbooks_failed", err.Error())
	}
	return control.Response{
		OK:        true,
		Playbooks: catalog,
	}
}

func (a *controlApp) handleBrainGC() control.Response {
	if a == nil || a.brainStore == nil {
		return control.ErrorResponse("brain_unavailable", "Brain workspace is not configured.")
	}
	service := brain.NewService(a.brainStore, a.watcher, a.execs)
	report, err := service.Housekeeping()
	if err != nil {
		return control.ErrorResponse("brain_gc_failed", err.Error())
	}
	return control.Response{
		OK:           true,
		Housekeeping: report,
	}
}

func (a *controlApp) handleBrainSetExecutor(req control.Request) control.Response {
	if a == nil || a.brainStore == nil {
		return control.ErrorResponse("brain_unavailable", "Brain workspace is not configured.")
	}
	executorID := strings.TrimSpace(req.ExecutorID)
	if executorID == "" {
		return control.ErrorResponse("missing_executor", "Brain host executor is required.")
	}
	if locked := brainHostExecutorOverride(); locked != "" && locked != executorID {
		return control.ErrorResponse("brain_executor_locked_by_env", "A Brain host executor environment override is set; unset it before changing the host executor through zen.")
	}
	if a.execs == nil {
		return control.ErrorResponse("executors_unavailable", "Executor config is not available.")
	}
	executor, ok := a.execs.AgentExecutor(executorID)
	if !ok {
		return control.ErrorResponse("invalid_executor", fmt.Sprintf("Brain host executor %q is not configured.", executorID))
	}
	if a.watcher != nil {
		service := brain.NewService(a.brainStore, a.watcher, a.execs)
		if _, err := service.SetHostExecutor(executor.ID); err != nil {
			return control.ErrorResponse("set_executor_failed", err.Error())
		}
	} else {
		if err := a.brainStore.SetHostExecutorID(executor.ID); err != nil {
			return control.ErrorResponse("set_executor_failed", err.Error())
		}
	}
	return a.handleBrainExecutors()
}

// handleSetDelegatedExecutor switches the live Delegated Executor on the shared
// ExecutorConfig owner. Existing sessions are not migrated.
func (a *controlApp) handleSetDelegatedExecutor(req control.Request) control.Response {
	if a == nil || a.execs == nil {
		return control.ErrorResponse("executors_unavailable", "Executor config is not available.")
	}
	executorID := strings.TrimSpace(req.ExecutorID)
	if executorID == "" {
		return control.ErrorResponse("missing_executor", "Delegated executor id is required.")
	}
	if err := a.execs.SetDelegatedExecutor(executorID); err != nil {
		if errors.Is(err, work.ErrUnknownExecutor) {
			return control.ErrorResponse("invalid_executor", err.Error())
		}
		if errors.Is(err, work.ErrDelegatedExecutorLocked) {
			return control.ErrorResponse("delegated_executor_locked_by_env", err.Error())
		}
		return control.ErrorResponse("set_delegated_executor_failed", err.Error())
	}
	return a.handleBrainExecutors()
}

func (a *controlApp) brainExecutorSnapshot() (*control.Executor, *control.Executor, []control.Executor, control.Response) {
	if a == nil || a.brainStore == nil {
		return nil, nil, nil, control.ErrorResponse("brain_unavailable", "Brain workspace is not configured.")
	}
	if a.execs == nil {
		return nil, nil, nil, control.ErrorResponse("executors_unavailable", "Executor config is not available.")
	}
	current, ok := a.currentBrainExecutor()
	if !ok {
		return nil, nil, nil, control.ErrorResponse("executor_unavailable", "No Brain host executors are configured.")
	}
	delegated, ok := a.brainDelegatedExecutor()
	if !ok {
		return nil, nil, nil, control.ErrorResponse("executor_unavailable", "No delegated executors are configured.")
	}
	executors := a.execs.AgentExecutors()
	out := make([]control.Executor, 0, len(executors))
	for _, executor := range executors {
		executor.Host = executor.ID == current.ID
		executor.Delegated = executor.ID == delegated.ID
		out = append(out, controlExecutor(executor))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Host != out[j].Host {
			return out[i].Host
		}
		if out[i].Delegated != out[j].Delegated {
			return out[i].Delegated
		}
		return out[i].ID < out[j].ID
	})
	current.Host = true
	if current.ID == delegated.ID {
		current.Delegated = true
	}
	delegated.Delegated = true
	if delegated.ID == current.ID {
		delegated.Host = true
	}
	converted := controlExecutor(current)
	convertedDelegated := controlExecutor(delegated)
	return &converted, &convertedDelegated, out, control.Response{OK: true}
}

func (a *controlApp) currentBrainExecutor() (work.AgentExecutor, bool) {
	if a == nil || a.execs == nil {
		return work.AgentExecutor{}, false
	}
	if preferred := brainHostExecutorOverride(); preferred != "" {
		return a.execs.AgentExecutor(preferred)
	}
	if a.brainStore != nil {
		if hostSession, err := a.brainStore.HostSession(); err == nil {
			if executorID := strings.TrimSpace(hostSession.ExecutorID); executorID != "" {
				return a.execs.AgentExecutor(executorID)
			}
		}
	}
	return a.execs.AgentExecutor("codex")
}

func (a *controlApp) resolveSpawnCommand(req control.Request) (string, error) {
	if command := strings.TrimSpace(req.Command); command != "" {
		// Explicit full-command overrides are user-authored; do not mutate
		// their authorization/sandbox configuration.
		return command, nil
	}
	executorName := strings.TrimSpace(req.Executor)
	if executorName == "" {
		if delegatedExecutor, ok := a.brainCallerDelegatedExecutor(req.AgentID); ok {
			executorName = delegatedExecutor
		}
	}
	if executorName == "" && a != nil && a.execs != nil {
		if delegatedExecutor, ok := a.execs.DelegatedAgentExecutor(); ok {
			executorName = delegatedExecutor.ID
		}
	}
	if executorName == "" {
		executorName = "codex"
	}
	if a != nil && a.execs != nil {
		executor, ok := a.execs.ByName[executorName]
		if !ok {
			return "", fmt.Errorf("executor %q is not configured", executorName)
		}
		command := strings.TrimSpace(executor.Command)
		if command == "" {
			command = executorName
		}
		provider := work.InferAgentProvider(executor.Kind, command, executorName, executor.Name)
		if provider == work.AgentProviderCodex {
			// Brain-delegated Codex sessions must run non-interactively with
			// the most permissive available authorization mode so internal
			// progress commands do not block on approval prompts.
			command = work.HardenCodexDelegatedCommand(command)
		} else if provider == work.AgentProviderClaude {
			// Brain-delegated Claude sessions must run non-interactively with
			// the most permissive authorization mode so internal progress
			// commands do not block on approval prompts.
			command = work.HardenClaudeCommand(command)
		}
		return command, nil
	}
	provider := work.InferAgentProvider(executorName)
	if provider == work.AgentProviderCodex {
		return work.HardenCodexDelegatedCommand(executorName), nil
	} else if provider == work.AgentProviderClaude {
		return work.HardenClaudeCommand(executorName), nil
	}
	return executorName, nil
}

func (a *controlApp) brainCallerDelegatedExecutor(agentID string) (string, bool) {
	if a == nil || a.brainStore == nil || a.execs == nil {
		return "", false
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return "", false
	}
	host, err := a.brainStore.HostSession()
	if err != nil || strings.TrimSpace(host.ID) == "" || strings.TrimSpace(host.ID) != agentID {
		return "", false
	}
	if delegatedExecutor, ok := a.brainDelegatedExecutor(); ok {
		return delegatedExecutor.ID, true
	}
	return "", false
}

func (a *controlApp) brainDelegatedExecutor() (work.AgentExecutor, bool) {
	if a == nil || a.execs == nil {
		return work.AgentExecutor{}, false
	}
	// Effective delegated selection (including startup env lock) lives only on
	// the shared ExecutorConfig owner — no parallel env readers here.
	return a.execs.DelegatedAgentExecutor()
}

func brainHostExecutorOverride() string {
	return strings.TrimSpace(os.Getenv("ZEN_BRAIN_HOST_EXECUTOR"))
}

func visibleControlAgents(agents []*classifier.Agent) []control.Agent {
	out := make([]control.Agent, 0, len(agents))
	for _, agent := range agents {
		if agent == nil || agent.Hidden {
			continue
		}
		out = append(out, controlAgent(agent))
	}
	return out
}

func controlAgent(agent *classifier.Agent) control.Agent {
	if agent == nil {
		return control.Agent{}
	}
	return control.Agent{
		ID:                  agent.ID,
		Name:                agent.Name,
		Status:              string(agent.State),
		Summary:             agent.Summary,
		Phase:               agent.Phase,
		Attention:           agent.Attention,
		TaskClass:           agent.TaskClass,
		EventKind:           agent.EventKind,
		DetailsJSON:         agent.DetailsJSON,
		NeedsAttention:      agent.NeedsAttention,
		LastProgressAt:      agent.LastProgressAt,
		ExpectedNextCheckAt: agent.ExpectedNextCheckAt,
		LeaseSeconds:        agent.LeaseSeconds,
		Cwd:                 agent.Cwd,
		Command:             agent.Command,
		UpdatedAt:           agent.UpdatedAt,
		Hidden:              agent.Hidden,
		Delegated:           agent.Delegated,
	}
}

func controlExecutor(executor work.AgentExecutor) control.Executor {
	return control.Executor{
		ID:       executor.ID,
		Name:     executor.Name,
		Provider: executor.Provider,
		Command:  executor.Command,
		Runtime:  executor.Runtime,
		Capabilities: control.ExecutorCapabilities{
			InteractiveTTY:   executor.Capabilities.InteractiveTTY,
			StructuredEvents: executor.Capabilities.StructuredEvents,
		},
		Host:      executor.Host,
		Delegated: executor.Delegated,
	}
}

func spawnPrompt(req control.Request) (string, error) {
	prompt := strings.TrimSpace(req.Prompt)
	promptFile := strings.TrimSpace(req.PromptFile)
	if promptFile != "" {
		raw, err := os.ReadFile(promptFile)
		if err != nil {
			return "", fmt.Errorf("read prompt file: %w", err)
		}
		filePrompt := strings.TrimSpace(string(raw))
		switch {
		case prompt == "":
			prompt = filePrompt
		case filePrompt != "":
			prompt = strings.TrimSpace(prompt + "\n\n" + filePrompt)
		}
	}
	protocol := lifecycleProtocol(req.Profile)
	if prompt == "" {
		return protocol, nil
	}
	return strings.TrimSpace(prompt + "\n\n" + protocol), nil
}

func progressEnvForStateDir(stateDir string) map[string]string {
	env := map[string]string{
		"ZEN_AGENT_PROGRESS_CMD": watcher.ZenExecutablePath(),
	}
	if worktreeRoot, err := work.DefaultWorktreeRoot(); err == nil {
		env["ZEN_WORKTREE_ROOT"] = worktreeRoot
	}
	if stateDir = strings.TrimSpace(stateDir); stateDir != "" {
		env["ZEN_STATE_DIR"] = stateDir
	}
	return env
}

func closeRequiresForce(agent *classifier.Agent) bool {
	if agent == nil || !agent.Delegated || agent.Hidden {
		return false
	}
	switch agent.State {
	case classifier.StateDone, classifier.StateFailed, classifier.StateBlocked:
		return false
	default:
		return true
	}
}

func lifecycleProtocol(profile string) string {
	profile = normalizeAgentProfile(profile)
	return strings.TrimSpace(fmt.Sprintf(`Zen lifecycle protocol:
- Profile: %s.
- Treat the prompt as a loop contract: preserve the objective, acceptance criteria, safety constraints, verification, and expected report.
- Start lasting design or implementation work by identifying the core invariants. Prefer making invalid states unrepresentable over adding fallback paths.
- Work in the supplied cwd by default and preserve unrelated existing changes. Delegation alone is not a reason to create a git worktree.
- Create a worktree only when the brief genuinely requires concurrent-write isolation. Reuse it for the larger task and place it under $ZEN_WORKTREE_ROOT; never use OS temporary or memory-backed storage for worktrees, repository copies, or large build roots.
- This session owns its descendants and private temporary directory. Reuse heavyweight persistent resources named in the brief instead of duplicating or detaching them.
- Respect Zen's resource boundary. If a resource limit blocks the task, report the limit and required next action through progress instead of escaping the owned lifecycle.
- Use TMPDIR/TMP/TEMP for Agent-owned scratch and audit state, and $ZEN_BUILD_TMPDIR for large disposable builds when supported. Never hard-code OS-global temp paths; bounded tool-internal temp is allowed. Remove owned artifacts before reporting done. Stop any persistent child that the task no longer needs; lifecycle teardown remains the final safety net.
- Use task classes consistently: exploration for research/scanning, mechanical_change for bounded repeatable edits, lasting_design for product semantics, data models, architecture, and long-lived code.
- Report progress through the Zen control plane only when your phase changes, when you take a meaningful long-running step, when you need attention, and when you finish.
- ZEN_AGENT_ID is already set for this session. ZEN_AGENT_PROGRESS_CMD is the absolute path to the currently running zen daemon executable (a single token, no spaces; may be named zen, zen-dev, or similar).
- Always invoke it as a quoted single token followed by the "agent progress" subcommand. Do not rely on shell word splitting of the variable.
- Command shape:
  "$ZEN_AGENT_PROGRESS_CMD" agent progress --status running --phase working --attention none --summary "Short current work" --lease 300
- Semantic event shape:
  "$ZEN_AGENT_PROGRESS_CMD" agent progress --status running --phase planning --attention none --task-class lasting_design --event-kind invariant --summary "Defined durable state invariants" --details-json '{"invariants":["canonical source is X"]}' --lease 300
- Valid status values: running, done, failed, blocked.
- Valid phase values: starting, reading, planning, working, verifying, reporting.
- Valid attention values: none, done, blocked, failed, user_input, stale.
- Valid task classes: exploration, mechanical_change, lasting_design.
- Valid event kinds: progress, invariant, artifact, risk, needs_judgment, verification, done.
- Use attention "none" while you are making normal progress.
- Use attention "user_input" only when user input is required.
- Use event-kind "needs_judgment" when the next step depends on product values, user risk tolerance, or a choice between root design and patching.
- Use attention "done" with status "done" only after the requested work and feasible verification are complete.
- For implementation work, several minutes of reading before file edits is normal.`, profile))
}

func normalizeAgentProfile(profile string) string {
	switch strings.TrimSpace(profile) {
	case "quick", "research", "implementation", "long_running":
		return strings.TrimSpace(profile)
	default:
		return "implementation"
	}
}

func defaultAgentName(executor, command string) string {
	if executor := strings.TrimSpace(executor); executor != "" {
		return executor
	}
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return "Agent"
	}
	return fields[0]
}

func ensureTrailingNewline(value string) string {
	if strings.HasSuffix(value, "\n") || strings.HasSuffix(value, "\r") {
		return value
	}
	return value + "\n"
}
