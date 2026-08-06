package work

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/daoleno/zen/daemon/classifier"
)

// CodexTranscriptIdentity is the stable provider transcript binding for a Host
// Executor Session. It is established from the target host process/executor
// session (provider data root + rollout/session path) and survives cwd moves.
type CodexTranscriptIdentity struct {
	SessionID string
	Path      string
	// DataRoot is the provider process HOME / data root that owns .codex.
	// Daemon os.UserHomeDir is never the owner when this is set.
	DataRoot string
}

// ResolveCodexTranscriptIdentity binds a live Codex process tree to its
// provider session/path. Open rollout FDs and the process environment data
// root win over daemon HOME and current cwd.
func ResolveCodexTranscriptIdentity(processID int) (CodexTranscriptIdentity, bool) {
	if processID <= 0 {
		return CodexTranscriptIdentity{}, false
	}
	dataRoots := providerDataRootsForProcessTree(processID)
	openPaths := openCodexRolloutPathsForProcess(processID)
	envSessionID := strings.TrimSpace(firstProcessTreeEnvironValue(processID, "CODEX_THREAD_ID"))

	if len(openPaths) > 0 {
		path := openPaths[0]
		sessionID := sessionIDFromCodexRolloutPath(path)
		if sessionID == "" {
			sessionID = envSessionID
		}
		dataRoot := dataRootForPath(path, dataRoots)
		return CodexTranscriptIdentity{
			SessionID: sessionID,
			Path:      path,
			DataRoot:  dataRoot,
		}, true
	}

	if envSessionID != "" {
		for _, root := range dataRoots {
			if row, ok, err := lookupCodexThreadByIDInRoot(envSessionID, root); err == nil && ok {
				path := strings.TrimSpace(row.RolloutPath)
				if path == "" {
					continue
				}
				if _, err := os.Stat(path); err != nil {
					continue
				}
				return CodexTranscriptIdentity{
					SessionID: envSessionID,
					Path:      path,
					DataRoot:  root,
				}, true
			}
		}
		// Daemon HOME is only a last-resort fallback, never the owner while a
		// process-local data root exists.
		if len(dataRoots) == 0 {
			if row, ok, err := lookupCodexThreadByIDInRoot(envSessionID, ""); err == nil && ok {
				path := strings.TrimSpace(row.RolloutPath)
				if path != "" {
					if _, err := os.Stat(path); err == nil {
						return CodexTranscriptIdentity{SessionID: envSessionID, Path: path}, true
					}
				}
			}
		}
	}

	if len(dataRoots) == 0 {
		return CodexTranscriptIdentity{}, false
	}
	return CodexTranscriptIdentity{DataRoot: dataRoots[0]}, false
}

// ResolveCodexTranscriptIdentityForAgent prefers an existing host binding, then
// resolves from the live process tree. Cwd matching is never authority.
func ResolveCodexTranscriptIdentityForAgent(
	agent classifier.Agent,
	existing CodexTranscriptIdentity,
) CodexTranscriptIdentity {
	existing.SessionID = strings.TrimSpace(existing.SessionID)
	existing.Path = strings.TrimSpace(existing.Path)
	existing.DataRoot = strings.TrimSpace(existing.DataRoot)
	if existing.Path != "" {
		if _, err := os.Stat(existing.Path); err == nil {
			if existing.SessionID == "" {
				existing.SessionID = sessionIDFromCodexRolloutPath(existing.Path)
			}
			if existing.DataRoot == "" {
				existing.DataRoot = dataRootForPath(existing.Path, nil)
			}
			return existing
		}
	}
	if existing.SessionID != "" {
		roots := []string{}
		if existing.DataRoot != "" {
			roots = append(roots, existing.DataRoot)
		}
		roots = append(roots, providerDataRootsForProcessTree(agent.ProcessID)...)
		for _, root := range uniqueStrings(roots) {
			if row, ok, err := lookupCodexThreadByIDInRoot(existing.SessionID, root); err == nil && ok {
				path := strings.TrimSpace(row.RolloutPath)
				if path == "" {
					continue
				}
				if _, err := os.Stat(path); err != nil {
					continue
				}
				return CodexTranscriptIdentity{
					SessionID: existing.SessionID,
					Path:      path,
					DataRoot:  firstNonEmpty(root, existing.DataRoot),
				}
			}
		}
	}
	if resolved, ok := ResolveCodexTranscriptIdentity(agent.ProcessID); ok {
		return resolved
	}
	return existing
}

