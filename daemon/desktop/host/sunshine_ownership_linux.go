package host

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SunshineOwnershipStore maps authenticated Zen device identities to the
// authoritative Sunshine client UUID. Sunshine generates that UUID itself
// (nvhttp.cpp add_authorized_client) and the public list API does not expose
// the client certificate, so the missing correlation comes from a structured,
// read-only validation of the Zen-owned Sunshine state file:
//
//	{"root":{"named_devices":[{"name","cert","uuid","enabled"}]}}
//
// Zen never writes upstream state and never matches friendly names.
type SunshineOwnershipStore struct {
	path string
}

type sunshineOwnership struct {
	DeviceID string `json:"device_id"`
	UUID     string `json:"uuid"`
}

type sunshineStateRoot struct {
	Root struct {
		NamedDevices []struct {
			Name    string `json:"name"`
			Cert    string `json:"cert"`
			UUID    string `json:"uuid"`
			Enabled bool   `json:"enabled"`
		} `json:"named_devices"`
	} `json:"root"`
}

var errSunshineEnrollmentNotFound = errors.New("sunshine_enrollment_not_found")
var errSunshineStateUnreadable = errors.New("sunshine_state_unreadable")

// One process-wide lock serializes every store instance sharing the file.
var sunshineOwnershipMu sync.Mutex

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
		return nil, fmt.Errorf("%w: %v", errSunshineStateUnreadable, err)
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

// Claim records the verified enrollment for one device, replacing only that
// device's previous record.
func (s *SunshineOwnershipStore) Claim(deviceID, uuid string) error {
	if deviceID == "" || uuid == "" {
		return errors.New("sunshine_ownership_incomplete")
	}
	sunshineOwnershipMu.Lock()
	defer sunshineOwnershipMu.Unlock()
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

// Get resolves one device's enrollment. Corrupt ownership state fails closed
// instead of looking like "not enrolled".
func (s *SunshineOwnershipStore) Get(deviceID string) (string, bool, error) {
	sunshineOwnershipMu.Lock()
	defer sunshineOwnershipMu.Unlock()
	entries, err := s.loadLocked()
	if err != nil {
		return "", false, err
	}
	for _, entry := range entries {
		if entry.DeviceID == deviceID {
			return entry.UUID, true, nil
		}
	}
	return "", false, nil
}

// Remove drops only the target device's enrollment.
func (s *SunshineOwnershipStore) Remove(deviceID string) (bool, error) {
	sunshineOwnershipMu.Lock()
	defer sunshineOwnershipMu.Unlock()
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

// SunshineOwnershipBound reports that the pinned host exposes the per-client
// admin APIs and the Zen-owned state correlation needed for target removal.
func SunshineOwnershipBound() bool { return true }

// EnrollFromState binds an authenticated Zen device to the Sunshine-generated
// UUID whose stored certificate exactly matches the client certificate the
// device presented. The state file is read-only; a forged certificate is
// rejected and malformed state fails closed.
func EnrollFromState(deviceID, clientCertPEM string) error {
	if deviceID == "" {
		return errors.New("sunshine_ownership_incomplete")
	}
	presented, err := certificateDER([]byte(clientCertPEM))
	if err != nil {
		return fmt.Errorf("sunshine_enrollment_invalid_certificate: %w", err)
	}
	stateBody, err := os.ReadFile(SunshineStateFilePath())
	if err != nil {
		return fmt.Errorf("%w: %v", errSunshineStateUnreadable, err)
	}
	var state sunshineStateRoot
	if err := json.Unmarshal(stateBody, &state); err != nil {
		return fmt.Errorf("%w: %v", errSunshineStateUnreadable, err)
	}
	for _, client := range state.Root.NamedDevices {
		stored, err := certificateDER([]byte(client.Cert))
		if err != nil || client.UUID == "" {
			continue
		}
		if string(stored) == string(presented) {
			return NewSunshineOwnershipStore(ZenStateDir()).Claim(deviceID, client.UUID)
		}
	}
	return errSunshineEnrollmentNotFound
}

func certificateDER(pemBytes []byte) ([]byte, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("not_a_certificate")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	return certificate.Raw, nil
}

// SunshineEnrollment returns the verified upstream UUID for a device.
func SunshineEnrollment(deviceID string) (string, bool, error) {
	return NewSunshineOwnershipStore(ZenStateDir()).Get(deviceID)
}

// RemoveSunshineEnrollment drops the target's record after a successful
// upstream revoke; unrelated enrollments stay untouched.
func RemoveSunshineEnrollment(deviceID string) error {
	_, err := NewSunshineOwnershipStore(ZenStateDir()).Remove(deviceID)
	return err
}

// RevokeSunshineTarget disables the target's client (upstream terminates its
// sessions by certificate) and removes only its pairing. Caller context is
// propagated, failures keep the enrollment for retry, and other devices are
// never touched.
func RevokeSunshineTarget(ctx context.Context, deviceID string) error {
	admin, err := SunshineAdminFromRuntime()
	if err != nil {
		return err
	}
	return revokeSunshineTargetWith(admin, ctx, deviceID)
}

// revokeSunshineTargetWith performs the bounded target removal with an
// explicit admin client (used by the production wrapper and inert tests).
func revokeSunshineTargetWith(admin *SunshineAdmin, ctx context.Context, deviceID string) error {
	uuid, ok, err := SunshineEnrollment(deviceID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := admin.SetClientEnabled(bounded, uuid, false); err != nil {
		return err
	}
	if err := admin.UnpairClient(bounded, uuid); err != nil {
		return err
	}
	_, err = NewSunshineOwnershipStore(ZenStateDir()).Remove(deviceID)
	return err
}
