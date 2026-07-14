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
		maxEnterAttempts:   3,
	}
}

func codexReadyPane(extra string) string {
	return "╭────╮\n│ >_ OpenAI Codex (v0.144.3) │\n│ model: gpt-5.6 medium │\n╰────╯\n" + extra + "\n› Find and fix a bug in @filename\n\n  gpt-5.6 medium · /tmp\n"
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

func TestSubmitCodexInputRetriesEnterWithoutRepasting(t *testing.T) {
	body := "follow-up unique marker ZEN_FOLLOWUP_67890"
	ready := codexReadyPane("")
	draft := ready + "\n› " + body + "\n"
	submitted := ready + "\n› " + body + "\n\n• Working (1s • esc to interrupt)\n"
	io := &fakeCodexInputIO{captures: []string{ready, ready, draft, draft, draft, submitted}}

	if err := submitCodexInput(io, "agent:@1", body, testCodexSubmitConfig()); err != nil {
		t.Fatalf("submitCodexInput returned error: %v", err)
	}
	if len(io.pastes) != 1 {
		t.Fatalf("paste count = %d, retry must not paste again", len(io.pastes))
	}
	if io.enters != 2 {
		t.Fatalf("Enter count = %d, want retry", io.enters)
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

func TestSubmitCodexInputReturnsAttentionFailureAfterBoundedEnterRetries(t *testing.T) {
	body := "prompt remains in composer"
	ready := codexReadyPane("")
	draft := ready + "\n› " + body + "\n"
	io := &fakeCodexInputIO{captures: []string{ready, ready, draft, draft}}

	err := submitCodexInput(io, "agent:@1", body, testCodexSubmitConfig())
	if err == nil || !strings.Contains(err.Error(), "requires attention") {
		t.Fatalf("error = %v, want attention failure", err)
	}
	if len(io.pastes) != 1 || io.enters != 3 {
		t.Fatalf("pastes=%d enters=%d, want one paste and three bounded Enter attempts", len(io.pastes), io.enters)
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
