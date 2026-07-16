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

	"github.com/google/uuid"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/calendar"
	"github.com/daoleno/zen/daemon/chatthread"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/codexshadow"
	"github.com/daoleno/zen/daemon/push"
	"github.com/daoleno/zen/daemon/stats"
	"github.com/daoleno/zen/daemon/terminal"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
	"github.com/gorilla/websocket"
	"github.com/oklog/ulid/v2"
)

const maxCodexAssetBytes = 6 << 20
const codexConversationSubscriptionInterval = 220 * time.Millisecond

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Server handles WebSocket connections from the zen mobile app.
type Server struct {
	auth                     *auth.Manager
	watcher                  *watcher.Watcher
	terminal                 *terminal.Manager
	pusher                   *push.Client
	stats                    *stats.Collector
	work                     *work.Store
	launcher                 *work.Launcher
	workLog                  *work.SessionLogger
	execs                    *work.ExecutorConfig
	brain                    *brain.Service
	calendar                 *calendar.Store
	calendarScheduler        *calendar.Scheduler
	lifecycle                *delegatedLifecycleManager
	structuredTurns          *structuredTurnRegistry
	structuredSnapshotLoader func(agentID string) (work.CodexConversation, error)
	canonicalCodex           *canonicalCodexProjection
	codexShadow              codexShadowObserver
	codexShadowBusy          atomic.Bool

	workSubID     int
	workSub       <-chan work.Event
	calendarSubID int
	calendarSub   <-chan calendar.Event

	clients   map[*websocket.Conn]bool
	active    map[*websocket.Conn]string
	writes    map[*websocket.Conn]*sync.Mutex
	codexSubs map[*websocket.Conn]map[string]codexConversationSubscription
	notified  map[string]struct{}
	brainSent map[string]struct{}
	mu        sync.Mutex
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

// codexShadowObserver is deliberately read-only and narrower than every
// provider-effect boundary. The server can offer structured records and legacy
// lifecycle metadata, but the observer cannot dispatch input or alter the wire
// projection returned to the App.
type codexShadowObserver interface {
	Enabled(ownerKey string) bool
	ObserveRollout(context.Context, codexshadow.Observation) (chatthread.ShadowSnapshot, error)
}

// New creates a WebSocket server.
func New(authManager *auth.Manager, w *watcher.Watcher, pusher *push.Client, sc *stats.Collector, workStore *work.Store, launcher *work.Launcher, execs *work.ExecutorConfig, brainService *brain.Service) *Server {
	srv := &Server{
		auth:            authManager,
		watcher:         w,
		terminal:        terminal.NewManager(&terminal.TmuxBackend{}),
		pusher:          pusher,
		stats:           sc,
		work:            workStore,
		launcher:        launcher,
		execs:           execs,
		brain:           brainService,
		clients:         make(map[*websocket.Conn]bool),
		active:          make(map[*websocket.Conn]string),
		writes:          make(map[*websocket.Conn]*sync.Mutex),
		codexSubs:       make(map[*websocket.Conn]map[string]codexConversationSubscription),
		notified:        make(map[string]struct{}),
		brainSent:       make(map[string]struct{}),
		structuredTurns: newStructuredTurnRegistry(),
		canonicalCodex:  newCanonicalCodexProjection(),
	}
	srv.lifecycle = newDelegatedLifecycleManager(
		func(event brain.HeartbeatEvent) (bool, error) {
			if brainService == nil {
				return false, nil
			}
			return brainService.Heartbeat(event)
		},
		func(agentID string) error {
			if w == nil {
				return nil
			}
			return w.KillSession(agentID)
		},
	)
	if workStore != nil {
		srv.workSubID, srv.workSub = workStore.Subscribe()
		srv.workLog = work.NewSessionLogger(workStore, work.NewAgentCLIDigestProvider(execs))
	}
	if authManager != nil {
		shadow, err := codexshadow.OpenConfigured(authManager.StorageDir(), nil)
		if err != nil {
			log.Printf("Codex Chat shadow diagnostics disabled (%s)", codexshadow.ErrorCode(err))
		} else {
			srv.codexShadow = shadow
		}
	}
	return srv
}

type clientMessage struct {
	Type                 string                 `json:"type"`
	RequestID            string                 `json:"request_id"`
	AgentID              string                 `json:"agent_id"`
	TargetID             string                 `json:"target_id"`
	Cwd                  string                 `json:"cwd"`
	Command              string                 `json:"command"`
	Name                 string                 `json:"name"`
	StartedAt            json.RawMessage        `json:"started_at"`
	Backend              string                 `json:"backend"`
	SessionID            string                 `json:"session_id"`
	Text                 string                 `json:"text"`
	Key                  string                 `json:"key"`
	Data                 string                 `json:"data"`
	Body                 string                 `json:"body"`
	Action               string                 `json:"action"`
	StateVersion         int64                  `json:"state_version"`
	PushToken            string                 `json:"push_token"`
	ServerRef            string                 `json:"server_ref"`
	Cols                 int                    `json:"cols"`
	Rows                 int                    `json:"rows"`
	Col                  int                    `json:"col"`
	Row                  int                    `json:"row"`
	Lines                int                    `json:"lines"`
	ProcessID            int                    `json:"process_id"`
	Path                 string                 `json:"path"`
	ID                   string                 `json:"id"`
	Project              string                 `json:"project"`
	Frontmatter          map[string]interface{} `json:"frontmatter"`
	BaseMtime            string                 `json:"base_mtime"`
	Prompt               string                 `json:"prompt"`
	Executor             string                 `json:"executor"`
	ExecutorID           string                 `json:"executor_id"`
	AdapterID            string                 `json:"adapter_id"`
	Personality          string                 `json:"personality"`
	Done                 bool                   `json:"done"`
	CalendarItem         *calendar.Item         `json:"calendar_item"`
	Revision             int64                  `json:"revision"`
	ConversationScopeKey string                 `json:"conversation_scope_key"`
	TurnID               string                 `json:"turn_id"`
	TurnStartedAt        json.RawMessage        `json:"turn_started_at"`
	TurnQueued           bool                   `json:"turn_queued"`
	TurnConversationID   string                 `json:"turn_conversation_identity"`
}

// Run starts the HTTP server and event broadcaster.
func (s *Server) Run(ctx context.Context, addr string) error {
	return s.RunWithReady(ctx, addr, nil)
}

// RunWithReady starts the HTTP server and calls onReady only after the TCP
// listener has been acquired successfully.
func (s *Server) RunWithReady(ctx context.Context, addr string, onReady func()) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/pair", s.handlePair)
	mux.HandleFunc("/auth-check", s.handleAuthCheck)
	mux.HandleFunc("/upload", s.handleUpload)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		s.writeJSONWithAssertion(w, http.StatusOK, "zen-health", map[string]any{
			"status":            "ok",
			"daemon_id":         s.auth.DaemonID(),
			"daemon_public_key": s.auth.PublicKeyHex(),
		})
	})

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: withCORS(mux)}

	go s.broadcastEvents(ctx)
	go s.heartbeat(ctx)

	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()

	log.Printf("zen listening on %s", listener.Addr())
	if onReady != nil {
		onReady()
	}
	return srv.Serve(listener)
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
	if _, ok := s.authenticateRequest(w, r, "zen-connect"); !ok {
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade: %v", err)
		return
	}
	defer conn.Close()
	defer s.terminal.CloseAll(clientID(conn))

	s.mu.Lock()
	s.clients[conn] = true
	s.active[conn] = ""
	s.writes[conn] = &sync.Mutex{}
	s.codexSubs[conn] = map[string]codexConversationSubscription{}
	s.mu.Unlock()

	log.Printf("client connected (%d total)", len(s.clients))
	s.sendAgentSessionList(conn)
	if s.work != nil {
		s.syncWorkLogsForAgents(false)
		s.sendJSON(conn, map[string]any{
			"type":                 "work_items_snapshot",
			"work_items":           work.FilterAgentWorkItems(s.work.List()),
			"executors":            s.executorRoles(),
			"work_digest_provider": s.workDigestProvider(),
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
		s.handleClientMessage(conn, msg)
	}

	s.mu.Lock()
	s.cancelCodexSubscriptionsLocked(conn)
	delete(s.clients, conn)
	delete(s.active, conn)
	delete(s.writes, conn)
	delete(s.codexSubs, conn)
	s.mu.Unlock()
	log.Printf("client disconnected (%d remaining)", len(s.clients))
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

func (s *Server) handleClientMessage(conn *websocket.Conn, msg []byte) {
	var raw clientMessage
	if err := json.Unmarshal(msg, &raw); err != nil {
		log.Printf("invalid message: %v", err)
		return
	}

	switch raw.Type {
	case "list_agents", "list_agent_sessions":
		s.sendAgentSessionList(conn)

	case "list_session_services":
		s.handleListSessionServices(conn, raw)

	case "list_work_items":
		s.handleListWorkItems(conn, raw)

	case "get_work_item":
		s.handleGetWorkItem(conn, raw)

	case "write_work_item":
		s.handleWriteWorkItem(conn, raw)

	case "start_work_item":
		s.handleStartWorkItem(conn, raw)

	case "rerun_work_item":
		s.handleRerunWorkItem(conn, raw)

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

	case "list_executors":
		s.handleListExecutors(conn, raw)

	case "set_work_digest_provider":
		s.handleSetWorkDigestProvider(conn, raw)

	case "brain_snapshot":
		s.sendBrainSnapshot(conn, raw.RequestID)

	case "brain_context":
		s.handleBrainContext(conn, raw)

	case "brain_gc":
		s.handleBrainGC(conn, raw)

	case "brain_set_executor":
		s.handleBrainSetExecutor(conn, raw)

	case "brain_chat_new":
		s.handleBrainChatNew(conn, raw)

	case "brain_workspace_tree":
		s.handleBrainWorkspaceTree(conn, raw)

	case "brain_workspace_file":
		s.handleBrainWorkspaceFile(conn, raw)

	case "register_push":
		if raw.PushToken != "" {
			s.pusher.SetRegistration(raw.PushToken, raw.ServerRef)
			s.sendJSON(conn, map[string]any{"type": "push_registered", "ok": true})
		}

	case "set_active_agent":
		s.mu.Lock()
		s.active[conn] = raw.AgentID
		s.mu.Unlock()

	case "send_input":
		dispatch := func() (structuredInputAcceptance, error) {
			return s.acceptStructuredInput(
				raw,
				func() error { return s.watcher.SendInput(raw.AgentID, raw.Text) },
			)
		}
		var acceptance structuredInputAcceptance
		var err error
		if strings.TrimSpace(raw.TurnID) != "" {
			scope := structuredTurnRegistryKey(raw.ConversationScopeKey, raw.AgentID)
			// Warm the provider frontier before allocating this Submission. Older
			// rollout input facts can therefore never bind to a newly accepted row.
			if baseline, baselineErr := s.loadStructuredConversationSnapshot(raw.AgentID); baselineErr == nil {
				s.canonicalCodexProjection().project(scope, baseline)
			}
			acceptance, err = s.canonicalCodexProjection().acceptWithDispatch(
				scope,
				canonicalCodexSubmission{
					ID:        raw.TurnID,
					Body:      raw.Text,
					StartedAt: clientStartedAt(raw.TurnStartedAt),
					AttemptID: raw.RequestID,
				},
				dispatch,
			)
		} else {
			acceptance, err = dispatch()
		}
		if err != nil {
			log.Printf("send_input error: %v", err)
			s.sendJSON(conn, structuredInputFailureResponse(raw, err))
		} else if raw.RequestID != "" {
			s.sendJSON(conn, map[string]any{
				"type":                  "input_accepted",
				"request_id":            raw.RequestID,
				"agent_id":              raw.AgentID,
				"turn_id":               acceptance.TurnID,
				"queued":                acceptance.Queued,
				"turn_revision":         acceptance.Revision,
				"turn_epoch":            acceptance.Epoch,
				"position":              acceptance.Position,
				"conversation_revision": acceptance.ConversationRevision,
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
		agentID, err := s.watcher.CreateSession(raw.TargetID, watcher.CreateSessionOptions{
			Cwd:     raw.Cwd,
			Command: raw.Command,
			Name:    raw.Name,
		})
		if err != nil {
			s.sendJSON(conn, map[string]any{
				"type":       "error",
				"code":       "create_session_failed",
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

	case "terminal_open":
		backend := raw.Backend
		if backend == "" {
			backend = "tmux"
		}
		targetID := raw.TargetID
		if targetID == "" {
			targetID = raw.AgentID
		}
		session, err := s.terminal.Open(clientID(conn), backend, targetID, terminal.OpenOptions{
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
		size := session.Size()
		s.sendJSON(conn, map[string]any{
			"type":       "terminal_opened",
			"session_id": session.ID(),
			"backend":    backend,
			"cols":       size.Cols,
			"rows":       size.Rows,
		})

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

	case "terminal_copy_buffer":
		buffer, err := s.terminal.CopyBuffer(clientID(conn), raw.SessionID)
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "terminal_copy_buffer_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{
			"type":       "terminal_copy_buffer",
			"request_id": raw.RequestID,
			"session_id": raw.SessionID,
			"text":       buffer,
		})

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
		// State version safety check: reject stale actions.
		if raw.StateVersion > 0 {
			agent := s.watcher.GetAgent(raw.AgentID)
			if agent != nil && agent.StateVersion != raw.StateVersion {
				s.sendErrorWithRequestID(conn, raw.RequestID, "stale_action",
					fmt.Sprintf("Agent state changed (version %d → %d). Refresh before acting.", raw.StateVersion, agent.StateVersion))
				return
			}
		}
		var actionAccepted = true
		var err error
		if raw.Action == "pause" {
			actionAccepted, err = s.interruptStructuredTurn(
				raw,
				func() error { return s.watcher.SendAction(raw.AgentID, raw.Action) },
			)
		} else {
			err = s.watcher.SendAction(raw.AgentID, raw.Action)
		}
		if err != nil {
			log.Printf("send_action error: %v", err)
			s.sendErrorWithRequestID(conn, raw.RequestID, "send_action_failed", err.Error())
		} else if !actionAccepted {
			s.sendErrorWithRequestID(conn, raw.RequestID, "stale_action", "Turn changed before Stop was accepted. Refresh and try again.")
		} else {
			s.sendJSON(conn, map[string]any{
				"type":       "action_confirmed",
				"request_id": raw.RequestID,
				"agent_id":   raw.AgentID,
				"action":     raw.Action,
				"turn_id":    raw.TurnID,
			})
		}

	case "kill_agent":
		if err := s.watcher.KillSession(raw.AgentID); err != nil {
			log.Printf("kill_agent error: %v", err)
			s.sendError(conn, "kill_failed", err.Error())
		} else {
			settledAt := time.Now()
			if strings.TrimSpace(raw.ConversationScopeKey) != "" {
				s.turnRegistry().cancelAll(
					structuredTurnRegistryKey(raw.ConversationScopeKey, raw.AgentID),
					settledAt,
				)
			}
			s.turnRegistry().cancelAllForAgent(raw.AgentID, settledAt)
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
			s.sendJSON(conn, payload)
		} else {
			s.sendJSON(conn, map[string]any{
				"type":       "stats_data",
				"request_id": raw.RequestID,
				"ranges":     map[string]any{},
			})
		}

	default:
		log.Printf("unknown message type: %s", raw.Type)
		if raw.RequestID != "" {
			s.sendErrorWithRequestID(conn, raw.RequestID, "unknown_message_type", fmt.Sprintf("Unknown message type: %s", raw.Type))
		}
	}
}

func structuredInputFailureResponse(raw clientMessage, err error) map[string]any {
	var rejected *structuredInputRejectedError
	if errors.As(err, &rejected) {
		return map[string]any{
			"type":       "input_rejected",
			"request_id": raw.RequestID,
			"agent_id":   raw.AgentID,
			"turn_id":    raw.TurnID,
			"code":       rejected.code,
			"message":    rejected.Error(),
			"retryable":  rejected.retryable,
		}
	}
	return map[string]any{
		"type":       "input_unconfirmed",
		"request_id": raw.RequestID,
		"agent_id":   raw.AgentID,
		"turn_id":    raw.TurnID,
		"code":       "send_input_unconfirmed",
		"message":    "The daemon could not confirm whether the executor accepted this input.",
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

func (s *Server) turnRegistry() *structuredTurnRegistry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.structuredTurns == nil {
		s.structuredTurns = newStructuredTurnRegistry()
	}
	return s.structuredTurns
}

func (s *Server) canonicalCodexProjection() *canonicalCodexProjection {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.canonicalCodex == nil {
		s.canonicalCodex = newCanonicalCodexProjection()
	}
	return s.canonicalCodex
}

// refreshStructuredTurnBaseline reconciles the latest trusted provider facts
// immediately before a structured Send or Stop. This is deliberately not
// conditional on the registry being empty: the provider may have settled A
// and promoted B between subscription polls, in which case Stop(A) must be
// rejected before any pause reaches the executor. Client hints and terminal
// panes never establish this baseline.
func (s *Server) refreshStructuredTurnBaseline(key, agentID string) (string, error) {
	registry := s.turnRegistry()
	if key == "" {
		return "", nil
	}
	conversation, err := s.loadStructuredConversationSnapshot(agentID)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(key, "agent:") {
		registry.markWorkAgentPresent(agentID)
	}
	registry.projectProviderHistoryWithContext(
		key,
		agentID,
		structuredConversationThreadIdentity(conversation),
		conversation.ProviderTurns,
		conversation.Turn,
	)
	return structuredConversationThreadIdentity(conversation), nil
}

func (s *Server) loadStructuredConversationSnapshot(agentID string) (work.CodexConversation, error) {
	if s.structuredSnapshotLoader != nil {
		return s.structuredSnapshotLoader(strings.TrimSpace(agentID))
	}
	if s.watcher == nil {
		return work.CodexConversation{}, fmt.Errorf("%w: executor watcher unavailable", errStructuredLifecycleSyncing)
	}
	agent := s.watcher.GetAgent(strings.TrimSpace(agentID))
	if agent == nil {
		return work.CodexConversation{}, fmt.Errorf("%w: executor unavailable", errStructuredLifecycleSyncing)
	}
	provider := s.structuredProviderForAgent(agent)
	if provider == "" {
		return work.CodexConversation{}, fmt.Errorf("%w: structured provider unavailable", errStructuredLifecycleSyncing)
	}
	conversation, err := work.LoadCodexConversationForProvider(*agent, provider, time.Now())
	if err != nil {
		return work.CodexConversation{}, fmt.Errorf("%w: %v", errStructuredLifecycleSyncing, err)
	}
	return conversation, nil
}

func (s *Server) acceptStructuredInput(
	raw clientMessage,
	dispatch func() error,
) (structuredInputAcceptance, error) {
	key := structuredTurnRegistryKey(raw.ConversationScopeKey, raw.AgentID)
	conversationIdentity := ""
	if strings.TrimSpace(raw.TurnID) != "" {
		if accepted, err, ok := s.turnRegistry().replayInput(key, raw.TurnID); ok {
			return accepted, err
		}
		var err error
		conversationIdentity, err = s.refreshStructuredTurnBaseline(key, raw.AgentID)
		if err != nil {
			return structuredInputAcceptance{}, &structuredInputRejectedError{
				cause:     err,
				code:      "structured_lifecycle_syncing",
				retryable: true,
			}
		}
	}
	threadControl := isStructuredThreadControlInput(raw.Text)
	baselineConversation := strings.TrimSpace(raw.TurnConversationID)
	if threadControl && baselineConversation == "" {
		baselineConversation = firstNonEmptyStructuredIdentity(
			conversationIdentity,
			s.currentStructuredConversationIdentity(raw.AgentID),
		)
	}
	return s.turnRegistry().acceptInputWithOptions(
		key,
		raw.AgentID,
		raw.TurnID,
		clientStructuredTurnStartedAt(raw.TurnStartedAt),
		raw.TurnQueued,
		threadControl,
		baselineConversation,
		dispatch,
	)
}

func (s *Server) interruptStructuredTurn(
	raw clientMessage,
	dispatch func() error,
) (bool, error) {
	key := structuredTurnRegistryKey(raw.ConversationScopeKey, raw.AgentID)
	if strings.TrimSpace(raw.TurnID) != "" {
		if _, err := s.refreshStructuredTurnBaseline(key, raw.AgentID); err != nil {
			return false, err
		}
	}
	return s.turnRegistry().interruptWithDispatch(
		key,
		raw.TurnID,
		time.Now(),
		dispatch,
	)
}

func (s *Server) projectStructuredConversationTurns(
	conversationScopeKey string,
	agentID string,
	conversation work.CodexConversation,
) work.CodexConversation {
	return s.projectStructuredConversationTurnsWithTrust(
		conversationScopeKey,
		agentID,
		conversation,
		true,
	)
}

func (s *Server) projectStructuredConversationTurnsWithTrust(
	conversationScopeKey string,
	agentID string,
	conversation work.CodexConversation,
	trustedProviderFacts bool,
) work.CodexConversation {
	if s.isHistoricalBrainConversationScope(conversationScopeKey) {
		key := structuredTurnRegistryKey(conversationScopeKey, agentID)
		s.turnRegistry().cancelAll(key, time.Now())
		conversation.Turn, conversation.QueuedTurns, conversation.TurnEpoch, conversation.TurnRevision = s.turnRegistry().projectProviderHistoryWithContext(
			key,
			"",
			"",
			nil,
			nil,
		)
		if conversation.QueuedTurns == nil {
			conversation.QueuedTurns = []work.CodexConversationTurn{}
		}
		active := false
		conversation.Active = &active
		return conversation
	}
	key := structuredTurnRegistryKey(conversationScopeKey, agentID)
	projectionAgentID := ""
	conversationIdentity := ""
	var providerHistory []work.CodexConversationTurn
	var providerTurn *work.CodexConversationTurn
	if trustedProviderFacts {
		projectionAgentID = agentID
		conversationIdentity = structuredConversationThreadIdentity(conversation)
		providerHistory = conversation.ProviderTurns
		providerTurn = conversation.Turn
	}
	conversation.Turn, conversation.QueuedTurns, conversation.TurnEpoch, conversation.TurnRevision = s.turnRegistry().projectProviderHistoryWithContext(
		key,
		projectionAgentID,
		conversationIdentity,
		providerHistory,
		providerTurn,
	)
	if conversation.QueuedTurns == nil {
		conversation.QueuedTurns = []work.CodexConversationTurn{}
	}
	active := conversation.Turn != nil &&
		conversation.Turn.Status == work.CodexConversationTurnRunning
	conversation.Active = &active
	return conversation
}

func isStructuredThreadControlInput(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "/new", "/clear":
		return true
	default:
		return false
	}
}

func (s *Server) currentStructuredConversationIdentity(agentID string) string {
	if s == nil || s.watcher == nil {
		return ""
	}
	agent := s.watcher.GetAgent(strings.TrimSpace(agentID))
	if agent == nil {
		return ""
	}
	provider := s.structuredProviderForAgent(agent)
	if provider == "" {
		return ""
	}
	conversation, err := work.LoadCodexConversationForProvider(*agent, provider, time.Now())
	if err != nil {
		return ""
	}
	return structuredConversationThreadIdentity(conversation)
}

func (s *Server) isHistoricalBrainConversationScope(scopeKey string) bool {
	const prefix = "brain-thread:"
	scopeKey = strings.TrimSpace(scopeKey)
	if s.brain == nil || !strings.HasPrefix(scopeKey, prefix) {
		return false
	}
	threadID := strings.TrimSpace(strings.TrimPrefix(scopeKey, prefix))
	if threadID == "" {
		return false
	}
	currentThreadID, err := s.brain.ChatThreadID()
	currentThreadID = strings.TrimSpace(currentThreadID)
	return err == nil && currentThreadID != "" && threadID != currentThreadID
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
		s.publishCodexConversationSubscription(ctx, conn, raw, subscriptionID, generation, &previous)
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
	previous **codexConversationSubscriptionSnapshot,
) {
	if ctx.Err() != nil || !s.isCurrentCodexSubscription(conn, subscriptionID, generation) {
		return
	}
	resolved := s.resolveCodexConversationAgent(raw)
	if !resolved.ready {
		if strings.TrimSpace(raw.ConversationScopeKey) == "" {
			s.turnRegistry().failWorkAgent(resolved.targetID, time.Now())
		}
		conversation := s.projectStructuredConversationTurnsWithTrust(
			raw.ConversationScopeKey,
			resolved.targetID,
			work.CodexConversation{
				Available:   false,
				Reason:      resolved.reason,
				Events:      []work.CodexConversationEvent{},
				QueuedTurns: []work.CodexConversationTurn{},
			},
			false,
		)
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
		s.sendJSON(conn, map[string]any{
			"type":            "codex_conversation_sync_status",
			"request_id":      subscriptionID,
			"generation":      generation,
			"agent_id":        resolved.targetID,
			"conversation_id": "",
			"revision":        revision,
			"state":           "syncing",
			"reason":          resolved.reason,
			"active":          conversation.Active,
			"turn":            conversation.Turn,
			"turn_revision":   conversation.TurnRevision,
			"turn_epoch":      conversation.TurnEpoch,
			"queued_turns":    conversation.QueuedTurns,
		})
		*previous = &codexConversationSubscriptionSnapshot{
			conversation: conversation,
			fingerprint:  fingerprint,
			eventsByID:   map[string]work.CodexConversationEvent{},
			revision:     revision,
		}
		return
	}
	if resolved.fromWatcher {
		s.turnRegistry().markWorkAgentPresent(resolved.targetID)
	}

	now := time.Now()
	conversation, err := work.LoadCodexConversationForProvider(
		resolved.agent,
		resolved.provider,
		now,
	)
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
	if !resolved.fromWatcher {
		if strings.TrimSpace(raw.ConversationScopeKey) == "" {
			s.turnRegistry().failWorkAgent(resolved.targetID, now)
		}
	}
	if work.ShouldUseTerminalSnapshotConversationFallback(resolved.agent, conversation) {
		if s.watcher == nil {
			conversation = work.TerminalSnapshotConversationUnavailableForAgent(resolved.agent, "terminal_snapshot_unavailable", now)
		} else if text, err := s.watcher.CapturePaneContent(resolved.agent.ID); err != nil {
			conversation = work.TerminalSnapshotConversationUnavailableForAgent(resolved.agent, "terminal_snapshot_unavailable", now)
		} else {
			conversation = work.TerminalSnapshotConversationForAgent(resolved.agent, text, now)
		}
	}
	providerConversation := cloneCodexConversation(conversation)
	legacyConversation := s.projectStructuredConversationTurnsWithTrust(
		raw.ConversationScopeKey,
		resolved.targetID,
		cloneCodexConversation(providerConversation),
		resolved.fromWatcher,
	)
	s.observeCodexConversationShadow(ctx, raw, resolved, legacyConversation)

	visibleSource := s.brainScopedConversation(
		raw.ConversationScopeKey,
		cloneCodexConversation(providerConversation),
		now,
	)
	canonicalReplace := false
	canonicalRevision := uint64(0)
	if strings.EqualFold(strings.TrimSpace(resolved.provider), "codex") &&
		strings.TrimSpace(providerConversation.Source) == "codex_rollout" {
		if visibleSource.Turn != nil && legacyConversation.Turn != nil &&
			legacyConversation.Turn.Status == work.CodexConversationTurnRunning {
			visibleSource.Turn.ControlID = legacyConversation.Turn.ID
		}
		visibleSource.TurnRevision = 0
		visibleSource.TurnEpoch = ""
		visibleSource.QueuedTurns = []work.CodexConversationTurn{}
		canonical := s.canonicalCodexProjection().project(
			structuredTurnRegistryKey(raw.ConversationScopeKey, resolved.targetID),
			visibleSource,
		)
		conversation = canonical.Conversation
		canonicalReplace = canonical.Replace
		canonicalRevision = canonical.Revision
	} else {
		// Provider-safe fallback keeps the legacy adapter for control metadata,
		// but Working is still owned by the raw provider Activity.
		conversation = s.brainScopedConversation(raw.ConversationScopeKey, legacyConversation, now)
		conversation.Activity = cloneCodexConversationTurn(providerConversation.Turn)
		if conversation.Activity != nil && conversation.Turn != nil &&
			conversation.Turn.Status == work.CodexConversationTurnRunning {
			conversation.Activity.ControlID = conversation.Turn.ID
		}
	}
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
	if canonicalRevision > 0 {
		next.revision = int64(canonicalRevision)
	}
	if (*previous) != nil {
		if next.revision <= (*previous).revision {
			next.revision = (*previous).revision + 1
		}
	}

	if (*previous) == nil || canonicalReplace || !(*previous).conversation.Available ||
		codexConversationIdentity((*previous).conversation) != codexConversationIdentity(conversation) {
		if !s.isCurrentCodexSubscription(conn, subscriptionID, generation) {
			return
		}
		s.sendJSON(conn, map[string]any{
			"type":            "codex_conversation_snapshot",
			"request_id":      subscriptionID,
			"generation":      generation,
			"agent_id":        resolved.targetID,
			"conversation_id": codexConversationIdentity(conversation),
			"revision":        next.revision,
			"conversation":    conversation,
		})
		*previous = &next
		return
	}

	upserts, deletes := codexConversationDelta((*previous).eventsByID, conversation.Events)
	if !s.isCurrentCodexSubscription(conn, subscriptionID, generation) {
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":            "codex_conversation_delta",
		"request_id":      subscriptionID,
		"generation":      generation,
		"agent_id":        resolved.targetID,
		"conversation_id": codexConversationIdentity(conversation),
		"base_revision":   (*previous).revision,
		"revision":        next.revision,
		"available":       conversation.Available,
		"reason":          conversation.Reason,
		"source":          conversation.Source,
		"path":            conversation.Path,
		"session_id":      conversation.SessionID,
		"cwd":             conversation.CWD,
		"updated_at":      conversation.Updated,
		"active":          conversation.Active,
		"activity":        conversation.Activity,
		"turn":            conversation.Turn,
		"turn_revision":   conversation.TurnRevision,
		"turn_epoch":      conversation.TurnEpoch,
		"queued_turns":    conversation.QueuedTurns,
		"upserts":         upserts,
		"deletes":         deletes,
	})
	*previous = &next
}

func (s *Server) isCurrentCodexSubscription(conn *websocket.Conn, subscriptionID, generation string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	subscriptions := s.codexSubs[conn]
	current, ok := subscriptions[subscriptionID]
	return ok && current.generation == generation
}

func (s *Server) observeCodexConversationShadow(
	ctx context.Context,
	raw clientMessage,
	resolved resolvedCodexConversationAgent,
	conversation work.CodexConversation,
) {
	if s == nil || s.codexShadow == nil || !resolved.fromWatcher ||
		strings.ToLower(strings.TrimSpace(resolved.provider)) != "codex" {
		return
	}
	ownerKey := structuredTurnRegistryKey(raw.ConversationScopeKey, resolved.targetID)
	if !s.codexShadow.Enabled(ownerKey) || strings.TrimSpace(conversation.Path) == "" ||
		strings.TrimSpace(conversation.SessionID) == "" {
		return
	}
	observation := codexshadow.Observation{
		OwnerKey:    ownerKey,
		RolloutPath: conversation.Path,
		SessionID:   conversation.SessionID,
		Legacy:      legacyShadowProjection(conversation),
	}
	// Diagnostics must never become a latency dependency of the v1 WebSocket
	// projection. Keep one bounded background read; a busy tick is retried by the
	// existing subscription poll instead of accumulating goroutines.
	if !s.codexShadowBusy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer s.codexShadowBusy.Store(false)
		_, err := s.codexShadow.ObserveRollout(ctx, observation)
		if err != nil && !errors.Is(err, codexshadow.ErrDisabled) && !errors.Is(err, context.Canceled) {
			// ErrorCode is a closed sanitized enum. In particular, never log the
			// rollout path or an underlying parser/storage error here.
			log.Printf("Codex Chat shadow diagnostics disabled (%s)", codexshadow.ErrorCode(err))
		}
	}()
}

func legacyShadowProjection(conversation work.CodexConversation) chatthread.LegacyShadowProjection {
	projection := chatthread.LegacyShadowProjection{
		OrderedTurns: []chatthread.LegacyShadowTurn{},
		Queued:       []chatthread.LegacyShadowTurn{},
	}
	if conversation.Turn != nil {
		turn := chatthread.LegacyShadowTurn{ID: conversation.Turn.ID, State: conversation.Turn.Status}
		projection.OrderedTurns = append(projection.OrderedTurns, turn)
		if conversation.Turn.Status == work.CodexConversationTurnRunning {
			current := turn
			projection.Current = &current
		} else if isStructuredTurnTerminal(conversation.Turn.Status) {
			projection.TerminalState = conversation.Turn.Status
		}
	}
	for _, queued := range conversation.QueuedTurns {
		turn := chatthread.LegacyShadowTurn{ID: queued.ID, State: queued.Status}
		projection.OrderedTurns = append(projection.OrderedTurns, turn)
		projection.Queued = append(projection.Queued, turn)
	}
	return projection
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
	currentThreadID, currentThreadErr := s.brain.ChatThreadID()
	messages, err := s.brain.ChatMessages(threadID)
	if err != nil {
		conversation.Available = false
		conversation.Reason = "brain_messages_unavailable"
		return conversation
	}
	if currentThreadErr == nil && strings.TrimSpace(currentThreadID) != "" &&
		threadID != strings.TrimSpace(currentThreadID) {
		conversation.Events = []work.CodexConversationEvent{}
		conversation.Turn = nil
		conversation.Activity = nil
		conversation.QueuedTurns = []work.CodexConversationTurn{}
		active := false
		conversation.Active = &active
	}
	eventsByID := make(map[string]int, len(conversation.Events)+len(messages))
	uniqueEvents := make([]work.CodexConversationEvent, 0, len(conversation.Events)+len(messages))
	for _, event := range conversation.Events {
		if id := strings.TrimSpace(event.ID); id != "" {
			if index, ok := eventsByID[id]; ok {
				uniqueEvents[index] = event
				continue
			}
			eventsByID[id] = len(uniqueEvents)
		}
		uniqueEvents = append(uniqueEvents, event)
	}
	conversation.Events = uniqueEvents
	for _, message := range messages {
		if message.Kind != "calendar_result" {
			continue
		}
		event := calendarResultConversationEvent(message)
		if index, ok := eventsByID[event.ID]; ok {
			conversation.Events[index] = event
			continue
		}
		eventsByID[event.ID] = len(conversation.Events)
		conversation.Events = append(conversation.Events, event)
	}
	sort.SliceStable(conversation.Events, func(left, right int) bool {
		return brainConversationEventLess(conversation.Events[left], conversation.Events[right])
	})
	conversation.Available = true
	conversation.Reason = ""
	conversation.Source = "brain_chat"
	conversation.SessionID = prefix + threadID
	conversation.Updated = &now
	return conversation
}

func calendarResultConversationEvent(message brain.ChatMessage) work.CodexConversationEvent {
	title := strings.TrimSpace(message.Title)
	status := strings.TrimSpace(message.Status)
	if title != "" && status != "" {
		title += " " + status
	}
	body := strings.TrimSpace(message.Body)
	if heading := strings.TrimSpace(message.Title + " " + status); heading != "" {
		body = strings.TrimSpace(strings.TrimPrefix(body, "**"+heading+"**"))
	}
	return work.CodexConversationEvent{
		ID:        message.ID,
		Timestamp: message.CreatedAt.Format(time.RFC3339Nano),
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

func structuredConversationThreadIdentity(conversation work.CodexConversation) string {
	if sessionID := strings.TrimSpace(conversation.SessionID); sessionID != "" {
		return "session:" + sessionID
	}
	if path := strings.TrimSpace(conversation.Path); path != "" {
		return "path:" + path
	}
	return ""
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
	writeFingerprintBool(hash, conversation.Available)
	writeFingerprintString(hash, conversation.Reason)
	writeFingerprintString(hash, conversation.Source)
	writeFingerprintString(hash, conversation.CWD)
	writeFingerprintString(hash, codexConversationActiveValue(conversation.Active))
	writeFingerprintString(hash, fmt.Sprintf("%d", conversation.TurnRevision))
	writeFingerprintString(hash, conversation.TurnEpoch)
	writeCodexConversationTurnFingerprint(hash, conversation.Turn)
	writeCodexConversationTurnFingerprint(hash, conversation.Activity)
	writeFingerprintInt(hash, len(conversation.QueuedTurns))
	for index := range conversation.QueuedTurns {
		writeCodexConversationTurnFingerprint(hash, &conversation.QueuedTurns[index])
	}
	writeCodexConversationEventsFingerprint(hash, conversation.Events)
	return fmt.Sprintf("%016x", hash.Sum64())
}

func writeCodexConversationTurnFingerprint(w io.Writer, turn *work.CodexConversationTurn) {
	if turn == nil {
		writeFingerprintString(w, "")
		return
	}
	writeFingerprintString(w, turn.ID)
	writeFingerprintString(w, turn.Status)
	writeFingerprintString(w, turn.StartedAt)
	writeFingerprintString(w, turn.SettledAt)
	writeFingerprintString(w, turn.ControlID)
}

func codexConversationActiveValue(active *bool) string {
	if active == nil {
		return ""
	}
	if *active {
		return "true"
	}
	return "false"
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
	writeFingerprintString(w, event.Explanation)
	writeFingerprintPlan(w, event.Plan)
	writeFingerprintString(w, event.Source)
	writeFingerprintString(w, fmt.Sprintf("%d", event.Position))
	writeFingerprintString(w, fmt.Sprintf("%d", event.EventRevision))
	writeFingerprintString(w, event.ActivityID)
	writeFingerprintString(w, event.SubmissionID)
	writeFingerprintString(w, event.SubmissionState)
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

func clientStructuredTurnStartedAt(raw json.RawMessage) string {
	startedAt := clientStartedAt(raw)
	if startedAt.IsZero() {
		return ""
	}
	return startedAt.UTC().Format(time.RFC3339Nano)
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

func (s *Server) executorRoles() []string {
	if s.execs == nil {
		return nil
	}
	return s.execs.Roles()
}

func (s *Server) workDigestProvider() string {
	if s.workLog != nil {
		if provider := s.workLog.DigestProvider(); provider != "" {
			return provider
		}
	}
	return "auto"
}

func (s *Server) handleListWorkItems(conn *websocket.Conn, raw clientMessage) {
	if s.work == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "list_work_items_failed", "work store not configured")
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":                 "work_items_snapshot",
		"request_id":           raw.RequestID,
		"work_items":           work.FilterAgentWorkItems(s.work.List()),
		"executors":            s.executorRoles(),
		"work_digest_provider": s.workDigestProvider(),
	})
}

func (s *Server) handleGetWorkItem(conn *websocket.Conn, raw clientMessage) {
	if s.work == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "get_work_item_failed", "work store not configured")
		return
	}
	item, ok := s.work.GetByID(strings.TrimSpace(raw.ID))
	if !ok {
		s.sendErrorWithRequestID(conn, raw.RequestID, "get_work_item_failed", "work item not found")
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "work_item",
		"request_id": raw.RequestID,
		"work_item":  item,
	})
}

func (s *Server) handleListExecutors(conn *websocket.Conn, raw clientMessage) {
	s.sendJSON(conn, map[string]any{
		"type":                 "executor_list",
		"request_id":           raw.RequestID,
		"executors":            s.executorRoles(),
		"work_digest_provider": s.workDigestProvider(),
	})
}

func (s *Server) handleSetWorkDigestProvider(conn *websocket.Conn, raw clientMessage) {
	if s.workLog == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "set_work_digest_provider_failed", "work log not configured")
		return
	}
	provider, ok := s.workLog.SetDigestProvider(raw.Name)
	if !ok {
		s.sendErrorWithRequestID(conn, raw.RequestID, "set_work_digest_provider_failed", "unsupported digest provider")
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":                 "work_digest_provider",
		"request_id":           raw.RequestID,
		"work_digest_provider": provider,
	})
	go s.syncWorkLogsForAgents(true)
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
	s.sendJSON(conn, map[string]any{
		"type":       "brain_snapshot",
		"request_id": requestID,
		"brain":      brainSnapshotWire(snapshot),
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
	s.sendJSON(conn, map[string]any{
		"type":       "brain_snapshot",
		"request_id": raw.RequestID,
		"brain":      brainSnapshotWire(snapshot),
	})
}

func (s *Server) handleBrainContext(conn *websocket.Conn, raw clientMessage) {
	if s.brain == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_unavailable", "Brain is not configured")
		return
	}
	context, err := s.brain.Context(12)
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
	s.sendJSON(conn, map[string]any{
		"type":       "brain_snapshot",
		"request_id": raw.RequestID,
		"brain":      brainSnapshotWire(snapshot),
	})
}

func brainSnapshotWire(snapshot brain.Snapshot) any {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return snapshot
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return snapshot
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
	return payload
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
	agentSessions := visibleAgentSessions(s.watcher.Agents())
	s.enrichCodexAgentSessionTitles(agentSessions)
	return agentSessions
}

func (s *Server) enrichCodexAgentSessionTitles(agents []*classifier.Agent) {
	now := time.Now()
	for _, agent := range agents {
		s.enrichCodexAgentSessionTitle(agent, now)
	}
}

func (s *Server) enrichCodexAgentSessionTitle(agent *classifier.Agent, now time.Time) {
	if agent == nil {
		return
	}
	title, ok, err := work.CodexThreadTitleForAgent(*agent, now)
	if err != nil || !ok {
		return
	}
	if name := agentSessionNameWithTarget(title, agent.ID); name != "" {
		agent.Name = name
	}
}

func agentSessionNameWithTarget(name, target string) string {
	name = strings.TrimSpace(name)
	target = strings.TrimSpace(target)
	if name == "" {
		return ""
	}
	if target == "" {
		return name
	}
	suffix := " (" + target + ")"
	if strings.HasSuffix(name, suffix) {
		return name
	}
	return name + suffix
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
		frontmatter = work.Frontmatter{
			ID:           existing.Frontmatter.ID,
			Kind:         existing.Frontmatter.Kind,
			Created:      existing.Frontmatter.Created,
			Done:         existing.Frontmatter.Done,
			Started:      existing.Frontmatter.Started,
			Status:       existing.Frontmatter.Status,
			Title:        existing.Frontmatter.Title,
			Outcome:      existing.Frontmatter.Outcome,
			Summary:      existing.Frontmatter.Summary,
			Progress:     existing.Frontmatter.Progress,
			Friction:     existing.Frontmatter.Friction,
			Cause:        existing.Frontmatter.Cause,
			Insight:      existing.Frontmatter.Insight,
			Next:         existing.Frontmatter.Next,
			AgentSource:  existing.Frontmatter.AgentSource,
			AgentSession: existing.Frontmatter.AgentSession,
			Cwd:          existing.Frontmatter.Cwd,
			Command:      existing.Frontmatter.Command,
			AIProvider:   existing.Frontmatter.AIProvider,
			AIUpdated:    existing.Frontmatter.AIUpdated,
			AIHash:       existing.Frontmatter.AIHash,
			AIError:      existing.Frontmatter.AIError,
			Extra:        existing.Frontmatter.Extra,
		}
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

func (s *Server) handleStartWorkItem(conn *websocket.Conn, raw clientMessage) {
	s.handleLaunchWorkItem(conn, raw, false)
}

func (s *Server) handleRerunWorkItem(conn *websocket.Conn, raw clientMessage) {
	s.handleLaunchWorkItem(conn, raw, true)
}

func (s *Server) handleLaunchWorkItem(conn *websocket.Conn, raw clientMessage, rerun bool) {
	failCode := "start_work_item_failed"
	if rerun {
		failCode = "rerun_work_item_failed"
	}
	if s.work == nil || s.launcher == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, failCode, "work launcher not configured")
		return
	}

	item, ok := s.work.GetByID(strings.TrimSpace(raw.ID))
	if !ok {
		s.sendErrorWithRequestID(conn, raw.RequestID, failCode, "work item not found")
		return
	}
	project, err := work.LoadProject(filepath.Dir(item.Path))
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, failCode, err.Error())
		return
	}

	var updated *work.Item
	if rerun {
		updated, err = s.launcher.Rerun(item, project)
	} else {
		updated, err = s.launcher.Start(item, project)
	}
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, failCode, err.Error())
		return
	}

	written, err := s.work.Write(updated, time.Time{})
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, failCode, err.Error())
		return
	}

	msgType := "work_item_started"
	if rerun {
		msgType = "work_item_rerun"
	}
	s.sendJSON(conn, map[string]any{
		"type":       msgType,
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
			s.broadcastJSON(map[string]any{"type": "calendar_item_changed", "calendar_item": event.Item})
		}
	}
}

