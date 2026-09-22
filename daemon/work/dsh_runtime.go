package work

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var dshSessionIDPattern = regexp.MustCompile(`^session-[a-zA-Z0-9-]{1,100}$`)

func DSHRoot() string {
	home, _ := os.UserHomeDir()
	root := os.Getenv("ZEN_STATE_DIR")
	if root == "" {
		root = filepath.Join(home, ".zen")
	}
	return filepath.Join(root, "provider-sessions", "dsh")
}

func DSHSessionID(command string) string {
	options, ok := inspectLaunchCommandOptions(command)
	if !ok || len(options.argv) == 0 || options.argv[0] != "dsh-session" || !strings.HasPrefix(filepath.Base(options.executable), "zen") && !strings.HasSuffix(options.executable, ".test") {
		return ""
	}
	_, id := options.optionValue("--dsh-session")
	if !dshSessionIDPattern.MatchString(id) {
		return ""
	}
	return id
}

func EnsureDSHSessionLaunchCommand(command string) (string, error) {
	if InferWorkerProvider(command) != WorkerProviderDSH {
		return command, nil
	}
	if DSHSessionID(command) != "" {
		return command, nil
	}
	options, ok := inspectLaunchCommandOptions(command)
	if !ok || len(options.argv) != 0 {
		return "", fmt.Errorf("DSH launch uses native saved settings; use the Session model picker after launch")
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return shellQuoteForLaunch(executable) + " dsh-session --dsh-session " + "session-" + uuid.NewString(), nil
}

func dshSocket(id string) (string, error) {
	if !dshSessionIDPattern.MatchString(id) {
		return "", fmt.Errorf("invalid DSH Session identity")
	}
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(DSHRoot(), fmt.Sprintf("%x.sock", sum[:8])), nil
}

// CallDSH only reaches the private socket of the explicitly owned Session.
func CallDSH(ctx context.Context, id, method string, payload any, result any) error {
	socket, err := dshSocket(id)
	if err != nil {
		return err
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://dsh/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	response, err := (&http.Client{Transport: transport, Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("DSH Session is not ready: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("DSH: %s", strings.TrimSpace(string(message)))
	}
	if result == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(result)
}

func callDSHNative(ctx context.Context, address, method string, payload any, result any) error {
	id := uuid.NewString()
	body, err := json.Marshal(map[string]any{"type": "client-request", "rpcId": id, "method": method, "payload": payload})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address+"/api/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var envelope struct {
		RPCID  string `json:"rpcId"`
		Result struct {
			OK    bool            `json:"ok"`
			Value json.RawMessage `json:"value"`
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(&envelope); err != nil {
		return err
	}
	if envelope.RPCID != id || !envelope.Result.OK {
		return fmt.Errorf("%s: %s", envelope.Result.Error.Code, envelope.Result.Error.Message)
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(envelope.Result.Value, result)
}

// RunDSHSession owns one native web-profile subprocess and one session socket.
// The mobile app never imports the harness runtime; the native API owns its logs/settings.
func RunDSHSession(ctx context.Context, id, cwd string) error {
	socket, err := dshSocket(id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(DSHRoot(), 0700); err != nil {
		return err
	}
	unlock, err := lockDSHSession(filepath.Join(DSHRoot(), id+".lock"))
	if err != nil {
		return fmt.Errorf("DSH Session already has an owner: %w", err)
	}
	defer unlock()
	// Listen fails closed if a previous owner is still present. Stale sockets are
	// removed only after a failed connect; two concurrent starts still race on Listen.
	if connection, err := net.DialTimeout("unix", socket, 100*time.Millisecond); err == nil {
		connection.Close()
		return fmt.Errorf("DSH Session already has a live owner")
	}
	_ = os.Remove(socket)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(socket)
	if err := os.Chmod(socket, 0600); err != nil {
		return err
	}
	reserve, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := reserve.Addr().(*net.TCPAddr).Port
	reserve.Close()
	// Keep native persistence under Zen's provider-session owner; do not edit DSH
	// user profiles, credentials, defaults, or existing Sessions.
	patchPath := filepath.Join(DSHRoot(), id+".patch.yml")
	patch := fmt.Sprintf("- id: session-persistence-jsonl\n  config:\n    root: %q\n", filepath.Join(DSHRoot(), "logs"))
	if err := os.WriteFile(patchPath, []byte(patch), 0600); err != nil {
		return err
	}
	defer os.Remove(patchPath)
	command := exec.Command("dsh", "--profile", "web", "--patch", patchPath, "--host", "127.0.0.1", "--port", fmt.Sprint(port))
	command.Dir = cwd
	bindDSHProcess(command)
	log, err := os.OpenFile(filepath.Join(DSHRoot(), id+".log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	command.Stdout = log
	command.Stderr = log
	if err := command.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()
	defer func() {
		_ = command.Process.Signal(os.Interrupt)
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			_ = command.Process.Kill()
			<-exited
		}
	}()
	address := fmt.Sprintf("http://127.0.0.1:%d", port)
	ready := false
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
		var created struct {
			SessionID string `json:"sessionId"`
		}
		if err := callDSHNative(ctx, address, "session.create", map[string]any{"sessionId": id, "cwd": cwd}, &created); err == nil && created.SessionID == id {
			ready = true
			break
		}
		select {
		case err := <-exited:
			exited <- err
			return fmt.Errorf("DSH exited during startup; inspect its private Session log: %v", err)
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	if !ready {
		return fmt.Errorf("DSH startup timed out; inspect its private Session log")
	}
	interactions := newDSHInteractionOwner(id)
	interactionContext, stopInteractions := context.WithCancel(ctx)
	defer stopInteractions()
	go interactions.run(interactionContext, address)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		method := strings.TrimPrefix(r.URL.Path, "/")
		switch method {
		case "session.interactions", "session.respond", "session.prompt", "session.cancel", "session.history", "session.models", "session.selectModel", "session.attachment":
		default:
			http.Error(w, "unsupported DSH operation", 400)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<20)).Decode(&payload); err != nil {
			http.Error(w, "invalid DSH request", 400)
			return
		}
		if value, ok := payload["sessionId"]; ok && value != id {
			http.Error(w, "wrong Session owner", 403)
			return
		}
		payload["sessionId"] = id
		if method == "session.interactions" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(interactions.snapshot())
			return
		}
		if method == "session.respond" {
			if err := interactions.answer(r.Context(), address, payload); err != nil {
				http.Error(w, err.Error(), 409)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]bool{"accepted": true})
			return
		}

		var result json.RawMessage
		if err := callDSHNative(r.Context(), address, method, payload, &result); err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(result)
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener)
	defer server.Close()
	fmt.Fprintln(os.Stdout, "DSH ready. Native saved model/settings apply.")
	return runDSHTerminal(ctx, id, exited)
}
