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
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
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
	key, model := os.Getenv("DEEPSEEK_API_KEY"), os.Getenv("ZEN_BDD_MODEL")
	if key == "" || model == "" || os.Getenv("ZEN_BDD_MAX_CALLS") != "2" {
		t.Fatal("environment_failure: require DEEPSEEK_API_KEY, explicit ZEN_BDD_MODEL, ZEN_BDD_MAX_CALLS=2")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	call := bddProviderCaller(ctx, "https://api.deepseek.com/chat/completions", key, model)
	runBDDProviderDecision(t, call, "real-provider-hybrid")
}

type bddProviderCall func(string) (string, error)

// One request per invocation, at most two requests, no redirects or retries.
// Input bytes and output tokens are hard-capped independently of model pricing.
func bddProviderCaller(ctx context.Context, endpoint, key, model string) bddProviderCall {
	calls := 0
	client := &http.Client{Timeout: 40 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return func(prompt string) (string, error) {
		if calls >= 2 || len(prompt) > 2048 {
			return "", fmt.Errorf("environment_failure: request budget exceeded")
		}
		calls++
		body, err := json.Marshal(map[string]any{
			"model": model, "messages": []map[string]string{{"role": "user", "content": prompt}},
			"max_tokens": 128, "stream": false, "response_format": map[string]string{"type": "json_object"},
		})
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
			return "", fmt.Errorf("provider_environment_failure: request failed or timed out")
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("provider_environment_failure: HTTP %d (not retried)", resp.StatusCode)
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
			call := bddProviderCaller(ctx, server.URL, "test-key", "scripted-model")
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
