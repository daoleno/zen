package server

import (
	"log"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/modelprofiles"
	"github.com/daoleno/zen/daemon/work"
)

// Codex TUI upstream limitation (researched, evidence-based): the running Codex
// TUI exposes NO external mutation protocol for thread settings (model +
// reasoning effort). There is no IPC/control channel into a live TUI process
// (remote-control is app-server-only), and /model is an interactive picker that
// cannot be driven reliably. The truthful least-disruptive mutation for a
// Zen-initiated model/effort change on a live Session is therefore the managed
// handoff implemented here: interrupt + exit the TUI process, then resume the
// SAME thread (`codex resume <thread> -c model=... -c model_reasoning_effort=...`)
// with the new identity. Thread history and the Zen Session/route are
// preserved; the TUI footer shows the real model (no stale display).
//
// When the handoff cannot run or fails, the route activation still applies to
// next admitted requests and the reply reports the handoff state truthfully —
// Zen never claims live UI convergence it did not prove.

// codexHandoffState is the wire projection of a managed handoff attempt.
type codexHandoffState struct {
	State   string `json:"state"` // applied | failed | skipped
	Message string `json:"message,omitempty"`
}

const (
	codexHandoffApplied  = "applied"
	codexHandoffFailed   = "failed"
	codexHandoffSkipped  = "skipped"
	codexHandoffExitWait = 8 * time.Second
	codexHandoffUpWait   = 10 * time.Second
	codexHandoffSettle   = 350 * time.Millisecond
)

// handoffManagedCodex applies a Zen-initiated model/effort change to a live
// managed Codex TUI Session via the truthful resume handoff. Returns the
// wire state; every failure path clears the handoff-pending transition so the
// router converges on the CLI's actual identity instead of rewriting silently.
func (s *Server) handoffManagedCodex(agentID, modelID, effortOverride string) codexHandoffState {
	if s == nil {
		return codexHandoffState{State: codexHandoffFailed, Message: "daemon unavailable"}
	}
	agentID = strings.TrimSpace(agentID)
	modelID = strings.TrimSpace(modelID)
	effortOverride = strings.TrimSpace(effortOverride)
	owner := s.modelProfiles()
	if owner == nil || s.watcher == nil {
		return codexHandoffState{State: codexHandoffSkipped, Message: "managed Codex handoff unavailable"}
	}
	routeID := owner.RouteIDForSession(agentID)
	if routeID == "" {
		return codexHandoffState{State: codexHandoffSkipped, Message: "session is not a routed managed Codex Session"}
	}
	agent := s.lookupAgent(agentID)
	if agent == nil || agent.ProcessID <= 0 {
		return codexHandoffState{State: codexHandoffSkipped, Message: "no live Codex process to hand off"}
	}
	rolloutPath := work.LiveCodexRolloutPath(agent.ProcessID)
	if rolloutPath == "" {
		return codexHandoffState{State: codexHandoffSkipped, Message: "Codex thread identity is not resolvable; the switch applies to the next admitted request only"}
	}
	threadID := work.CodexSessionIDFromRolloutPath(rolloutPath)
	if threadID == "" {
		return codexHandoffState{State: codexHandoffSkipped, Message: "Codex thread identity is not resolvable; the switch applies to the next admitted request only"}
	}
	resumeCommand, err := owner.CodexResumeCommand(agentID, threadID, modelID, effortOverride)
	if err != nil {
		return codexHandoffState{State: codexHandoffFailed, Message: err.Error()}
	}

	// Transition: the binding wins for admitted requests until the resumed CLI
	// carries the new identity natively. Every failure path below clears it.
	owner.SetSessionHandoffPending(routeID, true)
	clearPending := func() {
		owner.SetSessionHandoffPending(routeID, false)
	}
	fail := func(message string) codexHandoffState {
		clearPending()
		log.Printf("codex handoff for %s failed: %s", agentID, message)
		return codexHandoffState{State: codexHandoffFailed, Message: message}
	}

	// Interrupt any running turn, then exit the TUI cleanly.
	if err := s.watcher.SendControlKey(agentID, "C-c"); err != nil {
		log.Printf("codex handoff interrupt (best-effort) for %s: %v", agentID, err)
	}
	time.Sleep(codexHandoffSettle)
	if err := s.watcher.SendInput(agentID, "/exit\n"); err != nil {
		return fail("could not exit the Codex TUI: " + err.Error())
	}
	if !s.watcher.WaitForPaneProcessExit(agentID, agent.ProcessID, codexHandoffExitWait) {
		return fail("the Codex TUI did not exit; the switch applies to the next admitted request only")
	}

	// Resume the same thread with the new identity (history preserved).
	if err := s.watcher.SendInput(agentID, resumeCommand+"\n"); err != nil {
		return fail("could not start the resumed Codex thread: " + err.Error())
	}
	if !s.watcher.WaitForPaneCommand(agentID, "codex", codexHandoffUpWait) {
		return fail("the resumed Codex thread did not come up; the switch applies to the next admitted request only")
	}
	clearPending()
	return codexHandoffState{State: codexHandoffApplied}
}

// handoffTargetForActivation decides whether a Session activation should run
// the managed Codex handoff: only live routed managed Codex Sessions.
func (s *Server) handoffTargetForActivation(agentID string, selection *modelprofiles.WireBinding) bool {
	if s == nil || selection == nil || agentID == "" {
		return false
	}
	if selection.Client != "codex" || !selection.HotSwitchable {
		return false
	}
	agent := s.lookupAgent(agentID)
	return agent != nil && agent.ProcessID > 0
}
