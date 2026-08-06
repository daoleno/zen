package work

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// LoadLatestFreshCodexConversationForCWD loads the newest fresh Codex rollout
// for a workspace cwd without binding to a specific host process. Brain uses
// this to seed durable thread history when the current host Session publishes
// an available empty transcript after rotation.
func LoadLatestFreshCodexConversationForCWD(cwd string, now time.Time) (CodexConversation, error) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return CodexConversation{Available: false, Reason: "missing_cwd", Events: []CodexConversationEvent{}}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return CodexConversation{}, err
	}
	dbPath := filepath.Join(home, ".codex", "state_5.sqlite")
	if _, err := os.Stat(dbPath); err != nil {
		return CodexConversation{Available: false, Reason: "transcript_not_found", Events: []CodexConversationEvent{}}, nil
	}
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		return CodexConversation{Available: false, Reason: "transcript_not_found", Events: []CodexConversationEvent{}}, nil
	}

	var candidates []codexTranscriptCandidate
	for _, candidateCWD := range transcriptCWDCandidates(cwd) {
		rows, err := queryCodexThreads(sqlite3, dbPath, candidateCWD)
		if err != nil {
			return CodexConversation{}, err
		}
		for _, row := range rows {
			path := strings.TrimSpace(row.RolloutPath)
			if path == "" {
				continue
			}
			meta, err := readCodexMeta(path)
			if err != nil {
				continue
			}
			if meta.CWD != candidateCWD || strings.EqualFold(meta.Originator, "codex-exec") {
				continue
			}
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			candidates = append(candidates, codexTranscriptCandidate{
				Row:     row,
				Meta:    meta,
				Path:    path,
				Updated: info.ModTime(),
			})
		}
	}
	fresh := freshCodexTranscriptCandidates(candidates, now)
	if len(fresh) == 0 {
		return CodexConversation{Available: false, Reason: "transcript_not_found", Events: []CodexConversationEvent{}}, nil
	}
	best := latestUpdatedCodexTranscript(fresh)
	reader := &ProviderConversationReader{}
	conversation, err := reader.loadCodexConversation(best.Path)
	if err != nil {
		return CodexConversation{}, err
	}
	conversation.Available = true
	conversation.Source = "codex_rollout"
	conversation.Path = best.Path
	conversation.SessionID = firstNonEmpty(conversation.SessionID, best.Meta.ID, best.Row.ID)
	conversation.CWD = firstNonEmpty(conversation.CWD, best.Meta.CWD)
	conversation.Updated = &best.Updated
	if conversation.Events == nil {
		conversation.Events = []CodexConversationEvent{}
	}
	return SanitizeConversationProjection(conversation), nil
}