func (s *Server) handleWorkEvent(ev work.Event) {
	switch ev.Type {
	case work.EventChanged:
		if !work.IsAgentWorkItem(ev.Item) {
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
			agentSessions := s.currentVisibleAgentSessions()
			s.syncWorkLogs(agentSessions, false)
			s.observeDelegatedLifecycleAgents(agentSessions)
			data, _ := json.Marshal(map[string]any{"type": "agent_session_list", "agent_sessions": s.agentSessionsWire(agentSessions)})
			s.broadcast(data)
		}
	}
}

func (s *Server) handleWatcherEvent(ev watcher.SessionEvent) {
	if ev.Agent != nil && ev.Agent.Hidden {
		return
	}
	if ev.Agent != nil && ev.Type != "agent_output" {
		s.enrichCodexAgentSessionTitle(ev.Agent, time.Now())
	}
	s.recordWorkForSessionEvent(ev)
	brainWoke := false

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
		brainWoke = s.maybeWakeBrainForSessionEvent(ev)
	case "agent_metadata_change":
		if ev.Agent != nil {
			s.broadcastJSON(map[string]any{"type": "agent_session_updated", "agent_session": s.agentSessionWire(ev.Agent)})
		}
		brainWoke = s.maybeWakeBrainForSessionEvent(ev)
	case "agent_removed":
		// Watcher removal is an authoritative executor termination for an
		// ordinary Work agent. Brain's durable lifecycle is scope-keyed and is
		// intentionally not touched because its hidden host may be replaced
		// while the same delegated turn continues.
		s.turnRegistry().failWorkAgent(ev.AgentID, time.Now())
		if ev.Agent != nil {
			s.broadcastJSON(map[string]any{"type": "agent_session_archived", "agent_session": s.agentSessionWire(ev.Agent)})
		}
		s.maybeNotifyForSessionEvent(ev)
		brainWoke = s.maybeWakeBrainForSessionEvent(ev)
	}
	s.observeDelegatedLifecycleEvent(ev, brainWoke)
}

