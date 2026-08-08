package work

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

const (
	opencodeConversationSource = "opencode_db"
	maxOpenCodeConversationAge = 72 * time.Hour

	// openCodeCacheMaxSessions bounds the process-wide parsed-conversation
	// cache so many bindings cannot accumulate unbounded row text.
	openCodeCacheMaxSessions = 8
)

type openCodeSessionCandidate struct {
	ID        string
	CWD       string
	ParentID  string
	CreatedAt time.Time
	Updated   time.Time
}

func (r *ProviderConversationReader) loadOpenCodeConversationForAgent(agent classifier.Agent, now time.Time) (CodexConversation, error) {
	if strings.TrimSpace(agent.Cwd) == "" {
		r.resetSource()
		return CodexConversation{
			Available: false,
			Reason:    "missing_cwd",
			Events:    []CodexConversationEvent{},
		}, nil
	}

	dbPath, err := openCodeDBPath()
	if err != nil || dbPath == "" {
		r.resetSource()
		return CodexConversation{
			Available: false,
			Reason:    "db_unavailable",
			Events:    []CodexConversationEvent{},
		}, nil
	}

	// Stamp-first fast path: an unchanged DB stamp means no session row moved,
	// so the pinned session and the cached conversation are both still valid.
	// This poll performs zero sqlite3 spawns and zero full-history work.
	if owned := strings.TrimSpace(r.openCodeOwnedSessionID); owned != "" {
		conversation, version, changedIDs, stale, err := openCodeConversationCache.read(dbPath, owned)
		if err != nil {
			r.resetSource()
			return CodexConversation{}, err
		}
		if !stale {
			r.openCodeLastVersion = version
			r.openCodeLastChangedIDs = changedIDs
			return r.openCodeConversationResult(conversation, r.openCodeOwnedCandidate, dbPath), nil
		}
	}

	candidate, ok, err := r.findOpenCodeSession(agent, now)
	if err != nil {
		r.resetSource()
		return CodexConversation{}, err
	}
	if !ok {
		r.resetSource()
		return CodexConversation{
			Available: false,
			Reason:    "session_not_found",
			Events:    []CodexConversationEvent{},
		}, nil
	}

	conversation, version, changedIDs, err := openCodeConversationCache.load(dbPath, candidate.ID)
	if err != nil {
		r.resetSource()
		return CodexConversation{}, err
	}
	r.openCodeOwnedSessionID = candidate.ID
	r.openCodeOwnedCandidate = candidate
	r.openCodeLastVersion = version
	r.openCodeLastChangedIDs = changedIDs
	return r.openCodeConversationResult(conversation, candidate, dbPath), nil
}

func (r *ProviderConversationReader) openCodeConversationResult(conversation CodexConversation, candidate openCodeSessionCandidate, dbPath string) CodexConversation {
	conversation.Available = true
	conversation.Source = opencodeConversationSource
	conversation.Path = dbPath
	conversation.SessionID = firstNonEmpty(conversation.SessionID, candidate.ID)
	conversation.CWD = firstNonEmpty(conversation.CWD, candidate.CWD)
	conversation.Updated = &candidate.Updated
	if conversation.Events == nil {
		conversation.Events = []CodexConversationEvent{}
	}
	return conversation
}

func (r *ProviderConversationReader) findOpenCodeSession(agent classifier.Agent, now time.Time) (openCodeSessionCandidate, bool, error) {
	if owned := strings.TrimSpace(r.openCodeOwnedSessionID); owned != "" {
		if candidate, ok := r.revalidateOpenCodeOwnedSession(owned, agent.Cwd); ok {
			return candidate, true, nil
		}
		r.openCodeOwnedSessionID = ""
		r.openCodeOwnedCandidate = openCodeSessionCandidate{}
	}
	if owned := OpenCodeOwnedSessionID(agent.Command); owned != "" {
		if candidate, ok := r.revalidateOpenCodeOwnedSession(owned, agent.Cwd); ok {
			r.openCodeOwnedSessionID = candidate.ID
			return candidate, true, nil
		}
		return openCodeSessionCandidate{}, false, nil
	}

	dbPath, err := openCodeDBPath()
	if err != nil || dbPath == "" {
		return openCodeSessionCandidate{}, false, err
	}
	candidates, err := queryOpenCodeSessions(dbPath, agent.Cwd)
	if err != nil {
		return openCodeSessionCandidate{}, false, err
	}
	// Child sessions (session.parent_id set) are Task/sub-agent activity, not
	// the Zen agent's own transcript thread. Only root sessions can bind.
	roots := rootOpenCodeCandidates(candidates)
	fresh := freshOpenCodeSessionCandidates(roots, now)
	if len(openCodeWindowCandidates(fresh, agent.StartedAt)) > 0 {
		// A StartedAt window exists: unique min-delta bind, else refuse.
		if matched, ok := matchOpenCodeSessionToAgentStart(fresh, agent.StartedAt); ok {
			r.openCodeOwnedSessionID = matched.ID
			return matched, true, nil
		}
		return openCodeSessionCandidate{}, false, nil
	}
	// No window candidate (startedAt zero or drifted): bind the freshest root
	// so a live session is never reported as session_not_found and the
	// Interface does not collapse an active transcript to Working-only.
	if matched, ok := freshestOpenCodeRoot(fresh); ok {
		r.openCodeOwnedSessionID = matched.ID
		return matched, true, nil
	}
	return openCodeSessionCandidate{}, false, nil
}

func (r *ProviderConversationReader) revalidateOpenCodeOwnedSession(sessionID, agentCWD string) (openCodeSessionCandidate, bool) {
	dbPath, err := openCodeDBPath()
	if err != nil || dbPath == "" {
		return openCodeSessionCandidate{}, false
	}
	candidate, ok, err := queryOpenCodeSessionByID(dbPath, sessionID)
	if err != nil || !ok {
		return openCodeSessionCandidate{}, false
	}
	// A child (subtask) session must never anchor a Zen agent transcript even
	// when previously pinned by an older reader binding.
	if strings.TrimSpace(candidate.ParentID) != "" {
		return openCodeSessionCandidate{}, false
	}
	if !openCodeDirectoryMatches(candidate.CWD, agentCWD) {
		return openCodeSessionCandidate{}, false
	}
	return candidate, true
}

// ---------------------------------------------------------------------------
// Process-wide parsed-conversation cache.
//
// The cache is the durable owner of OpenCode's parsed structured data for a
// (dbPath, sessionID) pair. Readers share it so a reconnect or a second
// device never re-reads or re-parses the full history, and so every poll that
// finds the SQLite stamp unchanged costs zero spawns and zero full-history
// work. Rows are fetched incrementally with a time_updated cursor; unchanged
// rows are never re-parsed; the conversation is rebuilt only when fetched
// rows actually differ. A content version (and the event ids it changed) is
// exposed so the server can skip serialization work and compute cheap deltas.
// ---------------------------------------------------------------------------

type openCodeStamp struct {
	size       int64
	modTime    time.Time
	walSize    int64
	walModTime time.Time
	fileInfo   os.FileInfo
	walFile    os.FileInfo
}

