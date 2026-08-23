package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
	"github.com/gorilla/websocket"
)

type brainServiceTestWatcher struct {
	sessions  map[string]*classifier.Agent
	turnStore *brain.Store
}

func (w *brainServiceTestWatcher) Agents() []*classifier.Agent {
	return nil
}

func (w *brainServiceTestWatcher) GetAgent(id string) *classifier.Agent {
	if w.sessions == nil {
		return nil
	}
	return w.sessions[id]
}

func (w *brainServiceTestWatcher) HasSession(target string) bool {
	presence, err := w.ProbeSession(target)
	return err == nil && presence == watcher.SessionPresencePresent
}

func (w *brainServiceTestWatcher) ProbeSession(target string) (watcher.SessionPresence, error) {
	if w.sessions == nil {
		return watcher.SessionPresenceAbsent, nil
	}
	if _, ok := w.sessions[target]; ok {
		return watcher.SessionPresencePresent, nil
	}
	return watcher.SessionPresenceAbsent, nil
}

func (w *brainServiceTestWatcher) CreateSession(string, watcher.CreateSessionOptions) (string, error) {
	return "", nil
}

func (w *brainServiceTestWatcher) SendInput(string, string) error {
	return nil
}

func (w *brainServiceTestWatcher) SendInputWhenReady(string, string, string) error {
	return nil
}

