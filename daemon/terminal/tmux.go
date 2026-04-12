package terminal

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/creack/pty"
)

var sessionCounter atomic.Int64
var terminalIDCounter atomic.Int64

// TmuxBackend attaches a dedicated tmux client to an existing tmux session
// and streams the client's PTY output directly to the mobile terminal.
type TmuxBackend struct{}

func (b *TmuxBackend) Name() string { return "tmux" }

func (b *TmuxBackend) Open(targetID string, opts OpenOptions) (Session, error) {
	size := Size{Cols: 120, Rows: 36}
	if opts.Cols > 0 {
		size.Cols = opts.Cols
	}
	if opts.Rows > 0 {
		size.Rows = opts.Rows
	}
	id := terminalIDCounter.Add(1)
	return &tmuxSession{
		id:       fmt.Sprintf("%s#%d", targetID, id),
		targetID: targetID,
		size:     size,
		events:   make(chan Event, 128),
	}, nil
}

type tmuxSession struct {
	id            string
	targetID      string
	linkedSession string // grouped session name, cleaned up on close
	size          Size

	mu         sync.Mutex
	events     chan Event
	cancel     context.CancelFunc
	cmd        *exec.Cmd
	pty        *os.File
	closed     bool
	closeOnce  sync.Once
	inCopyMode bool
}

type tmuxReadResult struct {
	data string
	err  error
	eof  bool
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

	linkedName, cmd, err := tmuxGroupedSession(s.targetID, s.size)
	if err != nil {
		s.mu.Unlock()
		cancel()
		return err
	}
	s.linkedSession = linkedName
	cmd.Env = tmuxClientEnv(os.Environ())

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

	if err := s.syncWindowSize(); err != nil {
		return err
	}

	s.emitScrollState()

	go s.streamLoop(runCtx, ptmx)
	go s.waitLoop()

	return nil
}

func (s *tmuxSession) streamLoop(ctx context.Context, ptmx *os.File) {
	const flushInterval = 80 * time.Millisecond
	const maxFrameBytes = 2048

	results := make(chan tmuxReadResult, 128)
	go func() {
		defer close(results)
		s.readLoop(ctx, ptmx, results)
	}()

	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	timerActive := false
	var pending strings.Builder

	stopTimer := func() {
		if !timerActive {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timerActive = false
	}

	flush := func() {
		if pending.Len() == 0 {
			return
		}
		data := sanitizeTmuxOutput(pending.String())
		pending.Reset()
		chunk, rest := splitUTF8Prefix(data, maxFrameBytes)
		if rest != "" {
			pending.WriteString(rest)
		}
		s.sendEvent(Event{
			Type: EventOutput,
			Data: chunk,
		})
	}

	for {
		select {
		case <-ctx.Done():
			stopTimer()
			flush()
			return
		case result, ok := <-results:
			if !ok {
				stopTimer()
				flush()
				return
			}
			if result.data != "" {
				pending.WriteString(result.data)
				if pending.Len() >= maxFrameBytes {
					stopTimer()
					flush()
					if pending.Len() > 0 {
						timer.Reset(flushInterval)
						timerActive = true
					}
				} else if !timerActive {
					timer.Reset(flushInterval)
					timerActive = true
				}
			}
			if result.err != nil {
				stopTimer()
				flush()
				if !result.eof && ctx.Err() == nil {
					s.sendEvent(Event{Type: EventError, Err: result.err})
				}
				return
			}
			if result.eof {
				stopTimer()
				flush()
				return
			}
		case <-timer.C:
			timerActive = false
			flush()
			if pending.Len() > 0 {
				timer.Reset(flushInterval)
				timerActive = true
			}
		}
	}
}

func splitUTF8Prefix(s string, maxBytes int) (string, string) {
	if len(s) <= maxBytes {
		return s, ""
	}
	end := maxBytes
	for end > 0 && !utf8.ValidString(s[:end]) {
		end--
	}
	if end == 0 {
		_, size := utf8.DecodeRuneInString(s)
		end = size
	}
	return s[:end], s[end:]
}

func (s *tmuxSession) readLoop(ctx context.Context, ptmx *os.File, results chan<- tmuxReadResult) {
	buf := make([]byte, 8192)
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			select {
			case results <- tmuxReadResult{data: string(buf[:n])}:
			case <-ctx.Done():
				return
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if err == io.EOF {
				select {
				case results <- tmuxReadResult{eof: true}:
				case <-ctx.Done():
				}
				return
			}
			select {
			case results <- tmuxReadResult{err: fmt.Errorf("read tmux client pty: %w", err)}:
			case <-ctx.Done():
			}
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
	if s.pty == nil {
		s.mu.Unlock()
		return fmt.Errorf("tmux session is not started")
	}
	target := s.interactiveTargetLocked()
	emitScrollState := false
	// Exit copy-mode before sending user input so the terminal
	// returns to the live view.
	if s.inCopyMode {
		_ = exec.Command("tmux", "send-keys", "-t", target, "-X", "cancel").Run()
		s.inCopyMode = false
		emitScrollState = true
	}
	_, err := s.pty.Write([]byte(data))
	s.mu.Unlock()
	if emitScrollState {
		s.emitScrollState()
	}
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
	target := s.interactiveTargetLocked()

	if lines == 0 {
		s.mu.Unlock()
		s.emitScrollState()
		return nil
	}

	if !s.inCopyMode {
		if lines > 0 {
			s.mu.Unlock()
			s.emitScrollState()
			return nil
		}
		if err := exec.Command("tmux", "copy-mode", "-e", "-t", target).Run(); err != nil {
			s.mu.Unlock()
			return fmt.Errorf("enter copy-mode: %w", err)
		}
		s.inCopyMode = true
	}

	absLines := lines
	if absLines < 0 {
		absLines = -absLines
	}

	// Use copy-mode scroll commands, not cursor movement commands.
	// We need to move the viewport through history, not just the copy-mode cursor.
	cmd := "scroll-up"
	if lines > 0 {
		cmd = "scroll-down-and-cancel"
	}

	if err := exec.Command("tmux", "send-keys", "-t", target,
		"-X", "-N", fmt.Sprintf("%d", absLines), cmd).Run(); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("scroll copy-mode: %w", err)
	}
	s.mu.Unlock()
	s.emitScrollState()
	return nil
}

func (s *tmuxSession) CancelScroll() error {
	s.mu.Lock()
	target := s.interactiveTargetLocked()
	if !s.inCopyMode {
		s.mu.Unlock()
		s.emitScrollState()
		return nil
	}
	if err := exec.Command("tmux", "send-keys", "-t", target, "-X", "cancel").Run(); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("cancel copy-mode: %w", err)
	}
	s.inCopyMode = false
	s.mu.Unlock()
	s.emitScrollState()
	return nil
}

