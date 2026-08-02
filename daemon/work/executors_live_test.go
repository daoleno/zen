package work

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func fileInode(t *testing.T, info os.FileInfo) uint64 {
	t.Helper()
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		t.Fatalf("unexpected file info sys type %T", info.Sys())
	}
	return stat.Ino
}

func writeExecutorsTOML(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func loadExecutorsOrFatal(t *testing.T, path string) *ExecutorConfig {
	t.Helper()
	cfg, err := LoadExecutors(path)
	if err != nil {
		t.Fatalf("LoadExecutors: %v", err)
	}
	return cfg
}

func assertDelegated(t *testing.T, cfg *ExecutorConfig, want string) {
	t.Helper()
	if got := cfg.GetDelegatedExecutor(); got != want {
		t.Fatalf("GetDelegatedExecutor = %q, want %q", got, want)
	}
}

func assertLaunchRole(t *testing.T, cfg *ExecutorConfig, dir, wantRole, wantCmd string) {
	t.Helper()
	run := &fakeRunner{newID: wantRole + "-session"}
	item := &Item{Path: filepath.Join(dir, "item.md"), Frontmatter: Frontmatter{ID: "item", Created: time.Now()}}
	if _, err := NewLauncher(run, cfg).StartDedicated(item, dir); err != nil {
		t.Fatalf("StartDedicated: %v", err)
	}
	if len(run.spawnRoles) != 1 || run.spawnRoles[0] != wantRole {
		t.Fatalf("spawn roles = %#v, want %q", run.spawnRoles, wantRole)
	}
	if wantCmd != "" && (len(run.spawnCommands) != 1 || run.spawnCommands[0] != wantCmd) {
		t.Fatalf("spawn commands = %#v, want %q", run.spawnCommands, wantCmd)
	}
}

func assertFileUnchanged(t *testing.T, path string, original []byte, before os.FileInfo) {
	t.Helper()
	afterBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterBytes) != string(original) {
		t.Fatalf("durable file changed:\n%s", afterBytes)
	}
	afterInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fileInode(t, afterInfo) != fileInode(t, before) || !afterInfo.ModTime().Equal(before.ModTime()) {
		t.Fatal("durable file identity/mtime changed")
	}
}

func TestSetDelegatedExecutor_SameProcessSwitchUpdatesAllReadBoundaries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "executors.toml")
	writeExecutorsTOML(t, path, `
delegated_executor = "codex"

[[executors]]
name = "codex"
command = "codex"

[[executors]]
name = "grok"
command = "grok --live"
`)
	cfg := loadExecutorsOrFatal(t, path)
	assertDelegated(t, cfg, "codex")

	if err := cfg.SetDelegatedExecutor("grok"); err != nil {
		t.Fatalf("SetDelegatedExecutor: %v", err)
	}
	assertDelegated(t, cfg, "grok")
	delegated, ok := cfg.DelegatedAgentExecutor()
	if !ok || delegated.ID != "grok" || delegated.Command != "grok --live" || !delegated.Delegated {
		t.Fatalf("DelegatedAgentExecutor = %+v ok=%v", delegated, ok)
	}
	assertLaunchRole(t, cfg, dir, "grok", "grok --live --permission-mode bypassPermissions --sandbox off")
}

