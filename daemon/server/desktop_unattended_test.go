package server

import (
	"crypto/ed25519"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/gorilla/websocket"
)

func TestDesktopDefaultRequiresExplicitScopeAndRealTLS(t *testing.T) {
	manager, key, id := sessionFileAuthFixture(t)
	s := New(manager, nil, nil, nil, nil, nil, nil)
	defer s.shutdownAuthenticatedClients()
	plain := httptest.NewServer(s.Handler())
	defer plain.Close()
	encrypted := httptest.NewTLSServer(s.Handler())
	defer encrypted.Close()
	dialer := *websocket.DefaultDialer
	dialer.TLSClientConfig = encrypted.Client().Transport.(*http.Transport).TLSClientConfig
	dial := func(base string, mode string, status int) {
		t.Helper()
		header := http.Header{"Authorization": {desktopAuthorization(t, key, manager.DaemonID(), id, "zen-desktop")}, "X-Forwarded-Proto": {"https"}, "X-Zen-Desktop-Mode": {mode}}
		conn, response, err := dialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/desktop", header)
		if conn != nil {
			conn.Close()
		}
		if err == nil || response == nil || response.StatusCode != status {
			t.Fatalf("expected HTTP%d, got %v %v", status, response, err)
		}
	}
	dial(encrypted.URL, "", 403)
	publicKey := hex.EncodeToString(key.Public().(ed25519.PublicKey))
	token, err := manager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.EnrollDeviceWithDesktopScope(token.Value, manager.DaemonID(), manager.PublicKeyHex(), id, "Fixture", publicKey, 1,
		hex.EncodeToString(ed25519.Sign(key, auth.BuildPairingScopePayload(manager.PublicKeyHex(), token.Value, id, publicKey))))
	if err != nil {
		t.Fatal(err)
	}
	dial(plain.URL, "", 403)
	dial(encrypted.URL, "unknown", 400)
	conn, _, err := dialer.Dial("ws"+strings.TrimPrefix(encrypted.URL, "http")+"/desktop", http.Header{"Authorization": {desktopAuthorization(t, key, manager.DaemonID(), id, "zen-desktop")}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Admission may report unavailable service, but a scoped TLS connection
	// reaches the broker path without an attended start or permission prompt.
	var state struct{ State string }
	if conn.ReadJSON(&state) != nil || (state.State != "unsupported" && state.State != "disconnected") {
		t.Fatal("unexpected broker admission state")
	}
}
