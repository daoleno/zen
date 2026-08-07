package watcher

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/agentproc"
)

type fakeDelegatedResourceManager struct {
	spec        *delegatedResourceSpec
	prepareErr  error
	boundTarget string
	boundUnit   string
	released    []string
	releaseErr  error
}

func (m *fakeDelegatedResourceManager) Prepare(int) (*delegatedResourceSpec, error) {
	return m.spec, m.prepareErr
}

func (m *fakeDelegatedResourceManager) Bind(target, unit string) {
	m.boundTarget = target
	m.boundUnit = unit
}

func (m *fakeDelegatedResourceManager) UnitForTarget(target string) string {
	if target == m.boundTarget {
		return m.boundUnit
	}
	return ""
}

func (*fakeDelegatedResourceManager) Reconcile([]tmuxWindow) {}

func (m *fakeDelegatedResourceManager) Release(target, unit string) error {
	m.released = append(m.released, target+"\t"+unit)
	return m.releaseErr
}

func (m *fakeDelegatedResourceManager) Snapshot(target string) SessionResourceSnapshot {
	if m == nil {
		return SessionResourceSnapshot{}
	}
	unit := m.UnitForTarget(target)
	if unit == "" {
		return SessionResourceSnapshot{}
	}
	return SessionResourceSnapshot{
		Backend: resourceBackendPortableSupervisor,
		Managed: true,
	}
}

func newTestPortableResourceManager(t *testing.T, owner string, limits delegatedResourceLimits) *portableDelegatedResourceManager {
	t.Helper()
	root := t.TempDir()
	leaseDir := filepath.Join(root, "leases")
	tempRoot := filepath.Join(root, "t")
	for _, dir := range []string{leaseDir, tempRoot} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return &portableDelegatedResourceManager{
		owner:      owner,
		supervisor: "/usr/bin/zen",
		leaseDir:   leaseDir,
		tempRoot:   tempRoot,
		limits:     limits,
		byTarget:   make(map[string]string),
		reserved:   make(map[string]time.Time),
		now:        time.Now,
	}
}

func TestDelegatedResourceUnitIsStrictlyNamespaced(t *testing.T) {
	unit := delegatedResourceUnit("Daemon-ID-ABCDEF", "01234567-89ab-cdef-0123-456789abcdef")
	if unit != "zen-agent-daemonidabcdef-0123456789abcdef0123456789abcdef.scope" {
		t.Fatalf("unit = %q", unit)
	}
	if !validDelegatedResourceUnit("daemon-id-abcdef", unit) {
		t.Fatalf("expected %q to belong to the daemon", unit)
	}
	for _, candidate := range []string{
		"tmux-spawn-01234567.scope",
		"zen-agent-otherdaemon-0123456789abcdef0123456789abcdef.scope",
		"zen-agent-daemonidabcdef-not-a-uuid.scope",
		"zen-agent-daemonidabcdef-0123456789abcdef0123456789abcdef.service",
	} {
		if validDelegatedResourceUnit("daemon-id-abcdef", candidate) {
			t.Fatalf("accepted unowned or malformed unit %q", candidate)
		}
	}
}

