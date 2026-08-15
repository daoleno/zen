package server

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/modelprofiles"
	"github.com/daoleno/zen/daemon/work"
)

// Codex has no reliable external live-settings mutation API. Zen therefore
// stages a native lane handoff before publishing a new ThreadRuntime route:
// validate target, resume the same native thread with the target identity,
// prove the process is live, then commit the route. Failure restores the old
// resume command while the old route remains acknowledged.

type codexHandoffState struct {
	State   string `json:"state"`
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

type codexRuntimeStage struct {
	previousCommand string
	targetProcessID int
}

type codexHandoffIO interface {
	SendControlKey(sessionID, key string) error
	SendInput(sessionID, input string) error
	WaitForPaneProcessExit(sessionID string, processID int, timeout time.Duration) bool
	WaitForPaneCommandProcessID(sessionID, commandBase string, timeout time.Duration) (int, bool)
}

func switchCodexLane(io codexHandoffIO, agentID string, processID int, targetCommand, previousCommand string) (int, error) {
	if err := io.SendControlKey(agentID, "C-c"); err != nil {
		log.Printf("codex runtime handoff interrupt (best-effort) for %s: %v", agentID, err)
	}
	time.Sleep(codexHandoffSettle)
	if err := io.SendInput(agentID, "/exit\n"); err != nil {
		return 0, fmt.Errorf("could not exit the Codex TUI: %w", err)
	}
	if !io.WaitForPaneProcessExit(agentID, processID, codexHandoffExitWait) {
		return 0, fmt.Errorf("the Codex TUI did not exit")
	}
	if err := io.SendInput(agentID, targetCommand+"\n"); err != nil {
		return 0, restoreCodexLane(io, agentID, previousCommand, fmt.Errorf("could not start target Codex runtime: %w", err))
	}
	targetPID, ready := io.WaitForPaneCommandProcessID(agentID, "codex", codexHandoffUpWait)
	if !ready {
		return 0, restoreCodexLane(io, agentID, previousCommand, fmt.Errorf("target Codex runtime did not come up"))
	}
	return targetPID, nil
}

func restoreCodexLane(io codexHandoffIO, agentID, previousCommand string, cause error) error {
	_ = io.SendControlKey(agentID, "C-c")
	if err := io.SendInput(agentID, previousCommand+"\n"); err != nil {
		return fmt.Errorf("%v; previous runtime restore failed: %w", cause, err)
	}
	if _, ready := io.WaitForPaneCommandProcessID(agentID, "codex", codexHandoffUpWait); !ready {
		return fmt.Errorf("%v; previous runtime did not resume", cause)
	}
	return cause
}

func (s *Server) stageManagedCodexRuntime(agentID string, plan modelprofiles.PreparedThreadRuntime) (codexRuntimeStage, codexHandoffState) {
	if s != nil && s.stageRuntimeOverride != nil {
		return s.stageRuntimeOverride(agentID, plan)
	}
	if s == nil {
		return codexRuntimeStage{}, codexHandoffState{State: codexHandoffFailed, Message: "daemon unavailable"}
	}
	agentID = strings.TrimSpace(agentID)
	owner := s.modelProfiles()
	if owner == nil || s.watcher == nil {
		return codexRuntimeStage{}, codexHandoffState{State: codexHandoffFailed, Message: "managed Codex runtime staging unavailable"}
	}
	agent := s.lookupAgent(agentID)
	if agent == nil || agent.ProcessID <= 0 {
		return codexRuntimeStage{}, codexHandoffState{State: codexHandoffSkipped, Message: "no live Codex process requires staging"}
	}
	rolloutPath := work.LiveCodexRolloutPath(agent.ProcessID)
	threadID := work.CodexSessionIDFromRolloutPath(rolloutPath)
	if threadID == "" {
		return codexRuntimeStage{}, codexHandoffState{State: codexHandoffFailed, Message: "Codex thread identity is not resolvable"}
	}
	targetCommand, previousCommand, err := owner.CodexResumeCommandsForRuntime(plan, threadID)
	if err != nil {
		return codexRuntimeStage{}, codexHandoffState{State: codexHandoffFailed, Message: err.Error()}
	}
	owner.SetSessionHandoffPending(plan.RouteID, true)
	targetPID, err := switchCodexLane(s.watcher, agentID, agent.ProcessID, targetCommand, previousCommand)
	if err != nil {
		owner.SetSessionHandoffPending(plan.RouteID, false)
		return codexRuntimeStage{}, codexHandoffState{State: codexHandoffFailed, Message: err.Error()}
	}
	return codexRuntimeStage{previousCommand: previousCommand, targetProcessID: targetPID}, codexHandoffState{State: codexHandoffApplied}
}

func (s *Server) compensateManagedCodexRuntime(agentID string, plan modelprofiles.PreparedThreadRuntime, stage codexRuntimeStage, cause error) error {
	if s != nil && s.compensateRuntimeOverride != nil {
		return s.compensateRuntimeOverride(agentID, plan, stage, cause)
	}
	defer s.modelProfiles().SetSessionHandoffPending(plan.RouteID, false)
	if stage.targetProcessID <= 0 {
		return fmt.Errorf("%v; target route was not committed and previous Codex runtime could not be located", cause)
	}
	if _, err := switchCodexLane(s.watcher, agentID, stage.targetProcessID, stage.previousCommand, stage.previousCommand); err != nil {
		return fmt.Errorf("%v; previous Codex runtime compensation failed: %w", cause, err)
	}
	return cause
}

func (s *Server) managedCodexRuntimeNeedsStaging(agentID string, selection modelprofiles.ThreadRuntimeSelection) bool {
	if s == nil || selection.Client != "codex" || !selection.HotSwitchable {
		return false
	}
	agent := s.lookupAgent(agentID)
	return agent != nil && agent.ProcessID > 0
}
