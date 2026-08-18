package skills

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Native Skills deletion never shells out. Plugin lifecycle operations use
// the owning Agent's exact argv through PluginRuntime, never a shell.

const (
	// DefaultMutationTimeout bounds import/update (network and package
	// resolution can take minutes).
	DefaultMutationTimeout = 5 * time.Minute
	// DefaultRemovalTimeout bounds remove/uninstall (local bookkeeping).
	DefaultRemovalTimeout = 90 * time.Second
	// MutationMaxOutputBytes caps the combined output tail returned to the
	// App so a noisy CLI can never bloat the wire.
	MutationMaxOutputBytes = 16 << 10
)

var (
	ErrMutationBinaryMissing  = errors.New("the CLI required for this mutation is not installed on the server")
	ErrMutationCancelled      = errors.New("the Skills mutation was canceled by a newer request")
	ErrMutationTimedOut       = errors.New("the Skills mutation exceeded its time limit")
	ErrMutationCommandInvalid = errors.New("the mutation command is not a safe executable form")
)

// MutationExecution is the truthful outcome of running a reviewed command.
type MutationExecution struct {
	Success    bool   `json:"success"`
	ExitCode   int    `json:"exit_code"`
	Output     string `json:"output"`
	DurationMS int64  `json:"duration_ms"`
}

type MutationExecutionOptions struct {
	// CWD is the validated working directory for project-scope mutations.
	// Empty means the daemon's own working directory.
	CWD string
	// Timeout bounds native operations. Zero selects the operation default.
	Timeout time.Duration
	// InventoryOptions carries fixture home/state/env overrides for native
	// operations. Empty keeps the production user home.
	InventoryOptions InventoryOptions
	// PluginRuntime is the owning-manager boundary for Plugin operations. Nil
	// selects the production Codex/Claude CLI runtime.
	PluginRuntime PluginRuntime
}

// MutationTimeoutFor selects the bounded timeout for exact-copy deletion.
func MutationTimeoutFor(command MutationCommand) time.Duration {
	return DefaultRemovalTimeout
}

// executeCommandTokens runs one validated argv without shell interpretation.
func executeCommandTokens(ctx context.Context, binary string, args []string, options MutationExecutionOptions, fallbackTimeout time.Duration, env []string) (MutationExecution, error) {
	if binary == "" || len(args) == 0 {
		return MutationExecution{}, ErrMutationCommandInvalid
	}
	for _, token := range append([]string{binary}, args...) {
		if token == "" || len(token) > 1024 || strings.ContainsAny(token, "\x00\r\n") {
			return MutationExecution{}, ErrMutationCommandInvalid
		}
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return MutationExecution{}, ErrMutationBinaryMissing
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = fallbackTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	command := exec.CommandContext(runCtx, path, args...)
	if len(env) == 0 {
		env = os.Environ()
	}
	command.Env = env
	if options.CWD != "" {
		command.Dir = options.CWD
	}

	startedAt := time.Now()
	output, runErr := command.CombinedOutput()
	durationMS := time.Since(startedAt).Milliseconds()

	if runCtx.Err() == context.Canceled {
		return MutationExecution{}, ErrMutationCancelled
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return MutationExecution{
			Success:    false,
			ExitCode:   -1,
			Output:     boundedMutationOutput(output),
			DurationMS: durationMS,
		}, ErrMutationTimedOut
	}

	exitCode := 0
	if runErr != nil {
		var exitError *exec.ExitError
		if errors.As(runErr, &exitError) {
			exitCode = exitError.ExitCode()
		} else {
			return MutationExecution{
				Success:    false,
				ExitCode:   -1,
				Output:     boundedMutationOutput(output),
				DurationMS: durationMS,
			}, runErr
		}
	}
	return MutationExecution{
		Success:    exitCode == 0,
		ExitCode:   exitCode,
		Output:     boundedMutationOutput(output),
		DurationMS: durationMS,
	}, nil
}

func boundedMutationOutput(output []byte) string {
	if len(output) == 0 {
		return ""
	}
	text := string(output)
	text = strings.TrimSpace(text)
	if len(text) > MutationMaxOutputBytes {
		text = "...output truncated...\n" + text[len(text)-MutationMaxOutputBytes:]
	}
	return text
}
