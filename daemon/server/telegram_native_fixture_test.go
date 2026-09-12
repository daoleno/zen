//go:build linux

package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/telegram"
)

// Opt-in, loopback-only native UI fixture. Real authentication/WS handlers,
// disposable state, no provider and no Telegram network access.
func TestTelegramNativeFixture(t *testing.T) {
	root := os.Getenv("ZEN_TELEGRAM_UI_FIXTURE_DIR")
	if root == "" {
		t.Skip("owned native UI fixture only")
	}
	state, err := os.MkdirTemp(root, "state-")
	if err != nil {
		t.Fatal(err)
	}
	a, err := auth.NewManager(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(state, "telegram"), 0700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{"schema": 4, "enabled": true, "bot_id": 7001, "bot_name": "Zen fixture", "bot_username": "zen_fixture_bot", "owner_id": 10, "chat_id": 10, "owner_hint": "@fixture_owner", "topics_available": true, "brain_topic_id": 42, "delivery_started_at": time.Now(), "fallback_session_id": "fixture-session", "fallback_started_at": time.Now()})
	if err := os.WriteFile(filepath.Join(state, "telegram", "state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	m, err := telegram.NewManagerWithOptions(state, nil, telegram.Options{API: &nativeTelegramAPI{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Configure(t.Context(), "fixture-token"); err != nil {
		t.Fatal(err)
	}
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetTelegram(m)
	mux := http.NewServeMux()
	mux.Handle("/", s.Handler())
	stop := make(chan struct{}, 1)
	mux.HandleFunc("/fixture", func(w http.ResponseWriter, r *http.Request) {
		var err error
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return
		}
		switch r.URL.Query().Get("action") {
		case "remove":
			err = m.Remove()
		case "disable":
			err = m.Disable()
		case "enable":
			err = m.Enable()
		case "stop":
			select {
			case stop <- struct{}{}:
			default:
			}
		default:
			w.WriteHeader(400)
			return
		}
		if err != nil {
			w.WriteHeader(500)
		}
	})
	h := httptest.NewUnstartedServer(mux)
	h.Listener.Close()
	h.Listener, err = net.Listen("tcp", "127.0.0.1:19879")
	if err != nil {
		t.Fatal(err)
	}
	h.StartTLS()
	defer h.Close()
	defer s.shutdownAuthenticatedClients()
	link, err := mintFixtureLink(a, h.Certificate(), "https://10.0.2.2:19879")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pairing-link"), []byte(link), 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("Native fixture ready on loopback 19879; pairing link stays in private fixture directory")
	select {
	case <-stop:
	case <-time.After(20 * time.Minute):
	}
}

type nativeTelegramAPI struct{ telegram.API }

func (*nativeTelegramAPI) GetMe(context.Context, string) (telegram.User, error) {
	return telegram.User{ID: 7001, IsBot: true, Username: "zen_fixture_bot", Topics: true}, nil
}
func (*nativeTelegramAPI) GetWebhookInfo(context.Context, string) (telegram.WebhookInfo, error) {
	return telegram.WebhookInfo{}, nil
}