func openCodeConversationStamp(dbPath string) (openCodeStamp, error) {
	info, err := os.Stat(dbPath)
	if err != nil {
		return openCodeStamp{}, err
	}
	stamp := openCodeStamp{size: info.Size(), modTime: info.ModTime(), fileInfo: info}
	// SQLite WAL mode keeps new rows in the -wal file; the main file's stat
	// changes only at checkpoint. Without the WAL in the stamp the cache
	// returns a stale conversation (the previous completed Activity) while a
	// newly accepted turn is already in the WAL.
	if wal, err := os.Stat(dbPath + "-wal"); err == nil {
		stamp.walSize = wal.Size()
		stamp.walModTime = wal.ModTime()
		stamp.walFile = wal
	}
	return stamp, nil
}

func (s openCodeStamp) equal(other openCodeStamp) bool {
	return s.size == other.size &&
		s.modTime.Equal(other.modTime) &&
		s.walSize == other.walSize &&
		s.walModTime.Equal(other.walModTime) &&
		sameProviderSourceFile(s.fileInfo, other.fileInfo) &&
		sameProviderSourceFile(s.walFile, other.walFile)
}

// openCodeMessageRow is the subset of the OpenCode message table the
// conversation projection consumes.
type openCodeMessageRow struct {
	ID          string
	TimeCreated int64
	TimeUpdated int64
	Data        string
}

// openCodePartRow is the subset of the OpenCode part table the conversation
// projection consumes.
type openCodePartRow struct {
	ID          string
	MessageID   string
	TimeCreated int64
	TimeUpdated int64
	Data        string
}

// openCodeMessagePayload is message.Data after parse; cached per row so an
// unchanged row is never re-parsed across polls.
type openCodeMessagePayload struct {
	Role string
}

// openCodePartPayload is part.Data after parse; cached per row so unchanged
// rows are never re-parsed across polls.
type openCodePartPayload struct {
	Type        string
	Text        string
	Tool        string
	CallID      string
	Reason      string
	Prompt      string
	Description string
	Agent       string
	Command     string
	Status      string
	Input       string
	Output      string
	Error       string
}

func parseOpenCodeMessageData(data string) openCodeMessagePayload {
	var meta struct {
		Role string `json:"role"`
	}
	if json.Unmarshal([]byte(data), &meta) != nil {
		return openCodeMessagePayload{}
	}
	return openCodeMessagePayload{Role: strings.ToLower(strings.TrimSpace(meta.Role))}
}

func parseOpenCodePartData(data string) openCodePartPayload {
	var payload struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Tool        string `json:"tool"`
		CallID      string `json:"callID"`
		Reason      string `json:"reason"`
		Prompt      string `json:"prompt"`
		Description string `json:"description"`
		Agent       string `json:"agent"`
		Command     string `json:"command"`
		State       struct {
			Status string          `json:"status"`
			Input  json.RawMessage `json:"input"`
			Output string          `json:"output"`
			Error  string          `json:"error"`
		} `json:"state"`
	}
	if json.Unmarshal([]byte(data), &payload) != nil {
		return openCodePartPayload{}
	}
	return openCodePartPayload{
		Type:        strings.ToLower(strings.TrimSpace(payload.Type)),
		Text:        payload.Text,
		Tool:        payload.Tool,
		CallID:      payload.CallID,
		Reason:      payload.Reason,
		Prompt:      payload.Prompt,
		Description: payload.Description,
		Agent:       payload.Agent,
		Command:     payload.Command,
		Status:      strings.ToLower(strings.TrimSpace(payload.State.Status)),
		Input:       string(payload.State.Input),
		Output:      payload.State.Output,
		Error:       payload.State.Error,
	}
}

type openCodeCacheEntry struct {
	stamp openCodeStamp

	messageRows map[string]openCodeMessageRow
	partRows    map[string]openCodePartRow

	messagePayloads map[string]openCodeMessagePayload
	partPayloads    map[string]openCodePartPayload

	messageCursorMS int64
	partCursorMS    int64

	conversation CodexConversation
	version      int64

	// lastChangedIDs are the event ids affected by the last content-changing
	// load, used by the server for cheap memoized fingerprint deltas.
	lastChangedIDs []string

	// applyGeneration counts completed in-place mutations of this entry.
	// load() captures it under the lock before its SQLite fetches and returns
	// the cached result if it changed while fetching: a concurrent refresh
	// applied with a cursor >= ours, so its state contains every row we
	// fetched and serving it is lossless.
	applyGeneration uint64
}

// openCodeConversationCache is a process-wide bounded cache of parsed OpenCode
// conversations keyed by dbPath|sessionID. All methods are safe for concurrent
// readers; sqlite3 spawns happen outside the lock.
var openCodeConversationCache = newOpenCodeConversationCache()

// openCodeCacheRef is the unambiguous structured cache key. A delimiter-based
// composite key could collide when a dbPath or sessionID contains the
// delimiter; struct equality cannot.
type openCodeCacheRef struct {
	dbPath    string
	sessionID string
}

type openCodeConversationCacheImpl struct {
	mu         sync.Mutex
	entries    map[openCodeCacheRef]*openCodeCacheEntry
	entryOrder []openCodeCacheRef

	// versionSeq is the process-global monotonic content-version source.
	// Entry versions are drawn from it on every content-changing rebuild, so
	// an entry that is evicted, removed, and reloaded can never reuse a
	// version value the server already observed for a previous generation of
	// the same session — the server O(1) fast path can never suppress changed
	// content after a reload.
	versionSeq atomic.Uint64
}

func newOpenCodeConversationCache() *openCodeConversationCacheImpl {
	return &openCodeConversationCacheImpl{
		entries: map[openCodeCacheRef]*openCodeCacheEntry{},
	}
}

func (c *openCodeConversationCacheImpl) nextVersion() int64 {
	return int64(c.versionSeq.Add(1))
}

func openCodeCacheKey(dbPath, sessionID string) openCodeCacheRef {
	return openCodeCacheRef{dbPath: dbPath, sessionID: sessionID}
}

