package work

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

const defaultCodexAppServerTimeout = 15 * time.Second

// NativeThreadProvider is the executor boundary for tool-owned thread APIs.
// Brain should consume these portable records instead of depending on a
// provider-specific schema.
type NativeThreadProvider interface {
	ProviderID() string
	StartThread(context.Context, NativeThreadStartOptions) (NativeThread, error)
	ResumeThread(context.Context, string, NativeThreadResumeOptions) (NativeThread, error)
	ListThreads(context.Context, NativeThreadListOptions) (NativeThreadPage, error)
	SearchThreads(context.Context, NativeThreadSearchOptions) (NativeThreadPage, error)
	ReadThread(context.Context, string, NativeThreadReadOptions) (NativeThread, error)
	ForkThread(context.Context, string, NativeThreadForkOptions) (NativeThread, error)
	ArchiveThread(context.Context, string, bool) (NativeThread, error)
	GetGoal(context.Context, string) (*NativeThreadGoal, error)
	SetGoal(context.Context, string, NativeThreadGoalUpdate) (NativeThreadGoal, error)
	ClearGoal(context.Context, string) (bool, error)
}

type NativeThreadListOptions struct {
	Cursor         string
	Limit          int
	Cwd            string
	SearchTerm     string
	SortKey        string
	SortDirection  string
	Archived       *bool
	UseStateDBOnly bool
}

type NativeThreadStartOptions struct {
	Cwd                   string
	Model                 string
	ModelProvider         string
	DeveloperInstructions string
	BaseInstructions      string
	Ephemeral             bool
}

type NativeThreadResumeOptions struct {
	Cwd                   string
	Model                 string
	ModelProvider         string
	DeveloperInstructions string
	BaseInstructions      string
}

type NativeThreadSearchOptions struct {
	Cursor     string
	Limit      int
	Cwd        string
	SearchTerm string
}

type NativeThreadReadOptions struct {
	IncludeTurns bool
}

type NativeThreadForkOptions struct {
	Cwd                   string
	Model                 string
	ModelProvider         string
	DeveloperInstructions string
	BaseInstructions      string
	Ephemeral             bool
	ExcludeTurns          bool
}

type NativeThreadGoalUpdate struct {
	Objective   string
	Status      string
	TokenBudget *int64
}

type NativeThreadPage struct {
	Threads         []NativeThread `json:"threads"`
	NextCursor      string         `json:"next_cursor,omitempty"`
	BackwardsCursor string         `json:"backwards_cursor,omitempty"`
}

type NativeThread struct {
	ID            string     `json:"id"`
	NativeID      string     `json:"native_id,omitempty"`
	Provider      string     `json:"provider,omitempty"`
	SessionID     string     `json:"session_id,omitempty"`
	ForkedFromID  string     `json:"forked_from_id,omitempty"`
	Title         string     `json:"title,omitempty"`
	Preview       string     `json:"preview,omitempty"`
	Snippet       string     `json:"snippet,omitempty"`
	Status        string     `json:"status,omitempty"`
	Cwd           string     `json:"cwd,omitempty"`
	Path          string     `json:"path,omitempty"`
	Source        string     `json:"source,omitempty"`
	ModelProvider string     `json:"model_provider,omitempty"`
	Ephemeral     bool       `json:"ephemeral,omitempty"`
	Archived      bool       `json:"archived,omitempty"`
	Pinned        bool       `json:"pinned,omitempty"`
	ReviewState   string     `json:"review_state,omitempty"`
	CreatedAt     *time.Time `json:"created_at,omitempty"`
	UpdatedAt     *time.Time `json:"updated_at,omitempty"`
}

