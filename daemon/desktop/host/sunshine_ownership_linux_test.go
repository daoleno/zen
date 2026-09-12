package host

import (
	"path/filepath"
	"testing"
)

func TestOwnershipTargetsOnlyTheEnrolledDevice(t *testing.T) {
	store := NewSunshineOwnershipStore(t.TempDir())
	if _, fingerprint := store.Owner(); fingerprint != "" {
		t.Fatal("empty store reported an owner")
	}
	if err := store.Claim("device-a", "cert-a"); err != nil {
		t.Fatal(err)
	}
	if owner, fingerprint := store.Owner(); owner != "device-a" || fingerprint != "cert-a" {
		t.Fatalf("owner = %s %s", owner, fingerprint)
	}
	// An unrelated target removes nothing.
	removed, err := store.Remove("device-b")
	if err != nil || removed {
		t.Fatalf("unrelated remove = %v %v", removed, err)
	}
	if owner, _ := store.Owner(); owner != "device-a" {
		t.Fatalf("unrelated target changed the owner: %s", owner)
	}
	// The target device removes only its own enrollment.
	removed, err = store.Remove("device-a")
	if err != nil || !removed {
		t.Fatalf("target remove = %v %v", removed, err)
	}
	if owner, _ := store.Owner(); owner != "" {
		t.Fatalf("target enrollment survived: %s", owner)
	}
	// Persistence across instances.
	second := NewSunshineOwnershipStore(filepath.Dir(store.path))
	if err := second.Claim("device-b", "cert-b"); err != nil {
		t.Fatal(err)
	}
	if owner, fingerprint := NewSunshineOwnershipStore(filepath.Dir(store.path)).Owner(); owner != "device-b" || fingerprint != "cert-b" {
		t.Fatalf("reloaded owner = %s %s", owner, fingerprint)
	}
}

func TestOwnershipBindingIsClosedWithoutUpstreamPerClientRemoval(t *testing.T) {
	if SunshineOwnershipBound() {
		t.Fatal("availability must stay closed until per-client removal is verifiable")
	}
}
