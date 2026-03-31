package auth

import (
	"testing"
	"time"
)

func TestGenerateAndVerify(t *testing.T) {
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}

	msg := []byte("hello world")
	token := secret.Sign(msg)

	if !secret.Verify(token, msg, 5*time.Minute) {
		t.Error("valid token should verify")
	}
}

func TestVerifyWrongMessage(t *testing.T) {
	secret, _ := GenerateSecret()
	token := secret.Sign([]byte("hello"))
	if secret.Verify(token, []byte("world"), 5*time.Minute) {
		t.Error("wrong message should not verify")
	}
}

func TestVerifyWrongSecret(t *testing.T) {
	s1, _ := GenerateSecret()
	s2, _ := GenerateSecret()
	token := s1.Sign([]byte("test"))
	if s2.Verify(token, []byte("test"), 5*time.Minute) {
		t.Error("different secret should not verify")
	}
}

func TestVerifyExpired(t *testing.T) {
	secret, _ := GenerateSecret()
	token := secret.Sign([]byte("test"))
	// With a 0 maxAge, any token is expired.
	if secret.Verify(token, []byte("test"), 0) {
		t.Error("expired token should not verify")
	}
}

func TestVerifyBadFormat(t *testing.T) {
	secret, _ := GenerateSecret()
	if secret.Verify("not-a-valid-token", []byte("test"), 5*time.Minute) {
		t.Error("bad format should not verify")
	}
	if secret.Verify("abc:def", []byte("test"), 5*time.Minute) {
		t.Error("non-numeric timestamp should not verify")
	}
}

func TestLoadSecret(t *testing.T) {
	s1, _ := GenerateSecret()
	s2, err := LoadSecret(s1.Hex())
	if err != nil {
		t.Fatal(err)
	}

	msg := []byte("roundtrip")
	token := s1.Sign(msg)
	if !s2.Verify(token, msg, 5*time.Minute) {
		t.Error("loaded secret should verify tokens from original")
	}
}

func TestPairingCode(t *testing.T) {
	secret, _ := GenerateSecret()
	code := secret.PairingCode()
	if len(code) != 6 {
		t.Errorf("pairing code should be 6 digits, got %q", code)
	}
}

func TestVerifyRaw(t *testing.T) {
	secret, _ := GenerateSecret()
	if !secret.VerifyRaw(secret.Hex()) {
		t.Error("raw secret should verify")
	}
	if secret.VerifyRaw("deadbeef") {
		t.Error("wrong raw secret should not verify")
	}
}

func TestVerifyAuthorization(t *testing.T) {
	secret, _ := GenerateSecret()
	message := []byte("zen-connect")

	if !secret.VerifyAuthorization("Bearer "+secret.Hex(), message, 5*time.Minute) {
		t.Error("bearer raw secret should verify")
	}

	token := secret.Sign(message)
	if !secret.VerifyAuthorization("Bearer "+token, message, 5*time.Minute) {
		t.Error("bearer signed token should verify")
	}

	if secret.VerifyAuthorization("Bearer wrong", message, 5*time.Minute) {
		t.Error("invalid authorization header should not verify")
	}
}
