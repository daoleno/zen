package server

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestDeviceAdminAuthorizationBindsOperationAndRevokeTarget(t *testing.T) {
	manager, handler, actorKey := deviceAdminFixture(t)

	listAsDelete := deviceRequest(
		t,
		handler,
		http.MethodDelete,
		`{"device_id":"device-b"}`,
		deviceAdminAuthorization(
			t,
			actorKey,
			manager.DaemonID(),
			"device-admin",
			auth.DeviceListPurpose,
		),
	)
	if listAsDelete.Code != http.StatusUnauthorized {
		t.Fatalf(
			"list signature DELETE status=%d body=%q",
			listAsDelete.Code,
			listAsDelete.Body.String(),
		)
	}
	if !manager.IsDeviceTrusted("device-b") {
		t.Fatal("list signature revoked device B")
	}

	revokeBAsC := deviceRequest(
		t,
		handler,
		http.MethodDelete,
		`{"device_id":"device-c"}`,
		deviceAdminAuthorization(
			t,
			actorKey,
			manager.DaemonID(),
			"device-admin",
			auth.DeviceRevokePurpose("device-b"),
		),
	)
	if revokeBAsC.Code != http.StatusUnauthorized {
		t.Fatalf(
			"revoke-B signature for C status=%d body=%q",
			revokeBAsC.Code,
			revokeBAsC.Body.String(),
		)
	}
	if !manager.IsDeviceTrusted("device-c") {
		t.Fatal("revoke-B signature revoked device C")
	}

	revokeHeader := deviceAdminAuthorization(
		t,
		actorKey,
		manager.DaemonID(),
		"device-admin",
		auth.DeviceRevokePurpose("device-b"),
	)
	revokeB := deviceRequest(
		t,
		handler,
		http.MethodDelete,
		`{"device_id":"device-b"}`,
		revokeHeader,
	)
	if revokeB.Code != http.StatusOK {
		t.Fatalf(
			"valid revoke status=%d body=%q",
			revokeB.Code,
			revokeB.Body.String(),
		)
	}
	replayed := deviceRequest(
		t,
		handler,
		http.MethodDelete,
		`{"device_id":"device-b"}`,
		revokeHeader,
	)
	if replayed.Code != http.StatusUnauthorized ||
		!strings.Contains(replayed.Body.String(), auth.ErrReplayDetected.Error()) {
		t.Fatalf(
			"replayed revoke status=%d body=%q",
			replayed.Code,
			replayed.Body.String(),
		)
	}
	if manager.IsDeviceTrusted("device-b") ||
		!manager.IsDeviceTrusted("device-c") {
		t.Fatal("target-bound revoke changed the wrong trusted-device state")
	}
}

func TestDeviceAdminListOmitsPublicKeys(t *testing.T) {
	manager, handler, actorKey := deviceAdminFixture(t)
	response := deviceRequest(
		t,
		handler,
		http.MethodGet,
		"",
		deviceAdminAuthorization(
			t,
			actorKey,
			manager.DaemonID(),
			"device-admin",
			auth.DeviceListPurpose,
		),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%q", response.Code, response.Body.String())
	}
	if bytes.Contains(response.Body.Bytes(), []byte("public_key_hex")) {
		t.Fatalf("list leaked public key: %s", response.Body.String())
	}
	for _, deviceID := range []string{"device-admin", "device-b", "device-c"} {
		if !bytes.Contains(response.Body.Bytes(), []byte(deviceID)) {
			t.Fatalf("list omitted %s: %s", deviceID, response.Body.String())
		}
	}
}

func TestDeviceAdminMalformedTargetsUseStablePublicErrors(t *testing.T) {
	manager, handler, actorKey := deviceAdminFixture(t)
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid JSON", body: "{"},
		{name: "missing target", body: `{}`},
		{name: "blank target", body: `{"device_id":"   "}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := deviceRequest(
				t,
				handler,
				http.MethodDelete,
				test.body,
				deviceAdminAuthorization(
					t,
					actorKey,
					manager.DaemonID(),
					"device-admin",
					auth.DeviceRevokePurpose(""),
				),
			)
			assertDeviceAPIError(
				t,
				response,
				http.StatusBadRequest,
				"invalid_device",
			)
		})
	}

	missing := deviceRequest(
		t,
		handler,
		http.MethodDelete,
		`{"device_id":"does-not-exist"}`,
		deviceAdminAuthorization(
			t,
			actorKey,
			manager.DaemonID(),
			"device-admin",
			auth.DeviceRevokePurpose("does-not-exist"),
		),
	)
	assertDeviceAPIError(
		t,
		missing,
		http.StatusNotFound,
		"device_not_found",
	)
	if strings.Contains(missing.Body.String(), auth.ErrUnknownDevice.Error()) {
		t.Fatalf("missing target leaked internal error: %s", missing.Body.String())
	}
}

func deviceAdminFixture(
	t *testing.T,
) (*auth.Manager, http.Handler, ed25519.PrivateKey) {
	t.Helper()
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, actorKey := enrollDeviceForAdminTest(t, manager, "device-admin")
	enrollDeviceForAdminTest(t, manager, "device-b")
	enrollDeviceForAdminTest(t, manager, "device-c")
	handler := New(
		manager,
		watcher.New(time.Second),
		nil,
		nil,
		nil,
		nil,
		nil,
	).Handler()
	return manager, handler, actorKey
}

func enrollDeviceForAdminTest(
	t *testing.T,
	manager *auth.Manager,
	deviceID string,
) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	token, err := manager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.EnrollDevice(
		token.Value,
		manager.DaemonID(),
		manager.PublicKeyHex(),
		deviceID,
		deviceID,
		hex.EncodeToString(publicKey),
	); err != nil {
		t.Fatal(err)
	}
	return publicKey, privateKey
}

func deviceRequest(
	t *testing.T,
	handler http.Handler,
	method string,
	body string,
	authorization string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/devices", strings.NewReader(body))
	request.Header.Set("Authorization", authorization)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertDeviceAPIError(
	t *testing.T,
	response *httptest.ResponseRecorder,
	status int,
	code string,
) {
	t.Helper()
	if response.Code != status {
		t.Fatalf(
			"status=%d body=%q, want %d",
			response.Code,
			response.Body.String(),
			status,
		)
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload.Error.Code != code {
		t.Fatalf("error code=%q body=%q, want %q", payload.Error.Code, response.Body.String(), code)
	}
}

func deviceAdminAuthorization(
	t *testing.T,
	privateKey ed25519.PrivateKey,
	daemonID string,
	deviceID string,
	purpose string,
) string {
	t.Helper()
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		t.Fatal(err)
	}
	nonce := hex.EncodeToString(nonceBytes)
	signature := ed25519.Sign(
		privateKey,
		auth.BuildSignaturePayload(
			purpose,
			daemonID,
			deviceID,
			timestamp,
			nonce,
		),
	)
	return auth.AuthorizationHeaderPrefix +
		"v1:" + deviceID + ":" + daemonID + ":" + timestamp + ":" + nonce + ":" +
		hex.EncodeToString(signature)
}
