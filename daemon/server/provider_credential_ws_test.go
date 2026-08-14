package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/modelprofiles"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/gorilla/websocket"
)

type recordingCredStore struct {
	mu      sync.Mutex
	inner   *modelprofiles.MemoryCredentialStore
	sets    int
	deletes int
	failSet error
	failDel error
	avail   bool
}

func newRecordingCredStore() *recordingCredStore {
	return &recordingCredStore{inner: modelprofiles.NewMemoryCredentialStore(), avail: true}
}

func (r *recordingCredStore) Available() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.avail
}

func (r *recordingCredStore) Set(ref, secret string) error {
	r.mu.Lock()
	r.sets++
	fail := r.failSet
	avail := r.avail
	r.mu.Unlock()
	if !avail {
		return modelprofiles.ErrCredentialStoreUnavailable
	}
	if fail != nil {
		return fail
	}
	return r.inner.Set(ref, secret)
}

func (r *recordingCredStore) Get(ref string) (string, bool, error) {
	return r.inner.Get(ref)
}

func (r *recordingCredStore) Delete(ref string) error {
	r.mu.Lock()
	r.deletes++
	fail := r.failDel
	avail := r.avail
	r.mu.Unlock()
	if !avail {
		return modelprofiles.ErrCredentialStoreUnavailable
	}
	if fail != nil {
		return fail
	}
	return r.inner.Delete(ref)
}

func (r *recordingCredStore) Refs() ([]string, error) {
	return r.inner.Refs()
}

func (r *recordingCredStore) setCounts() (sets, deletes int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sets, r.deletes
}

