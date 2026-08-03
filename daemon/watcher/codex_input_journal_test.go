package watcher

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestCodexShellSnapshotIdentityBindsLatestExactPaneGeneration(t *testing.T) {
	codexHome := t.TempDir()
	snapshotDir := filepath.Join(codexHome, "shell_snapshots")
	if err := os.MkdirAll(snapshotDir, 0o700); err != nil {
		t.Fatalf("create snapshots: %v", err)
	}
	now := time.Unix(1785692000, 0)
	startedAt := now.Add(-20 * time.Second)
	writeSnapshot := func(sessionID string, createdAt time.Time, paneID string) {
		t.Helper()
		name := sessionID + "." + strconv.FormatInt(createdAt.UnixNano(), 10) + ".sh"
		body := "# exact Codex shell snapshot\nexport TMUX_PANE='" + paneID + "'\n"
		if err := os.WriteFile(filepath.Join(snapshotDir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write snapshot: %v", err)
		}
	}
	staleID := "019fc374-2430-7bf3-b209-717a76c41ea1"
	foreignID := "019fc374-2430-7bf3-b209-717a76c41ea2"
	targetID := "019fc374-2430-7bf3-b209-717a76c41ea3"
	writeSnapshot(staleID, startedAt.Add(-time.Minute), "%41")
	writeSnapshot(staleID, startedAt.Add(time.Second), "%41")
	writeSnapshot(foreignID, now.Add(-time.Second), "%99")
	writeSnapshot(targetID, now, "%41")

	processes := map[int]processInfo{
		101: {
			pid:       101,
			startedAt: startedAt,
			comm:      "codex",
			args:      "/opt/codex/bin/codex --no-alt-screen",
		},
	}
	identity := findCodexShellSnapshotIdentity(
		codexHome,
		"%41",
		[]int{101},
		processes,
		now,
	)
	if identity.Path != "" || identity.SessionID != targetID {
		t.Fatalf("identity = %#v, want exact latest pane thread %q", identity, targetID)
	}
}

func TestCodexSessionIdentityResolvesOnlyItsExactRollout(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	sessionDir := filepath.Join(codexHome, "sessions", "2026", "08", "03")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatalf("create sessions: %v", err)
	}
	targetID := "019fc374-2430-7bf3-b209-717a76c41ea3"
	foreignID := "019fc374-2430-7bf3-b209-717a76c41ea4"
	instruction := "exact session-bound instruction"
	now := time.Now().UTC()
	targetPath := filepath.Join(sessionDir, "rollout-target-"+targetID+".jsonl")
	foreignPath := filepath.Join(sessionDir, "rollout-foreign-"+foreignID+".jsonl")
	identity := codexRolloutIdentity{SessionID: targetID}
	if err := os.WriteFile(targetPath, nil, 0o600); err != nil {
		t.Fatalf("create pending target rollout: %v", err)
	}
	if matched, err := codexRolloutContainsExactUserMessage(identity, instruction, now); err != nil || matched {
		t.Fatalf("pending target identity matched=%v err=%v", matched, err)
	}
	writeCodexJournalTestRows(t, targetPath, []map[string]any{
		codexJournalSessionMeta(now, targetID),
	})
	writeCodexJournalTestRows(t, foreignPath, []map[string]any{
		codexJournalSessionMeta(now, foreignID),
		{
			"timestamp": now.Add(time.Second).Format(time.RFC3339Nano),
			"type":      "event_msg",
			"payload": map[string]any{
				"type":    "user_message",
				"message": instruction,
			},
		},
	})
	if matched, err := codexRolloutContainsExactUserMessage(identity, instruction, now); err != nil || matched {
		t.Fatalf("foreign exact message matched=%v err=%v", matched, err)
	}
	writeCodexJournalTestRows(t, targetPath, []map[string]any{
		codexJournalSessionMeta(now, targetID),
		{
			"timestamp": now.Add(2 * time.Second).Format(time.RFC3339Nano),
			"type":      "event_msg",
			"payload": map[string]any{
				"type":    "user_message",
				"message": instruction,
			},
		},
	})
	if matched, err := codexRolloutContainsExactUserMessage(identity, instruction, now); err != nil || !matched {
		t.Fatalf("target exact message matched=%v err=%v", matched, err)
	}
}

