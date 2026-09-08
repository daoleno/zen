package server

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
)

func TestPairDesktopScopeRequiresExplicitBoundProof(t *testing.T) {
	m, key, id := sessionFileAuthFixture(t)
	s := New(m, nil, nil, nil, nil, nil, nil)
	defer s.shutdownAuthenticatedClients()
	pub := hex.EncodeToString(key.Public().(ed25519.PublicKey))
	token, err := m.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"enrollment_token": token.Value, "expected_daemon_id": m.DaemonID(), "expected_daemon_public_key": m.PublicKeyHex(), "device_id": id, "device_name": "Fixture", "device_public_key": pub, "desktop_scope_version": 1}
	validBody, _ := json.Marshal(body)
	for _, invalidBody := range []string{string(validBody) + "{}", string(validBody) + strings.Repeat(" ", 8192)} {
		r := httptest.NewRequest(http.MethodPost, "/pair", strings.NewReader(invalidBody))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("unbounded or trailing pairing body: %d", w.Code)
		}
	}
	post := func() *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		r := httptest.NewRequest(http.MethodPost, "/pair", bytes.NewReader(data))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if w := post(); w.Code != http.StatusUnauthorized {
		t.Fatalf("scope without proof: %d", w.Code)
	}
	if m.HasDesktopScope(id, pub) {
		t.Fatal("scope obtained from legacy terminal trust")
	}
	body["desktop_scope_signature"] = hex.EncodeToString(ed25519.Sign(key, auth.BuildPairingScopePayload(m.PublicKeyHex(), token.Value, id, pub)))
	if w := post(); w.Code != http.StatusOK {
		t.Fatalf("explicit migration: %d", w.Code)
	}
	if !m.HasDesktopScope(id, pub) || len(m.ListDevices()) != 1 {
		t.Fatal("same identity was not migrated")
	}
	if w := post(); w.Code != http.StatusUnauthorized {
		t.Fatal("pair scope replay accepted")
	}
	r := httptest.NewRequest(http.MethodGet, "/auth-check", nil)
	r.Header.Set("Authorization", desktopAuthorization(t, key, m.DaemonID(), id, "zen-probe"))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var result struct {
		Scope int `json:"desktop_scope_version"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Scope != 1 {
		t.Fatal("authenticated scope not reported")
	}
	if _, err := m.RevokeDevice(id); err != nil {
		t.Fatal(err)
	}
	if m.HasDesktopScope(id, pub) {
		t.Fatal("scope outlived canonical revocation")
	}
}
