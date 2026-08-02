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
	owner         string
	target        string
	session       Session
	sessionReady  chan struct{}
	readyOnce     sync.Once
	cancel        context.CancelFunc
	cancelOnce    sync.Once
	closeOnce     sync.Once
	closeErr      error
	interactionMu sync.Mutex
}

func (s *managedSession) setSession(session Session) {
	s.session = session
	s.readyOnce.Do(func() {
		close(s.sessionReady)
	})
}

func (s *managedSession) waitForSession() Session {
	<-s.sessionReady
	return s.session
}

func (s *managedSession) cancelSession() {
	s.cancelOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
	})
}

func (s *managedSession) close() error {
	s.cancelSession()
	session := s.waitForSession()
	if session == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.closeErr = session.Close()
	})
	return s.closeErr
}

func (s *managedSession) cleanup() {
	if err := s.close(); err != nil {
		session := s.waitForSession()
		if session != nil {
			log.Printf("close terminal session %s: %v", session.ID(), err)
		}
	}
}

const maxTerminalScrollBatchLines = 12

// Manager owns terminal sessions and routes their output to the opening client.
type Manager struct {
	mu       sync.Mutex
	backends map[string]Backend
	sessions map[string]*managedSession
	detached map[string]struct{}
	pending  uint64
	submit   func(cleanup func())
}

// NewManager creates a terminal manager.
func NewManager(backends ...Backend) *Manager {
	mgr := &Manager{
		backends: make(map[string]Backend),
		sessions: make(map[string]*managedSession),
		detached: make(map[string]struct{}),
	}
	for _, backend := range backends {
		mgr.backends[backend.Name()] = backend
	}
	return mgr
}

// SetCleanupSubmitter binds non-interactive physical cleanup to the runtime
// owner. It must be configured before sessions are opened.
func (m *Manager) SetCleanupSubmitter(submit func(cleanup func())) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sessions) != 0 {
		panic("terminal cleanup submitter changed with live sessions")
	}
	m.submit = submit
}

// registerCleanupLocked transfers the physical cleanup claim while the failed
// session is still visible to DetachAll. The caller must hold m.mu until the
// configured runtime owner has counted the claim.
func (m *Manager) registerCleanupLocked(cleanup func()) {
	if cleanup == nil {
		return
	}
	submit := m.submit
	if submit == nil {
		cleanup()
		return
	}
	submit(cleanup)
}

// Open starts a terminal session and begins forwarding events to the client.
func (m *Manager) Open(ownerID, backendName, targetID string, opts OpenOptions, send SendFunc) (Session, error) {
	m.mu.Lock()
	if _, detached := m.detached[ownerID]; detached {
		m.mu.Unlock()
		return nil, fmt.Errorf("terminal owner is detached")
	}
	backend, ok := m.backends[backendName]
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("unknown terminal backend: %s", backendName)
	}
	existing := make([]string, 0)
	for id, ms := range m.sessions {
		if ms.owner == ownerID && ms.target == targetID {
			existing = append(existing, id)
		}
	}
	m.mu.Unlock()
	for _, id := range existing {
		if err := m.Close(ownerID, id); err != nil {
			log.Printf("close existing terminal session %s for target %s: %v", id, targetID, err)
		}
	}

	m.mu.Lock()
	if _, detached := m.detached[ownerID]; detached {
		m.mu.Unlock()
		return nil, fmt.Errorf("terminal owner is detached")
	}
	m.pending++
	pendingID := fmt.Sprintf("\x00pending-%d", m.pending)
	ctx, cancel := context.WithCancel(context.Background())
	managed := &managedSession{
		owner:        ownerID,
		target:       targetID,
		cancel:       cancel,
		sessionReady: make(chan struct{}),
	}
	m.sessions[pendingID] = managed
	m.mu.Unlock()

	session, err := backend.Open(targetID, opts)
	managed.setSession(session)
	if err != nil {
		m.mu.Lock()
		claimed := m.sessions[pendingID] == managed
		if claimed {
			m.registerCleanupLocked(managed.cleanup)
			delete(m.sessions, pendingID)
		}
		m.mu.Unlock()
		return nil, err
	}

	sessionID := session.ID()
	var registrationErr error
	detachedDuringOpen := false
	m.mu.Lock()
	switch {
	case m.sessions[pendingID] != managed:
		detachedDuringOpen = true
		registrationErr = fmt.Errorf("terminal owner is detached")
	case sessionID == "":
		registrationErr = fmt.Errorf("terminal session id is empty")
	case m.sessions[sessionID] != nil:
		registrationErr = fmt.Errorf(
			"terminal session already exists: %s",
			sessionID,
		)
	default:
		delete(m.sessions, pendingID)
		m.sessions[sessionID] = managed
	}
	if registrationErr != nil && !detachedDuringOpen {
		m.registerCleanupLocked(managed.cleanup)
		delete(m.sessions, pendingID)
	}
	m.mu.Unlock()
	if registrationErr != nil {
		return nil, registrationErr
	}

	if err := session.Start(ctx); err != nil {
		m.mu.Lock()
		claimed := m.sessions[sessionID] == managed
		if claimed {
			m.registerCleanupLocked(managed.cleanup)
			delete(m.sessions, sessionID)
		}
		m.mu.Unlock()
		return nil, err
	}

	m.mu.Lock()
	active := m.sessions[sessionID] == managed
	m.mu.Unlock()
	if !active {
		return nil, fmt.Errorf("terminal owner is detached")
	}

	size := session.Size()
	send(map[string]any{
		"type":       "terminal_opened",
		"session_id": session.ID(),
		"backend":    backendName,
		"cols":       size.Cols,
		"rows":       size.Rows,
	})

	go m.forward(ownerID, session, send)

	return session, nil
}

