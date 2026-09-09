package desktop

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSameExecutableRoleDispatchNoHelperEnv(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("same-ELF desktop roles are Linux host capture")
	}
	t.Setenv(HelperOverrideEnv, "")
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	daemonRoot := filepath.Join(filepath.Dir(thisFile), "..")
	outDir := t.TempDir()
	if buildTmp := strings.TrimSpace(os.Getenv("ZEN_BUILD_TMPDIR")); buildTmp != "" {
		outDir = filepath.Join(buildTmp, "zen-sameexec-"+strconv.Itoa(os.Getpid()))
		if err := os.MkdirAll(outDir, 0700); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(outDir) })
	}
	bin := filepath.Join(outDir, "zen")
	args := []string{"build", "-o", bin}
	env := os.Environ()
	if exec.Command("pkg-config", "--exists", "gtk+-3.0", "gstreamer-app-1.0", "gstreamer-video-1.0", "x11", "xtst", "gio-unix-2.0").Run() == nil {
		args = append(args, "-tags", "zen_desktop")
		env = append(env, "CGO_ENABLED=1")
	}
	args = append(args, "./cmd/zen")
	cmd := exec.Command("go", args...)
	cmd.Dir = daemonRoot
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build zen: %v\n%s", err, output)
	}
	sum, err := FileSHA256(bin)
	if err != nil {
		t.Fatal(err)
	}
	ident, err := exec.Command(bin, RoleIdentity).Output()
	if err != nil {
		t.Fatalf("desktop-identity: %v\n%s", err, ident)
	}
	var id Identity
	if json.Unmarshal(ident, &id) != nil || id.SHA256 != sum {
		t.Fatalf("identity hash mismatch: %s vs %s payload=%s", id.SHA256, sum, ident)
	}
	if strings.Contains(id.Executable, "zen-desktop-helper") {
		t.Fatal("identity referenced a separate helper ELF")
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
		exeLink := ""
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if target, readErr := os.Readlink("/proc/" + strconv.Itoa(proc.Process.Pid) + "/exe"); readErr == nil {
				exeLink = target
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		_ = proc.Process.Kill()
		_ = proc.Wait()
		if exeLink == "" {
			continue
		}
		if strings.Contains(exeLink, "zen-desktop-helper") || strings.Contains(exeLink, "zen-desktop-agent") {
			t.Fatalf("auxiliary helper executable substitution: %s", exeLink)
		}
		resolved, _ := filepath.EvalSymlinks(bin)
		if filepath.Base(exeLink) != "zen" && filepath.Clean(exeLink) != filepath.Clean(resolved) {
			t.Fatalf("role %s executed %q not %q", role[0], exeLink, resolved)
		}
	}
}
