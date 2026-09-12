package auth

import (
	"strings"
	"testing"
)

func TestDesktopCapabilitySignatureBindsPinAndRejectsForgery(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pin := strings.Repeat("ab", 32)
	sig := m.SignDesktopCapability(pin, true)
	if !VerifyDesktopCapabilitySignature(m.PublicKeyHex(), m.DaemonID(), pin, true, sig) {
		t.Fatal("valid identity pin signature rejected")
	}
	if VerifyDesktopCapabilitySignature(m.PublicKeyHex(), m.DaemonID(), strings.Repeat("cd", 32), true, sig) {
		t.Fatal("retargeted pin accepted")
	}
	if VerifyDesktopCapabilitySignature(m.PublicKeyHex(), m.DaemonID(), pin, false, sig) {
		t.Fatal("identity TLS flag was not bound")
	}
	unsigned := m.SignDesktopCapability("", false)
	if !VerifyDesktopCapabilitySignature(m.PublicKeyHex(), m.DaemonID(), "", false, unsigned) {
		t.Fatal("absent identity TLS signature rejected")
	}
	if VerifyDesktopCapabilitySignature(m.PublicKeyHex(), m.DaemonID(), pin, true, unsigned) {
		t.Fatal("unsigned pin grant accepted")
	}

	v2 := m.SignDesktopCapabilityV2(pin, true, "true\n47989\n47984\n1\nhost\ndevice")
	if !VerifyDesktopCapabilitySignatureV2(m.PublicKeyHex(), m.DaemonID(), pin, true, "true\n47989\n47984\n1\nhost\ndevice", v2) {
		t.Fatal("valid v2 binding rejected")
	}
	if VerifyDesktopCapabilitySignatureV2(m.PublicKeyHex(), m.DaemonID(), pin, true, "true\n47989\n47984\n1\nEVIL\ndevice", v2) {
		t.Fatal("altered v2 binding accepted")
	}
	if !VerifyDesktopCapabilitySignature(m.PublicKeyHex(), m.DaemonID(), pin, true, sig) {
		t.Fatal("legacy v1 signature changed by v2 support")
	}
}
