package auth

import (
	"crypto/ed25519"
	"encoding/hex"
	"strings"
)

const DesktopCapabilityPurpose = "zen-desktop-capability"

// BuildDesktopCapabilityPayload is the immutable legacy v1 payload: changing it
// would invalidate the signature for already installed clients.
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

// BuildDesktopCapabilityPayloadV2 appends the canonical Moonlight binding to
// the exact legacy fields; v1 signatures remain valid and unchanged.
func BuildDesktopCapabilityPayloadV2(daemonID, publicKeyHex, pin string, identityTLS bool, moonlightBinding string) []byte {
	return append(BuildDesktopCapabilityPayload(daemonID, publicKeyHex, pin, identityTLS), []byte("\n"+strings.TrimSpace(moonlightBinding))...)
}

// SignDesktopCapability returns the legacy v1 signature. Moonlight bindings use
// SignDesktopCapabilityV2 so old clients can still verify ordinary desktop.
func (m *Manager) SignDesktopCapability(pin string, identityTLS bool) string {
	if !identityTLS {
		pin = ""
	}
	signature := ed25519.Sign(m.privateKey, desktopCapabilitySignaturePayloadV1(
		BuildDesktopCapabilityPayload(m.daemonID, m.PublicKeyHex(), pin, identityTLS),
	))
	return hex.EncodeToString(signature)
}

func (m *Manager) SignDesktopCapabilityV2(pin string, identityTLS bool, moonlightBinding string) string {
	if !identityTLS {
		pin = ""
	}
	signature := ed25519.Sign(m.privateKey, desktopCapabilitySignaturePayloadV2(
		BuildDesktopCapabilityPayloadV2(m.daemonID, m.PublicKeyHex(), pin, identityTLS, moonlightBinding),
	))
	return hex.EncodeToString(signature)
}

// VerifyDesktopCapabilitySignature verifies the legacy v1 signature only.
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
		desktopCapabilitySignaturePayloadV1(BuildDesktopCapabilityPayload(daemonID, publicKeyHex, pin, identityTLS)),
		signature,
	)
}

// VerifyDesktopCapabilitySignatureV2 verifies the domain-separated v2 signature
// that covers the canonical Moonlight binding.
func VerifyDesktopCapabilitySignatureV2(publicKeyHex, daemonID, pin string, identityTLS bool, moonlightBinding, signatureHex string) bool {
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
		desktopCapabilitySignaturePayloadV2(BuildDesktopCapabilityPayloadV2(daemonID, publicKeyHex, pin, identityTLS, moonlightBinding)),
		signature,
	)
}

func desktopCapabilitySignaturePayloadV1(payload []byte) []byte {
	const domain = "zen-desktop-capability-v1\x00"
	signed := make([]byte, 0, len(domain)+len(payload))
	signed = append(signed, domain...)
	signed = append(signed, payload...)
	return signed
}

func desktopCapabilitySignaturePayloadV2(payload []byte) []byte {
	const domain = "zen-desktop-capability-v2\x00"
	signed := make([]byte, 0, len(domain)+len(payload))
	signed = append(signed, domain...)
	signed = append(signed, payload...)
	return signed
}
