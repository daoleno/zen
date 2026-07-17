package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/watcher"
)

func TestZeroViewAgentSessionNamesStayWatcherOwnedWithoutProviderLookup(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		observe func(*testing.T, *zeroViewCodexTitleFixture) string
	}{
		{
			name: "initial agent list",
			observe: func(t *testing.T, fixture *zeroViewCodexTitleFixture) string {
				conn := openThinProxyTestSocket(t, fixture.server)
				if err := conn.WriteJSON(clientMessage{Type: "list_agent_sessions", RequestID: "list"}); err != nil {
					t.Fatal(err)
				}
				if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
					t.Fatal(err)
				}
				var response struct {
					Type     string              `json:"type"`
					Sessions []*agentSessionWire `json:"agent_sessions"`
				}
				if err := conn.ReadJSON(&response); err != nil {
					t.Fatal(err)
				}
				if response.Type != "agent_session_list" || len(response.Sessions) != 1 {
					t.Fatalf("agent list response = %#v", response)
				}
				return response.Sessions[0].Name
			},
		},
		{
			name: "heartbeat list projection",
			observe: func(t *testing.T, fixture *zeroViewCodexTitleFixture) string {
				sessions := fixture.server.currentVisibleAgentSessions()
				if len(sessions) != 1 {
					t.Fatalf("visible sessions = %#v", sessions)
				}
				wire := fixture.server.agentSessionsWire(sessions)
				if len(wire) != 1 {
					t.Fatalf("wire sessions = %#v", wire)
				}
				return wire[0].Name
			},
		},
		{
			name: "watcher metadata event",
			observe: func(t *testing.T, fixture *zeroViewCodexTitleFixture) string {
				agent := fixture.watcher.GetAgent(fixture.agentID)
				if agent == nil {
					t.Fatal("missing watcher agent")
				}
				fixture.server.handleWatcherEvent(watcher.SessionEvent{
					Type:    "agent_metadata_change",
					AgentID: fixture.agentID,
					Agent:   agent,
				})
				return fixture.server.agentSessionWire(agent).Name
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newZeroViewCodexTitleFixture(t)
			if got := testCase.observe(t, fixture); got != fixture.watcherName {
				t.Errorf("agent name = %q, want Watcher/session name %q", got, fixture.watcherName)
			}
			if lookups := fixture.providerTitleLookups(t); lookups != 0 {
				t.Errorf("zero-view naming performed %d Codex SQLite/title lookups, want 0", lookups)
			}
		})
	}
}

type zeroViewCodexTitleFixture struct {
	server        *Server
	watcher       *watcher.Watcher
	agentID       string
	watcherName   string
	lookupCounter string
}

func newZeroViewCodexTitleFixture(t *testing.T) *zeroViewCodexTitleFixture {
	t.Helper()
	home := t.TempDir()
	binDir := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

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
		Command:  "codex",
		Name:     "Watcher session",
	})
	if err != nil {
		t.Fatal(err)
	}
	agent := w.GetAgent(agentID)
	if agent == nil {
		t.Fatal("fake tmux session was not registered")
	}

	rolloutPath := filepath.Join(home, ".codex", "sessions", "rollout.jsonl")
	if err := os.MkdirAll(filepath.Dir(rolloutPath), 0o755); err != nil {
		t.Fatal(err)
	}
	meta, err := json.Marshal(map[string]any{
		"type":      "session_meta",
		"timestamp": agent.StartedAt.Add(time.Millisecond).Format(time.RFC3339Nano),
		"payload": map[string]any{
			"id":         "renamed-thread",
			"cwd":        cwd,
			"originator": "codex-tui",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rolloutPath, append(meta, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	updated := agent.StartedAt.Add(time.Second)
	if err := os.Chtimes(rolloutPath, updated, updated); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(home, ".codex", "state_5.sqlite")
	if err := os.WriteFile(dbPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	responsePath := filepath.Join(t.TempDir(), "sqlite-response.json")
	response, err := json.Marshal([]map[string]any{{
		"id":                 "renamed-thread",
		"rollout_path":       rolloutPath,
		"created_at":         agent.StartedAt.Unix(),
		"created_at_ms":      agent.StartedAt.Add(time.Millisecond).UnixMilli(),
		"title":              "Provider-renamed thread",
		"first_user_message": "Original prompt",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(responsePath, response, 0o600); err != nil {
		t.Fatal(err)
	}
	lookupCounter := filepath.Join(t.TempDir(), "sqlite-lookups")
	t.Setenv("ZEN_ZERO_VIEW_SQLITE_RESPONSE", responsePath)
	t.Setenv("ZEN_ZERO_VIEW_SQLITE_COUNTER", lookupCounter)
	sqlitePath := filepath.Join(binDir, "sqlite3")
	sqliteScript := "#!/bin/sh\nprintf x >> \"$ZEN_ZERO_VIEW_SQLITE_COUNTER\"\nexec /bin/cat \"$ZEN_ZERO_VIEW_SQLITE_RESPONSE\"\n"
	if err := os.WriteFile(sqlitePath, []byte(sqliteScript), 0o700); err != nil {
		t.Fatal(err)
	}

	return &zeroViewCodexTitleFixture{
		server:        &Server{watcher: w},
		watcher:       w,
		agentID:       agentID,
		watcherName:   agent.Name,
		lookupCounter: lookupCounter,
	}
}

func (f *zeroViewCodexTitleFixture) providerTitleLookups(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile(f.lookupCounter)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(data)
}
