package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagerEnrollAndVerifyAuthorization(t *testing.T) {
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	pairing, err := manager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatalf("IssuePairingToken returned error: %v", err)
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}

	device, err := manager.EnrollDevice(
		pairing.Value,
		manager.DaemonID(),
		manager.PublicKeyHex(),
		"device-1",
		"phone",
		hex.EncodeToString(publicKey),
	)
	if err != nil {
		t.Fatalf("EnrollDevice returned error: %v", err)
	}
	if device.ID != "device-1" {
		t.Fatalf("unexpected device id: %s", device.ID)
	}

	header := buildTestAuthorizationHeader(t, privateKey, manager.DaemonID(), "device-1", "zen-probe")
	verifiedDevice, err := manager.VerifyAuthorization(header, "zen-probe", time.Minute)
	if err != nil {
		t.Fatalf("VerifyAuthorization returned error: %v", err)
	}
	if verifiedDevice.ID != "device-1" {
		t.Fatalf("unexpected verified device id: %s", verifiedDevice.ID)
	}
}

func TestManagerVerifyAuthorizationRejectsReplay(t *testing.T) {
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	pairing, err := manager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatalf("IssuePairingToken returned error: %v", err)
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}

	if _, err := manager.EnrollDevice(
		pairing.Value,
		manager.DaemonID(),
		manager.PublicKeyHex(),
		"device-1",
		"phone",
		hex.EncodeToString(publicKey),
	); err != nil {
		t.Fatalf("EnrollDevice returned error: %v", err)
	}

	header := buildTestAuthorizationHeader(t, privateKey, manager.DaemonID(), "device-1", "zen-probe")
	if _, err := manager.VerifyAuthorization(header, "zen-probe", time.Minute); err != nil {
		t.Fatalf("first VerifyAuthorization returned error: %v", err)
	}

	if _, err := manager.VerifyAuthorization(header, "zen-probe", time.Minute); !errors.Is(err, ErrReplayDetected) {
		t.Fatalf("expected ErrReplayDetected, got %v", err)
	}
}

