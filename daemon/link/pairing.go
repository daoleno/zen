package link

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/auth"
)

const PairingVersion = 2

type PairingCandidate struct {
	Name         string `json:"n"`
	AdmissionURL string `json:"a,omitempty"`
	StableURL    string `json:"s"`
}

type PairingPayload struct {
	Version         int                `json:"v"`
	DaemonID        string             `json:"d"`
	DaemonPublicKey string             `json:"k"`
	EnrollmentToken string             `json:"e"`
	RouteID         string             `json:"r"`
	TransportPin    string             `json:"p"`
	Candidates      []PairingCandidate `json:"c"`
	ExpiresAtMS     int64              `json:"x"`
	Signature       string             `json:"z"`
}

func BuildPairingLink(
	authManager *auth.Manager,
	identity *TransportIdentity,
	config ConnectorConfig,
	pairing auth.PairingToken,
	admissions []AdmissionOffer,
) (string, PairingPayload, error) {
	if authManager == nil || identity == nil {
		return "", PairingPayload{}, errors.New("daemon auth and Link transport identity are required")
	}
	admissionByRelay := make(map[string]AdmissionOffer, len(admissions))
	for _, admission := range admissions {
		admissionByRelay[admission.RelayName] = admission
	}
	candidates := make([]PairingCandidate, 0, len(config.Candidates))
	for _, candidate := range config.Candidates {
		stableHost := identity.RouteID + "." + candidate.ClientDomain
		entry := PairingCandidate{
			Name:      candidate.Name,
			StableURL: candidateURL(stableHost, candidate.ClientPort),
		}
		if admission, ok := admissionByRelay[candidate.Name]; ok {
			entry.AdmissionURL = admission.AdmissionURL
		}
		candidates = append(candidates, entry)
	}
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].Name == candidates[right].Name {
			return candidates[left].StableURL < candidates[right].StableURL
		}
		return candidates[left].Name < candidates[right].Name
	})
	hasAdmission := false
	for _, candidate := range candidates {
		if candidate.AdmissionURL != "" {
			hasAdmission = true
			break
		}
	}
	if !hasAdmission {
		return "", PairingPayload{}, errors.New("no connected Link relay issued a pairing admission")
	}
	payload := PairingPayload{
		Version:         PairingVersion,
		DaemonID:        authManager.DaemonID(),
		DaemonPublicKey: authManager.PublicKeyHex(),
		EnrollmentToken: strings.ToLower(strings.TrimSpace(pairing.Value)),
		RouteID:         identity.RouteID,
		TransportPin:    identity.SPKISHA256,
		Candidates:      candidates,
		ExpiresAtMS:     pairing.ExpiresAt.UnixMilli(),
	}
	payload.Signature = authManager.CreateLinkPairingSignature(
		PairingBindingPayload(payload),
	)
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", PairingPayload{}, fmt.Errorf("encode Link pairing payload: %w", err)
	}
	values := url.Values{}
	values.Set("v", fmt.Sprintf("%d", PairingVersion))
	values.Set(connectPayloadParameter, base64.RawURLEncoding.EncodeToString(raw))
	return "zen://settings?" + values.Encode(), payload, nil
}

const connectPayloadParameter = "p"

func PairingBindingPayload(payload PairingPayload) []byte {
	parts := []string{
		fmt.Sprintf("%d", payload.Version),
		strings.ToLower(strings.TrimSpace(payload.DaemonID)),
		strings.ToLower(strings.TrimSpace(payload.DaemonPublicKey)),
		strings.ToLower(strings.TrimSpace(payload.EnrollmentToken)),
		strings.ToLower(strings.TrimSpace(payload.RouteID)),
		strings.ToLower(strings.TrimSpace(payload.TransportPin)),
		fmt.Sprintf("%d", payload.ExpiresAtMS),
	}
	for _, candidate := range payload.Candidates {
		parts = append(parts,
			strings.TrimSpace(candidate.Name),
			strings.TrimSpace(candidate.AdmissionURL),
			strings.TrimSpace(candidate.StableURL),
		)
	}
	return []byte(strings.Join(parts, "\n"))
}

func ValidatePairingPayload(payload PairingPayload, now time.Time) error {
	if payload.Version != PairingVersion ||
		!linkID(payload.RouteID, 16) ||
		len(strings.TrimSpace(payload.TransportPin)) != 64 ||
		len(payload.Candidates) == 0 {
		return errors.New("invalid Link pairing payload")
	}
	if now.UnixMilli() > payload.ExpiresAtMS {
		return errors.New("Link pairing payload expired")
	}
	if !auth.VerifyLinkPairingSignature(
		payload.DaemonPublicKey,
		PairingBindingPayload(payload),
		payload.Signature,
	) {
		return errors.New("Link pairing payload failed daemon identity proof")
	}
	return nil
}

func linkID(value string, bytes int) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if len(normalized) != bytes*2 {
		return false
	}
	for _, character := range normalized {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}