func TestCodexRolloutReconciliationRequiresExactCurrentUserMessage(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	sessionDir := filepath.Join(codexHome, "sessions", "2026", "08", "02")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatalf("create sessions: %v", err)
	}
	instruction := "ZEN_TX=exact;PATH_B64URL=L3RtcA;PAYLOAD_SHA256=abc;ACTION=follow"
	now := time.Now().UTC()
	rows := []map[string]any{
		codexJournalSessionMeta(now, "session-reconcile"),
		{
			"timestamp": now.Add(-time.Hour).Format(time.RFC3339Nano),
			"type":      "event_msg",
			"payload": map[string]any{
				"type":    "user_message",
				"message": instruction,
			},
		},
		{
			"timestamp": now.Add(time.Second).Format(time.RFC3339Nano),
			"type":      "response_item",
			"payload": map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": instruction + "-foreign-suffix"},
				},
			},
		},
	}
	path := filepath.Join(sessionDir, "rollout-reconcile.jsonl")
	rollout := codexRolloutIdentity{Path: path, SessionID: "session-reconcile"}
	writeCodexJournalTestRows(t, path, rows)
	if matched, err := codexRolloutContainsExactUserMessage(rollout, instruction, now); err != nil || matched {
		t.Fatalf("stale/suffixed match = %v, err=%v", matched, err)
	}

	rows = append(rows, map[string]any{
		"timestamp": now.Add(2 * time.Second).Format(time.RFC3339Nano),
		"type":      "event_msg",
		"payload": map[string]any{
			"type":    "user_message",
			"message": instruction,
		},
	})
	writeCodexJournalTestRows(t, path, rows)
	if matched, err := codexRolloutContainsExactUserMessage(rollout, instruction, now); err != nil || !matched {
		t.Fatalf("exact match = %v, err=%v", matched, err)
	}
}

func TestCodexRolloutReconciliationRejectsMalformedTimestamp(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	sessionDir := filepath.Join(codexHome, "sessions")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatalf("create sessions: %v", err)
	}
	instruction := "ZEN_TX=malformed-timestamp"
	path := filepath.Join(sessionDir, "rollout-malformed.jsonl")
	writeCodexJournalTestRows(t, path, []map[string]any{
		codexJournalSessionMeta(time.Now().UTC(), "session-malformed"),
		{
			"timestamp": "not-a-time",
			"type":      "event_msg",
			"payload": map[string]any{
				"type":    "user_message",
				"message": instruction,
			},
		},
	})
	rollout := codexRolloutIdentity{Path: path, SessionID: "session-malformed"}
	if matched, err := codexRolloutContainsExactUserMessage(rollout, instruction, time.Now()); err != nil || matched {
		t.Fatalf("malformed timestamp match = %v, err=%v", matched, err)
	}
}

