package link

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/linkproto"
)

const (
	defaultControlTimeout = 5 * time.Second
	defaultReconnectMax   = 30 * time.Second
	defaultMaxStreams     = 32
)

type RelayCandidate struct {
	Name              string
	ControlAddress    string
	ControlServerName string
	ControlCAFile     string
	ControlRootCAs    *x509.CertPool
	ClientDomain      string
	ClientPort        int
}

type ConnectorConfig struct {
	ConnectorToken    string
	Candidates        []RelayCandidate
	ControlTimeout    time.Duration
	ReconnectMax      time.Duration
	MaxStreams        int
	HTTPIdleTimeout   time.Duration
	KeepAliveInterval time.Duration
	StateObserver     func(ConnectorState)
}

type ConnectorState struct {
	Phase       string
	Relay       string
	LastError   string
	MeasuredRTT time.Duration
	ChangedAt   time.Time
}

type AdmissionOffer struct {
	RelayName     string
	AdmissionHost string
	StableHost    string
	AdmissionURL  string
	StableURL     string
	ExpiresAt     time.Time
}

type Connector struct {
	config   ConnectorConfig
	auth     *auth.Manager
	identity *TransportIdentity
	handler  http.Handler
	streams  chan struct{}

	stateMu sync.RWMutex
	state   ConnectorState
}

type registeredRelay struct {
	candidate RelayCandidate
	conn      *tls.Conn
	rtt       time.Duration
	writeMu   sync.Mutex
}

func NewConnector(
	config ConnectorConfig,
	authManager *auth.Manager,
	identity *TransportIdentity,
	handler http.Handler,
) (*Connector, error) {
	normalized, err := normalizeConnectorConfig(config)
	if err != nil {
		return nil, err
	}
	if authManager == nil {
		return nil, errors.New("daemon auth manager is required")
	}
	if identity == nil || identity.RouteID == "" || identity.SPKISHA256 == "" {
		return nil, errors.New("Link transport identity is required")
	}
	if handler == nil {
		return nil, errors.New("daemon HTTP handler is required")
	}
	return &Connector{
		config:   normalized,
		auth:     authManager,
		identity: identity,
		handler:  handler,
		streams:  make(chan struct{}, normalized.MaxStreams),
		state: ConnectorState{
			Phase:     "connecting",
			ChangedAt: time.Now().UTC(),
		},
	}, nil
}

func (connector *Connector) State() ConnectorState {
	connector.stateMu.RLock()
	defer connector.stateMu.RUnlock()
	return connector.state
}

func (connector *Connector) Run(ctx context.Context) error {
	backoff := time.Second
	for {
		if err := ctx.Err(); err != nil {
			connector.setState("offline", "", 0, "")
			return err
		}
		connector.setState("connecting", "", 0, "")
		selected, err := connector.selectRelay(ctx)
		if err == nil {
			connector.setState("connected", selected.candidate.Name, selected.rtt, "")
			startedAt := time.Now()
			err = connector.runRegisteredRelay(ctx, selected)
			if ctx.Err() != nil {
				connector.setState("offline", "", 0, "")
				return ctx.Err()
			}
			if time.Since(startedAt) >= 30*time.Second {
				backoff = time.Second
			}
		}
		connector.setState("offline", "", 0, userFacingConnectorError(err))

		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			connector.setState("offline", "", 0, "")
			return ctx.Err()
		case <-timer.C:
		}
		if backoff < connector.config.ReconnectMax {
			backoff *= 2
			if backoff > connector.config.ReconnectMax {
				backoff = connector.config.ReconnectMax
			}
		}
	}
}

func (connector *Connector) selectRelay(ctx context.Context) (*registeredRelay, error) {
	type result struct {
		relay *registeredRelay
		err   error
	}
	results := make(chan result, len(connector.config.Candidates))
	for _, candidate := range connector.config.Candidates {
		candidate := candidate
		go func() {
			relay, err := registerRelay(
				ctx,
				connector.config,
				candidate,
				connector.auth,
				connector.identity,
			)
			results <- result{relay: relay, err: err}
		}()
	}

	var connected []*registeredRelay
	var failures []string
	for range connector.config.Candidates {
		result := <-results
		if result.err != nil {
			failures = append(failures, result.err.Error())
			continue
		}
		connected = append(connected, result.relay)
	}
	if len(connected) == 0 {
		sort.Strings(failures)
		return nil, fmt.Errorf("no Link relay is available: %s", strings.Join(failures, "; "))
	}
	sort.Slice(connected, func(left, right int) bool {
		if connected[left].rtt == connected[right].rtt {
			return candidateKey(connected[left].candidate) < candidateKey(connected[right].candidate)
		}
		return connected[left].rtt < connected[right].rtt
	})
	for _, standby := range connected[1:] {
		_ = standby.conn.Close()
	}
	return connected[0], nil
}

