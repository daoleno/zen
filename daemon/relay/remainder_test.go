package relay

import (
	"context"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/linkproto"
)

func TestServeShutdownClosesHeldPreAuthBeforeFinalStateCleanup(t *testing.T) {
	server := limitedRelay(t)
	server.config.HandshakeTimeout = time.Second
	clientListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rawConnectorListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	connectorListener := newHeldReadListener(rawConnectorListener)
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(ctx, clientListener, connectorListener)
	}()

	conn, err := net.Dial("tcp", rawConnectorListener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	select {
	case <-connectorListener.readStarted:
	case <-time.After(time.Second):
		t.Fatal("relay did not enter the held pre-auth read")
	}

	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	register := signedRelayRequest(
		t,
		manager,
		linkproto.TypeRegister,
		time.Now(),
		"9",
	)
	cancel()
	select {
	case <-connectorListener.closed:
	case <-time.After(time.Second):
		t.Fatal("relay did not close the connector listener during shutdown")
	}
	_ = linkproto.WriteMessage(conn, register)
	close(connectorListener.allowRead)

	select {
	case err := <-serveDone:
		if err != nil && err != context.Canceled {
			t.Fatalf("Serve shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not converge after closing held pre-auth connection")
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.routes) != 0 ||
		len(server.admissions) != 0 ||
		len(server.nonces) != 0 ||
		len(server.pending) != 0 ||
		len(server.acceptedConnections) != 0 {
		t.Fatalf(
			"post-shutdown state routes=%d admissions=%d nonces=%d pending=%d accepted=%d",
			len(server.routes),
			len(server.admissions),
			len(server.nonces),
			len(server.pending),
			len(server.acceptedConnections),
		)
	}
	if got := server.rejectedClients.Load(); got != 1 {
		t.Fatalf("shutdown rejection total=%d, want 1", got)
	}
	if got := server.rejectionCounts[rejectionShutdown].Load(); got != 1 {
		t.Fatalf("shutdown rejection reason=%d, want 1", got)
	}
}

func TestConnectorRejectOutcomesAreExactlyOnceAndAnonymous(t *testing.T) {
	server := limitedRelay(t)
	server.config.MaxRoutes = 1
	control, controlPeer := net.Pipe()
	defer controlPeer.Close()
	routeID := strings.Repeat("a", 32)
	daemonKey := strings.Repeat("f", 64)
	server.mu.Lock()
	server.routes[routeID] = &routeSession{
		routeID:         routeID,
		daemonPublicKey: daemonKey,
		conn:            control,
	}
	server.activeRoutes.Add(1)
	server.mu.Unlock()

	clientListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	connectorListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(ctx, clientListener, connectorListener)
	}()
	defer func() {
		cancel()
		select {
		case <-serveDone:
		case <-time.After(2 * time.Second):
			t.Fatal("relay rejection fixture did not stop")
		}
	}()

	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	nextSigned := func(
		messageType string,
		nonce string,
	) linkproto.Message {
		return signedRelayRequest(
			t,
			manager,
			messageType,
			time.Now(),
			nonce,
		)
	}
	resign := func(message *linkproto.Message) {
		message.Signature = manager.CreateLinkSignature(
			linkproto.SignaturePayload(*message),
		)
	}

	wrongToken := nextSigned(linkproto.TypeRegister, "1")
	wrongToken.ConnectorToken = "wrong-token"
	sendConnectorReject(t, connectorListener.Addr().String(), wrongToken)

	wrongSignature := nextSigned(linkproto.TypeRegister, "2")
	wrongSignature.Signature = strings.Repeat("0", 128)
	sendConnectorReject(t, connectorListener.Addr().String(), wrongSignature)

	expired := signedRelayRequest(
		t,
		manager,
		linkproto.TypeRegister,
		time.Now().Add(-time.Hour),
		"3",
	)
	sendConnectorReject(t, connectorListener.Addr().String(), expired)

	replayed := nextSigned(linkproto.TypeRegister, "4")
	if err := server.verifySignedRequest(replayed); err != nil {
		t.Fatalf("seed replay nonce: %v", err)
	}
	sendConnectorReject(t, connectorListener.Addr().String(), replayed)

	sendConnectorReject(t, connectorListener.Addr().String(), linkproto.Message{
		Type: "unsupported",
	})

	routeConflict := nextSigned(linkproto.TypeRegister, "5")
	sendConnectorReject(t, connectorListener.Addr().String(), routeConflict)

	routeCapacity := nextSigned(linkproto.TypeRegister, "6")
	routeCapacity.RouteID = strings.Repeat("b", 32)
	resign(&routeCapacity)
	sendConnectorReject(t, connectorListener.Addr().String(), routeCapacity)

	admissionMissingRoute := nextSigned(
		linkproto.TypeAdmissionRequest,
		"7",
	)
	admissionMissingRoute.RouteID = strings.Repeat("c", 32)
	admissionMissingRoute.TTLSeconds = 1
	resign(&admissionMissingRoute)
	sendConnectorReject(
		t,
		connectorListener.Addr().String(),
		admissionMissingRoute,
	)

	consumeInvalid := nextSigned(linkproto.TypeAdmissionConsume, "8")
	consumeInvalid.Alias = strings.Repeat("d", 32)
	consumeInvalid.StreamID = strings.Repeat("e", 32)
	resign(&consumeInvalid)
	sendConnectorReject(t, connectorListener.Addr().String(), consumeInvalid)

	sendConnectorReject(t, connectorListener.Addr().String(), linkproto.Message{
		Type:         linkproto.TypeAttachStream,
		StreamID:     strings.Repeat("f", 32),
		StreamTicket: strings.Repeat("1", 64),
	})

	first := scrapeRelayMetrics(t, server)
	second := scrapeRelayMetrics(t, server)
	if first != second {
		t.Fatalf("repeated metrics scrape mutated counters:\nfirst=%s\nsecond=%s", first, second)
	}
	for _, expected := range []string{
		"zen_link_rejected_connections_total 10",
		`zen_link_rejected_connections_total{reason="auth"} 3`,
		`zen_link_rejected_connections_total{reason="replay"} 1`,
		`zen_link_rejected_connections_total{reason="protocol"} 2`,
		`zen_link_rejected_connections_total{reason="capacity"} 1`,
		`zen_link_rejected_connections_total{reason="ticket"} 3`,
		`zen_link_rejected_connections_total{reason="shutdown"} 0`,
	} {
		if !strings.Contains(first, expected) {
			t.Fatalf("rejection metrics missing %q:\n%s", expected, first)
		}
	}
	for _, forbidden := range []string{
		routeID,
		daemonKey,
		server.config.ConnectorToken,
		replayed.Nonce,
		strings.Repeat("1", 64),
	} {
		if strings.Contains(first, forbidden) {
			t.Fatalf("rejection metrics leaked identifier %q", forbidden)
		}
	}
}

