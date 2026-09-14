package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/desktop/host"
	"github.com/daoleno/zen/daemon/link"
)

// inspectHostReadiness is the live broker probe. Tests may stub it; production
// never treats a header or pairing record as host installation.
var inspectHostReadiness = host.InspectReadiness

// Sunshine runtime hooks are the production entry point for the supervised
// Moonlight host; tests stub them so no process is started.
var moonlightSnapshot = host.SunshineSnapshot
var moonlightEnsure = host.EnsureSunshineRuntime
var moonlightAvailable = host.SunshineAvailable
var moonlightAdmission = host.SunshineAdmission

// desktopAuthorizationController is the scoped idle/suspend inhibitor owner.
// It is nil until the production entry point enables it, so unit tests never
// touch the developer's real D-Bus session.
type desktopAuthorizationController interface {
	Reconcile(ctx context.Context, input host.AuthorizationInput) host.AuthorizationStatus
	Release() host.AuthorizationStatus
	Status() host.AuthorizationStatus
}

// EnableDesktopAuthorization wires the production inhibitor controller.
func (s *Server) EnableDesktopAuthorization(controller desktopAuthorizationController) {
	s.desktopAuthorization = controller
	if s.desktopAuthorizationWake == nil {
		s.desktopAuthorizationWake = make(chan struct{}, 1)
	}
}

// nudgeDesktopAuthorization asks the background reconciler to apply the
// persisted authorization without blocking a request on D-Bus.
func (s *Server) nudgeDesktopAuthorization() {
	if s.desktopAuthorization == nil || s.desktopAuthorizationWake == nil {
		return
	}
	select {
	case s.desktopAuthorizationWake <- struct{}{}:
	default:
	}
}

func actualRequestTLS(r *http.Request) bool {
	return r.TLS != nil && r.TLS.HandshakeComplete
}

func (s *Server) handleDesktopCapability(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.RawQuery != "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	device, ok := s.authenticateRequest(w, r, auth.DesktopCapabilityPurpose)
	if !ok {
		return
	}
	s.nudgeDesktopAuthorization()
	s.writeJSONWithAssertion(w, http.StatusOK, auth.DesktopCapabilityPurpose, s.desktopCapability(r.Context(), device, s.desktopIngressOf(r)))
}

func (s *Server) desktopCapability(ctx context.Context, device *auth.TrustedDevice, ingress desktopIngress) map[string]any {
	scoped := s.auth.HasDesktopScope(device.ID, device.PublicKeyHex)
	trust := "legacy_terminal"
	if scoped {
		trust = "paired_unattended"
	}
	requestTLS := ingress.TLS
	trustedIngress := ingress.Trusted && !ingress.TLS
	identityTLS := s.desktopTransport.TLSConfig != nil && len(s.desktopTransport.Pin) == 64
	pin := ""
	if identityTLS {
		pin = s.desktopTransport.Pin
	}
	readiness := inspectHostReadiness()
	// The supervised host engine may be explicitly configured while this zen
	// process was started outside the desktop session (for example over SSH).
	// The owner session is discovered by the runtime, but configuration alone is
	// not readiness: the process must be running and administratively available.
	moonlightConfigured := moonlightSnapshot().Configured
	var moonlight map[string]any
	if scoped && moonlightConfigured {
		moonlight = moonlightBootstrap(ctx, device)
		if moonlight != nil {
			if admission, err := moonlightAdmission(device.ID); err == nil {
				moonlight["admission"] = admission
			} else {
				moonlight["admission"] = "unavailable"
			}
		}
	}
	moonlightReady := moonlight != nil && moonlight["available"] == true
	// An explicitly configured Sunshine host is not a usable desktop until its
	// supervised process is running and has passed the availability check. This
	// prevents a failed headless/KMS startup from falling through to the legacy
	// broker, which can only produce a generic connection-ended error.
	hostReady := readiness.Broker || readiness.CurrentSession || moonlightReady
	reason := ""
	recovery := ""
	unattended := false
	sessionOnly := readiness.CurrentSession && !readiness.Broker
	switch {
	case !scoped:
		reason = "desktop_scope_required"
		recovery = "Enable remote desktop in the Zen app on this phone."
	case !requestTLS && !identityTLS && !trustedIngress:
		reason = "desktop_tls_required"
		recovery = "Unattended desktop needs this computer's identity-bound encrypted transport. The unencrypted LAN switch is only for attended assistance and cannot carry OS passwords."
	case readiness.Status == host.ReadinessUnsupported:
		reason = "host_setup_required"
		recovery = "Unattended desktop is not implemented on this host platform yet."
	case !hostReady:
		reason = "host_setup_required"
		recovery = "No current desktop session is available to this zen process. Start zen from the logged-in session, or run one OS-admin zen desktop-host --install for lock and login after reboot."
	case !requestTLS && identityTLS && !trustedIngress:
		reason = "desktop_tls_required"
		recovery = "Connect using this computer's pairing identity pin. Zen starts encrypted desktop on the same address without a public certificate."
		unattended = true
	default:
		unattended = true
		if sessionOnly {
			recovery = "This connection uses the current logged-in session. Lock and login after reboot need one zen desktop-host --install."
		}
	}
	payload := map[string]any{
		"ok":                    true,
		"daemon_id":             s.auth.DaemonID(),
		"daemon_public_key":     s.auth.PublicKeyHex(),
		"device_id":             device.ID,
		"device_trust":          trust,
		"desktop_scope_version": device.DesktopScopeVersion,
		"transport": map[string]any{
			"request_encrypted":         requestTLS,
			"trusted_ingress":           trustedIngress,
			"identity_tls":              identityTLS,
			"identity_server_name":      link.DesktopIdentityServerName,
			"transport_pin":             pin,
			"attended_plaintext_lan":    true,
			"forwarded_headers_trusted": false,
		},
		"host": map[string]any{
			"status":          readiness.Status,
			"broker":          readiness.Broker,
			"current_session": readiness.CurrentSession,
			"lock_login":      readiness.Broker,
			"surface":         readiness.Surface,
			"session":         readiness.Session,
		},
		"connect": map[string]any{
			"unattended": unattended && scoped && (requestTLS || identityTLS || trustedIngress) && hostReady,
			"reason":     reason,
			"recovery":   recovery,
		},
	}
	if s.desktopAuthorization != nil {
		payload["host"].(map[string]any)["authorization"] = authorizationCapabilityBlock(s.desktopAuthorization.Status())
	}
	moonlightBinding := ""
	if moonlight != nil {
		payload["moonlight"] = moonlight
		moonlightBinding = moonlightBindingString(moonlight, trustedIngress)
	}
	// v1 stays byte-identical for installed clients; the Moonlight bootstrap is
	// covered by a separate v2 signature so old clients keep working.
	payload["capability_signature"] = s.auth.SignDesktopCapability(pin, identityTLS)
	if trustedIngress {
		// Deployment evidence is authenticated independently of the optional
		// Moonlight block so ordinary WS/scope paths are protected too.
		payload["deployment_proof"] = s.auth.SignDesktopDeploymentProof(pin, identityTLS, true)
	}
	if moonlightBinding != "" {
		payload["capability_signature_v2"] = s.auth.SignDesktopCapabilityV2(pin, identityTLS, moonlightBinding)
	}
	return payload
}

