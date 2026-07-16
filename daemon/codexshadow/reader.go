// Package codexshadow adapts append-only Codex rollout records into the
// provider-neutral canonical Chat fact model for read-only diagnostics.
package codexshadow

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/daoleno/zen/daemon/chatthread"
)

const (
	EnvScopes = "ZEN_CODEX_CHATTHREAD_SHADOW_SCOPES"
	EnvRoot   = "ZEN_CODEX_CHATTHREAD_SHADOW_ROOT"
)

var (
	ErrDisabled        = errors.New("Codex Chat shadow diagnostics are disabled")
	ErrSource          = errors.New("Codex structured shadow source is unavailable")
	ErrSourceMalformed = errors.New("Codex structured shadow source is malformed")
	ErrSourceIdentity  = errors.New("Codex structured shadow source identity changed")
	ErrAdapterGap      = errors.New("Codex structured shadow adapter has a chronology gap")
)

// FactSink is deliberately narrower than chatthread.Ledger. A Reader can only
// inspect a shadow snapshot and apply content-free observations/facts. There is
// no dispatch, executor, tmux, terminal, or provider-input capability.
type FactSink interface {
	ApplyShadowBatch(chatthread.ShadowBatch) (chatthread.ShadowSnapshot, error)
	ShadowSnapshot(chatthread.ThreadID) (chatthread.ShadowSnapshot, error)
}

type Observation struct {
	OwnerKey    string
	RolloutPath string
	SessionID   string
	Legacy      chatthread.LegacyShadowProjection
}

type Reader struct {
	mu                 sync.Mutex
	observeMu          sync.Mutex
	sink               FactSink
	readRecords        codexRecordReader
	scopes             map[string]struct{}
	legacyFingerprints map[chatthread.ThreadID][sha256.Size]byte
	allScopes          bool
	disabled           bool
}

func NewReader(sink FactSink, scopes []string) (*Reader, error) {
	if sink == nil {
		return nil, fmt.Errorf("%w: shadow fact sink is required", chatthread.ErrInvalidArgument)
	}
	reader := &Reader{
		sink:               sink,
		readRecords:        readCodexRecordsIncremental,
		scopes:             make(map[string]struct{}),
		legacyFingerprints: make(map[chatthread.ThreadID][sha256.Size]byte),
	}
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if scope == "*" {
			reader.allScopes = true
			continue
		}
		reader.scopes[scope] = struct{}{}
	}
	if !reader.allScopes && len(reader.scopes) == 0 {
		return nil, fmt.Errorf("%w: at least one explicit shadow scope is required", chatthread.ErrInvalidArgument)
	}
	return reader, nil
}

// OpenConfigured is default-off. It returns (nil, nil) unless EnvScopes is
// explicitly nonempty. Enabling never creates state: the isolated shadow store
// must already have been initialized deliberately.
func OpenConfigured(stateDir string, lookupEnv func(string) string) (*Reader, error) {
	if lookupEnv == nil {
		lookupEnv = os.Getenv
	}
	rawScopes := strings.TrimSpace(lookupEnv(EnvScopes))
	if rawScopes == "" {
		return nil, nil
	}
	root := strings.TrimSpace(lookupEnv(EnvRoot))
	if root == "" {
		if strings.TrimSpace(stateDir) == "" {
			return nil, fmt.Errorf("%w: state root unavailable", chatthread.ErrShadowNotInitialized)
		}
		root = filepath.Join(stateDir, "diagnostics", "codex-chatthread-shadow")
	}
	store, err := chatthread.OpenShadowStore(root)
	if err != nil {
		return nil, err
	}
	return NewReader(store, splitScopes(rawScopes))
}

func splitScopes(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func (reader *Reader) Enabled(ownerKey string) bool {
	if reader == nil {
		return false
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.disabled {
		return false
	}
	if reader.allScopes {
		return true
	}
	_, ok := reader.scopes[strings.TrimSpace(ownerKey)]
	return ok
}

func (reader *Reader) disable() {
	if reader == nil {
		return
	}
	reader.mu.Lock()
	reader.disabled = true
	reader.mu.Unlock()
}

func (reader *Reader) ObserveRollout(ctx context.Context, observation Observation) (chatthread.ShadowSnapshot, error) {
	if reader == nil || !reader.Enabled(observation.OwnerKey) {
		return chatthread.ShadowSnapshot{}, ErrDisabled
	}
	reader.observeMu.Lock()
	defer reader.observeMu.Unlock()
	if !reader.Enabled(observation.OwnerKey) {
		return chatthread.ShadowSnapshot{}, ErrDisabled
	}
	if err := ctx.Err(); err != nil {
		return chatthread.ShadowSnapshot{}, err
	}
	ownerKey := strings.TrimSpace(observation.OwnerKey)
	expectedSessionID := strings.TrimSpace(observation.SessionID)
	if ownerKey == "" || expectedSessionID == "" {
		reader.disable()
		return chatthread.ShadowSnapshot{}, fmt.Errorf("%w", ErrSourceIdentity)
	}
	prior, err := reader.sink.ShadowSnapshot(shadowThreadID(ownerKey, expectedSessionID))
	if err != nil && !errors.Is(err, chatthread.ErrThreadNotFound) {
		reader.disableUnlessCanceled(err)
		return chatthread.ShadowSnapshot{}, err
	}
	if errors.Is(err, chatthread.ErrThreadNotFound) {
		prior = chatthread.ShadowSnapshot{}
	}
	startCursor := prior.SourceCursor
	sessionID, records, endCursor, err := reader.readRecords(
		ctx,
		observation.RolloutPath,
		expectedSessionID,
		startCursor,
	)
	if err != nil {
		reader.disableUnlessCanceled(err)
		return chatthread.ShadowSnapshot{}, err
	}
	snapshot, err := reader.observeRecords(ctx, observation, prior, sessionID, records, endCursor)
	if err != nil {
		reader.disableUnlessCanceled(err)
		return snapshot, err
	}
	return snapshot, nil
}

func (reader *Reader) legacyFingerprintMatches(
	threadID chatthread.ThreadID,
	fingerprint [sha256.Size]byte,
) bool {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	prior, ok := reader.legacyFingerprints[threadID]
	return ok && prior == fingerprint
}

func (reader *Reader) rememberLegacyFingerprint(
	threadID chatthread.ThreadID,
	fingerprint [sha256.Size]byte,
) {
	reader.mu.Lock()
	reader.legacyFingerprints[threadID] = fingerprint
	reader.mu.Unlock()
}

func (reader *Reader) disableUnlessCanceled(err error) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	reader.disable()
}

func ErrorCode(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, chatthread.ErrShadowNotInitialized):
		return "state_not_initialized"
	case errors.Is(err, chatthread.ErrShadowCorrupt):
		return "state_corrupt"
	case errors.Is(err, chatthread.ErrShadowSchema):
		return "state_schema"
	case errors.Is(err, chatthread.ErrShadowUnavailable):
		return "state_unavailable"
	case errors.Is(err, chatthread.ErrShadowRecordConflict):
		return "record_conflict"
	case errors.Is(err, chatthread.ErrShadowRecordGap), errors.Is(err, ErrAdapterGap):
		return "record_gap"
	case errors.Is(err, ErrSourceIdentity):
		return "source_identity"
	case errors.Is(err, ErrSourceMalformed):
		return "source_malformed"
	case errors.Is(err, ErrSource):
		return "source_unavailable"
	case errors.Is(err, ErrDisabled):
		return "disabled"
	default:
		return "adapter_error"
	}
}