func TestDelegatedResourceLimitsReserveMemoryForHost(t *testing.T) {
	for _, key := range []string{
		"ZEN_DELEGATED_MEMORY_HIGH",
		"ZEN_DELEGATED_MEMORY_MAX",
		"ZEN_DELEGATED_TASKS_MAX",
		"ZEN_DELEGATED_MAX_SESSIONS",
	} {
		t.Setenv(key, "")
	}
	const gib = uint64(1024 * 1024 * 1024)
	tests := []struct {
		total    uint64
		wantHigh uint64
		wantMax  uint64
		wantHost uint64
	}{
		{total: 8 * gib, wantHigh: 4 * gib, wantMax: 6 * gib, wantHost: 4 * gib},
		{total: 16 * gib, wantHigh: 12 * gib, wantMax: 14 * gib, wantHost: 4 * gib},
		{total: 32 * gib, wantHigh: 32*gib - 32*gib*20/100, wantMax: 32*gib - 32*gib*10/100, wantHost: 32 * gib * 20 / 100},
		{total: 64 * gib, wantHigh: 64*gib - 64*gib*20/100, wantMax: 64*gib - 64*gib*10/100, wantHost: 64 * gib * 20 / 100},
	}
	for _, test := range tests {
		limits := delegatedResourceLimitsForMemory(test.total)
		if limits.MaxActiveSessions != 0 {
			t.Fatalf("total=%d MaxActiveSessions=%d, want disabled 0", test.total, limits.MaxActiveSessions)
		}
		high, err := strconv.ParseUint(limits.MemoryHigh, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		maxMemory, err := strconv.ParseUint(limits.MemoryMax, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		if high != test.wantHigh || maxMemory != test.wantMax || limits.HostReserve != test.wantHost {
			t.Fatalf("total=%d high=%d max=%d host=%d, want high=%d max=%d host=%d", test.total, high, maxMemory, limits.HostReserve, test.wantHigh, test.wantMax, test.wantHost)
		}
		again := delegatedResourceLimitsForMemory(test.total)
		if again.MemoryHigh != limits.MemoryHigh || again.MemoryMax != limits.MemoryMax {
			t.Fatalf("limits changed without Session input: %+v vs %+v", limits, again)
		}
	}
}

func TestDelegatedResourceLimitsAllowExplicitOverrides(t *testing.T) {
	t.Setenv("ZEN_DELEGATED_MEMORY_HIGH", "6G")
	t.Setenv("ZEN_DELEGATED_MEMORY_MAX", "25%")
	t.Setenv("ZEN_DELEGATED_TASKS_MAX", "2048")
	t.Setenv("ZEN_DELEGATED_MAX_SESSIONS", "7")
	limits := delegatedResourceLimitsForMemory(32 * 1024 * 1024 * 1024)
	if limits.MemoryHigh != "6G" || limits.MemoryMax != "25%" || limits.TasksMax != 2048 || limits.MaxActiveSessions != 7 {
		t.Fatalf("overridden limits = %+v", limits)
	}
}

func TestDelegatedMaxSessionOverrideDoesNotRepartitionSharedPool(t *testing.T) {
	for _, key := range []string{
		"ZEN_DELEGATED_MEMORY_HIGH",
		"ZEN_DELEGATED_MEMORY_MAX",
		"ZEN_DELEGATED_TASKS_MAX",
		"ZEN_DELEGATED_MAX_SESSIONS",
	} {
		t.Setenv(key, "")
	}
	cases := []struct {
		name    string
		total   uint64
		cap     string
		wantCap int
	}{
		{name: "small-fleet", total: 32 * 1024 * 1024 * 1024, cap: "7", wantCap: 7},
		{name: "large-fleet", total: 1024 * 1024 * 1024 * 1024, cap: "512", wantCap: 512},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ZEN_DELEGATED_MAX_SESSIONS", "")
			disabled := delegatedResourceLimitsForMemory(tc.total)
			t.Setenv("ZEN_DELEGATED_MAX_SESSIONS", tc.cap)
			capped := delegatedResourceLimitsForMemory(tc.total)
			if capped.MaxActiveSessions != tc.wantCap {
				t.Fatalf("MaxActiveSessions = %d, want %d", capped.MaxActiveSessions, tc.wantCap)
			}
			if capped.MemoryHigh != disabled.MemoryHigh || capped.MemoryMax != disabled.MemoryMax || capped.HostReserve != disabled.HostReserve {
				t.Fatalf("session cap repartitioned shared pool: capped=%+v disabled=%+v", capped, disabled)
			}
			high, err := strconv.ParseUint(capped.MemoryHigh, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			maximum, err := strconv.ParseUint(capped.MemoryMax, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if high != tc.total-tc.total*20/100 || maximum != tc.total-tc.total*10/100 {
				t.Fatalf("shared pool bytes unexpected: high=%d max=%d", high, maximum)
			}
		})
	}
}

func TestWrapDelegatedResourceCommandCreatesOwnedScopeAtomically(t *testing.T) {
	unit := delegatedResourceUnit("abc123", "0123456789abcdef0123456789abcdef")
	inner := `exec '/bin/zsh' -i -l -c 'echo "$HOME"'`
	got := wrapDelegatedResourceCommand(inner, &delegatedResourceSpec{
		Owner:      "abc123",
		Unit:       unit,
		Slice:      delegatedResourceSlice("abc123"),
		SystemdRun: "/usr/bin/systemd-run",
		Supervisor: "/usr/bin/zen",
		LeaseDir:   "/home/test/.zen/run/agent-resources/abc123",
		Limits: delegatedResourceLimits{
			MemoryHigh:        "4G",
			MemoryMax:         "6G",
			TasksMax:          1024,
			MaxActiveSessions: 4,
		},
	})
	for _, want := range []string{
		"exec '/usr/bin/systemd-run'",
		"'--scope'",
		"'--unit=" + unit + "'",
		"'--slice=zen-agents-abc123.slice'",
		"'--property=TasksMax=1024'",
		"'--property=KillMode=control-group'",
		"'/usr/bin/zen' 'agent' '__supervise'",
		"'--lease-dir=/home/test/.zen/run/agent-resources/abc123'",
		"'/bin/sh' '-c'",
		shellQuote(strings.ReplaceAll(inner, "$", "$$")),
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("wrapped command missing %q:\n%s", want, got)
		}
	}
	for _, forbidden := range []string{
		"MemoryHigh=",
		"MemoryMax=",
		"--pool-guard",
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("wrapped systemd command unexpectedly contains %q:\n%s", forbidden, got)
		}
	}
}

