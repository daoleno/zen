package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/work"
)

const defaultListenAddr = "127.0.0.1:9876"

// Options configures a doctor run. All fields are optional; defaults match the
// production daemon. Tests inject LookPath/RunCommand/HTTPGet/Listen for
// deterministic PATH/home isolation.
type Options struct {
	StateDir      string
	Addr          string
	ExecutorsPath string
	Home          string
	PathEnv       string
	Now           func() time.Time
	LookPath      func(file string) (string, error)
	RunCommand    func(ctx context.Context, name string, args ...string) ([]byte, error)
	HTTPGet       func(ctx context.Context, url string) (status int, body []byte, err error)
	Listen        func(network, address string) (io.Closer, error)
	ProbeTimeout  time.Duration
}

type env struct {
	opts Options
}

// Run performs a full non-destructive environment diagnosis.
func Run(opts Options) (Report, error) {
	e := env{opts: opts}
	if e.opts.Now == nil {
		e.opts.Now = time.Now
	}
	if e.opts.Addr == "" {
		e.opts.Addr = defaultListenAddr
	}
	if e.opts.ProbeTimeout <= 0 {
		e.opts.ProbeTimeout = 3 * time.Second
	}
	if e.opts.LookPath == nil {
		e.opts.LookPath = e.defaultLookPath
	}
	if e.opts.RunCommand == nil {
		e.opts.RunCommand = e.defaultRunCommand
	}
	if e.opts.HTTPGet == nil {
		e.opts.HTTPGet = e.defaultHTTPGet
	}
	if e.opts.Listen == nil {
		e.opts.Listen = func(network, address string) (io.Closer, error) {
			return net.Listen(network, address)
		}
	}

	report := Report{
		GeneratedAt: e.opts.Now().UTC(),
	}
	report.Platform = e.checkPlatform()
	report.Tmux = e.checkTmux()
	report.StateDir = e.checkStateDir()
	report.Listen = e.checkListen(report.StateDir.Path)
	report.Executors = e.checkExecutors()

	report.Checks = []NamedCheck{
		{ID: "platform", Status: report.Platform.Status, Remediation: report.Platform.Remediation, Summary: report.Platform.Summary},
		{ID: "tmux", Status: report.Tmux.Status, Remediation: report.Tmux.Remediation, Summary: report.Tmux.Summary},
		{ID: "state_dir", Status: report.StateDir.Status, Remediation: report.StateDir.Remediation, Summary: report.StateDir.Summary},
		{ID: "listen", Status: report.Listen.Status, Remediation: report.Listen.Remediation, Summary: report.Listen.Summary},
		{ID: "executors", Status: report.Executors.Status, Remediation: report.Executors.Remediation, Summary: report.Executors.Summary},
	}
	report.Remediations = collectRemediations(report)
	report.Warnings = collectWarnings(report)
	report.Ready = isReady(report)
	return report, nil
}

func isReady(report Report) bool {
	if report.Platform.Status != StatusOK {
		return false
	}
	if report.Tmux.Status != StatusOK {
		return false
	}
	if report.StateDir.Status != StatusOK {
		return false
	}
	if report.Listen.Status != StatusOK {
		return false
	}
	// StatusWarn is allowed: auth-unknown runnable candidates still satisfy readiness.
	return report.Executors.UsableCount > 0 && report.Executors.Status != StatusFail
}

func collectWarnings(report Report) []string {
	seen := map[string]bool{}
	var out []string
	add := func(msg string) {
		msg = strings.TrimSpace(msg)
		if msg == "" || seen[msg] {
			return
		}
		seen[msg] = true
		out = append(out, msg)
	}
	for _, msg := range report.Executors.Warnings {
		add(msg)
	}
	if report.Executors.Status == StatusWarn {
		add(report.Executors.Summary)
	}
	for _, item := range report.Executors.Items {
		if item.Status == StatusWarn && item.Summary != "" {
			add(item.Summary)
		}
	}
	return out
}

func collectRemediations(report Report) []Remediation {
	seen := map[Remediation]bool{}
	var out []Remediation
	add := func(code Remediation) {
		if code == "" || seen[code] {
			return
		}
		seen[code] = true
		out = append(out, code)
	}
	add(report.Platform.Remediation)
	add(report.Tmux.Remediation)
	add(report.StateDir.Remediation)
	add(report.Listen.Remediation)
	add(report.Executors.Remediation)
	for _, item := range report.Executors.Items {
		add(item.Remediation)
	}
	return out
}

