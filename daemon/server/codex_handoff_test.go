package server

import (
	"testing"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/modelprofiles"
)

// TestHandoffTargetForActivation gates the managed Codex handoff to live
// routed Codex Sessions only.
func TestHandoffTargetForActivation(t *testing.T) {
	var s *Server
	if s.handoffTargetForActivation("", nil) {
		t.Fatal("nil server must not hand off")
	}
	s = &Server{}
	if s.handoffTargetForActivation("", nil) {
		t.Fatal("empty input must not hand off")
	}
	live := &classifier.Agent{ProcessID: 42}
	dead := &classifier.Agent{}
	srv := &Server{getAgentOverride: func(string) *classifier.Agent { return live }}
	sel := &modelprofiles.WireBinding{Client: "codex", HotSwitchable: true}
	if !srv.handoffTargetForActivation("s", sel) {
		t.Fatal("live routed Codex Session must hand off")
	}
	srv.getAgentOverride = func(string) *classifier.Agent { return dead }
	if srv.handoffTargetForActivation("s", sel) {
		t.Fatal("dead process must not hand off")
	}
	srv.getAgentOverride = func(string) *classifier.Agent { return live }
	sel.Client = "claude"
	if srv.handoffTargetForActivation("s", sel) {
		t.Fatal("claude must not hand off")
	}
	sel.Client = "codex"
	sel.HotSwitchable = false
	if srv.handoffTargetForActivation("s", sel) {
		t.Fatal("non-switchable session must not hand off")
	}
}
