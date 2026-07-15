package work

import "testing"

func TestCodexConversationTurnLifecycle_PreservesIdentityAndAuthoritativePrecedence(t *testing.T) {
	var lifecycle codexConversationTurnLifecycle
	lifecycle.start("session:turn:first", "2026-07-15T01:00:00Z")
	lifecycle.start("session:turn:first", "2026-07-15T01:00:09Z")
	assertTurnLifecycle(t, lifecycle.snapshot(), "session:turn:first", CodexConversationTurnRunning, "2026-07-15T01:00:00Z", "")

	// Accepted/queued input cannot replace the executor turn before the current
	// turn has an authoritative terminal fact.
	lifecycle.start("session:turn:queued", "2026-07-15T01:00:10Z")
	lifecycle.settle("session:turn:queued", CodexConversationTurnFailed, "2026-07-15T01:00:11Z")
	assertTurnLifecycle(t, lifecycle.snapshot(), "session:turn:first", CodexConversationTurnRunning, "2026-07-15T01:00:00Z", "")

	lifecycle.settle("session:turn:first", CodexConversationTurnCompleted, "2026-07-15T01:00:12Z")
	assertTurnLifecycle(t, lifecycle.snapshot(), "session:turn:first", CodexConversationTurnCompleted, "2026-07-15T01:00:00Z", "2026-07-15T01:00:12Z")

	// Stale starts and later conflicting terminal records cannot reopen or
	// rewrite a settled turn.
	lifecycle.start("session:turn:first", "2026-07-15T01:00:13Z")
	lifecycle.settle("session:turn:first", CodexConversationTurnFailed, "2026-07-15T01:00:14Z")
	assertTurnLifecycle(t, lifecycle.snapshot(), "session:turn:first", CodexConversationTurnCompleted, "2026-07-15T01:00:00Z", "2026-07-15T01:00:12Z")

	// A distinct provider start after settlement advances to the queued turn.
	lifecycle.start("session:turn:queued", "2026-07-15T01:00:15Z")
	assertTurnLifecycle(t, lifecycle.snapshot(), "session:turn:queued", CodexConversationTurnRunning, "2026-07-15T01:00:15Z", "")
}

func TestCodexConversationTurnLifecycle_TerminalOnlyFactHasStableStart(t *testing.T) {
	for _, status := range []string{
		CodexConversationTurnCompleted,
		CodexConversationTurnFailed,
		CodexConversationTurnInterrupted,
		CodexConversationTurnCancelled,
	} {
		t.Run(status, func(t *testing.T) {
			var lifecycle codexConversationTurnLifecycle
			lifecycle.settle("session:turn:tail", status, "2026-07-15T01:00:20Z")
			assertTurnLifecycle(t, lifecycle.snapshot(), "session:turn:tail", status, "2026-07-15T01:00:20Z", "2026-07-15T01:00:20Z")
		})
	}
}

func TestConversationWithTurn_DerivesCompatibilityActive(t *testing.T) {
	var lifecycle codexConversationTurnLifecycle
	withoutTurn := conversationWithTurn(CodexConversation{}, &lifecycle)
	if withoutTurn.Turn != nil || withoutTurn.Active != nil {
		t.Fatalf("without lifecycle = %#v", withoutTurn)
	}

	lifecycle.start("session:turn:one", "2026-07-15T01:00:00Z")
	running := conversationWithTurn(CodexConversation{}, &lifecycle)
	if running.Turn == nil || running.Active == nil || !*running.Active {
		t.Fatalf("running projection = %#v", running)
	}

	lifecycle.settle("session:turn:one", CodexConversationTurnInterrupted, "2026-07-15T01:00:01Z")
	settled := conversationWithTurn(CodexConversation{}, &lifecycle)
	if settled.Turn == nil || settled.Active == nil || *settled.Active {
		t.Fatalf("settled projection = %#v", settled)
	}
}

func TestCodexConversationTurnLifecycle_RetainsOrderedProviderHistory(t *testing.T) {
	var lifecycle codexConversationTurnLifecycle
	lifecycle.start("provider:a", "2026-07-15T01:00:00Z")
	lifecycle.settle("provider:a", CodexConversationTurnCompleted, "2026-07-15T01:00:01Z")
	lifecycle.start("provider:b", "2026-07-15T01:00:02Z")
	lifecycle.settle("provider:b", CodexConversationTurnFailed, "2026-07-15T01:00:03Z")
	lifecycle.start("provider:c", "2026-07-15T01:00:04Z")

	conversation := conversationWithTurn(CodexConversation{}, &lifecycle)
	if len(conversation.ProviderTurns) != 3 {
		t.Fatalf("provider turns = %#v, want three ordered transitions", conversation.ProviderTurns)
	}
	assertTurnLifecycle(t, &conversation.ProviderTurns[0], "provider:a", CodexConversationTurnCompleted, "2026-07-15T01:00:00Z", "2026-07-15T01:00:01Z")
	assertTurnLifecycle(t, &conversation.ProviderTurns[1], "provider:b", CodexConversationTurnFailed, "2026-07-15T01:00:02Z", "2026-07-15T01:00:03Z")
	assertTurnLifecycle(t, &conversation.ProviderTurns[2], "provider:c", CodexConversationTurnRunning, "2026-07-15T01:00:04Z", "")
	if conversation.Turn == nil || conversation.Turn.ID != "provider:c" {
		t.Fatalf("public current turn = %#v", conversation.Turn)
	}

	var adopted codexConversationTurnLifecycle
	adopted.adopt(&lifecycle)
	if got := adopted.snapshots(); len(got) != 3 || got[0].ID != "provider:a" || got[2].ID != "provider:c" {
		t.Fatalf("adopted provider history = %#v", got)
	}
}

func assertTurnLifecycle(t *testing.T, turn *CodexConversationTurn, id, status, startedAt, settledAt string) {
	t.Helper()
	if turn == nil || turn.ID != id || turn.Status != status || turn.StartedAt != startedAt || turn.SettledAt != settledAt {
		t.Fatalf("turn = %#v, want id=%q status=%q start=%q settle=%q", turn, id, status, startedAt, settledAt)
	}
}
