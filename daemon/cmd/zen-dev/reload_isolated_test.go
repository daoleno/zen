package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/desktop"
)

func TestIsolatedWatcherNativeEditRetiresChild(t *testing.T) {
	if testing.Short() {
		t.Skip("isolated zen-dev native reload builds a desktop-capable zen")
	}
	if runtime.GOOS != "linux" {
		t.Skip("zen-dev native reload is Linux desktop")
	}
	if _, _, ok := desktopNativeBuild(); !ok {
		t.Fatal("pkg-config desktop modules required for isolated watcher proof")
	}
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Fatal("rsync required to copy an owned daemon fixture")
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	src := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../.."))
	root := filepath.Join(t.TempDir(), "daemon")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("rsync", "-a", "--exclude", "tmp", "--exclude", "bin", src+"/", root+"/").CombinedOutput(); err != nil {
		t.Fatalf("copy daemon: %v\n%s", err, output)
	}
	state := filepath.Join(t.TempDir(), "state")
	home := filepath.Join(t.TempDir(), "home")
	for _, dir := range []string{
		home,
		filepath.Join(home, ".config"),
		filepath.Join(home, ".local"),
		filepath.Join(home, ".zen"),
		state,
	} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, ".zen", "executors.toml"), []byte("# isolated fixture: no providers\n"), 0600); err != nil {
		t.Fatal(err)
	}
	addr := "127.0.0.1:" + strconv.Itoa(19000+os.Getpid()%1000)
	tmuxDir := filepath.Join(t.TempDir(), "tmux")
	if err := os.MkdirAll(tmuxDir, 0700); err != nil {
		t.Fatal(err)
	}
	tmpDir := t.TempDir()
	cmd := exec.Command("go", "run", "./cmd/zen-dev", "-state-dir", state, "-addr", addr)
	cmd.Dir = root
	cmd.Env = isolatedDevEnv(t, home, state, tmuxDir, tmpDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	logFile, err := os.Create(filepath.Join(t.TempDir(), "zen-dev.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logFile.Close() })
	cmd.Stderr = logFile
	cmd.Stdout = logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			_, _ = cmd.Process.Wait()
		}
		time.Sleep(50 * time.Millisecond)
	})

	bin := filepath.Join(root, "tmp", "zen-dev")
	waitFile(t, bin, logFile.Name(), 8*time.Minute)
	waitLogContains(t, logFile.Name(), "Watching", 30*time.Second)
	if _, err := os.Stat(filepath.Join(root, "tmp", "libzen-desktop.so")); err == nil {
		t.Fatal("zen-dev must not emit libzen-desktop.so")
	}
	firstPID := exePID(t, bin)
	if firstPID == 0 {
		logRaw, _ := os.ReadFile(logFile.Name())
		t.Fatalf("isolated zen-dev child was not running\nlog=\n%s", logRaw)
	}
	assertNoProviderChildren(t, firstPID)
	firstIdent := desktopIdentity(t, bin, home)
	if !firstIdent.Native || firstIdent.NativeBuildInput == "" {
		t.Fatalf("first identity=%+v", firstIdent)
	}

	linuxC := filepath.Join(root, "desktop/native/linux.c")
	raw, err := os.ReadFile(linuxC)
	if err != nil {
		t.Fatal(err)
	}
	marker := "zen-isolated-fixture-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	stamp := "\n__attribute__((used)) const char zen_isolated_fixture_marker[] = \"" + marker + "\";\n"
	if err := os.WriteFile(linuxC, append(raw, []byte(stamp)...), 0644); err != nil {
		t.Fatal(err)
	}
	header := filepath.Join(root, "desktop/native/encoder.h")
	headerRaw, err := os.ReadFile(header)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(header, append(headerRaw, []byte("\n/* zen-isolated-header-touch-not-proof */\n")...), 0644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(8 * time.Minute)
	var nextPID int
	for time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
		if !linkedMarker(bin, marker) {
			continue
		}
		nextPID = exePID(t, bin)
		if nextPID == 0 || nextPID == firstPID || processAlive(firstPID) {
			continue
		}
		break
	}
	if !linkedMarker(bin, marker) {
		logRaw, _ := os.ReadFile(logFile.Name())
		t.Fatalf("C/H edit did not link fixture marker %q into zen-dev\nlog=\n%s", marker, logRaw)
	}
	if nextPID == 0 || nextPID == firstPID || processAlive(firstPID) {
		logRaw, _ := os.ReadFile(logFile.Name())
		t.Fatalf("old child pid %d still present; next=%d log=\n%s", firstPID, nextPID, logRaw)
	}
	ident := desktopIdentity(t, bin, home)
	if ident.SHA256 == firstIdent.SHA256 {
		t.Fatalf("desktop-identity hash unchanged after semantic C marker: %s", ident.SHA256)
	}
	if ident.NativeBuildInput == "" || ident.NativeBuildInput == firstIdent.NativeBuildInput {
		t.Fatalf("native_build_input not updated: first=%q now=%q", firstIdent.NativeBuildInput, ident.NativeBuildInput)
	}
	wantInput, err := desktop.NativeBuildInputToken(filepath.Join(root, "desktop", "native"))
	if err != nil {
		t.Fatal(err)
	}
	if ident.NativeBuildInput != "h"+wantInput {
		t.Fatalf("identity native_build_input=%q want h%s", ident.NativeBuildInput, wantInput)
	}
	t.Logf("linked fixture marker %s native_build_input %s -> %s sha %s -> %s pids %d -> %d", marker, firstIdent.NativeBuildInput, ident.NativeBuildInput, firstIdent.SHA256, ident.SHA256, firstPID, nextPID)
	role := exec.Command(bin, desktop.RoleHelper, "--device", "IsolatedFixture", "--display", ":9")
	role.Env = []string{"PATH=/usr/bin", "HOME=" + home, "LANG=C.UTF-8", "ZEN_DESKTOP_HELPER="}
	if err := role.Start(); err != nil {
		t.Fatal(err)
	}
	assertSameLiveELF(t, role.Process.Pid, bin)
	_ = role.Process.Kill()
	_ = role.Wait()

	broken := append([]byte("\n#error zen-isolated-compile-failure\n"), stamp...)
	if err := os.WriteFile(linuxC, append(raw, broken...), 0644); err != nil {
		t.Fatal(err)
	}
	waitLogContains(t, logFile.Name(), "go build failed", 3*time.Minute)
	if !linkedMarker(bin, marker) {
		t.Fatal("failed compile replaced last-good ELF")
	}
	sum, err := desktop.FileSHA256(bin)
	if err != nil || sum != ident.SHA256 {
		t.Fatalf("last-good hash drifted after failed build: %s vs %s err=%v", sum, ident.SHA256, err)
	}
	live := exePID(t, bin)
	if live == 0 || processAlive(firstPID) {
		t.Fatalf("failed build must keep the rebuilt child running; live=%d first=%d", live, firstPID)
	}
	assertNoProviderChildren(t, live)
}

