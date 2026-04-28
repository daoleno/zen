package server

import (
	"context"
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
	"github.com/daoleno/zen/daemon/issue"
	"github.com/daoleno/zen/daemon/push"
	"github.com/daoleno/zen/daemon/stats"
	"github.com/daoleno/zen/daemon/terminal"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/gorilla/websocket"
	"github.com/oklog/ulid/v2"
)

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
	issues   *issue.Store
	dispatch *issue.Dispatcher
	execs    *issue.ExecutorConfig

	issueSubID int
	issueSub   <-chan issue.Event

	clients map[*websocket.Conn]bool
	active  map[*websocket.Conn]string
	writes  map[*websocket.Conn]*sync.Mutex
	mu      sync.Mutex
}

// New creates a WebSocket server.
func New(authManager *auth.Manager, w *watcher.Watcher, pusher *push.Client, sc *stats.Collector, issues *issue.Store, dispatcher *issue.Dispatcher, execs *issue.ExecutorConfig) *Server {
	srv := &Server{
		auth:     authManager,
		watcher:  w,
		terminal: terminal.NewManager(&terminal.TmuxBackend{}),
		pusher:   pusher,
		stats:    sc,
		issues:   issues,
		dispatch: dispatcher,
		execs:    execs,
		clients:  make(map[*websocket.Conn]bool),
		active:   make(map[*websocket.Conn]string),
		writes:   make(map[*websocket.Conn]*sync.Mutex),
	}
	if issues != nil {
		srv.issueSubID, srv.issueSub = issues.Subscribe()
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
	if s.issues != nil {
		s.sendJSON(conn, map[string]any{
			"type":      "issues_snapshot",
			"issues":    s.issues.List(),
			"executors": s.executorRoles(),
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

	case "list_issues":
		s.handleListIssues(conn, raw)

	case "get_issue":
		s.handleGetIssue(conn, raw)

	case "write_issue":
		s.handleWriteIssue(conn, raw)

	case "send_issue":
		s.handleSendIssue(conn, raw)

	case "redispatch_issue":
		s.handleRedispatchIssue(conn, raw)

	case "delete_issue":
		s.handleDeleteIssue(conn, raw)

	case "list_executors":
		s.handleListExecutors(conn, raw)

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
			s.sendJSON(conn, resp)
		} else {
			s.sendJSON(conn, map[string]any{"type": "stats_data", "ranges": map[string]any{}})
		}

	default:
		log.Printf("unknown message type: %s", raw.Type)
	}
}

func (s *Server) sendAgentSessionList(conn *websocket.Conn) {
	agentSessions := s.watcher.Agents()
	s.sendJSON(conn, map[string]any{"type": "agent_session_list", "agent_sessions": agentSessions})
}

func (s *Server) executorRoles() []string {
	if s.execs == nil {
		return nil
	}
	return s.execs.Roles()
}

func (s *Server) handleListIssues(conn *websocket.Conn, raw clientMessage) {
	if s.issues == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "list_issues_failed", "issue store not configured")
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "issues_snapshot",
		"request_id": raw.RequestID,
		"issues":     s.issues.List(),
		"executors":  s.executorRoles(),
	})
}

func (s *Server) handleGetIssue(conn *websocket.Conn, raw clientMessage) {
	if s.issues == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "get_issue_failed", "issue store not configured")
		return
	}
	iss, ok := s.issues.GetByID(strings.TrimSpace(raw.ID))
	if !ok {
		s.sendErrorWithRequestID(conn, raw.RequestID, "get_issue_failed", "issue not found")
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "issue",
		"request_id": raw.RequestID,
		"issue":      iss,
	})
}

func (s *Server) handleListExecutors(conn *websocket.Conn, raw clientMessage) {
	s.sendJSON(conn, map[string]any{
		"type":       "executor_list",
		"request_id": raw.RequestID,
		"executors":  s.executorRoles(),
	})
}