// read returns the cached conversation when the SQLite stamp is unchanged
// (stale=false). stale=true means the source changed and the caller must
// refresh; the cache entry remains usable for identity/pinning decisions.
func (c *openCodeConversationCacheImpl) read(dbPath, sessionID string) (CodexConversation, int64, []string, bool, error) {
	stamp, err := openCodeConversationStamp(dbPath)
	if err != nil {
		return CodexConversation{}, 0, nil, true, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[openCodeCacheKey(dbPath, sessionID)]
	if !ok {
		return CodexConversation{}, 0, nil, true, nil
	}
	if !entry.stamp.equal(stamp) {
		return CodexConversation{}, 0, nil, true, nil
	}
	return entry.conversation, entry.version, entry.lastChangedIDs, false, nil
}

// load refreshes the cache entry for a changed stamp using cursor-based
// incremental row fetches, then rebuilds the conversation only when rows
// actually changed. Returns the conversation, its content version, and the
// event ids changed by this load.
//
// All SQLite subprocess I/O happens OUTSIDE the process-wide cache lock; the
// lock is held only to snapshot state before the fetches and to revalidate
// and apply afterwards. The apply is guarded by the entry's applyGeneration:
// if another goroutine refreshed the entry while we fetched, its cursor was
// >= ours, so it already contains every row we fetched and returning the
// cached result is lossless.
//
// Every changed stamp also fetches the authoritative message/part row-ID
// sets. The cached key sets are checked against them on both sides of the
// fetch; any cached id missing from the authoritative set — a plain deletion
// or an equal-count delete+insert replacement — triggers a full cursor-zero
// read that atomically replaces the row/payload maps. Additions need no full
// read: the incremental cursor fetch already brings new rows.
func (c *openCodeConversationCacheImpl) load(dbPath, sessionID string) (CodexConversation, int64, []string, error) {
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		return CodexConversation{}, 0, nil, fmt.Errorf("sqlite3 required for opencode conversation")
	}
	key := openCodeCacheKey(dbPath, sessionID)

	for attempt := 0; attempt < 4; attempt++ {
		stamp, err := openCodeConversationStamp(dbPath)
		if err != nil {
			c.remove(key)
			return CodexConversation{}, 0, nil, err
		}
		c.mu.Lock()
		entry, ok := c.entries[key]
		if !ok {
			entry = &openCodeCacheEntry{
				messageRows:     map[string]openCodeMessageRow{},
				partRows:        map[string]openCodePartRow{},
				messagePayloads: map[string]openCodeMessagePayload{},
				partPayloads:    map[string]openCodePartPayload{},
			}
			c.entries[key] = entry
			c.entryOrder = append(c.entryOrder, key)
			c.evictIfNeeded()
		}
		if entry.stamp.equal(stamp) {
			// Unchanged source: a new subscription to an already-cached
			// session costs zero spawns and zero history work.
			conversation := entry.conversation
			version := entry.version
			changedIDs := entry.lastChangedIDs
			c.mu.Unlock()
			return conversation, version, changedIDs, nil
		}
		generation := entry.applyGeneration
		messageCursor := entry.messageCursorMS
		partCursor := entry.partCursorMS
		cachedMessageIDs := make(map[string]struct{}, len(entry.messageRows))
		for id := range entry.messageRows {
			cachedMessageIDs[id] = struct{}{}
		}
		cachedPartIDs := make(map[string]struct{}, len(entry.partRows))
		for id := range entry.partRows {
			cachedPartIDs[id] = struct{}{}
		}
		c.mu.Unlock()

		// ---- All SQLite subprocess I/O happens outside the cache lock. ----
		newMessages, err := queryOpenCodeMessagesSince(sqlite3, dbPath, sessionID, messageCursor)
		if err != nil {
			return CodexConversation{}, 0, nil, err
		}
		newParts, err := queryOpenCodePartsSince(sqlite3, dbPath, sessionID, partCursor)
		if err != nil {
			return CodexConversation{}, 0, nil, err
		}
		authoritativeMessageIDs, authoritativePartIDs, err := queryOpenCodeSessionRowIDs(sqlite3, dbPath, sessionID)
		if err != nil {
			return CodexConversation{}, 0, nil, err
		}

		// Deletion signal: a cached id absent from the authoritative set
		// cannot be seen by the time_updated cursor, so the maps must be
		// replaced from a full cursor-zero read. Additions never trigger this
		// (the incremental fetch brings them), so a busy streaming writer
		// does not force full reads per poll.
		var fullMessages []openCodeMessageRow
		var fullParts []openCodePartRow
		if openCodeCachedIDsMissing(cachedMessageIDs, authoritativeMessageIDs) ||
			openCodeCachedIDsMissing(cachedPartIDs, authoritativePartIDs) {
			fullMessages, err = queryOpenCodeMessagesSince(sqlite3, dbPath, sessionID, 0)
			if err != nil {
				return CodexConversation{}, 0, nil, err
			}
			fullParts, err = queryOpenCodePartsSince(sqlite3, dbPath, sessionID, 0)
			if err != nil {
				return CodexConversation{}, 0, nil, err
			}
		}

		after, err := openCodeConversationStamp(dbPath)
		if err != nil {
			c.remove(key)
			return CodexConversation{}, 0, nil, err
		}
		if !stamp.equal(after) {
			// The source moved while we fetched (an active OpenCode writer).
			// Retry against the new stamp; on the last attempt apply the
			// fetched rows anyway — the >= cursor makes the next poll
			// self-heal, so a busy writer can never wedge the reader.
			if attempt < 3 {
				continue
			}
			after = stamp
		}

		// ---- Apply under the lock; SQLite work is already done. ----
		c.mu.Lock()
		if entry.applyGeneration != generation {
			// A concurrent refresh applied while we fetched; its cursor was
			// >= ours, so it contains every row we fetched — serving the
			// cached result is lossless.
			conversation := entry.conversation
			version := entry.version
			changedIDs := entry.lastChangedIDs
			c.mu.Unlock()
			return conversation, version, changedIDs, nil
		}
		currentMessageIDs := make(map[string]struct{}, len(entry.messageRows))
		for id := range entry.messageRows {
			currentMessageIDs[id] = struct{}{}
		}
		currentPartIDs := make(map[string]struct{}, len(entry.partRows))
		for id := range entry.partRows {
			currentPartIDs[id] = struct{}{}
		}

		changedRows := false
		fullRead := false
		if fullMessages != nil &&
			(openCodeCachedIDsMissing(currentMessageIDs, authoritativeMessageIDs) ||
				openCodeCachedIDsMissing(currentPartIDs, authoritativePartIDs)) {
			// Full replacement (memory only under the lock): the row and
			// payload maps are atomically rebuilt from the cursor-zero read.
			entry.messageRows = make(map[string]openCodeMessageRow, len(fullMessages))
			entry.partRows = make(map[string]openCodePartRow, len(fullParts))
			entry.messagePayloads = make(map[string]openCodeMessagePayload, len(fullMessages))
			entry.partPayloads = make(map[string]openCodePartPayload, len(fullParts))
			messageCursor = applyOpenCodeMessageFetches(entry, fullMessages, &changedRows)
			partCursor = applyOpenCodePartFetches(entry, fullParts, &changedRows)
			// The replacement is a change even when it applied zero rows: the
			// conversation must be rebuilt without the deleted events.
			fullRead = true
			changedRows = true
		} else {
			messageCursor = applyOpenCodeMessageFetches(entry, newMessages, &changedRows)
			partCursor = applyOpenCodePartFetches(entry, newParts, &changedRows)
		}

		if !fullRead && !changedRows &&
			!openCodeCachedIDsMissing(currentMessageIDs, authoritativeMessageIDs) &&
			!openCodeCachedIDsMissing(currentPartIDs, authoritativePartIDs) {
			// Content is identical: keep the conversation and version, but
			// advance cursors/stamp so the next read is fast again.
			entry.stamp = after
			entry.messageCursorMS = messageCursor
			entry.partCursorMS = partCursor
			entry.applyGeneration++
			conversation := entry.conversation
			version := entry.version
			changedIDs := entry.lastChangedIDs
			c.mu.Unlock()
			return conversation, version, changedIDs, nil
		}

		// Row content changed or rows were added/removed: rebuild. The
		// version is drawn from the process-global monotonic source so it
		// never repeats across entry eviction/removal/reload.
		conversation, changedEventIDs := rebuildOpenCodeConversation(entry, sessionID)
		entry.conversation = conversation
		entry.version = c.nextVersion()
		entry.lastChangedIDs = changedEventIDs
		entry.stamp = after
		entry.messageCursorMS = messageCursor
		entry.partCursorMS = partCursor
		entry.applyGeneration++
		version := entry.version
		c.mu.Unlock()
		return conversation, version, changedEventIDs, nil
	}
	return CodexConversation{}, 0, nil, fmt.Errorf("opencode conversation source changed continuously during refresh")
}

