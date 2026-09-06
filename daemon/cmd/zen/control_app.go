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
	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/modelprofiles"
	telegramchannel "github.com/daoleno/zen/daemon/telegram"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
	"github.com/google/uuid"
)

type controlWatcher interface {
	Workers() []*classifier.Worker
	GetWorker(id string) *classifier.Worker
	HasSession(target string) bool
	ProbeSession(target string) (watcher.SessionPresence, error)
	CreateSession(preferredTarget string, opts watcher.CreateSessionOptions) (string, error)
	UpdateWorkerProgress(id string, progress classifier.WorkerProgress) (*classifier.Worker, error)
	RebindDelegatedTurnProjection(id string) (*classifier.Worker, error)
	SendInput(sessionID, text string) error
	SendInputWithReceiptResult(sessionID, text, receipt string) (watcher.InputResult, error)
	SendInputWithReceiptWhenReadyResult(sessionID, command, payload string, receiptFor watcher.InputReceiptForGeneration) (watcher.InputResult, watcher.OwnedGeneration, error)
	InputReceiptResult(sessionID, receipt string) (watcher.InputResult, bool, error)
	SendInputWhenReady(sessionID, command, text string) error
	SubmitInput(sessionID, payload string) error
	SubmitInputWhenReady(sessionID, command, payload string) error
	SubmitDelegatedInput(sessionID, payload, turnID string, acceptedAt time.Time) (watcher.InputResult, error)
	SubmitDelegatedInputWhenReady(sessionID, command, payload, workID, turnID string, acceptedAt time.Time) (watcher.InputResult, error)
	SubmitDelegatedInputWhenReadyBudgeted(sessionID, command, payload, workID, turnID string, acceptedAt time.Time, budget time.Duration) (watcher.InputResult, error)
	SubmitDelegatedWorkInput(sessionID, payload, workID, turnID, purpose, purposeID string, acceptedAt time.Time) (watcher.InputResult, error)
	SubmitBrainHostInput(sessionID, payload, claimToken, workID, providerTurnID string, acceptedAt time.Time) (watcher.InputResult, error)
	KillSession(sessionID string) error
	CapturePaneContent(sessionID string) (string, error)
	ProbeProviderEvidence(sessionID string) (watcher.ProviderActivityObservation, bool, error)
	ResolveOwnedGeneration(sessionID string) (watcher.OwnedGeneration, error)
	ResolveBrainHostGeneration(sessionID string) (watcher.OwnedGeneration, error)
	ResolveDelegatedControl(sessionID string) (watcher.OwnedGeneration, error)
}

type controlApp struct {
	auth              *auth.Manager
	watcher           controlWatcher
	execs             *work.ExecutorConfig
	brainStore        *brain.Store
	brainService      *brain.Service
	calendarStore     *calendar.Store
	calendarScheduler *calendar.Scheduler
	profiles          *modelprofiles.Owner
	telegram          telegramControlManager
	threadRuntimeSet  func(string, modelprofiles.ThreadRuntimeChoice) (modelprofiles.WireSessionSnapshot, modelprofiles.PersistResult, error)
	stateDir          string
}

type telegramControlManager interface {
	Configure(context.Context, string) (telegramchannel.Status, error)
	BeginBinding() (telegramchannel.BindingChallenge, error)
}

const delegatedInitialReadinessBudget = 45 * time.Second

func (a *controlApp) HandleControlRequest(req control.Request) control.Response {
	switch strings.TrimSpace(req.Type) {
	case "worker_list":
		return a.handleWorkerList()
	case "worker_spawn":
		return a.handleWorkerSpawn(req)
	case "worker_send":
		return a.handleWorkerSend(req)
	case "worker_capture":
		return a.handleWorkerCapture(req)
	case "worker_status":
		return a.handleWorkerStatus(req)
	case "worker_receipt":
		return a.handleWorkerReceipt(req)
	case "worker_progress":
		return a.handleWorkerProgress(req)
	case "worker_close":
		return a.handleWorkerClose(req)
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
	case "brain_work_close":
		return a.handleBrainWorkClose(req)
	case "brain_work_event":
		return a.handleBrainWorkEvent(req)
	case "brain_work_event_resolve":
		return a.handleBrainWorkEventResolve(req)
	case "brain_work_resolve":
		return a.handleBrainWorkResolve(req)
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
	case "telegram_setup":
		return a.handleTelegramSetup(req)
	case "provider_list":
		return a.handleProviderList()
	case "provider_upsert":
		return a.handleProviderUpsert(req)
	case "provider_delete":
		return a.handleProviderDelete(req)
	case "provider_set_default":
		return a.handleProviderSetDefault(req)
	case "provider_switch":
		return a.handleProviderSwitch(req)
	case "codex_gateway_status":
		return a.handleCodexGatewayStatus()
	case "codex_gateway_enable":
		return a.handleCodexGatewayEnable()
	case "codex_gateway_disable":
		return a.handleCodexGatewayDisable()
	case "codex_gateway_restore_backup":
		return a.handleCodexGatewayRestoreBackup()
	case "provider_set_models":
		return a.handleProviderSetModels(req)
	case "provider_discover":
		return a.handleProviderDiscover(req)
	case "thread_runtime_get":
		return a.handleThreadRuntimeGet(req)
	case "thread_runtime_set":
		return a.handleThreadRuntimeSet(req)
	case "model_profile_list", "model_profile_get", "model_profile_upsert",
		"model_profile_delete", "model_profile_set_default",
		"session_route_get", "session_route_activate":
		return control.ErrorResponse(modelprofiles.CodeProfileInvalid, "profile wire removed; use provider connection APIs")
	default:
		return control.ErrorResponse("unknown_request", fmt.Sprintf("Unknown control request: %s", req.Type))
	}
}

