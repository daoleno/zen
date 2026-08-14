package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/modelprofiles"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/gorilla/websocket"
)

type wsProfileVerifier struct{}

func (wsProfileVerifier) VerifyProfileContract(profile modelprofiles.Profile) (modelprofiles.VerifiedProfileContract, error) {
	env := modelprofiles.DefaultTestEnvelope()
	route, _ := modelprofiles.RouteProtocolFor(profile.Protocol)
	return modelprofiles.VerifiedProfileContract{
		Provenance:       modelprofiles.ContractProvenanceBuiltinCatalog,
		ClientModelID:    profile.ClientModel,
		UpstreamModelID:  profile.Model,
		ExecutorID:       profile.ExecutorID,
		Protocol:         profile.Protocol,
		RouteProtocol:    route,
		ProviderID:       profile.ProviderID,
		ClientEnvelope:   env,
		UpstreamEnvelope: env,
		HistoryDomain: modelprofiles.DeriveOpaqueHistoryDomain(
			profile.Protocol, profile.ProviderID, profile.BaseURL, profile.Model, profile.ClientModel,
		),
	}, nil
}

func startProfileOwner(t *testing.T) *modelprofiles.Owner {
	t.Helper()
	root := t.TempDir()
	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: filepath.Join(root, "model-profiles.toml"),
		RoutesPath:   filepath.Join(root, "route-bindings.json"),
		ListenerPath: filepath.Join(root, "route-listener.json"),
		Lookup:       func(string) (string, bool) { return "ready", true },
		Verifier:     wsProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	return owner
}

