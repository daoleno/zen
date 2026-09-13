package server

import (
	"fmt"
	"net/http"

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
	s.writeJSONWithAssertion(w, http.StatusOK, auth.DesktopCapabilityPurpose, s.desktopCapability(device, s.desktopIngressOf(r)))
}

func (s *Server) desktopCapability(device *auth.TrustedDevice, ingress desktopIngress) map[string]any {
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
	case !readiness.Broker && !readiness.CurrentSession:
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
			"unattended": unattended && scoped && (requestTLS || identityTLS || trustedIngress) && (readiness.Broker || readiness.CurrentSession),
			"reason":     reason,
			"recovery":   recovery,
		},
	}
	moonlightBinding := ""
	if scoped && (readiness.Broker || readiness.CurrentSession) {
		if moonlight := moonlightBootstrap(device); moonlight != nil {
			if admission, err := moonlightAdmission(device.ID); err == nil {
				moonlight["admission"] = admission
			} else {
				moonlight["admission"] = "unavailable"
			}
			payload["moonlight"] = moonlight
			moonlightBinding = moonlightBindingString(moonlight)
		}
	}
	// v1 stays byte-identical for installed clients; the Moonlight bootstrap is
	// covered by a separate v2 signature so old clients keep working.
	payload["capability_signature"] = s.auth.SignDesktopCapability(pin, identityTLS)
	if moonlightBinding != "" {
		payload["capability_signature_v2"] = s.auth.SignDesktopCapabilityV2(pin, identityTLS, moonlightBinding)
	}
	return payload
}

// moonlightBindingString is the canonical, newline-joined form of the emitted
// block. Both sides sign this exact string so an injected or altered block
// invalidates the capability signature.
func moonlightBindingString(block map[string]any) string {
	return fmt.Sprintf("%v\n%v\n%v\n%v\n%v\n%v\n%v",
		block["available"], block["http_port"], block["https_port"],
		block["app_id"], block["host_key"], block["identity_key"], block["admission"])
}

func moonlightAvailabilityReason() string {
	if moonlightAvailable() {
		return ""
	}
	return "admin_binding_unavailable"
}

// moonlightBootstrap advertises the explicitly configured Sunshine host. The
// identity key is the authenticated Zen device id; the host key is the
// Zen-owned Sunshine host identity, so private storage never falls back to a
// lossy hostname and the app keeps using signed desktop-scope authorization.
func moonlightBootstrap(device *auth.TrustedDevice) map[string]any {
	snapshot := moonlightSnapshot()
	if !snapshot.Configured {
		return nil
	}
	if !snapshot.Running {
		if updated, err := moonlightEnsure(nil); err == nil {
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
