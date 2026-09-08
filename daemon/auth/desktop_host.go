package auth

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
)

// SignDesktopHostAdmission is deliberately domain-separated from network auth,
// enrollment and Link signatures. Callers must verify the current scoped device
// and real TLS before signing the broker's one-use session challenge.
func (m *Manager) SignDesktopHostAdmission(payload []byte) string {
	return hex.EncodeToString(ed25519.Sign(m.privateKey, desktopHostPayload(payload)))
}

func VerifyDesktopHostAdmission(hostID, publicKey string, payload []byte, signature string) bool {
	key, err := decodeFixedHex(publicKey, ed25519.PublicKeySize)
	if err != nil {
		return false
	}
	fingerprint := sha256.Sum256(key)
	if hex.EncodeToString(fingerprint[:]) != hostID {
		return false
	}
	sig, err := decodeFixedHex(signature, ed25519.SignatureSize)
	return err == nil && ed25519.Verify(ed25519.PublicKey(key), desktopHostPayload(payload), sig)
}

func desktopHostPayload(payload []byte) []byte {
	return append([]byte("zen-desktop-host-admission-v1\x00"), payload...)
}
