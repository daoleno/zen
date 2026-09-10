package link

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTransportIdentityPersistsRouteAndPinAcrossCertificateReissue(t *testing.T) {
	stateDir := t.TempDir()

	first, err := LoadOrCreateTransportIdentity(stateDir, []string{"one.link.test"})
	if err != nil {
		t.Fatalf("LoadOrCreateTransportIdentity(first): %v", err)
	}
	second, err := LoadOrCreateTransportIdentity(stateDir, []string{"two.link.test"})
	if err != nil {
		t.Fatalf("LoadOrCreateTransportIdentity(second): %v", err)
	}

	if first.RouteID == "" || first.RouteID != second.RouteID {
		t.Fatalf("route id did not persist: first=%q second=%q", first.RouteID, second.RouteID)
	}
	if first.SPKISHA256 == "" || first.SPKISHA256 != second.SPKISHA256 {
		t.Fatalf("SPKI pin did not persist: first=%q second=%q", first.SPKISHA256, second.SPKISHA256)
	}
	if len(first.Certificate.Certificate) == 0 || len(second.Certificate.Certificate) == 0 {
		t.Fatal("transport certificate was not issued")
	}

	info, err := os.Stat(filepath.Join(stateDir, transportIdentityFilename))
	if err != nil {
		t.Fatalf("stat transport identity: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("transport identity mode=%o, want 600", got)
	}

	config := first.ServerTLSConfig()
	if config.MinVersion != tls.VersionTLS13 {
		t.Fatalf("minimum TLS version=%x, want TLS 1.3", config.MinVersion)
	}
	foundIdentityName := false
	for _, name := range first.Certificate.Leaf.DNSNames {
		if name == DesktopIdentityServerName {
			foundIdentityName = true
		}
	}
	if !foundIdentityName {
		t.Fatal("identity desktop server name missing from transport certificate")
	}
}

// Android's TLS stack offers ECDSA/RSA signature schemes but not Ed25519, so a
// self-signed Ed25519 transport certificate fails the pinned handshake with
// "peer doesn't support any of the certificate's signature algorithms".
func TestTransportCertificateUsesClientNegotiableECDSAKey(t *testing.T) {
	stateDir := t.TempDir()
	identity, err := LoadOrCreateTransportIdentity(stateDir, []string{"relay.link.test"})
	if err != nil {
		t.Fatalf("LoadOrCreateTransportIdentity: %v", err)
	}
	publicKey, ok := identity.Certificate.Leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("transport certificate key is %T, want ECDSA P-256", identity.Certificate.Leaf.PublicKey)
	}
	if publicKey.Curve != elliptic.P256() {
		t.Fatalf("transport certificate curve is %q, want P-256", publicKey.Curve.Params().Name)
	}
	if identity.Certificate.Leaf.SignatureAlgorithm != x509.ECDSAWithSHA256 {
		t.Fatalf("transport certificate signature=%v, want ECDSAWithSHA256", identity.Certificate.Leaf.SignatureAlgorithm)
	}
}

func TestLoadTransportIdentityRejectsCorruptStateWithoutMutation(t *testing.T) {
	route := strings.Repeat("ab", 16)
	identityKey := hex.EncodeToString([]byte(strings.Repeat("x", ed25519.PrivateKeySize)))
	cases := map[string]string{
		"bad_route":            `{"route_id":"zz","private_key_hex":"` + identityKey + `"}`,
		"short_identity_key":   `{"route_id":"` + route + `","private_key_hex":"00"}`,
		"missing_identity_key": `{"route_id":"` + route + `"}`,
		"bad_tls_key_hex":      `{"route_id":"` + route + `","private_key_hex":"` + identityKey + `","tls_private_key_hex":"zz"}`,
	}
	for name, contents := range cases {
		dir := t.TempDir()
		path := filepath.Join(dir, transportIdentityFilename)
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadOrCreateTransportIdentity(dir, nil); err == nil {
			t.Fatalf("%s: corrupt identity was accepted", name)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != contents {
			t.Fatalf("%s: corrupt identity was mutated", name)
		}
	}
}

func TestLoadTransportIdentityMigratesValidLegacyStateOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, transportIdentityFilename)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	legacy := `{"route_id":"` + strings.Repeat("cd", 16) + `","private_key_hex":"` + hex.EncodeToString(key) + `"}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := LoadOrCreateTransportIdentity(dir, nil)
	if err != nil {
		t.Fatalf("migrate legacy identity: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "tls_private_key_hex") {
		t.Fatal("valid legacy identity did not persist a TLS key")
	}
	second, err := LoadOrCreateTransportIdentity(dir, nil)
	if err != nil {
		t.Fatalf("reload migrated identity: %v", err)
	}
	if first.SPKISHA256 == "" || first.SPKISHA256 != second.SPKISHA256 {
		t.Fatalf("pin did not stay stable after migration: %q vs %q", first.SPKISHA256, second.SPKISHA256)
	}
}

func TestPinnedClientTLSConfigRejectsWrongPin(t *testing.T) {
	stateDir := t.TempDir()
	identity, err := LoadOrCreateTransportIdentity(stateDir, []string{"relay.link.test"})
	if err != nil {
		t.Fatalf("LoadOrCreateTransportIdentity: %v", err)
	}

	good, err := PinnedClientTLSConfig("route.relay.link.test", identity.SPKISHA256)
	if err != nil {
		t.Fatalf("PinnedClientTLSConfig(good): %v", err)
	}
	if err := good.VerifyConnection(tls.ConnectionState{
		PeerCertificates: identity.LeafCertificates(),
	}); err != nil {
		t.Fatalf("correct pin rejected: %v", err)
	}

	wrong, err := PinnedClientTLSConfig(
		"route.relay.link.test",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	)
	if err != nil {
		t.Fatalf("PinnedClientTLSConfig(wrong): %v", err)
	}
	if err := wrong.VerifyConnection(tls.ConnectionState{
		PeerCertificates: identity.LeafCertificates(),
	}); err == nil {
		t.Fatal("wrong pin was accepted")
	}
}
