package host

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SunshineOwnershipStore maps verified Zen device identities to the upstream
// Sunshine client UUID they enrolled with. The UUID is the client uniqueid the
// Zen app sends during pairing (the Zen device id), so enrollment is verified
// against GET /api/clients/list rather than guessed.
type SunshineOwnershipStore struct {
	path string
	mu   sync.Mutex
}

type sunshineOwnership struct {
	DeviceID string `json:"device_id"`
	UUID     string `json:"uuid"`
}

var errSunshineEnrollmentNotFound = errors.New("sunshine_enrollment_not_found")

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

// Claim records the verified enrollment for one device, replacing any previous
// record for that device only.
func (s *SunshineOwnershipStore) Claim(deviceID, uuid string) error {
	if deviceID == "" || uuid == "" {
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
	next = append(next, sunshineOwnership{DeviceID: deviceID, UUID: uuid})
	return s.persistLocked(next)
}

// Get returns the enrollment for a specific device; it never falls back to the
// first entry, so target B resolves correctly when A and B are both enrolled.
func (s *SunshineOwnershipStore) Get(deviceID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.loadLocked()
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if entry.DeviceID == deviceID {
			return entry.UUID, true
		}
	}
	return "", false
}

// Remove drops only the target device's enrollment.
func (s *SunshineOwnershipStore) Remove(deviceID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.loadLocked()
	if err != nil {
		return false, err
	}
	next := entries[:0]
	removed := false
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

// SunshineOwnershipBound reports that the pinned host has real per-client APIs
// (list/update/unpair) usable through SunshineAdmin.
func SunshineOwnershipBound() bool { return true }

// EnrollSunshineDevice verifies the device's UUID exists in the host client
// list and records the verified enrollment.
func EnrollSunshineDevice(admin *SunshineAdmin, deviceID string) error {
	if admin == nil {
		return errors.New("sunshine_admin_unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	clients, err := admin.ListClients(ctx)
	if err != nil {
		return err
	}
	for _, client := range clients {
		if client.UUID == deviceID {
			return NewSunshineOwnershipStore(ZenStateDir()).Claim(deviceID, client.UUID)
		}
	}
	return errSunshineEnrollmentNotFound
}

// SunshineEnrollment returns the verified upstream UUID for a device.
func SunshineEnrollment(deviceID string) (string, bool) {
	return NewSunshineOwnershipStore(ZenStateDir()).Get(deviceID)
}

// RemoveSunshineEnrollment drops the target device's enrollment record after a
// successful upstream revoke; unrelated enrollments stay untouched.
func RemoveSunshineEnrollment(deviceID string) error {
	_, err := NewSunshineOwnershipStore(ZenStateDir()).Remove(deviceID)
	return err
}

// RevokeSunshineTarget disables the target's client (upstream terminates its
// sessions by certificate) and removes only its pairing, then drops the local
// enrollment. Failures leave the enrollment for a retry and never touch other
// devices.
func RevokeSunshineTarget(ctx context.Context, deviceID string) error {
	admin, err := SunshineAdminFromRuntime()
	if err != nil {
		return err
	}
	return revokeSunshineTargetWith(admin, deviceID)
}

// revokeSunshineTargetWith is the bounded target-removal step used by the
// production wrapper and by tests with an inert admin server.
func revokeSunshineTargetWith(admin *SunshineAdmin, deviceID string) error {
	uuid, ok := SunshineEnrollment(deviceID)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := admin.SetClientEnabled(ctx, uuid, false); err != nil {
		return err
	}
	if err := admin.UnpairClient(ctx, uuid); err != nil {
		return err
	}
	return RemoveSunshineEnrollment(deviceID)
}
