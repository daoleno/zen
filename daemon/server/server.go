package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/push"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Server handles WebSocket connections from the zen mobile app.
type Server struct {
	secret  *auth.Secret
	watcher *watcher.Watcher
	pusher  *push.Client
	clients map[*websocket.Conn]bool
	mu      sync.Mutex
}

// New creates a WebSocket server.
func New(secret *auth.Secret, w *watcher.Watcher, pusher *push.Client) *Server {
	return &Server{
		secret:  secret,
		watcher: w,
		pusher:  pusher,
		clients: make(map[*websocket.Conn]bool),
	}
}

// Run starts the HTTP server and event broadcaster.
func (s *Server) Run(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
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
	token := r.Header.Get("Authorization")
	if token != "" {
		if !s.secret.Verify(token, []byte("zen-connect"), 5*time.Minute) {
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

	s.mu.Lock()
	s.clients[conn] = true
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
	s.mu.Unlock()
	log.Printf("client disconnected (%d remaining)", len(s.clients))
}

func (s *Server) handleClientMessage(conn *websocket.Conn, msg []byte) {
	var raw struct {
		Type         string `json:"type"`
		AgentID      string `json:"agent_id"`
		Text         string `json:"text"`
		Action       string `json:"action"`
		StateVersion int64  `json:"state_version"`
		PushToken    string `json:"push_token"`
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
			s.pusher.SetToken(raw.PushToken)
			s.sendJSON(conn, map[string]any{"type": "push_registered", "ok": true})
		}

	case "send_input":
		if err := s.watcher.SendInput(raw.AgentID, raw.Text); err != nil {
			log.Printf("send_input error: %v", err)
			s.sendError(conn, "send_input_failed", err.Error())
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
	conn.WriteMessage(websocket.TextMessage, data)
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
				switch ev.NewState {
				case "blocked":
					s.pusher.NotifyAgentBlocked(ev.AgentID, agent.Name, agent.Summary)
				case "failed":
					s.pusher.NotifyAgentFailed(ev.AgentID, agent.Name, agent.Summary)
				case "done":
					s.pusher.NotifyAgentDone(ev.AgentID, agent.Name)
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
	defer s.mu.Unlock()
	for conn := range s.clients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			conn.Close()
			delete(s.clients, conn)
		}
	}
}

// PairingInfo returns connection information for display/QR code.
func (s *Server) PairingInfo(addr string) string {
	return fmt.Sprintf(`{"url":"ws://%s/ws","code":"%s","secret":"%s"}`,
		addr, s.secret.PairingCode(), s.secret.Hex())
}
