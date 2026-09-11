package host

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/desktop"
)

func TestCanonicalPairingMigrationAndRevocationRetireAgent(t *testing.T) {
	m, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyHex := hex.EncodeToString(pub)
	token, err := m.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnrollDevice(token.Value, m.DaemonID(), m.PublicKeyHex(), "phone", "Phone", keyHex); err != nil {
		t.Fatal(err)
	}
	cfg := HostConfig{Version: 1, HostID: m.DaemonID(), OwnerUID: 1000, Seat: "seat0"}
	g, stop, err := NewManagedGate(cfg, m)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	_, r, s := fixture(t)
	r.HostID = m.DaemonID()
	r.Fingerprint = sha256.Sum256(pub)
	r.Generation = g.Observe(s)
	if _, err := g.Open(r, nil, &testAgent{}, time.Now()); err == nil {
		t.Fatal("legacy pairing became unattended")
	}
	token, err = m.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	proof := ed25519.Sign(key, auth.BuildPairingScopePayload(m.PublicKeyHex(), token.Value, "phone", keyHex))
	if _, err := m.EnrollDeviceWithDesktopScope(token.Value, m.DaemonID(), m.PublicKeyHex(), "phone", "Phone", keyHex, 1, hex.EncodeToString(proof)); err != nil {
		t.Fatal(err)
	}
	a := &testAgent{}
	lease, err := g.Open(r, nil, a, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.RevokeDevice("phone"); err != nil {
		t.Fatal(err)
	}
	if !a.closed || g.Input(lease, desktop.Command{Type: "release"}) == nil {
		t.Fatal("canonical revocation did not retire agent synchronously")
	}
	if _, err := g.Open(r, nil, &testAgent{}, time.Now()); err == nil {
		t.Fatal("reconnect after canonical revocation")
	}
	cfg.HostID = "another-host"
	if _, _, err := NewManagedGate(cfg, m); err == nil {
		t.Fatal("gate bound to wrong owner")
	}
}
