package work

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/modelprofiles"
	"github.com/daoleno/zen/daemon/watcher"
)

// ProfileLaunchOwner is the optional Model Profiles surface used by Calendar
// delegated launches. Defined here so work can depend on modelprofiles without
// calendar/modelprofiles importing each other through a cycle.
type ProfileLaunchOwner interface {
	PrepareLaunch(executorID, profileID, baseCommand string) (modelprofiles.SessionLaunchPlan, error)
	CommitLaunch(provisionalID, sessionID string) (modelprofiles.SessionRouteState, modelprofiles.WireSessionSnapshot, modelprofiles.PersistResult, error)
	AbortLaunch(provisionalID string) (modelprofiles.PersistResult, error)
	ReleaseSession(sessionID string) (modelprofiles.PersistResult, error)
}

// DelegatedSessionOwner is the tmux lifecycle surface Calendar Work requires.
// *watcher.Watcher implements it in production.
type DelegatedSessionOwner interface {
	CreateSession(preferredTarget string, opts watcher.CreateSessionOptions) (string, error)
	KillSession(sessionID string) error
	ProbeSession(target string) (watcher.SessionPresence, error)
	SendInputWhenReady(sessionID, command, text string) error
	// SendInputWhenReadyBudgeted bounds the initial handoff to the exact
	// spawned provider input surface for one scheduled occurrence: readiness
	// evidence must arrive within budget, definitely-not-submitted attempts
	// may retry within it, and ambiguous admission or loss of the spawned
	// identity fails closed without replay.
	SendInputWhenReadyBudgeted(sessionID, command, text string, budget time.Duration) error
}

// DefaultScheduledInputReadyBudget is the provider-neutral total bound for the
// initial delegated handoff of one scheduled occurrence. It covers adapter
// cold starts (including OpenCode TUI/model loading) without unbounded waiting;
// the per-attempt readiness window remains the adapter-specific one.
const DefaultScheduledInputReadyBudget = 90 * time.Second

// TmuxRunner adapts Watcher's owned tmux lifecycle to SessionRunner. Calendar
// Work must use this path instead of opening raw tmux sessions, otherwise it
// bypasses delegated ownership markers, resource limits, and orphan cleanup.
// When Profiles is set, configured Codex/Claude launches resolve the executor
// default (or ProfileID override) through the same Prepare/Create/Commit
// compiler as control and Brain; Abort is route-aware.
//
// InputReadyBudget bounds the initial ready-and-submit handoff for the spawned
// session. Zero keeps the legacy single-attempt behavior; Calendar sets
// DefaultScheduledInputReadyBudget so a definitely-not-submitted readiness
// timeout may retry within the same occurrence instead of failing it.
//
// Role remains the configured CLI executor identity (alias/wrapper name). The
// Profile compiler hint is derived from Provider/command inference
// (codex|claude only). Applied-but-not-durable Prepare/Commit fail closed:
// Calendar has no persistence wire, so uncertain routes are never reported as
// ordinary success. Commit durability failure intentionally tears down the
// live Session before returning, so Launcher does not observe a successful
// Spawn that later Abort-kills an applied route.
type TmuxRunner struct {
	Watcher   DelegatedSessionOwner
	Env       map[string]string
	Profiles  ProfileLaunchOwner
	ProfileID string
	// InputReadyBudget bounds the initial handoff; zero = legacy single attempt.
	InputReadyBudget time.Duration
}

