package work

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

// P1 regression: an equal-count delete+insert transaction (one row removed,
// one row added, counts unchanged) must still be detected. The authoritative
// row-ID set comparison is the only signal that can see it: the deleted id
// keeps its old time_updated, so neither the cursor nor the count changes.
func TestOpenCodeEqualCountDeleteInsertSwap(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 required")
	}
	started := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	dbPath := t.TempDir() + "/opencode.db"
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_swap", Directory: "/repo/swap", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(30 * time.Second).UnixMilli()},
	}, []openCodeMessageSeed{
		{ID: "msg_a", SessionID: "ses_swap", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"role":"user"}`},
		{ID: "msg_b", SessionID: "ses_swap", CreatedMS: started.Add(10 * time.Second).UnixMilli(), Data: `{"role":"user"}`},
	}, []openCodePartSeed{
		{ID: "prt_a", MessageID: "msg_a", SessionID: "ses_swap", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"type":"text","text":"alpha"}`},
		{ID: "prt_b", MessageID: "msg_b", SessionID: "ses_swap", CreatedMS: started.Add(10 * time.Second).UnixMilli(), Data: `{"type":"text","text":"beta"}`},
	})
	t.Setenv("ZEN_OPENCODE_DB", dbPath)
	reader := NewProviderConversationReader()
	agent := classifier.Agent{Cwd: "/repo/swap", Command: "opencode", StartedAt: started}
	first, err := reader.Load(agent, AgentProviderOpenCode, started.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if reader.ConversationVersion() == 0 {
		t.Fatal("no content version")
	}
	eventIDs := func(events []CodexConversationEvent) map[string]bool {
		ids := map[string]bool{}
		for _, event := range events {
			ids[event.ID] = true
		}
		return ids
	}
	before := eventIDs(first.Events)
	for _, want := range []string{"msg_a", "msg_b"} {
		if !before[want] {
			t.Fatalf("initial conversation missing %q: %v", want, before)
		}
	}

	// One transaction: delete msg_a + prt_a, insert msg_c + prt_c. The
	// message and part counts stay exactly equal.
	swap := fmt.Sprintf(
		"BEGIN;\n"+
			"DELETE FROM part WHERE id = 'prt_a';\n"+
			"DELETE FROM message WHERE id = 'msg_a';\n"+
			"INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES ('msg_c', 'ses_swap', %d, %d, '{\"role\":\"user\"}');\n"+
			"INSERT INTO part(id, message_id, session_id, time_created, time_updated, data) VALUES ('prt_c', 'msg_c', 'ses_swap', %d, %d, '{\"type\":\"text\",\"text\":\"gamma\"}');\n"+
			"COMMIT;\n",
		started.Add(20*time.Second).UnixMilli(), started.Add(20*time.Second).UnixMilli(),
		started.Add(20*time.Second).UnixMilli(), started.Add(20*time.Second).UnixMilli(),
	)
	cmd := exec.Command("sqlite3", dbPath)
	cmd.Stdin = strings.NewReader(swap)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("swap: %v: %s", err, out)
	}

	second, err := reader.Load(agent, AgentProviderOpenCode, started.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	after := eventIDs(second.Events)
	if after["msg_a"] {
		t.Fatalf("swapped-out message still visible: %v", after)
	}
	if !after["msg_c"] {
		t.Fatalf("swapped-in message missing: %v", after)
	}
	if !after["msg_b"] {
		t.Fatalf("untouched message vanished: %v", after)
	}
	if reader.ConversationVersion() == 0 {
		t.Fatal("version lost after swap")
	}

	// A fresh subscription must observe the swapped state too.
	fresh := NewProviderConversationReader()
	freshConversation, err := fresh.Load(agent, AgentProviderOpenCode, started.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if ids := eventIDs(freshConversation.Events); ids["msg_a"] || !ids["msg_c"] {
		t.Fatalf("fresh reader sees swapped state incorrectly: %v", ids)
	}
}

// P1 regression: SQLite subprocess I/O must never run while the process-wide
// cache lock is held. A locked database (SQLITE_BUSY with the configured busy
// timeout) must delay only the Session that reads it, never an unrelated
// Session on a different database.
func TestOpenCodeLockedDBSessionDoesNotBlockUnrelatedSession(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 required")
	}
	started := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	lockedDB := t.TempDir() + "/locked.db"
	freeDB := t.TempDir() + "/free.db"
	createOpenCodeFixtureDB(t, lockedDB, []openCodeSessionSeed{
		{ID: "ses_locked", Directory: "/repo/locked", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(30 * time.Second).UnixMilli()},
	}, []openCodeMessageSeed{
		{ID: "msg_1", SessionID: "ses_locked", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"role":"user"}`},
	}, []openCodePartSeed{
		{ID: "prt_1", MessageID: "msg_1", SessionID: "ses_locked", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"type":"text","text":"x"}`},
	})
	createOpenCodeFixtureDB(t, freeDB, []openCodeSessionSeed{
		{ID: "ses_free", Directory: "/repo/free", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(30 * time.Second).UnixMilli()},
	}, []openCodeMessageSeed{
		{ID: "msg_2", SessionID: "ses_free", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"role":"user"}`},
	}, []openCodePartSeed{
		{ID: "prt_2", MessageID: "msg_2", SessionID: "ses_free", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"type":"text","text":"y"}`},
	})

	// Hold an EXCLUSIVE write lock on the locked database for the whole test.
	locker := exec.Command("sqlite3", lockedDB)
	lockerStdin, err := locker.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := locker.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = locker.Process.Kill()
		_ = locker.Wait()
	}()
	if _, err := io.WriteString(lockerStdin, "BEGIN EXCLUSIVE;\n"); err != nil {
		t.Fatal(err)
	}
	// Let the exclusive lock actually take effect.
	time.Sleep(300 * time.Millisecond)

	var lockedResult error
	var lockedDone = make(chan struct{})
	go func() {
		_, _, _, err := openCodeConversationCache.load(lockedDB, "ses_locked")
		lockedResult = err
		close(lockedDone)
	}()

	// Give the locked load time to reach its blocked SQLite phase; the busy
	// timeout keeps it there for seconds. The free Session's load must not
	// wait on the process-wide lock behind it.
	time.Sleep(300 * time.Millisecond)
	startedFree := time.Now()
	conversation, _, _, err := openCodeConversationCache.load(freeDB, "ses_free")
	freeElapsed := time.Since(startedFree)
	if err != nil {
		t.Fatal(err)
	}
	if len(conversation.Events) != 1 {
		t.Fatalf("free session events = %d", len(conversation.Events))
	}
	// One SQLite busy timeout is 3s; if the free load had waited on the
	// process-wide lock behind the locked load it would take multiple
	// seconds. A one-second bound is generous for the lock-free path.
	if freeElapsed > time.Second {
		t.Fatalf("unrelated session blocked by locked database: free load took %v", freeElapsed)
	}

	// The locked load must eventually fail with a busy error once its timeout
	// expires, without affecting the cache.
	select {
	case <-lockedDone:
		if lockedResult == nil {
			t.Fatalf("locked load unexpectedly succeeded")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("locked load never timed out")
	}
}

