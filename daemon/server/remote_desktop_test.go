package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/gorilla/websocket"
)

func desktopAuthorization(t *testing.T, key ed25519.PrivateKey, daemon, device, purpose string) string {
	t.Helper()
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	ts, n := fmt.Sprint(time.Now().UnixMilli()), hex.EncodeToString(nonce)
	sig := ed25519.Sign(key, auth.BuildSignaturePayload(purpose, daemon, device, ts, n))
	return fmt.Sprintf("%sv1:%s:%s:%s:%s:%x", auth.AuthorizationHeaderPrefix, device, daemon, ts, n, sig)
}

func TestDesktopRequiresIndependentPurposeNonceAndHeader(t *testing.T) {
	manager, key, device := sessionFileAuthFixture(t)
	s := New(manager, nil, nil, nil, nil, nil, nil)
	defer s.shutdownAuthenticatedClients()
	host := httptest.NewServer(s.Handler())
	defer host.Close()
	url := "ws" + strings.TrimPrefix(host.URL, "http") + "/desktop"
	header := desktopAuthorization(t, key, manager.DaemonID(), device, "zen-desktop")
	for _, bad := range []string{"", desktopAuthorization(t, key, manager.DaemonID(), device, "zen-connect")} {
		conn, response, err := websocket.DefaultDialer.Dial(url, http.Header{"Authorization": {bad}})
		if conn != nil {
			conn.Close()
		}
		if err == nil || response == nil || response.StatusCode != 401 {
			t.Fatalf("unauthorized upgrade: %v %v", response, err)
		}
	}
	t.Setenv("ZEN_DESKTOP_HELPER", "")
	t.Setenv("ZEN_DESKTOP_DISPLAY", "")
	t.Setenv("ZEN_DESKTOP_BACKEND", "")
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{"Authorization": {header}, "X-Zen-Desktop-Mode": {"attended"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var status map[string]any
	if err := conn.ReadJSON(&status); err != nil || status["state"] != "unsupported" {
		t.Fatalf("status %v: %v", status, err)
	}
	_, response, err := websocket.DefaultDialer.Dial(url, http.Header{"Authorization": {header}})
	if err == nil || response.StatusCode != 401 {
		t.Fatal("desktop nonce replay accepted")
	}
	_, response, err = websocket.DefaultDialer.Dial(url+"?auth=ignored", http.Header{"Authorization": {desktopAuthorization(t, key, manager.DaemonID(), device, "zen-desktop")}})
	if err == nil || response.StatusCode != 401 {
		t.Fatal("query credentials accepted")
	}
}

func TestDesktopRevocationAndShutdownCloseSeparateMediaOwner(t *testing.T) {
	for _, action := range []string{"revoke", "shutdown"} {
		t.Run(action, func(t *testing.T) {
			manager, key, device := sessionFileAuthFixture(t)
			s := New(manager, nil, nil, nil, nil, nil, nil)
			defer s.shutdownAuthenticatedClients()
			t.Setenv("ZEN_DESKTOP_HELPER", "/bin/true")
			t.Setenv("ZEN_DESKTOP_DISPLAY", ":owned-test-never-opened")
			host := httptest.NewServer(s.Handler())
			defer host.Close()
			url := "ws" + strings.TrimPrefix(host.URL, "http") + "/desktop"
			dial := func() *websocket.Conn {
				conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{"Authorization": {desktopAuthorization(t, key, manager.DaemonID(), device, "zen-desktop")}, "X-Zen-Desktop-Mode": {"attended"}})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { conn.Close() })
				return conn
			}
			first := dial()
			var status map[string]any
			if err := first.ReadJSON(&status); err != nil || status["state"] != "sources" {
				t.Fatalf("source state %v %v", status, err)
			}
			if s.clientCount() != 0 {
				t.Fatal("media connection joined chat broadcast registry")
			}
			second := dial()
			_ = second.SetReadDeadline(time.Now().Add(time.Second))
			if _, _, err := second.ReadMessage(); err == nil {
				t.Fatal("second connection acquired desktop")
			}
			if action == "revoke" {
				if _, err := manager.RevokeDevice(device); err != nil {
					t.Fatal(err)
				}
			} else {
				s.shutdownAuthenticatedClients()
			}
			_ = first.SetReadDeadline(time.Now().Add(time.Second))
			if _, _, err := first.ReadMessage(); err == nil {
				t.Fatal("media owner survived revocation/shutdown")
			}
		})
	}
}
