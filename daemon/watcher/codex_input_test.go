package watcher

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeCodexInputIO struct {
	captures []string
	index    int
	clock    time.Time
	pastes   []string
	enters   int
	pasteErr error
	enterErr error
}

func (f *fakeCodexInputIO) capture(string) (string, bool) {
	if len(f.captures) == 0 {
		return "", false
	}
	index := f.index
	if index >= len(f.captures) {
		index = len(f.captures) - 1
	}
	f.index++
	return f.captures[index], true
}

func (f *fakeCodexInputIO) paste(_ string, body string) error {
	f.pastes = append(f.pastes, body)
	return f.pasteErr
}

func (f *fakeCodexInputIO) sendEnter(string) error {
	f.enters++
	return f.enterErr
}

func (f *fakeCodexInputIO) sleep(delay time.Duration) { f.clock = f.clock.Add(delay) }
func (f *fakeCodexInputIO) now() time.Time            { return f.clock }

func testCodexSubmitConfig() codexSubmitConfig {
	return codexSubmitConfig{
		readyTimeout:       3 * time.Second,
		draftTimeout:       time.Second,
		confirmationWindow: time.Second,
		pollInterval:       time.Second,
		stableReadyPolls:   2,
	}
}

func codexReadyPane(extra string) string {
	return "╭────╮\n│ >_ OpenAI Codex (v0.144.3) │\n│ model: gpt-5.6 medium │\n╰────╯\n" + extra + "\n› Find and fix a bug in @filename\n\n  gpt-5.6 medium · /tmp\n"
}

func TestCodexDraftVisibleMatchesCompleteBodyAcrossTerminalWhitespace(t *testing.T) {
	longChinese := "修复一个已经在 Brain delegated follow-up 中真实复现的 Codex 输入事务 bug。事实：Zen agent send 向 @341 粘贴了一条较长的单行中文任务文本；完整 Unicode 草稿不得丢失。"
	tests := []struct {
		name    string
		body    string
		draft   string
		visible bool
	}{
		{
			name:    "long Chinese single line visually wrapped",
			body:    longChinese,
			draft:   "› 修复一个已经在 Brain delegated\n  follow-up 中真实复现的 Codex 输入事务 bug。事实：\n  Zen agent send 向 @341 粘贴了一条较长的单行中文任务文本；\n  完整 Unicode 草稿不得丢失。\n",
			visible: true,
		},
		{
			name:    "ordinary ASCII",
			body:    "fix the Codex input transaction marker ZEN_ASCII_24680",
			draft:   "› fix the Codex input transaction marker ZEN_ASCII_24680\n",
			visible: true,
		},
		{
			name:    "multiline body with visual wrapping",
			body:    "inspect the watcher transaction\npreserve the provider-native queue\n验证完整 Unicode 草稿",
			draft:   "› inspect the watcher transaction\n  preserve the provider-native\n  queue\n  验证完整 Unicode 草稿\n",
			visible: true,
		},
		{
			name: "similar old text is not the requested draft",
			body: "shared delegated follow-up prefix deliberately longer than thirty-six runes with NEW_REQUIRED_TAIL",
			draft: "› shared delegated follow-up prefix deliberately longer than thirty-six runes with " +
				"OLD_STALE_TAIL\n",
			visible: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := codexReadyPane("")
			after := before + "\n" + tc.draft
			if got := codexDraftVisible(before, after, tc.body); got != tc.visible {
				t.Fatalf("codexDraftVisible() = %v, want %v", got, tc.visible)
			}
		})
	}
}

