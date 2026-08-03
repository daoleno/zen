//go:build linux || darwin

package control

import "testing"

func TestLifecycleLockExcludesFallbackAndRecoversAfterRelease(t *testing.T) {
	stateDir := t.TempDir()
	first, acquired, err := TryAcquireLifecycleLock(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("first lifecycle owner was not acquired")
	}
	defer first.Close()

	second, acquired, err := TryAcquireLifecycleLock(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if acquired || second != nil {
		t.Fatal("concurrent lifecycle owner was acquired")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	recovered, acquired, err := TryAcquireLifecycleLock(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if !acquired || recovered == nil {
		t.Fatal("lifecycle ownership did not recover after release")
	}
	if err := recovered.Close(); err != nil {
		t.Fatal(err)
	}
}
