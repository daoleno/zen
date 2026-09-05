package brain

import (
	"fmt"
	"log"
	"strings"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/modelprofiles"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

// hostDiscovery is the read-only decision produced before any route or tmux
// mutation. Keeping it separate makes liveness uncertainty fail closed at one
// boundary and leaves ensureHostAgent responsible for applying the decision.
type hostDiscovery struct {
	hostSession   HostSession
	command       string
	id            string
	reuse         *classifier.Agent
	bootstrap     bool
	replaceReason string
	replaceDetail string
}

type hostLaunchPreparation struct {
	command            string
	env                map[string]string
	provisionalID      string
	resumeBindingFound bool
}

func (s *Service) prepareHostLaunch(executor work.AgentExecutor, id, command, resumeToken string) (hostLaunchPreparation, error) {
	p := hostLaunchPreparation{command: command, env: brainSessionEnvironment()}
	routes := s.sessionRoutes()
	if routes != nil && strings.TrimSpace(id) != "" && resumeToken != "" {
		routeCommand, routeEnv, found, err := routes.ResumeLaunch(id, command)
		if err != nil {
			return p, fmt.Errorf("brain host refusing blank replacement: recorded route for session %q cannot be resumed: %w", id, err)
		}
		p.resumeBindingFound = found
		if found {
			if strings.TrimSpace(routeCommand) != "" {
				p.command = routeCommand
			}
			p.env = mergeStringMaps(p.env, routeEnv)
		}
	}
	if routes == nil || p.resumeBindingFound {
		return p, nil
	}
	clientHint := work.ProfileClientExecutor(executor.Provider, executor.Command, executor.ID)
	plan, planErr := routes.PrepareLaunch(clientHint, "", p.command)
	if planErr != nil && !plan.Persist.Applied && !plan.Bypass {
		return p, fmt.Errorf("brain host profile prepare: %w", planErr)
	}
	if plan.Applied && !plan.Bypass {
		if strings.TrimSpace(plan.Command) != "" {
			p.command = plan.Command
		}
		p.env = mergeStringMaps(p.env, plan.Env)
		p.provisionalID = plan.ProvisionalID
		if planErr != nil || !plan.Persist.Durable {
			if planErr == nil {
				planErr = modelprofiles.ErrPersistDirSync
			}
			log.Printf("brain host profile prepare applied; Commit is durability barrier (prepare uncertain: %v)", planErr)
		}
	}
	return p, nil
}

func (s *Service) discoverHostAgent(executor work.AgentExecutor) (hostDiscovery, error) {
	if s == nil || s.store == nil || s.watcher == nil {
		return hostDiscovery{}, nil
	}
	hostSession, err := s.store.HostSession()
	if err != nil {
		return hostDiscovery{}, err
	}
	command, err := s.hostCommand(executor)
	if err != nil {
		return hostDiscovery{}, err
	}
	d := hostDiscovery{hostSession: hostSession, command: command, id: strings.TrimSpace(hostSession.ID)}
	if d.id == "" {
		d.replaceReason = hostReplaceReasonNoRecordedHost
		return d, nil
	}
	presence, probeErr := s.watcher.ProbeSession(d.id)
	switch {
	case probeErr != nil || presence == watcher.SessionPresenceUnknown:
		if probeErr == nil {
			probeErr = fmt.Errorf("tmux probe returned unknown for %q", d.id)
		}
		return hostDiscovery{}, fmt.Errorf("brain host recorded session liveness unknown: %w", probeErr)
	case presence == watcher.SessionPresencePresent:
		if agent := s.watcher.GetAgent(d.id); agent != nil {
			if s.hostAgentMatches(agent, executor) {
				if strings.TrimSpace(hostSession.ExecutorID) != executor.ID {
					if err := s.store.SetHostSession(d.id, executor.ID); err != nil {
						return hostDiscovery{}, err
					}
				}
				if err := s.ensureHostActivation(d.id, command, executor, false, false); err != nil {
					return hostDiscovery{}, err
				}
				_, _ = s.BindHostProviderTranscript()
				return hostDiscovery{hostSession: hostSession, command: command, id: d.id, reuse: agent}, nil
			}
			d.replaceReason = hostReplaceReasonProviderMismatch
			d.replaceDetail = fmt.Sprintf("recorded_executor=%q resolved_executor=%q agent_command=%q agent_provider=%q", hostSession.ExecutorID, executor.ID, strings.TrimSpace(agent.Command), work.InferAgentProvider(agent.Command))
			s.recordHostReplacement(HostReplacementEvent{Reason: d.replaceReason, FromID: d.id, FromExecutorID: hostSession.ExecutorID, FromCommand: agent.Command, ResolvedExecutor: executor.ID, Detail: d.replaceDetail})
			if err := s.teardownHostSession(d.id); err != nil {
				return hostDiscovery{}, fmt.Errorf("brain host provider replacement teardown: %w", err)
			}
		} else {
			if strings.TrimSpace(hostSession.ExecutorID) != executor.ID {
				if err := s.store.SetHostSession(d.id, executor.ID); err != nil {
					return hostDiscovery{}, err
				}
			}
			return hostDiscovery{hostSession: hostSession, command: command, id: d.id, bootstrap: true}, nil
		}
	default:
		recovered, recoverErr := s.recoverMatchingHost(executor, hostSession)
		if recoverErr != nil {
			return hostDiscovery{}, recoverErr
		}
		if recovered != nil {
			if err := s.rebindRecoveredHost(d.id, recovered, executor, hostSession); err != nil {
				return hostDiscovery{}, err
			}
			if err := s.ensureHostActivation(recovered.ID, recovered.Command, executor, false, false); err != nil {
				return hostDiscovery{}, err
			}
			return hostDiscovery{hostSession: hostSession, command: recovered.Command, id: recovered.ID, reuse: recovered}, nil
		}
		d.replaceReason = hostReplaceReasonMissingTmux
		d.replaceDetail = fmt.Sprintf("probe=absent id=%q", d.id)
		s.recordHostReplacement(HostReplacementEvent{Reason: d.replaceReason, FromID: d.id, FromExecutorID: hostSession.ExecutorID, ResolvedExecutor: executor.ID, Detail: d.replaceDetail})
	}
	return d, nil
}

func hostBootstrapRef(s *Service, d hostDiscovery) AgentRef {
	return AgentRef{ID: d.id, Name: "Brain", Status: string(classifier.StateRunning), Summary: "Session starting", Cwd: s.brainWorkspace(), Command: d.command, Updated: firstNonZeroTime(d.hostSession.UpdatedAt, s.now().UTC()), Hidden: true}
}