func (a *controlApp) handleTelegramSetup(req control.Request) control.Response {
	if a == nil || a.telegram == nil {
		return control.ErrorResponse("telegram_unavailable", "Telegram is not configured.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	credential := req.Credential
	req.Credential = ""
	status, err := a.telegram.Configure(ctx, credential)
	credential = ""
	cancel()
	if err != nil {
		return control.ErrorResponse("telegram_configure_failed", err.Error())
	}
	binding, err := a.telegram.BeginBinding()
	if err != nil {
		return control.ErrorResponse("telegram_bind_failed", err.Error())
	}
	return control.Response{
		OK:              true,
		TelegramStatus:  &status,
		TelegramBinding: &binding,
	}
}

func (a *controlApp) handleBrainWorkResolve(req control.Request) control.Response {
	if a == nil || a.brainService == nil || req.BrainWorkDisposition == nil {
		return control.ErrorResponse("brain_unavailable", "Brain Work disposition is not configured.")
	}
	event, item, err := a.brainService.ResolveWorkReview(*req.BrainWorkDisposition)
	if err != nil {
		return brainWorkControlError(err)
	}
	return control.Response{OK: true, BrainWork: &item, BrainWorkEvent: &event}
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

func (a *controlApp) handleWorkerList() control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Worker watcher is not running.")
	}
	for _, worker := range a.watcher.Workers() {
		if worker != nil && !worker.Hidden {
			_, _ = a.resolveOwnedWorker(worker.ID)
		}
	}
	workers := visibleControlWorkers(a.watcher.Workers())
	sort.Slice(workers, func(i, j int) bool {
		return workers[i].UpdatedAt.After(workers[j].UpdatedAt)
	})
	return control.Response{OK: true, Workers: workers}
}

func (a *controlApp) handleWorkerSpawn(req control.Request) control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Worker watcher is not running.")
	}

	command, err := a.resolveSpawnCommand(req)
	if err != nil {
		return control.ErrorResponse("invalid_executor", err.Error())
	}

	cwd := strings.TrimSpace(req.Cwd)
	if cwd == "" {
		return control.ErrorResponse("missing_cwd", "Worker spawn requires an explicit working directory.")
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = defaultWorkerName(req.Executor, command)
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
	autoCreatedWork := ownedWork.ID != "" && strings.TrimSpace(req.WorkID) == ""

	createOpts := watcher.CreateSessionOptions{
		Cwd:         cwd,
		Command:     command,
		Name:        name,
		Detached:    true,
		Hidden:      req.Hidden,
		ProgressEnv: true,
		Delegated:   !req.Hidden,
		Env:         progressEnvForStateDir(a.stateDir),
	}
	var routeSnap *modelprofiles.WireSessionSnapshot
	var routePersist modelprofiles.PersistResult
	workerID := ""
	connectionID := strings.TrimSpace(req.ConnectionID)
	if connectionID == "" {
		connectionID = strings.TrimSpace(req.ProfileID)
	}
	if a.profiles != nil {
		plan, planErr := a.profiles.PrepareLaunchModel(a.spawnProfileClientHint(req, command), connectionID, strings.TrimSpace(req.ModelID), command)
		if planErr != nil && !plan.Persist.Applied && !plan.Bypass {
			a.recordSpawnWorkFailure(ownedWork, planErr, autoCreatedWork)
			return control.ErrorResponse(modelprofiles.ControlErrorCode(planErr), planErr.Error())
		}
		if plan.Applied && !plan.Bypass {
			createOpts.Command = plan.Command
			createOpts.Env = mergeControlEnv(createOpts.Env, plan.Env)
			workerID, err = a.watcher.CreateSession("", createOpts)
			if err != nil {
				abortPersist, abortErr := a.profiles.AbortLaunch(plan.ProvisionalID)
				a.recordSpawnWorkFailure(ownedWork, err, autoCreatedWork)
				joined := errors.Join(err, abortErr)
				if abortErr != nil || !abortPersist.Applied {
					return control.ErrorResponse(modelprofiles.ControlErrorCode(errors.Join(joined, modelprofiles.ErrLaunchCleanupIncomplete)), joined.Error())
				}
				return control.ErrorResponse("spawn_failed", joined.Error())
			}
			_, snap, persist, commitErr := a.profiles.CommitLaunch(plan.ProvisionalID, workerID)
			if !persist.Applied {
				cleanup := modelprofiles.CleanupFailedLaunch(a.profiles, plan.ProvisionalID, workerID, a.watcher.KillSession, a.sessionLivenessProbe)
				a.recordSpawnWorkFailure(ownedWork, commitErr, autoCreatedWork)
				joined := errors.Join(commitErr, cleanup.Err)
				code := modelprofiles.ControlErrorCode(joined)
				if code == "" || code == modelprofiles.CodeProfilesUnavailable {
					code = modelprofiles.ControlErrorCode(commitErr)
				}
				if code == "" {
					code = "spawn_failed"
				}
				return control.ErrorResponse(code, joined.Error())
			}
			routePersist = modelprofiles.CombinePersistResults(plan.Persist, persist)
			routeSnap = &snap
			if commitErr != nil || !routePersist.Durable {
				if commitErr == nil {
					commitErr = modelprofiles.ErrPersistDirSync
				}
				log.Printf("model profile launch applied with uncertain durability: %v", commitErr)
			}
		}
	}
	if workerID == "" {
		workerID, err = a.watcher.CreateSession("", createOpts)
		if err != nil {
			a.recordSpawnWorkFailure(ownedWork, err, autoCreatedWork)
			return control.ErrorResponse("spawn_failed", err.Error())
		}
	}
	admissionPending := false
	if prompt != "" {
		_, sendErr := a.submitWorkerHandoff(workerID, createOpts.Command, prompt, ownedWork.ID, true)
		if sendErr != nil && !a.keepSpawnAdmissionPending(workerID, sendErr) {
			if errors.Is(sendErr, brain.ErrWorkAttemptConflict) {
				var cleanup modelprofiles.LaunchCleanupResult
				if a.profiles != nil && routeSnap != nil {
					cleanup = modelprofiles.CleanupFailedLaunch(a.profiles, "", workerID, a.watcher.KillSession, a.sessionLivenessProbe)
				} else {
					cleanup.Err = a.watcher.KillSession(workerID)
				}
				joined := errors.Join(sendErr, cleanup.Err)
				if cleanup.Err != nil || (routeSnap != nil && !cleanup.Persist.Applied) {
					return control.ErrorResponse("spawn_failed", joined.Error())
				}
				return brainWorkControlError(sendErr)
			}
			if watcher.InputOutcomeFromError(sendErr) == watcher.InputNotSubmitted && ownedWork.ID != "" {
				var cleanupErr error
				if a.profiles != nil && routeSnap != nil {
					cleanup := modelprofiles.CleanupFailedLaunch(a.profiles, "", workerID, a.watcher.KillSession, a.sessionLivenessProbe)
					cleanupErr = cleanup.Err
					if cleanupErr == nil && !cleanup.Persist.Applied {
						cleanupErr = modelprofiles.ErrLaunchCleanupIncomplete
					}
				} else {
					cleanupErr = a.watcher.KillSession(workerID)
				}
				a.recordSpawnWorkFailure(ownedWork, sendErr, strings.TrimSpace(req.WorkID) == "" && cleanupErr == nil)
				return control.ErrorResponse(
					"send_prompt_failed",
					errors.Join(sendErr, cleanupErr).Error(),
				)
			}
			a.recordSpawnWorkFailure(ownedWork, sendErr, autoCreatedWork)
			return control.ErrorResponse("send_prompt_failed", sendErr.Error())
		}
		if sendErr != nil {
			admissionPending = true
			log.Printf("delegated Session %s remains pending after ambiguous initial submission: %v", workerID, sendErr)
		}
		if ownedWork.ID != "" {
			ownedWork, err = a.brainStore.Work(ownedWork.ID)
			if err != nil {
				return brainWorkControlError(err)
			}
		}
	}

	worker := a.watcher.GetWorker(workerID)
	if worker == nil {
		response := control.Response{
			OK:           true,
			Confirmation: spawnAdmissionConfirmation(admissionPending),
			Worker: &control.Worker{
				ID:        workerID,
				Name:      name,
				Status:    string(classifier.StateRunning),
				Cwd:       cwd,
				Command:   createOpts.Command,
				Hidden:    req.Hidden,
				Delegated: !req.Hidden,
			},
			SessionRoute: routeSnap,
		}
		if routeSnap != nil {
			if outcome, durable := modelprofiles.WirePersistFields(routePersist); outcome != "" {
				response.PersistenceOutcome = control.PersistenceOutcome(outcome)
				response.PersistenceDurable = durable
			}
		}
		if ownedWork.ID != "" {
			response.BrainWork = &ownedWork
		}
		return response
	}
	out := controlWorker(worker)
	response := control.Response{
		OK:           true,
		Worker:       &out,
		SessionRoute: routeSnap,
		Confirmation: spawnAdmissionConfirmation(admissionPending),
	}
	if routeSnap != nil {
		if outcome, durable := modelprofiles.WirePersistFields(routePersist); outcome != "" {
			response.PersistenceOutcome = control.PersistenceOutcome(outcome)
			response.PersistenceDurable = durable
		}
	}
	if ownedWork.ID != "" {
		response.BrainWork = &ownedWork
	}
	return response
}

