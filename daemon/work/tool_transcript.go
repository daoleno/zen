package work

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

const (
	maxTranscriptAge                      = 72 * time.Hour
	maxCodexActiveTranscriptStartBackdate = 2 * time.Minute
)

type codexThreadRow struct {
	ID          string `json:"id"`
	RolloutPath string `json:"rollout_path"`
	CreatedAt   int64  `json:"created_at"`
	CreatedAtMS int64  `json:"created_at_ms"`
}

type codexTranscriptCandidate struct {
	Row     codexThreadRow
	Meta    codexMeta
	Path    string
	Updated time.Time
}

func findCodexTranscript(agent classifier.Agent, now time.Time) (codexTranscriptCandidate, bool, error) {
	cwd := strings.TrimSpace(agent.Cwd)
	if cwd == "" {
		return codexTranscriptCandidate{}, false, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return codexTranscriptCandidate{}, false, err
	}
	dbPath := filepath.Join(home, ".codex", "state_5.sqlite")
	if _, err := os.Stat(dbPath); err != nil {
		return codexTranscriptCandidate{}, false, nil
	}
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		return codexTranscriptCandidate{}, false, nil
	}
	openRolloutPaths := openCodexRolloutPathsForProcess(agent.ProcessID)

	var candidates []codexTranscriptCandidate
	for _, candidateCWD := range transcriptCWDCandidates(cwd) {
		rows, err := queryCodexThreads(sqlite3, dbPath, candidateCWD)
		if err != nil {
			return codexTranscriptCandidate{}, false, err
		}
		for _, row := range rows {
			path := strings.TrimSpace(row.RolloutPath)
			if path == "" {
				continue
			}
			meta, err := readCodexMeta(path)
			if err != nil {
				continue
			}
			if meta.CWD != candidateCWD || strings.EqualFold(meta.Originator, "codex-exec") {
				continue
			}
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			candidates = append(candidates, codexTranscriptCandidate{
				Row:     row,
				Meta:    meta,
				Path:    path,
				Updated: info.ModTime(),
			})
		}
	}
	if len(candidates) == 0 {
		return codexTranscriptCandidate{}, false, nil
	}
	if matched, ok := matchCodexTranscriptToOpenRollouts(candidates, openRolloutPaths); ok {
		return matched, true, nil
	}
	freshCandidates := freshCodexTranscriptCandidates(candidates, now)
	if len(freshCandidates) == 0 {
		return codexTranscriptCandidate{}, false, nil
	}
	if matched, ok := matchCodexTranscriptToAgentStart(freshCandidates, agent.StartedAt); ok {
		return matched, true, nil
	}
	if isCodexResumeCommand(agent.Command) {
		return latestUpdatedCodexTranscript(freshCandidates), true, nil
	}
	if matched, ok := matchCodexTranscriptToActiveSession(freshCandidates, agent.StartedAt); ok {
		return matched, true, nil
	}
	if matched, ok := fallbackCodexTranscriptForAgent(freshCandidates, agent); ok {
		return matched, true, nil
	}
	return codexTranscriptCandidate{}, false, nil
}

func freshCodexTranscriptCandidates(candidates []codexTranscriptCandidate, now time.Time) []codexTranscriptCandidate {
	if len(candidates) == 0 {
		return nil
	}
	fresh := make([]codexTranscriptCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if isTranscriptFresh(candidate.Updated, now) {
			fresh = append(fresh, candidate)
		}
	}
	return fresh
}

func matchCodexTranscriptToActiveSession(candidates []codexTranscriptCandidate, startedAt time.Time) (codexTranscriptCandidate, bool) {
	if len(candidates) == 0 || startedAt.IsZero() {
		return codexTranscriptCandidate{}, false
	}
	startedAt = startedAt.UTC()
	minCreatedAt := startedAt.Add(-maxCodexActiveTranscriptStartBackdate)
	var eligible []codexTranscriptCandidate
	for _, candidate := range candidates {
		if candidate.Updated.IsZero() || candidate.Updated.Before(startedAt) {
			continue
		}
		if createdAt := candidateCreatedAt(candidate.Row); !createdAt.IsZero() && createdAt.Before(minCreatedAt) {
			continue
		}
		eligible = append(eligible, candidate)
	}
	if len(eligible) == 0 {
		return codexTranscriptCandidate{}, false
	}
	return latestUpdatedCodexTranscript(eligible), true
}

