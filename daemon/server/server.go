package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
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

const (
	taskStateMachineStatusStart = "<!-- ZEN:STATUS START -->"
	taskStateMachineStatusEnd   = "<!-- ZEN:STATUS END -->"
	workspaceStateDirName       = ".zen"
	workspaceTaskStateFileName  = "task.md"
	workspaceWorktreesDirName   = "worktrees"
)

// Server handles WebSocket connections from the zen mobile app.
type Server struct {
	auth     *auth.Manager
	watcher  *watcher.Watcher
	terminal *terminal.Manager
	pusher   *push.Client
	stats    *stats.Collector
	tasks    *task.Store
	runs     *task.RunStore
	guidance *task.GuidanceStore
	projects *task.ProjectStore
	clients  map[*websocket.Conn]bool
	active   map[*websocket.Conn]string
	writes   map[*websocket.Conn]*sync.Mutex
	mu       sync.Mutex
}

// New creates a WebSocket server.
func New(authManager *auth.Manager, w *watcher.Watcher, pusher *push.Client, sc *stats.Collector, ts *task.Store, rs *task.RunStore, gs *task.GuidanceStore, ps *task.ProjectStore) *Server {
	return &Server{
		auth:     authManager,
		watcher:  w,
		terminal: terminal.NewManager(&terminal.TmuxBackend{}),
		pusher:   pusher,
		stats:    sc,
		tasks:    ts,
		runs:     rs,
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
	s.sendAgentSessionList(conn)

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
		Type           string            `json:"type"`
		RequestID      string            `json:"request_id"`
		AgentID        string            `json:"agent_id"`
		TargetID       string            `json:"target_id"`
		Cwd            string            `json:"cwd"`
		Command        string            `json:"command"`
		Name           string            `json:"name"`
		Backend        string            `json:"backend"`
		SessionID      string            `json:"session_id"`
		Text           string            `json:"text"`
		Data           string            `json:"data"`
		Body           string            `json:"body"`
		Action         string            `json:"action"`
		StateVersion   int64             `json:"state_version"`
		PushToken      string            `json:"push_token"`
		ServerRef      string            `json:"server_ref"`
		Cols           int               `json:"cols"`
		Rows           int               `json:"rows"`
		Col            int               `json:"col"`
		Row            int               `json:"row"`
		Lines          int               `json:"lines"`
		Path           string            `json:"path"`
		TaskID         string            `json:"task_id"`
		RunID          string            `json:"run_id"`
		Title          string            `json:"title"`
		Description    string            `json:"description"`
		TaskStatus     string            `json:"task_status"`
		ExecutionMode  string            `json:"execution_mode"`
		DeliveryMode   string            `json:"delivery_mode"`
		AgentSessionID string            `json:"agent_session_id"`
		Icon           string            `json:"icon"`
		AgentCmd       string            `json:"agent_cmd"`
		Prompt         string            `json:"prompt"`
		Priority       int               `json:"priority"`
		Labels         []string          `json:"labels"`
		Attachments    []task.Attachment `json:"attachments"`
		ProjectID      string            `json:"project_id"`
		ProjectName    string            `json:"project_name"`
		ProjectKey     string            `json:"project_key"`
		ProjectIcon    string            `json:"project_icon"`
		RepoRoot       string            `json:"repo_root"`
		WorktreeRoot   string            `json:"worktree_root"`
		BaseBranch     string            `json:"base_branch"`
		Preamble       string            `json:"preamble"`
		Constraints    []string          `json:"constraints"`
	}
	if err := json.Unmarshal(msg, &raw); err != nil {
		log.Printf("invalid message: %v", err)
		return
	}

	switch raw.Type {
	case "list_agents", "list_agent_sessions":
		s.sendAgentSessionList(conn)

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

	// ── Task CRUD ──────────────────────────────────────────

	case "list_tasks":
		s.sendJSON(conn, map[string]any{"type": "task_list", "tasks": s.tasks.List()})

	case "list_runs":
		s.sendJSON(conn, map[string]any{"type": "run_list", "runs": s.runs.List()})

	case "get_task_state":
		snapshot, err := s.readTaskStateSnapshot(raw.TaskID)
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "get_task_state_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{
			"type":       "task_state",
			"request_id": raw.RequestID,
			"task_state": snapshot,
		})

	case "create_task":
		var input struct {
			RequestID   string            `json:"request_id"`
			Title       string            `json:"title"`
			Description string            `json:"description"`
			Attachments []task.Attachment `json:"attachments"`
			Cwd         string            `json:"cwd"`
			Priority    int               `json:"priority"`
			Labels      []string          `json:"labels"`
			ProjectID   string            `json:"project_id"`
			DueDate     string            `json:"due_date"`
		}
		if err := json.Unmarshal(msg, &input); err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "create_task_failed", "invalid create_task payload")
			return
		}

		t, err := s.tasks.Create(
			strings.TrimSpace(input.Title),
			input.Description,
			input.Attachments,
			input.Cwd,
			input.Priority,
			input.Labels,
			input.ProjectID,
			input.DueDate,
			s.resolveTaskIdentifierPrefix(input.ProjectID),
		)
		if err != nil {
			s.sendErrorWithRequestID(conn, input.RequestID, "create_task_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{"type": "task_created", "request_id": input.RequestID, "task": t})

	case "update_task":
		var input struct {
			RequestID   string             `json:"request_id"`
			TaskID      string             `json:"task_id"`
			Title       *string            `json:"title"`
			Description *string            `json:"description"`
			Attachments *[]task.Attachment `json:"attachments"`
			TaskStatus  *string            `json:"task_status"`
			Priority    *int               `json:"priority"`
			Labels      []string           `json:"labels"`
			ProjectID   *string            `json:"project_id"`
			DueDate     *string            `json:"due_date"`
		}
		if err := json.Unmarshal(msg, &input); err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "update_task_failed", "invalid update_task payload")
			return
		}

		normalizedDueDate := ""
		if input.DueDate != nil {
			var err error
			normalizedDueDate, err = task.NormalizeDueDate(*input.DueDate)
			if err != nil {
				s.sendErrorWithRequestID(conn, input.RequestID, "update_task_failed", err.Error())
				return
			}
		}

		t, err := s.tasks.Update(input.TaskID, func(t *task.Task) {
			if input.Title != nil {
				trimmed := strings.TrimSpace(*input.Title)
				if trimmed != "" {
					t.Title = trimmed
				}
			}
			if input.Description != nil {
				t.Description = *input.Description
			}
			if input.Attachments != nil {
				t.Attachments = append([]task.Attachment(nil), (*input.Attachments)...)
			}
			if input.TaskStatus != nil && *input.TaskStatus != "" {
				t.Status = task.TaskStatus(*input.TaskStatus)
			}
			if input.Priority != nil {
				t.Priority = *input.Priority
			}
			if input.Labels != nil {
				t.Labels = input.Labels
			}
			if input.ProjectID != nil {
				t.ProjectID = *input.ProjectID
			}
			if input.DueDate != nil {
				t.DueDate = normalizedDueDate
			}
		})
		if err != nil {
			s.sendErrorWithRequestID(conn, input.RequestID, "update_task_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{"type": "task_updated", "request_id": input.RequestID, "task": t})

	case "add_task_comment":
		var input struct {
			RequestID       string            `json:"request_id"`
			TaskID          string            `json:"task_id"`
			Body            string            `json:"body"`
			Attachments     []task.Attachment `json:"attachments"`
			ParentCommentID string            `json:"parent_comment_id"`
			DeliveryMode    string            `json:"delivery_mode"`
			AgentSessionID  string            `json:"agent_session_id"`
			AgentCmd        string            `json:"agent_cmd"`
		}
		if err := json.Unmarshal(msg, &input); err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "add_task_comment_failed", "invalid add_task_comment payload")
			return
		}

		currentTask := s.tasks.Get(input.TaskID)
		if currentTask == nil {
			s.sendErrorWithRequestID(conn, input.RequestID, "add_task_comment_failed", "task not found")
			return
		}

		body := strings.TrimSpace(input.Body)
		if body == "" {
			s.sendErrorWithRequestID(conn, input.RequestID, "add_task_comment_failed", "comment body is required")
			return
		}
		parentCommentID := strings.TrimSpace(input.ParentCommentID)
		if parentCommentID != "" && s.findTaskComment(currentTask, parentCommentID) == nil {
			s.sendErrorWithRequestID(conn, input.RequestID, "add_task_comment_failed", "parent comment not found")
			return
		}

		deliveryMode := strings.TrimSpace(input.DeliveryMode)
		if deliveryMode == "" {
			deliveryMode = "comment"
		}

		var (
			run             *task.Run
			targetLabel     string
			targetSessionID string
		)

		switch deliveryMode {
		case "comment":
			// Plain discussion comments stay on the issue and do not trigger agent work.
		case "current_run":
			currentRun, err := s.findLiveRunForTask(currentTask)
			if err != nil {
				s.sendErrorWithRequestID(conn, input.RequestID, "add_task_comment_failed", err.Error())
				return
			}
			if err := s.watcher.SendInput(currentRun.AgentSessionID, s.buildCurrentRunReplyMessage(currentTask, parentCommentID, body, input.Attachments)); err != nil {
				s.sendErrorWithRequestID(conn, input.RequestID, "add_task_comment_failed", err.Error())
				return
			}
			run = currentRun
			targetSessionID = currentRun.AgentSessionID
			targetLabel = s.agentSessionLabel(targetSessionID)
		case "spawn_new_session":
			createdRun, _, err := s.createRunForTask(
				currentTask,
				"spawn_new_session",
				"",
				s.buildSpawnRunCommentInstruction(currentTask, parentCommentID, body, input.Attachments),
				strings.TrimSpace(input.AgentCmd),
			)
			if err != nil {
				s.sendErrorWithRequestID(conn, input.RequestID, "add_task_comment_failed", err.Error())
				return
			}
			run = createdRun
			targetSessionID = createdRun.AgentSessionID
			targetLabel = s.agentSessionLabel(targetSessionID)
		case "attach_existing_session":
			agentSessionID := strings.TrimSpace(input.AgentSessionID)
			if agentSessionID == "" {
				s.sendErrorWithRequestID(conn, input.RequestID, "add_task_comment_failed", "agent session is required")
				return
			}

			createdRun, _, err := s.createRunForTask(currentTask, "attach_existing_session", agentSessionID, body, "")
			if err != nil {
				s.sendErrorWithRequestID(conn, input.RequestID, "add_task_comment_failed", err.Error())
				return
			}
			targetSessionID = createdRun.AgentSessionID
			if err := s.watcher.SendInput(targetSessionID, s.buildAttachedSessionMessage(currentTask, parentCommentID, body, input.Attachments)); err != nil {
				updatedRun, runErr := s.runs.Update(createdRun.ID, func(run *task.Run) {
					run.Status = task.RunStatusFailed
					run.LastError = err.Error()
					run.WaitingReason = ""
					run.Summary = "Failed to deliver issue context."
				})
				if runErr == nil && updatedRun != nil {
					_, _ = s.tasks.Update(currentTask.ID, func(current *task.Task) {
						current.CurrentRunID = updatedRun.ID
						current.LastRunStatus = string(updatedRun.Status)
					})
				}
				s.sendErrorWithRequestID(conn, input.RequestID, "add_task_comment_failed", err.Error())
				return
			}
			run = createdRun
			targetLabel = s.agentSessionLabel(targetSessionID)
		default:
			s.sendErrorWithRequestID(conn, input.RequestID, "add_task_comment_failed", "unknown delivery mode")
			return
		}

		comment := task.TaskComment{
			ID:             uuid.New().String(),
			Body:           body,
			Attachments:    append([]task.Attachment(nil), input.Attachments...),
			AuthorKind:     "user",
			AuthorLabel:    "You",
			ParentID:       parentCommentID,
			DeliveryMode:   deliveryMode,
			AgentSessionID: targetSessionID,
			TargetLabel:    targetLabel,
			CreatedAt:      time.Now().UTC(),
		}
		if run != nil {
			comment.RunID = run.ID
		}

		updatedTask, err := s.tasks.AddComment(currentTask.ID, comment)
		if err != nil {
			s.sendErrorWithRequestID(conn, input.RequestID, "add_task_comment_failed", err.Error())
			return
		}

		s.sendJSON(conn, map[string]any{
			"type":       "task_comment_added",
			"request_id": input.RequestID,
			"task":       updatedTask,
			"comment":    comment,
			"run":        run,
		})

	case "delete_task":
		if err := s.tasks.Delete(raw.TaskID); err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "delete_task_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{"type": "task_deleted", "request_id": raw.RequestID, "task_id": raw.TaskID})

	case "create_run", "delegate_task":
		t := s.tasks.Get(raw.TaskID)
		if t == nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "create_run_failed", "task not found")
			return
		}

		run, updatedTask, err := s.createRunForTask(t, raw.ExecutionMode, raw.AgentSessionID, "", raw.AgentCmd)
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "create_run_failed", err.Error())
			return
		}

		s.sendJSON(conn, map[string]any{
			"type":       "run_created",
			"request_id": raw.RequestID,
			"run":        run,
			"task":       updatedTask,
		})

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
		p, err := s.projects.Create(
			raw.ProjectName,
			raw.ProjectKey,
			raw.ProjectIcon,
			raw.RepoRoot,
			raw.WorktreeRoot,
			raw.BaseBranch,
		)
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "create_project_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{"type": "project_created", "request_id": raw.RequestID, "project": p})

	case "update_project":
		p, err := s.projects.Update(raw.ProjectID, func(current *task.Project) {
			if raw.ProjectName != "" {
				current.Name = raw.ProjectName
			}
			current.Icon = raw.ProjectIcon
			current.RepoRoot = raw.RepoRoot
			current.WorktreeRoot = raw.WorktreeRoot
			current.BaseBranch = raw.BaseBranch
		})
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "update_project_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{"type": "project_updated", "request_id": raw.RequestID, "project": p})

	case "delete_project":
		if _, err := s.tasks.ClearProject(raw.ProjectID); err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "delete_project_failed", err.Error())
			return
		}
		if err := s.projects.Delete(raw.ProjectID); err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "delete_project_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{"type": "project_deleted", "request_id": raw.RequestID, "project_id": raw.ProjectID})

	default:
		log.Printf("unknown message type: %s", raw.Type)
	}
}

