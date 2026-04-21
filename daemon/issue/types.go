package issue

import "time"

// Issue is the canonical in-memory representation of one Markdown file.
type Issue struct {
	ID          string      `json:"id"`
	Path        string      `json:"path"`
	Project     string      `json:"project"`
	Title       string      `json:"title"`
	Body        string      `json:"body"`
	Frontmatter Frontmatter `json:"frontmatter"`
	Mentions    []Mention   `json:"mentions"`
	Mtime       time.Time   `json:"mtime"`
}

// Frontmatter holds the structured fields we read/write in the YAML block.
// Unknown fields are preserved via Extra so agent-written metadata survives
// round-trips through the daemon and app.
type Frontmatter struct {
	ID           string                 `yaml:"id" json:"id"`
	Created      time.Time              `yaml:"created" json:"created"`
	Done         *time.Time             `yaml:"done,omitempty" json:"done,omitempty"`
	Dispatched   *time.Time             `yaml:"dispatched,omitempty" json:"dispatched,omitempty"`
	AgentSession string                 `yaml:"agent_session,omitempty" json:"agent_session,omitempty"`
	Extra        map[string]interface{} `yaml:"-" json:"extra,omitempty"`
}

// Mention is one @role or @role#session reference in the body.
type Mention struct {
	Role    string `json:"role"`
	Session string `json:"session,omitempty"`
	Index   int    `json:"index"`
}

// Executor is one configured agent kind (claude, codex, ...).
type Executor struct {
	Name    string `json:"name" toml:"name"`
	Command string `json:"command" toml:"command"`
}

// Project holds the content of one project.toml.
type Project struct {
	Name     string `toml:"name" json:"name"`
	Cwd      string `toml:"cwd" json:"cwd"`
	Executor string `toml:"executor" json:"executor"`
}

func cloneIssue(iss *Issue) *Issue {
	if iss == nil {
		return nil
	}
	cp := *iss
	cp.Frontmatter = cloneFrontmatter(iss.Frontmatter)
	if iss.Mentions != nil {
		cp.Mentions = append([]Mention(nil), iss.Mentions...)
	}
	return &cp
}

func cloneFrontmatter(fm Frontmatter) Frontmatter {
	cp := fm
	if fm.Done != nil {
		done := *fm.Done
		cp.Done = &done
	}
	if fm.Dispatched != nil {
		dispatched := *fm.Dispatched
		cp.Dispatched = &dispatched
	}
	if fm.Extra != nil {
		cp.Extra = make(map[string]interface{}, len(fm.Extra))
		for key, value := range fm.Extra {
			cp.Extra[key] = value
		}
	}
	return cp
}
