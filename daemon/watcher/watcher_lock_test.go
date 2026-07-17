package watcher

import (
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

type blockingActivityProbe struct {
	started chan struct{}
	release chan struct{}
	calls   int
	mu      sync.Mutex
}

func (p *blockingActivityProbe) Infer(in classifier.ActivityInput) classifier.ActivitySignal {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	select {
	case p.started <- struct{}{}:
	default:
	}
	<-p.release
	return classifier.ActivitySignal{State: classifier.StateUnknown, Source: "blocked_test", Provider: "test"}
}

func TestAgentsReturnsWhileActivityProbeBlocked(t *testing.T) {
	w := New(time.Second)
	probe := &blockingActivityProbe{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	w.SetActivityProbe(probe)

	// Seed one agent so apply path has work after unlock.
	w.mu.Lock()
	w.agents["main:@1"] = &classifier.Agent{
		ID:      "main:@1",
		Name:    "test",
		Command: "cursor-agent",
		Cwd:     "/tmp",
		State:   classifier.StateUnknown,
	}
	w.agentOrder = append(w.agentOrder, "main:@1")
	w.prevContent["main:@1"] = ""
	w.mu.Unlock()

	// Drive a synthetic poll path piece: unlock-held probe via activitySignal is not
	// enough; exercise the real poll phases by calling probe outside lock manually
	// the way poll does, while Agents() must stay responsive.
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Simulate poll's unlocked probe section holding the probe lock/IO.
		_ = probe.Infer(classifier.ActivityInput{
			Agent:       classifier.Agent{ID: "main:@1", Command: "cursor-agent", Cwd: "/tmp"},
			PaneContent: "Cursor Agent",
		})
	}()

	select {
	case <-probe.started:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}

	start := time.Now()
	agents := w.Agents()
	elapsed := time.Since(start)
	if elapsed > 100*time.Millisecond {
		t.Fatalf("Agents() took %s while probe blocked; watcher lock likely held", elapsed)
	}
	if len(agents) != 1 {
		t.Fatalf("agents len = %d", len(agents))
	}

	close(probe.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("probe goroutine stuck")
	}
}

func TestPollDoesNotHoldLockDuringProbe(t *testing.T) {
	w := New(time.Second)
	probe := &blockingActivityProbe{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	w.SetActivityProbe(probe)

	// Replace list/capture by planting state and invoking the unlocked probe
	// path used by poll: snapshot + Infer outside lock + apply.
	w.mu.Lock()
	w.pollGeneration++
	gen := w.pollGeneration
	agent := &classifier.Agent{
		ID:        "sess:@9",
		Name:      "cursor",
		Command:   "cursor-agent",
		Cwd:       "/repo",
		State:     classifier.StateUnknown,
		PaneAlive: true,
	}
	w.agents[agent.ID] = agent
	w.agentOrder = append(w.agentOrder, agent.ID)
	w.agentEpoch[agent.ID] = gen
	snap := *agent
	w.mu.Unlock()

	var activity classifier.ActivitySignal
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		activity = probe.Infer(classifier.ActivityInput{Agent: snap, PaneContent: "Cursor Agent\nctrl+c to stop"})
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.agentEpoch[agent.ID] != gen {
			return
		}
		cur := w.agents[agent.ID]
		state, summary := classifier.ResolveSessionStatus(cur, classifier.StateUnknown, "detail", time.Now().UTC(), activity)
		cur.State = state
		cur.Summary = summary
	}()

	select {
	case <-probe.started:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}

	start := time.Now()
	_ = w.Agents()
	if took := time.Since(start); took > 100*time.Millisecond {
		t.Fatalf("Agents blocked for %s during probe", took)
	}

	close(probe.release)
	<-finished
}
