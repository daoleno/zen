package desktop

import (
	"encoding/json"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestSameExecutableRoleDispatchNoHelperEnv(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("same-ELF desktop roles are Linux host capture")
	}
	t.Setenv(HelperOverrideEnv, "")
	bin, sum := builtDesktopELF(t)
	assertGTKLinked(t, bin)
	ident, err := exec.Command(bin, RoleIdentity).Output()
	if err != nil {
		t.Fatalf("desktop-identity: %v\n%s", err, ident)
	}
	var id Identity
	if json.Unmarshal(ident, &id) != nil || id.SHA256 != sum {
		t.Fatalf("identity hash mismatch: %s vs %s payload=%s", id.SHA256, sum, ident)
	}
	if !id.Native {
		t.Fatal("identity must report native cgo-linked capture")
	}
	if id.NativeBuildInput == "" || id.NativeBuildInput == "none" {
		t.Fatalf("identity must report native_build_input from linked C define: %+v", id)
	}
	if strings.Contains(id.Executable, "zen-desktop-helper") {
		t.Fatal("identity referenced a separate helper ELF")
	}
	report, _ := exec.Command(bin, "doctor", "--json").CombinedOutput()
	if !strings.Contains(string(report), `"native_linked": true`) {
		t.Fatalf("doctor must report native_linked on the desktop ELF:\n%s", report)
	}
	if strings.Contains(string(report), `"stream_ready": true`) {
		t.Fatalf("doctor must not claim stream_ready from linking:\n%s", report)
	}
	for _, role := range [][]string{
		{RoleHelper, "--device", "Fixture", "--display", ":9"},
		{RoleAgent, ":9", "view"},
	} {
		proc := exec.Command(bin, role...)
		proc.Env = []string{"PATH=/usr/bin", "HOME=/nonexistent", "LANG=C.UTF-8"}
		if err := proc.Start(); err != nil {
			t.Fatal(err)
		}
		assertSameELF(t, proc.Process.Pid, bin, sum)
		_ = proc.Process.Kill()
		_ = proc.Wait()
	}
}
