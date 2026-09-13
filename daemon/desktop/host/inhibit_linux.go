package host

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
)

// The unattended desktop authorization is the canonical per-device
// desktop_scope_version=1 grant on the trusted-device record. While that
// authorization is active the desktop must stay reachable without physical
// access: idle lock and suspend must not end the session that the paired phone
// controls. Zen holds exactly the supported, scoped inhibitors for that
// lifecycle and releases them on revoke/stop:
//
//   - logind "idle" (block): the session never reaches the idle action.
//   - logind "sleep" (block): the system is not suspended while authorized.
//   - KDE org.freedesktop.ScreenSaver.Inhibit on the owner session bus: the
//     compositor's own automatic lock does not lock the authorized session.
//
// This never fakes input, never disables lock policy, never weakens PAM and
// never stores an OS password. A manual lock (the lock key, loginctl
// lock-session) still works and still ends remote access exactly as documented.
// The KDE screen-saver API is the same supported interface video players and
// browsers use; the logind idle/sleep inhibitor is the same supported interface
// power management already honors.

const (
	// InhibitorWho is the fixed logind/ScreenSaver application name used to
	// recognize Zen's own scoped inhibitor in status output.
	InhibitorWho = "Zen Remote Desktop"
	// InhibitorReason is the fixed human-readable reason carried on every leg.
	InhibitorReason = "Unattended remote desktop authorization is active"
)

// Leg names are stable identifiers in the status contract.
const (
	legLogindIdle  = "logind_idle"
	legLogindSleep = "logind_sleep"
	legScreenSaver = "screen_saver"
)

const (
	legStateActive      = "active"
	legStateReleased    = "released"
	legStateUnavailable = "unavailable"
)

var inhibitorLegOrder = []string{legLogindIdle, legLogindSleep, legScreenSaver}

// InhibitorStatus is the truthful, per-leg state of the scoped authorization
// inhibitor. Active is true only when every supported leg is held.
type InhibitorStatus struct {
	Active           bool   `json:"active"`
	Reason           string `json:"reason"`
	IdleInhibited    bool   `json:"idle_inhibited"`
	SuspendInhibited bool   `json:"suspend_inhibited"`
	LockInhibited    bool   `json:"lock_inhibited"`
	LogindIdle       string `json:"logind_idle"`
	LogindSleep      string `json:"logind_sleep"`
	ScreenSaver      string `json:"screen_saver"`
	Error            string `json:"error,omitempty"`
}

// inhibitorLeg is one supported desktop API leg. Acquire must hold the kernel
// or compositor resource until Release; Alive reports whether the resource is
// still held (the bus connection may have dropped).
type inhibitorLeg interface {
	acquire(ctx context.Context) error
	release()
	alive() bool
}

// AuthorizationInhibitors owns the three legs and reconciles them
// idempotently. It is safe for concurrent use.
type AuthorizationInhibitors struct {
	mu      sync.Mutex
	legs    map[string]inhibitorLeg
	active  map[string]bool
	lastErr map[string]string
}

// NewAuthorizationInhibitors builds the production leg set for one owner UID.
func NewAuthorizationInhibitors(uid uint32) *AuthorizationInhibitors {
	return newAuthorizationInhibitors(uid, map[string]inhibitorLeg{
		legLogindIdle:  &logindInhibitor{what: "idle", fd: -1},
		legLogindSleep: &logindInhibitor{what: "sleep", fd: -1},
		legScreenSaver: &screenSaverInhibitor{uid: uid},
	})
}

func newAuthorizationInhibitors(_ uint32, legs map[string]inhibitorLeg) *AuthorizationInhibitors {
	return &AuthorizationInhibitors{legs: legs, active: map[string]bool{}, lastErr: map[string]string{}}
}

// Reconcile applies the requested authorization state. Acquiring is idempotent;
// a leg whose connection dropped is released and acquired again.
func (a *AuthorizationInhibitors) Reconcile(ctx context.Context, authorized bool) InhibitorStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !authorized {
		a.releaseLocked()
		return a.statusLocked("authorization_inactive")
	}
	for _, name := range inhibitorLegOrder {
		leg := a.legs[name]
		if leg == nil {
			continue
		}
		if a.active[name] && leg.alive() {
			continue
		}
		if a.active[name] {
			leg.release()
			a.active[name] = false
		}
		if err := leg.acquire(ctx); err != nil {
			a.lastErr[name] = err.Error()
			continue
		}
		a.active[name] = true
		delete(a.lastErr, name)
	}
	return a.statusLocked("")
}

