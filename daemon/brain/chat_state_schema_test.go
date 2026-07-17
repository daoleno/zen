package brain

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChatStateCurrentSchemaDecodesWithoutMutation(t *testing.T) {
	root := t.TempDir()
	raw := []byte(`{
	  "thread_id": " thread-current ",
	  "thread_ids": ["thread-old", " thread-current ", "thread-old"]
	}
`)
	path, before := writeChatStateFixture(t, root, raw)

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	state, err := store.ChatState("")
	if err != nil {
		t.Fatalf("ChatState() error = %v", err)
	}
	if state.ThreadID != "thread-current" || len(state.ThreadIDs) != 2 ||
		state.ThreadIDs[0] != "thread-old" || state.ThreadIDs[1] != "thread-current" {
		t.Fatalf("decoded current Chat state = %+v", state)
	}
	assertChatStateFixtureUnchanged(t, path, raw, before)
}

func TestChatStateThreadIDsOnlySurviveNormalLoadAndWrite(t *testing.T) {
	root := t.TempDir()
	raw := []byte("{\"thread_ids\":[\" historical-a \",\"historical-b\",\"historical-a\"]}\n")
	writeChatStateFixture(t, root, raw)

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	state, err := store.ChatState("thread-current")
	if err != nil {
		t.Fatalf("ChatState() error = %v", err)
	}
	if state.ThreadID != "thread-current" || len(state.ThreadIDs) != 3 ||
		state.ThreadIDs[0] != "historical-a" || state.ThreadIDs[1] != "historical-b" ||
		state.ThreadIDs[2] != "thread-current" {
		t.Fatalf("loaded Chat state lost current-shape thread IDs: %+v", state)
	}
	if err := store.SetChatState(ChatState{
		ThreadID:  "thread-next",
		ThreadIDs: []string{"historical-b", "thread-extra", "thread-extra"},
	}); err != nil {
		t.Fatalf("SetChatState() error = %v", err)
	}
	state, err = store.ChatState("")
	if err != nil {
		t.Fatalf("ChatState() after switch error = %v", err)
	}
	wantThreadIDs := []string{"historical-a", "historical-b", "thread-current", "thread-extra", "thread-next"}
	if state.ThreadID != "thread-next" || len(state.ThreadIDs) != len(wantThreadIDs) {
		t.Fatalf("switched Chat state = %+v", state)
	}
	for index, want := range wantThreadIDs {
		if state.ThreadIDs[index] != want {
			t.Fatalf("switched thread IDs = %#v, want %#v", state.ThreadIDs, wantThreadIDs)
		}
	}
	for _, historical := range wantThreadIDs {
		known, err := store.HasChatThread(historical)
		if err != nil {
			t.Fatalf("HasChatThread(%q) error = %v", historical, err)
		}
		if !known {
			t.Fatalf("historical thread %q was not preserved", historical)
		}
	}
	persistedRaw, err := os.ReadFile(store.ChatStatePath())
	if err != nil {
		t.Fatalf("read persisted Chat state: %v", err)
	}
	var persisted chatStateFile
	if err := json.Unmarshal(persistedRaw, &persisted); err != nil {
		t.Fatalf("decode persisted Chat state: %v", err)
	}
	if persisted.ThreadID != "thread-next" || len(persisted.ThreadIDs) != len(wantThreadIDs) {
		t.Fatalf("persisted Chat state lost registry: %s", persistedRaw)
	}
	for index, want := range wantThreadIDs {
		if persisted.ThreadIDs[index] != want {
			t.Fatalf("persisted thread IDs = %#v, want %#v", persisted.ThreadIDs, wantThreadIDs)
		}
	}
}

