package modelprofiles

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestProviderSwitchJournalRecoversWholeSnapshots(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		journalState    string
		catalogSnapshot string
		routeSnapshot   string
		wantSnapshot    string
	}{
		{
			name:            "prepared_rolls_back_mixed_files",
			journalState:    providerSwitchJournalStatePrepared,
			catalogSnapshot: "after",
			routeSnapshot:   "before",
			wantSnapshot:    "before",
		},
		{
			name:            "committed_rolls_forward_mixed_files",
			journalState:    providerSwitchJournalStateCommitted,
			catalogSnapshot: "before",
			routeSnapshot:   "after",
			wantSnapshot:    "after",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			owner, before, after := buildProviderSwitchSnapshots(t, root)

			catalogSnapshot := before
			if testCase.catalogSnapshot == "after" {
				catalogSnapshot = after
			}
			routeSnapshot := before
			if testCase.routeSnapshot == "after" {
				routeSnapshot = after
			}
			persistProviderCatalogSnapshot(t, owner, catalogSnapshot)
			if err := owner.routes.SaveStates(routeSnapshot.Routes); err != nil {
				t.Fatal(err)
			}
			if err := owner.writeProviderSwitchJournal(providerSwitchJournal{
				SchemaVersion: providerSwitchJournalSchemaVersion,
				State:         testCase.journalState,
				Client:        ClientCodex,
				Before:        before,
				After:         after,
			}); err != nil {
				t.Fatal(err)
			}
			if err := owner.Close(); err != nil {
				t.Fatal(err)
			}

			credentials, err := NewFileCredentialStore(filepath.Join(root, "credentials.json"))
			if err != nil {
				t.Fatal(err)
			}
			restored, err := StartOwner(OwnerConfig{
				ProfilesPath:  filepath.Join(root, "model-profiles.toml"),
				RoutesPath:    filepath.Join(root, "route-bindings.json"),
				ListenerPath:  filepath.Join(root, "route-listener.json"),
				DiscoveryPath: filepath.Join(root, "provider-discovery.json"),
				Credentials:   credentials,
				Lookup:        func(string) (string, bool) { return "", false },
				Verifier:      BuiltinEnvelopeVerifier{},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = restored.Close() })
			want := before
			if testCase.wantSnapshot == "after" {
				want = after
			}
			assertProviderSwitchSnapshot(t, captureProviderSwitchSnapshot(restored), want)
			if _, err := os.Stat(filepath.Join(root, providerSwitchJournalFileName)); !os.IsNotExist(err) {
				t.Fatalf("fully recovered journal was not removed: %v", err)
			}
		})
	}
}

