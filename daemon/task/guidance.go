package task

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// GuidanceStore persists agent guidance (preamble + constraints)
// that gets injected into every delegated prompt.
type GuidanceStore struct {
	mu       sync.RWMutex
	guidance *Guidance
	path     string
}

func NewGuidanceStore(dir string) (*GuidanceStore, error) {
	path := filepath.Join(dir, "guidance.json")
	gs := &GuidanceStore{
		guidance: &Guidance{},
		path:     path,
	}
	if err := gs.load(); err != nil {
		return nil, err
	}
	return gs, nil
}

func (gs *GuidanceStore) Get() Guidance {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return *gs.guidance
}

func (gs *GuidanceStore) Set(preamble string, constraints []string) (*Guidance, error) {
	gs.mu.Lock()
	gs.guidance = &Guidance{
		Preamble:    strings.TrimSpace(preamble),
		Constraints: constraints,
	}
	if err := gs.persist(); err != nil {
		gs.mu.Unlock()
		return nil, err
	}
	cp := *gs.guidance
	gs.mu.Unlock()
	return &cp, nil
}

// BuildPromptPrefix returns the guidance as a prompt prefix string.
// Returns empty string if no guidance is configured.
func (gs *GuidanceStore) BuildPromptPrefix() string {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	var parts []string
	if gs.guidance.Preamble != "" {
		parts = append(parts, gs.guidance.Preamble)
	}
	for _, c := range gs.guidance.Constraints {
		c = strings.TrimSpace(c)
		if c != "" {
			parts = append(parts, "- "+c)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n") + "\n\n"
}

type persistedGuidance struct {
	Preamble    string   `json:"preamble"`
	Constraints []string `json:"constraints"`
	UpdatedAt   string   `json:"updated_at"`
}

func (gs *GuidanceStore) load() error {
	data, err := os.ReadFile(gs.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read guidance file: %w", err)
	}
	var pg persistedGuidance
	if err := json.Unmarshal(data, &pg); err != nil {
		return fmt.Errorf("parse guidance file: %w", err)
	}
	gs.guidance = &Guidance{
		Preamble:    pg.Preamble,
		Constraints: pg.Constraints,
	}
	return nil
}

func (gs *GuidanceStore) persist() error {
	pg := persistedGuidance{
		Preamble:    gs.guidance.Preamble,
		Constraints: gs.guidance.Constraints,
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(pg, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(gs.path, data, 0o600)
}