func TestWrapDelegatedResourceCommandEnablesPortablePoolGuard(t *testing.T) {
	unit := delegatedResourceUnit("abc123", "0123456789abcdef0123456789abcdef")
	got := wrapDelegatedResourceCommand("true", &delegatedResourceSpec{
		Owner:      "abc123",
		Unit:       unit,
		Supervisor: "/usr/bin/zen",
		LeaseDir:   "/home/test/.zen/run/agent-resources/abc123",
		Limits: delegatedResourceLimits{
			MemoryHigh: "25G",
			MemoryMax:  "28G",
			TasksMax:   1024,
		},
	})
	for _, want := range []string{
		"'--memory-high=25G'",
		"'--memory-max=28G'",
		"'--pool-guard'",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("portable wrap missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "systemd-run") {
		t.Fatalf("portable wrap unexpectedly used systemd-run:\n%s", got)
	}
}

func TestCloneEnvironmentDoesNotMutateCaller(t *testing.T) {
	original := map[string]string{"KEEP": "value"}
	before := os.Getenv(delegatedMarkerEnv)
	copy := cloneEnvironment(original)
	copy[delegatedMarkerEnv] = "1"
	if _, exists := original[delegatedMarkerEnv]; exists {
		t.Fatal("cloneEnvironment mutated the caller map")
	}
	if os.Getenv(delegatedMarkerEnv) != before {
		t.Fatal("cloneEnvironment mutated the process environment")
	}
}

func TestUnavailableResourceManagerFailsClosed(t *testing.T) {
	manager := unavailableDelegatedResourceManager{reason: "no user manager"}
	if _, err := manager.Prepare(0); err == nil || !strings.Contains(err.Error(), "refusing to start an unbounded session") {
		t.Fatalf("Prepare error = %v", err)
	}
}

func TestDelegatedWorkspacePathRejectsVolatileRoots(t *testing.T) {
	for _, path := range []string{
		"/tmp/zen-worktree",
		"/private/tmp/zen-worktree",
		"/var/tmp/zen-worktree",
		"/dev/shm/zen-worktree",
		"/run/user/501/zen-worktree",
	} {
		if _, err := validateDelegatedWorkspacePath(path); err == nil || !strings.Contains(err.Error(), "volatile or memory-backed temporary storage") {
			t.Fatalf("validateDelegatedWorkspacePath(%q) error = %v", path, err)
		}
	}
}

func TestDelegatedWorkspacePathAcceptsDurableHomePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".zen", "worktrees", "zen", "task")
	resolved, err := validateDelegatedWorkspacePath(path)
	if err != nil {
		t.Fatal(err)
	}
	if resolved == "" {
		t.Fatal("expected resolved durable path")
	}
}

func TestCreateDelegatedSessionPassesOwnedResourceToTmux(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "tmux.log")
	tmuxPath := filepath.Join(dir, "tmux")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$ZEN_TEST_TMUX_LOG"
case "$1" in
  new-session) printf 'brain-agent-test:@1\n' ;;
