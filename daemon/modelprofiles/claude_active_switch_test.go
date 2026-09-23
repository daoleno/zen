package modelprofiles

import (
	"errors"
	"testing"
)

func TestClaudeActiveSelectionCannotOutrunNativeCLI(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	first := claudeMessagesProfile("claude-first", "claude-sonnet-4-6", "claude-sonnet-4-6")
	second := claudeMessagesProfile("claude-second", "claude-sonnet-4-6", "claude-sonnet-4-6")
	second.BaseURL = "https://another.example"
	for _, profile := range []Profile{first, second} {
		if _, err := owner.UpsertProfile(profile, owner.Catalog().Revision, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := owner.SetProviderDefault(ClientClaude, first.ID, first.Model, owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorClaude, first.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "claude-session"); err != nil {
		t.Fatal(err)
	}
	before, _ := owner.Table().Get("claude-session")
	if caps := CapabilitiesFor(ExecutorClaude); len(caps.Protocols) != 1 || caps.Protocols[0].ActiveSwitch != "" {
		t.Fatalf("Claude must not advertise native hot switching: %#v", caps)
	}
	if caps := owner.SessionRouteCapabilities("claude-session"); !caps.Managed || caps.ActiveSwitch {
		t.Fatalf("managed Claude should be read-only: %#v", caps)
	}
	if runtime, ok := owner.ThreadRuntime("claude-session"); !ok || runtime.HotSwitchable {
		t.Fatalf("Claude runtime advertised an unapplied mutation: %#v", runtime)
	}
	if _, err := owner.PrepareThreadRuntime("claude-session", ThreadRuntimeChoice{
		ConnectionID: second.ID, ModelID: second.Model,
	}); !errors.Is(err, ErrBindingNotRouted) {
		t.Fatalf("prepared route-only Claude mutation: %v", err)
	}
	if _, _, _, err := owner.ActivateSession("claude-session", second.ID, before.Generation); !errors.Is(err, ErrBindingNotRouted) {
		t.Fatalf("activated route-only Claude mutation: %v", err)
	}
	if _, err := owner.SwitchProvider(ClientClaude, second.ID, owner.Catalog().Revision); !errors.Is(err, ErrBindingNotRouted) {
		t.Fatalf("retargeted live Claude session: %v", err)
	}
	if got := owner.Catalog().Defaults[ClientClaude]; got != first.ID {
		t.Fatalf("rejected switch changed default: %q", got)
	}
	after, _ := owner.Table().Get("claude-session")
	if after.Generation != before.Generation || after.Binding.ProfileID != first.ID {
		t.Fatalf("rejected mutation changed route: before=%#v after=%#v", before.Binding, after.Binding)
	}
	if projection, err := owner.SetProviderDefault(ClientClaude, second.ID, second.Model, owner.Catalog().Revision); err != nil ||
		projection.Defaults[ClientClaude].ConnectionID != second.ID {
		t.Fatalf("future-launch default must remain selectable: projection=%#v err=%v", projection.Defaults, err)
	}
}
