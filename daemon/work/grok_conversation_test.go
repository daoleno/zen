package work

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

func TestMatchGrokSessionToAgentStart_UsesNearestCreatedSession(t *testing.T) {
	base := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	candidates := []grokSessionCandidate{
		{
			ID:        "newer-other-window",
			CreatedAt: base.Add(90 * time.Second),
			Updated:   base.Add(5 * time.Minute),
		},
		{
			ID:        "this-window",
			CreatedAt: base.Add(3 * time.Second),
			Updated:   base.Add(2 * time.Minute),
		},
		{
			ID:        "old-window",
			CreatedAt: base.Add(-10 * time.Minute),
			Updated:   base.Add(6 * time.Minute),
		},
	}

	got, ok := matchGrokSessionToAgentStart(candidates, base)
	if !ok {
		t.Fatal("expected a grok session match")
	}
	if got.ID != "this-window" {
		t.Fatalf("matched %q, want this-window", got.ID)
	}
}

func TestMatchGrokSessionToAgentStart_DoesNotFallBackToOldSession(t *testing.T) {
	base := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	candidates := []grokSessionCandidate{
		{
			ID:        "old-window",
			CreatedAt: base.Add(-30 * time.Second),
			Updated:   base.Add(5 * time.Minute),
		},
	}

	if got, ok := matchGrokSessionToAgentStart(candidates, base); ok {
		t.Fatalf("matched %#v, want no match", got)
	}
}

func TestMatchGrokSessionToActiveSession_UsesSessionUpdatedAfterStart(t *testing.T) {
	base := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	candidates := []grokSessionCandidate{
		{
			ID:        "old-ended",
			CreatedAt: base.Add(-30 * time.Minute),
			Updated:   base.Add(-10 * time.Minute),
		},
		{
			ID:        "active-private",
			CreatedAt: base.Add(-30 * time.Second),
			Updated:   base.Add(12 * time.Minute),
		},
	}

	got, ok := matchGrokSessionToActiveSession(candidates, base)
	if !ok {
		t.Fatal("expected active grok session match")
	}
	if got.ID != "active-private" {
		t.Fatalf("matched %q, want active-private", got.ID)
	}
}

func TestFindGrokSession_DoesNotReturnStaleSessionForNewAgent(t *testing.T) {
	homeRoot := t.TempDir()
	home := filepath.Join(homeRoot, "home")
	cwd := "/tmp/zen-grok-fixture"
	sessionID := "stale-grok-session"
	sessionDir := filepath.Join(home, ".grok", "sessions", encodeGrokSessionCWD(cwd), sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeGrokSummary(t, filepath.Join(sessionDir, grokSummaryFile), map[string]any{
		"info": map[string]any{
			"id":  sessionID,
			"cwd": cwd,
		},
		"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
		"created_at": time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339Nano),
	})
	writeJSONL(t, filepath.Join(sessionDir, grokChatHistoryFile),
		map[string]any{"type": "user", "content": "old"},
	)

	t.Setenv("HOME", home)
	agentStart := time.Now().UTC()
	got, ok, err := findGrokSession(classifier.Agent{
		Command:   "grok --no-alt-screen --permission-mode bypassPermissions",
		Cwd:       cwd,
		StartedAt: agentStart,
	}, time.Now())
	if err != nil {
		t.Fatalf("findGrokSession: %v", err)
	}
	if ok {
		t.Fatalf("matched stale grok session %#v, want no match for fresh agent", got)
	}
}

