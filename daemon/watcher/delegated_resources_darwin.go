//go:build darwin

package watcher

import (
	"fmt"
	"strings"
	"syscall"
)

func newDelegatedResourceManager(owner string) delegatedResourceManager {
	manager, err := newPortableDelegatedResourceManager(owner)
	if err != nil {
		return unavailableDelegatedResourceManager{reason: err.Error()}
	}
	return manager
}

func validateDelegatedWorkspace(cwd string) error {
	resolved, err := validateDelegatedWorkspacePath(cwd)
	if err != nil {
		return err
	}
	if resolved == "" {
		return nil
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(resolved, &stat); err != nil {
		return nil
	}
	filesystem := statfsTypeName(stat)
	switch filesystem {
	case "tmpfs", "ramfs", "mfs":
		return fmt.Errorf("delegated agent cwd %q is on memory-backed temporary storage; use a durable workspace such as $ZEN_WORKTREE_ROOT (default ~/.zen/worktrees)", cwd)
	default:
		return nil
	}
}

func statfsTypeName(stat syscall.Statfs_t) string {
	bytes := make([]byte, 0, len(stat.Fstypename))
	for _, char := range stat.Fstypename {
		if char == 0 {
			break
		}
		bytes = append(bytes, byte(char))
	}
	return strings.ToLower(string(bytes))
}