func spawnAdmissionConfirmation(pending bool) string {
	if !pending {
		return ""
	}
	return "Delegated Session created; prompt submission is awaiting exact turn-scoped admission."
}

// keepSpawnAdmissionPending owns only the initial spawn response boundary.
// Once the non-replayable input owner reports an ambiguous mutation, a live
// Zen-owned tmux Session means the canonical pending submission is still the
// truthful result. Exact turn-scoped progress or the existing provider/liveness
// reconciliation paths will settle that transaction; this owner must neither
// report a definitive launch failure nor replay the prompt. Proved absence and
// probe failure retain the existing failure path.
func (a *controlApp) keepSpawnAdmissionPending(workerID string, sendErr error) bool {
	if a == nil || a.watcher == nil || watcher.InputOutcomeFromError(sendErr) != watcher.InputAmbiguous {
		return false
	}
	presence, err := a.watcher.ProbeSession(workerID)
	if err != nil {
		log.Printf("delegated Session %s liveness probe failed after ambiguous initial submission: %v", workerID, err)
		return false
	}
	return presence == watcher.SessionPresencePresent
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
		inFlight, inFlightErr := a.brainStore.WorkHasDeliveredReview(item.ID)
		if inFlightErr != nil {
			return brain.Work{}, inFlightErr
		}
		owned := strings.TrimSpace(item.AttemptSessionID) != ""
		if owned || inFlight {
			if !inFlight || strings.TrimSpace(prompt) == "" {
				return brain.Work{}, fmt.Errorf(
					"%w: Work %s is owned by %s",
					brain.ErrWorkAttemptConflict,
					item.ID,
					item.AttemptSessionID,
				)
			}
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

func (a *controlApp) recordSpawnWorkFailure(item brain.Work, spawnErr error, autoCreated bool) {
	if a == nil || a.brainStore == nil || item.ID == "" {
		return
	}
	next := "Resolve the delegated Session launch failure."
	if watcher.InputOutcomeFromError(spawnErr) == watcher.InputAmbiguous {
		next = "Delivery is unknown; decide whether to reconcile or submit another attempt."
		if _, err := a.brainStore.FSM().OpenReview(lifecycle.WorkID(item.ID), "submission_ambiguous", spawnErr.Error()); err != nil {
			log.Printf("spawn work failure review: %v", err)
			return
		}
		if _, err := a.brainStore.FSM().Amend(lifecycle.WorkID(item.ID), 0, lifecycle.AmendedPayload{NextAction: &next}); err != nil {
			log.Printf("spawn work failure amendment: %v", err)
			return
		}
		if err := a.brainStore.SyncWorkProjection(item.ID); err != nil {
			log.Printf("spawn work failure projection: %v", err)
		}
		return
	}
	if autoCreated {
		status := brain.WorkCancelled
		next = "Delegated launch ended before a usable Session was created."
		if _, err := a.brainStore.UpdateWork(item.ID, brain.WorkUpdate{Status: &status, NextAction: &next}); err != nil {
			log.Printf("spawn work failure update: %v", err)
		}
		return
	}
	if _, err := a.brainStore.FSM().OpenReview(lifecycle.WorkID(item.ID), "submission_failed", spawnErr.Error()); err != nil {
		log.Printf("spawn work failure review: %v", err)
		return
	}
	if _, err := a.brainStore.FSM().Amend(lifecycle.WorkID(item.ID), 0, lifecycle.AmendedPayload{NextAction: &next}); err != nil {
		log.Printf("spawn work failure amendment: %v", err)
		return
	}
	if err := a.brainStore.SyncWorkProjection(item.ID); err != nil {
		log.Printf("spawn work failure projection: %v", err)
	}
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
		case "attempt_session_id":
			update.AttemptSessionID = &source.AttemptSessionID
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

func (a *controlApp) handleBrainWorkClose(req control.Request) control.Response {
	if a == nil || a.brainService == nil {
		return control.ErrorResponse("brain_unavailable", "Brain Work terminalization is not configured.")
	}
	if req.Revision <= 0 {
		return control.ErrorResponse("invalid_brain_work", "A positive expected Work revision is required.")
	}
	item, err := a.brainService.CloseWork(brain.WorkCloseRequest{
		WorkID: strings.TrimSpace(req.WorkID), ExpectedRevision: uint64(req.Revision),
		Status: brain.WorkStatus(strings.TrimSpace(req.Status)),
		Actor:  strings.TrimSpace(req.Actor), Reason: strings.TrimSpace(req.Reason),
	})
	if err != nil {
		return brainWorkControlError(err)
	}
	return control.Response{OK: true, BrainWork: &item, Confirmation: "Brain Work closed under audited operator authority."}
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

// handleBrainWorkEventResolve closes held review leases explicitly and
// actor-recorded (C.2.6, Work-scoped): mark_delivered, discard, or
// user-authorized replay. Automatic or time-based resolution is prohibited.
func (a *controlApp) handleBrainWorkEventResolve(req control.Request) control.Response {
	if a == nil || a.brainStore == nil {
		return control.ErrorResponse("brain_unavailable", "Brain Work is not configured.")
	}
	workID := strings.TrimSpace(req.WorkID)
	action := strings.TrimSpace(req.Operation)
	actor := strings.TrimSpace(req.Actor)
	reason := strings.TrimSpace(req.Reason)
	if workID == "" {
		return control.ErrorResponse("invalid_brain_work_event_resolution", "work_id is required")
	}
	switch action {
	case "mark_delivered":
		if _, _, err := a.brainStore.ResolveReviewLease(workID, brain.ReviewLeaseMarkDelivered, actor, reason); err != nil {
			return brainWorkControlError(err)
		}
		return control.Response{OK: true, Confirmation: "Held review lease marked delivered."}
	case "discard":
		if _, _, err := a.brainStore.ResolveReviewLease(workID, brain.ReviewLeaseDiscard, actor, reason); err != nil {
			return brainWorkControlError(err)
		}
		return control.Response{OK: true, Confirmation: "Held review lease discarded."}
	case "replay":
		_, _, err := a.brainStore.ResolveReviewLease(workID, brain.ReviewLeaseReplay, actor, reason)
		if err != nil {
			return brainWorkControlError(err)
		}
		return control.Response{OK: true, Confirmation: "Review lease replayed under the same audited action identity."}
	default:
		return control.ErrorResponse("invalid_brain_work_event_resolution",
			"resolution action must be mark_delivered, discard, or replay")
	}
}

func brainWorkControlError(err error) control.Response {
	code := "brain_work_failed"
	switch {
	case errors.Is(err, brain.ErrWorkNotFound):
		code = "brain_work_not_found"
	case errors.Is(err, brain.ErrWorkConflict):
		code = "conflict"
	case errors.Is(err, brain.ErrWorkAttemptConflict):
		code = "conflict"
	case errors.Is(err, brain.ErrWorkRevisionConflict):
		code = "brain_work_revision_conflict"
	case errors.Is(err, brain.ErrWorkCloseConflict):
		code = "brain_work_close_conflict"
	case errors.Is(err, brain.ErrEventHandled):
		code = "brain_work_event_handled"
	case errors.Is(err, brain.ErrEventClaim):
		code = "brain_work_event_claim_conflict"
	}
	return control.ErrorResponse(code, err.Error())
}

func (a *controlApp) handleWorkerSend(req control.Request) control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Worker watcher is not running.")
	}
	workerID := strings.TrimSpace(req.WorkerID)
	if workerID == "" {
		return control.ErrorResponse("missing_worker_id", "Worker id is required.")
	}
	worker, ownershipErr := a.resolveOwnedWorker(workerID)
	if ownershipErr != nil {
		return control.ErrorResponse(workerOwnershipErrorCode(ownershipErr), ownershipErr.Error())
	}
	if worker != nil && !worker.Delegated && !worker.Hidden && !req.Force {
		return control.ErrorResponse("worker_not_delegated", "Refusing to send input to a session that was not created as a Brain delegated Zen Worker. Use --force only when you intentionally want to control this external session.")
	}
	payload := req.Text
	if strings.TrimSpace(payload) == "" && !req.Submit {
		return control.ErrorResponse("missing_text", "Text is required.")
	}
	if worker != nil && !a.watcher.HasSession(workerID) {
		return control.ErrorResponse("worker_session_unavailable", "Worker is listed but the tmux target is no longer available. Refresh the worker list and spawn a new session if needed.")
	}
	if strings.TrimSpace(req.WorkID) != "" {
		if !req.Submit || strings.TrimSpace(payload) == "" {
			return control.ErrorResponse("missing_text", "Work input requires submitted prompt text.")
		}
		turnID := "turn:" + uuid.NewString()
		result, err := a.watcher.SubmitDelegatedWorkInput(workerID, delegatedLifecyclePayload(payload, turnID), req.WorkID, turnID, "", "", time.Now().UTC())
		if err != nil {
			return control.ErrorResponse("send_failed", err.Error())
		}
		return control.Response{OK: result.Outcome == watcher.InputAccepted, TurnID: turnID, Confirmation: string(result.Outcome)}
	}
	var sendErr error
	if req.Submit && worker != nil && payload != "" {
		_, sendErr = a.submitWorkerHandoff(workerID, worker.Command, payload, "", false)
	} else {
		if req.Submit {
			payload = ensureTrailingNewline(payload)
		}
		sendErr = a.watcher.SendInput(workerID, payload)
	}
	if sendErr != nil {
		return control.ErrorResponse("send_failed", sendErr.Error())
	}
	worker = a.watcher.GetWorker(workerID)
	if worker == nil {
		return control.Response{OK: true}
	}
	out := controlWorker(worker)
	return control.Response{OK: true, Worker: &out}
}

// submitWorkerHandoff is the single control-plane owner for initial delegated
// prompts and confirmed follow-ups for every interactive provider. The watcher owns the
// paste-once/Enter-once provider transaction and the canonical Admitted
// ledger record (persisted before the submit queue runs); this owner rebinds
// the Session projection from the canonical turn. A new command may retry an
// unknown outcome with a new receipt.
func (a *controlApp) submitWorkerHandoff(workerID, command, payload, workID string, initial bool) (string, error) {
	handoffStartedAt := time.Now().UTC()
	turnID := "turn:" + uuid.NewString()
	// One prompt, one aggregate identity. Provider adapters may implement a
	// follow-up as in-place steering, but that transport detail cannot replace
	// the newly prepared Turn token with a previous prompt's identity.
	payload = delegatedLifecyclePayload(payload, turnID)
	var result watcher.InputResult
	var err error
	if initial {
		result, err = a.watcher.SubmitDelegatedInputWhenReadyBudgeted(
			workerID, command, payload, workID, turnID, handoffStartedAt, delegatedInitialReadinessBudget,
		)
	} else {
		result, err = a.watcher.SubmitDelegatedInput(
			workerID, payload, turnID, handoffStartedAt,
		)
	}
	if err != nil {
		if initial {
			a.recordSubmissionFailure(workerID, err.Error(), watcher.InputOutcomeFromError(err))
		}
		return turnID, err
	}
	if result.Outcome != watcher.InputAccepted {
		return turnID, fmt.Errorf("delegated input was not authoritatively accepted")
	}
	if strings.TrimSpace(result.TurnID) != turnID {
		return turnID, fmt.Errorf(
			"delegated input accepted non-exact turn identity %q (want %q)",
			strings.TrimSpace(result.TurnID), turnID,
		)
	}
	if result.Duplicate {
		return turnID, nil
	}
	// Rebind the Session projection to the canonical turn: a reused Session
	// never inherits the previous turn's done state while its new provider
	// turn is live or admitted (the live OpenCode incident). A rebind failure is
	// non-fatal for the send (the watcher poll re-projects within one poll
	// interval) but must not be swallowed silently.
	if _, rebindErr := a.watcher.RebindDelegatedTurnProjection(workerID); rebindErr != nil {
		log.Printf("delegated Session %s projection rebind failed after accepted input: %v", workerID, rebindErr)
	}
	return turnID, nil
}

// delegatedLifecyclePayload exposes the pending submission's sole random
// identity to the Worker in the prompt that identity owns. The literal flag is
// intentionally turn-scoped rather than process environment: a reusable
// Session receives a different identity with every admitted prompt.
func delegatedLifecyclePayload(payload, turnID string) string {
	contract := fmt.Sprintf(`Zen delegated turn contract:
Use this turn's identity for every progress command; never reuse an earlier turn's identity:
  "$ZEN_WORKER_PROGRESS_CMD" worker progress --turn-id %s --status running --phase working --attention none --summary "Current work" --lease 300`, strings.TrimSpace(turnID))
	return payload + "\n\n" + contract
}

// recordSubmissionFailure rebinds the Session projection from the canonical
// turn after an initial handoff failure. An ambiguous outcome fails closed
// against replay but never falsely terminalizes a still-live provider
// Session: the canonical turn stays Admitted and the projection stays
// running until provider correlation or liveness rules reconcile it (C.6).
func (a *controlApp) recordSubmissionFailure(workerID, summary string, outcome watcher.InputOutcome) {
	if a == nil || a.watcher == nil {
		return
	}
	_ = summary
	_ = outcome
	if _, rebindErr := a.watcher.RebindDelegatedTurnProjection(workerID); rebindErr != nil {
		log.Printf("delegated Session %s projection rebind failed after submission failure: %v", workerID, rebindErr)
	}
}

func (a *controlApp) handleWorkerCapture(req control.Request) control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Worker watcher is not running.")
	}
	workerID := strings.TrimSpace(req.WorkerID)
	if workerID == "" {
		return control.ErrorResponse("missing_worker_id", "Worker id is required.")
	}
	if _, ownershipErr := a.resolveOwnedWorker(workerID); ownershipErr != nil {
		return control.ErrorResponse(workerOwnershipErrorCode(ownershipErr), ownershipErr.Error())
	}
	if worker := a.watcher.GetWorker(workerID); worker != nil && !a.watcher.HasSession(workerID) {
		return control.ErrorResponse("worker_session_unavailable", "Worker is listed but the tmux target is no longer available. Refresh the worker list and spawn a new session if needed.")
	}
	text, err := a.watcher.CapturePaneContent(workerID)
	if err != nil {
		return control.ErrorResponse("capture_failed", err.Error())
	}
	text = work.CleanCodexDisplayText(text)
	worker := a.watcher.GetWorker(workerID)
	if worker == nil {
		return control.Response{OK: true, Text: text}
	}
	out := controlWorker(worker)
	return control.Response{OK: true, Text: text, Worker: &out}
}

