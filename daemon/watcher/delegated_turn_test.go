package watcher

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

func TestReduceDelegatedTurnAdapterIdleUsesElapsedQuietTimeAndSettlesExactlyOnce(t *testing.T) {
	acceptedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	turn := delegatedTurnRecord{
		SchemaVersion: delegatedTurnSchema,
		ID:            "turn-1",
		Status:        delegatedTurnDispatched,
		AcceptedAt:    acceptedAt,
	}

	turn, changed := reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider: ProviderActivityObservation{Structured: false, FallbackAllowed: true},
		Pane:     classifier.ActivitySignal{State: classifier.StateRunning, Source: "codex_pane_working"},
		Live:     true,
		Now:      acceptedAt.Add(time.Second),
	})
	if !changed || turn.Status != delegatedTurnRunning {
		t.Fatalf("working observation = (%+v, %v), want running change", turn, changed)
	}

	turn, changed = reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider: ProviderActivityObservation{Structured: false, FallbackAllowed: true},
		Pane:     classifier.ActivitySignal{State: classifier.StateUnknown, Source: "codex_idle"},
		Live:     true,
		Now:      acceptedAt.Add(2 * time.Second),
	})
	if !changed || turn.Status != delegatedTurnIdle || turn.IdleSince == nil {
		t.Fatalf("first idle = (%+v, %v), want persisted quiet boundary", turn, changed)
	}

	idleSince := *turn.IdleSince
	for poll := 0; poll < 50; poll++ {
		next, pollChanged := reduceDelegatedTurn(turn, delegatedTurnObservation{
			Provider: ProviderActivityObservation{Structured: false, FallbackAllowed: true},
			Pane:     classifier.ActivitySignal{State: classifier.StateUnknown, Source: "codex_idle"},
			Live:     true,
			Now:      idleSince.Add(time.Duration(poll+1) * time.Millisecond),
		})
		if pollChanged || next.Status != delegatedTurnIdle {
			t.Fatalf("adapter-idle poll %d completed by poll count: (%+v, %v)", poll, next, pollChanged)
		}
		turn = next
	}

	turn, changed = reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider: ProviderActivityObservation{Structured: false, FallbackAllowed: true},
		Pane:     classifier.ActivitySignal{State: classifier.StateUnknown, Source: "codex_idle"},
		Live:     true,
		Now:      idleSince.Add(delegatedTurnGenericQuietWindow),
	})
	if !changed || turn.Status != delegatedTurnDone || turn.SettledAt == nil {
		t.Fatalf("adapter idle at quiet deadline = (%+v, %v), want done", turn, changed)
	}

	settled := turn
	turn, changed = reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider: ProviderActivityObservation{Structured: false, FallbackAllowed: true},
		Pane:     classifier.ActivitySignal{State: classifier.StateUnknown, Source: "codex_idle"},
		Live:     true,
		Now:      idleSince.Add(delegatedTurnGenericQuietWindow + time.Second),
	})
	if changed || turn != settled {
		t.Fatalf("terminal turn changed on duplicate poll: before=%+v after=%+v changed=%v", settled, turn, changed)
	}
}

func TestReduceDelegatedTurnRequiresObservedRunAndIgnoresRedraws(t *testing.T) {
	acceptedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	turn := delegatedTurnRecord{
		SchemaVersion: delegatedTurnSchema,
		ID:            "turn-2",
		Status:        delegatedTurnDispatched,
		AcceptedAt:    acceptedAt,
	}
	for index := 0; index < 5; index++ {
		var changed bool
		turn, changed = reduceDelegatedTurn(turn, delegatedTurnObservation{
			Provider: ProviderActivityObservation{Structured: false, FallbackAllowed: true},
			Pane:     classifier.ActivitySignal{State: classifier.StateUnknown, Source: "codex_idle"},
			Live:     true,
			Now:      acceptedAt.Add(time.Duration(index+1) * time.Second),
		})
		if changed || turn.Status != delegatedTurnDispatched {
			t.Fatalf("idle/redraw %d settled unobserved turn: %+v", index, turn)
		}
	}
}