func fallbackCodexTranscriptForAgent(candidates []codexTranscriptCandidate, agent classifier.Agent) (codexTranscriptCandidate, bool) {
	if len(candidates) == 0 || !isBrainCodexAgent(agent) {
		return codexTranscriptCandidate{}, false
	}
	if !agent.StartedAt.IsZero() {
		var eligible []codexTranscriptCandidate
		minCreatedAt := agent.StartedAt.UTC().Add(-5 * time.Second)
		for _, candidate := range candidates {
			createdAt := candidateCreatedAt(candidate.Row)
			if !createdAt.IsZero() && createdAt.Before(minCreatedAt) {
				continue
			}
			if candidate.Updated.IsZero() || candidate.Updated.Before(minCreatedAt) {
				continue
			}
			eligible = append(eligible, candidate)
		}
		if len(eligible) == 0 {
			return codexTranscriptCandidate{}, false
		}
		return latestUpdatedCodexTranscript(eligible), true
	}
	return latestUpdatedCodexTranscript(candidates), true
}

func isBrainCodexAgent(agent classifier.Agent) bool {
	if !agent.Hidden {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(agent.Name), "Brain") {
		return true
	}
	sessionName, _, _ := strings.Cut(strings.TrimSpace(agent.ID), ":")
	return strings.HasPrefix(sessionName, "brain-agent-brain-")
}

func matchCodexTranscriptToAgentProcess(candidates []codexTranscriptCandidate, processID int) (codexTranscriptCandidate, bool) {
	if processID <= 0 {
		return codexTranscriptCandidate{}, false
	}
	return matchCodexTranscriptToOpenRollouts(candidates, openCodexRolloutPathsForProcess(processID))
}

func matchCodexTranscriptToOpenRollouts(candidates []codexTranscriptCandidate, paths []string) (codexTranscriptCandidate, bool) {
	if len(candidates) == 0 || len(paths) == 0 {
		return codexTranscriptCandidate{}, false
	}

	openPaths := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if normalized := normalizeOpenFilePath(path); normalized != "" {
			openPaths[normalized] = struct{}{}
		}
	}
	if len(openPaths) == 0 {
		return codexTranscriptCandidate{}, false
	}

	var matched []codexTranscriptCandidate
	for _, candidate := range candidates {
		if _, ok := openPaths[normalizeOpenFilePath(candidate.Path)]; ok {
			matched = append(matched, candidate)
		}
	}
	if len(matched) == 0 {
		return codexTranscriptCandidate{}, false
	}
	return latestUpdatedCodexTranscript(matched), true
}

func matchCodexTranscriptToAgentStart(candidates []codexTranscriptCandidate, startedAt time.Time) (codexTranscriptCandidate, bool) {
	if startedAt.IsZero() {
		return codexTranscriptCandidate{}, false
	}
	startedAt = startedAt.UTC()
	minCreatedAt := startedAt.Add(-5 * time.Second)
	bestIndex := -1
	var bestDelta time.Duration
	for index, candidate := range candidates {
		createdAt := candidateCreatedAt(candidate.Row)
		if createdAt.IsZero() || createdAt.Before(minCreatedAt) || candidate.Updated.Before(startedAt) {
			continue
		}
		delta := createdAt.Sub(startedAt)
		if delta < 0 {
			delta = -delta
		}
		if bestIndex == -1 || delta < bestDelta ||
			(delta == bestDelta && candidate.Updated.After(candidates[bestIndex].Updated)) {
			bestIndex = index
			bestDelta = delta
		}
	}
	if bestIndex == -1 {
		return codexTranscriptCandidate{}, false
	}
	return candidates[bestIndex], true
}

func latestUpdatedCodexTranscript(candidates []codexTranscriptCandidate) codexTranscriptCandidate {
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.Updated.After(best.Updated) {
			best = candidate
		}
	}
	return best
}