func (connector *Connector) runRegisteredRelay(
	ctx context.Context,
	selected *registeredRelay,
) error {
	defer selected.conn.Close()
	pingErrors := make(chan error, 1)
	pingDone := make(chan struct{})
	defer close(pingDone)
	go func() {
		ticker := time.NewTicker(connector.config.KeepAliveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := selected.write(linkproto.Message{
					Type:        linkproto.TypePing,
					TimestampMS: time.Now().UnixMilli(),
				}); err != nil {
					select {
					case pingErrors <- err:
					default:
					}
					_ = selected.conn.Close()
					return
				}
			case <-pingDone:
				return
			case <-ctx.Done():
				_ = selected.conn.SetDeadline(time.Now())
				_ = selected.conn.Close()
				return
			}
		}
	}()
	for {
		_ = selected.conn.SetReadDeadline(
			time.Now().Add(3 * connector.config.KeepAliveInterval),
		)
		message, err := linkproto.ReadMessage(selected.conn)
		if err != nil {
			select {
			case pingErr := <-pingErrors:
				return fmt.Errorf("Link relay %s keepalive failed: %w", selected.candidate.Name, pingErr)
			default:
			}
			return fmt.Errorf("Link relay %s disconnected: %w", selected.candidate.Name, err)
		}
		switch message.Type {
		case linkproto.TypeOpenStream:
			if !linkproto.IsOpaqueID(message.StreamID, 16) ||
				!linkproto.IsOpaqueID(message.StreamTicket, 32) ||
				(message.Alias != "" && !linkproto.IsOpaqueID(message.Alias, 16)) {
				return errors.New("Link relay sent an invalid stream ticket")
			}
			select {
			case connector.streams <- struct{}{}:
				go func() {
					defer func() { <-connector.streams }()
					_ = connector.serveStream(ctx, selected.candidate, message)
				}()
			default:
				// The relay enforces a short attach deadline and returns a
				// clear overload failure to the client without buffering.
			}
		case linkproto.TypePing:
			if err := selected.write(linkproto.Message{
				Type:        linkproto.TypePong,
				TimestampMS: message.TimestampMS,
			}); err != nil {
				return err
			}
		case linkproto.TypePong:
		case linkproto.TypeError:
			return fmt.Errorf("Link relay rejected the route: %s", message.Message)
		default:
			return fmt.Errorf("Link relay sent unsupported message %q", message.Type)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

func (relay *registeredRelay) write(message linkproto.Message) error {
	relay.writeMu.Lock()
	defer relay.writeMu.Unlock()
	return linkproto.WriteMessage(relay.conn, message)
}

func (connector *Connector) serveStream(
	ctx context.Context,
	candidate RelayCandidate,
	open linkproto.Message,
) error {
	conn, err := dialControl(ctx, connector.config, candidate)
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			_ = conn.Close()
		}
	}()
	if err := linkproto.WriteMessage(conn, linkproto.Message{
		Type:         linkproto.TypeAttachStream,
		StreamID:     open.StreamID,
		StreamTicket: open.StreamTicket,
	}); err != nil {
		return err
	}
	response, err := linkproto.ReadMessage(conn)
	if err != nil {
		return err
	}
	if response.Type != linkproto.TypeAttached ||
		response.StreamID != open.StreamID {
		if response.Type == linkproto.TypeError {
			return fmt.Errorf("stream attachment rejected: %s", response.Message)
		}
		return errors.New("unexpected stream attachment response")
	}
	_ = conn.SetDeadline(time.Time{})

	inner := tls.Server(conn, connector.identity.ServerTLSConfig())
	handshakeCtx, cancel := context.WithTimeout(ctx, connector.config.ControlTimeout)
	defer cancel()
	if err := inner.HandshakeContext(handshakeCtx); err != nil {
		return fmt.Errorf("inner Link TLS handshake: %w", err)
	}
	handler := connector.handler
	if open.Alias != "" {
		handler = connector.pairingAdmissionHandler(ctx, candidate, open)
	}
	keep = true
	return serveSingleConnection(ctx, inner, handler, connector.config.HTTPIdleTimeout)
}

