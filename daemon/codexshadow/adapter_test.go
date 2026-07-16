package codexshadow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/daoleno/zen/daemon/chatthread"
)

const fixtureNativeActivityID = "019f68d5-6c34-71a3-9460-400759cbd8f9"
const fixtureSessionID = "019f68d5-4f3c-7820-bf46-f67b22f5f001"
const fixtureShadowDigest = "2c0aad053e7be40ea1daf055ef6b0e5a911598228d5c99b3eb1c283757fb1657"

type providerEffectTrap struct {
	store   *chatthread.ShadowStore
	effects *atomic.Int64
}

type countingFactSink struct {
	store         *chatthread.ShadowStore
	snapshotCalls atomic.Int64
	applyCalls    atomic.Int64
}

var _ FactSink = (*providerEffectTrap)(nil)
var _ FactSink = (*countingFactSink)(nil)
var _ chatthread.DispatchBoundary = (*providerEffectTrap)(nil)

func (trap *providerEffectTrap) ApplyShadowBatch(batch chatthread.ShadowBatch) (chatthread.ShadowSnapshot, error) {
	return trap.store.ApplyShadowBatch(batch)
}

func (trap *providerEffectTrap) ShadowSnapshot(threadID chatthread.ThreadID) (chatthread.ShadowSnapshot, error) {
	return trap.store.ShadowSnapshot(threadID)
}

func (sink *countingFactSink) ApplyShadowBatch(batch chatthread.ShadowBatch) (chatthread.ShadowSnapshot, error) {
	sink.applyCalls.Add(1)
	return sink.store.ApplyShadowBatch(batch)
}

func (sink *countingFactSink) ShadowSnapshot(threadID chatthread.ThreadID) (chatthread.ShadowSnapshot, error) {
	sink.snapshotCalls.Add(1)
	return sink.store.ShadowSnapshot(threadID)
}

func (trap *providerEffectTrap) Dispatch(context.Context, chatthread.ProviderDispatch) error {
	trap.effects.Add(1)
	return nil
}

// These methods are intentionally outside FactSink. They make the test trap
// sensitive to accidental future type assertions toward executor/tmux-style
// input capabilities as well as DispatchBoundary.
func (trap *providerEffectTrap) SendInput(string, string) error {
	trap.effects.Add(1)
	return nil
}

func (trap *providerEffectTrap) Input(string, string, string) error {
	trap.effects.Add(1)
	return nil
}

func TestOneTaskFiveInputShadowFixtureAndZeroSecondDispatch(t *testing.T) {
	root := t.TempDir()
	store, err := chatthread.InitializeShadowStore(filepath.Join(root, "shadow"))
	if err != nil {
		t.Fatalf("InitializeShadowStore: %v", err)
	}
	var providerEffects atomic.Int64
	trap := &providerEffectTrap{store: store, effects: &providerEffects}
	reader := mustNewReader(t, trap)
	rolloutPath := writeFixtureRollout(t, root, fixtureRolloutLines(t, "same body", "different body", "final body", true))

	// Legacy v1 remains the only dispatcher. The trap count starts at exactly the
	// five v1 effects and must not move during shadow read, restart, or replay.
	for index := 0; index < 5; index++ {
		if err := trap.Dispatch(context.Background(), chatthread.ProviderDispatch{}); err != nil {
			t.Fatalf("legacy effect %d: %v", index, err)
		}
	}
	if got := providerEffects.Load(); got != 5 {
		t.Fatalf("legacy provider effects = %d, want 5", got)
	}

	observation := fixtureObservation(rolloutPath)
	first, err := reader.ObserveRollout(context.Background(), observation)
	if err != nil {
		t.Fatalf("ObserveRollout: %v", err)
	}
	assertFiveInputSnapshot(t, first)
	rolloutInfo, err := os.Stat(rolloutPath)
	if err != nil {
		t.Fatal(err)
	}
	if first.SourceCursor != uint64(rolloutInfo.Size()) {
		t.Fatalf("durable source cursor = %d, want complete file boundary %d", first.SourceCursor, rolloutInfo.Size())
	}
	if got := providerEffects.Load(); got != 5 {
		t.Fatalf("provider effects after shadow observation = %d, want legacy-only 5", got)
	}

	beforeReplay, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("read shadow state: %v", err)
	}
	reopened, err := chatthread.OpenShadowStore(filepath.Join(root, "shadow"))
	if err != nil {
		t.Fatalf("OpenShadowStore: %v", err)
	}
	restartTrap := &providerEffectTrap{store: reopened, effects: &providerEffects}
	restartedReader := mustNewReader(t, restartTrap)
	var restartReadCursors []uint64
	productionRead := restartedReader.readRecords
	restartedReader.readRecords = func(ctx context.Context, path, sessionID string, startCursor uint64) (string, []codexRecord, uint64, error) {
		restartReadCursors = append(restartReadCursors, startCursor)
		return productionRead(ctx, path, sessionID, startCursor)
	}
	replayed, err := restartedReader.ObserveRollout(context.Background(), observation)
	if err != nil {
		t.Fatalf("replay after restart: %v", err)
	}
	if !reflect.DeepEqual(first, replayed) {
		t.Fatalf("restart/replay snapshot changed\nfirst: %#v\nreplay: %#v", first, replayed)
	}
	if !reflect.DeepEqual(restartReadCursors, []uint64{first.SourceCursor}) {
		t.Fatalf("restart read cursors = %v, want durable cursor %d", restartReadCursors, first.SourceCursor)
	}

	// A deliberately overlapping replay of every structured record is also a
	// no-op, proving replay safety independently of the incremental fast path.
	sessionID, records, endCursor, err := readCodexRecords(rolloutPath, fixtureSessionID)
	if err != nil {
		t.Fatal(err)
	}
	fullReplay, err := restartedReader.observeRecords(
		context.Background(),
		observation,
		replayed,
		sessionID,
		records,
		endCursor,
	)
	if err != nil {
		t.Fatalf("full overlapping replay: %v", err)
	}
	if !reflect.DeepEqual(first, fullReplay) {
		t.Fatalf("full overlapping replay changed snapshot")
	}
	afterReplay, err := os.ReadFile(reopened.Path())
	if err != nil {
		t.Fatalf("read replayed shadow state: %v", err)
	}
	if !reflect.DeepEqual(beforeReplay, afterReplay) {
		t.Fatalf("identical restart replay rewrote diagnostic state")
	}
	if got := providerEffects.Load(); got != 5 {
		t.Fatalf("provider effects after restart/replay = %d, want legacy-only 5", got)
	}

	assertSanitizedStateFile(t, afterReplay, rolloutPath)
}

