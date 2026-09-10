package link

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	transportIdentityFilename = "link-identity.json"
	// DesktopIdentityServerName is a non-resolving TLS server name so LAN IP
	// clients can present SNI without a user-owned domain. Authentication is
	// the SPKI pin, not this name or a public CA.
	DesktopIdentityServerName = "zen-desktop.invalid"
)

type persistedTransportIdentity struct {
	RouteID       string `json:"route_id"`
	PrivateKeyHex string `json:"private_key_hex"`
	// TLSPrivateKeyHex is the SEC1 DER-encoded ECDSA P-256 key used for the
	// self-signed TLS certificate. Android's TLS stack does not offer Ed25519,
	// so the certificate must use a curve every supported client negotiates.
	// The Ed25519 key remains for compatibility but no longer signs TLS.
	TLSPrivateKeyHex string `json:"tls_private_key_hex,omitempty"`
}

// TransportIdentity is a daemon-local TLS identity used only by Zen Link.
// Its SPKI pin remains stable while self-signed certificates are reissued for
// changed relay domains.
type TransportIdentity struct {
	RouteID     string
	SPKISHA256  string
	Certificate tls.Certificate
}

func LoadOrCreateTransportIdentity(stateDir string, relayDomains []string) (*TransportIdentity, error) {
	dir := strings.TrimSpace(stateDir)
	if dir == "" {
		return nil, errors.New("state directory is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}

	path := filepath.Join(dir, transportIdentityFilename)
	persisted, err := loadTransportIdentity(path)
	if errors.Is(err, os.ErrNotExist) {
		persisted, err = createTransportIdentity(path)
	}
	if err != nil {
		return nil, err
	}

	tlsKey, migrated, err := persistedTransportTLSKey(&persisted)
	if err != nil {
		return nil, err
	}
	if migrated {
		if err := writeTransportIdentity(path, persisted); err != nil {
			return nil, err
		}
	}

	certificate, leaf, err := issueTransportCertificate(
		persisted.RouteID,
		tlsKey,
		relayDomains,
		time.Now(),
	)
	if err != nil {
		return nil, err
	}
	certificate.Leaf = leaf

	spki := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
	return &TransportIdentity{
		RouteID:     persisted.RouteID,
		SPKISHA256:  hex.EncodeToString(spki[:]),
		Certificate: certificate,
	}, nil
}

func loadTransportIdentity(path string) (persistedTransportIdentity, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return persistedTransportIdentity{}, err
	}
	var persisted persistedTransportIdentity
	if err := json.Unmarshal(raw, &persisted); err != nil {
		return persistedTransportIdentity{}, fmt.Errorf("decode Link transport identity: %w", err)
	}
	persisted.RouteID = normalizeRouteID(persisted.RouteID)
	if persisted.RouteID == "" {
		return persistedTransportIdentity{}, errors.New("Link transport identity has an invalid route id")
	}
	identityKey, err := hex.DecodeString(strings.TrimSpace(persisted.PrivateKeyHex))
	if err != nil || len(identityKey) != ed25519.PrivateKeySize {
		return persistedTransportIdentity{}, errors.New("Link transport identity has an invalid identity key")
	}
	if rawTLS := strings.TrimSpace(persisted.TLSPrivateKeyHex); rawTLS != "" {
		der, err := hex.DecodeString(rawTLS)
		if err != nil {
			return persistedTransportIdentity{}, errors.New("Link transport identity has an invalid TLS key")
		}
		key, err := x509.ParseECPrivateKey(der)
		if err != nil || key.Curve != elliptic.P256() {
			return persistedTransportIdentity{}, errors.New("Link transport identity has an invalid TLS key")
		}
	}
	return persisted, nil
}

func createTransportIdentity(path string) (persistedTransportIdentity, error) {
	routeBytes := make([]byte, 16)
	if _, err := rand.Read(routeBytes); err != nil {
		return persistedTransportIdentity{}, fmt.Errorf("generate Link route id: %w", err)
	}
	_, identityKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return persistedTransportIdentity{}, fmt.Errorf("generate Link transport key: %w", err)
	}
	tlsKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return persistedTransportIdentity{}, fmt.Errorf("generate Link TLS key: %w", err)
	}
	tlsDER, err := x509.MarshalECPrivateKey(tlsKey)
	if err != nil {
		return persistedTransportIdentity{}, fmt.Errorf("encode Link TLS key: %w", err)
	}
	persisted := persistedTransportIdentity{
		RouteID:          hex.EncodeToString(routeBytes),
		PrivateKeyHex:    hex.EncodeToString(identityKey),
		TLSPrivateKeyHex: hex.EncodeToString(tlsDER),
	}
	if err := writeTransportIdentity(path, persisted); err != nil {
		return persistedTransportIdentity{}, err
	}
	return persisted, nil
}