func (c *openCodeConversationCacheImpl) remove(key openCodeCacheRef) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[key]; ok {
		delete(c.entries, key)
		for index, existing := range c.entryOrder {
			if existing == key {
				c.entryOrder = append(c.entryOrder[:index], c.entryOrder[index+1:]...)
				break
			}
		}
	}
}

// evict bounds the cache so long-lived daemons cannot accumulate parsed row
// text for unbounded session bindings. Called after a load beyond the cap.
func (c *openCodeConversationCacheImpl) evictIfNeeded() {
	if len(c.entries) <= openCodeCacheMaxSessions {
		return
	}
	for len(c.entries) > openCodeCacheMaxSessions && len(c.entryOrder) > 0 {
		key := c.entryOrder[0]
		c.entryOrder = c.entryOrder[1:]
		delete(c.entries, key)
	}
}

func applyOpenCodeMessageFetches(entry *openCodeCacheEntry, rows []openCodeMessageRow, changed *bool) int64 {
	cursor := entry.messageCursorMS
	for _, row := range rows {
		if existing, ok := entry.messageRows[row.ID]; ok && existing.Data == row.Data && existing.TimeUpdated == row.TimeUpdated {
			if row.TimeUpdated > cursor {
				cursor = row.TimeUpdated
			}
			continue
		}
		*changed = true
		entry.messageRows[row.ID] = row
		entry.messagePayloads[row.ID] = parseOpenCodeMessageData(row.Data)
		if row.TimeUpdated > cursor {
			cursor = row.TimeUpdated
		}
	}
	if len(rows) > 0 && rows[len(rows)-1].TimeUpdated > cursor {
		cursor = rows[len(rows)-1].TimeUpdated
	}
	return cursor
}

func applyOpenCodePartFetches(entry *openCodeCacheEntry, rows []openCodePartRow, changed *bool) int64 {
	cursor := entry.partCursorMS
	for _, row := range rows {
		if existing, ok := entry.partRows[row.ID]; ok && existing.Data == row.Data && existing.TimeUpdated == row.TimeUpdated {
			if row.TimeUpdated > cursor {
				cursor = row.TimeUpdated
			}
			continue
		}
		*changed = true
		entry.partRows[row.ID] = row
		entry.partPayloads[row.ID] = parseOpenCodePartData(row.Data)
		if row.TimeUpdated > cursor {
			cursor = row.TimeUpdated
		}
	}
	return cursor
}

// rebuildOpenCodeConversation reconstructs the conversation from the cached
// parsed rows. The returned ids are the event ids affected by this build:
// every event touched by a changed row, plus events whose state-dependent
// projection fields (seq, Partial, Status) moved because of a settle or
// ordering shift — those can change without their own rows changing, and the
// delta/fingerprint memoization must not miss them.
func rebuildOpenCodeConversation(entry *openCodeCacheEntry, sessionID string) (CodexConversation, []string) {
	messageIDs := make([]string, 0, len(entry.messageRows))
	for id := range entry.messageRows {
		messageIDs = append(messageIDs, id)
	}
	sort.Slice(messageIDs, func(i, j int) bool {
		left := entry.messageRows[messageIDs[i]]
		right := entry.messageRows[messageIDs[j]]
		if left.TimeCreated != right.TimeCreated {
			return left.TimeCreated < right.TimeCreated
		}
		return messageIDs[i] < messageIDs[j]
	})
	partIDsByMessage := map[string][]string{}
	for id, row := range entry.partRows {
		partIDsByMessage[row.MessageID] = append(partIDsByMessage[row.MessageID], id)
	}
	for messageID, ids := range partIDsByMessage {
		sort.Slice(ids, func(i, j int) bool {
			left := entry.partRows[ids[i]]
			right := entry.partRows[ids[j]]
			if left.TimeCreated != right.TimeCreated {
				return left.TimeCreated < right.TimeCreated
			}
			return ids[i] < ids[j]
		})
		partIDsByMessage[messageID] = ids
	}

	builder := newOpenCodeConversationBuilder(sessionID)
	changedEventIDs := map[string]struct{}{}
	for _, messageID := range messageIDs {
		message := entry.messageRows[messageID]
		payload := entry.messagePayloads[messageID]
		parts := make([]openCodePartRow, 0, len(partIDsByMessage[messageID]))
		for _, partID := range partIDsByMessage[messageID] {
			parts = append(parts, entry.partRows[partID])
		}
		affects := builder.consumeMessage(message, payload, parts, entry.partPayloads)
		for _, eventID := range affects {
			changedEventIDs[eventID] = struct{}{}
		}
	}
	conversation := builder.result()

	// State-dependent field diff against the previous build: settle transitions
	// clear Partial on every event and converge subtask Status without changing
	// any row other than the settling message, so those ids must be reported
	// for cheap memoized deltas even though their rows did not change.
	previousByID := map[string]CodexConversationEvent{}
	for _, event := range entry.conversation.Events {
		previousByID[event.ID] = event
	}
	for _, event := range conversation.Events {
		if previous, ok := previousByID[event.ID]; ok {
			if previous.Seq != event.Seq ||
				previous.Partial != event.Partial ||
				previous.Status != event.Status {
				changedEventIDs[event.ID] = struct{}{}
			}
		}
	}

	changedIDs := make([]string, 0, len(changedEventIDs))
	for id := range changedEventIDs {
		changedIDs = append(changedIDs, id)
	}
	return conversation, changedIDs
}

// ---------------------------------------------------------------------------
// SQLite queries (spawned sqlite3 CLI; read-only URI).
// ---------------------------------------------------------------------------

// openCodeDBPathResolved memoizes the resolved OpenCode SQLite path. The
// `opencode db path` CLI spawn is expensive (the opencode process startup can
// take hundreds of milliseconds), so it must never run per poll. The
// ZEN_OPENCODE_DB override is re-read every call (tests set it dynamically).
// openCodeDBPathResolved memoizes a successful OpenCode SQLite path
// resolution. The `opencode db path` CLI spawn is expensive (the opencode
// process startup can take hundreds of milliseconds), so it must never run
// per poll. A failed resolution is NOT cached: the discovery is retried on
// later calls so a transient failure (or a late-arriving opencode install)
// self-corrects. The ZEN_OPENCODE_DB override is re-read every call (tests
// set it dynamically).
var (
	openCodeDBPathMu       sync.Mutex
	openCodeDBPathResolved string
)

