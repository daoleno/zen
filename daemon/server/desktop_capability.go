package server

import (
	"net/http"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/desktop/host"
	"github.com/daoleno/zen/daemon/link"
)

// inspectHostReadiness is the live broker probe. Tests may stub it; production
// never treats a header or pairing record as host installation.
var inspectHostReadiness = host.InspectReadiness

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
	s.writeJSONWithAssertion(w, http.StatusOK, auth.DesktopCapabilityPurpose, s.desktopCapability(device, actualRequestTLS(r)))
}

func (s *Server) desktopCapability(device *auth.TrustedDevice, requestTLS bool) map[string]any {
	scoped := s.auth.HasDesktopScope(device.ID, device.PublicKeyHex)
	trust := "legacy_terminal"
	if scoped {
		trust = "paired_unattended"
	}
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
		recovery = "This phone has terminal access only. On the computer run zen pair and scan the new link once to grant unattended desktop. Zen never grants this silently."
	case !requestTLS && !identityTLS:
		reason = "desktop_tls_required"
		recovery = "Unattended desktop needs this computer's identity-bound encrypted transport. The unencrypted LAN switch is only for attended assistance and cannot carry OS passwords."
	case readiness.Status == host.ReadinessUnsupported:
		reason = "host_setup_required"
		recovery = "Unattended desktop is not implemented on this host platform yet."
	case !readiness.Broker && !readiness.CurrentSession:
		reason = "host_setup_required"
		recovery = "No current desktop session is available to this zen process. Start zen from the logged-in session, or run one OS-admin zen desktop-host --install for lock and login after reboot."
	case !requestTLS && identityTLS:
		reason = "desktop_tls_required"
		recovery = "Connect using this computer's pairing identity pin. Zen starts encrypted desktop on the same address without a public certificate."
		unattended = true
	default:
		unattended = true
		if sessionOnly {
			recovery = "This connection uses the current logged-in session. Lock and login after reboot need one zen desktop-host --install."
		}
	}
	return map[string]any{
		"ok":                    true,
		"device_id":             device.ID,
		"device_trust":          trust,
		"desktop_scope_version": device.DesktopScopeVersion,
		"transport": map[string]any{
			"request_encrypted":         requestTLS,
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
			"unattended": unattended && scoped && (requestTLS || identityTLS) && (readiness.Broker || readiness.CurrentSession),
			"reason":     reason,
			"recovery":   recovery,
		},
		"capability_signature": s.auth.SignDesktopCapability(pin, identityTLS),
	}
}
