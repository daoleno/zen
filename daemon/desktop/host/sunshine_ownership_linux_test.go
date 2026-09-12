package host

import (
	"path/filepath"
	"testing"
)

func TestOwnershipResolvesEachTargetIndependently(t *testing.T) {
	store := NewSunshineOwnershipStore(t.TempDir())
	if _, ok := store.Get("device-a"); ok {
		t.Fatal("empty store reported an enrollment")
	}
	if err := store.Claim("device-a", "uuid-a"); err != nil {
		t.Fatal(err)
	}
	if err := store.Claim("device-b", "uuid-b"); err != nil {
		t.Fatal(err)
	}
	// B resolves to B's UUID, not the first entry.
	if uuid, ok := store.Get("device-b"); !ok || uuid != "uuid-b" {
		t.Fatalf("device-b = %q %v", uuid, ok)
	}
	// An unrelated target resolves to nothing.
	if _, ok := store.Get("device-c"); ok {
		t.Fatal("unrelated target resolved")
	}
	// Removing B leaves A untouched.
	removed, err := store.Remove("device-b")
	if err != nil || !removed {
		t.Fatalf("remove b = %v %v", removed, err)
	}
	if uuid, ok := store.Get("device-a"); !ok || uuid != "uuid-a" {
		t.Fatalf("device-a after removing b = %q %v", uuid, ok)
	}
	if _, ok := store.Get("device-b"); ok {
		t.Fatal("device-b survived removal")
	}
	// Persistence across instances.
	reloaded := NewSunshineOwnershipStore(filepath.Dir(store.path))
	if uuid, ok := reloaded.Get("device-a"); !ok || uuid != "uuid-a" {
		t.Fatalf("reloaded device-a = %q %v", uuid, ok)
	}
}

func TestOwnershipBindingIsImplemented(t *testing.T) {
	if !SunshineOwnershipBound() {
		t.Fatal("the pinned host exposes per-client list/update/unpair APIs")
	}
}
