package work

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

// Deterministic concurrent multi-reader plus writer exercise for the shared
// OpenCode cache. Run under -race: every stamp comparison and returned field
// must be read while the cache lock is held, and the writer must never wedge
// or corrupt a reader.
//
// Dedicated read-hammer goroutines call cache.read() in tight loops so their
// post-unlock field reads overlap the refresh writes of concurrent load()
// calls; a read() that unlocked before a load() acquired the lock would
// trigger the race detector on the stamp, conversation, version, or
// changed-id fields.
//
// The test also asserts the monotonic superset contract: with only appends,
// an event observed by any reader must remain observable by every later read,
// and content versions must never regress.
func TestOpenCodeCacheConcurrentReadersWithWriter(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 required")
	}
	fixture := buildOpenCodePerfFixture(t, openCodePerfFixtureOptions{
		turns:         40,
		textPartBytes: 2000,
		toolPartBytes: 4000,
	})
	dbPath := fixture.dbPath
	sessionID := fixture.sessionID
	directory := fixture.directory
	// WAL mode mirrors the real OpenCode database: concurrent read-only
	// connections and a single writer do not block each other (the default
	// rollback-journal mode would starve the writer under reader load).
	if out, err := exec.Command("sqlite3", dbPath, "PRAGMA journal_mode=WAL;").CombinedOutput(); err != nil {
		t.Fatalf("enable WAL: %v: %s", err, out)
	}
	t.Setenv("ZEN_OPENCODE_DB", dbPath)

	const writerBursts = 10
	const readHammerLoops = 20000
	const loadLoops = 40
	const readerLoops = 12

	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	maxEventCount := 0
	minVersion := int64(0)
	seenIDs := map[string]struct{}{}

	var nextID int
	nextRowID := func(prefix string) string {
		nextID++
		return fmt.Sprintf("%s_race_%d", prefix, nextID)
	}

	writer := func() {
		defer wg.Done()
		<-start
		// One appended turn per burst: a new user message and an assistant
		// message with a growing text part, exactly like a live session.
		base := time.Now().Add(-time.Minute)
		for burst := 0; burst < writerBursts; burst++ {
			now := base.Add(time.Duration(burst*5) * time.Second)
			msgUser := nextRowID("msg")
			msgAsst := nextRowID("msg")
			partUser := nextRowID("prt")
			partText := nextRowID("prt")
			partText2 := nextRowID("prt")
			stmt := fmt.Sprintf(
				"BEGIN;\n"+
					"INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES ('%s','%s',%d,%d,'{\"role\":\"user\"}');\n"+
					"INSERT INTO part(id, message_id, session_id, time_created, time_updated, data) VALUES ('%s','%s','%s',%d,%d,'{\"type\":\"text\",\"text\":\"u\"}');\n"+
					"INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES ('%s','%s',%d,%d,'{\"role\":\"assistant\"}');\n"+
					"INSERT INTO part(id, message_id, session_id, time_created, time_updated, data) VALUES ('%s','%s','%s',%d,%d,'{\"type\":\"text\",\"text\":\"a%d\"}');\n"+
					"INSERT INTO part(id, message_id, session_id, time_created, time_updated, data) VALUES ('%s','%s','%s',%d,%d,'{\"type\":\"text\",\"text\":\"a%d\"}');\n"+
					"COMMIT;\n",
				msgUser, sessionID, now.UnixMilli(), now.UnixMilli(),
				partUser, msgUser, sessionID, now.UnixMilli(), now.UnixMilli(),
				msgAsst, sessionID, now.UnixMilli(), now.UnixMilli(),
				partText, msgAsst, sessionID, now.UnixMilli(), now.UnixMilli(), burst,
				partText2, msgAsst, sessionID, now.UnixMilli(), now.UnixMilli(), burst,
			)
			cmd := exec.Command("sqlite3", "-cmd", ".timeout 5000", dbPath)
			cmd.Stdin = strings.NewReader(stmt)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("writer insert: %v: %s", err, out)
				return
			}
		}
	}

	readHammer := func() {
		defer wg.Done()
		<-start
		for loop := 0; loop < readHammerLoops; loop++ {
			if _, _, _, _, err := openCodeConversationCache.read(dbPath, sessionID); err != nil {
				t.Errorf("cache read: %v", err)
				return
			}
		}
	}

	loadHammer := func() {
		defer wg.Done()
		<-start
		for loop := 0; loop < loadLoops; loop++ {
			conversation, version, _, err := openCodeConversationCache.load(dbPath, sessionID)
			if err != nil {
				t.Errorf("cache load: %v", err)
				return
			}
			mu.Lock()
			if version < minVersion {
				mu.Unlock()
				t.Errorf("cache content version regressed: %d < %d", version, minVersion)
				return
			}
			minVersion = version
			for _, event := range conversation.Events {
				if _, ok := seenIDs[event.ID]; !ok {
					seenIDs[event.ID] = struct{}{}
				}
			}
			if len(conversation.Events) < maxEventCount {
				mu.Unlock()
				t.Errorf("event count regressed: %d < %d (a row vanished)", len(conversation.Events), maxEventCount)
				return
			}
			maxEventCount = len(conversation.Events)
			mu.Unlock()
		}
	}

	reader := func() {
		defer wg.Done()
		<-start
		reader := NewProviderConversationReader()
		agent := classifier.Agent{Cwd: directory, Command: "opencode"}
		// Keep discovery inside the fixture's declared 72-hour freshness
		// window. Wall-clock time would make this deterministic concurrency
		// test start returning session_not_found as the fixed fixture ages.
		readAt := fixture.startedAt.Add(24 * time.Hour)
		for loop := 0; loop < readerLoops; loop++ {
			conversation, err := reader.Load(agent, AgentProviderOpenCode, readAt)
			if err != nil {
				t.Errorf("reader load: %v", err)
				return
			}
			version := reader.ConversationVersion()
			mu.Lock()
			if version < minVersion {
				mu.Unlock()
				t.Errorf("content version regressed: %d < %d", version, minVersion)
				return
			}
			minVersion = version
			for _, event := range conversation.Events {
				if _, ok := seenIDs[event.ID]; !ok {
					seenIDs[event.ID] = struct{}{}
				}
			}
			if len(conversation.Events) < maxEventCount {
				mu.Unlock()
				t.Errorf("event count regressed: %d < %d (a row vanished)", len(conversation.Events), maxEventCount)
				return
			}
			maxEventCount = len(conversation.Events)
			mu.Unlock()
		}
	}

	wg.Add(6)
	go readHammer()
	go readHammer()
	go loadHammer()
	go loadHammer()
	go reader()
	go writer()
	close(start)
	wg.Wait()

	// After the writer finishes, one final authoritative load must observe
	// every event any reader ever saw (append-only monotonicity).
	final, err := parseOpenCodeConversation(dbPath, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	finalIDs := map[string]struct{}{}
	for _, event := range final.Events {
		finalIDs[event.ID] = struct{}{}
	}
	mu.Lock()
	defer mu.Unlock()
	for id := range seenIDs {
		if _, ok := finalIDs[id]; !ok {
			t.Fatalf("event %q vanished from the final authoritative read", id)
		}
	}
}
