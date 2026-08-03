package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
)

func TestProbeAuthCheckAndWebSocketBoundaryRequireIndependentNonces(t *testing.T) {
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	server := New(manager, nil, nil, nil, nil, nil, nil)

	authCheckHeader := probeAuthorizationHeader(
		t,
		privateKey,
		manager.DaemonID(),
		deviceID,
	)
	websocketHeader := probeAuthorizationHeader(
		t,
		privateKey,
		manager.DaemonID(),
		deviceID,
	)
	if authCheckHeader == websocketHeader {
		t.Fatal("fresh probe authorizations unexpectedly matched")
	}

	authCheck := httptest.NewRequest(http.MethodGet, "/auth-check", nil)
	authCheck.Header.Set("Authorization", authCheckHeader)
	authCheckResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(authCheckResponse, authCheck)
	if authCheckResponse.Code != http.StatusOK {
		t.Fatalf(
			"auth-check status=%d body=%q",
			authCheckResponse.Code,
			authCheckResponse.Body.String(),
		)
	}

	websocketProbe := httptest.NewRequest(http.MethodGet, "/ws", nil)
	websocketProbe.Header.Set("Authorization", websocketHeader)
	websocketResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(websocketResponse, websocketProbe)
	if websocketResponse.Code != http.StatusOK {
		t.Fatalf(
			"websocket probe status=%d body=%q",
			websocketResponse.Code,
			websocketResponse.Body.String(),
		)
	}

	replay := httptest.NewRequest(http.MethodGet, "/ws", nil)
	replay.Header.Set("Authorization", authCheckHeader)
	replayResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(replayResponse, replay)
	if replayResponse.Code != http.StatusUnauthorized {
		t.Fatalf(
			"replayed probe status=%d body=%q",
			replayResponse.Code,
			replayResponse.Body.String(),
		)
	}
}

func probeAuthorizationHeader(
	t *testing.T,
	privateKey ed25519.PrivateKey,
	daemonID string,
	deviceID string,
) string {
	t.Helper()
	nonce := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		t.Fatal(err)
	}
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	nonceHex := hex.EncodeToString(nonce)
	signature := ed25519.Sign(
		privateKey,
		auth.BuildSignaturePayload(
			"zen-probe",
			daemonID,
			deviceID,
			timestamp,
			nonceHex,
		),
	)
	return auth.AuthorizationHeaderPrefix +
		"v1:" +
		deviceID +
		":" +
		daemonID +
		":" +
		timestamp +
		":" +
		nonceHex +
		":" +
		hex.EncodeToString(signature)
}
