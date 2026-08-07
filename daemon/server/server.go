package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/calendar"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/modelprofiles"
	"github.com/daoleno/zen/daemon/push"
	skillmgmt "github.com/daoleno/zen/daemon/skills"
	"github.com/daoleno/zen/daemon/stats"
	"github.com/daoleno/zen/daemon/terminal"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
	"github.com/gorilla/websocket"
	"github.com/oklog/ulid/v2"
)

const maxCodexAssetBytes = 6 << 20
const codexConversationSubscriptionInterval = 220 * time.Millisecond
const defaultScheduledResultLimit = 120
const maxUploadFileBytes int64 = 2 << 30
const maxUploadStoreBytes int64 = 8 << 30
const uploadNameHeader = "X-Zen-Upload-Name"
const maxUploadNameBytes = 1024
const maxUploadNameHeaderBytes = maxUploadNameBytes * 3
const uploadRetention = 7 * 24 * time.Hour

type uploadLimits struct {
	fileBytes  int64
	storeBytes int64
	retention  time.Duration
}

func productionUploadLimits() uploadLimits {
	return uploadLimits{
		fileBytes:  maxUploadFileBytes,
		storeBytes: maxUploadStoreBytes,
		retention:  uploadRetention,
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type notificationPusher interface {
	SetRegistration(token, serverRef string)
	NotifyAgentBlocked(agentID, agentName, summary string) error
	NotifyAgentFailed(agentID, agentName, summary string) error
	NotifyAgentDone(agentID, agentName, summary string) error
	NotifyScheduledResult(title, status, threadID, resultID string) error
}

// Server handles WebSocket connections from the zen mobile app.
type Server struct {
	auth                         *auth.Manager
	watcher                      *watcher.Watcher
	terminal                     *terminal.Manager
	pusher                       notificationPusher
	stats                        *stats.Collector
	work                         *work.Store
	execs                        *work.ExecutorConfig
	brain                        *brain.Service
	profiles                     *modelprofiles.Owner
	calendar                     *calendar.Store
	calendarScheduler            *calendar.Scheduler
	providerConversationLoader   func(reader *work.ProviderConversationReader, agentID string) (work.CodexConversation, error)
	sendInputOverride            func(agentID, text string) error
	sendInputWithReceiptOverride func(agentID, text, receipt string) error
	sendActionOverride           func(agentID, action string) error
	killSessionOverride          func(agentID string) error
	hasSessionOverride           func(agentID string) bool
	probeSessionOverride         func(agentID string) (watcher.SessionPresence, error)
	getAgentOverride             func(agentID string) *classifier.Agent
	brainSnapshotBroadcastHook   func(payload map[string]any)
	uploadDir                    string
	uploadMu                     sync.Mutex
	sessionFileAgentLoader       func(agentID string) *classifier.Agent
	sessionFileCapabilityClock   func() time.Time
	authRevocationUnsubscribe    func()
	runtimeClosing               bool
	terminalCleanup              terminalCleanupOwner

	workSubID              int
	workSub                <-chan work.Event
	calendarSubID          int
	calendarSub            <-chan calendar.Event
	brainWorkSubID         int
	brainWorkSub           <-chan brain.WorkChange
	brainMigrationComplete bool

	clients           map[*websocket.Conn]*authenticatedClient
	active            map[*websocket.Conn]string
	writes            map[*websocket.Conn]*sync.Mutex
	codexSubs         map[*websocket.Conn]map[string]codexConversationSubscription
	skillsSearcher    *skillmgmt.Searcher
	skillsCatalog     *skillmgmt.LeaderboardReader
	skillsInventories map[*websocket.Conn]skillsInventoryRequest
	skillsCatalogs    map[*websocket.Conn]skillsCatalogRequest
	skillsSearches    map[*websocket.Conn]skillsSearchRequest
	mu                sync.Mutex
}

func (s *Server) SetCalendar(store *calendar.Store, scheduler *calendar.Scheduler) {
	s.calendar, s.calendarScheduler = store, scheduler
	if store != nil {
		s.calendarSubID, s.calendarSub = store.Subscribe()
	}
}

type codexConversationSubscription struct {
	cancel     context.CancelFunc
	generation string
}

type authenticatedClient struct {
	deviceID        string
	secureTransport bool // TLS or loopback — required for secret writes
	revoked         atomic.Bool
}

type terminalCleanupState uint8

const (
	terminalCleanupOpen terminalCleanupState = iota
	terminalCleanupDraining
	terminalCleanupClosed
)

type terminalCleanupOwner struct {
	mu      sync.Mutex
	state   terminalCleanupState
	running int
	drained chan struct{}
	execute func(cleanup func())
}

func (o *terminalCleanupOwner) Submit(cleanup func()) {
	if cleanup == nil {
		return
	}
	o.mu.Lock()
	if o.state != terminalCleanupOpen {
		o.mu.Unlock()
		panic("terminal cleanup submitted after draining began")
	}
	if o.drained == nil {
		o.drained = make(chan struct{})
	}
	o.running++
	execute := o.execute
	o.mu.Unlock()
	go func() {
		defer o.finish()
		if execute != nil {
			execute(cleanup)
			return
		}
		cleanup()
	}()
}

func (o *terminalCleanupOwner) finish() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.running--
	if o.state == terminalCleanupDraining && o.running == 0 {
		o.state = terminalCleanupClosed
		close(o.drained)
	}
}

func (o *terminalCleanupOwner) Drain() {
	o.mu.Lock()
	if o.drained == nil {
		o.drained = make(chan struct{})
	}
	if o.state == terminalCleanupOpen {
		o.state = terminalCleanupDraining
		if o.running == 0 {
			o.state = terminalCleanupClosed
			close(o.drained)
		}
	}
	drained := o.drained
	o.mu.Unlock()
	<-drained
}

type clientDetachWork struct {
	conn *websocket.Conn
}

// New creates a WebSocket server.
func New(authManager *auth.Manager, w *watcher.Watcher, pusher *push.Client, sc *stats.Collector, workStore *work.Store, execs *work.ExecutorConfig, brainService *brain.Service) *Server {
	uploadDir := ""
	if authManager != nil {
		uploadDir = filepath.Join(authManager.StorageDir(), "uploads")
	}
	srv := &Server{
		auth:              authManager,
		watcher:           w,
		terminal:          terminal.NewManager(&terminal.TmuxBackend{}),
		pusher:            pusher,
		stats:             sc,
		work:              workStore,
		execs:             execs,
		brain:             brainService,
		uploadDir:         uploadDir,
		clients:           make(map[*websocket.Conn]*authenticatedClient),
		active:            make(map[*websocket.Conn]string),
		writes:            make(map[*websocket.Conn]*sync.Mutex),
		codexSubs:         make(map[*websocket.Conn]map[string]codexConversationSubscription),
		skillsSearcher:    skillmgmt.NewSearcher(),
		skillsCatalog:     skillmgmt.NewLeaderboardReader(),
		skillsInventories: make(map[*websocket.Conn]skillsInventoryRequest),
		skillsCatalogs:    make(map[*websocket.Conn]skillsCatalogRequest),
		skillsSearches:    make(map[*websocket.Conn]skillsSearchRequest),
	}
	if brainService != nil {
		srv.brainWorkSubID, srv.brainWorkSub = brainService.SubscribeWork()
	}
	if workStore != nil {
		srv.workSubID, srv.workSub = workStore.Subscribe()
	}
	if authManager != nil {
		srv.authRevocationUnsubscribe = authManager.SubscribeRevocations(
			srv.revokeAuthenticatedDevice,
		)
	}
	srv.terminal.SetCleanupSubmitter(srv.terminalCleanup.Submit)
	return srv
}

type clientMessage struct {
	Type                 string                                 `json:"type"`
	RequestID            string                                 `json:"request_id"`
	AgentID              string                                 `json:"agent_id"`
	TargetID             string                                 `json:"target_id"`
	Cwd                  string                                 `json:"cwd"`
	Command              string                                 `json:"command"`
	Name                 string                                 `json:"name"`
	StartedAt            json.RawMessage                        `json:"started_at"`
	Backend              string                                 `json:"backend"`
	SessionID            string                                 `json:"session_id"`
	Text                 string                                 `json:"text"`
	Key                  string                                 `json:"key"`
	Data                 string                                 `json:"data"`
	Body                 string                                 `json:"body"`
	Action               string                                 `json:"action"`
	PushToken            string                                 `json:"push_token"`
	ServerRef            string                                 `json:"server_ref"`
	Cols                 int                                    `json:"cols"`
	Rows                 int                                    `json:"rows"`
	Col                  int                                    `json:"col"`
	Row                  int                                    `json:"row"`
	Lines                int                                    `json:"lines"`
	ProcessID            int                                    `json:"process_id"`
	Path                 string                                 `json:"path"`
	FileGeneration       string                                 `json:"file_generation"`
	ID                   string                                 `json:"id"`
	Project              string                                 `json:"project"`
	Frontmatter          map[string]interface{}                 `json:"frontmatter"`
	BaseMtime            string                                 `json:"base_mtime"`
	Prompt               string                                 `json:"prompt"`
	Executor             string                                 `json:"executor"`
	ExecutorID           string                                 `json:"executor_id"`
	Client               string                                 `json:"client"`
	AdapterID            string                                 `json:"adapter_id"`
	Personality          string                                 `json:"personality"`
	Done                 bool                                   `json:"done"`
	CalendarItem         *calendar.Item                         `json:"calendar_item"`
	Revision             int64                                  `json:"revision"`
	ConversationScopeKey string                                 `json:"conversation_scope_key"`
	DisplayBody          string                                 `json:"display_body"`
	Generation           int64                                  `json:"generation"`
	Limit                int                                    `json:"limit"`
	Operation            string                                 `json:"operation"`
	Scope                string                                 `json:"scope"`
	Agents               []string                               `json:"agents"`
	SkillID              string                                 `json:"skill_id"`
	Source               string                                 `json:"source"`
	SkillName            string                                 `json:"skill_name"`
	ProfileID            string                                 `json:"profile_id"`
	ConnectionID         string                                 `json:"connection_id"`
	ModelID              string                                 `json:"model_id"`
	Credential           string                                 `json:"credential"`
	ProviderConnection   *modelprofiles.ProviderConnectionInput `json:"provider_connection"`
}

// Run starts the HTTP server and event broadcaster.
func (s *Server) Run(ctx context.Context, addr string) error {
	return s.RunWithReady(ctx, addr, nil)
}

// Handler exposes the daemon's complete origin as one transport boundary.
// LAN, tunnels, reverse proxies, and Zen Link all use this same handler so
// /ws and the HTTP streaming routes cannot drift between transports.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/pair", s.handlePair)
	mux.HandleFunc("/auth-check", s.handleAuthCheck)
	mux.HandleFunc("/devices", s.handleDevices)
	mux.HandleFunc("/upload", s.handleUpload)
	mux.HandleFunc("/session-file-capability", s.handleSessionFileCapability)
	mux.HandleFunc("/session-file", s.handleSessionFileBinary)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		s.writeJSONWithAssertion(w, http.StatusOK, "zen-health", map[string]any{
			"status":            "ok",
			"daemon_id":         s.auth.DaemonID(),
			"daemon_public_key": s.auth.PublicKeyHex(),
		})
	})
	return withCORS(mux)
}

