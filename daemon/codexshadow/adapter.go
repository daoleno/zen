package codexshadow

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/daoleno/zen/daemon/chatthread"
)

type codexRecordKind string

const (
	recordActivityStarted  codexRecordKind = "activity_started"
	recordInputEcho        codexRecordKind = "input_echo"
	recordInputAdmitted    codexRecordKind = "input_admitted"
	recordAssistantEcho    codexRecordKind = "assistant_echo"
	recordAssistant        codexRecordKind = "assistant"
	recordReasoning        codexRecordKind = "reasoning"
	recordToolStarted      codexRecordKind = "tool_started"
	recordToolTerminal     codexRecordKind = "tool_terminal"
	recordPlan             codexRecordKind = "plan"
	recordStatus           codexRecordKind = "status"
	recordActivityTerminal codexRecordKind = "activity_terminal"
)

type codexRecord struct {
	Key              chatthread.ProviderFactKey
	Cursor           uint64
	Kind             codexRecordKind
	NativeActivityID string
	NativeItemID     string
	NativeCallID     string
	EventFinal       bool
	TerminalState    chatthread.ActivityState
}

type codexRecordEnvelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type codexRecordPayload struct {
	Type   string `json:"type"`
	TurnID string `json:"turn_id"`
	ID     string `json:"id"`
	Role   string `json:"role"`
	CallID string `json:"call_id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type codexRecordReader func(
	ctx context.Context,
	path string,
	expectedSessionID string,
	startCursor uint64,
) (sessionID string, records []codexRecord, endCursor uint64, err error)

const maxSessionMetaProbeBytes = 1 << 20

func readCodexRecords(path, fallbackSessionID string) (string, []codexRecord, uint64, error) {
	return readCodexRecordsIncremental(context.Background(), path, fallbackSessionID, 0)
}

// readCodexRecordsIncremental verifies the rollout's provider-native session
// identity from a bounded prefix, then reads only bytes after the last durable
// complete-record boundary. startCursor and endCursor are zero-based byte
// offsets between records; ProviderFact cursors remain one-based record starts.
func readCodexRecordsIncremental(
	ctx context.Context,
	path string,
	expectedSessionID string,
	startCursor uint64,
) (string, []codexRecord, uint64, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, 0, err
	}
	if strings.TrimSpace(path) == "" {
		return "", nil, 0, fmt.Errorf("%w", ErrSource)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", nil, 0, fmt.Errorf("%w", ErrSource)
	}
	defer file.Close()
	sessionID, err := readCodexSessionIdentity(ctx, file, expectedSessionID)
	if err != nil {
		return "", nil, 0, err
	}
	info, err := file.Stat()
	if err != nil {
		return "", nil, 0, fmt.Errorf("%w", ErrSource)
	}
	if !info.Mode().IsRegular() || info.Size() < 0 {
		return "", nil, 0, fmt.Errorf("%w", ErrSource)
	}
	if startCursor > uint64(info.Size()) {
		return "", nil, 0, fmt.Errorf("%w: source truncated before durable cursor", ErrAdapterGap)
	}
	if _, err := file.Seek(int64(startCursor), io.SeekStart); err != nil {
		return "", nil, 0, fmt.Errorf("%w", ErrSource)
	}
	return readCodexRecordWindow(ctx, file, startCursor, sessionID)
}

func readCodexSessionIdentity(ctx context.Context, file *os.File, expectedSessionID string) (string, error) {
	if file == nil {
		return "", fmt.Errorf("%w", ErrSource)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("%w", ErrSource)
	}
	buffered := bufio.NewReader(io.LimitReader(file, maxSessionMetaProbeBytes+1))
	var consumed int
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		line, readErr := buffered.ReadBytes('\n')
		consumed += len(line)
		if consumed > maxSessionMetaProbeBytes {
			return "", fmt.Errorf("%w: session metadata exceeds bounded prefix", ErrSourceMalformed)
		}
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			var envelope codexRecordEnvelope
			if err := json.Unmarshal(trimmed, &envelope); err != nil || envelope.Type != "session_meta" {
				return "", fmt.Errorf("%w", ErrSourceIdentity)
			}
			var meta struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(envelope.Payload, &meta) != nil || strings.TrimSpace(meta.ID) == "" {
				return "", fmt.Errorf("%w", ErrSourceIdentity)
			}
			sessionID := strings.TrimSpace(meta.ID)
			expectedSessionID = strings.TrimSpace(expectedSessionID)
			if expectedSessionID != "" && sessionID != expectedSessionID {
				return "", fmt.Errorf("%w", ErrSourceIdentity)
			}
			return sessionID, nil
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return "", fmt.Errorf("%w", ErrSourceIdentity)
			}
			return "", fmt.Errorf("%w", ErrSource)
		}
	}
}

func readCodexRecordWindow(
	ctx context.Context,
	reader io.Reader,
	baseOffset uint64,
	fallbackSessionID string,
) (string, []codexRecord, uint64, error) {
	buffered := bufio.NewReader(reader)
	offset := baseOffset
	completeOffset := baseOffset
	sessionID := strings.TrimSpace(fallbackSessionID)
	var records []codexRecord
	for {
		if err := ctx.Err(); err != nil {
			return "", nil, completeOffset, err
		}
		lineStart := offset
		line, readErr := buffered.ReadBytes('\n')
		offset += uint64(len(line))
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			var envelope codexRecordEnvelope
			if err := json.Unmarshal(trimmed, &envelope); err != nil {
				if errors.Is(readErr, io.EOF) {
					break
				}
				return "", nil, completeOffset, fmt.Errorf("%w: invalid complete record", ErrSourceMalformed)
			}
			if envelope.Type == "session_meta" {
				var meta struct {
					ID string `json:"id"`
				}
				if json.Unmarshal(envelope.Payload, &meta) == nil {
					metaID := strings.TrimSpace(meta.ID)
					if metaID != "" && sessionID != "" && metaID != sessionID {
						return "", nil, offset, fmt.Errorf("%w", ErrSourceIdentity)
					}
					if metaID != "" {
						sessionID = metaID
					}
				}
			} else if record, ok := parseCodexRecord(envelope, lineStart+1); ok {
				records = append(records, record)
			}
		}
		if readErr == nil || (errors.Is(readErr, io.EOF) && len(trimmed) > 0) {
			completeOffset = offset
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return "", nil, completeOffset, fmt.Errorf("%w", ErrSource)
		}
	}
	if sessionID == "" {
		return "", nil, completeOffset, fmt.Errorf("%w", ErrSourceIdentity)
	}
	for index := range records {
		records[index].Key = recordKey(sessionID, records[index].Cursor)
	}
	return sessionID, records, completeOffset, nil
}

func parseCodexRecord(envelope codexRecordEnvelope, cursor uint64) (codexRecord, bool) {
	var payload codexRecordPayload
	if json.Unmarshal(envelope.Payload, &payload) != nil {
		return codexRecord{}, false
	}
	payload.Type = strings.TrimSpace(payload.Type)
	payload.TurnID = strings.TrimSpace(payload.TurnID)
	payload.ID = strings.TrimSpace(payload.ID)
	payload.Role = strings.ToLower(strings.TrimSpace(payload.Role))
	payload.CallID = strings.TrimSpace(payload.CallID)
	payload.Name = strings.TrimSpace(payload.Name)
	payload.Status = strings.ToLower(strings.TrimSpace(payload.Status))
	record := codexRecord{
		Cursor:           cursor,
		NativeActivityID: payload.TurnID,
		NativeItemID:     payload.ID,
		NativeCallID:     payload.CallID,
	}

	switch envelope.Type {
	case "event_msg":
		switch payload.Type {
		case "task_started", "turn_started":
			record.Kind = recordActivityStarted
		case "user_message":
			record.Kind = recordInputAdmitted
		case "agent_message":
			// The following response_item/message carries the stable item ID.
			record.Kind = recordAssistantEcho
		case "task_complete", "turn_complete":
			record.Kind = recordActivityTerminal
			record.TerminalState = chatthread.ActivityCompleted
		case "turn_aborted":
			// Abortion is structural; free-form reason text is intentionally ignored.
			record.Kind = recordActivityTerminal
			record.TerminalState = chatthread.ActivityInterrupted
		case "plan_update":
			record.Kind = recordPlan
			record.EventFinal = true
		case "exec_command_end", "patch_apply_end", "web_search_end":
			record.Kind = recordToolTerminal
			record.EventFinal = true
		case "warning", "guardian_warning", "context_compacted", "thread_goal_updated", "error", "stream_error":
			record.Kind = recordStatus
			record.EventFinal = true
		default:
			return codexRecord{}, false
		}
	case "response_item":
		switch payload.Type {
		case "message":
			switch payload.Role {
			case "user":
				// Rendering echo only. Input admission is event_msg/user_message.
				record.Kind = recordInputEcho
			case "assistant":
				record.Kind = recordAssistant
				record.EventFinal = true
			default:
				return codexRecord{}, false
			}
		case "reasoning":
			record.Kind = recordReasoning
			record.EventFinal = true
		case "function_call", "custom_tool_call", "web_search_call":
			record.Kind = recordToolStarted
		case "function_call_output", "custom_tool_call_output":
			record.Kind = recordToolTerminal
			record.EventFinal = true
		default:
			return codexRecord{}, false
		}
	default:
		return codexRecord{}, false
	}
	return record, true
}

type adapterProjection struct {
	currentExecution chatthread.ExecutionID
	currentCause     chatthread.SubmissionID
	inputCount       chatthread.InputOrdinal
	eventCauses      map[chatthread.EventID]chatthread.SubmissionID
}

func (reader *Reader) observeRecords(
	ctx context.Context,
	observation Observation,
	prior chatthread.ShadowSnapshot,
	sessionID string,
	records []codexRecord,
	endCursor uint64,
) (chatthread.ShadowSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return chatthread.ShadowSnapshot{}, err
	}
	ownerKey := strings.TrimSpace(observation.OwnerKey)
	if ownerKey == "" || strings.TrimSpace(sessionID) == "" {
		return chatthread.ShadowSnapshot{}, fmt.Errorf("%w", ErrSourceIdentity)
	}
	threadID := shadowThreadID(ownerKey, sessionID)
	sourceToken := sourceToken(ownerKey, sessionID)
	if prior.SourceToken != "" && prior.SourceToken != sourceToken {
		return prior, fmt.Errorf("%w", ErrSourceIdentity)
	}
	if endCursor < prior.SourceCursor {
		return prior, fmt.Errorf("%w: source ended before durable cursor", ErrAdapterGap)
	}
	legacyFingerprint, err := legacyProjectionFingerprint(observation.Legacy)
	if err != nil {
		return prior, err
	}
	if len(records) == 0 && endCursor == prior.SourceCursor &&
		reader.legacyFingerprintMatches(threadID, legacyFingerprint) {
		return prior, nil
	}

	applied := make(map[chatthread.ProviderFactKey]chatthread.AppliedShadowRecord, len(prior.AppliedRecords))
	var maxCursor uint64
	for _, record := range prior.AppliedRecords {
		applied[record.Key] = record
		if record.Cursor > maxCursor {
			maxCursor = record.Cursor
		}
	}
	if maxCursor > 0 && endCursor < maxCursor {
		return prior, fmt.Errorf("%w: source ended before durable cursor", ErrAdapterGap)
	}

	projection := projectionFromSnapshot(prior.Thread)
	gaps := gapsFromSnapshot(prior.Thread)
	batchRecords := make([]chatthread.ShadowRecord, 0, len(records))
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return prior, err
		}
		fingerprint, err := recordFingerprint(record)
		if err != nil {
			return prior, err
		}
		shadowRecord := chatthread.ShadowRecord{
			Key:         record.Key,
			Cursor:      record.Cursor,
			Fingerprint: fingerprint,
		}
		if existing, ok := applied[record.Key]; ok {
			if existing.Cursor != record.Cursor || existing.Fingerprint != fingerprint {
				return prior, fmt.Errorf("%w", chatthread.ErrShadowRecordConflict)
			}
			batchRecords = append(batchRecords, shadowRecord)
			continue
		}
		if maxCursor > 0 && record.Cursor <= maxCursor {
			return prior, fmt.Errorf("%w", chatthread.ErrShadowRecordGap)
		}

		operations, recordGaps, err := normalizeRecord(sessionID, record, &projection)
		if err != nil {
			return prior, err
		}
		shadowRecord.Operations = operations
		batchRecords = append(batchRecords, shadowRecord)
		gaps = append(gaps, recordGaps...)
		maxCursor = record.Cursor
	}

	snapshot, err := reader.sink.ApplyShadowBatch(chatthread.ShadowBatch{
		ThreadID:        threadID,
		SourceToken:     sourceToken,
		SourceCursor:    endCursor,
		Records:         batchRecords,
		Legacy:          observation.Legacy,
		CorrelationGaps: dedupeGaps(gaps),
	})
	if err != nil {
		return snapshot, err
	}
	reader.rememberLegacyFingerprint(threadID, legacyFingerprint)
	return snapshot, nil
}

func projectionFromSnapshot(thread chatthread.Thread) adapterProjection {
	projection := adapterProjection{
		currentExecution: thread.CurrentExecutionID,
		eventCauses:      make(map[chatthread.EventID]chatthread.SubmissionID, len(thread.Events)),
	}
	for _, activity := range thread.ExecutionActivities {
		if activity.ID == projection.currentExecution {
			projection.inputCount = activity.InputCount
		}
	}
	for _, submission := range thread.Submissions {
		if submission.ExecutionID == projection.currentExecution && submission.InputOrdinal == projection.inputCount {
			projection.currentCause = submission.ID
		}
	}
	for _, event := range thread.Events {
		projection.eventCauses[event.ID] = event.CausalSubmissionID
	}
	return projection
}

func gapsFromSnapshot(thread chatthread.Thread) []chatthread.ShadowCorrelationGap {
	gaps := make([]chatthread.ShadowCorrelationGap, 0, len(thread.Submissions))
	for _, submission := range thread.Submissions {
		if submission.Origin != chatthread.OriginProviderExternal || submission.AdmissionFactKey == "" {
			continue
		}
		gaps = append(gaps, chatthread.ShadowCorrelationGap{
			SubmissionID: submission.ID,
			RecordKey:    submission.AdmissionFactKey,
			Reason:       chatthread.CorrelationGapNoExplicitAppBinding,
		})
	}
	return gaps
}

func normalizeRecord(
	sessionID string,
	record codexRecord,
	projection *adapterProjection,
) ([]chatthread.ShadowOperation, []chatthread.ShadowCorrelationGap, error) {
	if projection == nil {
		return nil, nil, fmt.Errorf("%w: nil adapter projection", chatthread.ErrInvalidArgument)
	}
	switch record.Kind {
	case recordActivityStarted:
		executionID := executionIDForRecord(sessionID, record)
		if projection.currentExecution != "" {
			return nil, nil, fmt.Errorf("%w: activity started while another is current", ErrAdapterGap)
		}
		projection.currentExecution = executionID
		projection.currentCause = ""
		projection.inputCount = 0
		return []chatthread.ShadowOperation{
			chatthread.ProviderFactObserved{Fact: chatthread.ActivityStartedFact{
				Key:         record.Key,
				ExecutionID: executionID,
			}},
		}, nil, nil
	case recordInputEcho, recordAssistantEcho:
		return nil, nil, nil
	case recordInputAdmitted:
		if projection.currentExecution == "" {
			return nil, nil, fmt.Errorf("%w: input has no structured activity start", ErrAdapterGap)
		}
		if projection.inputCount == ^chatthread.InputOrdinal(0) {
			return nil, nil, fmt.Errorf("%w: input ordinal overflow", ErrAdapterGap)
		}
		submissionID := submissionIDForRecord(sessionID, record)
		projection.inputCount++
		projection.currentCause = submissionID
		return []chatthread.ShadowOperation{
				chatthread.ProviderExternalSubmissionObserved{SubmissionID: submissionID},
				chatthread.ProviderFactObserved{Fact: chatthread.InputAdmittedFact{
					Key:          record.Key,
					ExecutionID:  projection.currentExecution,
					SubmissionID: submissionID,
					Ordinal:      projection.inputCount,
				}},
			}, []chatthread.ShadowCorrelationGap{{
				SubmissionID: submissionID,
				RecordKey:    record.Key,
				Reason:       chatthread.CorrelationGapNoExplicitAppBinding,
			}}, nil
	case recordAssistant, recordReasoning, recordPlan, recordStatus, recordToolStarted, recordToolTerminal:
		if projection.currentExecution == "" || projection.currentCause == "" {
			return nil, nil, fmt.Errorf("%w: event has no structured causal input", ErrAdapterGap)
		}
		eventID := eventIDForRecord(sessionID, record)
		cause := projection.currentCause
		var gaps []chatthread.ShadowCorrelationGap
		if existingCause := projection.eventCauses[eventID]; existingCause != "" {
			cause = existingCause
		} else if record.Kind == recordToolTerminal {
			gaps = append(gaps, chatthread.ShadowCorrelationGap{
				SubmissionID: cause,
				RecordKey:    record.Key,
				Reason:       chatthread.CorrelationGapMissingToolStart,
			})
		}
		projection.eventCauses[eventID] = cause
		return []chatthread.ShadowOperation{
			chatthread.ProviderFactObserved{Fact: chatthread.EventUpsertFact{
				Key:                record.Key,
				EventID:            eventID,
				ExecutionID:        projection.currentExecution,
				CausalSubmissionID: cause,
				Kind:               eventKind(record.Kind),
				Final:              record.EventFinal,
				Payload:            "",
			}},
		}, gaps, nil
	case recordActivityTerminal:
		if projection.currentExecution == "" {
			return nil, nil, fmt.Errorf("%w: terminal has no current activity", ErrAdapterGap)
		}
		executionID := projection.currentExecution
		if record.NativeActivityID != "" {
			explicitID := executionIDForRecord(sessionID, record)
			if explicitID != executionID {
				return nil, nil, fmt.Errorf("%w: terminal activity identity mismatch", ErrAdapterGap)
			}
		}
		projection.currentExecution = ""
		projection.currentCause = ""
		projection.inputCount = 0
		return []chatthread.ShadowOperation{
			chatthread.ProviderFactObserved{Fact: chatthread.ActivityTerminalFact{
				Key:           record.Key,
				ExecutionID:   executionID,
				TerminalState: record.TerminalState,
				Reason:        "",
			}},
		}, nil, nil
	default:
		return nil, nil, fmt.Errorf("%w: unsupported record kind", chatthread.ErrInvalidArgument)
	}
}

func eventKind(kind codexRecordKind) chatthread.EventKind {
	switch kind {
	case recordAssistant:
		return chatthread.EventAssistant
	case recordToolStarted, recordToolTerminal:
		return chatthread.EventTool
	case recordPlan:
		return chatthread.EventPlan
	default:
		return chatthread.EventStatus
	}
}

func recordFingerprint(record codexRecord) (string, error) {
	payload := struct {
		Cursor           uint64                   `json:"cursor"`
		Kind             codexRecordKind          `json:"kind"`
		NativeActivityID string                   `json:"native_activity_id,omitempty"`
		NativeItemID     string                   `json:"native_item_id,omitempty"`
		NativeCallID     string                   `json:"native_call_id,omitempty"`
		EventFinal       bool                     `json:"event_final"`
		TerminalState    chatthread.ActivityState `json:"terminal_state,omitempty"`
	}{
		Cursor:           record.Cursor,
		Kind:             record.Kind,
		NativeActivityID: record.NativeActivityID,
		NativeItemID:     record.NativeItemID,
		NativeCallID:     record.NativeCallID,
		EventFinal:       record.EventFinal,
		TerminalState:    record.TerminalState,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func legacyProjectionFingerprint(projection chatthread.LegacyShadowProjection) ([sha256.Size]byte, error) {
	raw, err := json.Marshal(projection)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(raw), nil
}

func recordKey(sessionID string, cursor uint64) chatthread.ProviderFactKey {
	return chatthread.ProviderFactKey("codex-record-" + stableHash("record", sessionID, strconv.FormatUint(cursor, 10)))
}

func shadowThreadID(ownerKey, sessionID string) chatthread.ThreadID {
	return chatthread.ThreadID("codex-shadow-thread-" + stableHash("thread", ownerKey, sessionID))
}

func sourceToken(ownerKey, sessionID string) string {
	return "tok_" + stableHash("source", ownerKey, sessionID)
}

func executionIDForRecord(sessionID string, record codexRecord) chatthread.ExecutionID {
	native := record.NativeActivityID
	if native == "" {
		native = string(record.Key)
	}
	return chatthread.ExecutionID("codex-activity-" + stableHash("activity", sessionID, native))
}

func submissionIDForRecord(sessionID string, record codexRecord) chatthread.SubmissionID {
	return chatthread.SubmissionID("codex-submission-" + stableHash("submission", sessionID, string(record.Key)))
}

func eventIDForRecord(sessionID string, record codexRecord) chatthread.EventID {
	nativeKind := "record"
	native := string(record.Key)
	if record.NativeCallID != "" {
		nativeKind = "call"
		native = record.NativeCallID
	} else if record.NativeItemID != "" {
		nativeKind = "item"
		native = record.NativeItemID
	}
	return chatthread.EventID("codex-event-" + stableHash("event", sessionID, nativeKind, native))
}

func stableHash(kind string, parts ...string) string {
	hash := sha256.New()
	_, _ = io.WriteString(hash, kind)
	for _, part := range parts {
		_, _ = io.WriteString(hash, "\x00")
		_, _ = io.WriteString(hash, part)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func dedupeGaps(gaps []chatthread.ShadowCorrelationGap) []chatthread.ShadowCorrelationGap {
	type gapKey struct {
		submission chatthread.SubmissionID
		record     chatthread.ProviderFactKey
		reason     string
	}
	seen := make(map[gapKey]struct{}, len(gaps))
	out := make([]chatthread.ShadowCorrelationGap, 0, len(gaps))
	for _, gap := range gaps {
		key := gapKey{submission: gap.SubmissionID, record: gap.RecordKey, reason: gap.Reason}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, gap)
	}
	sort.Slice(out, func(left, right int) bool {
		if out[left].SubmissionID != out[right].SubmissionID {
			return out[left].SubmissionID < out[right].SubmissionID
		}
		if out[left].RecordKey != out[right].RecordKey {
			return out[left].RecordKey < out[right].RecordKey
		}
		return out[left].Reason < out[right].Reason
	})
	return out
}