// LoadCodexConversationByIdentity loads a Codex rollout by stable session/path
// identity under the bound provider data root. It never selects "latest by cwd".
func LoadCodexConversationByIdentity(identity CodexTranscriptIdentity) (CodexConversation, error) {
	identity.SessionID = strings.TrimSpace(identity.SessionID)
	identity.Path = strings.TrimSpace(identity.Path)
	identity.DataRoot = strings.TrimSpace(identity.DataRoot)
	path := identity.Path
	if path == "" && identity.SessionID != "" {
		row, ok, err := lookupCodexThreadByIDInRoot(identity.SessionID, identity.DataRoot)
		if err != nil {
			return CodexConversation{}, err
		}
		if !ok {
			return CodexConversation{
				Available: false,
				Reason:    "transcript_not_found",
				Events:    []CodexConversationEvent{},
			}, nil
		}
		path = strings.TrimSpace(row.RolloutPath)
	}
	if path == "" {
		return CodexConversation{
			Available: false,
			Reason:    "transcript_not_found",
			Events:    []CodexConversationEvent{},
		}, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return CodexConversation{
			Available: false,
			Reason:    "transcript_not_found",
			Events:    []CodexConversationEvent{},
		}, nil
	}
	reader := &ProviderConversationReader{}
	conversation, err := reader.loadCodexConversation(path)
	if err != nil {
		return CodexConversation{}, err
	}
	conversation.Available = true
	conversation.Source = "codex_rollout"
	conversation.Path = path
	if conversation.SessionID == "" {
		conversation.SessionID = identity.SessionID
	}
	if conversation.SessionID == "" {
		conversation.SessionID = sessionIDFromCodexRolloutPath(path)
	}
	if conversation.SessionID == "" {
		if meta, metaErr := readCodexMeta(path); metaErr == nil {
			conversation.SessionID = strings.TrimSpace(meta.ID)
			conversation.CWD = firstNonEmpty(conversation.CWD, meta.CWD)
		}
	}
	updated := info.ModTime()
	conversation.Updated = &updated
	if conversation.Events == nil {
		conversation.Events = []CodexConversationEvent{}
	}
	return conversation, nil
}

// PreferHostBoundConversation returns the bound host transcript when the live
// agent snapshot is unavailable, empty of durable chat, or attached a different
// rollout via cwd matching. Cwd must not override the Host Session bind.
func PreferHostBoundConversation(live, bound CodexConversation) CodexConversation {
	if !bound.Available {
		return live
	}
	if !live.Available || len(live.Events) == 0 {
		return bound
	}
	if strings.TrimSpace(live.Path) != "" &&
		strings.TrimSpace(bound.Path) != "" &&
		filepath.Clean(live.Path) != filepath.Clean(bound.Path) {
		return bound
	}
	return live
}

func lookupCodexThreadByID(sessionID string) (codexThreadRow, bool, error) {
	return lookupCodexThreadByIDInRoot(sessionID, "")
}

func lookupCodexThreadByIDInRoot(sessionID, dataRoot string) (codexThreadRow, bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return codexThreadRow{}, false, nil
	}
	dbPath, ok, err := codexStateDBPath(dataRoot)
	if err != nil || !ok {
		return codexThreadRow{}, false, err
	}
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		return codexThreadRow{}, false, nil
	}
	query := fmt.Sprintf(
		`SELECT id, rollout_path, created_at, coalesce(created_at_ms, 0) AS created_at_ms FROM threads WHERE id = %s LIMIT 1`,
		sqlString(sessionID),
	)
	rows, err := queryCodexThreadRows(sqlite3, dbPath, query)
	if err != nil {
		return codexThreadRow{}, false, err
	}
	if len(rows) == 0 {
		return codexThreadRow{}, false, nil
	}
	return rows[0], true, nil
}

func lookupCodexThreadByRolloutPath(path string) (codexThreadRow, bool, error) {
	return lookupCodexThreadByRolloutPathInRoot(path, dataRootForPath(path, nil))
}

