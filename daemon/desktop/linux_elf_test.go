package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	elfOnce sync.Once
	elfBin  string
	elfSum  string
	elfErr  error
)

func requireDesktopPkgConfig(t *testing.T) {
	t.Helper()
	if exec.Command("pkg-config", "--exists", "gtk+-3.0", "gstreamer-app-1.0", "gstreamer-video-1.0", "x11", "xtst", "gio-unix-2.0").Run() != nil {
		t.Skip("desktop pkg-config modules required")
	}
}

func builtDesktopELF(t *testing.T) (string, string) {
	t.Helper()
	requireDesktopPkgConfig(t)
	elfOnce.Do(func() {
		_, thisFile, _, ok := runtime.Caller(0)
		if !ok {
			elfErr = errString("caller")
			return
		}
		daemonRoot := filepath.Join(filepath.Dir(thisFile), "..")
		home := strings.TrimSpace(os.Getenv("HOME"))
		outDir := filepath.Join(home, ".zen", "artifacts", "2026-09-09-zen-linux-desktop")
		if home == "" {
			outDir = filepath.Join(daemonRoot, "tmp", "zen-desktop-elf")
		}
		if err := os.MkdirAll(outDir, 0700); err != nil {
			elfErr = err
			return
		}
		_ = os.Remove(filepath.Join(outDir, "libzen-desktop.so"))
		bin := filepath.Join(outDir, "zen")
		cmd := exec.Command("go", "build", "-tags", "zen_desktop", "-o", bin, "./cmd/zen")
		cmd.Dir = daemonRoot
		env := append(os.Environ(), "CGO_ENABLED=1")
		if token, err := NativeBuildInputToken(filepath.Join(daemonRoot, "desktop", "native")); err == nil {
			env = WithNativeBuildInput(env, token)
		}
		cmd.Env = env
		if output, err := cmd.CombinedOutput(); err != nil {
			elfErr = errString("build zen: " + err.Error() + "\n" + string(output))
			return
		}
		if _, err := os.Stat(filepath.Join(outDir, "libzen-desktop.so")); err == nil {
			elfErr = errString("build emitted libzen-desktop.so")
			return
		}
		sum, err := FileSHA256(bin)
		if err != nil {
			elfErr = err
			return
		}
		elfBin, elfSum = bin, sum
		if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
			durable := filepath.Join(home, ".zen", "artifacts", "2026-09-09-zen-linux-desktop")
			if err := os.MkdirAll(durable, 0700); err == nil {
				durableBin := filepath.Join(durable, "zen")
				if durableBin != bin {
					if data, readErr := os.ReadFile(bin); readErr == nil {
						_ = os.WriteFile(durableBin, data, 0755)
					}
				}
				_ = os.WriteFile(filepath.Join(durable, "SHA256"), []byte(sum+"\n"), 0644)
				_ = os.Remove(filepath.Join(durable, "libzen-desktop.so"))
			}
		}
	})
	if elfErr != nil {
		t.Fatal(elfErr)
	}
	return elfBin, elfSum
}

type stringError string

func (e stringError) Error() string { return string(e) }

func errString(msg string) error { return stringError(msg) }

func elfNeededNames(t *testing.T, path string) []string {
	t.Helper()
	out, err := exec.Command("readelf", "-d", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	var needed []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "(NEEDED)") {
			needed = append(needed, line)
		}
	}
	return needed
}

func assertGTKLinked(t *testing.T, bin string) {
	t.Helper()
	needed := elfNeededNames(t, bin)
	found := false
	for _, line := range needed {
		if strings.Contains(line, "libgtk-3.so") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("desktop-capable zen must DT_NEED libgtk-3.so.0\n%s", strings.Join(needed, "\n"))
	}
}

