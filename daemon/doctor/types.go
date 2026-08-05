// Package doctor inspects whether a machine can run Zen without performing
// destructive or paid actions. The Report type is the stable contract later
// consumed by `zen setup` and daemon/app control APIs.
package doctor

import (
	"errors"
	"time"

	"github.com/daoleno/zen/daemon/work"
)

// ErrNotReady is returned by the CLI after a successful diagnosis when Ready
// is false. Callers should treat the printed Report as authoritative.
var ErrNotReady = errors.New("zen doctor: environment is not ready")

// Status is a machine-readable check outcome.
type Status string

const (
	StatusOK      Status = "ok"
	StatusWarn    Status = "warn"
	StatusFail    Status = "fail"
	StatusUnknown Status = "unknown"
)

// AuthState is best-effort executor authentication state.
// Unknown means no safe official probe exists or the probe failed ambiguously.
type AuthState string

const (
	AuthAuthenticated   AuthState = "authenticated"
	AuthUnauthenticated AuthState = "unauthenticated"
	AuthUnknown         AuthState = "unknown"
)

// Remediation codes are stable identifiers for automated onboarding flows.
type Remediation string

const (
	RemediationUnsupportedPlatform Remediation = "unsupported_platform"
	RemediationInstallTmux         Remediation = "install_tmux"
	RemediationFixTmux             Remediation = "fix_tmux"
	RemediationStateDirUnwritable  Remediation = "state_dir_unwritable"
	RemediationPortInUse           Remediation = "port_in_use"
	RemediationInstallExecutor     Remediation = "install_executor"
	RemediationAuthenticateExec    Remediation = "authenticate_executor"
	RemediationConfigureExecutor   Remediation = "configure_executor"
)

// InstallHint is an OS-specific non-destructive install suggestion.
type InstallHint struct {
	ID      string `json:"id"`
	OS      string `json:"os"`
	Command string `json:"command"`
}

// RecommendationConfidence describes how strongly doctor trusts a recommendation.
type RecommendationConfidence string

const (
	// ConfidenceVerified means at least one recommended executor has verified auth.
	ConfidenceVerified RecommendationConfidence = "verified"
	// ConfidenceUnverified means recommendations rely on auth-unknown runnable candidates.
	ConfidenceUnverified RecommendationConfidence = "unverified"
	// ConfidenceNone means no runnable executor was available to recommend.
	ConfidenceNone RecommendationConfidence = "none"
)

// Report is the top-level doctor result. JSON field names are part of the
// onboarding API surface and should remain stable.
type Report struct {
	Ready        bool           `json:"ready"`
	GeneratedAt  time.Time      `json:"generated_at"`
	Platform     PlatformCheck  `json:"platform"`
	Tmux         TmuxCheck      `json:"tmux"`
	StateDir     StateDirCheck  `json:"state_dir"`
	Listen       ListenCheck    `json:"listen"`
	Executors    ExecutorsCheck `json:"executors"`
	Checks       []NamedCheck   `json:"checks"`
	Warnings     []string       `json:"warnings,omitempty"`
	Remediations []Remediation  `json:"remediations"`
}

// NamedCheck is a flat summary entry for UI/API consumers.
type NamedCheck struct {
	ID          string      `json:"id"`
	Status      Status      `json:"status"`
	Remediation Remediation `json:"remediation,omitempty"`
	Summary     string      `json:"summary"`
}

// PlatformCheck reports OS/arch support.
type PlatformCheck struct {
	OS          string      `json:"os"`
	Arch        string      `json:"arch"`
	Supported   bool        `json:"supported"`
	Status      Status      `json:"status"`
	Remediation Remediation `json:"remediation,omitempty"`
	Summary     string      `json:"summary"`
}

