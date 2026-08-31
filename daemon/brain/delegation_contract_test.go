package brain

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

func TestBrainWorkerRoleContractProjectedAcrossSurfaces(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, &fakeWatcher{}, work.NewExecutorConfig("codex", map[string]work.Executor{
		"codex": {Name: "codex", Command: "codex", Kind: "codex"},
	}))

	read := func(path string) string {
		t.Helper()
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		return string(raw)
	}
	surfaces := map[string]string{
		"workspace AGENTS":  read(store.workspaceInstructionsPath()),
		"delegation policy": read(store.policyPath("delegation.md")),
		"Host bootstrap":    service.hostBootstrapPrompt(service.hostExecutor()),
		"Host handoff":      formatHostHandoffPrompt("thread-one", "grok", "codex", "codex", "", nil),
		"brain-flows":       read(store.playbookPath(brainFlowsPlaybookName)),
	}
	for name, surface := range surfaces {
		if strings.Count(surface, brainWorkerRoleContract) != 1 {
			t.Fatalf("%s must contain the exact canonical contract once:\n%s", name, surface)
		}
		if strings.Contains(surface, brainWorkerRoleContractPlaceholder) {
			t.Fatalf("%s leaked the source placeholder:\n%s", name, surface)
		}
		for _, permissive := range []string{
			"normally create or reuse",
			"use judgment when direct execution",
			"use judgment when a direct action",
			"clearer or faster",
			"clearly the better route",
			"rigid prohibition on direct action",
			"patch over it directly",
		} {
			if strings.Contains(strings.ToLower(surface), permissive) {
				t.Fatalf("%s retains permissive routing phrase %q:\n%s", name, permissive, surface)
			}
		}
	}
}

func TestHostActivationContractDeliveredOncePerProcessGeneration(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, work.NewExecutorConfig("codex", map[string]work.Executor{
		"codex": {Name: "codex", Command: "codex", Kind: "codex"},
	}))

	first, err := service.EnsureHostSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if first.HostAgent == nil {
		t.Fatalf("fresh Host activation did not return a Host: %#v", first.HostAgent)
	}
	hostID := first.HostAgent.ID
	if len(fw.sentCalls) != 1 {
		t.Fatalf("fresh Host activation: host=%#v sends=%#v", first.HostAgent, fw.sentCalls)
	}
	if fw.readyInputCalls != 1 {
		t.Fatalf("fresh Host activation readiness calls = %d, want one", fw.readyInputCalls)
	}
	if !strings.Contains(fw.sentCalls[0].text, "You are Brain inside zen") ||
		!strings.Contains(fw.sentCalls[0].text, brainWorkerRoleContract) {
		t.Fatalf("fresh bootstrap did not serve as activation:\n%s", fw.sentCalls[0].text)
	}
	firstActivation, err := store.HostActivation()
	if err != nil {
		t.Fatal(err)
	}
	if firstActivation.SessionID != hostID || firstActivation.HostGeneration == "" ||
		firstActivation.ContractVersion != brainWorkerRoleContractVersion {
		t.Fatalf("fresh activation state = %+v", firstActivation)
	}

	if _, err := service.EnsureHostSnapshot(); err != nil {
		t.Fatal(err)
	}
	if len(fw.sentCalls) != 1 {
		t.Fatalf("repeated Snapshot duplicated activation: %#v", fw.sentCalls)
	}
	restarted := NewService(store, fw, service.execs)
	if _, err := restarted.EnsureHostSnapshot(); err != nil {
		t.Fatal(err)
	}
	if len(fw.sentCalls) != 1 {
		t.Fatalf("same process after Service restart duplicated activation: %#v", fw.sentCalls)
	}

	if fw.ownedGenerations == nil {
		fw.ownedGenerations = map[string]string{}
	}
	fw.ownedGenerations[hostID] = "host-generation-two"
	if _, err := service.EnsureHostSnapshot(); err != nil {
		t.Fatal(err)
	}
	if len(fw.sentCalls) != 2 {
		t.Fatalf("new process generation did not receive one activation: %#v", fw.sentCalls)
	}
	secondPrompt := fw.sentCalls[1].text
	if !strings.Contains(secondPrompt, "Brain Host activation contract:") ||
		!strings.Contains(secondPrompt, brainWorkerRoleContract) ||
		strings.Contains(secondPrompt, "You are Brain inside zen") {
		t.Fatalf("generation refresh must use compact private activation:\n%s", secondPrompt)
	}
	if _, err := service.EnsureHostSnapshot(); err != nil {
		t.Fatal(err)
	}
	if len(fw.sentCalls) != 2 {
		t.Fatalf("second generation activation duplicated: %#v", fw.sentCalls)
	}
	if fw.readyInputCalls != 1 {
		t.Fatalf("live generation refresh re-entered startup readiness: %d", fw.readyInputCalls)
	}
	secondActivation, err := store.HostActivation()
	if err != nil {
		t.Fatal(err)
	}
	if secondActivation.HostGeneration != "host-generation-two" || secondActivation.SessionID != hostID {
		t.Fatalf("second activation state = %+v", secondActivation)
	}
}

