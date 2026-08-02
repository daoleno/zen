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
	linkedSession string // disposable linked view session, cleaned up on close
	size          Size   // exact phone/Ghostty/client PTY grid

	mu                    sync.Mutex
	events                chan Event
	cancel                context.CancelFunc
	runContext            context.Context
	cmd                   *exec.Cmd
	pty                   *os.File
	closed                bool
	closeOnce             sync.Once
	inCopyMode            bool
	scrollStateVersion    uint64
	scrollStateTimer      tmuxScrollTimer
	runTmuxCommand        func(args ...string) error
	readTmuxCommand       func(args ...string) ([]byte, error)
	scheduleScrollCommand func(time.Duration, func()) tmuxScrollTimer
}

type tmuxReadResult struct {
	data string
	err  error
	eof  bool
}

const (
	tmuxScrollReconcileDelay = 32 * time.Millisecond
	tmuxCleanupTimeout       = 2 * time.Second
)

type tmuxScrollTimer interface {
	Stop() bool
}

func (s *tmuxSession) ID() string { return s.id }

func (s *tmuxSession) Events() <-chan Event { return s.events }

func (s *tmuxSession) Size() Size {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.size
}

func (s *tmuxSession) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.runContext = runCtx

	linkedName, cmd, err := tmuxLinkedViewSession(runCtx, s.targetID)
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
		killTmuxSessionBounded(s.linkedSession)
		s.linkedSession = ""
		s.mu.Unlock()
		cancel()
		return fmt.Errorf("start tmux client pty: %w", err)
	}

	s.cmd = cmd
	s.pty = ptmx
	s.mu.Unlock()

	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		s.streamLoop(runCtx, ptmx)
	}()
	go s.waitLoop(streamDone)

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
		data := pending.String()
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

func (s *tmuxSession) waitLoop(streamDone <-chan struct{}) {
	s.mu.Lock()
	cmd := s.cmd
	s.mu.Unlock()
	if cmd == nil {
		return
	}

	err := cmd.Wait()
	<-streamDone
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
	_, err := s.pty.Write([]byte(data))
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("write tmux client pty: %w", err)
	}
	return nil
}

// Scroll drives tmux's own copy-mode. Negative lines move toward older
// history; positive lines move toward the live bottom. tmux redraws the
// attached client PTY, so no capture-derived frame is involved.
func (s *tmuxSession) Scroll(lines int) error {
	if lines == 0 {
		return nil
	}
	absLines := lines
	if absLines < 0 {
		absLines = -absLines
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("tmux session is closed")
	}
	target := s.interactiveTargetLocked()
	if target == "" {
		s.mu.Unlock()
		return fmt.Errorf("tmux session has no interactive target")
	}

	var err error
	if !s.inCopyMode {
		if lines > 0 {
			s.mu.Unlock()
			s.scheduleScrollReconcile()
			return nil
		}
		// One tmux process enters exit-on-bottom copy-mode and applies the first
		// incremental scroll batch in order.
		err = s.runTmuxLocked(
			"copy-mode", "-e", "-t", target,
			";",
			"send-keys", "-t", target, "-X", "-N", strconv.Itoa(absLines), "scroll-up",
		)
		if err == nil {
			s.inCopyMode = true
		}
	} else {
		command := "scroll-up"
		if lines > 0 {
			command = "scroll-down-and-cancel"
		}
		err = s.runTmuxLocked(
			"send-keys", "-t", target, "-X", "-N", strconv.Itoa(absLines), command,
		)
		if err != nil && lines < 0 {
			// A fast reversal can arrive after scroll-down-and-cancel has exited
			// at the bottom but before the idle reconciliation observes it.
			// Retry by atomically re-entering copy-mode with the same line batch.
			err = s.runTmuxLocked(
				"copy-mode", "-e", "-t", target,
				";",
				"send-keys", "-t", target, "-X", "-N", strconv.Itoa(absLines), "scroll-up",
			)
			if err == nil {
				s.inCopyMode = true
			}
		}
	}
	s.mu.Unlock()
	if err != nil {
		// scroll-down-and-cancel legitimately leaves copy-mode at the bottom;
		// a following inertial batch can race the deferred reconciliation and
		// find no copy-mode command table. Treat that downward no-op as bottom,
		// then let the one idle reconciliation publish the exact state.
		if lines > 0 {
			s.scheduleScrollReconcile()
			return nil
		}
		return fmt.Errorf("scroll tmux copy-mode: %w", err)
	}
	s.scheduleScrollReconcile()
	return nil
}

// CancelScroll is an explicit live transition. Repeated cancellation is a
// no-op, so a real input can be forwarded exactly once after this returns.
func (s *tmuxSession) CancelScroll() error {
	s.mu.Lock()
	s.cancelScheduledScrollReconcileLocked()
	if s.closed || !s.inCopyMode {
		s.mu.Unlock()
		return nil
	}
	target := s.interactiveTargetLocked()
	err := s.runTmuxLocked(
		"if-shell", "-F", "-t", target, "#{pane_in_mode}",
		"send-keys -t "+target+" -X cancel",
		"",
	)
	if err == nil {
		s.inCopyMode = false
	}
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("cancel tmux copy-mode: %w", err)
	}
	s.sendEvent(Event{
		Type: EventScroll,
		ScrollState: ScrollState{
			AtBottom: true,
		},
	})
	return nil
}