func TestCodexRolloutReconciliationDoesNotUseForeignRollout(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	sessionDir := filepath.Join(codexHome, "sessions", "2026", "08", "03")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatalf("create sessions: %v", err)
	}
	instruction := "exact instruction belongs only to rollout B"
	now := time.Now().UTC()
	rolloutA := filepath.Join(sessionDir, "rollout-A.jsonl")
	rolloutB := filepath.Join(sessionDir, "rollout-B.jsonl")
	writeCodexJournalTestRows(t, rolloutA, []map[string]any{
		codexJournalSessionMeta(now, "session-A"),
		{
			"timestamp": now.Add(time.Second).Format(time.RFC3339Nano),
			"type":      "event_msg",
			"payload": map[string]any{
				"type":    "user_message",
				"message": "different instruction in rollout A",
			},
		},
	})
	writeCodexJournalTestRows(t, rolloutB, []map[string]any{
		codexJournalSessionMeta(now, "session-B"),
		{
			"timestamp": now.Add(time.Second).Format(time.RFC3339Nano),
			"type":      "event_msg",
			"payload": map[string]any{
				"type":    "user_message",
				"message": instruction,
			},
		},
	})

	matched, err := codexRolloutContainsExactUserMessage(
		codexRolloutIdentity{Path: rolloutA, SessionID: "session-A"},
		instruction,
		now,
	)
	if err != nil {
		t.Fatalf("reconcile target rollout: %v", err)
	}
	if matched {
		t.Fatal("an exact instruction in foreign rollout B must not confirm target rollout A")
	}
}

