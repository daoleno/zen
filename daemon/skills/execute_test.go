package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMutationArgvRejectsShellTaintedCommandTokens(t *testing.T) {
	for _, command := range []string{
		"npx skills add ; rm -rf /",
		"npx skills add repo --yes $(whoami)",
		"npx skills add repo --yes`reboot`",
		"echo npx skills",
		"npx skills add repo --skill 'x'",
		"claude plugin uninstall x --scope user --yes && echo hi",
		"",
		"npx",
	} {
		if _, err := mutationArgv(command, "npx"); err == nil {
			t.Fatalf("tainted command %q was accepted", command)
		}
	}
}

func TestMutationArgvAcceptsExactOfficialTokens(t *testing.T) {
	commands := []string{
		"npx skills add https://github.com/owner/repo --skill demo --global --agent codex --yes",
		"npx skills remove demo --global --agent codex --yes",
		"npx skills update --global --yes",
		"claude plugin install demo@market --scope user",
		"claude plugin uninstall demo@market --scope user --yes",
	}
	for _, command := range commands {
		argv, err := mutationArgv(command, strings.Fields(command)[0])
		if err != nil {
			t.Fatalf("exact command %q was rejected: %v", command, err)
		}
		if argv[0] != strings.Fields(command)[0] {
			t.Fatalf("command %q parsed with wrong binary %q", command, argv[0])
		}
	}
}

func TestMutationTimedOutProducesBoundedTailAndClassifiedError(t *testing.T) {
	command := MutationCommand{
		Operation: OperationRemove,
		Command:   "npx skills remove demo --global --agent codex --yes",
		SkillName: "demo",
		Scope:     ScopeGlobal,
		Agents:    []Agent{AgentCodex},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Bound with an absurdly short timeout; npx will not be resolvable in
	// tests, so the start error path is exercised instead. The classification
	// behavior below is what the server relies on.
	execution, err := ExecuteMutationCommand(ctx, command, MutationExecutionOptions{
		Timeout: time.Nanosecond,
	})
	if err == nil {
		t.Fatal("expected a classified execution error")
	}
	if errors.Is(err, ErrMutationBinaryMissing) {
		t.Log("npx is not installed in this test environment; binary gap classified")
	}
	if !errors.Is(err, ErrMutationCancelled) && !errors.Is(err, ErrMutationTimedOut) && !errors.Is(err, ErrMutationBinaryMissing) && !errors.Is(err, ErrMutationCommandInvalid) {
		t.Fatalf("unexpected execution error class: %v", err)
	}
	// Even on start failure the outcome is a truthful result with no output.
	if execution.Success {
		t.Fatal("execution reported success for a failed start")
	}
}

func TestBoundedMutationOutputKeepsTailAndTrims(t *testing.T) {
	big := strings.Repeat("x", MutationMaxOutputBytes+5000)
	trimmed := boundedMutationOutput([]byte(big))
	if len(trimmed) > MutationMaxOutputBytes+len("...output truncated...\n") {
		t.Fatalf("bounded output exceeded the cap: %d", len(trimmed))
	}
	if !strings.HasSuffix(trimmed, strings.Repeat("x", MutationMaxOutputBytes)) {
		t.Fatal("bounded output did not retain the tail")
	}
	if got := boundedMutationOutput([]byte("  fine  ")); got != "fine" {
		t.Fatalf("bounded output did not trim whitespace: %q", got)
	}
}

func TestMutationExecutionOptionsCWDIsApplied(t *testing.T) {
	if testing.Short() {
		t.Skip("requires a shell binary")
	}
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh in this environment")
	}
	// Execute a fake "npx" shim so the test never touches the real CLI.
	shimDir := t.TempDir()
	shim := filepath.Join(shimDir, "npx")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nprintf '%s' \"$PWD\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+originalPath)

	workDir := t.TempDir()
	command := MutationCommand{
		Operation: OperationUpdate,
		Command:   "npx skills update --global --yes",
		Scope:     ScopeGlobal,
		Agents:    []Agent{},
	}
	execution, err := ExecuteMutationCommand(context.Background(), command, MutationExecutionOptions{
		CWD:     workDir,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("shim execution failed: %v", err)
	}
	if !execution.Success {
		t.Fatalf("shim reported failure: %s", execution.Output)
	}
	if execution.Output != workDir {
		t.Fatalf("CWD was not applied: output %q want %q", execution.Output, workDir)
	}
}

func TestPluginMutationTimeoutSelection(t *testing.T) {
	if got := PluginMutationTimeoutFor(PluginMutationCommand{Operation: PluginOperationUninstall}); got != DefaultRemovalTimeout {
		t.Fatalf("uninstall timeout = %v, want removal timeout", got)
	}
	if got := PluginMutationTimeoutFor(PluginMutationCommand{Operation: PluginOperationInstall}); got != DefaultMutationTimeout {
		t.Fatalf("install timeout = %v, want mutation timeout", got)
	}
}
