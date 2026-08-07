package modelprofiles

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

// CredentialStore is the daemon secret vault for Provider API keys.
// Implementations must never log, serialize, or return secrets through public
// projections — only Set/Get/Delete of opaque refs.
type CredentialStore interface {
	// Available reports whether the backend can accept writes.
	Available() bool
	Set(ref, secret string) error
	Get(ref string) (string, bool, error)
	Delete(ref string) error
}

const keyringServiceName = "zen.daemon.provider"

// CredentialRefFor returns the opaque keyring user key for a connection.
// It is secret-free and safe to persist on routes/bindings.
func CredentialRefFor(connectionID string) string {
	id := normalizeID(connectionID)
	if id == "" {
		return ""
	}
	return "provider:" + id
}

// KeyringCredentialStore stores secrets in the OS credential manager via
// github.com/zalando/go-keyring (macOS Keychain, Linux Secret Service, Windows
// Credential Manager).
type KeyringCredentialStore struct {
	service string
}

// NewKeyringCredentialStore constructs the production OS-backed store.
func NewKeyringCredentialStore() *KeyringCredentialStore {
	return &KeyringCredentialStore{service: keyringServiceName}
}

func (s *KeyringCredentialStore) serviceName() string {
	if s == nil || strings.TrimSpace(s.service) == "" {
		return keyringServiceName
	}
	return s.service
}

func (s *KeyringCredentialStore) Available() bool {
	if s == nil {
		return false
	}
	// Probe with a throwaway Get; NotFound means the backend answered.
	_, err := keyring.Get(s.serviceName(), "__zen_probe__")
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return true
	}
	return false
}

func (s *KeyringCredentialStore) Set(ref, secret string) error {
	if s == nil {
		return ErrCredentialStoreUnavailable
	}
	ref = normalizeSpace(ref)
	secret = strings.TrimSpace(secret)
	if ref == "" || secret == "" {
		return fmt.Errorf("%w: credential ref and secret are required", ErrInvalid)
	}
	if err := keyring.Set(s.serviceName(), ref, secret); err != nil {
		return fmt.Errorf("%w: %v", ErrCredentialStoreUnavailable, err)
	}
	return nil
}

func (s *KeyringCredentialStore) Get(ref string) (string, bool, error) {
	if s == nil {
		return "", false, ErrCredentialStoreUnavailable
	}
	ref = normalizeSpace(ref)
	if ref == "" {
		return "", false, nil
	}
	val, err := keyring.Get(s.serviceName(), ref)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("%w: %v", ErrCredentialStoreUnavailable, err)
	}
	val = strings.TrimSpace(val)
	if val == "" {
		return "", false, nil
	}
	return val, true, nil
}

func (s *KeyringCredentialStore) Delete(ref string) error {
	if s == nil {
		return ErrCredentialStoreUnavailable
	}
	ref = normalizeSpace(ref)
	if ref == "" {
		return nil
	}
	err := keyring.Delete(s.serviceName(), ref)
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return fmt.Errorf("%w: %v", ErrCredentialStoreFailed, err)
}

// MemoryCredentialStore is a test fake. It never persists to disk.
type MemoryCredentialStore struct {
	mu        sync.Mutex
	secrets   map[string]string
	available bool
	failSet   error
	failGet   error
	failDel   error
}

// NewMemoryCredentialStore returns an available in-memory fake.
func NewMemoryCredentialStore() *MemoryCredentialStore {
	return &MemoryCredentialStore{
		secrets:   map[string]string{},
		available: true,
	}
}

func (m *MemoryCredentialStore) SetAvailable(ok bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.available = ok
	m.mu.Unlock()
}

func (m *MemoryCredentialStore) SetFail(set, get, del error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.failSet, m.failGet, m.failDel = set, get, del
	m.mu.Unlock()
}

func (m *MemoryCredentialStore) Available() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.available
}

func (m *MemoryCredentialStore) Set(ref, secret string) error {
	if m == nil || !m.Available() {
		return ErrCredentialStoreUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failSet != nil {
		return m.failSet
	}
	ref = normalizeSpace(ref)
	secret = strings.TrimSpace(secret)
	if ref == "" || secret == "" {
		return fmt.Errorf("%w: credential ref and secret are required", ErrInvalid)
	}
	if m.secrets == nil {
		m.secrets = map[string]string{}
	}
	m.secrets[ref] = secret
	return nil
}

func (m *MemoryCredentialStore) Get(ref string) (string, bool, error) {
	if m == nil {
		return "", false, ErrCredentialStoreUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.available {
		return "", false, ErrCredentialStoreUnavailable
	}
	if m.failGet != nil {
		return "", false, m.failGet
	}
	ref = normalizeSpace(ref)
	v, ok := m.secrets[ref]
	if !ok || strings.TrimSpace(v) == "" {
		return "", false, nil
	}
	return v, true, nil
}

func (m *MemoryCredentialStore) Delete(ref string) error {
	if m == nil || !m.Available() {
		return ErrCredentialStoreUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failDel != nil {
		return m.failDel
	}
	ref = normalizeSpace(ref)
	delete(m.secrets, ref)
	return nil
}

// SnapshotRefs returns stored refs only (never secret values) for tests.
func (m *MemoryCredentialStore) SnapshotRefs() []string {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.secrets))
	for k := range m.secrets {
		out = append(out, k)
	}
	return out
}

// resolveProviderSecret returns the secret for a connection: keyring ref first,
// then host-env fallback named by CredentialEnv. Never logs the value.
func resolveProviderSecret(ref, envName string, store CredentialStore, lookup func(string) (string, bool)) (string, error) {
	ref = normalizeSpace(ref)
	if store != nil && ref != "" {
		val, ok, err := store.Get(ref)
		if err != nil && !errors.Is(err, ErrCredentialStoreUnavailable) {
			return "", err
		}
		if ok && strings.TrimSpace(val) != "" {
			return strings.TrimSpace(val), nil
		}
		// Unavailable store or miss → env fallback.
	}
	return resolveSecretEnv(envName, lookup)
}

// providerCredentialReady reports readiness without exposing secret material.
func providerCredentialReady(connectionID, envName string, store CredentialStore, lookup func(string) (string, bool)) bool {
	ref := CredentialRefFor(connectionID)
	if store != nil && ref != "" {
		if val, ok, err := store.Get(ref); err == nil && ok && strings.TrimSpace(val) != "" {
			return true
		}
	}
	if normalizeSpace(envName) == "" {
		return true
	}
	return CredentialReady(envName, lookup)
}
