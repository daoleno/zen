package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/push"
	"github.com/daoleno/zen/daemon/stats"
	"github.com/daoleno/zen/daemon/terminal"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
	"github.com/gorilla/websocket"
	"github.com/oklog/ulid/v2"
)

const maxCodexAssetBytes = 6 << 20

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Server handles WebSocket connections from the zen mobile app.
type Server struct {
	auth     *auth.Manager
	watcher  *watcher.Watcher
	terminal *terminal.Manager
	pusher   *push.Client
	stats    *stats.Collector
	work     *work.Store
	launcher *work.Launcher
	workLog  *work.SessionLogger
	execs    *work.ExecutorConfig

	workSubID int
	workSub   <-chan work.Event

	clients map[*websocket.Conn]bool
	active  map[*websocket.Conn]string
	writes  map[*websocket.Conn]*sync.Mutex
	mu      sync.Mutex
}

// New creates a WebSocket server.
func New(authManager *auth.Manager, w *watcher.Watcher, pusher *push.Client, sc *stats.Collector, workStore *work.Store, launcher *work.Launcher, execs *work.ExecutorConfig) *Server {
	srv := &Server{
		auth:     authManager,
		watcher:  w,
		terminal: terminal.NewManager(&terminal.TmuxBackend{}),
		pusher:   pusher,
		stats:    sc,
		work:     workStore,
		launcher: launcher,
		execs:    execs,
		clients:  make(map[*websocket.Conn]bool),
		active:   make(map[*websocket.Conn]string),
		writes:   make(map[*websocket.Conn]*sync.Mutex),
	}
	if workStore != nil {
		srv.workSubID, srv.workSub = workStore.Subscribe()
		srv.workLog = work.NewSessionLogger(workStore, work.NewAgentCLIDigestProvider(execs))
	}
	return srv
}

type clientMessage struct {
	Type         string                 `json:"type"`
	RequestID    string                 `json:"request_id"`
	AgentID      string                 `json:"agent_id"`
	TargetID     string                 `json:"target_id"`
	Cwd          string                 `json:"cwd"`
	Command      string                 `json:"command"`
	Name         string                 `json:"name"`
	StartedAt    json.RawMessage        `json:"started_at"`
	Backend      string                 `json:"backend"`
	SessionID    string                 `json:"session_id"`
	Text         string                 `json:"text"`
	Data         string                 `json:"data"`
	Body         string                 `json:"body"`
	Action       string                 `json:"action"`
	StateVersion int64                  `json:"state_version"`
	PushToken    string                 `json:"push_token"`
	ServerRef    string                 `json:"server_ref"`
	Cols         int                    `json:"cols"`
	Rows         int                    `json:"rows"`
	Col          int                    `json:"col"`
	Row          int                    `json:"row"`
	Lines        int                    `json:"lines"`
	Path         string                 `json:"path"`
	ID           string                 `json:"id"`
	Project      string                 `json:"project"`
	Frontmatter  map[string]interface{} `json:"frontmatter"`
	BaseMtime    string                 `json:"base_mtime"`
	Prompt       string                 `json:"prompt"`
}

// Run starts the HTTP server and event broadcaster.
func (s *Server) Run(ctx context.Context, addr string) error {
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

	srv := &http.Server{Addr: addr, Handler: mux}

	go s.broadcastEvents(ctx)
	go s.heartbeat(ctx)

	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()

	log.Printf("zen-daemon listening on %s", addr)
	return srv.ListenAndServe()
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

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		s.handleClientMessage(conn, msg)
	}

	s.mu.Lock()
	delete(s.clients, conn)
	delete(s.active, conn)
	delete(s.writes, conn)
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

	case "list_executors":
		s.handleListExecutors(conn, raw)

	case "set_work_digest_provider":
		s.handleSetWorkDigestProvider(conn, raw)

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
		if err := s.watcher.SendInput(raw.AgentID, raw.Text); err != nil {
			log.Printf("send_input error: %v", err)
			s.sendError(conn, "send_input_failed", err.Error())
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
		s.sendJSON(conn, map[string]any{
			"type":       "session_created",
			"request_id": raw.RequestID,
			"agent_id":   agentID,
		})

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

	case "codex_conversation":
		s.handleCodexConversation(conn, raw)

	case "codex_slash_commands":
		s.handleCodexSlashCommands(conn, raw)

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
				s.sendError(conn, "stale_action",
					fmt.Sprintf("Agent state changed (version %d → %d). Refresh before acting.", raw.StateVersion, agent.StateVersion))
				return
			}
		}
		if err := s.watcher.SendAction(raw.AgentID, raw.Action); err != nil {
			log.Printf("send_action error: %v", err)
			s.sendError(conn, "send_action_failed", err.Error())
		} else {
			s.sendJSON(conn, map[string]any{"type": "action_confirmed", "agent_id": raw.AgentID, "action": raw.Action})
		}

	case "kill_agent":
		if err := s.watcher.KillSession(raw.AgentID); err != nil {
			log.Printf("kill_agent error: %v", err)
			s.sendError(conn, "kill_failed", err.Error())
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
			s.sendJSON(conn, map[string]any{
				"type":       "stats_data",
				"request_id": raw.RequestID,
				"ranges":     resp.Ranges,
			})
		} else {
			s.sendJSON(conn, map[string]any{
				"type":       "stats_data",
				"request_id": raw.RequestID,
				"ranges":     map[string]any{},
			})
		}

	default:
		log.Printf("unknown message type: %s", raw.Type)
	}
}