func (s *Server) sendAgentSessionList(conn *websocket.Conn) {
	agentSessions := s.watcher.Agents()
	s.sendJSON(conn, map[string]any{"type": "agent_session_list", "agent_sessions": agentSessions})
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
		case te := <-s.tasks.Events():
			data, err := json.Marshal(te)
			if err != nil {
				continue
			}
			s.broadcast(data)
		case re := <-s.runs.Events():
			data, err := json.Marshal(re)
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
		s.syncRunAndTaskForSessionEvent(ev)
		s.maybeNotifyForSessionEvent(ev)
	case "agent_removed":
		if ev.Agent != nil {
			s.broadcastJSON(map[string]any{"type": "agent_session_archived", "agent_session": ev.Agent})
		}
		s.syncRunAndTaskForSessionEvent(ev)
		s.maybeNotifyForSessionEvent(ev)
	}
}

func (s *Server) syncRunAndTaskForSessionEvent(ev watcher.SessionEvent) {
	run := s.runs.FindActiveByAgentSessionID(ev.AgentID)
	if run == nil {
		return
	}

	nextStatus := mapWatcherEventToRunStatus(ev)
	updatedRun, err := s.runs.Update(run.ID, func(current *task.Run) {
		current.Status = nextStatus
		if ev.Agent != nil {
			current.Summary = ev.Agent.Summary
			if nextStatus == task.RunStatusBlocked {
				current.WaitingReason = ev.Agent.Summary
				current.LastError = ""
			} else if nextStatus == task.RunStatusFailed {
				current.LastError = ev.Agent.Summary
				current.WaitingReason = ""
			} else {
				current.WaitingReason = ""
				if nextStatus != task.RunStatusFailed {
					current.LastError = ""
				}
			}
		}
	})
	if err != nil {
		return
	}

	updatedTask, err := s.tasks.Update(updatedRun.TaskID, func(current *task.Task) {
		current.CurrentRunID = updatedRun.ID
		current.LastRunStatus = string(updatedRun.Status)
	})
	if err != nil {
		return
	}

	if err := s.writeTaskStateFile(updatedTask, updatedRun); err != nil {
		log.Printf("write task state file: %v", err)
	}
}

