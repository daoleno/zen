package server

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/desktop"
)

// Runs the real distribution ELF against an owned marker, with no helper-path
// override, shared state, personal desktop, or root broker. The phone pairs
// through the production CLI and presents real H.264 from this isolated X11.
func TestDesktopDXNativeFixture(t *testing.T) {
	output, binary := os.Getenv("ZEN_DESKTOP_DX_OUTPUT"), os.Getenv("ZEN_DESKTOP_DX_BINARY")
	if output == "" || binary == "" {
		t.Skip("explicit desktop DX native fixture required")
	}
	if !filepath.IsAbs(output) || !filepath.IsAbs(binary) || os.Geteuid() == 0 {
		t.Fatal("absolute output and ELF paths, non-root owner required")
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	tmuxDir := filepath.Join(home, "tmux")
	if err := os.Mkdir(tmuxDir, 0700); err != nil {
		t.Fatal(err)
	}
	display := ":" + strconv.Itoa(200+os.Getpid()%200)
	t.Cleanup(startE2EXvfb(t, display))
	t.Cleanup(startE2EMarker(t, display))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_, port, _ := net.SplitHostPort(addr)
	listener.Close()
	state := filepath.Join(home, ".zen")
	if err := os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	// A desktop fixture must not auto-start a real provider through Brain's
	// normal default-executor selection or trigger provider login in a browser.
	executors := "delegated_executor = \"desktop-dx-idle\"\n[[executors]]\nname = \"desktop-dx-idle\"\ncommand = \"/usr/bin/cat\"\n"
	if err := os.WriteFile(filepath.Join(state, "executors.toml"), []byte(executors), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-state-dir", state, "-addr", addr)
	cmd.Dir = home
	cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin", "SHELL=/bin/sh", "LANG=C.UTF-8", "DISPLAY=" + display,
		"GDK_BACKEND=x11", "TMPDIR=" + os.TempDir(), "TMUX_TMPDIR=" + tmuxDir,
		"ZEN_BRAIN_HOST_EXECUTOR=desktop-dx-idle", "ZEN_DELEGATED_EXECUTOR=desktop-dx-idle"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM, Setpgid: true}
	log, err := os.OpenFile(filepath.Join(output, "daemon.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		timer := time.AfterFunc(5*time.Second, func() { _ = cmd.Process.Kill() })
		_ = cmd.Wait()
		timer.Stop()
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		socket := filepath.Join(tmuxDir, fmt.Sprintf("tmux-%d", os.Getuid()), "default")
		_ = exec.Command("tmux", "-S", socket, "-N", "kill-server").Run()
	})
	waitHTTP(t, addr)
	pair := exec.Command(binary, "pair", "-state-dir", state, "http://10.0.2.2:"+port)
	pair.Env, pair.Dir = cmd.Env, home
	raw, err := pair.CombinedOutput()
	if err != nil {
		t.Fatal("owned production pairing command failed")
	}
	pairing := ""
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "zen://") {
			pairing = strings.TrimSpace(line)
			break
		}
	}
	if pairing == "" {
		t.Fatal("pairing link missing")
	}
	if err := os.WriteFile(filepath.Join(output, "pairing.txt"), []byte(pairing), 0600); err != nil {
		t.Fatal(err)
	}
	sum, err := desktop.FileSHA256(binary)
	if err != nil {
		t.Fatal(err)
	}
	ready, _ := json.Marshal(map[string]any{"binary": binary, "sha256": sum, "pid": cmd.Process.Pid, "port": port, "state": state, "display": display})
	if err := os.WriteFile(filepath.Join(output, "ready.json"), ready, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("Owned real-ELF fixture ready; no credentials printed")
	defer os.Remove(filepath.Join(output, "pairing.txt"))
	for deadline := time.Now().Add(15 * time.Minute); time.Now().Before(deadline); time.Sleep(time.Second) {
		pixel := sampleE2EPixel(t, display, e2eMarkerX, e2eMarkerY)
		data, _ := json.Marshal(map[string]any{"pixel": fmt.Sprintf("%06x", pixel), "input_received": pixel == e2eMarkerHit})
		if err := os.WriteFile(filepath.Join(output, "marker.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(output, "stop")); err == nil {
			return
		}
	}
	t.Fatal("owned fixture deadline")
}
