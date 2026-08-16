package skills

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Native Skills mutations never shell out: every lifecycle effect is a
// bounded filesystem or fetch operation implemented in operations.go with
// exact rollback. The shell executor below exists only for the plugin
// subsystem, which still reviews and runs the official Claude plugin CLI
// command exactly as a user's terminal would.

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
}

// MutationTimeoutFor selects the bounded timeout for a built command.
func MutationTimeoutFor(command MutationCommand) time.Duration {
	switch command.Operation {
	case OperationUninstall, OperationForget, OperationBind, OperationUnbind,
		OperationEnable, OperationDisable, OperationMigrate:
		return DefaultRemovalTimeout
	default:
		return DefaultMutationTimeout
	}
}

// PluginMutationTimeoutFor selects the bounded timeout for a plugin command.
func PluginMutationTimeoutFor(command PluginMutationCommand) time.Duration {
	if command.Operation == PluginOperationUninstall {
		return DefaultRemovalTimeout
	}
	return DefaultMutationTimeout
}

// ExecutePluginMutationCommand runs a reviewed plugin-manager command with the
// exact argv tokens (never a shell). A non-zero exit is a truthful failure
// result with the bounded output tail; start/context errors are returned as
// errors so the server can classify them on the wire.
func ExecutePluginMutationCommand(ctx context.Context, command PluginMutationCommand, options MutationExecutionOptions) (MutationExecution, error) {
	argv, err := mutationArgv(command.Command, "claude")
	if err != nil {
		return MutationExecution{}, err
	}
	return executeCommandTokens(ctx, argv[0], argv[1:], options, PluginMutationTimeoutFor(command))
}

func mutationArgv(command, expectedBinary string) ([]string, error) {
	if command == "" || len(command) > 4096 {
		return nil, ErrMutationCommandInvalid
	}
	argv := strings.Fields(command)
	if len(argv) < 2 {
		return nil, ErrMutationCommandInvalid
	}
	if argv[0] != expectedBinary {
		return nil, ErrMutationCommandInvalid
	}
	for _, token := range argv {
		if len(token) == 0 || len(token) > 1024 {
			return nil, ErrMutationCommandInvalid
		}
		for _, current := range token {
			if current > 0x7f || invalidCommandTokenRune(current) {
				return nil, ErrMutationCommandInvalid
			}
		}
	}
	return argv, nil
}

func invalidCommandTokenRune(current rune) bool {
	switch current {
	case 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M',
		'N', 'O', 'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z',
		'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm',
		'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z',
		'0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
		'.', '_', '-', ':', '/', '@', '+':
		return false
	default:
		return true
	}
}

func executeCommandTokens(ctx context.Context, binary string, args []string, options MutationExecutionOptions, fallbackTimeout time.Duration) (MutationExecution, error) {
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
	env := append(os.Environ(), "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1")
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