func (e env) checkPlatform() PlatformCheck {
	check := PlatformCheck{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}
	switch runtime.GOOS {
	case "linux", "darwin":
		check.Supported = true
		check.Status = StatusOK
		check.Summary = fmt.Sprintf("%s/%s is supported", check.OS, check.Arch)
	default:
		check.Supported = false
		check.Status = StatusFail
		check.Remediation = RemediationUnsupportedPlatform
		check.Summary = fmt.Sprintf("%s/%s is not supported; Zen requires Linux or macOS", check.OS, check.Arch)
	}
	return check
}

func (e env) checkTmux() TmuxCheck {
	check := TmuxCheck{
		InstallHints: TmuxInstallHints(),
	}
	path, err := e.opts.LookPath("tmux")
	if err != nil || strings.TrimSpace(path) == "" {
		check.Status = StatusFail
		check.Remediation = RemediationInstallTmux
		check.Summary = "tmux not found on PATH"
		return check
	}
	check.Found = true
	check.Path = path

	ctx, cancel := context.WithTimeout(context.Background(), e.opts.ProbeTimeout)
	defer cancel()
	out, err := e.opts.RunCommand(ctx, path, "-V")
	if err == nil {
		check.Version = normalizeVersion(string(out))
	}

	if e.probeTmux(path) {
		check.Functional = true
		check.Status = StatusOK
		if check.Version != "" {
			check.Summary = fmt.Sprintf("tmux %s works", check.Version)
		} else {
			check.Summary = "tmux works"
		}
		return check
	}

	check.Status = StatusFail
	check.Remediation = RemediationFixTmux
	check.Summary = "tmux found but functional probe failed"
	return check
}

func (e env) probeTmux(tmuxPath string) bool {
	name := fmt.Sprintf("zen-doctor-%d", e.opts.Now().UnixNano()%1_000_000_000)
	ctx, cancel := context.WithTimeout(context.Background(), e.opts.ProbeTimeout)
	defer cancel()

	if _, err := e.opts.RunCommand(ctx, tmuxPath, "new-session", "-d", "-s", name, "true"); err != nil {
		return false
	}
	_, _ = e.opts.RunCommand(ctx, tmuxPath, "kill-session", "-t", name)
	return true
}

func (e env) checkStateDir() StateDirCheck {
	path, created, err := e.resolveStateDir()
	check := StateDirCheck{Path: path, Created: created}
	if err != nil {
		check.Status = StatusFail
		check.Remediation = RemediationStateDirUnwritable
		check.Summary = "state directory is not writable"
		return check
	}
	probe := filepath.Join(path, ".zen-doctor-write-probe")
	if writeErr := os.WriteFile(probe, []byte("ok"), 0o600); writeErr != nil {
		check.Status = StatusFail
		check.Remediation = RemediationStateDirUnwritable
		check.Summary = "state directory is not writable"
		return check
	}
	_ = os.Remove(probe)
	check.Writable = true
	check.Status = StatusOK
	check.Summary = fmt.Sprintf("state directory ready at %s", path)
	return check
}

func (e env) resolveStateDir() (string, bool, error) {
	dir := strings.TrimSpace(e.opts.StateDir)
	if dir == "" {
		if home := strings.TrimSpace(e.opts.Home); home != "" {
			dir = filepath.Join(home, ".zen")
		} else {
			var err error
			dir, err = auth.DefaultStorageDir()
			if err != nil {
				return "", false, err
			}
		}
	}
	_, err := os.Stat(dir)
	created := errors.Is(err, os.ErrNotExist)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return dir, created, err
	}
	return dir, created, nil
}

func (e env) checkListen(stateDir string) ListenCheck {
	check := ListenCheck{Addr: e.opts.Addr}
	if daemonID, ok := e.detectRunningZen(stateDir); ok {
		check.ZenRunning = true
		check.DaemonID = daemonID
		check.Available = false
		check.Status = StatusOK
		check.Summary = "Zen daemon already running"
		return check
	}

	closer, err := e.opts.Listen("tcp", e.opts.Addr)
	if err == nil {
		_ = closer.Close()
		check.Available = true
		check.Status = StatusOK
		check.Summary = fmt.Sprintf("listen address %s is available", e.opts.Addr)
		return check
	}

	check.Available = false
	check.Status = StatusFail
	check.Remediation = RemediationPortInUse
	check.Summary = fmt.Sprintf("listen address %s is in use by a non-Zen process", e.opts.Addr)
	return check
}

