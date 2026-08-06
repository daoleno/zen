package work

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

var (
	// ErrLaunchUnparseable means the command is not a supported launch shape.
	ErrLaunchUnparseable = errors.New("launch command is not parseable")
	// ErrResumeAmbiguous means multiple resume/session declarations were found.
	ErrResumeAmbiguous = errors.New("resume declaration is ambiguous")
	// ErrResumeMissingValue means a resume/session flag was present without a value.
	ErrResumeMissingValue = errors.New("resume declaration is missing a value")
	// ErrResumeTerminated means resume cannot be appended after "--".
	ErrResumeTerminated = errors.New("cannot append resume after option terminator")
)

// ProviderResumeToken extracts the unique provider-native resume/session token.
// present=false, err=nil means no resume declaration.
// Ambiguous, missing-value, or unparseable shapes return err (fail closed).
func ProviderResumeToken(provider, command string) (token string, present bool, err error) {
	provider = normalizeResumeProvider(provider, command)
	options, parseOK := inspectLaunchCommandOptions(command)
	if !parseOK {
		return "", false, ErrLaunchUnparseable
	}
	switch provider {
	case AgentProviderCodex:
		return codexResumeTokenFromArgv(options.argv)
	case AgentProviderClaude:
		return exclusiveResumeFlagToken(options.argv, "--resume", "-r")
	case AgentProviderGrok, AgentProviderCursor:
		return exclusiveResumeFlagToken(options.argv, "--resume")
	case AgentProviderOpenCode:
		return exclusiveOpenCodeSessionToken(options.argv)
	case AgentProviderPi:
		return exclusivePiSessionToken(options.argv)
	default:
		return "", false, ErrLaunchUnparseable
	}
}

// WithProviderResumeToken injects a provider-native resume/session token once.
// Unparseable, ambiguous, terminated, conflicting, or invalid tokens fail closed.
func WithProviderResumeToken(provider, command, token string) (string, error) {
	command = strings.TrimSpace(command)
	token = strings.TrimSpace(token)
	if command == "" {
		return "", fmt.Errorf("launch command required")
	}
	if token == "" {
		return "", fmt.Errorf("resume token required")
	}
	provider = normalizeResumeProvider(provider, command)
	options, parseOK := inspectLaunchCommandOptions(command)
	if !parseOK {
		return "", ErrLaunchUnparseable
	}
	existing, present, err := ProviderResumeToken(provider, command)
	if err != nil {
		return "", err
	}
	if present {
		if existing == token {
			return command, nil
		}
		return "", fmt.Errorf("command already resumes %q", existing)
	}
	if options.terminated {
		// Cannot safely append after "--"; matching existing resume already returned above.
		return "", ErrResumeTerminated
	}
	quoted := shellQuoteForLaunch(token)
	switch provider {
	case AgentProviderCodex:
		return appendCommandOptions(command, "resume", quoted), nil
	case AgentProviderClaude, AgentProviderGrok, AgentProviderCursor:
		return appendCommandOptions(command, "--resume", quoted), nil
	case AgentProviderOpenCode:
		if !strings.HasPrefix(token, "ses_") {
			return "", fmt.Errorf("opencode resume requires ses_* session id")
		}
		return appendCommandOptions(command, "-s", quoted), nil
	case AgentProviderPi:
		if !filepath.IsAbs(token) {
			return "", fmt.Errorf("pi resume requires an absolute --session path")
		}
		return appendCommandOptions(command, "--session", quoted), nil
	default:
		return "", fmt.Errorf("provider %q has no native resume launch shape", provider)
	}
}

// CodexSessionIDFromRolloutPath derives a Codex session UUID from a rollout path.
func CodexSessionIDFromRolloutPath(path string) string {
	return sessionIDFromCodexRolloutPath(path)
}

func normalizeResumeProvider(provider, command string) string {
	provider = strings.TrimSpace(provider)
	if provider == "" || provider == AgentProviderCustom {
		provider = InferAgentProvider(command)
	}
	return provider
}

func exclusiveResumeFlagToken(argv []string, names ...string) (token string, present bool, err error) {
	values, err := collectExclusiveOptionValues(argv, names...)
	if err != nil {
		if errors.Is(err, ErrResumeMissingValue) {
			return "", true, err
		}
		return "", false, err
	}
	switch len(values) {
	case 0:
		return "", false, nil
	case 1:
		if strings.TrimSpace(values[0]) == "" {
			return "", true, ErrResumeMissingValue
		}
		return values[0], true, nil
	default:
		return "", true, ErrResumeAmbiguous
	}
}

