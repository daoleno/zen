package codexctl

import (
	"context"
	"testing"
	"time"
)

func startMonitor(t *testing.T, f *fakeAppServer) *Monitor {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, err := OpenMonitor(ctx, f.socketPath, DialOptions{ResolveRetryWindow: 200 * time.Millisecond})
	if err != nil {
		t.Fatalf("OpenMonitor: %v", err)
	}
	t.Cleanup(m.Close)
	return m
}

func waitMonitorSettings(t *testing.T, m *Monitor, wantModel, wantEffort string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		settings, ok := m.Settings()
		if ok && settings.Matches(wantModel, wantEffort) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	settings, ok := m.Settings()
	t.Fatalf("monitor never saw %s/%s: ok=%v settings=%#v", wantModel, wantEffort, ok, settings)
}

func TestMonitorBootstrapsSnapshotFromResumeResponse(t *testing.T) {
	f := startFakeAppServer(t)
	f.loaded = []string{"t-main"}
	f.threads = []ThreadInfo{{ID: "t-main", Cwd: "/repo/zen", Status: "idle"}}
	// The fake's thread/resume handler replies with a generic thread object;
	// extend it to carry the current model + reasoning effort like the real
	// ThreadResumeResponse.
	f.resumeModel = "gpt-5.5"
	f.resumeEffort = "high"
	m := startMonitor(t, f)
	waitMonitorSettings(t, m, "gpt-5.5", "high")
	if settings, _ := m.Settings(); settings.ThreadID != "t-main" {
		t.Fatalf("thread id = %q", settings.ThreadID)
	}
}

func TestMonitorTracksAppliedSettingsNotifications(t *testing.T) {
	f := startFakeAppServer(t)
	f.loaded = []string{"t-main"}
	f.threads = []ThreadInfo{{ID: "t-main", Cwd: "/repo/zen", Status: "idle"}}
	f.resumeModel = "gpt-5.5"
	f.resumeEffort = ""
	m := startMonitor(t, f)
	waitMonitorSettings(t, m, "gpt-5.5", "")

	f.broadcast(notifThreadSettingsUpd, map[string]any{
		"threadId": "t-main",
		"threadSettings": map[string]any{
			"model":  "gpt-5.5",
			"effort": "low",
		},
	})
	waitMonitorSettings(t, m, "gpt-5.5", "low")

	// Native default arrives as "none" and normalizes to Zen's "".
	f.broadcast(notifThreadSettingsUpd, map[string]any{
		"threadId": "t-main",
		"threadSettings": map[string]any{
			"model":  "gpt-5.5",
			"effort": "none",
		},
	})
	waitMonitorSettings(t, m, "gpt-5.5", "")
}

func TestMonitorUnavailableUntilThreadExists(t *testing.T) {
	f := startFakeAppServer(t)
	// No loaded threads yet: the monitor must stay unavailable, then recover
	// once the thread appears.
	f.loaded = nil
	f.threads = nil
	m := startMonitor(t, f)
	if _, ok := m.Settings(); ok {
		t.Fatal("snapshot must be unavailable before the thread exists")
	}
	if !m.Alive() {
		t.Fatal("monitor must stay alive while the thread is pending")
	}
	f.loaded = []string{"t-main"}
	f.threads = []ThreadInfo{{ID: "t-main", Cwd: "/repo/zen", Status: "idle"}}
	f.resumeModel = "gpt-5"
	f.resumeEffort = ""
	waitMonitorSettings(t, m, "gpt-5", "")
}

func TestMonitorFailsClosedWhenConnectionDies(t *testing.T) {
	f := startFakeAppServer(t)
	f.loaded = []string{"t-main"}
	f.threads = []ThreadInfo{{ID: "t-main", Cwd: "/repo/zen", Status: "idle"}}
	f.resumeModel = "gpt-5.5"
	f.resumeEffort = "high"
	m := startMonitor(t, f)
	waitMonitorSettings(t, m, "gpt-5.5", "high")
	f.closeClients()
	deadline := time.Now().Add(5 * time.Second)
	for m.Alive() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if m.Alive() {
		t.Fatal("monitor must mark dead after the connection drops")
	}
	if _, ok := m.Settings(); ok {
		t.Fatal("dead monitor must not report settings")
	}
}

func TestNativeSettingsMatchesNormalizesDefault(t *testing.T) {
	if !(NativeSettings{Model: "gpt-5", Effort: ""}.Matches("gpt-5", "")) {
		t.Fatal("empty effort must match empty")
	}
	if !(NativeSettings{Model: "gpt-5", Effort: ""}.Matches("gpt-5", "none")) {
		t.Fatal("'none' must normalize to the empty default")
	}
	if (NativeSettings{Model: "gpt-5", Effort: "high"}).Matches("gpt-5", "") {
		t.Fatal("explicit high must not match default")
	}
}
