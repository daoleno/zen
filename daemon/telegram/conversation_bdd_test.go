package telegram

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

type providerReceipt struct{ SessionID, Body string }

// Only provider IO is replaced. Brain.Service, input admission, timeline and
// Work/FSM persistence below are production code on isolated on-disk Stores.
type conversationProvider struct {
	brain.Watcher
	mu         sync.Mutex
	workers    map[string]*classifier.Worker
	path       string
	created    int
	inputCalls []string
}

func (p *conversationProvider) Workers() []*classifier.Worker {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []*classifier.Worker
	for _, worker := range p.workers {
		copy := *worker
		out = append(out, &copy)
	}
	return out
}
func (p *conversationProvider) GetWorker(id string) *classifier.Worker {
	for _, worker := range p.Workers() {
		if worker.ID == id {
			return worker
		}
	}
	return nil
}
func (p *conversationProvider) HasSession(id string) bool { return p.GetWorker(id) != nil }
func (p *conversationProvider) ProbeSession(id string) (watcher.SessionPresence, error) {
	if p.HasSession(id) {
		return watcher.SessionPresencePresent, nil
	}
	return watcher.SessionPresenceAbsent, nil
}
func (p *conversationProvider) ResolveDelegatedAbsence(id string) (bool, error) {
	return !p.HasSession(id), nil
}
func (p *conversationProvider) ResolveOwnedGeneration(id string) (watcher.OwnedGeneration, error) {
	if !p.HasSession(id) {
		return watcher.OwnedGeneration{}, fmt.Errorf("fixture Session absent")
	}
	return watcher.OwnedGeneration{SessionID: id, Generation: "fixture-generation-" + id}, nil
}
func (p *conversationProvider) ResolveBrainHostGeneration(id string) (watcher.OwnedGeneration, error) {
	return p.ResolveOwnedGeneration(id)
}
func (p *conversationProvider) CreateSession(_ string, opts watcher.CreateSessionOptions) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.created++
	id := fmt.Sprintf("fixture-host:@%d", p.created)
	p.workers[id] = &classifier.Worker{ID: id, Name: opts.Name, Hidden: opts.Hidden, Delegated: opts.Delegated, Command: opts.Command, Cwd: opts.Cwd, State: classifier.StateRunning}
	return id, nil
}
func (p *conversationProvider) KillSession(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.workers, id)
	return nil
}
func (p *conversationProvider) ProbeProviderEvidence(string) (watcher.ProviderActivityObservation, bool, error) {
	return watcher.ProviderActivityObservation{}, false, nil
}
func (p *conversationProvider) receipts() map[string]providerReceipt {
	raw, _ := os.ReadFile(p.path)
	out := map[string]providerReceipt{}
	_ = json.Unmarshal(raw, &out)
	return out
}
func (p *conversationProvider) SendInputWithReceiptResult(id, body, receipt string) (watcher.InputResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.inputCalls = append(p.inputCalls, receipt)
	rows := p.receipts()
	if existing, found := rows[receipt]; found && existing != (providerReceipt{id, body}) {
		return watcher.InputResult{Outcome: watcher.InputNotSubmitted}, fmt.Errorf("receipt identity mismatch")
	}
	rows[receipt] = providerReceipt{id, body}
	raw, err := json.Marshal(rows)
	if err == nil {
		err = os.WriteFile(p.path, raw, 0600)
	}
	return watcher.InputResult{Outcome: watcher.InputAccepted, Receipt: receipt}, err
}
func (p *conversationProvider) InputReceiptResult(_ string, receipt string) (watcher.InputResult, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.receipts()[receipt]
	return watcher.InputResult{Outcome: watcher.InputAccepted, Receipt: receipt}, ok, nil
}
func (p *conversationProvider) SendInputWithReceiptWhenReadyResult(id, _, body string, receiptFor watcher.InputReceiptForGeneration) (watcher.InputResult, watcher.OwnedGeneration, error) {
	gen, err := p.ResolveOwnedGeneration(id)
	if err != nil {
		return watcher.InputResult{}, gen, err
	}
	result, err := p.SendInputWithReceiptResult(id, body, receiptFor(gen))
	return result, gen, err
}
func (p *conversationProvider) SendInput(id, body string) error {
	_, err := p.SendInputWithReceiptResult(id, body, "fixture-bootstrap:"+id)
	return err
}
func (p *conversationProvider) SendInputWhenReady(id, _, body string) error {
	return p.SendInput(id, body)
}

