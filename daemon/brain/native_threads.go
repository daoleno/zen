package brain

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

func (s *Service) NativeThreads(ctx context.Context, adapterID string, opts work.NativeThreadListOptions) (work.AgentAdapter, work.NativeThreadPage, error) {
	if s == nil {
		return work.AgentAdapter{}, work.NativeThreadPage{}, fmt.Errorf("brain service is not configured")
	}
	adapter, provider, err := s.nativeThreadProvider(adapterID)
	if err != nil {
		return work.AgentAdapter{}, work.NativeThreadPage{}, err
	}
	if strings.TrimSpace(opts.SearchTerm) != "" && adapter.Capabilities.NativeSearch {
		page, err := provider.SearchThreads(ctx, work.NativeThreadSearchOptions{
			Cursor:     opts.Cursor,
			Limit:      opts.Limit,
			Cwd:        opts.Cwd,
			SearchTerm: opts.SearchTerm,
		})
		if err != nil {
			return adapter, work.NativeThreadPage{}, err
		}
		page.Threads = s.annotateNativeThreads(page.Threads)
		return adapter, page, nil
	}
	page, err := provider.ListThreads(ctx, opts)
	if err != nil {
		return adapter, work.NativeThreadPage{}, err
	}
	page.Threads = s.annotateNativeThreads(page.Threads)
	return adapter, page, nil
}

func (s *Service) ReadNativeThread(ctx context.Context, adapterID, threadID string, opts work.NativeThreadReadOptions) (work.AgentAdapter, work.NativeThread, error) {
	if s == nil {
		return work.AgentAdapter{}, work.NativeThread{}, fmt.Errorf("brain service is not configured")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return work.AgentAdapter{}, work.NativeThread{}, fmt.Errorf("thread id is required")
	}
	adapter, provider, err := s.nativeThreadProvider(adapterID)
	if err != nil {
		return work.AgentAdapter{}, work.NativeThread{}, err
	}
	thread, err := provider.ReadThread(ctx, threadID, opts)
	if err != nil {
		return adapter, work.NativeThread{}, err
	}
	return adapter, s.annotateNativeThread(thread), nil
}

func (s *Service) ResumeNativeThread(ctx context.Context, adapterID, threadID string, opts work.NativeThreadResumeOptions) (work.AgentAdapter, work.NativeThread, error) {
	if s == nil {
		return work.AgentAdapter{}, work.NativeThread{}, fmt.Errorf("brain service is not configured")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return work.AgentAdapter{}, work.NativeThread{}, fmt.Errorf("thread id is required")
	}
	adapter, provider, err := s.nativeThreadProvider(adapterID)
	if err != nil {
		return work.AgentAdapter{}, work.NativeThread{}, err
	}
	thread, err := provider.ResumeThread(ctx, threadID, opts)
	if err != nil {
		return adapter, work.NativeThread{}, err
	}
	return adapter, s.annotateNativeThread(thread), nil
}

func (s *Service) ResumeNativeThreadAsHost(ctx context.Context, adapterID, threadID string, opts work.NativeThreadResumeOptions) (Snapshot, work.NativeThread, error) {
	if s == nil || s.store == nil || s.watcher == nil {
		return Snapshot{}, work.NativeThread{}, fmt.Errorf("brain service is not configured")
	}
	adapter, thread, err := s.ResumeNativeThread(ctx, adapterID, threadID, opts)
	if err != nil {
		return Snapshot{}, work.NativeThread{}, err
	}
	launch, ok := work.NativeThreadRuntimeResumeLaunch(adapter, thread, opts, s.brainWorkspace())
	if !ok {
		return Snapshot{}, work.NativeThread{}, fmt.Errorf("adapter %q cannot resume native threads through Brain's tmux host", adapter.ID)
	}
	if host, hostErr := s.store.HostSession(); hostErr == nil {
		if id := strings.TrimSpace(host.ID); id != "" && s.watcher.HasSession(id) {
			_ = s.watcher.KillSession(id)
		}
	}
	agentID, err := s.watcher.CreateSession("", watcher.CreateSessionOptions{
		Cwd:      launch.Cwd,
		Command:  launch.Command,
		Name:     "Brain",
		Detached: true,
		Hidden:   true,
	})
	if err != nil {
		return Snapshot{}, work.NativeThread{}, err
	}
	if err := s.store.SetHostSession(agentID, adapter.ID); err != nil {
		return Snapshot{}, work.NativeThread{}, err
	}
	if err := s.store.SetChatState(ChatState{
		ThreadID:       thread.ID,
		SessionIDs:     []string{agentID},
		LastTranscript: "",
		UpdatedAt:      s.nowUTC(),
	}); err != nil {
		return Snapshot{}, work.NativeThread{}, err
	}
	snapshot, err := s.Snapshot()
	if err != nil {
		return Snapshot{}, work.NativeThread{}, err
	}
	return snapshot, thread, nil
}