func TestObserveRolloutUsesOneSnapshotAndSkipsUnchangedApply(t *testing.T) {
	root := t.TempDir()
	store, err := chatthread.InitializeShadowStore(filepath.Join(root, "shadow"))
	if err != nil {
		t.Fatalf("InitializeShadowStore: %v", err)
	}
	sink := &countingFactSink{store: store}
	reader := mustNewReader(t, sink)
	rolloutPath := writeFixtureRollout(t, root, fixtureRolloutLines(t, "same body", "different body", "final body", true))
	observation := fixtureObservation(rolloutPath)

	first, err := reader.ObserveRollout(context.Background(), observation)
	if err != nil {
		t.Fatalf("first observation: %v", err)
	}
	if sink.snapshotCalls.Load() != 1 || sink.applyCalls.Load() != 1 {
		t.Fatalf("first observation snapshot/apply calls = %d/%d, want 1/1", sink.snapshotCalls.Load(), sink.applyCalls.Load())
	}
	beforeUnchanged, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("read first shadow state: %v", err)
	}

	unchanged, err := reader.ObserveRollout(context.Background(), observation)
	if err != nil {
		t.Fatalf("unchanged observation: %v", err)
	}
	if !reflect.DeepEqual(unchanged, first) {
		t.Fatalf("unchanged observation returned a different snapshot")
	}
	if sink.snapshotCalls.Load() != 2 || sink.applyCalls.Load() != 1 {
		t.Fatalf("unchanged observation cumulative snapshot/apply calls = %d/%d, want 2/1", sink.snapshotCalls.Load(), sink.applyCalls.Load())
	}
	afterUnchanged, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("read unchanged shadow state: %v", err)
	}
	if !reflect.DeepEqual(afterUnchanged, beforeUnchanged) {
		t.Fatalf("unchanged observation rewrote shadow state bytes")
	}

	legacyOnly := observation
	legacyOnly.Legacy.OrderedTurns = append([]chatthread.LegacyShadowTurn{}, observation.Legacy.OrderedTurns[:len(observation.Legacy.OrderedTurns)-1]...)
	legacyOnly.Legacy.Queued = append([]chatthread.LegacyShadowTurn{}, observation.Legacy.Queued[:len(observation.Legacy.Queued)-1]...)
	updated, err := reader.ObserveRollout(context.Background(), legacyOnly)
	if err != nil {
		t.Fatalf("legacy-only observation: %v", err)
	}
	if updated.SourceCursor != first.SourceCursor || updated.Digest != first.Digest {
		t.Fatalf("legacy-only update changed canonical state: cursor/digest %d/%s, want %d/%s", updated.SourceCursor, updated.Digest, first.SourceCursor, first.Digest)
	}
	if reflect.DeepEqual(updated.Diagnostics, first.Diagnostics) ||
		updated.Diagnostics.Cardinality.LegacyCount != first.Diagnostics.Cardinality.LegacyCount-1 ||
		updated.Diagnostics.Queue.LegacyCount != first.Diagnostics.Queue.LegacyCount-1 {
		t.Fatalf("legacy-only update did not refresh diagnostics: before %#v, after %#v", first.Diagnostics, updated.Diagnostics)
	}
	if sink.snapshotCalls.Load() != 3 || sink.applyCalls.Load() != 2 {
		t.Fatalf("legacy-only observation cumulative snapshot/apply calls = %d/%d, want 3/2", sink.snapshotCalls.Load(), sink.applyCalls.Load())
	}
	afterLegacy, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("read legacy-updated shadow state: %v", err)
	}
	if reflect.DeepEqual(afterLegacy, afterUnchanged) {
		t.Fatalf("legacy-only diagnostic update did not persist")
	}
}

