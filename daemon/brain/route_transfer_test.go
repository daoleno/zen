package brain

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/modelprofiles"
	"github.com/daoleno/zen/daemon/work"
)

type transferOutcome struct {
	persist modelprofiles.PersistResult
	err     error
}

type fakeRouteLifecycle struct {
	transferErr     error
	transferApplied bool
	// failTransferN makes the Nth TransferSession call (1-based) fail closed.
	failTransferN int
	// transferScript, when non-empty, returns outcomes in order (then durable ok).
	transferScript []transferOutcome
	transfers      [][2]string
	releases       []string
	releaseErr     error
	releasePersist modelprofiles.PersistResult
	resumeCmd      string
	resumeEnv      map[string]string
	resumeFound    bool
	resumeErr      error
	resumeCalls    []string

	prepareCalls  [][3]string
	preparePlan   *modelprofiles.SessionLaunchPlan
	prepareErr    error
	commitCalls   [][2]string
	commitPersist modelprofiles.PersistResult
	commitErr     error
	abortCalls    []string
	abortPersist  modelprofiles.PersistResult
	abortErr      error
	// controlSocket, when set, is returned by CodexControlSocket (teardown
	// artifact-cleanup assertions).
	controlSocket string
}

func (f *fakeRouteLifecycle) CodexControlSocket(sessionID string) string {
	return f.controlSocket
}

func (f *fakeRouteLifecycle) TransferSession(fromID, toID string) (modelprofiles.PersistResult, error) {
	f.transfers = append(f.transfers, [2]string{fromID, toID})
	if len(f.transferScript) > 0 {
		idx := len(f.transfers) - 1
		if idx < len(f.transferScript) {
			out := f.transferScript[idx]
			return out.persist, out.err
		}
		return modelprofiles.PersistResult{Applied: true, Durable: true}, nil
	}
	if f.failTransferN > 0 && len(f.transfers) == f.failTransferN {
		return modelprofiles.PersistResult{Applied: false, Durable: false}, errors.New("injected transfer N failure")
	}
	if f.transferErr != nil && !f.transferApplied {
		return modelprofiles.PersistResult{Applied: false, Durable: false}, f.transferErr
	}
	if f.transferErr != nil && f.transferApplied {
		return modelprofiles.PersistResult{Applied: true, Durable: false}, f.transferErr
	}
	return modelprofiles.PersistResult{Applied: true, Durable: true}, nil
}

func (f *fakeRouteLifecycle) ResumeLaunch(sessionID, baseCommand string) (string, map[string]string, bool, error) {
	f.resumeCalls = append(f.resumeCalls, sessionID)
	if f.resumeErr != nil {
		return "", nil, false, f.resumeErr
	}
	cmd := f.resumeCmd
	if cmd == "" {
		cmd = baseCommand
	}
	return cmd, f.resumeEnv, f.resumeFound, nil
}

func (f *fakeRouteLifecycle) ReleaseSession(sessionID string) (modelprofiles.PersistResult, error) {
	f.releases = append(f.releases, sessionID)
	if f.releaseErr != nil {
		if f.releasePersist.Applied || f.releasePersist.Durable {
			return f.releasePersist, f.releaseErr
		}
		return modelprofiles.PersistResult{Applied: false, Durable: false}, f.releaseErr
	}
	if f.releasePersist.Applied || f.releasePersist.Durable {
		return f.releasePersist, nil
	}
	return modelprofiles.PersistResult{Applied: true, Durable: true}, nil
}

func (f *fakeRouteLifecycle) PrepareLaunch(executorID, profileID, baseCommand string) (modelprofiles.SessionLaunchPlan, error) {
	f.prepareCalls = append(f.prepareCalls, [3]string{executorID, profileID, baseCommand})
	if f.prepareErr != nil && (f.preparePlan == nil || !f.preparePlan.Persist.Applied) {
		if f.preparePlan != nil {
			return *f.preparePlan, f.prepareErr
		}
		return modelprofiles.SessionLaunchPlan{}, f.prepareErr
	}
	if f.preparePlan != nil {
		return *f.preparePlan, f.prepareErr
	}
	// Default: bypass so legacy route-transfer tests keep raw host commands.
	return modelprofiles.SessionLaunchPlan{Bypass: true, Command: baseCommand}, nil
}

func (f *fakeRouteLifecycle) CommitLaunch(provisionalID, sessionID string) (modelprofiles.SessionRouteState, modelprofiles.WireSessionSnapshot, modelprofiles.PersistResult, error) {
	f.commitCalls = append(f.commitCalls, [2]string{provisionalID, sessionID})
	persist := f.commitPersist
	if !f.commitPersist.Applied && !f.commitPersist.Durable {
		if f.commitErr != nil {
			return modelprofiles.SessionRouteState{}, modelprofiles.WireSessionSnapshot{}, modelprofiles.PersistResult{Applied: false, Durable: false}, f.commitErr
		}
		persist = modelprofiles.PersistResult{Applied: true, Durable: true}
	}
	return modelprofiles.SessionRouteState{}, modelprofiles.WireSessionSnapshot{}, persist, f.commitErr
}

