package work

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

func TestInferAgentProviderPiAndOpenCode(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"pi", AgentProviderPi},
		{"/usr/bin/pi --session /tmp/x.jsonl", AgentProviderPi},
		{"env PATH=/x -- pi", AgentProviderPi},
		{"pipeline", ""},
		{"pixel", ""},
		{"opencode", AgentProviderOpenCode},
		{"opencode --auto", AgentProviderOpenCode},
		{"/opt/opencode --auto -s ses_1", AgentProviderOpenCode},
		{"env PATH=/x -- opencode --auto", AgentProviderOpenCode},
		{"myopencode", ""},
		{"opencodefake", ""},
		{"/opt/bin/myopencode", ""},
		{"wrapper-opencode", ""},
	}
	for _, tc := range cases {
		if got := InferAgentProvider(tc.in); got != tc.want {
			t.Fatalf("InferAgentProvider(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEnsurePiSessionLaunchCommandInjectsAbsoluteSession(t *testing.T) {
	got, err := EnsurePiSessionLaunchCommand("pi")
	if err != nil {
		t.Fatal(err)
	}
	path := PiOwnedSessionPath(got)
	if path == "" || !filepath.IsAbs(path) {
		t.Fatalf("expected absolute owned session path, got %q from %q", path, got)
	}
	if !strings.HasPrefix(path, filepath.Join(mustHome(t), ".zen", "provider-sessions", "pi")) {
		t.Fatalf("owned path outside zen root: %q", path)
	}
	again, err := EnsurePiSessionLaunchCommand(got)
	if err != nil {
		t.Fatal(err)
	}
	if again != got {
		t.Fatalf("second ensure mutated owned path: %q -> %q", got, again)
	}
}

func TestScheduledActionCommandPiAndOpenCode(t *testing.T) {
	piCmd, err := ScheduledActionCommand("pi", Executor{Name: "pi", Command: "pi", Kind: "pi"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(piCmd, PiNoExtensionsFlag) {
		t.Fatalf("pi scheduled command missing --no-extensions: %q", piCmd)
	}
	if PiOwnedSessionPath(piCmd) == "" {
		t.Fatalf("pi scheduled command missing owned session: %q", piCmd)
	}

	owned := filepath.Join(t.TempDir(), "owned.jsonl")
	kept, err := ScheduledActionCommand("pi", Executor{
		Name: "pi", Command: "pi --session " + owned, Kind: "pi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if PiOwnedSessionPath(kept) != owned {
		t.Fatalf("owned session not preserved: %q", kept)
	}

	if _, err := ScheduledActionCommand("pi", Executor{Name: "pi", Command: "pi -c", Kind: "pi"}); err == nil {
		t.Fatal("expected continue rejection")
	}

	oc, err := ScheduledActionCommand("opencode", Executor{Name: "opencode", Command: "opencode", Kind: "opencode"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(oc, OpenCodeAutoFlag) {
		t.Fatalf("opencode scheduled missing --auto: %q", oc)
	}
	if _, err := ScheduledActionCommand("opencode", Executor{Name: "opencode", Command: "opencode --continue", Kind: "opencode"}); err == nil {
		t.Fatal("expected --continue rejection")
	}
	if _, err := ScheduledActionCommand("opencode", Executor{Name: "opencode", Command: "opencode --auto=false", Kind: "opencode"}); err == nil {
		t.Fatal("expected scheduled --auto=false rejection")
	}
}

func TestParsePiConversationActiveBranchAndAdmission(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	payload := "zen-pi-admission-exact"
	content := strings.Join([]string{
		`{"type":"session","version":3,"id":"sess-1","timestamp":"2026-08-06T00:00:00.000Z","cwd":"/repo"}`,
		`{"type":"message","id":"u1","parentId":null,"timestamp":"2026-08-06T00:00:01.000Z","message":{"role":"user","content":"` + payload + `"}}`,
		`{"type":"message","id":"fork","parentId":"u1","timestamp":"2026-08-06T00:00:01.500Z","message":{"role":"user","content":"sibling branch"}}`,
		`{"type":"message","id":"a1","parentId":"u1","timestamp":"2026-08-06T00:00:02.000Z","message":{"role":"assistant","content":[{"type":"text","text":"ack"}],"stopReason":"stop"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := parsePiConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 2 {
		t.Fatalf("events = %#v, want active branch only", got.Events)
	}
	want := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	if got.Events[0].AdmissionSHA256 != want {
		t.Fatalf("admission = %q, want %q", got.Events[0].AdmissionSHA256, want)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityCompleted {
		t.Fatalf("activity = %+v", got.Activity)
	}
}

func TestPiOwnedSessionRejectsInvalidHeaderAndCWD(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	badHeader := filepath.Join(dir, "bad-header.jsonl")
	if err := os.WriteFile(badHeader, []byte(`{"type":"message","id":"u1"}\n`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := readPiOwnedSessionCandidate(badHeader, "/repo", now); err != nil || ok {
		t.Fatalf("invalid header must refuse: ok=%v err=%v", ok, err)
	}
	wrongCWD := filepath.Join(dir, "wrong-cwd.jsonl")
	writePiFixture(t, wrongCWD, "/other", "user")
	if _, ok, err := readPiOwnedSessionCandidate(wrongCWD, "/repo", now); err != nil || ok {
		t.Fatalf("cwd mismatch must refuse: ok=%v err=%v", ok, err)
	}
}

func TestPiNonmatchingUserDoesNotAdmitPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writePiFixture(t, path, "/repo", "other-user-text")
	got, err := parsePiConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%x", sha256.Sum256([]byte("zen-pi-admission-exact")))
	for _, event := range got.Events {
		if event.Kind == "user_message" && event.AdmissionSHA256 == want {
			t.Fatalf("nonmatching user admitted exact digest: %#v", event)
		}
	}
}

func TestFindPiTranscriptOwnedPathAndSharedDir(t *testing.T) {
	dir := t.TempDir()
	agent := classifier.Agent{
		Cwd:       "/repo",
		Command:   "pi",
		StartedAt: time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC),
	}
	// Owned --session wins and needs no shared directory.
	owned := filepath.Join(dir, "owned.jsonl")
	writePiFixture(t, owned, "/repo", "owned-user")
	agent.Command = "pi --session " + owned
	candidate, ok, err := NewProviderConversationReader().findPiTranscript(agent, time.Now().UTC())
	if err != nil || !ok || candidate.Path != owned {
		t.Fatalf("owned bind failed: ok=%v path=%q err=%v", ok, candidate.Path, err)
	}
	// Shared per-CWD directory auto-binds for interactive launches without
	// --session so the Interface is not left Working-only.
	agentDir := filepath.Join(dir, "agent")
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	sessionsDir := filepath.Join(agentDir, "sessions", encodePiSessionDirName("/repo"))
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(sessionsDir, "2026-08-06T00-00-10-000Z_sess1.jsonl")
	writePiFixture(t, shared, "/repo", "shared-user")
	agent.Command = "pi"
	candidate, ok, err = NewProviderConversationReader().findPiTranscript(agent, time.Now().UTC())
	if err != nil || !ok || candidate.Path != shared {
		t.Fatalf("shared dir bind failed: ok=%v path=%q err=%v", ok, candidate.Path, err)
	}
	// Wrong-cwd fixture in the same dir must not bind.
	wrong := filepath.Join(sessionsDir, "2026-08-06T00-00-11-000Z_sess2.jsonl")
	writePiFixture(t, wrong, "/other", "foreign")
	reader := NewProviderConversationReader()
	candidate, ok, err = reader.findPiTranscript(agent, time.Now().UTC())
	if err != nil || !ok || candidate.Path != shared {
		t.Fatalf("wrong-cwd must not replace pinned bind: ok=%v path=%q err=%v", ok, candidate.Path, err)
	}
}

func TestPiSharedDirAmbiguousWindowRefuses(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "agent")
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	sessionsDir := filepath.Join(agentDir, "sessions", encodePiSessionDirName("/repo"))
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	writePiFixture(t, filepath.Join(sessionsDir, "a.jsonl"), "/repo", "a-user")
	writePiFixture(t, filepath.Join(sessionsDir, "b.jsonl"), "/repo", "b-user")
	agent := classifier.Agent{Cwd: "/repo", Command: "pi", StartedAt: started}
	candidate, ok, err := NewProviderConversationReader().findPiTranscript(agent, time.Now().UTC())
	if err != nil || ok {
		t.Fatalf("ambiguous same-window transcripts must refuse: ok=%v path=%q err=%v", ok, candidate.Path, err)
	}
}

func TestPiToolLifecycleConvergesOnAbortAndError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := strings.Join([]string{
		`{"type":"session","version":3,"id":"sess-1","timestamp":"2026-08-06T00:00:00.000Z","cwd":"/repo"}`,
		`{"type":"message","id":"u1","parentId":null,"timestamp":"2026-08-06T00:00:01.000Z","message":{"role":"user","content":"run it"}}`,
		`{"type":"message","id":"a1","parentId":"u1","timestamp":"2026-08-06T00:00:02.000Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"call_1","name":"bash","arguments":{"command":"sleep 10"}}],"stopReason":"toolUse"}}`,
		`{"type":"message","id":"a2","parentId":"a1","timestamp":"2026-08-06T00:00:03.000Z","message":{"role":"assistant","content":[],"stopReason":"aborted"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := parsePiConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	var tool *CodexConversationEvent
	for i := range got.Events {
		if got.Events[i].Kind == "tool_call" {
			tool = &got.Events[i]
			break
		}
	}
	if tool == nil || tool.Status != "cancelled" || tool.Partial {
		t.Fatalf("aborted turn must cancel running tool: %#v", tool)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityInterrupted {
		t.Fatalf("activity = %+v", got.Activity)
	}

	errorPath := filepath.Join(t.TempDir(), "session.jsonl")
	errorContent := strings.Join([]string{
		`{"type":"session","version":3,"id":"sess-2","timestamp":"2026-08-06T00:00:00.000Z","cwd":"/repo"}`,
		`{"type":"message","id":"u1","parentId":null,"timestamp":"2026-08-06T00:00:01.000Z","message":{"role":"user","content":"run it"}}`,
		`{"type":"message","id":"a1","parentId":"u1","timestamp":"2026-08-06T00:00:02.000Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"call_2","name":"bash","arguments":{"command":"boom"}}],"stopReason":"toolUse"}}`,
		`{"type":"message","id":"a2","parentId":"a1","timestamp":"2026-08-06T00:00:03.000Z","message":{"role":"assistant","content":[],"stopReason":"error","errorMessage":"tool failed"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(errorPath, []byte(errorContent), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = parsePiConversation(errorPath)
	if err != nil {
		t.Fatal(err)
	}
	tool = nil
	for i := range got.Events {
		if got.Events[i].Kind == "tool_call" {
			tool = &got.Events[i]
			break
		}
	}
	if tool == nil || tool.Status != "failed" || tool.Partial {
		t.Fatalf("error turn must fail running tool: %#v", tool)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityFailed {
		t.Fatalf("activity = %+v", got.Activity)
	}
}

func TestPiSharedDirSessionSwitchCannotLeak(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "agent")
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	sessionsDir := filepath.Join(agentDir, "sessions", encodePiSessionDirName("/repo"))
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(sessionsDir, "2026-08-06T00-00-10-000Z_first.jsonl")
	writePiFixture(t, first, "/repo", "first-user")
	agent := classifier.Agent{Cwd: "/repo", Command: "pi"}
	reader := NewProviderConversationReader()
	candidate, ok, err := reader.findPiTranscript(agent, time.Now().UTC())
	if err != nil || !ok || candidate.Path != first {
		t.Fatalf("first bind failed: ok=%v path=%q err=%v", ok, candidate.Path, err)
	}
	// A newer same-CWD session must not leak into the pinned reader.
	second := filepath.Join(sessionsDir, "2026-08-06T00-01-00-000Z_second.jsonl")
	writePiFixture(t, second, "/repo", "second-user")
	if err := os.Chtimes(second, time.Now(), time.Now().Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	candidate, ok, err = reader.findPiTranscript(agent, time.Now().UTC())
	if err != nil || !ok || candidate.Path != first {
		t.Fatalf("newer session leaked into pinned bind: ok=%v path=%q err=%v", ok, candidate.Path, err)
	}
	// A reader bound to a different agent binding may pick the newer session.
	other := NewProviderConversationReader()
	candidate, ok, err = other.findPiTranscript(agent, time.Now().UTC())
	if err != nil || !ok || candidate.Path != second {
		t.Fatalf("fresh reader should bind newest: ok=%v path=%q err=%v", ok, candidate.Path, err)
	}
}

func TestPiLateFlushMissingFileIsNotFound(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "pending.jsonl")
	agent := classifier.Agent{
		Cwd:     "/repo",
		Command: "pi --session " + missing,
	}
	conversation, err := NewProviderConversationReader().Load(agent, AgentProviderPi, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if conversation.Available || conversation.Reason != "transcript_not_found" {
		t.Fatalf("late flush should remain transcript_not_found: %+v", conversation)
	}
}

func TestLoadExecutorsIncludesPiAndOpenCodeDefaults(t *testing.T) {
	cfg, err := LoadExecutors(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"pi", "opencode"} {
		executor, ok := cfg.ByName[id]
		if !ok {
			t.Fatalf("missing default executor %q", id)
		}
		agent := NewAgentExecutor(id, executor)
		if agent.Provider != id {
			t.Fatalf("%s provider = %q", id, agent.Provider)
		}
		if !agent.Capabilities.StructuredEvents {
			t.Fatalf("%s missing StructuredEvents", id)
		}
	}
	if cfg.GetDelegatedExecutor() != "codex" {
		t.Fatalf("delegated default changed: %q", cfg.GetDelegatedExecutor())
	}
}

func writePiFixture(t *testing.T, path, cwd, userText string) {
	t.Helper()
	content := strings.Join([]string{
		`{"type":"session","version":3,"id":"sess","timestamp":"2026-08-06T00:00:00.000Z","cwd":"` + cwd + `"}`,
		`{"type":"message","id":"u1","parentId":null,"timestamp":"2026-08-06T00:00:01.000Z","message":{"role":"user","content":"` + userText + `"}}`,
		`{"type":"message","id":"a1","parentId":"u1","timestamp":"2026-08-06T00:00:02.000Z","message":{"role":"assistant","content":[{"type":"text","text":"ok"}],"stopReason":"stop"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustHome(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return home
}