func (s *Server) recordWorkForSessionEvent(ev watcher.SessionEvent) {
	if s.workLog == nil || ev.Agent == nil {
		return
	}
	final := ev.Type == "agent_removed" || isFinalAgentState(ev.NewState)
	force := ev.Type != "agent_output" && ev.Type != "agent_metadata_change"
	if _, err := s.workLog.RecordAgent(ev.Agent, final, force); err != nil {
		log.Printf("work log sync failed for %s: %v", ev.AgentID, err)
	}
}

func (s *Server) syncWorkLogsForAgents(force bool) {
	if s.watcher == nil {
		return
	}
	s.syncWorkLogs(visibleAgentSessions(s.watcher.Agents()), force)
}

func (s *Server) syncWorkLogs(agents []*classifier.Agent, force bool) {
	if s.workLog == nil {
		return
	}
	for _, agent := range agents {
		if agent == nil {
			continue
		}
		final := isFinalAgentState(string(agent.State))
		if _, err := s.workLog.RecordAgent(agent, final, force); err != nil {
			log.Printf("work log sync failed for %s: %v", agent.ID, err)
		}
	}
}

func (s *Server) observeDelegatedLifecycleEvent(ev watcher.SessionEvent, alreadyWokeBrain bool) {
	if s.lifecycle == nil {
		return
	}
	if ev.Type == "agent_removed" {
		s.lifecycle.Forget(ev.AgentID)
		return
	}
	if ev.Agent == nil {
		return
	}
	s.lifecycle.Observe(ev.Agent, alreadyWokeBrain)
}

