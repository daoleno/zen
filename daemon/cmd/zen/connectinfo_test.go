package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"flag"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/link"
)

func TestNormalizeEndpoint(t *testing.T) {
	value, err := normalizeEndpoint("https://zen.example.com")
	if err != nil {
		t.Fatalf("normalizeEndpoint returned error: %v", err)
	}
	if value != "wss://zen.example.com/ws" {
		t.Fatalf("unexpected normalized URL: %s", value)
	}
}

func TestNormalizeEndpointRejectsMissingScheme(t *testing.T) {
	if _, err := normalizeEndpoint("zen.example.com"); err == nil {
		t.Fatal("expected error for missing scheme")
	}
}

func TestBuildConnectLinkIncludesDaemonIdentity(t *testing.T) {
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	pairing := auth.PairingToken{
		Value:     "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ExpiresAt: time.Date(2026, 4, 5, 8, 0, 0, 0, time.UTC),
	}

	rawLink := buildConnectLink("wss://zen.example.com/ws", manager, pairing)
	parsed, err := url.Parse(rawLink)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if parsed.Scheme != "zen" {
		t.Fatalf("unexpected scheme: %s", parsed.Scheme)
	}
	payloadValue := parsed.Query().Get(connectParamPayload)
	if payloadValue == "" {
		t.Fatal("expected compact payload query param")
	}

	payload, err := base64.RawURLEncoding.DecodeString(payloadValue)
	if err != nil {
		t.Fatalf("DecodeString returned error: %v", err)
	}
	if len(payload) != 1+2+len("wss://zen.example.com/ws")+connectPublicKeyBytes+connectTokenBytes {
		t.Fatalf("unexpected payload size: %d", len(payload))
	}
	if payload[0] != connectPayloadVersion {
		t.Fatalf("unexpected payload version: %d", payload[0])
	}

	urlLength := int(binary.BigEndian.Uint16(payload[1:3]))
	offset := 3
	gotURL := string(payload[offset : offset+urlLength])
	offset += urlLength
	gotPublicKey := hex.EncodeToString(payload[offset : offset+connectPublicKeyBytes])
	offset += connectPublicKeyBytes
	gotToken := hex.EncodeToString(payload[offset : offset+connectTokenBytes])

	if gotURL != "wss://zen.example.com/ws" {
		t.Fatalf("unexpected url: %s", gotURL)
	}
	if gotPublicKey != manager.PublicKeyHex() {
		t.Fatalf("unexpected daemon public key: %s", gotPublicKey)
	}
	if gotToken != pairing.Value {
		t.Fatalf("unexpected enrollment token: %s", gotToken)
	}
}

func TestBuildConnectionOffersUsesEndpoint(t *testing.T) {
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	pairing := auth.PairingToken{
		Value:     "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ExpiresAt: time.Date(2026, 4, 5, 8, 0, 0, 0, time.UTC),
	}

	offers, err := buildConnectionOffers("https://zen.example.com/gateway", manager, pairing)
	if err != nil {
		t.Fatalf("buildConnectionOffers returned error: %v", err)
	}
	if len(offers) != 1 {
		t.Fatalf("expected one offer, got %d", len(offers))
	}
	if offers[0].URL != "wss://zen.example.com/gateway" {
		t.Fatalf("unexpected offer URL: %s", offers[0].URL)
	}

	parsed, err := url.Parse(offers[0].ConnectLink)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if got := parsed.Query().Get(connectParamPayload); got == "" {
		t.Fatal("expected compact payload query param")
	}
}

func TestPrintStartupInfoForLoopback(t *testing.T) {
	var output bytes.Buffer
	printStartupInfo(&output, "127.0.0.1:9876", "/tmp/zen-state", nil)

	rendered := output.String()
	for _, want := range []string{
		"local-only mode",
		"restart with zen --lan",
		"expose http://127.0.0.1:9876",
		"zen pair -state-dir /tmp/zen-state https://your-zen-host.example",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("startup info missing %q: %q", want, rendered)
		}
	}
	if strings.Contains(rendered, "Daemon ID") || strings.Contains(rendered, "Auth:") {
		t.Fatalf("startup info contains novice noise: %q", rendered)
	}
}

