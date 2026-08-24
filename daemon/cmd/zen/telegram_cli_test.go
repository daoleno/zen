package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/control"
	telegramchannel "github.com/daoleno/zen/daemon/telegram"
)

const telegramCLITestSecret = "123456:cli-test-secret"

func TestTelegramSetupReadsSecretSecurelyAndSendsOneExactRequest(t *testing.T) {
	var requested control.Request
	secretReads := 0
	var stdout, stderr bytes.Buffer
	err := runTelegramCommandWithDeps(
		[]string{"setup", "-state-dir", t.TempDir()},
		&stderr,
		&stdout,
		func(io.Writer) (string, error) {
			secretReads++
			return telegramCLITestSecret, nil
		},
		func(cfg cliConfig, req control.Request) (control.Response, error) {
			requested = req
			return control.Response{OK: true, TelegramBinding: &telegramchannel.BindingChallenge{URL: "https://t.me/fixture_bot?start=one-time", ExpiresAt: time.Now().Add(time.Minute)}}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if secretReads != 1 {
		t.Fatalf("secure reads = %d, want 1", secretReads)
	}
	if requested.Type != "telegram_setup" || requested.Credential != telegramCLITestSecret {
		t.Fatalf("request = %#v", requested)
	}
	if strings.Count(stdout.String(), "https://t.me/fixture_bot?start=one-time") != 1 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), telegramCLITestSecret) {
		t.Fatal("CLI output exposed the raw Telegram token")
	}
}