func (w *brainServiceTestWatcher) SendInputWithReceiptResult(_, _, receipt string) (watcher.InputResult, error) {
	return watcher.InputResult{Outcome: watcher.InputAccepted, Receipt: receipt}, nil
}
func (w *brainServiceTestWatcher) SubmitBrainHostInput(sessionID, payload, eventID, claimToken, workID, providerTurnID string, acceptedAt time.Time) (watcher.InputResult, error) {
	if w.turnStore == nil {
		return watcher.InputResult{Outcome: watcher.InputAccepted, Receipt: eventID, TurnID: providerTurnID}, nil
	}
	existingTurnID := ""
	if current, found, err := w.turnStore.Turn(sessionID); err != nil {
		return watcher.InputResult{Outcome: watcher.InputNotSubmitted, Receipt: eventID, TurnID: providerTurnID}, err
	} else if found {
		existingTurnID = current.TurnID
		if !watcher.TurnImmutable(current.Status) {
			settledAt := time.Now().UTC()
			if _, _, err := w.turnStore.ApplyTurnFact(watcher.TurnFact{
				SessionID: current.SessionID, TurnID: current.TurnID,
				Class: watcher.EvidenceProvider, Kind: "done", Bound: true,
				SourceID:  "provider\x00test-host\x00" + current.TurnID + "\x00done",
				Admission: current.Admission, ActivityID: current.ActivityID,
				StartedAt: current.AcceptedAt, SettledAt: settledAt, At: settledAt,
			}); err != nil {
				return watcher.InputResult{Outcome: watcher.InputNotSubmitted, Receipt: eventID, TurnID: providerTurnID}, err
			}
		}
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	pending, created, err := w.turnStore.PrepareInputAdmission(watcher.InputAdmission{
		WorkID: workID, SessionID: sessionID, ProposedTurnID: providerTurnID,
		Receipt: eventID, ClaimToken: claimToken, PayloadSHA256: digest,
		ProcessIdentity: "host-process-identity", PaneGeneration: "host-pane-generation",
		AcceptedAt: acceptedAt.UTC(), Mode: watcher.InputAdmissionFresh, ExistingTurnID: existingTurnID,
	})
	if err != nil {
		return watcher.InputResult{Outcome: watcher.InputNotSubmitted, Receipt: eventID, TurnID: providerTurnID}, err
	}
	if !created {
		return watcher.InputResult{Outcome: watcher.InputAmbiguous, Receipt: eventID, TurnID: providerTurnID},
			fmt.Errorf("Host submission was not freshly prepared")
	}
	resolvedAt := acceptedAt.Add(time.Millisecond).UTC()
	resolved, err := w.turnStore.ResolveInputAdmission(watcher.InputAdmissionResolution{
		SessionID: sessionID, ProposedTurnID: providerTurnID, Receipt: eventID,
		PayloadSHA256: pending.PayloadSHA256, ActivityID: "host-activity-" + providerTurnID,
		Admission: watcher.TurnAdmission{
			Stream: "provider", ID: "host-admission-" + providerTurnID, Cursor: 1,
			SHA256: pending.PayloadSHA256, At: resolvedAt,
		},
		ResolvedAt: resolvedAt,
	})
	if err != nil {
		return watcher.InputResult{Outcome: watcher.InputAmbiguous, Receipt: eventID, TurnID: providerTurnID}, err
	}
	return watcher.InputResult{Outcome: watcher.InputAccepted, Receipt: eventID, TurnID: resolved.ResolvedTurnID}, nil
}
func (w *brainServiceTestWatcher) InputReceiptResult(_, receipt string) (watcher.InputResult, bool, error) {
	return watcher.InputResult{Outcome: watcher.InputNotSubmitted, Receipt: receipt}, false, nil
}

func (w *brainServiceTestWatcher) KillSession(string) error {
	return nil
}

func (w *brainServiceTestWatcher) CapturePaneContent(string) (string, error) {
	return "", nil
}

func (w *brainServiceTestWatcher) ProbeProviderEvidence(string) (watcher.ProviderActivityObservation, bool, error) {
	return watcher.ProviderActivityObservation{}, false, nil
}

func (w *brainServiceTestWatcher) ResolveOwnedGeneration(sessionID string) (watcher.OwnedGeneration, error) {
	if w.GetAgent(sessionID) == nil {
		return watcher.OwnedGeneration{}, fmt.Errorf("Session %s is unavailable", sessionID)
	}
	return watcher.OwnedGeneration{
		SessionID:  sessionID,
		Generation: "test-owned-generation",
	}, nil
}

func (w *brainServiceTestWatcher) ResolveBrainHostGeneration(sessionID string) (watcher.OwnedGeneration, error) {
	return w.ResolveOwnedGeneration(sessionID)
}

// killTrackingWatcher adds CreateSession and KillSession tracking needed for
// snapshot/live delegated coverage.
type killTrackingWatcher struct {
	brainServiceTestWatcher
	killed  []string
	created int
}

func (w *killTrackingWatcher) CreateSession(_ string, opts watcher.CreateSessionOptions) (string, error) {
	if w.sessions == nil {
		w.sessions = map[string]*classifier.Agent{}
	}
	w.created++
	id := fmt.Sprintf("brain-agent-host:@%d", w.created)
	w.sessions[id] = &classifier.Agent{
		ID:      id,
		Name:    opts.Name,
		Cwd:     opts.Cwd,
		Command: opts.Command,
		State:   classifier.StateRunning,
		Hidden:  opts.Hidden,
	}
	return id, nil
}

func (w *killTrackingWatcher) KillSession(sessionID string) error {
	w.killed = append(w.killed, sessionID)
	delete(w.sessions, sessionID)
	return nil
}

func TestHandleSetDelegatedExecutor(t *testing.T) {
	writeExecutors := func(t *testing.T, dir string) string {
		t.Helper()
		path := filepath.Join(dir, "executors.toml")
		if err := os.WriteFile(path, []byte(`delegated_executor = "codex"

[[executors]]
name = "codex"
command = "codex"

[[executors]]
name = "grok"
command = "grok --live"
`), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	t.Run("persists_live_and_returns_fresh_snapshot", func(t *testing.T) {
		dir := t.TempDir()
		path := writeExecutors(t, dir)
		execs, err := work.LoadExecutors(path)
		if err != nil {
			t.Fatal(err)
		}
		store, err := brain.NewStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		existingID := "brain-agent-worker:@9"
		fw := &killTrackingWatcher{
			brainServiceTestWatcher: brainServiceTestWatcher{
				sessions: map[string]*classifier.Agent{
					existingID: {
						ID:      existingID,
						Name:    "Worker",
						Command: "codex",
						State:   classifier.StateRunning,
					},
				},
			},
		}
		srv := &Server{
			brain: brain.NewService(store, fw, execs),
			execs: execs,
		}
		conn := openThinProxyTestSocket(t, srv)

		payload := writeAndReadJSON(t, conn, clientMessage{
			Type:       "set_delegated_executor",
			RequestID:  "delegated-1",
			ExecutorID: "grok",
		})
		if payload["type"] != "brain_snapshot" || payload["request_id"] != "delegated-1" {
			t.Fatalf("response = %#v", payload)
		}
		brainRaw, _ := json.Marshal(payload["brain"])
		var snapshot map[string]any
		if err := json.Unmarshal(brainRaw, &snapshot); err != nil {
			t.Fatal(err)
		}
		delegated, _ := snapshot["delegated_adapter"].(map[string]any)
		if delegated == nil {
			delegated, _ = snapshot["delegated_executor"].(map[string]any)
		}
		if delegated["id"] != "grok" {
			t.Fatalf("delegated = %#v", delegated)
		}
		if execs.GetDelegatedExecutor() != "grok" {
			t.Fatalf("live owner = %q", execs.GetDelegatedExecutor())
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `delegated_executor = "grok"`) {
			t.Fatalf("persisted file = %s", raw)
		}
		if len(fw.killed) != 0 {
			t.Fatalf("existing sessions migrated: killed=%v", fw.killed)
		}
		if fw.GetAgent(existingID) == nil {
			t.Fatal("existing ordinary session was removed")
		}
	})

	for _, tc := range []struct {
		name       string
		envLock    string
		executorID string
		requestID  string
		wantCode   string
		wantLive   string
	}{
		{
			name:       "invalid",
			executorID: "missing-cli",
			requestID:  "bad-1",
			wantCode:   "invalid_executor",
			wantLive:   "codex",
		},
		{
			name:       "env_locked",
			envLock:    "grok",
			executorID: "codex",
			requestID:  "lock-1",
			wantCode:   "delegated_executor_locked_by_env",
			wantLive:   "grok",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envLock != "" {
				t.Setenv("ZEN_DELEGATED_EXECUTOR", tc.envLock)
			}
			path := writeExecutors(t, t.TempDir())
			execs, err := work.LoadExecutors(path)
			if err != nil {
				t.Fatal(err)
			}
			store, err := brain.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			srv := &Server{
				brain: brain.NewService(store, &killTrackingWatcher{}, execs),
				execs: execs,
			}
			conn := openThinProxyTestSocket(t, srv)
			payload := writeAndReadJSON(t, conn, clientMessage{
				Type:       "set_delegated_executor",
				RequestID:  tc.requestID,
				ExecutorID: tc.executorID,
			})
			if payload["type"] != "error" || payload["code"] != tc.wantCode || payload["request_id"] != tc.requestID {
				t.Fatalf("response = %#v", payload)
			}
			if execs.GetDelegatedExecutor() != tc.wantLive {
				t.Fatalf("live owner = %q, want %q", execs.GetDelegatedExecutor(), tc.wantLive)
			}
		})
	}
}

func writeAndReadJSON(t *testing.T, conn *websocket.Conn, request clientMessage) map[string]any {
	t.Helper()
	if err := conn.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}
