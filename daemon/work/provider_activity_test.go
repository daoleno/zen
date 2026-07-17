package work

import "testing"

func TestProviderActivityLifecycle_PreservesCurrentActivityInvariants(t *testing.T) {
	var lifecycle providerActivityLifecycle
	lifecycle.start("session:activity:first", "2026-07-15T01:00:00Z")
	lifecycle.start("session:activity:first", "2026-07-15T01:00:09Z")
	assertProviderActivity(t, lifecycle.snapshot(), "session:activity:first", ProviderActivityRunning, "2026-07-15T01:00:00Z", "")

	// A distinct provider start and its terminal record cannot replace or settle
	// the Activity that is still running.
	lifecycle.start("session:activity:other", "2026-07-15T01:00:10Z")
	lifecycle.settle("session:activity:other", ProviderActivityFailed, "2026-07-15T01:00:11Z")
	assertProviderActivity(t, lifecycle.snapshot(), "session:activity:first", ProviderActivityRunning, "2026-07-15T01:00:00Z", "")

	lifecycle.settle("session:activity:first", ProviderActivityCompleted, "2026-07-15T01:00:12Z")
	assertProviderActivity(t, lifecycle.snapshot(), "session:activity:first", ProviderActivityCompleted, "2026-07-15T01:00:00Z", "2026-07-15T01:00:12Z")

	// Stale starts and later conflicting terminal records cannot reopen or
	// rewrite a settled Activity.
	lifecycle.start("session:activity:first", "2026-07-15T01:00:13Z")
	lifecycle.settle("session:activity:first", ProviderActivityFailed, "2026-07-15T01:00:14Z")
	assertProviderActivity(t, lifecycle.snapshot(), "session:activity:first", ProviderActivityCompleted, "2026-07-15T01:00:00Z", "2026-07-15T01:00:12Z")

	// Only a distinct provider start after settlement replaces the current fact.
	lifecycle.start("session:activity:next", "2026-07-15T01:00:15Z")
	assertProviderActivity(t, lifecycle.snapshot(), "session:activity:next", ProviderActivityRunning, "2026-07-15T01:00:15Z", "")
}

func TestProviderActivityLifecycle_TerminalOnlyFactHasStableStart(t *testing.T) {
	for _, status := range []ProviderActivityStatus{
		ProviderActivityCompleted,
		ProviderActivityFailed,
		ProviderActivityInterrupted,
		ProviderActivityCancelled,
	} {
		t.Run(string(status), func(t *testing.T) {
			var lifecycle providerActivityLifecycle
			lifecycle.settle("session:activity:tail", status, "2026-07-15T01:00:20Z")
			assertProviderActivity(t, lifecycle.snapshot(), "session:activity:tail", status, "2026-07-15T01:00:20Z", "2026-07-15T01:00:20Z")
		})
	}
}

func TestProviderActivityLifecycle_RejectsInvalidRequiredStart(t *testing.T) {
	var lifecycle providerActivityLifecycle
	lifecycle.start("session:activity:missing", "")
	lifecycle.start("session:activity:invalid", "not-a-time")
	lifecycle.settle("session:activity:terminal-invalid", ProviderActivityCompleted, "not-a-time")
	if got := lifecycle.snapshot(); got != nil {
		t.Fatalf("invalid provider timestamp created Activity: %#v", got)
	}
}

func TestConversationWithActivity_ProjectsOnlyCurrentActivity(t *testing.T) {
	var lifecycle providerActivityLifecycle
	withoutActivity := conversationWithActivity(CodexConversation{}, &lifecycle)
	if withoutActivity.Activity != nil {
		t.Fatalf("without lifecycle = %#v", withoutActivity)
	}

	lifecycle.start("session:activity:one", "2026-07-15T01:00:00Z")
	running := conversationWithActivity(CodexConversation{}, &lifecycle)
	assertProviderActivity(t, running.Activity, "session:activity:one", ProviderActivityRunning, "2026-07-15T01:00:00Z", "")

	var adopted providerActivityLifecycle
	adopted.adopt(&lifecycle)
	lifecycle.settle("session:activity:one", ProviderActivityInterrupted, "2026-07-15T01:00:01Z")
	assertProviderActivity(t, adopted.snapshot(), "session:activity:one", ProviderActivityRunning, "2026-07-15T01:00:00Z", "")
	assertProviderActivity(t, conversationWithActivity(CodexConversation{}, &lifecycle).Activity, "session:activity:one", ProviderActivityInterrupted, "2026-07-15T01:00:00Z", "2026-07-15T01:00:01Z")
}

func assertProviderActivity(t *testing.T, activity *ProviderActivity, id string, status ProviderActivityStatus, startedAt, settledAt string) {
	t.Helper()
	if activity == nil || activity.ID != id || activity.Status != status || activity.StartedAt != startedAt || activity.SettledAt != settledAt {
		t.Fatalf("activity = %#v, want id=%q status=%q start=%q settle=%q", activity, id, status, startedAt, settledAt)
	}
}
