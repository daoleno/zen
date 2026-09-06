package server

import (
	"path/filepath"
	"strings"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/work"
)

type workerSessionWireCapabilities struct {
	StructuredEvents         bool `json:"structured_events"`
	ModelProfileManaged      bool `json:"model_profile_managed"`
	ModelProfileActiveSwitch bool `json:"model_profile_active_switch"`
}

type workerSessionWire struct {
	*classifier.Worker
	Capabilities workerSessionWireCapabilities `json:"capabilities"`
}

func (s *Server) workerSessionWire(worker *classifier.Worker) *workerSessionWire {
	if worker == nil {
		return nil
	}
	managed, activeSwitch := s.modelProfileSessionCapabilities(worker.ID)
	return &workerSessionWire{
		Worker: worker,
		Capabilities: workerSessionWireCapabilities{
			StructuredEvents:         s.workerSupportsStructuredEvents(worker),
			ModelProfileManaged:      managed,
			ModelProfileActiveSwitch: activeSwitch,
		},
	}
}

// lookupAgent returns the live watcher Agent for sessionID. Missing watcher /
// agent fails closed (nil) so Brain Host capabilities never invent presence.
func (s *Server) lookupWorker(sessionID string) *classifier.Worker {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || s == nil {
		return nil
	}
	if s.getWorkerOverride != nil {
		return s.getWorkerOverride(sessionID)
	}
	if s.watcher == nil {
		return nil
	}
	return s.watcher.GetWorker(sessionID)
}

func (s *Server) hostWorkerWireCapabilities(sessionID string) workerSessionWireCapabilities {
	worker := s.lookupWorker(sessionID)
	if worker == nil {
		return workerSessionWireCapabilities{}
	}
	wire := s.workerSessionWire(worker)
	if wire == nil {
		return workerSessionWireCapabilities{}
	}
	return wire.Capabilities
}

// modelProfileSessionCapabilities reads the authoritative Model Profiles route
// table only. Command/name heuristics must never authorize App actions.
func (s *Server) modelProfileSessionCapabilities(sessionID string) (managed, activeSwitch bool) {
	if s == nil {
		return false, false
	}
	owner := s.modelProfiles()
	if owner == nil {
		return false, false
	}
	caps := owner.SessionRouteCapabilities(sessionID)
	return caps.Managed, caps.ActiveSwitch
}

func (s *Server) workerSessionsWire(workers []*classifier.Worker) []*workerSessionWire {
	if len(workers) == 0 {
		return nil
	}
	out := make([]*workerSessionWire, 0, len(workers))
	for _, worker := range workers {
		if wire := s.workerSessionWire(worker); wire != nil {
			out = append(out, wire)
		}
	}
	return out
}

func (s *Server) workerSupportsStructuredEvents(worker *classifier.Worker) bool {
	return s.structuredProviderForWorker(worker) != ""
}

func (s *Server) structuredProviderForWorker(worker *classifier.Worker) string {
	if worker == nil {
		return ""
	}
	// A real process command outranks the display title. Titles are user-facing
	// and may mention a provider without the shell actually running one.
	provider := work.InferWorkerProvider(worker.Command)
	if strings.TrimSpace(worker.Command) == "" {
		provider = work.InferWorkerProvider(worker.Name)
	}
	if provider != "" {
		portable := work.NewWorkerExecutor(provider, work.Executor{
			Name:    provider,
			Kind:    provider,
			Command: worker.Command,
		})
		if portable.Capabilities.StructuredEvents {
			return portable.Provider
		}
	}
	if s == nil || s.execs == nil {
		return ""
	}
	for name, configured := range s.execs.ByName {
		if !configuredExecutorMatchesWorker(name, configured, worker) {
			continue
		}
		portable := work.NewWorkerExecutor(name, configured)
		if portable.Capabilities.StructuredEvents {
			return portable.Provider
		}
	}
	return ""
}

func configuredExecutorMatchesWorker(name string, configured work.Executor, worker *classifier.Worker) bool {
	if worker == nil {
		return false
	}
	workerCommand := strings.TrimSpace(worker.Command)
	configuredCommand := strings.TrimSpace(configured.Command)
	if workerCommand != "" && configuredCommand != "" && workerCommand == configuredCommand {
		return true
	}
	workerExecutable := workerCommandExecutable(workerCommand)
	configuredExecutable := workerCommandExecutable(configuredCommand)
	if workerExecutable != "" && workerExecutable == configuredExecutable && !ambiguousWorkerExecutable(workerExecutable) {
		return true
	}
	if workerCommand != "" {
		configuredName := strings.TrimSpace(name)
		if configuredName == "" {
			configuredName = strings.TrimSpace(configured.Name)
		}
		return workerExecutable != "" && workerExecutable == filepath.Base(configuredName)
	}
	configuredName := strings.TrimSpace(name)
	if configuredName == "" {
		configuredName = strings.TrimSpace(configured.Name)
	}
	workerName := strings.TrimSpace(worker.Name)
	if workerName != "" && (workerName == configuredName || workerName == strings.TrimSpace(configured.Name)) {
		return true
	}
	return false
}

func workerCommandExecutable(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return ""
	}
	return filepath.Base(strings.Trim(fields[0], `"'`))
}

func ambiguousWorkerExecutable(executable string) bool {
	switch strings.ToLower(strings.TrimSpace(executable)) {
	case "bash", "bun", "deno", "fish", "node", "python", "python3", "sh", "zsh":
		return true
	default:
		return false
	}
}