func TestNewHostActivationAcceptedReceiptSettlesPersistenceGapWithoutReplay(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, work.NewExecutorConfig("codex", map[string]work.Executor{
		"codex": {Name: "codex", Command: "codex", Kind: "codex"},
	}))

	// A directory at the state-file path makes the post-admission atomic rename
	// fail without weakening or replacing the Store's production write path.
	if err := os.Mkdir(store.HostActivationPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := service.EnsureHostSnapshot(); err == nil || !strings.Contains(err.Error(), "persist Brain Host activation") {
		t.Fatalf("first startup activation error = %v", err)
	}
	if len(fw.sentCalls) != 1 || fw.readyInputCalls != 1 {
		t.Fatalf("accepted startup effects sends=%+v ready=%d", fw.sentCalls, fw.readyInputCalls)
	}
	if err := os.Remove(store.HostActivationPath()); err != nil {
		t.Fatal(err)
	}

	if _, err := service.EnsureHostSnapshot(); err != nil {
		t.Fatal(err)
	}
	if len(fw.sentCalls) != 1 || fw.readyInputCalls != 1 {
		t.Fatalf("accepted receipt was replayed after persistence recovery: sends=%+v ready=%d", fw.sentCalls, fw.readyInputCalls)
	}
	activation, err := store.HostActivation()
	if err != nil {
		t.Fatal(err)
	}
	if activation.SessionID == "" || activation.HostGeneration == "" || activation.Receipt == "" {
		t.Fatalf("accepted receipt did not settle durable activation: %+v", activation)
	}
}

