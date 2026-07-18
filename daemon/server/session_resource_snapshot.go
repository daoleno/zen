package server

import (
	"fmt"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
	"github.com/gorilla/websocket"
)

type sessionResourceSnapshotWire struct {
	Type      string                     `json:"type"`
	RequestID string                     `json:"request_id,omitempty"`
	AgentID   string                     `json:"agent_id"`
	Session   sessionResourceSessionWire `json:"session"`
	Pool      *sessionResourcePoolWire   `json:"pool,omitempty"`
	Host      *sessionResourceHostWire   `json:"host,omitempty"`
}

type sessionResourceSessionWire struct {
	Name               string  `json:"name,omitempty"`
	Executor           string  `json:"executor,omitempty"`
	Command            string  `json:"command,omitempty"`
	Status             string  `json:"status,omitempty"`
	Phase              string  `json:"phase,omitempty"`
	StartedAt          string  `json:"started_at,omitempty"`
	Cwd                string  `json:"cwd,omitempty"`
	Delegated          bool    `json:"delegated,omitempty"`
	Managed            bool    `json:"managed,omitempty"`
	Backend            string  `json:"backend,omitempty"`
	MemoryCurrentBytes *uint64 `json:"memory_current_bytes,omitempty"`
	MemoryPeakBytes    *uint64 `json:"memory_peak_bytes,omitempty"`
	TasksCurrent       *int    `json:"tasks_current,omitempty"`
}

type sessionResourcePoolWire struct {
	Backend            string  `json:"backend,omitempty"`
	MemoryCurrentBytes *uint64 `json:"memory_current_bytes,omitempty"`
	MemoryHighBytes    *uint64 `json:"memory_high_bytes,omitempty"`
	MemoryMaxBytes     *uint64 `json:"memory_max_bytes,omitempty"`
}

type sessionResourceHostWire struct {
	AvailableBytes *uint64 `json:"available_bytes,omitempty"`
	Pressure       string  `json:"pressure,omitempty"`
}

func (s *Server) handleGetSessionResourceSnapshot(conn *websocket.Conn, raw clientMessage) {
	agentID := strings.TrimSpace(raw.AgentID)
	if agentID == "" {
		agentID = strings.TrimSpace(raw.TargetID)
	}
	if agentID == "" {
		s.sendErrorWithRequestID(conn, raw.RequestID, "session_resource_snapshot_failed", "agent_id is required")
		return
	}

	agent, resource, err := s.resolveSessionResourceSnapshot(agentID)
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "session_resource_snapshot_failed", err.Error())
		return
	}

	s.sendJSON(conn, s.buildSessionResourceSnapshotWire(raw.RequestID, agentID, agent, resource))
}

// resolveSessionResourceSnapshot loads the live Session and one on-demand
// resource projection. Missing Sessions fail instead of synthesizing Local/Not managed.
func (s *Server) resolveSessionResourceSnapshot(agentID string) (*classifier.Agent, watcher.SessionResourceSnapshot, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, watcher.SessionResourceSnapshot{}, fmt.Errorf("agent_id is required")
	}
	if s == nil || s.watcher == nil {
		return nil, watcher.SessionResourceSnapshot{}, fmt.Errorf("agent session not found")
	}
	agent := s.watcher.GetAgent(agentID)
	if agent == nil {
		return nil, watcher.SessionResourceSnapshot{}, fmt.Errorf("agent session not found")
	}
	return agent, s.watcher.SessionResourceSnapshot(agentID), nil
}

func (s *Server) buildSessionResourceSnapshotWire(
	requestID, agentID string,
	agent *classifier.Agent,
	resource watcher.SessionResourceSnapshot,
) sessionResourceSnapshotWire {
	payload := sessionResourceSnapshotWire{
		Type:      "session_resource_snapshot",
		RequestID: strings.TrimSpace(requestID),
		AgentID:   agentID,
		Session: sessionResourceSessionWire{
			Managed: resource.Managed,
		},
	}
	if resource.Managed {
		payload.Session.Backend = resource.Backend
		payload.Session.MemoryCurrentBytes = resource.MemoryCurrentBytes
		payload.Session.MemoryPeakBytes = resource.MemoryPeakBytes
		payload.Session.TasksCurrent = resource.TasksCurrent
	}
	if agent != nil {
		payload.Session.Name = strings.TrimSpace(agent.Name)
		payload.Session.Command = strings.TrimSpace(agent.Command)
		payload.Session.Status = string(agent.State)
		payload.Session.Phase = strings.TrimSpace(agent.Phase)
		payload.Session.Cwd = strings.TrimSpace(agent.Cwd)
		payload.Session.Delegated = agent.Delegated
		if !agent.StartedAt.IsZero() {
			payload.Session.StartedAt = agent.StartedAt.UTC().Format(time.RFC3339Nano)
		}
		payload.Session.Executor = work.InferAgentProvider(agent.Command, agent.Name)
	}
	// Wire contract: pool is only for Sessions Zen actually manages. Watcher may
	// still observe the shared pool for unmanaged targets; do not expose it here.
	if resource.Managed {
		if pool := sessionResourcePoolWireFrom(resource); pool != nil {
			payload.Pool = pool
		}
	}
	if host := sessionResourceHostWireFrom(resource); host != nil {
		payload.Host = host
	}
	return payload
}

func sessionResourcePoolWireFrom(resource watcher.SessionResourceSnapshot) *sessionResourcePoolWire {
	if !resource.Managed {
		return nil
	}
	if resource.Backend == "" &&
		resource.PoolMemoryCurrentBytes == nil &&
		resource.PoolMemoryHighBytes == nil &&
		resource.PoolMemoryMaxBytes == nil {
		return nil
	}
	return &sessionResourcePoolWire{
		Backend:            resource.Backend,
		MemoryCurrentBytes: resource.PoolMemoryCurrentBytes,
		MemoryHighBytes:    resource.PoolMemoryHighBytes,
		MemoryMaxBytes:     resource.PoolMemoryMaxBytes,
	}
}

func sessionResourceHostWireFrom(resource watcher.SessionResourceSnapshot) *sessionResourceHostWire {
	if resource.HostAvailableBytes == nil && resource.HostPressure == "" {
		return nil
	}
	return &sessionResourceHostWire{
		AvailableBytes: resource.HostAvailableBytes,
		Pressure:       resource.HostPressure,
	}
}
