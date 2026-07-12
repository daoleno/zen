package classifier

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	cursorTranscriptProjectPrefix = ".cursor/projects"
	cursorTranscriptDirName       = "agent-transcripts"
	cursorTranscriptMaxAge        = 72 * time.Hour
)

// CursorTranscriptActiver is the Cursor-adapter-internal transcript turn probe.
// It is not a Watcher dependency; wire it through CursorActivityAdapter.
type CursorTranscriptActiver interface {
	Active(agent Agent) (active bool, ok bool)
}

// CursorTranscriptProbeStats are cumulative counters for cost tests.
type CursorTranscriptProbeStats struct {
	StatCalls         int
	OpenCalls         int
	ReadCalls         int
	BytesRead         int64
	PathResolveCalls  int
	PathMissCacheHits int
	FullScans         int
	TailScans         int
	CacheHits         int
}

// CursorTranscriptActiveProbe is a cheap activity probe for Cursor JSONL
// transcripts. Warm polls are Stat-only when size+mtime are unchanged; growth
// is scanned from the previous byte offset and only inspects user / turn_ended
// markers (not a full conversation parse).
type CursorTranscriptActiveProbe struct {
	mu      sync.Mutex
	homeDir string
	now     func() time.Time
	byAgent map[string]*cursorTranscriptActiveEntry
	stats   CursorTranscriptProbeStats
}

type cursorTranscriptActiveEntry struct {
	agentKey      string
	cwd           string
	resumeID      string
	path          string
	size          int64
	modTime       time.Time
	scannedSize   int64
	lastUserOff   int64 // byte offset of line start; -1 if none
	lastEndedOff  int64
	active        bool
	haveMeta      bool
	pathMissUntil time.Time
	missStreak    int
}

// NewCursorTranscriptActiveProbe builds a probe rooted at the user home
// directory (overridable via SetHomeDir for tests).
func NewCursorTranscriptActiveProbe() *CursorTranscriptActiveProbe {
	return &CursorTranscriptActiveProbe{
		now:     time.Now,
		byAgent: map[string]*cursorTranscriptActiveEntry{},
	}
}

// SetHomeDir overrides the home used to locate ~/.cursor/projects.
func (p *CursorTranscriptActiveProbe) SetHomeDir(home string) *CursorTranscriptActiveProbe {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.homeDir = strings.TrimSpace(home)
	return p
}

// Stats returns a snapshot of cumulative I/O counters.
func (p *CursorTranscriptActiveProbe) Stats() CursorTranscriptProbeStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stats
}

// ResetStats clears cumulative I/O counters (tests only).
func (p *CursorTranscriptActiveProbe) ResetStats() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats = CursorTranscriptProbeStats{}
}