func TestTelegramSetupUsesEchoDisabledTerminalReader(t *testing.T) {
	var prompt bytes.Buffer
	terminalChecks := 0
	passwordReads := 0
	secret, err := readTelegramSecretFrom(
		42,
		&prompt,
		func(fd int) bool {
			terminalChecks++
			return fd == 42
		},
		func(fd int) ([]byte, error) {
			passwordReads++
			if fd != 42 {
				t.Fatalf("password fd = %d", fd)
			}
			return []byte(telegramCLITestSecret), nil
		},
	)
	if err != nil || secret != telegramCLITestSecret || terminalChecks != 1 || passwordReads != 1 {
		t.Fatalf("secret=%q err=%v checks=%d reads=%d", secret, err, terminalChecks, passwordReads)
	}
	if prompt.String() != "Telegram bot token: \n" || strings.Contains(prompt.String(), telegramCLITestSecret) {
		t.Fatalf("prompt = %q", prompt.String())
	}

	_, err = readTelegramSecretFrom(42, &prompt, func(int) bool { return false }, func(int) ([]byte, error) {
		t.Fatal("non-terminal input must not be read")
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("non-terminal err = %v", err)
	}
}

func TestTelegramSetupRoundTripsThroughExactLocalControlSocket(t *testing.T) {
	stateDir, err := os.MkdirTemp(os.Getenv("TMPDIR"), "zen-telegram-control-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(stateDir) })
	manager := &fakeTelegramControlManager{binding: telegramchannel.BindingChallenge{URL: "https://t.me/fixture_bot?start=socket-correlation", ExpiresAt: time.Now().Add(time.Minute)}}
	socketPath, err := control.DefaultSocketPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	server := &control.Server{Path: socketPath, Handler: &controlApp{telegram: manager}}
	go func() { done <- server.Run(ctx) }()
	waitForCLISocketPath(t, socketPath)

	var stdout, stderr bytes.Buffer
	err = runTelegramCommandWithDeps(
		[]string{"setup", "-state-dir", stateDir},
		&stderr,
		&stdout,
		func(io.Writer) (string, error) { return telegramCLITestSecret, nil },
		callControl,
	)
	if err != nil {
		t.Fatal(err)
	}
	if manager.configureToken != telegramCLITestSecret || manager.bindCalls != 1 || !strings.Contains(stdout.String(), "socket-correlation") {
		t.Fatalf("manager=%#v stdout=%q", manager, stdout.String())
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("control socket survived shutdown: %v", err)
	}
	err = filepath.Walk(stateDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode().IsRegular() {
			t.Fatalf("CLI wrote durable state outside the daemon manager: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTelegramSetupRejectsTokenInArgv(t *testing.T) {
	var stdout, stderr bytes.Buffer
	reads := 0
	err := runTelegramCommandWithDeps(
		[]string{"setup", telegramCLITestSecret},
		&stderr,
		&stdout,
		func(io.Writer) (string, error) {
			reads++
			return telegramCLITestSecret, nil
		},
		func(cliConfig, control.Request) (control.Response, error) {
			t.Fatal("control request must not run")
			return control.Response{}, nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "unexpected arguments") || reads != 0 {
		t.Fatalf("err=%v reads=%d", err, reads)
	}
}

func TestTelegramSetupConfigureFailurePrintsNoBindingOrSecret(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runTelegramCommandWithDeps(
		[]string{"setup"},
		&stderr,
		&stdout,
		func(io.Writer) (string, error) { return telegramCLITestSecret, nil },
		func(cliConfig, control.Request) (control.Response, error) {
			return control.ErrorResponse("telegram_configure_failed", "Telegram bot credential could not be verified"), nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "telegram_configure_failed") {
		t.Fatalf("configure err = %v", err)
	}
	output := stdout.String() + stderr.String()
	if strings.Contains(output, telegramCLITestSecret) || strings.Contains(output, "https://t.me/") || strings.Contains(output, "bot configured") {
		t.Fatalf("failed setup exposed success or credential output: %q", output)
	}
}

type fakeTelegramControlManager struct {
	configureToken string
	configureErr   error
	bindCalls      int
	binding        telegramchannel.BindingChallenge
}

func (m *fakeTelegramControlManager) Configure(_ context.Context, token string) (telegramchannel.Status, error) {
	m.configureToken = token
	return telegramchannel.Status{State: telegramchannel.StateSetupPending, Enabled: true, BotUsername: "fixture_bot"}, m.configureErr
}

func (m *fakeTelegramControlManager) BeginBinding() (telegramchannel.BindingChallenge, error) {
	m.bindCalls++
	return m.binding, nil
}

func TestTelegramControlSetupReturnsBindingWithoutCredentialProjection(t *testing.T) {
	manager := &fakeTelegramControlManager{binding: telegramchannel.BindingChallenge{URL: "https://t.me/fixture_bot?start=one-time", ExpiresAt: time.Now().Add(time.Minute)}}
	resp := (&controlApp{telegram: manager}).HandleControlRequest(control.Request{Type: "telegram_setup", Credential: telegramCLITestSecret})
	if !resp.OK || manager.configureToken != telegramCLITestSecret || manager.bindCalls != 1 || resp.TelegramBinding == nil {
		t.Fatalf("response=%#v manager=%#v", resp, manager)
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(telegramCLITestSecret)) {
		t.Fatalf("control response retained credential: %s", raw)
	}
}

func TestTelegramControlSetupStopsOnConfigureFailure(t *testing.T) {
	manager := &fakeTelegramControlManager{configureErr: errors.New("credential rejected")}
	resp := (&controlApp{telegram: manager}).HandleControlRequest(control.Request{Type: "telegram_setup", Credential: telegramCLITestSecret})
	if resp.OK || resp.Error == nil || resp.Error.Code != "telegram_configure_failed" || manager.bindCalls != 0 || resp.TelegramBinding != nil {
		t.Fatalf("response=%#v manager=%#v", resp, manager)
	}
}

func TestTelegramCLIHelpAndStatusContainNoCredentialExamples(t *testing.T) {
	var stderr bytes.Buffer
	err := runTelegramCommandWithDeps(nil, &stderr, &bytes.Buffer{}, nil, nil)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help err = %v", err)
	}
	for _, forbidden := range []string{"-token", "--token", telegramCLITestSecret} {
		if strings.Contains(stderr.String(), forbidden) {
			t.Fatalf("help contains %q: %s", forbidden, stderr.String())
		}
	}
	status := telegramchannel.Status{State: telegramchannel.StateSetupPending, Enabled: true, BotUsername: "fixture_bot"}
	raw, _ := json.Marshal(status)
	if bytes.Contains(raw, []byte("token")) || bytes.Contains(raw, []byte(telegramCLITestSecret)) {
		t.Fatalf("status exposes credential material: %s", raw)
	}
}
