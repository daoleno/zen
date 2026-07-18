package watcher

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	delegatedMarkerEnv        = "ZEN_AGENT_DELEGATED"
	delegatedResourceUnitEnv  = "ZEN_AGENT_RESOURCE_UNIT"
	delegatedResourceOwnerEnv = "ZEN_AGENT_RESOURCE_OWNER"

	defaultDelegatedTasksMax    = 1024
	maxConfiguredActiveSessions = 65536

	// Short durable temp dirs keep AF_UNIX paths under sockaddr sun_path limits.
	delegatedTempMarkerName = ".zen-agent-unit"
	delegatedTempDigestLen  = 6
)

var (
	resourceOwnerSanitizer = regexp.MustCompile(`[^a-z0-9]+`)
	systemdMemoryValueRE   = regexp.MustCompile(`^[1-9][0-9]*(?:[KMGTPE])?$|^[1-9][0-9]?%$|^100%$`)
)

type delegatedResourceLimits struct {
	MemoryHigh        string
	MemoryMax         string
	HostReserve       uint64
	TasksMax          int
	MaxActiveSessions int // 0 means disabled; only an explicit admin cap applies
}

type delegatedResourceSpec struct {
	Owner      string
	Unit       string
	Slice      string
	SystemdRun string
	Supervisor string
	LeaseDir   string
	TempDir    string
	Limits     delegatedResourceLimits
}

type delegatedResourceManager interface {
	Prepare(activeSessions int) (*delegatedResourceSpec, error)
	Bind(target, unit string)
	UnitForTarget(target string) string
	Reconcile(windows []tmuxWindow)
	Release(target, unit string) error
	// Snapshot returns one on-demand read-only projection for target.
	// Unsupported measurements stay absent; never invent zeros.
	Snapshot(target string) SessionResourceSnapshot
}

type noopDelegatedResourceManager struct{}

func (noopDelegatedResourceManager) Prepare(int) (*delegatedResourceSpec, error) {
	return nil, nil
}

func (noopDelegatedResourceManager) Bind(string, string) {}

func (noopDelegatedResourceManager) UnitForTarget(string) string { return "" }

func (noopDelegatedResourceManager) Reconcile([]tmuxWindow) {}

func (noopDelegatedResourceManager) Release(string, string) error { return nil }

func (noopDelegatedResourceManager) Snapshot(string) SessionResourceSnapshot {
	return SessionResourceSnapshot{}
}

type unavailableDelegatedResourceManager struct {
	reason string
}

func (m unavailableDelegatedResourceManager) Prepare(int) (*delegatedResourceSpec, error) {
	return nil, fmt.Errorf("delegated resource isolation unavailable; refusing to start an unbounded session: %s", strings.TrimSpace(m.reason))
}

func (unavailableDelegatedResourceManager) Bind(string, string) {}

func (unavailableDelegatedResourceManager) UnitForTarget(string) string { return "" }

func (unavailableDelegatedResourceManager) Reconcile([]tmuxWindow) {}

func (unavailableDelegatedResourceManager) Release(string, string) error { return nil }

func (m unavailableDelegatedResourceManager) Snapshot(string) SessionResourceSnapshot {
	return SessionResourceSnapshot{}
}

func normalizeResourceOwner(owner string) string {
	owner = strings.ToLower(strings.TrimSpace(owner))
	owner = resourceOwnerSanitizer.ReplaceAllString(owner, "")
	// Daemon IDs are UUID-like. Preserve their full 128-bit normalized value so
	// two state directories cannot share a kill namespace merely because their
	// IDs have the same short prefix; the resulting systemd unit is still far
	// below the platform name limit.
	if len(owner) > 32 {
		owner = owner[:32]
	}
	return owner
}

func delegatedResourceUnit(owner, token string) string {
	owner = normalizeResourceOwner(owner)
	token = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(token), "-", ""))
	if owner == "" || len(token) != 32 {
		return ""
	}
	for _, char := range token {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return ""
		}
	}
	return "zen-agent-" + owner + "-" + token + ".scope"
}

func delegatedResourceSlice(owner string) string {
	owner = normalizeResourceOwner(owner)
	if owner == "" {
		return ""
	}
	return "zen-agents-" + owner + ".slice"
}

// shortDelegatedTempDigest returns a stable 6-character URL-safe digest of the
// full owned resource unit for use as a short durable TMPDIR basename.
func shortDelegatedTempDigest(unit string) string {
	sum := sha256.Sum256([]byte(unit))
	encoded := base64.RawURLEncoding.EncodeToString(sum[:])
	if len(encoded) < delegatedTempDigestLen {
		return encoded
	}
	return encoded[:delegatedTempDigestLen]
}

func validDelegatedResourceUnit(owner, unit string) bool {
	owner = normalizeResourceOwner(owner)
	unit = strings.TrimSpace(unit)
	prefix := "zen-agent-" + owner + "-"
	if owner == "" || !strings.HasPrefix(unit, prefix) || !strings.HasSuffix(unit, ".scope") {
		return false
	}
	token := strings.TrimSuffix(strings.TrimPrefix(unit, prefix), ".scope")
	return delegatedResourceUnit(owner, token) == unit
}