func TestSetDelegatedExecutor_PersistsTOMLAndFreshProcessState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "executors.toml")
	// Comment, inline comment, nested same-name key, and custom catalog must survive.
	original := []byte(`# keep this comment
delegated_executor = "codex" # keep why

[[executors]]
name = "codex"
command = "/opt/codex"
delegated_executor = "nested-must-stay"

[[executors]]
name = "grok"
command = "/opt/grok"

[[executors]]
name = "custom"
command = "my-agent --flag"
kind = "custom"
`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := loadExecutorsOrFatal(t, path)
	if err := cfg.SetDelegatedExecutor("grok"); err != nil {
		t.Fatalf("SetDelegatedExecutor: %v", err)
	}

	bodyBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(bodyBytes)
	if !strings.Contains(body, `# keep this comment`) {
		t.Fatalf("top comment lost:\n%s", body)
	}
	if !strings.Contains(body, `delegated_executor = "grok" # keep why`) {
		t.Fatalf("inline comment / value not preserved:\n%s", body)
	}
	if !strings.Contains(body, `delegated_executor = "nested-must-stay"`) {
		t.Fatalf("nested same-name key lost:\n%s", body)
	}
	if strings.Count(body, "delegated_executor") != 2 {
		t.Fatalf("unexpected delegated_executor count:\n%s", body)
	}
	if !strings.Contains(body, `name = "custom"`) || !strings.Contains(body, `command = "my-agent --flag"`) {
		t.Fatalf("custom executor lost:\n%s", body)
	}

	reloaded := loadExecutorsOrFatal(t, path)
	assertDelegated(t, reloaded, "grok")
	if reloaded.ByName["custom"].Command != "my-agent --flag" {
		t.Fatalf("custom executor lost: %+v", reloaded.ByName["custom"])
	}
	if reloaded.ByName["codex"].Command != "/opt/codex" {
		t.Fatalf("codex command lost: %+v", reloaded.ByName["codex"])
	}
}

func TestSetDelegatedExecutor_InvalidAndWriteFailureLeaveState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "executors.toml")
	original := []byte(`delegated_executor = "codex"

[[executors]]
name = "codex"
command = "codex"

[[executors]]
name = "grok"
command = "grok"
`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := loadExecutorsOrFatal(t, path)

	if err := cfg.SetDelegatedExecutor("missing-cli"); err == nil {
		t.Fatal("expected invalid selection error")
	}
	assertDelegated(t, cfg, "codex")
	if raw, err := os.ReadFile(path); err != nil || string(raw) != string(original) {
		t.Fatalf("file changed after invalid set: %s err=%v", raw, err)
	}
	if err := cfg.SetDelegatedExecutor("  "); err == nil {
		t.Fatal("expected empty id error")
	}
	assertDelegated(t, cfg, "codex")

	// (a) Existing ReadFile error on a directory path: Set leaves live selection unchanged.
	badPath := filepath.Join(t.TempDir(), "not-a-file")
	if err := os.Mkdir(badPath, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg.path = badPath
	if err := cfg.SetDelegatedExecutor("grok"); err == nil {
		t.Fatal("expected ReadFile/persist boundary failure for directory path")
	}
	assertDelegated(t, cfg, "codex")

	// (b) Same-package atomicWriteFile only: CreateTemp/write/close, Rename onto a
	// directory fails, deferred cleanup removes .executors-*.tmp (independent of Set).
	dest := filepath.Join(t.TempDir(), "executors.toml")
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(dest, []byte("delegated_executor = \"grok\"\n"), 0o600); err == nil {
		t.Fatal("expected atomicWriteFile rename failure")
	}
	parent := filepath.Dir(dest)
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".executors-") && strings.HasSuffix(name, ".tmp") {
			t.Fatalf("leftover temp file after failed atomicWriteFile: %s", name)
		}
	}
}

func TestSetDelegatedExecutor_MemoryOnlyWhenNoPath(t *testing.T) {
	cfg := NewExecutorConfig("codex", map[string]Executor{
		"codex": {Name: "codex", Command: "codex"},
		"grok":  {Name: "grok", Command: "grok"},
	})
	if err := cfg.SetDelegatedExecutor("grok"); err != nil {
		t.Fatalf("SetDelegatedExecutor: %v", err)
	}
	assertDelegated(t, cfg, "grok")
}

