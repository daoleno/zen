package link

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
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