func TestCodexTransactionStoreIsolatesCorruptForeignRecordAndExpiresTerminalState(t *testing.T) {
	stateDir := t.TempDir()
	store, err := newFileCodexTransactionStore(stateDir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	now := time.Now().UTC()
	active := codexTransactionRecord{
		SchemaVersion:     codexTransactionSchemaVersion,
		TransactionID:     "active-transaction",
		SessionID:         "agent:@active",
		SessionGeneration: "generation-active",
		Action:            "submit_codex_input",
		Phase:             codexTransactionAmbiguous,
		PayloadSHA256:     codexSHA256("active"),
		Instruction:       "active",
		InstructionSHA256: codexSHA256("active"),
		CreatedAt:         now.Add(-time.Hour),
		UpdatedAt:         now.Add(-time.Hour),
	}
	missingEnvelope := active
	missingEnvelope.TransactionID = "missing-envelope"
	missingEnvelope.EnvelopePath = filepath.Join(store.envelopeDir, missingEnvelope.PayloadSHA256)
	if err := store.Save(missingEnvelope); err == nil {
		t.Fatal("a transaction must not become durable without its referenced envelope")
	}
	if _, err := os.Stat(store.recordPath(missingEnvelope)); !os.IsNotExist(err) {
		t.Fatalf("missing-envelope transaction residue: %v", err)
	}
	if err := store.Save(active); err != nil {
		t.Fatalf("save active: %v", err)
	}
	terminal := active
	terminal.TransactionID = "expired-terminal"
	terminal.SessionID = "agent:@expired"
	terminal.SessionGeneration = "generation-expired"
	terminal.Phase = codexTransactionConfirmed
	terminal.CreatedAt = now.Add(-30 * 24 * time.Hour)
	terminal.UpdatedAt = terminal.CreatedAt
	sharedPayload := "shared retained envelope"
	sharedEnvelope, err := store.WriteEnvelope(codexSHA256(sharedPayload), []byte(sharedPayload))
	if err != nil {
		t.Fatalf("write shared envelope: %v", err)
	}
	terminal.EnvelopePath = sharedEnvelope
	terminal.PayloadSHA256 = codexSHA256(sharedPayload)
	if err := store.Save(terminal); err != nil {
		t.Fatalf("save terminal: %v", err)
	}
	retainedTerminal := terminal
	retainedTerminal.TransactionID = "retained-terminal"
	retainedTerminal.SessionID = "agent:@retained"
	retainedTerminal.SessionGeneration = "generation-retained"
	retainedTerminal.CreatedAt = now
	retainedTerminal.UpdatedAt = now
	if err := store.Save(retainedTerminal); err != nil {
		t.Fatalf("save retained terminal: %v", err)
	}
	corruptPath := filepath.Join(
		store.transactionDir,
		codexTransactionScope("agent:@foreign", "generation-foreign")+"-corrupt.json",
	)
	if err := os.WriteFile(corruptPath, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write corrupt record: %v", err)
	}

	records, err := store.Active(active.SessionID, active.SessionGeneration)
	if err != nil {
		t.Fatalf("unaffected active query blocked by corrupt foreign record: %v", err)
	}
	if len(records) != 1 || records[0].TransactionID != active.TransactionID {
		t.Fatalf("active records = %#v", records)
	}
	if _, err := os.Stat(store.recordPath(terminal)); !os.IsNotExist(err) {
		t.Fatalf("expired terminal record was not removed: %v", err)
	}
	if _, err := os.Stat(corruptPath); err != nil {
		t.Fatalf("corrupt record must remain isolated for inspection: %v", err)
	}
	if _, err := os.Stat(sharedEnvelope); err != nil {
		t.Fatalf("shared envelope expired while retained reference exists: %v", err)
	}
	if err := store.Maintain(now.Add(codexTerminalTransactionRetention + time.Hour)); err != nil {
		t.Fatalf("second retention pass: %v", err)
	}
	if _, err := os.Stat(store.recordPath(retainedTerminal)); !os.IsNotExist(err) {
		t.Fatalf("last terminal reference was not removed: %v", err)
	}
	if _, err := os.Stat(sharedEnvelope); !os.IsNotExist(err) {
		t.Fatalf("shared envelope survived its last retained reference: %v", err)
	}
	records, err = store.Active(active.SessionID, active.SessionGeneration)
	if err != nil || len(records) != 1 {
		t.Fatalf("active ambiguity was not retained: records=%#v err=%v", records, err)
	}
	currentCorruptPath := filepath.Join(
		store.transactionDir,
		codexTransactionScope(active.SessionID, active.SessionGeneration)+"-corrupt.json",
	)
	if err := os.WriteFile(currentCorruptPath, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write current-scope corrupt record: %v", err)
	}
	if _, err := store.Active(active.SessionID, active.SessionGeneration); err == nil {
		t.Fatal("a corrupt record in the target scope must fail closed for that Session")
	}
}

func TestCodexTransactionStoreExpiresOldOrphansButPreservesGraceAndReferences(t *testing.T) {
	stateDir := t.TempDir()
	store, err := newFileCodexTransactionStore(stateDir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	now := time.Now().UTC()
	old := now.Add(-30 * 24 * time.Hour)
	writeEnvelope := func(payload string) string {
		t.Helper()
		path, writeErr := store.WriteEnvelope(codexSHA256(payload), []byte(payload))
		if writeErr != nil {
			t.Fatalf("write envelope %q: %v", payload, writeErr)
		}
		return path
	}
	setAge := func(path string, updatedAt time.Time) {
		t.Helper()
		if err := os.Chtimes(path, updatedAt, updatedAt); err != nil {
			t.Fatalf("set envelope age: %v", err)
		}
	}

	oldOrphan := writeEnvelope("old unreferenced envelope")
	setAge(oldOrphan, old)
	postLastRecordOrphan := writeEnvelope("orphan after last record removal")
	orphanedRecord := codexTransactionRecord{
		SchemaVersion:     codexTransactionSchemaVersion,
		TransactionID:     "last-reference-crash",
		SessionID:         "agent:@last-reference",
		SessionGeneration: "generation-last-reference",
		Action:            "submit_codex_input",
		Phase:             codexTransactionConfirmed,
		PayloadSHA256:     codexSHA256("orphan after last record removal"),
		Instruction:       "last reference",
		InstructionSHA256: codexSHA256("last reference"),
		EnvelopePath:      postLastRecordOrphan,
		CreatedAt:         old,
		UpdatedAt:         old,
	}
	if err := store.Save(orphanedRecord); err != nil {
		t.Fatalf("save last reference: %v", err)
	}
	if err := os.Remove(store.recordPath(orphanedRecord)); err != nil {
		t.Fatalf("simulate crash after last-record removal: %v", err)
	}
	setAge(postLastRecordOrphan, old)

	recentOrphan := writeEnvelope("recent create-before-reference envelope")
	setAge(recentOrphan, now.Add(-time.Hour))
	reusedInFlightPayload := "reused content-addressed create-before-reference envelope"
	reusedInFlight := writeEnvelope(reusedInFlightPayload)
	setAge(reusedInFlight, old)
	if refreshed, err := store.WriteEnvelope(
		codexSHA256(reusedInFlightPayload),
		[]byte(reusedInFlightPayload),
	); err != nil || refreshed != reusedInFlight {
		t.Fatalf("refresh reused in-flight envelope path=%q err=%v", refreshed, err)
	}
	sharedPayload := "shared retained envelope round4"
	sharedEnvelope := writeEnvelope(sharedPayload)
	setAge(sharedEnvelope, old)
	active := codexTransactionRecord{
		SchemaVersion:     codexTransactionSchemaVersion,
		TransactionID:     "active-shared-reference",
		SessionID:         "agent:@active-round4",
		SessionGeneration: "generation-active-round4",
		Action:            "submit_codex_input",
		Phase:             codexTransactionAmbiguous,
		PayloadSHA256:     codexSHA256(sharedPayload),
		Instruction:       "active shared",
		InstructionSHA256: codexSHA256("active shared"),
		EnvelopePath:      sharedEnvelope,
		CreatedAt:         old,
		UpdatedAt:         old,
	}
	if err := store.Save(active); err != nil {
		t.Fatalf("save active reference: %v", err)
	}
	recentTerminal := active
	recentTerminal.TransactionID = "recent-shared-reference"
	recentTerminal.SessionID = "agent:@recent-round4"
	recentTerminal.SessionGeneration = "generation-recent-round4"
	recentTerminal.Phase = codexTransactionConfirmed
	recentTerminal.CreatedAt = now
	recentTerminal.UpdatedAt = now
	if err := store.Save(recentTerminal); err != nil {
		t.Fatalf("save recent shared reference: %v", err)
	}
	corruptForeign := filepath.Join(
		store.transactionDir,
		codexTransactionScope("agent:@foreign-round4", "generation-foreign-round4")+"-corrupt.json",
	)
	if err := os.WriteFile(corruptForeign, []byte("{corrupt"), 0o600); err != nil {
		t.Fatalf("write corrupt foreign record: %v", err)
	}

	if err := store.Maintain(now); err != nil {
		t.Fatalf("maintain envelopes: %v", err)
	}
	for _, path := range []string{oldOrphan, postLastRecordOrphan} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("old orphan retained at %s: %v", path, err)
		}
	}
	for _, path := range []string{recentOrphan, reusedInFlight, sharedEnvelope, corruptForeign} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("required retained state missing at %s: %v", path, err)
		}
	}
	records, err := store.Active(active.SessionID, active.SessionGeneration)
	if err != nil || len(records) != 1 || records[0].TransactionID != active.TransactionID {
		t.Fatalf("unaffected active query records=%#v err=%v", records, err)
	}
}

