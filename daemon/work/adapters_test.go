package work

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/modelprofiles"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestTmuxRunnerRequiresOwnedWatcherLifecycle(t *testing.T) {
	runner := TmuxRunner{}
	if _, err := runner.Spawn("codex", "/repo", "codex"); err == nil || !strings.Contains(err.Error(), "delegated watcher is required") {
		t.Fatalf("Spawn error = %v", err)
	}
	if err := runner.Abort("main:@42"); err == nil || !strings.Contains(err.Error(), "delegated watcher is required") {
		t.Fatalf("Abort error = %v", err)
	}
}

type fakeDelegatedWatcher struct {
	created        []watcher.CreateSessionOptions
	killed         []string
	createErr      error
	killErr        error
	sessions       map[string]bool
	nextID         string
	sendReadyErr   error
	sendReadyCalls []string
}

func (f *fakeDelegatedWatcher) CreateSession(_ string, opts watcher.CreateSessionOptions) (string, error) {
	f.created = append(f.created, opts)
	if f.createErr != nil {
		return "", f.createErr
	}
	id := f.nextID
	if id == "" {
		id = "codex-scheduled:@1"
	}
	if f.sessions == nil {
		f.sessions = map[string]bool{}
	}
	f.sessions[id] = true
	return id, nil
}

func (f *fakeDelegatedWatcher) KillSession(sessionID string) error {
	f.killed = append(f.killed, sessionID)
	if f.sessions != nil {
		delete(f.sessions, sessionID)
	}
	return f.killErr
}

func (f *fakeDelegatedWatcher) ProbeSession(target string) (watcher.SessionPresence, error) {
	if f.sessions != nil && f.sessions[target] {
		return watcher.SessionPresencePresent, nil
	}
	return watcher.SessionPresenceAbsent, nil
}

func (f *fakeDelegatedWatcher) SendInputWhenReady(sessionID, command, text string) error {
	f.sendReadyCalls = append(f.sendReadyCalls, sessionID+"|"+command+"|"+text)
	return f.sendReadyErr
}

type fakeProfileOwner struct {
	prepareCalls     [][3]string
	preparePlan      modelprofiles.SessionLaunchPlan
	prepareErr       error
	commitCalls      [][2]string
	commitErr        error
	commitOK         bool
	commitNotDurable bool
	abortCalls       []string
	releases         []string
}

func (f *fakeProfileOwner) PrepareLaunch(executorID, profileID, baseCommand string) (modelprofiles.SessionLaunchPlan, error) {
	f.prepareCalls = append(f.prepareCalls, [3]string{executorID, profileID, baseCommand})
	if f.prepareErr != nil && !f.preparePlan.Persist.Applied && !f.preparePlan.Bypass {
		return modelprofiles.SessionLaunchPlan{}, f.prepareErr
	}
	return f.preparePlan, f.prepareErr
}

func (f *fakeProfileOwner) CommitLaunch(provisionalID, sessionID string) (modelprofiles.SessionRouteState, modelprofiles.WireSessionSnapshot, modelprofiles.PersistResult, error) {
	f.commitCalls = append(f.commitCalls, [2]string{provisionalID, sessionID})
	if !f.commitOK {
		return modelprofiles.SessionRouteState{}, modelprofiles.WireSessionSnapshot{}, modelprofiles.PersistResult{Applied: false, Durable: false}, f.commitErr
	}
	persist := modelprofiles.PersistResult{Applied: true, Durable: !f.commitNotDurable}
	return modelprofiles.SessionRouteState{}, modelprofiles.WireSessionSnapshot{}, persist, f.commitErr
}

func (f *fakeProfileOwner) AbortLaunch(provisionalID string) (modelprofiles.PersistResult, error) {
	f.abortCalls = append(f.abortCalls, provisionalID)
	return modelprofiles.PersistResult{Applied: true, Durable: true}, nil
}

