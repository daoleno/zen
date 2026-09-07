package brain

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

// Real progress, emit/parse, review and durable projection paths; scripted
// Session transport only. No live daemon, provider or user state is touched.
func TestBDD_BrainCardUnicodeDeliveryHistoryAndAcceptance(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	const thread = "unicode-card-thread"
	if err := store.SetChatState(ChatState{ThreadID: thread}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession("host", "codex"); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{Title: "中文研究整理", Objective: "保留原始材料并完成中文整理"})
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{turnStore: store, outcomes: map[string]watcher.InputOutcome{}, sessions: map[string]*classifier.Worker{
		"host": {ID: "host", Hidden: true, State: classifier.StateDone}, "worker": {ID: "worker", Delegated: true, State: classifier.StateDone},
	}}
	service := NewService(store, fw, nil)
	if _, err := fw.SubmitDelegatedWorkInput("worker", item.Objective, item.ID, "unicode-turn", "", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	full := strings.Repeat("中文研究材料验证通过；", 20)
	progress, err := classifier.ValidateProgress(classifier.WorkerProgress{Status: "done", Phase: "reporting", Attention: "done", Summary: full})
	if err != nil || !utf8.ValidString(progress.Summary) || strings.ContainsRune(progress.Summary, '\ufffd') {
		t.Fatalf("source corruption: %+v %v", progress, err)
	}
	fact := watcher.TurnFact{SessionID: "worker", TurnID: "unicode-turn", Class: watcher.EvidenceControl, Kind: "done", SourceID: "unicode-result", Summary: progress.Summary, At: time.Now()}
	if _, err := service.ApplyDelegatedTurnProgress(fact); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyDelegatedTurnProgress(fact); err != nil {
		t.Fatal(err)
	}
	if err := service.ReconcileWorkChange(); err != nil {
		t.Fatal(err)
	}
	lease := requireReviewDelivered(t, store, item.ID)
	var delivered string
	var input work.DirectWorkEventInput
	for _, call := range fw.sentCalls {
		if parsed, ok := work.ParseCanonicalDirectWorkEventInput(call.text); ok && parsed.WorkID == item.ID {
			delivered, input = call.text, parsed
		}
	}
	if delivered == "" || input.Summary != progress.Summary {
		t.Fatal("actual emitted input failed parser or changed summary")
	}
	// Recreate the exact pre-fix byte-cut + JSON escape behavior, rather than
	// treating corruption in a new message as legitimate user prose.
	input.Summary = strings.Repeat("中", 60)[:157] + "..."
	legacyJSON, _ := json.Marshal(input)
	legacy := "<zen_work_event>\n" + string(legacyJSON) + "\n</zen_work_event>"
	if _, ok := work.ParseCanonicalDirectWorkEventInput(legacy); ok {
		t.Fatal("strict admission unexpectedly accepted legacy normalization")
	}
	if !work.IsDirectWorkEventPresentationInput(legacy) {
		t.Fatal("legacy reserved input leaked")
	}
	quoted := "Explain this quoted example:\n```text\n" + legacy + "\n```"
	provider := work.CodexConversation{Available: true, SessionID: "host", Events: []work.CodexConversationEvent{
		{ID: "internal", Kind: "user_message", Body: delivered, Timestamp: "2026-09-08T00:00:01Z"},
		{ID: "internal-duplicate", Kind: "user_message", Body: legacy, Timestamp: "2026-09-08T00:00:02Z"},
		{ID: "quoted", Kind: "user_message", Body: quoted, Timestamp: "2026-09-08T00:00:03Z"},
		{ID: "user", Kind: "user_message", Body: "请保留我的中文问题", Timestamp: "2026-09-08T00:00:04Z"},
		{ID: "assistant", Kind: "assistant_message", Body: "整理完成，原始证据保持不变。", Timestamp: "2026-09-08T00:00:05Z"},
	}}
	encoded, _ := json.Marshal(provider)
	if err := json.Unmarshal(encoded, &provider); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := service.MaterializeProviderConversation(thread, provider); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.AppendTimelineItem(TimelineItem{ID: "legacy-on-disk", ThreadID: thread, SessionID: "host", Role: "user", Kind: timelineKindUserMessage, Body: legacy, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	project := func(svc *Service) map[string]any {
		items, err := svc.ThreadTimeline(thread, 0)
		if err != nil {
			t.Fatal(err)
		}
		events := TimelineItemsToConversationEvents(items)
		if err := svc.AnnotateWorkResultEvents(events); err != nil {
			t.Fatal(err)
		}
		if len(events) != 4 {
			t.Fatalf("duplicate/hidden user history: %+v", events)
		}
		snapshot, err := svc.ProjectionSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		return map[string]any{"events": events, "current_work": snapshot.CurrentWork}
	}
	live := project(service)
	if _, _, err := service.ResolveWorkReview(WorkReviewDispositionRequest{WorkID: item.ID, HandlingID: lease.HandlingID, ProviderTurnID: lease.ProviderTurnID, ExpectedWorkRevision: lease.DeliveryWorkRevision, Disposition: WorkDispositionComplete}); err != nil {
		t.Fatal(err)
	}
	accepted := project(service)
	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := reopened.Work(item.ID)
	if err != nil || closed.Status != WorkDone {
		t.Fatal("acceptance not durable")
	}
	reconnect := project(NewService(reopened, fw, nil))
	rows, err := reopened.ThreadTimeline(thread, 0)
	if err != nil {
		t.Fatal(err)
	}
	foundRaw := false
	for _, row := range rows {
		if row.ID == "legacy-on-disk" && row.Body == legacy {
			foundRaw = true
		}
	}
	if !foundRaw {
		t.Fatal("repair deleted original history")
	}
	if compactWorkResultText("保留内容\ufffd\ufffd...") != "保留内容..." {
		t.Fatal("legacy summary was not honestly abbreviated")
	}
	t.Run("API-to-mobile-live-history-reconnect", func(t *testing.T) {
		bun, err := exec.LookPath("bun")
		if err != nil {
			t.Skip("Bun required for native projection contract")
		}
		payload, _ := json.Marshal(map[string]any{"live": live, "accepted": accepted, "reconnect": reconnect, "work_id": item.ID, "quoted": quoted})
		cmd := exec.Command(bun, "run", filepath.Join("..", "..", "app", "components", "brain", "brainCardApiContract.ts"))
		cmd.Stdin = bytes.NewReader(payload)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("mobile contract: %v\n%s", err, output)
		}
	})
}
