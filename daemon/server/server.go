package server

import (
	"context"
	"encoding/json"
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
	"github.com/daoleno/zen/daemon/push"
	"github.com/daoleno/zen/daemon/stats"
	"github.com/daoleno/zen/daemon/task"
	"github.com/daoleno/zen/daemon/terminal"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/gorilla/websocket"
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
	tasks    *task.Store
	skills   *task.SkillStore
	guidance *task.GuidanceStore
	projects *task.ProjectStore
	clients  map[*websocket.Conn]bool
	active   map[*websocket.Conn]string
	writes   map[*websocket.Conn]*sync.Mutex
	mu       sync.Mutex
}

// New creates a WebSocket server.
func New(authManager *auth.Manager, w *watcher.Watcher, pusher *push.Client, sc *stats.Collector, ts *task.Store, ss *task.SkillStore, gs *task.GuidanceStore, ps *task.ProjectStore) *Server {
	return &Server{
		auth:     authManager,
		watcher:  w,
		terminal: terminal.NewManager(&terminal.TmuxBackend{}),
		pusher:   pusher,
		stats:    sc,
		tasks:    ts,
		skills:   ss,
		guidance: gs,
		projects: ps,
		clients:  make(map[*websocket.Conn]bool),
		active:   make(map[*websocket.Conn]string),
		writes:   make(map[*websocket.Conn]*sync.Mutex),
	}
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
	s.sendAgentList(conn)

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
	var raw struct {
		Type         string `json:"type"`
		RequestID    string `json:"request_id"`
		AgentID      string `json:"agent_id"`
		TargetID     string `json:"target_id"`
		Cwd          string `json:"cwd"`
		Command      string `json:"command"`
		Name         string `json:"name"`
		Backend      string `json:"backend"`
		SessionID    string `json:"session_id"`
		Text         string `json:"text"`
		Data         string `json:"data"`
		Action       string `json:"action"`
		StateVersion int64  `json:"state_version"`
		PushToken    string `json:"push_token"`
		ServerRef    string `json:"server_ref"`
		Cols         int    `json:"cols"`
		Rows         int    `json:"rows"`
		Lines        int    `json:"lines"`
		TaskID       string `json:"task_id"`
		Title        string `json:"title"`
		Description  string `json:"description"`
		SkillID      string `json:"skill_id"`
		TaskStatus   string `json:"task_status"`
		Icon         string `json:"icon"`
		AgentCmd     string `json:"agent_cmd"`
		Prompt       string `json:"prompt"`
		Priority     int      `json:"priority"`
		Labels       []string `json:"labels"`
		ProjectID    string   `json:"project_id"`
		ProjectName  string   `json:"project_name"`
		ProjectIcon  string   `json:"project_icon"`
		Preamble     string   `json:"preamble"`
		Constraints  []string `json:"constraints"`
	}
	if err := json.Unmarshal(msg, &raw); err != nil {
		log.Printf("invalid message: %v", err)
		return
	}

	switch raw.Type {
	case "list_agents":
		s.sendAgentList(conn)

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

	// ── Task CRUD ──────────────────────────────────────────

	case "list_tasks":
		s.sendJSON(conn, map[string]any{"type": "task_list", "tasks": s.tasks.List()})

	case "create_task":
		t, err := s.tasks.Create(raw.Title, raw.Description, raw.SkillID, raw.Cwd, raw.Priority, raw.Labels, raw.ProjectID)
		if err != nil {
			s.sendError(conn, "create_task_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{"type": "task_created", "request_id": raw.RequestID, "task": t})

	case "update_task":
		t, err := s.tasks.Update(raw.TaskID, func(t *task.Task) {
			if raw.Title != "" {
				t.Title = raw.Title
			}
			if raw.Description != "" {
				t.Description = raw.Description
			}
			if raw.TaskStatus != "" {
				t.Status = task.TaskStatus(raw.TaskStatus)
			}
			if raw.Priority > 0 || raw.TaskStatus != "" {
				t.Priority = raw.Priority
			}
			if raw.Labels != nil {
				t.Labels = raw.Labels
			}
			if raw.ProjectID != "" {
				t.ProjectID = raw.ProjectID
			}
		})
		if err != nil {
			s.sendError(conn, "update_task_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{"type": "task_updated", "request_id": raw.RequestID, "task": t})

	case "delete_task":
		if err := s.tasks.Delete(raw.TaskID); err != nil {
			s.sendError(conn, "delete_task_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{"type": "task_deleted", "request_id": raw.RequestID, "task_id": raw.TaskID})

	case "delegate_task":
		t := s.tasks.Get(raw.TaskID)
		if t == nil {
			s.sendError(conn, "delegate_task_failed", "task not found")
			return
		}

		// Build prompt from task + optional skill + guidance
		prompt := s.guidance.BuildPromptPrefix()
		if t.SkillID != "" {
			if sk := s.skills.Get(t.SkillID); sk != nil {
				prompt += sk.Prompt + "\n\n"
			}
		}
		if t.Description != "" {
			prompt += t.Description
		} else {
			prompt += t.Title
		}

		cmd := "claude"
		if t.SkillID != "" {
			if sk := s.skills.Get(t.SkillID); sk != nil && sk.AgentCmd != "" {
				cmd = sk.AgentCmd
			}
		}

		agentID, err := s.watcher.CreateSession("", watcher.CreateSessionOptions{
			Cwd:     t.Cwd,
			Command: cmd + " " + shellQuoteSimple(prompt),
			Name:    t.Title,
		})
		if err != nil {
			s.sendError(conn, "delegate_task_failed", err.Error())
			return
		}

		updated, err := s.tasks.Update(t.ID, func(t *task.Task) {
			t.AgentID = agentID
			t.Status = task.StatusInProgress
			t.AgentStatus = "running"
		})
		if err != nil {
			s.sendError(conn, "delegate_task_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{
			"type":       "task_delegated",
			"request_id": raw.RequestID,
			"task":       updated,
			"agent_id":   agentID,
		})

	// ── Skills CRUD ────────────────────────────────────────

	case "list_skills":
		s.sendJSON(conn, map[string]any{"type": "skill_list", "skills": s.skills.List()})

	case "create_skill":
		sk, err := s.skills.Create(raw.Name, raw.Icon, raw.AgentCmd, raw.Prompt, raw.Cwd)
		if err != nil {
			s.sendError(conn, "create_skill_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{"type": "skill_created", "request_id": raw.RequestID, "skill": sk})

	case "update_skill":
		sk, err := s.skills.Update(raw.SkillID, func(sk *task.Skill) {
			if raw.Name != "" {
				sk.Name = raw.Name
			}
			if raw.Icon != "" {
				sk.Icon = raw.Icon
			}
			if raw.AgentCmd != "" {
				sk.AgentCmd = raw.AgentCmd
			}
			if raw.Prompt != "" {
				sk.Prompt = raw.Prompt
			}
			if raw.Cwd != "" {
				sk.Cwd = raw.Cwd
			}
		})
		if err != nil {
			s.sendError(conn, "update_skill_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{"type": "skill_updated", "request_id": raw.RequestID, "skill": sk})

	case "delete_skill":
		if err := s.skills.Delete(raw.SkillID); err != nil {
			s.sendError(conn, "delete_skill_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{"type": "skill_deleted", "request_id": raw.RequestID, "skill_id": raw.SkillID})

	// ── Guidance ───────────────────────────────────────────

	case "get_guidance":
		g := s.guidance.Get()
		s.sendJSON(conn, map[string]any{"type": "guidance", "guidance": g})

	case "set_guidance":
		g, err := s.guidance.Set(raw.Preamble, raw.Constraints)
		if err != nil {
			s.sendError(conn, "set_guidance_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{"type": "guidance_updated", "request_id": raw.RequestID, "guidance": g})

	// ── Projects CRUD ──────────────────────────────────────

	case "list_projects":
		s.sendJSON(conn, map[string]any{"type": "project_list", "projects": s.projects.List()})

	case "create_project":
		p, err := s.projects.Create(raw.ProjectName, raw.ProjectIcon)
		if err != nil {
			s.sendError(conn, "create_project_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{"type": "project_created", "request_id": raw.RequestID, "project": p})

	case "delete_project":
		if err := s.projects.Delete(raw.ProjectID); err != nil {
			s.sendError(conn, "delete_project_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{"type": "project_deleted", "request_id": raw.RequestID, "project_id": raw.ProjectID})

	default:
		log.Printf("unknown message type: %s", raw.Type)
	}
}

func (s *Server) sendAgentList(conn *websocket.Conn) {
	agents := s.watcher.Agents()
	s.sendJSON(conn, map[string]any{"type": "agent_list", "agents": agents})
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

func (s *Server) broadcastEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-s.watcher.Events():
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			s.broadcast(data)

			// Send push notifications for state changes.
			if ev.Type == "agent_state_change" {
				agent := s.watcher.GetAgent(ev.AgentID)
				if agent == nil {
					continue
				}

				// Auto-track: sync issue status from agent state.
				if linked := s.tasks.FindByAgentID(ev.AgentID); linked != nil {
					switch ev.NewState {
					case "blocked":
						// Issue stays in_progress, just update agent_status
						s.tasks.Update(linked.ID, func(t *task.Task) {
							t.AgentStatus = "blocked"
						})
					case "done":
						s.tasks.Update(linked.ID, func(t *task.Task) {
							t.Status = task.StatusDone
							t.AgentStatus = "done"
						})
					case "failed":
						// Agent failed → issue goes back to todo for retry
						s.tasks.Update(linked.ID, func(t *task.Task) {
							t.Status = task.StatusTodo
							t.AgentID = ""
							t.AgentStatus = ""
						})
					case "running":
						s.tasks.Update(linked.ID, func(t *task.Task) {
							t.AgentStatus = "running"
						})
					}
				}

				if s.hasAnyActiveViewer() {
					continue
				}
				switch ev.NewState {
				case "blocked":
					s.pusher.NotifyAgentBlocked(ev.AgentID, agent.Name, agent.Summary)
				case "failed":
					s.pusher.NotifyAgentFailed(ev.AgentID, agent.Name, agent.Summary)
				case "done":
					s.pusher.NotifyAgentDone(ev.AgentID, agent.Name, agent.Summary)
				}
			}
		case te := <-s.tasks.Events():
			data, err := json.Marshal(te)
			if err != nil {
				continue
			}
			s.broadcast(data)
		}
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
			agents := s.watcher.Agents()
			data, _ := json.Marshal(map[string]any{"type": "agent_list", "agents": agents})
			s.broadcast(data)
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
			conn.Close()
			delete(s.clients, conn)
			delete(s.active, conn)
			delete(s.writes, conn)
			s.mu.Unlock()
		}
	}
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

// shellQuoteSimple wraps a string in single quotes for safe shell injection.
func shellQuoteSimple(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
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