func TestReduceDelegatedTurnDeadProviderFailsExactlyOnce(t *testing.T) {
	acceptedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	turn := delegatedTurnRecord{
		SchemaVersion: delegatedTurnSchema,
		ID:            "turn-dead",
		Status:        delegatedTurnRunning,
		AcceptedAt:    acceptedAt,
	}
	turn, changed := reduceDelegatedTurn(turn, delegatedTurnObservation{
		Live: false,
		Now:  acceptedAt.Add(time.Second),
	})
	if !changed || turn.Status != delegatedTurnFailed || turn.SettledAt == nil {
		t.Fatalf("dead provider = (%+v, %v), want failed", turn, changed)
	}
	settled := turn
	turn, changed = reduceDelegatedTurn(turn, delegatedTurnObservation{
		Live: false,
		Now:  acceptedAt.Add(2 * time.Second),
	})
	if changed || turn != settled {
		t.Fatalf("dead provider duplicated settlement: before=%+v after=%+v", settled, turn)
	}
}

func TestReduceDelegatedTurnPrefersStructuredCrossProviderTerminalFacts(t *testing.T) {
	acceptedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	tests := []struct {
		provider string
		status   string
		want     delegatedTurnStatus
	}{
		{provider: "codex", status: "completed", want: delegatedTurnDone},
		{provider: "cursor", status: "failed", want: delegatedTurnFailed},
		{provider: "claude", status: "completed", want: delegatedTurnDone},
		{provider: "grok", status: "failed", want: delegatedTurnFailed},
	}
	for _, test := range tests {
		t.Run(test.provider+"_"+test.status, func(t *testing.T) {
			turn := delegatedTurnRecord{
				SchemaVersion:   delegatedTurnSchema,
				ID:              "turn-" + test.provider,
				Status:          delegatedTurnDispatched,
				AcceptedAt:      acceptedAt,
				ProcessIdentity: "process",
			}
			settledAt := acceptedAt.Add(2 * time.Second)
			got, changed := reduceDelegatedTurn(turn, delegatedTurnObservation{
				Provider: ProviderActivityObservation{
					ID:         test.provider + "-activity",
					Status:     test.status,
					StartedAt:  acceptedAt.Add(time.Second),
					SettledAt:  settledAt,
					Structured: true,
				},
				Pane: classifier.ActivitySignal{
					State:  classifier.StateRunning,
					Source: test.provider + "_pane_working",
				},
				Live: true,
				Now:  settledAt.Add(time.Second),
			})
			if !changed || got.Status != test.want || got.ProviderActivity != test.provider+"-activity" ||
				got.SettledAt == nil || !got.SettledAt.Equal(settledAt) {
				t.Fatalf("structured settlement = (%+v, %v), want %s", got, changed, test.want)
			}
		})
	}
}

func TestReduceDelegatedTurnDoesNotUsePaneFallbackWhileStructuredSourceIsPending(t *testing.T) {
	acceptedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	turn := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "turn-claude",
		Status:          delegatedTurnDispatched,
		AcceptedAt:      acceptedAt,
		ProcessIdentity: "process",
	}
	got, changed := reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider: ProviderActivityObservation{Structured: true, FallbackAllowed: false},
		Pane:     classifier.ActivitySignal{State: classifier.StateUnknown, Source: "claude_idle"},
		Live:     true,
		Now:      acceptedAt.Add(time.Second),
	})
	if changed || got != turn {
		t.Fatalf("structured provider fell back to pane idle: before=%+v after=%+v", turn, got)
	}
}

func TestDelegatedTurnProgressCannotSettleNewerUnobservedTurn(t *testing.T) {
	acceptedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	turn := delegatedTurnRecord{
		SchemaVersion: delegatedTurnSchema,
		ID:            "turn-new",
		Status:        delegatedTurnDispatched,
		AcceptedAt:    acceptedAt,
	}
	got, changed := settleDelegatedTurnFromProgress(
		turn,
		classifier.StateDone,
		"stale older completion",
		acceptedAt.Add(time.Second),
	)
	if changed || got != turn {
		t.Fatalf("unobserved newer turn was settled by stale progress: before=%+v after=%+v", turn, got)
	}

	turn.Status = delegatedTurnRunning
	got, changed = settleDelegatedTurnFromProgress(
		turn,
		classifier.StateDone,
		"current completion",
		acceptedAt.Add(2*time.Second),
	)
	if !changed || got.Status != delegatedTurnDone || got.Summary != "current completion" {
		t.Fatalf("current progress did not settle early: (%+v, %v)", got, changed)
	}
}

