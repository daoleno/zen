package modelprofiles

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSavedCustomClaudeLaunchRoutesSelectedModelAndKey(t *testing.T) {
	const model = "vendor/claude-test"
	const key = "fixture-key-not-real"
	var messagePath, messageModel string
	var keyMatches bool
	messageCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models") {
			http.Error(w, "model list unavailable", http.StatusNotFound)
			return
		}
		messagePath = r.URL.Path
		messageCount++
		keyMatches = r.Header.Get("x-api-key") == key
		var payload struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		messageModel = payload.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_fixture","type":"message","role":"assistant","model":"vendor/claude-test","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":1}}`)
	}))
	defer upstream.Close()

	root := t.TempDir()
	owner, _ := newDurableOwner(t, root)
	input := ProviderConnectionInput{
		Name: "Custom Claude", Client: ClientClaude, PresetID: ProviderPresetCustom,
		BaseURL: upstream.URL + "/proxy/v1/", ModelID: model, Advanced: true,
	}
	created, err := owner.UpsertProviderConnection(input, key, owner.Catalog().Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	conn := created.Connections[0]
	if !conn.CredentialReady || conn.ManualModelID != model || conn.BaseURL != strings.TrimRight(input.BaseURL, "/") {
		t.Fatalf("saved connection lost URL, model or readiness: %#v", conn)
	}
	if strings.Contains(conn.CredentialHint, key) {
		t.Fatal("public projection exposed the stored key")
	}
	if _, testErr := owner.TestSavedProviderConnection(conn.ID); testErr == nil ||
		!strings.Contains(testErr.Error(), "API key not verified") || !strings.Contains(testErr.Error(), "manual model ID") {
		t.Fatalf("unavailable model listing must not claim invalid credentials: %v", testErr)
	}
	discovery, discoveryErr := owner.DiscoverProviderModelsDetailed(conn.ID, true)
	manualFound := false
	bundledFound := false
	for _, entry := range discovery.Entries {
		if entry.ID == model && entry.Available && entry.Source == ModelSourceManual {
			manualFound = true
		}
		if entry.Source == ModelSourceBundled && entry.Available {
			bundledFound = true
		}
	}
	if discoveryErr == nil || !manualFound || !bundledFound {
		t.Fatalf("missing /models must keep manual model, add local candidates, and return an honest warning: entries=%#v warning=%t", discovery.Entries, discoveryErr != nil)
	}
	created, err = owner.SetProviderDefault(ClientClaude, conn.ID, model, created.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	owner, _ = newDurableOwner(t, root)
	defer owner.Close()
	reopened, err := owner.ProjectProviders()
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Defaults[ClientClaude] != (ProviderDefault{ConnectionID: conn.ID, ModelID: model}) {
		t.Fatalf("default/manual model not restored: %#v", reopened.Defaults)
	}
	reopenedManual := false
	reopenedBundled := false
	for _, entry := range reopened.Models[conn.ID] {
		if entry.ID == model && entry.Available && entry.Source == ModelSourceManual {
			reopenedManual = true
		}
		if entry.Source == ModelSourceBundled && entry.Available {
			reopenedBundled = true
		}
	}
	if !reopenedManual || !reopenedBundled || len(reopened.Connections) != 1 || reopened.Connections[0].ModelCatalogWarning == "" || !reopened.Connections[0].ModelCatalogStale {
		t.Fatalf("reopened fallback/warning projection is not truthful: connection=%#v models=%#v", reopened.Connections[0], reopened.Models[conn.ID])
	}
	plan, err := owner.PrepareLaunch(ExecutorClaude, "", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if plan.State.Binding.ProfileID != conn.ID || plan.State.Binding.UpstreamModel != model ||
		!strings.Contains(plan.Command, "--model claude-sonnet-4-6") || plan.Env[EnvAnthropicAuthToken] != LoopbackAuthPlaceholder ||
		plan.Env[EnvAnthropicAPIKey] != LoopbackClaudeAPIKeyPlaceholder {
		t.Fatalf("wrong managed launch identity: profile=%q model=%q command=%q", plan.State.Binding.ProfileID, plan.State.Binding.UpstreamModel, plan.Command)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "claude-custom-fixture"); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, plan.Env[EnvAnthropicBaseURL]+"/v1/messages", bytes.NewBufferString(`{"model":"vendor/claude-test","max_tokens":2,"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+LoopbackAuthPlaceholder)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || messagePath != "/proxy/v1/messages" || messageModel != model || !keyMatches {
		t.Fatalf("route response=%d path=%q model=%q key_matches=%t", response.StatusCode, messagePath, messageModel, keyMatches)
	}

	if os.Getenv("ZEN_CLAUDE_NATIVE_FIXTURE") == "" {
		return
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Fatal(err)
	}
	privateHome := t.TempDir()
	configDir := filepath.Join(privateHome, ".claude")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Competing private user settings must not replace the selected route.
	settings := []byte(`{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:1","ANTHROPIC_API_KEY":"sk-ant-competing-fixture-key"}}`)
	if err := os.WriteFile(filepath.Join(configDir, "settings.json"), settings, 0o600); err != nil {
		t.Fatal(err)
	}
	env := make([]string, 0, len(os.Environ())+6)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "HOME", "CLAUDE_CONFIG_DIR", "ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY":
			continue
		}
		env = append(env, entry)
	}
	env = append(env,
		"HOME="+privateHome, "CLAUDE_CONFIG_DIR="+configDir,
		"ANTHROPIC_API_KEY=sk-ant-competing-fixture-key", "CLAUDE_CODE_SIMPLE=1",
		"HTTP_PROXY=http://127.0.0.1:1", "HTTPS_PROXY=http://127.0.0.1:1",
		"ALL_PROXY=http://127.0.0.1:1", "NO_PROXY=127.0.0.1,localhost",
		"DISABLE_TELEMETRY=1", "DISABLE_ERROR_REPORTING=1",
		EnvAnthropicBaseURL+"="+plan.Env[EnvAnthropicBaseURL],
		EnvAnthropicAPIKey+"="+plan.Env[EnvAnthropicAPIKey],
	)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", "exec "+plan.Command+` -p --bare --dangerously-skip-permissions --output-format json 'Reply briefly.'`)
	cmd.Dir, cmd.Env = privateHome, env
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil || err != nil || !bytes.Contains(output, []byte("ok")) {
		sanitized := strings.NewReplacer(key, "[stored key]", "sk-ant-competing-fixture-key", "[competing key]", LoopbackClaudeAPIKeyPlaceholder, "[loopback key]", privateHome, "[private home]", plan.Env[EnvAnthropicBaseURL], "[loopback route]").Replace(string(output))
		t.Fatalf("managed Claude native fixture failed: exit=%v timeout=%t requests=%d output=%q", err, ctx.Err() != nil, messageCount-1, sanitized)
	}
	if messagePath != "/proxy/v1/messages" || messageModel != model || !keyMatches {
		t.Fatalf("native request ownership path=%q model=%q key_matches=%t", messagePath, messageModel, keyMatches)
	}
}
