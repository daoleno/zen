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
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/push"
	"github.com/daoleno/zen/daemon/terminal"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Server handles WebSocket connections from the zen mobile app.
type Server struct {
	secret   *auth.Secret
	watcher  *watcher.Watcher
	terminal *terminal.Manager
	pusher   *push.Client
	clients  map[*websocket.Conn]bool
	active   map[*websocket.Conn]string
	writes   map[*websocket.Conn]*sync.Mutex
	mu       sync.Mutex
}

// New creates a WebSocket server.
func New(secret *auth.Secret, w *watcher.Watcher, pusher *push.Client) *Server {
	return &Server{
		secret:   secret,
		watcher:  w,
		terminal: terminal.NewManager(&terminal.TmuxBackend{}),
		pusher:   pusher,
		clients:  make(map[*websocket.Conn]bool),
		active:   make(map[*websocket.Conn]string),
		writes:   make(map[*websocket.Conn]*sync.Mutex),
	}
}

// Run starts the HTTP server and event broadcaster.
func (s *Server) Run(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/upload", s.handleUpload)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
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
	if s.secret != nil {
		token := r.Header.Get("Authorization")
		if !s.secret.VerifyAuthorization(token, []byte("zen-connect"), 5*time.Minute) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
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

	default:
		log.Printf("unknown message type: %s", raw.Type)
	}
}

func (s *Server) sendAgentList(conn *websocket.Conn) {
	agents := s.watcher.Agents()
	s.sendJSON(conn, map[string]any{"type": "agent_list", "agents": agents})
}

func (s *Server) sendJSON(conn *websocket.Conn, v any) {
	data, _ := json.Marshal(v)
	s.writeMessage(conn, websocket.TextMessage, data)
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
	if s.secret != nil {
		token := r.Header.Get("Authorization")
		if !s.secret.VerifyAuthorization(token, []byte("zen-upload"), 5*time.Minute) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
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

func clientID(conn *websocket.Conn) string {
	return fmt.Sprintf("%p", conn)
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