func (s *Server) maybeNotifyForSessionEvent(ev watcher.SessionEvent) {
	if s.hasAnyActiveViewer() || ev.Agent == nil {
		return
	}

	switch mapWatcherEventToRunStatus(ev) {
	case task.RunStatusBlocked:
		s.pusher.NotifyAgentBlocked(ev.AgentID, ev.Agent.Name, ev.Agent.Summary)
	case task.RunStatusFailed:
		s.pusher.NotifyAgentFailed(ev.AgentID, ev.Agent.Name, ev.Agent.Summary)
	case task.RunStatusDone:
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

func (s *Server) buildTaskPrompt(t *task.Task, workspaceCwd, note, agentCmdOverride string) (string, string, error) {
	cmd := strings.TrimSpace(agentCmdOverride)
	if cmd == "" {
		return "", "", fmt.Errorf("agent command required for spawn_new_session")
	}

	project := s.taskProject(t)
	var builder strings.Builder
	builder.WriteString(s.guidance.BuildPromptPrefix())
	builder.WriteString("You are starting a fresh issue session in a dedicated git worktree.\n\n")
	builder.WriteString("Issue:\n")
	builder.WriteString(fmt.Sprintf("- ID: %s\n", task.DisplayID(t)))
	builder.WriteString(fmt.Sprintf("- Title: %s\n", strings.TrimSpace(t.Title)))
	builder.WriteString(fmt.Sprintf("- Status: %s\n", t.Status))
	builder.WriteString(fmt.Sprintf("- Priority: %s\n", taskPriorityLabel(t.Priority)))
	if dueDate := strings.TrimSpace(t.DueDate); dueDate != "" {
		builder.WriteString(fmt.Sprintf("- Due date: %s\n", dueDate))
	}
	if len(t.Labels) > 0 {
		builder.WriteString(fmt.Sprintf("- Labels: %s\n", strings.Join(t.Labels, ", ")))
	}
	if project != nil {
		builder.WriteString(fmt.Sprintf("- Project: %s\n", project.Name))
		if repoRoot := strings.TrimSpace(project.RepoRoot); repoRoot != "" {
			builder.WriteString(fmt.Sprintf("- Repo root: %s\n", repoRoot))
		}
		if baseBranch := strings.TrimSpace(project.BaseBranch); baseBranch != "" {
			builder.WriteString(fmt.Sprintf("- Base branch: %s\n", baseBranch))
		}
		builder.WriteString(fmt.Sprintf("- Issue branch: %s\n", issueBranchName(t)))
	}
	if cwd := strings.TrimSpace(workspaceCwd); cwd != "" {
		builder.WriteString(fmt.Sprintf("- Workspace: %s\n", cwd))
	}

	goal := strings.TrimSpace(t.Description)
	if goal == "" {
		goal = strings.TrimSpace(t.Title)
	}
	builder.WriteString("\nGoal:\n")
	builder.WriteString(goal)

	if attachmentBlock := formatAttachmentsBlock("Attached files", t.Attachments); attachmentBlock != "" {
		builder.WriteString("\n\n")
		builder.WriteString(attachmentBlock)
	}
	if discussionBlock := s.formatRecentTaskDiscussion(t, 5); discussionBlock != "" {
		builder.WriteString("\n\nRecent discussion:\n")
		builder.WriteString(discussionBlock)
	}
	if trimmedNote := strings.TrimSpace(note); trimmedNote != "" {
		builder.WriteString("\n\nAdditional instruction:\n")
		builder.WriteString(trimmedNote)
	}

	builder.WriteString("\n\nWorking rules:\n")
	builder.WriteString("- Operate only inside the workspace above.\n")
	builder.WriteString("- Inspect the codebase and any referenced files or docs before making changes.\n")
	builder.WriteString("- Keep .zen/task.md updated as you work.\n")
	builder.WriteString("- Keep changes scoped to this issue and leave a concise summary when you finish.")

	return builder.String(), cmd, nil
}

func (s *Server) createRunForTask(t *task.Task, executionMode, requestedAgentSessionID, note, agentCmdOverride string) (*task.Run, *task.Task, error) {
	mode := strings.TrimSpace(executionMode)
	if mode == "" {
		mode = "spawn_new_session"
	}

	agentSessionID := strings.TrimSpace(requestedAgentSessionID)
	runStatus := task.RunStatusQueued
	workspaceCwd := strings.TrimSpace(t.Cwd)
	executorKind := ""
	promptSnapshot := ""
	var err error

	switch mode {
	case "spawn_new_session":
		workspaceCwd, err = s.prepareTaskWorkspace(t)
		if err != nil {
			return nil, nil, err
		}
		prompt, agentCmd, err := s.buildTaskPrompt(t, workspaceCwd, note, agentCmdOverride)
		if err != nil {
			return nil, nil, err
		}
		executorKind = agentCmd
		promptSnapshot = prompt
		agentSessionID, err = s.watcher.CreateSession("", watcher.CreateSessionOptions{
			Cwd:     workspaceCwd,
			Command: agentCmd + " " + shellQuoteSimple(promptSnapshot),
			Name:    t.Title,
		})
		if err != nil {
			return nil, nil, err
		}
		runStatus = task.RunStatusRunning
	case "attach_existing_session":
		agent := s.watcher.GetAgent(agentSessionID)
		if agent == nil {
			return nil, nil, fmt.Errorf("agent session not found")
		}
		if activeRun := s.runs.FindActiveByAgentSessionID(agentSessionID); activeRun != nil {
			return nil, nil, fmt.Errorf("agent session already linked to an active task")
		}
		if cwd := strings.TrimSpace(agent.Cwd); cwd != "" {
			workspaceCwd = cwd
		}
		executorKind = strings.TrimSpace(agent.Command)
		runStatus = mapAgentStateToRunStatus(string(agent.State))
	default:
		return nil, nil, fmt.Errorf("unknown execution mode")
	}

	run, err := s.runs.Create(task.CreateRunOptions{
		TaskID:         t.ID,
		Status:         runStatus,
		ExecutionMode:  mode,
		ExecutorKind:   executorKind,
		ExecutorLabel:  t.Title,
		AgentSessionID: agentSessionID,
		PromptSnapshot: promptSnapshot,
		Summary:        t.Title,
	})
	if err != nil {
		return nil, nil, err
	}

	updatedTask, err := s.tasks.Update(t.ID, func(current *task.Task) {
		current.CurrentRunID = run.ID
		current.LastRunStatus = string(run.Status)
		if workspaceCwd != "" {
			current.Cwd = workspaceCwd
		}
		if current.Status != task.StatusCancelled {
			current.Status = task.StatusInProgress
		}
	})
	if err != nil {
		return nil, nil, err
	}

	if err := s.writeTaskStateFile(updatedTask, run); err != nil {
		log.Printf("write task state file: %v", err)
	}

	return run, updatedTask, nil
}

func (s *Server) prepareTaskWorkspace(currentTask *task.Task) (string, error) {
	if currentTask == nil {
		return "", fmt.Errorf("task not found")
	}

	if cwd := strings.TrimSpace(currentTask.Cwd); cwd != "" {
		if info, err := os.Stat(cwd); err == nil && info.IsDir() {
			return cwd, nil
		}
	}

	var project *task.Project
	if currentTask.ProjectID != "" {
		project = s.projects.Get(currentTask.ProjectID)
	}
	if project == nil {
		return "", fmt.Errorf("assign this issue requires a project with a repo root")
	}

	repoRoot := strings.TrimSpace(project.RepoRoot)
	if repoRoot == "" {
		return "", fmt.Errorf("project %q needs a repo root before an agent can be assigned", project.Name)
	}

	canonicalRepoRoot, err := gitOutput(repoRoot, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("project %q repo root is not a valid git repository", project.Name)
	}
	repoRoot = canonicalRepoRoot

	worktreeRoot := strings.TrimSpace(project.WorktreeRoot)
	if worktreeRoot == "" {
		worktreeRoot = defaultWorktreeRoot(repoRoot)
	}
	baseBranch := strings.TrimSpace(project.BaseBranch)
	if baseBranch == "" {
		baseBranch, err = detectBaseBranch(repoRoot)
		if err != nil {
			return "", err
		}
	}

	worktreePath := filepath.Join(worktreeRoot, issueWorktreeDirName(currentTask))
	if err := ensureIssueWorktree(repoRoot, worktreePath, issueBranchName(currentTask), baseBranch); err != nil {
		return "", err
	}

	return worktreePath, nil
}

func defaultWorktreeRoot(repoRoot string) string {
	if storageDir, err := auth.DefaultStorageDir(); err == nil && strings.TrimSpace(storageDir) != "" {
		return filepath.Join(storageDir, workspaceWorktreesDirName, filepath.Base(repoRoot))
	}
	return filepath.Join(filepath.Dir(repoRoot), workspaceStateDirName, workspaceWorktreesDirName, filepath.Base(repoRoot))
}

func taskStateFilePath(cwd string) string {
	return filepath.Join(cwd, workspaceStateDirName, workspaceTaskStateFileName)
}

func issueBranchName(currentTask *task.Task) string {
	return fmt.Sprintf("zen/%s-%d-%s", issueIdentifierSlug(currentTask), currentTask.Number, slugPathToken(currentTask.Title, 32))
}

func issueWorktreeDirName(currentTask *task.Task) string {
	return fmt.Sprintf("%s-%d-%s", issueIdentifierSlug(currentTask), currentTask.Number, slugPathToken(currentTask.Title, 32))
}

func slugPathToken(value string, maxLen int) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, r := range normalized {
		isASCIIAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isASCIIAlphaNum {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		result = "task"
	}
	if maxLen > 0 && len(result) > maxLen {
		result = strings.Trim(result[:maxLen], "-")
	}
	if result == "" {
		return "task"
	}
	return result
}

func issueIdentifierSlug(currentTask *task.Task) string {
	if currentTask == nil {
		return strings.ToLower(task.DefaultIdentifierPrefix)
	}
	return slugPathToken(currentTask.IdentifierPrefix, 12)
}

func (s *Server) resolveTaskIdentifierPrefix(projectID string) string {
	if s.projects == nil || strings.TrimSpace(projectID) == "" {
		return task.DefaultIdentifierPrefix
	}

	project := s.projects.Get(projectID)
	if project == nil {
		return task.DefaultIdentifierPrefix
	}

	return project.Key
}

func ensureIssueWorktree(repoRoot, worktreePath, branchName, baseBranch string) error {
	if info, err := os.Stat(worktreePath); err == nil && info.IsDir() {
		if _, err := gitOutput(worktreePath, "rev-parse", "--show-toplevel"); err == nil {
			return nil
		}
		return fmt.Errorf("worktree path exists but is not a git worktree: %s", worktreePath)
	}

	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return fmt.Errorf("create worktree parent: %w", err)
	}

	args := []string{"worktree", "add"}
	if gitRefExists(repoRoot, "refs/heads/"+branchName) {
		args = append(args, worktreePath, branchName)
	} else {
		args = append(args, "-b", branchName, worktreePath, baseBranch)
	}

	if _, err := gitOutput(repoRoot, args...); err != nil {
		return fmt.Errorf("prepare issue worktree: %w", err)
	}

	return nil
}

func gitRefExists(repoRoot, ref string) bool {
	cmd := exec.Command("git", "-C", repoRoot, "show-ref", "--verify", "--quiet", ref)
	return cmd.Run() == nil
}

func detectBaseBranch(repoRoot string) (string, error) {
	if symbolic, err := gitOutput(repoRoot, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if strings.HasPrefix(symbolic, "origin/") {
			return strings.TrimPrefix(symbolic, "origin/"), nil
		}
	}

	if branch, err := gitOutput(repoRoot, "branch", "--show-current"); err == nil && strings.TrimSpace(branch) != "" {
		return branch, nil
	}

	for _, candidate := range []string{"main", "master"} {
		if gitRefExists(repoRoot, "refs/heads/"+candidate) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("could not determine base branch for %s", repoRoot)
}

func gitOutput(repoRoot string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func (s *Server) writeTaskStateFile(currentTask *task.Task, currentRun *task.Run) error {
	if currentTask == nil || currentRun == nil {
		return nil
	}
	cwd := strings.TrimSpace(currentTask.Cwd)
	if cwd == "" {
		return nil
	}

	path := taskStateFilePath(cwd)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	managedBlock := s.renderTaskStateManagedBlock(currentTask, currentRun)
	content := string(existing)
	if strings.TrimSpace(content) == "" {
		content = s.renderInitialTaskStateFile(currentTask, managedBlock)
	} else {
		content = replaceManagedTaskState(content, managedBlock)
	}

	return writeServerFileAtomic(path, []byte(content), 0o644)
}

func (s *Server) renderInitialTaskStateFile(currentTask *task.Task, managedBlock string) string {
	goal := strings.TrimSpace(currentTask.Description)
	if goal == "" {
		goal = strings.TrimSpace(currentTask.Title)
	}

	var builder strings.Builder
	builder.WriteString("# ")
	builder.WriteString(fmt.Sprintf("%s %s", task.DisplayID(currentTask), strings.TrimSpace(currentTask.Title)))
	builder.WriteString("\n\n## Goal\n")
	builder.WriteString(goal)
	builder.WriteString("\n\n## Machine status\n")
	builder.WriteString(managedBlock)
	builder.WriteString("\n\n## Completed\n- \n\n## Known pitfalls / blockers\n- \n\n## Next step\n- Continue from the latest machine status above.\n")
	return builder.String()
}

func (s *Server) renderTaskStateManagedBlock(currentTask *task.Task, currentRun *task.Run) string {
	lines := []string{
		taskStateMachineStatusStart,
		fmt.Sprintf("- Updated: %s", time.Now().UTC().Format(time.RFC3339)),
		fmt.Sprintf("- Task status: %s", currentTask.Status),
		fmt.Sprintf("- Run status: %s", currentRun.Status),
		fmt.Sprintf("- Run attempt: %d", currentRun.AttemptNumber),
	}
	if cwd := strings.TrimSpace(currentTask.Cwd); cwd != "" {
		lines = append(lines, fmt.Sprintf("- Workspace: %s", cwd))
	}
	if currentRun.AgentSessionID != "" {
		lines = append(lines, fmt.Sprintf("- Session: %s", currentRun.AgentSessionID))
	}

	summary := strings.TrimSpace(currentRun.WaitingReason)
	if summary == "" {
		summary = strings.TrimSpace(currentRun.LastError)
	}
	if summary == "" {
		summary = strings.TrimSpace(currentRun.Summary)
	}
	if summary != "" {
		lines = append(lines, fmt.Sprintf("- Summary: %s", collapseCommentText(summary)))
	}
	lines = append(lines, taskStateMachineStatusEnd)
	return strings.Join(lines, "\n")
}

func replaceManagedTaskState(content, managedBlock string) string {
	start := strings.Index(content, taskStateMachineStatusStart)
	end := strings.Index(content, taskStateMachineStatusEnd)
	if start == -1 || end == -1 || end < start {
		content = strings.TrimRight(content, "\n")
		if content != "" {
			content += "\n\n"
		}
		content += "## Machine status\n" + managedBlock + "\n"
		return content
	}
	end += len(taskStateMachineStatusEnd)
	return content[:start] + managedBlock + content[end:]
}

func writeServerFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, mode); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (s *Server) findLiveRunForTask(currentTask *task.Task) (*task.Run, error) {
	if currentTask == nil {
		return nil, fmt.Errorf("task not found")
	}

	if currentTask.CurrentRunID != "" {
		if currentRun := s.runs.Get(currentTask.CurrentRunID); currentRun != nil {
			if currentRun.AgentSessionID != "" && s.watcher.GetAgent(currentRun.AgentSessionID) != nil {
				return currentRun, nil
			}
		}
	}

	for _, run := range s.runs.List() {
		if run.TaskID != currentTask.ID || run.AgentSessionID == "" {
			continue
		}
		if s.watcher.GetAgent(run.AgentSessionID) != nil {
			return run, nil
		}
	}

	return nil, fmt.Errorf("no live session is linked to this issue")
}

func (s *Server) findTaskComment(currentTask *task.Task, commentID string) *task.TaskComment {
	if currentTask == nil || commentID == "" {
		return nil
	}

	for i := range currentTask.Comments {
		if currentTask.Comments[i].ID == commentID {
			comment := currentTask.Comments[i]
			return &comment
		}
	}

	return nil
}

func (s *Server) commentAncestors(currentTask *task.Task, commentID string) []task.TaskComment {
	if currentTask == nil || commentID == "" {
		return nil
	}

	byID := make(map[string]task.TaskComment, len(currentTask.Comments))
	for _, comment := range currentTask.Comments {
		byID[comment.ID] = comment
	}

	chain := make([]task.TaskComment, 0, 6)
	seen := make(map[string]bool)
	currentID := commentID
	for currentID != "" && !seen[currentID] {
		current, ok := byID[currentID]
		if !ok {
			break
		}
		seen[currentID] = true
		chain = append(chain, current)
		currentID = current.ParentID
	}

	for left, right := 0, len(chain)-1; left < right; left, right = left+1, right-1 {
		chain[left], chain[right] = chain[right], chain[left]
	}

	if len(chain) > 6 {
		chain = chain[len(chain)-6:]
	}

	return chain
}

func (s *Server) formatRecentTaskDiscussion(currentTask *task.Task, limit int) string {
	if currentTask == nil || len(currentTask.Comments) == 0 || limit <= 0 {
		return ""
	}

	start := len(currentTask.Comments) - limit
	if start < 0 {
		start = 0
	}

	lines := make([]string, 0, (len(currentTask.Comments)-start)*2)
	for _, comment := range currentTask.Comments[start:] {
		label := strings.TrimSpace(comment.AuthorLabel)
		if label == "" {
			label = "Comment"
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", label, collapseCommentText(comment.Body)))
		if attachmentLine := formatAttachmentInlineLine(comment.Attachments); attachmentLine != "" {
			lines = append(lines, "  "+attachmentLine)
		}
	}

	return strings.Join(lines, "\n")
}

func (s *Server) formatCommentContextBlock(currentTask *task.Task, parentCommentID string) string {
	chain := s.commentAncestors(currentTask, parentCommentID)
	if len(chain) == 0 {
		return ""
	}

	lines := make([]string, 0, len(chain))
	for _, comment := range chain {
		label := strings.TrimSpace(comment.AuthorLabel)
		if label == "" {
			label = "Comment"
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", label, collapseCommentText(comment.Body)))
		if attachmentLine := formatAttachmentInlineLine(comment.Attachments); attachmentLine != "" {
			lines = append(lines, "  "+attachmentLine)
		}
	}

	return strings.Join(lines, "\n")
}

func (s *Server) taskProject(currentTask *task.Task) *task.Project {
	if s.projects == nil || currentTask == nil || strings.TrimSpace(currentTask.ProjectID) == "" {
		return nil
	}
	return s.projects.Get(currentTask.ProjectID)
}

func taskPriorityLabel(priority int) string {
	switch priority {
	case 1:
		return "Urgent"
	case 2:
		return "High"
	case 3:
		return "Medium"
	case 4:
		return "Low"
	default:
		return "None"
	}
}

func (s *Server) buildCurrentRunReplyMessage(t *task.Task, parentCommentID, body string, attachments []task.Attachment) string {
	contextBlock := s.formatCommentContextBlock(t, parentCommentID)
	if contextBlock == "" {
		return strings.TrimSpace(body) + formatTrailingAttachments(attachments)
	}

	var builder strings.Builder
	builder.WriteString("Reply context:\n")
	builder.WriteString(contextBlock)
	builder.WriteString("\n\nNew reply:\n")
	builder.WriteString(strings.TrimSpace(body))
	if attachmentBlock := formatAttachmentsBlock("Attached files", attachments); attachmentBlock != "" {
		builder.WriteString("\n\n")
		builder.WriteString(attachmentBlock)
	}
	return builder.String()
}

func (s *Server) buildSpawnRunCommentInstruction(t *task.Task, parentCommentID, body string, attachments []task.Attachment) string {
	contextBlock := s.formatCommentContextBlock(t, parentCommentID)
	if contextBlock == "" {
		return strings.TrimSpace(body) + formatTrailingAttachments(attachments)
	}

	var builder strings.Builder
	builder.WriteString("Relevant discussion:\n")
	builder.WriteString(contextBlock)
	builder.WriteString("\n\nPlease address this reply:\n")
	builder.WriteString(strings.TrimSpace(body))
	if attachmentBlock := formatAttachmentsBlock("Attached files", attachments); attachmentBlock != "" {
		builder.WriteString("\n\n")
		builder.WriteString(attachmentBlock)
	}
	return builder.String()
}

func (s *Server) buildAttachedSessionMessage(t *task.Task, parentCommentID, body string, attachments []task.Attachment) string {
	var builder strings.Builder
	builder.WriteString("Please work on this issue.\n\n")
	builder.WriteString("Issue: ")
	builder.WriteString(strings.TrimSpace(t.Title))
	if description := strings.TrimSpace(t.Description); description != "" {
		builder.WriteString("\n\nContext:\n")
		builder.WriteString(description)
	}
	if attachmentBlock := formatAttachmentsBlock("Issue attachments", t.Attachments); attachmentBlock != "" {
		builder.WriteString("\n\n")
		builder.WriteString(attachmentBlock)
	}
	if contextBlock := s.formatCommentContextBlock(t, parentCommentID); contextBlock != "" {
		builder.WriteString("\n\nRelevant discussion:\n")
		builder.WriteString(contextBlock)
	}
	if note := strings.TrimSpace(body); note != "" {
		builder.WriteString("\n\nUser message:\n")
		builder.WriteString(note)
	}
	if attachmentBlock := formatAttachmentsBlock("Comment attachments", attachments); attachmentBlock != "" {
		builder.WriteString("\n\n")
		builder.WriteString(attachmentBlock)
	}
	return builder.String()
}

func (s *Server) agentSessionLabel(agentSessionID string) string {
	agent := s.watcher.GetAgent(agentSessionID)
	if agent == nil {
		return agentSessionID
	}
	if project := strings.TrimSpace(agent.Project); project != "" {
		return project
	}
	if name := strings.TrimSpace(agent.Name); name != "" {
		return name
	}
	return agentSessionID
}

func mapAgentStateToRunStatus(state string) task.RunStatus {
	switch state {
	case "blocked":
		return task.RunStatusBlocked
	case "failed":
		return task.RunStatusFailed
	case "done":
		return task.RunStatusDone
	case "running":
		return task.RunStatusRunning
	default:
		return task.RunStatusRunning
	}
}

func mapWatcherEventToRunStatus(ev watcher.SessionEvent) task.RunStatus {
	if ev.Type == "agent_removed" {
		switch ev.OldState {
		case "failed":
			return task.RunStatusFailed
		case "done":
			return task.RunStatusDone
		default:
			return task.RunStatusCancelled
		}
	}
	return mapAgentStateToRunStatus(ev.NewState)
}

func collapseCommentText(text string) string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if len(normalized) <= 220 {
		return normalized
	}
	return normalized[:217] + "..."
}

func formatAttachmentsBlock(title string, attachments []task.Attachment) string {
	if len(attachments) == 0 {
		return ""
	}

	lines := make([]string, 0, len(attachments)+1)
	lines = append(lines, title+":")
	for _, attachment := range attachments {
		path := strings.TrimSpace(attachment.Path)
		if path == "" {
			continue
		}
		name := strings.TrimSpace(attachment.Name)
		if name != "" && name != path {
			lines = append(lines, fmt.Sprintf("- %s (%s)", path, name))
			continue
		}
		lines = append(lines, "- "+path)
	}
	if len(lines) == 1 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func formatAttachmentInlineLine(attachments []task.Attachment) string {
	if len(attachments) == 0 {
		return ""
	}
	parts := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		if path := strings.TrimSpace(attachment.Path); path != "" {
			parts = append(parts, path)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "Attachments: " + strings.Join(parts, ", ")
}

func formatTrailingAttachments(attachments []task.Attachment) string {
	if block := formatAttachmentsBlock("Attached files", attachments); block != "" {
		return "\n\n" + block
	}
	return ""
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