// Release drops every held leg. It restores the previous system policy because
// no system policy was ever changed; only scoped inhibitors were held.
func (a *AuthorizationInhibitors) Release() InhibitorStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.releaseLocked()
	return a.statusLocked("authorization_inactive")
}

// Status reports the current in-memory state without touching any bus.
func (a *AuthorizationInhibitors) Status() InhibitorStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.statusLocked("")
}

func (a *AuthorizationInhibitors) releaseLocked() {
	for _, name := range inhibitorLegOrder {
		if leg := a.legs[name]; leg != nil && a.active[name] {
			leg.release()
		}
		a.active[name] = false
	}
}

func (a *AuthorizationInhibitors) statusLocked(reason string) InhibitorStatus {
	status := InhibitorStatus{Reason: reason}
	states := map[string]string{}
	firstFailure := ""
	firstError := ""
	for _, name := range inhibitorLegOrder {
		if a.legs[name] == nil {
			states[name] = legStateUnavailable
			continue
		}
		switch {
		case a.active[name]:
			states[name] = legStateActive
		case a.lastErr[name] != "":
			states[name] = legStateUnavailable
			if firstFailure == "" {
				firstFailure = name
				firstError = a.lastErr[name]
			}
		default:
			states[name] = legStateReleased
		}
	}
	status.LogindIdle = states[legLogindIdle]
	status.LogindSleep = states[legLogindSleep]
	status.ScreenSaver = states[legScreenSaver]
	status.IdleInhibited = a.active[legLogindIdle]
	status.SuspendInhibited = a.active[legLogindSleep]
	status.LockInhibited = a.active[legScreenSaver]
	status.Active = status.IdleInhibited && status.SuspendInhibited && status.LockInhibited
	switch {
	case status.Active:
		status.Reason = "authorization_active"
	case reason == "":
		status.Reason = firstFailure + "_unavailable"
	}
	status.Error = firstError
	return status
}

// logindInhibitor holds one logind block inhibitor for the exact "what" scope.
// The returned file descriptor is the lifetime anchor: closing it (or process
// exit) releases the inhibitor, so it is kept open for as long as the
// authorization is active.
type logindInhibitor struct {
	what string
	conn *dbus.Conn
	fd   dbus.UnixFD
	open bool
}

func (l *logindInhibitor) acquire(ctx context.Context) error {
	l.release()
	// The connection outlives any single reconcile request: do not bind it to
	// a request context that is cancelled when the caller returns.
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return errors.New("system_bus_unavailable")
	}
	var fd dbus.UnixFD
	if err := conn.Object("org.freedesktop.login1", "/org/freedesktop/login1").
		CallWithContext(ctx, "org.freedesktop.login1.Manager.Inhibit", 0,
			l.what, InhibitorWho, InhibitorReason, "block").Store(&fd); err != nil {
		_ = conn.Close()
		return errors.New("logind_inhibit_rejected")
	}
	if fd < 0 {
		_ = conn.Close()
		return errors.New("logind_inhibit_invalid_descriptor")
	}
	l.conn, l.fd, l.open = conn, fd, true
	return nil
}

func (l *logindInhibitor) release() {
	if l.open && l.fd >= 0 {
		_ = syscall.Close(int(l.fd))
	}
	l.open, l.fd = false, -1
	if l.conn != nil {
		_ = l.conn.Close()
		l.conn = nil
	}
}

func (l *logindInhibitor) alive() bool {
	return l.open && l.fd >= 0 && l.conn != nil && l.conn.Connected()
}

// screenSaverInhibitor holds the owner session's ScreenSaver inhibit cookie.
// The session bus connection is kept open: KDE releases the inhibition when the
// client disconnects, and UnInhibit is sent on explicit release.
type screenSaverInhibitor struct {
	uid     uint32
	address string // test seam; empty resolves the owner runtime bus
	conn    *dbus.Conn
	cookie  uint32
	open    bool
}

func (s *screenSaverInhibitor) busAddress() (string, error) {
	if s.address != "" {
		return s.address, nil
	}
	return ownerSessionBusAddress(s.uid)
}

