package brain

import (
	"errors"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestUnmarkedAdmissionRecoveryRequiresExactPreparedIdentity(t *testing.T) {
	for _, mode := range []string{"prepared", "ambiguous", "accepted", "wrong-generation", "wrong-digest"} {
		t.Run(mode, func(t *testing.T) {
			s, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			w, err := s.CreateWork(Work{Title: "unmarked", Objective: "retain exact evidence"})
			if err != nil {
				t.Fatal(err)
			}
			candidate := delegatedSubmissionCandidate(w.ID, "session", "turn:unmarked", "input", time.Now().UTC())
			candidate.SignalProtocol = true
			if _, _, err = s.PrepareInputAdmission(candidate); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "ambiguous":
				err = s.MarkInputAdmissionAmbiguous(candidate.SessionID, candidate.ProposedTurnID, "unknown transport")
			case "accepted":
				_, err = s.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: candidate.SessionID, TurnID: candidate.ProposedTurnID, Class: watcher.EvidenceControl, Kind: "running", SourceID: "accepted", At: time.Now().UTC()})
			case "wrong-generation":
				candidate.ProcessIdentity = "replacement"
			case "wrong-digest":
				candidate.PayloadSHA256 = "different"
			}
			if err != nil {
				t.Fatal(err)
			}
			err = s.AbortUnmarkedInputAdmission(candidate)
			st, _ := s.FSM().State(lifecycle.WorkID(w.ID))
			if mode == "prepared" {
				if err != nil || st.AdmissionByToken(lifecycle.TurnToken(candidate.ProposedTurnID)).Status != lifecycle.AdmissionAborted {
					t.Fatalf("exact prepared recovery: %v", err)
				}
			} else if err == nil || st.AdmissionByToken(lifecycle.TurnToken(candidate.ProposedTurnID)).Status == lifecycle.AdmissionAborted {
				t.Fatalf("unsafe recovery accepted for %s", mode)
			}
		})
	}
}

func TestFailedPreparationProjectionDoesNotStrandAdmission(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateWork(Work{Title: "write failure", Objective: "no stranded preparation"})
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("projection write failed")
	s.writePresentation = func(string, any) error { return failure }
	candidate := delegatedSubmissionCandidate(w.ID, "session", "turn:failed-prepare", "input", time.Now().UTC())
	candidate.SignalProtocol = true
	if _, _, err = s.PrepareInputAdmission(candidate); !errors.Is(err, failure) {
		t.Fatalf("missing projection error: %v", err)
	}
	st, _ := s.FSM().State(lifecycle.WorkID(w.ID))
	if st.AdmissionByToken(lifecycle.TurnToken(candidate.ProposedTurnID)).Status != lifecycle.AdmissionAborted || st.Attempt != nil {
		t.Fatal("pre-mutation failure retained authority")
	}
}