func TestModelProfilesWebSocketCRUDActivateAndErrors(t *testing.T) {
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	deviceID := "device-profiles"
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), deviceID, "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	owner := startProfileOwner(t)
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv.SetModelProfiles(owner)
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	t.Cleanup(httpServer.Close)
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), deviceID, "zen-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	readType := func(want string) map[string]any {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		for {
			_, raw, readErr := conn.ReadMessage()
			if readErr != nil {
				t.Fatal(readErr)
			}
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["type"] == want {
				return payload
			}
		}
	}

	connBody := map[string]any{
		"id": "codex-main", "name": "Codex Main", "client": modelprofiles.ClientCodex,
		"preset_id": modelprofiles.ProviderPresetCustom, "base_url": "https://gateway.example/v1",
		"model_id": "up-1", "advanced": true,
	}
	if err := conn.WriteJSON(map[string]any{
		"type": "upsert_provider_connection", "request_id": "up-1", "operation": "create",
		"revision": 0, "provider_connection": connBody,
	}); err != nil {
		t.Fatal(err)
	}
	listed := readType("providers")
	if listed["request_id"] != "up-1" {
		t.Fatalf("listed=%#v", listed)
	}
	if listed["persistence_outcome"] != "applied" {
		t.Fatalf("upsert persistence_outcome=%#v", listed["persistence_outcome"])
	}
	if durable, ok := listed["persistence_durable"].(bool); !ok || !durable {
		t.Fatalf("upsert persistence_durable=%#v", listed["persistence_durable"])
	}
	rawJSON, _ := json.Marshal(listed)
	if strings.Contains(string(rawJSON), "ACME_KEY") || strings.Contains(string(rawJSON), "credential_env") {
		t.Fatalf("credential env leaked: %s", rawJSON)
	}
	if strings.Contains(string(rawJSON), "secret") {
		t.Fatalf("secret leaked: %s", rawJSON)
	}

	if err := conn.WriteJSON(map[string]any{"type": "list_providers", "request_id": "list-1"}); err != nil {
		t.Fatal(err)
	}
	listed = readType("providers")
	if listed["request_id"] != "list-1" {
		t.Fatalf("list=%#v", listed)
	}
	presets, _ := listed["presets"].([]any)
	if len(presets) == 0 {
		t.Fatalf("presets missing: %#v", listed["presets"])
	}

	plan, err := owner.PrepareLaunch(modelprofiles.ExecutorCodex, "codex-main", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "tmux:@9"); err != nil {
		t.Fatal(err)
	}

	altBody := map[string]any{
		"id": "codex-alt", "name": "Codex Alt", "client": modelprofiles.ClientCodex,
		"preset_id": modelprofiles.ProviderPresetCustom, "base_url": "https://gateway.example/v1",
		"model_id": "up-2", "advanced": true,
	}
	if err := conn.WriteJSON(map[string]any{
		"type": "upsert_provider_connection", "request_id": "up-2", "operation": "create",
		"revision": 1, "provider_connection": altBody,
	}); err != nil {
		t.Fatal(err)
	}
	_ = readType("providers")

	if err := conn.WriteJSON(map[string]any{
		"type": "activate_session_provider", "request_id": "act-bad",
		"agent_id": "tmux:@9", "connection_id": "missing-connection",
	}); err != nil {
		t.Fatal(err)
	}
	bad := readType("error")
	if bad["request_id"] != "act-bad" || bad["code"] != modelprofiles.CodeProfileNotFound {
		t.Fatalf("missing connection error=%#v", bad)
	}

	if err := conn.WriteJSON(map[string]any{
		"type": "activate_session_provider", "request_id": "act-ok",
		"agent_id": "tmux:@9", "connection_id": "codex-alt",
	}); err != nil {
		t.Fatal(err)
	}
	activated := readType("session_provider_activated")
	if activated["request_id"] != "act-ok" {
		t.Fatalf("activated=%#v", activated)
	}
	if activated["persistence_outcome"] != "applied" {
		t.Fatalf("missing persistence_outcome: %#v", activated)
	}
	if durable, ok := activated["persistence_durable"].(bool); !ok || !durable {
		t.Fatalf("persistence_durable=%#v", activated["persistence_durable"])
	}
	launched, _ := activated["launched"].(map[string]any)
	selection, _ := activated["selection"].(map[string]any)
	if launched["connection_id"] != "codex-main" || launched["model_id"] != "up-1" {
		t.Fatalf("activate launched=%#v", launched)
	}
	if selection["connection_id"] != "codex-alt" || selection["model_id"] != "up-2" {
		t.Fatalf("activate selection=%#v", selection)
	}

	if err := conn.WriteJSON(map[string]any{
		"type": "get_session_provider", "request_id": "get-1", "agent_id": "tmux:@9",
	}); err != nil {
		t.Fatal(err)
	}
	got := readType("session_provider")
	if got["request_id"] != "get-1" {
		t.Fatalf("got=%#v", got)
	}
	gotLaunched, _ := got["launched"].(map[string]any)
	if gotLaunched["connection_id"] != "codex-main" {
		t.Fatalf("get launched=%#v", gotLaunched)
	}

	if err := conn.WriteJSON(map[string]any{
		"type": "delete_provider_connection", "request_id": "del-1",
		"connection_id": "codex-alt", "revision": 2,
	}); err != nil {
		t.Fatal(err)
	}
	delErr := readType("error")
	if delErr["code"] != modelprofiles.CodeProfileInUse && delErr["code"] != modelprofiles.CodeProfileConflict {
		t.Fatalf("delete in-use=%#v", delErr)
	}
}

