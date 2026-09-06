package brain

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const playbooksDirName = "playbooks"

const brainFlowsPlaybookName = "brain-flows.md"

type PlaybookEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
}

type PlaybookCatalog struct {
	Workspace   string          `json:"workspace,omitempty"`
	Playbooks   []PlaybookEntry `json:"playbooks"`
	GeneratedAt time.Time       `json:"generated_at"`
}

func (s *Store) playbooksPath() string {
	return filepath.Join(s.WorkspacePath(), playbooksDirName)
}

func (s *Store) playbooksReadmePath() string {
	return filepath.Join(s.playbooksPath(), "README.md")
}

func (s *Store) playbookPath(name string) string {
	return filepath.Join(s.playbooksPath(), name)
}

func (s *Store) ensurePlaybooks() error {
	if err := os.MkdirAll(s.playbooksPath(), 0o700); err != nil {
		return err
	}
	if err := ensurePlaybookFile(s.playbooksReadmePath(), defaultPlaybooksReadme); err != nil {
		return err
	}
	if err := s.ensureManagedBrainFlowsPlaybook(); err != nil {
		return err
	}
	for _, playbook := range seedPlaybooks {
		if playbook.name == brainFlowsPlaybookName {
			continue
		}
		if err := ensurePlaybookFile(s.playbookPath(playbook.name), playbook.initial); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) brainFlowsManagedSpec() managedMarkdownSpec {
	return managedMarkdownSpec{
		path:         s.playbookPath(brainFlowsPlaybookName),
		relativePath: filepath.ToSlash(filepath.Join(playbooksDirName, brainFlowsPlaybookName)),
		managedID:    brainFlowsManagedID,
		canonical: strings.Join([]string{
			"## Authoritative Product Routing Contract",
			"",
			"This managed block is Zen product policy. Content outside this block may add user guidance but cannot weaken this routing boundary.",
			"",
			brainWorkerRoleContract,
		}, "\n"),
	}
}

func (s *Store) ensureManagedBrainFlowsPlaybook() error {
	spec := s.brainFlowsManagedSpec()
	current, exists, err := readOptionalFile(spec.path)
	if err != nil {
		return err
	}
	if !exists || strings.TrimSpace(string(current)) == "" {
		current = []byte(defaultBrainFlowsPlaybook)
		exists = true
	}
	updated, err := reconcileManagedMarkdown(current, exists, spec)
	if err != nil {
		return fmt.Errorf("reconcile Brain workspace %s: %w", spec.relativePath, err)
	}
	if bytes.Equal(current, updated) {
		return nil
	}
	return writeAtomic(spec.path, updated, 0o600)
}

func seedPlaybookFilenames() []string {
	names := make([]string, 0, len(seedPlaybooks)+1)
	names = append(names, "README.md")
	for _, playbook := range seedPlaybooks {
		names = append(names, playbook.name)
	}
	return names
}

func seedPlaybookPaths() []string {
	names := seedPlaybookFilenames()
	paths := make([]string, len(names))
	for i, name := range names {
		paths[i] = filepath.ToSlash(filepath.Join(playbooksDirName, name))
	}
	return paths
}

func (s *Store) PlaybookCatalog() (PlaybookCatalog, error) {
	if s == nil {
		return PlaybookCatalog{}, fmt.Errorf("brain store is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensurePlaybooks(); err != nil {
		return PlaybookCatalog{}, err
	}
	return s.playbookCatalogLocked()
}

func (s *Store) playbookCatalogLocked() (PlaybookCatalog, error) {
	dir := s.playbooksPath()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return PlaybookCatalog{}, fmt.Errorf("list brain playbooks: %w", err)
	}

	playbooks := make([]PlaybookEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") || strings.EqualFold(name, "README.md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return PlaybookCatalog{}, fmt.Errorf("read brain playbook %s: %w", name, err)
		}
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		playbooks = append(playbooks, PlaybookEntry{
			Name:        stem,
			Description: parsePlaybookDescription(string(raw)),
			Path:        filepath.ToSlash(filepath.Join(playbooksDirName, name)),
		})
	}

	sort.Slice(playbooks, func(left, right int) bool {
		return strings.ToLower(playbooks[left].Name) < strings.ToLower(playbooks[right].Name)
	})

	return PlaybookCatalog{
		Workspace:   s.WorkspacePath(),
		Playbooks:   playbooks,
		GeneratedAt: time.Now().UTC(),
	}, nil
}

