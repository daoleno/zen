package server

import (
	"bytes"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/desktop/host"
)

func hostAdmission(deviceID string) (string, error) {
	return host.SunshineAdmission(deviceID)
}

type enrollClient struct {
	key     *rsa.PrivateKey
	certPEM string
}

func newEnrollClient(t *testing.T) enrollClient {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "NVIDIA GameStream Client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return enrollClient{key: key, certPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}
}

func (c enrollClient) sign(t *testing.T, attempt, nonce string) string {
	t.Helper()
	message := []byte("zen-moonlight-enroll-v1\x00" + attempt + "\n" + nonce)
	digest := sha256.Sum256(message)
	signature, err := rsa.SignPKCS1v15(rand.Reader, c.key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(signature)
}

func writeEnrollState(t *testing.T, stateDir string, entries ...map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"root": map[string]any{"named_devices": entries}})
	if err := os.WriteFile(filepath.Join(stateDir, "sunshine_state.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func postEnroll(t *testing.T, server *httptest.Server, key ed25519.PrivateKey, daemonID, deviceID, path string, payload map[string]any) (int, map[string]any) {
	t.Helper()
	encoded, _ := json.Marshal(payload)
	request, err := http.NewRequest(http.MethodPost, server.URL+path, bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", desktopAuthorization(t, key, daemonID, deviceID, auth.DesktopCapabilityPurpose))
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	var decoded map[string]any
	_ = json.Unmarshal(body, &decoded)
	return response.StatusCode, decoded
}

func enrollFixture(t *testing.T) (*httptest.Server, ed25519.PrivateKey, string, string) {
	t.Helper()
	server, keyA, deviceA, daemonID, _, _ := enrollFixtureAB(t)
	_ = deviceA
	return server, keyA, deviceA, daemonID
}

func enrollFixtureAB(t *testing.T) (*httptest.Server, ed25519.PrivateKey, string, string, ed25519.PrivateKey, string) {
	t.Helper()
	t.Setenv("ZEN_STATE_DIR", t.TempDir())
	manager, keyA, deviceA := sessionFileAuthFixture(t)
	publicA := hex.EncodeToString(keyA.Public().(ed25519.PublicKey))
	if _, err := manager.GrantDesktopScope(deviceA, publicA, auth.DesktopScopeVersion); err != nil {
		t.Fatal(err)
	}
	publicB, keyB, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	token, err := manager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	deviceB := "session-file-device-b"
	if _, err := manager.EnrollDevice(token.Value, manager.DaemonID(), manager.PublicKeyHex(), deviceB, "Second device", hex.EncodeToString(publicB)); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.GrantDesktopScope(deviceB, hex.EncodeToString(publicB), auth.DesktopScopeVersion); err != nil {
		t.Fatal(err)
	}
	s := New(manager, nil, nil, nil, nil, nil, nil)
	t.Cleanup(s.shutdownAuthenticatedClients)
	server := httptest.NewTLSServer(s.Handler())
	t.Cleanup(server.Close)
	return server, keyA, deviceA, manager.DaemonID(), keyB, deviceB
}

func TestMoonlightEnrollmentProvesKeyPossessionAndBindsGeneratedUUID(t *testing.T) {
	server, key, deviceID, daemonID := enrollFixture(t)
	client := newEnrollClient(t)
	writeEnrollState(t, os.Getenv("ZEN_STATE_DIR"), map[string]any{
		"name": "phone", "cert": client.certPEM, "uuid": "host-uuid-a", "enabled": true,
	})

	attempt := hex.EncodeToString(bytes.Repeat([]byte{0xab}, 16))
	status, begin := postEnroll(t, server, key, daemonID, deviceID, "/desktop/moonlight/enroll/begin", map[string]any{"attempt": attempt})
	if status != http.StatusOK {
		t.Fatalf("begin status=%d body=%v", status, begin)
	}
	nonce, _ := begin["nonce"].(string)
	if nonce == "" {
		t.Fatalf("missing nonce: %v", begin)
	}
	// A forged signature (wrong key) must be rejected.
	other := newEnrollClient(t)
	status, _ = postEnroll(t, server, key, daemonID, deviceID, "/desktop/moonlight/enroll/complete", map[string]any{
		"attempt": attempt, "nonce": nonce, "client_cert_pem": client.certPEM, "signature": other.sign(t, attempt, nonce),
	})
	if status != http.StatusForbidden {
		t.Fatalf("forged signature status=%d", status)
	}
	// The real proof binds the generated host UUID.
	status, complete := postEnroll(t, server, key, daemonID, deviceID, "/desktop/moonlight/enroll/complete", map[string]any{
		"attempt": attempt, "nonce": nonce, "client_cert_pem": client.certPEM, "signature": client.sign(t, attempt, nonce),
	})
	if status != http.StatusOK || complete["uuid"] != "host-uuid-a" {
		t.Fatalf("complete status=%d body=%v", status, complete)
	}
	if admission, err := hostAdmission(deviceID); err != nil || admission != "verified" {
		t.Fatalf("admission=%q err=%v", admission, err)
	}
	// Replay of the consumed attempt is rejected.
	status, _ = postEnroll(t, server, key, daemonID, deviceID, "/desktop/moonlight/enroll/complete", map[string]any{
		"attempt": attempt, "nonce": nonce, "client_cert_pem": client.certPEM, "signature": client.sign(t, attempt, nonce),
	})
	if status != http.StatusConflict {
		t.Fatalf("replay status=%d", status)
	}
}

func TestMoonlightEnrollmentRejectsPendingAndCrossDeviceClaims(t *testing.T) {
	server, key, deviceID, daemonID := enrollFixture(t)
	clientA := newEnrollClient(t)
	// B's certificate is present in the owned state; A must not claim it.
	clientB := newEnrollClient(t)
	writeEnrollState(t, os.Getenv("ZEN_STATE_DIR"), map[string]any{
		"name": "b", "cert": clientB.certPEM, "uuid": "host-uuid-b", "enabled": true,
	})
	attempt := hex.EncodeToString(bytes.Repeat([]byte{0xcd}, 16))
	status, begin := postEnroll(t, server, key, daemonID, deviceID, "/desktop/moonlight/enroll/begin", map[string]any{"attempt": attempt})
	if status != http.StatusOK {
		t.Fatalf("begin status=%d", status)
	}
	nonce, _ := begin["nonce"].(string)
	status, complete := postEnroll(t, server, key, daemonID, deviceID, "/desktop/moonlight/enroll/complete", map[string]any{
		"attempt": attempt, "nonce": nonce, "client_cert_pem": clientA.certPEM, "signature": clientA.sign(t, attempt, nonce),
	})
	if status != http.StatusConflict {
		t.Fatalf("not-present certificate status=%d body=%v", status, complete)
	}
	if admission, _ := hostAdmission(deviceID); admission == "verified" {
		t.Fatal("unenrolled pending attempt produced a verified admission")
	}
}

func TestMoonlightEnrollmentRequiresScope(t *testing.T) {
	t.Setenv("ZEN_STATE_DIR", t.TempDir())
	manager, key, deviceID := sessionFileAuthFixture(t)
	s := New(manager, nil, nil, nil, nil, nil, nil)
	t.Cleanup(s.shutdownAuthenticatedClients)
	server := httptest.NewTLSServer(s.Handler())
	t.Cleanup(server.Close)
	attempt := hex.EncodeToString(bytes.Repeat([]byte{0xef}, 16))
	status, _ := postEnroll(t, server, key, manager.DaemonID(), deviceID, "/desktop/moonlight/enroll/begin", map[string]any{"attempt": attempt})
	if status != http.StatusForbidden {
		t.Fatalf("unscoped enrollment status=%d", status)
	}
}

func TestMoonlightEnrollmentRejectsRealBOwnedCertificateClaim(t *testing.T) {
	server, keyA, deviceA, daemonID, keyB, deviceB := enrollFixtureAB(t)
	clientB := newEnrollClient(t)
	// B's certificate is genuinely present in the owned state under UUID-B.
	writeEnrollState(t, os.Getenv("ZEN_STATE_DIR"), map[string]any{
		"name": "b", "cert": clientB.certPEM, "uuid": "host-uuid-b", "enabled": true,
	})
	// Enroll B first through the real handshake.
	attemptB := hex.EncodeToString(bytes.Repeat([]byte{0x11}, 16))
	status, begin := postEnroll(t, server, keyB, daemonID, deviceB, "/desktop/moonlight/enroll/begin", map[string]any{"attempt": attemptB})
	if status != http.StatusOK {
		t.Fatalf("begin b status=%d", status)
	}
	nonceB, _ := begin["nonce"].(string)
	status, complete := postEnroll(t, server, keyB, daemonID, deviceB, "/desktop/moonlight/enroll/complete", map[string]any{
		"attempt": attemptB, "nonce": nonceB, "client_cert_pem": clientB.certPEM, "signature": clientB.sign(t, attemptB, nonceB),
	})
	if status != http.StatusOK || complete["uuid"] != "host-uuid-b" {
		t.Fatalf("enroll b status=%d body=%v", status, complete)
	}
	// Device A tries to claim B's already-owned certificate.
	attemptA := hex.EncodeToString(bytes.Repeat([]byte{0x22}, 16))
	status, begin = postEnroll(t, server, keyA, daemonID, deviceA, "/desktop/moonlight/enroll/begin", map[string]any{"attempt": attemptA})
	if status != http.StatusOK {
		t.Fatalf("begin a status=%d", status)
	}
	nonceA, _ := begin["nonce"].(string)
	status, _ = postEnroll(t, server, keyA, daemonID, deviceA, "/desktop/moonlight/enroll/complete", map[string]any{
		"attempt": attemptA, "nonce": nonceA, "client_cert_pem": clientB.certPEM, "signature": clientB.sign(t, attemptA, nonceA),
	})
	if status != http.StatusConflict {
		t.Fatalf("cross-device claim status=%d", status)
	}
}

func TestMoonlightEnrollmentPendingIsDurableAndRevocable(t *testing.T) {
	server, key, deviceID, daemonID := enrollFixture(t)
	client := newEnrollClient(t)
	writeEnrollState(t, os.Getenv("ZEN_STATE_DIR")) // empty host state
	attempt := hex.EncodeToString(bytes.Repeat([]byte{0x33}, 16))
	status, begin := postEnroll(t, server, key, daemonID, deviceID, "/desktop/moonlight/enroll/begin", map[string]any{"attempt": attempt})
	if status != http.StatusOK {
		t.Fatalf("begin status=%d", status)
	}
	nonce, _ := begin["nonce"].(string)
	status, _ = postEnroll(t, server, key, daemonID, deviceID, "/desktop/moonlight/enroll/complete", map[string]any{
		"attempt": attempt, "nonce": nonce, "client_cert_pem": client.certPEM, "signature": client.sign(t, attempt, nonce),
	})
	if status != http.StatusConflict {
		t.Fatalf("pending status=%d", status)
	}
	// The verified intent is durable and attributable to the device.
	uuid, ok, err := host.SunshineEnrollment(deviceID)
	if err != nil || !ok || uuid != "" {
		t.Fatalf("pending enrollment = %q %v %v", uuid, ok, err)
	}
	if err := host.CancelSunshineEnrollment(deviceID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := host.SunshineEnrollment(deviceID); ok {
		t.Fatal("revoked pending intent survived")
	}
}

func TestMoonlightEnrollmentStorageFailureDoesNotDeadlock(t *testing.T) {
	server, key, deviceID, daemonID := enrollFixture(t)
	client := newEnrollClient(t)
	writeEnrollState(t, os.Getenv("ZEN_STATE_DIR"))
	if err := os.WriteFile(filepath.Join(os.Getenv("ZEN_STATE_DIR"), "sunshine_owners.json"), []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	attempt := hex.EncodeToString(bytes.Repeat([]byte{0x44}, 16))
	status, begin := postEnroll(t, server, key, daemonID, deviceID, "/desktop/moonlight/enroll/begin", map[string]any{"attempt": attempt})
	if status != http.StatusOK {
		t.Fatalf("begin status=%d", status)
	}
	nonce, _ := begin["nonce"].(string)
	status, _ = postEnroll(t, server, key, daemonID, deviceID, "/desktop/moonlight/enroll/complete", map[string]any{
		"attempt": attempt, "nonce": nonce, "client_cert_pem": client.certPEM, "signature": client.sign(t, attempt, nonce),
	})
	if status != http.StatusServiceUnavailable {
		t.Fatalf("storage failure status=%d", status)
	}
	// A later begin must still answer: no lock leak.
	attempt2 := hex.EncodeToString(bytes.Repeat([]byte{0x55}, 16))
	status, _ = postEnroll(t, server, key, daemonID, deviceID, "/desktop/moonlight/enroll/begin", map[string]any{"attempt": attempt2})
	if status != http.StatusOK {
		t.Fatalf("post-failure begin status=%d", status)
	}
}

func enrollFixturePlainHarness(t *testing.T) (*httptest.Server, ed25519.PrivateKey, string, string) {
	t.Helper()
	t.Setenv("ZEN_STATE_DIR", t.TempDir())
	manager, key, deviceID := sessionFileAuthFixture(t)
	publicHex := hex.EncodeToString(key.Public().(ed25519.PublicKey))
	if _, err := manager.GrantDesktopScope(deviceID, publicHex, auth.DesktopScopeVersion); err != nil {
		t.Fatal(err)
	}
	s := New(manager, nil, nil, nil, nil, nil, nil)
	t.Cleanup(s.shutdownAuthenticatedClients)
	server := httptest.NewServer(s.Handler())
	t.Cleanup(server.Close)
	return server, key, deviceID, manager.DaemonID()
}

func TestMoonlightEnrollmentRefusesPlaintextControlWithValidScope(t *testing.T) {
	plain, key, deviceID, daemonID := enrollFixturePlainHarness(t)
	client := newEnrollClient(t)
	writeEnrollState(t, os.Getenv("ZEN_STATE_DIR"), map[string]any{
		"name": "phone", "cert": client.certPEM, "uuid": "host-uuid-a", "enabled": true,
	})
	attempt := hex.EncodeToString(bytes.Repeat([]byte{0x66}, 16))
	// HTTP begin with a fully valid scoped device/auth nonce is still refused.
	status, _ := postEnroll(t, plain, key, daemonID, deviceID, "/desktop/moonlight/enroll/begin", map[string]any{"attempt": attempt})
	if status != http.StatusForbidden {
		t.Fatalf("plaintext begin status=%d", status)
	}
	// HTTP complete with a valid scoped device/auth nonce is refused too.
	status, _ = postEnroll(t, plain, key, daemonID, deviceID, "/desktop/moonlight/enroll/complete", map[string]any{
		"attempt": attempt, "nonce": "00", "client_cert_pem": client.certPEM, "signature": "00",
	})
	if status != http.StatusForbidden {
		t.Fatalf("plaintext complete status=%d", status)
	}
}

func TestMoonlightEnrollmentTLSBeginThenHTTPCompleteIsRefused(t *testing.T) {
	t.Setenv("ZEN_STATE_DIR", t.TempDir())
	manager, key, deviceID := sessionFileAuthFixture(t)
	publicHex := hex.EncodeToString(key.Public().(ed25519.PublicKey))
	if _, err := manager.GrantDesktopScope(deviceID, publicHex, auth.DesktopScopeVersion); err != nil {
		t.Fatal(err)
	}
	s := New(manager, nil, nil, nil, nil, nil, nil)
	t.Cleanup(s.shutdownAuthenticatedClients)
	tlsServer := httptest.NewTLSServer(s.Handler())
	t.Cleanup(tlsServer.Close)
	plainServer := httptest.NewServer(s.Handler())
	t.Cleanup(plainServer.Close)

	client := newEnrollClient(t)
	writeEnrollState(t, os.Getenv("ZEN_STATE_DIR"), map[string]any{
		"name": "phone", "cert": client.certPEM, "uuid": "host-uuid-a", "enabled": true,
	})
	attempt := hex.EncodeToString(bytes.Repeat([]byte{0x77}, 16))
	status, begin := postEnroll(t, tlsServer, key, manager.DaemonID(), deviceID, "/desktop/moonlight/enroll/begin", map[string]any{"attempt": attempt})
	if status != http.StatusOK {
		t.Fatalf("tls begin status=%d", status)
	}
	nonce, _ := begin["nonce"].(string)
	// The same valid challenge/proof sent over HTTP is refused.
	status, _ = postEnroll(t, plainServer, key, manager.DaemonID(), deviceID, "/desktop/moonlight/enroll/complete", map[string]any{
		"attempt": attempt, "nonce": nonce, "client_cert_pem": client.certPEM, "signature": client.sign(t, attempt, nonce),
	})
	if status != http.StatusForbidden {
		t.Fatalf("http complete after tls begin status=%d", status)
	}
	// The challenge survived and the real TLS completion still succeeds.
	status, complete := postEnroll(t, tlsServer, key, manager.DaemonID(), deviceID, "/desktop/moonlight/enroll/complete", map[string]any{
		"attempt": attempt, "nonce": nonce, "client_cert_pem": client.certPEM, "signature": client.sign(t, attempt, nonce),
	})
	if status != http.StatusOK || complete["enrolled"] != true {
		t.Fatalf("tls complete status=%d body=%v", status, complete)
	}
}

func TestMoonlightEnrollmentAllowsTrustedDeploymentHTTP(t *testing.T) {
	t.Setenv("ZEN_STATE_DIR", t.TempDir())
	manager, key, deviceID := sessionFileAuthFixture(t)
	publicHex := hex.EncodeToString(key.Public().(ed25519.PublicKey))
	if _, err := manager.GrantDesktopScope(deviceID, publicHex, auth.DesktopScopeVersion); err != nil {
		t.Fatal(err)
	}
	s := New(manager, nil, nil, nil, nil, nil, nil)
	t.Cleanup(s.shutdownAuthenticatedClients)
	// Operator explicitly trusts this deployment; the loopback peer is its
	// local connector. No r.TLS is fabricated.
	s.SetDesktopTrustedNetwork(true)
	server := httptest.NewServer(s.Handler())
	t.Cleanup(server.Close)

	client := newEnrollClient(t)
	writeEnrollState(t, os.Getenv("ZEN_STATE_DIR"), map[string]any{
		"name": "phone", "cert": client.certPEM, "uuid": "host-uuid-trusted", "enabled": true,
	})
	attempt := hex.EncodeToString(bytes.Repeat([]byte{0x88}, 16))
	status, begin := postEnroll(t, server, key, manager.DaemonID(), deviceID, "/desktop/moonlight/enroll/begin", map[string]any{"attempt": attempt})
	if status != http.StatusOK {
		t.Fatalf("trusted begin status=%d", status)
	}
	nonce, _ := begin["nonce"].(string)
	status, complete := postEnroll(t, server, key, manager.DaemonID(), deviceID, "/desktop/moonlight/enroll/complete", map[string]any{
		"attempt": attempt, "nonce": nonce, "client_cert_pem": client.certPEM, "signature": client.sign(t, attempt, nonce),
	})
	if status != http.StatusOK || complete["uuid"] != "host-uuid-trusted" {
		t.Fatalf("trusted complete status=%d body=%v", status, complete)
	}
}
