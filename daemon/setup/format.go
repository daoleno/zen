package setup

import (
	"fmt"
	"io"

	"github.com/daoleno/zen/daemon/doctor"
)

// WriteHuman renders a setup Result for terminals without secrets.
func WriteHuman(w io.Writer, result Result) error {
	if result.OK {
		writeLines(w, successLines(result)...)
		return nil
	}
	if result.Message != "" {
		fmt.Fprintf(w, "zen setup stopped: %s\n", result.Message)
	}
	for _, step := range result.NextSteps {
		fmt.Fprintf(w, "  - %s\n", step)
	}
	return nil
}

func successLines(result Result) []string {
	lines := []string{
		"",
		"zen setup complete",
		fmt.Sprintf("  profile: %s", result.Profile),
		fmt.Sprintf("  host: %s", result.Host),
		fmt.Sprintf("  delegated: %s", result.Delegated),
		fmt.Sprintf("  config: %s", result.ConfigPath),
	}
	if result.BackupPath != "" {
		lines = append(lines, "  backup: "+result.BackupPath)
	}
	if result.BrainConfigured {
		lines = append(lines, "  Brain host: configured (restart zen to load)")
	} else {
		lines = append(lines, "  Brain host: left unconfigured")
	}
	if result.RestartRequired {
		lines = append(lines, "  Restart zen to load new or changed executor definitions")
		lines = append(lines, "  (delegated selection can later switch live: zen brain set-delegated <id>)")
	}
	for _, warning := range result.Warnings {
		lines = append(lines, "  warning: "+warning)
	}
	lines = append(lines, "", "Next steps:")
	for _, step := range result.NextSteps {
		lines = append(lines, "  - "+step)
	}
	return lines
}

func formatCandidates(candidates []Candidate) []string {
	lines := []string{"Runnable executors (verified first):"}
	for _, c := range candidates {
		auth := string(c.Auth)
		if c.VerifiedAuthenticated {
			auth = "verified"
		}
		lines = append(lines, fmt.Sprintf("  - %s [%s] auth=%s", c.ID, c.Provider, auth))
	}
	lines = append(lines, "One executor is enough. Host and Delegated may be the same.")
	return lines
}

func machineBlockedLines(report doctor.Report) []string {
	lines := []string{
		"zen setup: machine is not ready",
		"",
		"Fix these blockers, then re-run: zen setup",
	}
	if report.Platform.Status == doctor.StatusFail {
		lines = append(lines, "  - platform: "+report.Platform.Summary)
	}
	if report.Tmux.Status == doctor.StatusFail {
		lines = append(lines, "  - tmux: "+report.Tmux.Summary)
		if report.Tmux.Remediation == doctor.RemediationInstallTmux {
			lines = append(lines, "    Install tmux (Zen will not run sudo for you):")
			for _, hint := range report.Tmux.InstallHints {
				lines = append(lines, fmt.Sprintf("      - %s: %s", hint.OS, hint.Command))
			}
		}
	}
	if report.StateDir.Status == doctor.StatusFail {
		lines = append(lines, "  - state dir: "+report.StateDir.Summary)
	}
	if report.Listen.Status == doctor.StatusFail {
		lines = append(lines, "  - listen: "+report.Listen.Summary)
	}
	return lines
}

func machineRemediationSteps(report doctor.Report) []string {
	var steps []string
	if report.Tmux.Status == doctor.StatusFail && report.Tmux.Remediation == doctor.RemediationInstallTmux {
		for _, hint := range report.Tmux.InstallHints {
			steps = append(steps, hint.Command)
		}
	}
	if report.StateDir.Status == doctor.StatusFail {
		steps = append(steps, "ensure the Zen state directory is writable")
	}
	if report.Listen.Status == doctor.StatusFail {
		steps = append(steps, "free the Zen listen address or stop the conflicting process")
	}
	steps = append(steps, "re-run: zen setup")
	return steps
}

func noExecutorLines(report doctor.Report) []string {
	lines := []string{
		"zen setup: no runnable executor found",
		"",
		"Install and authenticate at least one provider CLI on this host, then re-run zen setup.",
		"Official login/status commands (provider docs; Zen does not invent install URLs):",
	}
	lines = append(lines, executorInstallSteps(report)...)
	lines = append(lines, "", "Missing configured binaries:")
	for _, item := range report.Executors.Items {
		if item.BinaryFound {
			continue
		}
		lines = append(lines, fmt.Sprintf("  - %s (%s): not on PATH", item.ID, item.Provider))
	}
	return lines
}

func executorInstallSteps(report doctor.Report) []string {
	seen := map[string]bool{}
	var steps []string
	add := func(line string) {
		if seen[line] {
			return
		}
		seen[line] = true
		steps = append(steps, "  - "+line)
	}
	// Provider-neutral steps already documented in docs/executors.md.
	add("Codex: install Codex CLI, then run: codex login")
	add("Claude: install Claude Code, then run: claude auth login")
	add("Cursor: install cursor-agent, then run: cursor-agent login")
	add("Grok: install Grok CLI, then run: grok login")
	add("Pi: install Pi Coding Agent (`pi`), then configure provider auth (no official auth-status command)")
	add("OpenCode: install OpenCode (`opencode`), then run: opencode auth login")
	return steps
}