func openCodeDBPath() (string, error) {
	if override := strings.TrimSpace(os.Getenv("ZEN_OPENCODE_DB")); override != "" {
		return override, nil
	}
	openCodeDBPathMu.Lock()
	defer openCodeDBPathMu.Unlock()
	if openCodeDBPathResolved != "" {
		return openCodeDBPathResolved, nil
	}
	if sqlitePath, err := lookPathOpenCodeDB(); err == nil && sqlitePath != "" {
		openCodeDBPathResolved = sqlitePath
		return openCodeDBPathResolved, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", nil
	}
	fallback := filepath.Join(home, ".local", "share", "opencode", "opencode.db")
	if _, err := os.Stat(fallback); err == nil {
		openCodeDBPathResolved = fallback
		return openCodeDBPathResolved, nil
	}
	return "", nil
}

func lookPathOpenCodeDB() (string, error) {
	binary, err := exec.LookPath("opencode")
	if err != nil {
		return "", err
	}
	out, err := exec.Command(binary, "db", "path").CombinedOutput()
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", fmt.Errorf("empty opencode db path")
	}
	return path, nil
}

type openCodeSessionRow struct {
	ID          string `json:"id"`
	Directory   string `json:"directory"`
	ParentID    string `json:"parent_id"`
	TimeCreated int64  `json:"time_created"`
	TimeUpdated int64  `json:"time_updated"`
}

func queryOpenCodeSessions(dbPath, cwd string) ([]openCodeSessionCandidate, error) {
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		return nil, nil
	}
	var candidates []openCodeSessionCandidate
	seen := map[string]struct{}{}
	for _, candidateCWD := range transcriptCWDCandidates(cwd) {
		query := fmt.Sprintf(
			`SELECT id, directory, parent_id, time_created, time_updated FROM session WHERE directory = %s ORDER BY time_created DESC;`,
			sqliteStringLiteral(candidateCWD),
		)
		rows, err := queryOpenCodeSessionRows(sqlite3, dbPath, query)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if _, ok := seen[row.ID]; ok {
				continue
			}
			seen[row.ID] = struct{}{}
			candidates = append(candidates, openCodeSessionCandidate{
				ID:        row.ID,
				CWD:       row.Directory,
				ParentID:  strings.TrimSpace(row.ParentID),
				CreatedAt: time.UnixMilli(row.TimeCreated).UTC(),
				Updated:   time.UnixMilli(row.TimeUpdated).UTC(),
			})
		}
	}
	return candidates, nil
}

func queryOpenCodeSessionByID(dbPath, sessionID string) (openCodeSessionCandidate, bool, error) {
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		return openCodeSessionCandidate{}, false, nil
	}
	query := fmt.Sprintf(
		`SELECT id, directory, parent_id, time_created, time_updated FROM session WHERE id = %s LIMIT 1;`,
		sqliteStringLiteral(sessionID),
	)
	rows, err := queryOpenCodeSessionRows(sqlite3, dbPath, query)
	if err != nil || len(rows) == 0 {
		return openCodeSessionCandidate{}, false, err
	}
	row := rows[0]
	return openCodeSessionCandidate{
		ID:        row.ID,
		CWD:       row.Directory,
		ParentID:  strings.TrimSpace(row.ParentID),
		CreatedAt: time.UnixMilli(row.TimeCreated).UTC(),
		Updated:   time.UnixMilli(row.TimeUpdated).UTC(),
	}, true, nil
}

