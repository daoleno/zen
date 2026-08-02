package main

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/url"
	"sort"
	"strings"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/mdp/qrterminal/v3"
)

const (
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

type privateNetworkAddress struct {
	label string
	ip    net.IP
}

func buildConnectionOffers(endpoint string, authManager *auth.Manager, pairing auth.PairingToken) ([]connectionOffer, error) {
	return buildConnectionOffersWithPublicKey(
		endpoint,
		authManager.PublicKeyHex(),
		pairing,
	)
}

func buildConnectionOffersWithPublicKey(
	endpoint string,
	daemonPublicKey string,
	pairing auth.PairingToken,
) ([]connectionOffer, error) {
	if strings.TrimSpace(endpoint) == "" {
		return nil, nil
	}

	normalizedURL, err := normalizeEndpoint(endpoint)
	if err != nil {
		return nil, err
	}

	offer := connectionOffer{
		Label: "Server endpoint",
		URL:   normalizedURL,
	}
	offer.ConnectLink = buildConnectLinkWithPublicKey(
		offer.URL,
		daemonPublicKey,
		pairing,
	)
	return []connectionOffer{offer}, nil
}

func printStartupInfo(w io.Writer, listenAddr, stateDir string, addresses []privateNetworkAddress) {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		fmt.Fprintf(w, "Zen is listening on %s.\n", listenAddr)
		fmt.Fprintln(w, "Run zen pair with an HTTPS endpoint that forwards the full daemon origin.")
		return
	}

	if isLoopbackHost(host) {
		fmt.Fprintf(w, "Zen is ready in local-only mode on http://%s.\n", listenAddr)
		fmt.Fprintln(w, "To connect your phone:")
		fmt.Fprintln(w, "  - Same trusted Wi-Fi or Tailnet: restart with zen --lan")
		fmt.Fprintf(w, "  - HTTPS endpoint: expose http://%s, then in another terminal run:\n", listenAddr)
		fmt.Fprintf(w, "      %s\n", pairCommand(stateDir, "https://your-zen-host.example"))
		return
	}

	usable := startupPairingAddresses(host, addresses)
	if isWildcardHost(host) || len(usable) > 0 {
		fmt.Fprintln(w, "Zen is ready for trusted private-network access.")
		if len(usable) == 0 {
			fmt.Fprintln(w, "No LAN or Tailscale address was detected. Check your network, then restart Zen.")
			return
		}
		fmt.Fprintln(w, "In another terminal, run a pairing command for the network your phone uses:")
		for _, address := range usable {
			endpoint := "http://" + net.JoinHostPort(address.ip.String(), port)
			fmt.Fprintf(w, "  - %s: %s\n", address.label, pairCommand(stateDir, endpoint))
		}
		fmt.Fprintln(w, "Use HTTP only on a trusted private network.")
		return
	}

	fmt.Fprintf(w, "Zen is listening on %s.\n", listenAddr)
	fmt.Fprintln(w, "In another terminal, run zen pair with the private or HTTPS address your phone can reach.")
}

func printPairingInfo(w io.Writer, offers []connectionOffer) {
	if len(offers) == 0 {
		return
	}

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

func pairCommand(stateDir, endpoint string) string {
	parts := []string{"zen", "pair"}
	if strings.TrimSpace(stateDir) != "" {
		parts = append(parts, "-state-dir", stateDir)
	}
	parts = append(parts, endpoint)
	return strings.Join(parts, " ")
}

func detectPrivateNetworkAddresses() []privateNetworkAddress {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var detected []privateNetworkAddress
	seen := make(map[string]bool)
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if shouldSkipPrivateNetworkInterface(iface.Name) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err != nil || ip.To4() == nil {
				continue
			}
			ip = ip.To4()
			label := ""
			switch {
			case isTailscaleAddress(iface.Name, ip):
				label = "Tailscale"
			case ip.IsPrivate():
				label = "Same Wi-Fi/LAN"
			}
			key := ip.String()
			if label != "" && !seen[key] {
				seen[key] = true
				detected = append(detected, privateNetworkAddress{label: label, ip: ip})
			}
		}
	}
	sort.Slice(detected, func(i, j int) bool {
		if detected[i].label != detected[j].label {
			return detected[i].label < detected[j].label
		}
		return bytesCompare(detected[i].ip, detected[j].ip) < 0
	})
	return detected
}

func shouldSkipPrivateNetworkInterface(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range []string{"docker", "br-", "veth", "virbr", "cni", "podman", "kube"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func startupPairingAddresses(host string, detected []privateNetworkAddress) []privateNetworkAddress {
	if isWildcardHost(host) {
		return detected
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || ip.IsLoopback() {
		return nil
	}
	for _, address := range detected {
		if address.ip.Equal(ip) {
			return []privateNetworkAddress{address}
		}
	}
	if ip.To4() != nil && ip.IsPrivate() {
		return []privateNetworkAddress{{label: "Private network", ip: ip.To4()}}
	}
	return nil
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}

func isWildcardHost(host string) bool {
	host = strings.Trim(host, "[]")
	return host == "" || host == "0.0.0.0" || host == "::"
}

func isTailscaleAddress(interfaceName string, ip net.IP) bool {
	if strings.HasPrefix(strings.ToLower(interfaceName), "tailscale") {
		return true
	}
	ip4 := ip.To4()
	return ip4 != nil && ip4[0] == 100 && ip4[1]&0xc0 == 0x40
}

func bytesCompare(left, right net.IP) int {
	for index := 0; index < len(left) && index < len(right); index++ {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return len(left) - len(right)
}

func normalizeEndpoint(rawValue string) (string, error) {
	trimmed := strings.TrimSpace(rawValue)
	if trimmed == "" {
		return "", fmt.Errorf("endpoint is empty")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("endpoint must include scheme and host")
	}

	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported endpoint scheme %q", parsed.Scheme)
	}

	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/ws"
	}
	parsed.Fragment = ""
	return parsed.String(), nil
}

func buildConnectLink(serverURL string, authManager *auth.Manager, pairing auth.PairingToken) string {
	return buildConnectLinkWithPublicKey(
		serverURL,
		authManager.PublicKeyHex(),
		pairing,
	)
}

func buildConnectLinkWithPublicKey(
	serverURL string,
	daemonPublicKey string,
	pairing auth.PairingToken,
) string {
	payload, err := encodeConnectPayload(
		serverURL,
		daemonPublicKey,
		pairing.Value,
	)
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
