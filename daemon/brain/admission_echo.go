package brain

import (
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/work"
)

// IsBrainInputAdmission reports whether a durable user row was written by a
// Brain Interface send_input admission. Identity is solely the explicit
// brain_admission field; no pre-field heuristic exists.
func IsBrainInputAdmission(item TimelineItem) bool {
	return strings.TrimSpace(item.Kind) == timelineKindUserMessage && item.BrainAdmission
}

func admissionCorrelationDigest(item TimelineItem) string {
	if digest := strings.TrimSpace(item.AdmissionSHA256); digest != "" {
		return digest
	}
	if strings.TrimSpace(item.Body) == "" {
		return ""
	}
	return AdmissionDigest(item.Body)
}

func providerEchoMatchesAdmission(
	admission BrainInputAdmission,
	threadID string,
	providerSessionID string,
	body string,
	createdAt time.Time,
) bool {
	if admission.State != BrainInputAdmissionAccepted || admission.AcceptedAt == nil || createdAt.IsZero() {
		return false
	}
	if strings.TrimSpace(threadID) != admission.ThreadID ||
		strings.TrimSpace(providerSessionID) != admission.SessionID {
		return false
	}
	if AdmissionDigest(strings.TrimSpace(body)) != admission.BodySHA256 {
		return false
	}
	// No upper timestamp bound: the provider stamps its user_message row when
	// the turn loop processes the input, which is always after the daemon's
	// transport acceptance, so a bounded window could never match the echo it
	// must claim. Causal credit order (one echo per admission, timeline-ordered
	// admissions, stream-ordered events) is the only ordering authority.
	createdAt = createdAt.UTC()
	return !createdAt.Before(admission.CreatedAt.UTC())
}

func exactProviderEventTimestamp(event work.CodexConversationEvent) (time.Time, bool) {
	raw := strings.TrimSpace(event.Timestamp)
	if raw == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed.UTC(), true
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed.UTC(), true
	}
	return time.Time{}, false
}

// ProviderUserEchoSuppressions returns only provider user-message IDs already
// claimed by durable exact admission reconciliation for the current provider
// Session. Overlay rendering never performs a second, digest-only matching
// decision or carries a claim across a replacement Session.
func ProviderUserEchoSuppressions(items []TimelineItem, providerSessionID string) map[string]bool {
	providerSessionID = strings.TrimSpace(providerSessionID)
	suppress := map[string]bool{}
	for _, item := range items {
		if !IsBrainInputAdmission(item) || strings.TrimSpace(item.SessionID) != providerSessionID {
			continue
		}
		if echoID := strings.TrimSpace(item.AdmissionEchoEventID); echoID != "" {
			suppress[echoID] = true
		}
	}
	return suppress
}

// claimProviderUserEchoes matches provider user events against accepted
// orchestration admissions in causal order. Unconsumed admissions receive
// AdmissionEchoEventID for durable idempotence.
//
// Already-durable provider-native user event IDs are excluded from candidates
// so an older Terminal/direct row cannot consume a later admission's echo credit.
func claimProviderUserEchoes(
	items []TimelineItem,
	providerEvents []work.CodexConversationEvent,
	admissions []BrainInputAdmission,
	threadID string,
	providerSessionID string,
) (map[string]bool, []TimelineItem, bool) {
	suppress := map[string]bool{}
	if len(items) == 0 || len(providerEvents) == 0 {
		return suppress, items, false
	}

	knownProviderUserIDs := map[string]bool{}
	for _, item := range items {
		if strings.TrimSpace(item.Kind) != timelineKindUserMessage {
			continue
		}
		if IsBrainInputAdmission(item) {
			continue
		}
		if id := strings.TrimSpace(item.ID); id != "" {
			knownProviderUserIDs[id] = true
		}
	}

	type credit struct {
		index     int
		admission BrainInputAdmission
	}
	credits := make([]credit, 0, len(items))
	out := append([]TimelineItem(nil), items...)
	for index, item := range out {
		if !IsBrainInputAdmission(item) {
			continue
		}
		echoID := strings.TrimSpace(item.AdmissionEchoEventID)
		if echoID != "" {
			suppress[echoID] = true
			continue
		}
		for _, admission := range admissions {
			if admission.State != BrainInputAdmissionAccepted ||
				!timelineItemMatchesBrainInputAdmission(item, admission) {
				continue
			}
			credits = append(credits, credit{index: index, admission: admission})
			break
		}
	}

	dirty := false
	for _, event := range providerEvents {
		if strings.TrimSpace(event.Kind) != timelineKindUserMessage {
			continue
		}
		eventID := strings.TrimSpace(event.ID)
		if eventID == "" {
			continue
		}
		if suppress[eventID] {
			continue
		}
		// Causal boundary: durable provider-native rows are not echoes.
		if knownProviderUserIDs[eventID] {
			continue
		}
		createdAt, hasExactTimestamp := exactProviderEventTimestamp(event)
		if !hasExactTimestamp {
			continue
		}
		matched := -1
		for cursor := 0; cursor < len(credits); cursor++ {
			if !providerEchoMatchesAdmission(
				credits[cursor].admission,
				threadID,
				providerSessionID,
				event.Body,
				createdAt,
			) {
				continue
			}
			matched = cursor
			break
		}
		if matched < 0 {
			continue
		}
		suppress[eventID] = true
		out[credits[matched].index].AdmissionEchoEventID = eventID
		dirty = true
		credits = append(credits[:matched], credits[matched+1:]...)
	}
	return suppress, out, dirty
}