func queryOpenCodeSessionRows(sqlite3, dbPath, query string) ([]openCodeSessionRow, error) {
	uri := fmt.Sprintf("file:%s?mode=ro", dbPath)
	out, err := exec.Command(sqlite3, "-cmd", ".timeout 3000", "-json", uri, query).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("opencode db query: %w: %s", err, strings.TrimSpace(string(out)))
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "[]" {
		return nil, nil
	}
	var rows []openCodeSessionRow
	if err := json.Unmarshal([]byte(trimmed), &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func freshOpenCodeSessionCandidates(candidates []openCodeSessionCandidate, now time.Time) []openCodeSessionCandidate {
	if len(candidates) == 0 {
		return nil
	}
	fresh := make([]openCodeSessionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if isOpenCodeSessionFresh(candidate.Updated, now) {
			fresh = append(fresh, candidate)
		}
	}
	return fresh
}

func isOpenCodeSessionFresh(updated, now time.Time) bool {
	if updated.IsZero() || now.IsZero() {
		return true
	}
	if updated.After(now.Add(10 * time.Minute)) {
		return true
	}
	return now.Sub(updated) <= maxOpenCodeConversationAge
}

func rootOpenCodeCandidates(candidates []openCodeSessionCandidate) []openCodeSessionCandidate {
	if len(candidates) == 0 {
		return nil
	}
	roots := make([]openCodeSessionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.ParentID) == "" {
			roots = append(roots, candidate)
		}
	}
	return roots
}

func openCodeWindowCandidates(candidates []openCodeSessionCandidate, startedAt time.Time) []openCodeSessionCandidate {
	if startedAt.IsZero() {
		return nil
	}
	startedAt = startedAt.UTC()
	minCreatedAt := startedAt.Add(-5 * time.Second)
	maxCreatedAt := startedAt.Add(2 * time.Minute)
	out := make([]openCodeSessionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		createdAt := candidate.CreatedAt.UTC()
		if createdAt.IsZero() || createdAt.Before(minCreatedAt) || createdAt.After(maxCreatedAt) {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func matchOpenCodeSessionToAgentStart(candidates []openCodeSessionCandidate, startedAt time.Time) (openCodeSessionCandidate, bool) {
	window := openCodeWindowCandidates(candidates, startedAt)
	if len(window) == 0 {
		return openCodeSessionCandidate{}, false
	}
	bestIndex := 0
	bestDelta := time.Duration(0)
	for index, candidate := range window {
		delta := candidate.CreatedAt.UTC().Sub(startedAt.UTC())
		if delta < 0 {
			delta = -delta
		}
		if index == 0 || delta < bestDelta {
			bestIndex = index
			bestDelta = delta
		}
	}
	for index, candidate := range window {
		if index == bestIndex {
			continue
		}
		delta := candidate.CreatedAt.UTC().Sub(startedAt.UTC())
		if delta < 0 {
			delta = -delta
		}
		if delta == bestDelta {
			return openCodeSessionCandidate{}, false
		}
	}
	return window[bestIndex], true
}

// freshestOpenCodeRoot binds the most recently updated fresh root session
// when the StartedAt window is unavailable (zero or drifted). Equal-updated
// candidates refuse as ambiguous rather than guessing.
func freshestOpenCodeRoot(candidates []openCodeSessionCandidate) (openCodeSessionCandidate, bool) {
	if len(candidates) == 0 {
		return openCodeSessionCandidate{}, false
	}
	bestIndex := 0
	for index, candidate := range candidates {
		if index == 0 || candidate.Updated.After(candidates[bestIndex].Updated) {
			bestIndex = index
		}
	}
	for index, candidate := range candidates {
		if index == bestIndex {
			continue
		}
		if candidate.Updated.Equal(candidates[bestIndex].Updated) {
			return openCodeSessionCandidate{}, false
		}
	}
	return candidates[bestIndex], true
}

func openCodeDirectoryMatches(sessionDir, agentCWD string) bool {
	sessionDir = strings.TrimSpace(sessionDir)
	agentCWD = strings.TrimSpace(agentCWD)
	if sessionDir == "" || agentCWD == "" {
		return false
	}
	for _, candidate := range transcriptCWDCandidates(agentCWD) {
		if pathsEquivalent(sessionDir, candidate) || pathsEquivalent(sessionDir, agentCWD) {
			return true
		}
	}
	return false
}

// parseOpenCodeConversation is the full-read projection entry used by tests
// and the standalone real-DB timing probe. It bypasses the shared cache.
func parseOpenCodeConversation(dbPath, sessionID string) (CodexConversation, error) {
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		return CodexConversation{}, fmt.Errorf("sqlite3 required for opencode conversation")
	}
	messages, err := queryOpenCodeMessagesSince(sqlite3, dbPath, sessionID, 0)
	if err != nil {
		return CodexConversation{}, err
	}
	parts, err := queryOpenCodePartsSince(sqlite3, dbPath, sessionID, 0)
	if err != nil {
		return CodexConversation{}, err
	}
	entry := &openCodeCacheEntry{
		messageRows:     map[string]openCodeMessageRow{},
		partRows:        map[string]openCodePartRow{},
		messagePayloads: map[string]openCodeMessagePayload{},
		partPayloads:    map[string]openCodePartPayload{},
	}
	applyOpenCodeMessageFetches(entry, messages, new(bool))
	applyOpenCodePartFetches(entry, parts, new(bool))
	conversation, _ := rebuildOpenCodeConversation(entry, sessionID)
	conversation.Available = true
	conversation.Source = opencodeConversationSource
	conversation.Path = dbPath
	conversation.SessionID = sessionID
	return conversation, nil
}

// loadOpenCodeConversation loads the parsed conversation for a session
// through the shared cache. Retained for tests that exercise WAL
// invalidation directly.
func (r *ProviderConversationReader) loadOpenCodeConversation(dbPath, sessionID string) (CodexConversation, error) {
	conversation, _, _, err := openCodeConversationCache.load(dbPath, sessionID)
	if err != nil {
		r.resetSource()
		return CodexConversation{}, err
	}
	return conversation, nil
}

// openCodeDataQueryClauses is the pipe-delimited row projection used for the
// potentially large message/part tables. The sqlite3 CLI's -json serializer is
// pathologically slow on large string cells (a 700KB message data row takes
// ~11s in JSON mode vs milliseconds in plain mode), so rows are emitted as
// single-line records: fields pipe-separated with data last, backslash and
// newline escaped inside SQL. Each record maps to exactly one input line.
const (
	openCodeRowFieldSeparator = "|"
	// The row format is single-line records: fields pipe-separated with data
	// last, backslash and newline escaped inside SQL. SQLite string literals
	// do not process backslash escapes, so '\\\\' in the SQL text yields a
	// two-backslash replacement value and '\n' yields backslash-n.
	openCodeDataEscapeSQL = "replace(replace(data, char(92), '\\\\'), char(10), '\\n')"
	openCodePartEscapeSQL = "replace(replace(p.data, char(92), '\\\\'), char(10), '\\n')"
)

func parseOpenCodeEscapedData(value string) string {
	if !strings.Contains(value, "\\") {
		return value
	}
	// Single left-to-right decode: `\\` -> `\`, `\n` -> newline. A literal
	// backslash-n in the original data was escaped to `\\n`, which the scan
	// decodes back to `\n` without re-interpreting the n.
	var b strings.Builder
	b.Grow(len(value))
	for index := 0; index < len(value); index++ {
		if value[index] != '\\' || index+1 >= len(value) {
			b.WriteByte(value[index])
			continue
		}
		switch value[index+1] {
		case '\\':
			b.WriteByte('\\')
			index++
		case 'n':
			b.WriteByte('\n')
			index++
		default:
			b.WriteByte(value[index])
		}
	}
	return b.String()
}

func splitOpenCodeRowLine(line string, fieldCount int) []string {
	fields := make([]string, 0, fieldCount)
	rest := line
	for len(fields) < fieldCount-1 {
		index := strings.Index(rest, openCodeRowFieldSeparator)
		if index < 0 {
			return nil
		}
		fields = append(fields, rest[:index])
		rest = rest[index+1:]
	}
	fields = append(fields, rest)
	return fields
}

func queryOpenCodeMessagesSince(sqlite3, dbPath, sessionID string, cursorMS int64) ([]openCodeMessageRow, error) {
	query := fmt.Sprintf(
		`SELECT id, time_created, time_updated, %s FROM message WHERE session_id = %s AND time_updated >= %d ORDER BY time_created ASC, id ASC;`,
		openCodeDataEscapeSQL, sqliteStringLiteral(sessionID), cursorMS,
	)
	lines, err := runOpenCodeDataQuery(sqlite3, dbPath, query)
	if err != nil {
		return nil, err
	}
	rows := make([]openCodeMessageRow, 0, len(lines))
	for _, line := range lines {
		fields := splitOpenCodeRowLine(line, 4)
		if fields == nil {
			return nil, fmt.Errorf("opencode message row malformed")
		}
		timeCreated, timeUpdated, err := parseOpenCodeRowTimes(fields[1], fields[2])
		if err != nil {
			return nil, err
		}
		rows = append(rows, openCodeMessageRow{
			ID:          fields[0],
			TimeCreated: timeCreated,
			TimeUpdated: timeUpdated,
			Data:        parseOpenCodeEscapedData(fields[3]),
		})
	}
	return rows, nil
}

func queryOpenCodePartsSince(sqlite3, dbPath, sessionID string, cursorMS int64) ([]openCodePartRow, error) {
	query := fmt.Sprintf(
		`SELECT p.id, p.message_id, p.time_created, p.time_updated, %s FROM part p WHERE p.session_id = %s AND p.time_updated >= %d ORDER BY p.time_created ASC, p.id ASC;`,
		openCodePartEscapeSQL, sqliteStringLiteral(sessionID), cursorMS,
	)
	lines, err := runOpenCodeDataQuery(sqlite3, dbPath, query)
	if err != nil {
		return nil, err
	}
	rows := make([]openCodePartRow, 0, len(lines))
	for _, line := range lines {
		fields := splitOpenCodeRowLine(line, 5)
		if fields == nil {
			return nil, fmt.Errorf("opencode part row malformed")
		}
		timeCreated, timeUpdated, err := parseOpenCodeRowTimes(fields[2], fields[3])
		if err != nil {
			return nil, err
		}
		rows = append(rows, openCodePartRow{
			ID:          fields[0],
			MessageID:   fields[1],
			TimeCreated: timeCreated,
			TimeUpdated: timeUpdated,
			Data:        parseOpenCodeEscapedData(fields[4]),
		})
	}
	return rows, nil
}

func parseOpenCodeRowTimes(timeCreated, timeUpdated string) (int64, int64, error) {
	created, err := strconv.ParseInt(strings.TrimSpace(timeCreated), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("opencode row time_created: %w", err)
	}
	updated, err := strconv.ParseInt(strings.TrimSpace(timeUpdated), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("opencode row time_updated: %w", err)
	}
	return created, updated, nil
}

func runOpenCodeDataQuery(sqlite3, dbPath, query string) ([]string, error) {
	uri := fmt.Sprintf("file:%s?mode=ro", dbPath)
	out, err := exec.Command(sqlite3, "-cmd", ".timeout 3000", "-noheader", "-separator", openCodeRowFieldSeparator, uri, query).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("opencode data query: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if len(out) == 0 {
		return nil, nil
	}
	return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n"), nil
}

// queryOpenCodeSessionRowIDs returns the authoritative message and part id
// sets for a session in one spawn. 'm'/'p' discriminators are unconditionally
// prepended by SQL, so the first character of every returned line is exact
// regardless of the id's own prefix.
func queryOpenCodeSessionRowIDs(sqlite3, dbPath, sessionID string) (map[string]struct{}, map[string]struct{}, error) {
	query := fmt.Sprintf(
		`SELECT 'm'||id FROM message WHERE session_id = %s UNION ALL SELECT 'p'||id FROM part WHERE session_id = %s;`,
		sqliteStringLiteral(sessionID), sqliteStringLiteral(sessionID),
	)
	lines, err := runOpenCodeDataQuery(sqlite3, dbPath, query)
	if err != nil {
		return nil, nil, err
	}
	messages := make(map[string]struct{}, len(lines)/2)
	parts := make(map[string]struct{}, len(lines)/2)
	for _, line := range lines {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'm':
			messages[line[1:]] = struct{}{}
		case 'p':
			parts[line[1:]] = struct{}{}
		}
	}
	return messages, parts, nil
}

// openCodeCachedIDsMissing reports whether any cached row id is absent from
// the authoritative id set — the deletion signal that no time_updated cursor
// can ever observe (deleted rows keep their old time_updated, so plain drops
// and equal-count delete+insert replacements are indistinguishable by cursor
// or by count).
func openCodeCachedIDsMissing(cached, authoritative map[string]struct{}) bool {
	for id := range cached {
		if _, ok := authoritative[id]; !ok {
			return true
		}
	}
	return false
}

func sqliteStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// ---------------------------------------------------------------------------
// Conversation builder (parsed-payload driven).
// ---------------------------------------------------------------------------

type openCodeConversationBuilder struct {
	sessionID         string
	events            []CodexConversationEvent
	eventByCall       map[string]int
	subtaskIndexes    []int
	seq               int
	openSteps         int
	activityLifecycle providerActivityLifecycle
}

func newOpenCodeConversationBuilder(sessionID string) *openCodeConversationBuilder {
	return &openCodeConversationBuilder{
		sessionID:   strings.TrimSpace(sessionID),
		eventByCall: map[string]int{},
	}
}

// consumeMessage applies one message row with its parts to the builder. The
// returned ids are the event ids this message/part set touches (new or
// mutated), so the caller can report cheap delta scope.
func (b *openCodeConversationBuilder) consumeMessage(
	message openCodeMessageRow,
	payload openCodeMessagePayload,
	parts []openCodePartRow,
	partPayloads map[string]openCodePartPayload,
) []string {
	role := payload.Role
	timestamp := normalizeCodexTimestamp(time.UnixMilli(message.TimeCreated).UTC().Format(time.RFC3339Nano))
	switch role {
	case "user":
		exact := openCodeUserText(parts, partPayloads)
		if exact == "" {
			return nil
		}
		b.seq++
		b.events = append(b.events, CodexConversationEvent{
			ID:              firstNonEmpty(message.ID, fmt.Sprintf("%s:user:%d", b.sessionID, b.seq)),
			Seq:             b.seq,
			Timestamp:       timestamp,
			Kind:            "user_message",
			Role:            "user",
			Body:            exact,
			AdmissionSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(exact))),
		})
		if !b.activityLifecycle.running() {
			b.activityLifecycle.start(
				providerActivityID(b.sessionID, message.ID, b.seq),
				timestamp,
			)
		}
		return []string{firstNonEmpty(message.ID, fmt.Sprintf("%s:user:%d", b.sessionID, b.seq))}
	case "assistant":
		affected := b.projectAssistantParts(message.ID, timestamp, parts, partPayloads)
		b.settleFromAssistantMessage(message, timestamp)
		return affected
	default:
		affected := b.projectAssistantParts(message.ID, timestamp, parts, partPayloads)
		b.settleFromAssistantMessage(message, timestamp)
		return affected
	}
}