func TestPrintLinkStartupInfoUsesNoEndpointAsPrimaryAndKeepsAdvanced(t *testing.T) {
	var output bytes.Buffer
	printLinkStartupInfo(&output, "127.0.0.1:9876", "/tmp/zen-state")
	rendered := output.String()
	for _, expected := range []string{
		"Zen Link connecting outbound",
		"zen pair -state-dir /tmp/zen-state",
		"Advanced / Self-managed",
		"zen pair <endpoint>",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("Link startup missing %q: %q", expected, rendered)
		}
	}
	if strings.Contains(rendered, "your-zen-host") {
		t.Fatalf("Link startup hard-coded a nonexistent endpoint: %q", rendered)
	}
}

func TestPrintStartupInfoForLANUsesDetectedAddresses(t *testing.T) {
	var output bytes.Buffer
	printStartupInfo(&output, "0.0.0.0:9876", "", []privateNetworkAddress{
		{label: "Same Wi-Fi/LAN", ip: net.ParseIP("192.168.1.42")},
		{label: "Tailscale", ip: net.ParseIP("100.101.102.103")},
	})

	rendered := output.String()
	for _, want := range []string{
		"ready for trusted private-network access",
		"another terminal",
		"zen pair http://192.168.1.42:9876",
		"zen pair http://100.101.102.103:9876",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("startup info missing %q: %q", want, rendered)
		}
	}
	if strings.Contains(rendered, "zen pair http://0.0.0.0") {
		t.Fatalf("startup info offered wildcard pairing address: %q", rendered)
	}
}

func TestPrintStartupInfoForSpecificPrivateBind(t *testing.T) {
	var output bytes.Buffer
	printStartupInfo(&output, "192.168.1.42:9988", "", nil)

	rendered := output.String()
	if !strings.Contains(rendered, "zen pair http://192.168.1.42:9988") {
		t.Fatalf("specific private bind missing pairing command: %q", rendered)
	}
}

func TestPrintPairingInfo(t *testing.T) {
	var output bytes.Buffer
	printPairingInfo(&output, []connectionOffer{{
		Label:       "Server endpoint",
		URL:         "wss://zen.example.com/ws",
		ConnectLink: "zen://settings?p=compact-payload",
	}})

	rendered := output.String()
	if !strings.Contains(rendered, "Paste this link into Settings -> Pair Server:") {
		t.Fatalf("expected pair instruction, got %q", rendered)
	}
	if !strings.Contains(rendered, "zen://settings?p=compact-payload") {
		t.Fatalf("expected connect link, got %q", rendered)
	}
}

func TestPairConfigUsesOnePositionalEndpoint(t *testing.T) {
	cfg, err := parsePairConfig([]string{
		"-state-dir", "/tmp/zen-state",
		"https://zen.example.com",
	}, io.Discard)
	if err != nil {
		t.Fatalf("parsePairConfig returned error: %v", err)
	}
	if cfg.endpoint != "https://zen.example.com" {
		t.Fatalf("endpoint = %q", cfg.endpoint)
	}
	if cfg.stateDir != "/tmp/zen-state" {
		t.Fatalf("stateDir = %q", cfg.stateDir)
	}
}

func TestPairConfigAllowsNoEndpointOnlyForConfiguredLinkPath(t *testing.T) {
	cfg, err := parsePairConfig([]string{
		"-state-dir", "/tmp/zen-state",
		"-link-config", "/tmp/zen-link.json",
	}, io.Discard)
	if err != nil {
		t.Fatalf("parsePairConfig returned error: %v", err)
	}
	if cfg.endpoint != "" || cfg.linkConfigPath != "/tmp/zen-link.json" {
		t.Fatalf("unexpected no-endpoint config: %#v", cfg)
	}
}

func TestPairCommandWithoutEndpointOrLinkConfigFailsHonestly(t *testing.T) {
	stateDir := t.TempDir()
	var output bytes.Buffer
	err := runPairCommand([]string{"-state-dir", stateDir}, &output)
	if err == nil ||
		!strings.Contains(err.Error(), "Zen Link is not configured") ||
		!strings.Contains(err.Error(), "zen pair <endpoint>") {
		t.Fatalf("unexpected no-Link pair error: %v", err)
	}
	if strings.Contains(output.String(), "zen://") {
		t.Fatalf("no-Link command printed an unusable pairing link: %q", output.String())
	}
}

