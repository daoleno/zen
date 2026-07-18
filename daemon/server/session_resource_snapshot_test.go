package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestBuildSessionResourceSnapshotWireKeysIdentityAndOmitsAbsent(t *testing.T) {
	started := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	memory := uint64(1500)
	peak := uint64(3400)
	tasks := 4
	poolCurrent := uint64(9000)
	poolHigh := uint64(25000000000)
	poolMax := uint64(28000000000)
	hostAvail := uint64(8 * 1024 * 1024 * 1024)

	srv := &Server{}
	payload := srv.buildSessionResourceSnapshotWire(
		"req-1",
		"main:@7",
		&classifier.Agent{
			ID:        "main:@7",
			Name:      "cursor-agent",
			Command:   "cursor-agent",
			State:     classifier.StateRunning,
			Phase:     "working",
			Cwd:       "/home/daoleno/workspace/zen",
			Delegated: true,
			StartedAt: started,
		},
		watcher.SessionResourceSnapshot{
			Backend:                "cgroup_pool",
			Managed:                true,
			MemoryCurrentBytes:     &memory,
			MemoryPeakBytes:        &peak,
			TasksCurrent:           &tasks,
			PoolMemoryCurrentBytes: &poolCurrent,
			PoolMemoryHighBytes:    &poolHigh,
			PoolMemoryMaxBytes:     &poolMax,
			HostAvailableBytes:     &hostAvail,
			HostPressure:           "ok",
		},
	)

	if payload.Type != "session_resource_snapshot" {
		t.Fatalf("type = %q", payload.Type)
	}
	if payload.RequestID != "req-1" || payload.AgentID != "main:@7" {
		t.Fatalf("identity keys = %#v", payload)
	}
	if payload.Session.Executor != "cursor" {
		t.Fatalf("executor = %q", payload.Session.Executor)
	}
	if payload.Session.Managed != true || payload.Session.Backend != "cgroup_pool" {
		t.Fatalf("session managed/backend = %#v", payload.Session)
	}
	if payload.Pool == nil || payload.Pool.MemoryHighBytes == nil || *payload.Pool.MemoryHighBytes != poolHigh {
		t.Fatalf("pool = %#v", payload.Pool)
	}
	if payload.Host == nil || payload.Host.Pressure != "ok" {
		t.Fatalf("host = %#v", payload.Host)
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := decoded["generation"]; ok {
		t.Fatalf("generation must not appear on the wire, got %v", decoded["generation"])
	}
	session := decoded["session"].(map[string]any)
	if _, ok := session["memory_current_bytes"]; !ok {
		t.Fatalf("expected memory_current_bytes in %v", session)
	}

	// Live-but-unmanaged Sessions still serialize; missing agents fail earlier.
	unmanaged := srv.buildSessionResourceSnapshotWire(
		"req-2",
		"main:@8",
		&classifier.Agent{ID: "main:@8", Name: "shell", State: classifier.StateRunning},
		watcher.SessionResourceSnapshot{},
	)
	unmanagedRaw, err := json.Marshal(unmanaged)
	if err != nil {
		t.Fatalf("marshal unmanaged: %v", err)
	}
	var unmanagedDecoded map[string]any
	if err := json.Unmarshal(unmanagedRaw, &unmanagedDecoded); err != nil {
		t.Fatalf("unmarshal unmanaged: %v", err)
	}
	if _, ok := unmanagedDecoded["pool"]; ok {
		t.Fatalf("empty pool should be omitted, got %v", unmanagedDecoded["pool"])
	}
	if _, ok := unmanagedDecoded["host"]; ok {
		t.Fatalf("empty host should be omitted, got %v", unmanagedDecoded["host"])
	}
	unmanagedSession := unmanagedDecoded["session"].(map[string]any)
	for _, key := range []string{"memory_current_bytes", "memory_peak_bytes", "tasks_current", "backend"} {
		if _, ok := unmanagedSession[key]; ok {
			t.Fatalf("absent field %q should be omitted from %#v", key, unmanagedSession)
		}
	}

	// Unmanaged wire must omit pool even when Watcher observed shared-pool fields.
	unmanagedObserved := srv.buildSessionResourceSnapshotWire(
		"req-3",
		"main:@9",
		&classifier.Agent{ID: "main:@9", Name: "shell", State: classifier.StateRunning},
		watcher.SessionResourceSnapshot{
			Backend:                "cgroup_pool",
			Managed:                false,
			MemoryCurrentBytes:     &memory,
			MemoryPeakBytes:        &peak,
			TasksCurrent:           &tasks,
			PoolMemoryCurrentBytes: &poolCurrent,
			PoolMemoryHighBytes:    &poolHigh,
			PoolMemoryMaxBytes:     &poolMax,
			HostAvailableBytes:     &hostAvail,
			HostPressure:           "ok",
		},
	)
	if unmanagedObserved.Session.Managed {
		t.Fatalf("unmanaged managed flag = %#v", unmanagedObserved.Session)
	}
	if unmanagedObserved.Pool != nil {
		t.Fatalf("unmanaged response must omit pool, got %#v", unmanagedObserved.Pool)
	}
	if unmanagedObserved.Host == nil || unmanagedObserved.Host.Pressure != "ok" {
		t.Fatalf("unmanaged host should remain, got %#v", unmanagedObserved.Host)
	}
	unmanagedObservedRaw, err := json.Marshal(unmanagedObserved)
	if err != nil {
		t.Fatalf("marshal unmanaged observed: %v", err)
	}
	var unmanagedObservedDecoded map[string]any
	if err := json.Unmarshal(unmanagedObservedRaw, &unmanagedObservedDecoded); err != nil {
		t.Fatalf("unmarshal unmanaged observed: %v", err)
	}
	if _, ok := unmanagedObservedDecoded["pool"]; ok {
		t.Fatalf("unmanaged pool must be omitted from JSON, got %v", unmanagedObservedDecoded["pool"])
	}
	if _, ok := unmanagedObservedDecoded["host"]; !ok {
		t.Fatalf("unmanaged host must remain in JSON, got %v", unmanagedObservedDecoded)
	}
	unmanagedObservedSession := unmanagedObservedDecoded["session"].(map[string]any)
	for _, key := range []string{"backend", "memory_current_bytes", "memory_peak_bytes", "tasks_current"} {
		if _, ok := unmanagedObservedSession[key]; ok {
			t.Fatalf("unmanaged Session resource field %q must be omitted from %#v", key, unmanagedObservedSession)
		}
	}
}

func TestResolveSessionResourceSnapshotFailsWhenAgentDisappears(t *testing.T) {
	w := watcher.New(time.Second)
	srv := &Server{watcher: w}

	_, _, err := srv.resolveSessionResourceSnapshot("main:@missing")
	if err == nil || !strings.Contains(err.Error(), "agent session not found") {
		t.Fatalf("missing agent error = %v", err)
	}

	_, _, err = (&Server{}).resolveSessionResourceSnapshot("main:@1")
	if err == nil || !strings.Contains(err.Error(), "agent session not found") {
		t.Fatalf("nil watcher error = %v", err)
	}
}
