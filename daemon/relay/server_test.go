package relay

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/linkproto"
)

func TestSignedConnectorAuthRejectsWrongDaemonExpiredAndReplay(t *testing.T) {
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := newTestRelay(t)
	valid := signedRelayRequest(t, manager, linkproto.TypeRegister, time.Now(), "1")
	if err := server.verifySignedRequest(valid); err != nil {
		t.Fatalf("valid connector auth rejected: %v", err)
	}
	if err := server.verifySignedRequest(valid); err == nil ||
		!strings.Contains(err.Error(), "replayed") {
		t.Fatalf("replayed connector request error=%v", err)
	}
	caseVariant := signedRelayRequest(
		t,
		manager,
		linkproto.TypeRegister,
		time.Now(),
		"a",
	)
	if err := server.verifySignedRequest(caseVariant); err != nil {
		t.Fatalf("fresh case-variant fixture rejected: %v", err)
	}
	caseVariant.DaemonID = strings.ToUpper(caseVariant.DaemonID)
	caseVariant.Signature = manager.CreateLinkSignature(
		linkproto.SignaturePayload(caseVariant),
	)
	if err := server.verifySignedRequest(caseVariant); err == nil ||
		!strings.Contains(err.Error(), "replayed") {
		t.Fatalf("case-variant nonce replay error=%v", err)
	}

	expired := signedRelayRequest(
		t,
		manager,
		linkproto.TypeRegister,
		time.Now().Add(-time.Hour),
		"2",
	)
	if err := server.verifySignedRequest(expired); err == nil ||
		!strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired connector request error=%v", err)
	}

	wrongDaemon := signedRelayRequest(t, manager, linkproto.TypeRegister, time.Now(), "3")
	wrongDaemon.DaemonID = strings.Repeat("f", 64)
	wrongDaemon.Signature = manager.CreateLinkSignature(
		linkproto.SignaturePayload(wrongDaemon),
	)
	if err := server.verifySignedRequest(wrongDaemon); err == nil ||
		!strings.Contains(err.Error(), "does not match") {
		t.Fatalf("wrong daemon request error=%v", err)
	}

	consume := signedRelayRequest(
		t,
		manager,
		linkproto.TypeAdmissionConsume,
		time.Now(),
		"4",
	)
	consume.Alias = strings.Repeat("b", 32)
	consume.StreamID = strings.Repeat("c", 32)
	consume.Signature = manager.CreateLinkSignature(
		linkproto.SignaturePayload(consume),
	)
	consume.StreamID = strings.Repeat("d", 32)
	if err := server.verifySignedRequest(consume); err == nil ||
		!strings.Contains(err.Error(), "signature") {
		t.Fatalf("tampered admission stream binding error=%v", err)
	}
}

