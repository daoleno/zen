package server

import (
	"context"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/desktop/host"
)

// desktopAuthorizationInterval bounds how long a lock/unlock, host start or
// revoked inhibitor leg can go unnoticed while the daemon runs.
const desktopAuthorizationInterval = 30 * time.Second

// runDesktopAuthorization keeps the scoped idle/suspend inhibitor tied to the
// persisted unattended authorization. It reconciles immediately, on every
// explicit nudge (scope grant, device revoke, capability fetch) and on a bounded
// interval so a dropped leg or a later unlock is re-applied. Every leg is
// released when the daemon stops.
func (s *Server) runDesktopAuthorization(ctx context.Context) {
	ticker := time.NewTicker(desktopAuthorizationInterval)
	defer ticker.Stop()
	reconcile := func() {
		reconcileCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		s.reconcileDesktopAuthorization(reconcileCtx)
	}
	reconcile()
	for {
		select {
		case <-ctx.Done():
			if s.desktopAuthorization != nil {
				s.desktopAuthorization.Release()
			}
			return
		case <-ticker.C:
			reconcile()
		case <-s.desktopAuthorizationWake:
			reconcile()
		}
	}
}

// reconcileDesktopAuthorization recomputes the authorization input from the
// canonical auth store and the supervised host configuration, then applies it.
// The scoped inhibitor is tied to the explicitly configured Zen desktop host:
// a machine with no supervised host configured has no unattended path to keep
// reachable, so it must never hold a session-wide idle/suspend inhibitor.
func (s *Server) reconcileDesktopAuthorization(ctx context.Context) host.AuthorizationStatus {
	if s.desktopAuthorization == nil || s.auth == nil {
		return host.AuthorizationStatus{}
	}
	authorized := 0
	for _, device := range s.auth.ListDevices() {
		if device.DesktopScopeVersion == auth.DesktopScopeVersion {
			authorized++
		}
	}
	configured := host.SunshineConfigured()
	return s.desktopAuthorization.Reconcile(ctx, host.AuthorizationInput{
		AuthorizedDevices: authorized,
		HostConfigured:    configured,
		Reachable:         configured,
	})
}