esac
exit 0
`
	if err := os.WriteFile(tmuxPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ZEN_TEST_TMUX_LOG", logPath)

	unit := delegatedResourceUnit("abc123", "0123456789abcdef0123456789abcdef")
	manager := &fakeDelegatedResourceManager{spec: &delegatedResourceSpec{
		Owner:      "abc123",
		Unit:       unit,
		Slice:      delegatedResourceSlice("abc123"),
		SystemdRun: "/usr/bin/systemd-run",
		Supervisor: "/usr/bin/zen",
		LeaseDir:   filepath.Join(dir, "leases"),
		TempDir:    filepath.Join(dir, "owned-tmp"),
		Limits: delegatedResourceLimits{
			MemoryHigh:        "4G",
			MemoryMax:         "6G",
			TasksMax:          1024,
			MaxActiveSessions: 4,
		},
	}}
	w := New(0)
	w.resources = manager
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	callerEnv := map[string]string{"KEEP": "yes"}
	target, err := w.CreateSession("", CreateSessionOptions{
		Cwd:       cwd,
		Command:   "codex",
		Name:      "test",
		Detached:  true,
		Delegated: true,
		Env:       callerEnv,
	})
	if err != nil {
		t.Fatal(err)
	}
	if target != "brain-agent-test:@1" || manager.boundTarget != target || manager.boundUnit != unit {
		t.Fatalf("target/binding = %q %q %q", target, manager.boundTarget, manager.boundUnit)
	}
	if _, exists := callerEnv[delegatedMarkerEnv]; exists {
		t.Fatal("CreateSession mutated caller Env")
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	calls := string(raw)
	for _, want := range []string{
		delegatedMarkerEnv + "=1",
		delegatedResourceUnitEnv + "=" + unit,
		"TMPDIR=" + filepath.Join(dir, "owned-tmp"),
		"ZEN_BUILD_TMPDIR=" + filepath.Join(dir, "owned-tmp"),
		"--unit=" + unit,
		"set-option -w -t " + target + " @zen_agent_delegated 1",
		"set-option -w -t " + target + " @zen_agent_resource_unit " + unit,
	} {
		if !strings.Contains(calls, want) {
			t.Fatalf("tmux calls missing %q:\n%s", want, calls)
		}
	}
}

func TestCreateDelegatedSessionRollsBackWhenOwnershipMarkersFail(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "tmux.log")
	tmuxPath := filepath.Join(dir, "tmux")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$ZEN_TEST_TMUX_LOG"
case "$1" in
  new-session) printf 'brain-agent-test:@7\n' ;;
  set-option)
    case "$*" in
      *@zen_agent_resource_unit*) exit 1 ;;
    esac
    ;;
esac
exit 0
`
	if err := os.WriteFile(tmuxPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ZEN_TEST_TMUX_LOG", logPath)
	unit := delegatedResourceUnit("abc123", "0123456789abcdef0123456789abcdef")
	manager := &fakeDelegatedResourceManager{spec: &delegatedResourceSpec{
		Owner:      "abc123",
		Unit:       unit,
		Supervisor: "/usr/bin/zen",
		LeaseDir:   filepath.Join(dir, "leases"),
		TempDir:    filepath.Join(dir, "owned-tmp"),
		Limits: delegatedResourceLimits{
			MemoryHigh:        "4G",
			MemoryMax:         "6G",
			TasksMax:          1024,
			MaxActiveSessions: 4,
		},
	}}
	w := New(0)
	w.resources = manager
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.CreateSession("", CreateSessionOptions{
		Cwd:       cwd,
		Command:   "codex",
		Name:      "test",
		Detached:  true,
		Delegated: true,
	}); err == nil || !strings.Contains(err.Error(), "mark owned tmux window") {
		t.Fatalf("CreateSession error = %v", err)
	}
	if len(manager.released) != 1 || manager.released[0] != "\t"+unit {
		t.Fatalf("released = %#v", manager.released)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "kill-window -t brain-agent-test:@7") {
		t.Fatalf("unmarked window was not rolled back:\n%s", raw)
	}
}

