package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/calendar"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/gorilla/websocket"
)

func TestAuthenticatedCalendarWebSocketCRUD(t *testing.T) {
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), "device-calendar", "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	store, _ := calendar.NewStore(t.TempDir())
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv.SetCalendar(store, calendar.NewScheduler(store, nil))
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	defer httpServer.Close()
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), "device-calendar", "zen-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	readType := func(want string) map[string]any {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		for {
			_, raw, readErr := conn.ReadMessage()
			if readErr != nil {
				t.Fatal(readErr)
			}
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["type"] == want {
				return payload
			}
		}
	}
	snapshot := readType("calendar_items_snapshot")
	if len(snapshot["calendar_items"].([]any)) != 0 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	notify := time.Date(2026, time.July, 15, 9, 30, 0, 0, time.FixedZone("CST", 8*3600))
	if err := conn.WriteJSON(map[string]any{"type": "create_calendar_item", "request_id": "create-1", "calendar_item": calendar.Item{Title: "Morning review", Kind: calendar.KindReminder, NotifyAt: &notify, Timezone: "Asia/Shanghai", Recurrence: calendar.RecurrenceNone}}); err != nil {
		t.Fatal(err)
	}
	created := readType("calendar_item_created")
	if created["request_id"] != "create-1" {
		t.Fatalf("created = %#v", created)
	}
	itemRaw, _ := json.Marshal(created["calendar_item"])
	var item calendar.Item
	if err := json.Unmarshal(itemRaw, &item); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(map[string]any{"type": "cancel_calendar_item", "request_id": "cancel-1", "id": item.ID, "revision": item.Revision}); err != nil {
		t.Fatal(err)
	}
	cancelled := readType("calendar_item_cancelled")
	raw, _ := json.Marshal(cancelled["calendar_item"])
	if err := json.Unmarshal(raw, &item); err != nil {
		t.Fatal(err)
	}
	if item.Status != calendar.StatusCancelled {
		t.Fatalf("item = %#v", item)
	}
}

func calendarAuthHeader(privateKey ed25519.PrivateKey, daemonID, deviceID, purpose string) string {
	timestamp := strconv.FormatInt(time.Now().UTC().UnixMilli(), 10)
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	nonceHex := hex.EncodeToString(nonce)
	signature := ed25519.Sign(privateKey, auth.BuildSignaturePayload(purpose, daemonID, deviceID, timestamp, nonceHex))
	return auth.AuthorizationHeaderPrefix + "v1:" + deviceID + ":" + daemonID + ":" + timestamp + ":" + nonceHex + ":" + hex.EncodeToString(signature)
}