func (s *Service) ForkNativeThread(ctx context.Context, adapterID, threadID string, opts work.NativeThreadForkOptions) (work.AgentAdapter, work.NativeThread, error) {
	if s == nil {
		return work.AgentAdapter{}, work.NativeThread{}, fmt.Errorf("brain service is not configured")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return work.AgentAdapter{}, work.NativeThread{}, fmt.Errorf("thread id is required")
	}
	adapter, provider, err := s.nativeThreadProvider(adapterID)
	if err != nil {
		return work.AgentAdapter{}, work.NativeThread{}, err
	}
	thread, err := provider.ForkThread(ctx, threadID, opts)
	if err != nil {
		return adapter, work.NativeThread{}, err
	}
	return adapter, s.annotateNativeThread(thread), nil
}

func (s *Service) ArchiveNativeThread(ctx context.Context, adapterID, threadID string, archived bool) (work.AgentAdapter, work.NativeThread, error) {
	if s == nil {
		return work.AgentAdapter{}, work.NativeThread{}, fmt.Errorf("brain service is not configured")
	}
	adapter, provider, err := s.nativeThreadProvider(adapterID)
	if err != nil {
		return work.AgentAdapter{}, work.NativeThread{}, err
	}
	thread, err := provider.ArchiveThread(ctx, threadID, archived)
	if err != nil {
		return adapter, work.NativeThread{}, err
	}
	return adapter, s.annotateNativeThread(thread), nil
}

func (s *Service) SetNativeThreadPinned(threadID string, pinned bool) (work.NativeThread, error) {
	if s == nil || s.store == nil {
		return work.NativeThread{}, fmt.Errorf("brain service is not configured")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return work.NativeThread{}, fmt.Errorf("thread id is required")
	}
	meta, err := s.store.SetThreadPinned(threadID, pinned)
	if err != nil {
		return work.NativeThread{}, err
	}
	return work.NativeThread{
		ID:          meta.ThreadID,
		Pinned:      meta.Pinned,
		ReviewState: meta.ReviewState,
	}, nil
}

func (s *Service) SetNativeThreadReviewState(threadID, reviewState string) (work.NativeThread, error) {
	if s == nil || s.store == nil {
		return work.NativeThread{}, fmt.Errorf("brain service is not configured")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return work.NativeThread{}, fmt.Errorf("thread id is required")
	}
	meta, err := s.store.SetThreadReviewState(threadID, reviewState)
	if err != nil {
		return work.NativeThread{}, err
	}
	return work.NativeThread{
		ID:          meta.ThreadID,
		Pinned:      meta.Pinned,
		ReviewState: meta.ReviewState,
	}, nil
}

func (s *Service) GetNativeThreadGoal(ctx context.Context, adapterID, threadID string) (work.AgentAdapter, *work.NativeThreadGoal, error) {
	if s == nil {
		return work.AgentAdapter{}, nil, fmt.Errorf("brain service is not configured")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return work.AgentAdapter{}, nil, fmt.Errorf("thread id is required")
	}
	adapter, provider, err := s.nativeThreadProvider(adapterID)
	if err != nil {
		return work.AgentAdapter{}, nil, err
	}
	goal, err := provider.GetGoal(ctx, threadID)
	if err != nil {
		return adapter, nil, err
	}
	return adapter, goal, nil
}

