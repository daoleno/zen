package classifier

import (
	"strings"
	"time"
)

// ActivitySignal is a provider adapter observation from visible pane or process
// evidence. It upgrades Unknown after progress/classification merge; it never
// reads provider transcripts or invents Running from raw pane churn alone.
type ActivitySignal struct {
	State    AgentState
	Summary  string
	Source   string // stable machine tag, e.g. "cursor_pane_stop_marker"
	Provider string // adapter name, e.g. "cursor", "codex"
}

// ActivityInput is the provider-neutral observation bundle passed to adapters.
// Watcher fills shared fields; adapters ignore hints they do not use.
type ActivityInput struct {
	Agent           Agent
	PaneContent     string
	ToolChildActive bool // optional process-tree hint (non-idle worker child)
}

// ActivityAdapter is a provider-specific pane/process activity detector.
// Adapters must not rely on package init registration.
type ActivityAdapter interface {
	// Name returns a stable provider id used in ActivitySignal.Provider.
	Name() string
	// Match reports whether this adapter should interpret the session.
	Match(in ActivityInput) bool
	// Infer returns an activity signal. Empty State means "no opinion".
	// StateUnknown with a Source means "provider idle".
	Infer(in ActivityInput) ActivitySignal
}

// ActivityProbe is the watcher-facing contract for activity inference.
type ActivityProbe interface {
	Infer(in ActivityInput) ActivitySignal
}

// MultiActivityProbe dispatches to the first matching ActivityAdapter.
type MultiActivityProbe struct {
	adapters []ActivityAdapter
}

// NewActivityProbe builds an explicit, ordered adapter chain.
func NewActivityProbe(adapters ...ActivityAdapter) *MultiActivityProbe {
	out := make([]ActivityAdapter, 0, len(adapters))
	for _, adapter := range adapters {
		if adapter != nil {
			out = append(out, adapter)
		}
	}
	return &MultiActivityProbe{adapters: out}
}

// DefaultActivityProbe returns the shipped pane/process adapter chain. A
// provider with no such evidence has no adapter and stays honestly Unknown.
// Watcher wires this explicitly via SetActivityProbe.
func DefaultActivityProbe() *MultiActivityProbe {
	return NewActivityProbe(
		NewCursorActivityAdapter(),
		NewCodexActivityAdapter(),
		NewClaudeActivityAdapter(),
	)
}

// Infer implements ActivityProbe.
func (p *MultiActivityProbe) Infer(in ActivityInput) ActivitySignal {
	if p == nil {
		return ActivitySignal{}
	}
	for _, adapter := range p.adapters {
		if !adapter.Match(in) {
			continue
		}
		signal := adapter.Infer(in)
		if signal.Provider == "" {
			signal.Provider = adapter.Name()
		}
		return signal
	}
	return ActivitySignal{}
}

// MergeActivitySignal applies a provider activity signal to an already-resolved
// status. Progress leases, sticky done/failed/blocked, and pane blocked/failed
// always win: activity only fills Unknown.
func MergeActivitySignal(base AgentState, baseSummary string, signal ActivitySignal) (AgentState, string) {
	if signal.State == "" || signal.State == StateUnknown {
		return base, baseSummary
	}
	if base != StateUnknown {
		return base, baseSummary
	}
	switch signal.State {
	case StateRunning, StateBlocked, StateFailed, StateDone:
		return signal.State, firstNonEmpty(signal.Summary, baseSummary)
	default:
		return base, baseSummary
	}
}

// ResolveSessionStatus is the shared status pipeline:
//
//	Classify → MergeProgressAndClassification → MergeActivitySignal
//
// Brain progress leases remain highest priority for Running among durable
// progress signals; provider adapters only resolve remaining Unknown panes.
func ResolveSessionStatus(agent *Agent, classified AgentState, classifiedSummary string, now time.Time, activity ActivitySignal) (AgentState, string) {
	state, summary := MergeProgressAndClassification(agent, classified, classifiedSummary, now)
	return MergeActivitySignal(state, summary, activity)
}

func commandBaseName(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(filepathBase(fields[0]))
}

func filepathBase(path string) string {
	path = strings.Trim(path, `"'`)
	if path == "" {
		return ""
	}
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[i+1:]
	}
	return path
}