func TestPortableResourceReleaseRemovesOnlyOwnedTemporaryDirectory(t *testing.T) {
	owner := "abc123"
	unit := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	root := t.TempDir()
	leaseDir := filepath.Join(root, "leases")
	tempRoot := filepath.Join(root, "t")
	if err := os.MkdirAll(leaseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	manager := &portableDelegatedResourceManager{
		owner:      owner,
		supervisor: "/usr/bin/zen",
		leaseDir:   leaseDir,
		tempRoot:   tempRoot,
		limits:     delegatedResourceLimits{MaxActiveSessions: 4},
		byTarget:   make(map[string]string),
		reserved:   make(map[string]time.Time),
		now:        time.Now,
	}
	ownedTemp, err := manager.createOwnedTempDir(unit)
	if err != nil {
		t.Fatal(err)
	}
	foreignTemp := filepath.Join(tempRoot, "user-data")
	if err := os.MkdirAll(foreignTemp, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := manager.Release("", unit); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ownedTemp); !os.IsNotExist(err) {
		t.Fatalf("owned temporary directory still exists: %v", err)
	}
	if _, err := os.Stat(foreignTemp); err != nil {
		t.Fatalf("foreign directory was touched: %v", err)
	}
}

func TestPortableResourcePrepareReservesCapacityAndOwnedTemp(t *testing.T) {
	owner := "abc123"
	manager := newTestPortableResourceManager(t, owner, delegatedResourceLimits{
		MemoryHigh:        "4G",
		MemoryMax:         "6G",
		TasksMax:          1024,
		MaxActiveSessions: 1,
	})
	spec, err := manager.Prepare(0)
	if err != nil {
		t.Fatal(err)
	}
	wantTemp := filepath.Join(manager.tempRoot, shortDelegatedTempDigest(spec.Unit))
	if spec == nil || !validDelegatedResourceUnit(owner, spec.Unit) || spec.TempDir != wantTemp {
		t.Fatalf("spec = %+v", spec)
	}
	if base := filepath.Base(spec.TempDir); len(base) != delegatedTempDigestLen {
		t.Fatalf("temp basename %q length = %d, want %d", base, len(base), delegatedTempDigestLen)
	}
	markerPath := filepath.Join(spec.TempDir, delegatedTempMarkerName)
	raw, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != spec.Unit {
		t.Fatalf("marker = %q, want %q", raw, spec.Unit)
	}
	markerInfo, err := os.Stat(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	if markerInfo.Mode().Perm() != 0o600 {
		t.Fatalf("marker mode = %o, want 600", markerInfo.Mode().Perm())
	}
	if info, err := os.Stat(spec.TempDir); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("owned temporary directory info=%v error=%v", info, err)
	}
	if _, err := manager.Prepare(0); err == nil || !strings.Contains(err.Error(), "active delegated session capacity reached") {
		t.Fatalf("second Prepare error = %v", err)
	}
	if err := manager.Release("", spec.Unit); err != nil {
		t.Fatal(err)
	}
}

func TestPortableResourceTempDirCollisionFailsClosed(t *testing.T) {
	owner := "abc123"
	unit := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	root := t.TempDir()
	tempRoot := filepath.Join(root, "t")
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	manager := &portableDelegatedResourceManager{
		owner:    owner,
		tempRoot: tempRoot,
	}
	collision := filepath.Join(tempRoot, shortDelegatedTempDigest(unit))
	if err := os.Mkdir(collision, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.createOwnedTempDir(unit); err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("createOwnedTempDir error = %v", err)
	}
}

func TestPortableResourceReleaseLeavesForeignOrCorruptTempUntouched(t *testing.T) {
	owner := "abc123"
	unit := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	root := t.TempDir()
	leaseDir := filepath.Join(root, "leases")
	tempRoot := filepath.Join(root, "t")
	for _, dir := range []string{leaseDir, tempRoot} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	manager := &portableDelegatedResourceManager{
		owner:      owner,
		supervisor: "/usr/bin/zen",
		leaseDir:   leaseDir,
		tempRoot:   tempRoot,
		byTarget:   make(map[string]string),
		reserved:   make(map[string]time.Time),
		now:        time.Now,
	}
	tempDir := filepath.Join(tempRoot, shortDelegatedTempDigest(unit))
	if err := os.Mkdir(tempDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Corrupt marker: wrong unit content.
	if err := os.WriteFile(filepath.Join(tempDir, delegatedTempMarkerName), []byte("not-the-unit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(tempDir, "cache")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "NOTICE"), []byte("keep\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(nested, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = makeOwnedTreeDirsOwnerAccessible(tempDir)
	})
	if err := manager.Release("", unit); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tempDir); err != nil {
		t.Fatalf("corrupt temp directory was removed: %v", err)
	}
	if info, err := os.Stat(nested); err != nil || info.Mode().Perm()&0o200 != 0 {
		t.Fatalf("foreign/corrupt nested dir was mutated: info=%v err=%v", info, err)
	}
}

func TestPortableResourceReleaseRemovesShortRestrictedNestedTrees(t *testing.T) {
	cases := []struct {
		name  string
		seed  func(t *testing.T, ownedTemp, foreign string)
		check func(t *testing.T, ownedTemp, foreign string)
	}{
		{
			name: "readonly-cache",
			seed: func(t *testing.T, ownedTemp, foreign string) {
				writeReadonlyNestedCache(t, foreign)
				writeReadonlyNestedCache(t, ownedTemp)
				if err := os.Symlink(foreign, filepath.Join(ownedTemp, "escape")); err != nil {
					t.Fatal(err)
				}
			},
			check: func(t *testing.T, ownedTemp, foreign string) {
				if info, err := os.Stat(filepath.Join(foreign, "pkg", "mod", "cache")); err != nil || info.Mode().Perm()&0o200 != 0 {
					t.Fatalf("foreign tree reached through nested symlink was mutated: info=%v err=%v", info, err)
				}
			},
		},
		{
			name: "mode-zero",
			seed: func(t *testing.T, ownedTemp, _ string) {
				writeModeZeroNestedDir(t, ownedTemp)
			},
			check: func(t *testing.T, _, _ string) {},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			owner := "abc123"
			unit := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
			manager := newTestPortableResourceManager(t, owner, delegatedResourceLimits{})
			foreign := filepath.Join(t.TempDir(), "foreign")
			if err := os.MkdirAll(foreign, 0o700); err != nil {
				t.Fatal(err)
			}
			ownedTemp, err := manager.createOwnedTempDir(unit)
			if err != nil {
				t.Fatal(err)
			}
			tc.seed(t, ownedTemp, foreign)
			if err := manager.Release("", unit); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(ownedTemp); !os.IsNotExist(err) {
				t.Fatalf("owned short temp still exists: %v", err)
			}
			tc.check(t, ownedTemp, foreign)
		})
	}
}

func TestPortableResourceReleaseRemovesLegacyReadonlyNestedCache(t *testing.T) {
	owner := "abc123"
	unit := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	root := t.TempDir()
	leaseDir := filepath.Join(root, "leases")
	tempRoot := filepath.Join(root, "t")
	legacyRoot := filepath.Join(root, "tmp", "agent-resources", owner)
	for _, dir := range []string{leaseDir, tempRoot, legacyRoot} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	legacyTemp := filepath.Join(legacyRoot, unit)
	if err := os.MkdirAll(legacyTemp, 0o700); err != nil {
		t.Fatal(err)
	}
	writeReadonlyNestedCache(t, legacyTemp)
	manager := &portableDelegatedResourceManager{
		owner:          owner,
		supervisor:     "/usr/bin/zen",
		leaseDir:       leaseDir,
		tempRoot:       tempRoot,
		legacyTempRoot: legacyRoot,
		byTarget:       make(map[string]string),
		reserved:       make(map[string]time.Time),
		now:            time.Now,
	}
	if err := manager.Release("", unit); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacyTemp); !os.IsNotExist(err) {
		t.Fatalf("legacy owned temp with readonly cache still exists: %v", err)
	}
}