type countingProviderActivityProbe struct {
	calls int
}

func (p *countingProviderActivityProbe) ObserveProviderActivity(
	classifier.Agent,
	time.Time,
) ProviderActivityObservation {
	p.calls++
	return ProviderActivityObservation{Structured: true}
}

func (*countingProviderActivityProbe) ForgetProviderActivity(string) {}

func TestDelegatedTurnInventorySkipsHistoricalSessionsWithoutPerSessionProbe(t *testing.T) {
	probe := &countingProviderActivityProbe{}
	now := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	terminalAt := now.Add(-time.Minute)
	terminal := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "historical",
		Status:          delegatedTurnDone,
		AcceptedAt:      now.Add(-2 * time.Minute),
		ProcessIdentity: "process",
		SettledAt:       &terminalAt,
	}

	if _, observed := providerActivityForDelegatedTurn(
		classifier.Agent{ID: "empty-history"},
		now,
		delegatedTurnRecord{},
		false,
		nil,
		probe,
	); observed {
		t.Fatal("empty historical Session requested a provider probe")
	}
	if _, observed := providerActivityForDelegatedTurn(
		classifier.Agent{ID: "terminal-history"},
		now,
		terminal,
		true,
		nil,
		probe,
	); observed {
		t.Fatal("terminal historical Session requested a provider probe")
	}
	if probe.calls != 0 {
		t.Fatalf("provider probe calls = %d, want zero for idle history", probe.calls)
	}
}

func TestDelegatedTurnMarkerUsesBulkWatcherInventoryNotPerSessionTmuxRead(t *testing.T) {
	source, err := os.ReadFile("watcher.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if !strings.Contains(text, "#{@zen_delegated_turn}") ||
		!strings.Contains(text, "decodeDelegatedTurn(item.delegatedTurnRaw)") {
		t.Fatal("delegated turn marker is not decoded from the bulk tmux inventory row")
	}
	if strings.Contains(text, "readDelegatedTurn(") {
		t.Fatal("watcher poll contains a per-Session delegated turn read")
	}
}

func TestUnknownProviderFallbackAcceptedWorkingStableIdleAndRestartTerminal(t *testing.T) {
	acceptedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	turn := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "unknown-turn",
		Status:          delegatedTurnDispatched,
		AcceptedAt:      acceptedAt,
		ProcessIdentity: "unknown-process",
	}
	provider := ProviderActivityObservation{FallbackAllowed: true}

	pane := delegatedTurnFallbackPaneActivity(
		turn,
		provider,
		classifier.ActivitySignal{},
		true,
	)
	turn, changed := reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider: provider,
		Pane:     pane,
		Live:     true,
		Now:      acceptedAt.Add(time.Second),
	})
	if !changed || turn.Status != delegatedTurnRunning {
		t.Fatalf("unknown provider working = (%+v, %v)", turn, changed)
	}

	quietStartedAt := acceptedAt.Add(2 * time.Second)
	for observation := 0; observation < 100; observation++ {
		pane = delegatedTurnFallbackPaneActivity(
			turn,
			provider,
			classifier.ActivitySignal{},
			false,
		)
		next, idleChanged := reduceDelegatedTurn(turn, delegatedTurnObservation{
			Provider: provider,
			Pane:     pane,
			Live:     true,
			Now:      quietStartedAt.Add(time.Duration(observation) * time.Millisecond),
		})
		if observation == 0 {
			if !idleChanged || next.Status != delegatedTurnIdle || next.IdleSince == nil {
				t.Fatalf("first quiet observation did not persist idle boundary: (%+v, %v)", next, idleChanged)
			}
		} else if idleChanged {
			t.Fatalf("rapid quiet poll %d rewrote durable state: %+v", observation, next)
		}
		turn = next
	}
	if turn.Status != delegatedTurnIdle {
		t.Fatalf("rapid polls completed unknown provider before quiet deadline: %+v", turn)
	}
	idleSince := *turn.IdleSince
	before, changed := reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider: provider,
		Pane:     pane,
		Live:     true,
		Now:      idleSince.Add(delegatedTurnGenericQuietWindow - time.Nanosecond),
	})
	if changed || before.Status != delegatedTurnIdle {
		t.Fatalf("unknown provider completed before quiet deadline: (%+v, %v)", before, changed)
	}
	turn, changed = reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider: provider,
		Pane:     pane,
		Live:     true,
		Now:      idleSince.Add(delegatedTurnGenericQuietWindow),
	})
	if !changed || turn.Status != delegatedTurnDone || turn.SettledAt == nil {
		t.Fatalf("unknown provider did not settle at quiet deadline: (%+v, %v)", turn, changed)
	}

	restarted, changed := reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider: provider,
		Pane:     classifier.ActivitySignal{State: classifier.StateRunning, Source: "generic_pane_activity"},
		Live:     true,
		Now:      acceptedAt.Add(10 * time.Second),
	})
	if changed || restarted != turn {
		t.Fatalf("restart/repeated observation replayed terminal turn: before=%+v after=%+v", turn, restarted)
	}
}