func TestSessionCreatedAgentSessionModelProfileCapabilities(t *testing.T) {
	owner := startProfileOwner(t)
	profile := modelprofiles.Profile{
		ID: "codex-main", Name: "Codex Main", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "acme", ProviderLabel: "Acme",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "up-1",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://gateway.example/v1",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ACME_KEY",
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(modelprofiles.ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	agentID := "tmux:@caps"
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, agentID); err != nil {
		t.Fatal(err)
	}
	// session_created builds agent_session after Commit via the same helper.
	srv := &Server{}
	srv.SetModelProfiles(owner)
	wire := srv.agentSessionWire(&classifier.Agent{ID: agentID, Command: "zsh", Name: "Codex"})
	if wire == nil || !wire.Capabilities.ModelProfileManaged || !wire.Capabilities.ModelProfileActiveSwitch {
		t.Fatalf("post-commit agent_session caps %#v", wire)
	}
	raw, _ := json.Marshal(wire.Capabilities)
	if strings.Contains(string(raw), "gateway") || strings.Contains(string(raw), "ACME_KEY") || strings.Contains(string(raw), "history") {
		t.Fatalf("secret-ish leak: %s", raw)
	}
}
func TestCleanupFailedLaunchCommitReleasesAgentBinding(t *testing.T) {
	owner := startProfileOwner(t)
	profile := modelprofiles.Profile{
		ID: "codex-main", Name: "Codex Main", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "acme", ProviderLabel: "Acme",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "up-1",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://gateway.example/v1",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ACME_KEY",
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(modelprofiles.ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "before_rename" {
			return errors.New("injected")
		}
		return nil
	})
	_, _, _, err = owner.CommitLaunch(plan.ProvisionalID, "agent:@1")
	if err == nil {
		t.Fatal("expected commit failure")
	}
	owner.RoutesFile().SetPersistHook(nil)
	killed := []string{}
	cleanup := modelprofiles.CleanupFailedLaunch(owner, plan.ProvisionalID, "agent:@1",
		func(id string) error {
			killed = append(killed, id)
			return nil
		},
		func(string) (modelprofiles.SessionLiveness, error) {
			return modelprofiles.SessionLivenessAbsent, nil
		},
	)
	if cleanup.Err != nil {
		t.Fatalf("cleanup err=%v", cleanup.Err)
	}
	if !cleanup.Persist.Applied {
		t.Fatalf("cleanup persist=%#v", cleanup.Persist)
	}
	if owner.Table().Len() != 0 {
		t.Fatalf("orphan bindings remain: %d", owner.Table().Len())
	}
	if len(killed) != 1 || killed[0] != "agent:@1" {
		t.Fatalf("killed=%v", killed)
	}
}

func TestActivateSessionRouteAppliedNotDurableReturnsOutcome(t *testing.T) {
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	deviceID := "device-profiles-durable"
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), deviceID, "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	owner := startProfileOwner(t)
	profile := modelprofiles.Profile{
		ID: "codex-main", Name: "Codex Main", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "acme", ProviderLabel: "Acme",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "up-1",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://gateway.example/v1",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ACME_KEY",
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	alt := profile
	alt.ID = "codex-alt"
	alt.Name = "Codex Alt"
	alt.Model = "up-2"
	if _, err := owner.UpsertProfile(alt, 1, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(modelprofiles.ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "tmux:@9"); err != nil {
		t.Fatal(err)
	}

	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv.SetModelProfiles(owner)
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	t.Cleanup(httpServer.Close)
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), deviceID, "zen-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "before_dirsync" {
			return errors.New("injected before_dirsync")
		}
		return nil
	})
	if err := conn.WriteJSON(map[string]any{
		"type": "activate_session_provider", "request_id": "act-warn",
		"agent_id": "tmux:@9", "connection_id": "codex-alt",
	}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var activated map[string]any
	for {
		_, raw, readErr := conn.ReadMessage()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if err := json.Unmarshal(raw, &activated); err != nil {
			t.Fatal(err)
		}
		if activated["type"] == "session_provider_activated" || activated["type"] == "error" {
			break
		}
	}
	owner.RoutesFile().SetPersistHook(nil)
	if activated["type"] != "session_provider_activated" {
		t.Fatalf("want success with warning, got %#v", activated)
	}
	if activated["persistence_outcome"] != "applied" {
		t.Fatalf("outcome=%#v", activated)
	}
	if durable, ok := activated["persistence_durable"].(bool); !ok || durable {
		t.Fatalf("durable=%#v", activated["persistence_durable"])
	}
	if activated["persistence_warning"] == nil || activated["persistence_warning"] == "" {
		t.Fatalf("expected persistence_warning: %#v", activated)
	}
	state, ok := owner.Table().Get("tmux:@9")
	if !ok || state.Binding.ProfileID != "codex-alt" {
		t.Fatalf("memory must keep applied binding: %#v", state)
	}
}

