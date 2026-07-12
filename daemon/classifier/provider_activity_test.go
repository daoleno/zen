package classifier

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultActivityProbe_AllProvidersRegistered(t *testing.T) {
	probe := DefaultActivityProbe()
	if probe == nil || len(probe.adapters) != 4 {
		t.Fatalf("adapters = %d, want 4", len(probe.adapters))
	}
	names := map[string]bool{}
	for _, adapter := range probe.adapters {
		names[adapter.Name()] = true
	}
	for _, want := range []string{"cursor", "codex", "claude", "grok"} {
		if !names[want] {
			t.Fatalf("missing adapter %q in %#v", want, names)
		}
	}
}

func TestProviderAdapters_OrdinaryShellNoMatch(t *testing.T) {
	probe := DefaultActivityProbe()
	got := probe.Infer(ActivityInput{
		Agent:       Agent{Command: "zsh", Cwd: "/tmp"},
		PaneContent: "$ echo hi\nhi\n$ ls\nfile\n$",
	})
	if got.State != "" || got.Provider != "" {
		t.Fatalf("ordinary shell got %#v, want empty", got)
	}
}

func TestCodexAdapter_RunningIdleStalePaneLease(t *testing.T) {
	home := t.TempDir()
	cwd := "/repo/codex"
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	sessionID := "019aaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	rollout := writeCodexRolloutFixture(t, home, cwd, sessionID, now,
		`{"type":"session_meta","payload":{"cwd":"/repo/codex"}}`,
		`{"type":"event_msg","payload":{"type":"task_started"}}`,
	)
	probe := NewCodexTranscriptProbe().SetHomeDir(home).SetNow(func() time.Time { return now }).SetStaleAfter(20 * time.Minute)
	adapter := NewCodexActivityAdapterWithProbe(probe)

	running := adapter.Infer(ActivityInput{
		Agent:       Agent{ID: "c:@1", Command: "codex", Cwd: cwd, StartedAt: now.Add(-time.Minute)},
		PaneContent: "OpenAI Codex\n› ",
	})
	if running.State != StateRunning || running.Source != "codex_transcript_active" {
		t.Fatalf("running = %#v", running)
	}

	// Close turn.
	appendFileLine(t, rollout, `{"type":"event_msg","payload":{"type":"task_complete"}}`)
	_ = os.Chtimes(rollout, now, now)
	idle := adapter.Infer(ActivityInput{
		Agent:       Agent{ID: "c:@1", Command: "codex", Cwd: cwd},
		PaneContent: "OpenAI Codex\n› ",
	})
	if idle.State != StateUnknown || idle.Source != "codex_idle" {
		t.Fatalf("idle = %#v", idle)
	}

	// Stale open turn: mtime older than staleAfter.
	staleHome := t.TempDir()
	staleRollout := writeCodexRolloutFixture(t, staleHome, cwd, "stale-id", now.Add(-30*time.Minute),
		`{"type":"session_meta","payload":{"cwd":"/repo/codex"}}`,
		`{"type":"event_msg","payload":{"type":"task_started"}}`,
	)
	_ = staleRollout
	staleProbe := NewCodexTranscriptProbe().SetHomeDir(staleHome).SetNow(func() time.Time { return now }).SetStaleAfter(20 * time.Minute)
	staleAdapter := NewCodexActivityAdapterWithProbe(staleProbe)
	stale := staleAdapter.Infer(ActivityInput{
		Agent:       Agent{ID: "c:@stale", Command: "codex", Cwd: cwd},
		PaneContent: "OpenAI Codex",
	})
	if stale.State != StateUnknown {
		t.Fatalf("stale open turn = %#v, want unknown", stale)
	}

	// Pane Working+esc auxiliary when transcript idle/missing.
	paneOnly := NewCodexActivityAdapterWithProbe(nil).Infer(ActivityInput{
		Agent:       Agent{Command: "codex", Cwd: cwd},
		PaneContent: "OpenAI Codex\nWorking...\nesc to interrupt\n",
	})
	if paneOnly.State != StateRunning || paneOnly.Source != "codex_pane_working" {
		t.Fatalf("pane auxiliary = %#v", paneOnly)
	}

	// Lease precedence: activity cannot override active Brain lease via ResolveSessionStatus.
	progressAt := now.Add(-time.Minute)
	leaseUntil := now.Add(2 * time.Minute)
	agent := &Agent{
		PaneAlive:           true,
		State:               StateRunning,
		Summary:             "Brain lease",
		LastProgressAt:      &progressAt,
		ExpectedNextCheckAt: &leaseUntil,
		Command:             "codex",
	}
	got, summary := ResolveSessionStatus(agent, StateUnknown, "noise", now, ActivitySignal{State: StateUnknown, Source: "codex_idle"})
	if got != StateRunning || summary != "Brain lease" {
		t.Fatalf("lease precedence = %q %q", got, summary)
	}
}