func TestPortableResourceReleaseDoesNotFollowSymlinkOrForeignMarkerRoot(t *testing.T) {
	owner := "abc123"
	unit := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	root := t.TempDir()
	leaseDir := filepath.Join(root, "leases")
	tempRoot := filepath.Join(root, "t")
	legacyRoot := filepath.Join(root, "tmp", "agent-resources", owner)
	foreign := filepath.Join(root, "foreign")
	for _, dir := range []string{leaseDir, tempRoot, legacyRoot, foreign} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeReadonlyNestedCache(t, foreign)
	foreignNotice := filepath.Join(foreign, "pkg", "mod", "cache", "NOTICE")

	// Short path is a symlink to foreign content; marker check must fail closed.
	shortTemp := filepath.Join(tempRoot, shortDelegatedTempDigest(unit))
	if err := os.Symlink(foreign, shortTemp); err != nil {
		t.Fatal(err)
	}
	// Legacy path is a symlink to the same foreign tree.
	legacyTemp := filepath.Join(legacyRoot, unit)
	if err := os.Symlink(foreign, legacyTemp); err != nil {
		t.Fatal(err)
	}

	manager := &portableDelegatedResourceManager{
		owner:          owner,
		supervisor:     "/usr/bin/zen",
		leaseDir:       leaseDir,
		tempRoot:       tempRoot,
		legacyTempRoot: legacyRoot,
		byTarget:       make(map[string]string),
		reserved:       make(map[string]time.Time),
		now:            time.Now,
	}
	if err := manager.Release("", unit); err != nil {
		t.Fatal(err)
	}

	if info, err := os.Lstat(shortTemp); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("short symlink root was removed or replaced: info=%v err=%v", info, err)
	}
	if info, err := os.Lstat(legacyTemp); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("legacy symlink root was removed or replaced: info=%v err=%v", info, err)
	}
	if info, err := os.Stat(filepath.Join(foreign, "pkg", "mod", "cache")); err != nil || info.Mode().Perm()&0o200 != 0 {
		t.Fatalf("foreign nested dir was mutated through symlink: info=%v err=%v", info, err)
	}
	if _, err := os.Stat(foreignNotice); err != nil {
		t.Fatalf("foreign NOTICE was removed through symlink: %v", err)
	}
}