func (f *fakeRouteLifecycle) AbortLaunch(provisionalID string) (modelprofiles.PersistResult, error) {
	f.abortCalls = append(f.abortCalls, provisionalID)
	if f.abortErr != nil {
		if f.abortPersist.Applied || f.abortPersist.Durable {
			return f.abortPersist, f.abortErr
		}
		return modelprofiles.PersistResult{Applied: false, Durable: false}, f.abortErr
	}
	if f.abortPersist.Applied || f.abortPersist.Durable {
		return f.abortPersist, nil
	}
	return modelprofiles.PersistResult{Applied: true, Durable: true}, nil
}

func TestMissingTmuxTransferPersistFailureKillsAndKeepsOldResumable(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-missing-bound:@9"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	transcriptPath := "/home/daoleno/.codex/sessions/2026/08/06/rollout-" + providerSessionID + ".jsonl"
	if err := store.SetHostSession(oldID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, transcriptPath, "/home/daoleno"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	routes := &fakeRouteLifecycle{
		resumeFound:     true,
		resumeCmd:       "codex resume " + providerSessionID,
		resumeEnv:       map[string]string{"OPENAI_API_KEY": "zen-loopback-placeholder-not-a-secret"},
		transferErr:     errors.New("injected transfer persist failure"),
		transferApplied: false,
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	_, err = service.EnsureHostSnapshot()
	if err == nil {
		t.Fatal("expected fail-closed snapshot")
	}
	if !strings.Contains(err.Error(), "route transfer") {
		t.Fatalf("err=%v", err)
	}
	if len(fw.created) != 1 {
		t.Fatalf("created=%#v", fw.created)
	}
	newID := fw.created[0].id
	if len(fw.killed) != 1 || fw.killed[0] != newID {
		t.Fatalf("killed=%#v want %q", fw.killed, newID)
	}
	if len(routes.transfers) != 1 || routes.transfers[0] != [2]string{oldID, newID} {
		t.Fatalf("transfers=%#v", routes.transfers)
	}
	host, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if host.ID != oldID || host.ProviderSessionID != providerSessionID {
		t.Fatalf("old host binding must remain resumable: %+v", host)
	}
}

func TestMissingTmuxTransferAppliedNondurableFailsClosedNoSuccess(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-missing-bound:@9"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	transcriptPath := "/home/daoleno/.codex/sessions/2026/08/06/rollout-" + providerSessionID + ".jsonl"
	if err := store.SetHostSession(oldID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, transcriptPath, "/home/daoleno"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	routes := &fakeRouteLifecycle{
		resumeFound:     true,
		resumeCmd:       "codex resume " + providerSessionID,
		transferErr:     modelprofiles.ErrPersistDirSync,
		transferApplied: true, // forward and compensation both Applied+!Durable
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	_, err = service.EnsureHostSnapshot()
	if err == nil || !errors.Is(err, ErrRouteTransferNotDurable) || !strings.Contains(err.Error(), "live owner") {
		t.Fatalf("err=%v", err)
	}
	if len(fw.created) != 1 {
		t.Fatalf("created=%#v", fw.created)
	}
	newID := fw.created[0].id
	if len(fw.killed) != 0 {
		t.Fatalf("ambiguous compensation must preserve live owner: killed=%v", fw.killed)
	}
	if !fw.HasSession(newID) {
		t.Fatalf("live owner %q must remain", newID)
	}
	after, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("host binding mutated:\nbefore=%s\nafter=%s", before, after)
	}
	audit, _ := os.ReadFile(store.HostReplacementsPath())
	if strings.Contains(string(audit), hostReplaceReasonMissingTmuxResumeLaunched) {
		t.Fatalf("must not audit resume success: %s", audit)
	}
}

func TestNewChatReleasesRoutedHostBinding(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-host-routed:@1"
	if err := store.SetHostSession(oldID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			oldID: {ID: oldID, Name: "Brain", Command: "codex", State: classifier.StateRunning, Hidden: true},
		},
	}
	routes := &fakeRouteLifecycle{}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	if _, err := service.NewChat(); err != nil {
		t.Fatal(err)
	}
	if len(fw.killed) != 1 || fw.killed[0] != oldID {
		t.Fatalf("killed=%v", fw.killed)
	}
	if len(routes.releases) != 1 || routes.releases[0] != oldID {
		t.Fatalf("releases=%v", routes.releases)
	}
}

func TestProviderMismatchReplacementReleasesRoute(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-old-grok:@1"
	if err := store.SetHostSession(oldID, "grok"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			oldID: {ID: oldID, Name: "Brain", Command: "grok", State: classifier.StateRunning, Hidden: true, Cwd: store.WorkspacePath()},
		},
	}
	fw.agents = append(fw.agents, fw.sessions[oldID])
	routes := &fakeRouteLifecycle{}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{
			"grok":  {Name: "grok", Command: "grok", Kind: "grok"},
			"codex": {Name: "codex", Command: "codex", Kind: "codex"},
		},
	})
	service.SetSessionRouteLifecycle(routes)
	t.Setenv("ZEN_BRAIN_HOST_EXECUTOR", "codex")

	if _, err := service.EnsureHostSnapshot(); err != nil {
		t.Fatal(err)
	}
	if len(routes.releases) != 1 || routes.releases[0] != oldID {
		t.Fatalf("releases=%v", routes.releases)
	}
	if len(fw.killed) != 1 || fw.killed[0] != oldID {
		t.Fatalf("killed=%v", fw.killed)
	}
}