func TestOverlappingRecordWindowsAreNoOpsAndMatchWholeReplay(t *testing.T) {
	root := t.TempDir()
	rolloutPath := writeFixtureRollout(t, root, fixtureRolloutLines(t, "same body", "different body", "final body", true))
	sessionID, records, endCursor, err := readCodexRecords(rolloutPath, fixtureSessionID)
	if err != nil {
		t.Fatalf("readCodexRecords: %v", err)
	}
	terminalIndex := len(records) - 1
	if terminalIndex < 8 {
		t.Fatalf("fixture has only %d structured records", len(records))
	}
	cut := terminalIndex - 4
	if cut < 4 {
		cut = len(records) / 2
	}

	overlapStore, err := chatthread.InitializeShadowStore(filepath.Join(root, "overlap"))
	if err != nil {
		t.Fatal(err)
	}
	overlapReader := mustNewReader(t, overlapStore)
	observation := fixtureObservation(rolloutPath)
	partialEnd := records[cut-1].Cursor + 1
	partial, err := overlapReader.observeRecords(
		context.Background(),
		observation,
		chatthread.ShadowSnapshot{},
		sessionID,
		records[:cut],
		partialEnd,
	)
	if err != nil {
		t.Fatalf("first window: %v", err)
	}
	overlapped, err := overlapReader.observeRecords(
		context.Background(),
		observation,
		partial,
		sessionID,
		records[cut-3:],
		endCursor,
	)
	if err != nil {
		t.Fatalf("overlapping window: %v", err)
	}
	assertFiveInputSnapshot(t, overlapped)

	wholeStore, err := chatthread.InitializeShadowStore(filepath.Join(root, "whole"))
	if err != nil {
		t.Fatal(err)
	}
	wholeReader := mustNewReader(t, wholeStore)
	whole, err := wholeReader.ObserveRollout(context.Background(), observation)
	if err != nil {
		t.Fatalf("whole observation: %v", err)
	}
	if overlapped.Digest != whole.Digest ||
		!reflect.DeepEqual(overlapped.Thread, whole.Thread) ||
		!reflect.DeepEqual(overlapped.ProviderFactKeys, whole.ProviderFactKeys) ||
		!reflect.DeepEqual(overlapped.AppliedRecords, whole.AppliedRecords) {
		t.Fatalf("overlap result differs from whole replay\noverlap: %#v\nwhole: %#v", overlapped, whole)
	}
}

func TestIncrementalReaderUsesDurableAppendCursor(t *testing.T) {
	root := t.TempDir()
	lines := fixtureRolloutLines(t, "same body", "different body", "final body", true)
	cut := 12
	rolloutPath := writeFixtureRollout(t, root, lines[:cut])
	store, err := chatthread.InitializeShadowStore(filepath.Join(root, "shadow"))
	if err != nil {
		t.Fatal(err)
	}
	reader := mustNewReader(t, store)
	var readCursors []uint64
	productionRead := reader.readRecords
	reader.readRecords = func(ctx context.Context, path, sessionID string, startCursor uint64) (string, []codexRecord, uint64, error) {
		readCursors = append(readCursors, startCursor)
		return productionRead(ctx, path, sessionID, startCursor)
	}

	first, err := reader.ObserveRollout(context.Background(), fixtureObservation(rolloutPath))
	if err != nil {
		t.Fatalf("initial window: %v", err)
	}
	info, err := os.Stat(rolloutPath)
	if err != nil {
		t.Fatal(err)
	}
	if first.SourceCursor != uint64(info.Size()) || first.Thread.CurrentExecutionID == "" {
		t.Fatalf("initial cursor/lifecycle = %d / %#v", first.SourceCursor, first.Thread.ExecutionActivities)
	}
	appendRolloutLines(t, rolloutPath, lines[cut:])
	final, err := reader.ObserveRollout(context.Background(), fixtureObservation(rolloutPath))
	if err != nil {
		t.Fatalf("appended window: %v", err)
	}
	assertFiveInputSnapshot(t, final)
	if !reflect.DeepEqual(readCursors, []uint64{0, first.SourceCursor}) {
		t.Fatalf("incremental read cursors = %v, want [0 %d]", readCursors, first.SourceCursor)
	}
}

