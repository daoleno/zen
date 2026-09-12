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

var (
	moonlightChallengesMu sync.Mutex
	moonlightChallenges   = map[string]*moonlightChallenge{}
)

const moonlightChallengeTTL = 2 * time.Minute

func moonlightChallengeSweepLocked(now time.Time) {
	for attempt, challenge := range moonlightChallenges {
		if now.After(challenge.ExpiresAt) {
			delete(moonlightChallenges, attempt)
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
	if !s.auth.HasDesktopScope(device.ID, device.PublicKeyHex) || !actualRequestTLS(r) {
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
	moonlightChallengesMu.Lock()
	moonlightChallengeSweepLocked(now)
	moonlightChallenges[raw.Attempt] = &moonlightChallenge{
		DeviceID:  device.ID,
		DaemonID:  s.auth.DaemonID(),
		Nonce:     nonce,
		ExpiresAt: now.Add(moonlightChallengeTTL),
	}
	moonlightChallengesMu.Unlock()
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
	if !s.auth.HasDesktopScope(device.ID, device.PublicKeyHex) || !actualRequestTLS(r) {
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
	moonlightChallengesMu.Lock()
	moonlightChallengeSweepLocked(now)
	challenge := moonlightChallenges[raw.Attempt]
	if challenge == nil || challenge.DeviceID != device.ID || challenge.DaemonID != s.auth.DaemonID() {
		moonlightChallengesMu.Unlock()
		http.Error(w, "unknown_enrollment_attempt", http.StatusConflict)
		return
	}
	if challenge.Consumed {
		moonlightChallengesMu.Unlock()
		http.Error(w, "enrollment_attempt_replayed", http.StatusConflict)
		return
	}
	if challenge.Nonce != raw.Nonce {
		moonlightChallengesMu.Unlock()
		http.Error(w, "enrollment_challenge_mismatch", http.StatusConflict)
		return
	}
	// Verify possession of the engine private key before touching ownership.
	if err := host.VerifyEnrollmentProof(raw.ClientCertPEM, raw.Attempt, raw.Nonce, raw.Signature); err != nil {
		moonlightChallengesMu.Unlock()
		http.Error(w, "enrollment_proof_invalid", http.StatusForbidden)
		return
	}
	fingerprint, err := host.CertFingerprint(raw.ClientCertPEM)
	if err != nil {
		moonlightChallengesMu.Unlock()
		http.Error(w, "enrollment_certificate_invalid", http.StatusBadRequest)
		return
	}
	if owner, bound, err := host.SunshineEnrollmentByCert(fingerprint); err != nil {
		moonlightChallengesMu.Unlock()
		http.Error(w, "enrollment_state_unavailable", http.StatusServiceUnavailable)
		return
	} else if bound && owner != device.ID {
		// A must never claim a certificate already owned by B.
		moonlightChallengesMu.Unlock()
		http.Error(w, "enrollment_certificate_claimed", http.StatusConflict)
		return
	}
	if err := host.EnrollFromState(device.ID, raw.ClientCertPEM); err != nil {
		moonlightChallengesMu.Unlock()
		if errors.Is(err, host.ErrSunshineEnrollmentNotFound) {
			// Pairing has not reached the owned state yet; the attempt stays
			// valid until expiry so the client can retry after pairing.
			http.Error(w, "enrollment_pending", http.StatusConflict)
			return
		}
		http.Error(w, "enrollment_state_unavailable", http.StatusServiceUnavailable)
		return
	}
	uuid, ok, err := host.SunshineEnrollment(device.ID)
	if err != nil || !ok {
		http.Error(w, "enrollment_state_unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = host.BindEnrollmentCertificate(device.ID, fingerprint)
	challenge.Consumed = true
	moonlightChallengesMu.Unlock()
	s.writeJSONWithAssertion(w, http.StatusOK, auth.DesktopCapabilityPurpose, map[string]any{
		"enrolled":  true,
		"uuid":      uuid,
		"device_id": device.ID,
	})
}
