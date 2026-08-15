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

func TestInitialBrainHostPrepareCommitUsesExecutorDefault(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	routes := &fakeRouteLifecycle{
		preparePlan: &modelprofiles.SessionLaunchPlan{
			Applied:       true,
			Command:       "codex --profile-compiled",
			Env:           map[string]string{"OPENAI_BASE_URL": "http://127.0.0.1:9/v1"},
			ProvisionalID: "pending:host-1",
			Persist:       modelprofiles.PersistResult{Applied: true, Durable: true},
		},
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	if _, err := service.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if len(routes.prepareCalls) != 1 || routes.prepareCalls[0][0] != "codex" || routes.prepareCalls[0][1] != "" {
		t.Fatalf("prepareCalls=%#v", routes.prepareCalls)
	}
	if len(routes.resumeCalls) != 0 {
		t.Fatalf("resume must not run on initial host: %#v", routes.resumeCalls)
	}
	if len(routes.commitCalls) != 1 || routes.commitCalls[0][0] != "pending:host-1" {
		t.Fatalf("commitCalls=%#v", routes.commitCalls)
	}
	if len(fw.created) != 1 || fw.created[0].opts.Command != "codex --profile-compiled" {
		t.Fatalf("created=%#v", fw.created)
	}
	if fw.created[0].opts.Env["OPENAI_BASE_URL"] != "http://127.0.0.1:9/v1" {
		t.Fatalf("env=%#v", fw.created[0].opts.Env)
	}
	host, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if host.ID == "" || host.ExecutorID != "codex" {
		t.Fatalf("host=%+v", host)
	}
}

func TestNewChatPrepareCommitFreshHost(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-old:@1"
	if err := store.SetHostSession(oldID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			oldID: {ID: oldID, Name: "Brain", Command: "codex", State: classifier.StateRunning, Hidden: true},
		},
	}
	routes := &fakeRouteLifecycle{
		preparePlan: &modelprofiles.SessionLaunchPlan{
			Applied:       true,
			Command:       "codex --newchat",
			ProvisionalID: "pending:newchat",
			Persist:       modelprofiles.PersistResult{Applied: true, Durable: true},
		},
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	if _, err := service.NewChat(); err != nil {
		t.Fatal(err)
	}
	if len(routes.releases) != 1 || routes.releases[0] != oldID {
		t.Fatalf("releases=%#v", routes.releases)
	}
	if len(routes.prepareCalls) != 1 || len(routes.commitCalls) != 1 {
		t.Fatalf("prepare=%#v commit=%#v", routes.prepareCalls, routes.commitCalls)
	}
	if len(routes.resumeCalls) != 0 {
		t.Fatalf("resumeCalls=%#v", routes.resumeCalls)
	}
	if len(fw.created) != 1 || fw.created[0].opts.Command != "codex --newchat" {
		t.Fatalf("created=%#v", fw.created)
	}
}

func TestExecutorMismatchReplacementPrepareCommit(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-grok:@1"
	if err := store.SetHostSession(oldID, "grok"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			oldID: {ID: oldID, Name: "Brain", Command: "grok", State: classifier.StateRunning, Hidden: true},
		},
	}
	routes := &fakeRouteLifecycle{
		preparePlan: &modelprofiles.SessionLaunchPlan{
			Applied:       true,
			Command:       "codex --mismatch",
			ProvisionalID: "pending:mismatch",
			Persist:       modelprofiles.PersistResult{Applied: true, Durable: true},
		},
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{
			"grok":  {Name: "grok", Command: "grok", Kind: "grok"},
			"codex": {Name: "codex", Command: "codex", Kind: "codex"},
		},
	})
	service.SetSessionRouteLifecycle(routes)
	t.Setenv("ZEN_BRAIN_HOST_EXECUTOR", "codex")

	if _, err := service.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if len(fw.killed) != 1 || fw.killed[0] != oldID {
		t.Fatalf("killed=%#v", fw.killed)
	}
	if len(routes.prepareCalls) != 1 || routes.prepareCalls[0][0] != "codex" {
		t.Fatalf("prepareCalls=%#v", routes.prepareCalls)
	}
	if len(routes.resumeCalls) != 0 {
		t.Fatalf("resumeCalls=%#v", routes.resumeCalls)
	}
	if len(routes.commitCalls) != 1 {
		t.Fatalf("commitCalls=%#v", routes.commitCalls)
	}
	audit, err := os.ReadFile(store.HostReplacementsPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(audit), hostReplaceReasonProviderMismatch) {
		t.Fatalf("audit=%s", audit)
	}
}