func TestAdmissionAndStreamTicketsAreOneTimeAndBounded(t *testing.T) {
	server := newTestRelay(t)
	control, peer := net.Pipe()
	defer control.Close()
	defer peer.Close()
	route := &routeSession{
		routeID:         strings.Repeat("1", 32),
		daemonPublicKey: strings.Repeat("2", 64),
		conn:            control,
	}
	server.routes[route.routeID] = route
	server.activeRoutes.Add(1)
	conflictControl, conflictPeer := net.Pipe()
	defer conflictControl.Close()
	defer conflictPeer.Close()
	if _, err := server.registerRoute(conflictControl, linkproto.Message{
		RouteID:         route.routeID,
		DaemonPublicKey: route.daemonPublicKey,
	}); err == nil || !strings.Contains(err.Error(), "already connected") {
		t.Fatalf("route conflict error=%v", err)
	}
	alias := strings.Repeat("3", 32)
	server.admissions[alias] = admission{
		routeID:   route.routeID,
		expiresAt: time.Now().Add(time.Minute),
	}
	firstStream := strings.Repeat("5", 32)
	if _, gotRoute, gotAlias, ok := server.reserveRoute(alias, firstStream); !ok ||
		gotRoute != route.routeID || gotAlias != alias {
		t.Fatal("fresh admission was not reserved")
	}
	if _, _, _, ok := server.reserveRoute(alias, strings.Repeat("6", 32)); ok {
		t.Fatal("concurrent admission reservation was accepted")
	}
	server.releaseAdmissionReservation(alias, firstStream)
	pairStream := strings.Repeat("7", 32)
	if _, _, _, ok := server.reserveRoute(alias, pairStream); !ok {
		t.Fatal("abandoned admission reservation was not released")
	}
	if err := server.consumeAdmission(linkproto.Message{
		RouteID:         strings.Repeat("c", 32),
		DaemonPublicKey: route.daemonPublicKey,
		Alias:           alias,
		StreamID:        pairStream,
	}); err == nil {
		t.Fatal("admission was consumed with the wrong route binding")
	}
	if err := server.consumeAdmission(linkproto.Message{
		RouteID:         route.routeID,
		DaemonPublicKey: strings.Repeat("d", 64),
		Alias:           alias,
		StreamID:        pairStream,
	}); err == nil {
		t.Fatal("admission was consumed with the wrong daemon binding")
	}
	if err := server.consumeAdmission(linkproto.Message{
		RouteID:         route.routeID,
		DaemonPublicKey: route.daemonPublicKey,
		Alias:           alias,
		StreamID:        pairStream,
	}); err != nil {
		t.Fatalf("actual pairing admission was not consumed: %v", err)
	}
	if _, _, _, ok := server.reserveRoute(alias, strings.Repeat("8", 32)); ok {
		t.Fatal("consumed admission replay was accepted")
	}
	expiredAlias := strings.Repeat("4", 32)
	server.admissions[expiredAlias] = admission{
		routeID:   route.routeID,
		expiresAt: time.Now().Add(-time.Second),
	}
	if _, _, _, ok := server.reserveRoute(
		expiredAlias,
		strings.Repeat("9", 32),
	); ok {
		t.Fatal("expired admission was accepted")
	}

	streamID := strings.Repeat("a", 32)
	ticket := strings.Repeat("b", 64)
	server.pending[streamID] = &pendingStream{
		routeID:   route.routeID,
		ticket:    ticket,
		connCh:    make(chan net.Conn),
		done:      make(chan struct{}),
		expiresAt: time.Now().Add(time.Minute),
	}
	message := linkproto.Message{StreamID: streamID, StreamTicket: ticket}
	if _, err := server.claimPendingStream(message); err != nil {
		t.Fatalf("fresh stream ticket rejected: %v", err)
	}
	if _, err := server.claimPendingStream(message); err == nil {
		t.Fatal("stream ticket replay was accepted")
	}
}

func TestClientHelloRoutingIsBoundedAndSlowlorisTimesOut(t *testing.T) {
	server := newTestRelay(t)
	server.config.HandshakeTimeout = 25 * time.Millisecond
	serverSide, clientSide := net.Pipe()
	done := make(chan struct{})
	go func() {
		server.handleClient(context.Background(), serverSide)
		close(done)
	}()
	if _, err := clientSide.Write([]byte{22}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("slowloris connection was not closed at the handshake deadline")
	}
	_ = clientSide.Close()
	if server.Snapshot().RejectedClients != 1 {
		t.Fatalf("slowloris rejection was not counted: %#v", server.Snapshot())
	}

	parserSide, tlsSide := net.Pipe()
	type result struct {
		name   string
		prefix []byte
		err    error
	}
	resultCh := make(chan result, 1)
	go func() {
		name, prefix, err := readClientHelloServerName(parserSide)
		resultCh <- result{name: name, prefix: prefix, err: err}
		_ = parserSide.Close()
	}()
	client := tls.Client(tlsSide, &tls.Config{
		MinVersion:         tls.VersionTLS13,
		ServerName:         "abcdef0123456789abcdef0123456789.link.test",
		InsecureSkipVerify: true,
	})
	_ = client.Handshake()
	parsed := <-resultCh
	if parsed.err != nil ||
		parsed.name != "abcdef0123456789abcdef0123456789.link.test" ||
		len(parsed.prefix) == 0 ||
		len(parsed.prefix) > maxClientHelloBytes {
		t.Fatalf("ClientHello parse result=%#v", parsed)
	}
	_ = client.Close()
}

