package classifier

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultTranscriptMaxAge    = 72 * time.Hour
	defaultJSONLInitialTail    = 512 << 10 // 512KiB bounded first scan
	providerActivityStaleAfter = 20 * time.Minute
	pathMissBackoffMin         = 2 * time.Second
	pathMissBackoffMax         = 60 * time.Second
)

// JSONLTurnProbeStats are cumulative I/O counters for cost tests.
type JSONLTurnProbeStats struct {
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

// JSONLTurnState is mutated by provider line inspectors during cheap scans.
type JSONLTurnState struct {
	OpenOff  int64 // last open-turn marker byte offset; -1 if none
	CloseOff int64 // last close-turn marker byte offset; -1 if none
	Blocked  bool
	Failed   bool
}

// TurnProbeResult is the durable activity observation from a JSONL probe.
type TurnProbeResult struct {
	Active  bool
	Blocked bool
	Failed  bool
	OK      bool
}

// JSONLLineInspector inspects one JSONL line for turn markers.
type JSONLLineInspector func(line []byte, lineStart int64, state *JSONLTurnState)

// JSONLPathResolver locates the transcript/session file for an agent.
type JSONLPathResolver func(home string, agent Agent, now time.Time) (path string, ok bool)

// JSONLTurnProbe is a provider-neutral cheap JSONL activity probe.
// Warm polls are Stat-only; growth is scanned from the prior byte offset;
// first scans / truncations use a bounded tail when the file is large.
type JSONLTurnProbe struct {
	mu          sync.Mutex
	homeDir     string
	now         func() time.Time
	byAgent     map[string]*jsonlTurnEntry
	stats       JSONLTurnProbeStats
	maxAge      time.Duration
	staleAfter  time.Duration
	initialTail int64
	resolvePath JSONLPathResolver
	inspectLine JSONLLineInspector
}

type jsonlTurnEntry struct {
	agentKey      string
	cwd           string
	resumeID      string
	path          string
	size          int64
	modTime       time.Time
	scannedSize   int64
	state         JSONLTurnState
	haveMeta      bool
	pathMissUntil time.Time
	missStreak    int
}

// NewJSONLTurnProbe builds a probe. resolve/inspect are required.
func NewJSONLTurnProbe(resolve JSONLPathResolver, inspect JSONLLineInspector) *JSONLTurnProbe {
	return &JSONLTurnProbe{
		now:         time.Now,
		byAgent:     map[string]*jsonlTurnEntry{},
		maxAge:      defaultTranscriptMaxAge,
		staleAfter:  providerActivityStaleAfter,
		initialTail: defaultJSONLInitialTail,
		resolvePath: resolve,
		inspectLine: inspect,
	}
}

func (p *JSONLTurnProbe) SetHomeDir(home string) *JSONLTurnProbe {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.homeDir = strings.TrimSpace(home)
	return p
}

func (p *JSONLTurnProbe) SetNow(now func() time.Time) *JSONLTurnProbe {
	p.mu.Lock()
	defer p.mu.Unlock()
	if now != nil {
		p.now = now
	}
	return p
}

func (p *JSONLTurnProbe) SetStaleAfter(d time.Duration) *JSONLTurnProbe {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.staleAfter = d
	return p
}

func (p *JSONLTurnProbe) SetInitialTail(n int64) *JSONLTurnProbe {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.initialTail = n
	return p
}

func (p *JSONLTurnProbe) Stats() JSONLTurnProbeStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stats
}

func (p *JSONLTurnProbe) ResetStats() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats = JSONLTurnProbeStats{}
}

