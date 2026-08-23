package lifecycle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const lifecycleStoreSchema = 1

// lifecycleDatabase is the single durable transaction image. Current rows and
// append-only Events are replaced together, so recovery never replays a second
// log to repair a separate projection.
type lifecycleDatabase struct {
	Schema  int               `json:"schema"`
	NextSeq uint64            `json:"next_seq"`
	Works   map[string]*State `json:"works"`
	Events  []Event           `json:"events"`
}

func readLifecycleDatabase(path string) (lifecycleDatabase, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return lifecycleDatabase{Schema: lifecycleStoreSchema, NextSeq: 1, Works: map[string]*State{}}, nil
	}
	if err != nil {
		return lifecycleDatabase{}, err
	}
	database := lifecycleDatabase{}
	if err := json.Unmarshal(raw, &database); err != nil {
		return lifecycleDatabase{}, fmt.Errorf("decode lifecycle store: %w", err)
	}
	if database.Schema != lifecycleStoreSchema {
		return lifecycleDatabase{}, fmt.Errorf("lifecycle: unsupported store schema %d", database.Schema)
	}
	if database.Works == nil || database.Events == nil {
		return lifecycleDatabase{}, fmt.Errorf("lifecycle: works and events are required")
	}
	var maxSeq uint64
	for _, event := range database.Events {
		if event.Seq <= maxSeq {
			return lifecycleDatabase{}, fmt.Errorf("lifecycle: event sequence is not strictly increasing")
		}
		maxSeq = event.Seq
	}
	if database.NextSeq <= maxSeq {
		return lifecycleDatabase{}, fmt.Errorf("lifecycle: next event sequence is stale")
	}
	if database.NextSeq == 0 {
		database.NextSeq = 1
	}
	// Current Work rows are canonical. Development builds could append an
	// already-seen source even though Reduce correctly rejected it, producing a
	// duplicate audit storm. Compact those rejected records in memory without
	// replaying Events or making startup depend on a repair write. The next
	// successful lifecycle transaction persists this canonical image.
	database.Events = compactDuplicateSourceEvents(database.Events)
	for id, work := range database.Works {
		if work == nil || work.ID != WorkID(id) {
			return lifecycleDatabase{}, fmt.Errorf("lifecycle: Work row %q has mismatched identity", id)
		}
		if work.Attempt != nil && (work.Attempt.SessionID == "" || work.Attempt.TurnToken == "" || work.Attempt.Generation == 0) {
			return lifecycleDatabase{}, fmt.Errorf("lifecycle: Work %q has incomplete active Attempt identity", id)
		}
		for _, admission := range work.Admissions {
			if admission == nil || admission.TurnToken == "" || admission.SessionID == "" || admission.AttemptedAt.IsZero() {
				return lifecycleDatabase{}, fmt.Errorf("lifecycle: Work %q has incomplete Attempt admission", id)
			}
		}
		for _, attempt := range work.Attempts {
			if attempt == nil || attempt.SessionID == "" || attempt.Token == "" || attempt.Generation == 0 {
				return lifecycleDatabase{}, fmt.Errorf("lifecycle: Work %q has incomplete Attempt history identity", id)
			}
		}
	}
	return database, nil
}

func compactDuplicateSourceEvents(events []Event) []Event {
	seen := make(map[WorkID]map[string]bool)
	compacted := make([]Event, 0, len(events))
	for _, event := range events {
		if event.SourceID != "" {
			workSources := seen[event.WorkID]
			if workSources == nil {
				workSources = make(map[string]bool)
				seen[event.WorkID] = workSources
			}
			if workSources[event.SourceID] {
				continue
			}
			workSources[event.SourceID] = true
		}
		compacted = append(compacted, event)
	}
	return compacted
}

func writeLifecycleDatabase(path string, database lifecycleDatabase) error {
	database.Schema = lifecycleStoreSchema
	if database.NextSeq == 0 {
		database.NextSeq = 1
	}
	raw, err := json.Marshal(database)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return syncDir(dir)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// Protocol timing constants. Fixed so lifecycle decisions are deterministic.
const (
	LeaseGrace          = 10 * time.Minute
	LostGrace           = 30 * time.Minute
	MaxConsecutiveLost  = 3
	MaxDispatchAttempts = 5
	EventClaimTTL       = 2 * time.Minute
	RetryBaseDelay      = 5 * time.Second
	RetryMaxDelay       = 5 * time.Minute
)

const (
	DispatchSuccess           = "success"
	DispatchRetryable         = "retryable"
	DispatchUnknownSideEffect = "unknown_side_effect"
	DispatchTerminal          = "terminal"
)

func dispatchBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := RetryBaseDelay
	for i := 1; i < attempt && delay < RetryMaxDelay; i++ {
		delay *= 2
	}
	if delay > RetryMaxDelay {
		return RetryMaxDelay
	}
	return delay
}