func realConversationFixture(t *testing.T) (*Manager, *brain.Store, *brain.Service, *conversationProvider, *fakeAPI, string) {
	t.Helper()
	root := t.TempDir()
	s, err := brain.NewStore(filepath.Join(root, "brain"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetChatState(brain.ChatState{ThreadID: "brain-current"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetHostSession("host:@1", "claude"); err != nil {
		t.Fatal(err)
	}
	p := &conversationProvider{workers: map[string]*classifier.Worker{
		"host:@1":   {ID: "host:@1", Name: "Brain", Hidden: true, Command: "claude", Cwd: s.WorkspacePath(), State: classifier.StateRunning},
		"session-a": {ID: "session-a", Name: "Session A", Delegated: true, State: classifier.StateRunning},
		"session-b": {ID: "session-b", Name: "Session B", Delegated: true, State: classifier.StateRunning},
		"manual":    {ID: "manual", Name: "Session A", State: classifier.StateRunning},
	}, path: filepath.Join(root, "provider-receipts.json")}
	service := brain.NewService(s, p, work.NewExecutorConfig("claude", map[string]work.Executor{"claude": {Name: "claude", Kind: "claude", Command: "claude", Runtime: work.WorkerRuntimeTmux}}))
	api := &fakeAPI{bot: User{ID: 7001, IsBot: true, Username: "fixture_bot", Topics: true, UserTopics: true}}
	m, err := NewManagerWithOptions(root, service, Options{API: api, TypingDeadline: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.stopTyping)
	if _, err := m.Configure(t.Context(), "fixture-token"); err != nil {
		t.Fatal(err)
	}
	bindOwner(t, m, 1, 10, 10)
	return m, s, service, p, api, root
}

func TestRealBrainTelegramNativeTopicsContinueCanonicalConversation(t *testing.T) {
	m, s, _, p, api, root := realConversationFixture(t)
	created := func(id, thread int64) Update {
		update := topicUpdate(id, thread, "")
		update.Message.ForumTopicCreated = &ForumTopicCreated{Name: "User-created topic"}
		return update
	}
	for _, update := range []Update{created(2, 101), topicUpdate(3, 101, "first message"), topicUpdate(4, 101, "follow up"), created(5, 102), topicUpdate(6, 102, "General client created another native topic"), topicUpdate(7, 102, "/start"), topicUpdate(7, 102, "/start")} {
		if err := m.handleUpdate(t.Context(), "fixture-token", update); err != nil {
			t.Fatal(err)
		}
	}
	thread, _ := s.ChatThreadID()
	if thread != "brain-current" {
		t.Fatalf("implicit NewChat: %s", thread)
	}
	items, err := s.ThreadTimeline(thread, 0)
	if err != nil || len(items) != 3 {
		t.Fatalf("canonical timeline count=%d err=%v", len(items), err)
	}
	for _, item := range items {
		if item.ThreadID != thread || item.SessionID != "host:@1" || !item.BrainAdmission {
			t.Fatalf("noncanonical input: %+v", item)
		}
	}
	if m.store.snapshot().BrainReplyTopicID != 102 {
		t.Fatal("latest native Brain destination not persisted")
	}
	for _, id := range []string{"3", "4", "6"} {
		record := m.store.snapshot().Processed[id]
		if record.BrainThreadID != thread || record.Disposition != "accepted" {
			t.Fatalf("routing audit=%+v", record)
		}
	}
	reopenedStore, err := brain.NewStore(filepath.Join(root, "brain"))
	if err != nil {
		t.Fatal(err)
	}
	reopenedService := brain.NewService(reopenedStore, p, nil)
	reopened, err := NewManagerWithOptions(root, reopenedService, Options{API: api})
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.handleUpdate(t.Context(), "fixture-token", topicUpdate(4, 101, "follow up")); err != nil {
		t.Fatal(err)
	}
	got, _ := reopenedStore.ThreadTimeline(thread, 0)
	if len(got) != 3 || len(p.receipts()) != 3 {
		t.Fatalf("restart replayed input: timeline=%d receipts=%d", len(got), len(p.receipts()))
	}
	if err := m.handleUpdate(t.Context(), "fixture-token", topicUpdate(8, 102, "/new")); err != nil {
		t.Fatal(err)
	}
	if err := m.handleUpdate(t.Context(), "fixture-token", topicUpdate(8, 102, "/new")); err != nil {
		t.Fatal(err)
	}
	newThread, _ := s.ChatThreadID()
	if newThread == thread || p.created != 1 {
		t.Fatalf("explicit NewChat not exact: %s created=%d", newThread, p.created)
	}
	oldItems, _ := s.ThreadTimeline(thread, 0)
	if !reflect.DeepEqual(oldItems, items) {
		t.Fatal("NewChat changed old history")
	}
}

func TestRealBrainTelegramSessionRoutingPreservesWorkAuthority(t *testing.T) {
	m, s, _, p, api, root := realConversationFixture(t)
	item, err := s.CreateWork(brain.Work{Title: "Fixture work", Objective: "Keep exact Session ownership", CompletionPolicy: brain.CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	admission, _, err := s.PrepareInputAdmission(watcher.InputAdmission{WorkID: item.ID, SessionID: "session-a", ProposedTurnID: "turn:fixture-a", Receipt: "fixture-admit-a", PayloadSHA256: brain.AdmissionDigest("fixture"), PaneGeneration: "fixture-generation", ProcessIdentity: "fixture-process", AcceptedAt: now, Mode: watcher.InputAdmissionFresh})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ResolveInputAdmission(watcher.InputAdmissionResolution{SessionID: "session-a", ProposedTurnID: admission.ProposedTurnID, Receipt: admission.Receipt, PayloadSHA256: admission.PayloadSHA256, ActivityID: "fixture-activity", ResolvedAt: now, Admission: watcher.TurnAdmission{Stream: "fixture", ID: "fixture-input", Cursor: 1, SHA256: admission.PayloadSHA256, At: now}})
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.projectSessionTopics(t.Context(), "fixture-token"); err != nil {
		t.Fatal(err)
	}
	for m.hasDeliverableTopicOp() {
		if err := m.deliverTopicOpOne(t.Context(), "fixture-token"); err != nil {
			t.Fatal(err)
		}
	}
	a, b := topicThreadFor(t, m, "session-a"), topicThreadFor(t, m, "session-b")
	for _, update := range []Update{topicUpdate(2, a, "busy follow up"), topicUpdate(3, b, "only B"), topicUpdate(4, a, "next queued input"), topicUpdate(4, a, "next queued input")} {
		if err := m.handleUpdate(t.Context(), "fixture-token", update); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := s.FSM().State(lifecycle.WorkID(item.ID))
	if !reflect.DeepEqual(before, after) {
		t.Fatal("Telegram input manufactured Work transitions")
	}
	receipts := p.receipts()
	if receipts["telegram:update:7001:2"].SessionID != "session-a" || receipts["telegram:update:7001:3"].SessionID != "session-b" || receipts["telegram:update:7001:4"].SessionID != "session-a" {
		t.Fatalf("wrong destinations: %+v", receipts)
	}
	if timeline, _ := s.ThreadTimeline("brain-current", 0); len(timeline) != 0 {
		t.Fatal("Session messages leaked into Brain")
	}
	terminal, changed, err := s.ApplyTurnFact(watcher.TurnFact{SessionID: "session-a", TurnID: admission.ProposedTurnID, Class: watcher.EvidenceProvider, Kind: "done", Bound: true, SourceID: "fixture-provider-terminal", ActivityID: "fixture-activity", At: now.Add(time.Second), SettledAt: now.Add(time.Second), Summary: "Fixture provider completed"})
	if err != nil || !changed || terminal.Status != watcher.TurnDone {
		t.Fatalf("fixture terminal not applied: %+v %v", terminal, err)
	}
	eventsBefore, _ := s.ListWorkEvents(item.ID)
	if err := m.projectSessionTopics(t.Context(), "fixture-token"); err != nil {
		t.Fatal(err)
	}
	if mapping, _ := topicMappingByThread(m.store.snapshot(), a); mapping.State != topicStateCompleted {
		t.Fatal("completed real turn not projected")
	}
	if err := m.handleUpdate(t.Context(), "fixture-token", topicUpdate(5, a, "follow up to same completed Session")); err != nil {
		t.Fatal(err)
	}
	if p.receipts()["telegram:update:7001:5"].SessionID != "session-a" {
		t.Fatal("completed follow-up replaced Session")
	}
	eventsAfter, _ := s.ListWorkEvents(item.ID)
	if !reflect.DeepEqual(eventsBefore, eventsAfter) {
		t.Fatal("Telegram forged completion/follow-up facts")
	}
	p.mu.Lock()
	delete(p.workers, "session-a")
	p.mu.Unlock()
	if err := m.projectSessionTopics(t.Context(), "fixture-token"); err != nil {
		t.Fatal(err)
	}
	// Before the delete dispatches, the stale route fails closed and retains
	// the exact tombstone.
	if err := m.handleUpdate(t.Context(), "fixture-token", topicUpdate(6, a, "stale topic")); err != nil {
		t.Fatal(err)
	}
	if m.store.snapshot().Processed["6"].Disposition != "topic_stale" {
		t.Fatal("unconfirmed removal did not fail closed")
	}
	for m.hasDeliverableTopicOp() {
		if err := m.deliverTopicOpOne(t.Context(), "fixture-token"); err != nil {
			t.Fatal(err)
		}
	}
	if len(api.deletedTopics) != 1 || api.deletedTopics[0].MessageThreadID != a {
		t.Fatalf("exact mapped topic not deleted: %+v", api.deletedTopics)
	}
	// The removed topic cannot route to another recipient and the mapping is
	// gone locally, not merely renamed.
	if err := m.handleUpdate(t.Context(), "fixture-token", topicUpdate(7, a, "removed topic")); err != nil {
		t.Fatal(err)
	}
	if m.store.snapshot().Processed["7"].Disposition != "topic_unknown" {
		t.Fatal("removed topic routed after deletion")
	}
	reopened, err := NewManagerWithOptions(root, brain.NewService(s, p, nil), Options{API: api})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := topicMappingByThread(reopened.store.snapshot(), a); ok {
		t.Fatal("restart resurrected deleted topic mapping")
	}
}