func (e env) detectRunningZen(stateDir string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), e.opts.ProbeTimeout)
	defer cancel()

	url := "http://" + e.opts.Addr + "/health"
	status, body, err := e.opts.HTTPGet(ctx, url)
	if err == nil && status == http.StatusOK {
		var payload struct {
			Status   string `json:"status"`
			DaemonID string `json:"daemon_id"`
		}
		if json.Unmarshal(body, &payload) == nil && payload.Status == "ok" && strings.TrimSpace(payload.DaemonID) != "" {
			return payload.DaemonID, true
		}
	}

	socketPath, err := control.DefaultSocketPath(stateDir)
	if err != nil {
		return "", false
	}
	if _, err := os.Stat(socketPath); err != nil {
		return "", false
	}
	resp, err := control.Call(socketPath, control.Request{Type: "agent_list"})
	if err == nil && resp.OK {
		return "", true
	}
	return "", false
}

func (e env) checkExecutors() ExecutorsCheck {
	path := strings.TrimSpace(e.opts.ExecutorsPath)
	if path == "" {
		home := strings.TrimSpace(e.opts.Home)
		if home == "" {
			var err error
			home, err = os.UserHomeDir()
			if err != nil {
				home = ""
			}
		}
		if home != "" {
			path = filepath.Join(home, ".zen", "executors.toml")
		} else if p, err := work.DefaultExecutorsPath(); err == nil {
			path = p
		}
	}

	check := ExecutorsCheck{ConfigPath: path}
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			check.ConfigExists = true
		}
	}

	cfg, err := work.LoadExecutors(path)
	if err != nil {
		check.Status = StatusFail
		check.Remediation = RemediationConfigureExecutor
		check.Summary = "failed to load executors.toml"
		return check
	}
	check.DelegatedExecutor = cfg.GetDelegatedExecutor()

	for _, name := range cfg.Roles() {
		executor := cfg.ByName[name]
		check.Items = append(check.Items, e.probeExecutor(name, executor))
	}

	var usable []ExecutorCheck
	for _, item := range check.Items {
		if item.Usable {
			usable = append(usable, item)
			check.UsableCount++
		}
		if item.VerifiedAuthenticated {
			check.VerifiedCount++
		}
	}

	verified := filterByAuth(usable, AuthAuthenticated)
	unknown := filterByAuth(usable, AuthUnknown)
	recommendFrom := verified
	check.RecommendationConfidence = ConfidenceVerified
	if len(recommendFrom) == 0 {
		recommendFrom = unknown
		if len(recommendFrom) > 0 {
			check.RecommendationConfidence = ConfidenceUnverified
		} else {
			check.RecommendationConfidence = ConfidenceNone
		}
	}
	check.RecommendedHost = recommendHost(recommendFrom)
	check.RecommendedDelegated = recommendDelegated(recommendFrom, cfg.GetDelegatedExecutor(), check.RecommendedHost)

	switch {
	case check.VerifiedCount > 0:
		check.Status = StatusOK
		check.Summary = fmt.Sprintf("%d usable executor(s), %d verified; host=%s delegated=%s", check.UsableCount, check.VerifiedCount, check.RecommendedHost, check.RecommendedDelegated)
	case check.UsableCount > 0:
		check.Status = StatusWarn
		check.Summary = fmt.Sprintf("%d runnable executor(s) with unverified auth; host=%s delegated=%s", check.UsableCount, check.RecommendedHost, check.RecommendedDelegated)
		check.Warnings = append(check.Warnings, "auth state is unknown for recommended executors; Zen did not pretend they are authenticated")
		if check.RecommendationConfidence == ConfidenceUnverified {
			check.Warnings = append(check.Warnings, "recommendations selected auth-unknown candidates because no verified authenticated executor exists")
		}
	case anyAuth(check.Items, AuthUnauthenticated):
		check.Status = StatusFail
		check.Remediation = RemediationAuthenticateExec
		check.Summary = "executor binaries found but all probed executors are unauthenticated"
		check.RecommendationConfidence = ConfidenceNone
	default:
		check.Status = StatusFail
		check.Remediation = RemediationInstallExecutor
		check.Summary = "no usable executor found; install and authenticate at least one of Codex, Claude, Cursor, or Grok"
		check.RecommendationConfidence = ConfidenceNone
	}
	return check
}