// Spawn creates a detached tmux session and returns the watcher-compatible
// session identifier "<session>:<window_id>".
func (r TmuxRunner) Spawn(role, cwd, command string) (string, error) {
	if r.Watcher == nil {
		return "", fmt.Errorf("delegated watcher is required")
	}
	opts := watcher.CreateSessionOptions{
		Cwd:         cwd,
		Command:     command,
		Name:        role,
		Detached:    true,
		ProgressEnv: true,
		Delegated:   true,
		Env:         cloneStringMap(r.Env),
	}
	provisionalID := ""
	if r.Profiles != nil {
		clientHint := ProfileClientExecutor(role, command)
		plan, planErr := r.Profiles.PrepareLaunch(clientHint, r.ProfileID, command)
		if planErr != nil && !plan.Persist.Applied && !plan.Bypass {
			return "", planErr
		}
		if plan.Applied && !plan.Bypass {
			if !plan.Persist.Durable || planErr != nil {
				// Fail closed before create: no live Session, abort provisional.
				durabilityErr := planErr
				if durabilityErr == nil {
					durabilityErr = modelprofiles.ErrPersistDirSync
				}
				abortPersist, abortErr := r.Profiles.AbortLaunch(plan.ProvisionalID)
				err := errors.Join(durabilityErr, abortErr)
				if abortErr == nil && !abortPersist.Applied {
					err = errors.Join(err, modelprofiles.ErrLaunchCleanupIncomplete)
				} else if abortErr != nil && !abortPersist.Applied && !errors.Is(abortErr, modelprofiles.ErrLaunchCleanupIncomplete) {
					err = errors.Join(err, modelprofiles.ErrLaunchCleanupIncomplete)
				}
				return "", fmt.Errorf("calendar profile prepare not durable: %w", err)
			}
			opts.Command = plan.Command
			opts.Env = mergeStringMaps(opts.Env, plan.Env)
			provisionalID = plan.ProvisionalID
		}
	}
	agentID, err := r.Watcher.CreateSession("", opts)
	if err != nil {
		if provisionalID != "" && r.Profiles != nil {
			abortPersist, abortErr := r.Profiles.AbortLaunch(provisionalID)
			err = errors.Join(err, abortErr)
			if abortErr != nil || !abortPersist.Applied {
				if abortErr == nil || !errors.Is(abortErr, modelprofiles.ErrLaunchCleanupIncomplete) {
					err = errors.Join(err, modelprofiles.ErrLaunchCleanupIncomplete)
				}
			}
		}
		return "", err
	}
	if provisionalID != "" && r.Profiles != nil {
		_, _, persist, commitErr := r.Profiles.CommitLaunch(provisionalID, agentID)
		if !persist.Applied {
			cleanup := modelprofiles.CleanupFailedLaunch(r.Profiles, provisionalID, agentID, r.Watcher.KillSession, r.sessionLivenessProbe)
			return "", errors.Join(commitErr, cleanup.Err)
		}
		if !persist.Durable || commitErr != nil {
			// Intentional fail-closed: tear down the live applied route rather
			// than report ordinary success without durable evidence. Cleanup
			// runs inside Spawn so Launcher never receives a session ID that
			// would later be Abort-killed as a false "spawn success".
			durabilityErr := commitErr
			if durabilityErr == nil {
				durabilityErr = modelprofiles.ErrPersistDirSync
			}
			cleanup := modelprofiles.CleanupFailedLaunch(r.Profiles, "", agentID, r.Watcher.KillSession, r.sessionLivenessProbe)
			return "", fmt.Errorf("calendar profile commit not durable: %w", errors.Join(durabilityErr, cleanup.Err))
		}
	}
	return agentID, nil
}

// SendWhenReady waits for a freshly spawned known agent UI before sending the
// initial prompt. With InputReadyBudget set, the bounded handoff retries safe
// definitely-not-submitted readiness timeouts within the same occurrence and
// fails closed on ambiguous admission or a lost spawned identity.
func (r TmuxRunner) SendWhenReady(agentID, command, text string) error {
	if r.Watcher == nil {
		return fmt.Errorf("delegated watcher is required")
	}
	if r.InputReadyBudget > 0 {
		return r.Watcher.SendInputWhenReadyBudgeted(agentID, command, text, r.InputReadyBudget)
	}
	return r.Watcher.SendInputWhenReady(agentID, command, text)
}

// Abort terminates the one fresh window created for a failed Calendar launch
// and releases any committed Model Profile route only after kill proves the
// Session is gone (or was already missing).
func (r TmuxRunner) Abort(agentID string) error {
	if r.Watcher == nil {
		return fmt.Errorf("delegated watcher is required")
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil
	}
	if r.Profiles != nil {
		cleanup := modelprofiles.CleanupFailedLaunch(r.Profiles, "", agentID, r.Watcher.KillSession, r.sessionLivenessProbe)
		return cleanup.Err
	}
	return r.Watcher.KillSession(agentID)
}

func (r TmuxRunner) sessionLivenessProbe(sessionID string) (modelprofiles.SessionLiveness, error) {
	if r.Watcher == nil {
		return modelprofiles.SessionLivenessUnknown, fmt.Errorf("watcher unavailable")
	}
	presence, err := r.Watcher.ProbeSession(sessionID)
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

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mergeStringMaps(base, overlay map[string]string) map[string]string {
	if len(overlay) == 0 {
		return base
	}
	out := cloneStringMap(base)
	if out == nil {
		out = map[string]string{}
	}
	for k, v := range overlay {
		out[k] = v
	}
	return out
}
