package server

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/desktop/host"
	"github.com/daoleno/zen/daemon/link"
	"github.com/gorilla/websocket"
)

func stubDesktopReadiness(t *testing.T, readiness host.Readiness) {
	t.Helper()
	inspectHostReadiness = func() host.Readiness { return readiness }
	t.Cleanup(func() { inspectHostReadiness = host.InspectReadiness })
}

func fakePairedSessionHelper(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	helper := filepath.Join(dir, "helper")
	args := filepath.Join(dir, "args")
	var packet bytes.Buffer
	payload := []byte(`{"state":"streaming"}`)
	_ = binary.Write(&packet, binary.BigEndian, uint32(len(payload)+1))
	packet.WriteByte(1)
	packet.Write(payload)
	var escaped strings.Builder
	for _, b := range packet.Bytes() {
		fmt.Fprintf(&escaped, "\\%03o", b)
	}
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ZEN_DESKTOP_TEST_ARGS\"\nprintf '" + escaped.String() + "'\ncat >/dev/null\n"
	if err := os.WriteFile(helper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZEN_DESKTOP_HELPER", helper)
	t.Setenv("ZEN_DESKTOP_TEST_ARGS", args)
	return args
}

func TestBDD_DesktopConnectionContract(t *testing.T) {
	manager, key, id := sessionFileAuthFixture(t)
	s := New(manager, nil, nil, nil, nil, nil, nil)
	defer s.shutdownAuthenticatedClients()
	identity, err := link.LoadOrCreateTransportIdentity(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetDesktopTransport(DesktopTransport{TLSConfig: identity.ServerTLSConfig(), Pin: identity.SPKISHA256})
	plain := httptest.NewServer(s.Handler())
	defer plain.Close()
	encrypted := httptest.NewTLSServer(s.Handler())
	defer encrypted.Close()
	dialer := *websocket.DefaultDialer
	dialer.TLSClientConfig = encrypted.Client().Transport.(*http.Transport).TLSClientConfig

	t.Run("missing_encryption", func(t *testing.T) {
		// Given a paired device and unattended default
		// When /desktop is reached over HTTP even with X-Forwarded-Proto
		// Then admission fails closed as desktop_tls_required
		header := http.Header{"Authorization": {desktopAuthorization(t, key, manager.DaemonID(), id, "zen-desktop")}, "X-Forwarded-Proto": {"https"}}
		_, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(plain.URL, "http")+"/desktop", header)
		if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
			t.Fatalf("plaintext unattended: %v %v", response, err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if strings.TrimSpace(string(body)) != "desktop_tls_required" {
			t.Fatalf("plaintext body=%q", body)
		}
	})

	t.Run("legacy_scope", func(t *testing.T) {
		// Given a device without desktop_scope_version 1
		// When /desktop is reached over real TLS
		// Then admission fails as desktop_scope_required, not a combined TLS error
		header := http.Header{"Authorization": {desktopAuthorization(t, key, manager.DaemonID(), id, "zen-desktop")}}
		_, response, err := dialer.Dial("wss"+strings.TrimPrefix(encrypted.URL, "https")+"/desktop", header)
		if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
			t.Fatalf("legacy scope: %v %v", response, err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if strings.TrimSpace(string(body)) != "desktop_scope_required" {
			t.Fatalf("legacy body=%q", body)
		}
	})

	publicKey := hex.EncodeToString(key.Public().(ed25519.PublicKey))
	token, err := manager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.EnrollDeviceWithDesktopScope(token.Value, manager.DaemonID(), manager.PublicKeyHex(), id, "Fixture", publicKey, 1,
		hex.EncodeToString(ed25519.Sign(key, auth.BuildPairingScopePayload(manager.PublicKeyHex(), token.Value, id, publicKey)))); err != nil {
		t.Fatal(err)
	}

	t.Run("host_absent", func(t *testing.T) {
		// Given explicit scope and real TLS
		// When neither current session nor broker is available
		// Then /desktop reports host_setup_required before upgrade
		stubDesktopReadiness(t, host.Readiness{Status: host.ReadinessSetupRequired})
		header := http.Header{"Authorization": {desktopAuthorization(t, key, manager.DaemonID(), id, "zen-desktop")}}
		_, response, err := dialer.Dial("wss"+strings.TrimPrefix(encrypted.URL, "https")+"/desktop", header)
		if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
			t.Fatalf("absent host: %v %v", response, err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if strings.TrimSpace(string(body)) != "host_setup_required" {
			t.Fatalf("host body=%q", body)
		}
	})

	t.Run("capability_actionable_errors", func(t *testing.T) {
		// Given authenticated capability on plaintext
		// Then device trust, unsigned-header TLS, pin signature and host recovery are distinct
		stubDesktopReadiness(t, host.Readiness{Status: host.ReadinessSetupRequired})
		req, err := http.NewRequest(http.MethodGet, plain.URL+"/desktop/capability", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", desktopAuthorization(t, key, manager.DaemonID(), id, auth.DesktopCapabilityPurpose))
		req.Header.Set("X-Forwarded-Proto", "https")
		response, err := plain.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var payload map[string]any
		if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&payload) != nil {
			t.Fatalf("capability HTTP %d", response.StatusCode)
		}
		if payload["device_trust"] != "paired_unattended" {
			t.Fatalf("trust=%v", payload["device_trust"])
		}
		transport, _ := payload["transport"].(map[string]any)
		if transport["request_encrypted"] != false || transport["forwarded_headers_trusted"] != false {
			t.Fatalf("transport=%v", transport)
		}
		if transport["identity_tls"] != true || transport["transport_pin"] != identity.SPKISHA256 {
			t.Fatalf("pin not advertised: %v", transport)
		}
		if !auth.VerifyDesktopCapabilitySignature(manager.PublicKeyHex(), manager.DaemonID(), identity.SPKISHA256, true, payload["capability_signature"].(string)) {
			t.Fatal("capability pin was not daemon-signed")
		}
		if auth.VerifyDesktopCapabilitySignature(manager.PublicKeyHex(), manager.DaemonID(), strings.Repeat("ff", 32), true, payload["capability_signature"].(string)) {
			t.Fatal("forged pin verified")
		}
		connect, _ := payload["connect"].(map[string]any)
		if connect["reason"] != "host_setup_required" || connect["unattended"] != false {
			t.Fatalf("connect=%v", connect)
		}
		if !strings.Contains(connect["recovery"].(string), "current desktop session") {
			t.Fatalf("recovery=%v", connect["recovery"])
		}
	})

	t.Run("current_session_connect", func(t *testing.T) {
		// Given explicit scope, identity TLS, and a current logged-in session
		// When the broker is absent
		// Then Connect is available without claiming lock/login after reboot
		t.Setenv("DISPLAY", "")
		t.Setenv("WAYLAND_DISPLAY", "")
		t.Setenv("ZEN_DESKTOP_DISPLAY", ":owned-test-never-opened")
		argsPath := fakePairedSessionHelper(t)
		stubDesktopReadiness(t, host.Readiness{Status: host.ReadinessSession, CurrentSession: true, Surface: "desktop", Session: "current"})
		req, err := http.NewRequest(http.MethodGet, encrypted.URL+"/desktop/capability", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", desktopAuthorization(t, key, manager.DaemonID(), id, auth.DesktopCapabilityPurpose))
		response, err := encrypted.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&payload) != nil {
			response.Body.Close()
			t.Fatalf("session capability HTTP %d", response.StatusCode)
		}
		response.Body.Close()
		hostStatus, _ := payload["host"].(map[string]any)
		if hostStatus["current_session"] != true || hostStatus["lock_login"] != false || hostStatus["broker"] != false {
			t.Fatalf("host=%v", hostStatus)
		}
		connect, _ := payload["connect"].(map[string]any)
		if connect["unattended"] != true {
			t.Fatalf("session connect=%v", connect)
		}
		header := http.Header{"Authorization": {desktopAuthorization(t, key, manager.DaemonID(), id, "zen-desktop")}}
		conn, response, err := dialer.Dial("wss"+strings.TrimPrefix(encrypted.URL, "https")+"/desktop", header)
		if err != nil {
			t.Fatalf("current session upgrade: %v %v", response, err)
		}
		defer conn.Close()
		var status map[string]any
		if err := conn.ReadJSON(&status); err != nil {
			t.Fatal(err)
		}
		if status["state"] == "sources" || status["state"] == "requesting" {
			t.Fatalf("current session asked for extra setup: %v", status)
		}
		if status["state"] != "streaming" {
			t.Fatalf("current session status: %v", status)
		}
		args, err := os.ReadFile(argsPath)
		if err != nil || !strings.Contains(string(args), "--paired-session") || !strings.Contains(string(args), "--control") {
			t.Fatalf("paired helper args=%q err=%v", args, err)
		}
	})

	t.Run("ready_paired_connect", func(t *testing.T) {
		// Given explicit scope, real TLS, and a ready host probe
		// When /desktop is reached over TLS
		// Then admission upgrades instead of host_setup_required
		stubDesktopReadiness(t, host.Readiness{Status: host.ReadinessReady, Broker: true, Surface: "locked", Session: "locked"})
		req, err := http.NewRequest(http.MethodGet, encrypted.URL+"/desktop/capability", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", desktopAuthorization(t, key, manager.DaemonID(), id, auth.DesktopCapabilityPurpose))
		response, err := encrypted.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&payload) != nil {
			response.Body.Close()
			t.Fatalf("ready capability HTTP %d", response.StatusCode)
		}
		response.Body.Close()
		connect, _ := payload["connect"].(map[string]any)
		if connect["unattended"] != true || connect["reason"] != "" {
			t.Fatalf("ready connect=%v", connect)
		}
		header := http.Header{"Authorization": {desktopAuthorization(t, key, manager.DaemonID(), id, "zen-desktop")}}
		conn, response, err := dialer.Dial("wss"+strings.TrimPrefix(encrypted.URL, "https")+"/desktop", header)
		if err != nil {
			t.Fatalf("ready paired upgrade: %v %v", response, err)
		}
		defer conn.Close()
		var status map[string]any
		if err := conn.ReadJSON(&status); err != nil {
			t.Fatal(err)
		}
		if status["state"] == "denied" {
			t.Fatalf("ready paired denied: %v", status)
		}
	})

	t.Run("locked_session", func(t *testing.T) {
		// Given a ready host reporting the OS lock surface
		// Then capability keeps session state distinct from device trust
		stubDesktopReadiness(t, host.Readiness{Status: host.ReadinessReady, Broker: true, Surface: "locked", Session: "locked"})
		req, err := http.NewRequest(http.MethodGet, encrypted.URL+"/desktop/capability", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", desktopAuthorization(t, key, manager.DaemonID(), id, auth.DesktopCapabilityPurpose))
		response, err := encrypted.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var payload map[string]any
		if json.NewDecoder(response.Body).Decode(&payload) != nil {
			t.Fatal("locked capability decode")
		}
		if payload["device_trust"] != "paired_unattended" {
			t.Fatalf("trust=%v", payload["device_trust"])
		}
		hostStatus, _ := payload["host"].(map[string]any)
		if hostStatus["session"] != "locked" || hostStatus["broker"] != true {
			t.Fatalf("host=%v", hostStatus)
		}
	})

	t.Run("revoked_device", func(t *testing.T) {
		if _, err := manager.RevokeDevice(id); err != nil {
			t.Fatal(err)
		}
		header := http.Header{"Authorization": {desktopAuthorization(t, key, manager.DaemonID(), id, "zen-desktop")}}
		_, response, err := dialer.Dial("wss"+strings.TrimPrefix(encrypted.URL, "https")+"/desktop", header)
		if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("revoked: %v %v", response, err)
		}
		req, err := http.NewRequest(http.MethodGet, encrypted.URL+"/desktop/capability", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", desktopAuthorization(t, key, manager.DaemonID(), id, auth.DesktopCapabilityPurpose))
		response, err = encrypted.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("revoked capability %d", response.StatusCode)
		}
	})
}

func TestBDD_DesktopIdentityTLSSharesHTTPPort(t *testing.T) {
	// Given identity TLS on the daemon listen port
	// When HTTP health and pinned TLS /desktop share that port
	// Then HTTP still works and unattended TLS is actual r.TLS, not a header
	manager, key, id := sessionFileAuthFixture(t)
	s := New(manager, nil, nil, nil, nil, nil, nil)
	defer s.shutdownAuthenticatedClients()
	identity, err := link.LoadOrCreateTransportIdentity(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetDesktopTransport(DesktopTransport{TLSConfig: identity.ServerTLSConfig(), Pin: identity.SPKISHA256})
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener := &tlsHTTPListener{Listener: tcp, config: identity.ServerTLSConfig().Clone()}
	addr := listener.Addr().String()
	srv := &http.Server{Handler: s.Handler()}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(listener) }()
	t.Cleanup(func() {
		_ = srv.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("identity TLS listener did not stop")
		}
	})
	deadline := time.Now().Add(2 * time.Second)
	var health *http.Response
	for time.Now().Before(deadline) {
		health, err = http.Get("http://" + addr + "/health")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("HTTP health: %v", err)
	}
	health.Body.Close()
	if health.StatusCode != 200 {
		t.Fatalf("HTTP health %d", health.StatusCode)
	}
	pinned, err := link.PinnedClientTLSConfig(link.DesktopIdentityServerName, identity.SPKISHA256)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: pinned}}
	tlsHealth, err := client.Get("https://" + addr + "/health")
	if err != nil {
		t.Fatalf("identity TLS health: %v", err)
	}
	tlsHealth.Body.Close()
	if tlsHealth.StatusCode != 200 {
		t.Fatalf("TLS health %d", tlsHealth.StatusCode)
	}
	dialer := websocket.Dialer{TLSClientConfig: pinned, HandshakeTimeout: 2 * time.Second}
	_, response, err := dialer.Dial("wss://"+addr+"/desktop", http.Header{
		"Authorization": {desktopAuthorization(t, key, manager.DaemonID(), id, "zen-desktop")},
	})
	if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("legacy TLS desktop: %v %v", response, err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if strings.TrimSpace(string(body)) != "desktop_scope_required" {
		t.Fatalf("shared-port TLS body=%q", body)
	}
}

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
	dial := func(base string, mode string, status int, want string) {
		t.Helper()
		header := http.Header{"Authorization": {desktopAuthorization(t, key, manager.DaemonID(), id, "zen-desktop")}, "X-Forwarded-Proto": {"https"}, "X-Zen-Desktop-Mode": {mode}}
		conn, response, err := dialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/desktop", header)
		if conn != nil {
			conn.Close()
		}
		if err == nil || response == nil || response.StatusCode != status {
			t.Fatalf("expected HTTP%d, got %v %v", status, response, err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if want != "" && strings.TrimSpace(string(body)) != want {
			t.Fatalf("body=%q want %q", body, want)
		}
	}
	dial(encrypted.URL, "", 403, "desktop_scope_required")
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
	dial(plain.URL, "", 403, "desktop_tls_required")
	dial(encrypted.URL, "unknown", 400, "invalid_desktop_mode")
	stubDesktopReadiness(t, host.Readiness{Status: host.ReadinessSetupRequired})
	dial(encrypted.URL, "", 403, "host_setup_required")
}

func TestDesktopAttendedPlaintextStillAdmitted(t *testing.T) {
	manager, key, device := sessionFileAuthFixture(t)
	s := New(manager, nil, nil, nil, nil, nil, nil)
	defer s.shutdownAuthenticatedClients()
	host := httptest.NewServer(s.Handler())
	defer host.Close()
	url := "ws" + strings.TrimPrefix(host.URL, "http") + "/desktop"
	t.Setenv("ZEN_DESKTOP_HELPER", "")
	t.Setenv("ZEN_DESKTOP_DISPLAY", "")
	t.Setenv("ZEN_DESKTOP_BACKEND", "")
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{
		"Authorization":      {desktopAuthorization(t, key, manager.DaemonID(), device, "zen-desktop")},
		"X-Zen-Desktop-Mode": {"attended"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var status map[string]any
	if err := conn.ReadJSON(&status); err != nil || status["state"] != "unsupported" {
		t.Fatalf("attended plaintext status %v: %v", status, err)
	}
}