func filterByAuth(items []ExecutorCheck, want AuthState) []ExecutorCheck {
	out := make([]ExecutorCheck, 0, len(items))
	for _, item := range items {
		if item.Auth == want {
			out = append(out, item)
		}
	}
	return out
}

func anyAuth(items []ExecutorCheck, want AuthState) bool {
	for _, item := range items {
		if item.BinaryFound && item.Auth == want {
			return true
		}
	}
	return false
}

func (e env) probeExecutor(name string, executor work.Executor) ExecutorCheck {
	agent := work.NewAgentExecutor(name, executor)
	item := ExecutorCheck{
		ID:           agent.ID,
		Name:         agent.Name,
		Provider:     agent.Provider,
		Configured:   true,
		Command:      agent.Command,
		Auth:         AuthUnknown,
		Capabilities: agent.Capabilities,
	}

	bin := firstCommandToken(agent.Command)
	if bin == "" {
		item.Status = StatusFail
		item.Remediation = RemediationConfigureExecutor
		item.Summary = "executor command is empty"
		return item
	}

	path, err := e.opts.LookPath(bin)
	if err != nil || strings.TrimSpace(path) == "" {
		item.BinaryFound = false
		item.Status = StatusFail
		item.Remediation = RemediationInstallExecutor
		item.Summary = fmt.Sprintf("%s binary not found on PATH", bin)
		return item
	}
	item.BinaryFound = true
	item.BinaryPath = path
	item.Version = e.probeVersion(path, agent.Provider)
	item.Auth = e.probeAuth(path, agent.Provider)

	switch item.Auth {
	case AuthAuthenticated:
		item.Runnable = true
		item.Usable = true
		item.VerifiedAuthenticated = true
		item.Status = StatusOK
		item.Summary = fmt.Sprintf("%s ready (%s, auth verified)", item.ID, item.Provider)
	case AuthUnauthenticated:
		item.Runnable = false
		item.Usable = false
		item.Status = StatusFail
		item.Remediation = RemediationAuthenticateExec
		item.Summary = fmt.Sprintf("%s found but not authenticated", item.ID)
	default:
		// Auth unknown: treat as runnable candidate with warning, not a hard fail.
		item.Runnable = true
		item.Usable = true
		item.VerifiedAuthenticated = false
		item.Status = StatusWarn
		item.Summary = fmt.Sprintf("%s runnable (%s); auth state unknown", item.ID, item.Provider)
	}
	return item
}

func (e env) probeVersion(binaryPath, provider string) string {
	ctx, cancel := context.WithTimeout(context.Background(), e.opts.ProbeTimeout)
	defer cancel()
	out, err := e.opts.RunCommand(ctx, binaryPath, "--version")
	if err != nil {
		// Some CLIs use -v.
		out, err = e.opts.RunCommand(ctx, binaryPath, "-v")
		if err != nil {
			return ""
		}
	}
	return normalizeVersion(string(out))
}

func (e env) probeAuth(binaryPath, provider string) AuthState {
	ctx, cancel := context.WithTimeout(context.Background(), e.opts.ProbeTimeout)
	defer cancel()

	switch provider {
	case work.AgentProviderCodex:
		out, err := e.opts.RunCommand(ctx, binaryPath, "login", "status")
		return parseCodexAuth(out, err)
	case work.AgentProviderClaude:
		out, err := e.opts.RunCommand(ctx, binaryPath, "auth", "status", "--json")
		return parseClaudeAuth(out, err)
	case work.AgentProviderCursor:
		out, err := e.opts.RunCommand(ctx, binaryPath, "status", "--format", "json")
		return parseCursorAuth(out, err)
	case work.AgentProviderGrok:
		// Grok has login but no safe official non-interactive status command.
		return AuthUnknown
	default:
		return AuthUnknown
	}
}