func TestUnknownProviderPermanentHelperDoesNotPreventQuietCompletion(t *testing.T) {
	acceptedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	processes := map[int]processInfo{
		10: {pid: 10, ppid: 1, comm: "future-agent"},
		11: {pid: 11, ppid: 10, comm: "persistent-helper"},
	}
	if descendants := descendantProcesses(10, processes); len(descendants) != 1 {
		t.Fatalf("test setup descendants = %v, want one permanent helper", descendants)
	}

	provider := ProviderActivityObservation{FallbackAllowed: true}
	turn := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "unknown-with-helper",
		Status:          delegatedTurnDispatched,
		AcceptedAt:      acceptedAt,
		ProcessIdentity: "future-agent-process",
	}
	pane := delegatedTurnFallbackPaneActivity(
		turn,
		provider,
		classifier.ActivitySignal{},
		true,
	)
	turn, changed := reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider: provider,
		Pane:     pane,
		Live:     true,
		Now:      acceptedAt.Add(time.Second),
	})
	if !changed || turn.Status != delegatedTurnRunning {
		t.Fatalf("unknown provider did not start: (%+v, %v)", turn, changed)
	}

	pane = delegatedTurnFallbackPaneActivity(
		turn,
		provider,
		classifier.ActivitySignal{},
		false,
	)
	turn, changed = reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider: provider,
		Pane:     pane,
		Live:     true,
		Now:      acceptedAt.Add(2 * time.Second),
	})
	if !changed || turn.Status != delegatedTurnIdle || turn.IdleSince == nil {
		t.Fatalf("permanent helper prevented idle boundary: (%+v, %v)", turn, changed)
	}
	turn, changed = reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider: provider,
		Pane:     pane,
		Live:     true,
		Now:      turn.IdleSince.Add(delegatedTurnGenericQuietWindow),
	})
	if !changed || turn.Status != delegatedTurnDone {
		t.Fatalf("permanent helper prevented quiet completion: (%+v, %v)", turn, changed)
	}
}

func TestDispatchedTurnWithoutObservedStartFailsOnceAtExistingTimeout(t *testing.T) {
	dispatchedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	turn := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "never-started",
		Status:          delegatedTurnDispatched,
		AcceptedAt:      dispatchedAt,
		ProcessIdentity: "same-live-process",
	}
	timeout := 8 * time.Second
	got, changed := reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider:     ProviderActivityObservation{Structured: true},
		Live:         true,
		Now:          dispatchedAt.Add(timeout - time.Nanosecond),
		StartTimeout: timeout,
	})
	if changed || got != turn {
		t.Fatalf("turn failed before startup timeout: before=%+v after=%+v", turn, got)
	}
	got, changed = reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider:     ProviderActivityObservation{Structured: true},
		Live:         true,
		Now:          dispatchedAt.Add(timeout),
		StartTimeout: timeout,
	})
	if !changed || got.Status != delegatedTurnFailed || got.SettledAt == nil ||
		!strings.Contains(got.Summary, "start was not observed") {
		t.Fatalf("bounded no-start outcome = (%+v, %v)", got, changed)
	}
	settled := got
	got, changed = reduceDelegatedTurn(got, delegatedTurnObservation{
		Live:         true,
		Now:          dispatchedAt.Add(2 * timeout),
		StartTimeout: timeout,
	})
	if changed || got != settled {
		t.Fatalf("no-start failure duplicated: before=%+v after=%+v", settled, got)
	}
}

