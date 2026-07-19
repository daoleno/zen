package watcher

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

// Codex input is a transaction, not a timed paste followed by an optimistic
// Enter. A prompt is pasted once and its submission Enter is sent once;
// ambiguous completion requires attention instead of replaying either action.
type codexInputIO interface {
	capture(sessionID string) (string, bool)
	paste(sessionID, body string) error
	sendEnter(sessionID string) error
	sleep(time.Duration)
	now() time.Time
}

type codexSubmitConfig struct {
	readyTimeout       time.Duration
	draftTimeout       time.Duration
	confirmationWindow time.Duration
	pollInterval       time.Duration
	stableReadyPolls   int
}

func defaultCodexSubmitConfig() codexSubmitConfig {
	return codexSubmitConfig{
		readyTimeout:       codexInputReadyTimeout,
		draftTimeout:       8 * time.Second,
		confirmationWindow: 8 * time.Second,
		pollInterval:       150 * time.Millisecond,
		stableReadyPolls:   2,
	}
}

var codexPastePlaceholderRe = regexp.MustCompile(`(?im)\[pasted content\s+[0-9]+\s+chars\]`)
var codexTaskActiveRe = regexp.MustCompile(`(?im)^\s*•\s+(starting mcp servers|working|thinking|running|searching|reading|exploring|executing|waiting)\b`)
var codexNativeQueueRe = regexp.MustCompile(`(?im)^[ \t]*•[ \t]+messages\s+to\s+be\s+submitted\s+after\s+next\s+tool\s+call\b`)
var codexInputBufferSequence atomic.Uint64

type realCodexInputIO struct{}

func (realCodexInputIO) capture(sessionID string) (string, bool) {
	out, err := exec.Command("tmux", "capture-pane", "-J", "-t", sessionID, "-p", "-S", "-1000").Output()
	if err != nil {
		return "", false
	}
	alive := true
	if aliveOut, listErr := exec.Command("tmux", "list-panes", "-t", sessionID, "-F", "#{pane_dead}").Output(); listErr == nil && strings.TrimSpace(string(aliveOut)) == "1" {
		alive = false
	}
	return string(out), alive
}

func (realCodexInputIO) paste(sessionID, body string) error {
	buffer := fmt.Sprintf("zen-codex-input-%d-%d", os.Getpid(), codexInputBufferSequence.Add(1))
	load := exec.Command("tmux", "load-buffer", "-b", buffer, "-")
	load.Stdin = strings.NewReader(body)
	if out, err := load.CombinedOutput(); err != nil {
		return fmt.Errorf("load Codex prompt into tmux buffer: %w%s", err, commandOutputSuffix(out))
	}
	if out, err := exec.Command("tmux", "paste-buffer", "-p", "-d", "-b", buffer, "-t", sessionID).CombinedOutput(); err != nil {
		_ = exec.Command("tmux", "delete-buffer", "-b", buffer).Run()
		return fmt.Errorf("paste Codex prompt into composer: %w%s", err, commandOutputSuffix(out))
	}
	return nil
}

func (realCodexInputIO) sendEnter(sessionID string) error {
	if out, err := exec.Command("tmux", "send-keys", "-t", sessionID, "Enter").CombinedOutput(); err != nil {
		return fmt.Errorf("submit Codex prompt: %w%s", err, commandOutputSuffix(out))
	}
	return nil
}

func (realCodexInputIO) sleep(delay time.Duration) { time.Sleep(delay) }
func (realCodexInputIO) now() time.Time            { return time.Now() }

func submitCodexInput(io codexInputIO, sessionID, body string, cfg codexSubmitConfig) error {
	baseline, err := waitForStableCodexComposer(io, sessionID, cfg)
	if err != nil {
		return err
	}
	if err := io.paste(sessionID, body); err != nil {
		return err
	}

	draft, err := waitForCodexDraft(io, sessionID, baseline, body, cfg)
	if err != nil {
		return err
	}
	if err := io.sendEnter(sessionID); err != nil {
		return err
	}
	deadline := io.now().Add(cfg.confirmationWindow)
	for {
		content, alive := io.capture(sessionID)
		if !alive {
			return fmt.Errorf("Codex session exited before prompt submission was confirmed")
		}
		if codexSubmissionAdvanced(draft, content, body) {
			return nil
		}
		if !io.now().Before(deadline) {
			break
		}
		io.sleep(cfg.pollInterval)
	}
	return fmt.Errorf("Codex prompt was pasted once and Enter was sent once, but submission was not confirmed within %s; the session requires attention", cfg.confirmationWindow)
}

