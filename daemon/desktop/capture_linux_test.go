package desktop

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSameELFMissingDisplayIsNotCapture(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux host capture")
	}
	bin, _ := builtDesktopELF(t)
	t.Setenv(HelperOverrideEnv, "")
	cmd := exec.Command(bin, RoleHelper, "--device", "Fixture", "--display", ":9")
	cmd.Env = []string{"PATH=/usr/bin", "HOME=" + t.TempDir(), "LANG=C.UTF-8"}
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("missing display must fail, output=%s", output)
	}
	if status, ok := exitStatus(err); !ok || status != 3 {
		t.Fatalf("missing display want exit 3, got %v output=%s", err, output)
	}
}

func TestSameELFXvfbStreamInputAndAgent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux host capture")
	}
	prefix := restoreOwnedXvfb(t)
	bin, sum := builtDesktopELF(t)
	assertGTKLinked(t, bin)
	display := unusedDisplay()
	stop := startOwnedXvfb(t, prefix, display)
	t.Cleanup(stop)

	t.Setenv(HelperOverrideEnv, "")
	helper := exec.Command(bin, RoleHelper, "--device", "OwnedFixture", "--display", display, "--control")
	helper.Env = helperEnv(t, display)
	outFile, err := os.Create(filepath.Join(t.TempDir(), "helper.stdout"))
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := helper.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var helperErr strings.Builder
	helper.Stdout = outFile
	helper.Stderr = &helperErr
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = helper.Process.Kill(); _ = helper.Wait(); _ = outFile.Close() })
	assertSameELF(t, helper.Process.Pid, bin, sum)

	requesting := readMetadataFile(t, outFile.Name())
	if requesting["state"] != "requesting" {
		t.Fatalf("first packet=%v stderr=%s", requesting, helperErr.String())
	}
	clickGrant(t, display)
	streaming := waitStreamingFile(t, outFile.Name())
	if streaming["codec"] != "h264" {
		raw, _ := os.ReadFile(outFile.Name())
		t.Fatalf("streaming=%v stderr=%s packets=%q", streaming, helperErr.String(), raw)
	}
	if !readH264File(t, outFile.Name()) {
		t.Fatal("no H.264 access unit from same-ELF helper")
	}
	if _, err := io.WriteString(stdin, "pointer 0.5 0.5 0 false 0\nbutton 0.5 0.5 1 true 0\nbutton 0.5 0.5 1 false 0\n"); err != nil {
		t.Fatal(err)
	}
	assertSameELF(t, helper.Process.Pid, bin, sum)

	agent := exec.Command(bin, RoleAgent, display, "view")
	agent.Env = []string{"PATH=/usr/bin", "HOME=/nonexistent", "LANG=C.UTF-8"}
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Process.Kill(); _ = agent.Wait() })
	assertSameELF(t, agent.Process.Pid, bin, sum)
}

func TestSameELFPairedSessionSkipsConsent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux host capture")
	}
	prefix := restoreOwnedXvfb(t)
	bin, sum := builtDesktopELF(t)
	assertGTKLinked(t, bin)
	display := unusedDisplay()
	stop := startOwnedXvfb(t, prefix, display)
	t.Cleanup(stop)

	t.Setenv(HelperOverrideEnv, "")
	helper := exec.Command(bin, RoleHelper, "--device", "PairedFixture", "--display", display, "--paired-session", "--control")
	helper.Env = helperEnv(t, display)
	outFile, err := os.Create(filepath.Join(t.TempDir(), "paired.stdout"))
	if err != nil {
		t.Fatal(err)
	}
	helper.Stdout = outFile
	var helperErr strings.Builder
	helper.Stderr = &helperErr
	if _, err := helper.StdinPipe(); err != nil {
		t.Fatal(err)
	}
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = helper.Process.Kill(); _ = helper.Wait(); _ = outFile.Close() })
	assertSameELF(t, helper.Process.Pid, bin, sum)
	first := readMetadataFile(t, outFile.Name())
	if first["state"] == "requesting" {
		t.Fatalf("paired session showed a permission dialog: %v stderr=%s", first, helperErr.String())
	}
	if first["state"] != "streaming" {
		streaming := waitStreamingFile(t, outFile.Name())
		if streaming["state"] != "streaming" {
			t.Fatalf("paired session first=%v streaming=%v stderr=%s", first, streaming, helperErr.String())
		}
	}
}

