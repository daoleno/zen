package server

import (
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/classifier"
)

func TestDelegatedLifecycleWakesAndClosesDoneAgent(t *testing.T) {
	now := time.Date(2026, 6, 7, 10, 0, 0, 0, time.UTC)
	var wakes []brain.HeartbeatEvent
	var closed []string
	manager := newDelegatedLifecycleManager(
		func(event brain.HeartbeatEvent) (bool, error) {
			wakes = append(wakes, event)
			return true, nil
		},
		func(agentID string) error {
			closed = append(closed, agentID)
			return nil
		},
	)
	manager.now = func() time.Time { return now }
	manager.doneCloseAfter = time.Second

	agent := &classifier.Agent{
		ID:        "brain-agent-worker:@1",
		Name:      "Worker",
		State:     classifier.StateDone,
		Summary:   "complete",
		Delegated: true,
		LastLines: []string{"complete"},
	}
	manager.Observe(agent, false)

	if len(wakes) != 1 {
		t.Fatalf("wakes = %#v", wakes)
	}
	if wakes[0].Reason != "delegated_agent_done" || wakes[0].Status != string(classifier.StateDone) {
		t.Fatalf("wake event = %#v", wakes[0])
	}
	if len(closed) != 0 {
		t.Fatalf("closed before ttl = %#v", closed)
	}

	now = now.Add(500 * time.Millisecond)
	manager.Observe(agent, false)
	if len(wakes) != 1 || len(closed) != 0 {
		t.Fatalf("mid-ttl wakes=%#v closed=%#v", wakes, closed)
	}

	now = now.Add(500 * time.Millisecond)
	manager.Observe(agent, false)
	if len(closed) != 1 || closed[0] != agent.ID {
		t.Fatalf("closed after ttl = %#v", closed)
	}
}

func TestDelegatedLifecycleRecognizesCodexIdleCompletion(t *testing.T) {
	now := time.Date(2026, 6, 7, 10, 0, 0, 0, time.UTC)
	var wakes []brain.HeartbeatEvent
	var closed []string
	manager := newDelegatedLifecycleManager(
		func(event brain.HeartbeatEvent) (bool, error) {
			wakes = append(wakes, event)
			return true, nil
		},
		func(agentID string) error {
			closed = append(closed, agentID)
			return nil
		},
	)
	manager.now = func() time.Time { return now }
	manager.idleCloseAfter = time.Second

	agent := &classifier.Agent{
		ID:         "brain-agent-smoke:@2",
		Name:       "Smoke",
		State:      classifier.StateUnknown,
		Summary:    "No new output",
		Delegated:  true,
		StaleCount: 30,
		LastLines: []string{
			"› Delegated smoke test. Do not edit files.",
			"• DELEGATE_SMOKE_OK",
			"› Find and fix a bug in @filename",
		},
	}
	manager.Observe(agent, false)

	if len(wakes) != 1 {
		t.Fatalf("wakes = %#v", wakes)
	}
	if wakes[0].Reason != "delegated_agent_idle_completion" {
		t.Fatalf("wake reason = %q", wakes[0].Reason)
	}
	if wakes[0].Summary != "DELEGATE_SMOKE_OK" {
		t.Fatalf("wake summary = %q", wakes[0].Summary)
	}
	if len(closed) != 0 {
		t.Fatalf("closed before ttl = %#v", closed)
	}

	now = now.Add(time.Second)
	manager.Observe(agent, false)
	if len(closed) != 1 || closed[0] != agent.ID {
		t.Fatalf("closed after ttl = %#v", closed)
	}
}