// RunWithReady starts the HTTP server and calls onReady only after the TCP
// listener has been acquired successfully.
func (s *Server) RunWithReady(ctx context.Context, addr string, onReady func()) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		s.shutdownAuthenticatedClients()
		s.terminalCleanup.Drain()
		return err
	}
	runtimeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	srv := &http.Server{Handler: s.Handler()}

	var runtime sync.WaitGroup
	runtime.Add(2)
	go func() {
		defer runtime.Done()
		s.broadcastEvents(runtimeCtx)
	}()
	go func() {
		defer runtime.Done()
		s.heartbeat(runtimeCtx)
	}()

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-runtimeCtx.Done()
		s.shutdownAuthenticatedClients()
		_ = srv.Shutdown(context.Background())
	}()

	log.Printf("zen listening on %s", listener.Addr())
	if onReady != nil {
		onReady()
	}
	serveErr := srv.Serve(listener)
	cancel()
	<-shutdownDone
	runtime.Wait()
	s.terminalCleanup.Drain()
	return serveErr
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if !websocket.IsWebSocketUpgrade(r) {
		if _, ok := s.authenticateRequest(w, r, "zen-probe"); !ok {
			return
		}
		s.writeJSONWithAssertion(w, http.StatusOK, "zen-probe", map[string]any{
			"ok":                true,
			"daemon_id":         s.auth.DaemonID(),
			"daemon_public_key": s.auth.PublicKeyHex(),
		})
		return
	}
	device, ok := s.authenticateRequest(w, r, "zen-connect")
	if !ok {
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade: %v", err)
		return
	}
	owner := &authenticatedClient{
		deviceID:        device.ID,
		secureTransport: isSecureCredentialTransport(r),
	}
	if !s.bindAuthenticatedClient(conn, owner) {
		_ = conn.Close()
		return
	}
	defer s.terminal.ForgetOwner(clientID(conn))
	defer s.detachAuthenticatedClient(conn, owner)

	log.Printf("client connected (%d total)", s.clientCount())
	s.sendAgentSessionList(conn)
	if s.work != nil {
		s.sendJSON(conn, map[string]any{
			"type":       "work_items_snapshot",
			"work_items": work.FilterCalendarWorkItems(s.work.List()),
		})
	}
	if s.calendar != nil {
		s.sendCalendarSnapshot(conn, "")
	}
	s.sendBrainSnapshot(conn, "")

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if owner.revoked.Load() ||
			s.auth == nil ||
			!s.auth.IsDeviceTrusted(owner.deviceID) {
			break
		}
		s.handleClientMessage(conn, msg)
	}

	log.Printf("client disconnected (%d remaining)", s.clientCount())
}

func (s *Server) bindAuthenticatedClient(
	conn *websocket.Conn,
	owner *authenticatedClient,
) bool {
	if conn == nil || owner == nil || strings.TrimSpace(owner.deviceID) == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtimeClosing ||
		s.auth == nil ||
		!s.auth.IsDeviceTrusted(owner.deviceID) {
		owner.revoked.Store(true)
		return false
	}
	s.clients[conn] = owner
	s.active[conn] = ""
	s.writes[conn] = &sync.Mutex{}
	s.codexSubs[conn] = map[string]codexConversationSubscription{}
	return true
}

func (s *Server) detachAuthenticatedClient(
	conn *websocket.Conn,
	owner *authenticatedClient,
) {
	s.mu.Lock()
	var work clientDetachWork
	if s.clients[conn] == owner {
		work = s.removeClientLocked(conn)
	}
	s.mu.Unlock()
	s.completeClientDetach(work)
}

func (s *Server) revokeAuthenticatedDevice(deviceID string) {
	normalizedID := strings.TrimSpace(deviceID)
	if normalizedID == "" {
		return
	}
	var revoked []clientDetachWork
	s.mu.Lock()
	for conn, owner := range s.clients {
		if owner == nil || owner.deviceID != normalizedID {
			continue
		}
		revoked = append(revoked, s.removeClientLocked(conn))
	}
	s.mu.Unlock()

	for _, work := range revoked {
		s.completeClientDetach(work)
	}
}

func (s *Server) shutdownAuthenticatedClients() {
	var closing []clientDetachWork
	s.mu.Lock()
	if s.runtimeClosing {
		s.mu.Unlock()
		return
	}
	s.runtimeClosing = true
	for conn := range s.clients {
		closing = append(closing, s.removeClientLocked(conn))
	}
	unsubscribe := s.authRevocationUnsubscribe
	s.authRevocationUnsubscribe = nil
	s.mu.Unlock()

	for _, work := range closing {
		s.completeClientDetach(work)
	}
	if unsubscribe != nil {
		unsubscribe()
	}
}

func (s *Server) removeClientLocked(
	conn *websocket.Conn,
) clientDetachWork {
	owner, exists := s.clients[conn]
	if !exists {
		return clientDetachWork{}
	}
	if owner != nil {
		owner.revoked.Store(true)
	}
	s.cancelCodexSubscriptionsLocked(conn)
	s.cancelSkillsRequestsLocked(conn)
	terminalCleanup := s.terminal.DetachAll(clientID(conn))
	s.terminalCleanup.Submit(terminalCleanup)
	delete(s.clients, conn)
	delete(s.active, conn)
	delete(s.writes, conn)
	delete(s.codexSubs, conn)
	return clientDetachWork{
		conn: conn,
	}
}

func (s *Server) completeClientDetach(work clientDetachWork) {
	if work.conn == nil {
		return
	}
	_ = work.conn.Close()
}

func (s *Server) clientCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.clients)
}