func TestConcurrencyAndOperatorSurfaceExposeMetadataOnly(t *testing.T) {
	config := DefaultConfig("0123456789abcdef0123456789abcdef")
	config.MaxClients = 1
	config.MaxClientsPerRoute = 1
	server, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	session := &routeSession{}
	if !server.reserveClient(session) {
		t.Fatal("first stream reservation failed")
	}
	if server.reserveClient(session) {
		t.Fatal("over-limit stream reservation succeeded")
	}
	server.releaseClient(session)

	server.acceptedClients.Add(2)
	server.rejectedClients.Add(3)
	server.forwardedBytes.Add(4096)
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	server.OperatorHandler().ServeHTTP(response, request)
	body := response.Body.String()
	for _, expected := range []string{
		"zen_link_accepted_streams_total 2",
		"zen_link_rejected_connections_total 3",
		"zen_link_forwarded_bytes_total 4096",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics missing %q: %q", expected, body)
		}
	}
	for _, forbidden := range []string{
		"0123456789abcdef0123456789abcdef",
		"/private/session/path",
		"Terminal",
		"pair token",
	} {
		if bytes.Contains(response.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("operator metrics leaked %q", forbidden)
		}
	}
}

func TestForwardKeepsOneWayTrafficAlivePastSharedIdleWindow(t *testing.T) {
	server := newTestRelay(t)
	server.config.IdleTimeout = 35 * time.Millisecond
	leftRelay, leftPeer := tcpConnectionPair(t)
	rightRelay, rightPeer := tcpConnectionPair(t)
	defer leftPeer.Close()
	defer rightPeer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		server.forward(ctx, leftRelay, rightRelay)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	_ = leftPeer.SetDeadline(deadline)
	_ = rightPeer.SetDeadline(deadline)
	for index := byte(0); index < 8; index++ {
		if _, err := leftPeer.Write([]byte{index}); err != nil {
			t.Fatalf("one-way write %d after shared activity: %v", index, err)
		}
		var received [1]byte
		if _, err := io.ReadFull(rightPeer, received[:]); err != nil {
			t.Fatalf("one-way read %d after shared activity: %v", index, err)
		}
		if received[0] != index {
			t.Fatalf("one-way byte=%d want=%d", received[0], index)
		}
		time.Sleep(15 * time.Millisecond)
	}

	cancel()
	waitForForward(t, done)
}

func TestForwardPropagatesHalfCloseAndDrainsDelayedResponse(t *testing.T) {
	server := newTestRelay(t)
	server.config.IdleTimeout = 250 * time.Millisecond
	leftRelay, leftPeer := tcpConnectionPair(t)
	rightRelay, rightPeer := tcpConnectionPair(t)
	defer leftPeer.Close()
	defer rightPeer.Close()

	done := make(chan struct{})
	go func() {
		server.forward(context.Background(), leftRelay, rightRelay)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	_ = leftPeer.SetDeadline(deadline)
	_ = rightPeer.SetDeadline(deadline)
	request := []byte("request-body")
	if _, err := leftPeer.Write(request); err != nil {
		t.Fatal(err)
	}
	if err := leftPeer.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	gotRequest := make([]byte, len(request))
	if _, err := io.ReadFull(rightPeer, gotRequest); err != nil {
		t.Fatalf("read half-closed request: %v", err)
	}
	if !bytes.Equal(gotRequest, request) {
		t.Fatalf("request=%q want=%q", gotRequest, request)
	}
	var eofProbe [1]byte
	if count, err := rightPeer.Read(eofProbe[:]); count != 0 || err != io.EOF {
		t.Fatalf("request EOF count=%d err=%v", count, err)
	}

	time.Sleep(40 * time.Millisecond)
	response := []byte("delayed-response")
	if _, err := rightPeer.Write(response); err != nil {
		t.Fatalf("write delayed response after request EOF: %v", err)
	}
	if err := rightPeer.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	gotResponse := make([]byte, len(response))
	if _, err := io.ReadFull(leftPeer, gotResponse); err != nil {
		t.Fatalf("read delayed response: %v", err)
	}
	if !bytes.Equal(gotResponse, response) {
		t.Fatalf("response=%q want=%q", gotResponse, response)
	}
	if count, err := leftPeer.Read(eofProbe[:]); count != 0 || err != io.EOF {
		t.Fatalf("response EOF count=%d err=%v", count, err)
	}
	waitForForward(t, done)
}

func TestForwardSlowUploadDownloadBackpressureAndCancellationConverge(t *testing.T) {
	for _, direction := range []string{"upload", "download"} {
		t.Run(direction, func(t *testing.T) {
			server := newTestRelay(t)
			server.config.IdleTimeout = 30 * time.Millisecond
			leftRaw, leftPeer := net.Pipe()
			rightRaw, rightPeer := net.Pipe()
			leftRelay := &slowWriteConnection{
				Conn:     leftRaw,
				maxWrite: 1024,
				delay:    2 * time.Millisecond,
			}
			rightRelay := &slowWriteConnection{
				Conn:     rightRaw,
				maxWrite: 1024,
				delay:    2 * time.Millisecond,
			}
			defer leftPeer.Close()
			defer rightPeer.Close()

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				server.forward(ctx, leftRelay, rightRelay)
				close(done)
			}()

			var writer net.Conn
			var reader net.Conn
			if direction == "upload" {
				writer, reader = leftPeer, rightPeer
			} else {
				writer, reader = rightPeer, leftPeer
			}
			_ = writer.SetDeadline(time.Now().Add(3 * time.Second))
			_ = reader.SetDeadline(time.Now().Add(3 * time.Second))
			payload := bytes.Repeat([]byte(direction), (128<<10)/len(direction))
			writeResult := make(chan error, 1)
			go func() {
				writeResult <- writeAll(writer, payload)
			}()

			received := make([]byte, 0, len(payload))
			chunk := make([]byte, 16<<10)
			for len(received) < len(payload) {
				count, err := reader.Read(chunk)
				if count > 0 {
					received = append(received, chunk[:count]...)
					time.Sleep(time.Millisecond)
				}
				if err != nil {
					t.Fatalf(
						"slow %s read after %d bytes: %v",
						direction,
						len(received),
						err,
					)
				}
			}
			if err := <-writeResult; err != nil {
				t.Fatalf("slow %s write: %v", direction, err)
			}
			if !bytes.Equal(received, payload) {
				t.Fatalf(
					"slow %s received %d bytes, want %d",
					direction,
					len(received),
					len(payload),
				)
			}

			cancel()
			waitForForward(t, done)
		})
	}

	server := newTestRelay(t)
	server.config.IdleTimeout = time.Second
	var forwards []chan struct{}
	var peers []net.Conn
	for index := 0; index < 12; index++ {
		leftRelay, leftPeer := tcpConnectionPair(t)
		rightRelay, rightPeer := tcpConnectionPair(t)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			server.forward(ctx, leftRelay, rightRelay)
			close(done)
		}()
		cancel()
		forwards = append(forwards, done)
		peers = append(peers, leftPeer, rightPeer)
	}
	for _, done := range forwards {
		waitForForward(t, done)
	}
	for _, peer := range peers {
		_ = peer.Close()
	}
}