func TestProviderMismatchKillFailureStillLivePreservesAndAborts(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-old-grok:@1"
	if err := store.SetHostSession(oldID, "grok"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			oldID: {ID: oldID, Name: "Brain", Command: "grok", State: classifier.StateRunning, Hidden: true, Cwd: store.WorkspacePath()},
		},
		killErr:        errors.New("injected kill failure"),
		killLeavesLive: true,
	}
	fw.agents = append(fw.agents, fw.sessions[oldID])
	routes := &fakeRouteLifecycle{}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{
			"grok":  {Name: "grok", Command: "grok", Kind: "grok"},
			"codex": {Name: "codex", Command: "codex", Kind: "codex"},
		},
	})
	service.SetSessionRouteLifecycle(routes)
	t.Setenv("ZEN_BRAIN_HOST_EXECUTOR", "codex")

	_, err = service.EnsureHostSnapshot()
	if err == nil {
		t.Fatal("expected teardown failure")
	}
	if !strings.Contains(err.Error(), "injected kill failure") {
		t.Fatalf("err=%v", err)
	}
	if len(routes.releases) != 0 {
		t.Fatalf("must not release while still live: %v", routes.releases)
	}
	if len(fw.created) != 0 {
		t.Fatalf("must not create replacement: %#v", fw.created)
	}
}

func TestProviderMismatchReleaseFailureSurfaced(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-old-grok:@1"
	if err := store.SetHostSession(oldID, "grok"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			oldID: {ID: oldID, Name: "Brain", Command: "grok", State: classifier.StateRunning, Hidden: true, Cwd: store.WorkspacePath()},
		},
	}
	fw.agents = append(fw.agents, fw.sessions[oldID])
	routes := &fakeRouteLifecycle{
		releaseErr: errors.New("injected release failure"),
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{
			"grok":  {Name: "grok", Command: "grok", Kind: "grok"},
			"codex": {Name: "codex", Command: "codex", Kind: "codex"},
		},
	})
	service.SetSessionRouteLifecycle(routes)
	t.Setenv("ZEN_BRAIN_HOST_EXECUTOR", "codex")

	_, err = service.EnsureHostSnapshot()
	if err == nil || !strings.Contains(err.Error(), "injected release failure") {
		t.Fatalf("err=%v", err)
	}
	if len(fw.created) != 0 {
		t.Fatalf("must not create replacement after release failure: %#v", fw.created)
	}
}

