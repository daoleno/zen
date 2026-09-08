// Package desktop owns opt-in native desktop helpers, independently of chat.
package desktop

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const MaxFrame = 4 << 20

type Command struct {
	Events  []Command `json:"events,omitempty"`
	Type    string    `json:"type"`
	Source  string    `json:"source,omitempty"`
	Control bool      `json:"control,omitempty"`
	X       float64   `json:"x,omitempty"`
	Y       float64   `json:"y,omitempty"`
	Code    uint32    `json:"code,omitempty"`
	Down    bool      `json:"down,omitempty"`
	Delta   int       `json:"delta,omitempty"`
	Submit  bool      `json:"submit,omitempty"`
}

func (c Command) ValidateInput(control bool) error {
	if !control {
		return errors.New("view_only")
	}
	switch c.Type {
	case "text":
		if c.Code < 0x20 || c.Code > 0x7e {
			return errors.New("unsupported_text")
		}
	case "pointer":
		if math.IsNaN(c.X) || math.IsNaN(c.Y) || math.IsInf(c.X, 0) || math.IsInf(c.Y, 0) || c.X < 0 || c.X > 1 || c.Y < 0 || c.Y > 1 {
			return errors.New("invalid_pointer")
		}
	case "button":
		if c.Code < 1 || c.Code > 3 {
			return errors.New("invalid_button")
		}
	case "key":
		if !((c.Code >= 0x20 && c.Code <= 0x7e) || (c.Code >= 0xff08 && c.Code <= 0xffff)) {
			return errors.New("unsupported_key")
		}
	case "scroll":
		if c.Delta < -10 || c.Delta > 10 {
			return errors.New("invalid_scroll")
		}
	case "release":
	default:
		return errors.New("invalid_input")
	}
	return nil
}

// Manager admits one connection and helper, with device-specific revocation.
// A separate registry prevents media sockets receiving chat broadcasts.
type Manager struct {
	mu     sync.Mutex
	conn   *websocket.Conn
	input  *inputGate
	device string
	closed bool
	retire func()
}

func (m *Manager) Revoke(device string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn != nil && m.device == device {
		m.input.revoke()
		if m.retire != nil {
			m.retire()
		}
		_ = m.conn.Close()
	}
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	if m.conn != nil {
		m.input.revoke()
		if m.retire != nil {
			m.retire()
		}
		_ = m.conn.Close()
	}
}

// ServeExternal shares the existing one-connection owner with broker-backed
// sessions. The bound retirement barrier runs synchronously on revoke/shutdown.
func (m *Manager) ServeExternal(conn *websocket.Conn, device string, run func(func(func()))) {
	defer conn.Close()
	m.mu.Lock()
	if m.closed || m.conn != nil {
		m.mu.Unlock()
		return
	}
	retired := false
	m.conn, m.device, m.input = conn, device, &inputGate{}
	m.retire = func() { retired = true; _ = conn.Close() }
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.conn, m.device, m.input, m.retire = nil, "", nil, nil
		m.mu.Unlock()
	}()
	run(func(stop func()) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if retired || m.closed {
			stop()
			return
		}
		m.retire = func() { retired = true; stop(); _ = conn.Close() }
	})
}

func ReadPacket(r io.Reader) (byte, []byte, error) {
	var size uint32
	if err := binary.Read(r, binary.BigEndian, &size); err != nil {
		return 0, nil, err
	}
	if size < 2 || size > MaxFrame {
		return 0, nil, errors.New("invalid_packet_size")
	}
	packet := make([]byte, size)
	if _, err := io.ReadFull(r, packet); err != nil {
		return 0, nil, err
	}
	if packet[0] != 1 && packet[0] != 2 {
		return 0, nil, errors.New("invalid_packet_type")
	}
	return packet[0], packet[1:], nil
}