func exclusiveOpenCodeSessionToken(argv []string) (token string, present bool, err error) {
	values, err := collectExclusiveOptionValues(argv, "-s", "--session")
	if err != nil {
		if errors.Is(err, ErrResumeMissingValue) {
			return "", true, err
		}
		return "", false, err
	}
	switch len(values) {
	case 0:
		return "", false, nil
	case 1:
		value := strings.TrimSpace(values[0])
		if value == "" {
			return "", true, ErrResumeMissingValue
		}
		if !strings.HasPrefix(value, "ses_") {
			return "", true, fmt.Errorf("opencode session must be ses_*")
		}
		return value, true, nil
	default:
		return "", true, ErrResumeAmbiguous
	}
}

func exclusivePiSessionToken(argv []string) (token string, present bool, err error) {
	sessionValues, err := collectExclusiveOptionValues(argv, "--session")
	if err != nil {
		if errors.Is(err, ErrResumeMissingValue) {
			return "", true, err
		}
		return "", false, err
	}
	dirPresent := false
	for _, argument := range argv {
		if argument == "--session-dir" || strings.HasPrefix(argument, "--session-dir=") {
			dirPresent = true
			break
		}
	}
	if dirPresent && len(sessionValues) > 0 {
		return "", true, fmt.Errorf("%w: pi --session conflicts with --session-dir", ErrResumeAmbiguous)
	}
	switch len(sessionValues) {
	case 0:
		return "", false, nil
	case 1:
		value := strings.TrimSpace(sessionValues[0])
		if value == "" {
			return "", true, ErrResumeMissingValue
		}
		if !filepath.IsAbs(value) {
			return "", true, fmt.Errorf("pi --session requires an absolute path")
		}
		return value, true, nil
	default:
		return "", true, ErrResumeAmbiguous
	}
}

func collectExclusiveOptionValues(argv []string, names ...string) ([]string, error) {
	values := make([]string, 0, 1)
	for index := 0; index < len(argv); index++ {
		argument := argv[index]
		if argument == "--" {
			break
		}
		matched := false
		value := ""
		hasValue := false
		for _, name := range names {
			if argument == name {
				matched = true
				if index+1 >= len(argv) || argv[index+1] == "--" || strings.HasPrefix(argv[index+1], "-") {
					return nil, ErrResumeMissingValue
				}
				value = argv[index+1]
				hasValue = true
				index++
				break
			}
			if strings.HasPrefix(argument, name+"=") {
				matched = true
				value = strings.TrimPrefix(argument, name+"=")
				hasValue = true
				break
			}
			if (name == "-s" || name == "-r" || name == "-a") &&
				strings.HasPrefix(argument, name) && len(argument) > len(name) && !strings.HasPrefix(argument, name+"-") {
				matched = true
				value = strings.TrimPrefix(argument, name)
				hasValue = true
				break
			}
		}
		if !matched {
			continue
		}
		if !hasValue {
			return nil, ErrResumeMissingValue
		}
		values = append(values, value)
	}
	return values, nil
}

// codexResumeTokenFromArgv treats only a true positional "resume" subcommand as
// resume. Known value flags consume their next token; known boolean flags do not.
// Unknown bare flags (no "=") fail closed — the next token might be a flag value,
// not a subcommand. Equals-form flags do not create next-token ambiguity.
func codexResumeTokenFromArgv(argv []string) (token string, present bool, err error) {
	found := 0
	tok := ""
	for index := 0; index < len(argv); index++ {
		argument := argv[index]
		if argument == "--" {
			break
		}
		if strings.HasPrefix(argument, "-") {
			if strings.Contains(argument, "=") {
				continue
			}
			switch {
			case codexFlagTakesValue(argument):
				if index+1 >= len(argv) || argv[index+1] == "--" || strings.HasPrefix(argv[index+1], "-") {
					return "", false, ErrResumeAmbiguous
				}
				index++
			case codexFlagBoolean(argument):
				// no value
			default:
				// Unknown bare flag: cannot prove whether the next token is its
				// value or a positional subcommand (including resume).
				return "", false, ErrResumeAmbiguous
			}
			continue
		}
		if argument != "resume" {
			continue
		}
		found++
		if index+1 >= len(argv) || argv[index+1] == "--" || strings.HasPrefix(argv[index+1], "-") {
			return "", true, ErrResumeMissingValue
		}
		tok = argv[index+1]
		index++
		if found > 1 {
			return "", true, ErrResumeAmbiguous
		}
	}
	if found == 0 {
		return "", false, nil
	}
	return tok, true, nil
}

func codexFlagTakesValue(flag string) bool {
	switch flag {
	case "-c", "-C", "-s", "-a", "-p",
		"--config", "--cd", "--model", "--profile",
		"--sandbox", "--ask-for-approval", "--color",
		"--image", "--add-dir":
		return true
	default:
		return false
	}
}

func codexFlagBoolean(flag string) bool {
	switch flag {
	case "--dangerously-bypass-approvals-and-sandbox",
		"--no-alt-screen",
		"--full-auto",
		"--quiet",
		"--json",
		"--search",
		"--oss":
		return true
	default:
		return false
	}
}