func lookupCodexThreadByRolloutPathInRoot(path, dataRoot string) (codexThreadRow, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return codexThreadRow{}, false, nil
	}
	dbPath, ok, err := codexStateDBPath(dataRoot)
	if err != nil || !ok {
		return codexThreadRow{}, false, err
	}
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		return codexThreadRow{}, false, nil
	}
	query := fmt.Sprintf(
		`SELECT id, rollout_path, created_at, coalesce(created_at_ms, 0) AS created_at_ms FROM threads WHERE rollout_path = %s LIMIT 1`,
		sqlString(path),
	)
	rows, err := queryCodexThreadRows(sqlite3, dbPath, query)
	if err != nil {
		return codexThreadRow{}, false, err
	}
	if len(rows) == 0 {
		return codexThreadRow{}, false, nil
	}
	return rows[0], true, nil
}

func codexStateDBPath(dataRoot string) (string, bool, error) {
	dataRoot = strings.TrimSpace(dataRoot)
	if dataRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false, err
		}
		dataRoot = home
	}
	dbPath := filepath.Join(dataRoot, ".codex", "state_5.sqlite")
	if _, err := os.Stat(dbPath); err != nil {
		return "", false, nil
	}
	return dbPath, true, nil
}

func providerDataRootsForProcessTree(processID int) []string {
	var roots []string
	for _, pid := range append([]int{processID}, procDescendantPIDs(processID)...) {
		if home := strings.TrimSpace(procEnvironValue(pid, "HOME")); home != "" {
			roots = append(roots, filepath.Clean(home))
		}
	}
	return uniqueStrings(roots)
}

func firstProcessTreeEnvironValue(processID int, key string) string {
	for _, pid := range append([]int{processID}, procDescendantPIDs(processID)...) {
		if value := strings.TrimSpace(procEnvironValue(pid, key)); value != "" {
			return value
		}
	}
	return ""
}

func dataRootForPath(path string, preferred []string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	for _, root := range preferred {
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "" {
			continue
		}
		prefix := root + string(os.PathSeparator) + ".codex" + string(os.PathSeparator)
		if strings.HasPrefix(path, prefix) || path == filepath.Join(root, ".codex") {
			return root
		}
	}
	const marker = string(os.PathSeparator) + ".codex" + string(os.PathSeparator) + "sessions" + string(os.PathSeparator)
	if index := strings.Index(path, marker); index > 0 {
		return path[:index]
	}
	return ""
}

func sessionIDFromCodexRolloutPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if meta, err := readCodexMeta(path); err == nil {
		if id := strings.TrimSpace(meta.ID); id != "" {
			return id
		}
	}
	base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if !strings.HasPrefix(base, "rollout-") {
		return ""
	}
	// rollout-<timestamp>-<uuid>
	parts := strings.Split(base, "-")
	if len(parts) < 6 {
		return ""
	}
	return strings.Join(parts[len(parts)-5:], "-")
}

func procEnvironValue(processID int, key string) string {
	if processID <= 0 || strings.TrimSpace(key) == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(processID), "environ"))
	if err != nil {
		return ""
	}
	prefix := key + "="
	for _, entry := range strings.Split(string(raw), "\x00") {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(entry, prefix))
		}
	}
	return ""
}

func procDescendantPIDs(root int) []int {
	if root <= 0 {
		return nil
	}
	seen := map[int]bool{root: true}
	queue := []int{root}
	var out []int
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		for _, child := range procChildPIDs(pid) {
			if seen[child] {
				continue
			}
			seen[child] = true
			out = append(out, child)
			queue = append(queue, child)
		}
	}
	return out
}

func procChildPIDs(processID int) []int {
	if processID <= 0 {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(
		"/proc", strconv.Itoa(processID), "task", strconv.Itoa(processID), "children",
	))
	if err != nil {
		return nil
	}
	fields := strings.Fields(string(raw))
	out := make([]int, 0, len(fields))
	for _, field := range fields {
		pid, err := strconv.Atoi(field)
		if err != nil || pid <= 0 {
			continue
		}
		out = append(out, pid)
	}
	return out
}
