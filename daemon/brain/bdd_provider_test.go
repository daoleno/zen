package brain

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/modelprofiles"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

// This is a hybrid gate: real provider reasoning, real Store/Service persistence
// and event admission, but scripted Session transport. It is not a native CLI
// provider/watcher end-to-end proof. No tools, subprocess agents or live state.
func TestBDD_ZEN011_RealProviderDecision(t *testing.T) {
	if os.Getenv("ZEN_BDD_REAL_PROVIDER") != "1" {
		t.Skip("opt-in real provider: see docs/behavior-testing.md; not real-AI evidence when skipped")
	}
	endpoint, key, model, err := bddConfiguredProvider()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	call := bddProviderCaller(ctx, endpoint, key, model, os.Getenv("ZEN_BDD_PROTOCOL"))
	runBDDProviderDecision(t, call, "real-provider-hybrid")
}

func bddConfiguredProvider() (endpoint, key, model string, err error) {
	key, model = os.Getenv("ZEN_BDD_API_KEY"), os.Getenv("ZEN_BDD_MODEL")
	base := os.Getenv("ZEN_BDD_BASE_URL")
	if key == "" || model == "" || base == "" || os.Getenv("ZEN_BDD_MAX_CALLS") != "2" {
		return "", "", "", fmt.Errorf("environment_failure: require bound ZEN_BDD_BASE_URL, ZEN_BDD_API_KEY, ZEN_BDD_MODEL and ZEN_BDD_MAX_CALLS=2")
	}
	u, parseErr := url.Parse(base)
	if parseErr != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", "", "", fmt.Errorf("environment_failure: require credential-free HTTPS provider base URL")
	}
	path := ""
	switch os.Getenv("ZEN_BDD_PROTOCOL") {
	case "chat_completions":
		path = "/v1/chat/completions"
	case "responses":
		path = "/v1/responses"
	default:
		return "", "", "", fmt.Errorf("environment_failure: require explicit ZEN_BDD_PROTOCOL=chat_completions or responses")
	}
	endpoint, err = modelprofiles.UpstreamRequestURL(base, path)
	if err != nil {
		return "", "", "", fmt.Errorf("environment_failure: invalid provider base URL")
	}
	return endpoint, key, model, nil
}

type bddProviderCall func(string) (string, error)

// One request per invocation, at most two requests, no redirects or retries.
// Input bytes and output tokens are hard-capped independently of model pricing.
func bddProviderCaller(ctx context.Context, endpoint, key, model, protocol string) bddProviderCall {
	calls := 0
	client := modelprofiles.NewSafeHTTPClient(40 * time.Second)
	client.Timeout = 40 * time.Second
	return func(prompt string) (string, error) {
		if calls >= 2 || len(prompt) > 2048 {
			return "", fmt.Errorf("environment_failure: request budget exceeded")
		}
		calls++
		payload := map[string]any{
			"model": model, "messages": []map[string]string{{"role": "user", "content": prompt}},
			"max_tokens": 128, "stream": false,
		}
		if protocol == "responses" {
			payload = map[string]any{
				"model": model, "input": []map[string]string{{"role": "user", "content": prompt}},
				"max_output_tokens": 128, "stream": false, "store": false,
				"reasoning": map[string]string{"effort": "low"},
			}
		} else if protocol != "chat_completions" {
			return "", fmt.Errorf("environment_failure: unsupported protocol")
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return "", err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return "", fmt.Errorf("environment_failure: invalid endpoint")
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("provider_environment_failure: call %d request failed or timed out", calls)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", bddProviderFailure(resp, calls, key)
		}
		if protocol == "responses" {
			return bddResponsesText(resp.Body, calls)
		}
		var result struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 32768)).Decode(&result); err != nil {
			return "", fmt.Errorf("provider_environment_failure: invalid response envelope")
		}
		if len(result.Choices) != 1 || result.Choices[0].FinishReason != "stop" {
			return "", fmt.Errorf("provider_environment_failure: missing or truncated completion")
		}
		return result.Choices[0].Message.Content, nil
	}
}

