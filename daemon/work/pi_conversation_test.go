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

func TestFindPiTranscriptRequiresOwnedPath(t *testing.T) {
	dir := t.TempDir()
	shared := filepath.Join(dir, "shared.jsonl")
	writePiFixture(t, shared, "/repo", "foreign")
	agent := classifier.Agent{
		Cwd:       "/repo",
		Command:   "pi",
		StartedAt: time.Now().UTC(),
	}
	if _, ok, err := findPiTranscript(agent, time.Now().UTC()); err != nil || ok {
		t.Fatalf("shared dir must not auto-bind: ok=%v err=%v", ok, err)
	}
	owned := filepath.Join(dir, "owned.jsonl")
	writePiFixture(t, owned, "/repo", "owned-user")
	agent.Command = "pi --session " + owned
	candidate, ok, err := findPiTranscript(agent, time.Now().UTC())
	if err != nil || !ok || candidate.Path != owned {
		t.Fatalf("owned bind failed: ok=%v path=%q err=%v", ok, candidate.Path, err)
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