// P1 regression: content versions must stay monotonic across entry
// removal/eviction and reload. The server fast path skips a poll when the
// reader version equals the previously sent snapshot's version; a reloaded
// entry reusing an old version value would suppress changed content.
func TestOpenCodeVersionMonotonicAcrossRemoveAndEvict(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 required")
	}
	started := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	makeFixture := func(t *testing.T, id, directory string, turn int64) string {
		t.Helper()
		dbPath := t.TempDir() + "/opencode.db"
		createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
			{ID: id, Directory: directory, CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(30 * time.Second).UnixMilli()},
		}, []openCodeMessageSeed{
			{ID: "msg_1", SessionID: id, CreatedMS: turn, Data: `{"role":"user"}`},
		}, []openCodePartSeed{
			{ID: "prt_1", MessageID: "msg_1", SessionID: id, CreatedMS: turn, Data: `{"type":"text","text":"v"}`},
		})
		return dbPath
	}

	// remove() then reload: the reloaded entry must not reuse the old version.
	removedDB := makeFixture(t, "ses_rem", "/repo/rem", started.Add(time.Second).UnixMilli())
	_, version1, _, err := openCodeConversationCache.load(removedDB, "ses_rem")
	if err != nil {
		t.Fatal(err)
	}
	openCodeConversationCache.remove(openCodeCacheKey(removedDB, "ses_rem"))
	_, version2, _, err := openCodeConversationCache.load(removedDB, "ses_rem")
	if err != nil {
		t.Fatal(err)
	}
	if version2 <= version1 {
		t.Fatalf("version reused across remove/reload: %d -> %d", version1, version2)
	}

	// Eviction: loading more sessions than the cache cap evicts the oldest
	// entry; reloading it must not reuse its pre-eviction version.
	const cap = openCodeCacheMaxSessions
	dbPaths := make([]string, 0, cap+1)
	sessionIDs := make([]string, 0, cap+1)
	versions := make([]int64, 0, cap+1)
	for index := 0; index <= cap; index++ {
		sessionID := fmt.Sprintf("ses_evict_%d", index)
		dbPath := makeFixture(t, sessionID, fmt.Sprintf("/repo/evict_%d", index), started.Add(time.Duration(index+2)*time.Second).UnixMilli())
		_, version, _, err := openCodeConversationCache.load(dbPath, sessionID)
		if err != nil {
			t.Fatal(err)
		}
		dbPaths = append(dbPaths, dbPath)
		sessionIDs = append(sessionIDs, sessionID)
		versions = append(versions, version)
	}
	// The first entry was evicted when the cap+1-th was created.
	_, reloadedVersion, _, err := openCodeConversationCache.load(dbPaths[0], sessionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if reloadedVersion <= versions[0] {
		t.Fatalf("version reused across eviction/reload: %d -> %d", versions[0], reloadedVersion)
	}
	// And every other observed version stays strictly unique.
	seen := map[int64]bool{}
	for _, version := range append(versions, reloadedVersion) {
		if seen[version] {
			t.Fatalf("content version repeated: %d", version)
		}
		seen[version] = true
	}

	// Unchanged polls must NOT bump the version (the fast path keeps it), so
	// the server O(1) skip stays effective between changes.
	_, sameVersion, _, err := openCodeConversationCache.load(dbPaths[0], sessionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if sameVersion != reloadedVersion {
		t.Fatalf("unchanged poll bumped the version: %d -> %d", reloadedVersion, sameVersion)
	}

	// A content change after reload must bump to a fresh value.
	update := fmt.Sprintf(
		"UPDATE part SET data = '{\"type\":\"text\",\"text\":\"v2\"}', time_updated = %d WHERE id = 'prt_1';\n",
		started.Add(40*time.Second).UnixMilli(),
	)
	cmd := exec.Command("sqlite3", dbPaths[0])
	cmd.Stdin = strings.NewReader(update)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("update: %v: %s", err, out)
	}
	_, changedVersion, _, err := openCodeConversationCache.load(dbPaths[0], sessionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if changedVersion <= reloadedVersion {
		t.Fatalf("change after reload did not bump the version: %d -> %d", reloadedVersion, changedVersion)
	}
}
