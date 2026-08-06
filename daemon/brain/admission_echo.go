package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/daoleno/zen/daemon/work"
)

const admissionProvenanceMarker = "timeline_brain_admission_v1"

// IsBrainInputAdmission reports whether a durable user row was written by a
// Brain Interface send_input admission. After the one-time provenance
// migration, this is solely the explicit brain_admission field.
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

func providerUserCorrelationDigest(event work.CodexConversationEvent) string {
	if digest := strings.TrimSpace(event.AdmissionSHA256); digest != "" {
		return digest
	}
	if strings.TrimSpace(event.Body) == "" {
		return ""
	}
	return AdmissionDigest(event.Body)
}

// ProviderUserEchoSuppressions returns provider user_message IDs that are
// one-to-one echoes of Brain input admissions. Digests only correlate a
// candidate; each admission contributes at most one suppression credit.
// Already-materialized provider-native user row IDs are never candidates.
func ProviderUserEchoSuppressions(
	items []TimelineItem,
	providerEvents []work.CodexConversationEvent,
) map[string]bool {
	suppress, _, _ := claimProviderUserEchoes(items, providerEvents, false)
	return suppress
}

// claimProviderUserEchoes matches provider user events against Brain admissions
// in causal order. When assign is true, unconsumed admissions receive
// AdmissionEchoEventID for durable idempotence.
//
// Already-durable provider-native user event IDs are excluded from candidates
// so an older Terminal/direct row cannot consume a later admission's echo credit.
func claimProviderUserEchoes(
	items []TimelineItem,
	providerEvents []work.CodexConversationEvent,
	assign bool,
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
		index  int
		digest string
	}
	credits := make([]credit, 0, len(items))
	out := items
	if assign {
		out = append([]TimelineItem(nil), items...)
	}
	for index, item := range out {
		if !IsBrainInputAdmission(item) {
			continue
		}
		echoID := strings.TrimSpace(item.AdmissionEchoEventID)
		if echoID != "" {
			suppress[echoID] = true
			continue
		}
		digest := admissionCorrelationDigest(item)
		if digest == "" {
			continue
		}
		credits = append(credits, credit{index: index, digest: digest})
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
		digest := providerUserCorrelationDigest(event)
		if digest == "" {
			continue
		}
		matched := -1
		for cursor := 0; cursor < len(credits); cursor++ {
			if credits[cursor].digest != digest {
				continue
			}
			matched = cursor
			break
		}
		if matched < 0 {
			continue
		}
		suppress[eventID] = true
		if assign {
			out[credits[matched].index].AdmissionEchoEventID = eventID
			dirty = true
		}
		credits = append(credits[:matched], credits[matched+1:]...)
	}
	return suppress, out, dirty
}

func (s *Store) ensureAdmissionProvenanceLocked() error {
	markerPath := filepath.Join(s.statePath(), admissionProvenanceMarker)
	if _, err := os.Stat(markerPath); err == nil {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	items, err := s.readAllTimelineItemsLocked()
	if err != nil {
		return err
	}
	changed := false
	for index := range items {
		if applyAdmissionProvenanceMigration(&items[index]) {
			changed = true
		}
	}
	if changed {
		if err := s.rewriteTimelineLocked(items); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(markerPath, []byte("ok\n"), 0o600)
}

func applyAdmissionProvenanceMigration(item *TimelineItem) bool {
	if item == nil {
		return false
	}
	normalizeLegacyTimelineKind(item)
	if item.Kind != timelineKindUserMessage {
		return clearNonAdmissionCorrelation(item)
	}
	if item.BrainAdmission {
		changed := false
		if strings.TrimSpace(item.AdmissionSHA256) == "" && strings.TrimSpace(item.Body) != "" {
			item.AdmissionSHA256 = AdmissionDigest(item.Body)
			changed = true
		}
		return changed
	}
	if isUnreleasedPreFieldAdmission(*item) {
		item.BrainAdmission = true
		return true
	}
	return clearNonAdmissionCorrelation(item)
}

// isUnreleasedPreFieldAdmission recognizes the unreleased AdmitUserMessage
// shape written before brain_admission existed: receipt-shaped id, exact body
// digest, no colon owner:line provider id. Heuristic is migration-only.
func isUnreleasedPreFieldAdmission(item TimelineItem) bool {
	if strings.TrimSpace(item.Kind) != timelineKindUserMessage || item.BrainAdmission {
		return false
	}
	id := strings.TrimSpace(item.ID)
	body := strings.TrimSpace(item.Body)
	digest := strings.TrimSpace(item.AdmissionSHA256)
	if id == "" || body == "" || digest == "" {
		return false
	}
	// Provider transcript event ids use "owner:…" (session:line, path:offset).
	if strings.Contains(id, ":") {
		return false
	}
	return digest == AdmissionDigest(body)
}

func clearNonAdmissionCorrelation(item *TimelineItem) bool {
	if item == nil {
		return false
	}
	changed := false
	if item.BrainAdmission {
		item.BrainAdmission = false
		changed = true
	}
	if strings.TrimSpace(item.AdmissionSHA256) != "" {
		item.AdmissionSHA256 = ""
		changed = true
	}
	if strings.TrimSpace(item.AdmissionEchoEventID) != "" {
		item.AdmissionEchoEventID = ""
		changed = true
	}
	return changed
}

func formatAdmissionProvenanceError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("brain admission provenance: %w", err)
}
