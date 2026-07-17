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
		id:            fmt.Sprintf("%s#%d", targetID, id),
		targetID:      targetID,
		requestedSize: size,
		events:        make(chan Event, 128),
	}, nil
}

type tmuxSession struct {
	id            string
	targetID      string
	linkedSession string // disposable linked view session, cleaned up on close
	requestedSize Size   // mobile projection metadata returned by Size
	viewPTYSize   Size   // source-derived geometry reported to the tmux client

	mu                 sync.Mutex
	events             chan Event
	cancel             context.CancelFunc
	cmd                *exec.Cmd
	pty                *os.File
	closed             bool
	closeOnce          sync.Once
	inCopyMode         bool
	scrollStateTimer   *time.Timer
	scrollStateVersion uint64
}

type tmuxReadResult struct {
	data string
	err  error
	eof  bool
}

const (
	tmuxInitialHistoryViewportScreens = 4
	tmuxInitialHistoryMaxLines        = 240
	tmuxScrollStateDebounce           = 120 * time.Millisecond
)

func (s *tmuxSession) ID() string { return s.id }

func (s *tmuxSession) Events() <-chan Event { return s.events }

// Size reports the mobile projection dimensions requested through Open or
// Resize. The tmux-facing PTY deliberately mirrors source-owned geometry.
func (s *tmuxSession) Size() Size { return s.requestedSize }

func (s *tmuxSession) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	linkedName, cmd, viewPTYSize, err := tmuxLinkedViewSession(s.targetID)
	if err != nil {
		s.mu.Unlock()
		cancel()
		return err
	}
	s.linkedSession = linkedName
	cmd.Env = tmuxClientEnv(os.Environ())

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(viewPTYSize.Cols),
		Rows: uint16(viewPTYSize.Rows),
	})
	if err != nil {
		_ = exec.Command("tmux", "kill-session", "-t", s.linkedSession).Run()
		s.linkedSession = ""
		s.mu.Unlock()
		cancel()
		return fmt.Errorf("start tmux client pty: %w", err)
	}

	s.viewPTYSize = viewPTYSize
	s.cmd = cmd
	s.pty = ptmx
	s.mu.Unlock()

	s.emitScrollState()
	s.emitInitialHistory()

	go s.streamLoop(runCtx, ptmx)
	go s.waitLoop()

	return nil
}

func (s *tmuxSession) streamLoop(ctx context.Context, ptmx *os.File) {
	const flushInterval = 16 * time.Millisecond
	const maxFrameBytes = 8192

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
		for len(data) > 0 {
			chunk, rest := splitUTF8Prefix(data, maxFrameBytes)
			s.sendEvent(Event{
				Type: EventOutput,
				Data: chunk,
			})
			data = rest
		}
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
	if lines > 0 {
		s.emitScrollState()
		return nil
	}
	s.scheduleScrollStateEmit()
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

func (s *tmuxSession) CopyBuffer() (string, error) {
	s.mu.Lock()
	target := s.interactiveTargetLocked()
	s.mu.Unlock()
	if target == "" {
		return "", nil
	}
	return tmuxCaptureCopyBuffer(target)
}

func (s *tmuxSession) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.requestedSize = Size{Cols: cols, Rows: rows}
	if s.pty == nil {
		return nil
	}
	// Keep the mobile dimensions as projection metadata only. tmux still uses
	// an ignore-size normal client's PTY geometry when initializing later
	// windows, so the tmux-facing PTY must continue to mirror the source.
	return s.syncViewPTYToWindowLocked()
}

func (s *tmuxSession) syncViewPTYToWindowLocked() error {
	if s.pty == nil {
		return nil
	}
	viewSize, err := tmuxViewPTYSize(s.interactiveTargetLocked())
	if err != nil {
		return fmt.Errorf("read source tmux window size: %w", err)
	}
	if viewSize == s.viewPTYSize {
		return nil
	}
	if err := pty.Setsize(s.pty, &pty.Winsize{
		Cols: uint16(viewSize.Cols),
		Rows: uint16(viewSize.Rows),
	}); err != nil {
		return fmt.Errorf("sync tmux view pty to source window: %w", err)
	}
	s.viewPTYSize = viewSize
	return nil
}

func (s *tmuxSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.cancelScheduledScrollStateLocked()
	if s.cancel != nil {
		s.cancel()
	}
	if s.pty != nil {
		_ = s.pty.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	// Kill the disposable view session so it doesn't linger.
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

func (s *tmuxSession) interactiveTargetLocked() string {
	if s.linkedSession != "" {
		return s.linkedSession
	}
	return s.targetID
}

func (s *tmuxSession) scheduleScrollStateEmit() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}

	s.scrollStateVersion++
	version := s.scrollStateVersion
	if s.scrollStateTimer != nil {
		s.scrollStateTimer.Stop()
	}
	s.scrollStateTimer = time.AfterFunc(tmuxScrollStateDebounce, func() {
		s.emitScheduledScrollState(version)
	})
	s.mu.Unlock()
}

