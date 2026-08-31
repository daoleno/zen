package brain

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const hostActivationStateVersion = 1

// HostActivation records the private product contract delivered to one exact
// Host pane/process generation. It is deliberately separate from provider and
// chat identity so activation cannot rotate or rewrite either history owner.
type HostActivation struct {
	StateVersion    int       `json:"state_version"`
	SessionID       string    `json:"session_id"`
	HostGeneration  string    `json:"host_generation"`
	ProcessIdentity string    `json:"process_identity,omitempty"`
	PaneGeneration  string    `json:"pane_generation,omitempty"`
	ContractVersion string    `json:"contract_version"`
	Receipt         string    `json:"receipt,omitempty"`
	ActivatedAt     time.Time `json:"activated_at"`
}

func (s *Store) HostActivationPath() string {
	return filepath.Join(s.statePath(), "host_activation.json")
}

func (s *Store) HostActivation() (HostActivation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := os.ReadFile(s.HostActivationPath())
	if errors.Is(err, os.ErrNotExist) || len(bytes.TrimSpace(raw)) == 0 {
		return HostActivation{}, nil
	}
	if err != nil {
		return HostActivation{}, err
	}
	var activation HostActivation
	if err := json.Unmarshal(raw, &activation); err != nil {
		return HostActivation{}, err
	}
	activation.SessionID = strings.TrimSpace(activation.SessionID)
	activation.HostGeneration = strings.TrimSpace(activation.HostGeneration)
	activation.ProcessIdentity = strings.TrimSpace(activation.ProcessIdentity)
	activation.PaneGeneration = strings.TrimSpace(activation.PaneGeneration)
	activation.ContractVersion = strings.TrimSpace(activation.ContractVersion)
	activation.Receipt = strings.TrimSpace(activation.Receipt)
	return activation, nil
}

func (s *Store) MarkHostActivation(activation HostActivation) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	activation.StateVersion = hostActivationStateVersion
	activation.SessionID = strings.TrimSpace(activation.SessionID)
	activation.HostGeneration = strings.TrimSpace(activation.HostGeneration)
	activation.ProcessIdentity = strings.TrimSpace(activation.ProcessIdentity)
	activation.PaneGeneration = strings.TrimSpace(activation.PaneGeneration)
	activation.ContractVersion = strings.TrimSpace(activation.ContractVersion)
	activation.Receipt = strings.TrimSpace(activation.Receipt)
	activation.ActivatedAt = activation.ActivatedAt.UTC()
	return writeJSONFile(s.HostActivationPath(), activation)
}