func TestSubmitCodexInputRecognizesWrappedLongChineseDraft(t *testing.T) {
	body := "修复一个已经在 Brain delegated follow-up 中真实复现的 Codex 输入事务 bug。完整 Unicode 草稿在窄 pane 视觉折行后仍须只提交一次。"
	ready := codexReadyPane("")
	draft := ready + "\n› 修复一个已经在 Brain delegated\n  follow-up 中真实复现的 Codex 输入事务 bug。完整 Unicode\n  草稿在窄 pane 视觉折行后仍须只提交一次。\n"
	submitted := ready + "\n› 修复一个已经在 Brain delegated\n  follow-up 中真实复现的 Codex 输入事务 bug。完整 Unicode\n  草稿在窄 pane 视觉折行后仍须只提交一次。\n\n• Working (1s • esc to interrupt)\n"
	io := &fakeCodexInputIO{captures: []string{ready, ready, draft, submitted}}

	if err := submitCodexInput(io, "agent:@341", body, testCodexSubmitConfig()); err != nil {
		t.Fatalf("submitCodexInput returned error: %v", err)
	}
	if len(io.pastes) != 1 || io.pastes[0] != body {
		t.Fatalf("pastes = %#v, want wrapped Chinese body exactly once", io.pastes)
	}
	if io.enters != 1 {
		t.Fatalf("Enter count = %d, want exactly 1", io.enters)
	}
}

func TestSubmitCodexInputAcceptsProviderNativeQueuedMessage(t *testing.T) {
	body := "实现并验证完整的 Codex queued follow-up；忙碌时交给 provider-native queue，绝不由 Zen 重贴、重发或另建队列。"
	busy := codexReadyPane("• Working (4m 10s • esc to interrupt)")
	draft := busy + "\n› " + body + "\n"
	queued := busy + "\n• Messages to be submitted after next tool\n  call (press esc to interrupt and send immediately)\n" +
		"  ↳ 实现并验证完整的 Codex queued\n" +
		"    follow-up；忙碌时交给 provider-native queue，绝不由\n" +
		"    Zen 重贴、重发或另建\n" +
		"    …\n\n› Summarize recent commits\n"
	io := &fakeCodexInputIO{captures: []string{busy, busy, draft, queued}}

	if err := submitCodexInput(io, "agent:@341", body, testCodexSubmitConfig()); err != nil {
		t.Fatalf("submitCodexInput returned error: %v", err)
	}
	if len(io.pastes) != 1 || io.pastes[0] != body {
		t.Fatalf("pastes = %#v, want queued body exactly once", io.pastes)
	}
	if io.enters != 1 {
		t.Fatalf("Enter count = %d, want exactly 1 for native queue submission", io.enters)
	}
}

func TestCodexSubmissionAdvancedRequiresPostDraftTransition(t *testing.T) {
	body := "follow-up body with complete marker ZEN_TRANSITION_86420"
	preExistingQueue := "• Messages to be submitted after next tool call\n  ↳ older queued task\n"
	draft := "• Working (4m 10s • esc to interrupt)\n" + preExistingQueue + "\n› " + body + "\n"

	tests := []struct {
		name     string
		current  string
		advanced bool
	}{
		{
			name:     "pre-existing busy marker redraw",
			current:  "• Working (4m 11s • esc to interrupt)\n" + preExistingQueue + "\n› " + body + "\n",
			advanced: false,
		},
		{
			name: "changed pre-existing queue without marker increase",
			current: "• Working (4m 11s • esc to interrupt)\n" +
				"• Messages to be submitted after next tool call\n  ↳ older queued task redrawn\n\n› " + body + "\n",
			advanced: false,
		},
		{
			name:     "increased native queue marker",
			current:  draft + "\n• Messages to be submitted after next tool call\n  ↳ newly queued task\n",
			advanced: true,
		},
		{
			name:     "working marker after complete body",
			current:  "› " + body + "\n\n• Working (1s • esc to interrupt)\n",
			advanced: true,
		},
		{
			name:     "empty composer after complete body",
			current:  "› " + body + "\n\n› Summarize recent commits\n",
			advanced: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := codexSubmissionAdvanced(draft, tc.current, body); got != tc.advanced {
				t.Fatalf("codexSubmissionAdvanced() = %v, want %v", got, tc.advanced)
			}
		})
	}

	t.Run("stale complete body followed by old working marker", func(t *testing.T) {
		withOldHistory := "› " + body + "\n\n• Working (old turn)\n"
		draftWithNewComposer := withOldHistory + "\n› " + body + "\n"
		currentWithoutNewDraft := "› " + body + "\n\n• Working (old turn redrawn)\n"
		if codexSubmissionAdvanced(draftWithNewComposer, currentWithoutNewDraft, body) {
			t.Fatal("a surviving stale body must not stand in for the post-draft transition")
		}
	})

	t.Run("collapsed draft does not reuse pre-existing busy marker", func(t *testing.T) {
		collapsedDraft := "• Working (4m 10s • esc to interrupt)\n› [Pasted Content 2048 chars]\n"
		busyRedraw := "• Working (4m 11s • esc to interrupt)\n"
		if codexSubmissionAdvanced(collapsedDraft, busyRedraw, body) {
			t.Fatal("a pre-existing busy marker must not confirm a collapsed draft")
		}
	})
}