func TestModelProfileCatalogMutationsDirSyncAppliedNotDurable(t *testing.T) {
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	deviceID := "device-catalog-durable"
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), deviceID, "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	owner := startProfileOwner(t)
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv.SetModelProfiles(owner)
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	t.Cleanup(httpServer.Close)
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), deviceID, "zen-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	readTyped := func(want string) map[string]any {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		for {
			_, raw, readErr := conn.ReadMessage()
			if readErr != nil {
				t.Fatal(readErr)
			}
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["type"] == want {
				return payload
			}
		}
	}
	assertAppliedWarning := func(payload map[string]any) map[string]any {
		t.Helper()
		if payload["type"] != "providers" {
			t.Fatalf("want providers got %#v", payload)
		}
		if payload["persistence_outcome"] != "applied" {
			t.Fatalf("outcome=%#v", payload)
		}
		if durable, ok := payload["persistence_durable"].(bool); !ok || durable {
			t.Fatalf("durable=%#v", payload["persistence_durable"])
		}
		if payload["persistence_warning"] == nil || payload["persistence_warning"] == "" {
			t.Fatalf("expected warning: %#v", payload)
		}
		if payload["connections"] == nil || payload["presets"] == nil {
			t.Fatalf("missing authoritative connections/presets: %#v", payload)
		}
		presets, _ := payload["presets"].([]any)
		if len(presets) == 0 {
			t.Fatalf("presets missing: %#v", payload["presets"])
		}
		return payload
	}

	connBody := map[string]any{
		"id": "codex-main", "name": "Codex Main", "client": modelprofiles.ClientCodex,
		"preset_id": modelprofiles.ProviderPresetCustom, "base_url": "https://gateway.example/v1",
		"model_id": "up-1", "advanced": true,
	}

	// Pre-rename / CAS failure remains not-applied error (no catalog body).
	if err := conn.WriteJSON(map[string]any{
		"type": "upsert_provider_connection", "request_id": "create-conflict",
		"operation": "create", "revision": 99, "provider_connection": connBody,
	}); err != nil {
		t.Fatal(err)
	}
	bad := readTyped("error")
	if bad["code"] != modelprofiles.CodeProfileConflict {
		t.Fatalf("not-applied conflict=%#v", bad)
	}
	if owner.Catalog().Revision != 0 {
		t.Fatalf("memory must stay empty after not-applied: %#v", owner.Catalog())
	}

	owner.Store().SetDirSync(func(string) error {
		return errors.New("injected catalog dirSync")
	})
	defer owner.Store().SetDirSync(nil)

	// Create
	if err := conn.WriteJSON(map[string]any{
		"type": "upsert_provider_connection", "request_id": "create-ok",
		"operation": "create", "revision": 0, "provider_connection": connBody,
	}); err != nil {
		t.Fatal(err)
	}
	_ = assertAppliedWarning(readTyped("providers"))
	if owner.Catalog().Revision != 1 || len(owner.Catalog().Profiles) != 1 {
		t.Fatalf("create memory=%#v", owner.Catalog())
	}

	// Update
	updated := map[string]any{
		"id": "codex-main", "name": "Codex Updated", "client": modelprofiles.ClientCodex,
		"preset_id": modelprofiles.ProviderPresetCustom, "base_url": "https://gateway.example/v1",
		"model_id": "up-2", "advanced": true,
	}
	if err := conn.WriteJSON(map[string]any{
		"type": "upsert_provider_connection", "request_id": "update-ok",
		"operation": "update", "revision": 1, "provider_connection": updated,
	}); err != nil {
		t.Fatal(err)
	}
	_ = assertAppliedWarning(readTyped("providers"))
	got, err := owner.GetProfile("codex-main")
	if err != nil || got.Name != "Codex Updated" || got.Model != "up-2" {
		t.Fatalf("update memory=%#v err=%v", got, err)
	}

	// SetDefault
	if err := conn.WriteJSON(map[string]any{
		"type": "set_provider_default", "request_id": "default-ok",
		"executor_id": modelprofiles.ExecutorCodex, "connection_id": "codex-main", "revision": 2,
	}); err != nil {
		t.Fatal(err)
	}
	payload := assertAppliedWarning(readTyped("providers"))
	defaults, _ := payload["defaults"].(map[string]any)
	def, _ := defaults[modelprofiles.ClientCodex].(map[string]any)
	if def == nil {
		def, _ = defaults[modelprofiles.ExecutorCodex].(map[string]any)
	}
	if def["connection_id"] != "codex-main" {
		t.Fatalf("defaults=%#v", defaults)
	}

	// Clear default then Delete
	if err := conn.WriteJSON(map[string]any{
		"type": "set_provider_default", "request_id": "default-clear",
		"executor_id": modelprofiles.ExecutorCodex, "connection_id": "", "revision": 3,
	}); err != nil {
		t.Fatal(err)
	}
	_ = assertAppliedWarning(readTyped("providers"))
	if err := conn.WriteJSON(map[string]any{
		"type": "delete_provider_connection", "request_id": "delete-ok",
		"connection_id": "codex-main", "revision": 4,
	}); err != nil {
		t.Fatal(err)
	}
	payload = assertAppliedWarning(readTyped("providers"))
	connections, _ := payload["connections"].([]any)
	if len(connections) != 0 {
		t.Fatalf("delete connections=%#v", payload)
	}
	if owner.Catalog().Revision != 5 || len(owner.Catalog().Profiles) != 0 {
		t.Fatalf("delete memory=%#v", owner.Catalog())
	}
}

