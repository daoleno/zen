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

func TestDeploymentProofIsIndependentAndBound(t *testing.T) {
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pin := strings.Repeat("ab", 32)
	proof := manager.SignDesktopDeploymentProof(pin, true, true)
	if !VerifyDesktopDeploymentProof(manager.PublicKeyHex(), manager.DaemonID(), pin, true, true, proof) {
		t.Fatal("valid trusted deployment proof rejected")
	}
	if VerifyDesktopDeploymentProof(manager.PublicKeyHex(), manager.DaemonID(), pin, true, false, proof) {
		t.Fatal("deployment flag bit was not bound")
	}
	if VerifyDesktopDeploymentProof(manager.PublicKeyHex(), manager.DaemonID(), strings.Repeat("cd", 32), true, true, proof) {
		t.Fatal("deployment proof accepted for another pin")
	}
	if VerifyDesktopCapabilitySignature(manager.PublicKeyHex(), manager.DaemonID(), pin, true, proof) {
		t.Fatal("deployment proof accepted as a v1 capability signature")
	}
}