func (m *Manager) Serve(conn *websocket.Conn, device, name string, trusted func() bool) {
	defer conn.Close()
	m.mu.Lock()
	if m.closed || m.conn != nil {
		m.mu.Unlock()
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "desktop_busy"), time.Now().Add(time.Second))
		return
	}
	inputGate := &inputGate{}
	m.conn, m.device, m.input = conn, device, inputGate
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.conn, m.device, m.input = nil, "", nil; m.mu.Unlock() }()
	defer inputGate.revoke()
	if !trusted() {
		return
	}
	write := func(kind int, data []byte) error {
		_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		return conn.WriteMessage(kind, data)
	}
	status := func(state, reason string) {
		data, _ := json.Marshal(map[string]any{"version": 1, "state": state, "reason": reason})
		_ = write(websocket.TextMessage, data)
	}
	helper := os.Getenv("ZEN_DESKTOP_HELPER")
	if !filepath.IsAbs(helper) {
		status("unsupported", "Desktop sharing is not configured on this host.")
		return
	}
	if info, err := os.Stat(helper); err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		status("unsupported", "Desktop helper is unavailable.")
		return
	}
	source, sourceName, sourceArgs, err := configuredSource()
	if err != nil {
		status("unsupported", err.Error())
		return
	}
	inventory, _ := json.Marshal(map[string]any{"version": 1, "state": "sources", "sources": []map[string]any{{"id": source, "name": sourceName, "control": true}}})
	if err := write(websocket.TextMessage, inventory); err != nil {
		return
	}
	conn.SetReadLimit(8192)
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	var start Command
	for {
		if err := conn.ReadJSON(&start); err != nil {
			return
		}
		if start.Type != "ping" {
			break
		}
	}
	if start.Type != "start" || start.Source != source || !trusted() {
		status("disconnected", "Invalid desktop source or request.")
		return
	}
	args := append([]string{"--device", name}, sourceArgs...)
	if start.Control {
		args = append(args, "--control")
	}
	cmd := exec.Command(helper, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		status("disconnected", "Desktop helper could not start.")
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		status("disconnected", "Desktop helper could not start.")
		return
	}
	if err = inputGate.start(stdin, cmd.Start, trusted); err != nil {
		_ = stdin.Close()
		status("disconnected", "Desktop helper could not start.")
		return
	}
	// EOF requests graceful key release; Kill is only a bounded fallback.
	defer func() {
		inputGate.revoke()
		timer := time.AfterFunc(2*time.Second, func() { _ = cmd.Process.Kill() })
		_ = cmd.Wait()
		timer.Stop()
	}()
	done := make(chan struct{})
	var granted atomic.Bool
	consentTimeout := time.AfterFunc(60*time.Second, func() { _ = conn.Close() })
	defer consentTimeout.Stop()
	go func() {
		defer close(done)
		defer conn.Close()
		reader := bufio.NewReader(stdout)
		for {
			kind, data, err := ReadPacket(reader)
			if err != nil || !trusted() {
				return
			}
			wsKind := websocket.BinaryMessage
			if kind == 1 {
				var metadata struct {
					State string `json:"state"`
				}
				if json.Unmarshal(data, &metadata) != nil {
					return
				}
				if metadata.State == "streaming" {
					consentTimeout.Stop()
					granted.Store(true)
				}
				wsKind = websocket.TextMessage
			} else if !granted.Load() {
				return
			}
			if err = write(wsKind, data); err != nil {
				return
			}
		}
	}()
	defer func() { _ = conn.Close(); _ = stdout.Close(); <-done }()
	// One bounded stdin writer, no goroutine or queue per input message.
	for {
		_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
		var input Command
		if err := conn.ReadJSON(&input); err != nil || !trusted() {
			return
		}
		if input.Type == "stop" {
			return
		}
		if input.Type == "ping" {
			continue
		}
		if !granted.Load() {
			return
		}
		events := []Command{input}
		if input.Type == "batch" {
			if len(input.Events) == 0 || len(input.Events) > 64 {
				return
			}
			events = input.Events
		}
		for _, event := range events {
			if err := event.ValidateInput(start.Control); err != nil {
				return
			}
		}
		for _, event := range events {
			record := fmt.Sprintf("%s %.9f %.9f %d %t %d\n", event.Type, event.X, event.Y, event.Code, event.Down, event.Delta)
			if err := inputGate.write([]byte(record), trusted); err != nil {
				return
			}
		}
	}
}
