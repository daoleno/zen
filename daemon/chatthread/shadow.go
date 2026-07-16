package chatthread

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrShadowNotInitialized     = errors.New("canonical Chat shadow diagnostics are not initialized")
	ErrShadowAlreadyInitialized = errors.New("canonical Chat shadow diagnostics are already initialized")
	ErrShadowCorrupt            = errors.New("canonical Chat shadow diagnostics are corrupt")
	ErrShadowSchema             = errors.New("unsupported canonical Chat shadow diagnostics schema")
	ErrShadowUnavailable        = errors.New("canonical Chat shadow diagnostics are unavailable")
	ErrShadowOwnership          = errors.New("canonical Chat shadow ownership conflict")
	ErrShadowRecordConflict     = errors.New("canonical Chat shadow record conflicts with prior structure")
	ErrShadowRecordGap          = errors.New("canonical Chat shadow record arrived behind the durable cursor")
)

// ShadowOwnershipV1ReadOnly is deliberately separate from both durable Thread
// ownership values. A shadow Thread diagnoses a legacy-v1-owned scope; it never
// opts that scope into v2 dispatch or v2 public projection.
const ShadowOwnershipV1ReadOnly = "legacy_v1_read_only_shadow"

type ShadowOperation interface {
	shadowOperation()
}

// ProviderExternalSubmissionObserved creates one content-free Submission from
// a stable provider record identity. Shadow callers cannot pass a body,
// attachments, writer metadata, a dispatch attempt, or any provider effect.
type ProviderExternalSubmissionObserved struct {
	SubmissionID SubmissionID
}

func (ProviderExternalSubmissionObserved) shadowOperation() {}

// ProviderFactObserved is the only shadow route into the closed provider fact
// state machine. It deliberately exposes no Ledger, DispatchBoundary, executor,
// terminal, or provider-input capability.
type ProviderFactObserved struct {
	Fact ProviderFact
}

func (ProviderFactObserved) shadowOperation() {}

// ShadowRecord is one sanitized, append-only provider record. Fingerprint must
// cover structural fields only; provider content must not participate.
type ShadowRecord struct {
	Key         ProviderFactKey
	Cursor      uint64
	Fingerprint string
	Operations  []ShadowOperation
}

type LegacyShadowTurn struct {
	ID    string
	State string
}

// LegacyShadowProjection is transient comparison input. ApplyShadowBatch hashes
// every ID before persistence and accepts only lifecycle enum values. No v1
// message, attachment, path, prompt, command, or tool payload is represented.
type LegacyShadowProjection struct {
	OrderedTurns  []LegacyShadowTurn
	Current       *LegacyShadowTurn
	Queued        []LegacyShadowTurn
	TerminalState string
}

type ShadowCorrelationGap struct {
	SubmissionID SubmissionID
	RecordKey    ProviderFactKey
	Reason       string
}

const (
	CorrelationGapNoExplicitAppBinding = "no_explicit_app_binding"
	CorrelationGapMissingActivityStart = "missing_activity_start"
	CorrelationGapMissingCausalInput   = "missing_causal_input"
	CorrelationGapMissingToolStart     = "missing_tool_start"
)

// ShadowBatch is one transactional shadow update. ThreadID and SourceToken are
// deterministic sanitized tokens supplied by the provider adapter.
type ShadowBatch struct {
	ThreadID        ThreadID
	SourceToken     string
	SourceCursor    uint64
	Records         []ShadowRecord
	Legacy          LegacyShadowProjection
	CorrelationGaps []ShadowCorrelationGap
}

type ShadowComparisonState string

const (
	ShadowComparisonMatch      ShadowComparisonState = "match"
	ShadowComparisonDiverged   ShadowComparisonState = "diverged"
	ShadowComparisonUnprovable ShadowComparisonState = "unprovable"
)

type ShadowComparison struct {
	State       ShadowComparisonState `json:"state"`
	LegacyCount int                   `json:"legacy_count,omitempty"`
	ShadowCount int                   `json:"shadow_count,omitempty"`
	LegacyIDs   []string              `json:"legacy_ids"`
	ShadowIDs   []string              `json:"shadow_ids"`
	LegacyValue string                `json:"legacy_value,omitempty"`
	ShadowValue string                `json:"shadow_value,omitempty"`
}

type SanitizedCorrelationGap struct {
	SubmissionToken string `json:"submission_token"`
	RecordToken     string `json:"record_token"`
	Reason          string `json:"reason"`
}