// settleFromAssistantMessage uses OpenCode's authoritative message finish /
// time.completed markers: the assistant message row is the only turn-terminal
// fact. step-finish parts are per-step signals and must never settle the
// Activity (the first assistant message echo finishes with tool-calls while
// the provider is only starting real work, and treating it as completion
// pinned live Sessions to done).
func (b *openCodeConversationBuilder) settleFromAssistantMessage(message openCodeMessageRow, timestamp string) {
	var meta struct {
		Finish string `json:"finish"`
		Time   struct {
			Completed int64 `json:"completed"`
		} `json:"time"`
	}
	if json.Unmarshal([]byte(message.Data), &meta) != nil {
		return
	}
	finish := strings.ToLower(strings.TrimSpace(meta.Finish))
	if openCodeFinishContinuesTurn(finish) {
		return
	}
	// A finish-less message and OpenCode's "unknown" terminal step share the
	// same requirement: both are unclassified by the finish lexicon alone, so
	// they settle only on OpenCode's authoritative time.completed boundary
	// with no tool call still in flight. Production OpenCode writes finish
	// "unknown" + time.completed on the last message of completed turns;
	// mid-write message rows carry neither.
	unclassified := finish == "" || finish == "unknown"
	if unclassified && meta.Time.Completed <= 0 {
		return
	}
	if unclassified && b.hasRunningTools() {
		return
	}
	status := ProviderActivityCompleted
	switch finish {
	case "error", "failed":
		status = ProviderActivityFailed
	case "abort", "aborted", "interrupted", "cancel", "canceled", "cancelled":
		status = ProviderActivityInterrupted
	}
	settleAt := timestamp
	if meta.Time.Completed > 0 {
		settleAt = normalizeCodexTimestamp(time.UnixMilli(meta.Time.Completed).UTC().Format(time.RFC3339Nano))
	}
	b.openSteps = 0
	b.activityLifecycle.settle("", status, settleAt)
}