func TestOptionalLinkConfigIsInertUntilExplicitlyConfigured(t *testing.T) {
	stateDir := t.TempDir()
	config, path, enabled, err := loadOptionalLinkConfig(stateDir, "")
	if err != nil {
		t.Fatalf("absent default Link config returned error: %v", err)
	}
	if enabled || path != filepath.Join(stateDir, link.DefaultConfigFilename) {
		t.Fatalf(
			"absent default Link config = enabled %t path %q config %#v",
			enabled,
			path,
			config,
		)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "link-identity.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default config check created Link identity state: %v", err)
	}

	explicitPath := filepath.Join(stateDir, "operator-link.json")
	if _, _, _, err := loadOptionalLinkConfig(stateDir, explicitPath); err == nil {
		t.Fatal("explicit missing Link config was silently ignored")
	}
}

func TestRemovedAdvertiseURLFlagsAreRejected(t *testing.T) {
	if _, err := parseDaemonConfig([]string{"-advertise-url", "https://zen.example.com"}, io.Discard); err == nil {
		t.Fatal("daemon accepted removed -advertise-url flag")
	}
	if _, err := parsePairConfig([]string{"-url", "https://zen.example.com"}, io.Discard); err == nil {
		t.Fatal("pair accepted removed -url flag")
	}
}

func TestDaemonConfigLANModeAndAddrConflict(t *testing.T) {
	cfg, err := parseDaemonConfig([]string{"--lan"}, io.Discard)
	if err != nil {
		t.Fatalf("parseDaemonConfig(--lan): %v", err)
	}
	if cfg.addr != "0.0.0.0:9876" || !cfg.lan {
		t.Fatalf("LAN config = %#v", cfg)
	}

	defaultCfg, err := parseDaemonConfig(nil, io.Discard)
	if err != nil {
		t.Fatalf("parseDaemonConfig(default): %v", err)
	}
	if defaultCfg.addr != "127.0.0.1:9876" || defaultCfg.lan {
		t.Fatalf("default config = %#v", defaultCfg)
	}

	if _, err := parseDaemonConfig([]string{"--lan", "-addr", "192.168.1.42:9876"}, io.Discard); err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("expected explicit --lan/-addr conflict, got %v", err)
	}
	if _, err := parseDaemonConfig([]string{"-addr", "192.168.1.42:9876", "--lan"}, io.Discard); err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("expected order-independent --lan/-addr conflict, got %v", err)
	}
}

func TestStartupPairingAddressesNeverReturnsWildcard(t *testing.T) {
	detected := []privateNetworkAddress{{label: "Same Wi-Fi/LAN", ip: net.ParseIP("10.0.0.7")}}
	got := startupPairingAddresses("0.0.0.0", detected)
	if len(got) != 1 || !got[0].ip.Equal(net.ParseIP("10.0.0.7")) {
		t.Fatalf("startupPairingAddresses = %#v", got)
	}
	if got := startupPairingAddresses("192.168.2.9", nil); len(got) != 1 || got[0].ip.String() != "192.168.2.9" {
		t.Fatalf("specific private address = %#v", got)
	}
}

func TestPrivateNetworkAddressDetectionSkipsContainerInterfaces(t *testing.T) {
	for _, name := range []string{"docker0", "br-deadbeef", "veth123", "virbr0", "cni0", "podman0", "kube-bridge"} {
		if !shouldSkipPrivateNetworkInterface(name) {
			t.Fatalf("expected %q to be skipped", name)
		}
	}
	for _, name := range []string{"en0", "eth0", "wlan0", "tailscale0"} {
		if shouldSkipPrivateNetworkInterface(name) {
			t.Fatalf("expected %q to remain eligible", name)
		}
	}
}