func newTestRelay(t *testing.T) *Server {
	t.Helper()
	config := DefaultConfig("abcdef0123456789abcdef0123456789")
	config.AuthMaxAge = time.Minute
	server, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

type slowWriteConnection struct {
	net.Conn
	maxWrite int
	delay    time.Duration
}

func (connection *slowWriteConnection) Write(raw []byte) (int, error) {
	if connection.delay > 0 {
		time.Sleep(connection.delay)
	}
	if connection.maxWrite > 0 && len(raw) > connection.maxWrite {
		raw = raw[:connection.maxWrite]
	}
	return connection.Conn.Write(raw)
}

func tcpConnectionPair(t *testing.T) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	accepted := make(chan *net.TCPConn, 1)
	acceptError := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.AcceptTCP()
		if acceptErr != nil {
			acceptError <- acceptErr
			return
		}
		accepted <- connection
	}()
	peer, err := net.DialTCP("tcp", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case connection := <-accepted:
		return connection, peer
	case err := <-acceptError:
		_ = peer.Close()
		t.Fatal(err)
	case <-time.After(time.Second):
		_ = peer.Close()
		t.Fatal("timed out accepting local TCP test connection")
	}
	return nil, nil
}

func waitForForward(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("relay forward goroutines did not converge")
	}
}

func signedRelayRequest(
	t *testing.T,
	manager *auth.Manager,
	messageType string,
	at time.Time,
	noncePrefix string,
) linkproto.Message {
	t.Helper()
	nonce := noncePrefix + strings.Repeat("0", 31)
	message := linkproto.Message{
		Type:            messageType,
		ConnectorToken:  "abcdef0123456789abcdef0123456789",
		RouteID:         strings.Repeat("a", 32),
		DaemonID:        manager.DaemonID(),
		DaemonPublicKey: manager.PublicKeyHex(),
		TimestampMS:     at.UnixMilli(),
		Nonce:           nonce,
	}
	message.Signature = manager.CreateLinkSignature(
		linkproto.SignaturePayload(message),
	)
	return message
}