// persistedTransportTLSKey returns the ECDSA P-256 key that signs the TLS
// certificate. Older identity files predate the field; a key is generated and
// the migration flag tells the caller to persist it.
func persistedTransportTLSKey(persisted *persistedTransportIdentity) (*ecdsa.PrivateKey, bool, error) {
	raw := strings.TrimSpace(persisted.TLSPrivateKeyHex)
	if raw == "" {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, false, fmt.Errorf("generate Link TLS key: %w", err)
		}
		der, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			return nil, false, fmt.Errorf("encode Link TLS key: %w", err)
		}
		persisted.TLSPrivateKeyHex = hex.EncodeToString(der)
		return key, true, nil
	}
	der, err := hex.DecodeString(raw)
	if err != nil {
		return nil, false, errors.New("Link transport identity has an invalid TLS key")
	}
	key, err := x509.ParseECPrivateKey(der)
	if err != nil {
		return nil, false, errors.New("Link transport identity has an invalid TLS key")
	}
	return key, false, nil
}

func writeTransportIdentity(path string, persisted persistedTransportIdentity) error {
	raw, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Link transport identity: %w", err)
	}
	return writePrivateFileAtomic(path, raw)
}

func issueTransportCertificate(
	routeID string,
	privateKey *ecdsa.PrivateKey,
	relayDomains []string,
	now time.Time,
) (tls.Certificate, *x509.Certificate, error) {
	serialBytes := make([]byte, 16)
	if _, err := rand.Read(serialBytes); err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("generate Link certificate serial: %w", err)
	}
	serial := new(big.Int).SetBytes(serialBytes)
	if serial.Sign() <= 0 {
		serial = big.NewInt(1)
	}

	dnsNames := transportDNSNames(relayDomains)
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: "zen-link-" + routeID,
		},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
	}
	publicKey := privateKey.Public()
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("issue Link transport certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("parse Link transport certificate: %w", err)
	}
	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  privateKey,
	}, leaf, nil
}

func transportDNSNames(relayDomains []string) []string {
	seen := map[string]struct{}{DesktopIdentityServerName: {}}
	for _, raw := range relayDomains {
		domain := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(raw, ".")))
		if domain == "" {
			continue
		}
		seen[domain] = struct{}{}
		seen["*."+domain] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (identity *TransportIdentity) ServerTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{identity.Certificate},
		NextProtos:   []string{"http/1.1"},
	}
}

func (identity *TransportIdentity) LeafCertificates() []*x509.Certificate {
	if identity == nil || identity.Certificate.Leaf == nil {
		return nil
	}
	return []*x509.Certificate{identity.Certificate.Leaf}
}

// PinnedClientTLSConfig authenticates the daemon Link transport by SPKI pin.
// The self-signed X.509 certificate is a TLS container; the daemon identity
// signature over this pin is verified by the Pairing V2 owner.
func PinnedClientTLSConfig(serverName, spkiSHA256 string) (*tls.Config, error) {
	pin, err := decodePin(spkiSHA256)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion:         tls.VersionTLS13,
		ServerName:         strings.TrimSpace(serverName),
		InsecureSkipVerify: true, // Replaced by the explicit SPKI verification below.
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return errors.New("Link peer did not provide a certificate")
			}
			got := sha256.Sum256(state.PeerCertificates[0].RawSubjectPublicKeyInfo)
			if !equalBytes(got[:], pin) {
				return errors.New("Link transport certificate pin mismatch")
			}
			return nil
		},
		NextProtos: []string{"http/1.1"},
	}, nil
}

func decodePin(value string) ([]byte, error) {
	decoded, err := hex.DecodeString(strings.ToLower(strings.TrimSpace(value)))
	if err != nil || len(decoded) != sha256.Size {
		return nil, errors.New("Link transport SPKI pin must be 32-byte lowercase hex")
	}
	return decoded, nil
}

func normalizeRouteID(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if len(normalized) != 32 {
		return ""
	}
	if _, err := hex.DecodeString(normalized); err != nil {
		return ""
	}
	return normalized
}

func writePrivateFileAtomic(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create Link state directory: %w", err)
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, raw, 0o600); err != nil {
		return fmt.Errorf("write Link transport identity: %w", err)
	}
	if err := os.Chmod(temp, 0o600); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("protect Link transport identity: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("install Link transport identity: %w", err)
	}
	return nil
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var different byte
	for index := range left {
		different |= left[index] ^ right[index]
	}
	return different == 0
}