func TestFindGrokSession_ResumeCommandMatchesExplicitSessionID(t *testing.T) {
	homeRoot := t.TempDir()
	home := filepath.Join(homeRoot, "home")
	cwd := "/home/daoleno/workspace/pacagent"
	sessionID := "019f2826-12b8-7cc3-a094-a57522b559e6"
	sessionDir := filepath.Join(home, ".grok", "sessions", encodeGrokSessionCWD(cwd), sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	now := time.Now().UTC()
	writeGrokSummary(t, filepath.Join(sessionDir, grokSummaryFile), map[string]any{
		"info": map[string]any{
			"id":  sessionID,
			"cwd": cwd,
		},
		"updated_at": now.Format(time.RFC3339Nano),
		"created_at": now.Add(-24 * time.Hour).Format(time.RFC3339Nano),
	})

	t.Setenv("HOME", home)
	got, ok, err := findGrokSession(classifier.Agent{
		Command:   "grok --resume " + sessionID,
		Cwd:       cwd,
		StartedAt: now,
	}, now)
	if err != nil {
		t.Fatalf("findGrokSession: %v", err)
	}
	if !ok {
		t.Fatal("expected resume command to match explicit grok session id")
	}
	if got.ID != sessionID || got.Dir != sessionDir {
		t.Fatalf("matched session = (%q, %q), want (%q, %q)", got.ID, got.Dir, sessionID, sessionDir)
	}
}

func TestEncodeGrokSessionCWD(t *testing.T) {
	got := encodeGrokSessionCWD("/home/daoleno/workspace/zen")
	want := "%2Fhome%2Fdaoleno%2Fworkspace%2Fzen"
	if got != want {
		t.Fatalf("encodeGrokSessionCWD = %q, want %q", got, want)
	}
}

func TestAgentToolNameRecognizesGrok(t *testing.T) {
	if got := agentToolName("grok --no-alt-screen --permission-mode bypassPermissions", ""); got != "grok" {
		t.Fatalf("agentToolName = %q, want grok", got)
	}
}

func TestParseGrokConversation_AssistantToolCallsUseUniqueEventIDsBeforeChatTrim(t *testing.T) {
	dir := t.TempDir()
	writeGrokSummary(t, filepath.Join(dir, grokSummaryFile), map[string]any{
		"info": map[string]any{
			"id":  "grok-tools-1",
			"cwd": "/repo",
		},
	})
	writeJSONL(t, filepath.Join(dir, grokChatHistoryFile),
		map[string]any{
			"type":    "assistant",
			"content": "Running lookups.",
			"tool_calls": []map[string]any{
				{"id": "call-a", "name": "Grep", "arguments": `{"pattern":"foo"}`},
				{"id": "call-b", "name": "Glob", "arguments": `{"glob_pattern":"**/*.go"}`},
			},
		},
		map[string]any{
			"type":         "tool_result",
			"tool_call_id": "call-a",
			"content":      "found 1",
		},
		map[string]any{
			"type":    "assistant",
			"content": "Done checking.",
		},
	)

	builder := newGrokConversationBuilder(filepath.Base(dir))
	if err := consumeGrokJSONL(filepath.Join(dir, grokChatHistoryFile), builder.consumeChatHistoryLine); err != nil {
		t.Fatalf("consumeGrokJSONL: %v", err)
	}
	seen := map[string]int{}
	for _, event := range builder.events {
		seen[event.ID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Fatalf("duplicate event id %q (%d times)", id, count)
		}
	}
}

func TestParseGrokConversation_AssistantToolCallsUseUniqueEventIDs(t *testing.T) {
	dir := t.TempDir()
	writeGrokSummary(t, filepath.Join(dir, grokSummaryFile), map[string]any{
		"info": map[string]any{
			"id":  "grok-tools-1",
			"cwd": "/repo",
		},
	})
	writeJSONL(t, filepath.Join(dir, grokChatHistoryFile),
		map[string]any{
			"type":    "assistant",
			"content": "Running lookups.",
			"tool_calls": []map[string]any{
				{"id": "call-a", "name": "Grep", "arguments": `{"pattern":"foo"}`},
				{"id": "call-b", "name": "Glob", "arguments": `{"glob_pattern":"**/*.go"}`},
			},
		},
	)

	got, err := parseGrokConversation(dir)
	if err != nil {
		t.Fatalf("parseGrokConversation: %v", err)
	}
	seen := map[string]int{}
	for _, event := range got.Events {
		seen[event.ID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Fatalf("duplicate event id %q (%d times)", id, count)
		}
	}
	if len(got.Events) != 1 {
		t.Fatalf("events = %#v, want assistant message only in chat feed", got.Events)
	}
	if got.Events[0].Kind != "assistant_message" {
		t.Fatalf("event = %#v", got.Events[0])
	}
}

func TestParseGrokConversation_BuildsStructuredTimeline(t *testing.T) {
	dir := t.TempDir()
	writeGrokSummary(t, filepath.Join(dir, grokSummaryFile), map[string]any{
		"info": map[string]any{
			"id":  "grok-test-1",
			"cwd": "/repo",
		},
	})
	writeJSONL(t, filepath.Join(dir, grokChatHistoryFile),
		map[string]any{
			"type": "user",
			"content": []map[string]any{
				{"type": "text", "text": "<user_query>\nShip the Grok chat interface\n</user_query>"},
			},
		},
		map[string]any{
			"type":    "assistant",
			"content": "I'll inspect the existing Codex interface first.",
		},
		map[string]any{
			"type": "assistant",
			"tool_calls": []map[string]any{
				{
					"id":        "call-grep",
					"name":      "Grep",
					"arguments": `{"pattern":"codex"}`,
				},
			},
		},
		map[string]any{
			"type":         "tool_result",
			"tool_call_id": "call-grep",
			"content":      "found 3 matches",
		},
	)
	writeJSONL(t, filepath.Join(dir, grokUpdatesFile),
		map[string]any{
			"timestamp": "2026-06-29T10:00:00Z",
			"params": map[string]any{
				"sessionId": "grok-test-1",
				"update": map[string]any{
					"sessionUpdate": "plan",
					"entries": []map[string]any{
						{"content": "Add grok parser", "status": "in_progress"},
						{"content": "Wire app chat surface", "status": "pending"},
					},
				},
			},
		},
	)

	got, err := parseGrokConversation(dir)
	if err != nil {
		t.Fatalf("parseGrokConversation: %v", err)
	}
	if !got.Available {
		t.Fatal("conversation should be available")
	}
	if got.SessionID != "grok-test-1" || got.CWD != "/repo" {
		t.Fatalf("metadata = (%q, %q)", got.SessionID, got.CWD)
	}
	if len(got.Events) != 2 {
		t.Fatalf("events len = %d, want 2: %#v", len(got.Events), got.Events)
	}

	assertEvent(t, got.Events[0], "user_message", "user", "", "Ship the Grok chat interface")
	assertEvent(t, got.Events[1], "assistant_message", "assistant", "", "I'll inspect the existing Codex interface first.")
}

func TestLoadCodexConversationForAgent_GrokUnavailableWithoutSession(t *testing.T) {
	got, err := LoadCodexConversationForAgent(classifier.Agent{
		Command: "grok --no-alt-screen",
		Cwd:     filepath.Join(t.TempDir(), "missing-grok-session"),
	}, time.Now())
	if err != nil {
		t.Fatalf("LoadCodexConversationForAgent: %v", err)
	}
	if got.Available || got.Reason != "session_not_found" {
		t.Fatalf("conversation = %#v", got)
	}
}

func TestGrokGoalSessionPreservesLatestUserMessages(t *testing.T) {
	requireGrokRealSessionOptIn(t)
	sessionDir := filepath.Join(
		os.Getenv("HOME"),
		".grok",
		"sessions",
		"%2Fhome%2Fdaoleno%2Fworkspace%2Fzen",
		"019f11c1-341e-7483-8b72-3a253c152796",
	)
	if _, err := os.Stat(filepath.Join(sessionDir, grokChatHistoryFile)); err != nil {
		t.Skipf("goal grok session unavailable: %v", err)
	}
	got, err := parseGrokConversation(sessionDir)
	if err != nil {
		t.Fatalf("parseGrokConversation: %v", err)
	}
	for _, kind := range []string{"plan", "commentary"} {
		for _, event := range got.Events {
			if event.Kind == kind {
				t.Fatalf("unexpected %s event in grok chat feed: %#v", kind, event)
			}
		}
	}
	userBodies := make([]string, 0, 4)
	for _, event := range got.Events {
		if event.Kind == "user_message" {
			userBodies = append(userBodies, event.Body)
		}
	}
	if len(userBodies) < 2 {
		t.Fatalf("user messages = %#v, want at least 2", userBodies)
	}
	foundVisibleUserMessage := false
	for _, body := range userBodies {
		if !strings.Contains(body, "<summary_content>") && len(strings.TrimSpace(body)) < 2000 {
			foundVisibleUserMessage = true
			break
		}
	}
	if !foundVisibleUserMessage {
		t.Fatalf("user messages missing visible user message: %#v", userBodies)
	}
	latestEvent := got.Events[len(got.Events)-1]
	if latestEvent.Kind == "plan" {
		t.Fatal("latest event should not be a plan checklist")
	}
	if latestEvent.Kind != "assistant_message" {
		t.Fatalf("latest event = %#v, want assistant reply", latestEvent)
	}
	if strings.TrimSpace(latestEvent.Body) == "" {
		t.Fatalf("latest assistant reply empty: %#v", latestEvent)
	}
}

func TestGrokGoalSessionEventMix(t *testing.T) {
	requireGrokRealSessionOptIn(t)
	sessionDir := filepath.Join(
		os.Getenv("HOME"),
		".grok",
		"sessions",
		"%2Fhome%2Fdaoleno%2Fworkspace%2Fzen",
		"019f11c1-341e-7483-8b72-3a253c152796",
	)
	if _, err := os.Stat(filepath.Join(sessionDir, grokChatHistoryFile)); err != nil {
		t.Skipf("goal grok session unavailable: %v", err)
	}
	got, err := parseGrokConversation(sessionDir)
	if err != nil {
		t.Fatalf("parseGrokConversation: %v", err)
	}
	counts := map[string]int{}
	for _, event := range got.Events {
		counts[event.Kind]++
	}
	t.Logf("event mix: %#v total=%d", counts, len(got.Events))
	for i := len(got.Events) - 8; i < len(got.Events); i++ {
		if i < 0 {
			continue
		}
		event := got.Events[i]
		body := event.Body
		if len(body) > 60 {
			body = body[:60] + "..."
		}
		t.Logf("tail[%d] kind=%s ts=%q body=%q plan=%d", i, event.Kind, event.Timestamp, body, len(event.Plan))
	}
}

func TestLoadCodexConversationForAgent_GrokGoalSessionHasUniqueEventIDs(t *testing.T) {
	requireGrokRealSessionOptIn(t)
	sessionDir := filepath.Join(
		os.Getenv("HOME"),
		".grok",
		"sessions",
		"%2Fhome%2Fdaoleno%2Fworkspace%2Fzen",
		"019f11c1-341e-7483-8b72-3a253c152796",
	)
	if _, err := os.Stat(filepath.Join(sessionDir, grokChatHistoryFile)); err != nil {
		t.Skipf("goal grok session unavailable: %v", err)
	}

	got, err := parseGrokConversation(sessionDir)
	if err != nil {
		t.Fatalf("parseGrokConversation: %v", err)
	}
	seen := map[string]int{}
	for _, event := range got.Events {
		seen[event.ID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Fatalf("duplicate event id %q (%d times)", id, count)
		}
	}
	t.Logf("parsed %d unique grok events from goal session", len(got.Events))
}

func TestLoadCodexConversationForAgent_GrokRealSessionFixture(t *testing.T) {
	sourceDir := findLocalGrokSessionDir(t)
	fixtureHome, cwd := installGrokSessionFixture(t, sourceDir)

	t.Setenv("HOME", fixtureHome)
	got, err := LoadCodexConversationForAgent(classifier.Agent{
		Command:   "grok --no-alt-screen --permission-mode bypassPermissions",
		Cwd:       cwd,
		StartedAt: time.Now().Add(-time.Hour),
	}, time.Now())
	if err != nil {
		t.Fatalf("LoadCodexConversationForAgent: %v", err)
	}
	if !got.Available {
		t.Fatalf("conversation unavailable: %#v", got)
	}
	if len(got.Events) == 0 {
		t.Fatal("expected parsed grok events")
	}

	hasUser := false
	hasAssistant := false
	hasTool := false
	for _, event := range got.Events {
		switch event.Kind {
		case "user_message":
			hasUser = true
		case "assistant_message":
			hasAssistant = true
		case "tool":
			hasTool = true
		}
	}
	if !hasUser || !hasAssistant || !hasTool {
		t.Fatalf("missing event kinds: user=%v assistant=%v tool=%v events=%#v", hasUser, hasAssistant, hasTool, got.Events)
	}
	t.Logf("parsed %d grok events from fixture", len(got.Events))
}

// requireGrokRealSessionOptIn gates tests that read maintainer ~/.grok data.
// Default `go test ./...` must not inspect local agent session stores.
func requireGrokRealSessionOptIn(t *testing.T) {
	t.Helper()
	if os.Getenv("ZEN_GROK_REAL_SESSION") != "1" {
		t.Skip("set ZEN_GROK_REAL_SESSION=1 to run opt-in ~/.grok integration tests")
	}
}

func findLocalGrokSessionDir(t *testing.T) string {
	t.Helper()
	requireGrokRealSessionOptIn(t)
	source := filepath.Join(os.Getenv("HOME"), ".grok", "sessions", "%2Fhome%2Fdaoleno%2Fworkspace%2Fzen")
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Skipf("real grok sessions unavailable: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(source, entry.Name())
		if _, err := os.Stat(filepath.Join(candidate, grokChatHistoryFile)); err != nil {
			continue
		}
		return candidate
	}
	t.Skip("no grok session with chat history found")
	return ""
}

func installGrokSessionFixture(t *testing.T, sourceDir string) (home string, cwd string) {
	t.Helper()
	cwd = "/tmp/zen-grok-fixture"
	homeRoot := t.TempDir()
	home = filepath.Join(homeRoot, "home")
	sessionRoot := filepath.Join(home, ".grok", "sessions", encodeGrokSessionCWD(cwd), filepath.Base(sourceDir))
	if err := os.MkdirAll(sessionRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for _, name := range []string{grokChatHistoryFile, grokUpdatesFile} {
		sourcePath := filepath.Join(sourceDir, name)
		if _, err := os.Stat(sourcePath); err != nil {
			continue
		}
		copyFixtureFile(t, sourcePath, filepath.Join(sessionRoot, name))
	}
	writeGrokSummary(t, filepath.Join(sessionRoot, grokSummaryFile), map[string]any{
		"info": map[string]any{
			"id":  filepath.Base(sourceDir),
			"cwd": cwd,
		},
		"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
		"created_at": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano),
	})
	return home, cwd
}

func writeGrokSummary(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func copyFixtureFile(t *testing.T, source, dest string) {
	t.Helper()
	in, err := os.Open(source)
	if err != nil {
		t.Fatalf("Open %s: %v", source, err)
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		t.Fatalf("Create %s: %v", dest, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("Copy %s: %v", source, err)
	}
}

func TestGrokVisibleUserTextExtractsUserQuery(t *testing.T) {
	got := grokVisibleUserText("<user_query>\nHello Grok\n</user_query>")
	if got != "Hello Grok" {
		t.Fatalf("grokVisibleUserText = %q", got)
	}
}

func TestIsGrokBootstrapUserMessage(t *testing.T) {
	if !isGrokBootstrapUserMessage("<user_info>\nOS Version: linux\n</user_info>") {
		t.Fatal("expected bootstrap detection")
	}
	if isGrokBootstrapUserMessage("Implement the Grok interface") {
		t.Fatal("did not expect bootstrap detection")
	}
}

func TestAgentExecutorInfersGrokProviderAndCapabilities(t *testing.T) {
	cfg := &ExecutorConfig{
		DelegatedExecutor: "claude",
		ByName: map[string]Executor{
			"grok": {Name: "grok", Command: "grok --no-alt-screen --permission-mode bypassPermissions"},
		},
	}
	executor, ok := cfg.AgentExecutor("grok")
	if !ok {
		t.Fatal("grok executor missing")
	}
	if executor.Provider != AgentProviderGrok {
		t.Fatalf("provider = %q", executor.Provider)
	}
	if !executor.Capabilities.StructuredEvents || executor.Capabilities.NativeThreads {
		t.Fatalf("capabilities = %+v", executor.Capabilities)
	}
}

func TestIsAgentCommandRecognizesGrok(t *testing.T) {
	if !IsAgentCommand("grok --no-alt-screen --permission-mode bypassPermissions") {
		t.Fatal("expected grok command recognition")
	}
}

func TestIsNativeAgentSourceIncludesGrok(t *testing.T) {
	if !IsNativeAgentSource("grok") {
		t.Fatal("expected grok native source")
	}
}

func TestLoadExecutorsIncludesGrokDefault(t *testing.T) {
	cfg, err := LoadExecutors(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatalf("LoadExecutors: %v", err)
	}
	executor, ok := cfg.ByName["grok"]
	if !ok {
		t.Fatal("grok missing from defaults")
	}
	if !strings.Contains(executor.Command, "--no-alt-screen") || !strings.Contains(executor.Command, "bypassPermissions") {
		t.Fatalf("grok command = %q", executor.Command)
	}
	if cfg.DelegatedExecutor != "codex" {
		t.Fatalf("delegated executor changed to %q", cfg.DelegatedExecutor)
	}
}
