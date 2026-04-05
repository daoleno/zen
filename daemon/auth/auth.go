package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	AuthorizationHeaderPrefix = "ZenDevice "
	DefaultPairingTTL         = 15 * time.Minute
)

var (
	ErrUnauthorized        = errors.New("unauthorized")
	ErrUnknownDevice       = errors.New("unknown device")
	ErrReplayDetected      = errors.New("replayed auth nonce")
	ErrWrongDaemon         = errors.New("wrong daemon identity")
	ErrInvalidPairingToken = errors.New("invalid pairing token")
	ErrExpiredPairingToken = errors.New("expired pairing token")
)

type TrustedDevice struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	PublicKeyHex string    `json:"public_key_hex"`
	AddedAt      time.Time `json:"added_at"`
	LastSeenAt   time.Time `json:"last_seen_at"`
}

type PairingToken struct {
	Value     string
	ExpiresAt time.Time
}

type ServerAssertion struct {
	Timestamp    string
	NonceHex     string
	SignatureHex string
}

type Manager struct {
	mu           sync.Mutex
	storageDir   string
	daemonID     string
	publicKey    ed25519.PublicKey
	privateKey   ed25519.PrivateKey
	devices      map[string]*TrustedDevice
	pairings     map[string]PairingToken
	usedNonces   map[string]time.Time
	identityPath string
	devicesPath  string
	pairingsPath string
}

type persistedIdentity struct {
	PrivateKeyHex string `json:"private_key_hex"`
}

type persistedDevices struct {
	Devices []*TrustedDevice `json:"devices"`
}

type persistedPairings struct {
	Pairings []PairingToken `json:"pairings"`
}

func DefaultStorageDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config dir: %w", err)
	}
	return filepath.Join(base, "zen"), nil
}

