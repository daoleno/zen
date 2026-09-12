package host

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOwnershipResolvesEachTargetIndependently(t *testing.T) {
	store := NewSunshineOwnershipStore(t.TempDir())
	if _, ok, err := store.Get("device-a"); ok || err != nil {
		t.Fatal("empty store reported an enrollment")
	}
	if err := store.Claim("device-a", "uuid-a", "fp-a"); err != nil {
		t.Fatal(err)
	}
	if err := store.Claim("device-b", "uuid-b", "fp-b"); err != nil {
		t.Fatal(err)
	}
	// B resolves to B's UUID, not the first entry.
	if uuid, ok, err := store.Get("device-b"); !ok || err != nil || uuid != "uuid-b" {
		t.Fatalf("device-b = %q %v", uuid, ok)
	}
	// An unrelated target resolves to nothing.
	if _, ok, _ := store.Get("device-c"); ok {
		t.Fatal("unrelated target resolved")
	}
	// Removing B leaves A untouched.
	removed, err := store.Remove("device-b")
	if err != nil || !removed {
		t.Fatalf("remove b = %v %v", removed, err)
	}
	if uuid, ok, err := store.Get("device-a"); !ok || err != nil || uuid != "uuid-a" {
		t.Fatalf("device-a after removing b = %q %v", uuid, ok)
	}
	if _, ok, _ := store.Get("device-b"); ok {
		t.Fatal("device-b survived removal")
	}
	// Persistence across instances.
	reloaded := NewSunshineOwnershipStore(filepath.Dir(store.path))
	if uuid, ok, err := reloaded.Get("device-a"); !ok || err != nil || uuid != "uuid-a" {
		t.Fatalf("reloaded device-a = %q %v", uuid, ok)
	}
}

func TestOwnershipBindingIsImplemented(t *testing.T) {
	if !SunshineOwnershipBound() {
		t.Fatal("the pinned host exposes per-client list/update/unpair APIs")
	}
}

func fingerprintOf(t *testing.T, certPEM string) string {
	t.Helper()
	fingerprint, err := CertFingerprint(certPEM)
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint
}

func writeSunshineState(t *testing.T, stateDir, certPEM, uuid string) {
	t.Helper()
	body := map[string]any{"root": map[string]any{"named_devices": []map[string]any{
		{"name": "phone", "cert": certPEM, "uuid": uuid, "enabled": true},
	}}}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "sunshine_state.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestEnrollFromStateBindsCertToGeneratedUUID(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("ZEN_STATE_DIR", stateDir)
	certA := selfSignedPEM(t, "client-a")
	certB := selfSignedPEM(t, "client-b")
	body, _ := json.Marshal(map[string]any{"root": map[string]any{"named_devices": []map[string]any{
		{"name": "a", "cert": certA, "uuid": "gen-uuid-a", "enabled": true},
		{"name": "b", "cert": certB, "uuid": "gen-uuid-b", "enabled": true},
	}}})
	if err := os.WriteFile(filepath.Join(stateDir, "sunshine_state.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnrollFromState("device-a", certA, fingerprintOf(t, certA)); err != nil {
		t.Fatalf("enroll a: %v", err)
	}
	if err := EnrollFromState("device-b", certB, fingerprintOf(t, certB)); err != nil {
		t.Fatalf("enroll b: %v", err)
	}
	if uuid, ok, err := SunshineEnrollment("device-b"); err != nil || !ok || uuid != "gen-uuid-b" {
		t.Fatalf("device-b = %q %v %v", uuid, ok, err)
	}
	// A forged certificate is not present in the Zen-owned state.
	foreign := selfSignedPEM(t, "foreign")
	if err := EnrollFromState("device-c", foreign, fingerprintOf(t, foreign)); !errors.Is(err, errSunshineEnrollmentNotFound) {
		t.Fatalf("forged cert = %v", err)
	}
	// A certificate absent from the owned state keeps a pending ownership
	// intent (no UUID) instead of silently disappearing.
	if uuid, ok, err := SunshineEnrollment("device-c"); err != nil || !ok || uuid != "" {
		t.Fatalf("pending intent = %q %v %v", uuid, ok, err)
	}
	// Corrupt ownership storage fails closed instead of appearing unenrolled.
	if err := os.WriteFile(filepath.Join(stateDir, "sunshine_owners.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := SunshineEnrollment("device-a"); err == nil {
		t.Fatal("corrupt ownership state did not fail closed")
	}
	// Corrupt upstream state fails closed for new enrollment.
	if err := os.WriteFile(filepath.Join(stateDir, "sunshine_owners.json"), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "sunshine_state.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnrollFromState("device-a", certA, fingerprintOf(t, certA)); err == nil {
		t.Fatal("corrupt upstream state did not fail closed")
	}
}
