package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/terminal"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/gorilla/websocket"
)

func TestDeviceRevokeImmediatelyClosesAuthenticatedWebSocket(t *testing.T) {
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	server := New(
		manager,
		watcher.New(time.Second),
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	var actions atomic.Int64
	server.sendActionOverride = func(agentID string, action string) error {
		actions.Add(1)
		return nil
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	socketURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"
	header := http.Header{}
	header.Set(
		"Authorization",
		calendarAuthHeader(
			privateKey,
			manager.DaemonID(),
			deviceID,
			"zen-connect",
		),
	)
	conn, _, err := websocket.DefaultDialer.Dial(socketURL, header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(clientMessage{
		Type:      "send_action",
		RequestID: "before-revoke",
		AgentID:   "agent-one",
		Action:    "continue",
	}); err != nil {
		t.Fatal(err)
	}
	readWebSocketType(t, conn, "action_sent")
	if got := actions.Load(); got != 1 {
		t.Fatalf("actions before revoke=%d, want 1", got)
	}

	revokeRequest := httptest.NewRequest(
		http.MethodDelete,
		"/devices",
		bytes.NewBufferString(`{"device_id":"`+deviceID+`"}`),
	)
	revokeRequest.Header.Set(
		"Authorization",
		deviceAdminAuthorization(
			t,
			privateKey,
			manager.DaemonID(),
			deviceID,
			auth.DeviceRevokePurpose(deviceID),
		),
	)
	revokeResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusOK {
		t.Fatalf(
			"revoke status=%d body=%q",
			revokeResponse.Code,
			revokeResponse.Body.String(),
		)
	}
	waitForWebSocketClose(t, conn)

	_ = conn.WriteJSON(clientMessage{
		Type:      "send_action",
		RequestID: "after-revoke",
		AgentID:   "agent-one",
		Action:    "continue",
	})
	time.Sleep(25 * time.Millisecond)
	if got := actions.Load(); got != 1 {
		t.Fatalf("revoked socket executed %d actions, want 1", got)
	}

	reconnectHeader := http.Header{}
	reconnectHeader.Set(
		"Authorization",
		calendarAuthHeader(
			privateKey,
			manager.DaemonID(),
			deviceID,
			"zen-connect",
		),
	)
	reconnected, response, reconnectErr := websocket.DefaultDialer.Dial(
		socketURL,
		reconnectHeader,
	)
	if reconnected != nil {
		_ = reconnected.Close()
	}
	if reconnectErr == nil ||
		response == nil ||
		response.StatusCode != http.StatusUnauthorized {
		t.Fatalf(
			"revoked reconnect conn=%v status=%v err=%v",
			reconnected,
			responseStatus(response),
			reconnectErr,
		)
	}
}

func TestMessageAdmissionUsesSoleManagerStateBeforeSocketCallback(t *testing.T) {
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	server := New(
		manager,
		watcher.New(time.Second),
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	server.authRevocationUnsubscribe()
	server.authRevocationUnsubscribe = nil
	var actions atomic.Int64
	server.sendActionOverride = func(agentID string, action string) error {
		actions.Add(1)
		return nil
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	socketURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"
	conn := dialDeviceWebSocket(
		t,
		socketURL,
		privateKey,
		manager.DaemonID(),
		deviceID,
	)
	defer conn.Close()

	if _, err := manager.RevokeDevice(deviceID); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(clientMessage{
		Type:      "send_action",
		RequestID: "after-manager-commit",
		AgentID:   "agent-one",
		Action:    "continue",
	}); err != nil {
		t.Fatal(err)
	}
	waitForWebSocketClose(t, conn)
	if got := actions.Load(); got != 0 {
		t.Fatalf("post-revoke Manager state admitted %d commands", got)
	}
}

func TestConcurrentRevokeCloseAndReconnectConverges(t *testing.T) {
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	server := New(
		manager,
		watcher.New(time.Second),
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	socketURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"

	const socketCount = 8
	sockets := make([]*websocket.Conn, 0, socketCount)
	for index := 0; index < socketCount; index++ {
		sockets = append(
			sockets,
			dialDeviceWebSocket(
				t,
				socketURL,
				privateKey,
				manager.DaemonID(),
				deviceID,
			),
		)
	}

	start := make(chan struct{})
	revokeDone := make(chan error, 1)
	go func() {
		<-start
		_, err := manager.RevokeDevice(deviceID)
		revokeDone <- err
	}()
	var closeWait sync.WaitGroup
	for index, conn := range sockets {
		if index%2 != 0 {
			continue
		}
		closeWait.Add(1)
		go func(value *websocket.Conn) {
			defer closeWait.Done()
			<-start
			_ = value.Close()
		}(conn)
	}
	reconnectResult := make(chan *websocket.Conn, 1)
	go func() {
		<-start
		header := http.Header{}
		header.Set(
			"Authorization",
			calendarAuthHeader(
				privateKey,
				manager.DaemonID(),
				deviceID,
				"zen-connect",
			),
		)
		conn, _, _ := websocket.DefaultDialer.Dial(socketURL, header)
		reconnectResult <- conn
	}()
	close(start)

	if err := <-revokeDone; err != nil {
		t.Fatal(err)
	}
	closeWait.Wait()
	if racedReconnect := <-reconnectResult; racedReconnect != nil {
		waitForWebSocketClose(t, racedReconnect)
		_ = racedReconnect.Close()
	}
	for _, conn := range sockets {
		waitForWebSocketClose(t, conn)
		_ = conn.Close()
	}
	if got := server.clientCount(); got != 0 {
		t.Fatalf("authenticated clients after concurrent revoke=%d", got)
	}
}

func TestRevokeAndShutdownDoNotWaitForAdmittedCommand(t *testing.T) {
	for _, operation := range []string{"revoke", "shutdown"} {
		t.Run(operation, func(t *testing.T) {
			manager, privateKey, deviceID := sessionFileAuthFixture(t)
			server := New(
				manager,
				watcher.New(time.Second),
				nil,
				nil,
				nil,
				nil,
				nil,
			)
			entered := make(chan struct{})
			release := make(chan struct{})
			var actions atomic.Int64
			server.sendActionOverride = func(agentID string, action string) error {
				if actions.Add(1) == 1 {
					close(entered)
				}
				<-release
				return nil
			}
			httpServer := httptest.NewServer(server.Handler())
			socketURL := "ws" +
				strings.TrimPrefix(httpServer.URL, "http") +
				"/ws"
			conn := dialDeviceWebSocket(
				t,
				socketURL,
				privateKey,
				manager.DaemonID(),
				deviceID,
			)
			if err := conn.WriteJSON(clientMessage{
				Type:      "send_action",
				RequestID: "blocked-command",
				AgentID:   "agent-one",
				Action:    "continue",
			}); err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("command handler did not block")
			}

			done := make(chan error, 1)
			go func() {
				if operation == "revoke" {
					_, err := manager.RevokeDevice(deviceID)
					done <- err
					return
				}
				server.shutdownAuthenticatedClients()
				done <- nil
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(250 * time.Millisecond):
				t.Fatalf("%s waited for admitted command", operation)
			}
			waitForWebSocketClose(t, conn)
			_ = conn.WriteJSON(clientMessage{
				Type:      "send_action",
				RequestID: "must-not-start",
				AgentID:   "agent-two",
				Action:    "continue",
			})
			close(release)
			time.Sleep(25 * time.Millisecond)
			if got := actions.Load(); got != 1 {
				t.Fatalf("%s admitted %d commands, want 1", operation, got)
			}
			if operation == "revoke" && manager.IsDeviceTrusted(deviceID) {
				t.Fatal("revoke returned before persistence applied")
			}
			_ = conn.Close()
			httpServer.Close()
		})
	}
}

type blockingTerminalBackend struct {
	session terminal.Session
	openErr error
}

func (b *blockingTerminalBackend) Name() string {
	return "blocking"
}

func (b *blockingTerminalBackend) Open(
	string,
	terminal.OpenOptions,
) (terminal.Session, error) {
	return b.session, b.openErr
}

type blockingTerminalSession struct {
	id           string
	events       chan terminal.Event
	writeEntered chan struct{}
	releaseWrite chan struct{}
	closeEntered chan struct{}
	releaseClose chan struct{}
	canceled     chan struct{}
	writeOnce    sync.Once
	closeOnce    sync.Once
	cancelOnce   sync.Once
	eventsOnce   sync.Once
	closeCalls   atomic.Int32
}

func newBlockingTerminalSession() *blockingTerminalSession {
	return &blockingTerminalSession{
		id:           "blocked-terminal",
		events:       make(chan terminal.Event),
		writeEntered: make(chan struct{}),
		releaseWrite: make(chan struct{}),
		closeEntered: make(chan struct{}),
		releaseClose: make(chan struct{}),
		canceled:     make(chan struct{}),
	}
}

func (s *blockingTerminalSession) ID() string {
	return s.id
}

func (s *blockingTerminalSession) Start(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		s.cancelOnce.Do(func() { close(s.canceled) })
	}()
	return nil
}

func (s *blockingTerminalSession) Events() <-chan terminal.Event {
	return s.events
}

func (s *blockingTerminalSession) Write(string) error {
	s.writeOnce.Do(func() { close(s.writeEntered) })
	<-s.releaseWrite
	return nil
}

func (s *blockingTerminalSession) Resize(int, int) error {
	return nil
}

func (s *blockingTerminalSession) Close() error {
	s.closeCalls.Add(1)
	s.closeOnce.Do(func() { close(s.closeEntered) })
	<-s.releaseClose
	s.eventsOnce.Do(func() { close(s.events) })
	return nil
}

func (s *blockingTerminalSession) Size() terminal.Size {
	return terminal.Size{Cols: 80, Rows: 24}
}

type pendingStartTerminalSession struct {
	id            string
	events        chan terminal.Event
	startErr      error
	startEntered  chan struct{}
	startCanceled chan struct{}
	closeEntered  chan struct{}
	releaseClose  chan struct{}
	startOnce     sync.Once
	cancelOnce    sync.Once
	closeOnce     sync.Once
	eventsOnce    sync.Once
	closeCalls    atomic.Int32
}

func newPendingStartTerminalSession() *pendingStartTerminalSession {
	return &pendingStartTerminalSession{
		id:            "pending-terminal",
		events:        make(chan terminal.Event),
		startEntered:  make(chan struct{}),
		startCanceled: make(chan struct{}),
		closeEntered:  make(chan struct{}),
		releaseClose:  make(chan struct{}),
	}
}

func (s *pendingStartTerminalSession) ID() string {
	return s.id
}

func (s *pendingStartTerminalSession) Start(ctx context.Context) error {
	s.startOnce.Do(func() { close(s.startEntered) })
	if s.startErr != nil {
		return s.startErr
	}
	<-ctx.Done()
	s.cancelOnce.Do(func() { close(s.startCanceled) })
	return ctx.Err()
}

func (s *pendingStartTerminalSession) Events() <-chan terminal.Event {
	return s.events
}

func (s *pendingStartTerminalSession) Write(string) error {
	return nil
}

func (s *pendingStartTerminalSession) Resize(int, int) error {
	return nil
}

func (s *pendingStartTerminalSession) Close() error {
	s.closeCalls.Add(1)
	s.closeOnce.Do(func() { close(s.closeEntered) })
	<-s.releaseClose
	s.eventsOnce.Do(func() { close(s.events) })
	return nil
}

func (s *pendingStartTerminalSession) Size() terminal.Size {
	return terminal.Size{Cols: 80, Rows: 24}
}

func TestRevokeClaimsPendingTerminalStartExactlyOnce(t *testing.T) {
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	server := New(
		manager,
		watcher.New(time.Second),
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	session := newPendingStartTerminalSession()
	server.terminal = terminal.NewManager(
		&blockingTerminalBackend{session: session},
	)
	server.terminal.SetCleanupSubmitter(server.terminalCleanup.Submit)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	conn := dialDeviceWebSocket(
		t,
		"ws"+strings.TrimPrefix(httpServer.URL, "http")+"/ws",
		privateKey,
		manager.DaemonID(),
		deviceID,
	)
	defer conn.Close()

	server.mu.Lock()
	var ownedConn *websocket.Conn
	for candidate := range server.clients {
		ownedConn = candidate
	}
	server.mu.Unlock()
	if ownedConn == nil {
		t.Fatal("authenticated WebSocket ownership was not registered")
	}
	ownerID := clientID(ownedConn)
	if err := conn.WriteJSON(clientMessage{
		Type:     "terminal_open",
		Backend:  "blocking",
		TargetID: "target",
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-session.startEntered:
	case <-time.After(time.Second):
		t.Fatal("terminal Start did not enter")
	}

	revokeDone := make(chan error, 1)
	go func() {
		_, err := manager.RevokeDevice(deviceID)
		revokeDone <- err
	}()
	select {
	case err := <-revokeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("revoke waited for pending terminal Start or Close")
	}
	select {
	case <-session.startCanceled:
	case <-time.After(time.Second):
		t.Fatal("pending terminal Start was not canceled")
	}
	waitForWebSocketClose(t, conn)
	if got := server.clientCount(); got != 0 {
		t.Fatalf("client ownership after pending revoke=%d", got)
	}
	if err := server.terminal.Input(
		ownerID,
		session.ID(),
		"must-not-run",
	); err == nil {
		t.Fatal("pending terminal remained addressable after revoke")
	}
	select {
	case <-session.closeEntered:
	case <-time.After(time.Second):
		t.Fatal("pending terminal cleanup did not start")
	}
	if got := session.closeCalls.Load(); got != 1 {
		t.Fatalf("pending terminal close calls=%d, want 1", got)
	}

	drainDone := make(chan struct{})
	go func() {
		server.terminalCleanup.Drain()
		close(drainDone)
	}()
	select {
	case <-drainDone:
		t.Fatal("cleanup drain finished while pending Close was blocked")
	case <-time.After(50 * time.Millisecond):
	}
	close(session.releaseClose)
	select {
	case <-drainDone:
	case <-time.After(time.Second):
		t.Fatal("cleanup drain did not join pending Close")
	}
	if got := session.closeCalls.Load(); got != 1 {
		t.Fatalf("pending terminal close calls after drain=%d, want 1", got)
	}
}

func TestRunWithReadyCleanupDrainJoinsRacingDetach(t *testing.T) {
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	server := New(
		manager,
		watcher.New(time.Second),
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	session := newBlockingTerminalSession()
	server.terminal = terminal.NewManager(
		&blockingTerminalBackend{session: session},
	)
	server.terminal.SetCleanupSubmitter(server.terminalCleanup.Submit)

	cleanupPaused := make(chan struct{})
	releaseCleanup := make(chan struct{})
	var pauseOnce sync.Once
	server.terminalCleanup.execute = func(cleanup func()) {
		pauseOnce.Do(func() { close(cleanupPaused) })
		<-releaseCleanup
		cleanup()
	}

	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reservation.Addr().String()
	if err := reservation.Close(); err != nil {
		t.Fatal(err)
	}
	runtimeContext, cancelRuntime := context.WithCancel(context.Background())
	defer cancelRuntime()
	ready := make(chan struct{})
	runDone := make(chan error, 1)
	go func() {
		runDone <- server.RunWithReady(
			runtimeContext,
			address,
			func() { close(ready) },
		)
	}()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("server did not become ready")
	}

	conn := dialDeviceWebSocket(
		t,
		"ws://"+address+"/ws",
		privateKey,
		manager.DaemonID(),
		deviceID,
	)
	server.mu.Lock()
	var ownedConn *websocket.Conn
	for candidate := range server.clients {
		ownedConn = candidate
	}
	server.mu.Unlock()
	if ownedConn == nil {
		t.Fatal("authenticated WebSocket ownership was not registered")
	}
	if _, err := server.terminal.Open(
		clientID(ownedConn),
		"blocking",
		"target",
		terminal.OpenOptions{},
		func(any) {},
	); err != nil {
		t.Fatal(err)
	}

	detachStart := make(chan struct{})
	detachDone := make(chan struct{})
	go func() {
		<-detachStart
		_ = conn.Close()
		close(detachDone)
	}()
	close(detachStart)
	cancelRuntime()
	select {
	case <-cleanupPaused:
	case <-time.After(time.Second):
		t.Fatal("terminal cleanup claim was not registered")
	}
	deadline := time.Now().Add(time.Second)
	for server.clientCount() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("logical client removal did not finish")
		}
		runtime.Gosched()
	}
	select {
	case err := <-runDone:
		t.Fatalf("RunWithReady returned before cleanup ran: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseCleanup)
	select {
	case <-session.closeEntered:
	case <-time.After(time.Second):
		t.Fatal("terminal Close did not start")
	}
	if got := session.closeCalls.Load(); got != 1 {
		t.Fatalf("racing detach close calls=%d, want 1", got)
	}
	select {
	case err := <-runDone:
		t.Fatalf("RunWithReady returned while Close was blocked: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(session.releaseClose)
	select {
	case err := <-runDone:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunWithReady did not join terminal cleanup")
	}
	select {
	case <-detachDone:
	case <-time.After(time.Second):
		t.Fatal("normal WebSocket detach did not finish")
	}
	if got := session.closeCalls.Load(); got != 1 {
		t.Fatalf("racing detach close calls after shutdown=%d, want 1", got)
	}
	server.mu.Lock()
	ownershipRemaining := len(server.clients) +
		len(server.active) +
		len(server.writes) +
		len(server.codexSubs) +
		len(server.skillsSearches) +
		len(server.skillsInventories) +
		len(server.skillsCatalogs)
	server.mu.Unlock()
	if ownershipRemaining != 0 {
		t.Fatalf("shutdown left %d ownership entries", ownershipRemaining)
	}
}

func TestRunWithReadyJoinsCleanupBeforeFailedStartDisappears(
	t *testing.T,
) {
	testRunWithReadyJoinsCleanupBeforeFailedSessionDisappears(
		t,
		"start",
	)
}

func TestRunWithReadyJoinsCleanupBeforeFailedBackendOpenDisappears(
	t *testing.T,
) {
	testRunWithReadyJoinsCleanupBeforeFailedSessionDisappears(
		t,
		"backend_open",
	)
}

func testRunWithReadyJoinsCleanupBeforeFailedSessionDisappears(
	t *testing.T,
	failurePoint string,
) {
	t.Helper()
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	server := New(
		manager,
		watcher.New(time.Second),
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	injectedErr := errors.New("injected terminal " + failurePoint + " failure")
	session := newPendingStartTerminalSession()
	backend := &blockingTerminalBackend{session: session}
	switch failurePoint {
	case "start":
		session.startErr = injectedErr
	case "backend_open":
		backend.openErr = injectedErr
	default:
		t.Fatalf("unknown failure point %q", failurePoint)
	}
	server.terminal = terminal.NewManager(backend)

	submitPaused := make(chan struct{})
	releaseSubmit := make(chan struct{})
	var submitPausedOnce sync.Once
	var submitCalls atomic.Int32
	server.terminal.SetCleanupSubmitter(func(cleanup func()) {
		submitCalls.Add(1)
		submitPausedOnce.Do(func() { close(submitPaused) })
		<-releaseSubmit
		server.terminalCleanup.Submit(cleanup)
	})
	var releaseSubmitOnce sync.Once
	releaseSubmitter := func() {
		releaseSubmitOnce.Do(func() { close(releaseSubmit) })
	}
	defer releaseSubmitter()
	var releaseCloseOnce sync.Once
	releaseClose := func() {
		releaseCloseOnce.Do(func() { close(session.releaseClose) })
	}
	defer releaseClose()

	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reservation.Addr().String()
	if err := reservation.Close(); err != nil {
		t.Fatal(err)
	}
	runtimeContext, cancelRuntime := context.WithCancel(context.Background())
	defer cancelRuntime()
	ready := make(chan struct{})
	runDone := make(chan error, 1)
	go func() {
		runDone <- server.RunWithReady(
			runtimeContext,
			address,
			func() { close(ready) },
		)
	}()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("server did not become ready")
	}

	conn := dialDeviceWebSocket(
		t,
		"ws://"+address+"/ws",
		privateKey,
		manager.DaemonID(),
		deviceID,
	)
	defer conn.Close()
	server.mu.Lock()
	var ownedConn *websocket.Conn
	for candidate := range server.clients {
		ownedConn = candidate
	}
	server.mu.Unlock()
	if ownedConn == nil {
		t.Fatal("authenticated WebSocket ownership was not registered")
	}
	ownerID := clientID(ownedConn)

	openDone := make(chan error, 1)
	go func() {
		_, err := server.terminal.Open(
			ownerID,
			"blocking",
			"target",
			terminal.OpenOptions{},
			func(any) {},
		)
		openDone <- err
	}()
	select {
	case <-submitPaused:
	case <-time.After(time.Second):
		t.Fatal("failed terminal cleanup did not reach pre-submit barrier")
	}
	if got := submitCalls.Load(); got != 1 {
		t.Fatalf("cleanup submit calls at barrier=%d, want 1", got)
	}

	cancelRuntime()
	select {
	case err := <-runDone:
		t.Fatalf("RunWithReady returned before cleanup registration: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	releaseSubmitter()
	select {
	case <-session.closeEntered:
	case <-time.After(time.Second):
		t.Fatal("registered terminal cleanup did not start")
	}
	select {
	case err := <-openDone:
		if !errors.Is(err, injectedErr) {
			t.Fatalf("terminal Open error=%v, want %v", err, injectedErr)
		}
	case <-time.After(time.Second):
		t.Fatal("failed terminal Open did not return")
	}
	if got := submitCalls.Load(); got != 1 {
		t.Fatalf("cleanup submit calls=%d, want 1", got)
	}
	if got := session.closeCalls.Load(); got != 1 {
		t.Fatalf("terminal close calls=%d, want 1", got)
	}
	if err := server.terminal.Input(
		ownerID,
		session.ID(),
		"must-not-run",
	); err == nil {
		t.Fatal("failed terminal remained addressable after cleanup claim")
	}
	select {
	case err := <-runDone:
		t.Fatalf("RunWithReady returned while terminal Close blocked: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	releaseClose()
	select {
	case err := <-runDone:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunWithReady did not join registered terminal cleanup")
	}
	if got := session.closeCalls.Load(); got != 1 {
		t.Fatalf("terminal close calls after shutdown=%d, want 1", got)
	}
	server.mu.Lock()
	ownershipRemaining := len(server.clients) +
		len(server.active) +
		len(server.writes) +
		len(server.codexSubs) +
		len(server.skillsSearches) +
		len(server.skillsInventories) +
		len(server.skillsCatalogs)
	server.mu.Unlock()
	if ownershipRemaining != 0 {
		t.Fatalf("shutdown left %d ownership entries", ownershipRemaining)
	}
}

func TestRevokeAndShutdownDetachBlockedTerminalCleanupExactlyOnce(
	t *testing.T,
) {
	for _, operation := range []string{"revoke", "shutdown"} {
		t.Run(operation, func(t *testing.T) {
			manager, privateKey, deviceID := sessionFileAuthFixture(t)
			server := New(
				manager,
				watcher.New(time.Second),
				nil,
				nil,
				nil,
				nil,
				nil,
			)
			session := newBlockingTerminalSession()
			server.terminal = terminal.NewManager(
				&blockingTerminalBackend{session: session},
			)
			httpServer := httptest.NewServer(server.Handler())
			defer httpServer.Close()
			conn := dialDeviceWebSocket(
				t,
				"ws"+strings.TrimPrefix(httpServer.URL, "http")+"/ws",
				privateKey,
				manager.DaemonID(),
				deviceID,
			)
			defer conn.Close()
			server.mu.Lock()
			var ownedConn *websocket.Conn
			for candidate := range server.clients {
				ownedConn = candidate
			}
			server.mu.Unlock()
			if ownedConn == nil {
				t.Fatal("authenticated WebSocket ownership was not registered")
			}
			ownerID := clientID(ownedConn)
			if _, err := server.terminal.Open(
				ownerID,
				"blocking",
				"target",
				terminal.OpenOptions{},
				func(any) {},
			); err != nil {
				t.Fatal(err)
			}
			inputDone := make(chan error, 1)
			go func() {
				inputDone <- server.terminal.Input(
					ownerID,
					session.ID(),
					"blocked",
				)
			}()
			select {
			case <-session.writeEntered:
			case <-time.After(time.Second):
				t.Fatal("terminal input did not block")
			}

			done := make(chan error, 1)
			go func() {
				if operation == "revoke" {
					_, err := manager.RevokeDevice(deviceID)
					done <- err
					return
				}
				server.shutdownAuthenticatedClients()
				done <- nil
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(250 * time.Millisecond):
				t.Fatalf("%s waited for terminal cleanup", operation)
			}
			select {
			case <-session.canceled:
			case <-time.After(time.Second):
				t.Fatal("terminal context was not canceled during detach")
			}
			if err := server.terminal.Input(
				ownerID,
				session.ID(),
				"must-not-run",
			); err == nil {
				t.Fatal("detached terminal ownership remained addressable")
			}
			waitForWebSocketClose(t, conn)
			select {
			case <-session.closeEntered:
			case <-time.After(time.Second):
				t.Fatal("terminal cleanup did not start")
			}
			if got := session.closeCalls.Load(); got != 1 {
				t.Fatalf("terminal close calls before release=%d, want 1", got)
			}

			close(session.releaseWrite)
			if err := <-inputDone; err != nil {
				t.Fatal(err)
			}
			close(session.releaseClose)
			server.terminalCleanup.Drain()
			if got := session.closeCalls.Load(); got != 1 {
				t.Fatalf("terminal close calls after join=%d, want 1", got)
			}
		})
	}
}

func TestClientDetachRaceClaimsEveryOwnershipKindExactlyOnce(t *testing.T) {
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	server := New(
		manager,
		watcher.New(time.Second),
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	session := newBlockingTerminalSession()
	server.terminal = terminal.NewManager(
		&blockingTerminalBackend{session: session},
	)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	conn := dialDeviceWebSocket(
		t,
		"ws"+strings.TrimPrefix(httpServer.URL, "http")+"/ws",
		privateKey,
		manager.DaemonID(),
		deviceID,
	)
	defer conn.Close()

	codexContext, cancelCodex := context.WithCancel(context.Background())
	searchContext, cancelSearch := context.WithCancel(context.Background())
	inventoryContext, cancelInventory := context.WithCancel(context.Background())
	catalogContext, cancelCatalog := context.WithCancel(context.Background())
	server.mu.Lock()
	var ownedConn *websocket.Conn
	var owner *authenticatedClient
	for candidate, candidateOwner := range server.clients {
		ownedConn = candidate
		owner = candidateOwner
	}
	if ownedConn == nil || owner == nil {
		server.mu.Unlock()
		t.Fatal("authenticated WebSocket ownership was not registered")
	}
	server.active[ownedConn] = "agent"
	server.codexSubs[ownedConn]["thread"] = codexConversationSubscription{
		cancel: cancelCodex,
	}
	server.skillsSearches[ownedConn] = skillsSearchRequest{
		cancel: cancelSearch,
	}
	server.skillsInventories[ownedConn] = skillsInventoryRequest{
		cancel: cancelInventory,
	}
	server.skillsCatalogs[ownedConn] = skillsCatalogRequest{
		cancel: cancelCatalog,
	}
	server.mu.Unlock()

	ownerID := clientID(ownedConn)
	if _, err := server.terminal.Open(
		ownerID,
		"blocking",
		"target",
		terminal.OpenOptions{},
		func(any) {},
	); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	var raced sync.WaitGroup
	raced.Add(2)
	go func() {
		defer raced.Done()
		<-start
		server.detachAuthenticatedClient(ownedConn, owner)
	}()
	go func() {
		defer raced.Done()
		<-start
		server.revokeAuthenticatedDevice(deviceID)
	}()
	close(start)
	raced.Wait()
	server.detachAuthenticatedClient(ownedConn, owner)
	server.revokeAuthenticatedDevice(deviceID)

	select {
	case <-session.canceled:
	case <-time.After(time.Second):
		t.Fatal("terminal cancellation was not claimed")
	}
	select {
	case <-session.closeEntered:
	case <-time.After(time.Second):
		t.Fatal("terminal close did not start")
	}
	if got := session.closeCalls.Load(); got != 1 {
		t.Fatalf("raced terminal close calls=%d, want 1", got)
	}
	server.mu.Lock()
	ownershipRemaining := len(server.clients) +
		len(server.active) +
		len(server.writes) +
		len(server.codexSubs) +
		len(server.skillsSearches) +
		len(server.skillsInventories) +
		len(server.skillsCatalogs)
	server.mu.Unlock()
	if ownershipRemaining != 0 {
		t.Fatalf("raced detach left %d ownership entries", ownershipRemaining)
	}
	for name, done := range map[string]<-chan struct{}{
		"codex":     codexContext.Done(),
		"search":    searchContext.Done(),
		"inventory": inventoryContext.Done(),
		"catalog":   catalogContext.Done(),
	} {
		select {
		case <-done:
		default:
			t.Fatalf("%s cancellation was not invoked", name)
		}
	}
	if err := server.terminal.Input(
		ownerID,
		session.ID(),
		"must-not-run",
	); err == nil {
		t.Fatal("terminal ownership remained after raced detach")
	}

	close(session.releaseClose)
	server.terminalCleanup.Drain()
	if got := session.closeCalls.Load(); got != 1 {
		t.Fatalf("terminal close calls after repeated detach=%d, want 1", got)
	}
}

func TestServerShutdownClosesAuthenticatedWebSocketsWithoutOwnershipLeak(
	t *testing.T,
) {
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	server := New(
		manager,
		watcher.New(time.Second),
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	socketURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"
	first := dialDeviceWebSocket(
		t,
		socketURL,
		privateKey,
		manager.DaemonID(),
		deviceID,
	)
	second := dialDeviceWebSocket(
		t,
		socketURL,
		privateKey,
		manager.DaemonID(),
		deviceID,
	)
	defer first.Close()
	defer second.Close()

	server.shutdownAuthenticatedClients()
	server.shutdownAuthenticatedClients()
	waitForWebSocketClose(t, first)
	waitForWebSocketClose(t, second)

	server.mu.Lock()
	defer server.mu.Unlock()
	if !server.runtimeClosing ||
		len(server.clients) != 0 ||
		len(server.writes) != 0 ||
		len(server.active) != 0 {
		t.Fatalf(
			"shutdown ownership clients=%d writes=%d active=%d closing=%t",
			len(server.clients),
			len(server.writes),
			len(server.active),
			server.runtimeClosing,
		)
	}
}

func TestRemoveClientLockedIsIdempotentAndClearsAllOwnership(t *testing.T) {
	manager, _, _ := sessionFileAuthFixture(t)
	server := New(
		manager,
		watcher.New(time.Second),
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	conn := &websocket.Conn{}
	owner := &authenticatedClient{deviceID: "owned-device"}
	codexContext, cancelCodex := context.WithCancel(context.Background())
	searchContext, cancelSearch := context.WithCancel(context.Background())
	inventoryContext, cancelInventory := context.WithCancel(context.Background())
	catalogContext, cancelCatalog := context.WithCancel(context.Background())
	server.clients[conn] = owner
	server.active[conn] = "agent"
	server.writes[conn] = &sync.Mutex{}
	server.codexSubs[conn] = map[string]codexConversationSubscription{
		"thread": {cancel: cancelCodex},
	}
	server.skillsSearches[conn] = skillsSearchRequest{cancel: cancelSearch}
	server.skillsInventories[conn] = skillsInventoryRequest{
		cancel: cancelInventory,
	}
	server.skillsCatalogs[conn] = skillsCatalogRequest{cancel: cancelCatalog}

	server.mu.Lock()
	server.removeClientLocked(conn)
	server.removeClientLocked(conn)
	server.mu.Unlock()

	if !owner.revoked.Load() ||
		len(server.clients) != 0 ||
		len(server.active) != 0 ||
		len(server.writes) != 0 ||
		len(server.codexSubs) != 0 ||
		len(server.skillsSearches) != 0 ||
		len(server.skillsInventories) != 0 ||
		len(server.skillsCatalogs) != 0 {
		t.Fatal("idempotent removal left socket ownership or subscriptions")
	}
	for name, done := range map[string]<-chan struct{}{
		"codex":     codexContext.Done(),
		"search":    searchContext.Done(),
		"inventory": inventoryContext.Done(),
		"catalog":   catalogContext.Done(),
	} {
		select {
		case <-done:
		default:
			t.Fatalf("%s cancellation was not invoked", name)
		}
	}
}

func dialDeviceWebSocket(
	t *testing.T,
	socketURL string,
	privateKey ed25519.PrivateKey,
	daemonID string,
	deviceID string,
) *websocket.Conn {
	t.Helper()
	header := http.Header{}
	header.Set(
		"Authorization",
		calendarAuthHeader(
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
	return conn
}

func readWebSocketType(
	t *testing.T,
	conn *websocket.Conn,
	want string,
) map[string]any {
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
			return payload
		}
	}
}

func waitForWebSocketClose(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		if errors.Is(err, net.ErrClosed) {
			return
		}
		t.Fatal(err)
	}
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			var networkError net.Error
			if errors.As(err, &networkError) && networkError.Timeout() {
				t.Fatalf("revoked WebSocket remained open until deadline: %v", err)
			}
			return
		}
	}
}

func responseStatus(response *http.Response) any {
	if response == nil {
		return nil
	}
	return response.StatusCode
}
