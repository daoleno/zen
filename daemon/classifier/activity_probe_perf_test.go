package classifier

import (
	"testing"
	"time"
)

func TestJSONLTurnProbe_MissingPathResolveBackoff(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	clock := now
	probe := NewCodexTranscriptProbe().
		SetHomeDir(t.TempDir()).
		SetNow(func() time.Time { return clock })

	agent := Agent{ID: "miss:@1", Command: "codex", Cwd: "/no/such/project"}
	if res := probe.Probe(agent); res.OK {
		t.Fatalf("first miss should not be ok: %#v", res)
	}
	first := probe.Stats()
	if first.PathResolveCalls != 1 {
		t.Fatalf("first resolve calls = %d, want 1", first.PathResolveCalls)
	}

	probe.ResetStats()
	for i := 0; i < 100; i++ {
		clock = clock.Add(10 * time.Millisecond) // still within 2s backoff
		_ = probe.Probe(agent)
	}
	warm := probe.Stats()
	if warm.PathResolveCalls != 0 {
		t.Fatalf("warm miss resolve calls = %d, want 0 (negative cache)", warm.PathResolveCalls)
	}
	if warm.PathMissCacheHits < 100 {
		t.Fatalf("path miss cache hits = %d, want 100", warm.PathMissCacheHits)
	}

	// After backoff expires, exactly one more resolve is allowed.
	clock = clock.Add(3 * time.Second)
	probe.ResetStats()
	_ = probe.Probe(agent)
	after := probe.Stats()
	if after.PathResolveCalls != 1 {
		t.Fatalf("post-backoff resolve calls = %d, want 1", after.PathResolveCalls)
	}
}

func TestCursorTranscriptProbe_MissingPathResolveBackoff(t *testing.T) {
	probe := NewCursorTranscriptActiveProbe().SetHomeDir(t.TempDir())
	agent := Agent{ID: "c:@1", Command: "cursor-agent", Cwd: "/missing"}
	_, ok := probe.Active(agent)
	if ok {
		t.Fatal("expected miss")
	}
	first := probe.Stats().PathResolveCalls
	if first != 1 {
		t.Fatalf("first resolve = %d", first)
	}
	probe.ResetStats()
	for i := 0; i < 100; i++ {
		_, _ = probe.Active(agent)
	}
	if probe.Stats().PathResolveCalls != 0 {
		t.Fatalf("warm miss resolves = %d", probe.Stats().PathResolveCalls)
	}
	if probe.Stats().PathMissCacheHits < 100 {
		t.Fatalf("miss cache hits = %d", probe.Stats().PathMissCacheHits)
	}
}