func writeReadonlyNestedCache(t *testing.T, root string) {
	t.Helper()
	cacheDir := filepath.Join(root, "pkg", "mod", "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	notice := filepath.Join(cacheDir, "NOTICE")
	if err := os.WriteFile(notice, []byte("go module cache\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cacheDir, 0o555); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "pkg", "mod"), 0o555); err != nil {
		t.Fatal(err)
	}
	// TempDir teardown uses RemoveAll; restore owner rwx if the tree survives the test.
	t.Cleanup(func() {
		_ = makeOwnedTreeDirsOwnerAccessible(root)
	})
}

func writeModeZeroNestedDir(t *testing.T, root string) {
	t.Helper()
	sealed := filepath.Join(root, "sealed")
	inner := filepath.Join(sealed, "inner")
	if err := os.MkdirAll(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, "blob"), []byte("opaque\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(inner, 0o000); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sealed, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = makeOwnedTreeDirsOwnerAccessible(root)
	})
}

func TestPortableResourceReconcileRemovesOrphanShortTemp(t *testing.T) {
	owner := "abc123"
	orphan := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	live := delegatedResourceUnit(owner, "fedcba9876543210fedcba9876543210")
	root := t.TempDir()
	leaseDir := filepath.Join(root, "leases")
	tempRoot := filepath.Join(root, "t")
	for _, dir := range []string{leaseDir, tempRoot} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	manager := &portableDelegatedResourceManager{
		owner:      owner,
		supervisor: "/usr/bin/zen",
		leaseDir:   leaseDir,
		tempRoot:   tempRoot,
		byTarget:   make(map[string]string),
		reserved:   make(map[string]time.Time),
		now:        time.Now,
	}
	orphanTemp, err := manager.createOwnedTempDir(orphan)
	if err != nil {
		t.Fatal(err)
	}
	liveTemp, err := manager.createOwnedTempDir(live)
	if err != nil {
		t.Fatal(err)
	}
	foreignTemp := filepath.Join(tempRoot, "abcdef")
	if err := os.Mkdir(foreignTemp, 0o700); err != nil {
		t.Fatal(err)
	}
	manager.Reconcile([]tmuxWindow{{
		target:       "main:@1",
		delegated:    true,
		resourceUnit: live,
	}})
	if _, err := os.Stat(orphanTemp); !os.IsNotExist(err) {
		t.Fatalf("orphan temp still exists: %v", err)
	}
	if _, err := os.Stat(liveTemp); err != nil {
		t.Fatalf("live temp was removed: %v", err)
	}
	if _, err := os.Stat(foreignTemp); err != nil {
		t.Fatalf("foreign temp was touched: %v", err)
	}
}

func TestPortableResourceReleaseCleansLegacyFullUnitTemp(t *testing.T) {
	owner := "abc123"
	unit := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	root := t.TempDir()
	leaseDir := filepath.Join(root, "leases")
	tempRoot := filepath.Join(root, "t")
	legacyRoot := filepath.Join(root, "tmp", "agent-resources", owner)
	for _, dir := range []string{leaseDir, tempRoot, legacyRoot} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	legacyTemp := filepath.Join(legacyRoot, unit)
	foreignLegacy := filepath.Join(legacyRoot, "user-data")
	for _, dir := range []string{legacyTemp, foreignLegacy} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	manager := &portableDelegatedResourceManager{
		owner:          owner,
		supervisor:     "/usr/bin/zen",
		leaseDir:       leaseDir,
		tempRoot:       tempRoot,
		legacyTempRoot: legacyRoot,
		byTarget:       make(map[string]string),
		reserved:       make(map[string]time.Time),
		now:            time.Now,
	}
	if err := manager.Release("", unit); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacyTemp); !os.IsNotExist(err) {
		t.Fatalf("legacy owned temp still exists: %v", err)
	}
	if _, err := os.Stat(foreignLegacy); err != nil {
		t.Fatalf("foreign legacy directory was touched: %v", err)
	}
}

func TestPortableResourceReconcileCleansLegacyOrphanKeepsLive(t *testing.T) {
	owner := "abc123"
	orphan := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	live := delegatedResourceUnit(owner, "fedcba9876543210fedcba9876543210")
	root := t.TempDir()
	leaseDir := filepath.Join(root, "leases")
	tempRoot := filepath.Join(root, "t")
	legacyRoot := filepath.Join(root, "tmp", "agent-resources", owner)
	for _, dir := range []string{leaseDir, tempRoot, legacyRoot} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	orphanLegacy := filepath.Join(legacyRoot, orphan)
	liveLegacy := filepath.Join(legacyRoot, live)
	foreignLegacy := filepath.Join(legacyRoot, "not-a-unit")
	for _, dir := range []string{orphanLegacy, liveLegacy, foreignLegacy} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	manager := &portableDelegatedResourceManager{
		owner:          owner,
		supervisor:     "/usr/bin/zen",
		leaseDir:       leaseDir,
		tempRoot:       tempRoot,
		legacyTempRoot: legacyRoot,
		byTarget:       make(map[string]string),
		reserved:       make(map[string]time.Time),
		now:            time.Now,
	}
	manager.Reconcile([]tmuxWindow{{
		target:       "main:@1",
		delegated:    true,
		resourceUnit: live,
	}})
	if _, err := os.Stat(orphanLegacy); !os.IsNotExist(err) {
		t.Fatalf("legacy orphan still exists: %v", err)
	}
	if _, err := os.Stat(liveLegacy); err != nil {
		t.Fatalf("live legacy temp was removed: %v", err)
	}
	if _, err := os.Stat(foreignLegacy); err != nil {
		t.Fatalf("foreign legacy directory was touched: %v", err)
	}
}