func assertSameELF(t *testing.T, pid int, bin, sum string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var exeLink string
	for time.Now().Before(deadline) {
		if target, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe"); err == nil {
			exeLink = target
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if exeLink == "" {
		t.Fatal("could not read /proc/pid/exe")
	}
	if strings.Contains(exeLink, "zen-desktop-helper") || strings.Contains(exeLink, "zen-desktop-agent") || strings.Contains(exeLink, "libzen-desktop.so") {
		t.Fatalf("auxiliary helper executable: %s", exeLink)
	}
	resolved, _ := filepath.EvalSymlinks(bin)
	clean := filepath.Clean(strings.TrimSuffix(exeLink, " (deleted)"))
	if filepath.Base(clean) != "zen" && clean != filepath.Clean(resolved) {
		t.Fatalf("pid %d exe %q is not %q", pid, exeLink, resolved)
	}
	live, err := FileSHA256(bin)
	if err != nil || live != sum {
		t.Fatalf("hash changed or unreadable: %s vs %s err=%v", live, sum, err)
	}
}

func helperEnv(t *testing.T, display string) []string {
	t.Helper()
	env := []string{"DISPLAY=" + display, "ZEN_DESKTOP_HELPER=", "GDK_BACKEND=x11"}
	for _, item := range os.Environ() {
		switch {
		case strings.HasPrefix(item, "ZEN_DESKTOP_HELPER="):
			continue
		case strings.HasPrefix(item, "DISPLAY="):
			continue
		case strings.HasPrefix(item, "WAYLAND_DISPLAY="):
			continue
		default:
			env = append(env, item)
		}
	}
	return env
}

func restoreOwnedXvfb(t *testing.T) string {
	t.Helper()
	if prefix := strings.TrimSpace(os.Getenv("ZEN_REMOTE_DESKTOP_TEST_PREFIX")); prefix != "" {
		if _, err := os.Stat(filepath.Join(prefix, "usr/bin/Xvfb")); err == nil {
			return prefix
		}
	}
	script := filepath.Join(repoRoot(t), "scripts/restore-remote-desktop-test-deps.sh")
	cmd := exec.Command(script)
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("retained archive restore: %v\n%s", err, output)
		return ""
	}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "PREFIX=") {
			return strings.TrimPrefix(line, "PREFIX=")
		}
	}
	return ""
}

func startOwnedXvfb(t *testing.T, prefix, display string) func() {
	t.Helper()
	var xvfb *exec.Cmd
	if prefix != "" {
		xvfb = exec.Command(filepath.Join(prefix, "usr/bin/Xvfb"), display, "-screen", "0", "1280x720x24", "-ac", "-nolisten", "tcp")
		lib := filepath.Join(prefix, "usr/lib") + ":" + filepath.Join(prefix, "usr/lib64")
		xvfb.Env = append(os.Environ(), "LD_LIBRARY_PATH="+lib)
	} else if path, err := exec.LookPath("Xvfb"); err == nil {
		t.Logf("using PATH Xvfb at %s", path)
		xvfb = exec.Command(path, display, "-screen", "0", "1280x720x24", "-ac", "-nolisten", "tcp")
	} else {
		t.Fatal("owned Xvfb archives were not restored and Xvfb is not on PATH")
	}
	if err := xvfb.Start(); err != nil {
		t.Fatalf("Xvfb: %v", err)
	}
	waitDisplay(t, display)
	return func() { _ = xvfb.Process.Kill(); _ = xvfb.Wait() }
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../.."))
}

func unusedDisplay() string {
	return ":" + strconv.Itoa(90+os.Getpid()%50)
}

func waitDisplay(t *testing.T, display string) {
	t.Helper()
	probeBin := filepath.Join(t.TempDir(), "xprobe")
	build := exec.Command("cc", "-x", "c", "-", "-o", probeBin, "-lX11")
	build.Stdin = strings.NewReader(`#include <X11/Xlib.h>
int main(void){Display *d=XOpenDisplay(NULL); if(!d) return 1; XCloseDisplay(d); return 0;}
`)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("display probe: %v\n%s", err, output)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		probe := exec.Command(probeBin)
		probe.Env = []string{"DISPLAY=" + display}
		if probe.Run() == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("display %s did not become ready", display)
}

func clickGrant(t *testing.T, display string) {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	clicker := filepath.Join(t.TempDir(), "test-grant-click")
	src := filepath.Join(filepath.Dir(thisFile), "native/test-grant-click.c")
	build := exec.Command("cc", "-std=c11", "-O1", src, "-o", clicker, "-lX11", "-lXtst")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("grant clicker: %v\n%s", err, output)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		cmd := exec.Command(clicker)
		cmd.Env = []string{"DISPLAY=" + display, "PATH=/usr/bin"}
		_ = cmd.Run()
		time.Sleep(150 * time.Millisecond)
	}
}

func exitStatus(err error) (int, bool) {
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), true
	}
	return 0, false
}
