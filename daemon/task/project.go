package task

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ProjectStore persists projects for optional issue grouping.
type ProjectStore struct {
	mu       sync.RWMutex
	projects map[string]*Project
	path     string
}

func NewProjectStore(dir string) (*ProjectStore, error) {
	path := filepath.Join(dir, "projects.json")
	ps := &ProjectStore{
		projects: make(map[string]*Project),
		path:     path,
	}
	if err := ps.load(); err != nil {
		return nil, err
	}
	return ps, nil
}

func (ps *ProjectStore) Get(id string) *Project {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	p, ok := ps.projects[id]
	if !ok {
		return nil
	}
	cp := *p
	return &cp
}

func (ps *ProjectStore) List() []*Project {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	list := make([]*Project, 0, len(ps.projects))
	for _, p := range ps.projects {
		cp := *p
		list = append(list, &cp)
	}
	return list
}

func (ps *ProjectStore) Create(name, icon string) (*Project, error) {
	p := &Project{
		ID:        uuid.New().String(),
		Name:      name,
		Icon:      icon,
		CreatedAt: time.Now().UTC(),
	}

	ps.mu.Lock()
	ps.projects[p.ID] = p
	if err := ps.persist(); err != nil {
		delete(ps.projects, p.ID)
		ps.mu.Unlock()
		return nil, err
	}
	ps.mu.Unlock()
	return p, nil
}

func (ps *ProjectStore) Delete(id string) error {
	ps.mu.Lock()
	if _, ok := ps.projects[id]; !ok {
		ps.mu.Unlock()
		return fmt.Errorf("project %s not found", id)
	}
	delete(ps.projects, id)
	if err := ps.persist(); err != nil {
		ps.mu.Unlock()
		return err
	}
	ps.mu.Unlock()
	return nil
}

func (ps *ProjectStore) load() error {
	data, err := os.ReadFile(ps.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read projects file: %w", err)
	}
	var list []*Project
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("parse projects file: %w", err)
	}
	for _, p := range list {
		ps.projects[p.ID] = p
	}
	return nil
}

func (ps *ProjectStore) persist() error {
	list := make([]*Project, 0, len(ps.projects))
	for _, p := range ps.projects {
		list = append(list, p)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(ps.path, data, 0o600)
}