func (a *controlApp) handleWorkerStatus(req control.Request) control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Worker watcher is not running.")
	}
	workerID := strings.TrimSpace(req.WorkerID)
	if workerID == "" {
		return control.ErrorResponse("missing_worker_id", "Worker id is required.")
	}
	worker, ownershipErr := a.resolveOwnedWorker(workerID)
	if ownershipErr != nil {
		// ResolveOwnedGeneration has already durably deprojected a live
		// canonical turn before this rejection. Re-read the named recoverable
		// projection so status never reports the cached Running state.
		if projected := a.watcher.GetWorker(workerID); projected != nil {
			out := controlWorker(projected)
			return control.Response{OK: true, Worker: &out, Confirmation: ownershipErr.Error()}
		}
		return control.ErrorResponse(workerOwnershipErrorCode(ownershipErr), ownershipErr.Error())
	}
	if worker == nil {
		return control.ErrorResponse("worker_not_found", "Worker session was not found.")
	}
	out := controlWorker(worker)
	return control.Response{OK: true, Worker: &out}
}

func workerOwnershipErrorCode(err error) string {
	if errors.Is(err, watcher.ErrOwnershipProbeUnavailable) {
		return "worker_control_unavailable"
	}
	return "worker_ownership_lost"
}

func (a *controlApp) resolveOwnedWorker(workerID string) (*classifier.Worker, error) {
	if a == nil || a.watcher == nil {
		return nil, fmt.Errorf("Worker watcher is not running")
	}
	worker := a.watcher.GetWorker(workerID)
	if worker == nil {
		return nil, nil
	}
	if _, err := a.watcher.ResolveDelegatedControl(workerID); err != nil {
		return a.watcher.GetWorker(workerID), err
	}
	return a.watcher.GetWorker(workerID), nil
}

