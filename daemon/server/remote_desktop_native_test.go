package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// This opt-in fixture uses fresh test identities, never the user's daemon state.
func TestDesktopOwnedNativeHarness(t *testing.T) {
	output := os.Getenv("ZEN_DESKTOP_NATIVE_TEST_DIR")
	if output == "" {
		t.Skip("requires an explicitly owned native test runtime")
	}
	if !filepath.IsAbs(output) || os.Getenv("ZEN_DESKTOP_DISPLAY") == "" {
		t.Fatal("explicit owned directory and display required")
	}
	manager, key, device := sessionFileAuthFixture(t)
	s := New(manager, nil, nil, nil, nil, nil, nil)
	defer s.shutdownAuthenticatedClients()
	tlsHost := httptest.NewTLSServer(s.Handler())
	defer tlsHost.Close()
	_, tlsPort, _ := net.SplitHostPort(tlsHost.Listener.Addr().String())
	pin := sha256.Sum256(tlsHost.Certificate().RawSubjectPublicKeyInfo)
	fixture := http.NewServeMux()
	fixture.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"port": tlsPort, "pin": hex.EncodeToString(pin[:]),
			"authorization": desktopAuthorization(t, key, manager.DaemonID(), device, "zen-desktop"),
		})
	})
	fixture.HandleFunc("/revoke", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		_, _ = manager.RevokeDevice(device)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:18089")
	if err != nil {
		t.Fatal(err)
	}
	httpHost := &http.Server{Handler: fixture, ReadHeaderTimeout: time.Second}
	defer httpHost.Close()
	go httpHost.Serve(listener)
	if err := os.WriteFile(filepath.Join(output, "ready"), []byte(tlsPort), 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("owned harness ready; stops when stop file exists or after ten minutes")
	deadline := time.After(10 * time.Minute)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("owned harness timed out")
		case <-tick.C:
			if _, err := os.Stat(filepath.Join(output, "stop")); err == nil {
				return
			}
		}
	}
}
