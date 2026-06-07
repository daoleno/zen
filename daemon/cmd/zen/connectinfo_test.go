package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"flag"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
)

func TestNormalizeAdvertiseURL(t *testing.T) {
	value, err := normalizeAdvertiseURL("https://zen.example.com")
	if err != nil {
		t.Fatalf("normalizeAdvertiseURL returned error: %v", err)
	}
	if value != "wss://zen.example.com/ws" {
		t.Fatalf("unexpected normalized URL: %s", value)
	}
}

func TestNormalizeAdvertiseURLRejectsMissingScheme(t *testing.T) {
	if _, err := normalizeAdvertiseURL("zen.example.com"); err == nil {
		t.Fatal("expected error for missing scheme")
	}
}

func TestBuildConnectLinkIncludesDaemonIdentity(t *testing.T) {
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	pairing := auth.PairingToken{
		Value:     "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ExpiresAt: time.Date(2026, 4, 5, 8, 0, 0, 0, time.UTC),
	}

	rawLink := buildConnectLink("wss://zen.example.com/ws", manager, pairing)
	parsed, err := url.Parse(rawLink)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if parsed.Scheme != "zen" {
		t.Fatalf("unexpected scheme: %s", parsed.Scheme)
	}
	payloadValue := parsed.Query().Get(connectParamPayload)
	if payloadValue == "" {
		t.Fatal("expected compact payload query param")
	}

	payload, err := base64.RawURLEncoding.DecodeString(payloadValue)
	if err != nil {
		t.Fatalf("DecodeString returned error: %v", err)
	}
	if len(payload) != 1+2+len("wss://zen.example.com/ws")+connectPublicKeyBytes+connectTokenBytes {
		t.Fatalf("unexpected payload size: %d", len(payload))
	}
	if payload[0] != connectPayloadVersion {
		t.Fatalf("unexpected payload version: %d", payload[0])
	}

	urlLength := int(binary.BigEndian.Uint16(payload[1:3]))
	offset := 3
	gotURL := string(payload[offset : offset+urlLength])
	offset += urlLength
	gotPublicKey := hex.EncodeToString(payload[offset : offset+connectPublicKeyBytes])
	offset += connectPublicKeyBytes
	gotToken := hex.EncodeToString(payload[offset : offset+connectTokenBytes])

	if gotURL != "wss://zen.example.com/ws" {
		t.Fatalf("unexpected url: %s", gotURL)
	}
	if gotPublicKey != manager.PublicKeyHex() {
		t.Fatalf("unexpected daemon public key: %s", gotPublicKey)
	}
	if gotToken != pairing.Value {
		t.Fatalf("unexpected enrollment token: %s", gotToken)
	}
}

func TestBuildConnectionOffersUsesAdvertiseURL(t *testing.T) {
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	pairing := auth.PairingToken{
		Value:     "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ExpiresAt: time.Date(2026, 4, 5, 8, 0, 0, 0, time.UTC),
	}

	offers, err := buildConnectionOffers("https://zen.example.com/gateway", manager, pairing)
	if err != nil {
		t.Fatalf("buildConnectionOffers returned error: %v", err)
	}
	if len(offers) != 1 {
		t.Fatalf("expected one offer, got %d", len(offers))
	}
	if offers[0].URL != "wss://zen.example.com/gateway" {
		t.Fatalf("unexpected offer URL: %s", offers[0].URL)
	}

	parsed, err := url.Parse(offers[0].ConnectLink)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if got := parsed.Query().Get(connectParamPayload); got == "" {
		t.Fatal("expected compact payload query param")
	}
}

func TestPrintLocalOnlyInfo(t *testing.T) {
	var output bytes.Buffer
	printLocalOnlyInfo(&output, "/tmp/zen-state")

	rendered := output.String()
	if !strings.Contains(rendered, "State: LOCAL-ONLY") {
		t.Fatalf("expected LOCAL-ONLY output, got %q", rendered)
	}
	if !strings.Contains(rendered, "zen pair -advertise-url https://your-host/ws -state-dir /tmp/zen-state") {
		t.Fatalf("expected pair command example, got %q", rendered)
	}
}

func TestPrintPairingInfo(t *testing.T) {
	var output bytes.Buffer
	printPairingInfo(&output, []connectionOffer{{
		Label:       "Advertised endpoint",
		URL:         "wss://zen.example.com/ws",
		ConnectLink: "zen://settings?p=compact-payload",
	}})

	rendered := output.String()
	if !strings.Contains(rendered, "State: PAIRABLE") {
		t.Fatalf("expected PAIRABLE output, got %q", rendered)
	}
	if !strings.Contains(rendered, "Paste this link into Settings -> Pair Server:") {
		t.Fatalf("expected pair instruction, got %q", rendered)
	}
	if !strings.Contains(rendered, "zen://settings?p=compact-payload") {
		t.Fatalf("expected connect link, got %q", rendered)
	}
}

func TestTopLevelHelpIncludesAgentAndBrainCommands(t *testing.T) {
	var output bytes.Buffer
	_, err := parseDaemonConfig([]string{"--help"}, &output)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("parseDaemonConfig error = %v, want ErrHelp", err)
	}
	rendered := output.String()
	for _, want := range []string{
		"agent      List, spawn, inspect, message, and close agent sessions",
		"brain      Inspect Brain workspace and host adapter configuration",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("top-level help missing %q:\n%s", want, rendered)
		}
	}
}

func TestAgentAndBrainHelpAreDiscoverable(t *testing.T) {
	var agentOutput bytes.Buffer
	if err := runAgentCommand([]string{"--help"}, &agentOutput); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("runAgentCommand error = %v, want ErrHelp", err)
	}
	agentHelp := agentOutput.String()
	for _, want := range []string{
		"Usage: zen agent <list|spawn|send|capture|close|kill> [flags]",
		"zen agent spawn -name",
		"zen agent capture -id",
		"zen agent close -id",
	} {
		if !strings.Contains(agentHelp, want) {
			t.Fatalf("agent help missing %q:\n%s", want, agentHelp)
		}
	}

	var brainOutput bytes.Buffer
	if err := runBrainCommand([]string{"--help"}, &brainOutput); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("runBrainCommand error = %v, want ErrHelp", err)
	}
	brainHelp := brainOutput.String()
	for _, want := range []string{
		"Usage: zen brain <workspace|adapters|use> [flags]",
		"zen brain workspace --json",
		"zen brain adapters --json",
	} {
		if !strings.Contains(brainHelp, want) {
			t.Fatalf("brain help missing %q:\n%s", want, brainHelp)
		}
	}
}
