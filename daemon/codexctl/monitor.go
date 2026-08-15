package codexctl

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// NativeSettings is the authoritative applied native thread settings as
// reported by the app server: the `thread/resume` response at attach time and
// every `thread/settings/updated` notification afterwards.
//
// Effort follows Zen semantics: the native default (ReasoningEffort::None,
// wire "none") is normalized to "" so callers compare one vocabulary.
type NativeSettings struct {
	ThreadID string
	Model    string
	Effort   string
}

// Matches reports whether the native settings equal the given model/effort
// pair (effort "" = model default).
func (s NativeSettings) Matches(model, effort string) bool {
	return s.Model == model && normalizeNativeEffort(s.Effort) == normalizeNativeEffort(effort)
}

// Monitor maintains the authoritative native thread-settings snapshot for one
// live-control session by attaching to the app-server thread (thread/resume,
// whose response carries the current model + reasoning effort) and consuming
// thread/settings/updated notifications. It is the race-safe evidence source
// for distinguishing a real native thread-settings change from a stale
// in-flight request at the Router: the snapshot only ever reflects applied
// native state, never request bodies.
type Monitor struct {
	client *Client
	mu     sync.Mutex
	// attached is set once the resume response snapshot was captured.
	attached bool
	// ready reports whether a snapshot is currently available.
	ready bool
	// dead reports a terminal connection failure (snapshot unavailable).
	dead      bool
	current   NativeSettings
	done      chan struct{}
	closeOnce sync.Once
}

// OpenMonitor dials the app-server control socket, initializes, and starts
// the snapshot pump. The pump attaches to the native thread (retrying until
// the thread exists) and tracks applied-settings notifications. A nil
// error means the monitor is running; the snapshot becomes available once
// the thread exists and the resume response was parsed.
func OpenMonitor(ctx context.Context, socketPath string, opts DialOptions) (*Monitor, error) {
	if opts.ClientName == "" {
		opts.ClientName = "zen-monitor"
	}
	client, err := Open(ctx, socketPath, opts)
	if err != nil {
		return nil, err
	}
	m := &Monitor{
		client: client,
		done:   make(chan struct{}),
	}
	go m.pump()
	return m, nil
}

// WaitReady blocks until a snapshot is available, the subscription dies, or
// the context expires. The first caller that triggers the lazy monitor
// attach must wait for the resume-response snapshot so the authoritative
// evidence exists before the request is decided.
func (m *Monitor) WaitReady(ctx context.Context) (NativeSettings, bool) {
	for {
		if settings, ok := m.Settings(); ok {
			return settings, true
		}
		if m.isDead() {
			return NativeSettings{}, false
		}
		select {
		case <-ctx.Done():
			return NativeSettings{}, false
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Settings returns the latest authoritative native settings snapshot.
// ok=false means no snapshot is available yet (thread not created or not yet
// attached) or the subscription is dead — callers must fail closed.
func (m *Monitor) Settings() (NativeSettings, bool) {
	if m == nil {
		return NativeSettings{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.ready || m.dead {
		return NativeSettings{}, false
	}
	return m.current, true
}

// Alive reports whether the subscription connection is still healthy.
func (m *Monitor) Alive() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.dead
}

// Close terminates the subscription.
func (m *Monitor) Close() {
	if m == nil {
		return
	}
	m.closeOnce.Do(func() {
		close(m.done)
		_ = m.client.Close()
	})
}

func (m *Monitor) pump() {
	for {
		if m.isDead() {
			return
		}
		if !m.isAttached() {
			if !m.tryAttach() {
				select {
				case <-m.done:
					return
				case <-time.After(2 * time.Second):
				}
				continue
			}
		}
		select {
		case <-m.done:
			return
		case notification, ok := <-m.client.Notifications():
			if !ok {
				m.markDead()
				return
			}
			if notification.Method != notifThreadSettingsUpd {
				continue
			}
			m.applyNotification(notification.Params)
		}
	}
}

// tryAttach resolves the native thread and attaches via thread/resume,
// bootstrapping the snapshot from the resume response (the app server does
// not replay the applied-settings notification on attach). Returns false when
// the thread does not exist yet or the attach failed.
func (m *Monitor) tryAttach() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	threadID, err := m.client.ResolveThread(ctx, "")
	if err != nil {
		return false
	}
	settings, err := m.client.ResumeThread(ctx, threadID)
	if err != nil {
		return false
	}
	settings.ThreadID = threadID
	if settings.Model == "" {
		return false
	}
	m.mu.Lock()
	m.current = settings
	m.attached = true
	m.ready = true
	m.mu.Unlock()
	return true
}

func (m *Monitor) applyNotification(params json.RawMessage) {
	var payload struct {
		ThreadID string `json:"threadId"`
		Settings struct {
			Model  string `json:"model"`
			Effort string `json:"effort"`
		} `json:"threadSettings"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return
	}
	if strings.TrimSpace(payload.Settings.Model) == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Only the attached thread's applied settings are authoritative for this
	// route. A session-scoped socket can serve more than one loaded thread
	// (e.g. a side conversation), and another thread's settings notification
	// must never pollute this snapshot — nor retarget the snapshot's
	// ThreadID to the payload value. Empty or mismatched ThreadID: ignore
	// (fail closed; the snapshot stays as attached).
	//
	// Ordering/lock safety: tryAttach and applyNotification both run in the
	// single pump goroutine (attach completes before notifications are
	// consumed) and both serialize on m.mu, so the attached-ThreadID
	// comparison here is race-free.
	if !m.attached || payload.ThreadID != m.current.ThreadID {
		return
	}
	m.current = NativeSettings{
		ThreadID: m.current.ThreadID,
		Model:    strings.TrimSpace(payload.Settings.Model),
		Effort:   normalizeNativeEffort(payload.Settings.Effort),
	}
	m.ready = true
}

func (m *Monitor) isAttached() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.attached
}

func (m *Monitor) isDead() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dead
}

func (m *Monitor) markDead() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dead = true
	m.ready = false
}

// normalizeNativeEffort maps the native effort vocabulary to Zen's: the
// native model default ("none" / null / empty) is "".
func normalizeNativeEffort(effort string) string {
	effort = strings.ToLower(strings.TrimSpace(effort))
	if effort == "" || effort == "none" {
		return ""
	}
	return effort
}
