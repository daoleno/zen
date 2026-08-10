package watcher

import (
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

// fixedStateProbe replays one constant provider observation, used to drive
// the bounded provider-evidence-loss path deterministically.
type fixedStateProbe struct {
	obs ProviderActivityObservation
}

func (p *fixedStateProbe) ObserveProviderActivity(classifier.Agent, time.Time) ProviderActivityObservation {
	if p == nil {
		return ProviderActivityObservation{}
	}
	return p.obs
}

func (p *fixedStateProbe) ForgetProviderActivity(string) {}

// boundedLossPollTimes returns 1-second-spaced poll instants covering a
// bounded loss window plus margin.
func boundedLossPollTimes(base time.Time, seconds int) []time.Time {
	times := make([]time.Time, 0, seconds+1)
	for i := 0; i <= seconds; i++ {
		times = append(times, base.Add(time.Duration(i)*time.Second))
	}
	return times
}

// TestPollProviderEvidenceLossEmitsUncertainAfterBoundedWindow covers Slice 2:
// a persistently unreadable transcript yields exactly one canonical
// provider-uncertain fact after the bounded window; a healthy recovery before
// the window resets the streak; a successful read with no new fact is never a
// loss.
func TestPollProviderEvidenceLossEmitsUncertainAfterBoundedWindow(t *testing.T) {
	base := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	sessionID := "brain-agent-loss:@1"
	turnID := sessionID + ":turn:1"

	newWatcher := func(times []time.Time) (*Watcher, *fakeTurnLedger) {
		w := New(time.Second)
		w.pollNow = fakePollClock(times)
		ledger := newFakeTurnLedger()
		ledger.seed(sessionID, TurnSnapshot{
			SessionID:       sessionID,
			TurnID:          turnID,
			Status:          TurnAccepted,
			AcceptedAt:      base,
			ProcessIdentity: "recorded-proc",
		})
		w.turnLedger = ledger
		return w, ledger
	}

	t.Run("unreadable after window", func(t *testing.T) {
		w, ledger := newWatcher(boundedLossPollTimes(base, 95))
		w.providerActivityProbe = &fixedStateProbe{obs: ProviderActivityObservation{
			Structured: true, FallbackAllowed: true, ProbeState: ProbeStateUnreadable,
		}}
		windows := []tmuxWindow{{target: sessionID, name: "worker", cwd: "/repo/zen", command: "opencode", panePID: 333, delegated: true}}
		restore := installFakePollSeams(windows, map[string]string{sessionID: "OpenCode\nworking\n"}, map[int]processInfo{333: fakeProcess(333, base)})
		defer restore()
		for i := 0; i < 95; i++ {
			w.poll()
		}
		drainWatcherEvents(w)
		uncertain := 0
		for _, fact := range ledger.applied {
			if fact.Class == EvidenceProvider && fact.Kind == "uncertain" {
				uncertain++
			}
		}
		if uncertain != 1 {
			t.Fatalf("provider-loss uncertain facts = %d, want exactly one: %#v", uncertain, ledger.applied)
		}
	})

	t.Run("unlocatable after window", func(t *testing.T) {
		w, ledger := newWatcher(boundedLossPollTimes(base, 95))
		w.providerActivityProbe = &fixedStateProbe{obs: ProviderActivityObservation{
			Structured: true, FallbackAllowed: true, ProbeState: ProbeStateUnlocatable,
		}}
		windows := []tmuxWindow{{target: sessionID, name: "worker", cwd: "/repo/zen", command: "opencode", panePID: 333, delegated: true}}
		restore := installFakePollSeams(windows, map[string]string{sessionID: "OpenCode\nworking\n"}, map[int]processInfo{333: fakeProcess(333, base)})
		defer restore()
		for i := 0; i < 95; i++ {
			w.poll()
		}
		drainWatcherEvents(w)
		uncertain := 0
		for _, fact := range ledger.applied {
			if fact.Class == EvidenceProvider && fact.Kind == "uncertain" {
				uncertain++
			}
		}
		if uncertain != 1 {
			t.Fatalf("provider-loss uncertain facts = %d, want exactly one", uncertain)
		}
	})

	t.Run("recovery resets the streak", func(t *testing.T) {
		times := boundedLossPollTimes(base, 200)
		w, ledger := newWatcher(times)
		probe := &fixedStateProbe{obs: ProviderActivityObservation{
			Structured: true, FallbackAllowed: true, ProbeState: ProbeStateUnreadable,
		}}
		w.providerActivityProbe = probe
		windows := []tmuxWindow{{target: sessionID, name: "worker", cwd: "/repo/zen", command: "opencode", panePID: 333, delegated: true}}
		restore := installFakePollSeams(windows, map[string]string{sessionID: "OpenCode\nworking\n"}, map[int]processInfo{333: fakeProcess(333, base)})
		defer restore()

		// 50 seconds of loss, then a healthy read resets the streak, then
		// another 50 seconds of loss: no fact ever fires (each streak is
		// shorter than the 90s window).
		for i := 0; i < 50; i++ {
			w.poll()
		}
		probe.obs = ProviderActivityObservation{Structured: true, FallbackAllowed: true, ProbeState: ProbeStateOK}
		for i := 0; i < 10; i++ {
			w.poll()
		}
		probe.obs = ProviderActivityObservation{Structured: true, FallbackAllowed: true, ProbeState: ProbeStateUnreadable}
		for i := 0; i < 50; i++ {
			w.poll()
		}
		drainWatcherEvents(w)
		for _, fact := range ledger.applied {
			if fact.Class == EvidenceProvider && fact.Kind == "uncertain" {
				t.Fatalf("interrupted loss streaks fired uncertain: %#v", ledger.applied)
			}
		}
	})

	t.Run("new turn restarts the loss streak", func(t *testing.T) {
		// 1s-spaced polls: turn 1 loses evidence for 60s, then the current
		// turn becomes turn 2. The predecessor's streak must not make the new
		// turn immediately uncertain: by cumulative t=90s the session-keyed
		// streak would fire, but the turn-keyed streak is only ~30s old. The
		// new turn's own 90s window fires exactly one uncertain fact.
		times := make([]time.Time, 0, 155)
		for i := 0; i <= 154; i++ {
			times = append(times, base.Add(time.Duration(i)*time.Second))
		}
		w, ledger := newWatcher(times)
		w.providerActivityProbe = &fixedStateProbe{obs: ProviderActivityObservation{
			Structured: true, FallbackAllowed: true, ProbeState: ProbeStateUnreadable,
		}}
		windows := []tmuxWindow{{target: sessionID, name: "worker", cwd: "/repo/zen", command: "opencode", panePID: 333, delegated: true}}
		restore := installFakePollSeams(windows, map[string]string{sessionID: "OpenCode\nworking\n"}, map[int]processInfo{333: fakeProcess(333, base)})
		defer restore()

		for i := 0; i < 60; i++ {
			w.poll()
		}
		ledger.seed(sessionID, TurnSnapshot{
			SessionID: sessionID, TurnID: turnID + "-2",
			Status: TurnAccepted, AcceptedAt: base.Add(60 * time.Second),
			ProcessIdentity: "recorded-proc",
		})
		// 32 polls into the new turn: cumulative loss reaches 90s, but the
		// new turn's own streak has not — nothing may fire.
		for i := 0; i < 32; i++ {
			w.poll()
		}
		drainWatcherEvents(w)
		for _, fact := range ledger.applied {
			if fact.Class == EvidenceProvider && fact.Kind == "uncertain" {
				t.Fatalf("predecessor streak fired uncertain for the new turn: %#v", ledger.applied)
			}
		}
		// 62 more polls complete the new turn's own 90s window: exactly one
		// uncertain fact, targeting the new turn.
		for i := 0; i < 62; i++ {
			w.poll()
		}
		drainWatcherEvents(w)
		losses := 0
		for _, fact := range ledger.applied {
			if fact.Class == EvidenceProvider && fact.Kind == "uncertain" {
				losses++
				if fact.TurnID != turnID+"-2" {
					t.Fatalf("loss fact targets %s, want the new turn", fact.TurnID)
				}
			}
		}
		if losses != 1 {
			t.Fatalf("new-turn loss facts = %d, want exactly one", losses)
		}
	})

	t.Run("read success no new fact is never a loss", func(t *testing.T) {
		w, ledger := newWatcher(boundedLossPollTimes(base, 95))
		w.providerActivityProbe = &fixedStateProbe{obs: ProviderActivityObservation{
			Structured: true, FallbackAllowed: true, ProbeState: ProbeStateOK,
		}}
		windows := []tmuxWindow{{target: sessionID, name: "worker", cwd: "/repo/zen", command: "opencode", panePID: 333, delegated: true}}
		restore := installFakePollSeams(windows, map[string]string{sessionID: "OpenCode\nworking\n"}, map[int]processInfo{333: fakeProcess(333, base)})
		defer restore()
		for i := 0; i < 95; i++ {
			w.poll()
		}
		drainWatcherEvents(w)
		for _, fact := range ledger.applied {
			if fact.Class == EvidenceProvider && fact.Kind == "uncertain" {
				t.Fatalf("healthy no-fact reads fired uncertain: %#v", ledger.applied)
			}
		}
	})
}

// TestLedgerTranscriptBindingRestoresAndBackfills covers Slice 2 transcript
// binding: the ledger record is the durable truth restored onto the Session
// command on rediscovery; the tmux option / launch command is only an
// advisory cache that backfills a missing binding idempotently.
func TestLedgerTranscriptBindingRestoresAndBackfills(t *testing.T) {
	sessionID := "brain-agent-pi:@1"
	ownedPath := "/home/user/.zen/pi-sessions/owned.jsonl"

	t.Run("restore from ledger", func(t *testing.T) {
		w := New(time.Second)
		agent := &classifier.Agent{
			ID:      sessionID,
			Command: "pi", // rediscovered without the owned flag (argv rewrite)
		}
		turn := TurnSnapshot{
			SessionID: sessionID,
			TurnID:    sessionID + ":turn:1",
			TranscriptBinding: TranscriptBinding{
				Provider: "pi", PiFlag: "--session", PiPath: ownedPath,
			},
		}
		w.mu.Lock()
		w.restoreTurnTranscriptBindingLocked(agent, turn)
		w.mu.Unlock()
		want := "pi --session " + shellQuoteForLaunch(ownedPath)
		if agent.Command != want {
			t.Fatalf("restored command = %q, want %q", agent.Command, want)
		}
	})

	t.Run("backfill idempotently", func(t *testing.T) {
		w := New(time.Second)
		ledger := &bindingBackfillLedger{inner: newFakeTurnLedger()}
		w.turnLedger = ledger
		agent := &classifier.Agent{
			ID:      sessionID,
			Command: "pi --session " + shellQuoteForLaunch(ownedPath),
		}
		w.mu.Lock()
		w.restoreTurnTranscriptBindingLocked(agent, TurnSnapshot{SessionID: sessionID, TurnID: sessionID + ":turn:1"})
		w.mu.Unlock()
		if len(ledger.backfilled) != 1 || ledger.backfilled[0].PiPath != ownedPath {
			t.Fatalf("backfill calls = %#v", ledger.backfilled)
		}
		// Idempotent: a second restore with the binding now present performs
		// no store write.
		w.mu.Lock()
		w.restoreTurnTranscriptBindingLocked(agent, TurnSnapshot{
			SessionID: sessionID, TurnID: sessionID + ":turn:1",
			TranscriptBinding: TranscriptBinding{Provider: "pi", PiFlag: "--session", PiPath: ownedPath},
		})
		w.mu.Unlock()
		if len(ledger.backfilled) != 1 {
			t.Fatalf("idempotent backfill wrote again: %#v", ledger.backfilled)
		}
	})

	t.Run("non-pi command never rewritten", func(t *testing.T) {
		w := New(time.Second)
		agent := &classifier.Agent{ID: sessionID, Command: "opencode"}
		turn := TurnSnapshot{
			SessionID: sessionID,
			TranscriptBinding: TranscriptBinding{
				Provider: "pi", PiFlag: "--session", PiPath: ownedPath,
			},
		}
		w.mu.Lock()
		w.restoreTurnTranscriptBindingLocked(agent, turn)
		w.mu.Unlock()
		if agent.Command != "opencode" {
			t.Fatalf("non-pi command rewritten: %q", agent.Command)
		}
	})
}

// bindingBackfillLedger wraps the fake ledger with the transcript-binding
// backfill surface used by the watcher.
type bindingBackfillLedger struct {
	inner      *fakeTurnLedger
	backfilled []TranscriptBinding
}

func (l *bindingBackfillLedger) Turn(sessionID string) (TurnSnapshot, bool, error) {
	return l.inner.Turn(sessionID)
}

func (l *bindingBackfillLedger) ApplyTurnFact(fact TurnFact) (TurnSnapshot, bool, error) {
	return l.inner.ApplyTurnFact(fact)
}

func (l *bindingBackfillLedger) ApplyDelegatedTurnProgress(fact TurnFact) (TurnProgressResult, error) {
	return l.inner.ApplyDelegatedTurnProgress(fact)
}

func (l *bindingBackfillLedger) BackfillTurnTranscriptBinding(sessionID string, binding TranscriptBinding) (bool, error) {
	l.backfilled = append(l.backfilled, binding)
	return true, nil
}