func (s *Server) sendAgentSessionList(conn *websocket.Conn) {
	agentSessions := s.watcher.Agents()
	s.sendJSON(conn, map[string]any{"type": "agent_session_list", "agent_sessions": agentSessions})
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

func (s *Server) handleCodexConversation(conn *websocket.Conn, raw clientMessage) {
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
			s.sendJSON(conn, map[string]any{
				"type":       "codex_conversation",
				"request_id": raw.RequestID,
				"agent_id":   targetID,
				"conversation": work.CodexConversation{
					Available: false,
					Reason:    "session_not_ready",
					Events:    []work.CodexConversationEvent{},
				},
			})
			return
		}
		agent = classifier.Agent{
			ID:        targetID,
			Name:      raw.Name,
			Cwd:       raw.Cwd,
			Command:   raw.Command,
			StartedAt: startedAt,
		}
	}
	if !startedAt.IsZero() && (!agentFromWatcher || agent.StartedAt.IsZero()) {
		agent.StartedAt = startedAt
	}
	if agent.ID == "" && strings.TrimSpace(agent.Cwd) == "" {
		s.sendJSON(conn, map[string]any{
			"type":       "codex_conversation",
			"request_id": raw.RequestID,
			"agent_id":   targetID,
			"conversation": work.CodexConversation{
				Available: false,
				Reason:    "agent_not_found",
				Events:    []work.CodexConversationEvent{},
			},
		})
		return
	}

	conversation, err := work.LoadCodexConversationForAgent(agent, time.Now())
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "codex_conversation_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":         "codex_conversation",
		"request_id":   raw.RequestID,
		"agent_id":     targetID,
		"conversation": conversation,
	})
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
			agentSessions := s.watcher.Agents()
			s.syncWorkLogs(agentSessions, false)
			data, _ := json.Marshal(map[string]any{"type": "agent_session_list", "agent_sessions": agentSessions})
			s.broadcast(data)
		}
	}
}

func (s *Server) handleWatcherEvent(ev watcher.SessionEvent) {
	s.recordWorkForSessionEvent(ev)

	switch ev.Type {
	case "agent_discovered":
		if ev.Agent != nil {
			s.broadcastJSON(map[string]any{"type": "agent_session_created", "agent_session": ev.Agent})
		}
	case "agent_output":
		if ev.Agent != nil {
			s.broadcastJSON(map[string]any{"type": "agent_session_updated", "agent_session": ev.Agent})
		}
	case "agent_state_change":
		if ev.Agent != nil {
			s.broadcastJSON(map[string]any{"type": "agent_session_updated", "agent_session": ev.Agent})
		}
		s.maybeNotifyForSessionEvent(ev)
	case "agent_removed":
		if ev.Agent != nil {
			s.broadcastJSON(map[string]any{"type": "agent_session_archived", "agent_session": ev.Agent})
		}
		s.maybeNotifyForSessionEvent(ev)
	}
}

func (s *Server) recordWorkForSessionEvent(ev watcher.SessionEvent) {
	if s.workLog == nil || ev.Agent == nil {
		return
	}
	final := ev.Type == "agent_removed" || isFinalAgentState(ev.NewState)
	force := ev.Type != "agent_output"
	if _, err := s.workLog.RecordAgent(ev.Agent, final, force); err != nil {
		log.Printf("work log sync failed for %s: %v", ev.AgentID, err)
	}
}

func (s *Server) syncWorkLogsForAgents(force bool) {
	if s.watcher == nil {
		return
	}
	s.syncWorkLogs(s.watcher.Agents(), force)
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

func isFinalAgentState(state string) bool {
	return state == "done" || state == "failed"
}

func (s *Server) maybeNotifyForSessionEvent(ev watcher.SessionEvent) {
	if s.hasAnyActiveViewer() || ev.Agent == nil {
		return
	}

	state := ev.NewState
	if ev.Type == "agent_removed" {
		state = ev.OldState
	}

	switch state {
	case "blocked":
		s.pusher.NotifyAgentBlocked(ev.AgentID, ev.Agent.Name, ev.Agent.Summary)
	case "failed":
		s.pusher.NotifyAgentFailed(ev.AgentID, ev.Agent.Name, ev.Agent.Summary)
	case "done":
		s.pusher.NotifyAgentDone(ev.AgentID, ev.Agent.Name, ev.Agent.Summary)
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
	device, err := s.auth.VerifyAuthorization(r.Header.Get("Authorization"), purpose, 5*time.Minute)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return nil, false
	}
	return device, true
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