func isolatedDevEnv(t *testing.T, home, state, tmuxDir, tmpDir string) []string {
	t.Helper()
	env := []string{
		"PATH=" + isolatedBuildPath(t),
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local"),
		"XDG_CACHE_HOME=" + filepath.Join(tmpDir, "xdg-cache"),
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
		"ZEN_DESKTOP_HELPER=",
		"ZEN_STATE_DIR=" + state,
		"TMPDIR=" + tmpDir,
		"TMP=" + tmpDir,
		"TEMP=" + tmpDir,
		"TMUX_TMPDIR=" + tmuxDir,
		"CGO_ENABLED=1",
		"GIT_TERMINAL_PROMPT=0",
	}
	for _, key := range []string{
		"GOROOT", "GOPROXY", "GOSUMDB",
		"PKG_CONFIG_PATH", "PKG_CONFIG_SYSROOT_DIR",
	} {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = filepath.Join(tmpDir, "gopath")
		if err := os.MkdirAll(gopath, 0700); err != nil {
			t.Fatal(err)
		}
	}
	env = append(env, "GOPATH="+gopath)
	gocache := os.Getenv("GOCACHE")
	if gocache == "" {
		if realHome, err := os.UserHomeDir(); err == nil {
			gocache = filepath.Join(realHome, ".cache", "go-build")
		}
	}
	if gocache != "" {
		env = append(env, "GOCACHE="+gocache)
	}
	gomod := os.Getenv("GOMODCACHE")
	if gomod == "" {
		if realHome, err := os.UserHomeDir(); err == nil {
			gomod = filepath.Join(realHome, "go", "pkg", "mod")
		}
	}
	if gomod != "" {
		env = append(env, "GOMODCACHE="+gomod)
	}
	return env
}

