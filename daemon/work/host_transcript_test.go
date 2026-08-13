package work

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

func TestResolveHostTranscriptIdentityDiscardsPreviousExecutorBinding(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(t.TempDir(), "brain-workspace")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	codexRollout := filepath.Join(t.TempDir(), "rollout-old-codex.jsonl")
	writeCodexHostRollout(t, codexRollout, "old-codex-session", "codex user", "codex assistant")

	startedAt := time.Now().UTC().Add(-time.Minute)
	sessionID := "grok-after-switch"
	sessionDir := writeGrokHostSessionFixture(t, home, cwd, sessionID, startedAt, "please continue", "grok assistant after switch")

	got := ResolveHostTranscriptIdentityForAgent(classifier.Agent{
		Command:   "grok --no-alt-screen --permission-mode bypassPermissions --resume " + sessionID,
		Cwd:       cwd,
		StartedAt: startedAt,
	}, HostTranscriptIdentity{
		Provider:  AgentProviderCodex,
		SessionID: "old-codex-session",
		Path:      codexRollout,
	}, AgentProviderGrok)

	if got.SessionID == "old-codex-session" || got.Path == codexRollout {
		t.Fatalf("kept previous Codex identity: %+v", got)
	}
	if got.Provider != AgentProviderGrok || got.SessionID != sessionID || got.Path != sessionDir {
		t.Fatalf("resolved = %+v want grok %s %s", got, sessionID, sessionDir)
	}
}

func TestLoadHostConversationByIdentityUsesGrokReaderNotCodexParser(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "brain-workspace")
	sessionID := "grok-bound"
	sessionDir := writeGrokHostSessionFixture(t, home, cwd, sessionID, time.Now().UTC(), "public user", "real grok reply after switch")

	got, err := LoadHostConversationByIdentity(HostTranscriptIdentity{
		Provider:  AgentProviderGrok,
		SessionID: sessionID,
		Path:      sessionDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Available || got.Path != sessionDir || got.SessionID != sessionID {
		t.Fatalf("bound grok load = %+v", got)
	}
	found := false
	for _, event := range got.Events {
		if event.Kind == "assistant_message" && event.Body == "real grok reply after switch" {
			found = true
		}
		if strings.Contains(event.Body, "codex") {
			t.Fatalf("codex parser leaked into grok identity load: %+v", event)
		}
	}
	if !found {
		t.Fatalf("missing grok assistant: %+v", got.Events)
	}
}

func TestLoadHostConversationByIdentityKeepsCodexSeam(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout-host.jsonl")
	writeCodexHostRollout(t, path, "codex-host", "codex user", "codex assistant")
	got, err := LoadHostConversationByIdentity(HostTranscriptIdentity{
		Provider:  AgentProviderCodex,
		SessionID: "codex-host",
		Path:      path,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Available || got.Path != path {
		t.Fatalf("codex identity load = %+v", got)
	}
}

func TestLoadHostConversationByIdentityDoesNotFallThroughUnknownProviderToCodex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout-host.jsonl")
	writeCodexHostRollout(t, path, "codex-host", "codex user", "codex assistant")
	got, err := LoadHostConversationByIdentity(HostTranscriptIdentity{
		Provider:  "not-a-provider",
		SessionID: "codex-host",
		Path:      path,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Available || got.Reason != "not_structured_agent" {
		t.Fatalf("unknown provider must not load as Codex: %+v", got)
	}
}

func TestSuppressPrivateHostTurnsHidesHandoffAndKeepsLaterReplies(t *testing.T) {
	events := []CodexConversationEvent{
		{ID: "boot-user", Kind: "user_message", Role: "user", Body: "You are Brain inside zen\nTreat this bootstrap as a map"},
		{ID: "boot-asst", Kind: "assistant_message", Role: "assistant", Body: "Bootstrap ready."},
		{ID: "hand-user", Kind: "user_message", Role: "user", Body: "Brain host executor handoff:\nWait for the next user message."},
		{ID: "hand-asst", Kind: "assistant_message", Role: "assistant", Body: "Handoff acknowledged, continuing."},
		{ID: "real-user", Kind: "user_message", Role: "user", Body: "please continue after switching host"},
		{ID: "real-asst", Kind: "assistant_message", Role: "assistant", Body: "real grok reply after switch"},
	}
	got := SuppressPrivateHostTurns(events)
	if len(got) != 2 || got[0].ID != "real-user" || got[1].ID != "real-asst" {
		t.Fatalf("suppressed = %+v", got)
	}
}

func writeGrokHostSessionFixture(t *testing.T, home, cwd, sessionID string, startedAt time.Time, publicUser, assistant string) string {
	t.Helper()
	cwd = filepath.Clean(cwd)
	sessionDir := filepath.Join(home, ".grok", "sessions", encodeGrokSessionCWD(cwd), sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGrokSummary(t, filepath.Join(sessionDir, grokSummaryFile), map[string]any{
		"info": map[string]any{
			"id":  sessionID,
			"cwd": cwd,
		},
		"created_at": startedAt.UTC().Add(2 * time.Second).Format(time.RFC3339Nano),
		"updated_at": startedAt.UTC().Add(2 * time.Minute).Format(time.RFC3339Nano),
	})
	writeJSONL(t, filepath.Join(sessionDir, grokChatHistoryFile),
		map[string]any{"type": "user", "content": "You are Brain inside zen, the user's private second brain.\nTreat this bootstrap as a map, not the full context."},
		map[string]any{"type": "assistant", "content": "Bootstrap ready."},
		map[string]any{"type": "user", "content": "Brain host executor handoff:\nThe user switched Brain host executors."},
		map[string]any{"type": "assistant", "content": "Handoff acknowledged, continuing."},
		map[string]any{"type": "user", "content": publicUser},
		map[string]any{"type": "assistant", "content": assistant},
	)
	return sessionDir
}

func TestHostIdentityUsableRejectsCodexPathForGrok(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if hostIdentityUsable(HostTranscriptIdentity{Path: path}, AgentProviderGrok) {
		t.Fatal("codex file must not be a grok host identity")
	}
}