func TestClaudeAdapter_ToolAskUserIdleConservative(t *testing.T) {
	home := t.TempDir()
	cwd := "/repo/claude"
	now := time.Now().UTC()
	path := writeClaudeTranscriptFixture(t, home, cwd, "claude-session",
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","stop_reason":"tool_use","content":[{"type":"tool_use","id":"1","name":"Bash","input":{}}]}}`,
	)
	probe := NewClaudeTranscriptProbe().SetHomeDir(home).SetNow(func() time.Time { return now })
	adapter := NewClaudeActivityAdapterWithProbe(probe)
	agent := Agent{ID: "cl:@1", Command: "claude", Cwd: cwd}

	running := adapter.Infer(ActivityInput{Agent: agent, PaneContent: "Claude Code"})
	if running.State != StateRunning || running.Source != "claude_transcript_active" {
		t.Fatalf("tool running = %#v (path %s)", running, path)
	}

	appendFileLine(t, path, `{"type":"assistant","message":{"role":"assistant","stop_reason":"tool_use","content":[{"type":"tool_use","id":"2","name":"AskUserQuestion","input":{}}]}}`)
	_ = os.Chtimes(path, now, now.Add(time.Second))
	blocked := adapter.Infer(ActivityInput{Agent: agent, PaneContent: "Claude Code"})
	if blocked.State != StateBlocked || blocked.Source != "claude_ask_user" {
		t.Fatalf("ask user = %#v", blocked)
	}

	appendFileLine(t, path, `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"2","content":"ok"}]}}`)
	appendFileLine(t, path, `{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}}`)
	_ = os.Chtimes(path, now, now.Add(2*time.Second))
	idle := adapter.Infer(ActivityInput{Agent: agent, PaneContent: "Claude Code"})
	if idle.State != StateUnknown || idle.Source != "claude_idle" {
		t.Fatalf("idle = %#v", idle)
	}

	// Missing transcript → conservative idle.
	missing := NewClaudeActivityAdapterWithProbe(NewClaudeTranscriptProbe().SetHomeDir(t.TempDir())).Infer(ActivityInput{
		Agent:       Agent{Command: "claude", Cwd: "/missing"},
		PaneContent: "Claude Code",
	})
	if missing.State != StateUnknown {
		t.Fatalf("missing transcript = %#v, want idle", missing)
	}
}

func TestGrokAdapter_UpdatesLifecycleIgnoresSummary(t *testing.T) {
	home := t.TempDir()
	cwd := "/repo/grok"
	now := time.Now().UTC()
	sessionID := "019f0000-1111-2222-3333-444444444444"
	updates := writeGrokUpdatesFixture(t, home, cwd, sessionID, now,
		`{"params":{"update":{"sessionUpdate":"user_message_chunk"}}}`,
		`{"params":{"update":{"sessionUpdate":"tool_call"}}}`,
		`{"params":{"update":{"sessionUpdate":"tool_call_update","status":"in_progress"}}}`,
	)
	// Poison summary.updated_at far in the future — must be ignored.
	writeGrokSummaryFixture(t, filepath.Dir(updates), map[string]any{
		"info":       map[string]any{"id": sessionID, "cwd": cwd},
		"updated_at": "2099-01-01T00:00:00Z",
		"created_at": now.Add(-time.Hour).Format(time.RFC3339),
	})

	probe := NewGrokTranscriptProbe().SetHomeDir(home).SetNow(func() time.Time { return now })
	adapter := NewGrokActivityAdapterWithProbe(probe)
	agent := Agent{ID: "g:@1", Command: "grok", Cwd: cwd}

	running := adapter.Infer(ActivityInput{Agent: agent, PaneContent: "grok"})
	if running.State != StateRunning || running.Source != "grok_updates_active" {
		t.Fatalf("running = %#v", running)
	}

	appendFileLine(t, updates, `{"params":{"update":{"sessionUpdate":"turn_completed"}}}`)
	_ = os.Chtimes(updates, now, now.Add(time.Second))
	idle := adapter.Infer(ActivityInput{Agent: agent, PaneContent: "grok"})
	if idle.State != StateUnknown || idle.Source != "grok_idle" {
		t.Fatalf("idle = %#v", idle)
	}

	appendFileLine(t, updates, `{"params":{"update":{"sessionUpdate":"user_message_chunk"}}}`)
	appendFileLine(t, updates, `{"params":{"update":{"sessionUpdate":"task_backgrounded"}}}`)
	_ = os.Chtimes(updates, now, now.Add(2*time.Second))
	bg := adapter.Infer(ActivityInput{Agent: agent, PaneContent: "grok"})
	if bg.State != StateUnknown {
		t.Fatalf("task_backgrounded = %#v, want idle", bg)
	}
}

