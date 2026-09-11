package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/gorilla/websocket"
)

// A nil-watcher server (the owned-VM fixture shape) must serve an
// authenticated chat WebSocket with an empty worker session list instead of
// panicking the handler process. Regression for the fixture-route
// system-process panic that broke native chat reconnects and, through the
// shared pinned-tunnel key, desktop preflight retries.
func TestChatWebSocketNilWatcherStaysConnected(t *testing.T) {
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := manager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.EnrollDevice(pairing.Value, manager.DaemonID(), manager.PublicKeyHex(),
		"nil-watcher-test", "Nil Watcher Test", hex.EncodeToString(public)); err != nil {
		t.Fatal(err)
	}
	srv := New(manager, nil, nil, nil, nil, nil, nil)
	defer srv.shutdownAuthenticatedClients()
	host := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	defer host.Close()
	url := "ws" + strings.TrimPrefix(host.URL, "http")
	headers := http.Header{"Authorization": []string{
		calendarAuthHeader(private, manager.DaemonID(), "nil-watcher-test", "zen-connect")}}
	conn, _, err := websocket.DefaultDialer.Dial(url, headers)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var list struct {
		Type           string `json:"type"`
		WorkerSessions []any  `json:"worker_sessions"`
	}
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("chat socket closed on nil-watcher bootstrap: %v", err)
		}
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			continue
		}
		if envelope.Type != "worker_session_list" {
			continue
		}
		if err := json.Unmarshal(raw, &list); err != nil {
			t.Fatal(err)
		}
		break
	}
	if len(list.WorkerSessions) != 0 {
		t.Fatalf("nil watcher must list zero sessions, got %d", len(list.WorkerSessions))
	}
	// The connection must remain usable afterwards: a follow-up read with no
	// traffic must time out, not report a server-side close.
	conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("unexpected message")
	} else if websocket.IsCloseError(err,
		websocket.CloseNormalClosure, websocket.CloseGoingAway,
		websocket.CloseAbnormalClosure, websocket.CloseTryAgainLater) {
		t.Fatalf("chat socket closed after nil-watcher list: %v", err)
	}
}
