package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/lifecycle"
)

func TestWorkerReceiptReadsDurableAcceptanceWithoutWatcher(t *testing.T) {
	for _, status := range []lifecycle.AdmissionStatus{lifecycle.AdmissionPrepared, lifecycle.AdmissionAmbiguous, lifecycle.AdmissionAccepted} {
		t.Run(string(status), func(t *testing.T) {
			root := t.TempDir()
			store, err := brain.NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			item, err := store.CreateWork(brain.Work{Title: "receipt", Objective: "read without replay", CompletionPolicy: brain.CompletionBounded})
			if err != nil {
				t.Fatal(err)
			}
			id := lifecycle.WorkID(item.ID)
			if _, _, err := store.FSM().PrepareAdmission(id, lifecycle.PrepareAdmissionInput{
				SessionID: "worker:@1", TurnToken: "turn:receipt", Receipt: "turn:receipt",
				PayloadSHA256: "digest", ProcessIdentity: "process", PaneGeneration: "pane",
				Mode: lifecycle.AdmissionFresh, SignalProtocol: true, AttemptedAt: time.Now().UTC(),
			}); err != nil {
				t.Fatal(err)
			}
			switch status {
			case lifecycle.AdmissionAmbiguous:
				if _, err := store.FSM().MarkAdmissionAmbiguous(id, "turn:receipt", "response lost"); err != nil {
					t.Fatal(err)
				}
			case lifecycle.AdmissionAccepted:
				if _, err := store.FSM().AcceptAdmissionBySignal(id, "turn:receipt", "worker:@1"); err != nil {
					t.Fatal(err)
				}
			}
			reopened, err := brain.NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			app := &controlApp{brainStore: reopened}
			path := filepath.Join(root, "state", "lifecycle", "state.json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			req := control.Request{Type: "worker_receipt", WorkID: item.ID, WorkerID: "worker:@1", TurnID: "turn:receipt"}
			for i := 0; i < 2; i++ {
				resp := app.HandleControlRequest(req)
				if !resp.OK || resp.WorkerReceipt == nil {
					t.Fatalf("receipt=%+v", resp)
				}
				got := resp.WorkerReceipt
				if got.Admission.Status != status || got.OwnsAttempt != (status == lifecycle.AdmissionAccepted) || got.Admission.PayloadSHA256 != "digest" {
					t.Fatalf("receipt changed evidence: %+v", got)
				}
			}
			for _, wrong := range []control.Request{
				{Type: "worker_receipt", WorkID: item.ID, WorkerID: "wrong", TurnID: req.TurnID},
				{Type: "worker_receipt", WorkID: item.ID, WorkerID: req.WorkerID, TurnID: "wrong"},
				{Type: "worker_receipt", WorkID: "wrong", WorkerID: req.WorkerID, TurnID: req.TurnID},
			} {
				if resp := app.HandleControlRequest(wrong); resp.OK {
					t.Fatal("wrong identity obtained receipt")
				}
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("read-only receipt mutated canonical state: %v", err)
			}
		})
	}
}