func (s *Server) handleWriteIssue(conn *websocket.Conn, raw clientMessage) {
	if s.issues == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "write_issue_failed", "issue store not configured")
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
		root, err := issue.DefaultRoot()
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "write_issue_failed", err.Error())
			return
		}
		path = filepath.Join(root, project, buildIssueFilename(now, raw.Body, id))
	}

	frontmatter := issue.Frontmatter{
		ID:      id,
		Created: now,
	}
	if existing, ok := s.issues.GetByID(id); ok {
		frontmatter = issue.Frontmatter{
			ID:           existing.Frontmatter.ID,
			Created:      existing.Frontmatter.Created,
			Done:         existing.Frontmatter.Done,
			Dispatched:   existing.Frontmatter.Dispatched,
			AgentSession: existing.Frontmatter.AgentSession,
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

	written, err := s.issues.Write(&issue.Issue{
		ID:          frontmatter.ID,
		Path:        path,
		Project:     project,
		Body:        raw.Body,
		Frontmatter: frontmatter,
	}, baseMtime)
	if err != nil {
		if errors.Is(err, issue.ErrConflict) {
			current, _ := s.issues.GetByID(id)
			s.sendJSON(conn, map[string]any{
				"type":       "error",
				"request_id": raw.RequestID,
				"code":       "conflict",
				"message":    "issue changed on disk",
				"current":    current,
			})
			return
		}
		s.sendErrorWithRequestID(conn, raw.RequestID, "write_issue_failed", err.Error())
		return
	}

	s.sendJSON(conn, map[string]any{
		"type":       "issue_written",
		"request_id": raw.RequestID,
		"issue":      written,
	})
}

func (s *Server) handleSendIssue(conn *websocket.Conn, raw clientMessage) {
	s.handleDispatchIssue(conn, raw, false)
}

func (s *Server) handleRedispatchIssue(conn *websocket.Conn, raw clientMessage) {
	s.handleDispatchIssue(conn, raw, true)
}

func (s *Server) handleDispatchIssue(conn *websocket.Conn, raw clientMessage, redispatch bool) {
	if s.issues == nil || s.dispatch == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "send_issue_failed", "issue dispatch not configured")
		return
	}

	iss, ok := s.issues.GetByID(strings.TrimSpace(raw.ID))
	if !ok {
		s.sendErrorWithRequestID(conn, raw.RequestID, "send_issue_failed", "issue not found")
		return
	}
	project, err := issue.LoadProject(filepath.Dir(iss.Path))
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "send_issue_failed", err.Error())
		return
	}

	var updated *issue.Issue
	if redispatch {
		updated, err = s.dispatch.Redispatch(iss, project)
	} else {
		updated, err = s.dispatch.Dispatch(iss, project)
	}
	if err != nil {
		code := "send_issue_failed"
		if redispatch {
			code = "redispatch_issue_failed"
		}
		s.sendErrorWithRequestID(conn, raw.RequestID, code, err.Error())
		return
	}

	written, err := s.issues.Write(updated, time.Time{})
	if err != nil {
		code := "send_issue_failed"
		if redispatch {
			code = "redispatch_issue_failed"
		}
		s.sendErrorWithRequestID(conn, raw.RequestID, code, err.Error())
		return
	}

	msgType := "issue_dispatched"
	if redispatch {
		msgType = "issue_redispatched"
	}
	s.sendJSON(conn, map[string]any{
		"type":       msgType,
		"request_id": raw.RequestID,
		"issue":      written,
	})
}

func (s *Server) handleDeleteIssue(conn *websocket.Conn, raw clientMessage) {
	if s.issues == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "delete_issue_failed", "issue store not configured")
		return
	}
	if err := s.issues.Delete(strings.TrimSpace(raw.ID)); err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "delete_issue_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "issue_deleted_ack",
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
		case ie, ok := <-s.issueSub:
			if !ok {
				s.issueSub = nil
				continue
			}
			s.handleIssueEvent(ie)
		}
	}
}

func (s *Server) handleIssueEvent(ev issue.Event) {
	switch ev.Type {
	case issue.EventChanged:
		s.broadcastJSON(map[string]any{
			"type":  "issue_changed",
			"path":  ev.Path,
			"id":    ev.ID,
			"issue": ev.Issue,
		})
	case issue.EventDeleted:
		s.broadcastJSON(map[string]any{
			"type": "issue_deleted",
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
			data, _ := json.Marshal(map[string]any{"type": "agent_session_list", "agent_sessions": agentSessions})
			s.broadcast(data)
		}
	}
}

func (s *Server) handleWatcherEvent(ev watcher.SessionEvent) {
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

func applyFrontmatterOverrides(fm *issue.Frontmatter, raw map[string]interface{}) {
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
		case "dispatched":
			if parsed, ok := parseRFC3339Value(value); ok {
				fm.Dispatched = &parsed
			} else {
				fm.Dispatched = nil
			}
		case "agent_session":
			if s, ok := value.(string); ok {
				fm.AgentSession = s
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

func buildIssueFilename(now time.Time, body, fallbackID string) string {
	return now.Format("2006-01-02") + "-" + slugifyIssueTitle(firstLine(body), fallbackID) + ".md"
}

func slugifyIssueTitle(line, fallback string) string {
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
