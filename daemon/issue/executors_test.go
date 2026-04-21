package issue

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadExecutors_Defaults(t *testing.T) {
	cfg, err := LoadExecutors("/nonexistent/path")
	if err != nil {
		t.Fatalf("LoadExecutors: %v", err)
	}
	if cfg.Default != "claude" {
		t.Fatalf("default = %q", cfg.Default)
	}
	if _, ok := cfg.ByName["claude"]; !ok {
		t.Fatal("claude missing")
	}
	if _, ok := cfg.ByName["codex"]; !ok {
		t.Fatal("codex missing")
	}
}

func TestLoadExecutors_CustomFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "executors.toml")
	err := os.WriteFile(path, []byte(`
default_executor = "codex"

[[executors]]
name = "claude"
command = "/opt/claude"

[[executors]]
name = "codex"
command = "/opt/codex"

[[executors]]
name = "gpt5"
command = "/opt/gpt5"
`), 0o600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadExecutors(path)
	if err != nil {
		t.Fatalf("LoadExecutors: %v", err)
	}
	if cfg.Default != "codex" {
		t.Fatalf("default = %q", cfg.Default)
	}
	if cfg.ByName["claude"].Command != "/opt/claude" {
		t.Fatalf("claude = %+v", cfg.ByName["claude"])
	}
	if _, ok := cfg.ByName["gpt5"]; !ok {
		t.Fatal("gpt5 missing")
	}
}

func TestLoadExecutors_Roles(t *testing.T) {
	cfg, err := LoadExecutors("/nonexistent")
	if err != nil {
		t.Fatalf("LoadExecutors: %v", err)
	}
	names := cfg.Roles()
	if len(names) < 2 {
		t.Fatalf("roles = %v", names)
	}
}
