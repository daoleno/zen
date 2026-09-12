package server

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/desktop/host"
)

// The enrollment handshake proves possession of the native Moonlight client
// private key before a pairing is admitted as usable:
//
//	POST /desktop/moonlight/enroll/begin    {"attempt": "<32 hex>"} -> {"nonce"}
//	POST /desktop/moonlight/enroll/complete {"attempt","nonce","client_cert_pem","signature"}
//
// Both requests use the existing authenticated desktop-scope transport. The
// challenge binds the authenticated device, this daemon, the client attempt and
// a fresh nonce; replay and cross-device claiming are rejected. Association
// succeeds only when the read-only Zen-owned Sunshine state contains the exact
// certificate, which yields the authoritative generated UUID.
type moonlightChallenge struct {
	DeviceID  string
	DaemonID  string
	Nonce     string
	ExpiresAt time.Time
	Consumed  bool
}

const moonlightChallengeTTL = 2 * time.Minute

func (s *Server) moonlightChallengeStore() (map[string]*moonlightChallenge, *sync.Mutex) {
	if s.moonlightChallenges == nil {
		s.moonlightChallenges = map[string]*moonlightChallenge{}
	}
	return s.moonlightChallenges, &s.moonlightChallengesMu
}

func moonlightChallengeSweepLocked(challenges map[string]*moonlightChallenge, now time.Time) {
	for attempt, challenge := range challenges {
		if now.After(challenge.ExpiresAt) {
			delete(challenges, attempt)
		}
	}
}

func (s *Server) handleMoonlightEnrollBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.RawQuery != "" {
		http.Error(w, "invalid_moonlight_enroll_request", http.StatusBadRequest)
		return
	}
	device, ok := s.authenticateRequest(w, r, auth.DesktopCapabilityPurpose)
	if !ok {
		return
	}
	// The control call is authenticated by the signed Zen device assertion and
	// desktop scope, same as /desktop/capability. The app's verified control
	// channel is the signed HTTP endpoint; identity-bound TLS is used when the
	// transport provides it, and is never assumed for a self-signed daemon.
	if !s.auth.HasDesktopScope(device.ID, device.PublicKeyHex) {
		http.Error(w, "desktop_scope_required", http.StatusForbidden)
		return
	}
	var raw struct {
		Attempt string `json:"attempt"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	if decoder.Decode(&raw) != nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid_moonlight_enroll_request", http.StatusBadRequest)
		return
	}
	if len(raw.Attempt) != 32 {
		http.Error(w, "invalid_enrollment_attempt", http.StatusBadRequest)
		return
	}
	if _, err := hex.DecodeString(raw.Attempt); err != nil {
		http.Error(w, "invalid_enrollment_attempt", http.StatusBadRequest)
		return
	}
	nonce, err := host.NewEnrollmentNonce()
	if err != nil {
		http.Error(w, "enrollment_unavailable", http.StatusInternalServerError)
		return
	}
	now := time.Now()
	challenges, mu := s.moonlightChallengeStore()
	mu.Lock()
	moonlightChallengeSweepLocked(challenges, now)
	challenges[raw.Attempt] = &moonlightChallenge{
		DeviceID:  device.ID,
		DaemonID:  s.auth.DaemonID(),
		Nonce:     nonce,
		ExpiresAt: now.Add(moonlightChallengeTTL),
	}
	mu.Unlock()
	s.writeJSONWithAssertion(w, http.StatusOK, auth.DesktopCapabilityPurpose, map[string]any{
		"attempt":    raw.Attempt,
		"nonce":      nonce,
		"expires_in": int(moonlightChallengeTTL.Seconds()),
		"daemon_id":  s.auth.DaemonID(),
		"device_id":  device.ID,
	})
}

func (s *Server) handleMoonlightEnrollComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.RawQuery != "" {
		http.Error(w, "invalid_moonlight_enroll_request", http.StatusBadRequest)
		return
	}
	device, ok := s.authenticateRequest(w, r, auth.DesktopCapabilityPurpose)
	if !ok {
		return
	}
	// The control call is authenticated by the signed Zen device assertion and
	// desktop scope, same as /desktop/capability. The app's verified control
	// channel is the signed HTTP endpoint; identity-bound TLS is used when the
	// transport provides it, and is never assumed for a self-signed daemon.
	if !s.auth.HasDesktopScope(device.ID, device.PublicKeyHex) {
		http.Error(w, "desktop_scope_required", http.StatusForbidden)
		return
	}
	var raw struct {
		Attempt       string `json:"attempt"`
		Nonce         string `json:"nonce"`
		ClientCertPEM string `json:"client_cert_pem"`
		Signature     string `json:"signature"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	decoder := json.NewDecoder(r.Body)
	if decoder.Decode(&raw) != nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid_moonlight_enroll_request", http.StatusBadRequest)
		return
	}

	now := time.Now()
	challenges, mu := s.moonlightChallengeStore()
	mu.Lock()
	moonlightChallengeSweepLocked(challenges, now)
	challenge := challenges[raw.Attempt]
	if challenge == nil || challenge.DeviceID != device.ID || challenge.DaemonID != s.auth.DaemonID() {
		mu.Unlock()
		http.Error(w, "unknown_enrollment_attempt", http.StatusConflict)
		return
	}
	if challenge.Consumed {
		mu.Unlock()
		http.Error(w, "enrollment_attempt_replayed", http.StatusConflict)
		return
	}
	if challenge.Nonce != raw.Nonce {
		mu.Unlock()
		http.Error(w, "enrollment_challenge_mismatch", http.StatusConflict)
		return
	}
	// Verify possession of the engine private key before touching ownership.
	if err := host.VerifyEnrollmentProof(raw.ClientCertPEM, raw.Attempt, raw.Nonce, raw.Signature); err != nil {
		mu.Unlock()
		http.Error(w, "enrollment_proof_invalid", http.StatusForbidden)
		return
	}
	fingerprint, err := host.CertFingerprint(raw.ClientCertPEM)
	if err != nil {
		mu.Unlock()
		http.Error(w, "enrollment_certificate_invalid", http.StatusBadRequest)
		return
	}
	if owner, bound, err := host.SunshineEnrollmentByCert(fingerprint); err != nil {
		mu.Unlock()
		http.Error(w, "enrollment_state_unavailable", http.StatusServiceUnavailable)
		return
	} else if bound && owner != device.ID {
		// A must never claim a certificate already owned by B.
		mu.Unlock()
		http.Error(w, "enrollment_certificate_claimed", http.StatusConflict)
		return
	}
	// Ownership intent is durable: pending state is written before any response,
	// including the not-yet-paired case, so revoke can always cancel it.
	enrollErr := host.EnrollFromState(device.ID, raw.ClientCertPEM, fingerprint)
	// Re-check authorization at the commit point: a revocation or scope removal
	// racing this completion must not create a new authorized credential.
	if !s.auth.HasDesktopScope(device.ID, device.PublicKeyHex) {
		_ = host.CancelSunshineEnrollment(device.ID)
		mu.Unlock()
		http.Error(w, "desktop_scope_required", http.StatusForbidden)
		return
	}
	if enrollErr != nil {
		if errors.Is(enrollErr, host.ErrSunshineEnrollmentNotFound) {
			// Pending intent recorded; the attempt stays valid until expiry so
			// the client can retry after pairing completes.
			mu.Unlock()
			http.Error(w, "enrollment_pending", http.StatusConflict)
			return
		}
		if errors.Is(enrollErr, host.ErrSunshineStateCorrupt) {
			mu.Unlock()
			http.Error(w, "enrollment_state_unavailable", http.StatusServiceUnavailable)
			return
		}
		mu.Unlock()
		http.Error(w, "enrollment_state_unavailable", http.StatusServiceUnavailable)
		return
	}
	uuid, ok, err := host.SunshineEnrollment(device.ID)
	if err != nil {
		mu.Unlock()
		http.Error(w, "enrollment_state_unavailable", http.StatusServiceUnavailable)
		return
	}
	if !ok || uuid == "" {
		mu.Unlock()
		http.Error(w, "enrollment_pending", http.StatusConflict)
		return
	}
	challenge.Consumed = true
	mu.Unlock()
	s.writeJSONWithAssertion(w, http.StatusOK, auth.DesktopCapabilityPurpose, map[string]any{
		"enrolled":  true,
		"uuid":      uuid,
		"device_id": device.ID,
	})
}
