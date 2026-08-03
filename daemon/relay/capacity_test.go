package relay

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/linkproto"
)

func TestRelayStateCapacityLimitsRejectAndRecover(t *testing.T) {
	t.Run("routes", func(t *testing.T) {
		server := limitedRelay(t)
		server.config.MaxRoutes = 1
		first, firstPeer := net.Pipe()
		defer firstPeer.Close()
		second, secondPeer := net.Pipe()
		defer secondPeer.Close()
		firstMessage := linkproto.Message{
			RouteID:         strings.Repeat("1", 32),
			DaemonPublicKey: strings.Repeat("a", 64),
		}
		firstRoute, err := server.registerRoute(first, firstMessage)
		if err != nil {
			t.Fatal(err)
		}
		secondMessage := linkproto.Message{
			RouteID:         strings.Repeat("2", 32),
			DaemonPublicKey: strings.Repeat("b", 64),
		}
		if _, err := server.registerRoute(second, secondMessage); err == nil ||
			!strings.Contains(err.Error(), "route capacity") {
			t.Fatalf("route saturation error=%v", err)
		}
		alias := strings.Repeat("3", 32)
		streamID := strings.Repeat("4", 32)
		server.mu.Lock()
		server.admissions[alias] = admission{
			routeID:        firstRoute.routeID,
			expiresAt:      time.Now().Add(time.Minute),
			reservedStream: streamID,
		}
		pending := newPendingForCapacityTest(
			strings.Repeat("5", 32),
			time.Now().Add(time.Minute),
		)
		server.pending[strings.Repeat("5", 32)] = pending
		server.mu.Unlock()
		if err := server.consumeAdmission(linkproto.Message{
			RouteID:         firstRoute.routeID,
			DaemonPublicKey: firstRoute.daemonPublicKey,
			Alias:           alias,
			StreamID:        streamID,
		}); err != nil {
			t.Fatalf("route saturation starved admission recovery: %v", err)
		}
		if _, err := server.claimPendingStream(linkproto.Message{
			StreamID:     strings.Repeat("5", 32),
			StreamTicket: pending.ticket,
		}); err != nil {
			t.Fatalf("route saturation starved stream attachment: %v", err)
		}
		server.unregisterRoute(firstRoute)
		if _, err := server.registerRoute(second, secondMessage); err != nil {
			t.Fatalf("route capacity did not recover: %v", err)
		}
	})

	t.Run("clients", func(t *testing.T) {
		server := limitedRelay(t)
		server.config.MaxClients = 1
		server.config.MaxClientsPerRoute = 1
		firstRoute := &routeSession{}
		secondRoute := &routeSession{}
		if !server.reserveClient(firstRoute) {
			t.Fatal("first client reservation failed")
		}
		if server.reserveClient(secondRoute) {
			t.Fatal("global client saturation was accepted")
		}
		server.releaseClient(firstRoute)
		if !server.reserveClient(secondRoute) {
			t.Fatal("global client capacity did not recover")
		}
		server.releaseClient(secondRoute)

		server.config.MaxClients = 2
		if !server.reserveClient(firstRoute) {
			t.Fatal("per-route first client reservation failed")
		}
		if server.reserveClient(firstRoute) {
			t.Fatal("per-route client saturation was accepted")
		}
		if !server.reserveClient(secondRoute) {
			t.Fatal("per-route saturation starved another route")
		}
		server.releaseClient(firstRoute)
		server.releaseClient(secondRoute)
	})

	t.Run("admissions", func(t *testing.T) {
		server := limitedRelay(t)
		server.config.MaxRoutes = 3
		server.config.MaxAdmissions = 2
		server.config.MaxAdmissionsPerRoute = 1
		for index, routeID := range []string{
			strings.Repeat("1", 32),
			strings.Repeat("2", 32),
			strings.Repeat("3", 32),
		} {
			server.routes[routeID] = &routeSession{
				routeID: routeID,
				daemonPublicKey: strings.Repeat(
					string(rune('a'+index)),
					64,
				),
			}
		}
		first := linkproto.Message{
			RouteID:         strings.Repeat("1", 32),
			DaemonPublicKey: strings.Repeat("a", 64),
			TTLSeconds:      1,
		}
		firstAlias, _, err := server.createAdmission(first)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := server.createAdmission(first); err == nil ||
			!strings.Contains(err.Error(), "per-route admission capacity") {
			t.Fatalf("per-route admission saturation error=%v", err)
		}
		second := linkproto.Message{
			RouteID:         strings.Repeat("2", 32),
			DaemonPublicKey: strings.Repeat("b", 64),
			TTLSeconds:      1,
		}
		if _, _, err := server.createAdmission(second); err != nil {
			t.Fatal(err)
		}
		third := linkproto.Message{
			RouteID:         strings.Repeat("3", 32),
			DaemonPublicKey: strings.Repeat("c", 64),
			TTLSeconds:      1,
		}
		if _, _, err := server.createAdmission(third); err == nil ||
			!strings.Contains(err.Error(), "admission capacity") {
			t.Fatalf("global admission saturation error=%v", err)
		}
		server.mu.Lock()
		delete(server.admissions, firstAlias)
		server.mu.Unlock()
		if _, _, err := server.createAdmission(third); err != nil {
			t.Fatalf("admission capacity did not recover: %v", err)
		}
	})

	t.Run("nonces", func(t *testing.T) {
		server := limitedRelay(t)
		server.config.MaxNonces = 1
		manager, err := auth.NewManager(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		first := signedRelayRequest(
			t,
			manager,
			linkproto.TypeRegister,
			time.Now(),
			"1",
		)
		if err := server.verifySignedRequest(first); err != nil {
			t.Fatal(err)
		}
		second := signedRelayRequest(
			t,
			manager,
			linkproto.TypeRegister,
			time.Now(),
			"2",
		)
		if err := server.verifySignedRequest(second); err == nil ||
			!strings.Contains(err.Error(), "nonce capacity") {
			t.Fatalf("nonce saturation error=%v", err)
		}
		server.sweep(time.Now().Add(server.config.AuthMaxAge * 3))
		third := signedRelayRequest(
			t,
			manager,
			linkproto.TypeRegister,
			time.Now(),
			"3",
		)
		if err := server.verifySignedRequest(third); err != nil {
			t.Fatalf("nonce capacity did not recover: %v", err)
		}
	})

	t.Run("pending", func(t *testing.T) {
		server := limitedRelay(t)
		server.config.MaxPendingStreams = 1
		firstID := strings.Repeat("1", 32)
		first := newPendingForCapacityTest(firstID, time.Now().Add(time.Minute))
		if !server.storePending(firstID, first) {
			t.Fatal("first pending stream was rejected")
		}
		secondID := strings.Repeat("2", 32)
		second := newPendingForCapacityTest(secondID, time.Now().Add(time.Minute))
		if server.storePending(secondID, second) {
			t.Fatal("pending stream saturation was accepted")
		}
		server.removePending(firstID, first)
		if !server.storePending(secondID, second) {
			t.Fatal("pending capacity did not recover")
		}
	})
}

func TestConnectorAndClientHandshakePermitsReleaseAfterRouting(t *testing.T) {
	t.Run("connector registration", func(t *testing.T) {
		server := limitedRelay(t)
		server.config.MaxConnectorHandshakes = 1
		server.connectorHandshakes = make(chan struct{}, 1)
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go server.acceptConnectors(ctx, listener)

		manager, err := auth.NewManager(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		register := signedRelayRequest(
			t,
			manager,
			linkproto.TypeRegister,
			time.Now(),
			"1",
		)
		first, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer first.Close()
		if err := linkproto.WriteMessage(first, register); err != nil {
			t.Fatal(err)
		}
		if message, err := linkproto.ReadMessage(first); err != nil ||
			message.Type != linkproto.TypeRegistered {
			t.Fatalf("register response=%#v err=%v", message, err)
		}

		admission := signedRelayRequest(
			t,
			manager,
			linkproto.TypeAdmissionRequest,
			time.Now(),
			"2",
		)
		admission.TTLSeconds = 1
		admission.Signature = manager.CreateLinkSignature(
			linkproto.SignaturePayload(admission),
		)
		second, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer second.Close()
		_ = second.SetDeadline(time.Now().Add(time.Second))
		if err := linkproto.WriteMessage(second, admission); err != nil {
			t.Fatal(err)
		}
		if message, err := linkproto.ReadMessage(second); err != nil ||
			message.Type != linkproto.TypeAdmissionResponse {
			t.Fatalf("admission response=%#v err=%v", message, err)
		}
	})

	t.Run("client routing", func(t *testing.T) {
		server := limitedRelay(t)
		server.config.MaxClientHandshakes = 1
		server.config.MaxClients = 2
		server.config.MaxClientsPerRoute = 2
		server.config.MaxPendingStreams = 2
		server.clientHandshakes = make(chan struct{}, 1)
		control, controlPeer := net.Pipe()
		defer control.Close()
		defer controlPeer.Close()
		routeID := strings.Repeat("a", 32)
		server.routes[routeID] = &routeSession{
			routeID: routeID,
			conn:    control,
		}
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go server.acceptClients(ctx, listener)

		connect := func() *tls.Conn {
			raw, err := net.Dial("tcp", listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			client := tls.Client(raw, &tls.Config{
				MinVersion:         tls.VersionTLS13,
				ServerName:         routeID + ".link.test",
				InsecureSkipVerify: true,
			})
			go client.Handshake()
			return client
		}
		first := connect()
		defer first.Close()
		_ = controlPeer.SetReadDeadline(time.Now().Add(time.Second))
		if message, err := linkproto.ReadMessage(controlPeer); err != nil ||
			message.Type != linkproto.TypeOpenStream {
			t.Fatalf("first open stream=%#v err=%v", message, err)
		}
		second := connect()
		defer second.Close()
		if message, err := linkproto.ReadMessage(controlPeer); err != nil ||
			message.Type != linkproto.TypeOpenStream {
			t.Fatalf("second open stream=%#v err=%v", message, err)
		}
	})
}

func TestHandshakeCapacityRejectsWithoutConsumingRecoveryState(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		fill     func(*Server)
		release  func(*Server)
		accept   func(*Server, context.Context, net.Listener) error
	}{
		{
			name:     "clients",
			resource: "client_handshakes",
			fill: func(server *Server) {
				server.config.MaxClientHandshakes = 1
				server.clientHandshakes = make(chan struct{}, 1)
				server.clientHandshakes <- struct{}{}
			},
			release: func(server *Server) {
				<-server.clientHandshakes
			},
			accept: (*Server).acceptClients,
		},
		{
			name:     "connectors",
			resource: "connector_handshakes",
			fill: func(server *Server) {
				server.config.MaxConnectorHandshakes = 1
				server.connectorHandshakes = make(chan struct{}, 1)
				server.connectorHandshakes <- struct{}{}
			},
			release: func(server *Server) {
				<-server.connectorHandshakes
			},
			accept: (*Server).acceptConnectors,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := limitedRelay(t)
			test.fill(server)
			defer test.release(server)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			acceptDone := make(chan error, 1)
			go func() {
				acceptDone <- test.accept(server, ctx, listener)
			}()
			conn, err := net.Dial("tcp", listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			_ = conn.SetReadDeadline(time.Now().Add(time.Second))
			if _, err := conn.Read(make([]byte, 1)); err == nil {
				t.Fatal("saturated handshake connection remained open")
			}
			_ = conn.Close()
			waitForRelayCondition(t, time.Second, func() bool {
				return server.capacityRejects[test.resource].Load() == 1
			})

			response := httptest.NewRecorder()
			server.OperatorHandler().ServeHTTP(
				response,
				httptest.NewRequest(http.MethodGet, "/metrics", nil),
			)
			body := response.Body.String()
			for _, expected := range []string{
				`zen_link_capacity_used{resource="` + test.resource + `"} 1`,
				`zen_link_capacity_rejections_total{reason="` +
					test.resource + `"} 1`,
			} {
				if !strings.Contains(body, expected) {
					t.Fatalf("handshake metrics missing %q:\n%s", expected, body)
				}
			}

			_ = listener.Close()
			select {
			case <-acceptDone:
			case <-time.After(time.Second):
				t.Fatal("handshake accept loop did not stop")
			}
		})
	}
}

func TestPeriodicSweepAndShutdownClearBoundedState(t *testing.T) {
	server := limitedRelay(t)
	server.config.SweepInterval = 5 * time.Millisecond
	server.config.AuthMaxAge = 10 * time.Millisecond
	now := time.Now()
	alias := strings.Repeat("a", 32)
	nonce := strings.Repeat("b", 64)
	streamID := strings.Repeat("c", 32)
	pending := newPendingForCapacityTest(
		streamID,
		now.Add(10*time.Millisecond),
	)
	routeConn, routePeer := net.Pipe()
	defer routePeer.Close()
	server.mu.Lock()
	server.admissions[alias] = admission{
		routeID:   strings.Repeat("d", 32),
		expiresAt: now.Add(10 * time.Millisecond),
	}
	server.nonces[nonce] = now.Add(-time.Second)
	server.pending[streamID] = pending
	server.routes[strings.Repeat("d", 32)] = &routeSession{
		routeID: strings.Repeat("d", 32),
		conn:    routeConn,
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

	waitForRelayCondition(t, time.Second, func() bool {
		server.mu.Lock()
		defer server.mu.Unlock()
		return len(server.admissions) == 0 &&
			len(server.nonces) == 0 &&
			len(server.pending) == 0
	})
	select {
	case <-pending.done:
	default:
		t.Fatal("periodic sweep removed pending state without waking its owner")
	}

	metrics := httptest.NewRecorder()
	server.OperatorHandler().ServeHTTP(
		metrics,
		httptest.NewRequest(http.MethodGet, "/metrics", nil),
	)
	body := metrics.Body.String()
	for _, expected := range []string{
		`zen_link_swept_entries_total{kind="admissions"} 1`,
		`zen_link_swept_entries_total{kind="nonces"} 1`,
		`zen_link_swept_entries_total{kind="pending"} 1`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, body)
		}
	}
	for _, forbidden := range []string{alias, nonce, streamID} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("metrics leaked identifier %q", forbidden)
		}
	}

	cancel()
	select {
	case err := <-serveDone:
		if err != nil && err != context.Canceled {
			t.Fatalf("Serve shutdown error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop its sweep/accept goroutines")
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.routes) != 0 ||
		len(server.admissions) != 0 ||
		len(server.nonces) != 0 ||
		len(server.pending) != 0 {
		t.Fatalf(
			"shutdown state routes=%d admissions=%d nonces=%d pending=%d",
			len(server.routes),
			len(server.admissions),
			len(server.nonces),
			len(server.pending),
		)
	}
}