func (s *Server) observeDelegatedLifecycleAgents(agents []*classifier.Agent) {
	if s.lifecycle == nil {
		return
	}
	for _, agent := range agents {
		s.lifecycle.Observe(agent, false)
	}
}

func isFinalAgentState(state string) bool {
	return state == "done" || state == "failed"
}

func isActionableBrainHeartbeatState(state string) bool {
	return state == string(classifier.StateBlocked) ||
		state == string(classifier.StateDone) ||
		state == string(classifier.StateFailed)
}

func isAttentionSignal(attention string) bool {
	switch strings.TrimSpace(attention) {
	case "user_input", "blocked", "failed":
		return true
	default:
		return false
	}
}

func (s *Server) maybeWakeBrainForSessionEvent(ev watcher.SessionEvent) bool {
	if s.brain == nil || ev.Agent == nil {
		return false
	}
	if !ev.Agent.Delegated || ev.Agent.Hidden {
		return false
	}
	status := ev.NewState
	reason := "agent_state_change"
	if ev.Type == "agent_removed" {
		return false
	} else if ev.Type == "agent_metadata_change" && ev.Agent.NeedsAttention && isAttentionSignal(ev.Agent.Attention) {
		reason = "agent_attention"
		status = string(ev.Agent.State)
	} else if ev.Type != "agent_state_change" || ev.OldState == ev.NewState || !isActionableBrainHeartbeatState(status) {
		return false
	}
	signalKey := brainSignalKey(ev.AgentID, reason, status, ev.Agent.Attention)
	if s.rememberBrainSignal(signalKey) {
		return false
	}
	woke, err := s.brain.Heartbeat(brain.HeartbeatEvent{
		Reason:    reason,
		AgentID:   ev.AgentID,
		Name:      ev.Agent.Name,
		Status:    status,
		Summary:   ev.Agent.Summary,
		Cwd:       ev.Agent.Cwd,
		Phase:     ev.Agent.Phase,
		Attention: ev.Agent.Attention,
		OldState:  ev.OldState,
		NewState:  ev.NewState,
	})
	if err != nil {
		log.Printf("brain heartbeat wake failed for %s: %v", ev.AgentID, err)
		s.forgetBrainSignal(signalKey)
		return false
	}
	if woke {
		log.Printf("brain heartbeat wake sent for %s (%s)", ev.AgentID, reason)
	} else {
		s.forgetBrainSignal(signalKey)
	}
	return woke
}