func openCodexRolloutPathsForProcess(processID int) []string {
	if paths := procOpenCodexRolloutPaths(processID); len(paths) > 0 {
		return paths
	}
	return lsofOpenCodexRolloutPaths(processID)
}

func procOpenCodexRolloutPaths(processID int) []string {
	if processID <= 0 {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join("/proc", strconv.Itoa(processID), "fd"))
	if err != nil {
		return nil
	}
	var paths []string
	for _, entry := range entries {
		path, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(processID), "fd", entry.Name()))
		if err != nil {
			continue
		}
		if isCodexRolloutPath(path) {
			paths = append(paths, normalizeOpenFilePath(path))
		}
	}
	return uniqueStrings(paths)
}

func lsofOpenCodexRolloutPaths(processID int) []string {
	if processID <= 0 {
		return nil
	}
	lsof, err := exec.LookPath("lsof")
	if err != nil {
		return nil
	}
	out, err := exec.Command(lsof, "-w", "-p", strconv.Itoa(processID), "-Fn").Output()
	if err != nil {
		return nil
	}
	return parseLsofCodexRolloutPaths(string(out))
}

func parseLsofCodexRolloutPaths(output string) []string {
	var paths []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "n") {
			continue
		}
		path := strings.TrimSpace(strings.TrimPrefix(line, "n"))
		if isCodexRolloutPath(path) {
			paths = append(paths, normalizeOpenFilePath(path))
		}
	}
	return uniqueStrings(paths)
}

func isCodexRolloutPath(path string) bool {
	normalized := filepath.ToSlash(normalizeOpenFilePath(path))
	return strings.Contains(normalized, "/.codex/sessions/") &&
		strings.HasPrefix(filepath.Base(normalized), "rollout-") &&
		strings.HasSuffix(normalized, ".jsonl")
}

func normalizeOpenFilePath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimSuffix(path, " (deleted)")
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func isCodexResumeCommand(command string) bool {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(command)))
	for _, field := range fields[1:] {
		if strings.Trim(field, `"'`) == "resume" {
			return true
		}
	}
	return false
}

func candidateCreatedAt(row codexThreadRow) time.Time {
	switch {
	case row.CreatedAtMS > 0:
		return time.UnixMilli(row.CreatedAtMS).UTC()
	case row.CreatedAt > 0:
		return time.Unix(row.CreatedAt, 0).UTC()
	default:
		return time.Time{}
	}
}

func queryCodexThreads(sqlite3, dbPath, cwd string) ([]codexThreadRow, error) {
	query := fmt.Sprintf(`SELECT id, rollout_path, created_at, coalesce(created_at_ms, 0) AS created_at_ms FROM threads WHERE archived = 0 AND cwd = %s ORDER BY coalesce(updated_at_ms, updated_at * 1000) DESC LIMIT 48`, sqlString(cwd))
	return queryCodexThreadRows(sqlite3, dbPath, query)
}

func queryCodexThreadRows(sqlite3, dbPath, query string) ([]codexThreadRow, error) {
	out, err := exec.Command(sqlite3, "-json", dbPath, query).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("query codex threads: %w%s", err, stderrSuffix(string(out)))
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, nil
	}
	var rows []codexThreadRow
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

type codexMeta struct {
	ID         string
	CWD        string
	Originator string
}

func readCodexMeta(path string) (codexMeta, error) {
	file, err := os.Open(path)
	if err != nil {
		return codexMeta{}, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var envelope struct {
				Type    string `json:"type"`
				Payload struct {
					ID         string `json:"id"`
					CWD        string `json:"cwd"`
					Originator string `json:"originator"`
				} `json:"payload"`
			}
			if json.Unmarshal(line, &envelope) == nil && envelope.Type == "session_meta" {
				return codexMeta{
					ID:         strings.TrimSpace(envelope.Payload.ID),
					CWD:        strings.TrimSpace(envelope.Payload.CWD),
					Originator: strings.TrimSpace(envelope.Payload.Originator),
				}, nil
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return codexMeta{}, err
		}
	}
	return codexMeta{}, fmt.Errorf("missing codex session metadata")
}