func TestProviderSwitchJournalGateCannotClobberLaterMutation(t *testing.T) {
	root := t.TempDir()
	owner, before, after := buildProviderSwitchSnapshots(t, root)
	if err := owner.writeProviderSwitchJournal(providerSwitchJournal{
		SchemaVersion: providerSwitchJournalSchemaVersion,
		State:         providerSwitchJournalStateCommitted,
		Client:        ClientCodex,
		Before:        before,
		After:         after,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.SetThreadRuntime("journal-thread-1", ThreadRuntimeChoice{
		ConnectionID: after.Routes[0].Binding.ProfileID,
		ModelID:      "gpt-5.4",
		Effect:       ReasoningEffortLow,
	}); err != nil {
		t.Fatal(err)
	}
	newer := captureProviderSwitchSnapshot(owner)
	if err := owner.writeProviderSwitchJournal(providerSwitchJournal{
		SchemaVersion: providerSwitchJournalSchemaVersion,
		State:         providerSwitchJournalStateCleared,
		Client:        ClientCodex,
		Before:        before,
		After:         after,
	}); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}

	credentials, err := NewFileCredentialStore(filepath.Join(root, "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	restored, err := StartOwner(OwnerConfig{
		ProfilesPath:  filepath.Join(root, "model-profiles.toml"),
		RoutesPath:    filepath.Join(root, "route-bindings.json"),
		ListenerPath:  filepath.Join(root, "route-listener.json"),
		DiscoveryPath: filepath.Join(root, "provider-discovery.json"),
		Credentials:   credentials,
		Lookup:        func(string) (string, bool) { return "", false },
		Verifier:      BuiltinEnvelopeVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restored.Close() })
	assertProviderSwitchSnapshot(t, captureProviderSwitchSnapshot(restored), newer)
}

func buildProviderSwitchSnapshots(t *testing.T, root string) (*Owner, providerSwitchSnapshot, providerSwitchSnapshot) {
	t.Helper()
	owner := startSettingsSwitchOwner(t, root)
	connectionAProjection, err := owner.UpsertProviderConnection(
		e2eCustomInput("", "Alpha", "https://alpha.example/v1", "gpt-5.4"),
		"key-a",
		0,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	connectionA := connectionAProjection.Connections[0]
	connectionBProjection, err := owner.UpsertProviderConnection(
		e2eCustomInput("", "Beta", "https://beta.example/v1", "gpt-5.4"),
		"key-b",
		connectionAProjection.Revision,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	connectionB := connectionByName(t, connectionBProjection, "Beta")
	seedModelCatalogs(t, owner, map[string][]string{
		connectionA.ID: {"gpt-5.4", "gpt-5.5"},
		connectionB.ID: {"gpt-5.4", "gpt-5.5"},
	})
	if _, err := owner.SetProviderDefault(ClientCodex, connectionA.ID, "gpt-5.4", owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	for _, sessionID := range []string{"journal-thread-1", "journal-thread-2"} {
		launch, err := owner.PrepareLaunch(ExecutorCodex, connectionA.ID, "codex")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := owner.CommitLaunch(launch.ProvisionalID, sessionID); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, _, err := owner.SetThreadRuntime("journal-thread-1", ThreadRuntimeChoice{
		ConnectionID: connectionA.ID,
		ModelID:      "gpt-5.4",
		Effect:       ReasoningEffortHigh,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.SetThreadRuntime("journal-thread-2", ThreadRuntimeChoice{
		ConnectionID: connectionA.ID,
		ModelID:      "gpt-5.5",
		Effect:       ReasoningEffortLow,
	}); err != nil {
		t.Fatal(err)
	}
	before := captureProviderSwitchSnapshot(owner)
	if _, err := owner.SwitchProvider(ClientCodex, connectionB.ID, owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	after := captureProviderSwitchSnapshot(owner)
	return owner, before, after
}

func captureProviderSwitchSnapshot(owner *Owner) providerSwitchSnapshot {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	routes := owner.table.Snapshot()
	owner.store.mu.RLock()
	snapshot := providerSwitchSnapshotFromState(
		owner.store.revision,
		owner.store.defaults,
		owner.store.defaultModels,
		routes,
	)
	owner.store.mu.RUnlock()
	return snapshot
}

func persistProviderCatalogSnapshot(t *testing.T, owner *Owner, snapshot providerSwitchSnapshot) {
	t.Helper()
	owner.store.mu.RLock()
	profiles := cloneProfiles(owner.store.profiles)
	owner.store.mu.RUnlock()
	if err := owner.store.persistLocked(snapshot.Revision, profiles, snapshot.Defaults, snapshot.DefaultModels); err != nil {
		t.Fatal(err)
	}
}

func assertProviderSwitchSnapshot(t *testing.T, got, want providerSwitchSnapshot) {
	t.Helper()
	if got.Revision != want.Revision {
		t.Fatalf("revision=%d want=%d", got.Revision, want.Revision)
	}
	if !equalStringMap(got.Defaults, want.Defaults) || !equalStringMap(got.DefaultModels, want.DefaultModels) {
		t.Fatalf("defaults mismatch: got=%#v/%#v want=%#v/%#v", got.Defaults, got.DefaultModels, want.Defaults, want.DefaultModels)
	}
	sort.Slice(got.Routes, func(i, j int) bool {
		return got.Routes[i].Binding.SessionID < got.Routes[j].Binding.SessionID
	})
	sort.Slice(want.Routes, func(i, j int) bool {
		return want.Routes[i].Binding.SessionID < want.Routes[j].Binding.SessionID
	})
	gotDurable, err := EncodeDurableSnapshot(got.Routes)
	if err != nil {
		t.Fatal(err)
	}
	wantDurable, err := EncodeDurableSnapshot(want.Routes)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotDurable, wantDurable) {
		t.Fatalf("durable route snapshot mismatch\ngot:  %s\nwant: %s", gotDurable, wantDurable)
	}
	for _, route := range got.Routes {
		if !route.Binding.CredentialReady {
			t.Fatalf("recovered route %s did not rehydrate credential readiness", route.Binding.SessionID)
		}
	}
}

func equalStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}
