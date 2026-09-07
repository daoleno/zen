package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGitDiffPageAuthenticatedWebSocket(t *testing.T) {
	repo := initGitDiffTestRepo(t)
	writeGitDiffTestFile(t, repo, " odd\tfile ", "new\n")
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := manager.IssuePairingToken(time.Minute)
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	if _, err = manager.EnrollDevice(pairing.Value, manager.DaemonID(), manager.PublicKeyHex(), "diff-test", "Test", hex.EncodeToString(public)); err != nil {
		t.Fatal(err)
	}
	srv := New(manager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	host := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	defer host.Close()
	url := "ws" + strings.TrimPrefix(host.URL, "http")
	if conn, _, err := websocket.DefaultDialer.Dial(url, nil); err == nil {
		conn.Close()
		t.Fatal("unauthenticated diff socket accepted")
	}
	headers := http.Header{"Authorization": []string{calendarAuthHeader(private, manager.DaemonID(), "diff-test", "zen-connect")}}
	conn, _, err := websocket.DefaultDialer.Dial(url, headers)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err = conn.WriteJSON(map[string]any{"type": "git_diff_page", "request_id": "page-test", "cwd": repo, "path": " odd\tfile ", "scope": "all", "row": 0, "query": "new"}); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var response struct {
			Type      string      `json:"type"`
			RequestID string      `json:"request_id"`
			Page      gitDiffPage `json:"page"`
		}
		if err = json.Unmarshal(raw, &response); err != nil {
			t.Fatal(err)
		}
		if response.RequestID != "page-test" {
			continue
		}
		if response.Type != "git_diff_page" || response.Page.Path != " odd\tfile " || len(response.Page.Rows) == 0 {
			t.Fatalf("response: %s", raw)
		}
		break
	}
}
