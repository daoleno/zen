package setup

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/doctor"
	"github.com/daoleno/zen/daemon/work"
)

// Run executes the setup flow and returns a structured Result.
// Callers should print Result via WriteHuman (or JSON) and map sentinel errors
// to nonzero exit codes without stack traces.
func Run(opts Options) (Result, error) {
	out := opts.Stdout
	if out == nil {
		out = io.Discard
	}
	errOut := opts.Stderr
	if errOut == nil {
		errOut = io.Discard
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	result := Result{Step: "machine"}
	report, err := runDoctor(opts)
	if err != nil {
		return result, err
	}
	result.Doctor = report

	if blocked, message := machineBlocked(report); blocked {
		result.StoppedEarly = true
		result.Message = message
		result.NextSteps = machineRemediationSteps(report)
		writeLines(out, machineBlockedLines(report)...)
		return result, ErrBlocked
	}

	result.Step = "executors"
	candidates := selectableCandidates(report)
	if len(candidates) == 0 {
		result.StoppedEarly = true
		result.Message = "no runnable executor found"
		result.NextSteps = executorInstallSteps(report)
		writeLines(out, noExecutorLines(report)...)
		return result, ErrNoExecutor
	}

	hostDefault := report.Executors.RecommendedHost
	delegatedDefault := report.Executors.RecommendedDelegated
	if hostDefault == "" {
		hostDefault = candidates[0].ID
	}
	if delegatedDefault == "" {
		delegatedDefault = hostDefault
	}

	profile := opts.Profile
	if profile == "" {
		profile = ProfileSafe
	}
	profile, err = normalizeProfile(string(profile))
	if err != nil {
		return result, err
	}

	host := strings.TrimSpace(opts.Host)
	delegated := strings.TrimSpace(opts.Delegated)
	configureBrain := false

	if opts.NonInteractive {
		if host == "" || delegated == "" {
			return result, fmt.Errorf("%w: --host and --delegated are required in --non-interactive mode", ErrInvalidArgs)
		}
		if !isCandidate(candidates, host) || !isCandidate(candidates, delegated) {
			return result, fmt.Errorf("%w: host/delegated must be runnable candidates", ErrInvalidArgs)
		}
		if profile == ProfileAutonomous && !opts.Yes {
			writeLines(out, PermissionExplanation()...)
			writeLines(out, "", "Autonomous profile requires explicit confirmation (--yes).")
			result.StoppedEarly = true
			result.Message = "autonomous profile requires explicit confirmation (--yes)"
			result.Step = "permissions"
			return result, ErrConsentRequired
		}
		if profile == ProfileAutonomous && opts.Yes {
			configureBrain = true
		}
	} else {
		writeLines(out, "zen setup", "")
		writeLines(out, formatCandidates(candidates)...)
		host, err = promptChoice(opts, out, errOut, "Host executor", hostDefault, candidateIDs(candidates))
		if err != nil {
			return result, err
		}
		delegated, err = promptChoice(opts, out, errOut, "Delegated executor", delegatedDefault, candidateIDs(candidates))
		if err != nil {
			return result, err
		}

		result.Step = "permissions"
		writeLines(out, append([]string{""}, append(PermissionExplanation(), "")...)...)
		profileStr, err := promptChoice(opts, out, errOut, "Permission profile (safe/autonomous)", string(ProfileSafe), []string{string(ProfileSafe), string(ProfileAutonomous)})
		if err != nil {
			return result, err
		}
		profile, err = normalizeProfile(profileStr)
		if err != nil {
			return result, err
		}
		if profile == ProfileAutonomous {
			confirmed, cerr := promptYesNo(opts, out, errOut, "Confirm Autonomous profile (high risk)?", false)
			if cerr != nil {
				return result, cerr
			}
			if !confirmed {
				result.StoppedEarly = true
				result.Message = "autonomous profile requires explicit confirmation"
				result.Step = "permissions"
				return result, ErrConsentRequired
			}
			configureBrain = true
		}
	}

	if profile == ProfileSafe {
		configureBrain = false
	}

	result.Host = host
	result.Delegated = delegated
	result.Profile = profile
	result.Step = "config"

	paths, err := resolvePaths(opts)
	if err != nil {
		return result, err
	}
	result.ConfigPath = paths.ExecutorsPath

	selectedIDs := uniqueStrings(host, delegated)
	selected := make([]selectedExecutor, 0, len(selectedIDs))
	for _, id := range selectedIDs {
		item := findDoctorExecutor(report, id)
		existing := work.Executor{Name: id, Command: id}
		provider := ""
		if item != nil {
			provider = item.Provider
			existing.Command = item.Command
			existing.Name = item.Name
		}
		selected = append(selected, selectedExecutor{ID: id, Provider: provider, Existing: existing})
	}

	written, err := writeExecutorsConfig(configWriteRequest{
		Path:      paths.ExecutorsPath,
		Delegated: delegated,
		Profile:   profile,
		Selected:  selected,
		Now:       now().UTC(),
	})
	if err != nil {
		return result, err
	}
	result.BackupPath = written.BackupPath
	result.RestartRequired = true

	if configureBrain {
		if err := persistBrainHost(paths.BrainRoot, host); err != nil {
			return result, err
		}
		result.BrainConfigured = true
	} else if BrainHardensHostAtRuntime() {
		result.Warnings = append(result.Warnings,
			"Brain host left unconfigured: current runtime hardens Codex/Claude Brain sessions to bypass/noninteractive mode",
		)
	}

	result.Step = "next"
	result.OK = true
	result.NextSteps = nextSteps(paths.StateDir)
	result.Message = "setup complete"
	writeLines(out, successLines(result)...)
	return result, nil
}

type resolvedPaths struct {
	Home          string
	StateDir      string
	ExecutorsPath string
	BrainRoot     string
}

func resolvePaths(opts Options) (resolvedPaths, error) {
	home := strings.TrimSpace(opts.Home)
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return resolvedPaths{}, err
		}
	}
	stateDir := strings.TrimSpace(opts.StateDir)
	if stateDir == "" {
		if strings.TrimSpace(opts.Home) != "" {
			stateDir = filepath.Join(home, ".zen")
		} else if d, err := auth.DefaultStorageDir(); err == nil {
			stateDir = d
		} else {
			stateDir = filepath.Join(home, ".zen")
		}
	}
	executorsPath := strings.TrimSpace(opts.ExecutorsPath)
	if executorsPath == "" {
		if strings.TrimSpace(opts.Home) != "" {
			executorsPath = filepath.Join(home, ".zen", "executors.toml")
		} else if p, err := work.DefaultExecutorsPath(); err == nil {
			executorsPath = p
		} else {
			executorsPath = filepath.Join(home, ".zen", "executors.toml")
		}
	}
	brainRoot := strings.TrimSpace(opts.BrainRoot)
	if brainRoot == "" {
		if strings.TrimSpace(opts.Home) != "" {
			brainRoot = filepath.Join(home, ".zen", "brain")
		} else if p, err := brain.DefaultRoot(); err == nil {
			brainRoot = p
		} else {
			brainRoot = filepath.Join(home, ".zen", "brain")
		}
	}
	return resolvedPaths{
		Home:          home,
		StateDir:      stateDir,
		ExecutorsPath: executorsPath,
		BrainRoot:     brainRoot,
	}, nil
}