func TestSameELFMissingCodecIsNotStreaming(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux host capture")
	}
	prefix := restoreOwnedXvfb(t)
	bin, _ := builtDesktopELF(t)
	display := unusedDisplay()
	stop := startOwnedXvfb(t, prefix, display)
	t.Cleanup(stop)
	t.Setenv(HelperOverrideEnv, "")
	helper := exec.Command(bin, RoleHelper, "--device", "CodecNeg", "--display", display)
	helper.Env = append(helperEnv(t, display), "GST_PLUGIN_SYSTEM_PATH_1_0=", "GST_PLUGIN_PATH_1_0=", "GST_PLUGIN_PATH=")
	outFile, err := os.Create(filepath.Join(t.TempDir(), "codec.stdout"))
	if err != nil {
		t.Fatal(err)
	}
	helper.Stdout = outFile
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = helper.Process.Kill(); _ = helper.Wait(); _ = outFile.Close() })
	meta := readMetadataFile(t, outFile.Name())
	if meta["state"] != "requesting" {
		t.Fatalf("packet=%v", meta)
	}
	clickGrant(t, display)
	next := waitMetadataFile(t, outFile.Name(), 8*time.Second)
	if next["state"] == "streaming" {
		t.Fatalf("empty GStreamer plugin path must not stream: %v", next)
	}
}

func parsePackets(raw []byte) []struct {
	kind byte
	data []byte
} {
	var packets []struct {
		kind byte
		data []byte
	}
	for len(raw) >= 5 {
		n := binary.BigEndian.Uint32(raw[:4])
		if n == 0 || n > 4<<20 || int(n)+4 > len(raw) {
			break
		}
		payload := raw[4 : 4+n]
		packets = append(packets, struct {
			kind byte
			data []byte
		}{kind: payload[0], data: payload[1:]})
		raw = raw[4+n:]
	}
	return packets
}

func readFilePackets(t *testing.T, path string, timeout time.Duration) []struct {
	kind byte
	data []byte
} {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil && len(raw) >= 5 {
			if packets := parsePackets(raw); len(packets) > 0 {
				return packets
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for desktop packets in %s", path)
	return nil
}

func readMetadataFile(t *testing.T, path string) map[string]any {
	t.Helper()
	for _, packet := range readFilePackets(t, path, 8*time.Second) {
		if packet.kind != 1 {
			continue
		}
		var payload map[string]any
		if json.Unmarshal(packet.data, &payload) == nil {
			return payload
		}
	}
	t.Fatal("no metadata packet")
	return nil
}

func waitStreamingFile(t *testing.T, path string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(path)
		for _, packet := range parsePackets(raw) {
			if packet.kind != 1 {
				continue
			}
			var payload map[string]any
			if json.Unmarshal(packet.data, &payload) != nil {
				continue
			}
			if payload["state"] == "streaming" {
				return payload
			}
			if payload["state"] == "unsupported" || payload["state"] == "denied" {
				t.Fatalf("capture not granted: %v", payload)
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("no streaming metadata")
	return nil
}

func waitMetadataFile(t *testing.T, path string, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last map[string]any
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(path)
		for _, packet := range parsePackets(raw) {
			if packet.kind != 1 {
				continue
			}
			var payload map[string]any
			if json.Unmarshal(packet.data, &payload) == nil {
				last = payload
				if payload["state"] != "requesting" {
					return payload
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if last != nil {
		return last
	}
	return map[string]any{}
}

func readH264File(t *testing.T, path string) bool {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(path)
		for _, packet := range parsePackets(raw) {
			if packet.kind == 2 && len(packet.data) > 4 {
				return true
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}