func TestShortDelegatedTempDigestIsStableURLSafe(t *testing.T) {
	unit := delegatedResourceUnit("abc123", "0123456789abcdef0123456789abcdef")
	first := shortDelegatedTempDigest(unit)
	second := shortDelegatedTempDigest(unit)
	if first != second || len(first) != delegatedTempDigestLen {
		t.Fatalf("digest = %q / %q", first, second)
	}
	for _, char := range first {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			continue
		}
		t.Fatalf("digest %q is not URL-safe", first)
	}
}

func TestPortableResourcePrepareRejectsHostMemoryPressure(t *testing.T) {
	manager := newTestPortableResourceManager(t, "abc123", delegatedResourceLimits{
		MemoryHigh:  "1",
		MemoryMax:   "1",
		HostReserve: 101,
		TasksMax:    1024,
	})
	manager.availableMemory = func() uint64 { return 100 }
	if _, err := manager.Prepare(0); err == nil || !strings.Contains(err.Error(), "host memory pressure") {
		t.Fatalf("Prepare error = %v", err)
	}
}

func TestPortableResourcePrepareAllowsDisabledSessionCap(t *testing.T) {
	manager := newTestPortableResourceManager(t, "abc123", delegatedResourceLimits{
		MemoryHigh:        "25G",
		MemoryMax:         "28G",
		MaxActiveSessions: 0,
		TasksMax:          1024,
	})
	first, err := manager.Prepare(0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Prepare(0)
	if err != nil {
		t.Fatal(err)
	}
	if first.Unit == second.Unit {
		t.Fatalf("expected distinct resource units, got %q", first.Unit)
	}
}

func TestPortableResourcePrepareHonorsExplicitCapUnderRace(t *testing.T) {
	manager := newTestPortableResourceManager(t, "abc123", delegatedResourceLimits{
		MaxActiveSessions: 1,
		TasksMax:          1024,
	})
	const workers = 8
	results := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			_, err := manager.Prepare(0)
			results <- err
		}()
	}
	accepted := 0
	rejected := 0
	for i := 0; i < workers; i++ {
		err := <-results
		switch {
		case err == nil:
			accepted++
		case strings.Contains(err.Error(), "active delegated session capacity reached"):
			rejected++
		default:
			t.Fatalf("unexpected Prepare error: %v", err)
		}
	}
	if accepted != 1 || rejected != workers-1 {
		t.Fatalf("accepted=%d rejected=%d, want 1/%d", accepted, rejected, workers-1)
	}
}

func TestPortablePrepareLeaseScanFollowsSessionCap(t *testing.T) {
	cases := []struct {
		name     string
		cap      int
		wantScan int32
	}{
		{name: "disabled", cap: 0, wantScan: 0},
		{name: "enabled", cap: 2, wantScan: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var leaseReads atomic.Int32
			manager := newTestPortableResourceManager(t, "abc123", delegatedResourceLimits{MaxActiveSessions: tc.cap, TasksMax: 1024})
			manager.listLeases = func(string) ([]agentproc.Lease, error) {
				leaseReads.Add(1)
				return nil, nil
			}
			if _, err := manager.Prepare(0); err != nil {
				t.Fatal(err)
			}
			if got := leaseReads.Load(); got != tc.wantScan {
				t.Fatalf("lease scan count = %d, want %d", got, tc.wantScan)
			}
		})
	}
}

func TestKillDelegatedSessionReleasesExactBoundUnit(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "tmux.log")
	tmuxPath := filepath.Join(dir, "tmux")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$ZEN_TEST_TMUX_LOG"
exit 0
`
	if err := os.WriteFile(tmuxPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ZEN_TEST_TMUX_LOG", logPath)
	unit := delegatedResourceUnit("abc123", "0123456789abcdef0123456789abcdef")
	manager := &fakeDelegatedResourceManager{boundTarget: "main:@42", boundUnit: unit}
	w := New(0)
	w.resources = manager
	if err := w.KillSession("main:@42"); err != nil {
		t.Fatal(err)
	}
	if len(manager.released) != 1 || manager.released[0] != "main:@42\t"+unit {
		t.Fatalf("released = %#v", manager.released)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "kill-window -t main:@42") {
		t.Fatalf("tmux calls:\n%s", raw)
	}
}