func (a *controlApp) handleWorkerProgress(req control.Request) control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Worker watcher is not running.")
	}
	workerID := strings.TrimSpace(req.WorkerID)
	if workerID == "" {
		return control.ErrorResponse("missing_worker_id", "Worker id is required.")
	}
	if worker := a.watcher.GetWorker(workerID); worker == nil {
		return control.ErrorResponse("worker_not_found", "Worker session was not found.")
	}
	progress, err := classifier.ValidateProgress(classifier.WorkerProgress{
		TurnID:          req.TurnID,
		Status:          req.Status,
		Phase:           req.Phase,
		Attention:       req.Attention,
		Summary:         req.Summary,
		TaskClass:       req.TaskClass,
		EventKind:       req.EventKind,
		DetailsJSON:     req.DetailsJSON,
		LeaseSeconds:    req.LeaseSeconds,
		ProgressEventID: req.ProgressEventID,
	})
	if err != nil {
		return control.ErrorResponse("invalid_progress", err.Error())
	}
	worker, err := a.watcher.UpdateWorkerProgress(workerID, progress)
	if err != nil {
		return control.ErrorResponse("progress_failed", err.Error())
	}
	out := controlWorker(worker)
	return control.Response{OK: true, Worker: &out}
}

func (a *controlApp) handleWorkerClose(req control.Request) control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Worker watcher is not running.")
	}
	workerID := strings.TrimSpace(req.WorkerID)
	if workerID == "" {
		return control.ErrorResponse("missing_worker_id", "Worker id is required.")
	}
	worker := a.watcher.GetWorker(workerID)
	if worker != nil && !worker.Delegated && !worker.Hidden && !req.Force {
		return control.ErrorResponse("worker_not_delegated", "Refusing to close a session that was not created as a Brain delegated Zen Worker. Use --force only when you intentionally want to close this external session.")
	}
	requiresForce := worker != nil && !req.Force && closeRequiresForce(worker)
	if worker != nil && !req.Force && a.brainStore != nil {
		if turn, hasTurn, turnErr := a.brainStore.Turn(workerID); turnErr == nil && hasTurn {
			requiresForce = canonicalCloseAdmission(turn, hasTurn)
		}
	}
	if requiresForce {
		return control.ErrorResponse("worker_running_requires_force", "Worker is still running or unresolved. Send it a cancellation request first, wait for done/failed/blocked, or close with force.")
	}
	var release func(string) (modelprofiles.PersistResult, error)
	if a.profiles != nil {
		release = a.profiles.ReleaseSession
	}
	teardown := modelprofiles.TeardownSession(workerID, a.watcher.KillSession, a.sessionLivenessProbe, release)
	if teardown.Err != nil {
		code := modelprofiles.ControlErrorCode(teardown.Err)
		if code == "" || code == modelprofiles.CodeProfilesUnavailable {
			code = "close_failed"
		}
		resp := control.ErrorResponse(code, teardown.Err.Error())
		if teardown.Persist.Applied {
			outcome, durable := modelprofiles.WirePersistFields(teardown.Persist)
			resp.PersistenceOutcome = control.PersistenceOutcome(outcome)
			resp.PersistenceDurable = durable
		}
		return resp
	}
	if a.brainStore != nil {
		if _, err := a.brainStore.ReleaseSessionAttempt(workerID, "session_closed"); err != nil {
			return control.ErrorResponse("close_failed", fmt.Sprintf("Session closed but Work owner release failed: %v", err))
		}
	}
	if worker == nil {
		return control.Response{OK: true}
	}
	out := controlWorker(worker)
	out.Status = string(classifier.StateRemoved)
	return control.Response{OK: true, Worker: &out}
}

