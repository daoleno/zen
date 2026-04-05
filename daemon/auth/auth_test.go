package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
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

func TestPairingTokensPersistAcrossManagers(t *testing.T) {
	storageDir := t.TempDir()

	runningManager, err := NewManager(storageDir)
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	pairManager, err := NewManager(storageDir)
	if err != nil {
		t.Fatalf("second NewManager returned error: %v", err)
	}

	pairing, err := pairManager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatalf("IssuePairingToken returned error: %v", err)
	}

	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}

	device, err := runningManager.EnrollDevice(
		pairing.Value,
		runningManager.DaemonID(),
		runningManager.PublicKeyHex(),
		"device-2",
		"tablet",
		hex.EncodeToString(publicKey),
	)
	if err != nil {
		t.Fatalf("EnrollDevice returned error: %v", err)
	}
	if device.ID != "device-2" {
		t.Fatalf("unexpected device id: %s", device.ID)
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
