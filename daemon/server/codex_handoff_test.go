package server

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/modelprofiles"
	"github.com/daoleno/zen/daemon/watcher"
)

type fakeCodexHandoffIO struct {
	exitOK      bool
	commandPIDs []int
	sendErrors  map[string]error
	inputs      []string
	controls    []string
}

func (f *fakeCodexHandoffIO) SendControlKey(_ string, key string) error {
	f.controls = append(f.controls, key)
	return nil
}

func (f *fakeCodexHandoffIO) SendInput(_ string, input string) error {
	f.inputs = append(f.inputs, input)
	return f.sendErrors[input]
}

func (f *fakeCodexHandoffIO) WaitForPaneProcessExit(_ string, _ int, _ time.Duration) bool {
	return f.exitOK
}

func (f *fakeCodexHandoffIO) WaitForPaneCommandProcessID(_ string, _ string, _ time.Duration) (int, bool) {
	if len(f.commandPIDs) == 0 {
		return 0, false
	}
	pid := f.commandPIDs[0]
	f.commandPIDs = f.commandPIDs[1:]
	return pid, pid > 0
}

func TestSwitchCodexLaneExitFailureKeepsOldProcess(t *testing.T) {
	io := &fakeCodexHandoffIO{exitOK: false, sendErrors: map[string]error{}}
	_, err := switchCodexLane(io, "s", 42, "target", "previous")
	if err == nil || !strings.Contains(err.Error(), "did not exit") {
		t.Fatalf("err=%v", err)
	}
	if strings.Join(io.inputs, "|") != "/exit\n" {
		t.Fatalf("exit failure must not start another runtime: %#v", io.inputs)
	}
}

func TestSwitchCodexLaneReturnsProvenTargetProcessID(t *testing.T) {
	io := &fakeCodexHandoffIO{
		exitOK:      true,
		commandPIDs: []int{84},
		sendErrors:  map[string]error{},
	}
	pid, err := switchCodexLane(io, "s", 42, "target", "previous")
	if err != nil {
		t.Fatal(err)
	}
	if pid != 84 {
		t.Fatalf("proven target pid=%d", pid)
	}
}

func TestSwitchCodexLaneResumeFailureRestoresPreviousRuntime(t *testing.T) {
	io := &fakeCodexHandoffIO{
		exitOK:      true,
		commandPIDs: []int{0, 43},
		sendErrors:  map[string]error{},
	}
	_, err := switchCodexLane(io, "s", 42, "target", "previous")
	if err == nil || !strings.Contains(err.Error(), "target Codex runtime did not come up") {
		t.Fatalf("err=%v", err)
	}
	if got := strings.Join(io.inputs, "|"); got != "/exit\n|target\n|previous\n" {
		t.Fatalf("resume compensation inputs=%q", got)
	}
}

func TestSwitchCodexLaneStartFailureRestoresPreviousRuntime(t *testing.T) {
	io := &fakeCodexHandoffIO{
		exitOK:      true,
		commandPIDs: []int{43},
		sendErrors:  map[string]error{"target\n": errors.New("send failed")},
	}
	_, err := switchCodexLane(io, "s", 42, "target", "previous")
	if err == nil || !strings.Contains(err.Error(), "could not start target") {
		t.Fatalf("err=%v", err)
	}
	if got := strings.Join(io.inputs, "|"); got != "/exit\n|target\n|previous\n" {
		t.Fatalf("start compensation inputs=%q", got)
	}
}