func TestSubmitCodexInputWaitsForMCPThenPastesOnceAndConfirms(t *testing.T) {
	body := "execute unique marker ZEN_INITIAL_12345"
	starting := codexReadyPane("• Starting MCP servers (0/3): context7, playwright")
	ready := codexReadyPane("")
	draft := ready + "\n› " + body + "\n\n  gpt-5.6 medium · /tmp\n"
	submitted := ready + "\n› " + body + "\n\n• Working (1s • esc to interrupt)\n\n› Find and fix a bug in @filename\n"
	io := &fakeCodexInputIO{captures: []string{starting, ready, ready, draft, submitted}}

	if err := submitCodexInput(io, "agent:@1", body, testCodexSubmitConfig()); err != nil {
		t.Fatalf("submitCodexInput returned error: %v", err)
	}
	if len(io.pastes) != 1 || io.pastes[0] != body {
		t.Fatalf("pastes = %#v, want body exactly once", io.pastes)
	}
	if io.enters != 1 {
		t.Fatalf("Enter count = %d, want 1", io.enters)
	}
}

func TestSubmitCodexInputAfterLongTurnWithoutHeader(t *testing.T) {
	body := "follow-up unique marker ZEN_LONG_TURN_24680"
	ready := strings.Repeat("completed delegated output line\n", 1100) +
		"\n› Find and fix a bug in @filename\n\n  gpt-5.6 medium · /tmp\n"
	draft := ready + "\n› " + body + "\n\n  gpt-5.6 medium · /tmp\n"
	submitted := ready + "\n› " + body + "\n\n• Working (1s • esc to interrupt)\n"
	io := &fakeCodexInputIO{captures: []string{ready, ready, draft, submitted}}

	if err := submitCodexInput(io, "agent:@1", body, testCodexSubmitConfig()); err != nil {
		t.Fatalf("submitCodexInput returned error: %v", err)
	}
	if len(io.pastes) != 1 || io.pastes[0] != body {
		t.Fatalf("pastes = %#v, want headerless follow-up body exactly once", io.pastes)
	}
	if io.enters != 1 {
		t.Fatalf("Enter count = %d, want 1", io.enters)
	}
}

func TestSubmitCodexInputAdvancesStartupTrustBeforePasting(t *testing.T) {
	body := "execute unique marker ZEN_TRUST_12345"
	trust := "│ >_ OpenAI Codex │\nDo you trust the contents of this directory?\n› 1. Yes, continue\n  Press enter to continue\n"
	ready := codexReadyPane("")
	draft := ready + "\n› " + body + "\n"
	submitted := ready + "\n› " + body + "\n\n• Working (1s • esc to interrupt)\n"
	io := &fakeCodexInputIO{captures: []string{trust, ready, ready, draft, submitted}}

	if err := submitCodexInput(io, "agent:@1", body, testCodexSubmitConfig()); err != nil {
		t.Fatalf("submitCodexInput returned error: %v", err)
	}
	if len(io.pastes) != 1 || io.enters != 2 {
		t.Fatalf("pastes=%d enters=%d, want startup Enter plus submit Enter", len(io.pastes), io.enters)
	}
}

