//go:build !linux

package host

import (
	"context"
	"errors"
	"time"
)

// The scoped KDE/Wayland/logind authorization inhibitor is Linux-only. These
// adapters keep cross-platform builds honest: a non-Linux host never reports an
// active desktop authorization.

// OwnerSession is the enrolled owner's desktop session facts.
type OwnerSession struct {
	UID        uint32 `json:"uid"`
	ID         string `json:"id"`
	Seat       string `json:"seat"`
	Backend    string `json:"backend"`
	Display    string `json:"display"`
	BusAddress string `json:"-"`
	RuntimeDir string `json:"-"`
}

// InhibitorStatus is the truthful, per-leg inhibitor state.
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

// AuthorizationInput is the persisted authorization fact the controller acts on.
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

// AuthorizationController is a no-op outside Linux.
type AuthorizationController struct {
	status AuthorizationStatus
}

func NewAuthorizationController(uint32) *AuthorizationController {
	return &AuthorizationController{}
}

func DefaultAuthorizationStatusPath() string { return "" }

func (c *AuthorizationController) Reconcile(context.Context, AuthorizationInput) AuthorizationStatus {
	c.status = AuthorizationStatus{Reason: "authorization_unsupported", UpdatedAt: time.Now().UTC()}
	return c.status
}

func (c *AuthorizationController) Release() AuthorizationStatus {
	c.status = AuthorizationStatus{Reason: "authorization_unsupported", UpdatedAt: time.Now().UTC()}
	return c.status
}

func (c *AuthorizationController) Status() AuthorizationStatus { return c.status }

func ReadAuthorizationStatus(string) (AuthorizationStatus, error) {
	return AuthorizationStatus{}, errors.New("authorization_unsupported")
}

func AuthorizationStatusFresh(AuthorizationStatus, time.Time) bool { return false }

// LiveLogindInhibitorReport is the live kernel-level cross-check.
type LiveLogindInhibitorReport struct {
	Idle  bool   `json:"idle"`
	Sleep bool   `json:"sleep"`
	PID   uint32 `json:"pid,omitempty"`
}

// AuthorizationReport is the read-only operator view.
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

func InspectAuthorizationReport(context.Context, string) AuthorizationReport {
	return AuthorizationReport{StatusError: "authorization_unsupported"}
}

func PersistedDesktopScopeCount(string) (int, error) {
	return 0, errors.New("authorization_unsupported")
}