func parseCodexAuth(out []byte, err error) AuthState {
	_ = err
	// Never return or log raw output: Codex may embed API key fragments.
	text := strings.ToLower(string(out))
	switch {
	case strings.Contains(text, "not logged in") || strings.Contains(text, "logged out"):
		return AuthUnauthenticated
	case strings.Contains(text, "logged in"):
		return AuthAuthenticated
	default:
		// Failed/unsupported/ambiguous official probes must not become unauthenticated.
		return AuthUnknown
	}
}

func parseClaudeAuth(out []byte, err error) AuthState {
	_ = err
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return AuthUnknown
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(trimmed, &raw) != nil {
		text := strings.ToLower(string(trimmed))
		switch {
		case strings.Contains(text, "not logged") || strings.Contains(text, "logged out"):
			return AuthUnauthenticated
		case strings.Contains(text, "logged in"):
			return AuthAuthenticated
		default:
			return AuthUnknown
		}
	}
	loggedInRaw, ok := raw["loggedIn"]
	if !ok {
		return AuthUnknown
	}
	var loggedIn bool
	if json.Unmarshal(loggedInRaw, &loggedIn) != nil {
		return AuthUnknown
	}
	if loggedIn {
		return AuthAuthenticated
	}
	return AuthUnauthenticated
}

func parseCursorAuth(out []byte, err error) AuthState {
	_ = err
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return AuthUnknown
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(trimmed, &raw) != nil {
		text := strings.ToLower(string(trimmed))
		switch {
		case strings.Contains(text, "not logged") || strings.Contains(text, "unauthenticated"):
			return AuthUnauthenticated
		case strings.Contains(text, "logged in") || strings.Contains(text, "authenticated"):
			return AuthAuthenticated
		default:
			return AuthUnknown
		}
	}

	if statusRaw, ok := raw["status"]; ok {
		var status string
		if json.Unmarshal(statusRaw, &status) == nil {
			switch strings.ToLower(strings.TrimSpace(status)) {
			case "authenticated":
				return AuthAuthenticated
			case "unauthenticated":
				return AuthUnauthenticated
			}
		}
	}
	if authRaw, ok := raw["isAuthenticated"]; ok {
		var isAuth bool
		if json.Unmarshal(authRaw, &isAuth) == nil {
			if isAuth {
				return AuthAuthenticated
			}
			return AuthUnauthenticated
		}
	}
	// Empty object / missing fields / failed probe → unknown, never default-false.
	return AuthUnknown
}

func recommendHost(usable []ExecutorCheck) string {
	if len(usable) == 0 {
		return ""
	}
	prefer := []string{"codex", "claude", "agent", "grok"}
	index := map[string]ExecutorCheck{}
	for _, item := range usable {
		index[item.ID] = item
	}
	for _, id := range prefer {
		if _, ok := index[id]; ok {
			return id
		}
	}
	// Prefer by provider if IDs differ.
	for _, provider := range []string{work.AgentProviderCodex, work.AgentProviderClaude, work.AgentProviderCursor, work.AgentProviderGrok} {
		for _, item := range usable {
			if item.Provider == provider {
				return item.ID
			}
		}
	}
	return usable[0].ID
}

func recommendDelegated(usable []ExecutorCheck, configured, host string) string {
	if len(usable) == 0 {
		return ""
	}
	for _, item := range usable {
		if item.ID == configured {
			return item.ID
		}
	}
	if host != "" {
		return host
	}
	return recommendHost(usable)
}

func firstCommandToken(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func normalizeVersion(raw string) string {
	line := strings.TrimSpace(raw)
	if line == "" {
		return ""
	}
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	line = strings.TrimSpace(strings.TrimPrefix(line, "tmux"))
	line = strings.TrimSpace(strings.TrimPrefix(line, "codex-cli"))
	return line
}

func (e env) defaultLookPath(file string) (string, error) {
	if pathEnv := strings.TrimSpace(e.opts.PathEnv); pathEnv != "" {
		return lookPathEnv(file, pathEnv)
	}
	return exec.LookPath(file)
}

func lookPathEnv(file, pathEnv string) (string, error) {
	if strings.Contains(file, string(os.PathSeparator)) {
		return file, nil
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, file)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}

func (e env) defaultRunCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if pathEnv := strings.TrimSpace(e.opts.PathEnv); pathEnv != "" {
		cmd.Env = append(os.Environ(), "PATH="+pathEnv)
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

func (e env) defaultHTTPGet(ctx context.Context, url string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	client := &http.Client{Timeout: e.opts.ProbeTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}