func TestManagerRevokeDevicePersistsAndImmediatelyRejectsAuthorization(t *testing.T) {
	stateDir := t.TempDir()
	manager, err := NewManager(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := manager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.EnrollDevice(
		pairing.Value,
		manager.DaemonID(),
		manager.PublicKeyHex(),
		"device-revoke",
		"phone",
		hex.EncodeToString(publicKey),
	); err != nil {
		t.Fatal(err)
	}
	if len(manager.ListDevices()) != 1 {
		t.Fatal("paired device was not listed")
	}
	if _, err := manager.RevokeDevice("device-revoke"); err != nil {
		t.Fatalf("RevokeDevice: %v", err)
	}
	header := buildTestAuthorizationHeader(
		t,
		privateKey,
		manager.DaemonID(),
		"device-revoke",
		"zen-probe",
	)
	if _, err := manager.VerifyAuthorization(header, "zen-probe", time.Minute); !errors.Is(err, ErrUnknownDevice) {
		t.Fatalf("revoked device authorization error=%v, want ErrUnknownDevice", err)
	}
	reloaded, err := NewManager(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.ListDevices()) != 0 {
		t.Fatal("revoked device reappeared after manager reload")
	}
	if _, err := reloaded.RevokeDevice("device-revoke"); !errors.Is(err, ErrUnknownDevice) {
		t.Fatalf("second revoke error=%v, want ErrUnknownDevice", err)
	}
}

func TestManagerKeepsOwnedDeviceStateUntilRestart(t *testing.T) {
	stateDir := t.TempDir()
	manager, err := NewManager(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := manager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.EnrollDevice(
		pairing.Value,
		manager.DaemonID(),
		manager.PublicKeyHex(),
		"device-owned-state",
		"phone",
		hex.EncodeToString(publicKey),
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manager.devicesPath, []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !manager.IsDeviceTrusted("device-owned-state") ||
		len(manager.ListDevices()) != 1 {
		t.Fatal("live Manager reparsed externally corrupted device state")
	}
	if _, err := NewManager(stateDir); err == nil ||
		!strings.Contains(err.Error(), "decode trusted devices") {
		t.Fatalf("restart after corrupt device state error=%v", err)
	}
}

func TestManagerRevokePersistenceFailureKeepsTrustAndDoesNotPublish(
	t *testing.T,
) {
	stateDir := t.TempDir()
	manager, err := NewManager(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := manager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const deviceID = "device-persistence-failure"
	if _, err := manager.EnrollDevice(
		pairing.Value,
		manager.DaemonID(),
		manager.PublicKeyHex(),
		deviceID,
		"phone",
		hex.EncodeToString(publicKey),
	); err != nil {
		t.Fatal(err)
	}

	var published atomic.Int32
	unsubscribe := manager.SubscribeRevocations(func(string) {
		published.Add(1)
	})
	defer unsubscribe()
	originalPath := manager.devicesPath
	blocker := filepath.Join(stateDir, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager.devicesPath = filepath.Join(blocker, "trusted-devices.json")
	if persistence, err := manager.RevokeDevice(deviceID); err == nil ||
		persistence.Applied {
		t.Fatal("RevokeDevice unexpectedly survived persistence failure")
	}
	manager.devicesPath = originalPath

	if !manager.IsDeviceTrusted(deviceID) || len(manager.ListDevices()) != 1 {
		t.Fatal("failed revoke changed live trusted-device state")
	}
	if published.Load() != 0 {
		t.Fatalf("failed revoke published %d listener events", published.Load())
	}
	reloaded, err := NewManager(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.IsDeviceTrusted(deviceID) {
		t.Fatal("failed revoke changed persisted trusted-device state")
	}
}

func TestManagerRevokeCommitsAfterRenameWhenDirectorySyncFails(
	t *testing.T,
) {
	stateDir := t.TempDir()
	manager, err := NewManager(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := manager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const deviceID = "device-post-rename-failure"
	if _, err := manager.EnrollDevice(
		pairing.Value,
		manager.DaemonID(),
		manager.PublicKeyHex(),
		deviceID,
		"phone",
		hex.EncodeToString(publicKey),
	); err != nil {
		t.Fatal(err)
	}

	var published atomic.Int32
	unsubscribe := manager.SubscribeRevocations(func(string) {
		published.Add(1)
	})
	defer unsubscribe()
	manager.writeFile = func(
		path string,
		data []byte,
		perm os.FileMode,
	) (PersistenceResult, error) {
		syncCalls := 0
		return writeFileAtomicWithParentSync(
			path,
			data,
			perm,
			func(parent *os.File) error {
				syncCalls++
				if syncCalls == 2 {
					return errors.New("injected post-rename sync failure")
				}
				return parent.Sync()
			},
		)
	}

	persistence, err := manager.RevokeDevice(deviceID)
	if err == nil ||
		!persistence.Applied ||
		persistence.Durable ||
		!strings.Contains(err.Error(), "post-rename") {
		t.Fatalf("post-rename persistence=%#v error=%v", persistence, err)
	}
	if manager.IsDeviceTrusted(deviceID) {
		t.Fatal("post-rename revoke did not commit live state")
	}
	if published.Load() != 1 {
		t.Fatalf("post-rename revoke published %d events, want 1", published.Load())
	}
	reloaded, err := NewManager(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.IsDeviceTrusted(deviceID) {
		t.Fatal("post-rename revoke did not commit persisted state")
	}
}

func TestManagerRevokeReEnrollRetainsConsumedNonce(t *testing.T) {
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const deviceID = "device-reenroll"
	enroll := func() {
		t.Helper()
		pairing, issueErr := manager.IssuePairingToken(time.Minute)
		if issueErr != nil {
			t.Fatal(issueErr)
		}
		if _, enrollErr := manager.EnrollDevice(
			pairing.Value,
			manager.DaemonID(),
			manager.PublicKeyHex(),
			deviceID,
			"phone",
			hex.EncodeToString(publicKey),
		); enrollErr != nil {
			t.Fatal(enrollErr)
		}
	}
	enroll()
	header := buildTestAuthorizationHeader(
		t,
		privateKey,
		manager.DaemonID(),
		deviceID,
		"zen-probe",
	)
	if _, err := manager.VerifyAuthorization(header, "zen-probe", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RevokeDevice(deviceID); err != nil {
		t.Fatal(err)
	}
	enroll()
	if _, err := manager.VerifyAuthorization(header, "zen-probe", time.Minute); !errors.Is(err, ErrReplayDetected) {
		t.Fatalf("reused authorization error=%v, want ErrReplayDetected", err)
	}
}

func TestManagerEnrollDeviceRejectsExpiredPairingToken(t *testing.T) {
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	pairing, err := manager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatalf("IssuePairingToken returned error: %v", err)
	}

	manager.mu.Lock()
	manager.pairings[pairing.Value] = PairingToken{
		Value:     pairing.Value,
		ExpiresAt: time.Now().Add(-time.Minute),
	}
	if err := manager.savePairingsLocked(); err != nil {
		manager.mu.Unlock()
		t.Fatalf("savePairingsLocked returned error: %v", err)
	}
	manager.mu.Unlock()

	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}

	if _, err := manager.EnrollDevice(
		pairing.Value,
		manager.DaemonID(),
		manager.PublicKeyHex(),
		"device-1",
		"phone",
		hex.EncodeToString(publicKey),
	); !errors.Is(err, ErrExpiredPairingToken) {
		t.Fatalf("expected ErrExpiredPairingToken, got %v", err)
	}
}

func TestManagerCreateServerAssertion(t *testing.T) {
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	assertion, err := manager.CreateServerAssertion("zen-health")
	if err != nil {
		t.Fatalf("CreateServerAssertion returned error: %v", err)
	}

	signature, err := decodeFixedHex(assertion.SignatureHex, ed25519.SignatureSize)
	if err != nil {
		t.Fatalf("decodeFixedHex returned error: %v", err)
	}

	payload := BuildServerAssertionPayload("zen-health", manager.DaemonID(), assertion.Timestamp, assertion.NonceHex)
	if !ed25519.Verify(manager.publicKey, payload, signature) {
		t.Fatal("server assertion signature did not verify")
	}
}

func buildTestAuthorizationHeader(t *testing.T, privateKey ed25519.PrivateKey, daemonID, deviceID, purpose string) string {
	t.Helper()

	nonceHex, err := randomHex(16)
	if err != nil {
		t.Fatalf("randomHex returned error: %v", err)
	}
	timestamp := strconv.FormatInt(time.Now().UTC().UnixMilli(), 10)
	signature := ed25519.Sign(privateKey, BuildSignaturePayload(purpose, daemonID, deviceID, timestamp, nonceHex))
	return AuthorizationHeaderPrefix + "v1:" + deviceID + ":" + daemonID + ":" + timestamp + ":" + nonceHex + ":" + hex.EncodeToString(signature)
}