func NewManager(storageDir string) (*Manager, error) {
	dir := strings.TrimSpace(storageDir)
	if dir == "" {
		var err error
		dir, err = DefaultStorageDir()
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create auth storage dir: %w", err)
	}

	m := &Manager{
		storageDir:   dir,
		devices:      make(map[string]*TrustedDevice),
		pairings:     make(map[string]PairingToken),
		usedNonces:   make(map[string]time.Time),
		identityPath: filepath.Join(dir, "identity.json"),
		devicesPath:  filepath.Join(dir, "trusted-devices.json"),
		pairingsPath: filepath.Join(dir, "pairing-tokens.json"),
	}

	if err := m.loadOrCreateIdentity(); err != nil {
		return nil, err
	}
	if err := m.loadDevices(); err != nil {
		return nil, err
	}
	if err := m.loadPairings(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) DaemonID() string {
	return m.daemonID
}

func (m *Manager) PublicKeyHex() string {
	return hex.EncodeToString(m.publicKey)
}

func (m *Manager) IssuePairingToken(ttl time.Duration) (PairingToken, error) {
	if ttl <= 0 {
		ttl = DefaultPairingTTL
	}
	token, err := randomHex(32)
	if err != nil {
		return PairingToken{}, err
	}

	pairing := PairingToken{
		Value:     token,
		ExpiresAt: time.Now().Add(ttl).UTC(),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.loadPairingsLocked(); err != nil {
		return PairingToken{}, err
	}
	m.pairings[token] = pairing
	m.pruneExpiredLocked()
	if err := m.savePairingsLocked(); err != nil {
		return PairingToken{}, err
	}
	return pairing, nil
}

func (m *Manager) EnrollDevice(token, expectedDaemonID, expectedPublicKeyHex, deviceID, deviceName, devicePublicKeyHex string) (*TrustedDevice, error) {
	token = strings.TrimSpace(token)
	deviceID = strings.TrimSpace(deviceID)
	deviceName = strings.TrimSpace(deviceName)
	devicePublicKeyHex = normalizeHex(devicePublicKeyHex)
	expectedDaemonID = normalizeHex(expectedDaemonID)
	expectedPublicKeyHex = normalizeHex(expectedPublicKeyHex)

	if token == "" || deviceID == "" || devicePublicKeyHex == "" {
		return nil, ErrInvalidPairingToken
	}
	if _, err := decodeFixedHex(devicePublicKeyHex, ed25519.PublicKeySize); err != nil {
		return nil, fmt.Errorf("invalid device public key: %w", err)
	}
	if expectedDaemonID != "" && expectedDaemonID != m.daemonID {
		return nil, ErrWrongDaemon
	}
	if expectedPublicKeyHex != "" && expectedPublicKeyHex != m.PublicKeyHex() {
		return nil, ErrWrongDaemon
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.loadPairingsLocked(); err != nil {
		return nil, err
	}
	pairing, ok := m.pairings[token]
	if !ok {
		return nil, ErrInvalidPairingToken
	}
	if time.Now().After(pairing.ExpiresAt) {
		delete(m.pairings, token)
		if err := m.savePairingsLocked(); err != nil {
			return nil, err
		}
		return nil, ErrExpiredPairingToken
	}
	delete(m.pairings, token)
	if err := m.savePairingsLocked(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if deviceName == "" {
		deviceName = deviceID
	}
	existing, ok := m.devices[deviceID]
	if ok && existing.PublicKeyHex != devicePublicKeyHex {
		return nil, fmt.Errorf("device id already paired with a different key")
	}

	device := &TrustedDevice{
		ID:           deviceID,
		Name:         deviceName,
		PublicKeyHex: devicePublicKeyHex,
		AddedAt:      now,
		LastSeenAt:   now,
	}
	if existing != nil {
		device.AddedAt = existing.AddedAt
	}
	m.devices[deviceID] = device
	if err := m.saveDevicesLocked(); err != nil {
		return nil, err
	}

	copyDevice := *device
	return &copyDevice, nil
}

func (m *Manager) VerifyAuthorization(headerValue, purpose string, maxAge time.Duration) (*TrustedDevice, error) {
	deviceID, daemonID, timestamp, nonceHex, signatureHex, err := parseAuthorizationHeader(headerValue)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if daemonID != m.daemonID {
		return nil, ErrWrongDaemon
	}

	timestampMillis, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return nil, ErrUnauthorized
	}
	timestampValue := time.UnixMilli(timestampMillis)
	now := time.Now()
	if maxAge <= 0 {
		maxAge = 5 * time.Minute
	}
	if timestampValue.Before(now.Add(-maxAge)) || timestampValue.After(now.Add(maxAge)) {
		return nil, ErrUnauthorized
	}
	if _, err := decodeFixedHex(nonceHex, 16); err != nil {
		return nil, ErrUnauthorized
	}
	signature, err := decodeFixedHex(signatureHex, ed25519.SignatureSize)
	if err != nil {
		return nil, ErrUnauthorized
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneExpiredLocked()
	nonceKey := deviceID + ":" + nonceHex
	if _, seen := m.usedNonces[nonceKey]; seen {
		return nil, ErrReplayDetected
	}

	device, ok := m.devices[deviceID]
	if !ok {
		return nil, ErrUnknownDevice
	}
	publicKeyBytes, err := decodeFixedHex(device.PublicKeyHex, ed25519.PublicKeySize)
	if err != nil {
		return nil, ErrUnauthorized
	}

	payload := BuildSignaturePayload(purpose, daemonID, deviceID, timestamp, nonceHex)
	if !ed25519.Verify(ed25519.PublicKey(publicKeyBytes), payload, signature) {
		return nil, ErrUnauthorized
	}

	m.usedNonces[nonceKey] = now.UTC()
	device.LastSeenAt = now.UTC()
	if err := m.saveDevicesLocked(); err != nil {
		return nil, err
	}

	copyDevice := *device
	return &copyDevice, nil
}

func BuildSignaturePayload(purpose, daemonID, deviceID, timestamp, nonceHex string) []byte {
	return []byte(strings.Join([]string{
		strings.TrimSpace(purpose),
		normalizeHex(daemonID),
		strings.TrimSpace(deviceID),
		strings.TrimSpace(timestamp),
		normalizeHex(nonceHex),
	}, "\n"))
}

func BuildServerAssertionPayload(purpose, daemonID, timestamp, nonceHex string) []byte {
	return []byte(strings.Join([]string{
		strings.TrimSpace(purpose),
		normalizeHex(daemonID),
		strings.TrimSpace(timestamp),
		normalizeHex(nonceHex),
	}, "\n"))
}

func (m *Manager) CreateServerAssertion(purpose string) (ServerAssertion, error) {
	timestamp := time.Now().UTC().Format(time.RFC3339)
	nonceHex, err := randomHex(16)
	if err != nil {
		return ServerAssertion{}, err
	}

	signature := ed25519.Sign(
		m.privateKey,
		BuildServerAssertionPayload(purpose, m.daemonID, timestamp, nonceHex),
	)

	return ServerAssertion{
		Timestamp:    timestamp,
		NonceHex:     nonceHex,
		SignatureHex: hex.EncodeToString(signature),
	}, nil
}

func (m *Manager) loadOrCreateIdentity() error {
	value, err := os.ReadFile(m.identityPath)
	if err == nil {
		var persisted persistedIdentity
		if err := json.Unmarshal(value, &persisted); err != nil {
			return fmt.Errorf("decode daemon identity: %w", err)
		}
		privateKeyBytes, err := decodeFixedHex(persisted.PrivateKeyHex, ed25519.PrivateKeySize)
		if err != nil {
			return fmt.Errorf("parse daemon identity: %w", err)
		}
		m.privateKey = ed25519.PrivateKey(privateKeyBytes)
		m.publicKey = m.privateKey.Public().(ed25519.PublicKey)
		m.daemonID = fingerprintHex(m.publicKey)
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read daemon identity: %w", err)
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate daemon identity: %w", err)
	}
	m.privateKey = privateKey
	m.publicKey = publicKey
	m.daemonID = fingerprintHex(publicKey)

	payload, err := json.MarshalIndent(persistedIdentity{
		PrivateKeyHex: hex.EncodeToString(privateKey),
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode daemon identity: %w", err)
	}
	return writeFileAtomic(m.identityPath, payload, 0o600)
}

func (m *Manager) loadDevices() error {
	value, err := os.ReadFile(m.devicesPath)
	if err == nil {
		var persisted persistedDevices
		if err := json.Unmarshal(value, &persisted); err != nil {
			return fmt.Errorf("decode trusted devices: %w", err)
		}
		for _, device := range persisted.Devices {
			if device == nil || strings.TrimSpace(device.ID) == "" {
				continue
			}
			copyDevice := *device
			copyDevice.PublicKeyHex = normalizeHex(copyDevice.PublicKeyHex)
			m.devices[copyDevice.ID] = &copyDevice
		}
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("read trusted devices: %w", err)
}

func (m *Manager) saveDevicesLocked() error {
	devices := make([]*TrustedDevice, 0, len(m.devices))
	for _, device := range m.devices {
		copyDevice := *device
		devices = append(devices, &copyDevice)
	}
	payload, err := json.MarshalIndent(persistedDevices{Devices: devices}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode trusted devices: %w", err)
	}
	return writeFileAtomic(m.devicesPath, payload, 0o600)
}

func (m *Manager) loadPairings() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loadPairingsLocked()
}

func (m *Manager) loadPairingsLocked() error {
	value, err := os.ReadFile(m.pairingsPath)
	if err == nil {
		var persisted persistedPairings
		if err := json.Unmarshal(value, &persisted); err != nil {
			return fmt.Errorf("decode pairing tokens: %w", err)
		}

		m.pairings = make(map[string]PairingToken)
		for _, pairing := range persisted.Pairings {
			pairing.Value = strings.TrimSpace(pairing.Value)
			if pairing.Value == "" {
				continue
			}
			pairing.ExpiresAt = pairing.ExpiresAt.UTC()
			m.pairings[pairing.Value] = pairing
		}
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		m.pairings = make(map[string]PairingToken)
		return nil
	}
	return fmt.Errorf("read pairing tokens: %w", err)
}

func (m *Manager) savePairingsLocked() error {
	pairings := make([]PairingToken, 0, len(m.pairings))
	for _, pairing := range m.pairings {
		copyPairing := pairing
		copyPairing.Value = strings.TrimSpace(copyPairing.Value)
		copyPairing.ExpiresAt = copyPairing.ExpiresAt.UTC()
		if copyPairing.Value == "" {
			continue
		}
		pairings = append(pairings, copyPairing)
	}
	sort.Slice(pairings, func(left, right int) bool {
		return pairings[left].Value < pairings[right].Value
	})

	payload, err := json.MarshalIndent(persistedPairings{Pairings: pairings}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode pairing tokens: %w", err)
	}
	return writeFileAtomic(m.pairingsPath, payload, 0o600)
}

func (m *Manager) pruneExpiredLocked() {
	now := time.Now().UTC()
	for token, pairing := range m.pairings {
		if now.After(pairing.ExpiresAt) {
			delete(m.pairings, token)
		}
	}
	for nonceKey, seenAt := range m.usedNonces {
		if now.Sub(seenAt) > 10*time.Minute {
			delete(m.usedNonces, nonceKey)
		}
	}
}

func parseAuthorizationHeader(value string) (deviceID, daemonID, timestamp, nonceHex, signatureHex string, err error) {
	token := strings.TrimSpace(value)
	if !strings.HasPrefix(token, AuthorizationHeaderPrefix) {
		return "", "", "", "", "", ErrUnauthorized
	}
	parts := strings.Split(strings.TrimSpace(strings.TrimPrefix(token, AuthorizationHeaderPrefix)), ":")
	if len(parts) != 6 || parts[0] != "v1" {
		return "", "", "", "", "", ErrUnauthorized
	}
	return strings.TrimSpace(parts[1]), normalizeHex(parts[2]), strings.TrimSpace(parts[3]), normalizeHex(parts[4]), normalizeHex(parts[5]), nil
}

func fingerprintHex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func normalizeHex(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func randomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func decodeFixedHex(value string, wantBytes int) ([]byte, error) {
	decoded, err := hex.DecodeString(normalizeHex(value))
	if err != nil {
		return nil, err
	}
	if len(decoded) != wantBytes {
		return nil, fmt.Errorf("expected %d bytes, got %d", wantBytes, len(decoded))
	}
	return decoded, nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, perm); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
