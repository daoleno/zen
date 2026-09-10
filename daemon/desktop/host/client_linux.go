package host

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/desktop"
	"github.com/gorilla/websocket"
)

type brokerAgent struct {
	mu      sync.Mutex
	control *net.UnixConn
	media   net.Conn
	done    chan struct{}
	closed  bool
	session Session
}

func (a *brokerAgent) Write(event desktop.Command) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return errors.New("agent_closed")
	}
	_ = a.media.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err := fmt.Fprintf(a.media, "%s %.9f %.9f %d %t %d\n", event.Type, event.X, event.Y, event.Code, event.Down, event.Delta)
	return err
}

func (a *brokerAgent) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil
	}
	a.closed = true
	close(a.done)
	_ = a.media.Close()
	_ = SendCapability(a.control, []byte("stop"), nil)
	_ = a.control.SetReadDeadline(time.Now().Add(4 * time.Second))
	var data [1]byte
	_, err := a.control.Read(data[:])
	_ = a.control.Close()
	if err != io.EOF {
		return errors.New("broker_retirement_unconfirmed")
	}
	return nil
}

func (a *brokerAgent) heartbeat() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.done:
			return
		case <-ticker.C:
			a.mu.Lock()
			if a.closed {
				a.mu.Unlock()
				return
			}
			err := SendCapability(a.control, []byte("alive"), nil)
			a.mu.Unlock()
			if err != nil {
				_ = a.media.Close()
				return
			}
		}
	}
}

func openBroker(manager *auth.Manager, device *auth.TrustedDevice, gate *Gate) (*brokerAgent, Request, error) {
	conn, err := net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: OwnerSocket, Net: "unixpacket"})
	if err != nil {
		return nil, Request{}, errors.New("broker_unavailable")
	}
	transferred := false
	defer func() {
		if !transferred {
			conn.Close()
		}
	}()
	if AuthenticateLocalPeer(conn, 0) != nil {
		return nil, Request{}, errors.New("invalid_broker_peer")
	}
	if SendCapability(conn, []byte("hello"), nil) != nil {
		return nil, Request{}, errors.New("broker_unavailable")
	}
	data, _, err := ReceiveCapability(conn, 0, false)
	var challenge Challenge
	if err != nil || decodeMessage(data, &challenge) != nil || len(challenge.Nonce) != 64 || challenge.Generation == 0 {
		return nil, Request{}, errors.New("session_unavailable")
	}
	key, err := hex.DecodeString(device.PublicKeyHex)
	if err != nil || !manager.HasDesktopScope(device.ID, device.PublicKeyHex) {
		return nil, Request{}, errors.New("desktop_scope_required")
	}
	request := Request{HostID: manager.DaemonID(), DeviceID: device.ID, Fingerprint: sha256.Sum256(key), ConnectionID: challenge.Nonce, Generation: challenge.Generation, Mode: Unattended, Control: true, TLS: true}
	admission := Admission{Challenge: challenge, Request: request}
	payload, _ := json.Marshal(admission)
	proof := signedAdmission{Admission: admission, PublicKey: manager.PublicKeyHex(), Signature: manager.SignDesktopHostAdmission(payload)}
	if sendJSON(conn, proof, nil) != nil {
		return nil, Request{}, errors.New("broker_admission_failed")
	}
	data, fd, err := ReceiveCapability(conn, 0, true)
	if err != nil {
		return nil, Request{}, errors.New("broker_admission_failed")
	}
	defer fd.Close()
	var confirmed Challenge
	if decodeMessage(data, &confirmed) != nil || confirmed != challenge {
		return nil, Request{}, errors.New("stale_broker_generation")
	}
	media, err := net.FileConn(fd)
	if err != nil {
		return nil, Request{}, errors.New("invalid_agent_channel")
	}
	// The Gate owns a local generation, while the signed admission used the
	// broker generation. Both are immutable for the lifetime of this connection.
	request.Generation = gate.Observe(challenge.Session)
	agent := &brokerAgent{control: conn, media: media, done: make(chan struct{}), session: challenge.Session}
	transferred = true
	go agent.heartbeat()
	return agent, request, nil
}

// ServeUnattended consumes a freshly authenticated /desktop request. The caller
// must pass actual Request.TLS provenance, never a proxy or JSON assertion.
func ServeUnattended(conn *websocket.Conn, manager *auth.Manager, device *auth.TrustedDevice, tls bool, bindRetirement func(func())) {
	defer conn.Close()
	status := func(state, reason string) {
		_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_ = conn.WriteJSON(map[string]any{"state": state, "reason": reason})
	}
	if !tls || !manager.HasDesktopScope(device.ID, device.PublicKeyHex) {
		status("denied", "Encrypted transport and expanded pairing scope are required.")
		return
	}
	gate, cleanup, err := NewManagedGate(HostConfig{Version: 1, HostID: manager.DaemonID(), OwnerUID: uint32(os.Getuid()), Seat: "seat0"}, manager)
	if err != nil {
		status("unsupported", "The desktop owner is unavailable.")
		return
	}
	defer cleanup()
	bindRetirement(func() { gate.Deny(true); _ = conn.Close() })
	agent, request, err := openBroker(manager, device, gate)
	if err != nil {
		if err.Error() == "session_unavailable" || err.Error() == "broker_admission_failed" {
			status("disconnected", "The current desktop session is unavailable.")
		} else {
			status("unsupported", "The unattended host service is unavailable.")
		}
		return
	}
	lease, err := gate.Open(request, nil, agent, time.Now())
	if err != nil {
		agent.Close()
		status("denied", "Desktop authorization ended.")
		return
	}
	defer gate.Disconnect(lease)
	conn.SetReadLimit(8192)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		defer conn.Close()
		for {
			_ = agent.media.SetReadDeadline(time.Now().Add(10 * time.Second))
			kind, data, err := desktop.ReadPacket(agent.media)
			if err != nil {
				return
			}
			if gate.Forward(lease, kind, data, func(kind byte, data []byte) error {
				_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
				messageType := websocket.BinaryMessage
				if kind == 1 {
					messageType = websocket.TextMessage
					var metadata map[string]any
					if json.Unmarshal(data, &metadata) != nil || metadata == nil {
						return errors.New("invalid_agent_metadata")
					}
					metadata["surface"] = agent.session.Surface
					data, _ = json.Marshal(metadata)
				}
				return conn.WriteMessage(messageType, data)
			}) != nil {
				return
			}
		}
	}()
	defer func() { conn.Close(); gate.Disconnect(lease); <-finished }()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
		var command desktop.Command
		if conn.ReadJSON(&command) != nil {
			return
		}
		if command.Type == "stop" {
			return
		}
		if command.Type == "ping" {
			continue
		}
		events := []desktop.Command{command}
		if command.Type == "batch" || command.Type == "sensitive" {
			if len(command.Events) == 0 || len(command.Events) > 64 {
				return
			}
			events = command.Events
		}
		for _, event := range events {
			if event.ValidateInput(true) != nil || (command.Type == "sensitive" && event.Type != "text") {
				return
			}
			if agent.session.Surface != Desktop && command.Type != "sensitive" && (event.Type == "text" || (event.Type == "key" && event.Code < 0xff08)) {
				return
			}
		}
		for _, event := range events {
			if gate.Input(lease, event) != nil {
				return
			}
		}
		if command.Type == "sensitive" && command.Submit {
			for _, down := range []bool{true, false} {
				if gate.Input(lease, desktop.Command{Type: "key", Code: 0xff0d, Down: down}) != nil {
					return
				}
			}
		}
	}
}