func (s *tmuxSession) emitScheduledScrollState(version uint64) {
	s.mu.Lock()
	if s.closed || version != s.scrollStateVersion {
		s.mu.Unlock()
		return
	}
	s.scrollStateTimer = nil
	s.mu.Unlock()

	s.emitScrollState()
}

func (s *tmuxSession) cancelScheduledScrollStateLocked() {
	s.scrollStateVersion++
	if s.scrollStateTimer != nil {
		s.scrollStateTimer.Stop()
		s.scrollStateTimer = nil
	}
}

func (s *tmuxSession) emitScrollState() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.cancelScheduledScrollStateLocked()
	state := s.readScrollStateLocked()
	s.inCopyMode = state.InCopyMode
	s.mu.Unlock()
	s.sendEvent(Event{
		Type:        EventScroll,
		ScrollState: state,
	})
}

func (s *tmuxSession) emitInitialHistory() {
	s.mu.Lock()
	target := s.interactiveTargetLocked()
	s.mu.Unlock()
	if target == "" {
		return
	}

	history, err := tmuxCaptureHistory(target)
	if err != nil {
		return
	}
	if history == "" {
		return
	}

	s.sendEvent(Event{
		Type: EventHistory,
		Data: sanitizeTmuxHistory(history),
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

// tmuxLinkedViewSession creates an independent disposable session and links
// only the requested source window into it. It deliberately does not join the
// source session group: group members share future windows, and a small view
// session can otherwise seed those new shared windows with its own geometry.
func tmuxLinkedViewSession(targetID string) (string, *exec.Cmd, Size, error) {
	sourceTarget, err := tmuxSourceWindowTarget(targetID)
	if err != nil {
		return "", nil, Size{}, err
	}

	// Unique name per open (PID + counter).
	id := sessionCounter.Add(1)
	linkedName := fmt.Sprintf("zen-%d-%d", os.Getpid(), id)

	// Bootstrap an independent session, then atomically replace its private
	// window with a link to the source window. The bootstrap geometry is never
	// shared with the source.
	bootstrapOut, err := tmuxNewViewSessionCommand(linkedName).Output()
	if err != nil {
		return "", nil, Size{}, fmt.Errorf("create tmux view session: %w", err)
	}
	cleanup := func() {
		_ = exec.Command("tmux", "kill-session", "-t", linkedName).Run()
	}
	bootstrapTarget := strings.TrimSpace(string(bootstrapOut))
	if bootstrapTarget == "" {
		cleanup()
		return "", nil, Size{}, fmt.Errorf("create tmux view session: empty bootstrap window target")
	}
	if err := tmuxLinkViewWindowCommand(sourceTarget, bootstrapTarget).Run(); err != nil {
		cleanup()
		return "", nil, Size{}, fmt.Errorf("link source tmux window %s into view: %w", sourceTarget, err)
	}
	viewPTYSize, err := tmuxViewPTYSize(linkedName)
	if err != nil {
		cleanup()
		return "", nil, Size{}, fmt.Errorf("read source tmux window size: %w", err)
	}

	return linkedName, tmuxAttachCommand(linkedName), viewPTYSize, nil
}

func tmuxNewViewSessionCommand(sessionName string) *exec.Cmd {
	return exec.Command(
		"tmux",
		"new-session",
		"-d",
		"-P",
		"-F",
		"#{window_id}",
		"-s",
		sessionName,
		"sleep 86400",
	)
}

func tmuxLinkViewWindowCommand(sourceTarget, bootstrapTarget string) *exec.Cmd {
	return exec.Command(
		"tmux",
		"link-window",
		"-k",
		"-s",
		sourceTarget,
		"-t",
		bootstrapTarget,
	)
}

func tmuxWindowSize(target string) (Size, error) {
	out, err := exec.Command(
		"tmux",
		"display-message",
		"-p",
		"-t",
		target,
		"#{window_width}\t#{window_height}",
	).Output()
	if err != nil {
		return Size{}, err
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "\t")
	if len(parts) != 2 {
		return Size{}, fmt.Errorf("unexpected tmux window size %q", strings.TrimSpace(string(out)))
	}
	cols, err := strconv.Atoi(parts[0])
	if err != nil {
		return Size{}, fmt.Errorf("parse tmux window width: %w", err)
	}
	rows, err := strconv.Atoi(parts[1])
	if err != nil {
		return Size{}, fmt.Errorf("parse tmux window height: %w", err)
	}
	if cols <= 0 || cols > int(^uint16(0)) || rows <= 0 || rows > int(^uint16(0)) {
		return Size{}, fmt.Errorf("invalid tmux window size %dx%d", cols, rows)
	}
	return Size{Cols: cols, Rows: rows}, nil
}

func tmuxViewPTYSize(target string) (Size, error) {
	windowSize, err := tmuxWindowSize(target)
	if err != nil {
		return Size{}, err
	}
	out, err := exec.Command("tmux", "show-options", "-v", "-t", target, "status").Output()
	if err != nil {
		return Size{}, fmt.Errorf("read tmux view status size: %w", err)
	}
	statusValue := strings.TrimSpace(string(out))
	if statusValue == "" {
		out, err = exec.Command("tmux", "show-options", "-g", "-v", "status").Output()
		if err != nil {
			return Size{}, fmt.Errorf("read global tmux view status size: %w", err)
		}
		statusValue = strings.TrimSpace(string(out))
	}
	statusLines, err := tmuxStatusLines(statusValue)
	if err != nil {
		return Size{}, err
	}
	if windowSize.Rows > int(^uint16(0))-statusLines {
		return Size{}, fmt.Errorf(
			"tmux view size %dx%d with %d status lines exceeds PTY limits",
			windowSize.Cols,
			windowSize.Rows,
			statusLines,
		)
	}
	windowSize.Rows += statusLines
	return windowSize, nil
}

func tmuxStatusLines(value string) (int, error) {
	switch value {
	case "off", "0":
		return 0, nil
	case "on":
		return 1, nil
	}
	lines, err := strconv.Atoi(value)
	if err != nil || lines < 1 || lines > 5 {
		return 0, fmt.Errorf("invalid tmux status size %q", value)
	}
	return lines, nil
}

func tmuxSourceWindowTarget(targetID string) (string, error) {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return "", fmt.Errorf("empty tmux target")
	}

	sessionName, _, hasWindow := strings.Cut(targetID, ":")
	sessionName = strings.TrimSpace(sessionName)
	if sessionName == "" {
		return "", fmt.Errorf("invalid tmux target %q", targetID)
	}
	if !hasWindow {
		return sessionName, nil
	}

	windowRef := tmuxWindowRef(targetID)
	if windowRef == "" {
		return "", fmt.Errorf("invalid tmux window target %q", targetID)
	}
	return sessionName + ":" + windowRef, nil
}

func tmuxWindowRef(targetID string) string {
	_, windowRef, ok := strings.Cut(strings.TrimSpace(targetID), ":")
	if !ok {
		return ""
	}

	windowRef = strings.TrimSpace(windowRef)
	if idx := strings.Index(windowRef, "."); idx >= 0 {
		windowRef = windowRef[:idx]
	}

	return strings.TrimSpace(windowRef)
}

func tmuxAttachCommand(sessionName string) *exec.Cmd {
	return exec.Command(
		"tmux",
		"-T",
		"RGB,256",
		"attach-session",
		"-f",
		"ignore-size",
		"-t",
		sessionName,
	)
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

func tmuxCaptureHistory(targetID string) (string, error) {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return "", nil
	}

	paneHeight, historySize, err := tmuxHistoryBounds(targetID)
	if err != nil {
		return "", err
	}
	if historySize <= 0 {
		return "", nil
	}

	startLine, endLine := tmuxHistoryCaptureRange(paneHeight, historySize)
	cmd := exec.Command(
		"tmux",
		"capture-pane",
		"-p",
		"-e",
		"-S",
		fmt.Sprintf("%d", startLine),
		"-E",
		fmt.Sprintf("%d", endLine),
		"-t",
		targetID,
	)
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

func tmuxCaptureCopyBuffer(targetID string) (string, error) {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return "", nil
	}

	out, err := exec.Command(
		"tmux",
		"capture-pane",
		"-p",
		"-S",
		"-",
		"-E",
		"-",
		"-t",
		targetID,
	).Output()
	if err != nil {
		return "", fmt.Errorf("capture tmux copy buffer: %w", err)
	}

	buffer := string(out)
	if buffer == "" {
		return "", nil
	}
	if !strings.HasSuffix(buffer, "\n") {
		buffer += "\n"
	}
	return buffer, nil
}

func tmuxHistoryBounds(targetID string) (paneHeight int, historySize int, err error) {
	out, err := exec.Command(
		"tmux",
		"display-message",
		"-p",
		"-t",
		targetID,
		"#{pane_height}:#{history_size}",
	).Output()
	if err != nil {
		return 0, 0, fmt.Errorf("read tmux history bounds: %w", err)
	}

	parts := strings.SplitN(strings.TrimSpace(string(out)), ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected tmux history bounds: %q", strings.TrimSpace(string(out)))
	}

	paneHeight, err = strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("parse tmux pane height: %w", err)
	}
	historySize, err = strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("parse tmux history size: %w", err)
	}

	return paneHeight, historySize, nil
}

func tmuxHistoryCaptureRange(paneHeight int, historySize int) (startLine int, endLine int) {
	if paneHeight <= 0 || historySize <= 0 {
		return 0, -1
	}

	captureLines := paneHeight * tmuxInitialHistoryViewportScreens
	if captureLines > tmuxInitialHistoryMaxLines {
		captureLines = tmuxInitialHistoryMaxLines
	}
	if captureLines > historySize {
		captureLines = historySize
	}

	return -captureLines, -paneHeight
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