func (connector *Connector) pairingAdmissionHandler(
	ctx context.Context,
	candidate RelayCandidate,
	open linkproto.Message,
) http.Handler {
	var consumeOnce sync.Once
	var consumeErr error
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Connection", "close")
		request.Close = true
		if request.Method != http.MethodPost || request.URL.Path != "/pair" {
			http.Error(
				writer,
				"This one-time Zen Link admission only accepts the pairing request. Scan the QR code again.",
				http.StatusForbidden,
			)
			return
		}
		consumeOnce.Do(func() {
			consumeErr = connector.consumeAdmission(ctx, candidate, open)
		})
		if consumeErr != nil {
			http.Error(
				writer,
				"Zen Link pairing admission is unavailable or was already used. Scan a fresh QR code.",
				http.StatusUnauthorized,
			)
			return
		}
		connector.handler.ServeHTTP(writer, request)
	})
}

func (connector *Connector) consumeAdmission(
	ctx context.Context,
	candidate RelayCandidate,
	open linkproto.Message,
) error {
	conn, err := dialControl(ctx, connector.config, candidate)
	if err != nil {
		return fmt.Errorf("dial relay admission commit: %w", err)
	}
	defer conn.Close()
	message, err := signedRequest(
		connector.auth,
		connector.identity.RouteID,
		connector.config.ConnectorToken,
		linkproto.TypeAdmissionConsume,
		0,
	)
	if err != nil {
		return err
	}
	message.Alias = strings.ToLower(open.Alias)
	message.StreamID = strings.ToLower(open.StreamID)
	message.Signature = connector.auth.CreateLinkSignature(
		linkproto.SignaturePayload(message),
	)
	if err := linkproto.WriteMessage(conn, message); err != nil {
		return err
	}
	response, err := linkproto.ReadMessage(conn)
	if err != nil {
		return err
	}
	if response.Type == linkproto.TypeError {
		return errors.New(response.Message)
	}
	if response.Type != linkproto.TypeAdmissionConsumed ||
		response.RouteID != connector.identity.RouteID ||
		response.Alias != message.Alias ||
		response.StreamID != message.StreamID {
		return errors.New("relay returned an invalid admission commit")
	}
	return nil
}

func IssueAdmissions(
	ctx context.Context,
	config ConnectorConfig,
	authManager *auth.Manager,
	identity *TransportIdentity,
	ttl time.Duration,
) ([]AdmissionOffer, error) {
	normalized, err := normalizeConnectorConfig(config)
	if err != nil {
		return nil, err
	}
	if authManager == nil || identity == nil {
		return nil, errors.New("daemon auth and Link transport identity are required")
	}
	if ttl <= 0 {
		return nil, errors.New("admission TTL must be positive")
	}

	offers := make([]AdmissionOffer, 0, len(normalized.Candidates))
	var failures []string
	for _, candidate := range normalized.Candidates {
		conn, dialErr := dialControl(ctx, normalized, candidate)
		if dialErr != nil {
			failures = append(failures, candidate.Name+": "+dialErr.Error())
			continue
		}
		message, messageErr := signedRequest(
			authManager,
			identity.RouteID,
			normalized.ConnectorToken,
			linkproto.TypeAdmissionRequest,
			ttl,
		)
		if messageErr == nil {
			messageErr = linkproto.WriteMessage(conn, message)
		}
		var response linkproto.Message
		if messageErr == nil {
			response, messageErr = linkproto.ReadMessage(conn)
		}
		_ = conn.Close()
		if messageErr != nil {
			failures = append(failures, candidate.Name+": "+messageErr.Error())
			continue
		}
		if response.Type == linkproto.TypeError {
			failures = append(failures, candidate.Name+": "+response.Message)
			continue
		}
		if response.Type != linkproto.TypeAdmissionResponse ||
			!linkproto.IsOpaqueID(response.Alias, 16) {
			failures = append(failures, candidate.Name+": invalid admission response")
			continue
		}
		admissionHost := response.Alias + "." + candidate.ClientDomain
		stableHost := identity.RouteID + "." + candidate.ClientDomain
		offers = append(offers, AdmissionOffer{
			RelayName:     candidate.Name,
			AdmissionHost: admissionHost,
			StableHost:    stableHost,
			AdmissionURL:  candidateURL(admissionHost, candidate.ClientPort),
			StableURL:     candidateURL(stableHost, candidate.ClientPort),
			ExpiresAt:     time.UnixMilli(response.ExpiresAtMS).UTC(),
		})
	}
	if len(offers) == 0 {
		sort.Strings(failures)
		return nil, fmt.Errorf("no Link relay issued an admission: %s", strings.Join(failures, "; "))
	}
	sort.Slice(offers, func(left, right int) bool {
		return offers[left].RelayName < offers[right].RelayName
	})
	return offers, nil
}