func TestRelayConfigRejectsInvalidAndOverflowingValuesWithoutPanic(
	t *testing.T,
) {
	type integerField struct {
		name  string
		limit int
		set   func(*Config, int)
	}
	fields := []integerField{
		{"MaxRoutes", maxConfiguredRoutes, func(config *Config, value int) {
			config.MaxRoutes = value
		}},
		{"MaxClients", maxConfiguredClients, func(config *Config, value int) {
			config.MaxClients = value
		}},
		{"MaxClientsPerRoute", maxConfiguredClientsPerRoute, func(config *Config, value int) {
			config.MaxClientsPerRoute = value
		}},
		{"MaxClientHandshakes", maxConfiguredClientHandshakes, func(config *Config, value int) {
			config.MaxClientHandshakes = value
		}},
		{"MaxConnectorHandshakes", maxConfiguredConnectorHandshakes, func(config *Config, value int) {
			config.MaxConnectorHandshakes = value
		}},
		{"MaxAdmissions", maxConfiguredAdmissions, func(config *Config, value int) {
			config.MaxAdmissions = value
		}},
		{"MaxAdmissionsPerRoute", maxConfiguredAdmissionsPerRoute, func(config *Config, value int) {
			config.MaxAdmissionsPerRoute = value
		}},
		{"MaxNonces", maxConfiguredNonces, func(config *Config, value int) {
			config.MaxNonces = value
		}},
		{"MaxPendingStreams", maxConfiguredPendingStreams, func(config *Config, value int) {
			config.MaxPendingStreams = value
		}},
	}
	for _, field := range fields {
		for _, value := range []int{0, -1, field.limit + 1, math.MaxInt} {
			t.Run(field.name+"/"+integerName(value), func(t *testing.T) {
				config := validRelayConfig()
				field.set(&config, value)
				requireRelayConfigErrorWithoutPanic(t, config)
			})
		}
		t.Run(field.name+"/max", func(t *testing.T) {
			config := validRelayConfig()
			field.set(&config, field.limit)
			server, err := New(config)
			if err != nil {
				t.Fatalf("safe maximum rejected: %v", err)
			}
			if server == nil {
				t.Fatal("safe maximum returned nil server")
			}
		})
	}

	derivedOverflow := validRelayConfig()
	derivedOverflow.MaxClients = math.MaxInt
	derivedOverflow.MaxClientHandshakes = 0
	requireRelayConfigErrorWithoutPanic(t, derivedOverflow)

	durationFields := []struct {
		name string
		set  func(*Config, time.Duration)
	}{
		{"HandshakeTimeout", func(config *Config, value time.Duration) {
			config.HandshakeTimeout = value
		}},
		{"AttachTimeout", func(config *Config, value time.Duration) {
			config.AttachTimeout = value
		}},
		{"IdleTimeout", func(config *Config, value time.Duration) {
			config.IdleTimeout = value
		}},
		{"AuthMaxAge", func(config *Config, value time.Duration) {
			config.AuthMaxAge = value
		}},
		{"MaxAdmissionTTL", func(config *Config, value time.Duration) {
			config.MaxAdmissionTTL = value
		}},
		{"SweepInterval", func(config *Config, value time.Duration) {
			config.SweepInterval = value
		}},
	}
	for _, field := range durationFields {
		for _, value := range []time.Duration{0, -1} {
			t.Run(field.name+"/"+integerName(int(value)), func(t *testing.T) {
				config := validRelayConfig()
				field.set(&config, value)
				requireRelayConfigErrorWithoutPanic(t, config)
			})
		}
	}
}

