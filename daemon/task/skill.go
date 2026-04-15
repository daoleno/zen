package task

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
)

const DefaultClaudeAgentCmd = "claude --dangerously-skip-permissions"

var defaultSkills = []Skill{
	{
		ID:       "builtin-review",
		Name:     "Code Review",
		Icon:     "eye",
		AgentCmd: DefaultClaudeAgentCmd,
		Prompt:   "Review the recent changes in this repo. Focus on correctness, edge cases, and style.",
	},
	{
		ID:       "builtin-fix-tests",
		Name:     "Fix Tests",
		Icon:     "wrench",
		AgentCmd: DefaultClaudeAgentCmd,
		Prompt:   "Run the test suite, identify failures, and fix them.",
	},
	{
		ID:       "builtin-explain",
		Name:     "Explain",
		Icon:     "book",
		AgentCmd: DefaultClaudeAgentCmd,
		Prompt:   "Explain the architecture and key design decisions of this codebase.",
	},
}

// SkillStore persists reusable prompt templates.
type SkillStore struct {
	mu     sync.RWMutex
	skills map[string]*Skill
	path   string
}

func NewSkillStore(dir string) (*SkillStore, error) {
	path := filepath.Join(dir, "skills.json")
	s := &SkillStore{
		skills: make(map[string]*Skill),
		path:   path,
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	if len(s.skills) == 0 {
		for i := range defaultSkills {
			sk := defaultSkills[i]
			s.skills[sk.ID] = &sk
		}
		_ = s.persist()
	}
	return s, nil
}

func (s *SkillStore) Get(id string) *Skill {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sk, ok := s.skills[id]
	if !ok {
		return nil
	}
	cp := *sk
	return &cp
}

func (s *SkillStore) List() []*Skill {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]*Skill, 0, len(s.skills))
	for _, sk := range s.skills {
		cp := *sk
		list = append(list, &cp)
	}
	return list
}

func (s *SkillStore) Create(name, icon, agentCmd, prompt, cwd string) (*Skill, error) {
	sk := &Skill{
		ID:       uuid.New().String(),
		Name:     name,
		Icon:     icon,
		AgentCmd: agentCmd,
		Prompt:   prompt,
		Cwd:      cwd,
	}

	s.mu.Lock()
	s.skills[sk.ID] = sk
	if err := s.persist(); err != nil {
		delete(s.skills, sk.ID)
		s.mu.Unlock()
		return nil, err
	}
	s.mu.Unlock()
	return sk, nil
}

func (s *SkillStore) Update(id string, fn func(*Skill)) (*Skill, error) {
	s.mu.Lock()
	sk, ok := s.skills[id]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("skill %s not found", id)
	}
	fn(sk)
	if err := s.persist(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	cp := *sk
	s.mu.Unlock()
	return &cp, nil
}

func (s *SkillStore) Delete(id string) error {
	s.mu.Lock()
	if _, ok := s.skills[id]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("skill %s not found", id)
	}
	delete(s.skills, id)
	if err := s.persist(); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	return nil
}

func (s *SkillStore) load() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read skills file: %w", err)
	}
	var list []*Skill
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("parse skills file: %w", err)
	}
	for _, sk := range list {
		s.skills[sk.ID] = sk
	}
	return nil
}

func (s *SkillStore) persist() error {
	list := make([]*Skill, 0, len(s.skills))
	for _, sk := range s.skills {
		list = append(list, sk)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path, data, 0o600)
}