func TestCursorAdapter_StillWorksInDefaultProbe(t *testing.T) {
	probe := DefaultActivityProbe()
	got := probe.Infer(ActivityInput{
		Agent:       Agent{Command: "cursor-agent", Cwd: "/tmp"},
		PaneContent: "Cursor Agent\nctrl+c to stop\n",
	})
	if got.State != StateRunning || got.Provider != "cursor" {
		t.Fatalf("got %#v", got)
	}
}

func TestResolveSessionStatus_BlockedFailedLeaseTable(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	progressAt := now.Add(-time.Minute)
	lease := now.Add(time.Minute)

	tests := []struct {
		name       string
		agent      *Agent
		classified AgentState
		activity   ActivitySignal
		want       AgentState
	}{
		{
			name:       "classify blocked beats provider running",
			agent:      &Agent{PaneAlive: true, State: StateUnknown, Command: "codex"},
			classified: StateBlocked,
			activity:   ActivitySignal{State: StateRunning, Provider: "codex"},
			want:       StateBlocked,
		},
		{
			name:       "classify failed beats provider running",
			agent:      &Agent{PaneAlive: true, State: StateUnknown, Command: "claude"},
			classified: StateFailed,
			activity:   ActivitySignal{State: StateRunning, Provider: "claude"},
			want:       StateFailed,
		},
		{
			name: "active lease beats provider idle",
			agent: &Agent{
				PaneAlive: true, State: StateRunning, Summary: "lease",
				LastProgressAt: &progressAt, ExpectedNextCheckAt: &lease, Command: "grok",
			},
			classified: StateUnknown,
			activity:   ActivitySignal{State: StateUnknown, Source: "grok_idle", Provider: "grok"},
			want:       StateRunning,
		},
		{
			name: "sticky done beats provider running",
			agent: &Agent{
				PaneAlive: true, State: StateDone, Summary: "done",
				LastProgressAt: &progressAt, Command: "cursor-agent",
			},
			classified: StateUnknown,
			activity:   ActivitySignal{State: StateRunning, Provider: "cursor"},
			want:       StateDone,
		},
		{
			name:       "provider fills unknown",
			agent:      &Agent{PaneAlive: true, State: StateUnknown, Command: "codex"},
			classified: StateUnknown,
			activity:   ActivitySignal{State: StateRunning, Source: "codex_transcript_active", Provider: "codex"},
			want:       StateRunning,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := ResolveSessionStatus(tt.agent, tt.classified, "detail", now, tt.activity)
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestJSONLTurnProbe_WarmPollStatOnly(t *testing.T) {
	home := t.TempDir()
	cwd := "/repo"
	now := time.Now().UTC()
	writeCodexRolloutFixture(t, home, cwd, "warm", now,
		`{"type":"session_meta","payload":{"cwd":"/repo"}}`,
		`{"type":"event_msg","payload":{"type":"task_started"}}`,
	)
	probe := NewCodexTranscriptProbe().SetHomeDir(home).SetNow(func() time.Time { return now })
	agent := Agent{ID: "w:@1", Command: "codex", Cwd: cwd}
	if res := probe.Probe(agent); !res.OK || !res.Active {
		t.Fatalf("first = %#v", res)
	}
	probe.ResetStats()
	for i := 0; i < 50; i++ {
		if res := probe.Probe(agent); !res.Active {
			t.Fatalf("warm %d = %#v", i, res)
		}
	}
	stats := probe.Stats()
	if stats.CacheHits != 50 || stats.OpenCalls != 0 || stats.PathResolveCalls != 0 {
		t.Fatalf("warm stats = %#v", stats)
	}
}

func writeCodexRolloutFixture(t *testing.T, home, cwd, sessionID string, mod time.Time, lines ...string) string {
	t.Helper()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "07", "12")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "rollout-2026-07-12T00-00-00-"+sessionID+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = os.Chtimes(path, mod, mod)
	_ = cwd
	return path
}

func writeClaudeTranscriptFixture(t *testing.T, home, cwd, sessionID string, lines ...string) string {
	t.Helper()
	dir := filepath.Join(home, ".claude", "projects", encodeClaudeProjectDirName(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, sessionID+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	now := time.Now().UTC()
	_ = os.Chtimes(path, now, now)
	return path
}

func writeGrokUpdatesFixture(t *testing.T, home, cwd, sessionID string, mod time.Time, lines ...string) string {
	t.Helper()
	dir := filepath.Join(home, ".grok", "sessions", encodeGrokSessionCWDName(cwd), sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "updates.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = os.Chtimes(path, mod, mod)
	return path
}

func writeGrokSummaryFixture(t *testing.T, sessionDir string, summary map[string]any) {
	t.Helper()
	raw, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "summary.json"), raw, 0o644); err != nil {
		t.Fatalf("write summary: %v", err)
	}
}

func appendFileLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatalf("append: %v", err)
	}
}
