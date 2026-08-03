package linkproto

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	CurrentVersion = 2
	ControlALPN    = "zen-link-control/2"
	MaxFrameBytes  = 64 << 10

	TypeRegister          = "register"
	TypeRegistered        = "registered"
	TypePing              = "ping"
	TypePong              = "pong"
	TypeOpenStream        = "open_stream"
	TypeAttachStream      = "attach_stream"
	TypeAttached          = "attached"
	TypeAdmissionRequest  = "admission_request"
	TypeAdmissionResponse = "admission_response"
	TypeAdmissionConsume  = "admission_consume"
	TypeAdmissionConsumed = "admission_consumed"
	TypeError             = "error"
)

type Message struct {
	Version int    `json:"version"`
	Type    string `json:"type"`

	ConnectorToken  string `json:"connector_token,omitempty"`
	RouteID         string `json:"route_id,omitempty"`
	DaemonID        string `json:"daemon_id,omitempty"`
	DaemonPublicKey string `json:"daemon_public_key,omitempty"`
	TimestampMS     int64  `json:"timestamp_ms,omitempty"`
	Nonce           string `json:"nonce,omitempty"`
	Signature       string `json:"signature,omitempty"`

	StreamID     string `json:"stream_id,omitempty"`
	StreamTicket string `json:"stream_ticket,omitempty"`

	Alias       string `json:"alias,omitempty"`
	ExpiresAtMS int64  `json:"expires_at_ms,omitempty"`
	TTLSeconds  int64  `json:"ttl_seconds,omitempty"`

	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

func ReadMessage(reader io.Reader) (Message, error) {
	var sizeBytes [4]byte
	if _, err := io.ReadFull(reader, sizeBytes[:]); err != nil {
		return Message{}, err
	}
	size := binary.BigEndian.Uint32(sizeBytes[:])
	if size == 0 || size > MaxFrameBytes {
		return Message{}, fmt.Errorf("invalid Link frame size %d", size)
	}
	raw := make([]byte, size)
	if _, err := io.ReadFull(reader, raw); err != nil {
		return Message{}, err
	}
	var message Message
	if err := json.Unmarshal(raw, &message); err != nil {
		return Message{}, fmt.Errorf("decode Link frame: %w", err)
	}
	if message.Version != CurrentVersion || strings.TrimSpace(message.Type) == "" {
		return Message{}, errors.New("unsupported Link protocol version or message type")
	}
	return message, nil
}

func WriteMessage(writer io.Writer, message Message) error {
	message.Version = CurrentVersion
	raw, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode Link frame: %w", err)
	}
	if len(raw) == 0 || len(raw) > MaxFrameBytes {
		return fmt.Errorf("Link frame size %d exceeds limit", len(raw))
	}
	var sizeBytes [4]byte
	binary.BigEndian.PutUint32(sizeBytes[:], uint32(len(raw)))
	if err := writeAll(writer, sizeBytes[:]); err != nil {
		return err
	}
	return writeAll(writer, raw)
}

func SignaturePayload(message Message) []byte {
	return []byte(strings.Join([]string{
		strings.TrimSpace(message.Type),
		strings.ToLower(strings.TrimSpace(message.RouteID)),
		strings.ToLower(strings.TrimSpace(message.DaemonID)),
		strings.ToLower(strings.TrimSpace(message.DaemonPublicKey)),
		fmt.Sprintf("%d", message.TimestampMS),
		strings.ToLower(strings.TrimSpace(message.Nonce)),
		fmt.Sprintf("%d", message.TTLSeconds),
		strings.ToLower(strings.TrimSpace(message.Alias)),
		strings.ToLower(strings.TrimSpace(message.StreamID)),
	}, "\n"))
}

func RandomID(bytes int) (string, error) {
	if bytes <= 0 {
		return "", errors.New("random id size must be positive")
	}
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func IsOpaqueID(value string, bytes int) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if len(normalized) != bytes*2 {
		return false
	}
	decoded, err := hex.DecodeString(normalized)
	return err == nil && len(decoded) == bytes
}

func writeAll(writer io.Writer, raw []byte) error {
	for len(raw) > 0 {
		written, err := writer.Write(raw)
		if err != nil {
			return err
		}
		if written <= 0 {
			return io.ErrUnexpectedEOF
		}
		raw = raw[written:]
	}
	return nil
}
