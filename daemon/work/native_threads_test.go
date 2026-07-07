package work

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeCodexAppServerCaller struct {
	method string
	params any
	result any
}

func (f *fakeCodexAppServerCaller) Call(_ context.Context, method string, params any, result any) error {
	f.method = method
	f.params = params
	raw, err := json.Marshal(f.result)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, result)
}

func TestCodexAppServerThreadProviderMapsListThreads(t *testing.T) {
	name := "Architecture review"
	parent := "thread-parent"
	archived := true
	fake := &fakeCodexAppServerCaller{
		result: codexThreadListResponse{
			Data: []codexThread{{
				ID:            "thread-1",
				SessionID:     "session-1",
				ForkedFromID:  &parent,
				Preview:       "Review Brain executor direction\nwith details",
				Ephemeral:     true,
				ModelProvider: "openai",
				CreatedAt:     1780331643,
				UpdatedAt:     1780333776,
				Status:        json.RawMessage(`{"type":"notLoaded"}`),
				Path:          "/home/user/.codex/sessions/rollout.jsonl",
				Cwd:           "/repo/zen",
				Source:        "cli",
				Name:          &name,
			}},
			NextCursor:      "next",
			BackwardsCursor: "back",
		},
	}
	provider := &CodexAppServerThreadProvider{client: fake}

	page, err := provider.ListThreads(context.Background(), NativeThreadListOptions{
		Cursor:         "cursor",
		Limit:          12,
		Cwd:            "/repo/zen",
		SearchTerm:     "Brain",
		SortKey:        "updated_at",
		SortDirection:  "desc",
		Archived:       &archived,
		UseStateDBOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.method != "thread/list" {
		t.Fatalf("method = %q", fake.method)
	}
	params, ok := fake.params.(map[string]any)
	if !ok {
		t.Fatalf("params = %#v", fake.params)
	}
	for key, want := range map[string]any{
		"cursor":         "cursor",
		"limit":          12,
		"cwd":            "/repo/zen",
		"searchTerm":     "Brain",
		"sortKey":        "updated_at",
		"sortDirection":  "desc",
		"archived":       true,
		"useStateDbOnly": true,
	} {
		if got := params[key]; got != want {
			t.Fatalf("param %s = %#v, want %#v", key, got, want)
		}
	}
	if page.NextCursor != "next" || page.BackwardsCursor != "back" {
		t.Fatalf("cursors = %#v", page)
	}
	if len(page.Threads) != 1 {
		t.Fatalf("threads = %#v", page.Threads)
	}
	thread := page.Threads[0]
	if thread.ID != "codex:thread-1" || thread.NativeID != "thread-1" || thread.Provider != AgentProviderCodex {
		t.Fatalf("thread ids = %+v", thread)
	}
	if thread.Title != "Architecture review" || thread.Preview != "Review Brain executor direction\nwith details" {
		t.Fatalf("thread text = %+v", thread)
	}
	if thread.SessionID != "session-1" || thread.ForkedFromID != "thread-parent" || thread.Status != "notLoaded" {
		t.Fatalf("thread metadata = %+v", thread)
	}
	if !thread.Ephemeral || !thread.Archived || thread.ModelProvider != "openai" || thread.Source != "cli" {
		t.Fatalf("thread flags = %+v", thread)
	}
	if thread.CreatedAt == nil || !thread.CreatedAt.Equal(time.Unix(1780331643, 0).UTC()) {
		t.Fatalf("created_at = %v", thread.CreatedAt)
	}
	if thread.UpdatedAt == nil || !thread.UpdatedAt.Equal(time.Unix(1780333776, 0).UTC()) {
		t.Fatalf("updated_at = %v", thread.UpdatedAt)
	}
}

func TestCodexAppServerThreadProviderSearchesThreads(t *testing.T) {
	name := "Brain orchestration"
	fake := &fakeCodexAppServerCaller{
		result: codexThreadSearchResponse{
			Data: []codexThreadSearchResult{{
				Thread: codexThread{
					ID:            "thread-1",
					SessionID:     "session-1",
					Preview:       "Original thread preview",
					ModelProvider: "openai",
					CreatedAt:     1780331643,
					UpdatedAt:     1780333776,
					Status:        json.RawMessage(`{"type":"notLoaded"}`),
					Path:          "/home/user/.codex/sessions/rollout.jsonl",
					Cwd:           "/repo/zen",
					Source:        "cli",
					Name:          &name,
				},
				Snippet: "...Brain match snippet...",
			}},
			NextCursor:      "next-search",
			BackwardsCursor: "back-search",
		},
	}
	provider := &CodexAppServerThreadProvider{client: fake}

	page, err := provider.SearchThreads(context.Background(), NativeThreadSearchOptions{
		Cursor:     "cursor",
		Limit:      4,
		Cwd:        "/repo/zen",
		SearchTerm: "Brain",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.method != "thread/search" {
		t.Fatalf("method = %q", fake.method)
	}
	params, ok := fake.params.(map[string]any)
	if !ok {
		t.Fatalf("params = %#v", fake.params)
	}
	for key, want := range map[string]any{
		"cursor":     "cursor",
		"limit":      4,
		"cwd":        "/repo/zen",
		"searchTerm": "Brain",
	} {
		if got := params[key]; got != want {
			t.Fatalf("param %s = %#v, want %#v", key, got, want)
		}
	}
	if page.NextCursor != "next-search" || page.BackwardsCursor != "back-search" {
		t.Fatalf("cursors = %#v", page)
	}
	if len(page.Threads) != 1 {
		t.Fatalf("threads = %#v", page.Threads)
	}
	thread := page.Threads[0]
	if thread.ID != "codex:thread-1" || thread.Title != "Brain orchestration" {
		t.Fatalf("thread = %+v", thread)
	}
	if thread.Snippet != "...Brain match snippet..." || thread.Preview != "Original thread preview" {
		t.Fatalf("thread text = %+v", thread)
	}
}

func TestCodexAppServerThreadProviderStartsThread(t *testing.T) {
	fake := &fakeCodexAppServerCaller{
		result: codexThreadReadResponse{
			Thread: codexThread{
				ID:        "thread-new",
				SessionID: "session-new",
				Preview:   "New Brain thread",
				Status:    json.RawMessage(`{"type":"idle"}`),
				Cwd:       "/repo/zen",
				Source:    "appServer",
			},
		},
	}
	provider := &CodexAppServerThreadProvider{client: fake}

	thread, err := provider.StartThread(context.Background(), NativeThreadStartOptions{
		Cwd:                   "/repo/zen",
		Model:                 "gpt-5-codex",
		ModelProvider:         "openai",
		DeveloperInstructions: "Keep Brain executor-neutral.",
		Ephemeral:             true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.method != "thread/start" {
		t.Fatalf("method = %q", fake.method)
	}
	params := fake.params.(map[string]any)
	for key, want := range map[string]any{
		"cwd":                   "/repo/zen",
		"model":                 "gpt-5-codex",
		"modelProvider":         "openai",
		"developerInstructions": "Keep Brain executor-neutral.",
		"ephemeral":             true,
	} {
		if got := params[key]; got != want {
			t.Fatalf("param %s = %#v, want %#v", key, got, want)
		}
	}
	if thread.ID != "codex:thread-new" || thread.Status != "idle" || thread.Cwd != "/repo/zen" {
		t.Fatalf("thread = %+v", thread)
	}
}

func TestCodexAppServerThreadProviderResumesThread(t *testing.T) {
	fake := &fakeCodexAppServerCaller{
		result: codexThreadReadResponse{
			Thread: codexThread{
				ID:        "thread-1",
				SessionID: "session-1",
				Preview:   "Resumed Brain thread",
				Status:    json.RawMessage(`{"type":"idle"}`),
				Cwd:       "/repo/zen",
				Source:    "appServer",
			},
		},
	}
	provider := &CodexAppServerThreadProvider{client: fake}

	thread, err := provider.ResumeThread(context.Background(), "codex:thread-1", NativeThreadResumeOptions{
		Cwd:           "/repo/zen",
		Model:         "gpt-5-codex",
		ModelProvider: "openai",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.method != "thread/resume" {
		t.Fatalf("method = %q", fake.method)
	}
	params := fake.params.(map[string]any)
	for key, want := range map[string]any{
		"threadId":      "thread-1",
		"cwd":           "/repo/zen",
		"model":         "gpt-5-codex",
		"modelProvider": "openai",
	} {
		if got := params[key]; got != want {
			t.Fatalf("param %s = %#v, want %#v", key, got, want)
		}
	}
	if thread.ID != "codex:thread-1" || thread.Status != "idle" || thread.Cwd != "/repo/zen" {
		t.Fatalf("thread = %+v", thread)
	}
}

func TestNativeThreadRuntimeResumeLaunchBuildsCodexTmuxCommand(t *testing.T) {
	executor := NewAgentExecutor("codex", Executor{
		Name:    "codex",
		Command: "/opt/bin/codex --dangerously-bypass-approvals-and-sandbox",
	})
	thread := NativeThread{
		ID:       "codex:thread-1",
		NativeID: "thread-1",
		Provider: AgentProviderCodex,
		Cwd:      "/repo/zen",
	}

	launch, ok := NativeThreadRuntimeResumeLaunch(executor, thread, NativeThreadResumeOptions{
		Cwd: "/fallback/repo",
	}, "/brain/workspace")
	if !ok {
		t.Fatal("expected codex runtime resume launch")
	}
	if launch.Cwd != "/repo/zen" {
		t.Fatalf("cwd = %q", launch.Cwd)
	}
	if !strings.Contains(launch.Command, "/opt/bin/codex --dangerously-bypass-approvals-and-sandbox resume 'thread-1'") {
		t.Fatalf("command = %q", launch.Command)
	}
	if !strings.Contains(launch.Command, "--no-alt-screen") || !strings.Contains(launch.Command, "-C '/repo/zen'") {
		t.Fatalf("command = %q", launch.Command)
	}
}

func TestNativeThreadRuntimeResumeLaunchUsesOptionsCwdWhenThreadHasNone(t *testing.T) {
	executor := NewAgentExecutor("codex", Executor{Name: "codex", Command: "codex"})
	thread := NativeThread{
		ID:       "codex:thread-1",
		Provider: AgentProviderCodex,
	}

	launch, ok := NativeThreadRuntimeResumeLaunch(executor, thread, NativeThreadResumeOptions{
		Cwd: "/repo/from-options",
	}, "/brain/workspace")
	if !ok {
		t.Fatal("expected codex runtime resume launch")
	}
	if launch.Cwd != "/repo/from-options" || !strings.Contains(launch.Command, "-C '/repo/from-options'") {
		t.Fatalf("launch = %+v", launch)
	}
}

func TestCodexAppServerThreadProviderReadsGoal(t *testing.T) {
	budget := int64(12000)
	fake := &fakeCodexAppServerCaller{
		result: codexThreadGoalGetResponse{
			Goal: &codexThreadGoal{
				ThreadID:        "thread-1",
				Objective:       "Keep Brain portable",
				Status:          "active",
				TokenBudget:     &budget,
				TokensUsed:      300,
				TimeUsedSeconds: 42,
				CreatedAt:       1780331643,
				UpdatedAt:       1780333776,
			},
		},
	}
	provider := &CodexAppServerThreadProvider{client: fake}

	goal, err := provider.GetGoal(context.Background(), "codex:thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if fake.method != "thread/goal/get" {
		t.Fatalf("method = %q", fake.method)
	}
	params := fake.params.(map[string]any)
	if params["threadId"] != "thread-1" {
		t.Fatalf("thread id param = %#v", params["threadId"])
	}
	if goal == nil || goal.ThreadID != "codex:thread-1" || goal.Objective != "Keep Brain portable" || goal.Status != "active" {
		t.Fatalf("goal = %+v", goal)
	}
	if goal.TokenBudget == nil || *goal.TokenBudget != 12000 || goal.TokensUsed != 300 || goal.TimeUsedSeconds != 42 {
		t.Fatalf("goal usage = %+v", goal)
	}
}

func TestCodexAppServerThreadProviderReadsThread(t *testing.T) {
	name := "Readable Brain thread"
	fake := &fakeCodexAppServerCaller{
		result: codexThreadReadResponse{
			Thread: codexThread{
				ID:            "thread-1",
				SessionID:     "session-1",
				Preview:       "Thread preview",
				Ephemeral:     false,
				ModelProvider: "openai",
				CreatedAt:     1780331643,
				UpdatedAt:     1780333776,
				Status:        json.RawMessage(`{"type":"active"}`),
				Path:          "/repo/zen/.codex/thread.json",
				Cwd:           "/repo/zen",
				Source:        "cli",
				Name:          &name,
			},
		},
	}
	provider := &CodexAppServerThreadProvider{client: fake}

	thread, err := provider.ReadThread(context.Background(), "codex:thread-1", NativeThreadReadOptions{IncludeTurns: true})
	if err != nil {
		t.Fatal(err)
	}
	if fake.method != "thread/read" {
		t.Fatalf("method = %q", fake.method)
	}
	params := fake.params.(map[string]any)
	if params["threadId"] != "thread-1" || params["includeTurns"] != true {
		t.Fatalf("params = %#v", params)
	}
	if thread.ID != "codex:thread-1" || thread.NativeID != "thread-1" || thread.Title != "Readable Brain thread" {
		t.Fatalf("thread = %+v", thread)
	}
	if thread.Status != "active" || thread.SessionID != "session-1" || thread.Cwd != "/repo/zen" || thread.Path != "/repo/zen/.codex/thread.json" {
		t.Fatalf("thread metadata = %+v", thread)
	}
}

func TestCodexAppServerThreadProviderForksThread(t *testing.T) {
	fake := &fakeCodexAppServerCaller{
		result: codexThreadReadResponse{
			Thread: codexThread{
				ID:        "forked-thread",
				SessionID: "forked-session",
				Preview:   "Forked Brain work",
				Status:    json.RawMessage(`{"type":"idle"}`),
				Cwd:       "/repo/zen",
				Source:    "appServer",
			},
		},
	}
	provider := &CodexAppServerThreadProvider{client: fake}

	thread, err := provider.ForkThread(context.Background(), "codex:thread-1", NativeThreadForkOptions{
		Cwd:                   "/repo/zen",
		Model:                 "gpt-5-codex",
		ModelProvider:         "openai",
		DeveloperInstructions: "Keep Brain executor-neutral.",
		Ephemeral:             true,
		ExcludeTurns:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.method != "thread/fork" {
		t.Fatalf("method = %q", fake.method)
	}
	params := fake.params.(map[string]any)
	for key, want := range map[string]any{
		"threadId":              "thread-1",
		"cwd":                   "/repo/zen",
		"model":                 "gpt-5-codex",
		"modelProvider":         "openai",
		"developerInstructions": "Keep Brain executor-neutral.",
		"ephemeral":             true,
		"excludeTurns":          true,
	} {
		if got := params[key]; got != want {
			t.Fatalf("param %s = %#v, want %#v", key, got, want)
		}
	}
	if thread.ID != "codex:forked-thread" || thread.Status != "idle" || thread.Cwd != "/repo/zen" {
		t.Fatalf("thread = %+v", thread)
	}
}

func TestCodexAppServerThreadProviderSetsAndClearsGoal(t *testing.T) {
	budget := int64(8000)
	setFake := &fakeCodexAppServerCaller{
		result: codexThreadGoalSetResponse{
			Goal: codexThreadGoal{
				ThreadID:    "thread-1",
				Objective:   "Ship portable Brain",
				Status:      "active",
				TokenBudget: &budget,
				CreatedAt:   1780331643,
				UpdatedAt:   1780333776,
			},
		},
	}
	provider := &CodexAppServerThreadProvider{client: setFake}
	goal, err := provider.SetGoal(context.Background(), "codex:thread-1", NativeThreadGoalUpdate{
		Objective:   "Ship portable Brain",
		Status:      "active",
		TokenBudget: &budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	if setFake.method != "thread/goal/set" {
		t.Fatalf("method = %q", setFake.method)
	}
	params := setFake.params.(map[string]any)
	if params["threadId"] != "thread-1" || params["objective"] != "Ship portable Brain" || params["status"] != "active" || params["tokenBudget"] != int64(8000) {
		t.Fatalf("params = %#v", params)
	}
	if goal.ThreadID != "codex:thread-1" || goal.Objective != "Ship portable Brain" {
		t.Fatalf("goal = %+v", goal)
	}

	clearFake := &fakeCodexAppServerCaller{result: codexThreadGoalClearResponse{Cleared: true}}
	provider = &CodexAppServerThreadProvider{client: clearFake}
	cleared, err := provider.ClearGoal(context.Background(), "codex:thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if !cleared || clearFake.method != "thread/goal/clear" {
		t.Fatalf("cleared = %v method = %q", cleared, clearFake.method)
	}
}

func TestNativeThreadProviderFactoryOnlyEnablesCodex(t *testing.T) {
	codex := NewAgentExecutor("codex", Executor{Name: "codex", Command: "codex"})
	if provider, ok := NewNativeThreadProvider(codex); !ok || provider.ProviderID() != AgentProviderCodex {
		t.Fatalf("codex provider = (%#v, %v)", provider, ok)
	}
	claude := NewAgentExecutor("claude", Executor{Name: "claude", Command: "claude"})
	if provider, ok := NewNativeThreadProvider(claude); ok || provider != nil {
		t.Fatalf("claude provider = (%#v, %v)", provider, ok)
	}
}

func TestCodexAppServerClientSpeaksJSONRPCOverStdio(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake-codex")
	body := `#!/bin/sh
set -eu
read init
case "$init" in
  *'"method":"initialize"'*) ;;
  *) echo "bad initialize: $init" >&2; exit 11 ;;
esac
printf '%s\n' '{"id":"zen-init","result":{"userAgent":"fake","codexHome":"/tmp/codex","platformFamily":"unix","platformOs":"linux"}}'
printf '%s\n' '{"method":"remoteControl/status/changed","params":{"status":"disabled"}}'
read ready
case "$ready" in
  *'"method":"initialized"'*) ;;
  *) echo "bad initialized: $ready" >&2; exit 12 ;;
esac
read req
case "$req" in
  *'"method":"thread/list"'*) ;;
  *) echo "bad request: $req" >&2; exit 13 ;;
esac
printf '%s\n' '{"id":"zen-1","result":{"data":[],"nextCursor":"next","backwardsCursor":"back"}}'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	client := &CodexAppServerClient{Binary: script, Timeout: 3 * time.Second}

	var response codexThreadListResponse
	if err := client.Call(context.Background(), "thread/list", map[string]any{"limit": 1}, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data) != 0 || response.NextCursor != "next" || response.BackwardsCursor != "back" {
		t.Fatalf("response = %+v", response)
	}
}

func TestCodexBinaryFromCommandUsesConfiguredBinary(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    string
	}{
		{"", "codex"},
		{"codex --no-alt-screen", "codex"},
		{"/opt/bin/codex -C /repo", "/opt/bin/codex"},
		{`"/opt/bin/codex" --profile work`, "/opt/bin/codex"},
	} {
		got := codexBinaryFromCommand(tc.command)
		if got != tc.want {
			t.Fatalf("codexBinaryFromCommand(%q) = %q, want %q", tc.command, got, tc.want)
		}
		if strings.TrimSpace(got) == "" {
			t.Fatalf("empty binary for %q", tc.command)
		}
	}
}
