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
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/gorilla/websocket"
)

// The mobile Services sheet renders exactly what list_session_services
// returns. Persistent rows must arrive with their source/unit/state identity
// and without an invented live worker id, or the sheet would offer a dead
// terminal action.
func TestListSessionServicesIncludesPersistentRows(t *testing.T) {
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	deviceID := "device-services"
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), deviceID, "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	w := watcher.New(time.Second)
	w.SetManagedDiscoveryFunc(func(claimed map[string]bool, interfaces []watcher.SessionServiceInterface) []watcher.SessionService {
		return []watcher.SessionService{{
			ID:         "persistent:dsh-web.service:1610722:3080",
			WorkerName: "DeepSeek Harness",
			Project:    "dsh-smoke",
			PID:        1610722,
			Port:       3080,
			Protocol:   "tcp",
			Binds:      []string{"127.0.0.1"},
			URLs:       []watcher.SessionServiceURL{},
			LocalOnly:  true,
			Source:     watcher.ServiceSourcePersistent,
			Unit:       "dsh-web.service",
			State:      watcher.ServiceStateActive,
		}}
	})
	srv := New(authManager, w, nil, nil, nil, nil, nil)
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	t.Cleanup(httpServer.Close)
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), deviceID, "zen-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if err := conn.WriteJSON(map[string]any{"type": "list_session_services", "request_id": "svc-1"}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, raw, readErr := conn.ReadMessage()
		if readErr != nil {
			t.Fatal(readErr)
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["type"] != "session_service_list" || payload["request_id"] != "svc-1" {
			continue
		}
		services, ok := payload["services"].([]any)
		if !ok || len(services) != 1 {
			t.Fatalf("services = %v, want one persistent row", payload["services"])
		}
		row, ok := services[0].(map[string]any)
		if !ok {
			t.Fatalf("row = %v", services[0])
		}
		if row["source"] != "persistent" || row["unit"] != "dsh-web.service" || row["state"] != "active" {
			t.Fatalf("row identity = %v, want persistent/dsh-web.service/active", row)
		}
		if workerID, _ := row["worker_id"].(string); workerID != "" {
			t.Fatalf("persistent row worker_id = %q, want empty", workerID)
		}
		if port, _ := row["port"].(float64); port != 3080 {
			t.Fatalf("row port = %v, want 3080", row["port"])
		}
		if localOnly, _ := row["local_only"].(bool); !localOnly {
			t.Fatalf("loopback row local_only = %v, want true", row["local_only"])
		}
		rawJSON, _ := json.Marshal(row)
		if strings.Contains(string(rawJSON), "token=") {
			t.Fatalf("token-bearing URL leaked into discovery row: %s", rawJSON)
		}
		return
	}
}