func TestSetDelegatedExecutor_ConcurrentReadsAndSwitches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "executors.toml")
	writeExecutorsTOML(t, path, `
delegated_executor = "codex"

[[executors]]
name = "codex"
command = "codex"

[[executors]]
name = "grok"
command = "grok"

[[executors]]
name = "claude"
command = "claude"
`)
	cfg := loadExecutorsOrFatal(t, path)
	ids := []string{"codex", "grok", "claude"}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			_ = cfg.SetDelegatedExecutor(ids[i%len(ids)])
		}(i)
		go func() {
			defer wg.Done()
			got := cfg.GetDelegatedExecutor()
			if got != "codex" && got != "grok" && got != "claude" {
				t.Errorf("observed invalid partial selection %q", got)
			}
			if delegated, ok := cfg.DelegatedAgentExecutor(); ok {
				if delegated.ID != "codex" && delegated.ID != "grok" && delegated.ID != "claude" {
					t.Errorf("DelegatedAgentExecutor invalid id %q", delegated.ID)
				}
				if !delegated.Delegated {
					t.Errorf("DelegatedAgentExecutor missing delegated flag: %+v", delegated)
				}
			}
			role := cfg.GetDelegatedExecutor()
			if _, ok := cfg.ByName[role]; !ok {
				t.Errorf("launch boundary saw unconfigured role %q", role)
			}
		}()
	}
	wg.Wait()

	final := cfg.GetDelegatedExecutor()
	if final != "codex" && final != "grok" && final != "claude" {
		t.Fatalf("final selection invalid: %q", final)
	}
	assertDelegated(t, loadExecutorsOrFatal(t, path), final)
}

func TestSetDelegatedExecutor_IdempotentSameSelectionDoesNotRewriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "executors.toml")
	original := []byte(`delegated_executor = "codex"

[[executors]]
name = "codex"
command = "codex"
`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := loadExecutorsOrFatal(t, path)
	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeIno := fileInode(t, beforeInfo)
	time.Sleep(25 * time.Millisecond)

	if err := cfg.SetDelegatedExecutor("codex"); err != nil {
		t.Fatal(err)
	}
	assertDelegated(t, cfg, "codex")
	assertFileUnchanged(t, path, original, beforeInfo)
	if fileInode(t, mustStat(t, path)) != beforeIno {
		t.Fatal("idempotent set replaced inode")
	}

	time.Sleep(25 * time.Millisecond)
	if _, ok := cfg.ByName["grok"]; !ok {
		t.Fatal("expected built-in grok in catalog for contrast rewrite")
	}
	if err := cfg.SetDelegatedExecutor("grok"); err != nil {
		t.Fatal(err)
	}
	changed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(changed) == string(original) || !strings.Contains(string(changed), `delegated_executor = "grok"`) {
		t.Fatalf("real switch did not rewrite file:\n%s", changed)
	}
}

func mustStat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestSetDelegatedExecutor_EnvLockIsSoleEffectiveOwnerAcrossBoundaries(t *testing.T) {
	t.Setenv("ZEN_DELEGATED_EXECUTOR", "grok")

	dir := t.TempDir()
	path := filepath.Join(dir, "executors.toml")
	original := []byte(`delegated_executor = "codex"

[[executors]]
name = "codex"
command = "codex"

[[executors]]
name = "grok"
command = "grok --env"

[[executors]]
name = "claude"
command = "claude"
`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := loadExecutorsOrFatal(t, path)

	assertDelegated(t, cfg, "grok")
	delegated, ok := cfg.DelegatedAgentExecutor()
	if !ok || delegated.ID != "grok" || delegated.Command != "grok --env" {
		t.Fatalf("DelegatedAgentExecutor = %+v ok=%v", delegated, ok)
	}
	assertLaunchRole(t, cfg, dir, "grok", "grok --env --permission-mode bypassPermissions --sandbox off")

	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	err = cfg.SetDelegatedExecutor("claude")
	if !errors.Is(err, ErrDelegatedExecutorLocked) {
		t.Fatalf("conflicting set error = %v, want ErrDelegatedExecutorLocked", err)
	}
	assertDelegated(t, cfg, "grok")
	assertFileUnchanged(t, path, original, beforeInfo)

	time.Sleep(25 * time.Millisecond)
	if err := cfg.SetDelegatedExecutor("grok"); err != nil {
		t.Fatalf("set effective locked id: %v", err)
	}
	assertFileUnchanged(t, path, original, beforeInfo)
}