func TestResumeRouteTransferFailureSurfacesKillError(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-missing-bound:@9"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	transcriptPath := "/home/daoleno/.codex/sessions/2026/08/06/rollout-" + providerSessionID + ".jsonl"
	if err := store.SetHostSession(oldID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, transcriptPath, "/home/daoleno"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{killErr: errors.New("injected compensation kill failure")}
	routes := &fakeRouteLifecycle{
		resumeFound: true,
		resumeCmd:   "codex resume " + providerSessionID,
		transferErr: errors.New("injected transfer failure"),
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	_, err = service.EnsureHostSnapshot()
	if err == nil {
		t.Fatal("expected transfer+kill failure")
	}
	if !strings.Contains(err.Error(), "route transfer") || !strings.Contains(err.Error(), "injected compensation kill failure") {
		t.Fatalf("must join transfer and kill errors: %v", err)
	}
	if len(fw.created) != 1 || len(fw.killed) != 1 {
		t.Fatalf("created=%#v killed=%#v", fw.created, fw.killed)
	}
}

func TestRecoverLiveTransfersRouteBeforeHostBind(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deadID := "brain-agent-brain-dead:@1"
	aliveID := "brain-agent-brain-alive:@2"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	if err := store.SetHostSession(deadID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, "/tmp/"+providerSessionID+".jsonl", "/home"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			aliveID: {
				ID: aliveID, Name: "Brain (" + aliveID + ")", Cwd: store.WorkspacePath(),
				Command: "codex resume " + providerSessionID, State: classifier.StateRunning, Hidden: true,
			},
		},
	}
	fw.agents = append(fw.agents, fw.sessions[aliveID])
	routes := &fakeRouteLifecycle{}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	snapshot, err := service.EnsureHostSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID != aliveID {
		t.Fatalf("host=%#v", snapshot.HostAgent)
	}
	if len(routes.transfers) != 1 || routes.transfers[0] != [2]string{deadID, aliveID} {
		t.Fatalf("transfers=%#v", routes.transfers)
	}
	if len(fw.created) != 0 || len(fw.killed) != 0 {
		t.Fatalf("created=%#v killed=%#v", fw.created, fw.killed)
	}
	host, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if host.ID != aliveID {
		t.Fatalf("host=%+v", host)
	}
	audit, _ := os.ReadFile(store.HostReplacementsPath())
	if !strings.Contains(string(audit), hostReplaceReasonRecoveredAlive) {
		t.Fatalf("audit=%s", audit)
	}
}

func TestRecoverLiveRouteTransferFailurePreservesBindingNoAudit(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deadID := "brain-agent-brain-dead:@1"
	aliveID := "brain-agent-brain-alive:@2"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	if err := store.SetHostSession(deadID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, "/tmp/"+providerSessionID+".jsonl", "/home"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			aliveID: {
				ID: aliveID, Name: "Brain (" + aliveID + ")", Cwd: store.WorkspacePath(),
				Command: "codex resume " + providerSessionID, State: classifier.StateRunning, Hidden: true,
			},
		},
	}
	fw.agents = append(fw.agents, fw.sessions[aliveID])
	routes := &fakeRouteLifecycle{transferErr: errors.New("injected recover transfer failure")}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	_, err = service.EnsureHostSnapshot()
	if err == nil || !strings.Contains(err.Error(), "route transfer") {
		t.Fatalf("err=%v", err)
	}
	if len(fw.created) != 0 || len(fw.killed) != 0 {
		t.Fatalf("created=%#v killed=%#v", fw.created, fw.killed)
	}
	after, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("binding mutated:\nbefore=%s\nafter=%s", before, after)
	}
	if fw.HasSession(aliveID) == false {
		t.Fatal("live recovered host must remain")
	}
	audit, _ := os.ReadFile(store.HostReplacementsPath())
	if strings.Contains(string(audit), hostReplaceReasonRecoveredAlive) {
		t.Fatalf("must not audit recovered_alive on transfer failure: %s", audit)
	}
}

func TestRecoverLiveNoRouteTransferIsNoOp(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deadID := "brain-agent-brain-dead:@1"
	aliveID := "brain-agent-brain-alive:@2"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	if err := store.SetHostSession(deadID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, "/tmp/"+providerSessionID+".jsonl", "/home"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			aliveID: {
				ID: aliveID, Name: "Brain (" + aliveID + ")", Cwd: store.WorkspacePath(),
				Command: "codex resume " + providerSessionID, State: classifier.StateRunning, Hidden: true,
			},
		},
	}
	fw.agents = append(fw.agents, fw.sessions[aliveID])
	routes := &fakeRouteLifecycle{transferErr: modelprofiles.ErrBindingNotFound}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	snapshot, err := service.EnsureHostSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID != aliveID {
		t.Fatalf("host=%#v", snapshot.HostAgent)
	}
	if len(routes.transfers) != 1 || routes.transfers[0] != [2]string{deadID, aliveID} {
		t.Fatalf("expected no-op transfer attempt: %#v", routes.transfers)
	}
	host, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if host.ID != aliveID {
		t.Fatalf("host=%+v", host)
	}
	audit, _ := os.ReadFile(store.HostReplacementsPath())
	if !strings.Contains(string(audit), hostReplaceReasonRecoveredAlive) {
		t.Fatalf("audit=%s", audit)
	}
}