func (a *controlApp) sessionLivenessProbe(sessionID string) (modelprofiles.SessionLiveness, error) {
	if a == nil || a.watcher == nil {
		return modelprofiles.SessionLivenessUnknown, fmt.Errorf("watcher unavailable")
	}
	presence, err := a.watcher.ProbeSession(sessionID)
	return mapWatcherSessionPresence(presence, err)
}

func mapWatcherSessionPresence(presence watcher.SessionPresence, err error) (modelprofiles.SessionLiveness, error) {
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
	executor, ok := a.execs.WorkerExecutor(executorID)
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
	executors := a.execs.WorkerExecutors()
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

func (a *controlApp) currentBrainExecutor() (work.WorkerExecutor, bool) {
	if a == nil || a.execs == nil {
		return work.WorkerExecutor{}, false
	}
	if preferred := brainHostExecutorOverride(); preferred != "" {
		return a.execs.WorkerExecutor(preferred)
	}
	if a.brainStore != nil {
		if hostSession, err := a.brainStore.HostSession(); err == nil {
			if executorID := strings.TrimSpace(hostSession.ExecutorID); executorID != "" {
				return a.execs.WorkerExecutor(executorID)
			}
		}
	}
	return a.execs.WorkerExecutor("codex")
}

// spawnProfileClientHint derives the canonical Model Profiles client executor
// (codex|claude) from the configured CLI identity. req.Executor remains the
// process/executor selection; aliases must not be passed as PrepareLaunch IDs.
func (a *controlApp) spawnProfileClientHint(req control.Request, command string) string {
	name := strings.TrimSpace(req.ExecutorID)
	if name == "" {
		name = strings.TrimSpace(req.Executor)
	}
	if a != nil && a.execs != nil && name != "" {
		if ae, ok := a.execs.WorkerExecutor(name); ok {
			return ae.ProfileClientExecutor()
		}
	}
	return work.ProfileClientExecutor(name, command)
}

func (a *controlApp) resolveSpawnCommand(req control.Request) (string, error) {
	if command := strings.TrimSpace(req.Command); command != "" {
		// Explicit full-command overrides are user-authored; do not mutate
		// their authorization/sandbox configuration.
		return command, nil
	}
	executorName := strings.TrimSpace(req.Executor)
	if executorName == "" {
		if delegatedExecutor, ok := a.brainCallerDelegatedExecutor(req.WorkerID); ok {
			executorName = delegatedExecutor
		}
	}
	if executorName == "" && a != nil && a.execs != nil {
		if delegatedExecutor, ok := a.execs.DelegatedWorkerExecutor(); ok {
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
		provider := work.InferWorkerProvider(executor.Kind, command, executorName, executor.Name)
		if provider == work.WorkerProviderCodex {
			// Brain-delegated Codex sessions must run non-interactively with
			// the most permissive available authorization mode so internal
			// progress commands do not block on approval prompts.
			command = work.HardenCodexDelegatedCommand(command)
		} else if provider == work.WorkerProviderClaude {
			// Brain-delegated Claude sessions must run non-interactively with
			// the most permissive authorization mode so internal progress
			// commands do not block on approval prompts.
			command = work.HardenClaudeCommand(command)
		} else if provider == work.WorkerProviderOpenCode {
			hardened, hardenErr := work.HardenOpenCodeDelegatedCommand(command)
			if hardenErr != nil {
				return "", hardenErr
			}
			command = hardened
		} else if provider == work.WorkerProviderPi {
			var ensureErr error
			command, ensureErr = work.EnsurePiSessionLaunchCommand(command)
			if ensureErr != nil {
				return "", ensureErr
			}
		}
		return command, nil
	}
	provider := work.InferWorkerProvider(executorName)
	if provider == work.WorkerProviderCodex {
		return work.HardenCodexDelegatedCommand(executorName), nil
	} else if provider == work.WorkerProviderClaude {
		return work.HardenClaudeCommand(executorName), nil
	} else if provider == work.WorkerProviderOpenCode {
		return work.HardenOpenCodeDelegatedCommand(executorName)
	} else if provider == work.WorkerProviderPi {
		return work.EnsurePiSessionLaunchCommand(executorName)
	}
	return executorName, nil
}

func (a *controlApp) brainCallerDelegatedExecutor(workerID string) (string, bool) {
	if a == nil || a.brainStore == nil || a.execs == nil {
		return "", false
	}
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return "", false
	}
	host, err := a.brainStore.HostSession()
	if err != nil || strings.TrimSpace(host.ID) == "" || strings.TrimSpace(host.ID) != workerID {
		return "", false
	}
	if delegatedExecutor, ok := a.brainDelegatedExecutor(); ok {
		return delegatedExecutor.ID, true
	}
	return "", false
}

func (a *controlApp) brainDelegatedExecutor() (work.WorkerExecutor, bool) {
	if a == nil || a.execs == nil {
		return work.WorkerExecutor{}, false
	}
	// Effective delegated selection (including startup env lock) lives only on
	// the shared ExecutorConfig owner — no parallel env readers here.
	return a.execs.DelegatedWorkerExecutor()
}

func brainHostExecutorOverride() string {
	return strings.TrimSpace(os.Getenv("ZEN_BRAIN_HOST_EXECUTOR"))
}

func visibleControlWorkers(workers []*classifier.Worker) []control.Worker {
	out := make([]control.Worker, 0, len(workers))
	for _, worker := range workers {
		if worker == nil || worker.Hidden {
			continue
		}
		out = append(out, controlWorker(worker))
	}
	return out
}

func controlWorker(worker *classifier.Worker) control.Worker {
	if worker == nil {
		return control.Worker{}
	}
	var lastSeenAt *time.Time
	if !worker.LastSeenAt.IsZero() {
		value := worker.LastSeenAt
		lastSeenAt = &value
	}
	return control.Worker{
		ID:                  worker.ID,
		Name:                worker.Name,
		Status:              string(worker.State),
		Summary:             worker.Summary,
		Phase:               worker.Phase,
		Attention:           worker.Attention,
		TaskClass:           worker.TaskClass,
		EventKind:           worker.EventKind,
		DetailsJSON:         worker.DetailsJSON,
		NeedsAttention:      worker.NeedsAttention,
		LastProgressAt:      worker.LastProgressAt,
		ExpectedNextCheckAt: worker.ExpectedNextCheckAt,
		LeaseSeconds:        worker.LeaseSeconds,
		Cwd:                 worker.Cwd,
		Command:             worker.Command,
		UpdatedAt:           worker.UpdatedAt,
		LastSeenAt:          lastSeenAt,
		Hidden:              worker.Hidden,
		Delegated:           worker.Delegated,
	}
}

func controlExecutor(executor work.WorkerExecutor) control.Executor {
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
		"ZEN_WORKER_PROGRESS_CMD": watcher.ZenExecutablePath(),
	}
	if worktreeRoot, err := work.DefaultWorktreeRoot(); err == nil {
		env["ZEN_WORKTREE_ROOT"] = worktreeRoot
	}
	if stateDir = strings.TrimSpace(stateDir); stateDir != "" {
		env["ZEN_STATE_DIR"] = stateDir
	}
	return env
}

