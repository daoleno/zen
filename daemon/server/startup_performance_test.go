package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
	"github.com/gorilla/websocket"
)

// Opt-in measurement: the input must be a sanitized fixture, never live state.
// Copies it before timing. No scheduler, tmux poll, provider process, machine
// gateway, notification or transcript capture runs in this component harness.
func TestPopulatedStartupMeasurement(t *testing.T) {
	source := os.Getenv("ZEN_STARTUP_FIXTURE")
	if source == "" {
		t.Skip("set ZEN_STARTUP_FIXTURE to sanitized fixture")
	}
	marker, err := os.ReadFile(filepath.Join(source, "startup-fixture.marker"))
	if err != nil || strings.TrimSpace(string(marker)) != "sanitized-startup-fixture-v1" {
		t.Fatal("measurement requires an explicitly sanitized startup fixture")
	}
	root := t.TempDir()
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(root, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		if !entry.Type().IsRegular() {
			t.Fatal("fixture must contain only regular files")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, raw, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	phases := map[string]float64{}
	mark := func(name string) {
		phases[name] = time.Since(started).Seconds()
		t.Logf("phase %s %.6f", name, phases[name])
	}
	manager, err := auth.NewManager(filepath.Join(root, "auth"))
	if err != nil {
		t.Fatal(err)
	}
	mark("auth")
	workStore, err := work.NewStore(filepath.Join(root, "work"))
	if err != nil {
		t.Fatal(err)
	}
	defer workStore.Close()
	mark("work_store")
	store, err := brain.NewStore(filepath.Join(root, "brain"))
	if err != nil {
		t.Fatal(err)
	}
	mark("brain_store")
	w := watcher.New(time.Second)
	w.SetTmuxServer(filepath.Join(root, "tmux.sock"), filepath.Join(root, "tmux-scratch"))
	service := brain.NewService(store, w, nil)
	srv := New(manager, w, nil, nil, workStore, nil, service)
	defer srv.shutdownAuthenticatedClients()
	srv.providerConversationLoader = func(*work.ProviderConversationReader, string) (work.CodexConversation, error) {
		return work.CodexConversation{Events: []work.CodexConversationEvent{}}, nil
	}
	host := httptest.NewServer(srv.Handler())
	defer host.Close()
	mark("listener")
	pairing, err := manager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.EnrollDevice(pairing.Value, manager.DaemonID(), manager.PublicKeyHex(), "fixture-phone", "Fixture", hex.EncodeToString(pub))
	if err != nil {
		t.Fatal(err)
	}
	header := http.Header{"Authorization": []string{calendarAuthHeader(priv, manager.DaemonID(), "fixture-phone", "zen-connect")}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(host.URL, "http")+"/ws", header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	mark("authenticated_ws")
	read := func(want string) map[string]json.RawMessage {
		t.Helper()
		conn.SetReadDeadline(time.Now().Add(20 * time.Second))
		for {
			var payload map[string]json.RawMessage
			if err := conn.ReadJSON(&payload); err != nil {
				t.Fatal(err)
			}
			var kind string
			json.Unmarshal(payload["type"], &kind)
			if kind == "error" {
				t.Fatal("wire error during startup measurement")
			}
			if kind == want {
				return payload
			}
		}
	}
	read("worker_session_list")
	mark("session_list")
	read("brain_snapshot")
	mark("brain_list")
	threads, err := store.ChatThreadIDs()
	if err != nil || len(threads) == 0 {
		t.Fatal("fixture requires populated threads")
	}
	thread := threads[0]
	largest := 0
	for _, candidate := range threads {
		items, err := store.ThreadTimeline(candidate, 240)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) > largest {
			thread, largest = candidate, len(items)
		}
	}
	if largest < 100 {
		t.Fatal("fixture needs a populated history")
	}
	if err := conn.WriteJSON(clientMessage{Type: "codex_conversation_subscribe", RequestID: "open", TargetID: "fixture-provider", Command: "codex", StartedAt: json.RawMessage(`"2026-09-01T00:00:00Z"`), Cwd: root, ConversationScopeKey: "brain-thread:" + thread}); err != nil {
		t.Fatal(err)
	}
	payload := read("codex_conversation_snapshot")
	var conversation work.CodexConversation
	if err := json.Unmarshal(payload["conversation"], &conversation); err != nil {
		t.Fatal(err)
	}
	if !conversation.Available || len(conversation.Events) == 0 {
		t.Fatal("existing conversation not available")
	}
	mark("conversation_open")
	raw, _ := json.Marshal(map[string]any{"seconds_from_start": phases, "conversation_events": len(conversation.Events), "threads": len(threads)})
	t.Log(string(raw))
}
