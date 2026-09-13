package host

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/daoleno/zen/daemon/auth"
)

// The desktop authorization controller ties the scoped idle/suspend inhibitors
// to the persisted unattended authorization lifecycle:
//
//   - the canonical per-device desktop_scope_version=1 grant is the only input;
//   - a device revoke that leaves no authorized device releases every leg;
//   - stopping the host releases every leg;
//   - a locked, foreign, greeter or missing owner session releases every leg and
//     re-acquires only when the owner desktop is active and unlocked again.
//
// No system policy is changed, so release restores exactly the prior policy.
// The controller publishes an owner-only status record so the operator can
// verify the state over SSH without a live D-Bus query.

const authorizationStatusVersion = 1

// AuthorizationInput is the persisted authorization fact the controller acts on.
// The daemon computes it from the canonical auth store and the supervised host
// configuration; a network request never supplies it.
type AuthorizationInput struct {
	AuthorizedDevices int  `json:"authorized_devices"`
	HostConfigured    bool `json:"host_configured"`
	Reachable         bool `json:"reachable"`
}

// AuthorizationStatus is the operator-visible status contract.
type AuthorizationStatus struct {
	Version           int             `json:"version"`
	Active            bool            `json:"active"`
	Reason            string          `json:"reason"`
	AuthorizedDevices int             `json:"authorized_devices"`
	HostConfigured    bool            `json:"host_configured"`
	Reachable         bool            `json:"reachable"`
	Session           *OwnerSession   `json:"session,omitempty"`
	Inhibitors        InhibitorStatus `json:"inhibitors"`
	UpdatedAt         time.Time       `json:"updated_at"`
	PID               int             `json:"pid"`
	Error             string          `json:"error,omitempty"`
}

// AuthorizationController owns one inhibitor leg set for one owner UID.
type AuthorizationController struct {
	mu       sync.Mutex
	uid      uint32
	inhibit  *AuthorizationInhibitors
	discover func(ctx context.Context, uid uint32) (OwnerSession, error)
	path     string
	status   AuthorizationStatus
}

// NewAuthorizationController builds the production controller for the current
// process's owner UID.
func NewAuthorizationController(uid uint32) *AuthorizationController {
	return newAuthorizationController(uid, NewAuthorizationInhibitors(uid), DiscoverOwnerSession, DefaultAuthorizationStatusPath())
}

func newAuthorizationController(uid uint32, inhibit *AuthorizationInhibitors, discover func(context.Context, uint32) (OwnerSession, error), path string) *AuthorizationController {
	return &AuthorizationController{uid: uid, inhibit: inhibit, discover: discover, path: path}
}

// DefaultAuthorizationStatusPath is the Zen-owned status record location.
func DefaultAuthorizationStatusPath() string {
	if override := os.Getenv("ZEN_DESKTOP_AUTHORIZATION_STATUS"); override != "" {
		return override
	}
	return filepath.Join(ZenStateDir(), "authorization-status.json")
}

// Reconcile applies the requested authorization state and publishes the status.
// It is idempotent and safe for concurrent use.
func (c *AuthorizationController) Reconcile(ctx context.Context, input AuthorizationInput) AuthorizationStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	status := AuthorizationStatus{
		Version:           authorizationStatusVersion,
		AuthorizedDevices: input.AuthorizedDevices,
		HostConfigured:    input.HostConfigured,
		Reachable:         input.Reachable,
		UpdatedAt:         time.Now().UTC(),
		PID:               os.Getpid(),
	}
	switch {
	case input.AuthorizedDevices <= 0:
		status.Inhibitors = c.inhibit.Release()
		status.Reason = "authorization_inactive"
	case !input.Reachable:
		status.Inhibitors = c.inhibit.Release()
		status.Reason = "host_unreachable"
	default:
		session, err := c.discover(ctx, c.uid)
		if err != nil {
			status.Inhibitors = c.inhibit.Release()
			status.Reason = "session_unavailable"
			status.Error = err.Error()
			break
		}
		status.Session = &session
		status.Inhibitors = c.inhibit.Reconcile(ctx, true)
		status.Active = status.Inhibitors.Active
		if status.Active {
			status.Reason = "authorization_active"
		} else {
			status.Reason = status.Inhibitors.Reason
		}
	}
	if err := writeAuthorizationStatus(c.path, status); err != nil {
		status.Error = joinStatusError(status.Error, "status_write_failed")
	}
	c.status = status
	return status
}

// Release drops every leg without touching the persisted authorization.
func (c *AuthorizationController) Release() AuthorizationStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	status := AuthorizationStatus{
		Version:    authorizationStatusVersion,
		Reason:     "authorization_inactive",
		UpdatedAt:  time.Now().UTC(),
		PID:        os.Getpid(),
		Inhibitors: c.inhibit.Release(),
	}
	if previous := c.status; previous.Version != 0 {
		status.AuthorizedDevices = previous.AuthorizedDevices
		status.HostConfigured = previous.HostConfigured
		status.Reachable = previous.Reachable
	}
	if err := writeAuthorizationStatus(c.path, status); err != nil {
		status.Error = joinStatusError(status.Error, "status_write_failed")
	}
	c.status = status
	return status
}