func closeRequiresForce(worker *classifier.Worker) bool {
	if worker == nil || !worker.Delegated || worker.Hidden {
		return false
	}
	switch worker.State {
	case classifier.StateDone, classifier.StateFailed, classifier.StateBlocked:
		return false
	default:
		return true
	}
}

// canonicalCloseAdmission implements C.2.5 for canonical-turn sessions: close
// is admitted when the canonical ledger status is terminal (Done/Failed/
// Unknown); force is required otherwise. This is the same canonical row that
// capture and list project, so the lifecycle-close split (capture=done vs
// close=requires_force) cannot recur.
func canonicalCloseAdmission(turn watcher.TurnSnapshot, hasTurn bool) bool {
	if !hasTurn {
		return true
	}
	switch turn.Status {
	case watcher.TurnDone, watcher.TurnFailed, watcher.TurnUnknown:
		return false
	default:
		return true
	}
}

func lifecycleProtocol(profile string) string {
	return strings.TrimSpace(fmt.Sprintf(`Zen lifecycle protocol:
Profile: %s.
Complete the scoped objective and acceptance criteria. Ask Brain only for a material decision or missing authority; continue independent authorized work.
Preserve unrelated changes in the supplied cwd. Use $ZEN_WORKTREE_ROOT only for required concurrent-write isolation. Use TMPDIR/TMP/TEMP for scratch and $ZEN_BUILD_TMPDIR for large builds.
Keep descendants and resources within this Session's ownership. Reuse named resources; report resource limits rather than bypassing them. Clean up owned scratch and unneeded children before completion.
Return the report in the Worker result. Persist Brain reports only in the runtime Brain worklog/ when requested, never in the project repository.
Run meaningful, risk-proportionate checks and required repository gates. Repeat only for edits, failures or unresolved concerns; report unverified limitations.
Report through "$ZEN_WORKER_PROGRESS_CMD" worker progress at phase changes, meaningful long steps, blockers and completion. ZEN_WORKER_ID identifies this Session. Use the exact --turn-id from the appended turn contract; omit it only when no turn contract exists.
Progress is a check-in, not a request for Brain inspection. Continue authorized work without waiting for acknowledgement; exact completion/failure signals settle the Attempt.
Required fields: --status running|done|failed|blocked --phase starting|reading|planning|working|verifying|reporting --attention none|done|blocked|failed|user_input|stale --summary "<result>".
Optional semantics: --task-class exploration|mechanical_change|lasting_design --event-kind progress|invariant|artifact|risk|needs_judgment|verification|done --details-json '<evidence>' --lease 300.
Use attention none while working, user_input only for a necessary decision, and status done with attention done only after acceptance and feasible verification.`, normalizeWorkerProfile(profile)))
}

func normalizeWorkerProfile(profile string) string {
	switch strings.TrimSpace(profile) {
	case "quick", "research", "implementation", "long_running":
		return strings.TrimSpace(profile)
	default:
		return "implementation"
	}
}

func defaultWorkerName(executor, command string) string {
	if executor := strings.TrimSpace(executor); executor != "" {
		return executor
	}
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return "Worker"
	}
	return fields[0]
}

func ensureTrailingNewline(value string) string {
	if strings.HasSuffix(value, "\n") || strings.HasSuffix(value, "\r") {
		return value
	}
	return value + "\n"
}

