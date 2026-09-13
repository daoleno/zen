package main

import "testing"

func TestDesktopTrustedBindDocumentedOrigins(t *testing.T) {
	for _, trusted := range []string{"127.0.0.1:9876", "[::1]:9876", "10.0.0.9:9876", "100.101.102.103:9876"} {
		if !desktopTrustedBind(trusted) {
			t.Fatalf("%s should be a trusted documented origin", trusted)
		}
	}
	for _, untrusted := range []string{"0.0.0.0:9876", ":9876", "203.0.113.9:9876", "8.8.8.8:9876"} {
		if desktopTrustedBind(untrusted) {
			t.Fatalf("%s must stay untrusted", untrusted)
		}
	}
}
