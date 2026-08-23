package brain

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const playbooksDirName = "playbooks"

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
	for _, playbook := range seedPlaybooks {
		if err := ensurePlaybookFile(s.playbookPath(playbook.name), playbook.initial); err != nil {
			return err
		}
	}
	return nil
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
	{"brain-flows.md", defaultBrainFlowsPlaybook},
	{"align.md", defaultAlignPlaybook},
	{"delegate-brief.md", defaultDelegateBriefPlaybook},
	{"slice-work.md", defaultSliceWorkPlaybook},
	{"wayfind.md", defaultWayfindPlaybook},
}

const defaultPlaybooksReadme = `# Brain Playbooks

Provider-neutral operating playbooks for Brain lifecycle. Read on demand — bootstrap and policies mention the catalog, not full bodies.

Use ` + "`zen brain playbooks --json`" + ` for metadata: name, description, path.

## Seed playbooks

- **brain-flows** — Route situations to the right flow
- **align** — Decision-frontier alignment before delegation
- **delegate-brief** — Behavioral contract for delegated agents
- **slice-work** — Tracer-bullet work decomposition
- **wayfind** — Fog-of-war planning for large objectives

Progressive disclosure: pick one playbook, read it fully, apply it to the current task.
`

const defaultBrainFlowsPlaybook = `---
description: Route user situations to the smallest Brain flow that can produce an executable brief.
---

# Brain Flows

When intent is unclear or the task spans multiple modes, pick one flow:

| Situation | Playbook |
|-----------|----------|
| Goal fuzzy, decisions unresolved | align |
| Ready to delegate but brief is weak | delegate-brief |
| Large work needs decomposition | slice-work |
| Objective huge, map unknown | wayfind |
| Straight execution with clear brief | delegate directly |

When routing depends on a material user decision, include it in the next align round. When it does not, choose the smallest matching flow and proceed.
`

const defaultAlignPlaybook = `---
description: Resolve the current decision frontier in small rounds, then execute.
---

# Align

Resolve only decisions that materially change the outcome, risk, or user values. Keep the round small enough to answer quickly.

## Decision-frontier loop

1. State the understood goal, constraints, and concrete completion conditions.
2. Research discoverable environment facts with available tools or delegated agents.
3. Identify the required user decisions that are unblocked now. A decision is required only when its answer materially changes outcome, risk, or values.
4. Ask all currently independent required decisions in one numbered round. For each decision, give a recommended default and its relevant tradeoff.
5. Apply the answers, continue any newly unblocked research, and form another small round only when required.

Unresolved research blocks only decisions that depend on it. Continue independent research and ask independent required decisions without waiting for the whole tree.

## Stop when

- The brief is executable with observable completion conditions.
- Every remaining unknown has a safe default that does not materially change outcome, risk, or values.

Proceed without a mandatory final confirmation gate. Explicit confirmation remains appropriate only when the action itself is high-risk, irreversible, or permission-gated.

## Completion check

- Discoverable facts were researched instead of sent back as user questions.
- Each round contained every currently independent required decision and a recommended default.
- Resolved decisions were not reopened without new evidence.
- The execution or delegation brief states scope, safety constraints, verification, and expected evidence.
`

const defaultDelegateBriefPlaybook = `---
description: Write an executable delegated-agent contract with observable completion conditions.
---

# Delegate Brief

Write delegated prompts as behavioral contracts. Prefer how things should work over where files live.

## Template

- **Objective**: one sentence outcome
- **Current behavior**: what happens today (behavior, not paths)
- **Desired behavior**: what should change
- **Key interfaces/contracts**: APIs, types, invariants
- **Acceptance criteria**: observable done conditions
- **Out of scope**: explicit exclusions
- **Safety constraints**: what not to break, secrets, scope limits
- **Verification**: commands or checks the agent can run
- **Expected report**: files changed, behavior added, tests run, caveats

## Rules

- One concern per delegated session.
- Give the agent a concrete outcome and bounded implementation responsibility.
- Include the known context and environment facts needed to begin in the named workspace.
- Make acceptance criteria and verification observable in the agent's report.
- File paths only when the task is a narrow patch.
`

const defaultSliceWorkPlaybook = `---
description: Tracer-bullet decomposition with blocking edges and frontier tasks.
---

# Slice Work

Decompose large work into vertical slices that prove the path end-to-end.

## Concepts

- **Tracer bullet**: thinnest slice through all layers that validates the approach.
- **Blocking edge**: dependency that prevents a task from starting; resolve or defer explicitly.
- **Frontier**: unblocked, unclaimed tasks ready to delegate now.

## Process

1. Name the destination (user-visible outcome).
2. Identify the tracer bullet — smallest proof the approach works.
3. List tasks with blocking edges marked.
4. Delegate frontier tasks in parallel when independent.
5. Re-slice after each tracer bullet lands; the map will change.

## Ticket shape

Each slice: objective, acceptance criteria, verification, dependencies, out of scope.
Prefer fresh delegated sessions per slice when context would bloat; reuse when one thread is clearer.
`

const defaultWayfindPlaybook = `---
description: Fog-of-war planning when the objective is too large for upfront tickets.
---

# Wayfind

For huge or ambiguous objectives, discover the map before pretending you can plan every ticket.

## Map fields (maintain in current.md or a worklog record)

- **Destination**: where we're going in user terms
- **Notes**: observations, constraints, context
- **Decisions so far**: durable choices already made
- **Not yet specified**: open design space
- **Out of scope**: explicit boundaries
- **Frontier tasks**: unblocked, unclaimed, ready to delegate

## Rules

- Do not implement until the next frontier task is identifiable.
- Do not over-plan past the fog line — update the map as tracer bullets land.
- Prefer wayfind over slice-work when you cannot yet name vertical slices.
- Transition to slice-work once the tracer path is visible.
`