// ShadowDiagnostics is the only persisted diagnostic projection. Every ID is
// a one-way stable token and every other string is a closed enum or digest.
type ShadowDiagnostics struct {
	ThreadToken        string                    `json:"thread_token"`
	SourceToken        string                    `json:"source_token"`
	CanonicalRevision  Revision                  `json:"canonical_revision"`
	CanonicalDigest    string                    `json:"canonical_digest"`
	Cardinality        ShadowComparison          `json:"cardinality"`
	Chronology         ShadowComparison          `json:"chronology"`
	CurrentActivity    ShadowComparison          `json:"current_activity"`
	Queue              ShadowComparison          `json:"queue"`
	TerminalSettlement ShadowComparison          `json:"terminal_settlement"`
	CorrelationGaps    []SanitizedCorrelationGap `json:"correlation_gaps"`
}

type AppliedShadowRecord struct {
	Key         ProviderFactKey
	Cursor      uint64
	Fingerprint string
}

type ShadowSnapshot struct {
	Ownership        string
	SourceToken      string
	SourceCursor     uint64
	Thread           Thread
	Digest           string
	ProviderFactKeys []ProviderFactKey
	AppliedRecords   []AppliedShadowRecord
	Diagnostics      ShadowDiagnostics
}

func structuralFingerprint(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func validStructuralFingerprint(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}

func buildShadowDiagnostics(
	thread Thread,
	sourceToken string,
	legacy LegacyShadowProjection,
	gaps []ShadowCorrelationGap,
) (ShadowDiagnostics, error) {
	if !validDiagnosticToken(sourceToken) {
		return ShadowDiagnostics{}, fmt.Errorf("%w: invalid shadow source token", ErrInvalidArgument)
	}
	if err := validateLegacyShadowProjection(legacy); err != nil {
		return ShadowDiagnostics{}, err
	}

	legacyOrderedIDs := make([]string, 0, len(legacy.OrderedTurns))
	for _, turn := range legacy.OrderedTurns {
		legacyOrderedIDs = append(legacyOrderedIDs, diagnosticToken("legacy-turn", turn.ID))
	}
	shadowOrderedIDs := make([]string, 0, len(thread.Submissions))
	for _, submission := range thread.Submissions {
		shadowOrderedIDs = append(shadowOrderedIDs, diagnosticToken("shadow-submission", string(submission.ID)))
	}

	sanitizedGaps := make([]SanitizedCorrelationGap, 0, len(gaps))
	for _, gap := range gaps {
		if !validCorrelationGapReason(gap.Reason) || !present(string(gap.SubmissionID)) || !present(string(gap.RecordKey)) {
			return ShadowDiagnostics{}, fmt.Errorf("%w: invalid shadow correlation gap", ErrInvalidArgument)
		}
		sanitizedGaps = append(sanitizedGaps, SanitizedCorrelationGap{
			SubmissionToken: diagnosticToken("gap-submission", string(gap.SubmissionID)),
			RecordToken:     diagnosticToken("gap-record", string(gap.RecordKey)),
			Reason:          gap.Reason,
		})
	}

	diagnostics := ShadowDiagnostics{
		ThreadToken:       diagnosticToken("shadow-thread", string(thread.ID)),
		SourceToken:       sourceToken,
		CanonicalRevision: thread.Revision,
		CanonicalDigest:   StateDigest(thread),
		Cardinality: ShadowComparison{
			State:       compareCounts(len(legacyOrderedIDs), len(shadowOrderedIDs)),
			LegacyCount: len(legacyOrderedIDs),
			ShadowCount: len(shadowOrderedIDs),
			LegacyIDs:   cloneStrings(legacyOrderedIDs),
			ShadowIDs:   cloneStrings(shadowOrderedIDs),
		},
		Chronology: ShadowComparison{
			State:       compareChronology(len(legacyOrderedIDs), len(shadowOrderedIDs), len(sanitizedGaps)),
			LegacyCount: len(legacyOrderedIDs),
			ShadowCount: len(shadowOrderedIDs),
			LegacyIDs:   cloneStrings(legacyOrderedIDs),
			ShadowIDs:   cloneStrings(shadowOrderedIDs),
		},
		CorrelationGaps: sanitizedGaps,
	}

	legacyCurrentID, legacyCurrentState := "", "none"
	if legacy.Current != nil {
		legacyCurrentID = diagnosticToken("legacy-current", legacy.Current.ID)
		legacyCurrentState = legacy.Current.State
	}
	shadowCurrentID, shadowCurrentState := "", "none"
	if thread.CurrentExecutionID != "" {
		shadowCurrentID = diagnosticToken("shadow-current", string(thread.CurrentExecutionID))
		shadowCurrentState = string(activityStateForID(thread, thread.CurrentExecutionID))
	}
	diagnostics.CurrentActivity = ShadowComparison{
		State:       compareCurrent(legacy.Current != nil, thread.CurrentExecutionID != "", legacyCurrentState, shadowCurrentState),
		LegacyCount: boolCount(legacy.Current != nil),
		ShadowCount: boolCount(thread.CurrentExecutionID != ""),
		LegacyIDs:   optionalDiagnosticID(legacyCurrentID),
		ShadowIDs:   optionalDiagnosticID(shadowCurrentID),
		LegacyValue: legacyCurrentState,
		ShadowValue: shadowCurrentState,
	}

	legacyQueueIDs := make([]string, 0, len(legacy.Queued))
	for _, turn := range legacy.Queued {
		legacyQueueIDs = append(legacyQueueIDs, diagnosticToken("legacy-queued", turn.ID))
	}
	shadowQueueIDs := make([]string, 0, len(thread.QueuedSubmissionIDs))
	for _, submissionID := range thread.QueuedSubmissionIDs {
		shadowQueueIDs = append(shadowQueueIDs, diagnosticToken("shadow-queued", string(submissionID)))
	}
	diagnostics.Queue = ShadowComparison{
		State:       compareQueue(len(legacyQueueIDs), len(shadowQueueIDs)),
		LegacyCount: len(legacyQueueIDs),
		ShadowCount: len(shadowQueueIDs),
		LegacyIDs:   legacyQueueIDs,
		ShadowIDs:   shadowQueueIDs,
	}

	shadowTerminal := terminalShadowState(thread)
	diagnostics.TerminalSettlement = ShadowComparison{
		State:       compareTerminal(legacy.TerminalState, shadowTerminal),
		LegacyCount: boolCount(legacy.TerminalState != ""),
		ShadowCount: boolCount(shadowTerminal != ""),
		LegacyIDs:   []string{},
		ShadowIDs:   []string{},
		LegacyValue: firstNonEmptyDiagnosticValue(legacy.TerminalState, "none"),
		ShadowValue: firstNonEmptyDiagnosticValue(shadowTerminal, "none"),
	}

	if err := validateShadowDiagnostics(diagnostics); err != nil {
		return ShadowDiagnostics{}, err
	}
	return diagnostics, nil
}

func compareCounts(legacy, shadow int) ShadowComparisonState {
	if legacy == shadow {
		return ShadowComparisonMatch
	}
	return ShadowComparisonDiverged
}

func compareChronology(legacy, shadow, gaps int) ShadowComparisonState {
	if legacy != shadow {
		return ShadowComparisonDiverged
	}
	if legacy == 0 && shadow == 0 {
		return ShadowComparisonMatch
	}
	if gaps > 0 {
		return ShadowComparisonUnprovable
	}
	return ShadowComparisonUnprovable
}

func compareCurrent(legacyPresent, shadowPresent bool, legacyState, shadowState string) ShadowComparisonState {
	if legacyPresent != shadowPresent || legacyState != shadowState {
		return ShadowComparisonDiverged
	}
	if !legacyPresent {
		return ShadowComparisonMatch
	}
	return ShadowComparisonUnprovable
}

func compareQueue(legacy, shadow int) ShadowComparisonState {
	if legacy != shadow {
		return ShadowComparisonDiverged
	}
	if legacy == 0 {
		return ShadowComparisonMatch
	}
	return ShadowComparisonUnprovable
}

func compareTerminal(legacy, shadow string) ShadowComparisonState {
	if legacy == shadow {
		return ShadowComparisonMatch
	}
	return ShadowComparisonDiverged
}

func validateLegacyShadowProjection(projection LegacyShadowProjection) error {
	validateTurn := func(turn LegacyShadowTurn) error {
		if !present(turn.ID) || !validLegacyShadowState(turn.State) {
			return fmt.Errorf("%w: invalid legacy shadow turn", ErrInvalidArgument)
		}
		return nil
	}
	for _, turn := range projection.OrderedTurns {
		if err := validateTurn(turn); err != nil {
			return err
		}
	}
	if projection.Current != nil {
		if err := validateTurn(*projection.Current); err != nil {
			return err
		}
	}
	for _, turn := range projection.Queued {
		if err := validateTurn(turn); err != nil {
			return err
		}
	}
	if projection.TerminalState != "" && !terminalDiagnosticState(projection.TerminalState) {
		return fmt.Errorf("%w: invalid legacy terminal state", ErrInvalidArgument)
	}
	return nil
}

func validateShadowDiagnostics(diagnostics ShadowDiagnostics) error {
	if !validDiagnosticToken(diagnostics.ThreadToken) || !validDiagnosticToken(diagnostics.SourceToken) ||
		!validDigest(diagnostics.CanonicalDigest) {
		return fmt.Errorf("%w: malformed sanitized shadow diagnostics", ErrInvariant)
	}
	for _, comparison := range []ShadowComparison{
		diagnostics.Cardinality,
		diagnostics.Chronology,
		diagnostics.CurrentActivity,
		diagnostics.Queue,
		diagnostics.TerminalSettlement,
	} {
		if !validComparisonState(comparison.State) || comparison.LegacyCount < 0 || comparison.ShadowCount < 0 {
			return fmt.Errorf("%w: malformed shadow comparison", ErrInvariant)
		}
		for _, id := range append(cloneStrings(comparison.LegacyIDs), comparison.ShadowIDs...) {
			if !validDiagnosticToken(id) {
				return fmt.Errorf("%w: unsanitized diagnostic ID", ErrInvariant)
			}
		}
		if !validDiagnosticValue(comparison.LegacyValue) || !validDiagnosticValue(comparison.ShadowValue) {
			return fmt.Errorf("%w: unsanitized diagnostic state", ErrInvariant)
		}
	}
	for _, gap := range diagnostics.CorrelationGaps {
		if !validDiagnosticToken(gap.SubmissionToken) || !validDiagnosticToken(gap.RecordToken) ||
			!validCorrelationGapReason(gap.Reason) {
			return fmt.Errorf("%w: malformed sanitized correlation gap", ErrInvariant)
		}
	}
	return nil
}

func diagnosticToken(kind, value string) string {
	digest := sha256.Sum256([]byte(kind + "\x00" + value))
	return "tok_" + hex.EncodeToString(digest[:])
}

func validDiagnosticToken(value string) bool {
	if !strings.HasPrefix(value, "tok_") || len(value) != 4+sha256.Size*2 {
		return false
	}
	return validStructuralFingerprint(strings.TrimPrefix(value, "tok_"))
}

func validDigest(value string) bool {
	return validStructuralFingerprint(value)
}

func validComparisonState(state ShadowComparisonState) bool {
	return state == ShadowComparisonMatch || state == ShadowComparisonDiverged || state == ShadowComparisonUnprovable
}

func validCorrelationGapReason(reason string) bool {
	switch reason {
	case CorrelationGapNoExplicitAppBinding,
		CorrelationGapMissingActivityStart,
		CorrelationGapMissingCausalInput,
		CorrelationGapMissingToolStart:
		return true
	default:
		return false
	}
}

func validLegacyShadowState(state string) bool {
	switch state {
	case "queued", "running", "completed", "failed", "interrupted", "cancelled":
		return true
	default:
		return false
	}
}

func terminalDiagnosticState(state string) bool {
	switch state {
	case "completed", "failed", "interrupted", "cancelled":
		return true
	default:
		return false
	}
}

func validDiagnosticValue(value string) bool {
	return value == "" || value == "none" || validLegacyShadowState(value)
}

func activityStateForID(thread Thread, executionID ExecutionID) ActivityState {
	for _, activity := range thread.ExecutionActivities {
		if activity.ID == executionID {
			return activity.State
		}
	}
	return ""
}

func terminalShadowState(thread Thread) string {
	if len(thread.ExecutionActivities) == 0 {
		return ""
	}
	state := thread.ExecutionActivities[len(thread.ExecutionActivities)-1].State
	if !terminalActivityState(state) {
		return ""
	}
	return string(state)
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

func optionalDiagnosticID(value string) []string {
	if value == "" {
		return []string{}
	}
	return []string{value}
}

func firstNonEmptyDiagnosticValue(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func cloneStrings(values []string) []string {
	return append([]string{}, values...)
}
