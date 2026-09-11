package desktop

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIsInternalRole(t *testing.T) {
	for _, role := range []string{RoleHelper, RoleHost, RoleAgent, RoleIdentity} {
		if !IsInternalRole(role) {
			t.Fatalf("%s should be internal", role)
		}
	}
	if IsInternalRole("serve") || IsInternalRole("doctor") {
		t.Fatal("public commands must not be internal desktop roles")
	}
}

func TestHelperCommandDefaultUsesSameExecutable(t *testing.T) {
	t.Setenv(HelperOverrideEnv, "")
	exe := filepath.Join(t.TempDir(), "zen-fixture")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	lookupExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { lookupExecutable = os.Executable })
	cmd, err := HelperCommand([]string{"--device", "Phone", "--display", ":9"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != exe {
		t.Fatalf("path=%q want %q", cmd.Path, exe)
	}
	got := strings.Join(cmd.Args, " ")
	want := exe + " " + RoleHelper + " --device Phone --display :9"
	if got != want {
		t.Fatalf("args=%q want %q", got, want)
	}
}

func TestHelperCommandRejectsRelativeOverride(t *testing.T) {
	t.Setenv(HelperOverrideEnv, "zen-desktop-helper")
	if _, err := HelperCommand(nil); !errorsIsUnavailable(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestHelperCommandAbsoluteOverrideForTests(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "helper")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(HelperOverrideEnv, helper)
	cmd, err := HelperCommand([]string{"--control"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != helper || strings.Contains(strings.Join(cmd.Args, " "), RoleHelper) {
		t.Fatalf("override leaked role dispatch: path=%q args=%q", cmd.Path, cmd.Args)
	}
}

func TestIdentityHashesExecutable(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "zen")
	if err := os.WriteFile(exe, []byte("fixture-bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	lookupExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { lookupExecutable = os.Executable })
	id, err := NewIdentity(runtime.GOOS == "linux")
	if err != nil {
		t.Fatal(err)
	}
	sum, err := FileSHA256(exe)
	if err != nil || id.SHA256 != sum || id.Executable != exe {
		t.Fatalf("identity=%+v sum=%s err=%v", id, sum, err)
	}
	if len(id.Roles) != 3 {
		t.Fatalf("roles=%v", id.Roles)
	}
}

func errorsIsUnavailable(err error) bool {
	return err == ErrHelperUnavailable
}
