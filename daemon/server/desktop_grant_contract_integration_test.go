package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/link"
)

// TestDesktopGrantCrossLanguageSignedRoundtrip is the real cross-language
// contract proof: the app's TypeScript payload builder signs a device
// authorization with an isolated key, the real Go handler authenticates that
// exact purpose and grants scope on an isolated manager, and the app's
// TypeScript verifier accepts the real Go signed confirmation. It uses only
// temp state, temp identity and a test device; no user daemon, phone, broker or
// pairing token is involved.
func TestDesktopGrantCrossLanguageSignedRoundtrip(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("bun is required to prove the TypeScript signing contract against the real Go handler")
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	signer := filepath.Join(repoRoot, "app", "tests", "fixtures", "desktop-grant-contract-signer.ts")
	if _, err := os.Stat(signer); err != nil {
		t.Fatal(err)
	}

	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKeyHex := hex.EncodeToString(publicKey)
	deviceID := "cross-language-contract-device"
	token, err := manager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.EnrollDevice(token.Value, manager.DaemonID(), manager.PublicKeyHex(), deviceID, "Cross language contract", publicKeyHex); err != nil {
		t.Fatal(err)
	}

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
	srv := &http.Server{Handler: s.Handler()}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(listener) }()
	t.Cleanup(func() {
		_ = srv.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
	})

	inputPath := filepath.Join(t.TempDir(), "grant-input.json")
	inputBody, err := json.Marshal(map[string]string{
		"url":             "https://" + listener.Addr().String(),
		"daemonId":        manager.DaemonID(),
		"daemonPublicKey": manager.PublicKeyHex(),
		"deviceId":        deviceID,
		"seedHex":         hex.EncodeToString(privateKey.Seed()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inputPath, inputBody, 0600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bun, "run", signer, inputPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "NODE_TLS_REJECT_UNAUTHORIZED=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bun signer failed against the real Go handler: %v\n%s", err, output)
	}
	var result map[string]any
	if json.Unmarshal(output, &result) != nil || result["ok"] != true || result["deviceId"] != deviceID ||
		result["scopeVersion"] != float64(auth.DesktopScopeVersion) || result["purpose"] != auth.DesktopGrantPurpose {
		t.Fatalf("signer output=%s", output)
	}
	if !manager.HasDesktopScope(deviceID, publicKeyHex) {
		t.Fatal("real Go handler did not grant the TS-signed request")
	}
	if len(manager.ListDevices()) != 1 || !manager.IsDeviceTrusted(deviceID) {
		t.Fatal("cross-language grant changed the canonical device record set")
	}
	var pairings struct {
		Pairings []json.RawMessage `json:"pairings"`
	}
	raw, err := os.ReadFile(filepath.Join(manager.StorageDir(), "pairing-tokens.json"))
	if err != nil || json.Unmarshal(raw, &pairings) != nil || len(pairings.Pairings) != 0 {
		t.Fatalf("cross-language grant left pairing tokens: %s %v", raw, err)
	}
}