func (m *Manager) forward(ownerID string, session Session, send SendFunc) {
	for ev := range session.Events() {
		switch ev.Type {
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
	ms.interactionMu.Lock()
	defer ms.interactionMu.Unlock()
	if scroller, ok := ms.session.(Scroller); ok {
		if err := scroller.CancelScroll(); err != nil {
			return err
		}
	}
	return ms.session.Write(data)
}

// Scroll advances the one serialized tmux copy-mode owner for this attachment.
// Positive lines move toward live output; negative lines move toward history.
func (m *Manager) Scroll(ownerID, sessionID string, lines int) error {
	ms, err := m.withSession(ownerID, sessionID)
	if err != nil {
		return err
	}
	scroller, ok := ms.session.(Scroller)
	if !ok {
		return fmt.Errorf("session does not support scrolling")
	}
	if lines > maxTerminalScrollBatchLines {
		lines = maxTerminalScrollBatchLines
	} else if lines < -maxTerminalScrollBatchLines {
		lines = -maxTerminalScrollBatchLines
	}
	if lines == 0 {
		return nil
	}
	ms.interactionMu.Lock()
	defer ms.interactionMu.Unlock()
	return scroller.Scroll(lines)
}

// ScrollCancel is an explicit transition back to the live tmux pane.
func (m *Manager) ScrollCancel(ownerID, sessionID string) error {
	ms, err := m.withSession(ownerID, sessionID)
	if err != nil {
		return err
	}
	scroller, ok := ms.session.(Scroller)
	if !ok {
		return nil
	}
	ms.interactionMu.Lock()
	defer ms.interactionMu.Unlock()
	return scroller.CancelScroll()
}

// FocusPane selects the tmux pane that contains the given terminal cell.
func (m *Manager) FocusPane(ownerID, sessionID string, col, row int) error {
	ms, err := m.withSession(ownerID, sessionID)
	if err != nil {
		return err
	}
	ms.interactionMu.Lock()
	defer ms.interactionMu.Unlock()
	if focuser, ok := ms.session.(PaneFocuser); ok {
		return focuser.FocusPane(col, row)
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
	m.mu.Lock()
	ms, ok := m.sessions[sessionID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("unknown terminal session: %s", sessionID)
	}
	if ms.owner != ownerID {
		m.mu.Unlock()
		return fmt.Errorf("terminal session ownership mismatch")
	}
	delete(m.sessions, sessionID)
	m.mu.Unlock()
	ms.cancelSession()
	return ms.close()
}

// DetachAll atomically prevents new sessions for an owner, cancels every
// current session and returns the one physical-close claim. The returned work
// may call host processes and must run under the server's cleanup owner.
func (m *Manager) DetachAll(ownerID string) func() {
	m.mu.Lock()
	m.detached[ownerID] = struct{}{}
	detached := make([]*managedSession, 0)
	for id, ms := range m.sessions {
		if ms.owner == ownerID {
			delete(m.sessions, id)
			ms.cancelSession()
			detached = append(detached, ms)
		}
	}
	m.mu.Unlock()

	if len(detached) == 0 {
		return nil
	}
	return func() {
		for _, ms := range detached {
			ms.cleanup()
		}
	}
}

// ForgetOwner releases the short-lived detach tombstone after the owning
// WebSocket handler has returned and no admitted terminal operation can add
// more sessions.
func (m *Manager) ForgetOwner(ownerID string) {
	m.mu.Lock()
	delete(m.detached, ownerID)
	m.mu.Unlock()
}