// Status returns the last published status without touching any bus.
func (c *AuthorizationController) Status() AuthorizationStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func joinStatusError(current, next string) string {
	if current == "" {
		return next
	}
	return current + "; " + next
}

func writeAuthorizationStatus(path string, status AuthorizationStatus) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	tmp, err := os.CreateTemp(dir, ".authorization-status-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// ReadAuthorizationStatus reads the bounded owner-only status record.
func ReadAuthorizationStatus(path string) (AuthorizationStatus, error) {
	var status AuthorizationStatus
	if path == "" {
		path = DefaultAuthorizationStatusPath()
	}
	info, err := os.Lstat(path)
	if err != nil {
		return status, err
	}
	if !info.Mode().IsRegular() || info.Size() > 65536 || info.Mode().Perm() != 0o600 {
		return status, errors.New("unsafe_authorization_status")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return status, err
	}
	if err := json.Unmarshal(body, &status); err != nil {
		return status, errors.New("invalid_authorization_status")
	}
	if status.Version != authorizationStatusVersion {
		return AuthorizationStatus{}, errors.New("unsupported_authorization_status")
	}
	return status, nil
}

// LiveLogindInhibitorReport is the live kernel-level cross-check.
type LiveLogindInhibitorReport struct {
	Idle  bool   `json:"idle"`
	Sleep bool   `json:"sleep"`
	PID   uint32 `json:"pid,omitempty"`
}

// AuthorizationReport is the read-only operator view returned by
// `zen desktop-host --status`. It never acquires or releases anything.
type AuthorizationReport struct {
	StatusPath       string                    `json:"status_path"`
	Status           AuthorizationStatus       `json:"status"`
	StatusError      string                    `json:"status_error,omitempty"`
	StatusFresh      bool                      `json:"status_fresh"`
	PersistedDevices int                       `json:"persisted_authorized_devices"`
	PersistedError   string                    `json:"persisted_error,omitempty"`
	LiveLogind       LiveLogindInhibitorReport `json:"live_logind"`
	LiveError        string                    `json:"live_error,omitempty"`
}

// InspectAuthorizationReport reads the daemon's status record, the canonical
// persisted authorization and the live logind inhibitor. Read-only.
func InspectAuthorizationReport(ctx context.Context, stateDir string) AuthorizationReport {
	report := AuthorizationReport{StatusPath: DefaultAuthorizationStatusPath()}
	status, err := ReadAuthorizationStatus(report.StatusPath)
	switch {
	case err == nil:
		report.Status = status
		report.StatusFresh = AuthorizationStatusFresh(status, time.Now())
	case errors.Is(err, os.ErrNotExist):
		report.StatusError = "no_status_record"
	default:
		report.StatusError = err.Error()
	}
	if count, err := PersistedDesktopScopeCount(stateDir); err != nil {
		report.PersistedError = err.Error()
	} else {
		report.PersistedDevices = count
	}
	idle, sleep, pid, err := LiveLogindInhibitor(ctx, uint32(os.Getuid()))
	if err != nil {
		report.LiveError = err.Error()
	} else {
		report.LiveLogind = LiveLogindInhibitorReport{Idle: idle, Sleep: sleep, PID: pid}
	}
	return report
}

// PersistedDesktopScopeCount counts the canonical trusted-device records that
// hold desktop_scope_version=1. It reads the file directly so a read-only
// status command never creates identity or device state.
func PersistedDesktopScopeCount(stateDir string) (int, error) {
	if stateDir == "" {
		var err error
		stateDir, err = auth.DefaultStorageDir()
		if err != nil {
			return 0, err
		}
	}
	path := filepath.Join(stateDir, "trusted-devices.json")
	info, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || owner.Uid != uint32(os.Getuid()) || info.Size() > 1<<20 {
		return 0, errors.New("unsafe_trusted_devices")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var persisted struct {
		Devices []struct {
			DesktopScopeVersion int `json:"desktop_scope_version"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(body, &persisted); err != nil {
		return 0, errors.New("invalid_trusted_devices")
	}
	count := 0
	for _, device := range persisted.Devices {
		if device.DesktopScopeVersion == auth.DesktopScopeVersion {
			count++
		}
	}
	return count, nil
}

// AuthorizationStatusFresh reports whether the record was published by a live
// Zen process recently enough to describe the current system state.
func AuthorizationStatusFresh(status AuthorizationStatus, now time.Time) bool {
	if status.PID <= 0 || status.UpdatedAt.IsZero() {
		return false
	}
	if now.Sub(status.UpdatedAt) > 5*time.Minute {
		return false
	}
	return processAlive(status.PID)
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