func delegatedResourceLimitsForMemory(total uint64) delegatedResourceLimits {
	const gib = uint64(1024 * 1024 * 1024)
	if total == 0 {
		total = 8 * gib
	}

	// One static daemon-owned shared pool. Limits do not depend on Session count.
	hostReserve := total * 20 / 100
	if hostReserve < 4*gib {
		hostReserve = 4 * gib
	}
	if hostReserve > total/2 {
		hostReserve = total / 2
	}
	memoryHigh := total - hostReserve

	emergencyReserve := total * 10 / 100
	if emergencyReserve < 2*gib {
		emergencyReserve = 2 * gib
	}
	if emergencyReserve > total/2 {
		emergencyReserve = total / 2
	}
	memoryMax := total - emergencyReserve
	if memoryMax < memoryHigh {
		memoryMax = memoryHigh
	}

	// Default active-session ceiling is disabled. Only an explicit administrative
	// override may cap concurrency; it never repartitions the shared pool.
	maxActiveSessions := 0
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("ZEN_DELEGATED_MAX_SESSIONS"))); err == nil && value >= 1 && value <= maxConfiguredActiveSessions {
		maxActiveSessions = value
	}

	limits := delegatedResourceLimits{
		MemoryHigh:        strconv.FormatUint(memoryHigh, 10),
		MemoryMax:         strconv.FormatUint(memoryMax, 10),
		HostReserve:       hostReserve,
		TasksMax:          defaultDelegatedTasksMax,
		MaxActiveSessions: maxActiveSessions,
	}
	if value := strings.TrimSpace(os.Getenv("ZEN_DELEGATED_MEMORY_HIGH")); systemdMemoryValueRE.MatchString(value) {
		limits.MemoryHigh = value
	}
	if value := strings.TrimSpace(os.Getenv("ZEN_DELEGATED_MEMORY_MAX")); systemdMemoryValueRE.MatchString(value) {
		limits.MemoryMax = value
	}
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("ZEN_DELEGATED_TASKS_MAX"))); err == nil && value >= 64 && value <= 65536 {
		limits.TasksMax = value
	}
	return limits
}

func wrapDelegatedResourceCommand(inner string, spec *delegatedResourceSpec) string {
	if spec == nil || strings.TrimSpace(spec.Supervisor) == "" || strings.TrimSpace(spec.Unit) == "" {
		return inner
	}
	supervisorArgs := []string{
		spec.Supervisor,
		"agent",
		"__supervise",
		"--resource-id=" + spec.Unit,
		"--lease-dir=" + spec.LeaseDir,
		"--memory-high=" + spec.Limits.MemoryHigh,
		"--memory-max=" + spec.Limits.MemoryMax,
		fmt.Sprintf("--tasks-max=%d", spec.Limits.TasksMax),
	}
	// Portable/macOS supervisors elect one pool leader for aggregate RSS enforcement.
	// Linux systemd scopes inherit aggregate MemoryHigh/MemoryMax from the parent slice.
	if strings.TrimSpace(spec.SystemdRun) == "" {
		supervisorArgs = append(supervisorArgs, "--pool-guard")
	}
	supervisorArgs = append(supervisorArgs,
		"--",
		"/bin/sh",
		"-c",
		inner,
	)
	if strings.TrimSpace(spec.SystemdRun) == "" {
		return shellExecCommand(supervisorArgs)
	}

	args := []string{
		spec.SystemdRun,
		"--user",
		"--scope",
		"--quiet",
		"--collect",
		"--send-sighup",
		"--unit=" + spec.Unit,
		"--slice=" + spec.Slice,
		"--description=Zen delegated agent " + spec.Unit,
		"--property=TasksAccounting=yes",
		fmt.Sprintf("--property=TasksMax=%d", spec.Limits.TasksMax),
		"--property=OOMPolicy=stop",
		"--property=KillMode=control-group",
		"--",
	}
	// systemd-run expands $ expressions in command arguments. Doubling every
	// dollar is its documented escape and preserves the exact command for the
	// supervised shell without requiring the newer --expand-environment option.
	for _, arg := range supervisorArgs {
		args = append(args, strings.ReplaceAll(arg, "$", "$$"))
	}
	return shellExecCommand(args)
}

func shellExecCommand(args []string) string {
	quoted := make([]string, 0, len(args)+1)
	quoted = append(quoted, "exec")
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func cloneEnvironment(values map[string]string) map[string]string {
	copy := make(map[string]string, len(values)+3)
	for key, value := range values {
		copy[key] = value
	}
	return copy
}

func validateDelegatedWorkspacePath(cwd string) (string, error) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "", nil
	}
	absolute, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("resolve delegated workspace %q: %w", cwd, err)
	}
	resolved := absolute
	if value, resolveErr := filepath.EvalSymlinks(absolute); resolveErr == nil {
		resolved = value
	}

	roots := []string{
		os.TempDir(),
		"/tmp",
		"/private/tmp",
		"/var/tmp",
		"/private/var/tmp",
		"/dev/shm",
		"/run/user",
	}
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		rootAbsolute, rootErr := filepath.Abs(root)
		if rootErr != nil {
			continue
		}
		rootResolved := rootAbsolute
		if value, resolveErr := filepath.EvalSymlinks(rootAbsolute); resolveErr == nil {
			rootResolved = value
		}
		if pathWithinRoot(absolute, rootAbsolute) || pathWithinRoot(resolved, rootResolved) {
			return "", fmt.Errorf("delegated agent cwd %q is on volatile or memory-backed temporary storage; use a durable workspace such as $ZEN_WORKTREE_ROOT (default ~/.zen/worktrees)", cwd)
		}
	}
	return resolved, nil
}

func pathWithinRoot(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	relative, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
