package brain

import (
	"context"
	"log"
	"time"

	"github.com/daoleno/zen/daemon/work"
)

// hostTranscriptCaptureInterval bounds reply latency for consumers that read
// the durable Brain timeline without an App WebSocket subscription (Telegram).
// The provider reader caches the parsed source by size/mtime, so an unchanged
// transcript costs one stat per pass; only new provider rows are appended.
const hostTranscriptCaptureInterval = time.Second

// captureHostTranscript materializes the bound Host Executor transcript into
// the current canonical Brain thread timeline, exactly once per provider event
// id. Capture lives in the daemon, not in any App subscription or channel
// adapter: opening or closing the App never starts, stops, or duplicates this
// work.
func (s *Service) captureHostTranscript(reader *work.ProviderConversationReader) error {
	if s == nil || s.store == nil {
		return nil
	}
	threadID, err := s.ChatThreadID()
	if err != nil {
		return err
	}
	if threadID == "" {
		return nil
	}
	conversation, err := s.hostBoundProviderConversation(reader)
	if err != nil {
		return err
	}
	if !conversation.Available {
		// No bound host transcript, or the provider declares no structured
		// conversation yet. This is a healthy no-op, not an error.
		return nil
	}
	return s.MaterializeProviderConversation(threadID, conversation)
}

// RunHostTranscriptCapture owns one provider reader and captures the current
// Host transcript until ctx is cancelled. It never replays, retries, or
// duplicates durable rows: MaterializeProviderConversation is idempotent by
// provider event id. A read error is logged once per distinct message so a
// malformed transcript cannot spam the daemon log.
func (s *Service) RunHostTranscriptCapture(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	reader := work.NewProviderConversationReader()
	ticker := time.NewTicker(hostTranscriptCaptureInterval)
	defer ticker.Stop()
	lastError := ""
	for {
		err := s.captureHostTranscript(reader)
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
