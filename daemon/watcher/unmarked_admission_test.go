package watcher

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

type unmarkedTestLedger struct {
	*fakeTurnLedger
	repaired int
}

func (l *unmarkedTestLedger) AbortUnmarkedInputAdmission(in InputAdmission) error {
	l.repaired++
	_, err := l.AbortInputAdmission(in.SessionID, in.ProposedTurnID, in.Receipt, in.PayloadSHA256)
	return err
}

func TestUnmarkedPreparationTransportGuards(t *testing.T) {
	for _, mode := range []string{"unmarked", "accepted-marker", "ambiguous-marker", "full-history", "different-generation", "other-work"} {
		t.Run(mode, func(t *testing.T) {
			io := newFakeSessionInputIO()
			ledger := &unmarkedTestLedger{fakeTurnLedger: newFakeTurnLedger()}
			identity := testSessionInputIdentity("codex")
			prior := InputAdmission{WorkID: "work", SessionID: "agent:@1", ProposedTurnID: "old", Receipt: "old", PayloadSHA256: strings.Repeat("a", 64), ProcessIdentity: delegatedTurnIdentity(identity), PaneGeneration: io.paneValue.generation, SignalProtocol: true, Mode: InputAdmissionFresh, AcceptedAt: time.Now().UTC()}
			if mode == "different-generation" {
				prior.ProcessIdentity = "other"
			}
			if mode == "other-work" {
				prior.WorkID = "other"
			}
			ledger.PrepareInputAdmission(prior)
			if mode == "accepted-marker" || mode == "ambiguous-marker" {
				outcome := InputAccepted
				if mode == "ambiguous-marker" {
					outcome = InputAmbiguous
				}
				io.ledger.Entries = append(io.ledger.Entries, sessionInputReceiptEntry{Receipt: "old", PayloadSHA256: prior.PayloadSHA256, Outcome: outcome})
			}
			if mode == "full-history" {
				for i := 0; i < sessionInputReceiptLedgerLimit; i++ {
					io.ledger.Entries = append(io.ledger.Entries, sessionInputReceiptEntry{Receipt: fmt.Sprintf("receipt-%d", i), PayloadSHA256: prior.PayloadSHA256, Outcome: InputAccepted})
				}
			}
			owner := newSessionInputOwner(io)
			owner.ledger = ledger
			_, err := owner.submitDelegated(prior.SessionID, identity, fixedSessionInputResolver(identity), identity.Command, "explicit new input", delegatedTurnDraft{WorkID: "work", ID: "new", AcceptedAt: time.Now().UTC(), ProcessIdentity: delegatedTurnIdentity(identity), SignalProtocol: true}, scriptedCorrelatedAdmission("explicit new input"))
			if mode == "unmarked" {
				if err != nil || ledger.repaired != 1 {
					t.Fatalf("repair=%d err=%v", ledger.repaired, err)
				}
			} else if ledger.repaired != 0 {
				t.Fatalf("unsafe repair for %s", mode)
			}
			if mode == "full-history" || mode == "different-generation" {
				if err == nil || len(io.queues) != 0 {
					t.Fatal("unsafe condition crossed mutation boundary")
				}
			}
		})
	}
}
