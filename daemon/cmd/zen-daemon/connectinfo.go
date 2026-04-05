package main

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/mdp/qrterminal/v3"
)

const (
	startupModeLocalOnly  = "LOCAL-ONLY"
	startupModePairable   = "PAIRABLE"
	connectParamPayload   = "p"
	connectPayloadVersion = 1
	connectPublicKeyBytes = 32
	connectTokenBytes     = 32
)

type connectionOffer struct {
	Label       string
	URL         string
	ConnectLink string
}

func buildConnectionOffers(advertiseURL string, authManager *auth.Manager, pairing auth.PairingToken) ([]connectionOffer, error) {
	if strings.TrimSpace(advertiseURL) == "" {
		return nil, nil
	}

	normalizedURL, err := normalizeAdvertiseURL(advertiseURL)
	if err != nil {
		return nil, err
	}

	offer := connectionOffer{
		Label: "Advertised endpoint",
		URL:   normalizedURL,
	}
	offer.ConnectLink = buildConnectLink(offer.URL, authManager, pairing)
	return []connectionOffer{offer}, nil
}

func printStartupBanner(w io.Writer, listenAddr, daemonID, mode string) {
	fmt.Fprintln(w, "╔══════════════════════════════════════╗")
	fmt.Fprintln(w, "║         zen-daemon v0.1.0            ║")
	fmt.Fprintln(w, "╠══════════════════════════════════════╣")
	fmt.Fprintf(w, "║  Listening on %-22s ║\n", listenAddr)
	fmt.Fprintf(w, "║  Auth: %-28s ║\n", "device identity")
	fmt.Fprintf(w, "║  Mode: %-28s ║\n", mode)
	fmt.Fprintln(w, "╠══════════════════════════════════════╣")
	fmt.Fprintf(w, "║  Daemon ID: %-23s ║\n", daemonID[:23])
	fmt.Fprintln(w, "╚══════════════════════════════════════╝")
}

func printLocalOnlyInfo(w io.Writer, stateDir string) {
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "State: LOCAL-ONLY")
	fmt.Fprintln(w, "The daemon is running and has a stable identity, but it is not pairable from mobile yet.")
	fmt.Fprintln(w, "Next step:")
	fmt.Fprintln(w, "  1. Expose this daemon through your tunnel or private network.")
	fmt.Fprintln(w, "  2. Generate a pairing link without restarting:")
	fmt.Fprintf(w, "     %s\n", pairCommandExample(stateDir))
}

func printPairingInfo(w io.Writer, offers []connectionOffer) {
	if len(offers) == 0 {
		printLocalOnlyInfo(w, "")
		return
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "State: PAIRABLE")
	for _, offer := range offers {
		fmt.Fprintf(w, "  - %s\n", offer.Label)
		fmt.Fprintf(w, "    URL:  %s\n", offer.URL)
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Paste this link into Settings -> Pair Server:")
	fmt.Fprintln(w, offers[0].ConnectLink)

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Scan on your phone to pair this device:")
	renderPairingQR(w, offers[0].ConnectLink)
}

func printPairCommandInfo(w io.Writer, daemonID string, offers []connectionOffer) {
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Generated a fresh pairing link for the existing daemon identity.")
	fmt.Fprintf(w, "Daemon ID: %s\n", daemonID)
	printPairingInfo(w, offers)
}

func pairCommandExample(stateDir string) string {
	parts := []string{"zen-daemon", "pair", "-advertise-url", "https://your-host/ws"}
	if strings.TrimSpace(stateDir) != "" {
		parts = append(parts, "-state-dir", stateDir)
	}
	return strings.Join(parts, " ")
}

func normalizeAdvertiseURL(rawValue string) (string, error) {
	trimmed := strings.TrimSpace(rawValue)
	if trimmed == "" {
		return "", fmt.Errorf("advertise URL is empty")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse advertise URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("advertise URL must include scheme and host")
	}

	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported advertise URL scheme %q", parsed.Scheme)
	}

	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/ws"
	}
	parsed.Fragment = ""
	return parsed.String(), nil
}

func buildConnectLink(serverURL string, authManager *auth.Manager, pairing auth.PairingToken) string {
	payload, err := encodeConnectPayload(serverURL, authManager.PublicKeyHex(), pairing.Value)
	if err != nil {
		return "zen://settings"
	}
	params := url.Values{}
	params.Set(connectParamPayload, payload)
	return "zen://settings?" + params.Encode()
}

func renderPairingQR(w io.Writer, link string) {
	qrterminal.GenerateWithConfig(link, qrterminal.Config{
		Level:          qrterminal.L,
		Writer:         w,
		HalfBlocks:     true,
		BlackChar:      qrterminal.BLACK_BLACK,
		WhiteBlackChar: qrterminal.WHITE_BLACK,
		WhiteChar:      qrterminal.WHITE_WHITE,
		BlackWhiteChar: qrterminal.BLACK_WHITE,
		QuietZone:      1,
	})
}

func encodeConnectPayload(serverURL, daemonPublicKeyHex, enrollmentTokenHex string) (string, error) {
	urlBytes := []byte(strings.TrimSpace(serverURL))
	if len(urlBytes) == 0 {
		return "", fmt.Errorf("server URL is empty")
	}
	if len(urlBytes) > 0xffff {
		return "", fmt.Errorf("server URL is too long")
	}

	publicKey, err := hex.DecodeString(strings.TrimSpace(daemonPublicKeyHex))
	if err != nil {
		return "", fmt.Errorf("decode daemon public key: %w", err)
	}
	if len(publicKey) != connectPublicKeyBytes {
		return "", fmt.Errorf("daemon public key must be %d bytes", connectPublicKeyBytes)
	}

	token, err := hex.DecodeString(strings.TrimSpace(enrollmentTokenHex))
	if err != nil {
		return "", fmt.Errorf("decode enrollment token: %w", err)
	}
	if len(token) != connectTokenBytes {
		return "", fmt.Errorf("enrollment token must be %d bytes", connectTokenBytes)
	}

	payload := make([]byte, 1+2+len(urlBytes)+len(publicKey)+len(token))
	payload[0] = connectPayloadVersion
	binary.BigEndian.PutUint16(payload[1:3], uint16(len(urlBytes)))

	offset := 3
	copy(payload[offset:], urlBytes)
	offset += len(urlBytes)
	copy(payload[offset:], publicKey)
	offset += len(publicKey)
	copy(payload[offset:], token)

	return base64.RawURLEncoding.EncodeToString(payload), nil
}