func registerRelay(
	ctx context.Context,
	config ConnectorConfig,
	candidate RelayCandidate,
	authManager *auth.Manager,
	identity *TransportIdentity,
) (*registeredRelay, error) {
	startedAt := time.Now()
	conn, err := dialControl(ctx, config, candidate)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", candidate.Name, err)
	}
	message, err := signedRequest(
		authManager,
		identity.RouteID,
		config.ConnectorToken,
		linkproto.TypeRegister,
		0,
	)
	if err == nil {
		err = linkproto.WriteMessage(conn, message)
	}
	var response linkproto.Message
	if err == nil {
		response, err = linkproto.ReadMessage(conn)
	}
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%s registration: %w", candidate.Name, err)
	}
	if response.Type == linkproto.TypeError {
		_ = conn.Close()
		return nil, fmt.Errorf("%s registration: %s", candidate.Name, response.Message)
	}
	if response.Type != linkproto.TypeRegistered ||
		response.RouteID != identity.RouteID {
		_ = conn.Close()
		return nil, fmt.Errorf("%s registration returned an invalid response", candidate.Name)
	}
	_ = conn.SetDeadline(time.Time{})
	return &registeredRelay{
		candidate: candidate,
		conn:      conn,
		rtt:       time.Since(startedAt),
	}, nil
}

func signedRequest(
	authManager *auth.Manager,
	routeID string,
	connectorToken string,
	messageType string,
	ttl time.Duration,
) (linkproto.Message, error) {
	nonce, err := linkproto.RandomID(16)
	if err != nil {
		return linkproto.Message{}, err
	}
	message := linkproto.Message{
		Type:            messageType,
		ConnectorToken:  connectorToken,
		RouteID:         routeID,
		DaemonID:        authManager.DaemonID(),
		DaemonPublicKey: authManager.PublicKeyHex(),
		TimestampMS:     time.Now().UnixMilli(),
		Nonce:           nonce,
		TTLSeconds:      int64(ttl / time.Second),
	}
	message.Signature = authManager.CreateLinkSignature(
		linkproto.SignaturePayload(message),
	)
	return message, nil
}

func dialControl(
	ctx context.Context,
	config ConnectorConfig,
	candidate RelayCandidate,
) (*tls.Conn, error) {
	tlsConfig, err := candidateTLSConfig(candidate)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: config.ControlTimeout, KeepAlive: 30 * time.Second}
	raw, err := dialer.DialContext(ctx, "tcp", candidate.ControlAddress)
	if err != nil {
		return nil, err
	}
	conn := tls.Client(raw, tlsConfig)
	handshakeCtx, cancel := context.WithTimeout(ctx, config.ControlTimeout)
	defer cancel()
	if err := conn.HandshakeContext(handshakeCtx); err != nil {
		_ = raw.Close()
		return nil, err
	}
	if conn.ConnectionState().NegotiatedProtocol != linkproto.ControlALPN {
		_ = conn.Close()
		return nil, errors.New("relay did not negotiate Zen Link control protocol v2")
	}
	_ = conn.SetDeadline(time.Now().Add(config.ControlTimeout))
	return conn, nil
}