func (s *Service) SetNativeThreadGoal(ctx context.Context, adapterID, threadID string, update work.NativeThreadGoalUpdate) (work.AgentAdapter, work.NativeThreadGoal, error) {
	if s == nil {
		return work.AgentAdapter{}, work.NativeThreadGoal{}, fmt.Errorf("brain service is not configured")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return work.AgentAdapter{}, work.NativeThreadGoal{}, fmt.Errorf("thread id is required")
	}
	if strings.TrimSpace(update.Objective) == "" && strings.TrimSpace(update.Status) == "" && update.TokenBudget == nil {
		return work.AgentAdapter{}, work.NativeThreadGoal{}, fmt.Errorf("thread goal update is empty")
	}
	adapter, provider, err := s.nativeThreadProvider(adapterID)
	if err != nil {
		return work.AgentAdapter{}, work.NativeThreadGoal{}, err
	}
	goal, err := provider.SetGoal(ctx, threadID, update)
	if err != nil {
		return adapter, work.NativeThreadGoal{}, err
	}
	return adapter, goal, nil
}

func (s *Service) ClearNativeThreadGoal(ctx context.Context, adapterID, threadID string) (work.AgentAdapter, bool, error) {
	if s == nil {
		return work.AgentAdapter{}, false, fmt.Errorf("brain service is not configured")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return work.AgentAdapter{}, false, fmt.Errorf("thread id is required")
	}
	adapter, provider, err := s.nativeThreadProvider(adapterID)
	if err != nil {
		return work.AgentAdapter{}, false, err
	}
	cleared, err := provider.ClearGoal(ctx, threadID)
	if err != nil {
		return adapter, false, err
	}
	return adapter, cleared, nil
}

func (s *Service) nativeThreadProvider(adapterID string) (work.AgentAdapter, work.NativeThreadProvider, error) {
	adapter := s.hostAdapter()
	if requested := strings.TrimSpace(adapterID); requested != "" {
		if s.execs == nil {
			return work.AgentAdapter{}, nil, ErrAdapterNotConfigured
		}
		var ok bool
		adapter, ok = s.execs.AgentAdapter(requested)
		if !ok {
			return work.AgentAdapter{}, nil, fmt.Errorf("%w: %s", ErrAdapterNotConfigured, requested)
		}
	}
	provider, ok := work.NewNativeThreadProvider(adapter)
	if !ok {
		return work.AgentAdapter{}, nil, fmt.Errorf("adapter %q does not expose native threads", adapter.ID)
	}
	return adapter, provider, nil
}

func (s *Service) annotateNativeThread(thread work.NativeThread) work.NativeThread {
	threads := s.annotateNativeThreads([]work.NativeThread{thread})
	if len(threads) == 0 {
		return thread
	}
	return threads[0]
}

func (s *Service) annotateNativeThreads(threads []work.NativeThread) []work.NativeThread {
	if len(threads) == 0 || s == nil || s.store == nil {
		return threads
	}
	ids := make([]string, 0, len(threads))
	for _, thread := range threads {
		if id := strings.TrimSpace(thread.ID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return threads
	}
	metadata, err := s.store.ThreadMetadataMap(ids)
	if err != nil {
		return threads
	}
	next := append([]work.NativeThread(nil), threads...)
	for index := range next {
		meta := metadata[strings.TrimSpace(next[index].ID)]
		if meta.Pinned {
			next[index].Pinned = true
		}
		if meta.ReviewState != "" {
			next[index].ReviewState = meta.ReviewState
		}
	}
	sort.SliceStable(next, func(i, j int) bool {
		if next[i].Pinned != next[j].Pinned {
			return next[i].Pinned
		}
		leftNeedsReview := nativeThreadNeedsReview(next[i])
		rightNeedsReview := nativeThreadNeedsReview(next[j])
		if leftNeedsReview != rightNeedsReview {
			return leftNeedsReview
		}
		return false
	})
	return next
}

func nativeThreadNeedsReview(thread work.NativeThread) bool {
	switch strings.TrimSpace(thread.ReviewState) {
	case "needs_review", "reviewing":
		return true
	default:
		return false
	}
}
