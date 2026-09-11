package auth

import (
	"crypto/ed25519"
	"encoding/hex"
	"strings"
)

const DesktopCapabilityPurpose = "zen-desktop-capability"

// BuildDesktopCapabilityPayload binds the identity TLS pin to the paired daemon.
// Host readiness is not included; clients must not treat unsigned JSON as encryption.
func BuildDesktopCapabilityPayload(daemonID, publicKeyHex, pin string, identityTLS bool) []byte {
	flag := "false"
	if identityTLS {
		flag = "true"
	}
	return []byte(strings.Join([]string{
		normalizeHex(daemonID),
		normalizeHex(publicKeyHex),
		normalizeHex(pin),
		flag,
	}, "\n"))
}

func (m *Manager) SignDesktopCapability(pin string, identityTLS bool) string {
	if !identityTLS {
		pin = ""
	}
	signature := ed25519.Sign(m.privateKey, desktopCapabilitySignaturePayload(
		BuildDesktopCapabilityPayload(m.daemonID, m.PublicKeyHex(), pin, identityTLS),
	))
	return hex.EncodeToString(signature)
}

func VerifyDesktopCapabilitySignature(publicKeyHex, daemonID, pin string, identityTLS bool, signatureHex string) bool {
	publicKey, err := decodeFixedHex(publicKeyHex, ed25519.PublicKeySize)
	if err != nil {
		return false
	}
	signature, err := decodeFixedHex(signatureHex, ed25519.SignatureSize)
	if err != nil {
		return false
	}
	return ed25519.Verify(
		ed25519.PublicKey(publicKey),
		desktopCapabilitySignaturePayload(BuildDesktopCapabilityPayload(daemonID, publicKeyHex, pin, identityTLS)),
		signature,
	)
}

func desktopCapabilitySignaturePayload(payload []byte) []byte {
	const domain = "zen-desktop-capability-v1\x00"
	signed := make([]byte, 0, len(domain)+len(payload))
	signed = append(signed, domain...)
	signed = append(signed, payload...)
	return signed
}
