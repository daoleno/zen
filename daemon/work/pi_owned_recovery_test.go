package work

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

// ownedPiFixtureDir returns the Zen-owned Pi session directory for the given
// HOME, mirroring piOwnedSessionRoot("") with HOME overridden in tests.
func ownedPiFixtureDir(t *testing.T, home string) string {
	t.Helper()
	dir := filepath.Join(home, ".zen", "provider-sessions", "pi")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeOwnedPiRecoveryFixture writes a realistic version-3 Pi session (user
// text, reasoning, text, tool call, tool result, final text) into the
// Zen-owned sessions directory and returns its path and header CreatedAt.
func writeOwnedPiRecoveryFixture(t *testing.T, dir, cwd string, created time.Time) string {
	t.Helper()
	path := filepath.Join(dir, "recovery-"+strings.ReplaceAll(created.Format("150405"), ":", "")+".jsonl")
	content := strings.Join([]string{
		`{"type":"session","version":3,"id":"sess-recovery","timestamp":"` + created.UTC().Format(time.RFC3339Nano) + `","cwd":"` + cwd + `"}`,
		`{"type":"message","id":"u1","parentId":"c55d7d0c","timestamp":"2026-08-08T10:00:01.000Z","message":{"role":"user","content":"recovery user text"}}`,
		`{"type":"message","id":"a1","parentId":"u1","timestamp":"2026-08-08T10:00:02.000Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"recovery reasoning"},{"type":"text","text":"recovery text"},{"type":"toolCall","id":"call_1","name":"bash","arguments":{"command":"echo hi"}}],"stopReason":"toolUse"}}`,
		`{"type":"message","id":"r1","parentId":"a1","timestamp":"2026-08-08T10:00:03.000Z","message":{"role":"toolResult","toolCallId":"call_1","toolName":"bash","content":[{"type":"text","text":"recovery output"}],"isError":false}}`,
		`{"type":"message","id":"a2","parentId":"r1","timestamp":"2026-08-08T10:00:04.000Z","message":{"role":"assistant","content":[{"type":"text","text":"recovery final"}],"stopReason":"stop"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestPiOwnedDirAutoBindAfterBindingLoss reproduces the real cold-replay case
// end to end at the reader boundary: a Pi window whose durable owned binding
// is unavailable (pre-durable-binding sessions, argv-rewritten pi, daemon
// restart) must auto-bind the exact Zen-owned transcript for its cwd via the
// startedAt window, project messages plus reasoning and tool calls, and keep
// stable event IDs as the transcript grows — never a transcript_not_found
// empty projection while the authoritative JSONL exists and is fresh.
func TestPiOwnedDirAutoBindAfterBindingLoss(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := ownedPiFixtureDir(t, home)
	created := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	writeOwnedPiRecoveryFixture(t, dir, cwd, created)

	// Pre-fix launch shape: the pane command is bare "pi" (node-based Pi
	// rewrites its argv), no durable tmux binding, no --session in the
	// command. The owned transcript lives only in the Zen-owned directory.
	agent := classifier.Agent{
		ID:        "recovery-agent",
		Name:      "recovery",
		Cwd:       cwd,
		Command:   "pi",
		StartedAt: created.Add(2 * time.Second),
	}
	t.Setenv("HOME", home)
	reader := NewProviderConversationReader()
	conversation, err := reader.Load(agent, AgentProviderPi, created.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !conversation.Available || conversation.Reason != "" {
		t.Fatalf("binding loss must auto-bind the owned transcript, got %+v", conversation)
	}
	if conversation.SessionID != "sess-recovery" {
		t.Fatalf("session id = %q, want sess-recovery", conversation.SessionID)
	}
	if conversation.Path == "" || !strings.HasPrefix(conversation.Path, dir) {
		t.Fatalf("auto-bound path outside owned dir: %q", conversation.Path)
	}
	var kinds []string
	for _, event := range conversation.Events {
		kinds = append(kinds, event.Kind)
	}
	wantKinds := []string{"user_message", "reasoning", "assistant_message", "tool_call", "assistant_message"}
	if strings.Join(kinds, ",") != strings.Join(wantKinds, ",") {
		t.Fatalf("auto-bound kinds = %v, want %v", kinds, wantKinds)
	}
	firstUserID := conversation.Events[0].ID

	// The transcript keeps growing (live pi): the next load must return the
	// same stable first event plus the new one — the server then streams a
	// delta, never an empty replacement.
	appendOwnedPiLines(t, conversation.Path, []string{
		`{"type":"message","id":"a3","parentId":"a2","timestamp":"2026-08-08T10:00:10.000Z","message":{"role":"assistant","content":[{"type":"text","text":"recovery incremental"}],"stopReason":"stop"}}`,
	})
	again, err := reader.Load(agent, AgentProviderPi, created.Add(11*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Events) != len(conversation.Events)+1 {
		t.Fatalf("grown events = %d, want %d", len(again.Events), len(conversation.Events)+1)
	}
	if again.Events[0].ID != firstUserID {
		t.Fatalf("first event id changed across growth: %q -> %q", firstUserID, again.Events[0].ID)
	}
	last := again.Events[len(again.Events)-1]
	if last.ID != "a3-text" || last.Body != "recovery incremental" {
		t.Fatalf("incremental event = %#v", last)
	}
}

// TestPiOwnedDirAutoBindSelectionRules pins the fail-closed selection rules:
// only fresh owned transcripts whose header cwd matches the agent cwd bind;
// the startedAt window wins over mere freshness; an ambiguous window match or
// a stale transcript refuses instead of guessing.
func TestPiOwnedDirAutoBindSelectionRules(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := ownedPiFixtureDir(t, home)
	created := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	writeOwnedPiRecoveryFixture(t, dir, cwd, created)
	// A newer same-cwd session that is out of the startedAt window must not
	// win over the window-matched live session.
	newer := writeOwnedPiRecoveryFixture(t, dir, cwd, created.Add(time.Hour))

	// A foreign-cwd owned transcript must never bind. Its distinct timestamp
	// also keeps its file name unique so it cannot overwrite another fixture.
	foreignCwd := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.MkdirAll(foreignCwd, 0o755); err != nil {
		t.Fatal(err)
	}
	writeOwnedPiRecoveryFixture(t, dir, foreignCwd, created.Add(30*time.Minute))

	t.Setenv("HOME", home)
	agent := classifier.Agent{
		ID:        "recovery-agent",
		Name:      "recovery",
		Cwd:       cwd,
		Command:   "pi",
		StartedAt: created.Add(2 * time.Second),
	}
	reader := NewProviderConversationReader()
	conversation, err := reader.Load(agent, AgentProviderPi, created.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !conversation.Available || conversation.SessionID != "sess-recovery" {
		t.Fatalf("window-matched owned transcript must bind, got %+v", conversation)
	}
	if conversation.Path == newer {
		t.Fatalf("out-of-window newer session leaked in: %q", conversation.Path)
	}

	// StartedAt matching the newer session re-binds that one from a fresh
	// subscription (a new reader has no pinned transcript).
	agent.StartedAt = created.Add(time.Hour).Add(2 * time.Second)
	freshReader := NewProviderConversationReader()
	rebound, err := freshReader.Load(agent, AgentProviderPi, created.Add(25*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !rebound.Available || rebound.Path != newer {
		t.Fatalf("startedAt window must rebind the matching session, got %+v", rebound)
	}

	// Two same-cwd sessions inside the same window are ambiguous when
	// equidistant from the agent start: fail closed.
	writeOwnedPiRecoveryFixture(t, dir, cwd, created.Add(6*time.Second))
	ambiguous := classifier.Agent{
		ID:        "ambiguous-agent",
		Name:      "ambiguous",
		Cwd:       cwd,
		Command:   "pi",
		StartedAt: created.Add(3 * time.Second),
	}
	ambReader := NewProviderConversationReader()
	ambConversation, err := ambReader.Load(ambiguous, AgentProviderPi, created.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if ambConversation.Available || ambConversation.Reason != "transcript_not_found" {
		t.Fatalf("ambiguous window match must fail closed, got %+v", ambConversation)
	}

	// A stale owned transcript (older than 72h) never binds.
	staleDir := ownedPiFixtureDir(t, t.TempDir())
	stalePath := writeOwnedPiRecoveryFixture(t, staleDir, cwd, created.Add(-80*time.Hour))
	staleAgent := classifier.Agent{
		ID:        "stale-agent",
		Name:      "stale",
		Cwd:       cwd,
		Command:   "pi",
		StartedAt: created.Add(-80 * time.Hour).Add(2 * time.Second),
	}
	t.Setenv("HOME", t.TempDir())
	staleReader := NewProviderConversationReader()
	staleConversation, err := staleReader.Load(staleAgent, AgentProviderPi, created)
	if err != nil {
		t.Fatal(err)
	}
	if staleConversation.Available {
		t.Fatalf("stale owned transcript must not bind: %+v", staleConversation)
	}
	_ = stalePath

	// An explicit command binding always wins over the directory scan.
	t.Setenv("HOME", home)
	boundReader := NewProviderConversationReader()
	boundAgent := classifier.Agent{
		ID:        "bound-agent",
		Name:      "bound",
		Cwd:       cwd,
		Command:   "pi --session " + newer,
		StartedAt: created.Add(2 * time.Second),
	}
	boundConversation, err := boundReader.Load(boundAgent, AgentProviderPi, created.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !boundConversation.Available || boundConversation.Path != newer {
		t.Fatalf("command binding must win over auto-bind, got %+v", boundConversation)
	}
}

func appendOwnedPiLines(t *testing.T, path string, lines []string) {
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
