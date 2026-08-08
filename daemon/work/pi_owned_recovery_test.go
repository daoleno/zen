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

// writeOwnedPiRecoveryFixtureNamed writes the same fixture with an explicit
// file name so multiple transcripts can share an identical header CreatedAt
// (equal-timestamp ambiguity fixtures).
func writeOwnedPiRecoveryFixtureNamed(t *testing.T, dir, cwd string, created time.Time, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
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
		StartedAt: created,
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
	// Correct geometry: a process creates its transcript header at/after its
	// start (positive flush latency), never before.
	created := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	writeOwnedPiRecoveryFixture(t, dir, cwd, created)
	// A newer same-cwd session created well after the first process start.
	newer := writeOwnedPiRecoveryFixture(t, dir, cwd, created.Add(time.Hour).Add(5*time.Second))

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
		StartedAt: created,
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
	agent.StartedAt = created.Add(time.Hour).Add(5 * time.Second)
	freshReader := NewProviderConversationReader()
	rebound, err := freshReader.Load(agent, AgentProviderPi, created.Add(25*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !rebound.Available || rebound.Path != newer {
		t.Fatalf("startedAt window must rebind the matching session, got %+v", rebound)
	}

	// Two same-cwd sessions with equal CreatedAt inside the same window are
	// ambiguous: fail closed.
	ambiguousHome := t.TempDir()
	ambiguousDir := ownedPiFixtureDir(t, ambiguousHome)
	writeOwnedPiRecoveryFixtureNamed(t, ambiguousDir, cwd, created.Add(2*time.Second), "amb-a.jsonl")
	writeOwnedPiRecoveryFixtureNamed(t, ambiguousDir, cwd, created.Add(2*time.Second), "amb-b.jsonl")
	ambiguous := classifier.Agent{
		ID:        "ambiguous-agent",
		Name:      "ambiguous",
		Cwd:       cwd,
		Command:   "pi",
		StartedAt: created.Add(2 * time.Second),
	}
	t.Setenv("HOME", ambiguousHome)
	ambReader := NewProviderConversationReader()
	ambConversation, err := ambReader.Load(ambiguous, AgentProviderPi, created.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if ambConversation.Available || ambConversation.Reason != "transcript_not_found" {
		t.Fatalf("ambiguous window match must fail closed, got %+v", ambConversation)
	}

	// A stale owned transcript (mtime older than the 72h cutoff) never binds.
	// HOME points at the fixture owner root and the mtime is genuinely old,
	// so the staleness cutoff is what excludes the candidate — not an empty
	// scan of an unrelated HOME.
	staleHome := t.TempDir()
	staleDir := ownedPiFixtureDir(t, staleHome)
	stalePath := writeOwnedPiRecoveryFixture(t, staleDir, cwd, created.Add(-80*time.Hour))
	staleUpdated := created.Add(-80 * time.Hour)
	if err := os.Chtimes(stalePath, staleUpdated, staleUpdated); err != nil {
		t.Fatal(err)
	}
	staleAgent := classifier.Agent{
		ID:        "stale-agent",
		Name:      "stale",
		Cwd:       cwd,
		Command:   "pi",
		StartedAt: created.Add(-80 * time.Hour),
	}
	t.Setenv("HOME", staleHome)
	staleReader := NewProviderConversationReader()
	staleConversation, err := staleReader.Load(staleAgent, AgentProviderPi, created)
	if err != nil {
		t.Fatal(err)
	}
	if staleConversation.Available {
		t.Fatalf("stale owned transcript must not bind: %+v", staleConversation)
	}
	if staleConversation.Reason != "transcript_not_found" {
		t.Fatalf("stale owned transcript must fail as an honest miss, got reason %q", staleConversation.Reason)
	}
	// Positive control in a separate fixture root: the same-shaped transcript
	// with a current mtime binds through the same startedAt window, proving
	// only the mtime cutoff excludes the stale file.
	controlHome := t.TempDir()
	controlDir := ownedPiFixtureDir(t, controlHome)
	controlPath := writeOwnedPiRecoveryFixture(t, controlDir, cwd, created.Add(-80*time.Hour))
	controlAgent := classifier.Agent{
		ID:        "control-agent",
		Name:      "control",
		Cwd:       cwd,
		Command:   "pi",
		StartedAt: created.Add(-80 * time.Hour),
	}
	t.Setenv("HOME", controlHome)
	controlReader := NewProviderConversationReader()
	controlConversation, err := controlReader.Load(controlAgent, AgentProviderPi, created)
	if err != nil {
		t.Fatal(err)
	}
	if !controlConversation.Available || controlConversation.Path != controlPath {
		t.Fatalf("fresh control must bind via the same window, got %+v", controlConversation)
	}

	// An explicit command binding always wins over the directory scan.
	t.Setenv("HOME", home)
	boundReader := NewProviderConversationReader()
	boundAgent := classifier.Agent{
		ID:        "bound-agent",
		Name:      "bound",
		Cwd:       cwd,
		Command:   "pi --session " + newer,
		StartedAt: created,
	}
	boundConversation, err := boundReader.Load(boundAgent, AgentProviderPi, created.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !boundConversation.Available || boundConversation.Path != newer {
		t.Fatalf("command binding must win over auto-bind, got %+v", boundConversation)
	}
}

func TestPiOwnedDirAutoBindRebindsAfterInPaneRestart(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := ownedPiFixtureDir(t, home)
	created := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	oldTranscript := writeOwnedPiRecoveryFixture(t, dir, cwd, created)
	// The in-pane restart starts a new Pi process that creates its own owned
	// transcript whose header CreatedAt falls inside the new startedAt window
	// (positive flush latency after the new process start).
	restartAt := created.Add(time.Hour)
	newTranscript := writeOwnedPiRecoveryFixture(t, dir, cwd, restartAt)

	t.Setenv("HOME", home)
	agent := classifier.Agent{
		ID:        "restart-agent",
		Name:      "restart",
		Cwd:       cwd,
		Command:   "pi",
		StartedAt: created,
		ProcessID: 1000,
	}
	// One long-lived reader = one live subscription across the restart.
	reader := NewProviderConversationReader()

	// First process: the startedAt window binds and pins the old transcript.
	first, err := reader.Load(agent, AgentProviderPi, created.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !first.Available || first.Path != oldTranscript {
		t.Fatalf("first process must bind the old transcript, got %+v", first)
	}

	// In-pane restart: the process startedAt changes. The new window must
	// bind the new conversation — the pre-restart pin may not survive.
	agent.StartedAt = restartAt
	agent.ProcessID = 2000
	restarted, err := reader.Load(agent, AgentProviderPi, created.Add(25*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !restarted.Available || restarted.Path != newTranscript {
		t.Fatalf("in-pane restart must rebind the new transcript, got %+v", restarted)
	}

	// Ambiguity after the restart remains fail-closed: two transcripts with
	// equal CreatedAt in the new window refuse rather than guessing.
	ambiguousHome := t.TempDir()
	ambiguousDir := ownedPiFixtureDir(t, ambiguousHome)
	writeOwnedPiRecoveryFixtureNamed(t, ambiguousDir, cwd, restartAt.Add(2*time.Second), "amb-a.jsonl")
	writeOwnedPiRecoveryFixtureNamed(t, ambiguousDir, cwd, restartAt.Add(2*time.Second), "amb-b.jsonl")
	ambiguous := classifier.Agent{
		ID:        "ambiguous-agent",
		Name:      "ambiguous",
		Cwd:       cwd,
		Command:   "pi",
		StartedAt: restartAt.Add(2 * time.Second),
	}
	t.Setenv("HOME", ambiguousHome)
	ambReader := NewProviderConversationReader()
	ambConversation, err := ambReader.Load(ambiguous, AgentProviderPi, restartAt.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if ambConversation.Available || ambConversation.Reason != "transcript_not_found" {
		t.Fatalf("ambiguous restart window must fail closed, got %+v", ambConversation)
	}
}

// piSharedFixtureDir returns Pi's shared per-CWD session directory for the
// given HOME, mirroring findPiSharedCWDTranscript.
func piSharedFixtureDir(t *testing.T, home, cwd string) string {
	t.Helper()
	sessionsDir, err := piAgentSessionsDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(sessionsDir, encodePiSessionDirName(cwd))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPiOwnedDirAutoBindRealGeometryOldOwnedNewShared(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	created := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	restartAt := created.Add(time.Hour)

	// Pre-restart transcript stays in the Zen-owned directory; its mtime
	// freezes before the restart (the old process stopped writing it).
	ownedDir := ownedPiFixtureDir(t, home)
	oldPath := writeOwnedPiRecoveryFixture(t, ownedDir, cwd, created)
	oldUpdated := created.Add(30 * time.Minute)
	if err := os.Chtimes(oldPath, oldUpdated, oldUpdated); err != nil {
		t.Fatal(err)
	}

	// The restarted bare Pi writes its new transcript to the shared per-CWD
	// directory.
	sharedDir := piSharedFixtureDir(t, home, cwd)
	newPath := writeOwnedPiRecoveryFixture(t, sharedDir, cwd, restartAt)

	agent := classifier.Agent{
		ID:        "geometry-agent",
		Name:      "geometry",
		Cwd:       cwd,
		Command:   "pi",
		StartedAt: created,
		ProcessID: 1000,
	}
	// One long-lived reader = one live subscription across the restart.
	reader := NewProviderConversationReader()

	first, err := reader.Load(agent, AgentProviderPi, created.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !first.Available || first.Path != oldPath {
		t.Fatalf("first process must bind the old owned transcript, got %+v", first)
	}

	// In-pane restart: new process generation, new startedAt. The owned scan
	// must not re-pin the frozen old file; the shared scan binds the new one.
	agent.StartedAt = restartAt.Add(2 * time.Second)
	agent.ProcessID = 2000
	restarted, err := reader.Load(agent, AgentProviderPi, restartAt.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !restarted.Available || restarted.Path != newPath {
		t.Fatalf("restart must bind the new shared transcript, got %+v", restarted)
	}

	// Same instance afterwards: a processID-only observation change must not
	// flip the bind (the scan re-pins the same transcript; no churn).
	agent.ProcessID = 2001
	stable, err := reader.Load(agent, AgentProviderPi, restartAt.Add(11*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !stable.Available || stable.Path != newPath {
		t.Fatalf("processID-only wobble must keep the bind stable, got %+v", stable)
	}
}

// TestPiOwnedDirAutoBindDelayedNewHeaderFlush covers the restart race where a
// subscription poll lands before the new Pi session header exists: the reader
// must fail closed with transcript_not_found (never re-pin the old owned
// transcript), and the next poll after the header flushes must bind it.
func TestPiOwnedDirAutoBindDelayedNewHeaderFlush(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	created := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	restartAt := created.Add(time.Hour)

	ownedDir := ownedPiFixtureDir(t, home)
	oldPath := writeOwnedPiRecoveryFixture(t, ownedDir, cwd, created)
	oldUpdated := created.Add(30 * time.Minute)
	if err := os.Chtimes(oldPath, oldUpdated, oldUpdated); err != nil {
		t.Fatal(err)
	}
	sharedDir := piSharedFixtureDir(t, home, cwd)

	agent := classifier.Agent{
		ID:        "flush-agent",
		Name:      "flush",
		Cwd:       cwd,
		Command:   "pi",
		StartedAt: created,
		ProcessID: 1000,
	}
	reader := NewProviderConversationReader()
	first, err := reader.Load(agent, AgentProviderPi, created.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !first.Available || first.Path != oldPath {
		t.Fatalf("first process must bind the old owned transcript, got %+v", first)
	}

	// Restart poll before the new header exists anywhere.
	agent.StartedAt = restartAt.Add(2 * time.Second)
	agent.ProcessID = 2000
	missed, err := reader.Load(agent, AgentProviderPi, restartAt.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if missed.Available {
		t.Fatalf("restart with no new header must fail closed, got %+v", missed)
	}
	if missed.Reason != "transcript_not_found" {
		t.Fatalf("expected honest transcript_not_found, got %q", missed.Reason)
	}

	// The new header flushes into the shared directory: the next poll binds.
	newPath := writeOwnedPiRecoveryFixture(t, sharedDir, cwd, restartAt)
	bound, err := reader.Load(agent, AgentProviderPi, restartAt.Add(11*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !bound.Available || bound.Path != newPath {
		t.Fatalf("flushed new header must bind, got %+v", bound)
	}
}

// TestPiOwnedDirAutoBindResumeViaMtimeArm covers resume continuity: a
// transcript created before the current instance may still participate when
// its mtime is not earlier than startedAt (the instance is writing it). With
// the mtime frozen before startedAt the same transcript is excluded.
func TestPiOwnedDirAutoBindResumeViaMtimeArm(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	created := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	dir := ownedPiFixtureDir(t, home)
	path := writeOwnedPiRecoveryFixture(t, dir, cwd, created)

	// The instance started two hours after the transcript was created and
	// keeps writing it (mtime is real-now, not earlier than startedAt).
	agent := classifier.Agent{
		ID:        "resume-agent",
		Name:      "resume",
		Cwd:       cwd,
		Command:   "pi",
		StartedAt: created.Add(2 * time.Hour),
	}
	reader := NewProviderConversationReader()
	conversation, err := reader.Load(agent, AgentProviderPi, created.Add(25*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !conversation.Available || conversation.Path != path {
		t.Fatalf("resume continuity must bind the written transcript, got %+v", conversation)
	}

	// Freeze the mtime before startedAt: the same transcript no longer
	// belongs to this instance and must be excluded.
	frozen := created.Add(time.Hour)
	if err := os.Chtimes(path, frozen, frozen); err != nil {
		t.Fatal(err)
	}
	missed, err := reader.Load(agent, AgentProviderPi, created.Add(26*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if missed.Available || missed.Reason != "transcript_not_found" {
		t.Fatalf("frozen-before-start transcript must be excluded, got %+v", missed)
	}
}

// TestPiOwnedDirAutoBindZeroStartedAtRetainsFreshest pins the zero-startedAt
// limitation: without an instance signal the existing freshest fallback
// behavior is retained and a fresh out-of-window transcript still binds.
func TestPiOwnedDirAutoBindZeroStartedAtRetainsFreshest(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	created := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	dir := ownedPiFixtureDir(t, home)
	path := writeOwnedPiRecoveryFixture(t, dir, cwd, created)

	agent := classifier.Agent{
		ID:      "zero-agent",
		Name:    "zero",
		Cwd:     cwd,
		Command: "pi",
		// StartedAt intentionally zero: no instance signal.
	}
	reader := NewProviderConversationReader()
	conversation, err := reader.Load(agent, AgentProviderPi, created.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !conversation.Available || conversation.Path != path {
		t.Fatalf("zero startedAt must retain freshest binding, got %+v", conversation)
	}
}

// TestPiOwnedDirAutoBindSameSecondRestartProcessGeneration closes the
// same-second restart gap with the coherent process-generation signal: the
// watcher's startedAt is second-granularity ps lstart, so a restart within
// the same second shares startedAt. The detected provider process id is then
// the only instance signal: the pin must not survive a processID change, and
// the scan must rebind the closer window candidate (min-delta). An
// equidistant same-second pair stays fail-closed.
// TestPiOwnedDirAutoBindSubSecondRestartClosure covers the final P1: a
// sub-second in-pane restart must not re-admit the frozen old owned
// transcript through either eligibility arm. The new process start is
// sub-second precise (Linux /proc starttime evidence), so the header window
// lower bound (CreatedAt >= startedAt) and the mtime arm (mtime >=
// startedAt) both exclude the old file whose creation and last write precede
// the new process start, and the shared scan binds the new transcript.
// Phase A demonstrates the false admission with the second-rounded start the
// pre-fix watcher supplied (control); phase B proves the closure with the
// precise start.
func TestPiOwnedDirAutoBindSubSecondRestartClosure(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	base := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	oldStart := base                                   // previous instance start
	oldCreated := oldStart                             // header flushed at start
	oldFrozen := oldStart.Add(200 * time.Millisecond)  // old process last write
	newStart := oldStart.Add(900 * time.Millisecond)   // same second, precise
	newCreated := newStart.Add(300 * time.Millisecond) // header flush latency

	ownedDir := ownedPiFixtureDir(t, home)
	oldPath := writeOwnedPiRecoveryFixtureNamed(t, ownedDir, cwd, oldCreated, "old.jsonl")
	if err := os.Chtimes(oldPath, oldFrozen, oldFrozen); err != nil {
		t.Fatal(err)
	}
	sharedDir := piSharedFixtureDir(t, home, cwd)
	newPath := writeOwnedPiRecoveryFixtureNamed(t, sharedDir, cwd, newCreated, "new.jsonl")

	agent := classifier.Agent{
		ID:        "subsecond-agent",
		Name:      "subsecond",
		Cwd:       cwd,
		Command:   "pi",
		StartedAt: oldStart,
		ProcessID: 1000,
	}
	reader := NewProviderConversationReader()
	first, err := reader.Load(agent, AgentProviderPi, base.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !first.Available || first.Path != oldPath {
		t.Fatalf("first instance must bind the old transcript, got %+v", first)
	}

	// Phase A (control): with the second-rounded new start the pre-fix
	// watcher supplied, the frozen old file is re-admitted (its mtime sits in
	// the same rounded second) and shadows the new shared transcript.
	roundedAgent := agent
	roundedAgent.StartedAt = newStart.Truncate(time.Second)
	roundedAgent.ProcessID = 2000
	rounded, err := reader.Load(roundedAgent, AgentProviderPi, base.Add(11*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !rounded.Available || rounded.Path != oldPath {
		t.Fatalf("rounded start must re-admit the old file (control), got %+v", rounded)
	}

	// Phase B (closure): with the precise new start, the old file fails both
	// arms (CreatedAt and mtime before the new process start) and the shared
	// scan binds the new transcript.
	agent.StartedAt = newStart
	agent.ProcessID = 3000
	restarted, err := reader.Load(agent, AgentProviderPi, base.Add(12*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !restarted.Available || restarted.Path != newPath {
		t.Fatalf("precise start must bind the new shared transcript, got %+v", restarted)
	}

	// Same instance afterwards: a processID-only observation change must not
	// flip the bind (no churn).
	agent.ProcessID = 3001
	stable, err := reader.Load(agent, AgentProviderPi, base.Add(13*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !stable.Available || stable.Path != newPath {
		t.Fatalf("processID-only wobble must keep the bind stable, got %+v", stable)
	}
}

// TestPiOwnedDirAutoBindSubSecondWindowArmExclusion covers the window-arm
// variant in isolation: an old owned transcript whose header CreatedAt
// precedes the precise new process start within the same second must not be
// admitted by any negative window margin; the shared scan binds the new
// transcript.
func TestPiOwnedDirAutoBindSubSecondWindowArmExclusion(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	base := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	oldStart := base
	oldCreated := oldStart
	newStart := oldStart.Add(700 * time.Millisecond)
	newCreated := newStart.Add(300 * time.Millisecond)

	ownedDir := ownedPiFixtureDir(t, home)
	oldPath := writeOwnedPiRecoveryFixtureNamed(t, ownedDir, cwd, oldCreated, "old.jsonl")
	if err := os.Chtimes(oldPath, oldStart, oldStart); err != nil {
		t.Fatal(err)
	}
	sharedDir := piSharedFixtureDir(t, home, cwd)
	newPath := writeOwnedPiRecoveryFixtureNamed(t, sharedDir, cwd, newCreated, "new.jsonl")

	agent := classifier.Agent{
		ID:        "window-arm-agent",
		Name:      "window-arm",
		Cwd:       cwd,
		Command:   "pi",
		StartedAt: oldStart,
		ProcessID: 1000,
	}
	reader := NewProviderConversationReader()
	first, err := reader.Load(agent, AgentProviderPi, base.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !first.Available || first.Path != oldPath {
		t.Fatalf("first instance must bind the old transcript, got %+v", first)
	}

	agent.StartedAt = newStart
	agent.ProcessID = 2000
	restarted, err := reader.Load(agent, AgentProviderPi, base.Add(11*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !restarted.Available || restarted.Path != newPath {
		t.Fatalf("old CreatedAt before the new start must not re-admit, got %+v", restarted)
	}
}

// TestPiOwnedDirAutoBindSubSecondMtimeArmExclusion covers the mtime-arm
// variant in isolation: an old owned transcript whose last write precedes the
// precise new process start within the same second must not be admitted by
// the resume arm; the shared scan binds the new transcript. Phase A asserts
// the false admission with the second-rounded start (control), phase B the
// closure with the precise start.
func TestPiOwnedDirAutoBindSubSecondMtimeArmExclusion(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	base := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	oldStart := base.Add(-30 * time.Second) // old file created long before
	oldCreated := oldStart
	newStart := base.Add(700 * time.Millisecond) // precise new start
	newCreated := newStart.Add(300 * time.Millisecond)
	oldFrozen := base.Add(400 * time.Millisecond) // same rounded second, before the precise new start

	ownedDir := ownedPiFixtureDir(t, home)
	oldPath := writeOwnedPiRecoveryFixtureNamed(t, ownedDir, cwd, oldCreated, "old.jsonl")
	if err := os.Chtimes(oldPath, oldFrozen, oldFrozen); err != nil {
		t.Fatal(err)
	}
	sharedDir := piSharedFixtureDir(t, home, cwd)
	newPath := writeOwnedPiRecoveryFixtureNamed(t, sharedDir, cwd, newCreated, "new.jsonl")

	agent := classifier.Agent{
		ID:        "mtime-arm-agent",
		Name:      "mtime-arm",
		Cwd:       cwd,
		Command:   "pi",
		StartedAt: oldStart,
		ProcessID: 1000,
	}
	reader := NewProviderConversationReader()
	first, err := reader.Load(agent, AgentProviderPi, base.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !first.Available || first.Path != oldPath {
		t.Fatalf("first instance must bind the old transcript, got %+v", first)
	}

	// Phase A (control): the second-rounded new start admits the frozen old
	// file through the mtime arm (same rounded second).
	roundedAgent := agent
	roundedAgent.StartedAt = newStart.Truncate(time.Second)
	roundedAgent.ProcessID = 2000
	rounded, err := reader.Load(roundedAgent, AgentProviderPi, base.Add(11*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !rounded.Available || rounded.Path != oldPath {
		t.Fatalf("rounded start must re-admit via mtime arm (control), got %+v", rounded)
	}

	// Phase B (closure): the precise start excludes the frozen old file.
	agent.StartedAt = newStart
	agent.ProcessID = 3000
	restarted, err := reader.Load(agent, AgentProviderPi, base.Add(12*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !restarted.Available || restarted.Path != newPath {
		t.Fatalf("precise start must bind the new shared transcript, got %+v", restarted)
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