func (s *tmuxSession) FocusPane(col, row int) error {
	if col < 0 || row < 0 {
		return nil
	}

	s.mu.Lock()
	target := s.interactiveTargetLocked()
	s.mu.Unlock()

	out, err := exec.Command(
		"tmux",
		"list-panes",
		"-t",
		target,
		"-F",
		"#{pane_id}\t#{pane_left}\t#{pane_top}\t#{pane_width}\t#{pane_height}",
	).Output()
	if err != nil {
		return fmt.Errorf("list tmux panes: %w", err)
	}

	lines := bytes.Split(bytes.TrimSpace(out), []byte{'\n'})
	for _, line := range lines {
		fields := bytes.Split(line, []byte{'\t'})
		if len(fields) != 5 {
			continue
		}

		paneID := string(fields[0])
		left, errLeft := strconv.Atoi(string(fields[1]))
		top, errTop := strconv.Atoi(string(fields[2]))
		width, errWidth := strconv.Atoi(string(fields[3]))
		height, errHeight := strconv.Atoi(string(fields[4]))
		if errLeft != nil || errTop != nil || errWidth != nil || errHeight != nil {
			continue
		}

		if col < left || col >= left+width || row < top || row >= top+height {
			continue
		}

		if err := exec.Command("tmux", "select-pane", "-t", paneID).Run(); err != nil {
			return fmt.Errorf("select tmux pane: %w", err)
		}
		return nil
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
	return s.syncWindowSizeLocked()
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
	// Kill the grouped session so it doesn't linger
	if s.linkedSession != "" {
		_ = exec.Command("tmux", "kill-session", "-t", s.linkedSession).Run()
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

func (s *tmuxSession) syncWindowSize() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.syncWindowSizeLocked()
}

func (s *tmuxSession) syncWindowSizeLocked() error {
	target := s.interactiveTargetLocked()
	if target == "" || s.size.Cols <= 0 || s.size.Rows <= 0 {
		return nil
	}
	if err := tmuxResizeWindowCommand(target, s.size).Run(); err != nil {
		return fmt.Errorf("resize tmux window: %w", err)
	}
	return nil
}

func (s *tmuxSession) interactiveTargetLocked() string {
	if s.linkedSession != "" {
		return s.linkedSession
	}
	return s.targetID
}

func (s *tmuxSession) emitScrollState() {
	s.mu.Lock()
	state := s.readScrollStateLocked()
	s.inCopyMode = state.InCopyMode
	s.mu.Unlock()
	s.sendEvent(Event{
		Type:        EventScroll,
		ScrollState: state,
	})
}

func (s *tmuxSession) readScrollStateLocked() ScrollState {
	state := ScrollState{
		AtBottom:   !s.inCopyMode,
		InCopyMode: s.inCopyMode,
		Position:   0,
	}

	out, err := exec.Command("tmux", "display-message", "-p", "-t", s.interactiveTargetLocked(),
		"#{pane_in_mode}:#{scroll_position}").Output()
	if err != nil {
		return state
	}

	parts := strings.SplitN(strings.TrimSpace(string(out)), ":", 2)
	if len(parts) > 0 {
		state.InCopyMode = strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[0]) != "0"
	}
	if len(parts) > 1 {
		if position, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
			state.Position = position
		}
	}
	state.AtBottom = !state.InCopyMode
	return state
}