// Only recognized diagnostic enums and bounded opaque request IDs can leave
// the response. Never log messages, raw bodies, arbitrary headers or secrets.
func bddProviderFailure(resp *http.Response, call int, key string) error {
	var body struct {
		Error struct {
			Code string `json:"code"`
			Type string `json:"type"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 32768)).Decode(&body)
	allowed := map[string]bool{"invalid_api_key": true, "authentication_error": true, "permission_denied": true, "permission_error": true, "access_denied": true, "insufficient_permissions": true, "model_not_found": true, "rate_limit_exceeded": true, "rate_limit_error": true, "insufficient_quota": true, "invalid_request_error": true, "unsupported_parameter": true, "unsupported_value": true}
	evidence := map[string]any{"http_status": resp.StatusCode, "call": call, "retried": false}
	for name, value := range map[string]string{"error_code": body.Error.Code, "error_type": body.Error.Type} {
		if allowed[value] && !strings.Contains(value, key) {
			evidence[name] = value
		}
	}
	id := resp.Header.Get("x-request-id")
	if bddRequestID.MatchString(id) && !strings.Contains(id, key) {
		evidence["request_id"] = id
	}
	raw, _ := json.Marshal(evidence)
	return fmt.Errorf("provider_environment_failure: %s", raw)
}

var bddRequestID = regexp.MustCompile(`^(req_[A-Za-z0-9_-]{8,80}|[a-fA-F0-9-]{16,64}|[0-9]{14,32}[A-Za-z0-9]{0,40})$`)

func bddResponsesText(body io.Reader, call int) (string, error) {
	var response struct {
		Status string          `json:"status"`
		Error  json.RawMessage `json:"error"`
		Output []struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Status  string `json:"status"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.NewDecoder(io.LimitReader(body, 32768)).Decode(&response); err != nil || response.Status != "completed" || (len(response.Error) > 0 && string(response.Error) != "null") {
		return "", fmt.Errorf("provider_environment_failure: call %d invalid or incomplete Responses envelope", call)
	}
	var output strings.Builder
	for _, item := range response.Output {
		if item.Type == "reasoning" {
			continue
		}
		if item.Type != "message" || item.Role != "assistant" || item.Status != "completed" {
			return "", fmt.Errorf("provider_environment_failure: call %d unexpected Responses output", call)
		}
		for _, content := range item.Content {
			if content.Type != "output_text" {
				return "", fmt.Errorf("behavior_failure: call %d Responses refusal or non-text output", call)
			}
			output.WriteString(content.Text)
		}
	}
	if strings.TrimSpace(output.String()) == "" {
		return "", fmt.Errorf("provider_environment_failure: call %d missing Responses text", call)
	}
	return output.String(), nil
}

func runBDDProviderDecision(t *testing.T, call bddProviderCall, evidenceKind string) {
	t.Helper()
	n, err := rand.Int(rand.Reader, big.NewInt(900))
	if err != nil {
		t.Fatal(err)
	}
	a, b := int(n.Int64())+100, 37
	// Given a fresh objective with an independently computed result oracle.
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession("host", "codex"); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{Title: "bounded provider decision", Objective: fmt.Sprintf("Compute %d + %d", a, b)})
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{turnStore: store, outcomes: map[string]watcher.InputOutcome{}, sessions: map[string]*classifier.Worker{
		"host": {ID: "host", Hidden: true, State: classifier.StateDone}, "worker": {ID: "worker", Delegated: true, State: classifier.StateDone},
		"user": {ID: "user", State: classifier.StateDone},
	}}
	service := NewService(store, fw, nil)
	if _, err := fw.SubmitDelegatedWorkInput("worker", item.Objective, item.ID, "provider-turn", "", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	output, err := call(item.Objective + `. Return only a JSON object with integer field "sum".`)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Sum int `json:"sum"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil || result.Sum != a+b {
		t.Fatal("behavior_failure: Worker result disagrees with independent arithmetic oracle")
	}
	// When the actual result is persisted, its producer event admits a review.
	summary := fmt.Sprintf("sum=%d", result.Sum)
	id, changes := service.SubscribeWork()
	defer service.UnsubscribeWork(id)
	if report, err := service.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: "worker", TurnID: "provider-turn", Class: watcher.EvidenceControl, Kind: "done", SourceID: "provider-result", Summary: summary, At: time.Now()}); err != nil || !report.Changed {
		t.Fatalf("behavior_failure: report %+v %v", report, err)
	}
	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("behavior_failure: missing producer wake")
	}
	if err := service.ReconcileWorkChange(); err != nil {
		t.Fatal(err)
	}
	lease := requireReviewDelivered(t, store, item.ID)
	delivered := ""
	for _, sent := range fw.sentCalls {
		if input, ok := work.ParseCanonicalDirectWorkEventInput(sent.text); ok && input.WorkID == item.ID {
			delivered = input.Summary
		}
	}
	if delivered != summary || !fw.HasSession("worker") {
		t.Fatal("behavior_failure: delivery or pre-decision ownership")
	}
	decisionText, err := call(fmt.Sprintf(`Objective: %s. Delivered Worker result: %s. Independently verify the arithmetic. Return JSON {"disposition":"complete"} only if correct, otherwise {"disposition":"continue"}.`, item.Objective, delivered))
	if err != nil {
		t.Fatal(err)
	}
	var decision struct {
		Disposition string `json:"disposition"`
	}
	if err := json.Unmarshal([]byte(decisionText), &decision); err != nil || decision.Disposition != "complete" {
		t.Fatal("behavior_failure: Brain did not accept independently verified complete result")
	}
	// Then a model decision, not provider exit or its claim of PASS, closes Work.
	if _, _, err := service.ResolveWorkReview(WorkReviewDispositionRequest{WorkID: item.ID, HandlingID: lease.HandlingID, ProviderTurnID: lease.ProviderTurnID, ExpectedWorkRevision: lease.DeliveryWorkRevision, Disposition: WorkDispositionComplete}); err != nil {
		t.Fatal(err)
	}
	if fw.HasSession("worker") || !fw.HasSession("user") || !fw.HasSession("host") {
		t.Fatal("behavior_failure: exact owned cleanup")
	}
	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := reopened.Work(item.ID)
	if err != nil || closed.Status != WorkDone || closed.Review != nil {
		t.Fatal("behavior_failure: decision not durable")
	}
	if len(fw.sentCalls) != 2 {
		t.Fatal("behavior_failure: business input or review replayed")
	}
	evidence, _ := json.Marshal(map[string]any{"scenario": "ZEN011", "evidence_kind": evidenceKind, "outcome": "pass", "calls": 2, "oracle_checked": true, "event_delivered": true, "decision_durable": true, "owned_session_removed": true, "unrelated_sessions_preserved": true})
	t.Log(string(evidence))
}

func TestBDD_ZEN012_ProviderPathBudgetAndFailures(t *testing.T) {
	t.Run("responses-loop", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			var request struct {
				Model string `json:"model"`
				Input []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"input"`
				MaxOutputTokens int   `json:"max_output_tokens"`
				Store           *bool `json:"store"`
				Stream          *bool `json:"stream"`
				Reasoning       struct {
					Effort string `json:"effort"`
				} `json:"reasoning"`
				Messages json.RawMessage `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Model != "scripted-model" || len(request.Input) != 1 || request.Input[0].Role != "user" || request.MaxOutputTokens != 128 || request.Store == nil || *request.Store || request.Stream == nil || *request.Stream || request.Reasoning.Effort != "low" || len(request.Messages) != 0 || r.Header.Get("Authorization") != "Bearer test-key" {
				t.Error("incorrect Responses request/auth contract")
				w.WriteHeader(400)
				return
			}
			content := `{"disposition":"complete"}`
			if strings.HasPrefix(request.Input[0].Content, "Compute ") {
				var a, b int
				if _, err := fmt.Sscanf(request.Input[0].Content, "Compute %d + %d", &a, &b); err != nil {
					t.Error(err)
				}
				content = fmt.Sprintf(`{"sum":%d}`, a+b)
			}
			json.NewEncoder(w).Encode(map[string]any{"status": "completed", "output": []any{map[string]any{"type": "reasoning"}, map[string]any{"type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]string{"type": "output_text", "text": content}}}}})
		}))
		defer server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		call := bddProviderCaller(ctx, server.URL, "test-key", "scripted-model", "responses")
		runBDDProviderDecision(t, call, "scripted-responses-not-AI")
		if _, err := call("third request"); err == nil || calls.Load() != 2 {
			t.Fatal("Responses call budget violated")
		}
	})
	t.Run("responses-incomplete-and-refusal", func(t *testing.T) {
		for _, body := range []string{`{"status":"incomplete","output":[]}`, `{"status":"completed","output":[]}`, `{"status":"completed","error":{"code":"server_error"}}`, `{"status":"completed","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"refusal","text":"secret"}]}]}`} {
			if _, err := bddResponsesText(strings.NewReader(body), 1); err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatal("incomplete/refused output accepted or exposed")
			}
		}
	})
	t.Run("safe-failure-evidence", func(t *testing.T) {
		resp := &http.Response{StatusCode: 403, Header: http.Header{"X-Request-Id": []string{"req_safe12345678"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"permission_denied","type":"permission_error","message":"private-body test-secret"}}`))}
		err := bddProviderFailure(resp, 1, "test-secret")
		if !strings.Contains(err.Error(), `"http_status":403`) || !strings.Contains(err.Error(), `"call":1`) || !strings.Contains(err.Error(), "permission_denied") || !strings.Contains(err.Error(), "req_safe12345678") || strings.Contains(err.Error(), "private-body") || strings.Contains(err.Error(), "test-secret") {
			t.Fatalf("unsafe/incomplete diagnostic: %v", err)
		}
		resp.Header.Set("X-Request-Id", "req_test-secret1234")
		resp.Body = io.NopCloser(strings.NewReader(`{"error":{"code":"test-secret","type":"private-body"}}`))
		err = bddProviderFailure(resp, 2, "test-secret")
		if strings.Contains(err.Error(), "test-secret") || strings.Contains(err.Error(), "private-body") || strings.Contains(err.Error(), "request_id") {
			t.Fatal("untrusted metadata leaked")
		}
	})
	t.Run("bound-configuration", func(t *testing.T) {
		t.Setenv("ZEN_BDD_API_KEY", "test-key")
		t.Setenv("ZEN_BDD_MODEL", "configured-chat-model")
		t.Setenv("ZEN_BDD_MAX_CALLS", "2")
		t.Setenv("ZEN_BDD_PROTOCOL", "chat_completions")
		for _, base := range []string{"https://provider.example", "https://provider.example/v1"} {
			t.Setenv("ZEN_BDD_BASE_URL", base)
			endpoint, key, model, err := bddConfiguredProvider()
			if err != nil || endpoint != "https://provider.example/v1/chat/completions" || key != "test-key" || model != "configured-chat-model" {
				t.Fatal("configured endpoint/model/credential binding failed")
			}
		}
		for _, base := range []string{"", "http://provider.example", "https://key@provider.example", "https://provider.example?key=secret"} {
			t.Setenv("ZEN_BDD_BASE_URL", base)
			if _, _, _, err := bddConfiguredProvider(); err == nil {
				t.Fatal("unsafe or missing binding accepted")
			}
		}
		t.Setenv("ZEN_BDD_BASE_URL", "https://provider.example/v1")
		t.Setenv("ZEN_BDD_PROTOCOL", "responses")
		if endpoint, _, _, err := bddConfiguredProvider(); err != nil || endpoint != "https://provider.example/v1/responses" {
			t.Fatal("Responses binding failed")
		}
		t.Setenv("ZEN_BDD_PROTOCOL", "unknown")
		if _, _, _, err := bddConfiguredProvider(); err == nil {
			t.Fatal("unknown protocol accepted")
		}
	})
	for _, mode := range []string{"scripted-loop", "rate-limit", "truncated", "invalid-envelope", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				var request struct {
					MaxTokens int `json:"max_tokens"`
					Messages  []struct {
						Content string `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.MaxTokens != 128 || len(request.Messages) != 1 {
					t.Error("request budget/shape invalid")
					w.WriteHeader(400)
					return
				}
				if mode == "timeout" {
					<-r.Context().Done()
					return
				}
				if mode == "rate-limit" {
					w.WriteHeader(429)
					return
				}
				if mode == "invalid-envelope" {
					fmt.Fprint(w, "invalid")
					return
				}
				finish, content := "stop", `{"disposition":"complete"}`
				if mode == "truncated" {
					finish = "length"
				}
				if strings.HasPrefix(request.Messages[0].Content, "Compute ") {
					var a, b int
					if _, err := fmt.Sscanf(request.Messages[0].Content, "Compute %d + %d", &a, &b); err != nil {
						t.Error(err)
					}
					content = fmt.Sprintf(`{"sum":%d}`, a+b)
				}
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}, "finish_reason": finish}}})
			}))
			defer server.Close()
			timeout := 5 * time.Second
			if mode == "timeout" {
				timeout = 100 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			call := bddProviderCaller(ctx, server.URL, "test-key", "scripted-model", "chat_completions")
			if mode == "scripted-loop" {
				runBDDProviderDecision(t, call, "scripted-not-AI")
				if _, err := call("third request"); err == nil {
					t.Fatal("call cap not enforced")
				}
				if requests.Load() != 2 {
					t.Fatalf("calls=%d", requests.Load())
				}
			} else {
				if _, err := call("test"); err == nil || !strings.Contains(err.Error(), "provider_environment_failure") {
					t.Fatalf("failure misclassified: %v", err)
				}
				if requests.Load() > 1 {
					t.Fatalf("unexpected retry: %d", requests.Load())
				}
			}
		})
	}
}