func TestIncrementalReaderRetainsIncompleteTrailingRecord(t *testing.T) {
	root := t.TempDir()
	lines := fixtureRolloutLines(t, "same body", "different body", "final body", true)
	terminal := lines[len(lines)-1]
	prefix := strings.Join(lines[:len(lines)-1], "\n") + "\n"
	partialLength := len(terminal) / 2
	rolloutPath := filepath.Join(root, "rollout-fixture.jsonl")
	if err := os.WriteFile(rolloutPath, []byte(prefix+terminal[:partialLength]), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := chatthread.InitializeShadowStore(filepath.Join(root, "shadow"))
	if err != nil {
		t.Fatal(err)
	}
	reader := mustNewReader(t, store)
	var readCursors []uint64
	productionRead := reader.readRecords
	reader.readRecords = func(ctx context.Context, path, sessionID string, startCursor uint64) (string, []codexRecord, uint64, error) {
		readCursors = append(readCursors, startCursor)
		return productionRead(ctx, path, sessionID, startCursor)
	}

	running, err := reader.ObserveRollout(context.Background(), fixtureObservation(rolloutPath))
	if err != nil {
		t.Fatalf("partial observation: %v", err)
	}
	if running.SourceCursor != uint64(len(prefix)) || running.Thread.CurrentExecutionID == "" {
		t.Fatalf("partial source cursor/lifecycle = %d / %#v", running.SourceCursor, running.Thread.ExecutionActivities)
	}
	file, err := os.OpenFile(rolloutPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(terminal[partialLength:] + "\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	settled, err := reader.ObserveRollout(context.Background(), fixtureObservation(rolloutPath))
	if err != nil {
		t.Fatalf("completed trailing record: %v", err)
	}
	assertFiveInputSnapshot(t, settled)
	if !reflect.DeepEqual(readCursors, []uint64{0, uint64(len(prefix))}) {
		t.Fatalf("partial read cursors = %v", readCursors)
	}
}

func TestIncrementalReaderFailsClosedOnTruncationAndSourceIdentityChange(t *testing.T) {
	t.Run("truncation", func(t *testing.T) {
		root := t.TempDir()
		rolloutPath := writeFixtureRollout(t, root, fixtureRolloutLines(t, "same body", "different body", "final body", true))
		store, err := chatthread.InitializeShadowStore(filepath.Join(root, "shadow"))
		if err != nil {
			t.Fatal(err)
		}
		reader := mustNewReader(t, store)
		before, err := reader.ObserveRollout(context.Background(), fixtureObservation(rolloutPath))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(rolloutPath, int64(before.SourceCursor-1)); err != nil {
			t.Fatal(err)
		}
		if _, err := reader.ObserveRollout(context.Background(), fixtureObservation(rolloutPath)); !errors.Is(err, ErrAdapterGap) {
			t.Fatalf("truncation error = %v", err)
		}
		if reader.Enabled("agent:fixture") {
			t.Fatalf("reader remained enabled after source truncation")
		}
		after, err := store.ShadowSnapshot(before.Thread.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("source truncation changed durable shadow state")
		}
	})

	t.Run("identity", func(t *testing.T) {
		root := t.TempDir()
		rolloutPath := writeFixtureRollout(t, root, fixtureRolloutLines(t, "same body", "different body", "final body", true))
		store, err := chatthread.InitializeShadowStore(filepath.Join(root, "shadow"))
		if err != nil {
			t.Fatal(err)
		}
		reader := mustNewReader(t, store)
		before, err := reader.ObserveRollout(context.Background(), fixtureObservation(rolloutPath))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(rolloutPath)
		if err != nil {
			t.Fatal(err)
		}
		changedSessionID := strings.Repeat("a", len(fixtureSessionID))
		replaced := strings.Replace(string(raw), fixtureSessionID, changedSessionID, 1)
		if replaced == string(raw) || len(replaced) != len(raw) {
			t.Fatal("failed to construct equal-length identity replacement")
		}
		if err := os.WriteFile(rolloutPath, []byte(replaced), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := reader.ObserveRollout(context.Background(), fixtureObservation(rolloutPath)); !errors.Is(err, ErrSourceIdentity) {
			t.Fatalf("identity error = %v", err)
		}
		if reader.Enabled("agent:fixture") {
			t.Fatalf("reader remained enabled after source identity change")
		}
		after, err := store.ShadowSnapshot(before.Thread.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("source identity change altered durable shadow state")
		}
	})
}

func TestSubscriptionCancellationDoesNotPoisonReader(t *testing.T) {
	root := t.TempDir()
	rolloutPath := writeFixtureRollout(t, root, fixtureRolloutLines(t, "same body", "different body", "final body", true))
	store, err := chatthread.InitializeShadowStore(filepath.Join(root, "shadow"))
	if err != nil {
		t.Fatal(err)
	}
	reader := mustNewReader(t, store)
	productionRead := reader.readRecords
	ctx, cancel := context.WithCancel(context.Background())
	reader.readRecords = func(ctx context.Context, path, sessionID string, startCursor uint64) (string, []codexRecord, uint64, error) {
		session, records, endCursor, err := productionRead(ctx, path, sessionID, startCursor)
		cancel()
		return session, records, endCursor, err
	}
	if _, err := reader.ObserveRollout(ctx, fixtureObservation(rolloutPath)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled observation error = %v", err)
	}
	if !reader.Enabled("agent:fixture") {
		t.Fatalf("ordinary subscription cancellation poisoned the reader")
	}
	reader.readRecords = productionRead
	settled, err := reader.ObserveRollout(context.Background(), fixtureObservation(rolloutPath))
	if err != nil {
		t.Fatalf("observation after cancellation: %v", err)
	}
	assertFiveInputSnapshot(t, settled)
}

func TestCodexRecordSeamUsesOnlyNativeIDsAndCursors(t *testing.T) {
	root := t.TempDir()
	firstPath := writeFixtureRollout(t, root, fixtureRolloutLines(t, "same body", "different body", "final body", true))
	firstSession, first, _, err := readCodexRecords(firstPath, "")
	if err != nil {
		t.Fatalf("first parse: %v", err)
	}
	if firstSession != fixtureSessionID {
		t.Fatalf("session ID = %q", firstSession)
	}

	// All replacements are equal-length. The structured records, cursors, keys,
	// and fingerprints must remain identical when bodies, attachments, commands,
	// and tool output change.
	secondDir := filepath.Join(root, "second")
	if err := os.MkdirAll(secondDir, 0o700); err != nil {
		t.Fatal(err)
	}
	secondLines := fixtureRolloutLines(t, "xxxx xxxx", "zzzzzzzzz zzzz", "yyyyy zzzz", true)
	for index := range secondLines {
		for _, content := range []string{
			"/private/attachment",
			"secret-command",
			"secret-output",
			"/private/workspace",
		} {
			secondLines[index] = strings.ReplaceAll(secondLines[index], content, strings.Repeat("x", len(content)))
		}
	}
	secondPath := writeFixtureRollout(t, secondDir, secondLines)
	_, second, _, err := readCodexRecords(secondPath, "")
	if err != nil {
		t.Fatalf("second parse: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("content changed the structural record seam\nfirst: %#v\nsecond: %#v", first, second)
	}

	var starts, admissions, inputEchoes, assistants, tools, terminals int
	seenInputKeys := map[chatthread.ProviderFactKey]struct{}{}
	for _, record := range first {
		if record.Key == "" || record.Cursor == 0 {
			t.Fatalf("record lacks stable key/cursor: %#v", record)
		}
		switch record.Kind {
		case recordActivityStarted:
			starts++
			if record.NativeActivityID != fixtureNativeActivityID {
				t.Fatalf("native task ID = %q", record.NativeActivityID)
			}
		case recordInputAdmitted:
			admissions++
			seenInputKeys[record.Key] = struct{}{}
		case recordInputEcho:
			inputEchoes++
		case recordAssistant:
			assistants++
			if !strings.HasPrefix(record.NativeItemID, "msg-") {
				t.Fatalf("assistant lacks response item ID: %#v", record)
			}
		case recordToolStarted:
			tools++
			if !strings.HasPrefix(record.NativeCallID, "call-") {
				t.Fatalf("tool lacks call ID: %#v", record)
			}
		case recordActivityTerminal:
			terminals++
			if record.NativeActivityID != fixtureNativeActivityID || record.TerminalState != chatthread.ActivityCompleted {
				t.Fatalf("terminal structure = %#v", record)
			}
		}
	}
	if starts != 1 || admissions != 5 || inputEchoes != 5 || assistants != 3 || tools != 2 || terminals != 1 {
		t.Fatalf("record cardinality = starts %d admissions %d echoes %d assistants %d tools %d terminals %d", starts, admissions, inputEchoes, assistants, tools, terminals)
	}
	if len(seenInputKeys) != 5 {
		t.Fatalf("five admissions produced %d distinct record keys", len(seenInputKeys))
	}
}

func TestAssistantTextNeverInfersTerminal(t *testing.T) {
	root := t.TempDir()
	lines := fixtureRolloutLines(t, "same body", "different body", "final body", false)
	for index := range lines {
		lines[index] = strings.ReplaceAll(lines[index], "assistant reply five", "task_complete finished")
	}
	rolloutPath := writeFixtureRollout(t, root, lines)
	store, err := chatthread.InitializeShadowStore(filepath.Join(root, "shadow"))
	if err != nil {
		t.Fatal(err)
	}
	reader := mustNewReader(t, store)
	snapshot, err := reader.ObserveRollout(context.Background(), fixtureObservation(rolloutPath))
	if err != nil {
		t.Fatalf("observe without terminal: %v", err)
	}
	if snapshot.Thread.CurrentExecutionID == "" || len(snapshot.Thread.ExecutionActivities) != 1 ||
		snapshot.Thread.ExecutionActivities[0].State != chatthread.ActivityRunning {
		t.Fatalf("assistant text inferred terminal lifecycle: %#v", snapshot.Thread.ExecutionActivities)
	}
}

func TestUnprovableAppCorrelationNeverReportsIdentityMatch(t *testing.T) {
	root := t.TempDir()
	rolloutPath := writeFixtureRollout(t, root, fixtureRolloutLines(t, "same body", "different body", "final body", true))
	store, err := chatthread.InitializeShadowStore(filepath.Join(root, "shadow"))
	if err != nil {
		t.Fatal(err)
	}
	reader := mustNewReader(t, store)
	legacyTurns := make([]chatthread.LegacyShadowTurn, 5)
	for index := range legacyTurns {
		legacyTurns[index] = chatthread.LegacyShadowTurn{ID: "app-public-" + strconv.Itoa(index+1), State: "completed"}
	}
	snapshot, err := reader.ObserveRollout(context.Background(), Observation{
		OwnerKey:    "agent:fixture",
		RolloutPath: rolloutPath,
		SessionID:   fixtureSessionID,
		Legacy: chatthread.LegacyShadowProjection{
			OrderedTurns:  legacyTurns,
			TerminalState: "completed",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Diagnostics.Cardinality.State != chatthread.ShadowComparisonMatch {
		t.Fatalf("equal cardinality was not detected: %#v", snapshot.Diagnostics.Cardinality)
	}
	if snapshot.Diagnostics.Chronology.State != chatthread.ShadowComparisonUnprovable {
		t.Fatalf("unbound IDs were silently matched: %#v", snapshot.Diagnostics.Chronology)
	}
	if len(snapshot.Diagnostics.CorrelationGaps) != 5 {
		t.Fatalf("correlation gaps = %d, want 5", len(snapshot.Diagnostics.CorrelationGaps))
	}
}

func TestDefaultOffAndMissingConfiguredStateNeverInitializes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-created")
	lookup := func(string) string { return "" }
	reader, err := OpenConfigured(root, lookup)
	if err != nil || reader != nil {
		t.Fatalf("default-off OpenConfigured = (%#v, %v)", reader, err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default-off mode created state root: %v", err)
	}

	lookup = func(key string) string {
		switch key {
		case EnvScopes:
			return "agent:fixture"
		case EnvRoot:
			return root
		default:
			return ""
		}
	}
	reader, err = OpenConfigured(t.TempDir(), lookup)
	if reader != nil || !errors.Is(err, chatthread.ErrShadowNotInitialized) {
		t.Fatalf("missing configured state = (%#v, %v)", reader, err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing configured store was initialized: %v", err)
	}

	configuredRoot := filepath.Join(t.TempDir(), "configured")
	if _, err := chatthread.InitializeShadowStore(configuredRoot); err != nil {
		t.Fatal(err)
	}
	lookup = func(key string) string {
		switch key {
		case EnvScopes:
			return "agent:fixture, scope:brain-thread:explicit"
		case EnvRoot:
			return configuredRoot
		default:
			return ""
		}
	}
	reader, err = OpenConfigured(t.TempDir(), lookup)
	if err != nil || reader == nil {
		t.Fatalf("explicit configured reader = (%#v, %v)", reader, err)
	}
	if !reader.Enabled("agent:fixture") || !reader.Enabled("scope:brain-thread:explicit") || reader.Enabled("agent:other") {
		t.Fatalf("configured scope allowlist was not exact")
	}
}

func assertFiveInputSnapshot(t *testing.T, snapshot chatthread.ShadowSnapshot) {
	t.Helper()
	thread := snapshot.Thread
	if snapshot.Digest != fixtureShadowDigest {
		t.Fatalf("shadow digest = %s, want %s", snapshot.Digest, fixtureShadowDigest)
	}
	if snapshot.Ownership != chatthread.ShadowOwnershipV1ReadOnly {
		t.Fatalf("ownership = %q", snapshot.Ownership)
	}
	if len(thread.ExecutionActivities) != 1 || len(thread.Submissions) != 5 || len(thread.Events) != 5 {
		t.Fatalf("canonical cardinality = activities %d submissions %d events %d", len(thread.ExecutionActivities), len(thread.Submissions), len(thread.Events))
	}
	if len(snapshot.ProviderFactKeys) != 14 || len(snapshot.AppliedRecords) != 22 {
		t.Fatalf("fact/record cardinality = %d/%d, want 14/22", len(snapshot.ProviderFactKeys), len(snapshot.AppliedRecords))
	}
	for _, key := range snapshot.ProviderFactKeys {
		if !strings.HasPrefix(string(key), "codex-record-") {
			t.Fatalf("non-deterministic fact key %q", key)
		}
	}
	activity := thread.ExecutionActivities[0]
	if activity.State != chatthread.ActivityCompleted || activity.InputCount != 5 || thread.CurrentExecutionID != "" {
		t.Fatalf("terminal activity = %#v, current = %q", activity, thread.CurrentExecutionID)
	}
	if len(thread.QueuedSubmissionIDs) != 0 {
		t.Fatalf("terminal queue = %v", thread.QueuedSubmissionIDs)
	}
	wantSubmissionPositions := []chatthread.Position{1, 3, 5, 7, 9}
	wantEventPositions := []chatthread.Position{2, 4, 6, 8, 10}
	wantEventKinds := []chatthread.EventKind{
		chatthread.EventAssistant,
		chatthread.EventTool,
		chatthread.EventAssistant,
		chatthread.EventTool,
		chatthread.EventAssistant,
	}
	seenSubmissionIDs := map[chatthread.SubmissionID]struct{}{}
	for index, submission := range thread.Submissions {
		if submission.Position != wantSubmissionPositions[index] || submission.InputOrdinal != chatthread.InputOrdinal(index+1) ||
			submission.Origin != chatthread.OriginProviderExternal || submission.Delivery != chatthread.DeliveryDelivered ||
			submission.Payload.Body != "" || len(submission.Payload.AttachmentIDs) != 0 {
			t.Fatalf("Submission[%d] = %#v", index, submission)
		}
		if _, duplicate := seenSubmissionIDs[submission.ID]; duplicate {
			t.Fatalf("duplicate Submission ID %q", submission.ID)
		}
		seenSubmissionIDs[submission.ID] = struct{}{}
	}
	for index, event := range thread.Events {
		if event.Position != wantEventPositions[index] || event.Payload != "" ||
			event.CausalSubmissionID != thread.Submissions[index].ID ||
			event.Kind != wantEventKinds[index] || !event.Final {
			t.Fatalf("Event[%d] = %#v", index, event)
		}
	}
	if thread.Revision != 19 || thread.NextPosition != 11 {
		t.Fatalf("revision/next position = %d/%d, want 19/11", thread.Revision, thread.NextPosition)
	}
	if len(snapshot.Diagnostics.CorrelationGaps) != 5 {
		t.Fatalf("correlation gaps = %d, want 5", len(snapshot.Diagnostics.CorrelationGaps))
	}
	for _, gap := range snapshot.Diagnostics.CorrelationGaps {
		if gap.Reason != chatthread.CorrelationGapNoExplicitAppBinding {
			t.Fatalf("unexpected correlation gap: %#v", gap)
		}
	}
	if snapshot.Diagnostics.Cardinality.State != chatthread.ShadowComparisonDiverged ||
		snapshot.Diagnostics.Chronology.State != chatthread.ShadowComparisonDiverged ||
		snapshot.Diagnostics.CurrentActivity.State != chatthread.ShadowComparisonDiverged ||
		snapshot.Diagnostics.Queue.State != chatthread.ShadowComparisonDiverged ||
		snapshot.Diagnostics.TerminalSettlement.State != chatthread.ShadowComparisonDiverged {
		t.Fatalf("expected five divergence categories: %#v", snapshot.Diagnostics)
	}
}

func assertSanitizedStateFile(t *testing.T, raw []byte, rolloutPath string) {
	t.Helper()
	for _, forbidden := range []string{
		"same body",
		"different body",
		"final body",
		"assistant reply",
		"secret-command",
		"secret-output",
		"/private/attachment",
		fixtureNativeActivityID,
		fixtureSessionID,
		"public-turn-02",
		rolloutPath,
		filepath.Dir(rolloutPath),
	} {
		if forbidden != "" && bytesContains(raw, forbidden) {
			t.Fatalf("diagnostic state contains forbidden content %q", forbidden)
		}
	}
}

func bytesContains(raw []byte, value string) bool {
	return strings.Contains(string(raw), value)
}

func mustNewReader(t *testing.T, sink FactSink) *Reader {
	t.Helper()
	reader, err := NewReader(sink, []string{"agent:fixture"})
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	return reader
}

func fixtureObservation(path string) Observation {
	current := chatthread.LegacyShadowTurn{ID: "public-turn-02", State: "running"}
	queued := []chatthread.LegacyShadowTurn{
		{ID: "public-turn-03", State: "queued"},
		{ID: "public-turn-04", State: "queued"},
		{ID: "public-turn-05", State: "queued"},
	}
	ordered := append([]chatthread.LegacyShadowTurn{current}, queued...)
	return Observation{
		OwnerKey:    "agent:fixture",
		RolloutPath: path,
		SessionID:   fixtureSessionID,
		Legacy: chatthread.LegacyShadowProjection{
			OrderedTurns: ordered,
			Current:      &current,
			Queued:       queued,
		},
	}
}

func writeFixtureRollout(t *testing.T, root string, lines []string) string {
	t.Helper()
	path := filepath.Join(root, "rollout-fixture.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write rollout: %v", err)
	}
	return path
}

func appendRolloutLines(t *testing.T, path string, lines []string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func fixtureRolloutLines(t *testing.T, repeatedBody, fourthBody, fifthBody string, terminal bool) []string {
	t.Helper()
	type line struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
		Payload   any    `json:"payload"`
	}
	encode := func(timestamp, envelopeType string, payload any) string {
		raw, err := json.Marshal(line{Type: envelopeType, Timestamp: timestamp, Payload: payload})
		if err != nil {
			t.Fatalf("marshal fixture: %v", err)
		}
		return string(raw)
	}
	userPair := func(timestamp, body string) []string {
		return []string{
			encode(timestamp, "response_item", map[string]any{
				"type": "message", "role": "user", "content": body,
			}),
			encode(timestamp, "event_msg", map[string]any{
				"type": "user_message", "message": body,
				"attachments": []string{"/private/attachment"},
			}),
		}
	}
	assistantPair := func(timestamp, itemID, body string) []string {
		return []string{
			encode(timestamp, "event_msg", map[string]any{"type": "agent_message", "message": body}),
			encode(timestamp, "response_item", map[string]any{
				"type": "message", "role": "assistant", "id": itemID, "content": body,
			}),
		}
	}
	toolPair := func(timestamp, itemID, callID string) []string {
		return []string{
			encode(timestamp, "response_item", map[string]any{
				"type": "custom_tool_call", "id": itemID, "call_id": callID,
				"name": "exec", "input": "secret-command /private/workspace",
			}),
			encode(timestamp, "response_item", map[string]any{
				"type": "custom_tool_call_output", "call_id": callID, "output": "secret-output",
			}),
		}
	}

	lines := []string{
		encode("2026-07-16T02:50:45.100Z", "session_meta", map[string]any{
			"id": fixtureSessionID, "cwd": "/private/workspace", "originator": "codex-tui",
		}),
		encode("2026-07-16T02:50:45.176Z", "event_msg", map[string]any{
			"type": "task_started", "turn_id": fixtureNativeActivityID,
		}),
	}
	lines = append(lines, userPair("2026-07-16T02:50:45.194Z", repeatedBody)...)
	lines = append(lines, assistantPair("2026-07-16T02:50:58.176Z", "msg-01", "assistant reply one")...)
	lines = append(lines, userPair("2026-07-16T02:55:19.791Z", repeatedBody)...)
	lines = append(lines, toolPair("2026-07-16T02:55:19.793Z", "tool-02", "call-02")...)
	lines = append(lines, userPair("2026-07-16T02:55:19.795Z", repeatedBody)...)
	lines = append(lines, assistantPair("2026-07-16T02:56:00.000Z", "msg-03", "assistant reply three")...)
	lines = append(lines, userPair("2026-07-16T02:58:49.404Z", fourthBody)...)
	lines = append(lines, toolPair("2026-07-16T02:59:00.000Z", "tool-04", "call-04")...)
	lines = append(lines, userPair("2026-07-16T03:00:01.931Z", fifthBody)...)
	lines = append(lines, assistantPair("2026-07-16T03:03:45.380Z", "msg-05", "assistant reply five")...)
	if terminal {
		lines = append(lines, encode("2026-07-16T03:03:45.411Z", "event_msg", map[string]any{
			"type": "task_complete", "turn_id": fixtureNativeActivityID,
		}))
	}
	return lines
}
