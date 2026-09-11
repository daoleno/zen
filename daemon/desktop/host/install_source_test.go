package host

import (
	"os"
	"path/filepath"
	"testing"
)

func TestForbiddenBrokerPathRejectsUserWritableDEV(t *testing.T) {
	for _, path := range []string{
		"/home/daoleno/workspace/zen/daemon/tmp/zen-dev",
		"/tmp/zen",
		"/var/tmp/zen-dev",
		"/run/user/1000/zen",
		"/home/daoleno/.zen/bin/zen",
	} {
		if !ForbiddenBrokerPath(path) {
			t.Fatalf("allowed user-writable path %s", path)
		}
	}
	if ForbiddenBrokerPath(InstalledBinary) {
		t.Fatal("installed path must be allowed")
	}
}

func TestResolveInstallSourceRequiresIdenticalBytes(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	c := filepath.Join(dir, "c")
	if err := os.WriteFile(a, []byte("same-elf"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("same-elf"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c, []byte("other-elf"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveInstallSource(a, b, a)
	if err != nil || got != a {
		t.Fatalf("identical sources: %q %v", got, err)
	}
	if _, err := ResolveInstallSource("", a, c); err == nil {
		t.Fatal("mismatched broker/agent accepted")
	}
	if _, err := ResolveInstallSource("", "", ""); err == nil {
		t.Fatal("empty source accepted")
	}
}