func waitForStableCodexComposer(io codexInputIO, sessionID string, cfg codexSubmitConfig) (string, error) {
	deadline := io.now().Add(cfg.readyTimeout)
	stable := 0
	advancedStartupPrompt := false
	for {
		content, alive := io.capture(sessionID)
		if !alive {
			return "", fmt.Errorf("Codex session exited before its composer became ready")
		}
		if !advancedStartupPrompt && isCodexStartupContinuePrompt("codex", content) {
			if err := io.sendEnter(sessionID); err != nil {
				return "", fmt.Errorf("advance Codex startup prompt: %w", err)
			}
			advancedStartupPrompt = true
			stable = 0
		} else if isAgentInputReady("codex", content) {
			stable++
			if stable >= cfg.stableReadyPolls {
				return content, nil
			}
		} else {
			stable = 0
		}
		if !io.now().Before(deadline) {
			return "", fmt.Errorf("Codex composer did not become ready within %s", cfg.readyTimeout)
		}
		io.sleep(cfg.pollInterval)
	}
}

func waitForCodexDraft(io codexInputIO, sessionID, baseline, body string, cfg codexSubmitConfig) (string, error) {
	deadline := io.now().Add(cfg.draftTimeout)
	for {
		content, alive := io.capture(sessionID)
		if !alive {
			return "", fmt.Errorf("Codex session exited after the prompt was pasted")
		}
		if codexDraftVisible(baseline, content, body) {
			return content, nil
		}
		if !io.now().Before(deadline) {
			return "", fmt.Errorf("Codex prompt was pasted once but the composer did not expose a complete draft within %s; Enter was not sent", cfg.draftTimeout)
		}
		io.sleep(cfg.pollInterval)
	}
}

func codexDraftVisible(before, after, body string) bool {
	if codexPastePlaceholderRe.FindAllStringIndex(after, -1) != nil &&
		len(codexPastePlaceholderRe.FindAllStringIndex(after, -1)) > len(codexPastePlaceholderRe.FindAllStringIndex(before, -1)) {
		return true
	}
	return codexCompleteBodyCount(after, body) > codexCompleteBodyCount(before, body)
}

func codexSubmissionAdvanced(draft, current, body string) bool {
	if current == draft {
		return false
	}
	// The complete draft was observed before Enter. An increased explicit
	// provider-native queue marker therefore confirms that Codex consumed that
	// composer. Zen deliberately leaves queue timing and interruption to Codex.
	if codexNativeQueueAdvanced(draft, current) {
		return true
	}
	draftPlaceholders := len(codexPastePlaceholderRe.FindAllStringIndex(draft, -1))
	currentPlaceholders := len(codexPastePlaceholderRe.FindAllStringIndex(current, -1))
	if draftPlaceholders > currentPlaceholders {
		workingAdvanced := len(codexTaskActiveRe.FindAllStringIndex(current, -1)) >
			len(codexTaskActiveRe.FindAllStringIndex(draft, -1))
		composerAdvanced := len(codexInputPromptRe.FindAllStringIndex(current, -1)) >
			len(codexInputPromptRe.FindAllStringIndex(draft, -1))
		if workingAdvanced || composerAdvanced {
			return true
		}
	}
	draftBodyCount := codexCompleteBodyCount(draft, body)
	currentBodyCount := codexCompleteBodyCount(current, body)
	if currentBodyCount < draftBodyCount {
		return false
	}
	lastBodyEnd := codexLastCompleteBodyEnd(current, body)
	if lastBodyEnd < 0 {
		return false
	}
	suffix := current[lastBodyEnd:]
	return codexTaskActiveRe.MatchString(suffix) || codexInputPromptRe.MatchString(suffix)
}

func codexCompleteBodyCount(content, body string) int {
	comparableBody := codexComparableText(body)
	if comparableBody == "" {
		return 0
	}
	return strings.Count(codexComparableText(content), comparableBody)
}

func codexLastCompleteBodyEnd(content, body string) int {
	comparableBody := codexComparableText(body)
	if comparableBody == "" {
		return -1
	}
	comparableContent := codexComparableText(content)
	start := strings.LastIndex(comparableContent, comparableBody)
	if start < 0 {
		return -1
	}
	targetEnd := start + len(comparableBody)
	comparableOffset := 0
	for rawOffset, r := range content {
		if unicode.IsSpace(r) {
			continue
		}
		comparableOffset += utf8.RuneLen(r)
		if comparableOffset >= targetEnd {
			_, rawSize := utf8.DecodeRuneInString(content[rawOffset:])
			return rawOffset + rawSize
		}
	}
	return -1
}

func codexComparableText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
}

func codexNativeQueueAdvanced(before, after string) bool {
	return len(codexNativeQueueRe.FindAllStringIndex(after, -1)) >
		len(codexNativeQueueRe.FindAllStringIndex(before, -1))
}
