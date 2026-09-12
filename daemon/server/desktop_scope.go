package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/daoleno/zen/daemon/auth"
)

// handleDesktopScope is the explicit in-place consent endpoint for one already
// trusted device. It requires the authenticated device's own signed request and
// identity-bound TLS; it never issues a pairing token, never creates a device
// record and never accepts a target device from the request body.
func (s *Server) handleDesktopScope(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.RawQuery != "" {
		http.Error(w, "invalid_desktop_scope_request", http.StatusBadRequest)
		return
	}
	device, ok := s.authenticateRequest(w, r, auth.DesktopGrantPurpose)
	if !ok {
		return
	}
	if !actualRequestTLS(r) {
		http.Error(w, "desktop_tls_required", http.StatusForbidden)
		return
	}
	var raw struct {
		DesktopScopeVersion int `json:"desktop_scope_version"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	if decoder.Decode(&raw) != nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid_desktop_scope_request", http.StatusBadRequest)
		return
	}
	if raw.DesktopScopeVersion != auth.DesktopScopeVersion {
		http.Error(w, "unsupported_desktop_scope_version", http.StatusBadRequest)
		return
	}
	granted, err := s.auth.GrantDesktopScope(device.ID, device.PublicKeyHex, raw.DesktopScopeVersion)
	if err != nil {
		switch err {
		case auth.ErrUnknownDevice, auth.ErrUnauthorized:
			http.Error(w, err.Error(), http.StatusUnauthorized)
		default:
			http.Error(w, "desktop_scope_persist_failed", http.StatusInternalServerError)
		}
		return
	}
	s.writeJSONWithAssertion(w, http.StatusOK, auth.DesktopGrantPurpose, map[string]any{
		"ok":                    true,
		"device_id":             granted.ID,
		"desktop_scope_version": granted.DesktopScopeVersion,
	})
}
