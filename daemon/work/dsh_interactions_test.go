package work

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDSHInteractionExactSessionEpochAndNativeReceipt(t *testing.T) {
	owner := newDSHInteractionOwner("session-owned")
	owner.reset(true)
	frame := func(id, session, kind, extra string) {
		owner.consume([]byte(`{"type":"server-request","rpcId":"` + id + `","payload":{"type":"` + kind + `","sessionId":"` + session + `"` + extra + `}}`))
	}
	frame("foreign", "session-other", "approval/requested", `,"approvalId":"a"`)
	frame("approval", "session-owned", "approval/requested", `,"approvalId":"a","toolName":"bash","reason":"Write outside workspace"`)
	frame("question", "session-owned", "question/requested", `,"questions":[{"id":"q","question":"Choose","options":[{"label":"A"}]}]`)
	if len(owner.pending) != 2 {
		t.Fatal("foreign request admitted")
	}
	calls := 0
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Type   string `json:"type"`
			RPCID  string `json:"rpcId"`
			Result struct {
				OK    bool           `json:"ok"`
				Value map[string]any `json:"value"`
			} `json:"result"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.URL.Path != "/api/respond" || body.Type != "client-response" || body.Result.Value["sessionId"] != "session-owned" {
			t.Error("wrong native response ownership")
		}
		if body.RPCID == "approval" && (body.Result.Value["approvalId"] != "a" || body.Result.Value["outcome"] != "rejected") {
			t.Error("approval changed")
		}
		_, _ = w.Write([]byte(`{"accepted":true}`))
	}))
	defer native.Close()
	if err := owner.answer(context.Background(), native.URL, map[string]any{"epoch": "old", "rpcId": "approval", "outcome": "allowed-once"}); err == nil {
		t.Fatal("stale epoch accepted")
	}
	if calls != 0 {
		t.Fatal("stale answer crossed native boundary")
	}
	if err := owner.answer(context.Background(), native.URL, map[string]any{"epoch": owner.epoch, "rpcId": "question"}); err == nil {
		t.Fatal("empty question answer accepted")
	}
	if err := owner.answer(context.Background(), native.URL, map[string]any{"epoch": owner.epoch, "rpcId": "approval", "outcome": "allowed-always"}); err == nil {
		t.Fatal("unsupported approval accepted")
	}
	if calls != 0 {
		t.Fatal("invalid answer crossed native boundary")
	}
	if err := owner.answer(context.Background(), native.URL, map[string]any{"epoch": owner.epoch, "rpcId": "approval", "outcome": "rejected"}); err != nil {
		t.Fatal(err)
	}
	if err := owner.answer(context.Background(), native.URL, map[string]any{"epoch": owner.epoch, "rpcId": "approval", "outcome": "allowed-once"}); err == nil {
		t.Fatal("answered request reused")
	}
	frame("resolved", "session-owned", "question/resolved", `,"questionRpcId":"question"`)
	if len(owner.pending) != 0 {
		t.Fatal("resolved question retained")
	}
	frame("approval2", "session-owned", "approval/requested", `,"approvalId":"b"`)
	epoch := owner.epoch
	owner.reset(false)
	if owner.epoch == epoch || len(owner.pending) != 0 {
		t.Fatal("disconnect retained answer authority")
	}
}

func TestDSHQuestionAnswerAndNativeStaleRejection(t *testing.T) {
	owner := newDSHInteractionOwner("session-owned")
	owner.reset(true)
	owner.consume([]byte(`{"type":"server-request","rpcId":"question","payload":{"type":"question/requested","sessionId":"session-owned","questions":[{"id":"q","question":"Choose"}]}}`))
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		result := body["result"].(map[string]any)
		value := result["value"].(map[string]any)
		answer := value["answer"].(map[string]any)
		if len(answer["answers"].([]any)) != 1 {
			t.Error("question batch lost")
		}
		_, _ = w.Write([]byte(`{"accepted":false,"reason":"not-pending"}`))
	}))
	defer native.Close()
	err := owner.answer(context.Background(), native.URL, map[string]any{"epoch": owner.epoch, "rpcId": "question", "answer": map[string]any{"answers": []any{map[string]any{"id": "q", "selected": []string{"A"}}}}})
	if err == nil || len(owner.pending) != 0 {
		t.Fatal("native stale rejection not respected")
	}
}
