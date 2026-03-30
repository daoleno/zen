package terminal

import (
	"context"
	"fmt"
	"log"
	"sync"
)

// SendFunc delivers protocol messages to a single client.
type SendFunc func(v any)

type managedSession struct {
	owner   string
	target  string
	session Session
	cancel  context.CancelFunc
}

// Manager owns terminal sessions and routes their output to the opening client.
type Manager struct {
	mu       sync.Mutex
	backends map[string]Backend
	sessions map[string]*managedSession
}

// NewManager creates a terminal manager.
func NewManager(backends ...Backend) *Manager {
	mgr := &Manager{
		backends: make(map[string]Backend),
		sessions: make(map[string]*managedSession),
	}
	for _, backend := range backends {
		mgr.backends[backend.Name()] = backend
	}
	return mgr
}

// Open starts a terminal session and begins forwarding events to the client.
func (m *Manager) Open(ownerID, backendName, targetID string, opts OpenOptions, send SendFunc) (Session, error) {
	m.mu.Lock()
	backend, ok := m.backends[backendName]
	existing := make([]string, 0)
	for id, ms := range m.sessions {
		if ms.owner == ownerID && ms.target == targetID {
			existing = append(existing, id)
		}
	}
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown terminal backend: %s", backendName)
	}
	for _, id := range existing {
		if err := m.Close(ownerID, id); err != nil {
			log.Printf("close existing terminal session %s for target %s: %v", id, targetID, err)
		}
	}

	session, err := backend.Open(targetID, opts)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := session.Start(ctx); err != nil {
		cancel()
		return nil, err
	}

	m.mu.Lock()
	m.sessions[session.ID()] = &managedSession{
		owner:   ownerID,
		target:  targetID,
		session: session,
		cancel:  cancel,
	}
	m.mu.Unlock()

	go m.forward(ownerID, session, send)

	return session, nil
}

func (m *Manager) forward(ownerID string, session Session, send SendFunc) {
	for ev := range session.Events() {
		switch ev.Type {
		case EventHistory:
			send(map[string]any{
				"type":       "terminal_history",
				"session_id": session.ID(),
				"data":       ev.Data,
			})
		case EventOutput:
			send(map[string]any{
				"type":       "terminal_output",
				"session_id": session.ID(),
				"data":       ev.Data,
			})
		case EventScroll:
			send(map[string]any{
				"type":            "terminal_scroll_state",
				"session_id":      session.ID(),
				"at_bottom":       ev.ScrollState.AtBottom,
				"in_copy_mode":    ev.ScrollState.InCopyMode,
				"scroll_position": ev.ScrollState.Position,
			})
		case EventExit:
			send(map[string]any{
				"type":       "terminal_exit",
				"session_id": session.ID(),
				"exit_code":  ev.ExitCode,
			})
			m.cleanup(ownerID, session.ID())
			return
		case EventError:
			message := "terminal session error"
			if ev.Err != nil {
				message = ev.Err.Error()
				log.Printf("terminal session %s error: %v", session.ID(), ev.Err)
			}
			send(map[string]any{
				"type":       "terminal_error",
				"session_id": session.ID(),
				"code":       "session_error",
				"message":    message,
			})
		}
	}

	m.cleanup(ownerID, session.ID())
}

func (m *Manager) cleanup(ownerID, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ms, ok := m.sessions[sessionID]
	if !ok || ms.owner != ownerID {
		return
	}
	delete(m.sessions, sessionID)
}

func (m *Manager) withSession(ownerID, sessionID string) (*managedSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ms, ok := m.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("unknown terminal session: %s", sessionID)
	}
	if ms.owner != ownerID {
		return nil, fmt.Errorf("terminal session ownership mismatch")
	}
	return ms, nil
}

// Input forwards input bytes to a session.
func (m *Manager) Input(ownerID, sessionID, data string) error {
	ms, err := m.withSession(ownerID, sessionID)
	if err != nil {
		return err
	}
	return ms.session.Write(data)
}

// Scroll uses tmux copy-mode to scroll through tmux's own scrollback buffer.
// Positive lines = scroll down (toward newer), negative = scroll up (toward older).
func (m *Manager) Scroll(ownerID, sessionID string, lines int) error {
	ms, err := m.withSession(ownerID, sessionID)
	if err != nil {
		return err
	}
	if scroller, ok := ms.session.(Scroller); ok {
		return scroller.Scroll(lines)
	}
	return fmt.Errorf("session does not support scrolling")
}

// ScrollCancel exits tmux copy-mode and returns to the live view.
func (m *Manager) ScrollCancel(ownerID, sessionID string) error {
	ms, err := m.withSession(ownerID, sessionID)
	if err != nil {
		return err
	}
	if scroller, ok := ms.session.(Scroller); ok {
		return scroller.CancelScroll()
	}
	return nil
}

// Resize updates a terminal session's dimensions.
func (m *Manager) Resize(ownerID, sessionID string, cols, rows int) error {
	ms, err := m.withSession(ownerID, sessionID)
	if err != nil {
		return err
	}
	return ms.session.Resize(cols, rows)
}

// Close tears down a session.
func (m *Manager) Close(ownerID, sessionID string) error {
	ms, err := m.withSession(ownerID, sessionID)
	if err != nil {
		return err
	}
	ms.cancel()
	if err := ms.session.Close(); err != nil {
		return err
	}

	m.mu.Lock()
	delete(m.sessions, sessionID)
	m.mu.Unlock()
	return nil
}

// CloseAll tears down all sessions owned by a client.
func (m *Manager) CloseAll(ownerID string) {
	m.mu.Lock()
	ids := make([]string, 0)
	for id, ms := range m.sessions {
		if ms.owner == ownerID {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()

	for _, id := range ids {
		if err := m.Close(ownerID, id); err != nil {
			log.Printf("close terminal session %s: %v", id, err)
		}
	}
}
