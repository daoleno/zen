package server

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/desktop/host"
	"github.com/daoleno/zen/daemon/link"
	"github.com/gorilla/websocket"
)

// Isolated-host proof: identity TLS to a LAN IP with SNI zen-desktop.invalid,
// live InspectReadiness (no stub), owned Xvfb capture/input, reconnect, re-pair,
// revoke, wrong pin, and forged readiness headers. Never opens the personal :0.
func TestDesktopIdentityPinnedLANSessionE2E(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux isolated host")
	}
	if exec.Command("pkg-config", "--exists", "gtk+-3.0", "gstreamer-app-1.0", "x11", "xtst").Run() != nil {
		t.Skip("desktop pkg-config modules required")
	}
	inspectHostReadiness = host.InspectReadiness
	t.Cleanup(func() { inspectHostReadiness = host.InspectReadiness })

	display := ":" + strconv.Itoa(90+os.Getpid()%50)
	stopXvfb := startE2EXvfb(t, display)
	t.Cleanup(stopXvfb)
	elf := e2eDesktopELF(t)
	wrapper := filepath.Join(t.TempDir(), "helper")
	argsPath := filepath.Join(t.TempDir(), "helper.args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + argsPath + "'\nexec '" + elf + "' desktop-helper \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZEN_DESKTOP_HELPER", wrapper)
	t.Setenv("ZEN_DESKTOP_BACKEND", "x11")
	t.Setenv("ZEN_DESKTOP_DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", display)
	stopMarker := startE2EMarker(t, display)
	t.Cleanup(stopMarker)

	manager, key, id := sessionFileAuthFixture(t)
	s := New(manager, nil, nil, nil, nil, nil, nil)
	defer s.shutdownAuthenticatedClients()
	identity, err := link.LoadOrCreateTransportIdentity(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetDesktopTransport(DesktopTransport{TLSConfig: identity.ServerTLSConfig(), Pin: identity.SPKISHA256})

	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener := &tlsHTTPListener{Listener: tcp, config: identity.ServerTLSConfig().Clone()}
	srv := &http.Server{Handler: s.Handler()}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(listener) }()
	t.Cleanup(func() {
		_ = srv.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("identity listener did not stop")
		}
	})
	addr := listener.Addr().String()
	waitHTTP(t, addr)

	pinned, err := link.PinnedClientTLSConfig(link.DesktopIdentityServerName, identity.SPKISHA256)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: pinned}}
	dialer := websocket.Dialer{TLSClientConfig: pinned, HandshakeTimeout: 8 * time.Second}

	t.Run("wrong_pin", func(t *testing.T) {
		wrong, err := link.PinnedClientTLSConfig(link.DesktopIdentityServerName, strings.Repeat("ab", 32))
		if err != nil {
			t.Fatal(err)
		}
		bad := &http.Client{Transport: &http.Transport{TLSClientConfig: wrong, DisableKeepAlives: true}}
		_, err = bad.Get("https://" + addr + "/health")
		if err == nil {
			t.Fatal("wrong pin was accepted")
		}
	})

	t.Run("identity_sni_to_lan_ip", func(t *testing.T) {
		if pinned.ServerName != link.DesktopIdentityServerName {
			t.Fatalf("client SNI=%q", pinned.ServerName)
		}
		resp, err := client.Get("https://" + addr + "/health")
		if err != nil {
			t.Fatalf("identity SNI to 127.0.0.1 failed: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("identity TLS health %d", resp.StatusCode)
		}
	})

	t.Run("forged_readiness_headers_plaintext", func(t *testing.T) {
		header := http.Header{
			"Authorization":         {desktopAuthorization(t, key, manager.DaemonID(), id, "zen-desktop")},
			"X-Forwarded-Proto":     {"https"},
			"X-Zen-Desktop-Host":    {"ready"},
			"X-Zen-Host-Broker":     {"true"},
			"X-Zen-Current-Session": {"true"},
		}
		_, response, err := websocket.DefaultDialer.Dial("ws://"+addr+"/desktop", header)
		if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
			t.Fatalf("plaintext forged readiness: %v %v", response, err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if strings.TrimSpace(string(body)) != "desktop_tls_required" {
			t.Fatalf("plaintext body=%q", body)
		}
	})

	publicKey := hex.EncodeToString(key.Public().(ed25519.PublicKey))
	t.Run("legacy_then_repair", func(t *testing.T) {
		t.Setenv("DISPLAY", display)
		header := http.Header{"Authorization": {desktopAuthorization(t, key, manager.DaemonID(), id, "zen-desktop")}}
		_, response, err := dialer.Dial("wss://"+addr+"/desktop", header)
		if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
			t.Fatalf("legacy: %v %v", response, err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if strings.TrimSpace(string(body)) != "desktop_scope_required" {
			t.Fatalf("legacy body=%q", body)
		}
		token, err := manager.IssuePairingToken(time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := manager.EnrollDeviceWithDesktopScope(token.Value, manager.DaemonID(), manager.PublicKeyHex(), id, "Fixture", publicKey, 1,
			hex.EncodeToString(ed25519.Sign(key, auth.BuildPairingScopePayload(manager.PublicKeyHex(), token.Value, id, publicKey)))); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("forged_readiness_headers_tls", func(t *testing.T) {
		t.Setenv("DISPLAY", "")
		t.Setenv("WAYLAND_DISPLAY", "")
		t.Setenv("ZEN_DESKTOP_DISPLAY", "")
		if host.InspectReadiness().Broker {
			t.Skip("a host broker socket exists; empty-session fail-closed cannot be proven here")
		}
		if host.InspectReadiness().CurrentSession {
			t.Fatal("cleared session env still reported a current session")
		}
		header := http.Header{
			"Authorization":         {desktopAuthorization(t, key, manager.DaemonID(), id, "zen-desktop")},
			"X-Forwarded-Proto":     {"https"},
			"X-Zen-Desktop-Host":    {"ready"},
			"X-Zen-Host-Broker":     {"true"},
			"X-Zen-Current-Session": {"true"},
		}
		_, response, err := dialer.Dial("wss://"+addr+"/desktop", header)
		if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
			t.Fatalf("tls forged readiness: %v %v", response, err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if strings.TrimSpace(string(body)) != "host_setup_required" {
			t.Fatalf("tls forged body=%q", body)
		}
	})

	t.Run("pinned_session_capture_input_reconnect", func(t *testing.T) {
		t.Setenv("DISPLAY", display)
		if !host.InspectReadiness().CurrentSession {
			t.Fatal("owned Xvfb was not visible to InspectReadiness")
		}
		if host.InspectReadiness().Broker {
			t.Skip("a host broker socket exists; session-only capture is not isolated on this machine")
		}
		req, err := http.NewRequest(http.MethodGet, "https://"+addr+"/desktop/capability", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", desktopAuthorization(t, key, manager.DaemonID(), id, auth.DesktopCapabilityPurpose))
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&payload) != nil {
			response.Body.Close()
			t.Fatalf("capability HTTP %d", response.StatusCode)
		}
		response.Body.Close()
		hostStatus, _ := payload["host"].(map[string]any)
		connect, _ := payload["connect"].(map[string]any)
		if hostStatus["current_session"] != true || hostStatus["lock_login"] != false || connect["unattended"] != true {
			t.Fatalf("capability host=%v connect=%v", hostStatus, connect)
		}
		header := func() http.Header {
			return http.Header{"Authorization": {desktopAuthorization(t, key, manager.DaemonID(), id, "zen-desktop")}}
		}
		before := sampleE2EPixel(t, display, e2eMarkerX, e2eMarkerY)
		if before != e2eMarkerIdle {
			t.Fatalf("owned marker idle pixel=%06x want %06x", before, e2eMarkerIdle)
		}
		conn := openStreamingDesktop(t, &dialer, addr, header)
		args, err := os.ReadFile(argsPath)
		if err != nil || !strings.Contains(string(args), "--paired-session") || !strings.Contains(string(args), "--control") || !strings.Contains(string(args), "--display") {
			t.Fatalf("paired helper args=%q err=%v", args, err)
		}
		x := float64(e2eMarkerX) / 1280
		y := float64(e2eMarkerY) / 720
		if err := conn.WriteJSON(map[string]any{"type": "pointer", "x": x, "y": y}); err != nil {
			t.Fatal(err)
		}
		if err := conn.WriteJSON(map[string]any{"type": "button", "code": 1, "down": true, "x": x, "y": y}); err != nil {
			t.Fatal(err)
		}
		if err := conn.WriteJSON(map[string]any{"type": "button", "code": 1, "down": false, "x": x, "y": y}); err != nil {
			t.Fatal(err)
		}
		media := readAnnexBH264(t, conn)
		if len(media) < 64 {
			t.Fatalf("H.264 access unit too small: %d", len(media))
		}
		t.Logf("identity-pinned session H.264 bytes=%d marker %06x -> waiting, elf native", len(media), before)
		deadline := time.Now().Add(3 * time.Second)
		after := before
		for time.Now().Before(deadline) {
			after = sampleE2EPixel(t, display, e2eMarkerX, e2eMarkerY)
			if after == e2eMarkerHit {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if after != e2eMarkerHit {
			t.Fatalf("click did not change owned marker pixels idle=%06x after=%06x want %06x media=%d", before, after, e2eMarkerHit, len(media))
		}
		_ = conn.Close()
		reconnect := openStreamingDesktop(t, &dialer, addr, header)
		_ = reconnect.Close()
	})

	t.Run("revoke", func(t *testing.T) {
		t.Setenv("DISPLAY", display)
		conn := openStreamingDesktop(t, &dialer, addr, func() http.Header {
			return http.Header{"Authorization": {desktopAuthorization(t, key, manager.DaemonID(), id, "zen-desktop")}}
		})
		if _, err := manager.RevokeDevice(id); err != nil {
			t.Fatal(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		if _, _, err := conn.ReadMessage(); err == nil {
			t.Fatal("revoked session kept media")
		}
		_, response, err := dialer.Dial("wss://"+addr+"/desktop", http.Header{
			"Authorization": {desktopAuthorization(t, key, manager.DaemonID(), id, "zen-desktop")},
		})
		if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("revoked reconnect: %v %v", response, err)
		}
	})
}

func openStreamingDesktop(t *testing.T, dialer *websocket.Dialer, addr string, header func() http.Header) *websocket.Conn {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		conn, response, err := dialer.Dial("wss://"+addr+"/desktop", header())
		if err != nil {
			last = err
			if response != nil && response.StatusCode == http.StatusForbidden {
				body, _ := io.ReadAll(response.Body)
				response.Body.Close()
				t.Fatalf("pinned desktop dial forbidden: %s", body)
			}
			if !strings.Contains(err.Error(), "desktop_busy") {
				t.Fatalf("pinned desktop dial: %v %v", response, err)
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
		kind, data, err := conn.ReadMessage()
		if err == nil && kind == websocket.TextMessage {
			var status map[string]any
			if json.Unmarshal(data, &status) == nil {
				switch status["state"] {
				case "streaming":
					if status["codec"] != "h264" {
						_ = conn.Close()
						t.Fatalf("streaming without h264: %v", status)
					}
					t.Cleanup(func() { _ = conn.Close() })
					return conn
				case "sources", "requesting":
					_ = conn.Close()
					t.Fatalf("current session asked for extra setup: %v", status)
				}
			}
		}
		last = err
		_ = conn.Close()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("waiting for streaming: %v", last)
	return nil
}

const (
	e2eMarkerIdle uint32 = 0xcc3366
	e2eMarkerHit  uint32 = 0x22ddaa
	e2eMarkerX           = 150
	e2eMarkerY           = 600
)

func readAnnexBH264(t *testing.T, conn *websocket.Conn) []byte {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	for i := 0; i < 24; i++ {
		kind, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("waiting for H.264: %v", err)
		}
		if kind != websocket.BinaryMessage || !annexB(data) {
			continue
		}
		return data
	}
	t.Fatal("pinned session produced no Annex-B H.264")
	return nil
}

func annexB(data []byte) bool {
	if len(data) < 5 {
		return false
	}
	if data[0] == 0 && data[1] == 0 && data[2] == 0 && data[3] == 1 {
		return true
	}
	return data[0] == 0 && data[1] == 0 && data[2] == 1
}

func compileE2EC(t *testing.T, out, source string) {
	t.Helper()
	cmd := exec.Command("cc", "-x", "c", "-", "-o", out, "-lX11")
	cmd.Stdin = strings.NewReader(source)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cc %s: %v\n%s", out, err, output)
	}
}

func startE2EMarker(t *testing.T, display string) func() {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	sampler := filepath.Join(dir, "sampler")
	compileE2EC(t, marker, `#include <X11/Xlib.h>
int main(void) {
  Display *d = XOpenDisplay(NULL);
  if (!d) return 1;
  int s = DefaultScreen(d);
  Window root = RootWindow(d, s);
  XSetWindowBackground(d, root, 0x00112233);
  XClearWindow(d, root);
  Window w = XCreateSimpleWindow(d, root, 50, 500, 200, 200, 0, 0, 0x00cc3366);
  XSelectInput(d, w, ButtonPressMask);
  XMapRaised(d, w);
  XFlush(d);
  for (;;) {
    XEvent e;
    XNextEvent(d, &e);
    if (e.type == ButtonPress) {
      XSetWindowBackground(d, w, 0x0022ddaa);
      XClearWindow(d, w);
      XFlush(d);
    }
  }
}
`)
	compileE2EC(t, sampler, `#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <stdio.h>
#include <stdlib.h>
int main(int argc, char **argv) {
  if (argc != 3) return 2;
  Display *d = XOpenDisplay(NULL);
  if (!d) return 1;
  int x = atoi(argv[1]), y = atoi(argv[2]);
  XImage *img = XGetImage(d, RootWindow(d, DefaultScreen(d)), x, y, 1, 1, AllPlanes, ZPixmap);
  if (!img) return 3;
  unsigned long p = XGetPixel(img, 0, 0);
  printf("%06lx\n", p & 0xffffff);
  XDestroyImage(img);
  XCloseDisplay(d);
  return 0;
}
`)
	t.Setenv("ZEN_E2E_PIXEL_BIN", sampler)
	cmd := exec.Command(marker)
	cmd.Env = []string{"DISPLAY=" + display}
	if err := cmd.Start(); err != nil {
		t.Fatalf("marker: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if sampleE2EPixel(t, display, e2eMarkerX, e2eMarkerY) == e2eMarkerIdle {
			return func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }
		}
		time.Sleep(30 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	t.Fatalf("owned marker did not map on %s", display)
	return func() {}
}

func sampleE2EPixel(t *testing.T, display string, x, y int) uint32 {
	t.Helper()
	bin := strings.TrimSpace(os.Getenv("ZEN_E2E_PIXEL_BIN"))
	if bin == "" {
		t.Fatal("pixel sampler missing")
	}
	cmd := exec.Command(bin, strconv.Itoa(x), strconv.Itoa(y))
	cmd.Env = []string{"DISPLAY=" + display}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sample %d,%d: %v\n%s", x, y, err, output)
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(output)), 16, 32)
	if err != nil {
		t.Fatalf("pixel %q: %v", output, err)
	}
	return uint32(value)
}

func waitHTTP(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("HTTP health on identity port failed")
}

func e2eDesktopELF(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	daemonRoot := filepath.Join(filepath.Dir(thisFile), "..")
	home := strings.TrimSpace(os.Getenv("HOME"))
	var candidates []string
	if home != "" {
		candidates = append(candidates, filepath.Join(home, ".zen", "artifacts", "2026-09-09-zen-linux-desktop", "zen"))
	}
	for _, bin := range candidates {
		if e2eNativeDesktop(bin) {
			return bin
		}
	}
	out := filepath.Join(t.TempDir(), "zen")
	cmd := exec.Command("go", "build", "-tags", "zen_desktop", "-o", out, "./cmd/zen")
	cmd.Dir = daemonRoot
	cmd.Env = append(stripTestCGO(os.Environ()), "CGO_ENABLED=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build zen: %v\n%s", err, output)
	}
	if !e2eNativeDesktop(out) {
		t.Fatal("built zen is missing native desktop roles")
	}
	return out
}

func stripTestCGO(env []string) []string {
	out := make([]string, 0, len(env))
	for _, item := range env {
		if !strings.HasPrefix(item, "CGO_ENABLED=") {
			out = append(out, item)
		}
	}
	return out
}

func e2eNativeDesktop(bin string) bool {
	info, err := os.Stat(bin)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return false
	}
	cmd := exec.Command(bin, "desktop-identity")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent"}
	output, err := cmd.CombinedOutput()
	return err == nil && strings.Contains(string(output), `"native": true`)
}

func startE2EXvfb(t *testing.T, display string) func() {
	t.Helper()
	prefix := strings.TrimSpace(os.Getenv("ZEN_REMOTE_DESKTOP_TEST_PREFIX"))
	if prefix == "" || func() bool { _, err := os.Stat(filepath.Join(prefix, "usr/bin/Xvfb")); return err != nil }() {
		prefix = restoreE2EXvfbPrefix(t)
	}
	var xvfb *exec.Cmd
	if prefix != "" {
		bin := filepath.Join(prefix, "usr/bin/Xvfb")
		if _, err := os.Stat(bin); err != nil {
			t.Fatal("owned Xvfb archives were not restored")
		}
		xvfb = exec.Command(bin, display, "-screen", "0", "1280x720x24", "-ac", "-nolisten", "tcp")
		lib := filepath.Join(prefix, "usr/lib") + ":" + filepath.Join(prefix, "usr/lib64")
		xvfb.Env = append(os.Environ(), "LD_LIBRARY_PATH="+lib)
	} else if path, err := exec.LookPath("Xvfb"); err == nil {
		xvfb = exec.Command(path, display, "-screen", "0", "1280x720x24", "-ac", "-nolisten", "tcp")
	} else {
		t.Fatal("owned Xvfb archives were not restored and Xvfb is not on PATH")
	}
	if err := xvfb.Start(); err != nil {
		t.Fatalf("Xvfb: %v", err)
	}
	probeBin := filepath.Join(t.TempDir(), "xprobe")
	build := exec.Command("cc", "-x", "c", "-", "-o", probeBin, "-lX11")
	build.Stdin = strings.NewReader(`#include <X11/Xlib.h>
int main(void){Display *d=XOpenDisplay(NULL); if(!d) return 1; XCloseDisplay(d); return 0;}
`)
	if output, err := build.CombinedOutput(); err != nil {
		_ = xvfb.Process.Kill()
		t.Fatalf("display probe: %v\n%s", err, output)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		probe := exec.Command(probeBin)
		probe.Env = []string{"DISPLAY=" + display}
		if probe.Run() == nil {
			return func() { _ = xvfb.Process.Kill(); _ = xvfb.Wait() }
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = xvfb.Process.Kill()
	t.Fatalf("display %s did not become ready", display)
	return func() {}
}

// Mirrors daemon/desktop restoreOwnedXvfb: a missing developer archive must not
// fail CI. The daemon CI prerequisites install system Xvfb, which is used when
// the retained archive is unavailable. Isolated display and temp paths are
// unchanged; only the archive requirement is relaxed.
func restoreE2EXvfbPrefix(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	script := filepath.Join(filepath.Dir(thisFile), "../..", "scripts/restore-remote-desktop-test-deps.sh")
	cmd := exec.Command(script)
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("owned Xvfb restore unavailable, falling back to system Xvfb: %v\n%s", err, output)
		return ""
	}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "PREFIX=") {
			return strings.TrimPrefix(line, "PREFIX=")
		}
	}
	return ""
}
