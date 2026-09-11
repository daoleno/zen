package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func scopeFixture(t *testing.T) (*Manager, ed25519.PrivateKey, string) {
	t.Helper()
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return m, priv, hex.EncodeToString(key)
}

func scopePair(t *testing.T, m *Manager, key ed25519.PrivateKey, pub string) (*TrustedDevice, error) {
	t.Helper()
	token, err := m.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(key, BuildPairingScopePayload(m.PublicKeyHex(), token.Value, "phone", pub))
	return m.EnrollDeviceWithDesktopScope(token.Value, m.DaemonID(), m.PublicKeyHex(), "phone", "Phone", pub, 1, hex.EncodeToString(sig))
}

func TestDesktopScopeNewPairingMigrationAndRevocation(t *testing.T) {
	m, key, pub := scopeFixture(t)
	token, err := m.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := m.EnrollDevice(token.Value, m.DaemonID(), m.PublicKeyHex(), "phone", "Phone", pub)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.DesktopScopeVersion != 0 || m.HasDesktopScope("phone", pub) {
		t.Fatal("old pairing gained scope")
	}
	reloaded, err := NewManager(m.StorageDir())
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.HasDesktopScope("phone", pub) {
		t.Fatal("load backfilled scope")
	}
	if _, err := m.VerifyAuthorization(buildTestAuthorizationHeader(t, key, m.DaemonID(), "phone", "zen-probe"), "zen-probe", time.Minute); err != nil {
		t.Fatal(err)
	}
	if m.HasDesktopScope("phone", pub) {
		t.Fatal("ordinary auth migrated scope")
	}
	updated, err := scopePair(t, m, key, pub)
	if err != nil {
		t.Fatal(err)
	}
	if updated.DesktopScopeVersion != 1 || !updated.AddedAt.Equal(legacy.AddedAt) || !m.HasDesktopScope("phone", pub) || len(m.ListDevices()) != 1 {
		t.Fatal("migration did not preserve same identity")
	}
	reloaded, err = NewManager(m.StorageDir())
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.HasDesktopScope("phone", pub) || reloaded.ListDevices()[0].DesktopScopeVersion != 1 {
		t.Fatal("scope was not persisted")
	}
	if m.HasDesktopScope("other", pub) || m.HasDesktopScope("phone", strings.Repeat("0", 64)) {
		t.Fatal("scope crossed device/key binding")
	}
	observed := false
	unsubscribe := m.SubscribeRevocations(func(id string) { observed = id == "phone" && !m.HasDesktopScope(id, pub) })
	defer unsubscribe()
	if _, err := m.RevokeDevice("phone"); err != nil {
		t.Fatal(err)
	}
	if !observed || m.HasDesktopScope("phone", pub) {
		t.Fatal("revocation owner retained scope")
	}
	device, err := scopePair(t, m, key, pub)
	if err != nil || device.DesktopScopeVersion != 1 {
		t.Fatalf("new pairing default scope: %v", err)
	}
}

func TestDesktopScopeProofCannotBeAddedReplayedOrRetargeted(t *testing.T) {
	for _, mutation := range []string{"missing", "version", "device", "token", "host", "key"} {
		t.Run(mutation, func(t *testing.T) {
			m, key, pub := scopeFixture(t)
			token, err := m.IssuePairingToken(time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			proofToken, proofHost, proofID, proofKey := token.Value, m.PublicKeyHex(), "phone", pub
			scope := 1
			switch mutation {
			case "version":
				scope = 2
			case "device":
				proofID = "other"
			case "token":
				proofToken = strings.Repeat("a", 64)
			case "host":
				proofHost = strings.Repeat("b", 64)
			case "key":
				proofKey = strings.Repeat("c", 64)
			}
			sig := hex.EncodeToString(ed25519.Sign(key, BuildPairingScopePayload(proofHost, proofToken, proofID, proofKey)))
			if mutation == "missing" {
				sig = ""
			}
			if _, err := m.EnrollDeviceWithDesktopScope(token.Value, m.DaemonID(), m.PublicKeyHex(), "phone", "Phone", pub, scope, sig); err == nil {
				t.Fatal("invalid scope proof accepted")
			}
			if m.HasDesktopScope("phone", pub) || m.IsDeviceTrusted("phone") {
				t.Fatal("failed proof granted trust")
			}
			valid := hex.EncodeToString(ed25519.Sign(key, BuildPairingScopePayload(m.PublicKeyHex(), token.Value, "phone", pub)))
			if _, err := m.EnrollDeviceWithDesktopScope(token.Value, m.DaemonID(), m.PublicKeyHex(), "phone", "Phone", pub, 1, valid); err != nil {
				t.Fatal(err)
			}
			if _, err := m.EnrollDeviceWithDesktopScope(token.Value, m.DaemonID(), m.PublicKeyHex(), "phone", "Phone", pub, 1, valid); !errors.Is(err, ErrInvalidPairingToken) {
				t.Fatalf("token replay: %v", err)
			}
		})
	}
}

func TestDesktopScopePersistenceFailureDoesNotGrantMemoryPrivileges(t *testing.T) {
	m, key, pub := scopeFixture(t)
	token, err := m.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnrollDevice(token.Value, m.DaemonID(), m.PublicKeyHex(), "phone", "Phone", pub); err != nil {
		t.Fatal(err)
	}
	writer := m.writeFile
	m.writeFile = func(path string, data []byte, mode os.FileMode) (PersistenceResult, error) {
		if path == m.devicesPath {
			return PersistenceResult{}, errors.New("synthetic persistence failure")
		}
		return writer(path, data, mode)
	}
	if _, err := scopePair(t, m, key, pub); err == nil {
		t.Fatal("persistence failure ignored")
	}
	if m.HasDesktopScope("phone", pub) || m.ListDevices()[0].DesktopScopeVersion != 0 {
		t.Fatal("uncommitted privilege visible")
	}
	reloaded, err := NewManager(m.StorageDir())
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.HasDesktopScope("phone", pub) {
		t.Fatal("uncommitted privilege on disk")
	}
}