func TestProviderCredentialWebSocketAuthenticatedHTTPAndWire(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	deviceID := "device-cred-ws"
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), deviceID, "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}

	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: t.TempDir() + "/model-profiles.toml",
		RoutesPath:   t.TempDir() + "/route-bindings.json",
		ListenerPath: t.TempDir() + "/route-listener.json",
		Lookup:       func(string) (string, bool) { return "", false },
		Verifier:     wsProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	creds := newRecordingCredStore()
	owner.SetCredentialStore(creds)
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

	readJSON := func() (map[string]any, []byte) {
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
			switch payload["type"] {
			case "agent_session_list", "work_items_snapshot", "calendar_snapshot":
				continue
			}
			return payload, raw
		}
	}

	secret := "sk-live-secret-never-echo"
	if err := conn.WriteJSON(map[string]any{
		"type": "upsert_provider_connection", "request_id": "create-1", "operation": "create",
		"revision": 0, "provider_connection": map[string]any{
			"preset_id": modelprofiles.ProviderPresetDeepSeek, "name": "DeepSeek",
			"client": modelprofiles.ClientCodex,
		},
	}); err != nil {
		t.Fatal(err)
	}
	listed, listedRaw := readJSON()
	if listed["type"] != "providers" {
		t.Fatalf("listed=%#v", listed)
	}
	if strings.Contains(string(listedRaw), secret) {
		t.Fatal("secret leaked in create reply")
	}
	connections, _ := listed["connections"].([]any)
	if len(connections) == 0 {
		t.Fatal("expected connection")
	}
	connObj := connections[0].(map[string]any)
	connectionID, _ := connObj["id"].(string)
	if connectionID == "" || connObj["name"] != "DeepSeek" {
		t.Fatalf("curated create wire=%#v", connObj)
	}
	for _, banned := range []string{"provider_id", "base_url", "model_id", "credential_env"} {
		if _, ok := connObj[banned]; ok {
			t.Fatalf("curated reply leaked %q: %#v", banned, connObj)
		}
	}

	setsBefore, _ := creds.setCounts()
	if err := conn.WriteJSON(map[string]any{
		"type": "set_provider_credential", "request_id": "alias-1",
		"connection_id": connectionID, "text": secret, "body": secret,
	}); err != nil {
		t.Fatal(err)
	}
	aliasResp, aliasRaw := readJSON()
	if aliasResp["type"] != "error" {
		t.Fatalf("text/body alias must not apply: %#v", aliasResp)
	}
	if strings.Contains(string(aliasRaw), secret) {
		t.Fatal("secret leaked in alias error")
	}
	setsAfter, _ := creds.setCounts()
	if setsAfter != setsBefore {
		t.Fatal("text/body must not call store")
	}

	if err := conn.WriteJSON(map[string]any{
		"type": "set_provider_credential", "request_id": "set-1",
		"connection_id": connectionID, "credential": secret,
	}); err != nil {
		t.Fatal(err)
	}
	setResp, setRaw := readJSON()
	if setResp["type"] != "provider_credential" || setResp["credential_ready"] != true {
		t.Fatalf("set=%#v", setResp)
	}
	if strings.Contains(string(setRaw), secret) {
		t.Fatal("secret leaked in set reply")
	}
	if _, ok := setResp["credential"]; ok {
		t.Fatal("read model must be readiness-only")
	}

	if err := conn.WriteJSON(map[string]any{
		"type": "upsert_provider_connection", "request_id": "create-2", "operation": "create",
		"revision": listed["revision"], "provider_connection": map[string]any{
			"preset_id": modelprofiles.ProviderPresetDeepSeek, "name": "DeepSeek B",
			"client": modelprofiles.ClientCodex,
		},
	}); err != nil {
		t.Fatal(err)
	}
	listed2, _ := readJSON()
	if listed2["type"] != "providers" {
		t.Fatalf("create B=%#v", listed2)
	}
	var connB string
	var readyA, readyB bool
	for _, c := range listed2["connections"].([]any) {
		m := c.(map[string]any)
		switch m["name"] {
		case "DeepSeek":
			readyA, _ = m["credential_ready"].(bool)
			if id, _ := m["id"].(string); id != connectionID {
				t.Fatalf("A id drift: %q vs %q", id, connectionID)
			}
		case "DeepSeek B":
			connB, _ = m["id"].(string)
			readyB, _ = m["credential_ready"].(bool)
		}
	}
	if connB == "" || connB == connectionID {
		t.Fatalf("missing isolated B: a=%q b=%q", connectionID, connB)
	}
	if !readyA || readyB {
		t.Fatalf("readiness isolation: A=%v B=%v", readyA, readyB)
	}
	if _, ok, _ := creds.Get(modelprofiles.CredentialRefFor(connB)); ok {
		t.Fatal("credential store must isolate across connections")
	}

	creds.mu.Lock()
	creds.avail = false
	creds.mu.Unlock()
	if err := conn.WriteJSON(map[string]any{
		"type": "set_provider_credential", "request_id": "unavail",
		"connection_id": connB, "credential": "sk-b",
	}); err != nil {
		t.Fatal(err)
	}
	unavail, unavailRaw := readJSON()
	if unavail["type"] != "error" || unavail["code"] != modelprofiles.CodeCredentialStoreUnavailable {
		t.Fatalf("unavailable=%#v", unavail)
	}
	if strings.Contains(string(unavailRaw), "sk-b") {
		t.Fatal("secret leaked in unavailable error")
	}
	creds.mu.Lock()
	creds.avail = true
	creds.mu.Unlock()

	creds.mu.Lock()
	creds.failSet = modelprofiles.ErrCredentialStoreFailed
	creds.mu.Unlock()
	if err := conn.WriteJSON(map[string]any{
		"type": "set_provider_credential", "request_id": "fail",
		"connection_id": connB, "credential": "sk-fail",
	}); err != nil {
		t.Fatal(err)
	}
	failResp, failRaw := readJSON()
	if failResp["type"] != "error" || failResp["code"] != modelprofiles.CodeCredentialStoreFailed {
		t.Fatalf("failed=%#v", failResp)
	}
	if strings.Contains(string(failRaw), "sk-fail") {
		t.Fatal("secret leaked in failed error")
	}
	creds.mu.Lock()
	creds.failSet = nil
	creds.mu.Unlock()

	if err := conn.WriteJSON(map[string]any{
		"type": "clear_provider_credential", "request_id": "clear-1",
		"connection_id": connectionID,
	}); err != nil {
		t.Fatal(err)
	}
	clearResp, clearRaw := readJSON()
	if clearResp["type"] != "provider_credential" || clearResp["credential_ready"] != false {
		t.Fatalf("clear=%#v", clearResp)
	}
	if strings.Contains(string(clearRaw), secret) {
		t.Fatal("secret leaked in clear reply")
	}

}