func TestGenericRunningAtStartDeadlineOutranksUnavailableStructuredAdapter(t *testing.T) {
	dispatchedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	turn := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "adapter-unavailable",
		Status:          delegatedTurnDispatched,
		AcceptedAt:      dispatchedAt,
		ProcessIdentity: "process",
		PaneBaseline:    delegatedTurnPaneIdentity("prompt"),
	}
	provider := ProviderActivityObservation{
		Structured:      true,
		FallbackAllowed: true,
	}
	pane := delegatedTurnFallbackPaneActivity(
		turn,
		provider,
		classifier.ActivitySignal{},
		true,
	)
	got, changed := reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider:     provider,
		Pane:         pane,
		Live:         true,
		Now:          dispatchedAt.Add(8 * time.Second),
		StartTimeout: 8 * time.Second,
	})
	if !changed || got.Status != delegatedTurnRunning {
		t.Fatalf("generic running lost to no-start deadline: (%+v, %v)", got, changed)
	}
}

func TestZenOwnedPromptPaneMutationDoesNotEstablishGenericRunning(t *testing.T) {
	dispatchedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	promptPane := "future-agent\nuser prompt still in composer"
	turn := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "draft-not-submitted",
		Status:          delegatedTurnDispatched,
		AcceptedAt:      dispatchedAt,
		ProcessIdentity: "same-process",
		PaneBaseline:    delegatedTurnPaneIdentity(promptPane),
	}
	provider := ProviderActivityObservation{FallbackAllowed: true}
	pane := delegatedTurnFallbackPaneActivity(
		turn,
		provider,
		classifier.ActivitySignal{},
		// Watcher observed its own prompt paste, but the post-dispatch content
		// still equals the marker baseline.
		delegatedTurnPaneIdentity(promptPane) != turn.PaneBaseline,
	)
	got, changed := reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider:     provider,
		Pane:         pane,
		Live:         true,
		Now:          dispatchedAt.Add(8 * time.Second),
		StartTimeout: 8 * time.Second,
	})
	if !changed || got.Status != delegatedTurnFailed ||
		!strings.Contains(got.Summary, "start was not observed") {
		t.Fatalf("Zen-owned prompt mutation became provider work: (%+v, %v)", got, changed)
	}
}

func TestStructuredActivityAndFallbackCannotCreateCompetingTerminalOwner(t *testing.T) {
	acceptedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	settledAt := acceptedAt.Add(2 * time.Second)
	turn := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "one-owner",
		Status:          delegatedTurnDispatched,
		AcceptedAt:      acceptedAt,
		ProcessIdentity: "process",
	}
	turn, changed := reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider: ProviderActivityObservation{
			ID:         "native-activity",
			Status:     "completed",
			StartedAt:  acceptedAt.Add(time.Second),
			SettledAt:  settledAt,
			Structured: true,
		},
		Pane: classifier.ActivitySignal{State: classifier.StateRunning, Source: "generic_pane_activity"},
		Live: true,
		Now:  settledAt,
	})
	if !changed || turn.Status != delegatedTurnDone {
		t.Fatalf("structured owner did not settle turn: (%+v, %v)", turn, changed)
	}
	got, changed := reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider: ProviderActivityObservation{FallbackAllowed: true},
		Pane:     classifier.ActivitySignal{State: classifier.StateUnknown, Source: "generic_pane_stable"},
		Live:     true,
		Now:      settledAt.Add(time.Second),
	})
	if changed || got != turn {
		t.Fatalf("fallback created competing terminal lifecycle: before=%+v after=%+v", turn, got)
	}
}