func TestSetThreadRuntimeCommitFailureCompensatesWithProvenTargetPID(t *testing.T) {
	owner := startProfileOwner(t)
	previousProfile := modelprofiles.Profile{
		ID: "codex-main", Name: "Codex Main", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "acme", ProviderLabel: "Acme",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "up-1",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://gateway.example/v1",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ACME_KEY",
	}
	if _, err := owner.UpsertProfile(previousProfile, 0, true); err != nil {
		t.Fatal(err)
	}
	targetProfile := previousProfile
	targetProfile.ID = "codex-alt"
	targetProfile.Name = "Codex Alt"
	targetProfile.Model = "up-2"
	if _, err := owner.UpsertProfile(targetProfile, 1, true); err != nil {
		t.Fatal(err)
	}

	const agentID = "tmux:@runtime-transaction"
	launch, err := owner.PrepareLaunch(modelprofiles.ExecutorCodex, previousProfile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, persist, err := owner.CommitLaunch(launch.ProvisionalID, agentID); err != nil || !persist.Applied {
		t.Fatalf("commit launch err=%v persist=%#v", err, persist)
	}
	acknowledged, ok := owner.ThreadRuntime(agentID)
	if !ok {
		t.Fatal("missing acknowledged runtime")
	}
	before, ok := owner.Table().Get(agentID)
	if !ok {
		t.Fatal("missing acknowledged route")
	}

	srv := New(nil, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv.SetModelProfiles(owner)
	srv.getAgentOverride = func(id string) *classifier.Agent {
		if id != agentID {
			return nil
		}
		return &classifier.Agent{ID: id, ProcessID: 4101}
	}

	const provenTargetPID = 4202
	stageCalled := false
	srv.stageRuntimeOverride = func(id string, plan modelprofiles.PreparedThreadRuntime) (codexRuntimeStage, codexHandoffState) {
		stageCalled = true
		if id != agentID || plan.Previous.ConnectionID != acknowledged.ConnectionID || plan.Previous.ModelID != before.Binding.ClientModel {
			t.Fatalf("stage plan=%#v acknowledged=%#v", plan, acknowledged)
		}
		owner.SetSessionHandoffPending(plan.RouteID, true)
		current, exists := owner.Table().Get(agentID)
		if !exists {
			t.Fatal("route disappeared during staging")
		}
		_, _, persist, err := owner.ActivateSession(agentID, current.Binding.ProfileID, current.Generation)
		if err != nil || !persist.Applied {
			t.Fatalf("advance generation err=%v persist=%#v", err, persist)
		}
		return codexRuntimeStage{
			previousCommand: "codex resume previous",
			targetProcessID: provenTargetPID,
		}, codexHandoffState{State: codexHandoffApplied}
	}

	compensatedPID := 0
	srv.compensateRuntimeOverride = func(id string, plan modelprofiles.PreparedThreadRuntime, stage codexRuntimeStage, cause error) error {
		if id != agentID || cause == nil {
			t.Fatalf("compensation id=%q cause=%v", id, cause)
		}
		compensatedPID = stage.targetProcessID
		owner.SetSessionHandoffPending(plan.RouteID, false)
		return cause
	}

	_, persist, handoff, err := srv.SetThreadRuntime(agentID, modelprofiles.ThreadRuntimeChoice{
		ConnectionID: targetProfile.ID,
		ModelID:      targetProfile.Model,
	})
	if err != nil || !persist.Applied {
		t.Fatalf("commit err=%v", err)
	}
	if stageCalled || compensatedPID != 0 || handoff.State != codexHandoffSkipped {
		t.Fatalf("staging called=%v compensatedPID=%d handoff=%#v", stageCalled, compensatedPID, handoff)
	}

	after, ok := owner.ThreadRuntime(agentID)
	if !ok || after.ConnectionID != targetProfile.ID || after.ModelID != targetProfile.Model || after.ReasoningEffort != acknowledged.ReasoningEffort {
		t.Fatalf("runtime did not switch: before=%#v after=%#v", acknowledged, after)
	}
	route, ok := owner.Table().Get(agentID)
	if !ok || route.Binding.RouteID != before.Binding.RouteID || route.Binding.ProfileID != targetProfile.ID || route.Binding.UpstreamModel != targetProfile.Model {
		t.Fatalf("route did not switch in place: before=%#v after=%#v", before.Binding, route.Binding)
	}
	if owner.Table().HandoffPending(route.Binding.RouteID) {
		t.Fatal("compensation must restore the prior lane to sendable state")
	}
	binding, flightToken, err := owner.Table().BeginRouteFlight(route.Binding.RouteID)
	if err != nil {
		t.Fatalf("prior runtime is not sendable: %v", err)
	}
	if binding.ProfileID != targetProfile.ID || binding.UpstreamModel != targetProfile.Model {
		t.Fatalf("sendable binding=%#v", binding)
	}
	if err := owner.Table().EndRouteFlight(route.Binding.RouteID, flightToken, false); err != nil {
		t.Fatalf("release sendability probe: %v", err)
	}
}
