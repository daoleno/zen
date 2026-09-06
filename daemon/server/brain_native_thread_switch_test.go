package server

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

type nativeThreadHostWatcher struct {
	brain.Watcher
	host classifier.Worker
}

func (w nativeThreadHostWatcher) GetWorker(id string) *classifier.Worker {
	if id == w.host.ID {
		host := w.host
		return &host
	}
	return nil
}

func TestBrainNativeThreadSwitchRestoresFinalsOverSocketAndReconnect(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires procfs open-file evidence")
	}
	root := t.TempDir()
	store, err := brain.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	const threadID, hostID = "thread-native-switch", "host-native-switch"
	if err := store.SetChatState(brain.ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(t.TempDir(), "rollout-old.jsonl")
	writeServerCodexRollout(t, oldPath, "old-native-thread", "earlier user", "earlier reply", time.Now().Add(-time.Hour))
	if err := store.SetHostProviderTranscript("old-native-thread", oldPath, ""); err != nil {
		t.Fatal(err)
	}
	service := brain.NewService(store, nil, nil)
	old, err := service.HostBoundProviderConversation()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.MaterializeProviderConversation(threadID, old); err != nil {
		t.Fatal(err)
	}

	newPath := filepath.Join(t.TempDir(), ".codex", "sessions", "rollout-current.jsonl")
	if err := os.MkdirAll(filepath.Dir(newPath), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(newPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	write := func(kind string, payload any) {
		t.Helper()
		if err := encoder.Encode(map[string]any{"timestamp": "2026-09-06T23:06:41Z", "type": kind, "payload": payload}); err != nil {
			t.Fatal(err)
		}
	}
	message := func(role, body string) {
		write("response_item", map[string]any{"type": "message", "role": role, "phase": "final_answer", "content": []map[string]string{{"type": "output_text", "text": body}}})
	}
	write("session_meta", map[string]string{"id": "current-native-thread"})
	message("user", "Brain Host activation contract: private bootstrap")
	message("assistant", "private answer must stay hidden")
	message("user", "merge request")
	for i := 0; i < 120; i++ {
		id := fmt.Sprintf("tool-%d", i)
		write("response_item", map[string]string{"type": "function_call", "name": "read_file", "call_id": id, "arguments": `{}`})
		write("response_item", map[string]string{"type": "function_call_output", "call_id": id, "output": "tool result"})
	}
	message("assistant", "merged reply after tool-heavy turn")
	message("user", "automated loop input")
	message("assistant", "")
	message("user", "hi")
	message("assistant", "reply after hi")
	cmd := exec.Command("sleep", "30")
	cmd.ExtraFiles = []*os.File{file}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	hostWatcher := nativeThreadHostWatcher{Watcher: watcher.New(time.Second), host: classifier.Worker{ID: hostID, Command: "codex", ProcessID: cmd.Process.Pid}}
	service = brain.NewService(store, hostWatcher, nil)

	checkSocket := func(service *brain.Service) {
		t.Helper()
		srv := &Server{brain: service, watcher: watcher.New(time.Second), providerConversationLoader: func(*work.ProviderConversationReader, string) (work.CodexConversation, error) {
			return work.CodexConversation{}, nil
		}}
		conn := openThinProxyTestSocket(t, srv)
		defer conn.Close()
		if err := conn.WriteJSON(clientMessage{Type: "codex_conversation_subscribe", RequestID: "reply-check", TargetID: hostID, Command: "codex", StartedAt: json.RawMessage(`"2026-09-06T23:00:00Z"`), ConversationScopeKey: "brain-thread:" + threadID}); err != nil {
			t.Fatal(err)
		}
		if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		var response struct {
			Type         string                 `json:"type"`
			Conversation work.CodexConversation `json:"conversation"`
		}
		if err := conn.ReadJSON(&response); err != nil {
			t.Fatal(err)
		}
		if response.Type != "codex_conversation_snapshot" {
			t.Fatalf("response type = %s", response.Type)
		}
		counts := map[string]int{}
		for _, event := range response.Conversation.Events {
			if event.Kind == "assistant_message" {
				counts[event.Body]++
			}
		}
		for _, body := range []string{"earlier reply", "merged reply after tool-heavy turn", "reply after hi"} {
			if counts[body] != 1 {
				t.Fatalf("assistant %q count=%d; counts=%v", body, counts[body], counts)
			}
		}
		if len(counts) != 3 {
			t.Fatalf("private/empty assistant leaked: %v", counts)
		}
	}
	checkSocket(service)
	host, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if host.ProviderSessionID != "current-native-thread" || host.TranscriptPath != newPath {
		t.Fatalf("stale persisted binding: %+v", host)
	}
	checkSocket(service)
	// Disconnect and reopen the store with no provider file. Durable finals and
	// earlier-thread history must survive without user input or replay.
	if err := os.Remove(newPath); err != nil {
		t.Fatal(err)
	}
	reopened, err := brain.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	checkSocket(brain.NewService(reopened, nil, nil))
}
