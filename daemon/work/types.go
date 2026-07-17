package work

import "time"

// Item is the canonical in-memory representation of one Markdown file.
type Item struct {
	ID          string      `json:"id"`
	Path        string      `json:"path"`
	Project     string      `json:"project"`
	Title       string      `json:"title"`
	Body        string      `json:"body"`
	Frontmatter Frontmatter `json:"frontmatter"`
	Mtime       time.Time   `json:"mtime"`
}

// Frontmatter holds the structured fields we read/write in the YAML block.
// Unknown fields are preserved via Extra so agent-written metadata survives
// round-trips through the daemon and app.
type Frontmatter struct {
	ID           string                 `yaml:"id" json:"id"`
	Kind         string                 `yaml:"kind,omitempty" json:"kind,omitempty"`
	Created      time.Time              `yaml:"created" json:"created"`
	Done         *time.Time             `yaml:"done,omitempty" json:"done,omitempty"`
	Started      *time.Time             `yaml:"started,omitempty" json:"started,omitempty"`
	Status       string                 `yaml:"status,omitempty" json:"status,omitempty"`
	Title        string                 `yaml:"title,omitempty" json:"title,omitempty"`
	AgentSession string                 `yaml:"agent_session,omitempty" json:"agent_session,omitempty"`
	Extra        map[string]interface{} `yaml:"-" json:"extra,omitempty"`
}

// Executor is one configured agent kind (claude, codex, custom CLI, ...).
type Executor struct {
	Name    string `json:"name" toml:"name"`
	Command string `json:"command" toml:"command"`
	Kind    string `json:"kind,omitempty" toml:"kind"`
	Runtime string `json:"runtime,omitempty" toml:"runtime"`
}

func cloneItem(iss *Item) *Item {
	if iss == nil {
		return nil
	}
	cp := *iss
	cp.Frontmatter = cloneFrontmatter(iss.Frontmatter)
	return &cp
}

func cloneFrontmatter(fm Frontmatter) Frontmatter {
	cp := fm
	if fm.Done != nil {
		done := *fm.Done
		cp.Done = &done
	}
	if fm.Started != nil {
		started := *fm.Started
		cp.Started = &started
	}
	if fm.Extra != nil {
		cp.Extra = make(map[string]interface{}, len(fm.Extra))
		for key, value := range fm.Extra {
			cp.Extra[key] = value
		}
	}
	return cp
}