func TestWSActivateLaunchedSurvivesHistoryTrimAndRestart(t *testing.T) {
	root := t.TempDir()
	profiles := filepath.Join(root, "model-profiles.toml")
	routes := filepath.Join(root, "route-bindings.json")
	listener := filepath.Join(root, "route-listener.json")
	lookup := func(string) (string, bool) { return "ready", true }
	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: profiles, RoutesPath: routes, ListenerPath: listener,
		Lookup: lookup, Verifier: wsProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	base := modelprofiles.Profile{
		ID: "p0", Name: "P0", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "acme", ProviderLabel: "Acme",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "up-0",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://gateway.example/v1",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ACME_KEY",
	}
	if _, err := owner.UpsertProfile(base, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(modelprofiles.ExecutorCodex, base.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "tmux:@trim"); err != nil {
		t.Fatal(err)
	}

	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	deviceID := "device-trim"
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), deviceID, "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv.SetModelProfiles(owner)
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), deviceID, "zen-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	readType := func(want string) map[string]any {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			_, raw, readErr := conn.ReadMessage()
			if readErr != nil {
				t.Fatal(readErr)
			}
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["type"] == want {
				return payload
			}
		}
	}

	rev := int64(1)
	for i := 1; i <= modelprofiles.MaxRouteHistoryEvents+8; i++ {
		p := base
		p.ID = "p" + itoaWS(i)
		p.Name = p.ID
		p.Model = "up-" + itoaWS(i)
		if _, err := owner.UpsertProfile(p, rev, true); err != nil {
			t.Fatal(err)
		}
		rev++
		_, ok := owner.Table().Get("tmux:@trim")
		if !ok {
			t.Fatal("missing")
		}
		if err := conn.WriteJSON(map[string]any{
			"type": "activate_session_provider", "request_id": "act-" + itoaWS(i),
			"agent_id": "tmux:@trim", "connection_id": p.ID,
		}); err != nil {
			t.Fatal(err)
		}
		activated := readType("session_provider_activated")
		launched, _ := activated["launched"].(map[string]any)
		selection, _ := activated["selection"].(map[string]any)
		if launched["connection_id"] != "p0" || launched["model_id"] != "up-0" {
			t.Fatalf("launched drifted i=%d %#v", i, launched)
		}
		if selection["connection_id"] != p.ID {
			t.Fatalf("current %#v", selection)
		}
	}
	_ = conn.Close()
	httpServer.Close()
	_ = owner.Close()

	owner2, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: profiles, RoutesPath: routes, ListenerPath: listener,
		Lookup: lookup, Verifier: wsProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner2.Close() })
	srv2 := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv2.SetModelProfiles(owner2)
	httpServer2 := httptest.NewServer(http.HandlerFunc(srv2.handleWS))
	t.Cleanup(httpServer2.Close)
	header2 := http.Header{}
	header2.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), deviceID, "zen-connect"))
	conn2, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer2.URL, "http"), header2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn2.Close() })
	if err := conn2.WriteJSON(map[string]any{
		"type": "get_session_provider", "request_id": "get-restart", "agent_id": "tmux:@trim",
	}); err != nil {
		t.Fatal(err)
	}
	_ = conn2.SetReadDeadline(time.Now().Add(3 * time.Second))
	var got map[string]any
	for {
		_, raw, readErr := conn2.ReadMessage()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got["type"] == "session_provider" {
			break
		}
	}
	launched, _ := got["launched"].(map[string]any)
	if launched["connection_id"] != "p0" || launched["model_id"] != "up-0" {
		t.Fatalf("restart launched %#v", launched)
	}
	restored, ok := owner2.Table().Get("tmux:@trim")
	if !ok || len(restored.History) > modelprofiles.MaxRouteHistoryEvents {
		t.Fatalf("history len=%d ok=%v", len(restored.History), ok)
	}
}