func TestRecoverLiveRouteTransferAppliedNondurableCompensatesNoBind(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deadID := "brain-agent-brain-dead:@1"
	aliveID := "brain-agent-brain-alive:@2"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	if err := store.SetHostSession(deadID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, "/tmp/"+providerSessionID+".jsonl", "/home"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			aliveID: {
				ID: aliveID, Name: "Brain (" + aliveID + ")", Cwd: store.WorkspacePath(),
				Command: "codex resume " + providerSessionID, State: classifier.StateRunning, Hidden: true,
			},
		},
	}
	fw.agents = append(fw.agents, fw.sessions[aliveID])
	routes := &fakeRouteLifecycle{
		transferScript: []transferOutcome{
			{persist: modelprofiles.PersistResult{Applied: true, Durable: false}, err: modelprofiles.ErrPersistDirSync},
			{persist: modelprofiles.PersistResult{Applied: true, Durable: true}, err: nil},
		},
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	_, err = service.EnsureHostSnapshot()
	if err == nil || !errors.Is(err, ErrRouteTransferNotDurable) {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(err.Error(), "compensated") {
		t.Fatalf("want durable compensation signal: %v", err)
	}
	if len(routes.transfers) != 2 ||
		routes.transfers[0] != [2]string{deadID, aliveID} ||
		routes.transfers[1] != [2]string{aliveID, deadID} {
		t.Fatalf("transfers=%#v", routes.transfers)
	}
	if len(fw.created) != 0 || len(fw.killed) != 0 {
		t.Fatalf("recovered live host must not be killed: created=%#v killed=%#v", fw.created, fw.killed)
	}
	if !fw.HasSession(aliveID) {
		t.Fatal("live recovered owner must remain")
	}
	after, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("host binding mutated:\nbefore=%s\nafter=%s", before, after)
	}
	audit, _ := os.ReadFile(store.HostReplacementsPath())
	if strings.Contains(string(audit), hostReplaceReasonRecoveredAlive) {
		t.Fatalf("must not audit recovered_alive: %s", audit)
	}
}

func TestRecoverLiveRouteTransferAppliedNondurableCompensationAmbiguityPreservesLive(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deadID := "brain-agent-brain-dead:@1"
	aliveID := "brain-agent-brain-alive:@2"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	if err := store.SetHostSession(deadID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, "/tmp/"+providerSessionID+".jsonl", "/home"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			aliveID: {
				ID: aliveID, Name: "Brain (" + aliveID + ")", Cwd: store.WorkspacePath(),
				Command: "codex resume " + providerSessionID, State: classifier.StateRunning, Hidden: true,
			},
		},
	}
	fw.agents = append(fw.agents, fw.sessions[aliveID])
	routes := &fakeRouteLifecycle{
		transferScript: []transferOutcome{
			{persist: modelprofiles.PersistResult{Applied: true, Durable: false}, err: modelprofiles.ErrPersistDirSync},
			{persist: modelprofiles.PersistResult{Applied: true, Durable: false}, err: modelprofiles.ErrPersistDirSync},
		},
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	_, err = service.EnsureHostSnapshot()
	if err == nil || !errors.Is(err, ErrRouteTransferNotDurable) || !strings.Contains(err.Error(), "live owner") {
		t.Fatalf("err=%v", err)
	}
	if len(fw.killed) != 0 || !fw.HasSession(aliveID) {
		t.Fatalf("must preserve live owner: killed=%#v has=%v", fw.killed, fw.HasSession(aliveID))
	}
	after, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("host binding mutated:\nbefore=%s\nafter=%s", before, after)
	}
	audit, _ := os.ReadFile(store.HostReplacementsPath())
	if strings.Contains(string(audit), hostReplaceReasonRecoveredAlive) {
		t.Fatalf("must not audit success: %s", audit)
	}
}