func mergeControlEnv(base, overlay map[string]string) map[string]string {
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

func (a *controlApp) handleProviderList() control.Response {
	if a == nil || a.profiles == nil {
		return control.ErrorResponse(modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
	}
	proj, err := a.profiles.ProjectProviders()
	if err != nil {
		return control.ErrorResponse(modelprofiles.ControlErrorCode(err), err.Error())
	}
	return control.Response{OK: true, Providers: &proj}
}

func (a *controlApp) handleProviderUpsert(req control.Request) control.Response {
	if a == nil || a.profiles == nil {
		return control.ErrorResponse(modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
	}
	if req.ProviderConnection == nil {
		return control.ErrorResponse(modelprofiles.CodeProfileInvalid, "provider_connection is required")
	}
	create := strings.EqualFold(strings.TrimSpace(req.Operation), "create")
	if !create && req.ProviderConnection.ID != "" {
		if _, err := a.profiles.GetProfile(req.ProviderConnection.ID); errors.Is(err, modelprofiles.ErrNotFound) {
			create = true
		}
	}
	apiKey := strings.TrimSpace(req.Credential)
	req.Credential = ""
	proj, err := a.profiles.UpsertProviderConnection(*req.ProviderConnection, apiKey, req.Revision, create)
	apiKey = ""
	return a.providersMutationResponse(proj, err)
}

func (a *controlApp) handleProviderDelete(req control.Request) control.Response {
	if a == nil || a.profiles == nil {
		return control.ErrorResponse(modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
	}
	id := strings.TrimSpace(req.ConnectionID)
	if id == "" {
		id = strings.TrimSpace(req.ProfileID)
	}
	if id == "" {
		id = strings.TrimSpace(req.ID)
	}
	proj, err := a.profiles.DeleteProviderConnection(id, req.Revision)
	return a.providersMutationResponse(proj, err)
}

func (a *controlApp) handleProviderSetDefault(req control.Request) control.Response {
	if a == nil || a.profiles == nil {
		return control.ErrorResponse(modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
	}
	executorID := strings.TrimSpace(req.Client)
	if executorID == "" {
		executorID = strings.TrimSpace(req.ExecutorID)
	}
	if executorID == "" {
		executorID = strings.TrimSpace(req.Executor)
	}
	if executorID == "" {
		executorID = strings.TrimSpace(req.Executor)
	}
	connectionID := strings.TrimSpace(req.ConnectionID)
	if connectionID == "" {
		connectionID = strings.TrimSpace(req.ProfileID)
	}
	proj, err := a.profiles.SetProviderDefault(executorID, connectionID, req.ModelID, req.Revision)
	return a.providersMutationResponse(proj, err)
}

// handleCodexGatewayStatus reports the truthful machine-level Codex gateway
// takeover state.
func (a *controlApp) handleCodexGatewayStatus() control.Response {
	if a == nil || a.profiles == nil {
		return control.ErrorResponse(modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
	}
	status := a.profiles.GatewayStatus()
	return control.Response{OK: true, Gateway: &status}
}

// handleCodexGatewayEnable activates the machine-level takeover: exact backup
// of the CLI config, atomic projection to the stable gateway endpoint, and the
// gateway pointed at the currently selected Codex Provider.
func (a *controlApp) handleCodexGatewayEnable() control.Response {
	if a == nil || a.profiles == nil {
		return control.ErrorResponse(modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
	}
	status, err := a.profiles.EnableCodexGateway(modelprofiles.DefaultGatewayListenAddr)
	if err != nil {
		return control.ErrorResponse(modelprofiles.ControlErrorCode(err), err.Error())
	}
	return control.Response{OK: true, Gateway: &status}
}

// handleCodexGatewayDisable removes only the Zen-owned projection.
func (a *controlApp) handleCodexGatewayDisable() control.Response {
	if a == nil || a.profiles == nil {
		return control.ErrorResponse(modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
	}
	status, err := a.profiles.DisableCodexGateway()
	if err != nil {
		return control.ErrorResponse(modelprofiles.ControlErrorCode(err), err.Error())
	}
	return control.Response{OK: true, Gateway: &status}
}

// handleCodexGatewayRestoreBackup rolls the exact pre-takeover config back.
func (a *controlApp) handleCodexGatewayRestoreBackup() control.Response {
	if a == nil || a.profiles == nil {
		return control.ErrorResponse(modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
	}
	status, err := a.profiles.RestoreCodexGatewayBackup()
	if err != nil {
		return control.ErrorResponse(modelprofiles.ControlErrorCode(err), err.Error())
	}
	return control.Response{OK: true, Gateway: &status}
}

func (a *controlApp) handleProviderSwitch(req control.Request) control.Response {
	if a == nil || a.profiles == nil {
		return control.ErrorResponse(modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
	}
	executorID := strings.TrimSpace(req.Client)
	if executorID == "" {
		executorID = strings.TrimSpace(req.ExecutorID)
	}
	if executorID == "" {
		executorID = strings.TrimSpace(req.Executor)
	}
	connectionID := strings.TrimSpace(req.ConnectionID)
	if connectionID == "" {
		connectionID = strings.TrimSpace(req.ProfileID)
	}
	proj, err := a.profiles.SwitchProvider(executorID, connectionID, req.Revision)
	return a.providersMutationResponse(proj, err)
}

// handleProviderSetModels persists the client-side model support allowlist of
// one connection (provider_set_models). The gateway never owns a default
// model: this write only decides which discovered models the client exposes.
func (a *controlApp) handleProviderSetModels(req control.Request) control.Response {
	if a == nil || a.profiles == nil {
		return control.ErrorResponse(modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
	}
	id := strings.TrimSpace(req.ConnectionID)
	if id == "" {
		id = strings.TrimSpace(req.ProfileID)
	}
	if id == "" {
		return control.ErrorResponse(modelprofiles.CodeProfileInvalid, "connection_id is required")
	}
	proj, persist, err := a.profiles.SetProviderModelSupport(id, req.ModelIDs)
	if !persist.Applied {
		return control.ErrorResponse(modelprofiles.ControlErrorCode(err), err.Error())
	}
	response := control.Response{OK: true, Providers: &proj}
	if outcome, durable := modelprofiles.WirePersistFields(persist); outcome != "" {
		response.PersistenceOutcome = control.PersistenceOutcome(outcome)
		response.PersistenceDurable = durable
	}
	if err != nil {
		log.Printf("provider model support applied with uncertain durability: %v", err)
		response.Confirmation = "Model support updated; persistence was applied but directory durability is uncertain."
	}
	return response
}

func (a *controlApp) handleProviderDiscover(req control.Request) control.Response {
	if a == nil || a.profiles == nil {
		return control.ErrorResponse(modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
	}
	id := strings.TrimSpace(req.ConnectionID)
	if id == "" {
		id = strings.TrimSpace(req.ProfileID)
	}
	if id == "" {
		id = strings.TrimSpace(req.ID)
	}
	entries, err := a.profiles.DiscoverProviderModels(id, true)
	if err != nil && len(entries) == 0 {
		return control.ErrorResponse(modelprofiles.ControlErrorCode(err), err.Error())
	}
	proj, _ := a.profiles.ProjectProviders()
	proj.Models[id] = entries
	return control.Response{OK: true, Providers: &proj}
}

func (a *controlApp) providersMutationResponse(proj modelprofiles.ProviderCatalogProjection, err error) control.Response {
	persist := modelprofiles.PersistResultFromError(err)
	if !persist.Applied {
		return control.ErrorResponse(modelprofiles.ControlErrorCode(err), err.Error())
	}
	response := control.Response{OK: true, Providers: &proj}
	if outcome, durable := modelprofiles.WirePersistFields(persist); outcome != "" {
		response.PersistenceOutcome = control.PersistenceOutcome(outcome)
		response.PersistenceDurable = durable
	}
	if err != nil {
		log.Printf("provider catalog mutation applied with uncertain durability: %v", err)
		response.Confirmation = "Provider catalog updated; persistence was applied but directory durability is uncertain."
	}
	return response
}

func (a *controlApp) handleThreadRuntimeGet(req control.Request) control.Response {
	if a == nil || a.profiles == nil {
		return control.ErrorResponse(modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
	}
	sessionID := strings.TrimSpace(req.WorkerID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(req.ID)
	}
	sel, ok := a.profiles.ThreadRuntime(sessionID)
	if !ok {
		return control.ErrorResponse(modelprofiles.CodeBindingNotFound, "thread runtime not found")
	}
	snap, _ := a.profiles.SessionSnapshot(sessionID)
	return control.Response{OK: true, ThreadRuntime: &sel, SessionRoute: &snap}
}

func (a *controlApp) handleThreadRuntimeSet(req control.Request) control.Response {
	if a == nil || a.profiles == nil {
		return control.ErrorResponse(modelprofiles.CodeProfilesUnavailable, "Providers are not available.")
	}
	sessionID := strings.TrimSpace(req.WorkerID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(req.ID)
	}
	if req.Runtime == nil {
		return control.ErrorResponse(modelprofiles.CodeProfileInvalid, "runtime is required")
	}
	if a.threadRuntimeSet == nil {
		return control.ErrorResponse(modelprofiles.CodeProfilesUnavailable, "Thread runtime service is not available.")
	}
	snap, persist, err := a.threadRuntimeSet(sessionID, *req.Runtime)
	if !persist.Applied {
		return control.ErrorResponse(modelprofiles.ControlErrorCode(err), err.Error())
	}
	if snap.Current == nil {
		return control.ErrorResponse(modelprofiles.CodeBindingNotFound, "thread runtime not found after switch")
	}
	binding := *snap.Current
	sel := modelprofiles.ThreadRuntimeSelection{
		SessionID:              binding.SessionID,
		Client:                 binding.Client,
		ConnectionID:           binding.ConnectionID,
		ConnectionName:         binding.ConnectionName,
		ProviderLabel:          binding.ProviderLabel,
		ModelID:                binding.ModelID,
		ReasoningEffort:        binding.ReasoningEffort,
		ReasoningEffortDefault: binding.ReasoningEffortDefault,
		ReasoningEfforts:       append([]string(nil), binding.ReasoningEfforts...),
		CredentialReady:        binding.CredentialReady,
		HotSwitchable:          binding.HotSwitchable,
	}
	response := control.Response{OK: true, ThreadRuntime: &sel, SessionRoute: &snap, Binding: &binding}
	if outcome, durable := modelprofiles.WirePersistFields(persist); outcome != "" {
		response.PersistenceOutcome = control.PersistenceOutcome(outcome)
		response.PersistenceDurable = durable
	}
	if err != nil {
		log.Printf("thread runtime switch applied with uncertain durability: %v", err)
	}
	return response
}