func (s *Server) cancelSkillsRequestsLocked(conn *websocket.Conn) {
	if search, ok := s.skillsSearches[conn]; ok {
		search.cancel()
		delete(s.skillsSearches, conn)
	}
	if inventory, ok := s.skillsInventories[conn]; ok {
		inventory.cancel()
		delete(s.skillsInventories, conn)
	}
	if catalog, ok := s.skillsCatalogs[conn]; ok {
		catalog.cancel()
		delete(s.skillsCatalogs, conn)
	}
}

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var raw struct {
		EnrollmentToken   string `json:"enrollment_token"`
		ExpectedDaemonID  string `json:"expected_daemon_id"`
		ExpectedPublicKey string `json:"expected_daemon_public_key"`
		DeviceID          string `json:"device_id"`
		DeviceName        string `json:"device_name"`
		DevicePublicKey   string `json:"device_public_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	device, err := s.auth.EnrollDevice(
		raw.EnrollmentToken,
		raw.ExpectedDaemonID,
		raw.ExpectedPublicKey,
		raw.DeviceID,
		raw.DeviceName,
		raw.DevicePublicKey,
	)
	if err != nil {
		status := http.StatusUnauthorized
		switch err {
		case auth.ErrWrongDaemon:
			status = http.StatusConflict
		case auth.ErrInvalidPairingToken, auth.ErrExpiredPairingToken:
			status = http.StatusUnauthorized
		default:
			if strings.Contains(err.Error(), "different key") {
				status = http.StatusConflict
			}
		}
		http.Error(w, err.Error(), status)
		return
	}

	s.writeJSONWithAssertion(w, http.StatusOK, "zen-pair", map[string]any{
		"ok":                true,
		"daemon_id":         s.auth.DaemonID(),
		"daemon_public_key": s.auth.PublicKeyHex(),
		"device_id":         device.ID,
		"device_name":       device.Name,
	})
}

func (s *Server) handleAuthCheck(w http.ResponseWriter, r *http.Request) {
	device, ok := s.authenticateRequest(w, r, "zen-probe")
	if !ok {
		return
	}
	s.writeJSONWithAssertion(w, http.StatusOK, "zen-probe", map[string]any{
		"ok":                true,
		"device_id":         device.ID,
		"daemon_id":         s.auth.DaemonID(),
		"daemon_public_key": s.auth.PublicKeyHex(),
	})
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authenticateRequest(w, r, auth.DeviceListPurpose); !ok {
			return
		}
		s.writeJSONWithAssertion(w, http.StatusOK, auth.DeviceListPurpose, map[string]any{
			"devices": s.auth.ListDevices(),
		})
	case http.MethodDelete:
		var raw struct {
			DeviceID string `json:"device_id"`
		}
		decodeErr := json.NewDecoder(
			io.LimitReader(r.Body, 4<<10),
		).Decode(&raw)
		deviceID := strings.TrimSpace(raw.DeviceID)
		purpose := auth.DeviceRevokePurpose(deviceID)
		if _, ok := s.authenticateRequest(w, r, purpose); !ok {
			return
		}
		if decodeErr != nil || deviceID == "" {
			writeDeviceAPIError(
				w,
				http.StatusBadRequest,
				"invalid_device",
				"A valid device_id is required.",
			)
			return
		}
		persistence, err := s.auth.RevokeDevice(deviceID)
		if !persistence.Applied {
			if errors.Is(err, auth.ErrUnknownDevice) {
				writeDeviceAPIError(
					w,
					http.StatusNotFound,
					"device_not_found",
					"The trusted device was not found.",
				)
				return
			}
			log.Printf("revoke trusted device %q: %v", deviceID, err)
			writeDeviceAPIError(
				w,
				http.StatusInternalServerError,
				"device_revoke_failed",
				"The device could not be revoked.",
			)
			return
		}
		if err != nil {
			log.Printf(
				"revoked trusted device %q but directory durability is uncertain: %v",
				deviceID,
				err,
			)
		}
		s.writeJSONWithAssertion(w, http.StatusOK, purpose, map[string]any{
			"ok":                  true,
			"device_id":           deviceID,
			"persistence_applied": persistence.Applied,
			"persistence_durable": persistence.Durable,
		})
	default:
		writeDeviceAPIError(
			w,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Method not allowed.",
		)
	}
}

func writeDeviceAPIError(
	w http.ResponseWriter,
	status int,
	code string,
	message string,
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func (s *Server) handleClientMessage(conn *websocket.Conn, msg []byte) {
	var raw clientMessage
	if err := json.Unmarshal(msg, &raw); err != nil {
		log.Printf("invalid message: %v", err)
		return
	}

	if s.handleModelProfileMessage(conn, raw) {
		return
	}

	switch raw.Type {
	case "list_agents", "list_agent_sessions":
		s.sendAgentSessionList(conn)

	case "list_session_services":
		s.handleListSessionServices(conn, raw)

	case "list_work_items":
		s.handleListWorkItems(conn, raw)

	case "write_work_item":
		s.handleWriteWorkItem(conn, raw)

	case "delete_work_item":
		s.handleDeleteWorkItem(conn, raw)

	case "list_calendar_items":
		s.sendCalendarSnapshot(conn, raw.RequestID)
	case "get_calendar_item":
		s.handleGetCalendarItem(conn, raw)
	case "create_calendar_item":
		s.handleCreateCalendarItem(conn, raw)
	case "update_calendar_item":
		s.handleUpdateCalendarItem(conn, raw)
	case "cancel_calendar_item":
		s.handleCancelCalendarItem(conn, raw)
	case "run_calendar_item":
		s.handleRunCalendarItem(conn, raw)

	case "brain_snapshot":
		s.sendBrainSnapshot(conn, raw.RequestID)

	case "brain_context":
		s.handleBrainContext(conn, raw)

	case "brain_gc":
		s.handleBrainGC(conn, raw)

	case "brain_work_read":
		s.handleBrainWorkRead(conn, raw)

	case "brain_set_executor":
		s.handleBrainSetExecutor(conn, raw)

	case "set_delegated_executor":
		s.handleSetDelegatedExecutor(conn, raw)

	case "brain_chat_new":
		s.handleBrainChatNew(conn, raw)

	case "brain_workspace_tree":
		s.handleBrainWorkspaceTree(conn, raw)

	case "brain_workspace_file":
		s.handleBrainWorkspaceFile(conn, raw)

	case "register_push":
		if raw.PushToken != "" && s.pusher != nil {
			s.pusher.SetRegistration(raw.PushToken, raw.ServerRef)
			s.sendJSON(conn, map[string]any{"type": "push_registered", "ok": true})
		}

	case "set_active_agent":
		s.mu.Lock()
		s.active[conn] = raw.AgentID
		s.mu.Unlock()

	case "send_input":
		brainSteering := s.brain != nil && s.brain.NoteUserSteering(raw.AgentID)
		err := s.sendInputWithReceipt(raw.AgentID, raw.Text, raw.RequestID)
		if err != nil {
			if watcher.InputOutcomeFromError(err) == watcher.InputAmbiguous {
				s.sendJSON(conn, map[string]any{
					"type":       "input_pending",
					"request_id": raw.RequestID,
				})
				break
			}
			if brainSteering {
				s.brain.CancelUserSteering(raw.AgentID)
			}
			log.Printf("send_input error: %v", err)
			s.sendJSON(conn, map[string]any{
				"type":       "input_failed",
				"request_id": raw.RequestID,
				"code":       "send_input_failed",
				"message":    err.Error(),
			})
		} else {
			if s.brain != nil {
				displayBody := strings.TrimSpace(raw.DisplayBody)
				if displayBody == "" {
					displayBody = strings.TrimSpace(raw.Text)
				}
				if admitErr := s.brain.AdmitHostUserInput(
					raw.AgentID,
					raw.RequestID,
					displayBody,
					raw.ConversationScopeKey,
				); admitErr != nil {
					// Provider already accepted. Canonical Brain persistence failed,
					// so do not falsely ack input_sent. Pending preserves the local
					// row for transcript recovery without encouraging a duplicate retry.
					log.Printf("brain admit user input error: %v", admitErr)
					s.sendJSON(conn, map[string]any{
						"type":       "input_pending",
						"request_id": raw.RequestID,
					})
					break
				}
			}
			s.sendJSON(conn, map[string]any{
				"type":       "input_sent",
				"request_id": raw.RequestID,
			})
		}

	case "send_key":
		if err := s.watcher.SendKey(raw.AgentID, raw.Key); err != nil {
			log.Printf("send_key error: %v", err)
			s.sendErrorWithRequestID(conn, raw.RequestID, "send_key_failed", err.Error())
		} else if raw.RequestID != "" {
			s.sendJSON(conn, map[string]any{
				"type":       "key_sent",
				"request_id": raw.RequestID,
				"agent_id":   raw.AgentID,
				"key":        raw.Key,
			})
		}

	case "create_session":
		command := strings.TrimSpace(raw.Command)
		if work.InferAgentProvider(command) == work.AgentProviderPi {
			ensured, err := work.EnsurePiSessionLaunchCommand(command)
			if err != nil {
				s.sendJSON(conn, map[string]any{
					"type":       "error",
					"code":       "create_session_failed",
					"message":    err.Error(),
					"request_id": raw.RequestID,
				})
				return
			}
			command = ensured
		}
		connectionID := strings.TrimSpace(raw.ConnectionID)
		if connectionID == "" {
			connectionID = strings.TrimSpace(raw.ProfileID)
		}
		agentID, routeSnap, persist, err := s.createSessionWithProfiles(raw.TargetID, watcher.CreateSessionOptions{
			Cwd:     raw.Cwd,
			Command: command,
			Name:    raw.Name,
		}, connectionID)
		if err != nil && (!persist.Applied || strings.TrimSpace(agentID) == "") {
			code := "create_session_failed"
			if mapped := modelprofiles.ControlErrorCode(err); mapped != "" && mapped != modelprofiles.CodeProfilesUnavailable {
				code = mapped
			}
			s.sendJSON(conn, map[string]any{
				"type":       "error",
				"code":       code,
				"message":    err.Error(),
				"request_id": raw.RequestID,
			})
			return
		}
		response := map[string]any{
			"type":       "session_created",
			"request_id": raw.RequestID,
			"agent_id":   agentID,
		}
		if agent := s.watcher.GetAgent(agentID); agent != nil {
			response["agent_session"] = s.agentSessionWire(agent)
		}
		if routeSnap != nil {
			response["session_route"] = routeSnap
			if outcome, durable := modelprofiles.WirePersistFields(persist); outcome != "" {
				response["persistence_outcome"] = outcome
				response["persistence_durable"] = durable
			}
			if err != nil {
				response["persistence_warning"] = err.Error()
			}
		}
		s.sendJSON(conn, response)

	case "git_diff_status":
		payload, err := s.buildGitDiffStatus(raw.TargetID, raw.Cwd)
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "git_diff_status_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{
			"type":       "git_diff_status",
			"request_id": raw.RequestID,
			"status":     payload,
		})

	case "git_diff_patch":
		payload, err := s.buildGitDiffPatch(raw.TargetID, raw.Cwd, raw.Path)
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "git_diff_patch_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{
			"type":       "git_diff_patch",
			"request_id": raw.RequestID,
			"patch":      payload,
		})

	case "git_diff_file_content":
		payload, err := s.buildGitDiffFileContent(raw.TargetID, raw.Cwd, raw.Path)
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "git_diff_file_content_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{
			"type":       "git_diff_file_content",
			"request_id": raw.RequestID,
			"content":    payload,
		})

	case "git_repo_entries":
		payload, err := s.buildGitRepoEntries(raw.TargetID, raw.Cwd, raw.Path)
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "git_repo_entries_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{
			"type":       "git_repo_entries",
			"request_id": raw.RequestID,
			"browser":    payload,
		})

	case "git_repo_file_content":
		payload, err := s.buildGitRepoFileContent(raw.TargetID, raw.Cwd, raw.Path)
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "git_repo_file_content_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{
			"type":       "git_repo_file_content",
			"request_id": raw.RequestID,
			"content":    payload,
		})

	case "codex_conversation_subscribe":
		s.handleCodexConversationSubscribe(conn, raw)

	case "codex_conversation_unsubscribe":
		s.handleCodexConversationUnsubscribe(conn, raw)

	case "codex_slash_commands":
		s.handleCodexSlashCommands(conn, raw)

	case "codex_skills":
		s.handleCodexSkills(conn, raw)

	case "skills_inventory":
		s.handleSkillsInventory(conn, raw)

	case "skills_catalog":
		s.handleSkillsCatalog(conn, raw)

	case "skills_search":
		s.handleSkillsSearch(conn, raw)

	case "skills_search_cancel":
		s.handleSkillsSearchCancel(conn, raw)

	case "skills_command":
		s.handleSkillsCommand(conn, raw)

	case "codex_terminal_snapshot":
		text, err := s.watcher.CapturePaneContent(raw.TargetID)
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "codex_terminal_snapshot_failed", err.Error())
			return
		}
		text = work.CleanCodexDisplayText(text)
		s.sendJSON(conn, map[string]any{
			"type":       "codex_terminal_snapshot",
			"request_id": raw.RequestID,
			"target_id":  raw.TargetID,
			"text":       text,
		})

	case "terminal_snapshot":
		text, err := s.watcher.CapturePaneContent(raw.TargetID)
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "terminal_snapshot_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{
			"type":       "terminal_snapshot",
			"request_id": raw.RequestID,
			"target_id":  raw.TargetID,
			"text":       text,
		})

	case "codex_asset":
		s.handleCodexAsset(conn, raw)

	case "session_file_metadata":
		s.handleSessionFileMetadata(conn, raw)

	case "session_file_text":
		s.handleSessionFileText(conn, raw)

	case "terminal_open":
		backend := raw.Backend
		if backend == "" {
			backend = "tmux"
		}
		targetID := raw.TargetID
		if targetID == "" {
			targetID = raw.AgentID
		}
		_, err := s.terminal.Open(clientID(conn), backend, targetID, terminal.OpenOptions{
			Cols: raw.Cols,
			Rows: raw.Rows,
		}, func(v any) {
			s.sendJSON(conn, v)
		})
		if err != nil {
			s.sendJSON(conn, map[string]any{
				"type":    "terminal_error",
				"code":    "open_failed",
				"message": err.Error(),
			})
			return
		}
	case "terminal_input":
		if err := s.terminal.Input(clientID(conn), raw.SessionID, raw.Data); err != nil {
			s.sendJSON(conn, map[string]any{
				"type":       "terminal_error",
				"session_id": raw.SessionID,
				"code":       "input_failed",
				"message":    err.Error(),
			})
		}

	case "terminal_resize":
		if err := s.terminal.Resize(clientID(conn), raw.SessionID, raw.Cols, raw.Rows); err != nil {
			s.sendJSON(conn, map[string]any{
				"type":       "terminal_error",
				"session_id": raw.SessionID,
				"code":       "resize_failed",
				"message":    err.Error(),
			})
		}

	case "terminal_scroll":
		if err := s.terminal.Scroll(clientID(conn), raw.SessionID, raw.Lines); err != nil {
			s.sendJSON(conn, map[string]any{
				"type":       "terminal_error",
				"session_id": raw.SessionID,
				"code":       "scroll_failed",
				"message":    err.Error(),
			})
		}

	case "terminal_scroll_cancel":
		if err := s.terminal.ScrollCancel(clientID(conn), raw.SessionID); err != nil {
			s.sendJSON(conn, map[string]any{
				"type":       "terminal_error",
				"session_id": raw.SessionID,
				"code":       "scroll_cancel_failed",
				"message":    err.Error(),
			})
		}

	case "terminal_focus_pane":
		if err := s.terminal.FocusPane(clientID(conn), raw.SessionID, raw.Col, raw.Row); err != nil {
			s.sendJSON(conn, map[string]any{
				"type":       "terminal_error",
				"session_id": raw.SessionID,
				"code":       "focus_pane_failed",
				"message":    err.Error(),
			})
		}

	case "terminal_close":
		if err := s.terminal.Close(clientID(conn), raw.SessionID); err != nil {
			s.sendJSON(conn, map[string]any{
				"type":       "terminal_error",
				"session_id": raw.SessionID,
				"code":       "close_failed",
				"message":    err.Error(),
			})
		}

	case "send_action":
		err := s.sendAction(raw.AgentID, raw.Action)
		if err != nil {
			log.Printf("send_action error: %v", err)
			s.sendJSON(conn, map[string]any{
				"type":       "action_failed",
				"request_id": raw.RequestID,
				"code":       "send_action_failed",
				"message":    err.Error(),
			})
		} else {
			s.sendJSON(conn, map[string]any{
				"type":       "action_sent",
				"request_id": raw.RequestID,
			})
		}

	case "kill_agent":
		teardown := s.teardownAgentSession(raw.AgentID)
		if teardown.Err != nil {
			log.Printf("kill_agent error: %v", teardown.Err)
			code := modelprofiles.ControlErrorCode(teardown.Err)
			if code == "" || code == modelprofiles.CodeProfilesUnavailable || code == "close_failed" {
				code = "kill_failed"
			}
			payload := map[string]any{
				"type":    "error",
				"code":    code,
				"message": teardown.Err.Error(),
			}
			if raw.RequestID != "" {
				payload["request_id"] = raw.RequestID
			}
			if teardown.Persist.Applied {
				outcome, durable := modelprofiles.WirePersistFields(teardown.Persist)
				if outcome != "" {
					payload["persistence_outcome"] = outcome
					payload["persistence_durable"] = durable
				}
			}
			s.sendJSON(conn, payload)
		}

	case "list_dir":
		dirPath := raw.Cwd
		if dirPath == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				s.sendJSON(conn, map[string]any{
					"type":       "error",
					"code":       "list_dir_failed",
					"message":    err.Error(),
					"request_id": raw.RequestID,
				})
				return
			}
			dirPath = home
		}
		dirPath = filepath.Clean(dirPath)
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			s.sendJSON(conn, map[string]any{
				"type":       "error",
				"code":       "list_dir_failed",
				"message":    err.Error(),
				"request_id": raw.RequestID,
			})
			return
		}
		dirs := make([]map[string]string, 0)
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if len(name) > 0 && name[0] == '.' {
				continue
			}
			dirs = append(dirs, map[string]string{
				"name": name,
				"path": filepath.Join(dirPath, name),
			})
		}
		s.sendJSON(conn, map[string]any{
			"type":       "dir_list",
			"request_id": raw.RequestID,
			"path":       dirPath,
			"entries":    dirs,
		})

	case "get_stats":
		if resp := s.stats.Stats(); resp != nil {
			payload := map[string]any{
				"type":       "stats_data",
				"request_id": raw.RequestID,
				"ranges":     resp.Ranges,
			}
			if resp.CodexSubscription != nil && resp.CodexSubscription.AuthKind == "official" {
				payload["codexSubscription"] = resp.CodexSubscription
			}
			if resp.OpenCodeGoSubscription != nil && resp.OpenCodeGoSubscription.AuthKind == "official" {
				payload["opencodeGoSubscription"] = resp.OpenCodeGoSubscription
			}
			s.sendJSON(conn, payload)
		} else {
			s.sendJSON(conn, map[string]any{
				"type":       "stats_data",
				"request_id": raw.RequestID,
				"ranges":     map[string]any{},
			})
		}

	case "get_session_resource_snapshot":
		s.handleGetSessionResourceSnapshot(conn, raw)

	default:
		log.Printf("unknown message type: %s", raw.Type)
		if raw.RequestID != "" {
			s.sendErrorWithRequestID(conn, raw.RequestID, "unknown_message_type", fmt.Sprintf("Unknown message type: %s", raw.Type))
		}
	}
}

func (s *Server) sendAgentSessionList(conn *websocket.Conn) {
	agentSessions := s.currentVisibleAgentSessions()
	s.sendJSON(conn, map[string]any{"type": "agent_session_list", "agent_sessions": s.agentSessionsWire(agentSessions)})
}

type resolvedCodexConversationAgent struct {
	targetID    string
	agent       classifier.Agent
	provider    string
	fromWatcher bool
	ready       bool
	reason      string
}

func (s *Server) resolveCodexConversationAgent(raw clientMessage) resolvedCodexConversationAgent {
	targetID := strings.TrimSpace(raw.TargetID)
	if targetID == "" {
		targetID = strings.TrimSpace(raw.AgentID)
	}

	var agent classifier.Agent
	agentFromWatcher := false
	if targetID != "" {
		if snapshot := s.watcher.GetAgent(targetID); snapshot != nil {
			agent = *snapshot
			agentFromWatcher = true
		}
	}
	startedAt := clientStartedAt(raw.StartedAt)
	if agent.ID == "" {
		if targetID != "" && startedAt.IsZero() {
			return resolvedCodexConversationAgent{
				targetID: targetID,
				ready:    false,
				reason:   "session_not_ready",
			}
		}
		agent = classifier.Agent{
			ID:        targetID,
			Name:      raw.Name,
			Cwd:       raw.Cwd,
			Command:   raw.Command,
			StartedAt: startedAt,
			ProcessID: raw.ProcessID,
		}
	}
	if raw.ProcessID > 0 && agent.ProcessID == 0 {
		agent.ProcessID = raw.ProcessID
	}
	if !startedAt.IsZero() && (!agentFromWatcher || agent.StartedAt.IsZero()) {
		agent.StartedAt = startedAt
	}
	if agent.ID == "" && strings.TrimSpace(agent.Cwd) == "" {
		return resolvedCodexConversationAgent{
			targetID: targetID,
			ready:    false,
			reason:   "agent_not_found",
		}
	}
	return resolvedCodexConversationAgent{
		targetID:    targetID,
		agent:       agent,
		provider:    s.structuredProviderForAgent(&agent),
		fromWatcher: agentFromWatcher,
		ready:       true,
	}
}

func (s *Server) sendInput(agentID, text string) error {
	if s != nil && s.sendInputOverride != nil {
		return s.sendInputOverride(strings.TrimSpace(agentID), text)
	}
	if s == nil || s.watcher == nil {
		return fmt.Errorf("executor watcher unavailable")
	}
	return s.watcher.SendInput(strings.TrimSpace(agentID), text)
}

func (s *Server) sendInputWithReceipt(agentID, text, receipt string) error {
	if s != nil && s.sendInputWithReceiptOverride != nil {
		return s.sendInputWithReceiptOverride(strings.TrimSpace(agentID), text, strings.TrimSpace(receipt))
	}
	if s != nil && s.sendInputOverride != nil {
		return s.sendInputOverride(strings.TrimSpace(agentID), text)
	}
	if s == nil || s.watcher == nil {
		return fmt.Errorf("executor watcher unavailable")
	}
	if strings.TrimSpace(receipt) == "" {
		return s.watcher.SendInput(strings.TrimSpace(agentID), text)
	}
	return s.watcher.SendInputWithReceipt(strings.TrimSpace(agentID), text, strings.TrimSpace(receipt))
}

func (s *Server) sendAction(agentID, action string) error {
	if s != nil && s.sendActionOverride != nil {
		return s.sendActionOverride(strings.TrimSpace(agentID), action)
	}
	if s == nil || s.watcher == nil {
		return fmt.Errorf("executor watcher unavailable")
	}
	return s.watcher.SendAction(strings.TrimSpace(agentID), action)
}

func (s *Server) loadProviderConversationSnapshot(
	reader *work.ProviderConversationReader,
	resolved resolvedCodexConversationAgent,
	now time.Time,
) (work.CodexConversation, error) {
	var conversation work.CodexConversation
	var err error
	if s.providerConversationLoader != nil {
		conversation, err = s.providerConversationLoader(reader, strings.TrimSpace(resolved.targetID))
	} else {
		conversation, err = reader.Load(resolved.agent, resolved.provider, now)
	}
	if err != nil {
		return work.CodexConversation{}, err
	}
	return work.SanitizeConversationProjection(conversation), nil
}

func (s *Server) handleListSessionServices(conn *websocket.Conn, raw clientMessage) {
	payload, err := s.watcher.DiscoverSessionServices()
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "list_session_services_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":         "session_service_list",
		"request_id":   raw.RequestID,
		"generated_at": payload.GeneratedAt,
		"interfaces":   payload.Interfaces,
		"services":     payload.Services,
	})
}

func (s *Server) handleCodexConversationSubscribe(conn *websocket.Conn, raw clientMessage) {
	subscriptionID := strings.TrimSpace(raw.RequestID)
	if subscriptionID == "" {
		subscriptionID = strings.TrimSpace(raw.TargetID)
	}
	if subscriptionID == "" {
		subscriptionID = strings.TrimSpace(raw.AgentID)
	}
	if subscriptionID == "" {
		s.sendErrorWithRequestID(conn, raw.RequestID, "codex_conversation_subscribe_failed", "missing subscription id")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	generation := uuid.NewString()
	s.mu.Lock()
	if s.codexSubs[conn] == nil {
		s.codexSubs[conn] = map[string]codexConversationSubscription{}
	}
	if previous := s.codexSubs[conn][subscriptionID]; previous.cancel != nil {
		previous.cancel()
	}
	s.codexSubs[conn][subscriptionID] = codexConversationSubscription{
		cancel:     cancel,
		generation: generation,
	}
	s.mu.Unlock()

	go s.runCodexConversationSubscription(ctx, conn, raw, subscriptionID, generation)
}

func (s *Server) handleCodexConversationUnsubscribe(conn *websocket.Conn, raw clientMessage) {
	subscriptionID := strings.TrimSpace(raw.RequestID)
	if subscriptionID == "" {
		subscriptionID = strings.TrimSpace(raw.TargetID)
	}
	if subscriptionID == "" {
		subscriptionID = strings.TrimSpace(raw.AgentID)
	}
	if subscriptionID == "" {
		return
	}
	s.mu.Lock()
	if sub := s.codexSubs[conn][subscriptionID]; sub.cancel != nil {
		sub.cancel()
		delete(s.codexSubs[conn], subscriptionID)
	}
	s.mu.Unlock()
}

func (s *Server) cancelCodexSubscriptionsLocked(conn *websocket.Conn) {
	for id, sub := range s.codexSubs[conn] {
		if sub.cancel != nil {
			sub.cancel()
		}
		delete(s.codexSubs[conn], id)
	}
}

type codexConversationSubscriptionSnapshot struct {
	conversation work.CodexConversation
	fingerprint  string
	eventsByID   map[string]work.CodexConversationEvent
	revision     int64
}

func (s *Server) runCodexConversationSubscription(
	ctx context.Context,
	conn *websocket.Conn,
	raw clientMessage,
	subscriptionID string,
	generation string,
) {
	reader := work.NewProviderConversationReader()
	ticker := time.NewTicker(codexConversationSubscriptionInterval)
	defer ticker.Stop()
	defer func() {
		s.mu.Lock()
		if current, ok := s.codexSubs[conn][subscriptionID]; ok && current.generation == generation {
			delete(s.codexSubs[conn], subscriptionID)
		}
		s.mu.Unlock()
	}()

	var previous *codexConversationSubscriptionSnapshot
	for {
		s.publishCodexConversationSubscription(ctx, conn, raw, subscriptionID, generation, reader, &previous)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) publishCodexConversationSubscription(
	ctx context.Context,
	conn *websocket.Conn,
	raw clientMessage,
	subscriptionID string,
	generation string,
	reader *work.ProviderConversationReader,
	previous **codexConversationSubscriptionSnapshot,
) {
	if ctx.Err() != nil || !s.isCurrentCodexSubscription(conn, subscriptionID, generation) {
		return
	}
	resolved := s.resolveCodexConversationAgent(raw)
	if !resolved.ready {
		conversation := work.CodexConversation{
			Available: false,
			Reason:    resolved.reason,
			Events:    []work.CodexConversationEvent{},
		}
		fingerprint := fmt.Sprintf(
			"sync:%s:%s:%s",
			resolved.targetID,
			resolved.reason,
			codexConversationSubscriptionFingerprint(conversation),
		)
		if (*previous) != nil && (*previous).fingerprint == fingerprint {
			return
		}
		revision := int64(1)
		if (*previous) != nil {
			revision = (*previous).revision + 1
		}
		if !s.isCurrentCodexSubscription(conn, subscriptionID, generation) {
			return
		}
		s.sendJSON(conn, codexConversationSyncStatusPayload(
			subscriptionID,
			generation,
			resolved.targetID,
			revision,
			resolved.reason,
		))
		*previous = &codexConversationSubscriptionSnapshot{
			conversation: conversation,
			fingerprint:  fingerprint,
			eventsByID:   map[string]work.CodexConversationEvent{},
			revision:     revision,
		}
		return
	}

	now := time.Now()
	conversation, err := s.loadProviderConversationSnapshot(reader, resolved, now)
	if err != nil {
		if !s.isCurrentCodexSubscription(conn, subscriptionID, generation) {
			return
		}
		s.sendJSON(conn, map[string]any{
			"type":       "error",
			"request_id": subscriptionID,
			"generation": generation,
			"code":       "codex_conversation_subscribe_failed",
			"message":    err.Error(),
		})
		return
	}
	conversation = conversationForProviderAttachment(conversation, resolved.fromWatcher)
	conversation = s.brainScopedConversation(raw.ConversationScopeKey, conversation, now)

	fingerprint := codexConversationSubscriptionFingerprint(conversation)
	if (*previous) != nil && (*previous).fingerprint == fingerprint {
		return
	}
	next := codexConversationSubscriptionSnapshot{
		conversation: conversation,
		fingerprint:  fingerprint,
		eventsByID:   codexConversationEventsByID(conversation.Events),
		revision:     1,
	}
	if (*previous) != nil {
		next.revision = (*previous).revision + 1
	}

	if (*previous) == nil || !(*previous).conversation.Available ||
		codexConversationIdentity((*previous).conversation) != codexConversationIdentity(conversation) {
		if !s.isCurrentCodexSubscription(conn, subscriptionID, generation) {
			return
		}
		s.sendJSON(conn, codexConversationSnapshotPayload(
			subscriptionID,
			generation,
			resolved.targetID,
			next.revision,
			conversation,
		))
		*previous = &next
		return
	}

	upserts, deletes := codexConversationDelta((*previous).eventsByID, conversation.Events)
	if !s.isCurrentCodexSubscription(conn, subscriptionID, generation) {
		return
	}
	delta := codexConversationDeltaPayload(
		subscriptionID,
		generation,
		resolved.targetID,
		**previous,
		next,
		upserts,
		deletes,
	)
	s.sendJSON(conn, delta)
	*previous = &next
}

func conversationForProviderAttachment(conversation work.CodexConversation, attached bool) work.CodexConversation {
	if !attached {
		// Detached transcripts remain valid history but cannot claim a current
		// executor Activity.
		conversation.Activity = nil
	}
	return conversation
}

func codexConversationSyncStatusPayload(
	subscriptionID string,
	generation string,
	targetID string,
	revision int64,
	reason string,
) map[string]any {
	return map[string]any{
		"type":            "codex_conversation_sync_status",
		"request_id":      subscriptionID,
		"generation":      generation,
		"agent_id":        targetID,
		"conversation_id": "",
		"revision":        revision,
		"state":           "syncing",
		"reason":          reason,
	}
}

func codexConversationSnapshotPayload(
	subscriptionID string,
	generation string,
	targetID string,
	revision int64,
	conversation work.CodexConversation,
) map[string]any {
	return map[string]any{
		"type":            "codex_conversation_snapshot",
		"request_id":      subscriptionID,
		"generation":      generation,
		"agent_id":        targetID,
		"conversation_id": codexConversationIdentity(conversation),
		"revision":        revision,
		"conversation":    conversation,
	}
}

func codexConversationDeltaPayload(
	subscriptionID string,
	generation string,
	targetID string,
	previous codexConversationSubscriptionSnapshot,
	next codexConversationSubscriptionSnapshot,
	upserts []work.CodexConversationEvent,
	deletes []string,
) map[string]any {
	conversation := next.conversation
	delta := map[string]any{
		"type":            "codex_conversation_delta",
		"request_id":      subscriptionID,
		"generation":      generation,
		"agent_id":        targetID,
		"conversation_id": codexConversationIdentity(conversation),
		"base_revision":   previous.revision,
		"revision":        next.revision,
		"available":       conversation.Available,
		"reason":          conversation.Reason,
		"source":          conversation.Source,
		"path":            conversation.Path,
		"session_id":      conversation.SessionID,
		"cwd":             conversation.CWD,
		"updated_at":      conversation.Updated,
		"upserts":         upserts,
		"deletes":         deletes,
	}
	if !providerActivitiesEqual(previous.conversation.Activity, conversation.Activity) {
		// The key is intentionally present with nil when a prior Activity clears.
		delta["activity"] = conversation.Activity
	}
	return delta
}

func (s *Server) isCurrentCodexSubscription(conn *websocket.Conn, subscriptionID, generation string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	subscriptions := s.codexSubs[conn]
	current, ok := subscriptions[subscriptionID]
	return ok && current.generation == generation
}

func (s *Server) brainScopedConversation(scopeKey string, conversation work.CodexConversation, now time.Time) work.CodexConversation {
	const prefix = "brain-thread:"
	if s.brain == nil || !strings.HasPrefix(strings.TrimSpace(scopeKey), prefix) {
		return conversation
	}
	threadID := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(scopeKey), prefix))
	if threadID == "" {
		return conversation
	}
	known, err := s.brain.HasChatThread(threadID)
	if err != nil {
		conversation.Available = false
		conversation.Reason = "brain_threads_unavailable"
		return conversation
	}
	if !known {
		conversation.Available = false
		conversation.Reason = "brain_thread_unknown"
		conversation.Events = []work.CodexConversationEvent{}
		conversation.Activity = nil
		return conversation
	}
	currentThreadID, currentThreadErr := s.brain.ChatThreadID()
	if currentThreadErr != nil {
		conversation.Available = false
		conversation.Reason = "brain_threads_unavailable"
		return conversation
	}
	isCurrentThread := currentThreadErr == nil && strings.TrimSpace(currentThreadID) != "" &&
		threadID == strings.TrimSpace(currentThreadID)

	liveActivity := conversation.Activity
	providerEvents := []work.CodexConversationEvent(nil)
	if isCurrentThread {
		// Durable finals come from the Host Executor Session transcript
		// identity. Live cwd matching is never the recurring authority.
		bound, boundErr := s.brain.HostBoundProviderConversation()
		if boundErr == nil && bound.Available {
			conversation = work.PreferHostBoundConversation(conversation, bound)
			_ = s.brain.MaterializeProviderConversation(threadID, bound)
		}
		_ = s.brain.MaterializeProviderConversation(threadID, conversation)
		providerEvents = conversation.Events
	} else {
		liveActivity = nil
	}

	timelineItems, timelineErr := s.brain.ThreadTimeline(threadID, 0)
	if timelineErr != nil {
		conversation.Available = false
		conversation.Reason = "brain_timeline_unavailable"
		return conversation
	}
	eventsByID := make(map[string]int, len(timelineItems)+len(providerEvents)+8)
	uniqueEvents := make([]work.CodexConversationEvent, 0, len(timelineItems)+len(providerEvents)+8)
	appendUnique := func(event work.CodexConversationEvent) {
		id := strings.TrimSpace(event.ID)
		if id != "" {
			if index, ok := eventsByID[id]; ok {
				uniqueEvents[index] = event
				return
			}
			eventsByID[id] = len(uniqueEvents)
		}
		uniqueEvents = append(uniqueEvents, event)
	}
	for _, event := range timelineItemsToConversationEventsForServer(timelineItems) {
		appendUnique(event)
	}
	echoSuppressions := brain.ProviderUserEchoSuppressions(timelineItems, providerEvents)
	// Current-host provider events overlay the durable timeline so tools,
	// streaming partials, and live Activity remain visible. An empty provider
	// snapshot leaves the durable timeline intact. Durable calendar/work_result
	// rows win over provider duplicates with the same ID. Brain input admissions
	// suppress provider echoes one-to-one; same-body Terminal inputs still overlay.
	for _, event := range providerEvents {
		if strings.TrimSpace(event.Kind) == "user_message" {
			if echoSuppressions[strings.TrimSpace(event.ID)] {
				continue
			}
		}
		id := strings.TrimSpace(event.ID)
		if id != "" {
			if index, ok := eventsByID[id]; ok {
				existing := uniqueEvents[index]
				if existing.Source == "calendar_result" || existing.Source == "work_result" {
					continue
				}
				uniqueEvents[index] = event
				continue
			}
			eventsByID[id] = len(uniqueEvents)
		}
		uniqueEvents = append(uniqueEvents, event)
	}
	results := []calendar.ScheduledResult{}
	if s.calendar != nil {
		results = s.calendar.ScheduledResults(threadID, 0)
	}
	for _, result := range results {
		appendUnique(calendarResultConversationEvent(result))
	}

	sort.SliceStable(uniqueEvents, func(left, right int) bool {
		return brainConversationEventLess(uniqueEvents[left], uniqueEvents[right])
	})
	conversation.Events = uniqueEvents
	conversation.Activity = liveActivity
	conversation.Available = true
	conversation.Reason = ""
	conversation.Source = "brain_chat"
	conversation.SessionID = prefix + threadID
	conversation.Updated = &now
	return conversation
}

// timelineItemsToConversationEventsForServer adapts brain timeline rows without
// importing unexported helpers across packages.
func timelineItemsToConversationEventsForServer(items []brain.TimelineItem) []work.CodexConversationEvent {
	return brain.TimelineItemsToConversationEvents(items)
}

func calendarResultConversationEvent(result calendar.ScheduledResult) work.CodexConversationEvent {
	title := strings.TrimSpace(result.Title)
	status := strings.TrimSpace(string(result.Status))
	if title != "" && status != "" {
		title += " " + status
	}
	body := strings.TrimSpace(result.Body)
	if heading := strings.TrimSpace(result.Title + " " + status); heading != "" {
		body = strings.TrimSpace(strings.TrimPrefix(body, "**"+heading+"**"))
	}
	return work.CodexConversationEvent{
		ID:        result.ID,
		Timestamp: result.CreatedAt.Format(time.RFC3339Nano),
		Kind:      "status",
		Title:     title,
		Body:      body,
		Status:    status,
		Source:    "calendar_result",
	}
}

func brainConversationEventLess(left, right work.CodexConversationEvent) bool {
	leftTime, leftOK := parseConversationEventTime(left.Timestamp)
	rightTime, rightOK := parseConversationEventTime(right.Timestamp)
	if leftOK != rightOK {
		// Structured events normally carry timestamps. Legacy undated events sort
		// first so they cannot pin a later scheduled result at the footer.
		return !leftOK
	}
	if leftOK && !leftTime.Equal(rightTime) {
		return leftTime.Before(rightTime)
	}
	if left.Seq != right.Seq {
		return left.Seq < right.Seq
	}
	return left.ID < right.ID
}

func parseConversationEventTime(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	return parsed, err == nil
}

func codexConversationIdentity(conversation work.CodexConversation) string {
	return firstNonEmptyString(conversation.SessionID, conversation.Path, conversation.CWD)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func codexConversationSubscriptionFingerprint(conversation work.CodexConversation) string {
	hash := fnv.New64a()
	writeFingerprintString(hash, codexConversationIdentity(conversation))
	writeFingerprintString(hash, conversation.Path)
	writeFingerprintString(hash, conversation.SessionID)
	writeFingerprintBool(hash, conversation.Available)
	writeFingerprintString(hash, conversation.Reason)
	writeFingerprintString(hash, conversation.Source)
	writeFingerprintString(hash, conversation.CWD)
	writeProviderActivityFingerprint(hash, conversation.Activity)
	writeCodexConversationEventsFingerprint(hash, conversation.Events)
	return fmt.Sprintf("%016x", hash.Sum64())
}

func writeProviderActivityFingerprint(w io.Writer, activity *work.ProviderActivity) {
	if activity == nil {
		writeFingerprintString(w, "")
		return
	}
	writeFingerprintString(w, activity.ID)
	writeFingerprintString(w, string(activity.Status))
	writeFingerprintString(w, activity.StartedAt)
	writeFingerprintString(w, activity.SettledAt)
}

func providerActivitiesEqual(left, right *work.ProviderActivity) bool {
	return left == right || left != nil && right != nil &&
		left.ID == right.ID &&
		left.Status == right.Status &&
		left.StartedAt == right.StartedAt &&
		left.SettledAt == right.SettledAt
}

func codexConversationEventsFingerprint(events []work.CodexConversationEvent) string {
	hash := fnv.New64a()
	writeCodexConversationEventsFingerprint(hash, events)
	return fmt.Sprintf("%016x", hash.Sum64())
}

func codexConversationEventsByID(events []work.CodexConversationEvent) map[string]work.CodexConversationEvent {
	byID := make(map[string]work.CodexConversationEvent, len(events))
	for _, event := range events {
		if strings.TrimSpace(event.ID) == "" {
			continue
		}
		byID[event.ID] = event
	}
	return byID
}

func codexConversationDelta(
	previous map[string]work.CodexConversationEvent,
	next []work.CodexConversationEvent,
) ([]work.CodexConversationEvent, []string) {
	var upserts []work.CodexConversationEvent
	nextIDs := make(map[string]struct{}, len(next))
	for _, event := range next {
		id := strings.TrimSpace(event.ID)
		if id == "" {
			upserts = append(upserts, event)
			continue
		}
		nextIDs[id] = struct{}{}
		if previousEvent, ok := previous[id]; !ok || codexConversationEventFingerprint(previousEvent) != codexConversationEventFingerprint(event) {
			upserts = append(upserts, event)
		}
	}
	var deletes []string
	for id := range previous {
		if _, ok := nextIDs[id]; !ok {
			deletes = append(deletes, id)
		}
	}
	return upserts, deletes
}

func codexConversationEventFingerprint(event work.CodexConversationEvent) string {
	hash := fnv.New64a()
	writeCodexConversationEventFingerprint(hash, event)
	return fmt.Sprintf("%016x", hash.Sum64())
}

func writeCodexConversationEventsFingerprint(w io.Writer, events []work.CodexConversationEvent) {
	writeFingerprintInt(w, len(events))
	for _, event := range events {
		writeCodexConversationEventFingerprint(w, event)
	}
}

func writeCodexConversationEventFingerprint(w io.Writer, event work.CodexConversationEvent) {
	writeFingerprintString(w, event.ID)
	writeFingerprintInt(w, event.Seq)
	writeFingerprintString(w, event.Timestamp)
	writeFingerprintString(w, event.Kind)
	writeFingerprintString(w, event.Role)
	writeFingerprintString(w, event.Title)
	writeFingerprintString(w, event.Body)
	writeFingerprintString(w, event.Command)
	writeFingerprintString(w, event.ToolName)
	writeFingerprintString(w, event.Input)
	writeFingerprintString(w, event.Output)
	writeFingerprintString(w, event.CallID)
	if event.ExitCode == nil {
		writeFingerprintString(w, "")
	} else {
		writeFingerprintInt(w, *event.ExitCode)
	}
	writeFingerprintString(w, event.Status)
	writeFingerprintBool(w, event.Partial)
	writeFingerprintBool(w, event.Transient)
	writeFingerprintStrings(w, event.Files)
	writeFingerprintFileChanges(w, event.FileChanges)
	writeFingerprintString(w, event.Explanation)
	writeFingerprintPlan(w, event.Plan)
	writeFingerprintString(w, event.Source)
}

func writeFingerprintFileChanges(w io.Writer, changes []work.CodexConversationFileChange) {
	writeFingerprintInt(w, len(changes))
	for _, change := range changes {
		writeFingerprintString(w, change.Path)
		writeFingerprintString(w, change.MovePath)
		writeFingerprintString(w, change.Operation)
		if change.Additions == nil {
			writeFingerprintString(w, "")
		} else {
			writeFingerprintInt(w, *change.Additions)
		}
		if change.Deletions == nil {
			writeFingerprintString(w, "")
		} else {
			writeFingerprintInt(w, *change.Deletions)
		}
	}
}

func writeFingerprintString(w io.Writer, value string) {
	_, _ = fmt.Fprintf(w, "%d:", len(value))
	_, _ = io.WriteString(w, value)
	_, _ = io.WriteString(w, "\x00")
}

func writeFingerprintBool(w io.Writer, value bool) {
	if value {
		writeFingerprintString(w, "true")
		return
	}
	writeFingerprintString(w, "false")
}

func writeFingerprintInt(w io.Writer, value int) {
	_, _ = fmt.Fprintf(w, "%d\x00", value)
}

func writeFingerprintStrings(w io.Writer, values []string) {
	writeFingerprintInt(w, len(values))
	for _, value := range values {
		writeFingerprintString(w, value)
	}
}

func writeFingerprintPlan(w io.Writer, steps []work.CodexPlanStep) {
	writeFingerprintInt(w, len(steps))
	for _, step := range steps {
		writeFingerprintString(w, step.Step)
		writeFingerprintString(w, step.Status)
	}
}

func clientStartedAt(raw json.RawMessage) time.Time {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}
	}
	var numeric float64
	if err := json.Unmarshal(raw, &numeric); err == nil && numeric > 0 {
		seconds := int64(numeric)
		nanos := int64((numeric - float64(seconds)) * 1_000_000_000)
		if numeric > 10_000_000_000 {
			seconds = int64(numeric / 1000)
			nanos = int64(numeric-float64(seconds*1000)) * int64(time.Millisecond)
		}
		return time.Unix(seconds, nanos).UTC()
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return time.Time{}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
		return parsed.UTC()
	}
	if parsed, err := time.Parse(time.RFC3339, text); err == nil {
		return parsed.UTC()
	}
	return time.Time{}
}

func (s *Server) handleCodexSlashCommands(conn *websocket.Conn, raw clientMessage) {
	snapshot := discoverCodexSlashCommands(time.Now())
	s.sendJSON(conn, map[string]any{
		"type":         "codex_slash_commands",
		"request_id":   raw.RequestID,
		"generated_at": snapshot.GeneratedAt,
		"source":       snapshot.Source,
		"version":      snapshot.Version,
		"commands":     snapshot.Commands,
	})
}

func (s *Server) handleCodexSkills(conn *websocket.Conn, raw clientMessage) {
	s.sendJSON(conn, map[string]any{
		"type":       "codex_skills",
		"request_id": raw.RequestID,
		"cwd":        raw.Cwd,
		"skills":     discoverCodexSkills(raw.Cwd),
	})
}

func (s *Server) handleCodexAsset(conn *websocket.Conn, raw clientMessage) {
	path := strings.TrimSpace(raw.Path)
	if path == "" {
		s.sendErrorWithRequestID(conn, raw.RequestID, "codex_asset_failed", "missing asset path")
		return
	}
	if !filepath.IsAbs(path) {
		if strings.TrimSpace(raw.Cwd) == "" {
			s.sendErrorWithRequestID(conn, raw.RequestID, "codex_asset_failed", "relative asset path requires cwd")
			return
		}
		path = filepath.Join(raw.Cwd, path)
	}
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "codex_asset_failed", err.Error())
		return
	}
	if info.IsDir() {
		s.sendErrorWithRequestID(conn, raw.RequestID, "codex_asset_failed", "asset path is a directory")
		return
	}
	if info.Size() > maxCodexAssetBytes {
		s.sendErrorWithRequestID(conn, raw.RequestID, "codex_asset_failed", "asset is too large to preview")
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "codex_asset_failed", err.Error())
		return
	}
	contentType := codexAssetContentType(path, data)
	if !strings.HasPrefix(contentType, "image/") {
		s.sendErrorWithRequestID(conn, raw.RequestID, "codex_asset_failed", "asset is not a supported image")
		return
	}

	s.sendJSON(conn, map[string]any{
		"type":         "codex_asset",
		"request_id":   raw.RequestID,
		"path":         path,
		"content_type": contentType,
		"data_url":     "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data),
	})
}

func codexAssetContentType(path string, data []byte) string {
	contentType := http.DetectContentType(data)
	if strings.HasPrefix(contentType, "image/") {
		return contentType
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	}
	return contentType
}

func (s *Server) handleListWorkItems(conn *websocket.Conn, raw clientMessage) {
	if s.work == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "list_work_items_failed", "work store not configured")
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "work_items_snapshot",
		"request_id": raw.RequestID,
		"work_items": work.FilterCalendarWorkItems(s.work.List()),
	})
}

func (s *Server) sendBrainSnapshot(conn *websocket.Conn, requestID string) {
	if s.brain == nil {
		return
	}
	snapshot, err := s.brain.Snapshot()
	if err != nil {
		s.sendErrorWithRequestID(conn, requestID, "brain_snapshot_failed", err.Error())
		return
	}
	payload, err := s.brainSnapshotWire(snapshot)
	if err != nil {
		s.sendErrorWithRequestID(conn, requestID, "brain_snapshot_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "brain_snapshot",
		"request_id": requestID,
		"brain":      payload,
	})
}

func (s *Server) handleBrainSetExecutor(conn *websocket.Conn, raw clientMessage) {
	if s.brain == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_unavailable", "Brain is not configured")
		return
	}
	executorID := strings.TrimSpace(raw.ExecutorID)
	if executorID == "" {
		executorID = strings.TrimSpace(raw.AdapterID)
	}
	snapshot, err := s.brain.SetHostExecutor(executorID)
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_set_executor_failed", err.Error())
		return
	}
	payload, err := s.brainSnapshotWire(snapshot)
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_snapshot_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "brain_snapshot",
		"request_id": raw.RequestID,
		"brain":      payload,
	})
}

// handleSetDelegatedExecutor switches the live/persisted default Agents executor
// through ExecutorConfig.SetDelegatedExecutor. Existing sessions are not migrated.
func (s *Server) handleSetDelegatedExecutor(conn *websocket.Conn, raw clientMessage) {
	if s.execs == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "executors_unavailable", "Executor config is not available.")
		return
	}
	if s.brain == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_unavailable", "Brain is not configured")
		return
	}
	executorID := strings.TrimSpace(raw.ExecutorID)
	if executorID == "" {
		executorID = strings.TrimSpace(raw.AdapterID)
	}
	if executorID == "" {
		s.sendErrorWithRequestID(conn, raw.RequestID, "missing_executor", "Delegated executor id is required.")
		return
	}
	if err := s.execs.SetDelegatedExecutor(executorID); err != nil {
		if errors.Is(err, work.ErrUnknownExecutor) {
			s.sendErrorWithRequestID(conn, raw.RequestID, "invalid_executor", err.Error())
			return
		}
		if errors.Is(err, work.ErrDelegatedExecutorLocked) {
			s.sendErrorWithRequestID(conn, raw.RequestID, "delegated_executor_locked_by_env", err.Error())
			return
		}
		s.sendErrorWithRequestID(conn, raw.RequestID, "set_delegated_executor_failed", err.Error())
		return
	}
	snapshot, err := s.brain.Snapshot()
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_snapshot_failed", err.Error())
		return
	}
	payload, err := s.brainSnapshotWire(snapshot)
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_snapshot_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "brain_snapshot",
		"request_id": raw.RequestID,
		"brain":      payload,
	})
}

func (s *Server) handleBrainContext(conn *websocket.Conn, raw clientMessage) {
	if s.brain == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_unavailable", "Brain is not configured")
		return
	}
	context, err := s.brain.Context()
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_context_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "brain_context",
		"request_id": raw.RequestID,
		"context":    context,
	})
}

func (s *Server) handleBrainGC(conn *websocket.Conn, raw clientMessage) {
	if s.brain == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_unavailable", "Brain is not configured")
		return
	}
	report, err := s.brain.Housekeeping()
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_gc_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":         "brain_gc",
		"request_id":   raw.RequestID,
		"housekeeping": report,
	})
}

func (s *Server) handleBrainWorkRead(conn *websocket.Conn, raw clientMessage) {
	if s.brain == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_unavailable", "Brain is not configured")
		return
	}
	if err := s.brain.MarkWorkRead(strings.TrimSpace(raw.ID)); err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_work_read_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "brain_work_read",
		"request_id": raw.RequestID,
		"work_id":    strings.TrimSpace(raw.ID),
	})
}

func (s *Server) handleBrainChatNew(conn *websocket.Conn, raw clientMessage) {
	if s.brain == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_unavailable", "Brain is not configured")
		return
	}
	snapshot, err := s.brain.NewChat()
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_chat_new_failed", err.Error())
		return
	}
	payload, err := s.brainSnapshotWire(snapshot)
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_snapshot_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "brain_snapshot",
		"request_id": raw.RequestID,
		"brain":      payload,
	})
}

func (s *Server) brainSnapshotWire(snapshot brain.Snapshot) (any, error) {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	if _, ok := payload["host_adapter"]; !ok && snapshot.HostExecutor != nil {
		payload["host_adapter"] = snapshot.HostExecutor
	}
	if _, ok := payload["delegated_adapter"]; !ok && snapshot.DelegatedExecutor != nil {
		payload["delegated_adapter"] = snapshot.DelegatedExecutor
	}
	if _, ok := payload["adapters"]; !ok && snapshot.Executors != nil {
		payload["adapters"] = snapshot.Executors
	}
	s.enrichBrainSnapshotHostAgentCapabilities(payload, snapshot)
	results, err := s.scheduledResultsForKnownBrainThreads(defaultScheduledResultLimit)
	if err != nil {
		return nil, err
	}
	payload["scheduled_results"] = results
	return payload, nil
}

// enrichBrainSnapshotHostAgentCapabilities attaches the same flat capabilities
// object as agent_session wire onto brain_snapshot.host_agent. Derivation uses
// the hidden watcher Agent + Model Profiles route table only; missing agent or
// profile owner fail closed to false. The host stays Hidden and is never added
// to agent_session_list.
func (s *Server) enrichBrainSnapshotHostAgentCapabilities(payload map[string]any, snapshot brain.Snapshot) {
	if payload == nil {
		return
	}
	hostRaw, ok := payload["host_agent"]
	if !ok || hostRaw == nil {
		return
	}
	hostMap, ok := hostRaw.(map[string]any)
	if !ok {
		return
	}
	sessionID := ""
	if snapshot.HostAgent != nil {
		sessionID = strings.TrimSpace(snapshot.HostAgent.ID)
	}
	if sessionID == "" {
		if id, _ := hostMap["id"].(string); strings.TrimSpace(id) != "" {
			sessionID = strings.TrimSpace(id)
		}
	}
	hostMap["capabilities"] = s.hostAgentWireCapabilities(sessionID)
	payload["host_agent"] = hostMap
}

func (s *Server) scheduledResultsForKnownBrainThreads(limit int) ([]calendar.ScheduledResult, error) {
	if s == nil || s.calendar == nil || s.brain == nil {
		return []calendar.ScheduledResult{}, nil
	}
	threadIDs, err := s.brain.ChatThreadIDs()
	if err != nil {
		return nil, err
	}
	known := make(map[string]struct{}, len(threadIDs))
	for _, threadID := range threadIDs {
		if threadID = strings.TrimSpace(threadID); threadID != "" {
			known[threadID] = struct{}{}
		}
	}
	all := s.calendar.ScheduledResults("", 0)
	results := make([]calendar.ScheduledResult, 0, len(all))
	for _, result := range all {
		if _, ok := known[result.ThreadID]; ok {
			results = append(results, result)
		}
	}
	if limit > 0 && len(results) > limit {
		results = results[len(results)-limit:]
	}
	return results, nil
}

func visibleAgentSessions(agents []*classifier.Agent) []*classifier.Agent {
	if len(agents) == 0 {
		return nil
	}
	out := make([]*classifier.Agent, 0, len(agents))
	for _, agent := range agents {
		if agent == nil || agent.Hidden {
			continue
		}
		out = append(out, agent)
	}
	return out
}

func (s *Server) currentVisibleAgentSessions() []*classifier.Agent {
	return visibleAgentSessions(s.watcher.Agents())
}

func (s *Server) handleWriteWorkItem(conn *websocket.Conn, raw clientMessage) {
	if s.work == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "write_work_item_failed", "work store not configured")
		return
	}

	now := time.Now().UTC()
	project := strings.TrimSpace(raw.Project)
	if project == "" {
		project = "inbox"
	}

	id := strings.TrimSpace(raw.ID)
	if id == "" {
		id = ulid.Make().String()
	}

	path := strings.TrimSpace(raw.Path)
	if path == "" {
		path = filepath.Join(s.work.Root, project, buildWorkFilename(now, raw.Body, id))
	}

	frontmatter := work.Frontmatter{
		ID:      id,
		Created: now,
	}
	if existing, ok := s.work.GetByID(id); ok {
		frontmatter = existing.Frontmatter
	}
	applyFrontmatterOverrides(&frontmatter, raw.Frontmatter)

	var baseMtime time.Time
	if strings.TrimSpace(raw.BaseMtime) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw.BaseMtime)
		if err == nil {
			baseMtime = parsed
		}
	}

	written, err := s.work.Write(&work.Item{
		ID:          frontmatter.ID,
		Path:        path,
		Project:     project,
		Body:        raw.Body,
		Frontmatter: frontmatter,
	}, baseMtime)
	if err != nil {
		if errors.Is(err, work.ErrConflict) {
			current, _ := s.work.GetByID(id)
			s.sendJSON(conn, map[string]any{
				"type":       "error",
				"request_id": raw.RequestID,
				"code":       "conflict",
				"message":    "work item changed on disk",
				"current":    current,
			})
			return
		}
		s.sendErrorWithRequestID(conn, raw.RequestID, "write_work_item_failed", err.Error())
		return
	}

	s.sendJSON(conn, map[string]any{
		"type":       "work_item_written",
		"request_id": raw.RequestID,
		"work_item":  written,
	})
}

func (s *Server) handleDeleteWorkItem(conn *websocket.Conn, raw clientMessage) {
	if s.work == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "delete_work_item_failed", "work store not configured")
		return
	}
	if err := s.work.Delete(strings.TrimSpace(raw.ID)); err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "delete_work_item_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "work_item_deleted_ack",
		"request_id": raw.RequestID,
		"id":         strings.TrimSpace(raw.ID),
	})
}

func (s *Server) sendJSON(conn *websocket.Conn, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("sendJSON marshal error: %v", err)
		return
	}
	if err := s.writeMessage(conn, websocket.TextMessage, data); err != nil {
		log.Printf("sendJSON write error: %v", err)
	}
}

func (s *Server) sendError(conn *websocket.Conn, code, message string) {
	s.sendJSON(conn, map[string]any{"type": "error", "code": code, "message": message})
}

func (s *Server) sendErrorWithRequestID(conn *websocket.Conn, requestID, code, message string) {
	s.sendJSON(conn, map[string]any{
		"type":       "error",
		"request_id": requestID,
		"code":       code,
		"message":    message,
	})
}

func (s *Server) broadcastEvents(ctx context.Context) {
	calendarSub := s.calendarSub
	if s.calendar != nil && calendarSub != nil {
		defer s.calendar.Unsubscribe(s.calendarSubID)
	}
	brainWorkSub := s.brainWorkSub
	if s.brain != nil && brainWorkSub != nil {
		defer s.brain.UnsubscribeWork(s.brainWorkSubID)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-s.watcher.Events():
			s.handleWatcherEvent(ev)
		case ie, ok := <-s.workSub:
			if !ok {
				s.workSub = nil
				continue
			}
			s.handleWorkEvent(ie)
		case event, ok := <-calendarSub:
			if !ok {
				calendarSub = nil
				continue
			}
			s.handleCalendarEvent(event)
		case _, ok := <-brainWorkSub:
			if !ok {
				brainWorkSub = nil
				continue
			}
			s.broadcastBrainSnapshot()
		}
	}
}

func (s *Server) broadcastBrainSnapshot() {
	if s.brain == nil {
		return
	}
	snapshot, err := s.brain.Snapshot()
	if err != nil {
		log.Printf("brain snapshot broadcast failed: %v", err)
		return
	}
	s.emitBrainSnapshotBroadcast(snapshot)
}

// broadcastBrainHostCapabilityRefresh projects brain_snapshot for Hidden-host
// discovery/removal without ensureHostAgent. Continuity (create/resume/rebind)
// is owned by intentional Snapshot/NewChat paths, not by projection events.
func (s *Server) broadcastBrainHostCapabilityRefresh() {
	if s.brain == nil {
		return
	}
	snapshot, err := s.brain.ProjectionSnapshot()
	if err != nil {
		log.Printf("brain host capability refresh failed: %v", err)
		return
	}
	s.emitBrainSnapshotBroadcast(snapshot)
}

func (s *Server) emitBrainSnapshotBroadcast(snapshot brain.Snapshot) {
	payload, err := s.brainSnapshotWire(snapshot)
	if err != nil {
		log.Printf("brain snapshot projection failed: %v", err)
		return
	}
	message := map[string]any{
		"type":  "brain_snapshot",
		"brain": payload,
	}
	if s.brainSnapshotBroadcastHook != nil {
		s.brainSnapshotBroadcastHook(message)
	}
	s.broadcastJSON(message)
}

func (s *Server) handleCalendarEvent(event calendar.Event) {
	s.broadcastJSON(map[string]any{"type": "calendar_item_changed", "calendar_item": event.Item})
	if s.brain != nil {
		if _, err := s.brain.RouteCalendarEvent(event); err != nil {
			log.Printf("calendar Work event routing failed for %s: %v", event.Item.ID, err)
		}
	}
	if event.ScheduledResult == nil || s.pusher == nil {
		return
	}
	result := event.ScheduledResult
	if err := s.pusher.NotifyScheduledResult(result.Title, string(result.Status), result.ThreadID, result.ID); err != nil {
		log.Printf("scheduled-result push failed: %v", err)
	}
}

func (s *Server) handleWorkEvent(ev work.Event) {
	switch ev.Type {
	case work.EventChanged:
		if !work.IsCalendarWorkItem(ev.Item) {
			return
		}
		s.broadcastJSON(map[string]any{
			"type":      "work_item_changed",
			"path":      ev.Path,
			"id":        ev.ID,
			"work_item": ev.Item,
		})
	case work.EventDeleted:
		s.broadcastJSON(map[string]any{
			"type": "work_item_deleted",
			"path": ev.Path,
			"id":   ev.ID,
		})
	}
}

func (s *Server) heartbeat(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			allAgentSessions := s.watcher.Agents()
			agentSessions := visibleAgentSessions(allAgentSessions)
			if s.brain != nil && s.watcher != nil && s.watcher.SnapshotReady() {
				if !s.brainMigrationComplete {
					if _, err := s.brain.MigrateDelegatedSessionsV1(agentSessions); err != nil {
						log.Printf("brain delegated Session migration failed: %v", err)
					} else {
						s.brainMigrationComplete = true
					}
				}
				s.brain.ReconcileDelegatedSessions(allAgentSessions)
			}
			data, _ := json.Marshal(map[string]any{"type": "agent_session_list", "agent_sessions": s.agentSessionsWire(agentSessions)})
			s.broadcast(data)
		}
	}
}

func (s *Server) handleWatcherEvent(ev watcher.SessionEvent) {
	if ev.Agent != nil && ev.Agent.Hidden {
		if s.brain != nil {
			if _, err := s.brain.ObserveHostSessionEvent(ev); err != nil {
				log.Printf("brain host event dispatch failed: %v", err)
			}
			// Hidden hosts stay off agent_session_list. Refresh brain_snapshot
			// only when the current Host appears or disappears so capabilities
			// converge after reconnect discovery — never on output/turn noise,
			// and never via ensureHostAgent (projection must not create/resume/
			// rebind as a side effect of a discovery event).
			if s.shouldBroadcastHiddenHostBrainSnapshot(ev) {
				s.broadcastBrainHostCapabilityRefresh()
			}
		}
		return
	}

	switch ev.Type {
	case "agent_discovered":
		if ev.Agent != nil {
			s.broadcastJSON(map[string]any{"type": "agent_session_created", "agent_session": s.agentSessionWire(ev.Agent)})
		}
	case "agent_output":
		if ev.Agent != nil {
			s.broadcastJSON(map[string]any{"type": "agent_session_updated", "agent_session": s.agentSessionWire(ev.Agent)})
		}
	case "agent_state_change":
		if ev.Agent != nil {
			s.broadcastJSON(map[string]any{"type": "agent_session_updated", "agent_session": s.agentSessionWire(ev.Agent)})
		}
		s.maybeNotifyForSessionEvent(ev)
		s.routeSessionEventToBrain(ev)
	case "agent_metadata_change":
		if ev.Agent != nil {
			s.broadcastJSON(map[string]any{"type": "agent_session_updated", "agent_session": s.agentSessionWire(ev.Agent)})
		}
		s.routeSessionEventToBrain(ev)
	case "agent_removed":
		if ev.Agent != nil {
			s.broadcastJSON(map[string]any{"type": "agent_session_archived", "agent_session": s.agentSessionWire(ev.Agent)})
		}
		s.maybeNotifyForSessionEvent(ev)
		s.routeSessionEventToBrain(ev)
	}
}

// shouldBroadcastHiddenHostBrainSnapshot admits only current-Host discovery and
// removal for capability projection. Those refreshes must not call
// ensureHostAgent. Output chunks, turn state, and unrelated Hidden agents never
// churn brain_snapshot.
func (s *Server) shouldBroadcastHiddenHostBrainSnapshot(ev watcher.SessionEvent) bool {
	if s == nil || s.brain == nil || ev.Agent == nil || !ev.Agent.Hidden {
		return false
	}
	switch ev.Type {
	case "agent_discovered", "agent_removed":
	default:
		return false
	}
	hostID := s.brain.CurrentHostSessionID()
	if hostID == "" {
		return false
	}
	return hostID == strings.TrimSpace(ev.Agent.ID)
}

func (s *Server) routeSessionEventToBrain(ev watcher.SessionEvent) {
	if s.brain == nil {
		return
	}
	woke, err := s.brain.RouteSessionEvent(ev)
	if err != nil {
		log.Printf("brain Work event routing failed for %s: %v", ev.AgentID, err)
		return
	}
	if woke {
		log.Printf("brain Work event dispatched for %s", ev.AgentID)
	}
}

func (s *Server) maybeNotifyForSessionEvent(ev watcher.SessionEvent) {
	if s.pusher == nil || ev.Agent == nil || ev.Agent.Hidden || !ev.Agent.Delegated {
		return
	}
	if ev.Type == "agent_removed" {
		return
	}
	if ev.Type != "agent_state_change" || ev.OldState == ev.NewState {
		return
	}
	if s.hasActiveViewer(ev.AgentID) {
		return
	}

	state := ev.NewState
	switch state {
	case "blocked":
		if err := s.pusher.NotifyAgentBlocked(ev.AgentID, ev.Agent.Name, ev.Agent.Summary); err != nil {
			log.Printf("blocked-agent push failed: %v", err)
		}
	case "failed":
		if err := s.pusher.NotifyAgentFailed(ev.AgentID, ev.Agent.Name, ev.Agent.Summary); err != nil {
			log.Printf("failed-agent push failed: %v", err)
		}
	case "done":
		if err := s.pusher.NotifyAgentDone(ev.AgentID, ev.Agent.Name, ev.Agent.Summary); err != nil {
			log.Printf("done-agent push failed: %v", err)
		}
	}
}

func (s *Server) broadcast(data []byte) {
	s.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(s.clients))
	for conn := range s.clients {
		conns = append(conns, conn)
	}
	s.mu.Unlock()

	for _, conn := range conns {
		if err := s.writeMessage(conn, websocket.TextMessage, data); err != nil {
			s.mu.Lock()
			work := s.removeClientLocked(conn)
			s.mu.Unlock()
			s.completeClientDetach(work)
		}
	}
}

func (s *Server) broadcastJSON(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	s.broadcast(data)
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	s.handleUploadWithLimits(w, r, productionUploadLimits())
}

func (s *Server) handleUploadWithLimits(w http.ResponseWriter, r *http.Request, limits uploadLimits) {
	if _, ok := s.authenticateRequest(w, r, "zen-upload"); !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	originalName, err := decodeUploadNameHeader(r.Header.Values(uploadNameHeader))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(r.Header.Get("Content-Type")) == "" {
		http.Error(w, "Content-Type header is required", http.StatusBadRequest)
		return
	}
	if r.ContentLength > limits.fileBytes {
		writeUploadTooLarge(w, limits)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, limits.fileBytes)
	if strings.TrimSpace(s.uploadDir) == "" {
		http.Error(w, "upload storage unavailable", http.StatusServiceUnavailable)
		return
	}

	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()
	if err := os.MkdirAll(s.uploadDir, 0o700); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	storedBytes, err := cleanupUploadStore(s.uploadDir, time.Now(), limits)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	remainingStore := limits.storeBytes - storedBytes
	if remainingStore <= 0 {
		http.Error(w, "upload storage capacity reached; remove old uploads or wait for retention cleanup", http.StatusInsufficientStorage)
		return
	}
	if r.ContentLength >= 0 && r.ContentLength > remainingStore {
		http.Error(w, "upload exceeds remaining storage capacity; remove old uploads or wait for retention cleanup", http.StatusInsufficientStorage)
		return
	}
	copyLimit := min(limits.fileBytes, remainingStore)
	name := uuid.New().String() + safeUploadExtension(originalName)
	path := filepath.Join(s.uploadDir, name)
	dst, createErr := os.CreateTemp(s.uploadDir, ".upload-*.partial")
	if createErr != nil {
		http.Error(w, createErr.Error(), http.StatusInternalServerError)
		return
	}
	partialPath := dst.Name()
	keepPartial := true
	defer func() {
		if keepPartial {
			_ = os.Remove(partialPath)
		}
	}()
	written, copyErr := io.Copy(dst, io.LimitReader(r.Body, copyLimit+1))
	closeErr := dst.Close()
	if copyErr != nil {
		writeUploadReadError(w, copyErr, limits)
		return
	}
	if closeErr != nil {
		http.Error(w, closeErr.Error(), http.StatusInternalServerError)
		return
	}
	if written > copyLimit {
		if remainingStore < limits.fileBytes {
			http.Error(w, "upload exceeds remaining storage capacity; remove old uploads or wait for retention cleanup", http.StatusInsufficientStorage)
			return
		}
		writeUploadTooLarge(w, limits)
		return
	}
	if r.ContentLength >= 0 && written != r.ContentLength {
		http.Error(w, "upload body does not match Content-Length", http.StatusBadRequest)
		return
	}
	if contextErr := r.Context().Err(); contextErr != nil {
		writeUploadReadError(w, contextErr, limits)
		return
	}
	if renameErr := os.Rename(partialPath, path); renameErr != nil {
		http.Error(w, renameErr.Error(), http.StatusInternalServerError)
		return
	}
	keepPartial = false

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"path": path, "name": originalName})
}

func cleanupUploadStore(dir string, now time.Time, limits uploadLimits) (int64, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("inspect upload storage: %w", err)
	}
	var total int64
	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr != nil {
			return 0, fmt.Errorf("inspect upload %s: %w", entry.Name(), infoErr)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if strings.HasPrefix(entry.Name(), ".upload-") && strings.HasSuffix(entry.Name(), ".partial") {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return 0, fmt.Errorf("remove partial upload %s: %w", entry.Name(), err)
			}
			continue
		}
		if now.Sub(info.ModTime()) >= limits.retention {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return 0, fmt.Errorf("remove expired upload %s: %w", entry.Name(), err)
			}
			continue
		}
		if info.Size() > 0 && total > limits.storeBytes-info.Size() {
			return limits.storeBytes, nil
		}
		total += max(info.Size(), 0)
	}
	return total, nil
}

func safeUploadExtension(name string) string {
	ext := filepath.Ext(filepath.Base(strings.TrimSpace(name)))
	if len(ext) < 2 || len(ext) > 11 || ext[0] != '.' {
		return ""
	}
	for _, char := range ext[1:] {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') {
			return ""
		}
	}
	return strings.ToLower(ext)
}

func decodeUploadNameHeader(values []string) (string, error) {
	if len(values) == 0 {
		return "", errors.New("upload filename header is required")
	}
	if len(values) != 1 {
		return "", errors.New("upload filename header must appear once")
	}
	encoded := values[0]
	if len(encoded) > maxUploadNameHeaderBytes {
		return "", errors.New("upload filename header is too long")
	}
	for index := range encoded {
		if encoded[index] < 0x21 || encoded[index] > 0x7e {
			return "", errors.New("upload filename header is invalid")
		}
	}
	decoded, err := url.PathUnescape(encoded)
	if err != nil || !utf8.ValidString(decoded) {
		return "", errors.New("upload filename header is invalid")
	}
	if len(decoded) > maxUploadNameBytes {
		return "", errors.New("upload filename header is too long")
	}
	if strings.TrimSpace(decoded) == "" {
		return "", errors.New("upload filename header is invalid")
	}
	for _, char := range decoded {
		if unicode.IsControl(char) {
			return "", errors.New("upload filename header is invalid")
		}
	}
	return decoded, nil
}

func writeUploadReadError(w http.ResponseWriter, err error, limits uploadLimits) {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		writeUploadTooLarge(w, limits)
		return
	}
	http.Error(w, err.Error(), http.StatusBadRequest)
}

func writeUploadTooLarge(w http.ResponseWriter, limits uploadLimits) {
	http.Error(w, fmt.Sprintf("upload exceeds the %s file limit", formatUploadBytes(limits.fileBytes)), http.StatusRequestEntityTooLarge)
}

func formatUploadBytes(size int64) string {
	for _, unit := range []struct {
		bytes int64
		name  string
	}{
		{bytes: 1 << 30, name: "GiB"},
		{bytes: 1 << 20, name: "MiB"},
		{bytes: 1 << 10, name: "KiB"},
	} {
		if size >= unit.bytes && size%unit.bytes == 0 {
			return fmt.Sprintf("%d %s", size/unit.bytes, unit.name)
		}
	}
	return fmt.Sprintf("%d bytes", size)
}

func (s *Server) authenticateRequest(w http.ResponseWriter, r *http.Request, purpose string) (*auth.TrustedDevice, bool) {
	device, err := s.auth.VerifyAuthorization(authorizationFromRequest(r), purpose, 5*time.Minute)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return nil, false
	}
	return device, true
}

func authorizationFromRequest(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("Authorization")); value != "" {
		return value
	}
	encoded := strings.TrimSpace(r.URL.Query().Get("auth"))
	if encoded == "" {
		return ""
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return ""
	}
	return string(decoded)
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if isAllowedCORSOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isAllowedCORSOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	host := parsed.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func (s *Server) writeJSONWithAssertion(w http.ResponseWriter, status int, purpose string, payload map[string]any) {
	assertion, err := s.auth.CreateServerAssertion(purpose)
	if err != nil {
		http.Error(w, "failed to sign daemon response", http.StatusInternalServerError)
		return
	}

	payload["assertion_purpose"] = purpose
	payload["assertion_timestamp"] = assertion.Timestamp
	payload["assertion_nonce"] = assertion.NonceHex
	payload["assertion_signature"] = assertion.SignatureHex

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func clientID(conn *websocket.Conn) string {
	return fmt.Sprintf("%p", conn)
}

func applyFrontmatterOverrides(fm *work.Frontmatter, raw map[string]interface{}) {
	if fm == nil || raw == nil {
		return
	}

	extra := make(map[string]interface{}, len(fm.Extra))
	for key, value := range fm.Extra {
		extra[key] = value
	}
	if value, ok := raw["extra"]; ok {
		extra = map[string]interface{}{}
		if nested, ok := value.(map[string]interface{}); ok {
			for nestedKey, nestedValue := range nested {
				extra[nestedKey] = nestedValue
			}
		}
	}
	for key, value := range raw {
		if key == "extra" {
			continue
		}
		switch key {
		case "id":
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				fm.ID = strings.TrimSpace(s)
			}
		case "kind":
			if s, ok := value.(string); ok {
				fm.Kind = strings.TrimSpace(s)
			}
		case "created":
			if parsed, ok := parseRFC3339Value(value); ok {
				fm.Created = parsed
			}
		case "done":
			if parsed, ok := parseRFC3339Value(value); ok {
				fm.Done = &parsed
			} else {
				fm.Done = nil
			}
		case "started":
			if parsed, ok := parseRFC3339Value(value); ok {
				fm.Started = &parsed
			} else {
				fm.Started = nil
			}
		case "status":
			if s, ok := value.(string); ok {
				fm.Status = strings.TrimSpace(s)
			}
		case "title":
			if s, ok := value.(string); ok {
				fm.Title = strings.TrimSpace(s)
			}
		case "agent_session":
			if s, ok := value.(string); ok {
				fm.AgentSession = s
			}
		default:
			extra[key] = value
		}
	}
	if len(extra) == 0 {
		fm.Extra = nil
		return
	}
	fm.Extra = extra
}

func parseRFC3339Value(value interface{}) (time.Time, bool) {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return time.Time{}, false
		}
		if parsed, err := time.Parse(time.RFC3339Nano, trimmed); err == nil {
			return parsed, true
		}
	case nil:
		return time.Time{}, false
	}
	return time.Time{}, false
}

func buildWorkFilename(now time.Time, body, fallbackID string) string {
	return now.Format("2006-01-02") + "-" + slugifyWorkTitle(firstLine(body), fallbackID) + ".md"
}

func slugifyWorkTitle(line, fallback string) string {
	trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
	if trimmed == "" {
		return strings.ToLower(fallback)
	}

	out := make([]rune, 0, len(trimmed))
	lastDash := false
	for _, r := range strings.ToLower(trimmed) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
			lastDash = false
		case r == ' ' || r == '-' || r == '_':
			if !lastDash {
				out = append(out, '-')
				lastDash = true
			}
		}
	}
	slug := strings.Trim(string(out), "-")
	if slug == "" {
		slug = strings.ToLower(fallback)
	}
	if len(slug) > 60 {
		slug = strings.Trim(slug[:60], "-")
	}
	if slug == "" {
		return strings.ToLower(fallback)
	}
	return slug
}

func firstLine(value string) string {
	if idx := strings.IndexByte(value, '\n'); idx >= 0 {
		return value[:idx]
	}
	return value
}

func (s *Server) hasActiveViewer(agentID string) bool {
	target := strings.TrimSpace(agentID)
	if target == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, activeAgentID := range s.active {
		if strings.TrimSpace(activeAgentID) == target {
			return true
		}
	}

	return false
}

func (s *Server) writeMessage(conn *websocket.Conn, messageType int, data []byte) error {
	s.mu.Lock()
	writeMu, ok := s.writes[conn]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("connection is closed")
	}

	writeMu.Lock()
	defer writeMu.Unlock()
	return conn.WriteMessage(messageType, data)
}
