package brain

import (
	"context"
	"fmt"
	"hash"
	"hash/fnv"
	"log"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/work"
)

const (
	// hostTranscriptCaptureInterval bounds reply latency for consumers that read
	// the durable Brain timeline without an App WebSocket subscription
	// (Telegram). Unchanged provider sources never take the timeline lock.
	hostTranscriptCaptureInterval = time.Second
	// hostTranscriptCaptureAttempts bounds retries when NewChat or a Host
	// replacement races one capture read. Each retry re-reads the current
	// binding; any result that no longer belongs to it is discarded.
	hostTranscriptCaptureAttempts = 4
)

// hostTranscriptBinding pins the exact Brain thread and Host executor
// transcript that one capture read belongs to. The store re-proves the same
// binding under the timeline lock before appending, so a NewChat or Host
// replacement that races the read can never write the new Host output into the
// old thread.
type hostTranscriptBinding struct {
	ThreadID          string
	Provider          string
	ProviderSessionID string
	TranscriptPath    string
	ProviderDataRoot  string
}

// hostTranscriptCaptureState is the in-memory checkpoint of the last applied
// capture. It is intentionally not durable: a daemon restart re-materializes
// once, which is idempotent. The checkpoint advances only after a successful
// store append for the exact binding and revision it was read from.
type hostTranscriptCaptureState struct {
	binding  hostTranscriptBinding
	revision string
}

// captureHostTranscript performs one bounded capture pass. captured reports
// that this pass appended provider events to the durable timeline. The reader
// reuses its parsed source for an unchanged identity, and a matching
// checkpoint skips the store entirely: an idle Host re-reads its source
// identity and source stat, without reparsing the transcript tail or taking
// the timeline lock.
func (s *Service) captureHostTranscript(
	reader *work.ProviderConversationReader,
	state *hostTranscriptCaptureState,
) (bool, error) {
	if s == nil || s.store == nil {
		return false, nil
	}
	if reader == nil {
		reader = work.NewProviderConversationReader()
	}
	if state == nil {
		state = &hostTranscriptCaptureState{}
	}
	for attempt := 0; attempt < hostTranscriptCaptureAttempts; attempt++ {
		threadID, err := s.ChatThreadID()
		if err != nil {
			return false, err
		}
		if threadID == "" {
			return false, nil
		}
		identity, err := s.hostBoundProviderTranscriptIdentity()
		if err != nil {
			return false, err
		}
		if !identity.Bound() {
			// No bound Host transcript yet: a healthy no-op, not an error.
			return false, nil
		}
		binding := hostTranscriptBinding{
			ThreadID:          threadID,
			Provider:          strings.TrimSpace(identity.Provider),
			ProviderSessionID: strings.TrimSpace(identity.SessionID),
			TranscriptPath:    strings.TrimSpace(identity.Path),
			ProviderDataRoot:  strings.TrimSpace(identity.DataRoot),
		}
		conversation, err := reader.LoadByIdentity(identity)
		if err != nil {
			return false, err
		}
		if !conversation.Available {
			return false, nil
		}
		conversation.Events = work.SuppressPrivateHostTurns(conversation.Events)
		revision := providerConversationRevision(conversation)
		if state.binding == binding && state.revision == revision {
			return false, nil
		}
		applied, err := s.store.materializeProviderConversationBound(binding, conversation)
		if err != nil {
			// The checkpoint stays put: the next pass retries the same source
			// instead of skipping a write that never landed.
			return false, err
		}
		if !applied {
			// NewChat or a Host replacement raced the read. Discard the
			// result and re-read the current binding.
			continue
		}
		state.binding, state.revision = binding, revision
		return true, nil
	}
	return false, nil
}

// providerConversationRevision fingerprints exactly the provider events the
// timeline would materialize. It is computed without the store lock and is
// stable across unchanged reads, so an unchanged source never rewrites
// presentation state or scans timeline rows.
func providerConversationRevision(conversation work.CodexConversation) string {
	digest := fnv.New64a()
	writeCaptureRevision(digest, conversation.Source)
	writeCaptureRevision(digest, conversation.SessionID)
	writeCaptureRevision(digest, conversation.Path)
	count := 0
	for _, event := range conversation.Events {
		if !providerEventMaterializable(event) {
			continue
		}
		count++
		writeCaptureRevision(digest, event.ID)
		writeCaptureRevision(digest, event.Kind)
		writeCaptureRevision(digest, event.Body)
	}
	writeCaptureRevision(digest, fmt.Sprintf("%d", count))
	return fmt.Sprintf("%016x", digest.Sum64())
}

func writeCaptureRevision(digest hash.Hash64, value string) {
	_, _ = digest.Write([]byte(strings.TrimSpace(value)))
	_, _ = digest.Write([]byte{0})
}

// RunHostTranscriptCapture owns one provider reader and the unchanged-source
// checkpoint, capturing the current Host transcript until ctx is cancelled.
// It never replays, retries, or duplicates durable rows: materialization is
// idempotent by provider event id. A read error is logged once per distinct
// message so a malformed transcript cannot spam the daemon log.
func (s *Service) RunHostTranscriptCapture(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	reader := work.NewProviderConversationReader()
	state := &hostTranscriptCaptureState{}
	ticker := time.NewTicker(hostTranscriptCaptureInterval)
	defer ticker.Stop()
	lastError := ""
	for {
		_, err := s.captureHostTranscript(reader, state)
		switch {
		case err == nil:
			lastError = ""
		case err.Error() != lastError:
			lastError = err.Error()
			log.Printf("brain host transcript capture: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
