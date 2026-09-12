package host

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/daoleno/zen/daemon/desktop"
)

type probeResult struct {
	OK         bool    `json:"ok"`
	Error      string  `json:"error,omitempty"`
	Session    string  `json:"session,omitempty"`
	Surface    Surface `json:"surface,omitempty"`
	Width      int     `json:"width,omitempty"`
	Height     int     `json:"height,omitempty"`
	FrameBytes int     `json:"frameBytes,omitempty"`
}

var errDesktopProbeBusy = errors.New("desktop_busy_existing_connection_preserved")

func sameAuthority(a, b *os.File) bool {
	if a == nil || b == nil {
		return false
	}
	left, e1 := a.Stat()
	right, e2 := b.Stat()
	return e1 == nil && e2 == nil && os.SameFile(left, right) && left.Size() == right.Size() && left.ModTime() == right.ModTime()
}

// This root-only setup probe cannot send input, take over an active owner, or
// publish pixels. It discards the first H.264 access unit after the existing
// UID-dropped agent has opened X11, checked XTest and initialized its encoder.
func (b *broker) probeCurrentLocked(display string) probeResult {
	fail := func(code string) probeResult { return probeResult{Error: code} }
	if b.owner != nil || b.process != nil {
		return fail(errDesktopProbeBusy.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if display != b.display || b.observeLocked(ctx) != nil {
		return fail("registered_session_unavailable")
	}
	before, err := InspectLinux(ctx)
	if err != nil {
		return fail("registered_session_unavailable")
	}
	channel, cmd, err := b.startAgentLocked(false)
	if err != nil {
		return fail("view_probe_agent_failed")
	}
	timer := time.AfterFunc(6*time.Second, func() { _ = channel.Close() })
	defer func() {
		timer.Stop()
		_ = channel.Close()
		_ = cmd.Process.Signal(syscall.SIGTERM)
		kill := time.AfterFunc(time.Second, func() { _ = cmd.Process.Kill() })
		_ = cmd.Wait()
		kill.Stop()
	}()
	width, height, size, err := readProbeFrame(channel)
	if err != nil {
		return fail("capture_or_encoder_probe_failed")
	}
	after, err := InspectLinux(ctx)
	if err != nil || before != after {
		return fail("session_changed_during_probe")
	}
	return probeResult{OK: true, Session: before.Session.ID, Surface: before.Session.Surface, Width: width, Height: height, FrameBytes: size}
}

func readProbeFrame(reader io.Reader) (width, height, size int, err error) {
	kind, data, err := desktop.ReadPacket(reader)
	if err != nil || kind != 1 {
		return 0, 0, 0, errors.New("probe_metadata_missing")
	}
	var meta struct {
		State   string
		Codec   string
		Width   int
		Height  int
		Control bool
	}
	if json.Unmarshal(data, &meta) != nil || meta.State != "streaming" || meta.Codec != "h264" || meta.Control || meta.Width < 2 || meta.Width > 4096 || meta.Height < 2 || meta.Height > 4096 {
		return 0, 0, 0, errors.New("probe_metadata_invalid")
	}
	kind, data, err = desktop.ReadPacket(reader)
	if err != nil || kind != 2 || len(data) < 5 || data[0] != 0 || data[1] != 0 || (data[2] != 1 && !(data[2] == 0 && data[3] == 1)) {
		return 0, 0, 0, errors.New("probe_frame_missing")
	}
	return meta.Width, meta.Height, len(data), nil
}

func probeRegisteredDisplay(display string) (probeResult, error) {
	file, err := os.Open("/dev/null")
	if err != nil {
		return probeResult{}, err
	}
	defer file.Close()
	return registerDisplayFile("probe", display, file)
}
