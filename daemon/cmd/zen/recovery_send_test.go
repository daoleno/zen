package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
)

type recoveryNativeProbe struct{}

func (recoveryNativeProbe) ObserveProviderActivity(w classifier.Worker, now time.Time) watcher.ProviderActivityObservation {
	return watcher.ProviderActivityObservation{ID: "native:" + w.ID, Status: "running", Structured: true, ProgressAt: now}
}
func (recoveryNativeProbe) ForgetProviderActivity(string) {}

// Real control -> watcher identity/paste/receipt -> Store/FSM. The owned native
// executable only records stdin; no provider executable or network is used.
func TestControlSendAfterLostOriginalAndClosedRecovery(t *testing.T) {
	for _, orphan := range []bool{false, true} {
		t.Run(fmt.Sprintf("orphan_prepare=%t", orphan), func(t *testing.T) { testControlRecoverySend(t, orphan) })
	}
}

func testControlRecoverySend(t *testing.T, orphan bool) {
	root, err := os.MkdirTemp("", "zen-recovery-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	realTmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux required")
	}
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("C compiler required for inert native fixture")
	}
	source := filepath.Join(root, "fixture.c")
	native := filepath.Join(root, "codex")
	code := `#include <stdio.h>
#include <stdlib.h>
#include <signal.h>
int main(int argc, char **argv) { signal(SIGINT,SIG_IGN); FILE *f=fopen(argv[1],"a"); if(!f) return 2; setbuf(f,NULL); puts("fixture ready"); fflush(stdout); int c; while((c=getchar())!=EOF) fputc(c,f); fclose(f); return 0; }
`
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(cc, "-o", native, source).CombinedOutput(); err != nil {
		t.Fatalf("native fixture: %v %s", err, out)
	}
	socket := filepath.Join(root, "tmux.sock")
	if out, err := exec.Command(realTmux, "-S", socket, "-f", "/dev/null", "new-session", "-d", "-s", "keeper", "/bin/cat").CombinedOutput(); err != nil {
		t.Fatalf("owned server: %v %s", err, out)
	}
	t.Cleanup(func() { exec.Command(realTmux, "-S", socket, "kill-server").Run() })
	// Reject any accidental attempt to reach the caller's tmux socket.
	bin := filepath.Join(root, "bin")
	os.MkdirAll(bin, 0700)
	shim := fmt.Sprintf("#!/bin/sh\n[ \"$1\" = -S ] && [ \"$2\" = '%s' ] || exit 97\nexec '%s' \"$@\"\n", socket, realTmux)
	os.WriteFile(filepath.Join(bin, "tmux"), []byte(shim), 0700)
	for _, name := range []string{"codex", "claude", "pi", "opencode", "cursor-agent", "amp", "grok"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nprintf blocked > '"+filepath.Join(root, "unexpected-provider")+"'\nexit 98\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		if _, err := os.Stat(filepath.Join(root, "unexpected-provider")); err == nil {
			t.Error("unexpected provider launch attempted")
		}
	})
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ZDOTDIR", root)
	if err := os.WriteFile(filepath.Join(root, ".zshrc"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	w := watcher.New(time.Hour)
	w.SetTmuxServer(socket, filepath.Join(root, "scratch"))
	w.SetProviderActivityProbe(recoveryNativeProbe{})
	store, err := brain.NewStore(filepath.Join(root, "brain"))
	if err != nil {
		t.Fatal(err)
	}
	service := brain.NewService(store, w, nil)
	w.SetTurnLedger(store)
	app := &controlApp{watcher: w, brainStore: store, brainService: service}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	path := filepath.Join(root, "control.sock")
	srv := &control.Server{Path: path, Handler: app}
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	wait := func(what string, ready func() bool) {
		t.Helper()
		end := time.Now().Add(3 * time.Second)
		for !ready() {
			if time.Now().After(end) {
				t.Fatal(what)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	wait("control listener", func() bool {
		c, e := net.DialTimeout("unix", path, 50*time.Millisecond)
		if e == nil {
			c.Close()
		}
		return e == nil
	})
	call := func(req control.Request) control.Response {
		t.Helper()
		r, e := control.CallWithTimeout(path, req, 5*time.Second)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	durableCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	create := func(name string) string {
		t.Helper()
		id, e := w.CreateSession("", watcher.CreateSessionOptions{Name: name, Cwd: durableCwd, Command: "exec " + native + " " + filepath.Join(root, name+".input"), Detached: true, Delegated: true, Env: map[string]string{"ZEN_FIXTURE_INPUT": filepath.Join(root, name+".input"), "ZDOTDIR": root}})
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() {
			if t.Failed() {
				out, _ := exec.Command(realTmux, "-S", socket, "capture-pane", "-p", "-t", id).CombinedOutput()
				t.Logf("owned fixture pane: %s", out)
			}
		})
		wait("native fixture entered", func() bool { _, e := os.Stat(filepath.Join(root, name+".input")); return e == nil })
		wait("native process identity", func() bool { _, e := w.ResolveOwnedGeneration(id); return e == nil })
		return id
	}
	original, recovery := create("original"), create("recovery")
	item, err := store.CreateWork(brain.Work{Title: "recovery", Objective: "one owner", CompletionPolicy: brain.CompletionUntilDone, DoneCriteriaRef: "one fenced owner"})
	if err != nil {
		t.Fatal(err)
	}
	send := func(sid, text string) string {
		t.Helper()
		r := call(control.Request{Type: "worker_send", WorkerID: sid, WorkID: item.ID, Text: text, Submit: true})
		if !r.OK {
			t.Fatalf("send: %+v", r.Error)
		}
		return r.TurnID
	}
	progress := func(sid, turn string) {
		t.Helper()
		r := call(control.Request{Type: "worker_progress", WorkerID: sid, TurnID: turn, Status: "running", Phase: "working", Attention: "none", Summary: "inert fixture", LeaseSeconds: 300})
		if !r.OK {
			t.Fatalf("progress: %+v", r.Error)
		}
	}
	first := send(original, "original input")
	progress(original, first)
	steer := send(original, "steering input")
	progress(original, steer)
	if _, changed, e := store.ReconcileAbsentWorkAttempt(item.ID, original); e != nil || !changed {
		t.Fatalf("canonical loss: %t %v", changed, e)
	}
	recovered := send(recovery, "recovery input")
	blocked := call(control.Request{Type: "worker_progress", WorkerID: recovery, TurnID: recovered, Status: "blocked", Phase: "working", Attention: "blocked", Summary: "fixture recovery needs coordination", LeaseSeconds: 300})
	if !blocked.OK {
		t.Fatalf("blocked recovery: %+v", blocked.Error)
	}
	closed := call(control.Request{Type: "worker_close", WorkerID: recovery, Force: true})
	if !closed.OK {
		t.Fatalf("close recovery: %+v", closed.Error)
	}
	claim, ok, err := store.ClaimNextReviewAction("fixture-host")
	if err != nil || !ok {
		t.Fatalf("claim: %t %v", ok, err)
	}
	_, delivered, err := store.ConsumeReviewDelivery(item.ID, claim.HandlingID, claim.ProviderTurnID)
	if err != nil {
		t.Fatal(err)
	}
	dependency, err := store.CreateWork(brain.Work{Title: "startup dependency", Objective: "independent live turn", CompletionPolicy: brain.CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	depCandidate := watcher.InputAdmission{WorkID: dependency.ID, SessionID: "dependency", ProposedTurnID: "turn:dependency", Receipt: "turn:dependency", PayloadSHA256: strings.Repeat("a", 64), ProcessIdentity: "dependency-process", PaneGeneration: "dependency-pane", AcceptedAt: time.Now().UTC(), Mode: watcher.InputAdmissionFresh, SignalProtocol: true}
	if _, _, err = store.PrepareInputAdmission(depCandidate); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: "dependency", TurnID: "turn:dependency", Class: watcher.EvidenceControl, Kind: "running", SourceID: "dependency-running", At: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	ref := brain.SessionTerminalWakeRef("dependency", "turn:dependency")
	_, _, err = store.ResolveWorkReview(brain.WorkReviewDispositionRequest{WorkID: item.ID, HandlingID: claim.HandlingID, ProviderTurnID: claim.ProviderTurnID, ExpectedWorkRevision: delivered.Review.Lease.DeliveryWorkRevision, Disposition: brain.WorkDispositionWait, Wake: &brain.WorkWake{Kind: brain.WorkWakeSessionTerminal, Ref: ref}})
	if err != nil {
		t.Fatal(err)
	}
	// The named dependency completes. Canonical wake release commits before
	// projection, matching the live queued-engine/waiting-projection boundary.
	if _, err = store.FSM().ClearWait(lifecycle.WorkID(item.ID), lifecycle.WakeSessionTerminal, ref, "dependency-done"); err != nil {
		t.Fatal(err)
	}
	historical := make(map[string]watcher.TurnSnapshot)
	for _, token := range []string{first, steer} {
		snapshot, found, err := store.TurnByID(original, token)
		if err != nil || !found {
			t.Fatal("missing original evidence")
		}
		historical[token] = snapshot
	}
	recoveryHistory, found, err := store.TurnByID(recovery, recovered)
	if err != nil || !found {
		t.Fatal("missing recovery evidence")
	}
	historical[recovered] = recoveryHistory
	// Reproduce a previous version's crash/error after canonical preparation
	// but before the transport marker. No input was sent for this candidate.
	if orphan {
		owned, err := w.ResolveOwnedGeneration(original)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = store.FSM().PrepareAdmission(lifecycle.WorkID(item.ID), lifecycle.PrepareAdmissionInput{SessionID: original, TurnToken: "turn:unsent-old-version", Receipt: "turn:unsent-old-version", PayloadSHA256: strings.Repeat("b", 64), ProcessIdentity: owned.ProcessIdentity, PaneGeneration: owned.PaneGeneration, Mode: lifecycle.AdmissionFresh, SignalProtocol: true, AttemptedAt: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
	}
	fresh := send(original, "fresh explicit continuation")
	if orphan {
		st, _ := store.FSM().State(lifecycle.WorkID(item.ID))
		if st.AdmissionByToken("turn:unsent-old-version").Status != lifecycle.AdmissionAborted {
			t.Fatal("unmarked preparation was not durably aborted")
		}
	}

	wait("one native input delivery", func() bool {
		raw, _ := os.ReadFile(filepath.Join(root, "original.input"))
		return strings.Count(string(raw), fresh) == 1
	})
	progress(original, fresh)
	for token, before := range historical {
		after, _, _ := store.TurnByID(before.SessionID, token)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("historical provider evidence mutated")
		}
	}

	st, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || st.Attempt == nil || string(st.Attempt.TurnToken) != fresh || st.Attempt.SessionID != original {
		t.Fatal("fresh input did not own the sole Attempt")
	}
	for _, old := range []struct{ sid, turn string }{{original, first}, {original, steer}, {recovery, recovered}} {
		prior, found, err := store.TurnByID(old.sid, old.turn)
		if err != nil || !found {
			t.Fatal("history discarded")
		}
		_ = prior
		for _, status := range []string{"running", "done"} {
			r := call(control.Request{Type: "worker_progress", WorkerID: old.sid, TurnID: old.turn, Status: status, Phase: "reporting", Attention: "none", Summary: "stale fixture signal"})
			if r.OK {
				t.Fatal("old signal accepted as current owner")
			}
		}
	}
	after, _ := store.FSM().State(lifecycle.WorkID(item.ID))
	if after.Attempt == nil || string(after.Attempt.TurnToken) != fresh || after.Attempt.Generation != st.Attempt.Generation || after.Revision != st.Revision {
		t.Fatal("old signal finished new owner")
	}
	reopened, err := brain.NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := reopened.FSM().State(lifecycle.WorkID(item.ID))
	if again.Attempt == nil || string(again.Attempt.TurnToken) != fresh {
		t.Fatal("owner changed on restart")
	}
	t.Log("real control/watcher/native receipt and sole canonical ownership verified")
}
