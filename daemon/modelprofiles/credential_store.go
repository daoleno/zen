package modelprofiles

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// CredentialStore is the daemon secret vault for Provider API keys.
// Implementations must never log, serialize, or return secrets through public
// projections — only Set/Get/Delete/Refs of opaque refs.
type CredentialStore interface {
	// Available reports whether the backend can accept writes.
	Available() bool
	Set(ref, secret string) error
	Get(ref string) (string, bool, error)
	Delete(ref string) error
	// Refs returns the opaque refs currently stored (never secret values).
	Refs() ([]string, error)
}

// CredentialRefFor returns the opaque credential key for a connection.
// It is secret-free and safe to persist on routes/bindings.
func CredentialRefFor(connectionID string) string {
	id := normalizeID(connectionID)
	if id == "" {
		return ""
	}
	return "provider:" + id
}

// activeCredentialRef returns the credential ref a connection currently
// resolves: the durable versioned ref when the catalog row carries one, else
// the canonical legacy provider:<id> ref.
func activeCredentialRef(profile Profile) string {
	profile = normalizeProfile(profile)
	if ref := normalizeSpace(profile.CredentialRef); ref != "" {
		return ref
	}
	return CredentialRefFor(profile.ID)
}

// newStagedCredentialRef builds a private, not-yet-active credential ref for a
// connection edit. The secret staged under this ref is invisible to routing
// until the single catalog commit flips the row to reference it, so an
// observer always sees either the complete old version or the complete new
// version of (Name, Base URL, active key).
func newStagedCredentialRef(connectionID string) string {
	id := normalizeID(connectionID)
	if id == "" {
		return ""
	}
	return "provider:" + id + ":" + hex.EncodeToString(randomTokenBytes())
}

func randomTokenBytes() []byte {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is effectively unreachable; keep a unique token.
		return []byte(fmt.Sprintf("%x", time.Now().UnixNano()))
	}
	return b[:]
}

// isProviderCredentialRef reports whether a ref belongs to Zen's Provider
// credential namespace (safe to sweep/delete).
func isProviderCredentialRef(ref string) bool {
	return strings.HasPrefix(normalizeSpace(ref), "provider:")
}

// providerCredentialRefsForConnection returns the refs owned by one
// connection: the canonical provider:<id> ref plus every staged/versioned
// provider:<id>:<token> ref. refs is typically the store's Refs() listing.
func providerCredentialRefsForConnection(connectionID string, refs []string) []string {
	id := normalizeID(connectionID)
	out := make([]string, 0, len(refs))
	canonical := "provider:" + id
	prefix := canonical + ":"
	for _, ref := range refs {
		ref = normalizeSpace(ref)
		if ref == canonical || strings.HasPrefix(ref, prefix) {
			out = append(out, ref)
		}
	}
	return out
}

const credentialFileSchemaVersion = 1

type credentialFile struct {
	SchemaVersion int               `json:"schema_version"`
	Secrets       map[string]string `json:"secrets"`
}

// FileCredentialStore is the production secret store for the headless Zen
// daemon. The parent directory is private and every committed file is 0600, so
// Provider credentials do not depend on a desktop Secret Service being active.
type FileCredentialStore struct {
	mu      sync.Mutex
	path    string
	secrets map[string]string
}

func NewFileCredentialStore(path string) (*FileCredentialStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("%w: credential file path is required", ErrInvalid)
	}
	store := &FileCredentialStore{path: path, secrets: map[string]string{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: read credential file: %v", ErrCredentialStoreFailed, err)
	}
	var doc credentialFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%w: decode credential file: %v", ErrCredentialStoreFailed, err)
	}
	if doc.SchemaVersion != credentialFileSchemaVersion || doc.Secrets == nil {
		return nil, fmt.Errorf("%w: unsupported credential file", ErrCredentialStoreFailed)
	}
	for ref, secret := range doc.Secrets {
		ref = normalizeSpace(ref)
		secret = strings.TrimSpace(secret)
		if ref != "" && secret != "" {
			store.secrets[ref] = secret
		}
	}
	return store, nil
}

