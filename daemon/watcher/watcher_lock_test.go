package watcher

import (
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

type blockingActivityProbe struct {
	started chan struct{}
	release chan struct{}
}

func (p *blockingActivityProbe) Infer(in classifier.ActivityInput) classifier.ActivitySignal {
	p.started <- struct{}{}
	<-p.release
	return classifier.ActivitySignal{State: classifier.StateUnknown, Source: "blocked_test", Provider: "test"}
}

func TestPollDoesNotHoldLockDuringProbe(t *testing.T) {
	for _, superseded := range []bool{false, true} {
		name := "current"
		if superseded {
			name = "superseded"
		}
		t.Run(name, func(t *testing.T) {
			w := New(time.Second)
			probe := &blockingActivityProbe{started: make(chan struct{}, 1), release: make(chan struct{})}
			w.SetActivityProbe(probe)
			id := "sess-a:@1"
			restore := installFakePollSeams(w, testWindows()[:1], map[string]string{id: contentA}, liveSessionProcesses())
			defer restore()
			finished := make(chan struct{})
			go func() { defer close(finished); w.poll() }()
			t.Cleanup(func() {
				close(probe.release)
				select {
				case <-finished:
				case <-time.After(time.Second):
					t.Error("real poll failed to release")
				}
			})
			select {
			case <-probe.started:
			case <-time.After(time.Second):
				t.Fatal("real poll did not enter probe")
			}
			if w.SnapshotReady() {
				t.Fatal("incomplete first poll advertised a ready snapshot")
			}
			read := make(chan []*classifier.Worker, 1)
			go func() { read <- w.Workers() }()
			select {
			case workers := <-read:
				if len(workers) != 1 || workers[0].ID != id {
					t.Fatalf("observation snapshot=%+v", workers)
				}
			case <-time.After(time.Second):
				t.Fatal("Agents blocked during real poll probe")
			}
			if superseded {
				if _, err := w.UpdateWorkerProgress(id, classifier.WorkerProgress{Status: "running", Phase: "working", Attention: "none", Summary: "newer input"}); err != nil {
					t.Fatal(err)
				}
				drainWatcherEvents(w)
			}
			probe.release <- struct{}{}
			<-finished
			if !w.SnapshotReady() {
				t.Fatal("completed poll did not publish snapshot readiness")
			}
			if superseded {
				if got := w.GetWorker(id); got.Summary != "newer input" || len(w.events) != 0 {
					t.Fatalf("superseded projection published: %+v events=%d", got, len(w.events))
				}
			} else {
				select {
				case event := <-w.events:
					if event.Type != "worker_discovered" || event.WorkerID != id {
						t.Fatalf("first projection event=%+v", event)
					}
				default:
					t.Fatal("discovery event missing")
				}
			}
		})
	}
}