func TestDelegatedLifecycleDoesNotCloseUnsafeStates(t *testing.T) {
	now := time.Date(2026, 6, 7, 10, 0, 0, 0, time.UTC)
	var wakes []brain.HeartbeatEvent
	var closed []string
	manager := newDelegatedLifecycleManager(
		func(event brain.HeartbeatEvent) (bool, error) {
			wakes = append(wakes, event)
			return true, nil
		},
		func(agentID string) error {
			closed = append(closed, agentID)
			return nil
		},
	)
	manager.now = func() time.Time { return now }
	manager.doneCloseAfter = time.Nanosecond
	manager.idleCloseAfter = time.Nanosecond

	agents := []*classifier.Agent{
		{
			ID:        "brain-agent-blocked:@1",
			State:     classifier.StateBlocked,
			Delegated: true,
			LastLines: []string{"Do you want to proceed?"},
		},
		{
			ID:        "brain-agent-failed:@1",
			State:     classifier.StateFailed,
			Delegated: true,
			LastLines: []string{"FAILED"},
		},
		{
			ID:        "main:@1",
			State:     classifier.StateDone,
			Delegated: false,
			LastLines: []string{"complete"},
		},
		{
			ID:        "brain-agent-hidden:@1",
			State:     classifier.StateDone,
			Delegated: true,
			Hidden:    true,
			LastLines: []string{"complete"},
		},
		{
			ID:         "brain-agent-prompt-only:@1",
			State:      classifier.StateUnknown,
			Delegated:  true,
			StaleCount: 30,
			LastLines:  []string{"› Find and fix a bug in @filename"},
		},
	}

	for _, agent := range agents {
		manager.Observe(agent, false)
	}
	now = now.Add(time.Second)
	for _, agent := range agents {
		manager.Observe(agent, false)
	}

	if len(wakes) != 0 {
		t.Fatalf("unexpected wakes = %#v", wakes)
	}
	if len(closed) != 0 {
		t.Fatalf("unexpected closes = %#v", closed)
	}
}

func TestDelegatedLifecycleDoesNotCloseBeforeBrainWakeSucceeds(t *testing.T) {
	now := time.Date(2026, 6, 7, 10, 0, 0, 0, time.UTC)
	var wakeAttempts int
	var closed []string
	manager := newDelegatedLifecycleManager(
		func(brain.HeartbeatEvent) (bool, error) {
			wakeAttempts++
			return false, nil
		},
		func(agentID string) error {
			closed = append(closed, agentID)
			return nil
		},
	)
	manager.now = func() time.Time { return now }
	manager.doneCloseAfter = time.Nanosecond

	agent := &classifier.Agent{
		ID:        "brain-agent-worker:@1",
		State:     classifier.StateDone,
		Delegated: true,
		LastLines: []string{"complete"},
	}
	manager.Observe(agent, false)
	now = now.Add(time.Second)
	manager.Observe(agent, false)

	if wakeAttempts != 2 {
		t.Fatalf("wake attempts = %d", wakeAttempts)
	}
	if len(closed) != 0 {
		t.Fatalf("closed before wake succeeded = %#v", closed)
	}
}

func TestDelegatedLifecycleResetsCandidateWhenOutputChanges(t *testing.T) {
	now := time.Date(2026, 6, 7, 10, 0, 0, 0, time.UTC)
	var closed []string
	manager := newDelegatedLifecycleManager(
		func(brain.HeartbeatEvent) (bool, error) { return true, nil },
		func(agentID string) error {
			closed = append(closed, agentID)
			return nil
		},
	)
	manager.now = func() time.Time { return now }
	manager.doneCloseAfter = time.Second

	agent := &classifier.Agent{
		ID:        "brain-agent-worker:@1",
		State:     classifier.StateDone,
		Delegated: true,
		LastLines: []string{"complete v1"},
	}
	manager.Observe(agent, true)

	now = now.Add(900 * time.Millisecond)
	agent.LastLines = []string{"complete v2"}
	manager.Observe(agent, true)

	now = now.Add(200 * time.Millisecond)
	manager.Observe(agent, true)
	if len(closed) != 0 {
		t.Fatalf("candidate did not reset, closed = %#v", closed)
	}

	now = now.Add(800 * time.Millisecond)
	manager.Observe(agent, true)
	if len(closed) != 1 || closed[0] != agent.ID {
		t.Fatalf("closed after reset ttl = %#v", closed)
	}
}