func (s *Server) maybeNotifyForSessionEvent(ev watcher.SessionEvent) {
	if s.pusher == nil || s.hasAnyActiveViewer() || ev.Agent == nil || ev.Agent.Hidden || !ev.Agent.Delegated {
		return
	}
	if ev.Type == "agent_removed" {
		return
	}
	if ev.Type != "agent_state_change" || ev.OldState == ev.NewState {
		return
	}

	state := ev.NewState
	switch state {
	case "blocked":
		if s.rememberNotificationSignal(notificationSignalKey(ev.AgentID, state)) {
			return
		}
		s.pusher.NotifyAgentBlocked(ev.AgentID, ev.Agent.Name, ev.Agent.Summary)
	case "failed":
		if s.rememberNotificationSignal(notificationSignalKey(ev.AgentID, state)) {
			return
		}
		s.pusher.NotifyAgentFailed(ev.AgentID, ev.Agent.Name, ev.Agent.Summary)
	case "done":
		if s.rememberNotificationSignal(notificationSignalKey(ev.AgentID, state)) {
			return
		}
		s.pusher.NotifyAgentDone(ev.AgentID, ev.Agent.Name, ev.Agent.Summary)
	}
}

func notificationSignalKey(agentID, state string) string {
	return strings.TrimSpace(agentID) + "|notification|" + strings.TrimSpace(state)
}