func persistBrainHost(brainRoot, hostID string) error {
	store, err := brain.NewStore(brainRoot)
	if err != nil {
		return err
	}
	return store.SetHostExecutorID(hostID)
}

func runDoctor(opts Options) (doctor.Report, error) {
	dopts := doctor.Options{
		StateDir:      opts.StateDir,
		Addr:          opts.Addr,
		ExecutorsPath: opts.ExecutorsPath,
		Home:          opts.Home,
		PathEnv:       opts.PathEnv,
		Now:           opts.DoctorNow,
		LookPath:      opts.DoctorLookPath,
		Listen:        opts.DoctorListen,
	}
	if opts.DoctorNow == nil && opts.Now != nil {
		dopts.Now = opts.Now
	}
	if opts.DoctorRunCommand != nil {
		dopts.RunCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return opts.DoctorRunCommand(0, name, args...)
		}
	}
	if opts.DoctorHTTPGet != nil {
		dopts.HTTPGet = func(ctx context.Context, url string) (int, []byte, error) {
			return opts.DoctorHTTPGet(url)
		}
	}
	return doctor.Run(dopts)
}

func machineBlocked(report doctor.Report) (bool, string) {
	if report.Platform.Status == doctor.StatusFail {
		return true, report.Platform.Summary
	}
	if report.Tmux.Status == doctor.StatusFail {
		return true, report.Tmux.Summary
	}
	if report.StateDir.Status == doctor.StatusFail {
		return true, report.StateDir.Summary
	}
	if report.Listen.Status == doctor.StatusFail {
		return true, report.Listen.Summary
	}
	return false, ""
}

