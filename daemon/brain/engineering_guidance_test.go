package brain

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/work"
)

func TestEngineeringPlaybookUpgradePreservesCustomFiles(t *testing.T) {
	for _, name := range []string{"align.md", "wayfind.md", "slice-work.md", "delegate-brief.md"} {
		for _, customized := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/custom=%t", name, customized), func(t *testing.T) {
				root := t.TempDir()
				store, err := NewStore(root)
				if err != nil {
					t.Fatal(err)
				}
				legacy := mustReadFile(t, filepath.Join("testdata", "engineering-v1", name))
				if customized {
					legacy = append(legacy, []byte("\nUser-owned override.  \n")...)
				}
				if err := os.WriteFile(store.playbookPath(name), legacy, 0o640); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(store.playbookPath(name), 0o640); err != nil {
					t.Fatal(err)
				}
				service := NewService(store, nil, nil)
				report, err := service.Housekeeping()
				if err != nil {
					t.Fatal(err)
				}
				got := mustReadFile(t, store.playbookPath(name))
				changed := containsString(report.ChangedPaths, "playbooks/"+name)
				if customized {
					if !bytes.Equal(got, legacy) || changed {
						t.Fatal("customized unmarked playbook was changed")
					}
					assertFileMode(t, store.playbookPath(name), 0o640)
				} else {
					if bytes.Equal(got, legacy) || !changed {
						t.Fatal("known shipped seed did not upgrade through Housekeeping")
					}
					assertFileMode(t, store.playbookPath(name), 0o600)
				}
				if _, err := NewStore(root); err != nil {
					t.Fatal(err)
				}
				if after := mustReadFile(t, store.playbookPath(name)); !bytes.Equal(after, got) {
					t.Fatal("reopening store changed the reconciled playbook")
				}
				report, err = service.Housekeeping()
				if err != nil || len(report.ChangedPaths) != 0 {
					t.Fatalf("second repair not idempotent: %+v, %v", report, err)
				}
			})
		}
	}
}