// Active implements CursorTranscriptActiver.
func (p *CursorTranscriptActiveProbe) Active(agent Agent) (bool, bool) {
	if p == nil {
		return false, false
	}
	if !isCursorAgentCommandLine(agent.Command) {
		return false, false
	}
	cwd := strings.TrimSpace(agent.Cwd)
	if cwd == "" {
		return false, false
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now().UTC()
	if p.now != nil {
		now = p.now()
	}

	key := cursorTranscriptAgentKey(agent)
	resumeID := cursorResumeSessionIDFromCommand(agent.Command)
	entry := p.byAgent[key]
	if entry == nil || entry.cwd != cwd || entry.resumeID != resumeID {
		entry = &cursorTranscriptActiveEntry{
			agentKey:     key,
			cwd:          cwd,
			resumeID:     resumeID,
			lastUserOff:  -1,
			lastEndedOff: -1,
		}
		p.byAgent[key] = entry
	}

	if entry.path == "" {
		if !entry.pathMissUntil.IsZero() && now.Before(entry.pathMissUntil) {
			p.stats.PathMissCacheHits++
			return false, false
		}
		path, ok := p.resolveTranscriptPathLocked(agent, resumeID)
		p.stats.PathResolveCalls++
		if !ok {
			p.markCursorPathMissLocked(entry, now)
			return false, false
		}
		entry.missStreak = 0
		entry.pathMissUntil = time.Time{}
		entry.path = path
	}

	info, err := os.Stat(entry.path)
	p.stats.StatCalls++
	if err != nil {
		entry.path = ""
		if !entry.pathMissUntil.IsZero() && now.Before(entry.pathMissUntil) {
			p.stats.PathMissCacheHits++
			return false, false
		}
		path, ok := p.resolveTranscriptPathLocked(agent, resumeID)
		p.stats.PathResolveCalls++
		if !ok {
			p.markCursorPathMissLocked(entry, now)
			return false, false
		}
		entry.missStreak = 0
		entry.pathMissUntil = time.Time{}
		entry.path = path
		entry.scannedSize = 0
		entry.lastUserOff = -1
		entry.lastEndedOff = -1
		entry.haveMeta = false
		info, err = os.Stat(entry.path)
		p.stats.StatCalls++
		if err != nil {
			entry.path = ""
			p.markCursorPathMissLocked(entry, now)
			return false, false
		}
	}

	if entry.haveMeta && entry.size == info.Size() && entry.modTime.Equal(info.ModTime()) {
		p.stats.CacheHits++
		return entry.active, true
	}

	if info.Size() < entry.scannedSize {
		entry.scannedSize = 0
		entry.lastUserOff = -1
		entry.lastEndedOff = -1
		p.stats.FullScans++
	} else if entry.scannedSize == 0 {
		p.stats.FullScans++
	} else {
		p.stats.TailScans++
	}

	if err := p.scanEntryLocked(entry, info.Size()); err != nil {
		return false, false
	}
	entry.size = info.Size()
	entry.modTime = info.ModTime()
	entry.haveMeta = true
	entry.active = transcriptOffsetsActive(entry.lastUserOff, entry.lastEndedOff)
	return entry.active, true
}

func transcriptOffsetsActive(lastUserOff, lastEndedOff int64) bool {
	if lastUserOff < 0 {
		return false
	}
	return lastUserOff > lastEndedOff
}

func (p *CursorTranscriptActiveProbe) markCursorPathMissLocked(entry *cursorTranscriptActiveEntry, now time.Time) {
	if entry == nil {
		return
	}
	entry.missStreak++
	shift := entry.missStreak - 1
	if shift > 5 {
		shift = 5
	}
	backoff := pathMissBackoffMin << shift
	if backoff > pathMissBackoffMax {
		backoff = pathMissBackoffMax
	}
	entry.pathMissUntil = now.Add(backoff)
}

func cursorTranscriptAgentKey(agent Agent) string {
	if id := strings.TrimSpace(agent.ID); id != "" {
		return id
	}
	return strings.TrimSpace(agent.Cwd) + "\x00" + strings.TrimSpace(agent.Command)
}

func (p *CursorTranscriptActiveProbe) resolveHomeLocked() string {
	if p.homeDir != "" {
		return p.homeDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func (p *CursorTranscriptActiveProbe) resolveTranscriptPathLocked(agent Agent, resumeID string) (string, bool) {
	home := p.resolveHomeLocked()
	if home == "" {
		return "", false
	}
	now := p.now()
	if now.IsZero() {
		now = time.Now().UTC()
	}

	var candidates []cursorTranscriptPathCandidate
	for _, candidateCWD := range cursorTranscriptCWDCandidates(agent.Cwd) {
		projectDir := filepath.Join(home, cursorTranscriptProjectPrefix, encodeCursorProjectDirName(candidateCWD))
		root := filepath.Join(projectDir, cursorTranscriptDirName)
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			sessionID := entry.Name()
			path := filepath.Join(root, sessionID, sessionID+".jsonl")
			info, err := os.Stat(path)
			if err != nil || info.IsDir() {
				continue
			}
			if !cursorTranscriptFresh(info.ModTime(), now) {
				continue
			}
			candidates = append(candidates, cursorTranscriptPathCandidate{
				ID:      sessionID,
				Path:    path,
				Updated: info.ModTime(),
			})
		}
	}
	if len(candidates) == 0 {
		return "", false
	}
	if resumeID != "" {
		for _, candidate := range candidates {
			if strings.EqualFold(candidate.ID, resumeID) {
				return candidate.Path, true
			}
		}
	}
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.Updated.After(best.Updated) {
			best = candidate
		}
	}
	if !agent.StartedAt.IsZero() {
		started := agent.StartedAt.UTC()
		closest := best
		bestDelta := absDuration(closest.Updated.Sub(started))
		for _, candidate := range candidates {
			if candidate.Updated.Before(started.Add(-5 * time.Second)) {
				continue
			}
			delta := absDuration(candidate.Updated.Sub(started))
			if delta < bestDelta || (delta == bestDelta && candidate.Updated.After(closest.Updated)) {
				closest = candidate
				bestDelta = delta
			}
		}
		return closest.Path, true
	}
	return best.Path, true
}

type cursorTranscriptPathCandidate struct {
	ID      string
	Path    string
	Updated time.Time
}

func (p *CursorTranscriptActiveProbe) scanEntryLocked(entry *cursorTranscriptActiveEntry, size int64) error {
	file, err := os.Open(entry.path)
	p.stats.OpenCalls++
	if err != nil {
		return err
	}
	defer file.Close()

	offset := entry.scannedSize
	if offset > 0 {
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return err
		}
	}

	reader := bufio.NewReader(file)
	p.stats.ReadCalls++
	for {
		lineStart := offset
		line, err := reader.ReadBytes('\n')
		n := len(line)
		if n > 0 {
			p.stats.BytesRead += int64(n)
			offset += int64(n)
			inspectCursorTranscriptActivityLine(line, lineStart, &entry.lastUserOff, &entry.lastEndedOff)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	entry.scannedSize = size
	if offset < size {
		// File grew during read; keep offset we consumed.
		entry.scannedSize = offset
	}
	return nil
}

func inspectCursorTranscriptActivityLine(line []byte, lineStart int64, lastUserOff, lastEndedOff *int64) {
	trimmed := bytesTrimSpace(line)
	if len(trimmed) == 0 {
		return
	}
	var typed struct {
		Type string `json:"type"`
		Role string `json:"role"`
	}
	if json.Unmarshal(trimmed, &typed) != nil {
		return
	}
	if strings.EqualFold(strings.TrimSpace(typed.Type), "turn_ended") {
		*lastEndedOff = lineStart
		return
	}
	if strings.EqualFold(strings.TrimSpace(typed.Role), "user") {
		*lastUserOff = lineStart
	}
}

func bytesTrimSpace(b []byte) []byte {
	start := 0
	end := len(b)
	for start < end && (b[start] == ' ' || b[start] == '\t' || b[start] == '\r' || b[start] == '\n') {
		start++
	}
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\r' || b[end-1] == '\n') {
		end--
	}
	return b[start:end]
}

func encodeCursorProjectDirName(cwd string) string {
	cwd = filepath.Clean(strings.TrimSpace(cwd))
	if cwd == "" || cwd == "." {
		return ""
	}
	return strings.Trim(strings.ReplaceAll(cwd, string(filepath.Separator), "-"), "-")
}

func cursorTranscriptCWDCandidates(cwd string) []string {
	cwd = filepath.Clean(strings.TrimSpace(cwd))
	if cwd == "" {
		return nil
	}
	out := []string{cwd}
	// Include parent when cwd is a nested worktree-style path; keep small.
	parent := filepath.Dir(cwd)
	if parent != cwd && parent != "." && parent != string(filepath.Separator) {
		out = append(out, parent)
	}
	return out
}

func cursorTranscriptFresh(updated, now time.Time) bool {
	if updated.IsZero() || now.IsZero() {
		return true
	}
	if updated.After(now.Add(10 * time.Minute)) {
		return true
	}
	return now.Sub(updated) <= cursorTranscriptMaxAge
}

func cursorResumeSessionIDFromCommand(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return ""
	}
	base := strings.ToLower(filepathBase(fields[0]))
	if base != "cursor-agent" && base != "agent" {
		return ""
	}
	for index := 1; index < len(fields); index++ {
		field := strings.Trim(fields[index], `"'`)
		switch {
		case field == "--resume":
			if index+1 < len(fields) {
				sessionID := strings.Trim(fields[index+1], `"'`)
				if sessionID != "" && !strings.HasPrefix(sessionID, "-") {
					return sessionID
				}
			}
		case strings.HasPrefix(field, "--resume="):
			return strings.Trim(strings.TrimPrefix(field, "--resume="), `"'`)
		}
	}
	return ""
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