type NativeThreadGoal struct {
	ThreadID        string     `json:"thread_id"`
	Objective       string     `json:"objective"`
	Status          string     `json:"status"`
	TokenBudget     *int64     `json:"token_budget,omitempty"`
	TokensUsed      int64      `json:"tokens_used"`
	TimeUsedSeconds int64      `json:"time_used_seconds"`
	CreatedAt       *time.Time `json:"created_at,omitempty"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
}

// NativeThreadRuntimeLaunch describes how Brain should attach a provider-owned
// native thread to its portable interactive runtime.
type NativeThreadRuntimeLaunch struct {
	Command string
	Cwd     string
}

// NativeThreadRuntimeResumeLaunch returns the tmux launch command for
// resuming a provider-owned native thread. This keeps provider-specific CLI
// syntax out of Brain core.
func NativeThreadRuntimeResumeLaunch(executor AgentExecutor, thread NativeThread, opts NativeThreadResumeOptions, fallbackCwd string) (NativeThreadRuntimeLaunch, bool) {
	provider := strings.TrimSpace(executor.Provider)
	if provider == "" || provider == AgentProviderCustom {
		provider = InferAgentProvider(executor.Command, executor.ID)
	}
	switch provider {
	case AgentProviderCodex:
		return codexNativeThreadRuntimeResumeLaunch(executor, thread, opts, fallbackCwd), true
	default:
		return NativeThreadRuntimeLaunch{}, false
	}
}

// NewNativeThreadProvider returns the provider-specific native thread executor
// for a configured executor. CLI-only tools still run through tmux and return
// no native provider here.
func NewNativeThreadProvider(executor AgentExecutor) (NativeThreadProvider, bool) {
	if strings.TrimSpace(executor.Provider) != AgentProviderCodex || !executor.Capabilities.NativeThreads {
		return nil, false
	}
	return NewCodexAppServerThreadProvider(executor.Command), true
}

func codexNativeThreadRuntimeResumeLaunch(executor AgentExecutor, thread NativeThread, opts NativeThreadResumeOptions, fallbackCwd string) NativeThreadRuntimeLaunch {
	command := strings.TrimSpace(executor.Command)
	if command == "" {
		command = strings.TrimSpace(executor.ID)
	}
	if command == "" {
		command = "codex"
	}
	threadID := strings.TrimSpace(thread.NativeID)
	if threadID == "" {
		threadID = nativeThreadIDForProvider(AgentProviderCodex, thread.ID)
	}
	args := []string{command, "resume"}
	if threadID != "" {
		args = append(args, nativeThreadShellQuote(threadID))
	}
	if !strings.Contains(command, "--no-alt-screen") {
		args = append(args, "--no-alt-screen")
	}
	cwd := firstNonEmptyString(thread.Cwd, opts.Cwd, fallbackCwd)
	if cwd != "" && !strings.Contains(command, " -C ") && !strings.Contains(command, " --cd ") {
		args = append(args, "-C", nativeThreadShellQuote(cwd))
	}
	return NativeThreadRuntimeLaunch{
		Command: strings.Join(args, " "),
		Cwd:     cwd,
	}
}

func nativeThreadShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

type CodexAppServerThreadProvider struct {
	client codexAppServerCaller
}

type codexAppServerCaller interface {
	Call(ctx context.Context, method string, params any, result any) error
}

func NewCodexAppServerThreadProvider(command string) *CodexAppServerThreadProvider {
	return &CodexAppServerThreadProvider{
		client: NewCodexAppServerClient(command),
	}
}

func (p *CodexAppServerThreadProvider) ProviderID() string {
	return AgentProviderCodex
}

func (p *CodexAppServerThreadProvider) StartThread(ctx context.Context, opts NativeThreadStartOptions) (NativeThread, error) {
	if p == nil || p.client == nil {
		return NativeThread{}, fmt.Errorf("codex native thread provider is not configured")
	}
	var response codexThreadReadResponse
	if err := p.client.Call(ctx, "thread/start", codexThreadStartParams(opts), &response); err != nil {
		return NativeThread{}, err
	}
	return codexThreadToNative(response.Thread, false), nil
}

func (p *CodexAppServerThreadProvider) ResumeThread(ctx context.Context, id string, opts NativeThreadResumeOptions) (NativeThread, error) {
	if p == nil || p.client == nil {
		return NativeThread{}, fmt.Errorf("codex native thread provider is not configured")
	}
	var response codexThreadReadResponse
	params := codexThreadResumeParams(opts)
	params["threadId"] = nativeThreadIDForProvider(AgentProviderCodex, id)
	if err := p.client.Call(ctx, "thread/resume", params, &response); err != nil {
		return NativeThread{}, err
	}
	return codexThreadToNative(response.Thread, false), nil
}

func (p *CodexAppServerThreadProvider) ListThreads(ctx context.Context, opts NativeThreadListOptions) (NativeThreadPage, error) {
	if p == nil || p.client == nil {
		return NativeThreadPage{}, fmt.Errorf("codex native thread provider is not configured")
	}
	var response codexThreadListResponse
	if err := p.client.Call(ctx, "thread/list", codexThreadListParams(opts), &response); err != nil {
		return NativeThreadPage{}, err
	}
	threads := make([]NativeThread, 0, len(response.Data))
	for _, thread := range response.Data {
		threads = append(threads, codexThreadToNative(thread, opts.Archived != nil && *opts.Archived))
	}
	return NativeThreadPage{
		Threads:         threads,
		NextCursor:      strings.TrimSpace(response.NextCursor),
		BackwardsCursor: strings.TrimSpace(response.BackwardsCursor),
	}, nil
}

func (p *CodexAppServerThreadProvider) SearchThreads(ctx context.Context, opts NativeThreadSearchOptions) (NativeThreadPage, error) {
	if p == nil || p.client == nil {
		return NativeThreadPage{}, fmt.Errorf("codex native thread provider is not configured")
	}
	if strings.TrimSpace(opts.SearchTerm) == "" {
		return NativeThreadPage{}, fmt.Errorf("native thread search term is required")
	}
	var response codexThreadSearchResponse
	if err := p.client.Call(ctx, "thread/search", codexThreadSearchParams(opts), &response); err != nil {
		return NativeThreadPage{}, err
	}
	threads := make([]NativeThread, 0, len(response.Data))
	for _, result := range response.Data {
		thread := codexThreadToNative(result.Thread, false)
		thread.Snippet = strings.TrimSpace(result.Snippet)
		threads = append(threads, thread)
	}
	return NativeThreadPage{
		Threads:         threads,
		NextCursor:      strings.TrimSpace(response.NextCursor),
		BackwardsCursor: strings.TrimSpace(response.BackwardsCursor),
	}, nil
}

func (p *CodexAppServerThreadProvider) ReadThread(ctx context.Context, id string, opts NativeThreadReadOptions) (NativeThread, error) {
	if p == nil || p.client == nil {
		return NativeThread{}, fmt.Errorf("codex native thread provider is not configured")
	}
	var response codexThreadReadResponse
	params := map[string]any{
		"threadId":     nativeThreadIDForProvider(AgentProviderCodex, id),
		"includeTurns": opts.IncludeTurns,
	}
	if err := p.client.Call(ctx, "thread/read", params, &response); err != nil {
		return NativeThread{}, err
	}
	return codexThreadToNative(response.Thread, false), nil
}

func (p *CodexAppServerThreadProvider) ForkThread(ctx context.Context, id string, opts NativeThreadForkOptions) (NativeThread, error) {
	if p == nil || p.client == nil {
		return NativeThread{}, fmt.Errorf("codex native thread provider is not configured")
	}
	params := map[string]any{
		"threadId": nativeThreadIDForProvider(AgentProviderCodex, id),
	}
	if cwd := strings.TrimSpace(opts.Cwd); cwd != "" {
		params["cwd"] = cwd
	}
	if model := strings.TrimSpace(opts.Model); model != "" {
		params["model"] = model
	}
	if provider := strings.TrimSpace(opts.ModelProvider); provider != "" {
		params["modelProvider"] = provider
	}
	if instructions := strings.TrimSpace(opts.DeveloperInstructions); instructions != "" {
		params["developerInstructions"] = instructions
	}
	if instructions := strings.TrimSpace(opts.BaseInstructions); instructions != "" {
		params["baseInstructions"] = instructions
	}
	if opts.Ephemeral {
		params["ephemeral"] = true
	}
	if opts.ExcludeTurns {
		params["excludeTurns"] = true
	}
	var response codexThreadReadResponse
	if err := p.client.Call(ctx, "thread/fork", params, &response); err != nil {
		return NativeThread{}, err
	}
	return codexThreadToNative(response.Thread, false), nil
}

func (p *CodexAppServerThreadProvider) ArchiveThread(ctx context.Context, id string, archived bool) (NativeThread, error) {
	if p == nil || p.client == nil {
		return NativeThread{}, fmt.Errorf("codex native thread provider is not configured")
	}
	method := "thread/archive"
	if !archived {
		method = "thread/unarchive"
	}
	var response codexThreadReadResponse
	if err := p.client.Call(ctx, method, map[string]any{
		"threadId": nativeThreadIDForProvider(AgentProviderCodex, id),
	}, &response); err != nil {
		return NativeThread{}, err
	}
	thread := codexThreadToNative(response.Thread, archived)
	thread.Archived = archived
	return thread, nil
}

func (p *CodexAppServerThreadProvider) GetGoal(ctx context.Context, id string) (*NativeThreadGoal, error) {
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("codex native thread provider is not configured")
	}
	var response codexThreadGoalGetResponse
	if err := p.client.Call(ctx, "thread/goal/get", map[string]any{
		"threadId": nativeThreadIDForProvider(AgentProviderCodex, id),
	}, &response); err != nil {
		return nil, err
	}
	if response.Goal == nil {
		return nil, nil
	}
	goal := codexGoalToNative(*response.Goal)
	return &goal, nil
}

func (p *CodexAppServerThreadProvider) SetGoal(ctx context.Context, id string, update NativeThreadGoalUpdate) (NativeThreadGoal, error) {
	if p == nil || p.client == nil {
		return NativeThreadGoal{}, fmt.Errorf("codex native thread provider is not configured")
	}
	params := map[string]any{
		"threadId": nativeThreadIDForProvider(AgentProviderCodex, id),
	}
	if objective := strings.TrimSpace(update.Objective); objective != "" {
		params["objective"] = objective
	}
	if status := strings.TrimSpace(update.Status); status != "" {
		params["status"] = status
	}
	if update.TokenBudget != nil {
		params["tokenBudget"] = *update.TokenBudget
	}
	var response codexThreadGoalSetResponse
	if err := p.client.Call(ctx, "thread/goal/set", params, &response); err != nil {
		return NativeThreadGoal{}, err
	}
	return codexGoalToNative(response.Goal), nil
}

func (p *CodexAppServerThreadProvider) ClearGoal(ctx context.Context, id string) (bool, error) {
	if p == nil || p.client == nil {
		return false, fmt.Errorf("codex native thread provider is not configured")
	}
	var response codexThreadGoalClearResponse
	if err := p.client.Call(ctx, "thread/goal/clear", map[string]any{
		"threadId": nativeThreadIDForProvider(AgentProviderCodex, id),
	}, &response); err != nil {
		return false, err
	}
	return response.Cleared, nil
}

type CodexAppServerClient struct {
	Binary  string
	Timeout time.Duration
	nextID  atomic.Uint64
}

func NewCodexAppServerClient(command string) *CodexAppServerClient {
	return &CodexAppServerClient{
		Binary:  codexBinaryFromCommand(command),
		Timeout: defaultCodexAppServerTimeout,
	}
}

func (c *CodexAppServerClient) Call(ctx context.Context, method string, params any, result any) error {
	if c == nil {
		return fmt.Errorf("codex app-server client is not configured")
	}
	method = strings.TrimSpace(method)
	if method == "" {
		return fmt.Errorf("codex app-server method is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok && c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}

	binary := strings.TrimSpace(c.Binary)
	if binary == "" {
		binary = "codex"
	}
	cmd := exec.CommandContext(ctx, binary, "app-server", "--listen", "stdio://")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("start codex app-server: %w", err)
	}
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	encoder := json.NewEncoder(stdin)
	decoder := json.NewDecoder(stdout)
	initID := "zen-init"
	if err := encoder.Encode(jsonRPCRequest{
		ID:     initID,
		Method: "initialize",
		Params: map[string]any{
			"clientInfo": map[string]any{
				"name":    "zen",
				"version": "dev",
			},
			"capabilities": map[string]any{
				"experimentalApi": true,
			},
		},
	}); err != nil {
		return fmt.Errorf("write codex app-server initialize: %w", err)
	}
	if _, err := readJSONRPCResult(decoder, initID); err != nil {
		return fmt.Errorf("initialize codex app-server: %w%s", err, stderrSuffix(stderr.String()))
	}
	if err := encoder.Encode(map[string]any{"method": "initialized"}); err != nil {
		return fmt.Errorf("write codex app-server initialized notification: %w", err)
	}

	requestID := fmt.Sprintf("zen-%d", c.nextID.Add(1))
	if params == nil {
		params = map[string]any{}
	}
	if err := encoder.Encode(jsonRPCRequest{
		ID:     requestID,
		Method: method,
		Params: params,
	}); err != nil {
		return fmt.Errorf("write codex app-server request: %w", err)
	}
	raw, err := readJSONRPCResult(decoder, requestID)
	if err != nil {
		return fmt.Errorf("codex app-server %s: %w%s", method, err, stderrSuffix(stderr.String()))
	}
	if result == nil {
		return nil
	}
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return fmt.Errorf("decode codex app-server %s response: %w", method, err)
	}
	return nil
}

type jsonRPCRequest struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params"`
}

type jsonRPCEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *jsonRPCError   `json:"error"`
}

type jsonRPCError struct {
	Code    int64           `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func readJSONRPCResult(decoder *json.Decoder, wantID string) (json.RawMessage, error) {
	for {
		var envelope jsonRPCEnvelope
		if err := decoder.Decode(&envelope); err != nil {
			if err == io.EOF {
				return nil, fmt.Errorf("connection closed before response %s", wantID)
			}
			return nil, err
		}
		if !jsonRPCIDMatches(envelope.ID, wantID) {
			continue
		}
		if envelope.Error != nil {
			return nil, fmt.Errorf("error %d: %s", envelope.Error.Code, envelope.Error.Message)
		}
		return envelope.Result, nil
	}
}

func jsonRPCIDMatches(raw json.RawMessage, want string) bool {
	if len(raw) == 0 {
		return false
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text == want
	}
	var number int64
	if err := json.Unmarshal(raw, &number); err == nil {
		return fmt.Sprintf("%d", number) == want
	}
	return false
}

func stderrSuffix(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return ": " + value
}

type codexThreadListResponse struct {
	Data            []codexThread `json:"data"`
	NextCursor      string        `json:"nextCursor"`
	BackwardsCursor string        `json:"backwardsCursor"`
}

type codexThreadSearchResponse struct {
	Data            []codexThreadSearchResult `json:"data"`
	NextCursor      string                    `json:"nextCursor"`
	BackwardsCursor string                    `json:"backwardsCursor"`
}

type codexThreadSearchResult struct {
	Thread  codexThread `json:"thread"`
	Snippet string      `json:"snippet"`
}

type codexThreadReadResponse struct {
	Thread codexThread `json:"thread"`
}

type codexThread struct {
	ID            string          `json:"id"`
	SessionID     string          `json:"sessionId"`
	ForkedFromID  *string         `json:"forkedFromId"`
	Preview       string          `json:"preview"`
	Ephemeral     bool            `json:"ephemeral"`
	ModelProvider string          `json:"modelProvider"`
	CreatedAt     int64           `json:"createdAt"`
	UpdatedAt     int64           `json:"updatedAt"`
	Status        json.RawMessage `json:"status"`
	Path          string          `json:"path"`
	Cwd           string          `json:"cwd"`
	Source        string          `json:"source"`
	Name          *string         `json:"name"`
}

type codexThreadGoalGetResponse struct {
	Goal *codexThreadGoal `json:"goal"`
}

type codexThreadGoalSetResponse struct {
	Goal codexThreadGoal `json:"goal"`
}

type codexThreadGoalClearResponse struct {
	Cleared bool `json:"cleared"`
}

type codexThreadGoal struct {
	ThreadID        string `json:"threadId"`
	Objective       string `json:"objective"`
	Status          string `json:"status"`
	TokenBudget     *int64 `json:"tokenBudget"`
	TokensUsed      int64  `json:"tokensUsed"`
	TimeUsedSeconds int64  `json:"timeUsedSeconds"`
	CreatedAt       int64  `json:"createdAt"`
	UpdatedAt       int64  `json:"updatedAt"`
}

func codexThreadListParams(opts NativeThreadListOptions) map[string]any {
	params := map[string]any{}
	if cursor := strings.TrimSpace(opts.Cursor); cursor != "" {
		params["cursor"] = cursor
	}
	if opts.Limit > 0 {
		params["limit"] = opts.Limit
	}
	if cwd := strings.TrimSpace(opts.Cwd); cwd != "" {
		params["cwd"] = cwd
	}
	if searchTerm := strings.TrimSpace(opts.SearchTerm); searchTerm != "" {
		params["searchTerm"] = searchTerm
	}
	if sortKey := strings.TrimSpace(opts.SortKey); sortKey != "" {
		params["sortKey"] = sortKey
	}
	if sortDirection := strings.TrimSpace(opts.SortDirection); sortDirection != "" {
		params["sortDirection"] = sortDirection
	}
	if opts.Archived != nil {
		params["archived"] = *opts.Archived
	}
	if opts.UseStateDBOnly {
		params["useStateDbOnly"] = true
	}
	return params
}

func codexThreadStartParams(opts NativeThreadStartOptions) map[string]any {
	params := map[string]any{}
	if cwd := strings.TrimSpace(opts.Cwd); cwd != "" {
		params["cwd"] = cwd
	}
	if model := strings.TrimSpace(opts.Model); model != "" {
		params["model"] = model
	}
	if provider := strings.TrimSpace(opts.ModelProvider); provider != "" {
		params["modelProvider"] = provider
	}
	if instructions := strings.TrimSpace(opts.DeveloperInstructions); instructions != "" {
		params["developerInstructions"] = instructions
	}
	if instructions := strings.TrimSpace(opts.BaseInstructions); instructions != "" {
		params["baseInstructions"] = instructions
	}
	if opts.Ephemeral {
		params["ephemeral"] = true
	}
	return params
}

func codexThreadResumeParams(opts NativeThreadResumeOptions) map[string]any {
	params := map[string]any{}
	if cwd := strings.TrimSpace(opts.Cwd); cwd != "" {
		params["cwd"] = cwd
	}
	if model := strings.TrimSpace(opts.Model); model != "" {
		params["model"] = model
	}
	if provider := strings.TrimSpace(opts.ModelProvider); provider != "" {
		params["modelProvider"] = provider
	}
	if instructions := strings.TrimSpace(opts.DeveloperInstructions); instructions != "" {
		params["developerInstructions"] = instructions
	}
	if instructions := strings.TrimSpace(opts.BaseInstructions); instructions != "" {
		params["baseInstructions"] = instructions
	}
	return params
}

func codexThreadSearchParams(opts NativeThreadSearchOptions) map[string]any {
	params := map[string]any{
		"searchTerm": strings.TrimSpace(opts.SearchTerm),
	}
	if cursor := strings.TrimSpace(opts.Cursor); cursor != "" {
		params["cursor"] = cursor
	}
	if opts.Limit > 0 {
		params["limit"] = opts.Limit
	}
	if cwd := strings.TrimSpace(opts.Cwd); cwd != "" {
		params["cwd"] = cwd
	}
	return params
}

func codexThreadToNative(thread codexThread, archived bool) NativeThread {
	nativeID := strings.TrimSpace(thread.ID)
	preview := strings.TrimSpace(thread.Preview)
	title := strings.TrimSpace(derefString(thread.Name))
	if title == "" {
		title = firstNonEmptyString(firstLine(preview), nativeID)
	}
	return NativeThread{
		ID:            providerQualifiedThreadID(AgentProviderCodex, nativeID),
		NativeID:      nativeID,
		Provider:      AgentProviderCodex,
		SessionID:     strings.TrimSpace(thread.SessionID),
		ForkedFromID:  strings.TrimSpace(derefString(thread.ForkedFromID)),
		Title:         title,
		Preview:       preview,
		Snippet:       "",
		Status:        codexThreadStatus(thread.Status),
		Cwd:           strings.TrimSpace(thread.Cwd),
		Path:          strings.TrimSpace(thread.Path),
		Source:        strings.TrimSpace(thread.Source),
		ModelProvider: strings.TrimSpace(thread.ModelProvider),
		Ephemeral:     thread.Ephemeral,
		Archived:      archived,
		CreatedAt:     unixSecondsTime(thread.CreatedAt),
		UpdatedAt:     unixSecondsTime(thread.UpdatedAt),
	}
}

func codexGoalToNative(goal codexThreadGoal) NativeThreadGoal {
	return NativeThreadGoal{
		ThreadID:        providerQualifiedThreadID(AgentProviderCodex, goal.ThreadID),
		Objective:       strings.TrimSpace(goal.Objective),
		Status:          strings.TrimSpace(goal.Status),
		TokenBudget:     goal.TokenBudget,
		TokensUsed:      goal.TokensUsed,
		TimeUsedSeconds: goal.TimeUsedSeconds,
		CreatedAt:       unixSecondsTime(goal.CreatedAt),
		UpdatedAt:       unixSecondsTime(goal.UpdatedAt),
	}
}

func codexThreadStatus(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var withType struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &withType); err == nil && strings.TrimSpace(withType.Type) != "" {
		return strings.TrimSpace(withType.Type)
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	return ""
}

func providerQualifiedThreadID(provider, nativeID string) string {
	provider = strings.TrimSpace(provider)
	nativeID = strings.TrimSpace(nativeID)
	if provider == "" || nativeID == "" {
		return nativeID
	}
	if strings.HasPrefix(nativeID, provider+":") {
		return nativeID
	}
	return provider + ":" + nativeID
}

func nativeThreadIDForProvider(provider, id string) string {
	id = strings.TrimSpace(id)
	prefix := strings.TrimSpace(provider) + ":"
	return strings.TrimPrefix(id, prefix)
}

func codexBinaryFromCommand(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return "codex"
	}
	return strings.Trim(fields[0], `"'`)
}

func unixSecondsTime(value int64) *time.Time {
	if value <= 0 {
		return nil
	}
	t := time.Unix(value, 0).UTC()
	return &t
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	line, _, _ := strings.Cut(value, "\n")
	return strings.TrimSpace(line)
}