func selectableCandidates(report doctor.Report) []Candidate {
	var verified, unknown []Candidate
	for _, item := range report.Executors.Items {
		if !item.Runnable || !item.Usable {
			continue
		}
		c := Candidate{
			ID:                    item.ID,
			Provider:              item.Provider,
			Auth:                  item.Auth,
			VerifiedAuthenticated: item.VerifiedAuthenticated,
			Runnable:              item.Runnable,
			Summary:               item.Summary,
		}
		if item.VerifiedAuthenticated {
			verified = append(verified, c)
		} else {
			unknown = append(unknown, c)
		}
	}
	return append(verified, unknown...)
}

func isCandidate(candidates []Candidate, id string) bool {
	for _, c := range candidates {
		if c.ID == id {
			return true
		}
	}
	return false
}

func candidateIDs(candidates []Candidate) []string {
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, c.ID)
	}
	return out
}

func findDoctorExecutor(report doctor.Report, id string) *doctor.ExecutorCheck {
	for i := range report.Executors.Items {
		if report.Executors.Items[i].ID == id {
			return &report.Executors.Items[i]
		}
	}
	return nil
}

func uniqueStrings(values ...string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func promptChoice(opts Options, out, errOut io.Writer, label, def string, allowed []string) (string, error) {
	if opts.Stdin == nil {
		return "", fmt.Errorf("%w: stdin required for interactive setup", ErrIncomplete)
	}
	allowedSet := map[string]bool{}
	for _, value := range allowed {
		allowedSet[value] = true
	}
	scanner := bufio.NewScanner(opts.Stdin)
	for {
		fmt.Fprintf(out, "%s [%s]: ", label, def)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return "", err
			}
			return "", fmt.Errorf("%w: missing %s", ErrIncomplete, label)
		}
		value := strings.TrimSpace(scanner.Text())
		if value == "" {
			value = def
		}
		if allowedSet[value] {
			return value, nil
		}
		fmt.Fprintf(errOut, "choose one of: %s\n", strings.Join(allowed, ", "))
	}
}

func promptYesNo(opts Options, out, errOut io.Writer, label string, def bool) (bool, error) {
	defLabel := "n"
	if def {
		defLabel = "y"
	}
	if opts.Stdin == nil {
		return false, fmt.Errorf("%w: stdin required for interactive setup", ErrIncomplete)
	}
	scanner := bufio.NewScanner(opts.Stdin)
	for {
		fmt.Fprintf(out, "%s [%s]: ", label, defLabel)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return false, err
			}
			return false, fmt.Errorf("%w: missing confirmation", ErrIncomplete)
		}
		value := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if value == "" {
			return def, nil
		}
		switch value {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(errOut, "enter y or n")
		}
	}
}

func writeLines(w io.Writer, lines ...string) {
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
}

func nextSteps(stateDir string) []string {
	pairHint := "zen pair https://your-origin.example"
	if strings.TrimSpace(stateDir) != "" {
		pairHint = "zen pair -state-dir " + stateDir + " https://your-origin.example"
	}
	return []string{
		"Restart or start the daemon so it reloads executors.toml: zen",
		"Expose the full origin (LAN/tailnet/reverse-proxy/tunnel) that reaches this host",
		"Generate a pairing link: " + pairHint,
		"Optional: re-check with zen doctor",
	}
}
