package auth

import (
	"testing"
)

func TestDesktopHostSignatureBindsCanonicalIdentityAndDomain(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"challenge":"one-use","generation":2}`)
	signature := m.SignDesktopHostAdmission(payload)
	if !VerifyDesktopHostAdmission(m.DaemonID(), m.PublicKeyHex(), payload, signature) {
		t.Fatal("valid admission rejected")
	}
	for _, signature := range []string{"", m.CreateLinkSignature(payload), m.CreateLinkPairingSignature(payload)} {
		if VerifyDesktopHostAdmission(m.DaemonID(), m.PublicKeyHex(), payload, signature) {
			t.Fatal("signature domain crossed")
		}
	}
	if VerifyDesktopHostAdmission("other", m.PublicKeyHex(), payload, signature) || VerifyDesktopHostAdmission(m.DaemonID(), m.PublicKeyHex(), append(payload, ' '), signature) {
		t.Fatal("signature binding changed")
	}
}
