package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/daoleno/zen/daemon/control"
	"golang.org/x/term"
)

type telegramSecretReader func(io.Writer) (string, error)
type telegramControlCaller func(cliConfig, control.Request) (control.Response, error)

func runTelegramCommand(args []string, stderr io.Writer) error {
	return runTelegramCommandWithDeps(
		args,
		stderr,
		os.Stdout,
		readTelegramSecret,
		callControl,
	)
}

func runTelegramCommandWithDeps(
	args []string,
	stderr io.Writer,
	stdout io.Writer,
	readSecret telegramSecretReader,
	call telegramControlCaller,
) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprintln(stderr, "Usage: zen telegram setup [flags]")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Configures the running local Zen daemon and returns a one-time owner-binding URL.")
		return flag.ErrHelp
	}
	if args[0] != "setup" {
		return fmt.Errorf("unknown telegram command: %s", args[0])
	}

	fs := flag.NewFlagSet("zen telegram setup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{}
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon control socket")
	fs.BoolVar(&cfg.json, "json", false, "print JSON output")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if readSecret == nil || call == nil {
		return fmt.Errorf("Telegram setup dependencies are unavailable")
	}

	credential, err := readSecret(stderr)
	if err != nil {
		return err
	}
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return fmt.Errorf("Telegram bot token is required")
	}
	req := control.Request{Type: "telegram_setup", Credential: credential}
	resp, err := call(cfg, req)
	req.Credential = ""
	credential = ""
	if err != nil {
		return err
	}
	if !resp.OK {
		return writeControlResponse(stdout, resp, cfg.json)
	}
	if resp.TelegramBinding == nil || strings.TrimSpace(resp.TelegramBinding.URL) == "" {
		return fmt.Errorf("Telegram setup returned no owner-binding URL")
	}
	if cfg.json {
		return writeControlResponse(stdout, resp, true)
	}
	fmt.Fprintln(stdout, "Telegram bot configured.")
	fmt.Fprintln(stdout, "Open this one-time owner-binding link:")
	fmt.Fprintln(stdout, resp.TelegramBinding.URL)
	return nil
}

func readTelegramSecret(prompt io.Writer) (string, error) {
	fd := int(os.Stdin.Fd())
	return readTelegramSecretFrom(fd, prompt, term.IsTerminal, term.ReadPassword)
}

func readTelegramSecretFrom(
	fd int,
	prompt io.Writer,
	isTerminal func(int) bool,
	readPassword func(int) ([]byte, error),
) (string, error) {
	if !isTerminal(fd) {
		return "", fmt.Errorf("Telegram setup requires an interactive terminal for secure token input")
	}
	fmt.Fprint(prompt, "Telegram bot token: ")
	raw, err := readPassword(fd)
	fmt.Fprintln(prompt)
	if err != nil {
		return "", fmt.Errorf("read Telegram bot token: %w", err)
	}
	secret := string(raw)
	for index := range raw {
		raw[index] = 0
	}
	return secret, nil
}