func itoaWS(i int) string {
	if i == 0 {
		return "0"
	}
	var b [16]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	return string(b[n:])
}

func commitTestRoute(t *testing.T, owner *modelprofiles.Owner, agentID string) {
	t.Helper()
	if owner.Catalog().Revision == 0 {
		profile := modelprofiles.Profile{
			ID: "codex-main", Name: "Codex Main", ExecutorID: modelprofiles.ExecutorCodex,
			ProviderID: "acme", ProviderLabel: "Acme",
			Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "up-1",
			ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
			BaseURL:               "https://gateway.example/v1",
			AuthMode:              modelprofiles.AuthModeBearerEnv,
			CredentialEnv:         "ACME_KEY",
		}
		if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := owner.PrepareLaunch(modelprofiles.ExecutorCodex, "codex-main", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, agentID); err != nil {
		t.Fatal(err)
	}
}

func TestKillAgentRouteAwareTeardown(t *testing.T) {
	owner := startProfileOwner(t)
	srv := New(nil, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv.SetModelProfiles(owner)
	agentID := "tmux:@kill"

	// kill success + release success
	commitTestRoute(t, owner, agentID)
	srv.killSessionOverride = func(string) error { return nil }
	srv.probeSessionOverride = func(string) (watcher.SessionPresence, error) {
		return watcher.SessionPresenceAbsent, nil
	}
	if result := srv.teardownAgentSession(agentID); result.Err != nil || !result.Persist.Applied {
		t.Fatalf("success=%#v", result)
	}
	if _, ok := owner.SessionSnapshot(agentID); ok {
		t.Fatal("route must be released")
	}

	// kill succeeds / window gone + release pre-rename failure
	commitTestRoute(t, owner, agentID)
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "before_rename" {
			return errors.New("injected release pre-rename")
		}
		return nil
	})
	result := srv.teardownAgentSession(agentID)
	owner.RoutesFile().SetPersistHook(nil)
	if result.Err == nil || !strings.Contains(result.Err.Error(), "injected release pre-rename") {
		t.Fatalf("release fail=%#v", result)
	}
	if _, ok := owner.SessionSnapshot(agentID); !ok {
		t.Fatal("route must remain when release not applied")
	}

	// retry convergence: idempotent missing kill (nil) + release applies
	srv.killSessionOverride = func(string) error { return nil }
	srv.probeSessionOverride = func(string) (watcher.SessionPresence, error) {
		return watcher.SessionPresenceAbsent, nil
	}
	if result := srv.teardownAgentSession(agentID); result.Err != nil {
		t.Fatalf("retry=%#v", result)
	}
	if _, ok := owner.SessionSnapshot(agentID); ok {
		t.Fatal("retry must release")
	}

	// kill succeeds but resource cleanup fails — preserve route even if absent
	commitTestRoute(t, owner, agentID)
	srv.killSessionOverride = func(string) error {
		return fmt.Errorf("%w: injected resource cleanup", watcher.ErrDelegatedResourceRelease)
	}
	srv.probeSessionOverride = func(string) (watcher.SessionPresence, error) {
		return watcher.SessionPresenceAbsent, nil
	}
	result = srv.teardownAgentSession(agentID)
	if result.Err == nil || !errors.Is(result.Err, watcher.ErrDelegatedResourceRelease) {
		t.Fatalf("resource fail=%#v", result)
	}
	if _, ok := owner.SessionSnapshot(agentID); !ok {
		t.Fatal("resource failure must preserve route")
	}

	// true missing + successful resource cleanup converges
	srv.killSessionOverride = func(string) error { return nil }
	if result := srv.teardownAgentSession(agentID); result.Err != nil {
		t.Fatalf("resource retry=%#v", result)
	}

	// kill fails + still live preserves route
	commitTestRoute(t, owner, agentID)
	srv.killSessionOverride = func(string) error { return errors.New("injected kill failure") }
	srv.probeSessionOverride = func(string) (watcher.SessionPresence, error) {
		return watcher.SessionPresencePresent, nil
	}
	result = srv.teardownAgentSession(agentID)
	if result.Err == nil || !errors.Is(result.Err, modelprofiles.ErrSessionStillLive) {
		t.Fatalf("still live=%#v", result)
	}
	if _, ok := owner.SessionSnapshot(agentID); !ok {
		t.Fatal("still-live must preserve route")
	}

	// liveness probe error preserves route
	srv.probeSessionOverride = func(string) (watcher.SessionPresence, error) {
		return watcher.SessionPresenceUnknown, errors.New("injected probe failure")
	}
	result = srv.teardownAgentSession(agentID)
	if result.Err == nil || !errors.Is(result.Err, modelprofiles.ErrSessionLivenessUnknown) {
		t.Fatalf("probe fail=%#v", result)
	}
	if _, ok := owner.SessionSnapshot(agentID); !ok {
		t.Fatal("probe failure must preserve route")
	}

	// applied + non-durable cleanup surfaces error
	srv.killSessionOverride = func(string) error { return nil }
	srv.probeSessionOverride = func(string) (watcher.SessionPresence, error) {
		return watcher.SessionPresenceAbsent, nil
	}
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "after_rename" {
			return errors.New("injected dirsync")
		}
		return nil
	})
	result = srv.teardownAgentSession(agentID)
	owner.RoutesFile().SetPersistHook(nil)
	if result.Err == nil || !errors.Is(result.Err, modelprofiles.ErrPersistDirSync) {
		t.Fatalf("non-durable=%#v", result)
	}
	if !result.Persist.Applied || result.Persist.Durable {
		t.Fatalf("persist=%#v", result.Persist)
	}
}