// openCodeFinishContinuesTurn reports whether an OpenCode message finish means
// the assistant yielded to a tool or is otherwise mid-turn. Such messages must
// never settle the turn Activity. "unknown" is deliberately not listed here:
// OpenCode writes it as the terminal finish of completed turns (alongside the
// authoritative time.completed), and settleFromAssistantMessage still gates it
// behind a completed timestamp and no running tools.
func openCodeFinishContinuesTurn(finish string) bool {
	switch finish {
	case "tool-calls", "running", "pending":
		return true
	default:
		return false
	}
}

func (b *openCodeConversationBuilder) hasRunningTools() bool {
	for _, event := range b.events {
		if event.Kind != "tool_call" {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(event.Status))
		if status == "running" || status == "pending" || event.Partial {
			return true
		}
	}
	return false
}

func (b *openCodeConversationBuilder) projectAssistantParts(
	messageID, timestamp string,
	parts []openCodePartRow,
	partPayloads map[string]openCodePartPayload,
) []string {
	var affected []string
	for _, part := range parts {
		payload := partPayloads[part.ID]
		partTime := timestamp
		if part.TimeCreated > 0 {
			partTime = normalizeCodexTimestamp(time.UnixMilli(part.TimeCreated).UTC().Format(time.RFC3339Nano))
		}
		switch payload.Type {
		case "step-start":
			b.openSteps++
			if !b.activityLifecycle.running() {
				b.activityLifecycle.start(
					providerActivityID(b.sessionID, part.ID, b.seq+1),
					partTime,
				)
			}
		case "step-finish":
			// A step-finish part closes one assistant step, never the turn.
			// OpenCode writes one step per assistant message and mirrors the
			// step outcome onto the message row (finish/time.completed), so the
			// step sequence goes 1 -> 0 on every step-finish while the turn is
			// still Thinking/Preparing edit/Build. Settling the whole Activity
			// here pinned live turns to done after every tool-call step.
			if b.openSteps > 0 {
				b.openSteps--
			}
		case "text":
			text := strings.TrimSpace(payload.Text)
			if text == "" {
				continue
			}
			b.seq++
			eventID := firstNonEmpty(part.ID, messageID+"-text")
			b.events = append(b.events, CodexConversationEvent{
				ID:        eventID,
				Seq:       b.seq,
				Timestamp: partTime,
				Kind:      "assistant_message",
				Role:      "assistant",
				Body:      text,
				Partial:   b.openSteps > 0,
			})
			affected = append(affected, eventID)
		case "reasoning":
			text := strings.TrimSpace(payload.Text)
			if text == "" {
				continue
			}
			b.seq++
			eventID := firstNonEmpty(part.ID, messageID+"-reasoning")
			b.events = append(b.events, CodexConversationEvent{
				ID:        eventID,
				Seq:       b.seq,
				Timestamp: partTime,
				Kind:      "reasoning",
				Body:      text,
				Partial:   b.openSteps > 0,
				Transient: true,
			})
			affected = append(affected, eventID)
		case "tool":
			b.seq++
			callID := firstNonEmpty(payload.CallID, part.ID)
			status := strings.ToLower(strings.TrimSpace(payload.Status))
			switch status {
			case "error":
				status = "failed"
			case "":
				status = "running"
			}
			input := strings.TrimSpace(payload.Input)
			output := strings.TrimSpace(payload.Output)
			if output == "" {
				// Upstream ToolStateError carries the failure text in state.error
				// (no state.output), so a failed card must not collapse to empty.
				output = strings.TrimSpace(payload.Error)
			}
			eventID := firstNonEmpty(part.ID, fmt.Sprintf("%s:tool:%d", b.sessionID, b.seq))
			event := CodexConversationEvent{
				ID:        eventID,
				Seq:       b.seq,
				Timestamp: partTime,
				Kind:      "tool_call",
				ToolName:  strings.TrimSpace(payload.Tool),
				CallID:    callID,
				Input:     input,
				Output:    output,
				Status:    status,
				Partial:   status == "running" || status == "pending",
			}
			if index, ok := b.eventByCall[callID]; ok && index >= 0 && index < len(b.events) {
				previousID := b.events[index].ID
				if previousID != eventID {
					// A new part replaced a same-call event: both ids move.
					affected = append(affected, previousID)
				}
				event.Seq = b.events[index].Seq
				b.events[index] = event
			} else {
				b.events = append(b.events, event)
				b.eventByCall[callID] = len(b.events) - 1
			}
			affected = append(affected, eventID)
			if !b.activityLifecycle.running() && (status == "running" || status == "pending") {
				b.activityLifecycle.start(
					providerActivityID(b.sessionID, callID, b.seq),
					partTime,
				)
			}
		case "subtask":
			// OpenCode's Task/sub-agent launch part. Project it as a tool call
			// node so child activity is represented in the Interface instead of
			// silently dropping the parent transcript's Task activity.
			b.seq++
			callID := firstNonEmpty(part.ID, fmt.Sprintf("%s:subtask:%d", b.sessionID, b.seq))
			input, _ := json.Marshal(map[string]string{
				"prompt":      payload.Prompt,
				"description": payload.Description,
				"agent":       payload.Agent,
				"command":     payload.Command,
			})
			b.events = append(b.events, CodexConversationEvent{
				ID:        callID,
				Seq:       b.seq,
				Timestamp: partTime,
				Kind:      "tool_call",
				ToolName:  "subtask",
				CallID:    callID,
				Input:     string(input),
				Status:    "running",
				Partial:   true,
			})
			b.subtaskIndexes = append(b.subtaskIndexes, len(b.events)-1)
			affected = append(affected, callID)
		case "file":
			// Optional file/ref parts are not projected into chat body yet.
		}
	}
	return affected
}

// resolveSubtaskStates converges subtask part events to their final state at
// result time: a settled turn marks child activity completed, while a still
// running turn keeps it in-flight.
func (b *openCodeConversationBuilder) resolveSubtaskStates() {
	settled := !b.activityLifecycle.running()
	for _, index := range b.subtaskIndexes {
		if index < 0 || index >= len(b.events) {
			continue
		}
		event := &b.events[index]
		if settled {
			event.Status = "completed"
			event.Partial = false
		}
	}
}

func (b *openCodeConversationBuilder) result() CodexConversation {
	b.resolveSubtaskStates()
	// A settled turn means the DB rows are the final state: partial-to-final
	// replacement clears in-flight flags so the Interface renders the turn as
	// finished instead of perpetually streaming.
	if !b.activityLifecycle.running() {
		for i := range b.events {
			b.events[i].Partial = false
		}
	}
	return conversationWithActivity(CodexConversation{
		SessionID: b.sessionID,
		Events:    b.events,
	}, &b.activityLifecycle)
}

func openCodeUserText(parts []openCodePartRow, partPayloads map[string]openCodePartPayload) string {
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		payload := partPayloads[part.ID]
		if payload.Type != "text" {
			continue
		}
		if text := payload.Text; text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "")
}