func TestResumeRouteTransferAppliedNondurableCompensatesKillsSpawn(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-missing-bound:@9"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	transcriptPath := "/home/daoleno/.codex/sessions/2026/08/06/rollout-" + providerSessionID + ".jsonl"
	if err := store.SetHostSession(oldID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, transcriptPath, "/home/daoleno"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	routes := &fakeRouteLifecycle{
		resumeFound: true,
		resumeCmd:   "codex resume " + providerSessionID,
		transferScript: []transferOutcome{
			{persist: modelprofiles.PersistResult{Applied: true, Durable: false}, err: modelprofiles.ErrPersistDirSync},
			{persist: modelprofiles.PersistResult{Applied: true, Durable: true}, err: nil},
		},
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	_, err = service.EnsureHostSnapshot()
	if err == nil || !errors.Is(err, ErrRouteTransferNotDurable) {
		t.Fatalf("err=%v", err)
	}
	if len(fw.created) != 1 {
		t.Fatalf("created=%#v", fw.created)
	}
	newID := fw.created[0].id
	if len(routes.transfers) != 2 ||
		routes.transfers[0] != [2]string{oldID, newID} ||
		routes.transfers[1] != [2]string{newID, oldID} {
		t.Fatalf("transfers=%#v", routes.transfers)
	}
	if len(fw.killed) != 1 || fw.killed[0] != newID {
		t.Fatalf("durable compensation may kill resume spawn: killed=%#v", fw.killed)
	}
	after, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("host binding mutated:\nbefore=%s\nafter=%s", before, after)
	}
	audit, _ := os.ReadFile(store.HostReplacementsPath())
	if strings.Contains(string(audit), hostReplaceReasonMissingTmuxResumeLaunched) {
		t.Fatalf("must not audit resume success: %s", audit)
	}
	if !strings.Contains(string(audit), hostReplaceReasonMissingTmuxUnrecoverable) {
		t.Fatalf("want unrecoverable audit after nondurable transfer: %s", audit)
	}
}

func TestResumeRouteTransferAppliedNondurableCompensationFailurePreservesLive(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-missing-bound:@9"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	transcriptPath := "/home/daoleno/.codex/sessions/2026/08/06/rollout-" + providerSessionID + ".jsonl"
	if err := store.SetHostSession(oldID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, transcriptPath, "/home/daoleno"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	routes := &fakeRouteLifecycle{
		resumeFound: true,
		resumeCmd:   "codex resume " + providerSessionID,
		transferScript: []transferOutcome{
			{persist: modelprofiles.PersistResult{Applied: true, Durable: false}, err: modelprofiles.ErrPersistDirSync},
			{persist: modelprofiles.PersistResult{Applied: false, Durable: false}, err: errors.New("injected compensation failure")},
		},
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	_, err = service.EnsureHostSnapshot()
	if err == nil || !errors.Is(err, ErrRouteTransferNotDurable) || !strings.Contains(err.Error(), "live owner") {
		t.Fatalf("err=%v", err)
	}
	if len(fw.created) != 1 {
		t.Fatalf("created=%#v", fw.created)
	}
	newID := fw.created[0].id
	if len(fw.killed) != 0 {
		t.Fatalf("ambiguous compensation must not kill live owner: killed=%#v", fw.killed)
	}
	if !fw.HasSession(newID) {
		t.Fatalf("live owner %q must remain", newID)
	}
	after, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("host binding mutated:\nbefore=%s\nafter=%s", before, after)
	}
	audit, _ := os.ReadFile(store.HostReplacementsPath())
	if strings.Contains(string(audit), hostReplaceReasonMissingTmuxResumeLaunched) {
		t.Fatalf("must not audit resume success: %s", audit)
	}
}

func TestRecoverLiveBindFailureAfterTransferRollsRouteNoAudit(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deadID := "brain-agent-brain-dead:@1"
	aliveID := "brain-agent-brain-alive:@2"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	if err := store.SetHostSession(deadID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, "/tmp/"+providerSessionID+".jsonl", "/home"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			aliveID: {
				ID: aliveID, Name: "Brain (" + aliveID + ")", Cwd: store.WorkspacePath(),
				Command: "codex resume " + providerSessionID, State: classifier.StateRunning, Hidden: true,
			},
		},
	}
	fw.agents = append(fw.agents, fw.sessions[aliveID])
	routes := &fakeRouteLifecycle{}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)
	store.replaceHostBindingWrite = func(string, any) error {
		return errors.New("injected recover bind failure after transfer")
	}

	_, err = service.EnsureHostSnapshot()
	if err == nil || !strings.Contains(err.Error(), "injected recover bind failure") {
		t.Fatalf("err=%v", err)
	}
	if len(routes.transfers) != 2 ||
		routes.transfers[0] != [2]string{deadID, aliveID} ||
		routes.transfers[1] != [2]string{aliveID, deadID} {
		t.Fatalf("expected transfer then rollback: %#v", routes.transfers)
	}
	if len(fw.killed) != 0 {
		t.Fatalf("must not kill live recovered host: %#v", fw.killed)
	}
	after, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("binding mutated:\nbefore=%s\nafter=%s", before, after)
	}
	audit, _ := os.ReadFile(store.HostReplacementsPath())
	if strings.Contains(string(audit), hostReplaceReasonRecoveredAlive) {
		t.Fatalf("must not audit recovered_alive: %s", audit)
	}
}

