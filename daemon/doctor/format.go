package doctor

import (
	"fmt"
	"io"
	"strings"
)

// WriteHuman renders a doctor report for terminals. It never prints tokens,
// credential paths, or sensitive environment values.
func WriteHuman(w io.Writer, report Report) error {
	status := "NOT READY"
	if report.Ready {
		if len(report.Warnings) > 0 || report.Executors.Status == StatusWarn {
			status = "READY (with warnings)"
		} else {
			status = "READY"
		}
	}
	if _, err := fmt.Fprintf(w, "zen doctor: %s\n\n", status); err != nil {
		return err
	}

	writeCheck(w, "Platform", report.Platform.Status, report.Platform.Summary)
	writeCheck(w, "tmux", report.Tmux.Status, report.Tmux.Summary)
	if report.Tmux.Status == StatusFail && report.Tmux.Remediation == RemediationInstallTmux {
		fmt.Fprintln(w, "  Install tmux (pick your OS; Zen will not run sudo for you):")
		for _, hint := range report.Tmux.InstallHints {
			fmt.Fprintf(w, "    - %s: %s\n", hint.OS, hint.Command)
		}
	}
	writeCheck(w, "State dir", report.StateDir.Status, report.StateDir.Summary)
	writeCheck(w, "Listen", report.Listen.Status, report.Listen.Summary)
	writeCheck(w, "Executors", report.Executors.Status, report.Executors.Summary)

	if len(report.Executors.Items) > 0 {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Configured executors:")
		for _, item := range report.Executors.Items {
			fmt.Fprintf(w, "  - %s [%s] binary=%s auth=%s runnable=%t verified=%t usable=%t\n",
				item.ID,
				item.Provider,
				boolWord(item.BinaryFound),
				item.Auth,
				item.Runnable,
				item.VerifiedAuthenticated,
				item.Usable,
			)
			if item.Summary != "" {
				fmt.Fprintf(w, "      %s\n", item.Summary)
			}
			if item.Remediation != "" {
				fmt.Fprintf(w, "      remediation: %s\n", item.Remediation)
			}
		}
	}

	if report.Executors.RecommendedHost != "" || report.Executors.RecommendedDelegated != "" {
		fmt.Fprintln(w, "")
		fmt.Fprintf(w, "Recommended host executor: %s\n", emptyDash(report.Executors.RecommendedHost))
		fmt.Fprintf(w, "Recommended delegated executor: %s\n", emptyDash(report.Executors.RecommendedDelegated))
		fmt.Fprintf(w, "Recommendation confidence: %s\n", emptyDash(string(report.Executors.RecommendationConfidence)))
	}

	if len(report.Warnings) > 0 {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Warnings:")
		for _, warning := range report.Warnings {
			fmt.Fprintf(w, "  - %s\n", warning)
		}
	}

	if len(report.Remediations) > 0 {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Remediation codes:")
		for _, code := range report.Remediations {
			fmt.Fprintf(w, "  - %s\n", code)
		}
	}

	if !report.Ready {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Fix the failing checks above, then re-run: zen doctor")
		if report.Tmux.Status == StatusFail && report.Tmux.Remediation == RemediationInstallTmux {
			fmt.Fprintln(w, "Missing tmux blocks all session runtimes.")
		}
		if report.Executors.UsableCount == 0 {
			fmt.Fprintln(w, "Zen needs at least one runnable executor (binary present and not explicitly unauthenticated).")
		}
	}
	return nil
}

func writeCheck(w io.Writer, name string, status Status, summary string) {
	fmt.Fprintf(w, "[%s] %s: %s\n", strings.ToUpper(string(status)), name, summary)
}

func boolWord(v bool) string {
	if v {
		return "found"
	}
	return "missing"
}

func emptyDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}