func isolatedBuildPath(t *testing.T) string {
	t.Helper()
	seen := map[string]bool{}
	var dirs []string
	add := func(dir string) {
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	add("/usr/bin")
	add("/bin")
	add("/usr/lib/go/bin")
	for _, name := range []string{"go", "gcc", "cc", "pkg-config", "rsync", "strings", "git", "make", "tmux"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		add(filepath.Dir(path))
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}

func desktopIdentity(t *testing.T, bin, home string) desktop.Identity {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, desktop.RoleIdentity)
	cmd.Env = []string{"PATH=/usr/bin", "HOME=" + home, "LANG=C.UTF-8", "ZEN_DESKTOP_HELPER=", "DISPLAY="}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("desktop-identity: %v\n%s", err, out)
	}
	var id desktop.Identity
	if err := json.Unmarshal(out, &id); err != nil {
		t.Fatalf("identity json: %v payload=%s", err, out)
	}
	return id
}

func linkedMarker(path, marker string) bool {
	raw, err := exec.Command("strings", "-a", path).Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), marker)
}

func assertSameLiveELF(t *testing.T, pid int, bin string) {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(bin)
	if err != nil {
		resolved = bin
	}
	target, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	if err != nil {
		t.Fatalf("role /proc/%d/exe: %v", pid, err)
	}
	clean := filepath.Clean(strings.TrimSuffix(target, " (deleted)"))
	if strings.Contains(clean, "zen-desktop-helper") || strings.Contains(clean, "libzen-desktop.so") {
		t.Fatalf("auxiliary helper executable: %s", target)
	}
	if filepath.Base(clean) != "zen-dev" && clean != filepath.Clean(resolved) {
		t.Fatalf("pid %d exe %q is not %q", pid, target, resolved)
	}
}

func assertNoProviderChildren(t *testing.T, rootPID int) {
	t.Helper()
	seen := map[int]struct{}{rootPID: {}}
	queue := []int{rootPID}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
		if err == nil && len(raw) > 0 {
			args := strings.Split(string(raw), "\x00")
			base := filepath.Base(args[0])
			switch base {
			case "codex", "cursor-agent", "opencode", "claude", "pi":
				t.Fatalf("isolated zen-dev started provider %q pid=%d argv=%q", base, pid, args)
			}
		}
		tasks, err := os.ReadDir("/proc/" + strconv.Itoa(pid) + "/task")
		if err != nil {
			continue
		}
		for _, task := range tasks {
			children, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "task", task.Name(), "children"))
			if err != nil {
				continue
			}
			for _, field := range strings.Fields(string(children)) {
				child, err := strconv.Atoi(field)
				if err != nil {
					continue
				}
				if _, ok := seen[child]; ok {
					continue
				}
				seen[child] = struct{}{}
				queue = append(queue, child)
			}
		}
	}
}

func waitFile(t *testing.T, path, logPath string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	raw, _ := os.ReadFile(logPath)
	t.Fatalf("missing %s\nlog=\n%s", path, raw)
}

func waitLogContains(t *testing.T, path, needle string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(path)
		if strings.Contains(string(raw), needle) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	raw, _ := os.ReadFile(path)
	t.Fatalf("zen-dev log missing %q:\n%s", needle, raw)
}

func exePID(t *testing.T, bin string) int {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(bin)
	if err != nil {
		resolved = bin
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		target, err := os.Readlink(filepath.Join("/proc", entry.Name(), "exe"))
		if err != nil {
			continue
		}
		target = strings.TrimSuffix(target, " (deleted)")
		if filepath.Clean(target) != filepath.Clean(resolved) {
			continue
		}
		if cmd, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline")); err == nil && strings.Contains(string(cmd), "-state-dir") {
			return pid
		}
	}
	return 0
}

func processAlive(pid int) bool {
	_, err := os.Stat("/proc/" + strconv.Itoa(pid))
	return err == nil
}
