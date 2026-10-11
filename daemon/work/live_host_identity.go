package work

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/providerpaths"
)

// Claude's process registry names the native session even for a fresh CLI with
// no resume argument. procStart ties the record to the OS process, not just a
// reusable PID. Never infer this identity from the newest transcript in a cwd.
type claudeProcessSession struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	ProcStart string `json:"procStart"`
}

func claudeProcessRecord(root string, pid int) (claudeProcessSession, bool, error) {
	var record claudeProcessSession
	raw, err := os.ReadFile(filepath.Join(root, "sessions", strconv.Itoa(pid)+".json"))
	if os.IsNotExist(err) {
		return record, false, nil
	}
	if err != nil {
		return record, false, err
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return record, false, err
	}
	if record.PID != pid || record.SessionID == "" || filepath.Base(record.SessionID) != record.SessionID || record.SessionID == "." || record.SessionID == ".." || !filepath.IsAbs(record.Cwd) {
		return record, false, fmt.Errorf("invalid Claude process session for pid %d", pid)
	}
	live, err := claudeProcessRecordLive(record)
	return record, live, err
}

func claudeIdentityRoot(pid int) string {
	if root := firstProcessTreeEnvironValue(pid, "CLAUDE_CONFIG_DIR"); root != "" {
		return root
	}
	if home := firstProcessTreeEnvironValue(pid, "HOME"); home != "" {
		return filepath.Join(home, ".claude")
	}
	return ""
}

// ResolveLiveHostTranscriptIdentity requires evidence from the candidate's live
// process. The recorded binding is deliberately not an input to this proof.
func ResolveLiveHostTranscriptIdentity(worker classifier.Worker, provider string) (HostTranscriptIdentity, bool, error) {
	if provider == WorkerProviderCodex {
		identity, found := ResolveCodexTranscriptIdentity(worker.ProcessID)
		return hostTranscriptFromCodex(provider, identity), found, nil
	}
	if provider != WorkerProviderClaude || worker.ProcessID <= 0 {
		return HostTranscriptIdentity{}, false, nil
	}
	root := claudeIdentityRoot(worker.ProcessID)
	if !filepath.IsAbs(root) {
		return HostTranscriptIdentity{}, false, nil
	}
	record, live, err := claudeProcessRecord(root, worker.ProcessID)
	if err != nil || !live {
		return HostTranscriptIdentity{}, false, err
	}
	if !pathsEquivalent(record.Cwd, worker.Cwd) {
		return HostTranscriptIdentity{}, false, fmt.Errorf("Claude process session cwd does not match host %q", worker.ID)
	}
	path := filepath.Join(claudeProjectDir(root, record.Cwd), record.SessionID+".jsonl")
	return HostTranscriptIdentity{Provider: provider, SessionID: record.SessionID, Path: path, DataRoot: root}, true, nil
}

// LiveClaudeSessionOwner fences a resume even when the process is outside the
// selected tmux inventory. Stale records cannot claim a recycled process ID.
func LiveClaudeSessionOwner(sessionID, root string) (int, error) {
	if strings.TrimSpace(sessionID) == "" {
		return 0, nil
	}
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0, err
		}
		root = providerpaths.ClaudeConfigDir(home)
	}
	if !filepath.IsAbs(root) {
		return 0, fmt.Errorf("Claude provider root must be absolute")
	}
	entries, err := os.ReadDir(filepath.Join(root, "sessions"))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil || pid <= 0 {
			continue
		}
		record, live, err := claudeProcessRecord(root, pid)
		if record.SessionID != "" && record.SessionID != sessionID {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("Claude session ownership unknown: %w", err)
		}
		if live && record.SessionID == sessionID {
			return pid, nil
		}
	}
	return 0, nil
}
