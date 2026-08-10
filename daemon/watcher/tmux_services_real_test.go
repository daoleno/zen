package watcher

import (
	"path/filepath"
	"testing"
)

func TestRealTmuxServiceInventoryIncludesOnlyLocallyOwnedWindows(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	ambientTarget := createHarnessPane(t, h.selected, "ambient-service", "exec /bin/sh")
	if out, err := tmuxHarnessCommand(h.selected, "set-option", "-wg", "@zen_agent_created", "1").CombinedOutput(); err != nil {
		t.Fatalf("set global collision marker: %v: %s", err, out)
	}
	ownedTarget, err := h.w.CreateSession("", CreateSessionOptions{
		Name: "owned-service", Command: "exec /bin/sh", Detached: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	panes, err := h.w.listServicePanes()
	if err != nil {
		t.Fatal(err)
	}
	if !servicePanesContainTarget(panes, ownedTarget) {
		t.Fatalf("owned service pane missing: %#v", panes)
	}
	if servicePanesContainTarget(panes, ambientTarget) {
		t.Fatalf("ambient service pane inherited ownership: %#v", panes)
	}

	h.w.SetTmuxServer(filepath.Join(h.root, "missing.sock"), h.scratch)
	panes, err = h.w.listServicePanes()
	if err != nil {
		t.Fatalf("missing selected server: %v", err)
	}
	if len(panes) != 0 {
		t.Fatalf("missing selected server panes = %#v", panes)
	}
}

func servicePanesContainTarget(panes []servicePane, target string) bool {
	for _, pane := range panes {
		if pane.target == target {
			return true
		}
	}
	return false
}