func TestRouteTransferRollbackFailureRetainsLiveOwner(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-missing-bound:@9"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	transcriptPath := "/home/daoleno/.codex/sessions/2026/08/06/rollout-" + providerSessionID + ".jsonl"
	if err := store.SetHostSession(oldID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, transcriptPath, "/home/daoleno"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	routes := &fakeRouteLifecycle{
		resumeFound:   true,
		resumeCmd:     "codex resume " + providerSessionID,
		failTransferN: 2, // forward ok, rollback fails
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	// Force ReplaceHostSessionBinding failure via read-only host session file after create.
	// Simpler: inject by making store path unwritable after create is awkward.
	// Use a stub store failure by closing workspace permissions on host_session after create...
	// Instead: monkey via temporary chmod on host session path after first snapshot path.
	// Direct unit of rollback: call ensure path by making SetHostProviderTranscript path broken.

	// Make host_session.json directory not writable after CreateSession by wrapping isn't available.
	// Use failpoint: replace store root files — chmod parent after CreateSession via custom watcher onCreate.

	// Practical approach: use ReplaceHostSessionBinding failure by deleting the store mid-flight
	// is flaky. Call the transfer/rollback sequence through Snapshot after making
	// host_session.json a directory so ReplaceHostSessionBinding fails.
	hostPath := store.HostSessionPath()
	fw.createHook = func() {
		_ = os.Remove(hostPath)
		if mkErr := os.Mkdir(hostPath, 0o700); mkErr != nil {
			t.Fatalf("mkdir host path: %v", mkErr)
		}
	}

	_, err = service.EnsureHostSnapshot()
	if err == nil {
		t.Fatal("expected bind+rollback failure")
	}
	if !strings.Contains(err.Error(), "route rollback") || !strings.Contains(err.Error(), "did not durably restore") {
		t.Fatalf("err=%v", err)
	}
	if len(fw.created) != 1 {
		t.Fatalf("created=%#v", fw.created)
	}
	newID := fw.created[0].id
	if len(fw.killed) != 0 {
		t.Fatalf("must not kill live route owner: killed=%v", fw.killed)
	}
	if len(routes.transfers) != 2 || routes.transfers[0] != [2]string{oldID, newID} || routes.transfers[1] != [2]string{newID, oldID} {
		t.Fatalf("transfers=%#v", routes.transfers)
	}
	if !fw.HasSession(newID) {
		t.Fatalf("live owner %q must remain", newID)
	}
}

func TestHostTeardownResourceReleaseFailurePreservesAndSurfaces(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-host-routed:@1"
	if err := store.SetHostSession(oldID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			oldID: {ID: oldID, Name: "Brain", Command: "codex", State: classifier.StateRunning, Hidden: true},
		},
		killErr: errors.New("delegated resource release failed: injected"),
	}
	routes := &fakeRouteLifecycle{}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	_, err = service.NewChat()
	if err == nil || !strings.Contains(err.Error(), "resource release") {
		t.Fatalf("err=%v", err)
	}
	if len(routes.releases) != 0 {
		t.Fatalf("must not release on resource failure: %v", routes.releases)
	}
}

func TestHostTeardownProbeFailurePreservesRoute(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-host-routed:@1"
	if err := store.SetHostSession(oldID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			oldID: {ID: oldID, Name: "Brain", Command: "codex", State: classifier.StateRunning, Hidden: true},
		},
		killErr:        errors.New("injected kill failure"),
		killLeavesLive: true,
		probeErr:       errors.New("injected probe failure"),
	}
	routes := &fakeRouteLifecycle{}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	_, err = service.NewChat()
	if err == nil || !strings.Contains(err.Error(), "probe failure") {
		t.Fatalf("err=%v", err)
	}
	if len(routes.releases) != 0 {
		t.Fatalf("releases=%v", routes.releases)
	}
}

func TestHostTeardownMissingRetryAfterResourceFailureConverges(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-host-routed:@1"
	if err := store.SetHostSession(oldID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			oldID: {ID: oldID, Name: "Brain", Command: "codex", State: classifier.StateRunning, Hidden: true},
		},
		killErr: errors.New("delegated resource release failed: injected"),
	}
	routes := &fakeRouteLifecycle{}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	if _, err := service.NewChat(); err == nil {
		t.Fatal("expected first failure")
	}
	fw.killErr = nil
	// Window already removed by first KillSession; second NewChat should succeed.
	if _, err := service.NewChat(); err != nil {
		t.Fatalf("retry NewChat: %v", err)
	}
	if len(routes.releases) != 1 || routes.releases[0] != oldID {
		t.Fatalf("releases=%v", routes.releases)
	}
}

func startBrainRouteOwner(t *testing.T) *modelprofiles.Owner {
	t.Helper()
	root := t.TempDir()
	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: filepath.Join(root, "model-profiles.toml"),
		RoutesPath:   filepath.Join(root, "route-bindings.json"),
		ListenerPath: filepath.Join(root, "route-listener.json"),
		Lookup:       func(string) (string, bool) { return "ready", true },
		Verifier:     brainRouteTestVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	return owner
}

type brainRouteTestVerifier struct{}

func (brainRouteTestVerifier) VerifyProfileContract(profile modelprofiles.Profile) (modelprofiles.VerifiedProfileContract, error) {
	env := modelprofiles.DefaultTestEnvelope()
	route, _ := modelprofiles.RouteProtocolFor(profile.Protocol)
	return modelprofiles.VerifiedProfileContract{
		Provenance:       modelprofiles.ContractProvenanceBuiltinCatalog,
		ClientModelID:    profile.ClientModel,
		UpstreamModelID:  profile.Model,
		ExecutorID:       profile.ExecutorID,
		Protocol:         profile.Protocol,
		RouteProtocol:    route,
		ProviderID:       profile.ProviderID,
		ClientEnvelope:   env,
		UpstreamEnvelope: env,
		HistoryDomain: modelprofiles.DeriveOpaqueHistoryDomain(
			profile.Protocol, profile.ProviderID, profile.BaseURL, profile.Model, profile.ClientModel,
		),
	}, nil
}