func TestAdmissionTTLValidatesSecondsBeforeDurationConversion(t *testing.T) {
	server := limitedRelay(t)
	server.config.MaxAdmissionTTL = 15 * time.Minute
	routeID := strings.Repeat("a", 32)
	daemonKey := strings.Repeat("b", 64)
	server.routes[routeID] = &routeSession{
		routeID:         routeID,
		daemonPublicKey: daemonKey,
	}
	base := linkproto.Message{
		RouteID:         routeID,
		DaemonPublicKey: daemonKey,
	}
	maxSeconds := int64(server.config.MaxAdmissionTTL / time.Second)
	for _, ttl := range []int64{
		-1,
		-36028797018963128,
		0,
		maxSeconds + 1,
		math.MaxInt64,
	} {
		before := len(server.admissions)
		message := base
		message.TTLSeconds = ttl
		if _, _, err := server.createAdmission(message); err == nil {
			t.Fatalf("invalid TTLSeconds %d was accepted", ttl)
		}
		if len(server.admissions) != before {
			t.Fatalf(
				"invalid TTLSeconds %d consumed admission capacity",
				ttl,
			)
		}
	}

	boundary := base
	boundary.TTLSeconds = maxSeconds
	if _, _, err := server.createAdmission(boundary); err != nil {
		t.Fatalf("boundary TTLSeconds %d rejected: %v", maxSeconds, err)
	}
	if len(server.admissions) != 1 {
		t.Fatalf("boundary admission count=%d, want 1", len(server.admissions))
	}
}

type heldReadListener struct {
	net.Listener
	readStarted chan struct{}
	allowRead   chan struct{}
	closed      chan struct{}
	closeOnce   sync.Once
}

func newHeldReadListener(listener net.Listener) *heldReadListener {
	return &heldReadListener{
		Listener:    listener,
		readStarted: make(chan struct{}),
		allowRead:   make(chan struct{}),
		closed:      make(chan struct{}),
	}
}

func (listener *heldReadListener) Accept() (net.Conn, error) {
	conn, err := listener.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &heldReadConnection{
		Conn:        conn,
		readStarted: listener.readStarted,
		allowRead:   listener.allowRead,
	}, nil
}

func (listener *heldReadListener) Close() error {
	listener.closeOnce.Do(func() {
		close(listener.closed)
	})
	return listener.Listener.Close()
}

type heldReadConnection struct {
	net.Conn
	readStarted chan struct{}
	allowRead   chan struct{}
	startOnce   sync.Once
}

func (connection *heldReadConnection) Read(raw []byte) (int, error) {
	connection.startOnce.Do(func() {
		close(connection.readStarted)
	})
	<-connection.allowRead
	return connection.Conn.Read(raw)
}

func sendConnectorReject(
	t *testing.T,
	address string,
	message linkproto.Message,
) {
	t.Helper()
	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if err := linkproto.WriteMessage(conn, message); err != nil {
		t.Fatal(err)
	}
	response, err := linkproto.ReadMessage(conn)
	if err != nil {
		t.Fatalf("read protocol rejection: %v", err)
	}
	if response.Type != linkproto.TypeError {
		t.Fatalf("rejection response=%#v", response)
	}
}

func scrapeRelayMetrics(t *testing.T, server *Server) string {
	t.Helper()
	response := httptest.NewRecorder()
	server.OperatorHandler().ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/metrics", nil),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("metrics status=%d", response.Code)
	}
	return response.Body.String()
}

func validRelayConfig() Config {
	return Config{
		ConnectorToken:         "abcdef0123456789abcdef0123456789",
		MaxRoutes:              4,
		MaxClients:             4,
		MaxClientsPerRoute:     2,
		MaxClientHandshakes:    2,
		MaxConnectorHandshakes: 2,
		MaxAdmissions:          4,
		MaxAdmissionsPerRoute:  2,
		MaxNonces:              8,
		MaxPendingStreams:      4,
		HandshakeTimeout:       time.Second,
		AttachTimeout:          time.Second,
		IdleTimeout:            time.Minute,
		AuthMaxAge:             time.Minute,
		MaxAdmissionTTL:        time.Minute,
		SweepInterval:          time.Second,
	}
}

func requireRelayConfigErrorWithoutPanic(t *testing.T, config Config) {
	t.Helper()
	defer func() {
		if value := recover(); value != nil {
			t.Fatalf("New panicked for invalid config: %v", value)
		}
	}()
	if _, err := New(config); err == nil {
		t.Fatal("invalid relay config was accepted")
	}
}

func integerName(value int) string {
	switch value {
	case math.MaxInt:
		return "MaxInt"
	case -1:
		return "negative"
	case 0:
		return "zero"
	default:
		return "above-max"
	}
}
