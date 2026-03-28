package terminal

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"github.com/creack/pty"
)

// TmuxBackend attaches a dedicated tmux client to an existing tmux session
// and streams the client's PTY output directly to the mobile terminal.
type TmuxBackend struct{}

var tmuxAltScreenSeq = regexp.MustCompile(`\x1b\[\?(1049|1047|47)(h|l)`)

func (b *TmuxBackend) Name() string { return "tmux" }

func (b *TmuxBackend) Open(targetID string, opts OpenOptions) (Session, error) {
	size := Size{Cols: 120, Rows: 36}
	if opts.Cols > 0 {
		size.Cols = opts.Cols
	}
	if opts.Rows > 0 {
		size.Rows = opts.Rows
	}
	return &tmuxSession{
		id:       targetID,
		targetID: targetID,
		size:     size,
		events:   make(chan Event, 128),
	}, nil
}

type tmuxSession struct {
	id       string
	targetID string
	size     Size

	mu         sync.Mutex
	events     chan Event
	cancel     context.CancelFunc
	cmd        *exec.Cmd
	pty        *os.File
	closed     bool
	closeOnce  sync.Once
	inCopyMode bool
}

func (s *tmuxSession) ID() string { return s.id }

func (s *tmuxSession) Events() <-chan Event { return s.events }

func (s *tmuxSession) Size() Size { return s.size }

func (s *tmuxSession) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	cmd, err := tmuxAttachCommand(s.targetID)
	if err != nil {
		s.mu.Unlock()
		cancel()
		return err
	}
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(s.size.Cols),
		Rows: uint16(s.size.Rows),
	})
	if err != nil {
		s.mu.Unlock()
		cancel()
		return fmt.Errorf("start tmux client pty: %w", err)
	}

	s.cmd = cmd
	s.pty = ptmx
	s.mu.Unlock()

	if history, err := tmuxCaptureHistory(s.targetID, 600); err == nil && history != "" {
		s.sendEvent(Event{
			Type: EventHistory,
			Data: sanitizeTmuxHistory(history),
		})
	}

	go s.readLoop(runCtx, ptmx)
	go s.waitLoop()

	return nil
}

func (s *tmuxSession) readLoop(ctx context.Context, ptmx *os.File) {
	buf := make([]byte, 8192)
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			s.sendEvent(Event{
				Type: EventOutput,
				Data: sanitizeTmuxOutput(string(buf[:n])),
			})
		}
		if err != nil {
			if ctx.Err() != nil || err == io.EOF {
				return
			}
			s.sendEvent(Event{Type: EventError, Err: fmt.Errorf("read tmux client pty: %w", err)})
			return
		}
	}
}

func (s *tmuxSession) waitLoop() {
	s.mu.Lock()
	cmd := s.cmd
	s.mu.Unlock()
	if cmd == nil {
		return
	}

	err := cmd.Wait()
	if err == nil {
		s.sendEvent(Event{Type: EventExit, ExitCode: 0})
		s.closeEvents()
		return
	}

	exitCode := 1
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	}
	s.sendEvent(Event{Type: EventExit, ExitCode: exitCode})
	s.closeEvents()
}

func (s *tmuxSession) Write(data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pty == nil {
		return fmt.Errorf("tmux session is not started")
	}
	// Exit copy-mode before sending user input so the terminal
	// returns to the live view.
	if s.inCopyMode {
		_ = exec.Command("tmux", "send-keys", "-t", s.targetID, "-X", "cancel").Run()
		s.inCopyMode = false
	}
	_, err := s.pty.Write([]byte(data))
	if err != nil {
		return fmt.Errorf("write tmux client pty: %w", err)
	}
	return nil
}

// Scroll enters tmux copy-mode (if needed) and scrolls through tmux's
// own scrollback buffer. Negative lines = scroll up (older content),
// positive = scroll down (newer content).
// This is the correct approach because tmux renders ALL output via cursor
// positioning to the client PTY, so xterm.js never accumulates scrollback.
// tmux's internal scrollback is the only source of history.
func (s *tmuxSession) Scroll(lines int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.inCopyMode {
		if err := exec.Command("tmux", "copy-mode", "-t", s.targetID).Run(); err != nil {
			return fmt.Errorf("enter copy-mode: %w", err)
		}
		s.inCopyMode = true
	}

	absLines := lines
	if absLines < 0 {
		absLines = -absLines
	}

	// Use tmux copy-mode commands via send-keys -X.
	// -N flag sets repeat count (tmux 3.1+).
	cmd := "cursor-up"
	if lines > 0 {
		cmd = "cursor-down"
	}

	if err := exec.Command("tmux", "send-keys", "-t", s.targetID,
		"-X", "-N", fmt.Sprintf("%d", absLines), cmd).Run(); err != nil {
		return fmt.Errorf("scroll copy-mode: %w", err)
	}
	return nil
}

func (s *tmuxSession) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.size = Size{Cols: cols, Rows: rows}
	if s.pty == nil {
		return nil
	}
	if err := pty.Setsize(s.pty, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	}); err != nil {
		return fmt.Errorf("resize tmux client pty: %w", err)
	}
	return nil
}

func (s *tmuxSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	if s.pty != nil {
		_ = s.pty.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	return nil
}

func (s *tmuxSession) sendEvent(ev Event) {
	defer func() {
		_ = recover()
	}()
	s.events <- ev
}

func (s *tmuxSession) closeEvents() {
	s.closeOnce.Do(func() {
		close(s.events)
	})
}

func tmuxAttachCommand(targetID string) (*exec.Cmd, error) {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return nil, fmt.Errorf("empty tmux target")
	}

	sessionName := targetID
	if idx := strings.Index(targetID, ":"); idx > 0 {
		sessionName = targetID[:idx]
	}

	args := []string{"attach-session", "-t", sessionName}
	if strings.Contains(targetID, ":") {
		args = append(args, ";", "select-window", "-t", targetID)
	}
	return exec.Command("tmux", args...), nil
}

func tmuxCaptureHistory(targetID string, lines int) (string, error) {
	if lines <= 0 {
		lines = 2000
	}
	cmd := exec.Command("tmux", "capture-pane", "-p", "-e", "-S", fmt.Sprintf("-%d", lines), "-t", targetID)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("capture tmux history: %w", err)
	}
	history := string(out)
	if history == "" {
		return "", nil
	}
	if !strings.HasSuffix(history, "\n") {
		history += "\n"
	}
	return history, nil
}

func sanitizeTmuxOutput(data string) string {
	// Strip alt-screen sequences. tmux itself wraps ALL content in
	// alt-screen (\x1b[?1049h), so if we pass these through, xterm.js
	// always thinks it's in alternate buffer. Stripping lets xterm.js
	// stay in normal buffer and accumulate scrollback for shell sessions.
	if data == "" {
		return ""
	}
	return tmuxAltScreenSeq.ReplaceAllString(data, "")
}

func sanitizeTmuxHistory(data string) string {
	data = sanitizeTmuxOutput(data)
	// Normalize to \n first, then convert to \r\n for xterm.js
	data = strings.ReplaceAll(data, "\r\n", "\n")
	data = strings.ReplaceAll(data, "\r", "\n")
	data = strings.ReplaceAll(data, "\n", "\r\n")
	return data
}
