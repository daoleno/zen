package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

func TestWatcherLifecycleDoesNotInvokeAutomaticWorkDigest(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	transcriptDir := filepath.Join(home, ".claude", "projects", strings.ReplaceAll(filepath.Clean(cwd), string(filepath.Separator), "-"))
	if err := os.MkdirAll(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := fmt.Sprintf("{\"type\":\"user\",\"cwd\":%q,\"sessionId\":\"fixture\",\"message\":{\"role\":\"user\",\"content\":\"fixture transcript\"}}\n", cwd)
	if err := os.WriteFile(filepath.Join(transcriptDir, "fixture.jsonl"), []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}

	marker := filepath.Join(t.TempDir(), "digest-invoked")
	binDir := t.TempDir()
	fakeClaude := filepath.Join(binDir, "claude")
	script := fmt.Sprintf("#!/bin/sh\n: > %q\nprintf '%%s\\n' '{\"title\":\"fixture digest\",\"next\":\"none\"}'\n", marker)
	if err := os.WriteFile(fakeClaude, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir)

	workRoot := t.TempDir()
	historicalDir := filepath.Join(workRoot, "fixture-project")
	if err := os.MkdirAll(historicalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	historicalPath := filepath.Join(historicalDir, "brain.md")
	historicalBytes := []byte("---\nid: historical-brain-log\nkind: brain_log\ncreated: 2026-07-17T00:00:00Z\ntitle: Historical readout\n---\n\nUser-owned historical body.\n")
	if err := os.WriteFile(historicalPath, historicalBytes, 0o640); err != nil {
		t.Fatal(err)
	}
	historicalMtime := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(historicalPath, historicalMtime, historicalMtime); err != nil {
		t.Fatal(err)
	}

	store, err := work.NewStore(workRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server := New(nil, nil, nil, nil, store, nil, nil)
	agent := &classifier.Agent{
		ID:      "fixture-agent",
		Name:    "Fixture agent",
		Command: "claude",
		Cwd:     cwd,
		Project: "fixture-project",
	}

	for _, event := range []watcher.SessionEvent{
		{Type: "agent_discovered", AgentID: agent.ID, Agent: agent},
		{Type: "agent_output", AgentID: agent.ID, Agent: agent},
		{Type: "agent_state_change", AgentID: agent.ID, Agent: agent, NewState: "done"},
		{Type: "agent_removed", AgentID: agent.ID, Agent: agent},
	} {
		server.handleWatcherEvent(event)
	}

	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			t.Fatal("Watcher lifecycle invoked the retired automatic digest provider")
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	afterBytes, err := os.ReadFile(historicalPath)
	if err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(historicalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterBytes) != string(historicalBytes) || afterInfo.Mode().Perm() != 0o640 || !afterInfo.ModTime().Equal(historicalMtime) {
		t.Fatalf("Watcher lifecycle rewrote historical brain.md: bytes_equal=%v mode=%#o mtime=%s", string(afterBytes) == string(historicalBytes), afterInfo.Mode().Perm(), afterInfo.ModTime())
	}
}

func TestWorkWireKeepsExplicitItemsWithoutDigestControlPlane(t *testing.T) {
	store, err := work.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server := New(nil, nil, nil, nil, store, nil, nil)
	conn := openThinProxyTestSocket(t, server)

	if err := conn.WriteJSON(clientMessage{Type: "list_work_items", RequestID: "list-work"}); err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]json.RawMessage
	if err := conn.ReadJSON(&snapshot); err != nil {
		t.Fatal(err)
	}
	if got := jsonStringField(t, snapshot, "type"); got != "work_items_snapshot" {
		t.Fatalf("snapshot type = %q", got)
	}
	if _, ok := snapshot["executors"]; ok {
		t.Fatalf("Work snapshot exposed retired executor roles: %#v", snapshot)
	}
	if _, ok := snapshot["work_digest_provider"]; ok {
		t.Fatalf("Work snapshot exposed retired digest provider: %#v", snapshot)
	}

	if err := conn.WriteJSON(clientMessage{
		Type:      "write_work_item",
		RequestID: "write-work",
		Project:   "calendar",
		Body:      "explicit deliverable",
		Frontmatter: map[string]interface{}{
			"kind": "calendar_action",
		},
	}); err != nil {
		t.Fatal(err)
	}
	var written map[string]json.RawMessage
	if err := conn.ReadJSON(&written); err != nil {
		t.Fatal(err)
	}
	if got := jsonStringField(t, written, "type"); got != "work_item_written" {
		t.Fatalf("write response type = %q", got)
	}

	var event work.Event
	select {
	case event = <-server.workSub:
	case <-time.After(time.Second):
		t.Fatal("explicit Work write did not emit a store event")
	}
	server.mu.Lock()
	for registeredConn := range server.writes {
		server.clients[registeredConn] = true
	}
	server.mu.Unlock()
	server.handleWorkEvent(event)

	var changed map[string]json.RawMessage
	if err := conn.ReadJSON(&changed); err != nil {
		t.Fatal(err)
	}
	if got := jsonStringField(t, changed, "type"); got != "work_item_changed" {
		t.Fatalf("broadcast type = %q", got)
	}
}

func TestWorkFrontmatterUpdatePreservesCurrentFactsAndGenericExtra(t *testing.T) {
	started := time.Date(2026, time.July, 17, 3, 0, 0, 0, time.UTC)
	frontmatter := work.Frontmatter{
		ID:           "work-1",
		Kind:         "calendar_action",
		Created:      started.Add(-time.Minute),
		Started:      &started,
		Status:       "running",
		Title:        "Original",
		AgentSession: "agent-1",
		Extra:        map[string]interface{}{"legacy_note": "keep"},
	}

	applyFrontmatterOverrides(&frontmatter, map[string]interface{}{
		"title":      " Updated ",
		"custom_key": "new",
	})

	if frontmatter.ID != "work-1" || frontmatter.Kind != "calendar_action" ||
		frontmatter.Started == nil || !frontmatter.Started.Equal(started) ||
		frontmatter.Status != "running" || frontmatter.Title != "Updated" ||
		frontmatter.AgentSession != "agent-1" {
		t.Fatalf("current frontmatter facts changed: %#v", frontmatter)
	}
	if frontmatter.Extra["legacy_note"] != "keep" || frontmatter.Extra["custom_key"] != "new" {
		t.Fatalf("generic extra = %#v, want preserved and new keys", frontmatter.Extra)
	}
}

func jsonStringField(t *testing.T, value map[string]json.RawMessage, key string) string {
	t.Helper()
	var out string
	if err := json.Unmarshal(value[key], &out); err != nil {
		t.Fatalf("decode %s: %v", key, err)
	}
	return out
}
