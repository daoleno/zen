package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

// TestPiLiveSubscriptionBindsOwnedTranscriptSnapshotAndDelta reproduces the
// real launch-to-watcher-to-reader gap end to end at the server boundary: a
// delegated Pi session is created with the injected owned --session launch
// command (registered in the watcher), and the conversation subscription must
// snapshot the exact owned transcript and then stream incremental growth as a
// delta with stable event IDs — never a Working-only fallback.
func TestPiLiveSubscriptionBindsOwnedTranscriptSnapshotAndDelta(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(t.TempDir(), "owned.jsonl")
	writePiServerFixture(t, owned, cwd)

	tmuxPath := filepath.Join(binDir, "tmux")
	if err := os.WriteFile(tmuxPath, []byte("#!/bin/sh\nprintf '%s\\n' 'zero-view:@1'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	w := watcher.New(time.Second)
	launchCommand := "pi --session " + owned
	agentID, err := w.CreateSession("", watcher.CreateSessionOptions{
		Detached: true,
		Cwd:      cwd,
		Command:  launchCommand,
		Name:     "Pi live task",
	})
	if err != nil {
		t.Fatal(err)
	}
	agent := w.GetAgent(agentID)
	if agent == nil {
		t.Fatal("fake tmux session was not registered")
	}
	if agent.Command != launchCommand {
		t.Fatalf("registered launch command = %q, want %q", agent.Command, launchCommand)
	}

	srv := &Server{watcher: w}
	conn := openThinProxyTestSocket(t, srv)
	request := clientMessage{
		Type:      "codex_conversation_subscribe",
		RequestID: "subscription-pi-live",
		TargetID:  agentID,
	}
	writeConversationSubscriptionRequest(t, conn, request)

	var snapshot struct {
		Type         string                 `json:"type"`
		RequestID    string                 `json:"request_id"`
		Conversation work.CodexConversation `json:"conversation"`
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := conn.ReadJSON(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Type != "codex_conversation_snapshot" || snapshot.RequestID != request.RequestID {
		t.Fatalf("first response = %#v", snapshot)
	}
	conversation := snapshot.Conversation
	if !conversation.Available || conversation.Reason != "" {
		t.Fatalf("owned transcript must be available, got reason %q", conversation.Reason)
	}
	if conversation.SessionID != "sess-server-live" {
		t.Fatalf("session id = %q, want sess-server-live", conversation.SessionID)
	}
	if conversation.Path != owned || conversation.Source != "pi_session_jsonl" {
		t.Fatalf("source = path %q source %q", conversation.Path, conversation.Source)
	}
	var kinds []string
	for _, event := range conversation.Events {
		kinds = append(kinds, event.Kind)
	}
	wantKinds := []string{"user_message", "reasoning", "assistant_message", "tool_call", "assistant_message"}
	if strings.Join(kinds, ",") != strings.Join(wantKinds, ",") {
		t.Fatalf("snapshot kinds = %v, want %v", kinds, wantKinds)
	}
	if conversation.Activity == nil || conversation.Activity.Status != work.ProviderActivityCompleted {
		t.Fatalf("attached snapshot activity = %+v", conversation.Activity)
	}
	firstUserID := conversation.Events[0].ID
	toolID := ""
	for _, event := range conversation.Events {
		if event.Kind == "tool_call" {
			toolID = event.ID
		}
	}
	if toolID == "" {
		t.Fatal("snapshot tool event missing")
	}

	// Incremental growth: the next poll must stream a delta with only the new
	// event, keeping every settled event ID untouched.
	appendPiServerLines(t, owned, []string{
		`{"type":"message","id":"a3","parentId":"a2","timestamp":"2026-08-07T10:00:10.000Z","message":{"role":"assistant","content":[{"type":"text","text":"incremental text"}],"stopReason":"stop"}}`,
	})
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var delta struct {
		Type      string                        `json:"type"`
		RequestID string                        `json:"request_id"`
		Upserts   []work.CodexConversationEvent `json:"upserts"`
	}
	if err := conn.ReadJSON(&delta); err != nil {
		t.Fatal(err)
	}
	if delta.Type != "codex_conversation_delta" {
		t.Fatalf("second response = %#v, want delta", delta)
	}
	hasIncremental := false
	for _, upsert := range delta.Upserts {
		if upsert.ID == "a3-text" && upsert.Kind == "assistant_message" && upsert.Body == "incremental text" {
			hasIncremental = true
		}
		if upsert.ID == firstUserID {
			t.Fatalf("settled user event re-sent in delta: %#v", upsert)
		}
		if upsert.ID == toolID {
			t.Fatalf("settled tool event re-sent in delta: %#v", upsert)
		}
	}
	if !hasIncremental {
		t.Fatalf("incremental upsert missing: %#v", delta.Upserts)
	}
}

// TestPiLiveSubscriptionAmbiguityFailsClosed pins the fail-closed rule at the
// server boundary: an owned --session path whose header cwd does not match the
// session cwd must never fall back to another transcript.
func TestPiLiveSubscriptionAmbiguityFailsClosed(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	// The owned file declares a different cwd than the session pane.
	foreign := filepath.Join(t.TempDir(), "foreign.jsonl")
	writePiServerFixture(t, foreign, filepath.Join(t.TempDir(), "elsewhere"))

	tmuxPath := filepath.Join(binDir, "tmux")
	if err := os.WriteFile(tmuxPath, []byte("#!/bin/sh\nprintf '%s\\n' 'zero-view:@2'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	w := watcher.New(time.Second)
	agentID, err := w.CreateSession("", watcher.CreateSessionOptions{
		Detached: true,
		Cwd:      cwd,
		Command:  "pi --session " + foreign,
		Name:     "Pi mismatched",
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{watcher: w}
	conn := openThinProxyTestSocket(t, srv)
	request := clientMessage{
		Type:      "codex_conversation_subscribe",
		RequestID: "subscription-pi-ambiguous",
		TargetID:  agentID,
	}
	writeConversationSubscriptionRequest(t, conn, request)

	var response struct {
		Type         string                 `json:"type"`
		Reason       string                 `json:"reason"`
		Conversation work.CodexConversation `json:"conversation"`
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	if response.Type != "codex_conversation_snapshot" {
		t.Fatalf("response = %#v", response)
	}
	if response.Conversation.Available {
		t.Fatalf("ambiguous owned transcript must fail closed: %+v", response.Conversation)
	}
	if response.Conversation.Reason != "transcript_not_found" {
		t.Fatalf("ambiguous reason = %q, want transcript_not_found", response.Conversation.Reason)
	}
}

// TestPiLiveSubscriptionQuotedOwnedPathBinds covers the reviewed P2 at the
// full launch-to-watcher-to-reader chain: a Zen-owned --session path that
// requires shell quoting (space and metacharacters, matching
// EnsurePiSessionLaunchCommand output) must bind the exact transcript through
// watcher metadata and the server subscription.
func TestPiLiveSubscriptionQuotedOwnedPathBinds(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	spaced := filepath.Join(t.TempDir(), "My Zen", "co$t file.jsonl")
	if err := os.MkdirAll(filepath.Dir(spaced), 0o700); err != nil {
		t.Fatal(err)
	}
	writePiServerFixture(t, spaced, cwd)
	// The launcher wraps values containing shell metacharacters in single
	// quotes (work.shellQuoteForLaunch); the fixture path has no embedded
	// apostrophe, so the simple wrap is byte-identical to its output.
	quoted := "'" + spaced + "'"

	tmuxPath := filepath.Join(binDir, "tmux")
	if err := os.WriteFile(tmuxPath, []byte("#!/bin/sh\nprintf '%s\\n' 'zero-view:@3'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	w := watcher.New(time.Second)
	launchCommand := "pi --session " + quoted
	agentID, err := w.CreateSession("", watcher.CreateSessionOptions{
		Detached: true,
		Cwd:      cwd,
		Command:  launchCommand,
		Name:     "Pi quoted live task",
	})
	if err != nil {
		t.Fatal(err)
	}
	agent := w.GetAgent(agentID)
	if agent == nil {
		t.Fatal("fake tmux session was not registered")
	}
	if agent.Command != launchCommand {
		t.Fatalf("registered launch command = %q, want %q", agent.Command, launchCommand)
	}

	srv := &Server{watcher: w}
	conn := openThinProxyTestSocket(t, srv)
	request := clientMessage{
		Type:      "codex_conversation_subscribe",
		RequestID: "subscription-pi-quoted",
		TargetID:  agentID,
	}
	writeConversationSubscriptionRequest(t, conn, request)

	var snapshot struct {
		Type         string                 `json:"type"`
		Conversation work.CodexConversation `json:"conversation"`
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := conn.ReadJSON(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Type != "codex_conversation_snapshot" {
		t.Fatalf("first response = %#v", snapshot)
	}
	if !snapshot.Conversation.Available || snapshot.Conversation.SessionID != "sess-server-live" {
		t.Fatalf("quoted owned transcript = %+v", snapshot.Conversation)
	}
	if snapshot.Conversation.Path != spaced {
		t.Fatalf("quoted owned path = %q, want %q", snapshot.Conversation.Path, spaced)
	}
	if len(snapshot.Conversation.Events) == 0 || snapshot.Conversation.Events[0].Kind != "user_message" {
		t.Fatalf("quoted owned events = %#v", snapshot.Conversation.Events)
	}
}

// TestPiLiveSubscriptionColdReplayAutoBindsOwnedTranscript reproduces the
// real pre-durable-binding scenario at the server boundary: the window was
// created before the @zen_agent_pi_session option existed (or the option was
// lost), the node-based Pi rewrites its argv to bare "pi", and a daemon
// restart re-discovers the window with no recoverable launch binding. The
// subscription must auto-bind the exact Zen-owned transcript for the cwd via
// the startedAt window and snapshot messages plus reasoning and tool calls
// with stable event IDs — never an empty transcript_not_found Interface
// while the authoritative JSONL exists and is fresh.
func TestPiLiveSubscriptionColdReplayAutoBindsOwnedTranscript(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	// The owned transcript lives in the Zen-owned sessions directory under
	// this HOME, exactly like a real pre-fix launch would have placed it.
	ownedDir := filepath.Join(home, ".zen", "provider-sessions", "pi")
	if err := os.MkdirAll(ownedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(ownedDir, "cold-replay.jsonl")
	writePiServerFixture(t, owned, cwd)

	tmuxPath := filepath.Join(binDir, "tmux")
	if err := os.WriteFile(tmuxPath, []byte("#!/bin/sh\nprintf '%s\\n' 'zero-view:@1'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Run 1: the daemon creates the session with the injected owned launch
	// command; markCreatedSession writes no durable binding in this shape.
	w1 := watcher.New(time.Second)
	launchCommand := "pi --session " + owned
	agentID, err := w1.CreateSession("", watcher.CreateSessionOptions{
		Detached: true,
		Cwd:      cwd,
		Command:  launchCommand,
		Name:     "Pi live task",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Run 2: daemon restart. The fresh watcher has no launch record and the
	// window carries NO durable binding (pre-fix shape); the pane command is
	// the argv-rewritten bare "pi". The pane process start time must still
	// fall inside the owned transcript's created-at window.
	w2 := watcher.New(10 * time.Millisecond)
	restore := w2.SetPollSources(watcher.PollSources{
		ListWindows: func() ([]watcher.PollWindow, error) {
			return []watcher.PollWindow{{
				Target:  agentID,
				Name:    "pi",
				Cwd:     cwd,
				Command: "pi",
				PanePID: 500,
			}}, nil
		},
		CapturePane: func(target string) (string, bool, int) {
			return "pi v0.73.1\nworking\n", true, -1
		},
		SnapshotProcesses: func() map[int]watcher.PollProcess {
			return map[int]watcher.PollProcess{
				500: {PID: 500, PPID: 1, PGID: 500, TPGID: 500, StartedAt: time.Date(2026, 8, 7, 10, 0, 2, 0, time.UTC), Comm: "pi", Args: "pi"},
			}
		},
	})
	defer restore()
	restartCtx, cancelRestart := context.WithCancel(context.Background())
	defer cancelRestart()
	go func() {
		for {
			select {
			case <-restartCtx.Done():
				return
			case <-w2.Events():
			}
		}
	}()
	go func() { _ = w2.Run(restartCtx) }()
	agent := waitForWatcherAgent(t, w2, agentID)
	if agent.Command != "pi" {
		t.Fatalf("pre-fix window command = %q, want bare pi", agent.Command)
	}
	if agent.StartedAt.UTC() != time.Date(2026, 8, 7, 10, 0, 2, 0, time.UTC) {
		t.Fatalf("startedAt not recovered from process table: %v", agent.StartedAt)
	}

	srv := &Server{watcher: w2}
	conn := openThinProxyTestSocket(t, srv)
	request := clientMessage{
		Type:      "codex_conversation_subscribe",
		RequestID: "subscription-pi-cold-replay",
		TargetID:  agentID,
	}
	writeConversationSubscriptionRequest(t, conn, request)

	var snapshot struct {
		Type         string                 `json:"type"`
		Conversation work.CodexConversation `json:"conversation"`
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := conn.ReadJSON(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Type != "codex_conversation_snapshot" {
		t.Fatalf("first response = %#v", snapshot)
	}
	if !snapshot.Conversation.Available || snapshot.Conversation.Reason != "" {
		t.Fatalf("cold replay must auto-bind the owned transcript, got %+v", snapshot.Conversation)
	}
	if snapshot.Conversation.SessionID != "sess-server-live" || snapshot.Conversation.Path != owned {
		t.Fatalf("cold replay rebound wrong transcript: %+v", snapshot.Conversation)
	}
	if len(snapshot.Conversation.Events) == 0 {
		t.Fatal("cold replay produced an empty Interface history")
	}
	var kinds []string
	for _, event := range snapshot.Conversation.Events {
		kinds = append(kinds, event.Kind)
	}
	wantKinds := []string{"user_message", "reasoning", "assistant_message", "tool_call", "assistant_message"}
	if strings.Join(kinds, ",") != strings.Join(wantKinds, ",") {
		t.Fatalf("cold replay kinds = %v, want %v", kinds, wantKinds)
	}
	if snapshot.Conversation.Events[0].ID != "u1" {
		t.Fatalf("first event id = %q, want stable u1", snapshot.Conversation.Events[0].ID)
	}
}

// writePiServerFixture writes a realistic version-3 Pi session with user text,
// reasoning, assistant text, one tool call with result, and a final stop.
func writePiServerFixture(t *testing.T, path, cwd string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	content := strings.Join([]string{
		`{"type":"session","version":3,"id":"sess-server-live","timestamp":"2026-08-07T10:00:00.000Z","cwd":"` + cwd + `"}`,
		`{"type":"message","id":"u1","parentId":null,"timestamp":"2026-08-07T10:00:01.000Z","message":{"role":"user","content":"server user text"}}`,
		`{"type":"message","id":"a1","parentId":"u1","timestamp":"2026-08-07T10:00:02.000Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"server reasoning"},{"type":"text","text":"server text"},{"type":"toolCall","id":"call_1","name":"bash","arguments":{"command":"echo hi"}}],"stopReason":"toolUse"}}`,
		`{"type":"message","id":"r1","parentId":"a1","timestamp":"2026-08-07T10:00:03.000Z","message":{"role":"toolResult","toolCallId":"call_1","toolName":"bash","content":[{"type":"text","text":"server tool output"}],"isError":false}}`,
		`{"type":"message","id":"a2","parentId":"r1","timestamp":"2026-08-07T10:00:04.000Z","message":{"role":"assistant","content":[{"type":"text","text":"server final"}],"stopReason":"stop"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendPiServerLines(t *testing.T, path string, lines []string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		t.Fatal(err)
	}
}

// waitForWatcherAgent polls the watcher snapshot until the agent is
// rediscovered, failing the test after a bounded wait.
func waitForWatcherAgent(t *testing.T, w *watcher.Watcher, agentID string) *classifier.Agent {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if agent := w.GetAgent(agentID); agent != nil {
			return agent
		}
		if time.Now().After(deadline) {
			t.Fatalf("agent %q was not rediscovered within 3s", agentID)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestPiLiveSubscriptionRebindsOwnedTranscriptAcrossDaemonRestart reproduces
// the restart/reopen path end to end: run 1 creates the delegated Pi session
// with the injected owned --session launch command; the daemon then restarts
// (a fresh watcher with no in-memory launch record), and the window is
// re-discovered from tmux where the durable @zen_agent_pi_session ownership
// binding survives while the pi process rewrites its argv to bare "pi". The
// new subscription must snapshot the exact same transcript history with
// stable event IDs — never a transcript_not_found empty state.
func TestPiLiveSubscriptionRebindsOwnedTranscriptAcrossDaemonRestart(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(t.TempDir(), "owned.jsonl")
	writePiServerFixture(t, owned, cwd)

	tmuxPath := filepath.Join(binDir, "tmux")
	if err := os.WriteFile(tmuxPath, []byte("#!/bin/sh\nprintf '%s\\n' 'zero-view:@1'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Run 1: the daemon creates the session with the owned launch command.
	w1 := watcher.New(time.Second)
	launchCommand := "pi --session " + owned
	agentID, err := w1.CreateSession("", watcher.CreateSessionOptions{
		Detached: true,
		Cwd:      cwd,
		Command:  launchCommand,
		Name:     "Pi live task",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Run 2: daemon restart. The fresh watcher has no launch record; the
	// window comes back from tmux with the durable Pi ownership binding and
	// the argv-rewritten pi process (pane command "pi").
	w2 := watcher.New(10 * time.Millisecond)
	restore := w2.SetPollSources(watcher.PollSources{
		ListWindows: func() ([]watcher.PollWindow, error) {
			return []watcher.PollWindow{{
				Target:           agentID,
				Name:             "pi",
				Cwd:              cwd,
				Command:          "pi",
				PiSessionBinding: watcher.EncodePiSessionBinding("--session", owned),
				PanePID:          500,
			}}, nil
		},
		CapturePane: func(target string) (string, bool, int) {
			return "pi v0.73.1\nworking\n", true, -1
		},
		SnapshotProcesses: func() map[int]watcher.PollProcess {
			return map[int]watcher.PollProcess{
				500: {PID: 500, PPID: 1, PGID: 500, TPGID: 500, StartedAt: time.Now().Add(-time.Hour), Comm: "pi", Args: "pi"},
			}
		},
	})
	defer restore()
	restartCtx, cancelRestart := context.WithCancel(context.Background())
	defer cancelRestart()
	go func() {
		for {
			select {
			case <-restartCtx.Done():
				return
			case <-w2.Events():
			}
		}
	}()
	go func() { _ = w2.Run(restartCtx) }()
	agent := waitForWatcherAgent(t, w2, agentID)
	if agent.Command != launchCommand {
		t.Fatalf("restart lost owned launch command: %q, want %q", agent.Command, launchCommand)
	}

	srv := &Server{watcher: w2}
	conn := openThinProxyTestSocket(t, srv)
	request := clientMessage{
		Type:      "codex_conversation_subscribe",
		RequestID: "subscription-pi-restart",
		TargetID:  agentID,
	}
	writeConversationSubscriptionRequest(t, conn, request)

	var snapshot struct {
		Type         string                 `json:"type"`
		Conversation work.CodexConversation `json:"conversation"`
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := conn.ReadJSON(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Type != "codex_conversation_snapshot" {
		t.Fatalf("first response = %#v", snapshot)
	}
	if !snapshot.Conversation.Available || snapshot.Conversation.Reason != "" {
		t.Fatalf("restart must rebind the owned transcript, got %+v", snapshot.Conversation)
	}
	if snapshot.Conversation.SessionID != "sess-server-live" || snapshot.Conversation.Path != owned {
		t.Fatalf("restart rebound wrong transcript: %+v", snapshot.Conversation)
	}
	if len(snapshot.Conversation.Events) == 0 {
		t.Fatal("restart/reopen produced an empty Interface history")
	}
	if snapshot.Conversation.Events[0].ID != "u1" {
		t.Fatalf("first event id = %q, want stable u1", snapshot.Conversation.Events[0].ID)
	}
}

// TestPiLiveSubscriptionUnavailableLoadNeverErasesHistory pins the
// never-erase invariant at the server boundary: while a subscription is live,
// a transient unavailable load (transcript temporarily missing) must not
// replace the served history with an empty snapshot or a delete-everything
// delta. The next poll after the source returns streams a delta with only the
// new event, keeping every settled event ID untouched.
func TestPiLiveSubscriptionUnavailableLoadNeverErasesHistory(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(t.TempDir(), "owned.jsonl")
	writePiServerFixture(t, owned, cwd)
	hidden := owned + ".hidden"

	tmuxPath := filepath.Join(binDir, "tmux")
	if err := os.WriteFile(tmuxPath, []byte("#!/bin/sh\nprintf '%s\\n' 'zero-view:@1'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	w := watcher.New(time.Second)
	agentID, err := w.CreateSession("", watcher.CreateSessionOptions{
		Detached: true,
		Cwd:      cwd,
		Command:  "pi --session " + owned,
		Name:     "Pi live task",
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{watcher: w}
	conn := openThinProxyTestSocket(t, srv)
	request := clientMessage{
		Type:      "codex_conversation_subscribe",
		RequestID: "subscription-pi-never-erase",
		TargetID:  agentID,
	}
	writeConversationSubscriptionRequest(t, conn, request)

	var snapshot struct {
		Type         string                 `json:"type"`
		Conversation work.CodexConversation `json:"conversation"`
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := conn.ReadJSON(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Type != "codex_conversation_snapshot" || len(snapshot.Conversation.Events) == 0 {
		t.Fatalf("initial snapshot = %#v", snapshot)
	}
	firstUserID := snapshot.Conversation.Events[0].ID

	// The transcript temporarily disappears (mid-flush rename, mtime flap,
	// binding loss): hold for more than two full poll cycles so at least one
	// poll must have observed the unavailable load. Then the source returns
	// with one new event. The very next message on the wire must be the
	// incremental delta — never an empty snapshot or a delete-everything
	// delta — so the served history is provably untouched end to end.
	if err := os.Rename(owned, hidden); err != nil {
		t.Fatal(err)
	}
	time.Sleep(codexConversationSubscriptionInterval*2 + 100*time.Millisecond)

	if err := os.Rename(hidden, owned); err != nil {
		t.Fatal(err)
	}
	appendPiServerLines(t, owned, []string{
		`{"type":"message","id":"a3","parentId":"a2","timestamp":"2026-08-07T10:00:10.000Z","message":{"role":"assistant","content":[{"type":"text","text":"incremental text"}],"stopReason":"stop"}}`,
	})
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var delta struct {
		Type      string                        `json:"type"`
		Upserts   []work.CodexConversationEvent `json:"upserts"`
		Deletes   []string                      `json:"deletes"`
		Available bool                          `json:"available"`
	}
	if err := conn.ReadJSON(&delta); err != nil {
		t.Fatal(err)
	}
	if delta.Type != "codex_conversation_delta" {
		t.Fatalf("unavailable window must not emit an empty replacement; first message after restore = %#v", delta)
	}
	if len(delta.Deletes) != 0 {
		t.Fatalf("restored source deleted settled history: %v", delta.Deletes)
	}
	hasIncremental := false
	for _, upsert := range delta.Upserts {
		if upsert.ID == "a3-text" && upsert.Kind == "assistant_message" && upsert.Body == "incremental text" {
			hasIncremental = true
		}
		if upsert.ID == firstUserID {
			t.Fatalf("settled user event re-sent after unavailable hold: %#v", upsert)
		}
	}
	if !hasIncremental {
		t.Fatalf("incremental upsert missing after restore: %#v", delta.Upserts)
	}
}