func TestKillAgentWebSocketSurfacesTeardownError(t *testing.T) {
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	deviceID := "device-kill-ws"
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), deviceID, "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	owner := startProfileOwner(t)
	commitTestRoute(t, owner, "tmux:@ws-kill")
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv.SetModelProfiles(owner)
	srv.killSessionOverride = func(string) error { return errors.New("injected kill failure") }
	srv.probeSessionOverride = func(string) (watcher.SessionPresence, error) {
		return watcher.SessionPresencePresent, nil
	}

	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	t.Cleanup(httpServer.Close)
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), deviceID, "zen-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.WriteJSON(map[string]any{"type": "kill_agent", "request_id": "kill-live", "agent_id": "tmux:@ws-kill"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		_ = conn.SetReadDeadline(deadline)
		_, raw, readErr := conn.ReadMessage()
		if readErr != nil {
			t.Fatal(readErr)
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["type"] != "error" {
			continue
		}
		if payload["request_id"] != "kill-live" {
			continue
		}
		if payload["code"] != "kill_failed" || !strings.Contains(fmt.Sprint(payload["message"]), "injected kill failure") {
			t.Fatalf("payload=%#v", payload)
		}
		if _, ok := owner.SessionSnapshot("tmux:@ws-kill"); !ok {
			t.Fatal("WS still-live must preserve route")
		}
		return
	}
}
