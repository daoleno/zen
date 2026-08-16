package skills

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Fixture builds an isolated HOME + project + Zen state dir so every test runs
// against real filesystem behavior without ever touching a user installation.
type fixture struct {
	T        *testing.T
	Home     string
	StateDir string
	Project  string
	Env      map[string]string
	Now      time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	home := t.TempDir()
	project := filepath.Join(home, "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(home, ".zen")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	return &fixture{
		T: t, Home: home, StateDir: state, Project: project,
		Env: map[string]string{},
		Now: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
	}
}

func (f *fixture) options(cwd string) InventoryOptions {
	return InventoryOptions{
		Home: f.Home, ZenStateDir: f.StateDir, CWD: cwd, Env: f.Env,
		Now: func() time.Time { return f.Now },
	}
}

func (f *fixture) store() Store {
	return Store{StateDir: f.StateDir, Home: f.Home, Now: func() time.Time { return f.Now }}
}

// writeSkill creates <dir>/<name>/SKILL.md (+optional extra files).
func (f *fixture) writeSkill(dir, name, body string) string {
	f.T.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(path, 0o700); err != nil {
		f.T.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: test skill\n---\n" + body
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte(content), 0o600); err != nil {
		f.T.Fatal(err)
	}
	return path
}

// agentGlobalDir returns the adapter global skills dir under this fixture home.
func (f *fixture) agentGlobalDir(agent Agent) string {
	adapter := Adapters[agent]
	return globalSkillsDir(adapter, f.Home, func(key string) string { return f.Env[key] })
}

func (f *fixture) agentProjectDir(agent Agent, cwd string) string {
	adapter := Adapters[agent]
	return projectSkillsDir(adapter, cwd)
}

// writeTestSkill creates <dir>/SKILL.md (dir already exists) with the given
// name and description. Shared with the plugin discovery tests.
func writeTestSkill(t *testing.T, dir, name, description string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\nBody of " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