func TestCodexTransactionStoreExpiresOnlyOldZenAtomicTemps(t *testing.T) {
	stateDir := t.TempDir()
	store, err := newFileCodexTransactionStore(stateDir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	now := time.Now().UTC()
	old := now.Add(-codexUnreferencedEnvelopeGrace - time.Hour)
	recent := now.Add(-time.Hour)
	writeAt := func(path, content string, modified time.Time) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		if err := os.Chtimes(path, modified, modified); err != nil {
			t.Fatalf("age %s: %v", path, err)
		}
	}

	oldTransactionTemp := filepath.Join(store.transactionDir, ".codex-input-old-transaction")
	recentTransactionTemp := filepath.Join(store.transactionDir, ".codex-input-recent-transaction")
	oldEnvelopeTemp := filepath.Join(store.envelopeDir, ".codex-input-old-envelope")
	recentEnvelopeTemp := filepath.Join(store.envelopeDir, ".codex-input-recent-envelope")
	writeAt(oldTransactionTemp, "old sensitive transaction temp", old)
	writeAt(recentTransactionTemp, "recent in-flight transaction temp", recent)
	writeAt(oldEnvelopeTemp, "old sensitive envelope temp", old)
	writeAt(recentEnvelopeTemp, "recent in-flight envelope temp", recent)

	foreignTransaction := filepath.Join(store.transactionDir, ".operator-scratch")
	foreignEnvelope := filepath.Join(store.envelopeDir, "operator-malformed-state")
	corruptRecord := filepath.Join(store.transactionDir, "foreign-corrupt.json")
	writeAt(foreignTransaction, "operator transaction state", old)
	writeAt(foreignEnvelope, "operator envelope state", old)
	writeAt(corruptRecord, "{not-json", old)

	sharedPayload := "round5 active referenced envelope"
	sharedEnvelope, err := store.WriteEnvelope(codexSHA256(sharedPayload), []byte(sharedPayload))
	if err != nil {
		t.Fatalf("write shared envelope: %v", err)
	}
	if err := os.Chtimes(sharedEnvelope, old, old); err != nil {
		t.Fatalf("age shared envelope: %v", err)
	}
	active := codexTransactionRecord{
		SchemaVersion:     codexTransactionSchemaVersion,
		TransactionID:     "round5-active",
		SessionID:         "agent:@round5-active",
		SessionGeneration: "generation-round5-active",
		Action:            "submit_codex_input",
		Phase:             codexTransactionAmbiguous,
		PayloadSHA256:     codexSHA256(sharedPayload),
		Instruction:       "round5 active",
		InstructionSHA256: codexSHA256("round5 active"),
		EnvelopePath:      sharedEnvelope,
		CreatedAt:         old,
		UpdatedAt:         old,
	}
	if err := store.Save(active); err != nil {
		t.Fatalf("save active record: %v", err)
	}
	recentOrphanPayload := "round5 recent content-addressed orphan"
	recentOrphan, err := store.WriteEnvelope(
		codexSHA256(recentOrphanPayload),
		[]byte(recentOrphanPayload),
	)
	if err != nil {
		t.Fatalf("write recent orphan: %v", err)
	}
	if err := os.Chtimes(recentOrphan, recent, recent); err != nil {
		t.Fatalf("age recent orphan: %v", err)
	}

	if err := store.Maintain(now); err != nil {
		t.Fatalf("maintain atomic temps: %v", err)
	}
	for _, path := range []string{oldTransactionTemp, oldEnvelopeTemp} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("old Zen atomic temp retained at %s: %v", path, err)
		}
	}
	for _, path := range []string{
		recentTransactionTemp,
		recentEnvelopeTemp,
		foreignTransaction,
		foreignEnvelope,
		corruptRecord,
		sharedEnvelope,
		recentOrphan,
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("required retained state missing at %s: %v", path, err)
		}
	}
	records, err := store.Active(active.SessionID, active.SessionGeneration)
	if err != nil || len(records) != 1 || records[0].TransactionID != active.TransactionID {
		t.Fatalf("unaffected Active records=%#v err=%v", records, err)
	}
}

func codexJournalSessionMeta(now time.Time, sessionID string) map[string]any {
	return map[string]any{
		"timestamp": now.Format(time.RFC3339Nano),
		"type":      "session_meta",
		"payload": map[string]any{
			"id": sessionID,
		},
	}
}

func writeCodexJournalTestRows(t *testing.T, path string, rows []map[string]any) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open rollout: %v", err)
	}
	encoder := json.NewEncoder(file)
	for _, row := range rows {
		if err := encoder.Encode(row); err != nil {
			_ = file.Close()
			t.Fatalf("encode rollout: %v", err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close rollout: %v", err)
	}
}
