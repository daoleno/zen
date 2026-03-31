package main

import (
	"net"
	"net/url"
	"testing"

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

func TestBuildConnectLink(t *testing.T) {
	secret, err := auth.LoadSecret("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("LoadSecret returned error: %v", err)
	}

	rawLink := buildConnectLink("local-lan", "192.168.1.10", "lab-box", secret)
	parsed, err := url.Parse(rawLink)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if parsed.Scheme != "zen" {
		t.Fatalf("unexpected scheme: %s", parsed.Scheme)
	}
	if got := parsed.Query().Get("provider"); got != "local-lan" {
		t.Fatalf("unexpected provider: %s", got)
	}
	if got := parsed.Query().Get("endpoint"); got != "192.168.1.10" {
		t.Fatalf("unexpected endpoint: %s", got)
	}
	if got := parsed.Query().Get("name"); got != "lab-box" {
		t.Fatalf("unexpected name: %s", got)
	}
	if got := parsed.Query().Get("secret"); got != secret.Hex() {
		t.Fatalf("unexpected secret: %s", got)
	}
}

func TestBuildLANURL(t *testing.T) {
	if got := buildLANURL("192.168.1.10", defaultDaemonPort); got != "ws://192.168.1.10/ws" {
		t.Fatalf("unexpected default port URL: %s", got)
	}
	if got := buildLANURL("192.168.1.10", "7777"); got != "ws://192.168.1.10:7777/ws" {
		t.Fatalf("unexpected custom port URL: %s", got)
	}
}

func TestListenPort(t *testing.T) {
	if got := listenPort(":9876"); got != "9876" {
		t.Fatalf("unexpected port for wildcard listen: %s", got)
	}
	if got := listenPort("0.0.0.0:7777"); got != "7777" {
		t.Fatalf("unexpected port for explicit listen: %s", got)
	}
}

func TestIsDirectReachableIPv4(t *testing.T) {
	testCases := []struct {
		name string
		ip   string
		want bool
	}{
		{name: "lan", ip: "192.168.1.10", want: true},
		{name: "tailscale", ip: "100.101.102.103", want: true},
		{name: "public", ip: "8.8.8.8", want: false},
	}

	for _, testCase := range testCases {
		ip := net.ParseIP(testCase.ip).To4()
		if got := isDirectReachableIPv4(ip); got != testCase.want {
			t.Fatalf("%s: got %v want %v", testCase.name, got, testCase.want)
		}
	}
}
