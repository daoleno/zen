package classifier

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCursorTranscriptActiveProbe_WarmPollIsStatOnly(t *testing.T) {
	home := t.TempDir()
	cwd := "/home/daoleno/workspace/zen"
	sessionID := "11111111-1111-1111-1111-111111111111"
	path := writeCursorTranscriptFixture(t, home, cwd, sessionID,
		`{"role":"user","message":{"content":[{"type":"text","text":"hi"}]}}`,
		`{"role":"assistant","message":{"content":[{"type":"text","text":"working"}]}}`,
	)

	probe := NewCursorTranscriptActiveProbe().SetHomeDir(home)
	agent := Agent{
		ID:      "win:@1",
		Command: "cursor-agent --force",
		Cwd:     cwd,
	}

	active, ok := probe.Active(agent)
	if !ok || !active {
		t.Fatalf("first Active = (%v, %v), want active", active, ok)
	}
	first := probe.Stats()
	if first.PathResolveCalls != 1 || first.OpenCalls != 1 || first.ReadCalls != 1 {
		t.Fatalf("first stats = %#v, want one resolve/open/read", first)
	}

	probe.ResetStats()
	for i := 0; i < 100; i++ {
		active, ok = probe.Active(agent)
		if !ok || !active {
			t.Fatalf("warm Active = (%v, %v)", active, ok)
		}
	}
	warm := probe.Stats()
	if warm.CacheHits != 100 {
		t.Fatalf("cache hits = %d, want 100", warm.CacheHits)
	}
	if warm.StatCalls != 100 {
		t.Fatalf("stat calls = %d, want 100", warm.StatCalls)
	}
	if warm.OpenCalls != 0 || warm.ReadCalls != 0 || warm.BytesRead != 0 || warm.PathResolveCalls != 0 {
		t.Fatalf("warm poll must be Stat-only, got %#v", warm)
	}

	// Touch path identity so later append test can reuse fixture layout.
	_ = path
}

func TestCursorTranscriptActiveProbe_AppendScansOnlyNewBytes(t *testing.T) {
	home := t.TempDir()
	cwd := "/repo/project"
	sessionID := "22222222-2222-2222-2222-222222222222"
	path := writeCursorTranscriptFixture(t, home, cwd, sessionID,
		`{"role":"user","message":{"content":[{"type":"text","text":"go"}]}}`,
		`{"role":"assistant","message":{"content":[{"type":"text","text":"on it"}]}}`,
	)

	probe := NewCursorTranscriptActiveProbe().SetHomeDir(home)
	agent := Agent{ID: "win:@2", Command: "cursor-agent", Cwd: cwd}

	active, ok := probe.Active(agent)
	if !ok || !active {
		t.Fatalf("initial Active = (%v, %v), want active", active, ok)
	}
	afterFirst := probe.Stats()

	appendLine := "{\"type\":\"turn_ended\",\"status\":\"success\"}\n"
	if err := appendFile(path, appendLine); err != nil {
		t.Fatalf("append: %v", err)
	}
	// Ensure mtime/size change is visible.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	_ = os.Chtimes(path, info.ModTime().Add(time.Second), info.ModTime().Add(time.Second))

	probe.ResetStats()
	active, ok = probe.Active(agent)
	if !ok || active {
		t.Fatalf("after turn_ended Active = (%v, %v), want inactive", active, ok)
	}
	afterAppend := probe.Stats()
	if afterAppend.PathResolveCalls != 0 {
		t.Fatalf("append path resolve = %d, want 0 (sticky path)", afterAppend.PathResolveCalls)
	}
	if afterAppend.TailScans != 1 || afterAppend.FullScans != 0 {
		t.Fatalf("append scan stats = %#v, want one tail scan", afterAppend)
	}
	if afterAppend.OpenCalls != 1 || afterAppend.ReadCalls != 1 {
		t.Fatalf("append open/read = %#v", afterAppend)
	}
	// Allow small slack over the appended line for reader buffering accounting.
	if afterAppend.BytesRead < int64(len(appendLine)) || afterAppend.BytesRead > int64(len(appendLine))+16 {
		t.Fatalf("bytes read = %d, want ~%d (tail only); first pass read %d", afterAppend.BytesRead, len(appendLine), afterFirst.BytesRead)
	}
}

func TestCursorTranscriptActiveProbe_IdleAfterTurnEnded(t *testing.T) {
	home := t.TempDir()
	cwd := "/repo"
	sessionID := "33333333-3333-3333-3333-333333333333"
	writeCursorTranscriptFixture(t, home, cwd, sessionID,
		`{"role":"user","message":{"content":[{"type":"text","text":"hi"}]}}`,
		`{"role":"assistant","message":{"content":[{"type":"text","text":"done"}]}}`,
		`{"type":"turn_ended","status":"success"}`,
	)
	probe := NewCursorTranscriptActiveProbe().SetHomeDir(home)
	active, ok := probe.Active(Agent{ID: "x", Command: "cursor-agent", Cwd: cwd})
	if !ok || active {
		t.Fatalf("Active = (%v, %v), want inactive", active, ok)
	}
}

func TestCursorTranscriptActiveProbe_IgnoresNonCursor(t *testing.T) {
	probe := NewCursorTranscriptActiveProbe()
	active, ok := probe.Active(Agent{Command: "zsh", Cwd: "/tmp"})
	if active || ok {
		t.Fatalf("Active = (%v, %v), want miss", active, ok)
	}
	if probe.Stats().PathResolveCalls != 0 || probe.Stats().StatCalls != 0 {
		t.Fatalf("non-cursor must not touch filesystem: %#v", probe.Stats())
	}
}

func writeCursorTranscriptFixture(t *testing.T, home, cwd, sessionID string, lines ...string) string {
	t.Helper()
	dir := filepath.Join(home, cursorTranscriptProjectPrefix, encodeCursorProjectDirName(cwd), cursorTranscriptDirName, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, sessionID+".jsonl")
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	now := time.Now().UTC()
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	return path
}

func appendFile(path, extra string) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(extra)
	return err
}