func (s *tmuxSession) runTmuxLocked(args ...string) error {
	if s.runTmuxCommand != nil {
		return s.runTmuxCommand(args...)
	}
	ctx := s.runContext
	if ctx == nil {
		ctx = context.Background()
	}
	return exec.CommandContext(ctx, "tmux", args...).Run()
}

func (s *tmuxSession) readTmuxLocked(args ...string) ([]byte, error) {
	if s.readTmuxCommand != nil {
		return s.readTmuxCommand(args...)
	}
	ctx := s.runContext
	if ctx == nil {
		ctx = context.Background()
	}
	return exec.CommandContext(ctx, "tmux", args...).Output()
}

func (s *tmuxSession) scheduleScrollReconcile() {
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
	schedule := s.scheduleScrollCommand
	if schedule == nil {
		schedule = func(delay time.Duration, fn func()) tmuxScrollTimer {
			return time.AfterFunc(delay, fn)
		}
	}
	s.scrollStateTimer = schedule(tmuxScrollReconcileDelay, func() {
		s.reconcileScheduledScrollState(version)
	})
	s.mu.Unlock()
}

func (s *tmuxSession) reconcileScheduledScrollState(version uint64) {
	s.mu.Lock()
	if s.closed || version != s.scrollStateVersion {
		s.mu.Unlock()
		return
	}
	s.scrollStateTimer = nil
	target := s.interactiveTargetLocked()
	out, err := s.readTmuxLocked(
		"display-message", "-p", "-t", target,
		"#{pane_in_mode}:#{scroll_position}",
	)
	state := ScrollState{
		AtBottom:   !s.inCopyMode,
		InCopyMode: s.inCopyMode,
	}
	if err == nil {
		parts := strings.SplitN(strings.TrimSpace(string(out)), ":", 2)
		if len(parts) == 2 {
			position, positionErr := strconv.Atoi(parts[1])
			if positionErr == nil {
				state.Position = position
			}
			state.InCopyMode = parts[0] == "1"
			state.AtBottom = !state.InCopyMode || state.Position == 0
			s.inCopyMode = state.InCopyMode
		}
	}
	s.mu.Unlock()
	s.sendEvent(Event{Type: EventScroll, ScrollState: state})
}

func (s *tmuxSession) cancelScheduledScrollReconcileLocked() {
	s.scrollStateVersion++
	if s.scrollStateTimer != nil {
		s.scrollStateTimer.Stop()
		s.scrollStateTimer = nil
	}
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
	if cols <= 0 || rows <= 0 || cols > int(^uint16(0)) || rows > int(^uint16(0)) {
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
	s.cancelScheduledScrollReconcileLocked()
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
		killTmuxSessionBounded(s.linkedSession)
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

// tmuxLinkedViewSession creates an independent disposable session and links
// only the requested source window into it. It deliberately does not join the
// source session group: group members share future windows, and a small view
// session can otherwise seed those new shared windows with its own geometry.
func tmuxLinkedViewSession(
	ctx context.Context,
	targetID string,
) (string, *exec.Cmd, error) {
	sourceTarget, err := tmuxSourceWindowTarget(targetID)
	if err != nil {
		return "", nil, err
	}

	// Unique name per open (PID + counter).
	id := sessionCounter.Add(1)
	linkedName := fmt.Sprintf("zen-%d-%d", os.Getpid(), id)

	// Bootstrap an independent session, then atomically replace its private
	// window with a link to the source window. The bootstrap geometry is never
	// shared with the source.
	bootstrapOut, err := tmuxNewViewSessionCommand(ctx, linkedName).Output()
	if err != nil {
		return "", nil, fmt.Errorf("create tmux view session: %w", err)
	}
	cleanup := func() {
		killTmuxSessionBounded(linkedName)
	}
	bootstrapTarget := strings.TrimSpace(string(bootstrapOut))
	if bootstrapTarget == "" {
		cleanup()
		return "", nil, fmt.Errorf("create tmux view session: empty bootstrap window target")
	}
	if err := tmuxLinkViewWindowCommand(
		ctx,
		sourceTarget,
		bootstrapTarget,
	).Run(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("link source tmux window %s into view: %w", sourceTarget, err)
	}

	return linkedName, tmuxAttachCommand(ctx, linkedName), nil
}

func killTmuxSessionBounded(sessionName string) {
	if strings.TrimSpace(sessionName) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(
		context.Background(),
		tmuxCleanupTimeout,
	)
	defer cancel()
	_ = exec.CommandContext(
		ctx,
		"tmux",
		"kill-session",
		"-t",
		sessionName,
	).Run()
}

func tmuxNewViewSessionCommand(
	ctx context.Context,
	sessionName string,
) *exec.Cmd {
	return exec.CommandContext(
		ctx,
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

func tmuxLinkViewWindowCommand(
	ctx context.Context,
	sourceTarget string,
	bootstrapTarget string,
) *exec.Cmd {
	return exec.CommandContext(
		ctx,
		"tmux",
		"link-window",
		"-k",
		"-s",
		sourceTarget,
		"-t",
		bootstrapTarget,
	)
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

func tmuxAttachCommand(
	ctx context.Context,
	sessionName string,
) *exec.Cmd {
	return exec.CommandContext(
		ctx,
		"tmux",
		"-T",
		"RGB,256",
		"attach-session",
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