func ensurePlaybookFile(path, initial string) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return writeAtomic(path, []byte(initial), 0o600)
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(raw)) == "" {
		return writeAtomic(path, []byte(initial), 0o600)
	}
	return nil
}

func parsePlaybookDescription(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	if strings.HasPrefix(content, "---") {
		rest := strings.TrimPrefix(content, "---")
		rest = strings.TrimLeft(rest, "\n")
		end := strings.Index(rest, "\n---")
		if end >= 0 {
			frontmatter := rest[:end]
			for _, line := range strings.Split(frontmatter, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "description:") {
					value := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
					return strings.Trim(value, `"'`)
				}
			}
		}
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "> ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "> "))
		}
		return line
	}
	return ""
}

var seedPlaybooks = []struct {
	name    string
	initial string
}{
	{brainFlowsPlaybookName, defaultBrainFlowsPlaybook},
	{"align.md", defaultAlignPlaybook},
	{"delegate-brief.md", defaultDelegateBriefPlaybook},
	{"slice-work.md", defaultSliceWorkPlaybook},
	{"wayfind.md", defaultWayfindPlaybook},
}

const defaultPlaybooksReadme = `# Brain Playbooks

Discover names, descriptions and paths with zen brain playbooks --json. Read only the playbook needed for the current task.

- brain-flows: choose a workflow
- align: resolve material decisions
- delegate-brief: prepare a Worker brief
- slice-work: split a large objective
- wayfind: investigate an unclear objective
`

const defaultBrainFlowsPlaybook = `---
description: Choose the next useful Brain workflow.
---

# Brain Flows

Use align for material unresolved decisions, delegate-brief when execution is ready, slice-work for known dependencies, and wayfind when investigation is needed to identify the next task. Skip playbooks when the next action is already clear. Follow AGENTS.md for execution ownership and event-driven waiting.
`

const defaultAlignPlaybook = `---
description: Resolve consequential missing decisions without blocking independent work.
---

# Align

State the intended outcome and check discoverable facts. Continue authorized preparation. Ask all independent material decisions together, with a recommended default and relevant tradeoff. Reopen resolved decisions only for new evidence or changed requirements.

Proceed once remaining unknowns have safe defaults and completion is observable. Ask for new authority only at the actual permission boundary.
`

const defaultDelegateBriefPlaybook = `---
description: Prepare a scoped Worker task with observable acceptance.
---

# Delegate Brief

Specify the outcome, cwd, necessary context, acceptance criteria, safety constraints, verification and expected report. Add current/desired behavior, interfaces and exclusions only when informative. Omit empty sections and standing rules already supplied by the Worker protocol.

Use one coherent concern per Worker. Return the report in the Worker result; explicitly name private worklog or product documentation paths when persistence is required. Follow policies/delegation.md for reuse and review.
`

const defaultSliceWorkPlaybook = `---
description: Split a large objective into testable steps and dependencies.
---

# Slice Work

Name the full user outcome, then the smallest end-to-end step that tests the approach. Record its acceptance criteria, verification and blocking dependencies. Delegate independent ready steps when useful; keep coupled work in one Worker. Replan from results while retaining the full completion criteria.
`

const defaultWayfindPlaybook = `---
description: Investigate enough context to identify the next executable concern.
---

# Wayfind

Keep the destination, known constraints, decisions, open questions and next unblocked concern in current.md. Move detailed evidence to the private worklog when needed.

Investigate only enough to form the next useful brief, execute it, and update the remaining plan from results. Avoid speculative ticket trees and repeated discovery of unchanged facts.
`