// tmuxGroupedSession creates a linked/grouped tmux session that shares the
// same windows as the target, but with a separate client attachment. This
// lets us attach a dedicated mobile client without hijacking the user's
// original desktop client process. It does NOT create an independent pane
// geometry model: tmux still chooses a single effective window size across
// attached clients according to window-size/aggressive-resize.
func tmuxGroupedSession(targetID string, size Size) (string, *exec.Cmd, error) {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return "", nil, fmt.Errorf("empty tmux target")
	}

	sessionName := targetID
	windowTarget := ""
	if idx := strings.Index(targetID, ":"); idx > 0 {
		sessionName = targetID[:idx]
		windowTarget = targetID
	}

	// Unique name per open (PID + counter)
	id := sessionCounter.Add(1)
	linkedName := fmt.Sprintf("zen-%d-%d", os.Getpid(), id)

	// Create grouped session with its own attached client process.
	createCmd := exec.Command("tmux", "new-session", "-d",
		"-t", sessionName,
		"-s", linkedName,
		"-x", fmt.Sprintf("%d", size.Cols),
		"-y", fmt.Sprintf("%d", size.Rows))
	if err := createCmd.Run(); err != nil {
		return "", nil, fmt.Errorf("create grouped tmux session: %w", err)
	}

	// Prefer the most recently active client size for the shared window.
	// This still means the pane geometry is shared across attached clients.
	_ = exec.Command("tmux", "set-option", "-t", linkedName, "window-size", "latest").Run()
	_ = exec.Command("tmux", "set-option", "-t", linkedName, "aggressive-resize", "on").Run()
	_ = exec.Command(
		"tmux",
		"set-hook",
		"-t",
		linkedName,
		"after-select-window",
		tmuxResizeWindowHook(linkedName, size),
	).Run()

	// Select the correct window in the linked session before attaching
	if windowTarget != "" {
		_ = exec.Command("tmux", "select-window", "-t", linkedName+":"+strings.SplitN(windowTarget, ":", 2)[1]).Run()
	}

	return linkedName, tmuxAttachCommand(linkedName), nil
}

func tmuxAttachCommand(sessionName string) *exec.Cmd {
	return exec.Command("tmux", "-T", "RGB,256", "attach-session", "-t", sessionName)
}

func tmuxResizeWindowCommand(target string, size Size) *exec.Cmd {
	return exec.Command(
		"tmux",
		"resize-window",
		"-t",
		target,
		"-x",
		strconv.Itoa(size.Cols),
		"-y",
		strconv.Itoa(size.Rows),
	)
}

func tmuxResizeWindowHook(target string, size Size) string {
	return fmt.Sprintf("resize-window -t %s -x %d -y %d", target, size.Cols, size.Rows)
}

func tmuxClientEnv(base []string) []string {
	const (
		termKey      = "TERM"
		colorTermKey = "COLORTERM"
	)

	overrides := map[string]string{
		termKey:      "xterm-256color",
		colorTermKey: "truecolor",
	}

	order := make([]string, 0, len(base)+len(overrides))
	values := make(map[string]string, len(base)+len(overrides))

	for _, entry := range base {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		if _, seen := values[key]; !seen {
			order = append(order, key)
		}
		values[key] = value
	}

	for _, key := range []string{termKey, colorTermKey} {
		if _, seen := values[key]; !seen {
			order = append(order, key)
		}
		values[key] = overrides[key]
	}

	env := make([]string, 0, len(order))
	for _, key := range order {
		env = append(env, key+"="+values[key])
	}

	return env
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
	// libghostty is the terminal emulator now, so tmux output must stay intact.
	// Stripping alt-screen or other control sequences breaks tmux's own UI
	// semantics, including pane borders, status areas, and copy-mode redraws.
	return data
}

func sanitizeTmuxHistory(data string) string {
	// Preserve the capture as-is so the emulator receives the same bytes tmux
	// intended to present, rather than an xterm.js-shaped approximation.
	return data
}