type claudeMeta struct {
	CWD       string
	SessionID string
}

func readClaudeMeta(path string) (claudeMeta, error) {
	file, err := os.Open(path)
	if err != nil {
		return claudeMeta{}, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	lineCount := 0
	meta := claudeMeta{}
	for {
		line, err := reader.ReadBytes('\n')
		lineCount++
		if len(bytes.TrimSpace(line)) > 0 {
			var envelope struct {
				CWD       string `json:"cwd"`
				SessionID string `json:"sessionId"`
			}
			if json.Unmarshal(line, &envelope) == nil {
				if cwd := strings.TrimSpace(envelope.CWD); cwd != "" {
					meta.CWD = cwd
				}
				if sessionID := strings.TrimSpace(envelope.SessionID); sessionID != "" {
					meta.SessionID = sessionID
				}
				if meta.CWD != "" && meta.SessionID != "" {
					return meta, nil
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return claudeMeta{}, err
		}
		if lineCount >= 80 && (meta.CWD != "" || meta.SessionID != "") {
			return meta, nil
		}
	}
	return meta, nil
}

func codexExecCommand(arguments string) string {
	var payload struct {
		Cmd     string          `json:"cmd"`
		Command json.RawMessage `json:"command"`
	}
	if json.Unmarshal([]byte(arguments), &payload) != nil {
		return ""
	}
	if cmd := strings.TrimSpace(payload.Cmd); cmd != "" {
		return cmd
	}
	if command := jsonString(payload.Command); command != "" {
		return command
	}
	var command []string
	if json.Unmarshal(payload.Command, &command) == nil {
		return shellCommandLabel(command)
	}
	return ""
}

func claudeContentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		return jsonString(raw)
	}
	var items []map[string]json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return ""
	}
	var parts []string
	for _, item := range items {
		if text := jsonString(item["text"]); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func claudeToolSurface(name string, raw json.RawMessage) string {
	if name != "Read" && name != "Edit" && name != "MultiEdit" && name != "Write" {
		return ""
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(raw, &input) != nil {
		return ""
	}
	return jsonString(input["file_path"])
}

func patchSurfaces(patch string) []string {
	return patchSurfacesFromChanges(patchFileChanges(patch))
}

func patchSurfacesFromChanges(changes []CodexConversationFileChange) []string {
	seen := map[string]bool{}
	var out []string
	for _, change := range changes {
		path := strings.TrimSpace(change.Path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func patchFileChanges(patch string) []CodexConversationFileChange {
	if strings.TrimSpace(patch) == "" {
		return nil
	}

	normalized := strings.ReplaceAll(strings.ReplaceAll(patch, "\r\n", "\n"), "\r", "\n")
	complete := false
	for _, line := range strings.Split(normalized, "\n") {
		if strings.TrimRight(line, " \t") == "*** End Patch" {
			complete = true
			break
		}
	}

	var changes []CodexConversationFileChange
	var current *CodexConversationFileChange
	added := 0
	removed := 0
	finishCurrent := func() {
		if current == nil {
			return
		}
		if complete && current.Operation != "delete" {
			current.Additions = intPointer(added)
			current.Deletions = intPointer(removed)
		}
		changes = append(changes, *current)
		current = nil
		added = 0
		removed = 0
	}

	for _, rawLine := range strings.Split(normalized, "\n") {
		line := strings.TrimRight(rawLine, " \t")
		var operation string
		var path string
		switch {
		case strings.HasPrefix(line, "*** Update File: "):
			operation = "update"
			path = strings.TrimSpace(strings.TrimPrefix(line, "*** Update File: "))
		case strings.HasPrefix(line, "*** Add File: "):
			operation = "add"
			path = strings.TrimSpace(strings.TrimPrefix(line, "*** Add File: "))
		case strings.HasPrefix(line, "*** Delete File: "):
			operation = "delete"
			path = strings.TrimSpace(strings.TrimPrefix(line, "*** Delete File: "))
		}
		if path != "" {
			finishCurrent()
			current = &CodexConversationFileChange{
				Path:      path,
				Operation: operation,
			}
			continue
		}
		if current == nil {
			continue
		}
		if strings.HasPrefix(line, "*** Move to: ") {
			current.MovePath = strings.TrimSpace(strings.TrimPrefix(line, "*** Move to: "))
			continue
		}
		if strings.HasPrefix(line, "***") || strings.HasPrefix(line, "@@") {
			continue
		}
		if strings.HasPrefix(rawLine, "+") {
			added++
		} else if strings.HasPrefix(rawLine, "-") {
			removed++
		}
	}
	finishCurrent()
	return changes
}

func intPointer(value int) *int {
	return &value
}

func shellCommandLabel(command []string) string {
	if len(command) == 0 {
		return ""
	}
	if len(command) >= 3 && (strings.HasSuffix(command[0], "sh") || strings.HasSuffix(command[0], "zsh") || strings.HasSuffix(command[0], "bash")) && command[1] == "-lc" {
		return command[2]
	}
	return strings.Join(command, " ")
}

func isTranscriptBoilerplate(value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	return isCodexContextualFragment(trimmed) ||
		isCodexInstructionContextFragment(trimmed) ||
		strings.HasPrefix(trimmed, "<local-command-caveat>") ||
		strings.HasPrefix(trimmed, "<command-name>") ||
		strings.HasPrefix(trimmed, "<local-command-stdout>") ||
		strings.Contains(lower, "base directory for this skill") ||
		strings.Contains(lower, "you are codex")
}

func isCodexContextualFragment(value string) bool {
	return (strings.TrimSpace(value) != "" && stripCodexContextualFragments(value) == "") ||
		isLegacyCodexContextualFragment(value)
}

// CleanCodexDisplayText removes Codex-internal context blocks from text before
// it is shown in user-facing surfaces.
func CleanCodexDisplayText(value string) string {
	value = cleanConversationText(stripCodexContextualFragments(value))
	if isCodexInstructionContextFragment(value) {
		return ""
	}
	return value
}

type codexContextualFragmentMarker struct {
	open  string
	close string
}

var codexContextualFragmentMarkers = []codexContextualFragmentMarker{
	{open: "# AGENTS.md instructions for ", close: "</INSTRUCTIONS>"},
	{open: "<environment_context>", close: "</environment_context>"},
	{open: "<apps_instructions>", close: "</apps_instructions>"},
	{open: "<skills_instructions>", close: "</skills_instructions>"},
	{open: "<plugins_instructions>", close: "</plugins_instructions>"},
	{open: "<collaboration_mode>", close: "</collaboration_mode>"},
	{open: "<realtime_conversation>", close: "</realtime_conversation>"},
	{open: "<permissions instructions>", close: "</permissions instructions>"},
	{open: "<skill>", close: "</skill>"},
	{open: "<user_shell_command>", close: "</user_shell_command>"},
	{open: "<turn_aborted>", close: "</turn_aborted>"},
	{open: "<subagent_notification>", close: "</subagent_notification>"},
	{open: "<goal_context>", close: "</goal_context>"},
	{open: "<model_switch>", close: "</model_switch>"},
	{open: "<personality_spec>", close: "</personality_spec>"},
}

func stripCodexContextualFragments(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	stripped := value
	for {
		start, end, ok := firstCodexContextualFragmentRange(stripped)
		if !ok {
			break
		}
		stripped = stripped[:start] + "\n" + stripped[end:]
	}
	return strings.TrimSpace(stripped)
}

func firstCodexContextualFragmentRange(value string) (int, int, bool) {
	bestStart := -1
	bestEnd := -1
	for _, markers := range codexContextualFragmentMarkers {
		start, end, ok := markedTextRange(markers.open, markers.close, value)
		if !ok {
			continue
		}
		if bestStart == -1 || start < bestStart {
			bestStart = start
			bestEnd = end
		}
	}
	return bestStart, bestEnd, bestStart >= 0
}

func markedTextRange(openMarker, closeMarker, value string) (int, int, bool) {
	if openMarker == "" || closeMarker == "" {
		return 0, 0, false
	}
	searchFrom := 0
	for searchFrom < len(value) {
		relativeStart := strings.Index(value[searchFrom:], openMarker)
		if relativeStart < 0 {
			return 0, 0, false
		}
		start := searchFrom + relativeStart
		if !isLineStartMarker(value, start) {
			searchFrom = start + len(openMarker)
			continue
		}
		closeSearchFrom := start + len(openMarker)
		relativeEnd := strings.Index(value[closeSearchFrom:], closeMarker)
		if relativeEnd < 0 {
			return 0, 0, false
		}
		end := closeSearchFrom + relativeEnd + len(closeMarker)
		return start, end, true
	}
	return 0, 0, false
}

func isLineStartMarker(value string, index int) bool {
	for cursor := index - 1; cursor >= 0; cursor-- {
		switch value[cursor] {
		case ' ', '\t', '\r':
			continue
		case '\n':
			return true
		default:
			return false
		}
	}
	return true
}

func isLegacyCodexContextualFragment(value string) bool {
	trimmed := strings.TrimSpace(value)
	return strings.HasPrefix(trimmed, "Warning: apply_patch was requested via ") &&
		strings.HasSuffix(trimmed, "Use the apply_patch tool instead of exec_command.") ||
		strings.HasPrefix(trimmed, "Warning: Your account was flagged for potentially high-risk cyber activity") ||
		strings.HasPrefix(trimmed, "Warning: The maximum number of unified exec processes you can keep open is")
}

func isCodexInstructionContextFragment(value string) bool {
	trimmed := cleanConversationText(value)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "# repository guidelines") ||
		strings.HasPrefix(lower, "repository guidelines\n") ||
		strings.HasPrefix(lower, "## project structure & module organization") ||
		strings.Contains(lower, "agents.md instructions for ") {
		return true
	}
	markers := []string{
		"repository guidelines",
		"project structure & module organization",
		"build, test, and development commands",
		"coding style & naming conventions",
		"testing guidelines",
		"commit & pull request guidelines",
		"security & configuration tips",
		"configuration & secrets",
		"agent & sandbox releases",
		"first-principles engineering",
		"exchange data & trading state",
		"refresh cadence is part of the product contract",
		"avoid compatibility barrels",
	}
	strongMarkers := []string{
		"agent & sandbox releases",
		"first-principles engineering",
		"exchange data & trading state",
		"refresh cadence is part of the product contract",
		"avoid compatibility barrels",
		"freeride-sandbox",
		"daytona",
	}
	count := 0
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			count++
		}
	}
	hasStrongMarker := false
	for _, marker := range strongMarkers {
		if strings.Contains(lower, marker) {
			hasStrongMarker = true
			break
		}
	}
	return count >= 2 && hasStrongMarker
}

func encodeClaudeProjectDir(cwd string) string {
	clean := filepath.Clean(cwd)
	return strings.ReplaceAll(clean, string(filepath.Separator), "-")
}

func transcriptCWDCandidates(cwd string) []string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return nil
	}
	clean := filepath.Clean(cwd)
	if clean == "." {
		return nil
	}

	var out []string
	seen := map[string]bool{}
	add := func(path string) {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "" || path == "." || seen[path] {
			return
		}
		seen[path] = true
		out = append(out, path)
	}

	add(clean)
	if gitRoot := nearestGitRoot(clean); gitRoot != "" {
		add(gitRoot)
		return out
	}
	for parent := filepath.Dir(clean); parent != clean && parent != "." && parent != string(filepath.Separator); parent = filepath.Dir(parent) {
		add(parent)
		if len(out) >= 3 {
			break
		}
		clean = parent
	}
	return out
}

func nearestGitRoot(cwd string) string {
	for dir := filepath.Clean(cwd); dir != "." && dir != string(filepath.Separator); dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
	}
	return ""
}

func isTranscriptFresh(updated, now time.Time) bool {
	if updated.IsZero() || now.IsZero() {
		return true
	}
	if updated.After(now.Add(10 * time.Minute)) {
		return true
	}
	return now.Sub(updated) <= maxTranscriptAge
}

func sqlString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func jsonString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var out string
	if err := json.Unmarshal(raw, &out); err == nil {
		return strings.TrimSpace(out)
	}
	return ""
}
