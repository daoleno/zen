package host

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// SunshineOwnershipStore maps Zen device identities to the upstream client
// certificate/session they enrolled with, so revocation can target the owning
// device instead of erasing every paired client.
type SunshineOwnershipStore struct {
	path string
	mu   sync.Mutex
}

type sunshineOwnership struct {
	DeviceID          string `json:"device_id"`
	ClientFingerprint string `json:"client_fingerprint"`
}

func NewSunshineOwnershipStore(stateDir string) *SunshineOwnershipStore {
	return &SunshineOwnershipStore{path: filepath.Join(stateDir, "sunshine_owners.json")}
}

func (s *SunshineOwnershipStore) loadLocked() ([]sunshineOwnership, error) {
	body, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var entries []sunshineOwnership
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (s *SunshineOwnershipStore) persistLocked(entries []sunshineOwnership) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	body, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	temp := s.path + ".tmp"
	if err := os.WriteFile(temp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(temp, s.path)
}

// Claim records (or replaces) one device's enrollment with its upstream client
// fingerprint. A device always owns at most one engine enrollment.
func (s *SunshineOwnershipStore) Claim(deviceID, clientFingerprint string) error {
	if deviceID == "" || clientFingerprint == "" {
		return errors.New("sunshine_ownership_incomplete")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.loadLocked()
	if err != nil {
		return err
	}
	next := entries[:0]
	for _, entry := range entries {
		if entry.DeviceID != deviceID {
			next = append(next, entry)
		}
	}
	next = append(next, sunshineOwnership{DeviceID: deviceID, ClientFingerprint: clientFingerprint})
	return s.persistLocked(next)
}

// Owner returns the enrolled device and fingerprint, or empty strings.
func (s *SunshineOwnershipStore) Owner() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.loadLocked()
	if err != nil || len(entries) == 0 {
		return "", ""
	}
	return entries[0].DeviceID, entries[0].ClientFingerprint
}

// Remove drops only the target device's enrollment. Unrelated devices and an
// unrelated target are left untouched.
func (s *SunshineOwnershipStore) Remove(deviceID string) (removed bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.loadLocked()
	if err != nil {
		return false, err
	}
	next := entries[:0]
	for _, entry := range entries {
		if entry.DeviceID == deviceID {
			removed = true
			continue
		}
		next = append(next, entry)
	}
	if !removed {
		return false, nil
	}
	return true, s.persistLocked(next)
}

// SunshineOwnershipBound reports whether per-device enrollment/removal is
// enforceable against the pinned upstream host. Sunshine exposes a host-wide
// state file and no per-client unpair API, so Zen cannot remove one device's
// pairing without erasing every paired client; availability stays closed until
// a verified per-client binding exists.
func SunshineOwnershipBound() bool { return false }

// SunshineOwner returns the currently enrolled device identity.
func SunshineOwner() (string, string) {
	return NewSunshineOwnershipStore(ZenStateDir()).Owner()
}