// authorizationCapabilityBlock is the additive, truthful host-side view of the
// scoped authorization inhibitor. It is not part of the signed v1/v2 payload;
// the phone uses it for status and recovery copy only.
func authorizationCapabilityBlock(status host.AuthorizationStatus) map[string]any {
	updated := ""
	if !status.UpdatedAt.IsZero() {
		updated = status.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return map[string]any{
		"active":             status.Active,
		"reason":             status.Reason,
		"idle_inhibited":     status.Inhibitors.IdleInhibited,
		"suspend_inhibited":  status.Inhibitors.SuspendInhibited,
		"lock_inhibited":     status.Inhibitors.LockInhibited,
		"authorized_devices": status.AuthorizedDevices,
		"host_configured":    status.HostConfigured,
		"updated_at":         updated,
	}
}

// moonlightBindingString is the canonical, newline-joined form of the emitted
// block. Both sides sign this exact string so an injected or altered block
// invalidates the capability signature.
func moonlightBindingString(block map[string]any, trustedIngress bool) string {
	return fmt.Sprintf("%v\n%v\n%v\n%v\n%v\n%v\n%v\n%v",
		block["available"], block["http_port"], block["https_port"],
		block["app_id"], block["host_key"], block["identity_key"], block["admission"],
		trustedIngress)
}

func moonlightHostKey() string {
	return moonlightSnapshot().HostKey
}

func moonlightAvailabilityReason() string {
	if moonlightAvailable() {
		return ""
	}
	return host.SunshineAvailabilityReason()
}

// moonlightBootstrap advertises the explicitly configured Sunshine host. The
// identity key is the authenticated Zen device id; the host key is the
// Zen-owned Sunshine host identity, so private storage never falls back to a
// lossy hostname and the app keeps using signed desktop-scope authorization.
func moonlightBootstrap(ctx context.Context, device *auth.TrustedDevice) map[string]any {
	snapshot := moonlightSnapshot()
	if !snapshot.Configured {
		return nil
	}
	if !snapshot.Running {
		if updated, err := moonlightEnsure(ctx, nil); err == nil {
			snapshot = updated
		}
	}
	if snapshot.HostKey == "" || snapshot.HTTPPort <= 0 {
		return nil
	}
	// No host in the block: the client must use its own verified, directly
	// reachable server endpoint; request Host/forwarded headers are not proof.
	return map[string]any{
		"available":    moonlightAvailable(),
		"reason":       moonlightAvailabilityReason(),
		"http_port":    snapshot.HTTPPort,
		"https_port":   snapshot.HTTPSPort,
		"app_id":       snapshot.AppID,
		"host_key":     snapshot.HostKey,
		"identity_key": device.ID,
	}
}
