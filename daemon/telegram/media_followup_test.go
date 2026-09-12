package telegram

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
)

func TestRunFileThenPromptKeepsExactRecipientOrderWithoutBlockingControls(t *testing.T) {
	for _, gate := range []string{"getFile", "body"} {
		for _, recipient := range []string{"brain", "session"} {
			t.Run(gate+"/"+recipient, func(t *testing.T) {
				r := newRunningMediaFixture(t, gate, 0)
				poll := nextMediaPoll(t, r)
				topic, other, session := int64(101), int64(76315), "session-a"
				if recipient == "brain" {
					topic, other, session = 76315, 102, "host:@1"
				}
				poll.reply <- []Update{r.document(2, topic, "doc", "caption stays with file")}
				waitMediaSignal(t, r.blocked, "file did not start")
				poll = nextMediaPoll(t, r)
				if _, err := r.store.AppendTimelineItem(brain.TimelineItem{ID: "during-file", ThreadID: "brain-current", SessionID: "host:@1", Role: "assistant", Kind: "assistant_message", Body: "Output remains responsive", CreatedAt: r.now.Add(time.Second)}); err != nil {
					t.Fatal(err)
				}
				poll.reply <- []Update{topicUpdate(3, topic, "please analyze this"), topicUpdate(4, topic, "focus on the second paragraph"), topicUpdate(5, other, "independent recipient")}
				poll = nextMediaPoll(t, r)
				// Separate the control burst from the output burst so Telegram's
				// existing one-message/second outbound throttle is not the gate.
				poll.reply <- []Update{topicUpdate(6, topic, "/brain"), topicUpdate(7, topic, "/status"), navigationCallback(8, topic, "sessions")}
				poll = nextMediaPoll(t, r)
				rows := mediaProviderReceipts(r)
				if len(rows) != 1 || rows["telegram:update:7001:5"].Body != "independent recipient" {
					t.Fatalf("same recipient overtook file or other recipient blocked: %v", rows)
				}
				state := r.m.store.snapshot()
				if state.Processed["3"].Disposition != "deferred_queued" || state.Processed["4"].Disposition != "deferred_queued" || state.Processed["6"].Disposition != "command" || state.Processed["7"].Disposition != "command" {
					t.Fatal("follow-up/control routing incorrect")
				}
				r.api.mu.Lock()
				callback := len(r.api.callbackAnswers) > 0
				output := false
				for _, sent := range r.api.sent {
					if sent.Text == "Output remains responsive" {
						output = true
					}
				}
				r.api.mu.Unlock()
				if !callback || !output {
					t.Fatal("callback/output blocked")
				}
				close(r.release)
				r.m.mediaMu.Lock()
				active := r.m.mediaActive
				r.m.mediaMu.Unlock()
				waitMediaSignal(t, active.done, "download did not finish")
				for _, id := range []int64{2, 3, 4} {
					poll.reply <- nil
					poll = nextMediaPoll(t, r)
					row := mediaProviderReceipts(r)[fmt.Sprintf("telegram:update:7001:%d", id)]
					if row.SessionID != session {
						t.Fatalf("ordered recipient for %d=%+v", id, row)
					}
				}
				r.provider.mu.Lock()
				calls := slices.Clone(r.provider.inputCalls)
				r.provider.mu.Unlock()
				want := []string{"telegram:update:7001:5", "telegram:update:7001:2", "telegram:update:7001:3", "telegram:update:7001:4"}
				if !slices.Equal(calls, want) {
					t.Fatalf("actual provider call order=%v", calls)
				}
				row := mediaProviderReceipts(r)["telegram:update:7001:2"]
				envelope := decodeAttachment(t, row.Body)
				assertAttachment(t, r.m.attachments, envelope.Files[0], r.files["doc"])
				if envelope.Captions[0].Text != "caption stays with file" {
					t.Fatal("caption lost atomicity")
				}
			})
		}
	}
}

func TestDeferredFollowupSurvivesRestartAndDuplicatesWithoutExtraTurns(t *testing.T) {
	f := newMediaFixture(t)
	file := f.document(2, 101, "doc", "file")
	file.Message.MediaGroupID = "owned-group"
	followup := topicUpdate(3, 101, "please analyze this")
	f.apply(t, file, followup, followup)
	if len(f.provider.receipts()) != 0 || !f.m.store.snapshot().MediaInputs["update:3"].TextOnly {
		t.Fatal("text did not wait")
	}
	f.reopen(t)
	f.apply(t, file, followup)
	f.advance(t, 3*time.Second)
	if !slices.Equal(f.provider.inputCalls, []string{"telegram:update:7001:2", "telegram:update:7001:3"}) {
		t.Fatalf("restart call order=%v", f.provider.inputCalls)
	}
	f.reopen(t)
	f.apply(t, file, followup)
	if len(f.provider.inputCalls) != 2 {
		t.Fatal("duplicate caused provider call")
	}
	assertAttachment(t, f.m.attachments, decodeAttachment(t, f.provider.receipts()["telegram:update:7001:2"].Body).Files[0], f.files["doc"])
}