func TestAmbiguousHandoffAdoptsCorrelatedActivityAndSettlesOnce(t *testing.T) {
	dispatchedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	turn := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "ambiguous-correlated",
		Status:          delegatedTurnAmbiguous,
		AcceptedAt:      dispatchedAt,
		ProcessIdentity: "process",
		PaneBaseline:    delegatedTurnPaneIdentity("before"),
	}
	turn, changed := reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider: ProviderActivityObservation{
			ID:         "native-turn",
			Status:     "running",
			StartedAt:  dispatchedAt.Add(time.Second),
			Structured: true,
		},
		Live: true,
		Now:  dispatchedAt.Add(time.Second),
	})
	if !changed || turn.Status != delegatedTurnRunning {
		t.Fatalf("ambiguous correlated running = (%+v, %v)", turn, changed)
	}
	settledAt := dispatchedAt.Add(2 * time.Second)
	turn, changed = reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider: ProviderActivityObservation{
			ID:         "native-turn",
			Status:     "completed",
			StartedAt:  dispatchedAt.Add(time.Second),
			SettledAt:  settledAt,
			Structured: true,
		},
		Live: true,
		Now:  settledAt,
	})
	if !changed || turn.Status != delegatedTurnDone {
		t.Fatalf("ambiguous correlated completion = (%+v, %v)", turn, changed)
	}
	restarted, changed := reduceDelegatedTurn(turn, delegatedTurnObservation{
		Live: true,
		Now:  settledAt.Add(time.Minute),
	})
	if changed || restarted != turn {
		t.Fatalf("ambiguous completion replayed after restart: before=%+v after=%+v", turn, restarted)
	}
}

func TestAmbiguousGenericHandoffRequiresActivityBeyondComposerMutation(t *testing.T) {
	dispatchedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	turn := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "ambiguous-generic",
		Status:          delegatedTurnAmbiguous,
		AcceptedAt:      dispatchedAt,
		ProcessIdentity: "process",
		PaneBaseline:    delegatedTurnPaneIdentity("before"),
	}
	firstPane := delegatedTurnPaneIdentity("Zen pasted prompt")
	turn, changed := reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider:     ProviderActivityObservation{FallbackAllowed: true},
		Pane:         classifier.ActivitySignal{State: classifier.StateRunning, Source: "generic_pane_activity"},
		PaneIdentity: firstPane,
		Live:         true,
		Now:          dispatchedAt.Add(time.Second),
	})
	if !changed || turn.Status != delegatedTurnAmbiguous ||
		turn.PaneBaseline != firstPane || !turn.ComposerObserved {
		t.Fatalf("first ambiguous pane mutation incorrectly proved work: (%+v, %v)", turn, changed)
	}
	turn, changed = reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider:     ProviderActivityObservation{FallbackAllowed: true},
		Pane:         classifier.ActivitySignal{State: classifier.StateRunning, Source: "generic_pane_activity"},
		PaneIdentity: delegatedTurnPaneIdentity("provider output"),
		Live:         true,
		Now:          dispatchedAt.Add(2 * time.Second),
	})
	if !changed || turn.Status != delegatedTurnRunning {
		t.Fatalf("second distinct ambiguous activity did not prove running: (%+v, %v)", turn, changed)
	}
}

func TestAmbiguousHandoffWithoutEvidenceFailsOnceAtBoundedDeadline(t *testing.T) {
	dispatchedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	turn := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "ambiguous-timeout",
		Status:          delegatedTurnAmbiguous,
		AcceptedAt:      dispatchedAt,
		ProcessIdentity: "process",
		PaneBaseline:    delegatedTurnPaneIdentity("prompt"),
	}
	turn, changed := reduceDelegatedTurn(turn, delegatedTurnObservation{
		Provider:     ProviderActivityObservation{FallbackAllowed: true},
		Live:         true,
		Now:          dispatchedAt.Add(8 * time.Second),
		StartTimeout: 8 * time.Second,
	})
	if !changed || turn.Status != delegatedTurnFailed ||
		!strings.Contains(turn.Summary, "stayed ambiguous") {
		t.Fatalf("ambiguous timeout = (%+v, %v)", turn, changed)
	}
	settled := turn
	turn, changed = reduceDelegatedTurn(turn, delegatedTurnObservation{
		Live:         true,
		Now:          dispatchedAt.Add(time.Minute),
		StartTimeout: 8 * time.Second,
	})
	if changed || turn != settled {
		t.Fatalf("ambiguous timeout duplicated after restart: before=%+v after=%+v", settled, turn)
	}
}