func bindCodexRoute(t *testing.T, owner *modelprofiles.Owner, sessionID string) {
	t.Helper()
	profile := modelprofiles.Profile{
		ID: "codex-routed", Name: "Routed", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "acme", ProviderLabel: "Acme",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "up-1",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://gateway.example/v1",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ACME_KEY",
	}
	if _, err := owner.UpsertProfile(profile, owner.Catalog().Revision, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(modelprofiles.ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, sessionID); err != nil {
		t.Fatal(err)
	}
}

func TestResumeNondurableTransferRealOwnerRestoresRestartVisibleRoute(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-missing-bound:@9"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	transcriptPath := "/home/daoleno/.codex/sessions/2026/08/06/rollout-" + providerSessionID + ".jsonl"
	if err := store.SetHostSession(oldID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, transcriptPath, "/home/daoleno"); err != nil {
		t.Fatal(err)
	}
	beforeHost, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	owner := startBrainRouteOwner(t)
	bindCodexRoute(t, owner, oldID)
	routesPath := owner.RoutesFile().Path()

	var dirSyncCalls int
	owner.RoutesFile().SetDirSync(func(string) error {
		dirSyncCalls++
		if dirSyncCalls == 1 {
			return errors.New("injected first transfer dirSync failure")
		}
		return nil
	})
	fw := &fakeWatcher{}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(owner)

	_, err = service.EnsureHostSnapshot()
	if err == nil || !errors.Is(err, ErrRouteTransferNotDurable) {
		t.Fatalf("err=%v", err)
	}
	afterHost, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeHost, afterHost) {
		t.Fatalf("host binding mutated:\nbefore=%s\nafter=%s", beforeHost, afterHost)
	}
	raw, err := os.ReadFile(routesPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), oldID) {
		t.Fatalf("restart-visible routes must still name old Session %q: %s", oldID, raw)
	}
	if len(fw.created) != 1 || len(fw.killed) != 1 || fw.killed[0] != fw.created[0].id {
		t.Fatalf("created=%#v killed=%#v", fw.created, fw.killed)
	}
	if _, ok := owner.Table().Get(oldID); !ok {
		t.Fatal("memory route must be restored on oldID after durable compensation")
	}
	if _, ok := owner.Table().Get(fw.created[0].id); ok {
		t.Fatal("compensated route must not remain on killed spawn id")
	}
}

func TestRecoverNondurableTransferRealOwnerRestoresRestartVisibleRoute(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deadID := "brain-agent-brain-dead:@1"
	aliveID := "brain-agent-brain-alive:@2"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	if err := store.SetHostSession(deadID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, "/tmp/"+providerSessionID+".jsonl", "/home"); err != nil {
		t.Fatal(err)
	}
	beforeHost, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	owner := startBrainRouteOwner(t)
	bindCodexRoute(t, owner, deadID)
	routesPath := owner.RoutesFile().Path()

	var dirSyncCalls int
	owner.RoutesFile().SetDirSync(func(string) error {
		dirSyncCalls++
		if dirSyncCalls == 1 {
			return errors.New("injected first transfer dirSync failure")
		}
		return nil
	})
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			aliveID: {
				ID: aliveID, Name: "Brain (" + aliveID + ")", Cwd: store.WorkspacePath(),
				Command: "codex resume " + providerSessionID, State: classifier.StateRunning, Hidden: true,
			},
		},
	}
	fw.agents = append(fw.agents, fw.sessions[aliveID])
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(owner)

	_, err = service.EnsureHostSnapshot()
	if err == nil || !errors.Is(err, ErrRouteTransferNotDurable) {
		t.Fatalf("err=%v", err)
	}
	afterHost, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeHost, afterHost) {
		t.Fatalf("host binding mutated:\nbefore=%s\nafter=%s", beforeHost, afterHost)
	}
	raw, err := os.ReadFile(routesPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), deadID) {
		t.Fatalf("restart-visible routes must still name old Session %q: %s", deadID, raw)
	}
	if len(fw.killed) != 0 || !fw.HasSession(aliveID) {
		t.Fatalf("recovered live host must survive: killed=%#v", fw.killed)
	}
	if _, ok := owner.Table().Get(deadID); !ok {
		t.Fatal("memory route must be restored on dead/recorded id")
	}
	if _, ok := owner.Table().Get(aliveID); ok {
		t.Fatal("compensated route must not remain on recovered id after durable rollback")
	}
	audit, _ := os.ReadFile(store.HostReplacementsPath())
	if strings.Contains(string(audit), hostReplaceReasonRecoveredAlive) {
		t.Fatalf("must not audit recovered_alive: %s", audit)
	}
}
