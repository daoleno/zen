package server

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/desktop/host"
)

func TestDesktopTrustedPeerRanges(t *testing.T) {
	for _, trusted := range []string{"127.0.0.1:1", "[::1]:1", "10.0.0.5:9", "172.16.4.2:9", "192.168.1.7:9", "100.101.102.103:9", "[fd7a:115c:a1e0::1]:9"} {
		if !desktopTrustedPeer(trusted) {
			t.Fatalf("%s should be a trusted deployment peer", trusted)
		}
	}
	for _, untrusted := range []string{"203.0.113.9:9", "8.8.8.8:9", "[2001:4860:4860::8888]:9", "example.com:9"} {
		if desktopTrustedPeer(untrusted) {
			t.Fatalf("%s must not be a trusted deployment peer", untrusted)
		}
	}
}

func TestDesktopIngressUsesPeerNotHeaders(t *testing.T) {
	s := New(nil, nil, nil, nil, nil, nil, nil)
	s.SetDesktopTrustedNetwork(true)
	request := httptest.NewRequest(http.MethodGet, "/desktop/capability", nil)
	request.RemoteAddr = "203.0.113.9:5555"
	// Forwarded/Host headers never establish trust.
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("Forwarded", "for=127.0.0.1")
	if ingress := s.desktopIngressOf(request); ingress.TLS || ingress.Trusted {
		t.Fatalf("public peer with proxy headers trusted: %+v", ingress)
	}
	request.RemoteAddr = "100.64.1.2:5555"
	if ingress := s.desktopIngressOf(request); ingress.TLS || !ingress.Trusted {
		t.Fatalf("tailnet peer not trusted: %+v", ingress)
	}
}

func TestDesktopTrustedDeploymentAllowsHTTPControl(t *testing.T) {
	t.Setenv("ZEN_STATE_DIR", t.TempDir())
	manager, key, deviceID := sessionFileAuthFixture(t)
	publicHex := hex.EncodeToString(key.Public().(ed25519.PublicKey))
	if _, err := manager.GrantDesktopScope(deviceID, publicHex, auth.DesktopScopeVersion); err != nil {
		t.Fatal(err)
	}
	stubDesktopReadiness(t, host.Readiness{Status: host.ReadinessReady, Broker: true, CurrentSession: true})
	s := New(manager, nil, nil, nil, nil, nil, nil)
	defer s.shutdownAuthenticatedClients()

	// Untrusted (default) plain HTTP keeps refusing.
	plain := httptest.NewServer(s.Handler())
	defer plain.Close()
	status, body := getCapability(t, plain, key, deviceID, manager.DaemonID())
	if status != http.StatusOK || body["connect"].(map[string]any)["reason"] != "desktop_tls_required" {
		t.Fatalf("default HTTP capability = %d %v", status, body)
	}

	// Operator-configured trusted deployment (loopback is always its local hop).
	s.SetDesktopTrustedNetwork(true)
	status, body = getCapability(t, plain, key, deviceID, manager.DaemonID())
	if status != http.StatusOK {
		t.Fatalf("trusted HTTP capability status=%d", status)
	}
	transport := body["transport"].(map[string]any)
	if transport["trusted_ingress"] != true || transport["request_encrypted"] != false {
		t.Fatalf("trusted ingress not truthful: %v", transport)
	}
	if body["connect"].(map[string]any)["unattended"] != true {
		t.Fatalf("trusted HTTP deployment not unattended: %v", body["connect"])
	}
}

func getCapability(t *testing.T, server *httptest.Server, key ed25519.PrivateKey, deviceID, daemonID string) (int, map[string]any) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, server.URL+"/desktop/capability", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", desktopAuthorization(t, key, daemonID, deviceID, auth.DesktopCapabilityPurpose))
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("capability request: %v", err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	var body map[string]any
	_ = json.Unmarshal(bytes.TrimSpace(raw), &body)
	return response.StatusCode, body
}