func TestMissingTmuxResumeUsesImmutableBindingNotDefaultPrepare(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-missing:@9"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	transcriptPath := filepath.Join(t.TempDir(), "rollout-"+providerSessionID+".jsonl")
	if err := store.SetHostSession(oldID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, transcriptPath, "/home/daoleno"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	routes := &fakeRouteLifecycle{
		resumeFound: true,
		resumeCmd:   "codex resume " + providerSessionID + " --immutable-route",
		resumeEnv:   map[string]string{"OPENAI_API_KEY": "zen-loopback-placeholder-not-a-secret"},
		preparePlan: &modelprofiles.SessionLaunchPlan{
			Applied:       true,
			Command:       "codex --later-default",
			ProvisionalID: "pending:must-not-use",
			Persist:       modelprofiles.PersistResult{Applied: true, Durable: true},
		},
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	if _, err := service.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if len(routes.prepareCalls) != 0 {
		t.Fatalf("missing-tmux resume must not PrepareLaunch default: %#v", routes.prepareCalls)
	}
	if len(routes.resumeCalls) != 1 || routes.resumeCalls[0] != oldID {
		t.Fatalf("resumeCalls=%#v", routes.resumeCalls)
	}
	if len(routes.commitCalls) != 0 {
		t.Fatalf("commitCalls=%#v", routes.commitCalls)
	}
	if len(routes.transfers) != 1 || routes.transfers[0][0] != oldID {
		t.Fatalf("transfers=%#v", routes.transfers)
	}
	if len(fw.created) != 1 || fw.created[0].opts.Command != routes.resumeCmd {
		t.Fatalf("created=%#v", fw.created)
	}
	if fw.created[0].opts.Env["OPENAI_API_KEY"] != "zen-loopback-placeholder-not-a-secret" {
		t.Fatalf("env=%#v", fw.created[0].opts.Env)
	}
}

// TestMissingTmuxResumeWithoutBindingPreparesDefault covers the dropped-route
// case (for example a route released before the daemon upgrade): the thread's
// binding no longer exists, so the resume falls through to PrepareLaunch with
// the current executor default and gets a fresh route — never a dead one.
func TestMissingTmuxResumeWithoutBindingPreparesDefault(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-missing-unbound:@9"
	providerSessionID := "019fd717-589c-7a11-9966-917f43dc336a"
	transcriptPath := filepath.Join(t.TempDir(), "rollout-"+providerSessionID+".jsonl")
	if err := store.SetHostSession(oldID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(providerSessionID, transcriptPath, "/home/daoleno"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	routes := &fakeRouteLifecycle{
		resumeFound: false, // binding dropped: ResumeLaunch reports not found
		preparePlan: &modelprofiles.SessionLaunchPlan{
			Applied:       true,
			Command:       "codex resume " + providerSessionID + " --fresh-route",
			Env:           map[string]string{"OPENAI_BASE_URL": "http://127.0.0.1:9/r/rt_fresh/v1"},
			ProvisionalID: "pending:resume-fresh",
			Persist:       modelprofiles.PersistResult{Applied: true, Durable: true},
		},
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	if _, err := service.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if len(routes.resumeCalls) != 1 || routes.resumeCalls[0] != oldID {
		t.Fatalf("resumeCalls=%#v", routes.resumeCalls)
	}
	if len(routes.prepareCalls) != 1 || routes.prepareCalls[0][0] != "codex" || routes.prepareCalls[0][1] != "" {
		t.Fatalf("dropped-binding resume must PrepareLaunch the default: %#v", routes.prepareCalls)
	}
	if len(routes.commitCalls) != 1 || routes.commitCalls[0][0] != "pending:resume-fresh" {
		t.Fatalf("commitCalls=%#v", routes.commitCalls)
	}
	if len(fw.created) != 1 || fw.created[0].opts.Command != routes.preparePlan.Command {
		t.Fatalf("created=%#v", fw.created)
	}
	if fw.created[0].opts.Env["OPENAI_BASE_URL"] != "http://127.0.0.1:9/r/rt_fresh/v1" {
		t.Fatalf("env=%#v", fw.created[0].opts.Env)
	}
}

func TestBrainHostCreateFailureAbortsProvisional(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{createErr: errors.New("tmux down")}
	routes := &fakeRouteLifecycle{
		preparePlan: &modelprofiles.SessionLaunchPlan{
			Applied:       true,
			Command:       "codex --compiled",
			ProvisionalID: "pending:abort-me",
			Persist:       modelprofiles.PersistResult{Applied: true, Durable: true},
		},
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	if _, err := service.Snapshot(); err == nil {
		t.Fatal("expected create failure")
	}
	if len(routes.abortCalls) != 1 || routes.abortCalls[0] != "pending:abort-me" {
		t.Fatalf("abortCalls=%#v", routes.abortCalls)
	}
	if len(routes.commitCalls) != 0 {
		t.Fatalf("commitCalls=%#v", routes.commitCalls)
	}
}

func TestBrainHostCommitPersistFailureCleansUp(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	routes := &fakeRouteLifecycle{
		preparePlan: &modelprofiles.SessionLaunchPlan{
			Applied:       true,
			Command:       "codex --compiled",
			ProvisionalID: "pending:commit-fail",
			Persist:       modelprofiles.PersistResult{Applied: true, Durable: true},
		},
		commitErr: errors.New("route persist failed"),
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	if _, err := service.Snapshot(); err == nil {
		t.Fatal("expected commit failure")
	}
	if len(routes.abortCalls) != 1 || routes.abortCalls[0] != "pending:commit-fail" {
		t.Fatalf("abortCalls=%#v", routes.abortCalls)
	}
	if len(fw.killed) != 1 {
		t.Fatalf("killed=%#v", fw.killed)
	}
	if len(routes.releases) != 1 || routes.releases[0] != fw.killed[0] {
		t.Fatalf("releases=%#v killed=%#v", routes.releases, fw.killed)
	}
}

func TestBrainHostAliasPrepareUsesCanonicalClientHint(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	routes := &fakeRouteLifecycle{
		preparePlan: &modelprofiles.SessionLaunchPlan{
			Applied:       true,
			Command:       "codex --compiled",
			ProvisionalID: "pending:alias",
			Persist:       modelprofiles.PersistResult{Applied: true, Durable: true},
		},
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{
			"primary": {Name: "primary", Command: "codex", Kind: "codex"},
		},
	})
	service.SetSessionRouteLifecycle(routes)
	t.Setenv("ZEN_BRAIN_HOST_EXECUTOR", "primary")

	if _, err := service.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if len(routes.prepareCalls) != 1 || routes.prepareCalls[0][0] != "codex" {
		t.Fatalf("prepareCalls=%#v, want canonical codex hint", routes.prepareCalls)
	}
}

func TestBrainHostClaudeAliasPrepareUsesCanonicalClientHint(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	routes := &fakeRouteLifecycle{
		preparePlan: &modelprofiles.SessionLaunchPlan{
			Applied: true, Command: "claude --compiled", ProvisionalID: "pending:claude",
			Persist: modelprofiles.PersistResult{Applied: true, Durable: true},
		},
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{
			"desk-claude": {Name: "desk-claude", Command: "claude", Kind: "claude"},
		},
	})
	service.SetSessionRouteLifecycle(routes)
	t.Setenv("ZEN_BRAIN_HOST_EXECUTOR", "desk-claude")

	if _, err := service.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if routes.prepareCalls[0][0] != "claude" {
		t.Fatalf("prepareCalls=%#v", routes.prepareCalls)
	}
}

func TestBrainHostRawCustomBypassSkipsPrepareBinding(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	routes := &fakeRouteLifecycle{} // default PrepareLaunch bypass
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"custom": {Name: "custom", Command: "my-custom-agent", Kind: "custom"}},
	})
	service.SetSessionRouteLifecycle(routes)
	t.Setenv("ZEN_BRAIN_HOST_EXECUTOR", "custom")

	if _, err := service.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if len(routes.prepareCalls) != 1 {
		t.Fatalf("prepareCalls=%#v", routes.prepareCalls)
	}
	if len(routes.commitCalls) != 0 || len(routes.abortCalls) != 0 {
		t.Fatalf("bypass must not commit/abort: commit=%#v abort=%#v", routes.commitCalls, routes.abortCalls)
	}
	if len(fw.created) != 1 || fw.created[0].opts.Command != "my-custom-agent" {
		t.Fatalf("created=%#v", fw.created)
	}
}

// Prepare Applied+!Durable may proceed because Commit is the durability barrier
// for the exact final Session-owned route. A later durable Commit succeeds.
func TestPrepareNondurableThenDurableCommitIsDurabilityBarrier(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	routes := &fakeRouteLifecycle{
		preparePlan: &modelprofiles.SessionLaunchPlan{
			Applied:       true,
			Command:       "codex --prepared-uncertain",
			ProvisionalID: "pending:prep-nd",
			Persist:       modelprofiles.PersistResult{Applied: true, Durable: false},
		},
		prepareErr:    modelprofiles.ErrPersistDirSync,
		commitPersist: modelprofiles.PersistResult{Applied: true, Durable: true},
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	if _, err := service.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if len(routes.abortCalls) != 0 {
		t.Fatalf("prepare nondurable must not abort before create when Commit is barrier: %#v", routes.abortCalls)
	}
	if len(routes.commitCalls) != 1 {
		t.Fatalf("commitCalls=%#v", routes.commitCalls)
	}
	host, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if host.ID == "" || host.ExecutorID != "codex" {
		t.Fatalf("durable commit must bind host: %+v", host)
	}
}

func TestInitialCommitNondurableFailsClosedNoHostBind(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.HostSessionPath())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	routes := &fakeRouteLifecycle{
		preparePlan: &modelprofiles.SessionLaunchPlan{
			Applied:       true,
			Command:       "codex --compiled",
			ProvisionalID: "pending:commit-nd",
			Persist:       modelprofiles.PersistResult{Applied: true, Durable: true},
		},
		commitPersist: modelprofiles.PersistResult{Applied: true, Durable: false},
		commitErr:     modelprofiles.ErrPersistDirSync,
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	_, err = service.Snapshot()
	if err == nil || !errors.Is(err, modelprofiles.ErrPersistDirSync) || !strings.Contains(err.Error(), "not durable") {
		t.Fatalf("err=%v", err)
	}
	if len(fw.created) != 1 {
		t.Fatalf("created=%#v", fw.created)
	}
	agentID := fw.created[0].id
	if len(fw.killed) != 1 || fw.killed[0] != agentID {
		t.Fatalf("killed=%#v want %s", fw.killed, agentID)
	}
	if len(routes.releases) != 1 || routes.releases[0] != agentID {
		t.Fatalf("releases=%#v", routes.releases)
	}
	if len(routes.abortCalls) != 0 {
		t.Fatalf("committed route must Release not Abort provisional: %#v", routes.abortCalls)
	}
	after, err := os.ReadFile(store.HostSessionPath())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("host binding must not change:\nbefore=%s\nafter=%s", before, after)
	}
	audit, _ := os.ReadFile(store.HostReplacementsPath())
	if strings.Contains(string(audit), "_created") || strings.Contains(string(audit), hostReplaceReasonRecoveredAlive) {
		t.Fatalf("must not audit successful replacement: %s", audit)
	}
}

func TestNewChatCommitNondurableFailsClosedNoHostBind(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-old:@1"
	if err := store.SetHostSession(oldID, "codex"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.HostSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			oldID: {ID: oldID, Name: "Brain", Command: "codex", State: classifier.StateRunning, Hidden: true},
		},
	}
	routes := &fakeRouteLifecycle{
		preparePlan: &modelprofiles.SessionLaunchPlan{
			Applied:       true,
			Command:       "codex --newchat-nd",
			ProvisionalID: "pending:newchat-nd",
			Persist:       modelprofiles.PersistResult{Applied: true, Durable: true},
		},
		commitPersist: modelprofiles.PersistResult{Applied: true, Durable: false},
		commitErr:     modelprofiles.ErrPersistDirSync,
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	_, err = service.NewChat()
	if err == nil || !strings.Contains(err.Error(), "not durable") {
		t.Fatalf("err=%v", err)
	}
	if len(fw.created) != 1 {
		t.Fatalf("created=%#v", fw.created)
	}
	newID := fw.created[0].id
	if len(fw.killed) < 1 || fw.killed[len(fw.killed)-1] != newID {
		t.Fatalf("must kill committed nondurable host last: killed=%#v new=%s", fw.killed, newID)
	}
	if len(routes.releases) < 1 || routes.releases[len(routes.releases)-1] != newID {
		t.Fatalf("releases=%#v", routes.releases)
	}
	// NewChat clears/tears down old host before spawn; nondurable commit must
	// not bind the new Session. After teardown, host may be empty — never the new ID.
	host, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if host.ID == newID {
		t.Fatalf("must not bind nondurable host: %+v (before=%s)", host, before)
	}
	audit, _ := os.ReadFile(store.HostReplacementsPath())
	if strings.Contains(string(audit), "_created") {
		t.Fatalf("must not audit successful replacement: %s", audit)
	}
}

func TestAliasCommitNondurableFailsClosedNoHostBind(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	routes := &fakeRouteLifecycle{
		preparePlan: &modelprofiles.SessionLaunchPlan{
			Applied:       true,
			Command:       "codex --alias-nd",
			ProvisionalID: "pending:alias-nd",
			Persist:       modelprofiles.PersistResult{Applied: true, Durable: true},
		},
		commitPersist: modelprofiles.PersistResult{Applied: true, Durable: false},
		commitErr:     modelprofiles.ErrPersistDirSync,
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{
			"primary": {Name: "primary", Command: "codex", Kind: "codex"},
		},
	})
	service.SetSessionRouteLifecycle(routes)
	t.Setenv("ZEN_BRAIN_HOST_EXECUTOR", "primary")

	_, err = service.Snapshot()
	if err == nil || !strings.Contains(err.Error(), "not durable") {
		t.Fatalf("err=%v", err)
	}
	if len(routes.prepareCalls) != 1 || routes.prepareCalls[0][0] != "codex" {
		t.Fatalf("prepareCalls=%#v", routes.prepareCalls)
	}
	host, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if host.ID != "" {
		t.Fatalf("must not bind host after nondurable commit: %+v", host)
	}
	if len(fw.killed) != 1 || len(routes.releases) != 1 {
		t.Fatalf("killed=%#v releases=%#v", fw.killed, routes.releases)
	}
}

func TestCommitNondurableCleanupKillFailurePreservesRouteNoHostBind(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		killErr:        errors.New("injected kill failure"),
		killLeavesLive: true,
	}
	routes := &fakeRouteLifecycle{
		preparePlan: &modelprofiles.SessionLaunchPlan{
			Applied:       true,
			Command:       "codex --compiled",
			ProvisionalID: "pending:kill-fail",
			Persist:       modelprofiles.PersistResult{Applied: true, Durable: true},
		},
		commitPersist: modelprofiles.PersistResult{Applied: true, Durable: false},
		commitErr:     modelprofiles.ErrPersistDirSync,
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}},
	})
	service.SetSessionRouteLifecycle(routes)

	_, err = service.Snapshot()
	if err == nil || !strings.Contains(err.Error(), "not durable") {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(err.Error(), "injected kill failure") {
		t.Fatalf("must surface kill cleanup error: %v", err)
	}
	if len(fw.killed) != 1 {
		t.Fatalf("killed=%#v", fw.killed)
	}
	if len(routes.releases) != 0 {
		t.Fatalf("kill failure must preserve committed route: releases=%#v", routes.releases)
	}
	host, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if host.ID != "" {
		t.Fatalf("must not bind host: %+v", host)
	}
	audit, _ := os.ReadFile(store.HostReplacementsPath())
	if strings.Contains(string(audit), "_created") {
		t.Fatalf("must not audit success: %s", audit)
	}
}