func TestSubmitCodexInputRecognizesCollapsedLongPaste(t *testing.T) {
	body := strings.Repeat("long delegated task line\n", 100) + "unique final marker"
	ready := codexReadyPane("")
	draft := ready + "\n› [Pasted Content 2519 chars]\n"
	submitted := ready + "\n› long delegated task line\n  unique final marker\n\n• Working (1s • esc to interrupt)\n"
	io := &fakeCodexInputIO{captures: []string{ready, ready, draft, submitted}}

	if err := submitCodexInput(io, "agent:@1", body, testCodexSubmitConfig()); err != nil {
		t.Fatalf("submitCodexInput returned error: %v", err)
	}
	if len(io.pastes) != 1 || io.enters != 1 {
		t.Fatalf("pastes=%d enters=%d, want 1 each", len(io.pastes), io.enters)
	}
}

func TestSubmitCodexInputDoesNotRetryEnterWhenConfirmationIsDelayed(t *testing.T) {
	body := "follow-up unique marker ZEN_FOLLOWUP_67890"
	ready := codexReadyPane("")
	draft := ready + "\n› " + body + "\n"
	submitted := ready + "\n› " + body + "\n\n• Working (1s • esc to interrupt)\n"
	io := &fakeCodexInputIO{captures: []string{ready, ready, draft, draft, draft, submitted}}

	err := submitCodexInput(io, "agent:@1", body, testCodexSubmitConfig())
	if err == nil || !strings.Contains(err.Error(), "requires attention") {
		t.Fatalf("error = %v, want attention failure after one unconfirmed Enter", err)
	}
	if len(io.pastes) != 1 {
		t.Fatalf("paste count = %d, want one paste", len(io.pastes))
	}
	if io.enters != 1 {
		t.Fatalf("Enter count = %d, an unconfirmed transaction must not retry Enter", io.enters)
	}
}

func TestSubmitCodexInputDoesNotPressEnterWhenDraftCannotBeObserved(t *testing.T) {
	body := "prompt that never appears"
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{captures: []string{ready, ready, ready, ready}}
	cfg := testCodexSubmitConfig()

	err := submitCodexInput(io, "agent:@1", body, cfg)
	if err == nil || !strings.Contains(err.Error(), "Enter was not sent") {
		t.Fatalf("error = %v, want explicit unobserved-draft failure", err)
	}
	if len(io.pastes) != 1 || io.enters != 0 {
		t.Fatalf("pastes=%d enters=%d, want one paste and no Enter", len(io.pastes), io.enters)
	}
}

func TestSubmitCodexInputReturnsAttentionFailureAfterSingleEnter(t *testing.T) {
	body := "prompt remains in composer"
	ready := codexReadyPane("")
	draft := ready + "\n› " + body + "\n"
	io := &fakeCodexInputIO{captures: []string{ready, ready, draft, draft}}

	err := submitCodexInput(io, "agent:@1", body, testCodexSubmitConfig())
	if err == nil || !strings.Contains(err.Error(), "requires attention") {
		t.Fatalf("error = %v, want attention failure", err)
	}
	if len(io.pastes) != 1 || io.enters != 1 {
		t.Fatalf("pastes=%d enters=%d, want one paste and one Enter", len(io.pastes), io.enters)
	}
}

func TestSubmitCodexInputReturnsPasteFailureWithoutEnter(t *testing.T) {
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{captures: []string{ready, ready}, pasteErr: errors.New("paste failed")}
	err := submitCodexInput(io, "agent:@1", "unique prompt body", testCodexSubmitConfig())
	if err == nil || err.Error() != "paste failed" || io.enters != 0 {
		t.Fatalf("error=%v enters=%d", err, io.enters)
	}
}
