package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Secret holds the pairing secret used for HMAC authentication.
type Secret struct {
	key []byte
}

// GenerateSecret creates a new random 32-byte secret.
func GenerateSecret() (*Secret, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generating secret: %w", err)
	}
	return &Secret{key: key}, nil
}

// LoadSecret loads a secret from a hex-encoded string.
func LoadSecret(hexKey string) (*Secret, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("decoding secret: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("secret must be 32 bytes, got %d", len(key))
	}
	return &Secret{key: key}, nil
}

// Hex returns the hex-encoded secret string.
func (s *Secret) Hex() string {
	return hex.EncodeToString(s.key)
}

// Sign creates an HMAC-SHA256 signature for the given message with a timestamp.
// Returns "timestamp:signature" string.
func (s *Secret) Sign(message []byte) string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(ts))
	mac.Write(message)
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return ts + ":" + sig
}

// Verify checks an HMAC-SHA256 signature. Token format: "timestamp:signature".
// Rejects tokens older than maxAge.
func (s *Secret) Verify(token string, message []byte, maxAge time.Duration) bool {
	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 {
		return false
	}

	ts, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false
	}

	// Check timestamp freshness.
	age := time.Since(time.Unix(ts, 0))
	if age < 0 || age > maxAge {
		return false
	}

	// Recompute HMAC.
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(parts[0]))
	mac.Write(message)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(parts[1]), []byte(expected))
}

// VerifyRaw checks a raw hex-encoded secret value.
func (s *Secret) VerifyRaw(hexKey string) bool {
	key, err := hex.DecodeString(strings.TrimSpace(hexKey))
	if err != nil {
		return false
	}
	return hmac.Equal(key, s.key)
}

// VerifyAuthorization checks an Authorization header value. Supports either
// a raw hex secret or the existing timestamp:signature token format.
func (s *Secret) VerifyAuthorization(value string, message []byte, maxAge time.Duration) bool {
	token := strings.TrimSpace(value)
	if token == "" {
		return false
	}

	lower := strings.ToLower(token)
	switch {
	case strings.HasPrefix(lower, "bearer "):
		token = strings.TrimSpace(token[len("bearer "):])
	case strings.HasPrefix(lower, "token "):
		token = strings.TrimSpace(token[len("token "):])
	}

	if token == "" {
		return false
	}

	if strings.Contains(token, ":") {
		return s.Verify(token, message, maxAge)
	}

	return s.VerifyRaw(token)
}
