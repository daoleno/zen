package host

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"time"
)

type busValue struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type LinuxObservation struct {
	Session    Session
	Path       string
	Class      string
	Display    string
	LockedHint bool
}

func busScalar[T any](properties map[string]busValue, name, signature string) (T, error) {
	var result T
	v, ok := properties[name]
	if !ok || v.Type != signature || bytes.Equal(bytes.TrimSpace(v.Data), []byte("null")) || json.Unmarshal(v.Data, &result) != nil {
		return result, errors.New("invalid_logind_property")
	}
	return result, nil
}

func tuple(v busValue, signature string) ([]json.RawMessage, error) {
	var values []json.RawMessage
	if v.Type != signature || json.Unmarshal(v.Data, &values) != nil || len(values) != 2 {
		return nil, errors.New("invalid_logind_tuple")
	}
	return values, nil
}

// DecodeLinuxObservation consumes the typed Properties.GetAll reply. It never
// treats metadata discovery as capture/input/lock-screen permission evidence.
func DecodeLinuxObservation(data []byte, bootID string) (LinuxObservation, error) {
	var result LinuxObservation
	var reply struct {
		Type string                `json:"type"`
		Data []map[string]busValue `json:"data"`
	}
	if len(data) > 65536 || bootID == "" || json.Unmarshal(data, &reply) != nil || reply.Type != "a{sv}" || len(reply.Data) != 1 {
		return result, errors.New("invalid_logind_reply")
	}
	p := reply.Data[0]
	id, err := busScalar[string](p, "Id", "s")
	if err != nil || id == "" {
		return result, errors.New("invalid_logind_session")
	}
	class, err := busScalar[string](p, "Class", "s")
	if err != nil || (class != "user" && class != "greeter") {
		return result, errors.New("unsupported_session_class")
	}
	backend, err := busScalar[string](p, "Type", "s")
	if err != nil || (backend != "x11" && backend != "wayland") {
		return result, errors.New("unsupported_session_type")
	}
	active, err := busScalar[bool](p, "Active", "b")
	if err != nil {
		return result, err
	}
	state, err := busScalar[string](p, "State", "s")
	if err != nil || state != "active" || !active {
		return result, errors.New("inactive_session")
	}
	locked, err := busScalar[bool](p, "LockedHint", "b")
	if err != nil {
		return result, err
	}
	display, err := busScalar[string](p, "Display", "s")
	if err != nil {
		return result, err
	}
	seat, err := tuple(p["Seat"], "(so)")
	if err != nil {
		return result, err
	}
	user, err := tuple(p["User"], "(uo)")
	if err != nil {
		return result, err
	}
	var seatID string
	var uid uint32
	if json.Unmarshal(seat[0], &seatID) != nil || seatID != "seat0" || json.Unmarshal(user[0], &uid) != nil || uid == 0 {
		return result, errors.New("invalid_session_owner")
	}
	surface := Desktop
	if locked {
		surface = Locked
	}
	if class == "greeter" {
		surface = Greeter
	}
	result = LinuxObservation{Session: Session{BootID: bootID, ID: id, Seat: seatID, UID: uid, Backend: backend, Surface: surface, Active: true}, Class: class, Display: display, LockedHint: locked}
	return result, nil
}

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 65536 {
		return 0, errors.New("logind_reply_too_large")
	}
	return b.Buffer.Write(p)
}

func readBus(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/busctl", append([]string{"--system", "--json=short"}, args...)...)
	var output boundedOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		return nil, errors.New("logind_unavailable")
	}
	return output.Bytes(), nil
}

type activeSession struct{ ID, Path string }

var sessionPath = regexp.MustCompile(`^/org/freedesktop/login1/session/[a-zA-Z0-9_]+$`)

func readActive(ctx context.Context) (activeSession, error) {
	var result activeSession
	data, err := readBus(ctx, "get-property", "org.freedesktop.login1", "/org/freedesktop/login1/seat/seat0", "org.freedesktop.login1.Seat", "ActiveSession")
	if err != nil {
		return result, err
	}
	var v busValue
	if json.Unmarshal(data, &v) != nil {
		return result, errors.New("invalid_logind_reply")
	}
	values, err := tuple(v, "(so)")
	if err != nil {
		return result, err
	}
	if json.Unmarshal(values[0], &result.ID) != nil || result.ID == "" || json.Unmarshal(values[1], &result.Path) != nil || !sessionPath.MatchString(result.Path) {
		return activeSession{}, errors.New("no_active_session")
	}
	return result, nil
}

// InspectLinux is read-only preflight, not a session subscription or an atomic
// authorization decision. No X/Wayland connection, cookie read or input occurs.
func InspectLinux(ctx context.Context) (LinuxObservation, error) {
	var empty LinuxObservation
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return empty, errors.New("boot_identity_unavailable")
	}
	before, err := readActive(ctx)
	if err != nil {
		return empty, err
	}
	data, err := readBus(ctx, "call", "org.freedesktop.login1", before.Path, "org.freedesktop.DBus.Properties", "GetAll", "s", "org.freedesktop.login1.Session")
	if err != nil {
		return empty, err
	}
	result, err := DecodeLinuxObservation(data, string(bytes.TrimSpace(boot)))
	if err != nil {
		return empty, err
	}
	after, err := readActive(ctx)
	if err != nil || before != after || result.Session.ID != after.ID {
		return empty, errors.New("session_changed")
	}
	result.Path = before.Path
	return result, nil
}