func TestLiveHostActivationQueuesWithUserInputsOutsideSnapshot(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const (
		hostID            = "brain-agent-brain-live:@41"
		hostGeneration    = "host-generation-live"
		providerSessionID = "provider-history-must-not-change"
		transcriptPath    = "/private/provider/transcript.jsonl"
		providerDataRoot  = "/private/provider"
		memorySecret      = "MEMORY_TRANSCRIPT_SECRET"
		profileSecret     = "PROFILE_TRANSCRIPT_SECRET"
	)
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, transcriptPath, providerDataRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.memoryPath(), []byte(memorySecret), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.profileNotesPath(), []byte(profileSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	beforeHost, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}

	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			hostID: {
				ID:      hostID,
				Name:    "Brain",
				Command: "codex --dangerously-bypass-approvals-and-sandbox",
				Hidden:  true,
				State:   classifier.StateRunning,
			},
		},
		ownedGenerations: map[string]string{hostID: hostGeneration},
		outcomes:         map[string]watcher.InputOutcome{},
		providerEvidence: map[string]watcher.ProviderActivityObservation{
			hostID: {ID: "active-provider-turn", Status: "running", StartedAt: time.Now().UTC()},
		},
	}
	service := NewService(store, fw, work.NewExecutorConfig("codex", map[string]work.Executor{
		"codex": {Name: "codex", Command: "codex", Kind: "codex"},
	}))

	if _, err := fw.SendInputWithReceiptResult(hostID, "queued user input one", "user-receipt-one"); err != nil {
		t.Fatal(err)
	}
	startedAt := time.Now()
	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(startedAt); elapsed > 100*time.Millisecond {
		t.Fatalf("read-only Snapshot entered Host input delivery: %s", elapsed)
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID != hostID || snapshot.Memory != memorySecret || snapshot.Profile != profileSecret {
		t.Fatalf("Snapshot client data was not projected while activation was busy: %+v", snapshot)
	}
	if len(fw.sentCalls) != 1 || fw.readyInputCalls != 0 {
		t.Fatalf("Snapshot mutated Host input: sends=%+v ready_calls=%d", fw.sentCalls, fw.readyInputCalls)
	}
	activation, err := store.HostActivation()
	if err != nil {
		t.Fatal(err)
	}
	if activation.SessionID != "" {
		t.Fatalf("Snapshot falsely persisted activation: %+v", activation)
	}

	if _, err := service.EnsureHostSnapshot(); err != nil {
		t.Fatal(err)
	}
	if _, err := fw.SendInputWithReceiptResult(hostID, "queued user input two", "user-receipt-two"); err != nil {
		t.Fatal(err)
	}
	activation, err = store.HostActivation()
	if err != nil {
		t.Fatal(err)
	}
	wantReceipt := hostActivationReceipt(hostID, hostGeneration, brainWorkerRoleContractVersion)
	if activation.SessionID != hostID || activation.HostGeneration != hostGeneration ||
		activation.ContractVersion != brainWorkerRoleContractVersion || activation.Receipt != wantReceipt {
		t.Fatalf("queued activation did not persist exact receipt identity: %+v", activation)
	}
	if fw.readyInputCalls != 0 {
		t.Fatalf("live activation called startup readiness API %d times", fw.readyInputCalls)
	}
	if len(fw.sentCalls) != 3 || fw.sentCalls[0].text != "queued user input one" ||
		!strings.Contains(fw.sentCalls[1].text, brainWorkerRoleContract) ||
		fw.sentCalls[2].text != "queued user input two" {
		t.Fatalf("per-Session queue order = %+v", fw.sentCalls)
	}
	for _, secret := range []string{memorySecret, profileSecret, transcriptPath, providerSessionID} {
		if strings.Contains(fw.sentCalls[1].text, secret) {
			t.Fatalf("activation prompt leaked private value %q", secret)
		}
	}
	if _, err := service.EnsureHostSnapshot(); err != nil {
		t.Fatal(err)
	}
	if len(fw.sentCalls) != 3 {
		t.Fatalf("repeated lifecycle reconciliation duplicated activation: %+v", fw.sentCalls)
	}
	afterHost, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterHost, beforeHost) {
		t.Fatalf("activation changed provider/history identity:\nbefore=%+v\nafter=%+v", beforeHost, afterHost)
	}
}

func TestLiveHostActivationAmbiguousReceiptIsNeverReplayed(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const (
		hostID         = "brain-agent-brain-live:@ambiguous"
		hostGeneration = "host-generation-ambiguous"
	)
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	receipt := hostActivationReceipt(hostID, hostGeneration, brainWorkerRoleContractVersion)
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			hostID: {ID: hostID, Command: "codex", Hidden: true, State: classifier.StateRunning},
		},
		ownedGenerations: map[string]string{hostID: hostGeneration},
		outcomes:         map[string]watcher.InputOutcome{receipt: watcher.InputAmbiguous},
	}
	service := NewService(store, fw, work.NewExecutorConfig("codex", map[string]work.Executor{
		"codex": {Name: "codex", Command: "codex", Kind: "codex"},
	}))

	if _, err := service.EnsureHostSnapshot(); !errors.Is(err, ErrHostActivationAmbiguous) {
		t.Fatalf("ambiguous activation error = %v", err)
	}
	if len(fw.sentCalls) != 0 || fw.readyInputCalls != 0 {
		t.Fatalf("ambiguous receipt was replayed: sends=%+v ready_calls=%d", fw.sentCalls, fw.readyInputCalls)
	}
	activation, err := store.HostActivation()
	if err != nil {
		t.Fatal(err)
	}
	if activation.SessionID != "" {
		t.Fatalf("ambiguous receipt was falsely marked accepted: %+v", activation)
	}
}
