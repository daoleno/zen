package server

import (
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
)

// This is a disposable-VM test entry point, not a production server mode. The
// root-created marker and QEMU DMI check prevent accidental personal-host use.
func TestDesktopHostVMFixture(t *testing.T) {
	state := os.Getenv("ZEN_DESKTOP_OWNED_VM_STATE")
	if state == "" {
		t.Skip("requires the explicitly owned QEMU fixture")
	}
	marker, err := os.Stat("/run/zen-owned-desktop-fixture")
	if err != nil || marker.Sys().(*syscall.Stat_t).Uid != 0 || marker.Mode().Perm() != 0600 || os.Geteuid() == 0 {
		t.Fatal("owned non-root guest fixture required")
	}
	vendor, err := os.ReadFile("/sys/class/dmi/id/sys_vendor")
	if err != nil || strings.TrimSpace(string(vendor)) != "QEMU" || !filepath.IsAbs(state) {
		t.Fatal("QEMU guest identity required")
	}
	m, err := auth.NewManager(state)
	if err != nil {
		t.Fatal("fixture identity unavailable")
	}
	s := New(m, nil, nil, nil, nil, nil, nil)
	defer s.shutdownAuthenticatedClients()
	mux := http.NewServeMux()
	mux.Handle("/", s.Handler())
	mux.HandleFunc("/fixture-token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.TLS == nil {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		token, err := m.IssuePairingToken(time.Minute)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": token.Value})
	})
	mux.HandleFunc("/fixture-revoke", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.TLS == nil {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var request struct{ Device string }
		if json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&request) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, err := m.RevokeDevice(request.Device)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	host := httptest.NewUnstartedServer(mux)
	host.Listener.Close()
	host.Listener, err = net.Listen("tcp", "127.0.0.1:19876")
	if err != nil {
		t.Fatal("fixture port unavailable")
	}
	// Owned-VM native route: serve a test-CA-chained certificate (files
	// injected by the owned harness) so the debuggable product client can
	// complete system TLS trust. Daemon authentication stays key-bound
	// (signed /pair and capability assertions); the CA is test-only and
	// never a production trust anchor.
	var rawCert []byte
	if certFile, keyFile := os.Getenv("ZEN_FIXTURE_TLS_CERT"), os.Getenv("ZEN_FIXTURE_TLS_KEY"); certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil || len(cert.Certificate) == 0 {
			t.Fatal("fixture test certificate unavailable")
		}
		host.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
		host.StartTLS()
		rawCert = cert.Certificate[0]
	} else {
		host.StartTLS()
		rawCert = host.Certificate().Raw
	}
	defer host.Close()
	ready, _ := json.Marshal(map[string]any{
		"hostId": m.DaemonID(), "publicKey": m.PublicKeyHex(), "ownerUid": os.Getuid(),
		"certificate": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rawCert})),
	})
	if os.WriteFile(filepath.Join(state, "ready.json"), ready, 0644) != nil {
		t.Fatal("fixture readiness unavailable")
	}
	// systemd owns termination and restart; credentials never leave process memory
	// except canonical encrypted /pair/auth traffic and ordinary daemon state.
	select {}
}
