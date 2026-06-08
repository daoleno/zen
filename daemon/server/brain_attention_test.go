package server

import (
	"strings"
	"testing"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
)

type brainWakeWatcher struct {
	sessions map[string]*classifier.Agent
	sent     []brainWakeSend
}

type brainWakeSend struct {
	sessionID string
	text      string
}

func (w *brainWakeWatcher) Agents() []*classifier.Agent {
	return nil
}

func (w *brainWakeWatcher) GetAgent(id string) *classifier.Agent {
	if w.sessions == nil {
		return nil
	}
	return w.sessions[id]
}

func (w *brainWakeWatcher) HasSession(target string) bool {
	if w.sessions == nil {
		return false
	}
	_, ok := w.sessions[target]
	return ok
}

func (w *brainWakeWatcher) CreateSession(string, watcher.CreateSessionOptions) (string, error) {
	return "", nil
}

func (w *brainWakeWatcher) SendInput(sessionID, text string) error {
	w.sent = append(w.sent, brainWakeSend{sessionID: sessionID, text: text})
	return nil
}

func (w *brainWakeWatcher) SendInputWhenReady(sessionID, _ string, text string) error {
	return w.SendInput(sessionID, text)
}

func (w *brainWakeWatcher) KillSession(string) error {
	return nil
}

func TestMaybeWakeBrainForProgressAttentionMetadata(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &brainWakeWatcher{
		sessions: map[string]*classifier.Agent{
			hostID: {ID: hostID, Name: "Brain", Hidden: true, State: classifier.StateRunning},
		},
	}
	srv := &Server{brain: brain.NewService(store, fw, nil)}

	woke := srv.maybeWakeBrainForSessionEvent(watcher.SessionEvent{
		Type:    "agent_metadata_change",
		AgentID: "brain-agent-worker:@1",
		Agent: &classifier.Agent{
			ID:             "brain-agent-worker:@1",
			Name:           "Worker",
			State:          classifier.StateRunning,
			Summary:        "Need a decision",
			Cwd:            "/repo",
			Phase:          "working",
			Attention:      "user_input",
			NeedsAttention: true,
		},
	})

	if !woke {
		t.Fatal("expected progress attention to wake Brain")
	}
	if len(fw.sent) != 1 || fw.sent[0].sessionID != hostID {
		t.Fatalf("sent = %#v", fw.sent)
	}
	for _, want := range []string{
		"reason: agent_attention",
		"status: running",
		"phase: working",
		"attention: user_input",
		"summary: Need a decision",
	} {
		if !strings.Contains(fw.sent[0].text, want) {
			t.Fatalf("heartbeat missing %q:\n%s", want, fw.sent[0].text)
		}
	}
}

func TestMaybeWakeBrainIgnoresProgressMetadataWithoutAttention(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &brainWakeWatcher{
		sessions: map[string]*classifier.Agent{
			hostID: {ID: hostID, Name: "Brain", Hidden: true, State: classifier.StateRunning},
		},
	}
	srv := &Server{brain: brain.NewService(store, fw, nil)}

	woke := srv.maybeWakeBrainForSessionEvent(watcher.SessionEvent{
		Type:    "agent_metadata_change",
		AgentID: "brain-agent-worker:@1",
		Agent: &classifier.Agent{
			ID:        "brain-agent-worker:@1",
			Name:      "Worker",
			State:     classifier.StateRunning,
			Summary:   "Still working",
			Phase:     "working",
			Attention: "none",
		},
	})

	if woke || len(fw.sent) != 0 {
		t.Fatalf("unexpected wake=%v sent=%#v", woke, fw.sent)
	}
}