func candidateTLSConfig(candidate RelayCandidate) (*tls.Config, error) {
	roots := candidate.ControlRootCAs
	if roots == nil && candidate.ControlCAFile != "" {
		raw, err := os.ReadFile(candidate.ControlCAFile)
		if err != nil {
			return nil, fmt.Errorf("read relay CA file: %w", err)
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(raw) {
			return nil, errors.New("relay CA file contains no certificates")
		}
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: candidate.ControlServerName,
		RootCAs:    roots,
		NextProtos: []string{linkproto.ControlALPN},
	}, nil
}

func normalizeConnectorConfig(config ConnectorConfig) (ConnectorConfig, error) {
	config.ConnectorToken = strings.TrimSpace(config.ConnectorToken)
	if config.ConnectorToken == "" {
		return ConnectorConfig{}, errors.New("Link connector token is required")
	}
	if len(config.ConnectorToken) < 32 {
		return ConnectorConfig{}, errors.New("Link connector token must contain at least 32 characters")
	}
	if len(config.Candidates) == 0 {
		return ConnectorConfig{}, errors.New("at least one Link relay candidate is required")
	}
	if len(config.Candidates) > 16 {
		return ConnectorConfig{}, errors.New("at most 16 Link relay candidates are supported")
	}
	if config.ControlTimeout <= 0 {
		config.ControlTimeout = defaultControlTimeout
	}
	if config.ReconnectMax <= 0 {
		config.ReconnectMax = defaultReconnectMax
	}
	if config.MaxStreams <= 0 {
		config.MaxStreams = defaultMaxStreams
	}
	if config.HTTPIdleTimeout <= 0 {
		config.HTTPIdleTimeout = 2 * time.Minute
	}
	if config.KeepAliveInterval <= 0 {
		config.KeepAliveInterval = 15 * time.Second
	}
	seen := make(map[string]struct{})
	for index := range config.Candidates {
		candidate := &config.Candidates[index]
		candidate.Name = strings.TrimSpace(candidate.Name)
		candidate.ControlAddress = strings.TrimSpace(candidate.ControlAddress)
		candidate.ControlServerName = strings.TrimSpace(candidate.ControlServerName)
		candidate.ControlCAFile = strings.TrimSpace(candidate.ControlCAFile)
		candidate.ClientDomain = strings.ToLower(
			strings.TrimSuffix(strings.TrimSpace(candidate.ClientDomain), "."),
		)
		if candidate.Name == "" {
			candidate.Name = candidate.ControlAddress
		}
		if candidate.ControlAddress == "" ||
			candidate.ControlServerName == "" ||
			candidate.ClientDomain == "" {
			return ConnectorConfig{}, fmt.Errorf(
				"relay candidate %d requires control address, TLS server name, and client domain",
				index,
			)
		}
		if candidate.ClientPort <= 0 {
			candidate.ClientPort = 443
		}
		if candidate.ClientPort > 65535 {
			return ConnectorConfig{}, fmt.Errorf("relay candidate %q has an invalid client port", candidate.Name)
		}
		key := candidateKey(*candidate)
		if _, exists := seen[key]; exists {
			return ConnectorConfig{}, fmt.Errorf("duplicate relay candidate %q", candidate.Name)
		}
		seen[key] = struct{}{}
	}
	sort.Slice(config.Candidates, func(left, right int) bool {
		return candidateKey(config.Candidates[left]) < candidateKey(config.Candidates[right])
	})
	return config, nil
}

func candidateKey(candidate RelayCandidate) string {
	return candidate.Name + "\x00" + candidate.ControlAddress
}

func candidateURL(host string, port int) string {
	if port == 443 {
		return "wss://" + host + "/ws"
	}
	return "wss://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/ws"
}

func userFacingConnectorError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 240 {
		return message[:240]
	}
	return message
}

func (connector *Connector) setState(
	phase string,
	relay string,
	rtt time.Duration,
	lastError string,
) {
	connector.stateMu.Lock()
	connector.state = ConnectorState{
		Phase:       phase,
		Relay:       relay,
		LastError:   lastError,
		MeasuredRTT: rtt,
		ChangedAt:   time.Now().UTC(),
	}
	snapshot := connector.state
	connector.stateMu.Unlock()
	if connector.config.StateObserver != nil {
		connector.config.StateObserver(snapshot)
	}
}

type singleConnListener struct {
	conn     net.Conn
	once     sync.Once
	done     chan struct{}
	doneOnce sync.Once
}

func (listener *singleConnListener) Accept() (net.Conn, error) {
	var accepted net.Conn
	listener.once.Do(func() {
		accepted = &closeObservedConn{
			Conn:     listener.conn,
			done:     listener.done,
			doneOnce: &listener.doneOnce,
		}
	})
	if accepted != nil {
		return accepted, nil
	}
	<-listener.done
	return nil, net.ErrClosed
}

func (listener *singleConnListener) Close() error {
	listener.doneOnce.Do(func() { close(listener.done) })
	return listener.conn.Close()
}

func (listener *singleConnListener) Addr() net.Addr {
	return listener.conn.LocalAddr()
}

type closeObservedConn struct {
	net.Conn
	done     chan struct{}
	doneOnce *sync.Once
}

func (conn *closeObservedConn) Close() error {
	conn.doneOnce.Do(func() { close(conn.done) })
	return conn.Conn.Close()
}

func serveSingleConnection(
	ctx context.Context,
	conn net.Conn,
	handler http.Handler,
	idleTimeout time.Duration,
) error {
	listener := &singleConnListener{
		conn: conn,
		done: make(chan struct{}),
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       idleTimeout,
	}
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()
		case <-stop:
		}
	}()
	err := server.Serve(listener)
	close(stop)
	if errors.Is(err, net.ErrClosed) || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