func TestEngineeringGuidanceGeneratedAndLazy(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := store.PlaybookCatalog()
	if err != nil {
		t.Fatal(err)
	}
	agents := string(mustReadFile(t, store.workspaceInstructionsPath()))
	if !strings.Contains(agents, "## Engineering Judgment") || len(agents) > 6500 {
		t.Fatalf("standing guidance missing or oversized: %d bytes", len(agents))
	}
	detailMarkers := map[string]string{
		"align":          "suggested implementation",
		"wayfind":        "licensing",
		"slice-work":     "discriminating experiment",
		"delegate-brief": "fail-before/pass-after",
	}
	for _, entry := range catalog.Playbooks {
		marker, relevant := detailMarkers[entry.Name]
		if !relevant {
			continue
		}
		if !strings.Contains(agents, entry.Name) || entry.Description == "" {
			t.Fatalf("method %s is not discoverable from standing guidance", entry.Name)
		}
		file, err := store.ReadWorkspaceFile(entry.Path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(file.Content, marker) || len(file.Content) > 3500 {
			t.Fatalf("%s missing method or oversized: %d bytes", entry.Path, len(file.Content))
		}
		delete(detailMarkers, entry.Name)
	}
	if len(detailMarkers) != 0 {
		t.Fatalf("missing methods: %v", detailMarkers)
	}
	for _, provider := range []string{"pi", "codex"} {
		t.Run(provider, func(t *testing.T) {
			service := NewService(store, &fakeWatcher{}, work.NewExecutorConfig(provider, map[string]work.Executor{
				provider: {Name: provider, Command: provider, Kind: provider},
			}))
			bootstrap := service.hostBootstrapPrompt(service.hostExecutor())
			activation := brainHostActivationPrompt()
			handoff := formatHostHandoffPrompt("arbitrary-project", provider, provider, provider, nil)
			for name, prompt := range map[string]string{"bootstrap": bootstrap, "activation": activation, "handoff": handoff} {
				if !strings.Contains(prompt, "AGENTS.md") || !work.IsPrivateHostPrompt(prompt) {
					t.Fatalf("%s lost guidance pointer or privacy", name)
				}
				for _, excluded := range []string{"fail-before/pass-after", "licensing", ".agents/zen-verification", "/private/project/secret", "pstack"} {
					if strings.Contains(prompt, excluded) {
						t.Fatalf("%s eagerly loaded or leaked %q", name, excluded)
					}
				}
			}
			if len(activation) > 800 || len(bootstrap) > 2400 || len(handoff) > 1000 {
				t.Fatalf("prompt growth: activation=%d bootstrap=%d handoff=%d", len(activation), len(bootstrap), len(handoff))
			}
			t.Logf("%s: AGENTS=%d bootstrap=%d activation=%d handoff=%d bytes", provider, len(agents), len(bootstrap), len(activation), len(handoff))
		})
	}
}

func TestEngineeringGuidanceRepairsManagedHomeWithOverlays(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	private := map[string][]byte{}
	for _, path := range []string{"soul.md", "profile.md", "memory.md", "current.md", "worklog/private.md"} {
		private[path] = []byte("private-" + path + "\n")
		if err := os.WriteFile(filepath.Join(store.WorkspacePath(), path), private[path], 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, spec := range store.managedMarkdownSpecs() {
		stale := "user prefix  \n\n" + managedStartMarker(spec.managedID) + "\nold product\n" + managedEndMarker(spec.managedID) + "\nuser suffix\n"
		if err := os.WriteFile(spec.path, []byte(stale), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		store, err = NewStore(root)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range store.managedMarkdownSpecs() {
			want := "user prefix  \n\n" + string(canonicalManagedBlock(spec)) + "\nuser suffix\n"
			if got := string(mustReadFile(t, spec.path)); got != want {
				t.Fatalf("%s damaged user text or duplicated guidance", spec.relativePath)
			}
		}
		for path, want := range private {
			if got := mustReadFile(t, filepath.Join(store.WorkspacePath(), path)); !bytes.Equal(got, want) {
				t.Fatalf("private overlay %s changed", path)
			}
		}
	}
	before, err := workspaceContentIdentity(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(store, nil, nil).Housekeeping(); err != nil {
		t.Fatal(err)
	}
	after, err := workspaceContentIdentity(store)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("repair changed reconciled files: %v", err)
	}
}

func TestEngineeringGuidanceRefreshesExistingHost(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const hostID, generation = "host:@existing", "existing-generation"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Worker{
			hostID: {ID: hostID, Command: "codex", Hidden: true, State: classifier.StateRunning},
		},
		ownedGenerations: map[string]string{hostID: generation},
	}
	service := NewService(store, fw, work.NewExecutorConfig("codex", map[string]work.Executor{
		"codex": {Name: "codex", Command: "codex", Kind: "codex"},
	}))
	// Previous releases hashed only the role, so a method-only update was invisible.
	activation := HostActivation{SessionID: hostID, HostGeneration: generation,
		ContractDigest: fmt.Sprintf("%x", sha256.Sum256([]byte(brainWorkerRoleContract)))}
	activation.Receipt = hostActivationReceipt(activation.SessionID, activation.HostGeneration, activation.ContractDigest)
	if err := store.MarkHostActivation(activation); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := NewService(store, fw, service.execs).EnsureHostSnapshot(); err != nil {
			t.Fatal(err)
		}
	}
	if len(fw.sentCalls) != 1 || !strings.Contains(fw.sentCalls[0].text, "Read AGENTS.md") {
		t.Fatalf("method upgrade must refresh existing Host once: sends=%d", len(fw.sentCalls))
	}
}

func TestHostContractDigestTracksReleaseGuidance(t *testing.T) {
	original := brainHostContractDigest()
	for _, source := range []*string{&productWorkspaceInstructions, &productDelegationPolicy, &productEnginePolicy, &productHandoffPolicy, &seedPlaybooks[1].initial} {
		before := *source
		*source += "\nChanged release guidance.\n"
		changed := brainHostContractDigest()
		*source = before
		if changed == original || brainHostContractDigest() != original {
			t.Fatal("activation digest failed to track a product-only guidance change")
		}
	}
}

func TestEngineeringSeedUpgradeLeavesSymlinkOwnedByUser(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "custom.md")
	legacy := mustReadFile(t, "testdata/engineering-v1/wayfind.md")
	if err := os.WriteFile(target, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	path := store.playbookPath("wayfind.md")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	catalog, err := store.PlaybookCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(path); err != nil || got != target || !bytes.Equal(mustReadFile(t, target), legacy) {
		t.Fatalf("seed migration changed user symlink: %v", err)
	}
	for _, entry := range catalog.Playbooks {
		if entry.Name == "wayfind" {
			t.Fatal("catalog followed a user symlink")
		}
	}
}
