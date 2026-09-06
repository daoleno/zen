package modelprofiles

import (
	"errors"
	"testing"
)

func TestStartupReclaimsOnlyConfirmedAbsentRoutes(t *testing.T) {
	profiles, routes, listener := stage2bRoot(t)
	cfg := OwnerConfig{ProfilesPath: profiles, RoutesPath: routes, ListenerPath: listener,
		Lookup: readyLookup("test-secret"), Verifier: lifecycleTestVerifier{}}
	owner, err := StartOwner(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if _, err := owner.UpsertProfile(codexResponsesProfile("a", "gpt-5", "up-a"), 0, true); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"present:@1", "absent:@2", "unknown:@3", "error:@4"} {
		plan, err := owner.PrepareLaunch(ExecutorCodex, "a", "codex")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.SessionProbe = func(id string) (SessionLiveness, error) {
		switch id {
		case "present:@1":
			return SessionLivenessPresent, nil
		case "absent:@2":
			return SessionLivenessAbsent, nil
		case "error:@4":
			return SessionLivenessAbsent, errors.New("probe failed")
		default:
			return SessionLivenessUnknown, nil
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		reopened, err := StartOwner(cfg)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"present:@1", "absent:@2", "unknown:@3", "error:@4"} {
			_, found := reopened.SessionSnapshot(id)
			if found != (id != "absent:@2") {
				t.Errorf("restart %d: %s found=%t", attempt, id, found)
			}
		}
		reopened.restoreNotices = []RestoreContractNotice{{SessionID: "absent:@2"}, {SessionID: "present:@1"}}
		notices := reopened.RestoreContractNotices()
		if len(notices) != 1 || notices[0].SessionID != "present:@1" {
			t.Errorf("notices must describe only retained routes: %+v", notices)
		}
		if err := reopened.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