func TestCapacityMetricsExposeCountsAndFixedReasonsOnly(t *testing.T) {
	server := limitedRelay(t)
	server.config.MaxRoutes = 1
	server.config.MaxClients = 1
	server.config.MaxClientsPerRoute = 1
	server.config.MaxAdmissions = 1
	server.config.MaxAdmissionsPerRoute = 1
	server.config.MaxNonces = 1
	server.config.MaxPendingStreams = 1
	routeID := strings.Repeat("1", 32)
	daemonKey := strings.Repeat("2", 64)
	control, controlPeer := net.Pipe()
	defer control.Close()
	defer controlPeer.Close()
	route := &routeSession{
		routeID:         routeID,
		daemonPublicKey: daemonKey,
		conn:            control,
	}
	server.routes[routeID] = route
	server.activeRoutes.Add(1)

	overflow, overflowPeer := net.Pipe()
	defer overflow.Close()
	defer overflowPeer.Close()
	_, _ = server.registerRoute(overflow, linkproto.Message{
		RouteID:         strings.Repeat("3", 32),
		DaemonPublicKey: strings.Repeat("4", 64),
	})
	_, _, _ = server.createAdmission(linkproto.Message{
		RouteID:         routeID,
		DaemonPublicKey: daemonKey,
		TTLSeconds:      1,
	})
	_, _, _ = server.createAdmission(linkproto.Message{
		RouteID:         routeID,
		DaemonPublicKey: daemonKey,
		TTLSeconds:      1,
	})
	server.config.MaxAdmissionsPerRoute = 2
	_, _, _ = server.createAdmission(linkproto.Message{
		RouteID:         routeID,
		DaemonPublicKey: daemonKey,
		TTLSeconds:      1,
	})
	server.config.MaxAdmissionsPerRoute = 1

	if !server.reserveClient(route) {
		t.Fatal("reserve client for global metric rejection")
	}
	_ = server.reserveClient(&routeSession{})
	server.releaseClient(route)
	server.config.MaxClients = 2
	if !server.reserveClient(route) {
		t.Fatal("reserve client for per-route metric rejection")
	}
	_ = server.reserveClient(route)
	server.releaseClient(route)
	server.config.MaxClients = 1

	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server.nonces["occupied"] = time.Now()
	_ = server.verifySignedRequest(signedRelayRequest(
		t,
		manager,
		linkproto.TypeRegister,
		time.Now(),
		"7",
	))
	firstPending := newPendingForCapacityTest(
		strings.Repeat("5", 32),
		time.Now().Add(time.Minute),
	)
	server.storePending(strings.Repeat("5", 32), firstPending)
	server.storePending(
		strings.Repeat("6", 32),
		newPendingForCapacityTest(
			strings.Repeat("6", 32),
			time.Now().Add(time.Minute),
		),
	)

	response := httptest.NewRecorder()
	server.OperatorHandler().ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/metrics", nil),
	)
	body := response.Body.String()
	for _, expected := range []string{
		`zen_link_capacity_used{resource="routes"} 1`,
		`zen_link_capacity_limit{resource="routes"} 1`,
		`zen_link_capacity_used{resource="admissions"} 1`,
		`zen_link_capacity_limit{resource="pending"} 1`,
		`zen_link_capacity_rejections_total{reason="routes"} 1`,
		`zen_link_capacity_rejections_total{reason="clients"} 1`,
		`zen_link_capacity_rejections_total{reason="clients_per_route"} 1`,
		`zen_link_capacity_rejections_total{reason="admissions"} 1`,
		`zen_link_capacity_rejections_total{reason="admissions_per_route"} 1`,
		`zen_link_capacity_rejections_total{reason="nonces"} 1`,
		`zen_link_capacity_rejections_total{reason="pending"} 1`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("capacity metrics missing %q:\n%s", expected, body)
		}
	}
	for _, forbidden := range []string{
		routeID,
		daemonKey,
		server.config.ConnectorToken,
		"device-id",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("capacity metrics leaked %q", forbidden)
		}
	}
}

func limitedRelay(t *testing.T) *Server {
	t.Helper()
	server, err := New(Config{
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
		HandshakeTimeout:       100 * time.Millisecond,
		AttachTimeout:          100 * time.Millisecond,
		IdleTimeout:            time.Second,
		AuthMaxAge:             time.Minute,
		MaxAdmissionTTL:        time.Minute,
		SweepInterval:          10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func newPendingForCapacityTest(
	streamID string,
	expiresAt time.Time,
) *pendingStream {
	return &pendingStream{
		routeID:   strings.Repeat("d", 32),
		ticket:    strings.Repeat("e", 64),
		connCh:    make(chan net.Conn),
		done:      make(chan struct{}),
		expiresAt: expiresAt,
	}
}

func waitForRelayCondition(
	t *testing.T,
	timeout time.Duration,
	condition func() bool,
) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for relay condition")
}