func (s *FileCredentialStore) Available() bool {
	return s != nil && strings.TrimSpace(s.path) != ""
}

func (s *FileCredentialStore) Set(ref, secret string) error {
	if !s.Available() {
		return ErrCredentialStoreUnavailable
	}
	ref = normalizeSpace(ref)
	secret = strings.TrimSpace(secret)
	if ref == "" || secret == "" {
		return fmt.Errorf("%w: credential ref and secret are required", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.secrets[ref]
	s.secrets[ref] = secret
	if err := s.saveLocked(); err != nil {
		if existed {
			s.secrets[ref] = previous
		} else {
			delete(s.secrets, ref)
		}
		return err
	}
	return nil
}

func (s *FileCredentialStore) Get(ref string) (string, bool, error) {
	if !s.Available() {
		return "", false, ErrCredentialStoreUnavailable
	}
	ref = normalizeSpace(ref)
	if ref == "" {
		return "", false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	secret, ok := s.secrets[ref]
	return secret, ok && secret != "", nil
}

func (s *FileCredentialStore) Delete(ref string) error {
	if !s.Available() {
		return ErrCredentialStoreUnavailable
	}
	ref = normalizeSpace(ref)
	if ref == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.secrets[ref]
	if !existed {
		return nil
	}
	delete(s.secrets, ref)
	if err := s.saveLocked(); err != nil {
		s.secrets[ref] = previous
		return err
	}
	return nil
}

// Refs returns the stored refs sorted (never secret values).
func (s *FileCredentialStore) Refs() ([]string, error) {
	if !s.Available() {
		return nil, ErrCredentialStoreUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.secrets))
	for ref := range s.secrets {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out, nil
}

func (s *FileCredentialStore) saveLocked() error {
	raw, err := json.MarshalIndent(credentialFile{
		SchemaVersion: credentialFileSchemaVersion,
		Secrets:       s.secrets,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: encode credential file: %v", ErrCredentialStoreFailed, err)
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("%w: create credential directory: %v", ErrCredentialStoreFailed, err)
	}
	tmp, err := os.CreateTemp(dir, ".provider-credentials-*.tmp")
	if err != nil {
		return fmt.Errorf("%w: create credential file: %v", ErrCredentialStoreFailed, err)
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("%w: secure credential file: %v", ErrCredentialStoreFailed, err)
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("%w: write credential file: %v", ErrCredentialStoreFailed, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("%w: sync credential file: %v", ErrCredentialStoreFailed, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("%w: close credential file: %v", ErrCredentialStoreFailed, err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("%w: commit credential file: %v", ErrCredentialStoreFailed, err)
	}
	removeTemp = false
	// After Rename the new credential set is authoritative. Directory sync is
	// best-effort because the CredentialStore contract has no applied-with-
	// warning state; returning an error here would make memory disagree with the
	// file that callers will read on restart.
	if parent, openErr := os.Open(dir); openErr == nil {
		_ = parent.Sync()
		_ = parent.Close()
	}
	return nil
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

// Refs returns the stored refs sorted (never secret values).
func (m *MemoryCredentialStore) Refs() ([]string, error) {
	if m == nil || !m.Available() {
		return nil, ErrCredentialStoreUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.secrets))
	for ref := range m.secrets {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out, nil
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

// resolveProviderSecret returns the secret for a connection: Zen's private
// credential file first, then the host-env fallback named by CredentialEnv.
// It never logs the value.
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
// The connection's active credential ref is resolved from the durable row so
// readiness always matches the version the catalog currently references.
func providerCredentialReady(profile Profile, store CredentialStore, lookup func(string) (string, bool)) bool {
	ref := activeCredentialRef(profile)
	if store != nil && ref != "" {
		if val, ok, err := store.Get(ref); err == nil && ok && strings.TrimSpace(val) != "" {
			return true
		}
	}
	if normalizeSpace(profile.CredentialEnv) == "" {
		return true
	}
	return CredentialReady(profile.CredentialEnv, lookup)
}