func TestTopLevelHelpIncludesAgentAndBrainCommands(t *testing.T) {
	var output bytes.Buffer
	_, err := parseDaemonConfig([]string{"--help"}, &output)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("parseDaemonConfig error = %v, want ErrHelp", err)
	}
	rendered := output.String()
	for _, want := range []string{
		"agent      List, spawn, inspect, message, progress, and close agent sessions",
		"brain      Inspect Brain workspace and host executor configuration",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("top-level help missing %q:\n%s", want, rendered)
		}
	}
}

func TestAgentAndBrainHelpAreDiscoverable(t *testing.T) {
	var agentOutput bytes.Buffer
	if err := runAgentCommand([]string{"--help"}, &agentOutput); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("runAgentCommand error = %v, want ErrHelp", err)
	}
	agentHelp := agentOutput.String()
	for _, want := range []string{
		"Usage: zen agent <list|spawn|send|capture|status|progress|close|kill> [flags]",
		"zen agent spawn -name",
		"zen agent capture -id",
		"zen agent status -id",
		"zen agent progress --status running",
		"zen agent close -id",
	} {
		if !strings.Contains(agentHelp, want) {
			t.Fatalf("agent help missing %q:\n%s", want, agentHelp)
		}
	}

	var progressOutput bytes.Buffer
	if err := runAgentCommand([]string{"progress", "--help"}, &progressOutput); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("runAgentCommand progress help error = %v, want ErrHelp", err)
	}
	progressHelp := progressOutput.String()
	for _, want := range []string{
		"Usage: zen agent progress --status running --phase working --attention none",
		"-id",
		"-lease",
		"-status",
		"-task-class",
		"-event-kind",
		"-details-json",
	} {
		if !strings.Contains(progressHelp, want) {
			t.Fatalf("progress help missing %q:\n%s", want, progressHelp)
		}
	}

	var brainOutput bytes.Buffer
	if err := runBrainCommand([]string{"--help"}, &brainOutput); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("runBrainCommand error = %v, want ErrHelp", err)
	}
	brainHelp := brainOutput.String()
	for _, want := range []string{
		"Usage: zen brain <workspace|context|playbooks|gc|work|executors|use|set-delegated> [flags]",
		"Reconcile product-owned Brain workspace blocks while preserving user content",
		"zen brain workspace --json",
		"zen brain context --json",
		"zen brain playbooks --json",
		"zen brain gc --json",
		"zen brain executors --json",
		"zen brain set-delegated grok",
	} {
		if !strings.Contains(brainHelp, want) {
			t.Fatalf("brain help missing %q:\n%s", want, brainHelp)
		}
	}
}

type captureCLIControlHandler struct {
	requests chan control.Request
}

func (h *captureCLIControlHandler) HandleControlRequest(req control.Request) control.Response {
	h.requests <- req
	response := control.Response{OK: true}
	if req.Type == "device_revoke" {
		durable := true
		response.PersistenceOutcome = control.PersistenceApplied
		response.PersistenceDurable = &durable
	}
	return response
}

func TestAgentProgressCommandUsesZenAgentIDFallback(t *testing.T) {
	req := runProgressCLIAndCaptureRequest(t,
		"brain-agent-env:@1",
		[]string{
			"--status", "running",
			"--phase", "working",
			"--attention", "none",
			"--summary", "Reading files",
			"--task-class", "exploration",
			"--event-kind", "progress",
			"--details-json", `{"files":3}`,
			"--lease", "300",
			"--json=false",
		},
	)

	if req.Type != "agent_progress" || req.AgentID != "brain-agent-env:@1" {
		t.Fatalf("request identity = %#v", req)
	}
	if req.Status != "running" || req.Phase != "working" || req.Attention != "none" {
		t.Fatalf("request progress = %#v", req)
	}
	if req.Summary != "Reading files" || req.LeaseSeconds != 300 {
		t.Fatalf("request progress metadata = %#v", req)
	}
	if req.TaskClass != "exploration" || req.EventKind != "progress" || req.DetailsJSON != `{"files":3}` {
		t.Fatalf("semantic request metadata = %#v", req)
	}
}