func (f *fakeProfileOwner) ReleaseSession(sessionID string) (modelprofiles.PersistResult, error) {
	f.releases = append(f.releases, sessionID)
	return modelprofiles.PersistResult{Applied: true, Durable: true}, nil
}

func TestTmuxRunnerSpawnPrepareCommitExecutorDefault(t *testing.T) {
	fw := &fakeDelegatedWatcher{nextID: "codex-cal:@1"}
	profiles := &fakeProfileOwner{
		preparePlan: modelprofiles.SessionLaunchPlan{
			Applied:       true,
			Command:       "codex --compiled-cal",
			Env:           map[string]string{"OPENAI_BASE_URL": "http://127.0.0.1:9/v1"},
			ProvisionalID: "pending:cal",
			Persist:       modelprofiles.PersistResult{Applied: true, Durable: true},
		},
		commitOK: true,
	}
	runner := TmuxRunner{Watcher: fw, Env: map[string]string{"ZEN": "1"}, Profiles: profiles}
	id, err := runner.Spawn("codex", "/repo", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if id != "codex-cal:@1" {
		t.Fatalf("id=%q", id)
	}
	if len(profiles.prepareCalls) != 1 || profiles.prepareCalls[0][0] != "codex" || profiles.prepareCalls[0][1] != "" {
		t.Fatalf("prepareCalls=%#v", profiles.prepareCalls)
	}
	if len(profiles.commitCalls) != 1 || profiles.commitCalls[0][0] != "pending:cal" {
		t.Fatalf("commitCalls=%#v", profiles.commitCalls)
	}
	if len(fw.created) != 1 || fw.created[0].Command != "codex --compiled-cal" {
		t.Fatalf("created=%#v", fw.created)
	}
	if fw.created[0].Env["OPENAI_BASE_URL"] != "http://127.0.0.1:9/v1" || fw.created[0].Env["ZEN"] != "1" {
		t.Fatalf("env=%#v", fw.created[0].Env)
	}
}

func TestTmuxRunnerSpawnCreateFailureAbortsProvisional(t *testing.T) {
	fw := &fakeDelegatedWatcher{createErr: errors.New("tmux down")}
	profiles := &fakeProfileOwner{
		preparePlan: modelprofiles.SessionLaunchPlan{
			Applied:       true,
			Command:       "codex --compiled",
			ProvisionalID: "pending:abort",
			Persist:       modelprofiles.PersistResult{Applied: true, Durable: true},
		},
		commitOK: true,
	}
	runner := TmuxRunner{Watcher: fw, Profiles: profiles}
	if _, err := runner.Spawn("codex", "/repo", "codex"); err == nil {
		t.Fatal("expected create failure")
	}
	if len(profiles.abortCalls) != 1 || profiles.abortCalls[0] != "pending:abort" {
		t.Fatalf("abortCalls=%#v", profiles.abortCalls)
	}
	if len(profiles.commitCalls) != 0 {
		t.Fatalf("commitCalls=%#v", profiles.commitCalls)
	}
}

func TestTmuxRunnerAbortReleasesCommittedRoute(t *testing.T) {
	fw := &fakeDelegatedWatcher{sessions: map[string]bool{"codex-cal:@9": true}}
	profiles := &fakeProfileOwner{}
	runner := TmuxRunner{Watcher: fw, Profiles: profiles}
	if err := runner.Abort("codex-cal:@9"); err != nil {
		t.Fatal(err)
	}
	if len(fw.killed) != 1 || fw.killed[0] != "codex-cal:@9" {
		t.Fatalf("killed=%#v", fw.killed)
	}
	if len(profiles.releases) != 1 || profiles.releases[0] != "codex-cal:@9" {
		t.Fatalf("releases=%#v", profiles.releases)
	}
}

func TestTmuxRunnerRawCustomBypass(t *testing.T) {
	fw := &fakeDelegatedWatcher{nextID: "custom:@1"}
	profiles := &fakeProfileOwner{
		preparePlan: modelprofiles.SessionLaunchPlan{Bypass: true, Command: "my-custom-agent"},
	}
	runner := TmuxRunner{Watcher: fw, Profiles: profiles}
	id, err := runner.Spawn("custom", "/repo", "my-custom-agent")
	if err != nil {
		t.Fatal(err)
	}
	if id != "custom:@1" {
		t.Fatalf("id=%q", id)
	}
	if len(profiles.commitCalls) != 0 || len(profiles.abortCalls) != 0 {
		t.Fatalf("bypass must not commit/abort: %#v %#v", profiles.commitCalls, profiles.abortCalls)
	}
	if len(fw.created) != 1 || fw.created[0].Command != "my-custom-agent" {
		t.Fatalf("created=%#v", fw.created)
	}
}

func TestTmuxRunnerCommitPersistFailureCleansUp(t *testing.T) {
	fw := &fakeDelegatedWatcher{nextID: "codex-cal:@2"}
	profiles := &fakeProfileOwner{
		preparePlan: modelprofiles.SessionLaunchPlan{
			Applied:       true,
			Command:       "codex --compiled",
			ProvisionalID: "pending:fail",
			Persist:       modelprofiles.PersistResult{Applied: true, Durable: true},
		},
		commitErr: errors.New("persist failed"),
	}
	runner := TmuxRunner{Watcher: fw, Profiles: profiles}
	if _, err := runner.Spawn("codex", "/repo", "codex"); err == nil {
		t.Fatal("expected commit failure")
	}
	if len(fw.killed) != 1 {
		t.Fatalf("killed=%#v", fw.killed)
	}
	if len(profiles.abortCalls) != 1 || profiles.abortCalls[0] != "pending:fail" {
		t.Fatalf("abortCalls=%#v", profiles.abortCalls)
	}
}

func TestTmuxRunnerAliasUsesCanonicalClientHint(t *testing.T) {
	fw := &fakeDelegatedWatcher{nextID: "alias:@1"}
	profiles := &fakeProfileOwner{
		preparePlan: modelprofiles.SessionLaunchPlan{
			Applied: true, Command: "codex --compiled", ProvisionalID: "pending:alias",
			Persist: modelprofiles.PersistResult{Applied: true, Durable: true},
		},
		commitOK: true,
	}
	runner := TmuxRunner{Watcher: fw, Profiles: profiles}
	// Configured alias identity stays the session name; PrepareLaunch gets codex.
	if _, err := runner.Spawn("primary", "/repo", "codex --dangerously-bypass-approvals-and-sandbox"); err != nil {
		t.Fatal(err)
	}
	if len(profiles.prepareCalls) != 1 || profiles.prepareCalls[0][0] != "codex" {
		t.Fatalf("prepare hint=%#v, want canonical codex", profiles.prepareCalls)
	}
	if len(fw.created) != 1 || fw.created[0].Name != "primary" {
		t.Fatalf("CLI identity must remain alias name: %#v", fw.created)
	}

	profilesClaude := &fakeProfileOwner{
		preparePlan: modelprofiles.SessionLaunchPlan{
			Applied: true, Command: "claude --compiled", ProvisionalID: "pending:claude",
			Persist: modelprofiles.PersistResult{Applied: true, Durable: true},
		},
		commitOK: true,
	}
	fw2 := &fakeDelegatedWatcher{nextID: "claude-alias:@1"}
	runner2 := TmuxRunner{Watcher: fw2, Profiles: profilesClaude}
	if _, err := runner2.Spawn("work-claude", "/repo", "claude --permission-mode bypassPermissions"); err != nil {
		t.Fatal(err)
	}
	if profilesClaude.prepareCalls[0][0] != "claude" {
		t.Fatalf("claude hint=%#v", profilesClaude.prepareCalls)
	}
}

func TestTmuxRunnerPrepareNotDurableFailsClosedBeforeCreate(t *testing.T) {
	fw := &fakeDelegatedWatcher{nextID: "must-not-create"}
	profiles := &fakeProfileOwner{
		preparePlan: modelprofiles.SessionLaunchPlan{
			Applied:       true,
			Command:       "codex --compiled",
			ProvisionalID: "pending:undurable",
			Persist:       modelprofiles.PersistResult{Applied: true, Durable: false},
		},
		prepareErr: modelprofiles.ErrPersistDirSync,
	}
	runner := TmuxRunner{Watcher: fw, Profiles: profiles}
	_, err := runner.Spawn("codex", "/repo", "codex")
	if err == nil || !errors.Is(err, modelprofiles.ErrPersistDirSync) {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(err.Error(), "not durable") {
		t.Fatalf("err=%v", err)
	}
	if len(fw.created) != 0 {
		t.Fatalf("must not create session: %#v", fw.created)
	}
	if len(profiles.abortCalls) != 1 || profiles.abortCalls[0] != "pending:undurable" {
		t.Fatalf("abortCalls=%#v", profiles.abortCalls)
	}
	if len(profiles.commitCalls) != 0 {
		t.Fatalf("commitCalls=%#v", profiles.commitCalls)
	}
}

func TestTmuxRunnerCommitNotDurableFailsClosedWithTeardown(t *testing.T) {
	fw := &fakeDelegatedWatcher{nextID: "codex-cal:@undurable"}
	profiles := &fakeProfileOwner{
		preparePlan: modelprofiles.SessionLaunchPlan{
			Applied: true, Command: "codex --compiled", ProvisionalID: "pending:c",
			Persist: modelprofiles.PersistResult{Applied: true, Durable: true},
		},
		commitOK:         true,
		commitNotDurable: true,
		commitErr:        modelprofiles.ErrPersistDirSync,
	}
	runner := TmuxRunner{Watcher: fw, Profiles: profiles}
	id, err := runner.Spawn("codex", "/repo", "codex")
	if id != "" || err == nil {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if !errors.Is(err, modelprofiles.ErrPersistDirSync) || !strings.Contains(err.Error(), "not durable") {
		t.Fatalf("err=%v", err)
	}
	// Intentional fail-closed teardown inside Spawn — Launcher never sees a live ID.
	if len(fw.killed) != 1 || fw.killed[0] != "codex-cal:@undurable" {
		t.Fatalf("killed=%#v", fw.killed)
	}
	if len(profiles.releases) != 1 || profiles.releases[0] != "codex-cal:@undurable" {
		t.Fatalf("releases=%#v", profiles.releases)
	}

	// Launcher-compatible: Spawn error means StartDedicated does not Abort a
	// successful session ID (cleanup already happened).
	item, err := ParseFile("/tmp/scheduled.md", []byte(`---
id: scheduled
created: 2026-07-17T00:00:00Z
---
# Scheduled
`), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	fw2 := &fakeDelegatedWatcher{nextID: "codex-cal:@launcher"}
	profiles2 := &fakeProfileOwner{
		preparePlan: modelprofiles.SessionLaunchPlan{
			Applied: true, Command: "codex --compiled", ProvisionalID: "pending:l",
			Persist: modelprofiles.PersistResult{Applied: true, Durable: true},
		},
		commitOK:         true,
		commitNotDurable: true,
	}
	launcher := NewLauncher(TmuxRunner{Watcher: fw2, Profiles: profiles2}, NewExecutorConfig("codex", map[string]Executor{
		"codex": {Name: "codex", Command: "codex"},
	}))
	if _, err := launcher.StartDedicated(item, "/repo"); err == nil || !strings.Contains(err.Error(), "not durable") {
		t.Fatalf("launcher err=%v", err)
	}
	if len(fw2.killed) != 1 {
		t.Fatalf("launcher path must tear down inside Spawn, killed=%#v", fw2.killed)
	}
}