func (s *screenSaverInhibitor) acquire(ctx context.Context) error {
	s.release()
	address, err := s.busAddress()
	if err != nil {
		return err
	}
	conn, err := dbus.Connect(address)
	if err != nil {
		return errors.New("session_bus_unavailable")
	}
	var cookie uint32
	if err := conn.Object("org.freedesktop.ScreenSaver", "/ScreenSaver").
		CallWithContext(ctx, "org.freedesktop.ScreenSaver.Inhibit", 0,
			InhibitorWho, InhibitorReason).Store(&cookie); err != nil {
		_ = conn.Close()
		return errors.New("screen_saver_inhibit_rejected")
	}
	s.conn, s.cookie, s.open = conn, cookie, true
	return nil
}

func (s *screenSaverInhibitor) release() {
	if s.open && s.conn != nil {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = s.conn.Object("org.freedesktop.ScreenSaver", "/ScreenSaver").
			CallWithContext(releaseCtx, "org.freedesktop.ScreenSaver.UnInhibit", 0, s.cookie).Err
		cancel()
	}
	s.open, s.cookie = false, 0
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
}

func (s *screenSaverInhibitor) alive() bool {
	return s.open && s.conn != nil && s.conn.Connected()
}

// ownerSessionBusAddress resolves the owner's session bus from the systemd user
// runtime directory. It never guesses a bus from the caller environment, so an
// SSH-started daemon binds to the enrolled owner's session and nothing else.
func ownerSessionBusAddress(uid uint32) (string, error) {
	return ownerSessionBusAddressIn("/run/user", uid)
}

func ownerSessionBusAddressIn(root string, uid uint32) (string, error) {
	if uid == 0 {
		return "", errors.New("owner_session_bus_requires_user")
	}
	dir := filepath.Join(root, strconv.FormatUint(uint64(uid), 10))
	dirInfo, err := os.Lstat(dir)
	if err != nil {
		return "", errors.New("owner_runtime_dir_unavailable")
	}
	dirStat, ok := dirInfo.Sys().(*syscall.Stat_t)
	if !ok || !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 || dirStat.Uid != uid || dirInfo.Mode().Perm() != 0o700 {
		return "", errors.New("unsafe_owner_runtime_dir")
	}
	bus := filepath.Join(dir, "bus")
	busInfo, err := os.Lstat(bus)
	if err != nil {
		return "", errors.New("owner_session_bus_unavailable")
	}
	busStat, ok := busInfo.Sys().(*syscall.Stat_t)
	if !ok || busInfo.Mode()&os.ModeSocket == 0 || busStat.Uid != uid {
		return "", errors.New("unsafe_owner_session_bus")
	}
	return "unix:path=" + bus, nil
}

// logindInhibitorEntry mirrors the (ssssuu) ListInhibitors reply. It is used by
// the read-only status surface to cross-check the live kernel-level inhibitor.
type logindInhibitorEntry struct {
	What string
	Who  string
	Why  string
	Mode string
	UID  uint32
	PID  uint32
}

// LiveLogindInhibitor reports whether logind currently holds Zen's idle and
// sleep block inhibitors for ownerUID. It performs no acquisition.
func LiveLogindInhibitor(ctx context.Context, ownerUID uint32) (idle bool, sleep bool, pid uint32, err error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return false, false, 0, errors.New("system_bus_unavailable")
	}
	defer conn.Close()
	var entries []logindInhibitorEntry
	if err := conn.Object("org.freedesktop.login1", "/org/freedesktop/login1").
		CallWithContext(ctx, "org.freedesktop.login1.Manager.ListInhibitors", 0).Store(&entries); err != nil {
		return false, false, 0, errors.New("logind_inhibitor_list_unavailable")
	}
	for _, entry := range entries {
		if entry.Who != InhibitorWho || entry.UID != ownerUID || entry.Mode != "block" {
			continue
		}
		for _, what := range splitInhibitorWhat(entry.What) {
			switch what {
			case "idle":
				idle, pid = true, entry.PID
			case "sleep":
				sleep, pid = true, entry.PID
			}
		}
	}
	return idle, sleep, pid, nil
}

func splitInhibitorWhat(value string) []string {
	var result []string
	start := 0
	for i := 0; i <= len(value); i++ {
		if i == len(value) || value[i] == ':' {
			if i > start {
				result = append(result, value[start:i])
			}
			start = i + 1
		}
	}
	return result
}
