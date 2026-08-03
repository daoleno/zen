package link

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
)

func TestPairingV2BindsRoutePinCandidatesAndEnrollment(t *testing.T) {
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("auth.NewManager: %v", err)
	}
	identity, err := LoadOrCreateTransportIdentity(t.TempDir(), []string{"link.test"})
	if err != nil {
		t.Fatalf("LoadOrCreateTransportIdentity: %v", err)
	}
	pairing, err := manager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatalf("IssuePairingToken: %v", err)
	}
	config, err := normalizeConnectorConfig(ConnectorConfig{
		ConnectorToken: "0123456789abcdef0123456789abcdef",
		Candidates: []RelayCandidate{
			{
				Name:              "region-b",
				ControlAddress:    "region-b.test:8443",
				ControlServerName: "region-b.test",
				ClientDomain:      "b.link.test",
				ClientPort:        443,
			},
			{
				Name:              "region-a",
				ControlAddress:    "region-a.test:8443",
				ControlServerName: "region-a.test",
				ClientDomain:      "a.link.test",
				ClientPort:        443,
			},
		},
	})
	if err != nil {
		t.Fatalf("normalizeConnectorConfig: %v", err)
	}
	linkValue, payload, err := BuildPairingLink(
		manager,
		identity,
		config,
		pairing,
		[]AdmissionOffer{{
			RelayName:    "region-a",
			AdmissionURL: "wss://11111111111111111111111111111111.a.link.test/ws",
		}},
	)
	if err != nil {
		t.Fatalf("BuildPairingLink: %v", err)
	}
	if err := ValidatePairingPayload(payload, time.Now()); err != nil {
		t.Fatalf("ValidatePairingPayload: %v", err)
	}
	if payload.RouteID != identity.RouteID ||
		payload.TransportPin != identity.SPKISHA256 ||
		payload.EnrollmentToken != pairing.Value ||
		len(payload.Candidates) != 2 {
		t.Fatalf("unexpected V2 payload: %#v", payload)
	}
	if payload.Candidates[0].Name != "region-a" ||
		payload.Candidates[0].AdmissionURL == "" ||
		!strings.Contains(payload.Candidates[1].StableURL, identity.RouteID) {
		t.Fatalf("unexpected sorted candidates: %#v", payload.Candidates)
	}

	parsedURL, err := url.Parse(linkValue)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	if parsedURL.Scheme != "zen" || parsedURL.Query().Get("v") != "2" {
		t.Fatalf("unexpected pairing link: %s", linkValue)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parsedURL.Query().Get("p"))
	if err != nil {
		t.Fatalf("decode V2 payload: %v", err)
	}
	var decoded PairingPayload
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal V2 payload: %v", err)
	}
	if decoded.Signature != payload.Signature {
		t.Fatal("serialized pairing signature changed")
	}

	tampered := payload
	tampered.TransportPin = strings.Repeat("a", 64)
	if tampered.TransportPin == payload.TransportPin {
		tampered.TransportPin = strings.Repeat("b", 64)
	}
	if err := ValidatePairingPayload(tampered, time.Now()); err == nil {
		t.Fatal("tampered transport pin passed daemon identity proof")
	}
	if err := ValidatePairingPayload(payload, pairing.ExpiresAt.Add(time.Second)); err == nil {
		t.Fatal("expired Pairing V2 payload was accepted")
	}
}