// TmuxCheck reports tmux availability and a safe functional probe.
type TmuxCheck struct {
	Found        bool          `json:"found"`
	Path         string        `json:"path,omitempty"`
	Version      string        `json:"version,omitempty"`
	Functional   bool          `json:"functional"`
	Status       Status        `json:"status"`
	Remediation  Remediation   `json:"remediation,omitempty"`
	Summary      string        `json:"summary"`
	InstallHints []InstallHint `json:"install_hints,omitempty"`
}

// StateDirCheck reports Zen state directory readiness.
type StateDirCheck struct {
	Path        string      `json:"path"`
	Writable    bool        `json:"writable"`
	Created     bool        `json:"created"`
	Status      Status      `json:"status"`
	Remediation Remediation `json:"remediation,omitempty"`
	Summary     string      `json:"summary"`
}

// ListenCheck reports daemon listen readiness.
type ListenCheck struct {
	Addr        string      `json:"addr"`
	Available   bool        `json:"available"`
	ZenRunning  bool        `json:"zen_running"`
	DaemonID    string      `json:"daemon_id,omitempty"`
	Status      Status      `json:"status"`
	Remediation Remediation `json:"remediation,omitempty"`
	Summary     string      `json:"summary"`
}

// ExecutorsCheck summarizes configured executors and recommendations.
type ExecutorsCheck struct {
	ConfigPath               string                   `json:"config_path"`
	ConfigExists             bool                     `json:"config_exists"`
	DelegatedExecutor        string                   `json:"delegated_executor,omitempty"`
	Status                   Status                   `json:"status"`
	Remediation              Remediation              `json:"remediation,omitempty"`
	Summary                  string                   `json:"summary"`
	Items                    []ExecutorCheck          `json:"items"`
	UsableCount              int                      `json:"usable_count"`
	VerifiedCount            int                      `json:"verified_count"`
	RecommendedHost          string                   `json:"recommended_host,omitempty"`
	RecommendedDelegated     string                   `json:"recommended_delegated,omitempty"`
	RecommendationConfidence RecommendationConfidence `json:"recommendation_confidence"`
	Warnings                 []string                 `json:"warnings,omitempty"`
}

// ExecutorCheck is one configured executor's capability and readiness view.
//
// Runnable/Usable means the binary is present and not explicitly
// unauthenticated. VerifiedAuthenticated is stricter: an official probe
// confirmed login. Auth-unknown candidates are usable with warning.
type ExecutorCheck struct {
	ID                    string                 `json:"id"`
	Name                  string                 `json:"name"`
	Provider              string                 `json:"provider"`
	Configured            bool                   `json:"configured"`
	Command               string                 `json:"command,omitempty"`
	BinaryFound           bool                   `json:"binary_found"`
	BinaryPath            string                 `json:"binary_path,omitempty"`
	Version               string                 `json:"version,omitempty"`
	Auth                  AuthState              `json:"auth"`
	Runnable              bool                   `json:"runnable"`
	Usable                bool                   `json:"usable"`
	VerifiedAuthenticated bool                   `json:"verified_authenticated"`
	Capabilities          work.AgentCapabilities `json:"capabilities"`
	Status                Status                 `json:"status"`
	Remediation           Remediation            `json:"remediation,omitempty"`
	Summary               string                 `json:"summary"`
	// OpenCode-only non-mutating probes. StatusUnknown means the probe was not
	// run or failed ambiguously; never includes model lists or secrets.
	ModelsStatus Status `json:"models_status,omitempty"`
	DBPathStatus Status `json:"db_path_status,omitempty"`
}

// TmuxInstallHints returns OS-specific install commands. Doctor never runs them.
func TmuxInstallHints() []InstallHint {
	return []InstallHint{
		{ID: "debian", OS: "ubuntu/debian", Command: "sudo apt install tmux"},
		{ID: "fedora", OS: "fedora", Command: "sudo dnf install tmux"},
		{ID: "arch", OS: "arch", Command: "sudo pacman -S tmux"},
		{ID: "macos", OS: "macos", Command: "brew install tmux"},
	}
}
