package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/control"
	daemonserver "github.com/daoleno/zen/daemon/server"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/gorilla/websocket"
)

const deviceCLIHelperEnv = "ZEN_TEST_DEVICE_CLI_HELPER"

func TestDeviceCLIHelper(t *testing.T) {
	if os.Getenv(deviceCLIHelperEnv) != "1" {
		return
	}
	separator := -1
	for index, arg := range os.Args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		fmt.Fprintln(os.Stderr, "missing helper CLI arguments")
		os.Exit(2)
	}
	if err := run(os.Args[separator+1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

func TestDevicesCLIListsTrustedDevicesThroughOfflineOwner(t *testing.T) {
	stateDir := shortControlStateDir(t)
	manager, err := auth.NewManager(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	enrollControlDevice(t, manager, "phone-list", "Listed phone")

	output := captureDeviceCommandStdout(t, func() error {
		return runDevicesCommand(
			[]string{"list", "-state-dir", stateDir},
			io.Discard,
		)
	})
	if !strings.Contains(output, "phone-list\tListed phone\t") {
		t.Fatalf("device list output=%q", output)
	}
	jsonOutput := captureDeviceCommandStdout(t, func() error {
		return runDevicesCommand(
			[]string{"list", "-json", "-state-dir", stateDir},
			io.Discard,
		)
	})
	if strings.Contains(jsonOutput, "public_key_hex") {
		t.Fatalf("device list JSON leaked public key: %s", jsonOutput)
	}
}

func TestControlAppListsAndRevokesThroughSoleAuthOwner(t *testing.T) {
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	enrollControlDevice(t, manager, "phone-one", "Phone")

	app := &controlApp{auth: manager}
	listed := app.HandleControlRequest(control.Request{Type: "device_list"})
	if !listed.OK ||
		len(listed.Devices) != 1 ||
		listed.Devices[0].ID != "phone-one" {
		t.Fatalf("list response=%#v", listed)
	}
	response := app.HandleControlRequest(control.Request{
		Type: "device_revoke",
		ID:   "phone-one",
	})
	if !response.OK ||
		response.PersistenceOutcome != control.PersistenceApplied ||
		response.PersistenceDurable == nil ||
		!*response.PersistenceDurable ||
		len(manager.ListDevices()) != 0 {
		t.Fatalf("revoke response=%#v devices=%#v", response, manager.ListDevices())
	}
	missing := app.HandleControlRequest(control.Request{
		Type: "device_revoke",
		ID:   "phone-one",
	})
	if missing.OK ||
		missing.Error == nil ||
		missing.Error.Code != "device_not_found" ||
		missing.PersistenceOutcome != control.PersistenceVerifiedAbsent ||
		missing.PersistenceDurable == nil ||
		*missing.PersistenceDurable {
		t.Fatalf("missing revoke response=%#v", missing)
	}
}

func TestControlRevokeReportsAppliedWithUncertainDurability(t *testing.T) {
	response := deviceRevokeControlResponseFromResult(
		"phone-uncertain",
		auth.PersistenceResult{Applied: true},
		errors.New("injected post-rename directory sync failure"),
	)
	if !response.OK ||
		response.PersistenceOutcome != control.PersistenceApplied ||
		response.PersistenceDurable == nil ||
		*response.PersistenceDurable ||
		!strings.Contains(response.Confirmation, "durability is uncertain") {
		t.Fatalf("uncertain durability response=%#v", response)
	}
	var output bytes.Buffer
	if err := writeDeviceRevokeResult(
		&output,
		"phone-uncertain",
		deviceRevokeResult{
			Outcome: control.PersistenceApplied,
		},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "persistence was applied") ||
		!strings.Contains(output.String(), "durability is uncertain") {
		t.Fatalf("uncertain durability CLI output=%q", output.String())
	}
}

func TestPairCLIUsesOnlineControlOwnerWithoutSecondManager(t *testing.T) {
	stateDir := shortControlStateDir(t)
	lifecycle, manager, err := acquireDaemonAuthOwner(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer lifecycle.Close()
	socketPath, err := control.DefaultSocketPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	controlContext, stopControl := context.WithCancel(context.Background())
	controlDone := make(chan error, 1)
	go func() {
		controlDone <- (&control.Server{
			Path:    socketPath,
			Handler: &controlApp{auth: manager},
		}).Run(controlContext)
	}()
	waitForDeviceControlReady(t, socketPath)

	if err := os.WriteFile(
		filepath.Join(stateDir, "identity.json"),
		[]byte("{invalid"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runPairCommand(
		[]string{"-state-dir", stateDir, "http://127.0.0.1:9876"},
		&output,
	); err != nil {
		t.Fatalf("online pair through live owner: %v", err)
	}
	if !strings.Contains(output.String(), manager.DaemonID()) ||
		!strings.Contains(output.String(), "zen://settings?") {
		t.Fatalf("online pairing output=%q", output.String())
	}

	stopControl()
	select {
	case err := <-controlDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("control server did not join")
	}
}

func TestPairCLILocksOfflineOwnerBeforeManagerConstruction(t *testing.T) {
	stateDir := shortControlStateDir(t)
	blocker, acquired, err := control.TryAcquireLifecycleLock(stateDir)
	if err != nil || !acquired {
		t.Fatalf("blocking lifecycle acquired=%t error=%v", acquired, err)
	}
	retry := make(chan time.Time)
	deadline := make(chan time.Time, 1)
	deadline <- time.Now()
	_, err = withAuthRuntimeOwnerWait(
		stateDir,
		control.Request{Type: "pair"},
		retry,
		deadline,
		"pairing token outcome is unknown",
		func(manager *auth.Manager) (control.Response, error) {
			return issuePairingControlResponse(manager), nil
		},
	)
	if err == nil {
		t.Fatal("pair unexpectedly bypassed active lifecycle owner")
	}
	for _, name := range []string{
		"identity.json",
		"trusted-devices.json",
		"pairing-tokens.json",
	} {
		if _, statErr := os.Lstat(filepath.Join(stateDir, name)); !errors.Is(
			statErr,
			os.ErrNotExist,
		) {
			t.Fatalf("offline pair constructed Manager before lock: %s", name)
		}
	}
	if err := blocker.Close(); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := runPairCommand(
		[]string{"-state-dir", stateDir, "http://127.0.0.1:9876"},
		&output,
	); err != nil {
		t.Fatalf("offline pair: %v", err)
	}
	if !strings.Contains(output.String(), "Generated a fresh pairing link") ||
		!strings.Contains(output.String(), "zen://settings?") {
		t.Fatalf("offline pairing output=%q", output.String())
	}
}

type delayedFirstRevokeHandler struct {
	app   *controlApp
	calls atomic.Int32
}

func (h *delayedFirstRevokeHandler) HandleControlRequest(
	request control.Request,
) control.Response {
	response := h.app.HandleControlRequest(request)
	if request.Type == "device_revoke" && h.calls.Add(1) == 1 {
		if response.OK {
			durable := false
			response.PersistenceDurable = &durable
			response.Confirmation =
				"Device revocation applied; crash durability is uncertain."
		}
		time.Sleep(deviceControlRequestTimeout + 150*time.Millisecond)
	}
	return response
}

func TestCLIRevokeTimeoutThenAbsenceNeverReportsNoChange(t *testing.T) {
	stateDir := shortControlStateDir(t)
	lifecycle, manager, err := acquireDaemonAuthOwner(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer lifecycle.Close()
	enrollControlDevice(t, manager, "delayed-phone", "Delayed phone")
	socketPath, err := control.DefaultSocketPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	handler := &delayedFirstRevokeHandler{
		app: &controlApp{auth: manager},
	}
	controlContext, stopControl := context.WithCancel(context.Background())
	controlDone := make(chan error, 1)
	go func() {
		controlDone <- (&control.Server{
			Path:    socketPath,
			Handler: handler,
		}).Run(controlContext)
	}()
	waitForDeviceControlReady(t, socketPath)

	output := captureDeviceCommandStdout(t, func() error {
		return runDevicesCommand(
			[]string{
				"revoke",
				"-state-dir",
				stateDir,
				"-id",
				"delayed-phone",
			},
			io.Discard,
		)
	})
	if !strings.Contains(output, "earlier request may have applied") ||
		!strings.Contains(output, "crash durability is uncertain") ||
		strings.Contains(output, "no persistence change") {
		t.Fatalf("timeout/retry CLI output=%q", output)
	}
	if manager.IsDeviceTrusted("delayed-phone") {
		t.Fatal("delayed revoke did not persist")
	}
	if handler.calls.Load() < 2 {
		t.Fatalf("revoke control calls=%d, want retry", handler.calls.Load())
	}

	stopControl()
	select {
	case err := <-controlDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("control server did not join")
	}
}

func TestCLIRevokeUsesRuntimeOwnerAcrossTargetAndUnaffectedSockets(
	t *testing.T,
) {
	stateDir := shortControlStateDir(t)
	lifecycle, manager, err := acquireDaemonAuthOwner(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer lifecycle.Close()
	targetPublic, targetPrivate := enrollControlDevice(
		t,
		manager,
		"target-phone",
		"Target phone",
	)
	_ = targetPublic
	_, otherPrivate := enrollControlDevice(
		t,
		manager,
		"other-phone",
		"Other phone",
	)

	runtimeServer := daemonserver.New(
		manager,
		watcher.New(time.Second),
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	httpServer := httptest.NewServer(runtimeServer.Handler())
	socketURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"
	targetSockets := dialControlSockets(
		t,
		socketURL,
		targetPrivate,
		manager.DaemonID(),
		"target-phone",
		3,
	)
	otherSockets := dialControlSockets(
		t,
		socketURL,
		otherPrivate,
		manager.DaemonID(),
		"other-phone",
		2,
	)

	socketPath, err := control.DefaultSocketPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	controlContext, stopControl := context.WithCancel(context.Background())
	controlDone := make(chan error, 1)
	go func() {
		controlDone <- (&control.Server{
			Path:    socketPath,
			Handler: &controlApp{auth: manager},
		}).Run(controlContext)
	}()
	waitForDeviceControlReady(t, socketPath)

	listOutput := runDeviceCLIHelper(
		t,
		"devices",
		"list",
		"-state-dir",
		stateDir,
	)
	if !strings.Contains(listOutput, "target-phone") ||
		!strings.Contains(listOutput, "other-phone") {
		t.Fatalf("online helper list output=%q", listOutput)
	}
	revokeOutput := runDeviceCLIHelper(
		t,
		"devices",
		"revoke",
		"-state-dir",
		stateDir,
		"-id",
		"target-phone",
	)
	if !strings.Contains(revokeOutput, "Revoked device target-phone.") {
		t.Fatalf("helper CLI output=%q", revokeOutput)
	}
	if manager.IsDeviceTrusted("target-phone") ||
		!manager.IsDeviceTrusted("other-phone") {
		t.Fatal("runtime revoke changed the wrong trust state")
	}

	for _, conn := range targetSockets {
		waitForControlWebSocketClose(t, conn)
		_ = conn.Close()
	}
	assertControlReconnectUnauthorized(
		t,
		socketURL,
		targetPrivate,
		manager.DaemonID(),
		"target-phone",
	)
	for _, conn := range otherSockets {
		if err := conn.WriteJSON(map[string]any{
			"type": "list_agent_sessions",
		}); err != nil {
			t.Fatalf("unaffected socket write: %v", err)
		}
		readControlWebSocketType(t, conn, "agent_session_list")
		_ = conn.Close()
	}
	reconnectedOther := dialControlSockets(
		t,
		socketURL,
		otherPrivate,
		manager.DaemonID(),
		"other-phone",
		1,
	)
	_ = reconnectedOther[0].Close()

	stopControl()
	select {
	case err := <-controlDone:
		if err != nil {
			t.Fatalf("control shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("control server did not join")
	}
	httpServer.Close()
}

func TestCLIRevokeFailsClosedWhenRuntimeOwnerSocketStaysMissing(t *testing.T) {
	stateDir := shortControlStateDir(t)
	lifecycle, manager, err := acquireDaemonAuthOwner(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer lifecycle.Close()
	enrollControlDevice(t, manager, "missing-owner-phone", "Missing owner phone")

	retry := make(chan time.Time)
	deadline := make(chan time.Time, 1)
	deadline <- time.Now()
	result, err := revokeDeviceWithRuntimeOwnerWait(
		stateDir,
		"missing-owner-phone",
		retry,
		deadline,
	)
	if err == nil ||
		result.Outcome != control.PersistenceUnknown ||
		!strings.Contains(err.Error(), "outcome is unknown") {
		t.Fatalf("missing runtime owner result=%#v error=%v", result, err)
	}
	if !manager.IsDeviceTrusted("missing-owner-phone") {
		t.Fatal("fail-closed revoke mutated persistent trust")
	}
}

func TestCLIRevokeUsesDirectPersistenceOnlyWithOfflineLifecycleLock(t *testing.T) {
	stateDir := shortControlStateDir(t)
	manager, err := auth.NewManager(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	enrollControlDevice(t, manager, "offline-owner-phone", "Offline owner phone")

	retry := make(chan time.Time)
	deadline := make(chan time.Time)
	result, err := revokeDeviceWithRuntimeOwnerWait(
		stateDir,
		"offline-owner-phone",
		retry,
		deadline,
	)
	if err != nil ||
		result.Outcome != control.PersistenceApplied ||
		!result.Durable {
		t.Fatalf("offline lifecycle revoke result=%#v error=%v", result, err)
	}
	reloaded, err := auth.NewManager(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.IsDeviceTrusted("offline-owner-phone") {
		t.Fatal("offline lifecycle owner did not persist revoke")
	}
	result, err = revokeDeviceWithRuntimeOwnerWait(
		stateDir,
		"offline-owner-phone",
		retry,
		deadline,
	)
	if err != nil ||
		result.Outcome != control.PersistenceVerifiedAbsent {
		t.Fatalf("already-absent revoke result=%#v error=%v", result, err)
	}
	output := captureDeviceCommandStdout(t, func() error {
		return runDevicesCommand(
			[]string{
				"revoke",
				"-state-dir",
				stateDir,
				"-id",
				"offline-owner-phone",
			},
			io.Discard,
		)
	})
	if !strings.Contains(output, "current absence was verified") ||
		!strings.Contains(output, "crash durability is uncertain") {
		t.Fatalf("already-absent CLI output=%q", output)
	}
}

func TestDaemonAuthOwnerLoadsOnlyAfterOfflineRevokeReleasesLock(t *testing.T) {
	stateDir := shortControlStateDir(t)
	setup, err := auth.NewManager(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	_, privateKey := enrollControlDevice(
		t,
		setup,
		"offline-first-phone",
		"Offline first phone",
	)
	header := controlDeviceAuthorization(
		t,
		privateKey,
		setup.DaemonID(),
		"offline-first-phone",
		"zen-probe",
	)

	offlineLock, acquired, err := control.TryAcquireLifecycleLock(stateDir)
	if err != nil || !acquired {
		t.Fatalf("offline lock acquired=%t error=%v", acquired, err)
	}
	offlineManager, err := auth.NewManager(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := offlineManager.RevokeDevice("offline-first-phone"); err != nil {
		t.Fatal(err)
	}
	if err := offlineLock.Close(); err != nil {
		t.Fatal(err)
	}

	runtimeLock, runtimeManager, err := acquireDaemonAuthOwner(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeLock.Close()
	if _, err := runtimeManager.VerifyAuthorization(
		header,
		"zen-probe",
		time.Minute,
	); !errors.Is(err, auth.ErrUnknownDevice) {
		t.Fatalf("post-offline-revoke runtime authorization error=%v", err)
	}
}

func TestDaemonAuthOwnerDoesNotConstructManagerBeforeLifecycleLock(t *testing.T) {
	stateDir := shortControlStateDir(t)
	blocker, acquired, err := control.TryAcquireLifecycleLock(stateDir)
	if err != nil || !acquired {
		t.Fatalf("blocking lifecycle acquired=%t error=%v", acquired, err)
	}
	defer blocker.Close()

	lifecycle, manager, err := acquireDaemonAuthOwner(stateDir)
	if err == nil || lifecycle != nil || manager != nil {
		t.Fatalf(
			"contended owner lifecycle=%v manager=%v error=%v",
			lifecycle,
			manager,
			err,
		)
	}
	for _, name := range []string{
		"identity.json",
		"trusted-devices.json",
		"pairing-tokens.json",
	} {
		if _, statErr := os.Lstat(filepath.Join(stateDir, name)); !errors.Is(
			statErr,
			os.ErrNotExist,
		) {
			t.Fatalf("Manager constructed %s before lifecycle ownership: %v", name, statErr)
		}
	}
}

func TestJoinedRuntimeRetainsLifecycleLockUntilEveryOwnerStops(t *testing.T) {
	stateDir := shortControlStateDir(t)
	lifecycle, acquired, err := control.TryAcquireLifecycleLock(stateDir)
	if err != nil || !acquired {
		t.Fatalf("lifecycle acquired=%t error=%v", acquired, err)
	}
	draining := make(chan struct{})
	drainStarted := make(chan struct{})
	runDone := make(chan error, 1)
	go func() {
		err := runJoinedRuntime(context.Background(), []runtimeOwner{
			{
				name: "forced control server",
				run: func(context.Context) error {
					return errors.New("forced control failure")
				},
			},
			{
				name: "authenticated shutdown",
				run: func(ctx context.Context) error {
					<-ctx.Done()
					close(drainStarted)
					<-draining
					return nil
				},
			},
		})
		if closeErr := lifecycle.Close(); err == nil {
			err = closeErr
		}
		runDone <- err
	}()
	select {
	case <-drainStarted:
	case <-time.After(time.Second):
		t.Fatal("joined owner did not begin draining")
	}
	second, acquired, err := control.TryAcquireLifecycleLock(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if acquired || second != nil {
		t.Fatal("lifecycle lock released before joined owner drained")
	}
	close(draining)
	if err := <-runDone; err == nil ||
		!strings.Contains(err.Error(), "forced control failure") {
		t.Fatalf("joined runtime error=%v", err)
	}
	recovered, acquired, err := control.TryAcquireLifecycleLock(stateDir)
	if err != nil || !acquired {
		t.Fatalf("recovered lifecycle acquired=%t error=%v", acquired, err)
	}
	if err := recovered.Close(); err != nil {
		t.Fatal(err)
	}
}

func runDeviceCLIHelper(t *testing.T, args ...string) string {
	t.Helper()
	commandArgs := []string{
		"-test.run=^TestDeviceCLIHelper$",
		"--",
	}
	command := exec.Command(os.Args[0], append(commandArgs, args...)...)
	command.Env = append(os.Environ(), deviceCLIHelperEnv+"=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("helper CLI failed: %v\n%s", err, output)
	}
	return string(output)
}

func enrollControlDevice(
	t *testing.T,
	manager *auth.Manager,
	deviceID string,
	deviceName string,
) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pairing, err := manager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.EnrollDevice(
		pairing.Value,
		manager.DaemonID(),
		manager.PublicKeyHex(),
		deviceID,
		deviceName,
		hex.EncodeToString(publicKey),
	); err != nil {
		t.Fatal(err)
	}
	return publicKey, privateKey
}

func dialControlSockets(
	t *testing.T,
	socketURL string,
	privateKey ed25519.PrivateKey,
	daemonID string,
	deviceID string,
	count int,
) []*websocket.Conn {
	t.Helper()
	sockets := make([]*websocket.Conn, 0, count)
	for range count {
		header := http.Header{}
		header.Set(
			"Authorization",
			controlDeviceAuthorization(
				t,
				privateKey,
				daemonID,
				deviceID,
				"zen-connect",
			),
		)
		conn, _, err := websocket.DefaultDialer.Dial(socketURL, header)
		if err != nil {
			t.Fatal(err)
		}
		sockets = append(sockets, conn)
		readControlWebSocketType(t, conn, "agent_session_list")
	}
	return sockets
}

func assertControlReconnectUnauthorized(
	t *testing.T,
	socketURL string,
	privateKey ed25519.PrivateKey,
	daemonID string,
	deviceID string,
) {
	t.Helper()
	header := http.Header{}
	header.Set(
		"Authorization",
		controlDeviceAuthorization(
			t,
			privateKey,
			daemonID,
			deviceID,
			"zen-connect",
		),
	)
	conn, response, err := websocket.DefaultDialer.Dial(socketURL, header)
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil ||
		response == nil ||
		response.StatusCode != http.StatusUnauthorized {
		t.Fatalf(
			"revoked reconnect conn=%v status=%v error=%v",
			conn,
			controlResponseStatus(response),
			err,
		)
	}
}

func readControlWebSocketType(
	t *testing.T,
	conn *websocket.Conn,
	want string,
) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for {
		var payload map[string]any
		if err := conn.ReadJSON(&payload); err != nil {
			t.Fatalf("read %s: %v", want, err)
		}
		if payload["type"] == want {
			return
		}
	}
}

func waitForControlWebSocketClose(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

func waitForDeviceControlReady(t *testing.T, socketPath string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		response, err := control.CallWithTimeout(
			socketPath,
			control.Request{Type: "device_list"},
			100*time.Millisecond,
		)
		if err == nil && response.OK {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("control socket did not become ready: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func shortControlStateDir(t *testing.T) string {
	t.Helper()
	stateDir, err := os.MkdirTemp("", "zen-revoke-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(stateDir); err != nil {
			t.Errorf("remove short control state dir: %v", err)
		}
	})
	return stateDir
}

func controlDeviceAuthorization(
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
		"v1:" +
		deviceID +
		":" +
		daemonID +
		":" +
		timestamp +
		":" +
		nonce +
		":" +
		hex.EncodeToString(signature)
}

func controlResponseStatus(response *http.Response) any {
	if response == nil {
		return nil
	}
	return response.StatusCode
}

func captureDeviceCommandStdout(t *testing.T, run func() error) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	runErr := run()
	os.Stdout = original
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	output, readErr := io.ReadAll(reader)
	if closeErr := reader.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if runErr != nil {
		t.Fatal(runErr)
	}
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(output)
}
