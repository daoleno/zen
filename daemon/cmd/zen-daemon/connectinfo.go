package main

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/mdp/qrterminal/v3"
)

const defaultDaemonPort = "9876"

var carrierGradeNATRange = mustCIDR("100.64.0.0/10")

type connectionOffer struct {
	Label       string
	Provider    string
	Endpoint    string
	URL         string
	ConnectLink string
}

func buildConnectionOffers(listenAddr string, advertiseURL string, secret *auth.Secret) ([]connectionOffer, error) {
	serverName := defaultServerName()
	if strings.TrimSpace(advertiseURL) != "" {
		normalizedURL, err := normalizeAdvertiseURL(advertiseURL)
		if err != nil {
			return nil, err
		}

		offer := connectionOffer{
			Label:    "Advertised endpoint",
			Provider: "custom-endpoint",
			Endpoint: normalizedURL,
			URL:      normalizedURL,
		}
		offer.ConnectLink = buildConnectLink(offer.Provider, offer.Endpoint, serverName, secret)
		return []connectionOffer{offer}, nil
	}

	port := listenPort(listenAddr)
	ips := privateIPv4s()
	offers := make([]connectionOffer, 0, len(ips))
	for _, ip := range ips {
		endpoint := ip
		if port != defaultDaemonPort {
			endpoint = net.JoinHostPort(ip, port)
		}

		offer := connectionOffer{
			Label:    fmt.Sprintf("Direct %s", ip),
			Provider: "local-lan",
			Endpoint: endpoint,
			URL:      buildLANURL(ip, port),
		}
		offer.ConnectLink = buildConnectLink(offer.Provider, offer.Endpoint, serverName, secret)
		offers = append(offers, offer)
	}

	return offers, nil
}

func printConnectionInfo(w io.Writer, offers []connectionOffer) {
	if len(offers) == 0 {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "No reachable LAN endpoint detected.")
		fmt.Fprintln(w, "Use -advertise-url with your own public or tunneled WebSocket URL.")
		return
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Connection offers:")
	for _, offer := range offers {
		fmt.Fprintf(w, "  - %s\n", offer.Label)
		fmt.Fprintf(w, "    URL:  %s\n", offer.URL)
		fmt.Fprintf(w, "    Link: %s\n", offer.ConnectLink)
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Scan on your phone to import the primary endpoint:")
	qrterminal.GenerateHalfBlock(offers[0].ConnectLink, qrterminal.L, w)
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

func buildLANURL(host string, port string) string {
	endpoint := host
	if port != defaultDaemonPort {
		endpoint = net.JoinHostPort(host, port)
	}
	return fmt.Sprintf("ws://%s/ws", endpoint)
}

func buildConnectLink(provider string, endpoint string, name string, secret *auth.Secret) string {
	params := url.Values{}
	params.Set("provider", provider)
	params.Set("endpoint", endpoint)
	if name != "" {
		params.Set("name", name)
	}
	if secret != nil {
		params.Set("secret", secret.Hex())
	}
	return "zen://settings?" + params.Encode()
}

func listenPort(listenAddr string) string {
	trimmed := strings.TrimSpace(listenAddr)
	if trimmed == "" {
		return defaultDaemonPort
	}
	if strings.HasPrefix(trimmed, ":") {
		return strings.TrimPrefix(trimmed, ":")
	}
	if _, port, err := net.SplitHostPort(trimmed); err == nil && port != "" {
		return port
	}
	return defaultDaemonPort
}

func privateIPv4s() []string {
	var addresses []string

	ifaces, err := net.Interfaces()
	if err != nil {
		return addresses
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ip := extractIP(addr)
			if ip == nil {
				continue
			}

			ip = ip.To4()
			if ip == nil || !isDirectReachableIPv4(ip) {
				continue
			}
			addresses = append(addresses, ip.String())
		}
	}

	sort.Strings(addresses)
	return dedupeStrings(addresses)
}

func extractIP(addr net.Addr) net.IP {
	switch value := addr.(type) {
	case *net.IPNet:
		return value.IP
	case *net.IPAddr:
		return value.IP
	default:
		return nil
	}
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}

	seen := make(map[string]struct{}, len(values))
	deduped := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		deduped = append(deduped, value)
	}
	return deduped
}

func isDirectReachableIPv4(ip net.IP) bool {
	return ip.IsPrivate() || carrierGradeNATRange.Contains(ip)
}

func mustCIDR(value string) *net.IPNet {
	_, network, err := net.ParseCIDR(value)
	if err != nil {
		panic(err)
	}
	return network
}

func defaultServerName() string {
	hostname, err := os.Hostname()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(hostname)
}
