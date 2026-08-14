package work

import (
	"path/filepath"
	"strings"
)

// LiveCodexRolloutPath resolves the live Codex rollout file for a managed
// Codex process (the thread/session identity file Codex keeps open). Returns
// "" when no rollout can be proven open — the caller must treat the handoff
// as unavailable rather than guess.
func LiveCodexRolloutPath(processID int) string {
	paths := openCodexRolloutPathsForProcess(processID)
	if len(paths) == 0 {
		return ""
	}
	// Newest by file name (rollout-<timestamp>-<uuid>.jsonl) when several are
	// open; the live session is the most recent.
	best := ""
	for _, path := range paths {
		if best == "" || strings.Compare(filepath.Base(path), filepath.Base(best)) > 0 {
			best = path
		}
	}
	return best
}