func brainSignalKey(agentID, reason, status, attention string) string {
	if reason == "agent_attention" {
		return strings.TrimSpace(agentID) + "|brain|" + reason + "|" + strings.TrimSpace(attention)
	}
	return strings.TrimSpace(agentID) + "|brain|" + strings.TrimSpace(status)
}

func (s *Server) rememberNotificationSignal(key string) bool {
	if key == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.notified == nil {
		s.notified = make(map[string]struct{})
	}
	if _, ok := s.notified[key]; ok {
		return true
	}
	s.notified[key] = struct{}{}
	return false
}

func (s *Server) rememberBrainSignal(key string) bool {
	if key == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.brainSent == nil {
		s.brainSent = make(map[string]struct{})
	}
	if _, ok := s.brainSent[key]; ok {
		return true
	}
	s.brainSent[key] = struct{}{}
	return false
}

func (s *Server) forgetBrainSignal(key string) {
	if key == "" {
		return
	}
	s.mu.Lock()
	delete(s.brainSent, key)
	s.mu.Unlock()
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
			conn.Close()
			delete(s.clients, conn)
			delete(s.active, conn)
			delete(s.writes, conn)
			s.mu.Unlock()
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
	if _, ok := s.authenticateRequest(w, r, "zen-upload"); !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	dir := "/tmp/zen-uploads"
	if err := os.MkdirAll(dir, 0755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ext := filepath.Ext(header.Filename)
	name := uuid.New().String() + ext
	path := filepath.Join(dir, name)
	dst, err := os.Create(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"path": path, "name": header.Filename})
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
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
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

	extra := map[string]interface{}{}
	for key, value := range raw {
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
		case "summary":
			if s, ok := value.(string); ok {
				fm.Summary = strings.TrimSpace(s)
			}
		case "progress":
			fm.Progress = parseStringList(value)
		case "outcome":
			if s, ok := value.(string); ok {
				fm.Outcome = strings.TrimSpace(s)
			}
		case "friction":
			if s, ok := value.(string); ok {
				fm.Friction = strings.TrimSpace(s)
			}
		case "cause":
			if s, ok := value.(string); ok {
				fm.Cause = strings.TrimSpace(s)
			}
		case "insight":
			if s, ok := value.(string); ok {
				fm.Insight = strings.TrimSpace(s)
			}
		case "next":
			if s, ok := value.(string); ok {
				fm.Next = strings.TrimSpace(s)
			}
		case "agent_source":
			if s, ok := value.(string); ok {
				fm.AgentSource = strings.TrimSpace(s)
			}
		case "agent_session":
			if s, ok := value.(string); ok {
				fm.AgentSession = s
			}
		case "cwd":
			if s, ok := value.(string); ok {
				fm.Cwd = strings.TrimSpace(s)
			}
		case "command":
			if s, ok := value.(string); ok {
				fm.Command = strings.TrimSpace(s)
			}
		case "ai_provider":
			if s, ok := value.(string); ok {
				fm.AIProvider = strings.TrimSpace(s)
			}
		case "ai_updated":
			if parsed, ok := parseRFC3339Value(value); ok {
				fm.AIUpdated = &parsed
			} else {
				fm.AIUpdated = nil
			}
		case "ai_hash":
			if s, ok := value.(string); ok {
				fm.AIHash = strings.TrimSpace(s)
			}
		case "ai_error":
			if s, ok := value.(string); ok {
				fm.AIError = strings.TrimSpace(s)
			}
		case "extra":
			if nested, ok := value.(map[string]interface{}); ok {
				for nestedKey, nestedValue := range nested {
					extra[nestedKey] = nestedValue
				}
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

func parseStringList(value interface{}) []string {
	raw, ok := value.([]interface{})
	if !ok {
		if typed, ok := value.([]string); ok {
			return typed
		}
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
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

func (s *Server) hasAnyActiveViewer() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, activeAgentID := range s.active {
		if activeAgentID != "" {
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
