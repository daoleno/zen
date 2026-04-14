package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type pairRequest struct {
	EnrollmentToken   string `json:"enrollment_token"`
	ExpectedDaemonID  string `json:"expected_daemon_id,omitempty"`
	ExpectedPublicKey string `json:"expected_daemon_public_key"`
	DeviceID          string `json:"device_id"`
	DeviceName        string `json:"device_name"`
	DevicePublicKey   string `json:"device_public_key"`
}

type wsEnvelope struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

func main() {
	serverURL := flag.String("server-url", "ws://127.0.0.1:9876/ws", "daemon websocket URL")
	daemonID := flag.String("daemon-id", "", "daemon identity hex")
	daemonPublicKey := flag.String("daemon-public-key", "", "daemon public key hex")
	enrollmentToken := flag.String("enrollment-token", "", "pairing enrollment token hex")
	cwd := flag.String("cwd", "", "session working directory")
	command := flag.String("command", "", "session command")
	name := flag.String("name", "colorcheck", "session display name")
	setup := flag.String("setup", "", "terminal input to send after opening the tmux backend")
	cols := flag.Int("cols", 120, "terminal columns for setup attach")
	rows := flag.Int("rows", 36, "terminal rows for setup attach")
	flag.Parse()

	if strings.TrimSpace(*daemonID) == "" || strings.TrimSpace(*daemonPublicKey) == "" || strings.TrimSpace(*enrollmentToken) == "" {
		fail("daemon-id, daemon-public-key, and enrollment-token are required")
	}

	deviceID := fmt.Sprintf("debug-%d", time.Now().UnixNano())
	deviceName := "zen-debug-client"
	publicKeyHex, signer := newSigner()

	if err := pair(*serverURL, pairRequest{
		EnrollmentToken:   strings.TrimSpace(*enrollmentToken),
		ExpectedDaemonID:  strings.TrimSpace(*daemonID),
		ExpectedPublicKey: strings.TrimSpace(*daemonPublicKey),
		DeviceID:          deviceID,
		DeviceName:        deviceName,
		DevicePublicKey:   publicKeyHex,
	}); err != nil {
		fail("pair device: %v", err)
	}

	header, err := buildAuthorizationHeader(strings.TrimSpace(*daemonID), deviceID, signer)
	if err != nil {
		fail("build auth header: %v", err)
	}

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, resp, err := dialer.Dial(*serverURL, http.Header{"Authorization": []string{header}})
	if err != nil {
		if resp != nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			fail("connect websocket: %v (%s)", err, strings.TrimSpace(string(body)))
		}
		fail("connect websocket: %v", err)
	}
	defer conn.Close()

	requestID := fmt.Sprintf("create_%d", time.Now().UnixNano())
	if err := conn.WriteJSON(map[string]any{
		"type":       "create_session",
		"request_id": requestID,
		"cwd":        *cwd,
		"command":    *command,
		"name":       *name,
	}); err != nil {
		fail("send create_session: %v", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for {
		if err := conn.SetReadDeadline(deadline); err != nil {
			fail("set read deadline: %v", err)
		}

		var env wsEnvelope
		if err := conn.ReadJSON(&env); err != nil {
			fail("wait for session_created: %v", err)
		}

		switch env.Type {
		case "session_created":
			if env.RequestID == requestID {
				if strings.TrimSpace(*setup) != "" {
					if err := openAndSeedTerminal(conn, env.AgentID, *setup, *cols, *rows); err != nil {
						fail("seed terminal: %v", err)
					}
				}
				fmt.Println(env.AgentID)
				return
			}
		case "error":
			if env.RequestID == requestID {
				fail("create session failed: %s", env.Message)
			}
		}
	}
}

func openAndSeedTerminal(conn *websocket.Conn, agentID, setup string, cols, rows int) error {
	requestID := fmt.Sprintf("open_%d", time.Now().UnixNano())
	if err := conn.WriteJSON(map[string]any{
		"type":       "terminal_open",
		"request_id": requestID,
		"target_id":  agentID,
		"backend":    "tmux",
		"cols":       cols,
		"rows":       rows,
	}); err != nil {
		return fmt.Errorf("send terminal_open: %w", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for {
		if err := conn.SetReadDeadline(deadline); err != nil {
			return fmt.Errorf("set read deadline: %w", err)
		}

		var env wsEnvelope
		if err := conn.ReadJSON(&env); err != nil {
			return fmt.Errorf("wait for terminal_opened: %w", err)
		}

		switch env.Type {
		case "terminal_opened":
			if strings.TrimSpace(env.SessionID) == "" {
				return fmt.Errorf("terminal opened without session id")
			}
			if err := conn.WriteJSON(map[string]any{
				"type":       "terminal_input",
				"session_id": env.SessionID,
				"data":       setup,
			}); err != nil {
				return fmt.Errorf("send terminal_input: %w", err)
			}
			time.Sleep(1200 * time.Millisecond)
			return nil
		case "terminal_error":
			return fmt.Errorf("terminal open failed: %s", env.Message)
		case "error":
			if env.RequestID == requestID {
				return fmt.Errorf("open terminal failed: %s", env.Message)
			}
		}
	}
}

func pair(serverURL string, payload pairRequest) error {
	httpURL, err := toHTTPURL(serverURL, "/pair")
	if err != nil {
		return err
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode pair request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, httpURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build pair request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send pair request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pair returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

func toHTTPURL(raw, path string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("parse server url: %w", err)
	}
	switch parsed.Scheme {
	case "ws":
		parsed.Scheme = "http"
	case "wss":
		parsed.Scheme = "https"
	case "http", "https":
	default:
		return "", fmt.Errorf("unsupported scheme: %s", parsed.Scheme)
	}
	parsed.Path = path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func newSigner() (string, ed25519.PrivateKey) {
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		fail("generate seed: %v", err)
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	return hex.EncodeToString(publicKey), privateKey
}

func buildAuthorizationHeader(daemonID, deviceID string, signer ed25519.PrivateKey) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	timestamp := strconvFormatInt(time.Now().UnixMilli())
	nonceHex := hex.EncodeToString(nonce)
	payload := strings.Join([]string{
		"zen-connect",
		daemonID,
		deviceID,
		timestamp,
		nonceHex,
	}, "\n")
	signature := ed25519.Sign(signer, []byte(payload))
	return fmt.Sprintf(
		"ZenDevice v1:%s:%s:%s:%s:%s",
		deviceID,
		daemonID,
		timestamp,
		nonceHex,
		hex.EncodeToString(signature),
	), nil
}

func strconvFormatInt(value int64) string {
	return fmt.Sprintf("%d", value)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
