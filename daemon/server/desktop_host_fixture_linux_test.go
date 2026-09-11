package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/link"
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
	var servedCert *x509.Certificate
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
	// Test-only Link v2 pairing mint for the owned native route. The payload
	// is the exact production shape (daemon-signed binding, SPKI pin of the
	// served TLS cert, emulator-visible admission URL); no relay is involved
	// at any step — the client tunnels directly to the candidate with the
	// pin. Never exposed outside the owned VM harness.
	mux.HandleFunc("/fixture-link", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.TLS == nil {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			BaseURL string `json:"base_url"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&request) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		link, err := mintFixtureLink(m, servedCert, request.BaseURL)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"link": link})
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
	var served *x509.Certificate
	if certFile, keyFile := os.Getenv("ZEN_FIXTURE_TLS_CERT"), os.Getenv("ZEN_FIXTURE_TLS_KEY"); certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil || len(cert.Certificate) == 0 {
			t.Fatal("fixture test certificate unavailable")
		}
		host.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
		host.StartTLS()
		served, err = x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			t.Fatal("fixture test certificate unparsable")
		}
	} else {
		host.StartTLS()
		served = host.Certificate()
	}
	defer host.Close()
	servedCert = served
	rawCert := servedCert.Raw
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

// mintFixtureLink builds a test-only Link v2 pairing link for the owned
// native route: daemon-signed binding, SPKI pin of the served TLS cert, and
// the emulator-visible admission URL. No relay is involved at any step.
func mintFixtureLink(m *auth.Manager, servedCert *x509.Certificate, baseURL string) (string, error) {
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || base.Scheme != "https" || base.Host == "" || servedCert == nil {
		return "", errors.New("invalid fixture link base")
	}
	base.RawQuery, base.Fragment, base.Path = "", "", ""
	token, err := m.IssuePairingToken(10 * time.Minute)
	if err != nil {
		return "", err
	}
	var route [16]byte
	if _, err := rand.Read(route[:]); err != nil {
		return "", err
	}
	pin := sha256.Sum256(servedCert.RawSubjectPublicKeyInfo)
	payload := link.PairingPayload{
		Version:         link.PairingVersion,
		DaemonID:        m.DaemonID(),
		DaemonPublicKey: m.PublicKeyHex(),
		EnrollmentToken: strings.ToLower(strings.TrimSpace(token.Value)),
		RouteID:         hex.EncodeToString(route[:]),
		TransportPin:    hex.EncodeToString(pin[:]),
		Candidates: []link.PairingCandidate{
			{Name: "Owned VM", AdmissionURL: base.String(), StableURL: base.String()},
		},
		ExpiresAtMS: time.Now().Add(10 * time.Minute).UnixMilli(),
	}
	payload.Signature = m.CreateLinkPairingSignature(link.PairingBindingPayload(payload))
	if err := link.ValidatePairingPayload(payload, time.Now()); err != nil {
		return "", err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	values := url.Values{}
	values.Set("v", fmt.Sprintf("%d", link.PairingVersion))
	values.Set("p", base64.RawURLEncoding.EncodeToString(raw))
	return "zen://settings?" + values.Encode(), nil
}
