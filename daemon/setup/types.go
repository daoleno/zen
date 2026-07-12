// Package setup turns doctor.Report into a safe, resumable first-run flow.
// It never silently installs packages, runs sudo, logs into third-party agents,
// opens paid model calls, or weakens permissions without explicit consent.
package setup

import (
	"errors"
	"io"
	"time"

	"github.com/daoleno/zen/daemon/doctor"
)

var (
	// ErrBlocked is returned when platform/tmux/state-dir (or listen) blocks readiness.
	ErrBlocked = errors.New("zen setup: machine is not ready")
	// ErrNoExecutor is returned when no runnable executor is available.
	ErrNoExecutor = errors.New("zen setup: no runnable executor")
	// ErrConsentRequired is returned when Autonomous is selected without confirmation.
	ErrConsentRequired = errors.New("zen setup: autonomous profile requires explicit confirmation")
	// ErrInvalidArgs is returned for invalid non-interactive flags.
	ErrInvalidArgs = errors.New("zen setup: invalid arguments")
	// ErrIncomplete is returned when interactive input is missing required answers.
	ErrIncomplete = errors.New("zen setup: incomplete input")
)

// Profile selects permission posture for written executor commands.
type Profile string

const (
	ProfileSafe       Profile = "safe"
	ProfileAutonomous Profile = "autonomous"
)

// Options configures a setup run. Stdin/Stdout/Stderr and Doctor options are
// injectable for deterministic tests and automation.
type Options struct {
	NonInteractive bool
	Host           string
	Delegated      string
	Profile        Profile
	// Yes confirms Autonomous. Required in non-interactive Autonomous mode.
	Yes bool
	// ConfigureBrain writes Brain host executor state. Forced false for Safe.
	ConfigureBrain bool

	StateDir      string
	Addr          string
	Home          string
	ExecutorsPath string
	BrainRoot     string
	PathEnv       string

	DoctorLookPath   func(file string) (string, error)
	DoctorRunCommand func(ctxDeadline time.Duration, name string, args ...string) ([]byte, error)
	DoctorHTTPGet    func(url string) (status int, body []byte, err error)
	DoctorListen     func(network, address string) (io.Closer, error)
	DoctorNow        func() time.Time

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Now    func() time.Time
}

// Result is the machine-readable outcome of a setup attempt.
type Result struct {
	OK              bool          `json:"ok"`
	StoppedEarly    bool          `json:"stopped_early,omitempty"`
	Step            string        `json:"step"`
	Profile         Profile       `json:"profile,omitempty"`
	Host            string        `json:"host,omitempty"`
	Delegated       string        `json:"delegated,omitempty"`
	BrainConfigured bool          `json:"brain_configured"`
	ConfigPath      string        `json:"config_path,omitempty"`
	BackupPath      string        `json:"backup_path,omitempty"`
	RestartRequired bool          `json:"restart_required"`
	Doctor          doctor.Report `json:"doctor"`
	Warnings        []string      `json:"warnings,omitempty"`
	NextSteps       []string      `json:"next_steps,omitempty"`
	Message         string        `json:"message,omitempty"`
}

// Candidate is one selectable executor for Host/Delegated.
type Candidate struct {
	ID                    string           `json:"id"`
	Provider              string           `json:"provider"`
	Auth                  doctor.AuthState `json:"auth"`
	VerifiedAuthenticated bool             `json:"verified_authenticated"`
	Runnable              bool             `json:"runnable"`
	Summary               string           `json:"summary"`
}