// Probe returns durable turn activity for agent. ok=false means no usable file.
func (p *JSONLTurnProbe) Probe(agent Agent) TurnProbeResult {
	if p == nil || p.resolvePath == nil || p.inspectLine == nil {
		return TurnProbeResult{}
	}
	cwd := strings.TrimSpace(agent.Cwd)
	if cwd == "" {
		return TurnProbeResult{}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	now := p.now()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	key := providerTranscriptAgentKey(agent)
	resumeID := resumeSessionIDFromCommand(agent.Command)
	entry := p.byAgent[key]
	if entry == nil || entry.cwd != cwd || entry.resumeID != resumeID {
		entry = &jsonlTurnEntry{
			agentKey: key,
			cwd:      cwd,
			resumeID: resumeID,
			state:    JSONLTurnState{OpenOff: -1, CloseOff: -1},
		}
		p.byAgent[key] = entry
	}

	if entry.path == "" {
		if !entry.pathMissUntil.IsZero() && now.Before(entry.pathMissUntil) {
			p.stats.PathMissCacheHits++
			return TurnProbeResult{}
		}
		path, ok := p.resolvePath(p.resolveHomeLocked(), agent, now)
		p.stats.PathResolveCalls++
		if !ok {
			p.markPathMissLocked(entry, now)
			return TurnProbeResult{}
		}
		entry.missStreak = 0
		entry.pathMissUntil = time.Time{}
		entry.path = path
		entry.scannedSize = 0
		entry.state = JSONLTurnState{OpenOff: -1, CloseOff: -1}
		entry.haveMeta = false
	}

	info, err := os.Stat(entry.path)
	p.stats.StatCalls++
	if err != nil {
		entry.path = ""
		if !entry.pathMissUntil.IsZero() && now.Before(entry.pathMissUntil) {
			p.stats.PathMissCacheHits++
			return TurnProbeResult{}
		}
		path, ok := p.resolvePath(p.resolveHomeLocked(), agent, now)
		p.stats.PathResolveCalls++
		if !ok {
			p.markPathMissLocked(entry, now)
			return TurnProbeResult{}
		}
		entry.missStreak = 0
		entry.pathMissUntil = time.Time{}
		entry.path = path
		entry.scannedSize = 0
		entry.state = JSONLTurnState{OpenOff: -1, CloseOff: -1}
		entry.haveMeta = false
		info, err = os.Stat(entry.path)
		p.stats.StatCalls++
		if err != nil {
			entry.path = ""
			p.markPathMissLocked(entry, now)
			return TurnProbeResult{}
		}
	}

	if p.maxAge > 0 && !info.ModTime().IsZero() && now.Sub(info.ModTime()) > p.maxAge {
		return TurnProbeResult{OK: true, Active: false}
	}

	if entry.haveMeta && entry.size == info.Size() && entry.modTime.Equal(info.ModTime()) {
		p.stats.CacheHits++
		return p.resultFromEntryLocked(entry, now)
	}

	if info.Size() < entry.scannedSize {
		entry.scannedSize = 0
		entry.state = JSONLTurnState{OpenOff: -1, CloseOff: -1}
		p.stats.FullScans++
	} else if entry.scannedSize == 0 {
		p.stats.FullScans++
	} else {
		p.stats.TailScans++
	}

	if err := p.scanEntryLocked(entry, info.Size()); err != nil {
		return TurnProbeResult{}
	}
	entry.size = info.Size()
	entry.modTime = info.ModTime()
	entry.haveMeta = true
	return p.resultFromEntryLocked(entry, now)
}

func (p *JSONLTurnProbe) markPathMissLocked(entry *jsonlTurnEntry, now time.Time) {
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

func (p *JSONLTurnProbe) resultFromEntryLocked(entry *jsonlTurnEntry, now time.Time) TurnProbeResult {
	active := transcriptOffsetsActive(entry.state.OpenOff, entry.state.CloseOff)
	if active && p.staleAfter > 0 && !entry.modTime.IsZero() && now.Sub(entry.modTime) > p.staleAfter {
		active = false
	}
	return TurnProbeResult{
		Active:  active,
		Blocked: entry.state.Blocked,
		Failed:  entry.state.Failed,
		OK:      true,
	}
}

func (p *JSONLTurnProbe) resolveHomeLocked() string {
	if p.homeDir != "" {
		return p.homeDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func (p *JSONLTurnProbe) scanEntryLocked(entry *jsonlTurnEntry, size int64) error {
	file, err := os.Open(entry.path)
	p.stats.OpenCalls++
	if err != nil {
		return err
	}
	defer file.Close()

	offset := entry.scannedSize
	skipPartial := false
	if offset == 0 && p.initialTail > 0 && size > p.initialTail {
		offset = size - p.initialTail
		skipPartial = true
	}
	if offset > 0 {
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return err
		}
	}

	reader := bufio.NewReader(file)
	p.stats.ReadCalls++
	first := true
	for {
		lineStart := offset
		line, err := reader.ReadBytes('\n')
		n := len(line)
		if n > 0 {
			p.stats.BytesRead += int64(n)
			offset += int64(n)
			if first && skipPartial {
				first = false
				// Discard potentially partial line after bounded seek.
			} else {
				first = false
				p.inspectLine(line, lineStart, &entry.state)
			}
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
		entry.scannedSize = offset
	}
	return nil
}

func providerTranscriptAgentKey(agent Agent) string {
	if id := strings.TrimSpace(agent.ID); id != "" {
		return id
	}
	return strings.TrimSpace(agent.Cwd) + "\x00" + strings.TrimSpace(agent.Command)
}

func resumeSessionIDFromCommand(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	for index := 1; index < len(fields); index++ {
		field := strings.Trim(fields[index], `"'`)
		lower := strings.ToLower(field)
		switch {
		case lower == "--resume" || lower == "-r" || lower == "resume":
			if index+1 < len(fields) {
				sessionID := strings.Trim(fields[index+1], `"'`)
				if sessionID != "" && !strings.HasPrefix(sessionID, "-") {
					return sessionID
				}
			}
		case strings.HasPrefix(lower, "--resume="):
			return strings.Trim(field[len("--resume="):], `"'`)
		}
	}
	return ""
}

func cwdPathCandidates(cwd string) []string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return nil
	}
	clean := filepath.Clean(cwd)
	if clean == "" || clean == "." {
		return nil
	}
	out := []string{clean}
	parent := filepath.Dir(clean)
	if parent != clean && parent != "." && parent != string(filepath.Separator) {
		out = append(out, parent)
	}
	return out
}

func transcriptFresh(updated, now time.Time, maxAge time.Duration) bool {
	if updated.IsZero() || now.IsZero() {
		return true
	}
	if updated.After(now.Add(10 * time.Minute)) {
		return true
	}
	return now.Sub(updated) <= maxAge
}