func TestAgentProgressCommandExplicitIDOverridesEnv(t *testing.T) {
	req := runProgressCLIAndCaptureRequest(t,
		"brain-agent-env:@1",
		[]string{
			"-id", "brain-agent-explicit:@2",
			"--status", "done",
			"--phase", "reporting",
			"--attention", "done",
			"--summary", "Finished",
			"--json=false",
		},
	)

	if req.AgentID != "brain-agent-explicit:@2" {
		t.Fatalf("request agent id = %q", req.AgentID)
	}
	if req.Status != "done" || req.Phase != "reporting" || req.Attention != "done" {
		t.Fatalf("request progress = %#v", req)
	}
}

func TestAgentProgressCommandUsesZenStateDirFallback(t *testing.T) {
	stateDir := t.TempDir()
	handler, done, cancel := startCLIControlServer(t, stateDir)
	defer cancel()

	t.Setenv("ZEN_AGENT_ID", "brain-agent-env:@1")
	t.Setenv("ZEN_STATE_DIR", stateDir)
	var stderr bytes.Buffer
	if err := runAgentProgress([]string{
		"--status", "running",
		"--phase", "working",
		"--attention", "none",
		"--summary", "Reading files",
		"--json=false",
	}, &stderr); err != nil {
		t.Fatalf("runAgentProgress returned error: %v stderr=%s", err, stderr.String())
	}

	select {
	case req := <-handler.requests:
		if req.AgentID != "brain-agent-env:@1" || req.Status != "running" {
			t.Fatalf("request = %#v", req)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for control request")
	}

	cancel()
	waitForCLIControlServerShutdown(t, done)
}

func TestRevokeDeviceUsesRunningDaemonControlOwner(t *testing.T) {
	stateDir := t.TempDir()
	handler, done, cancel := startCLIControlServer(t, stateDir)
	defer cancel()

	if _, err := revokeDevice(stateDir, "phone-one"); err != nil {
		t.Fatalf("revokeDevice returned error: %v", err)
	}
	select {
	case request := <-handler.requests:
		if request.Type != "device_revoke" || request.ID != "phone-one" {
			t.Fatalf("revoke request=%#v", request)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for device revoke request")
	}

	cancel()
	waitForCLIControlServerShutdown(t, done)
}

func TestAgentProgressCommandRequiresIDOrEnv(t *testing.T) {
	var stderr bytes.Buffer
	t.Setenv("ZEN_AGENT_ID", "")
	err := runAgentProgress([]string{
		"--status", "running",
		"--phase", "working",
		"--attention", "none",
		"--json=false",
	}, &stderr)
	if err == nil || !strings.Contains(err.Error(), "agent id is required") {
		t.Fatalf("runAgentProgress error = %v", err)
	}
}

func runProgressCLIAndCaptureRequest(t *testing.T, envAgentID string, args []string) control.Request {
	t.Helper()
	stateDir := t.TempDir()
	handler, done, cancel := startCLIControlServer(t, stateDir)
	defer cancel()

	t.Setenv("ZEN_AGENT_ID", envAgentID)
	commandArgs := append([]string{"--state-dir", stateDir}, args...)
	var stderr bytes.Buffer
	if err := runAgentProgress(commandArgs, &stderr); err != nil {
		t.Fatalf("runAgentProgress returned error: %v stderr=%s", err, stderr.String())
	}

	var req control.Request
	select {
	case req = <-handler.requests:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for control request")
	}

	cancel()
	waitForCLIControlServerShutdown(t, done)
	return req
}

func startCLIControlServer(t *testing.T, stateDir string) (*captureCLIControlHandler, chan error, context.CancelFunc) {
	t.Helper()
	socketPath, err := control.DefaultSocketPath(stateDir)
	if err != nil {
		t.Fatalf("DefaultSocketPath returned error: %v", err)
	}
	handler := &captureCLIControlHandler{requests: make(chan control.Request, 1)}
	server := &control.Server{Path: socketPath, Handler: handler}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.Run(ctx)
	}()
	waitForCLISocketPath(t, socketPath)
	return handler, done, cancel
}

func waitForCLIControlServerShutdown(t *testing.T, done chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server exited with error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for control server shutdown")
	}
}

func waitForCLISocketPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for control socket at %s", path)
}
