package link_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/link"
	"github.com/daoleno/zen/daemon/linkproto"
	"github.com/daoleno/zen/daemon/relay"
	daemonserver "github.com/daoleno/zen/daemon/server"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/gorilla/websocket"
)

const testConnectorToken = "0123456789abcdef0123456789abcdef"

func TestOpaqueRelayConnectorHealthAndAdmissionReplay(t *testing.T) {
	relayTLS, relayRoots, relayServerName := relayTLSFixture(t)
	clientListener := listenLocal(t)
	controlTCP := listenLocal(t)
	controlListener := tls.NewListener(controlTCP, relayTLS)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	relayConfig := relay.DefaultConfig(testConnectorToken)
	relayConfig.MaxClients = 8
	relayConfig.MaxClientsPerRoute = 4
	relayConfig.HandshakeTimeout = time.Second
	relayConfig.AttachTimeout = time.Second
	relayConfig.IdleTimeout = 5 * time.Second
	relayServer, err := relay.New(relayConfig)
	if err != nil {
		t.Fatalf("relay.New: %v", err)
	}
	relayErrors := make(chan error, 1)
	go func() {
		relayErrors <- relayServer.Serve(ctx, clientListener, controlListener)
	}()

	stateDir := t.TempDir()
	authManager, err := auth.NewManager(stateDir)
	if err != nil {
		t.Fatalf("auth.NewManager: %v", err)
	}
	transportIdentity, err := link.LoadOrCreateTransportIdentity(
		stateDir,
		[]string{"link.test"},
	)
	if err != nil {
		t.Fatalf("LoadOrCreateTransportIdentity: %v", err)
	}
	var pathsMu sync.Mutex
	seenPaths := make(map[string]int)
	uploadStarted := make(chan struct{})
	releaseUpload := make(chan struct{})
	var uploadStartedOnce sync.Once
	deviceSeed := bytes.Repeat([]byte{9}, ed25519.SeedSize)
	devicePrivateKey := ed25519.NewKeyFromSeed(deviceSeed)
	devicePublicKey := devicePrivateKey.Public().(ed25519.PublicKey)
	deviceID := "e2e-phone"
	deviceName := "E2E Phone"
	if os.Getenv("ZEN_LINK_MOBILE_RELAY_E2E") == "1" {
		deviceID = "00000000-0000-4000-8000-000000000009"
		deviceName = "Relay E2E phone"
	}
	pairing, err := authManager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatalf("IssuePairingToken: %v", err)
	}
	daemonOrigin := daemonserver.New(
		authManager,
		watcher.New(time.Second),
		nil,
		nil,
		nil,
		nil,
		nil,
	).Handler()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathsMu.Lock()
		seenPaths[r.URL.Path]++
		pathsMu.Unlock()
		switch r.URL.Path {
		case "/upload":
			uploadStartedOnce.Do(func() { close(uploadStarted) })
			<-releaseUpload
			daemonOrigin.ServeHTTP(w, r)
		case "/session-file":
			if r.Header.Get("Range") != "bytes=2-5" {
				http.Error(w, "range required", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Range", "bytes 2-5/10")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = io.WriteString(w, "2345")
		default:
			daemonOrigin.ServeHTTP(w, r)
		}
	})
	connectorConfig := link.ConnectorConfig{
		ConnectorToken: testConnectorToken,
		Candidates: []link.RelayCandidate{{
			Name:              "local",
			ControlAddress:    controlTCP.Addr().String(),
			ControlServerName: relayServerName,
			ControlRootCAs:    relayRoots,
			ClientDomain:      "link.test",
			ClientPort:        portOf(t, clientListener.Addr()),
		}},
	}
	connector, err := link.NewConnector(
		connectorConfig,
		authManager,
		transportIdentity,
		handler,
	)
	if err != nil {
		t.Fatalf("link.NewConnector: %v", err)
	}
	connectorErrors := make(chan error, 1)
	go func() {
		connectorErrors <- connector.Run(ctx)
	}()
	waitFor(t, 3*time.Second, func() bool {
		return relayServer.Snapshot().ActiveRoutes == 1
	})

	offers, err := link.IssueAdmissions(
		context.Background(),
		connectorConfig,
		authManager,
		transportIdentity,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("IssueAdmissions: %v", err)
	}
	if len(offers) != 1 || offers[0].AdmissionHost == "" {
		t.Fatalf("admission offers=%#v", offers)
	}

	// Starting the platform bridge must not consume the one-time admission.
	// Android and iOS may establish the listener before the actual fetch, but
	// only the POST /pair request is allowed to commit this admission.
	preflight, err := dialPinnedLink(
		clientListener.Addr().String(),
		offers[0].AdmissionHost,
		transportIdentity.SPKISHA256,
	)
	if err != nil {
		t.Fatalf("dial admission preflight: %v", err)
	}
	if err := preflight.Close(); err != nil {
		t.Fatalf("close admission preflight: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool {
		return relayServer.Snapshot().ActiveClients == 0
	})

	wrongPathResponse, wrongPathBody := requestHTTPThroughLink(
		t,
		clientListener.Addr().String(),
		offers[0].AdmissionHost,
		transportIdentity.SPKISHA256,
		http.MethodGet,
		"/health",
		nil,
		nil,
	)
	if wrongPathResponse.StatusCode != http.StatusForbidden ||
		!strings.Contains(wrongPathBody, "only accepts the pairing request") {
		t.Fatalf(
			"admission wrong path status=%d body=%q",
			wrongPathResponse.StatusCode,
			wrongPathBody,
		)
	}

	if !runMobilePairingImport(
		t,
		clientListener.Addr().String(),
		connectorConfig,
		authManager,
		transportIdentity,
		pairing,
		offers,
	) {
		pairBody, err := json.Marshal(map[string]string{
			"enrollment_token":           pairing.Value,
			"expected_daemon_id":         authManager.DaemonID(),
			"expected_daemon_public_key": authManager.PublicKeyHex(),
			"device_id":                  deviceID,
			"device_name":                deviceName,
			"device_public_key":          hex.EncodeToString(devicePublicKey),
		})
		if err != nil {
			t.Fatalf("encode pairing request: %v", err)
		}
		response, body := requestHTTPThroughLink(
			t,
			clientListener.Addr().String(),
			offers[0].AdmissionHost,
			transportIdentity.SPKISHA256,
			http.MethodPost,
			"/pair",
			bytes.NewReader(pairBody),
			map[string]string{"Content-Type": "application/json"},
		)
		if response.StatusCode != http.StatusOK ||
			!strings.Contains(body, `"device_id":"`+deviceID+`"`) {
			t.Fatalf("pair status=%d body=%q", response.StatusCode, body)
		}
	}

	response, body := requestThroughLink(
		t,
		clientListener.Addr().String(),
		offers[0].StableHost,
		transportIdentity.SPKISHA256,
	)
	if response.StatusCode != http.StatusOK ||
		!strings.Contains(body, `"daemon_id":"`+authManager.DaemonID()+`"`) {
		t.Fatalf("health status=%d body=%q", response.StatusCode, body)
	}

	probeAuthorization := deviceAuthorization(
		t,
		authManager.DaemonID(),
		deviceID,
		devicePrivateKey,
		"zen-probe",
	)
	response, body = requestHTTPThroughLink(
		t,
		clientListener.Addr().String(),
		offers[0].StableHost,
		transportIdentity.SPKISHA256,
		http.MethodGet,
		"/auth-check",
		nil,
		map[string]string{"Authorization": probeAuthorization},
	)
	if response.StatusCode != http.StatusOK ||
		!strings.Contains(body, `"device_id":"`+deviceID+`"`) {
		t.Fatalf("auth-check status=%d body=%q", response.StatusCode, body)
	}

	capabilityResponse, capabilityBody := requestHTTPThroughLink(
		t,
		clientListener.Addr().String(),
		offers[0].StableHost,
		transportIdentity.SPKISHA256,
		http.MethodPost,
		"/session-file-capability",
		strings.NewReader(`{}`),
		map[string]string{
			"Authorization": deviceAuthorization(
				t,
				authManager.DaemonID(),
				deviceID,
				devicePrivateKey,
				"zen-session-file",
			),
			"Content-Type": "application/json",
		},
	)
	if capabilityResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf(
			"session capability boundary status=%d body=%q",
			capabilityResponse.StatusCode,
			capabilityBody,
		)
	}

	rangeResponse, rangeBody := requestHTTPThroughLink(
		t,
		clientListener.Addr().String(),
		offers[0].StableHost,
		transportIdentity.SPKISHA256,
		http.MethodGet,
		"/session-file",
		nil,
		map[string]string{
			"Authorization": deviceAuthorization(
				t,
				authManager.DaemonID(),
				deviceID,
				devicePrivateKey,
				"zen-session-file",
			),
			"Range": "bytes=2-5",
		},
	)
	if rangeResponse.StatusCode != http.StatusPartialContent ||
		rangeBody != "2345" {
		t.Fatalf("session range status=%d body=%q", rangeResponse.StatusCode, rangeBody)
	}

	uploadDone := make(chan error, 1)
	go func() {
		response, body, err := doHTTPThroughLink(
			clientListener.Addr().String(),
			offers[0].StableHost,
			transportIdentity.SPKISHA256,
			http.MethodPost,
			"/upload",
			bytes.NewReader(make([]byte, 256<<10)),
			map[string]string{
				"Authorization": deviceAuthorization(
					t,
					authManager.DaemonID(),
					deviceID,
					devicePrivateKey,
					"zen-upload",
				),
				"Content-Type":      "application/octet-stream",
				"X-Zen-Upload-Name": "bounded.bin",
			},
		)
		if err != nil {
			uploadDone <- err
			return
		}
		if response.StatusCode != http.StatusOK ||
			!strings.Contains(body, `"name":"bounded.bin"`) {
			uploadDone <- fmt.Errorf("upload status=%d body=%q", response.StatusCode, body)
			return
		}
		uploadDone <- nil
	}()
	select {
	case <-uploadStarted:
	case <-time.After(time.Second):
		t.Fatal("upload did not reach daemon handler")
	}
	assertDaemonWebSocketThroughLink(
		t,
		clientListener.Addr().String(),
		offers[0].StableHost,
		transportIdentity.SPKISHA256,
		deviceAuthorization(
			t,
			authManager.DaemonID(),
			deviceID,
			devicePrivateKey,
			"zen-connect",
		),
	)
	close(releaseUpload)
	if err := <-uploadDone; err != nil {
		t.Fatal(err)
	}

	pathsMu.Lock()
	for _, path := range []string{
		"/health",
		"/pair",
		"/auth-check",
		"/upload",
		"/session-file-capability",
		"/session-file",
		"/ws",
	} {
		if seenPaths[path] == 0 {
			pathsMu.Unlock()
			t.Fatalf("full-origin route %s did not traverse Link", path)
		}
	}
	pathsMu.Unlock()

	if _, err := dialPinnedLink(
		clientListener.Addr().String(),
		offers[0].AdmissionHost,
		transportIdentity.SPKISHA256,
	); err == nil {
		t.Fatal("replayed one-time admission was accepted")
	}

	wrongPin := strings.Repeat("a", 64)
	if wrongPin == transportIdentity.SPKISHA256 {
		wrongPin = strings.Repeat("b", 64)
	}
	if _, err := dialPinnedLink(
		clientListener.Addr().String(),
		offers[0].StableHost,
		wrongPin,
	); err == nil {
		t.Fatal("wrong transport pin was accepted")
	}

	snapshot := relayServer.Snapshot()
	if snapshot.ActiveRoutes != 1 || snapshot.AcceptedClients == 0 ||
		snapshot.ForwardedBytes == 0 || snapshot.RejectedClients == 0 {
		t.Fatalf("unexpected metadata-only relay snapshot: %#v", snapshot)
	}

	cancel()
	select {
	case err := <-connectorErrors:
		if err != nil && err != context.Canceled {
			t.Fatalf("connector shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("connector did not shut down")
	}
	select {
	case err := <-relayErrors:
		if err != nil && err != context.Canceled {
			t.Fatalf("relay shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("relay did not shut down")
	}
}

func TestConnectorFailsOverAndReregistersAfterRelayRestart(t *testing.T) {
	relayTLS, relayRoots, relayServerName := relayTLSFixture(t)
	first := startRelayFixture(t, relayTLS, "", "")
	second := startRelayFixture(t, relayTLS, "", "")

	stateDir := t.TempDir()
	authManager, err := auth.NewManager(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := link.LoadOrCreateTransportIdentity(
		stateDir,
		[]string{"first.link.test", "second.link.test"},
	)
	if err != nil {
		t.Fatal(err)
	}
	config := link.ConnectorConfig{
		ConnectorToken: testConnectorToken,
		ReconnectMax:   2 * time.Second,
		ControlTimeout: time.Second,
		Candidates: []link.RelayCandidate{
			{
				Name:              "first",
				ControlAddress:    first.controlAddress,
				ControlServerName: relayServerName,
				ControlRootCAs:    relayRoots,
				ClientDomain:      "first.link.test",
				ClientPort:        first.clientPort,
			},
			{
				Name:              "second",
				ControlAddress:    second.controlAddress,
				ControlServerName: relayServerName,
				ControlRootCAs:    relayRoots,
				ClientDomain:      "second.link.test",
				ClientPort:        second.clientPort,
			},
		},
	}
	connector, err := link.NewConnector(
		config,
		authManager,
		identity,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	connectorDone := make(chan error, 1)
	go func() { connectorDone <- connector.Run(ctx) }()

	waitFor(t, 4*time.Second, func() bool {
		state := connector.State()
		return state.Phase == "connected" &&
			first.server.Snapshot().ActiveRoutes+
				second.server.Snapshot().ActiveRoutes == 1
	})
	selected := first
	survivor := second
	if second.server.Snapshot().ActiveRoutes == 1 {
		selected, survivor = second, first
	}
	selected.stop()
	waitFor(t, 6*time.Second, func() bool {
		return connector.State().Phase == "connected" &&
			survivor.server.Snapshot().ActiveRoutes == 1
	})

	restarted := startRelayFixture(
		t,
		relayTLS,
		selected.clientAddress,
		selected.controlAddress,
	)
	survivor.stop()
	waitFor(t, 8*time.Second, func() bool {
		return connector.State().Phase == "connected" &&
			restarted.server.Snapshot().ActiveRoutes == 1
	})
	if state := connector.State(); state.Phase != "connected" {
		t.Fatalf("connector state after relay restart=%#v", state)
	}

	cancel()
	restarted.stop()
	select {
	case err := <-connectorDone:
		if err != nil && err != context.Canceled {
			t.Fatalf("connector shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("connector did not stop")
	}
}

func runMobilePairingImport(
	t *testing.T,
	relayAddress string,
	config link.ConnectorConfig,
	authManager *auth.Manager,
	identity *link.TransportIdentity,
	pairing auth.PairingToken,
	offers []link.AdmissionOffer,
) bool {
	t.Helper()
	if os.Getenv("ZEN_LINK_MOBILE_RELAY_E2E") != "1" {
		return false
	}
	if len(offers) != 1 {
		t.Fatalf("mobile Relay E2E requires one admission offer, got %d", len(offers))
	}
	pairingLink, _, err := link.BuildPairingLink(
		authManager,
		identity,
		config,
		pairing,
		offers,
	)
	if err != nil {
		t.Fatalf("build mobile Pairing V2 link: %v", err)
	}
	bunPath, err := exec.LookPath("bun")
	if err != nil {
		t.Fatalf("mobile Relay E2E requires bun: %v", err)
	}
	appDirectory, err := filepath.Abs(filepath.Join("..", "..", "app"))
	if err != nil {
		t.Fatalf("resolve app directory: %v", err)
	}
	bridgeListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for mobile pinned bridge: %v", err)
	}
	defer bridgeListener.Close()
	var bridgeConnections atomic.Int64
	bridgeDone := make(chan error, 1)
	go func() {
		local, acceptErr := bridgeListener.Accept()
		if acceptErr != nil {
			bridgeDone <- acceptErr
			return
		}
		bridgeConnections.Add(1)
		remote, dialErr := dialPinnedLink(
			relayAddress,
			offers[0].AdmissionHost,
			identity.SPKISHA256,
		)
		if dialErr != nil {
			_ = local.Close()
			bridgeDone <- dialErr
			return
		}
		copyDone := make(chan struct{}, 2)
		go func() {
			_, _ = io.Copy(remote, local)
			_ = remote.Close()
			copyDone <- struct{}{}
		}()
		go func() {
			_, _ = io.Copy(local, remote)
			_ = local.Close()
			copyDone <- struct{}{}
		}()
		<-copyDone
		<-copyDone
		bridgeDone <- nil
	}()
	commandContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(
		commandContext,
		bunPath,
		"test",
		"services/importConnectionRelayE2E.test.ts",
	)
	command.Dir = appDirectory
	command.Env = append(
		os.Environ(),
		"ZEN_LINK_RELAY_E2E=1",
		"ZEN_LINK_E2E_PAIRING_LINK="+pairingLink,
		fmt.Sprintf(
			"ZEN_LINK_E2E_BRIDGE_PORT=%d",
			portOf(t, bridgeListener.Addr()),
		),
		"ZEN_LINK_E2E_STABLE_URL="+offers[0].StableURL,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		_ = bridgeListener.Close()
		t.Fatalf("mobile import through Relay failed: %v\n%s", err, output)
	}
	select {
	case bridgeErr := <-bridgeDone:
		if bridgeErr != nil {
			t.Fatalf("mobile pinned bridge failed: %v", bridgeErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("mobile pinned bridge did not close after pairing")
	}
	if bridgeConnections.Load() != 1 {
		t.Fatalf(
			"mobile pairing opened %d remote streams, want exactly one",
			bridgeConnections.Load(),
		)
	}
	return true
}

func requestThroughLink(
	t *testing.T,
	address string,
	serverName string,
	pin string,
) (*http.Response, string) {
	t.Helper()
	conn, err := dialPinnedLink(address, serverName, pin)
	if err != nil {
		t.Fatalf("dial pinned Link: %v", err)
	}
	defer conn.Close()
	if _, err := io.WriteString(
		conn,
		"GET /health HTTP/1.1\r\nHost: "+serverName+"\r\nConnection: close\r\n\r\n",
	); err != nil {
		t.Fatalf("write health request: %v", err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read health response: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read health body: %v", err)
	}
	return response, string(body)
}

func requestHTTPThroughLink(
	t *testing.T,
	address string,
	serverName string,
	pin string,
	method string,
	path string,
	body io.Reader,
	headers map[string]string,
) (*http.Response, string) {
	t.Helper()
	response, responseBody, err := doHTTPThroughLink(
		address,
		serverName,
		pin,
		method,
		path,
		body,
		headers,
	)
	if err != nil {
		t.Fatal(err)
	}
	return response, responseBody
}

func doHTTPThroughLink(
	address string,
	serverName string,
	pin string,
	method string,
	path string,
	body io.Reader,
	headers map[string]string,
) (*http.Response, string, error) {
	conn, err := dialPinnedLink(address, serverName, pin)
	if err != nil {
		return nil, "", fmt.Errorf("dial pinned Link: %w", err)
	}
	defer conn.Close()
	request, err := http.NewRequest(method, "https://"+serverName+path, body)
	if err != nil {
		return nil, "", fmt.Errorf("new request: %w", err)
	}
	request.Host = serverName
	request.Close = true
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	if err := request.Write(conn); err != nil {
		return nil, "", fmt.Errorf("write request: %w", err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), request)
	if err != nil {
		return nil, "", fmt.Errorf("read response: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read response body: %w", err)
	}
	return response, string(raw), nil
}

func assertDaemonWebSocketThroughLink(
	t *testing.T,
	address string,
	serverName string,
	pin string,
	authorization string,
) {
	t.Helper()
	dialer := websocket.Dialer{
		HandshakeTimeout: time.Second,
		NetDialTLSContext: func(
			context.Context,
			string,
			string,
		) (net.Conn, error) {
			return dialPinnedLink(address, serverName, pin)
		},
	}
	headers := http.Header{}
	headers.Set("Authorization", authorization)
	conn, response, err := dialer.Dial("wss://"+serverName+"/ws", headers)
	if response != nil {
		defer response.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial Link WebSocket: %v", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	messageType, message, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read Link WebSocket: %v", err)
	}
	if messageType != websocket.TextMessage ||
		!bytes.Contains(message, []byte(`"type":"agent_session_list"`)) {
		t.Fatalf("unexpected daemon WebSocket frame type=%d body=%q", messageType, message)
	}
}

func deviceAuthorization(
	t *testing.T,
	daemonID string,
	deviceID string,
	privateKey ed25519.PrivateKey,
	purpose string,
) string {
	t.Helper()
	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		t.Fatalf("generate auth nonce: %v", err)
	}
	nonce := hex.EncodeToString(nonceBytes)
	signature := ed25519.Sign(
		privateKey,
		auth.BuildSignaturePayload(purpose, daemonID, deviceID, timestamp, nonce),
	)
	return strings.Join([]string{
		auth.AuthorizationHeaderPrefix + "v1",
		deviceID,
		daemonID,
		timestamp,
		nonce,
		hex.EncodeToString(signature),
	}, ":")
}

func dialPinnedLink(address, serverName, pin string) (*tls.Conn, error) {
	raw, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		return nil, err
	}
	config, err := link.PinnedClientTLSConfig(serverName, pin)
	if err != nil {
		raw.Close()
		return nil, err
	}
	config.NextProtos = []string{"http/1.1"}
	conn := tls.Client(raw, config)
	if err := conn.HandshakeContext(context.Background()); err != nil {
		raw.Close()
		return nil, err
	}
	return conn, nil
}

func relayTLSFixture(t *testing.T) (*tls.Config, *x509.CertPool, string) {
	t.Helper()
	serverName := "relay-control.test"
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate relay TLS key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: serverName},
		DNSNames:     []string{serverName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatalf("create relay TLS certificate: %v", err)
	}
	certificate := tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  privateKey,
	}
	roots := x509.NewCertPool()
	roots.AddCert(templateFromDER(t, der))
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		NextProtos:   []string{linkproto.ControlALPN},
	}, roots, serverName
}

func templateFromDER(t *testing.T, der []byte) *x509.Certificate {
	t.Helper()
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return certificate
}

func listenLocal(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

type relayFixture struct {
	server         *relay.Server
	clientAddress  string
	controlAddress string
	clientPort     int
	stop           func()
}

func startRelayFixture(
	t *testing.T,
	relayTLS *tls.Config,
	clientAddress string,
	controlAddress string,
) relayFixture {
	t.Helper()
	if clientAddress == "" {
		clientAddress = "127.0.0.1:0"
	}
	if controlAddress == "" {
		controlAddress = "127.0.0.1:0"
	}
	clientListener, err := net.Listen("tcp", clientAddress)
	if err != nil {
		t.Fatalf("listen relay client %s: %v", clientAddress, err)
	}
	controlTCP, err := net.Listen("tcp", controlAddress)
	if err != nil {
		_ = clientListener.Close()
		t.Fatalf("listen relay control %s: %v", controlAddress, err)
	}
	controlListener := tls.NewListener(controlTCP, relayTLS.Clone())
	relayConfig := relay.DefaultConfig(testConnectorToken)
	relayConfig.MaxClients = 8
	relayConfig.MaxClientsPerRoute = 4
	relayConfig.HandshakeTimeout = time.Second
	relayConfig.AttachTimeout = time.Second
	relayConfig.IdleTimeout = 5 * time.Second
	server, err := relay.New(relayConfig)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, clientListener, controlListener) }()
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Errorf("relay fixture did not stop")
			}
		})
	}
	t.Cleanup(stop)
	return relayFixture{
		server:         server,
		clientAddress:  clientListener.Addr().String(),
		controlAddress: controlTCP.Addr().String(),
		clientPort:     portOf(t, clientListener.Addr()),
		stop:           stop,
	}
}

func portOf(t *testing.T, address net.Addr) int {
	t.Helper()
	_, rawPort, err := net.SplitHostPort(address.String())
	if err != nil {
		t.Fatalf("split address: %v", err)
	}
	var port int
	if _, err := fmt.Sscanf(rawPort, "%d", &port); err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return port
}

func waitFor(t *testing.T, timeout time.Duration, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}