func TestRunDeferredFollowupRejectsChangedRecipientWhileWaiting(t *testing.T) {
	for _, recipient := range []string{"brain", "session"} {
		t.Run(recipient, func(t *testing.T) {
			r := newRunningMediaFixture(t, "body", 0)
			poll := nextMediaPoll(t, r)
			topic := int64(101)
			if recipient == "brain" {
				topic = 76315
			}
			poll.reply <- []Update{r.document(2, topic, "doc", "original file"), topicUpdate(3, topic, "please analyze this")}
			waitMediaSignal(t, r.blocked, "download did not block")
			poll = nextMediaPoll(t, r)
			if recipient == "brain" {
				if err := r.store.SetChatState(brain.ChatState{ThreadID: "changed-brain"}); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := r.provider.KillSession("session-a"); err != nil {
					t.Fatal(err)
				}
			}
			close(r.release)
			r.m.mediaMu.Lock()
			active := r.m.mediaActive
			r.m.mediaMu.Unlock()
			waitMediaSignal(t, active.done, "download stuck")
			for range 2 {
				poll.reply <- nil
				poll = nextMediaPoll(t, r)
			}
			state := r.m.store.snapshot()
			if len(mediaProviderReceipts(r)) != 0 || state.MediaInputs["update:3"].State != "not_submitted" || state.Processed["3"].Disposition != "deferred_not_submitted" {
				t.Fatal("waiting follow-up migrated or remained pending")
			}
		})
	}
}

func TestDeferredFollowupFailsWithMediaAndUnblocksLaterInputs(t *testing.T) {
	for _, outcome := range []string{"download_failure", "uncertain"} {
		t.Run(outcome, func(t *testing.T) {
			f := newMediaFixture(t)
			if outcome == "download_failure" {
				f.fail["doc"] = 3
			}
			file := f.document(2, 101, "doc", "file")
			file.Message.MediaGroupID = "group"
			f.apply(t, file, topicUpdate(3, 101, "please analyze this"), topicUpdate(4, 101, "then summarize"))
			if outcome == "uncertain" {
				if err := f.m.store.mutate(func(s *durableState) error {
					for key, row := range s.MediaInputs {
						if !row.TextOnly {
							row.State = "admitting"
							s.MediaInputs[key] = row
						}
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			f.advance(t, 3*time.Second)
			f.advance(t, 3*time.Second)
			f.advance(t, 5*time.Second)
			if len(f.provider.receipts()) != 0 {
				t.Fatal("prompt ran without confirmed file")
			}
			for _, id := range []string{"update:3", "update:4"} {
				if f.m.store.snapshot().MediaInputs[id].State != "not_submitted" {
					t.Fatal("failed dependency left queue hanging")
				}
				found := false
				for _, row := range f.m.store.snapshot().Outbox {
					if row.ID == "media-error:"+id && strings.Contains(row.Text, "This follow-up was not submitted") && row.MessageThreadID == 101 {
						found = true
					}
				}
				if !found {
					t.Fatal("truthful source-topic retry feedback missing")
				}
			}
			f.apply(t, topicUpdate(5, 101, "independent new prompt"))
			if len(f.provider.receipts()) != 1 {
				t.Fatal("terminal dependency blocked subsequent explicit prompt")
			}
		})
	}
}

func TestDeferredFollowupKeepsCapturedFallbackAndUncertainAdmission(t *testing.T) {
	f := newMediaFixture(t)
	if err := f.m.store.mutate(func(s *durableState) error { s.TopicsAvailable = false; s.FallbackSessionID = "session-a"; return nil }); err != nil {
		t.Fatal(err)
	}
	file := f.document(2, 0, "doc", "file")
	file.Message.MediaGroupID = "group"
	f.apply(t, file, topicUpdate(3, 0, "follow up on A"))
	if f.m.store.snapshot().Processed["3"].SessionID != "session-a" {
		t.Fatal("queued attribution missing")
	}
	if err := f.m.store.mutate(func(s *durableState) error { s.FallbackSessionID = "session-b"; return nil }); err != nil {
		t.Fatal(err)
	}
	f.apply(t, topicUpdate(4, 0, "independent B"))
	f.advance(t, 3*time.Second)
	if f.provider.receipts()["telegram:update:7001:3"].SessionID != "session-a" || f.provider.receipts()["telegram:update:7001:4"].SessionID != "session-b" {
		t.Fatal("fallback switch retargeted waiting input")
	}
	// Simulate a restart after crossing the follow-up's admission boundary.
	if err := f.m.store.mutate(func(s *durableState) error {
		row := s.MediaInputs["update:3"]
		row.State = "admitting"
		s.MediaInputs["update:3"] = row
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.reopen(t)
	f.drain(t)
	if len(f.provider.inputCalls) != 3 || f.m.store.snapshot().MediaInputs["update:3"].State != "uncertain" {
		t.Fatal("uncertain follow-up was replayed")
	}
}

func TestDeferredFollowupDoesNotWaitForUnrelatedMedia(t *testing.T) {
	f := newMediaFixture(t)
	a := f.document(2, 101, "doc", "A")
	a.Message.MediaGroupID = "A-album"
	b := f.document(3, 102, "other", "B")
	f.fail["other"] = 3
	f.apply(t, a, b, topicUpdate(4, 101, "analyze A"))
	f.advance(t, 3*time.Second)
	if !slices.Equal(f.provider.inputCalls, []string{"telegram:update:7001:2", "telegram:update:7001:4"}) {
		t.Fatalf("A follow-up waited on B: %v", f.provider.inputCalls)
	}
	if mediaTerminal(f.m.store.snapshot().MediaInputs["update:3"].State) {
		t.Fatal("B should still be waiting to retry")
	}
}