func TestChatStateBootstrapInputsRemainSupported(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		missing bool
		raw     []byte
	}{
		{name: "missing", missing: true},
		{name: "whitespace", raw: []byte(" \n\t\r\n")},
		{name: "empty object", raw: []byte("{}\n")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatalf("NewStore() error = %v", err)
			}
			if testCase.missing {
				if err := os.Remove(store.ChatStatePath()); err != nil {
					t.Fatalf("remove Chat state fixture: %v", err)
				}
			} else if err := os.WriteFile(store.ChatStatePath(), testCase.raw, 0o600); err != nil {
				t.Fatalf("write Chat state fixture: %v", err)
			}

			wantThreadID := "bootstrap-" + strings.ReplaceAll(testCase.name, " ", "-")
			state, err := store.ChatState(wantThreadID)
			if err != nil {
				t.Fatalf("ChatState() error = %v", err)
			}
			if state.ThreadID != wantThreadID || len(state.ThreadIDs) != 1 || state.ThreadIDs[0] != wantThreadID {
				t.Fatalf("bootstrap Chat state = %+v", state)
			}
			persisted, err := os.ReadFile(store.ChatStatePath())
			if err != nil {
				t.Fatalf("read bootstrapped Chat state: %v", err)
			}
			if _, err := decodeChatStateFile(persisted); err != nil {
				t.Fatalf("bootstrapped file is not current schema: %v", err)
			}
		})
	}
}

func TestChatStateInvalidSchemaFailsWithoutMutation(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		raw     []byte
		wantErr string
	}{
		{name: "malformed", raw: []byte("{\"thread_id\":"), wantErr: "decode Brain chat state"},
		{name: "null thread ID", raw: []byte("{\"thread_id\":null}\n"), wantErr: "thread_id must be a JSON string"},
		{name: "null thread IDs", raw: []byte("{\"thread_ids\":null}\n"), wantErr: "thread_ids must be a JSON array of strings"},
		{name: "null thread ID element", raw: []byte("{\"thread_ids\":[\"known\",null]}\n"), wantErr: "thread_ids[1] must be a JSON string"},
		{name: "session IDs", raw: []byte("{\"thread_id\":\"one\",\"session_ids\":[\"host\"]}\n"), wantErr: `unknown field "session_ids"`},
		{name: "transcript cursor", raw: []byte("{\"thread_id\":\"one\",\"last_transcript\":\"cursor\"}\n"), wantErr: `unknown field "last_transcript"`},
		{name: "updated time", raw: []byte("{\"thread_id\":\"one\",\"updated_at\":\"2026-07-17T01:02:03Z\"}\n"), wantErr: `unknown field "updated_at"`},
		{name: "obsolete sessions shape", raw: []byte("{\"sessions\":{\"old-host\":{\"last_transcript\":\"cursor\"}}}\n"), wantErr: `unknown field "sessions"`},
		{name: "other unknown field", raw: []byte("{\"thread_id\":\"one\",\"revision\":2}\n"), wantErr: `unknown field "revision"`},
		{name: "multiple values", raw: []byte("{\"thread_id\":\"one\"} {\"thread_id\":\"two\"}\n"), wantErr: "trailing data"},
		{name: "trailing garbage", raw: []byte("{\"thread_id\":\"one\"} not-json\n"), wantErr: "trailing data"},
		{name: "non-object", raw: []byte("null\n"), wantErr: "expected a JSON object"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			path, before := writeChatStateFixture(t, root, testCase.raw)
			store, err := NewStore(root)
			if err != nil {
				t.Fatalf("NewStore() error = %v", err)
			}

			if _, err := store.ChatState(""); err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("ChatState() error = %v, want containing %q", err, testCase.wantErr)
			}
			assertChatStateFixtureUnchanged(t, path, testCase.raw, before)

			err = store.SetChatState(ChatState{ThreadID: "replacement"})
			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("SetChatState() error = %v, want containing %q", err, testCase.wantErr)
			}
			assertChatStateFixtureUnchanged(t, path, testCase.raw, before)
		})
	}
}

func writeChatStateFixture(t *testing.T, root string, raw []byte) (string, os.FileInfo) {
	t.Helper()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("create state fixture directory: %v", err)
	}
	path := filepath.Join(stateDir, "chat_state.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write Chat state fixture: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat Chat state fixture: %v", err)
	}
	return path, info
}

func assertChatStateFixtureUnchanged(t *testing.T, path string, want []byte, before os.FileInfo) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Chat state fixture: %v", err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat Chat state fixture: %v", err)
	}
	if !bytes.Equal(got, want) || after.Mode() != before.Mode() || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("Chat state fixture changed: bytes_equal=%t mode=%v->%v mtime=%v->%v", bytes.Equal(got, want), before.Mode(), after.Mode(), before.ModTime(), after.ModTime())
	}
}
