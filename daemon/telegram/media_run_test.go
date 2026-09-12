package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
)

type mediaPoll struct {
	offset int64
	reply  chan []Update
}

type controlledMediaAPI struct {
	*mediaFixtureAPI
	polls chan mediaPoll
}

func (a *controlledMediaAPI) GetUpdates(ctx context.Context, _ string, offset int64, _ int, _ []string) ([]Update, error) {
	poll := mediaPoll{offset: offset, reply: make(chan []Update, 1)}
	select {
	case a.polls <- poll:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case updates := <-poll.reply:
		return updates, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type runningMediaFixture struct {
	*mediaFixture
	api       *controlledMediaAPI
	blocked   chan struct{}
	release   chan struct{}
	cancelled chan struct{}
	getFiles  atomic.Int32
	active    atomic.Int32
	maximum   atomic.Int32
	cancel    context.CancelFunc
	done      chan error
}

func newRunningMediaFixture(t *testing.T, blockAt string, backlog int) *runningMediaFixture {
	t.Helper()
	f := newMediaFixture(t)
	f.files["doc"] = bytes.Repeat([]byte("d"), 1024)
	r := &runningMediaFixture{mediaFixture: f, blocked: make(chan struct{}), release: make(chan struct{}), cancelled: make(chan struct{}), done: make(chan error, 1)}
	var bodyCalls atomic.Int32
	block := func(request *http.Request) bool {
		close(r.blocked)
		select {
		case <-r.release:
			return false
		case <-request.Context().Done():
			close(r.cancelled)
			return true
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		active := r.active.Add(1)
		defer r.active.Add(-1)
		for previous := r.maximum.Load(); active > previous && !r.maximum.CompareAndSwap(previous, active); previous = r.maximum.Load() {
		}
		if strings.HasSuffix(request.URL.Path, "/getFile") {
			first := r.getFiles.Add(1) == 1
			var payload struct {
				FileID string `json:"file_id"`
			}
			_ = json.NewDecoder(request.Body).Decode(&payload)
			if blockAt == "getFile" && first && block(request) {
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": File{FileID: payload.FileID, FilePath: "owned/" + payload.FileID, FileSize: 1024}})
			return
		}
		w.Header().Set("Content-Length", "1024")
		_, _ = w.Write(f.files["doc"][:512])
		w.(http.Flusher).Flush()
		if blockAt == "body" && bodyCalls.Add(1) == 1 && block(request) {
			return
		}
		_, _ = w.Write(f.files["doc"][512:])
	}))
	t.Cleanup(server.Close)
	r.api = &controlledMediaAPI{mediaFixtureAPI: &mediaFixtureAPI{fakeAPI: f.api.fakeAPI, client: NewClient(server.URL, server.Client())}, polls: make(chan mediaPoll)}
	f.m.api = r.api
	if err := f.m.store.mutate(func(s *durableState) error { s.BrainEntryState = "pinned"; return nil }); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < backlog; i++ {
		if err := f.m.handleUpdate(t.Context(), "fixture-token", f.document(int64(i+2), 101, "doc", fmt.Sprintf("file-%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	r.cancel = cancel
	go func() { r.done <- f.m.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-r.done:
		case <-time.After(3 * time.Second):
			t.Error("Run did not join its media worker")
		}
	})
	return r
}

func nextMediaPoll(t *testing.T, r *runningMediaFixture) mediaPoll {
	t.Helper()
	select {
	case poll := <-r.api.polls:
		return poll
	case <-time.After(3 * time.Second):
		t.Fatal("polling blocked behind media IO")
		return mediaPoll{}
	}
}

func waitMediaSignal(t *testing.T, signal <-chan struct{}, reason string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal(reason)
	}
}

func mediaProviderReceipts(r *runningMediaFixture) map[string]providerReceipt {
	r.provider.mu.Lock()
	defer r.provider.mu.Unlock()
	return r.provider.receipts()
}

func TestRunMediaBlockedIODoesNotBlockTextCallbacksOrOutput(t *testing.T) {
	for _, blockedAt := range []string{"getFile", "body"} {
		t.Run(blockedAt, func(t *testing.T) {
			r := newRunningMediaFixture(t, blockedAt, maxMediaInputs)
			poll := nextMediaPoll(t, r)
			poll.reply <- nil
			waitMediaSignal(t, r.blocked, "download did not reach gate")
			poll = nextMediaPoll(t, r)
			if _, err := r.store.AppendTimelineItem(brain.TimelineItem{ID: "ready-output", ThreadID: "brain-current", SessionID: "host:@1", Role: "assistant", Kind: "assistant_message", Body: "Output while file pending", CreatedAt: r.now.Add(time.Second)}); err != nil {
				t.Fatal(err)
			}
			poll.reply <- []Update{topicUpdate(130, 76315, "Brain text while file pending"), topicUpdate(131, 102, "Session B text while file pending"), navigationCallback(132, 102, "sessions")}
			nextMediaPoll(t, r)
			receipts := mediaProviderReceipts(r)
			if len(receipts) != 2 || receipts["telegram:update:7001:130"].SessionID != "host:@1" || receipts["telegram:update:7001:131"].SessionID != "session-b" {
				t.Fatalf("text stalled/misrouted: %+v", receipts)
			}
			r.api.mu.Lock()
			acked := len(r.api.callbackAnswers) > 0
			output := false
			for _, sent := range r.api.sent {
				if sent.Text == "Output while file pending" {
					output = true
				}
			}
			r.api.mu.Unlock()
			if !acked || !output {
				t.Fatal("callback/output blocked behind pending file")
			}
			if r.getFiles.Load() != 1 || r.maximum.Load() != 1 {
				t.Fatal("media backlog was drained or concurrency exceeded one")
			}
			active, reserved := r.m.attachments.Usage()
			if active > 1 || reserved > 1024 {
				t.Fatalf("unbounded reservations: %d %d", active, reserved)
			}
			if r.m.store.snapshot().MediaInputs["update:2"].State != "downloading" {
				t.Fatal("pending file was prematurely admitted")
			}
		})
	}
}

func TestRunMediaDisableRotationAndShutdownCancelIO(t *testing.T) {
	for _, blockedAt := range []string{"getFile", "body"} {
		for _, action := range []string{"disable", "rotate", "token", "revoke", "shutdown"} {
			t.Run(blockedAt+"/"+action, func(t *testing.T) {
				r := newRunningMediaFixture(t, blockedAt, 1)
				poll := nextMediaPoll(t, r)
				poll.reply <- nil
				waitMediaSignal(t, r.blocked, "download did not reach gate")
				poll = nextMediaPoll(t, r)
				switch action {
				case "disable":
					if err := r.m.Disable(); err != nil {
						t.Fatal(err)
					}
				case "rotate":
					r.api.bot = User{ID: 8002, IsBot: true, Username: "replacement_bot", Topics: true}
					if _, err := r.m.Configure(t.Context(), "replacement-token"); err != nil {
						t.Fatal(err)
					}
				case "token":
					if _, err := r.m.Configure(t.Context(), "replacement-token"); err != nil {
						t.Fatal(err)
					}
				case "revoke":
					if err := r.m.RevokeOwner(); err != nil {
						t.Fatal(err)
					}
				case "shutdown":
					r.cancel()
				}
				waitMediaSignal(t, r.cancelled, "authority change did not cancel HTTP")
				r.m.mediaMu.Lock()
				active := r.m.mediaActive
				r.m.mediaMu.Unlock()
				if active != nil {
					waitMediaSignal(t, active.done, "cancelled worker did not finish")
				}
				if count, reserved := r.m.attachments.Usage(); count != 0 || reserved != 0 {
					t.Fatalf("cancel leaked reservations: %d %d", count, reserved)
				}
				entries, err := os.ReadDir(r.m.attachments.Dir)
				if err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				if len(entries) != 0 || len(mediaProviderReceipts(r)) != 0 {
					t.Fatal("cancelled file/provider work escaped authority fence")
				}
				if action == "rotate" || action == "revoke" {
					if len(r.m.store.snapshot().MediaInputs) != 0 {
						t.Fatal("old binding's media reappeared")
					}
				}
				if action == "disable" || action == "token" {
					if action == "disable" {
						if err := r.m.Enable(); err != nil {
							t.Fatal(err)
						}
					}
					poll.reply <- nil
					for range 6 {
						poll = nextMediaPoll(t, r)
						r.m.mediaMu.Lock()
						active = r.m.mediaActive
						r.m.mediaMu.Unlock()
						if active != nil {
							waitMediaSignal(t, active.done, "resumed worker stuck")
						}
						if len(mediaProviderReceipts(r)) == 1 {
							break
						}
						poll.reply <- nil
					}
					rows := mediaProviderReceipts(r)
					if len(rows) != 1 || rows["telegram:update:7001:2"].SessionID != "session-a" {
						t.Fatal("resumption duplicated/retargeted input")
					}
					file := decodeAttachment(t, rows["telegram:update:7001:2"].Body).Files[0]
					assertAttachment(t, r.m.attachments, file, r.files["doc"])
					if r.getFiles.Load() != 2 || r.maximum.Load() != 1 {
						t.Fatal("resumption overlapped or duplicated download")
					}
				}
			})
		}
	}
}

func TestRunMediaKeepsAlbumBeforeLaterBatchAndRechecksRecipient(t *testing.T) {
	r := newRunningMediaFixture(t, "getFile", 0)
	poll := nextMediaPoll(t, r)
	first, second := r.document(2, 101, "doc", "caption one"), r.document(3, 101, "doc", "caption two")
	first.Message.MediaGroupID = "owned-album"
	second.Message.MediaGroupID = "owned-album"
	poll.reply <- []Update{first, second, r.document(4, 102, "doc", "later batch")}
	poll = nextMediaPoll(t, r)
	if r.getFiles.Load() != 0 {
		t.Fatal("later batch overtook collecting album")
	}
	if err := r.m.store.mutate(func(s *durableState) error {
		for key, row := range s.MediaInputs {
			row.ReadyAt = r.now
			s.MediaInputs[key] = row
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	poll.reply <- nil
	waitMediaSignal(t, r.blocked, "album download not started")
	poll = nextMediaPoll(t, r)
	close(r.release)
	r.m.mediaMu.Lock()
	active := r.m.mediaActive
	r.m.mediaMu.Unlock()
	waitMediaSignal(t, active.done, "album download stuck")
	poll.reply <- nil
	poll = nextMediaPoll(t, r)
	rows := mediaProviderReceipts(r)
	if len(rows) != 1 || rows["telegram:update:7001:2"].SessionID != "session-a" {
		t.Fatal("album admission order changed")
	}
	album := decodeAttachment(t, rows["telegram:update:7001:2"].Body)
	if len(album.Files) != 2 || album.Captions[0].Text != "caption one" || album.Captions[1].Text != "caption two" {
		t.Fatal("album parts/captions lost ordering")
	}
	for _, file := range album.Files {
		assertAttachment(t, r.m.attachments, file, r.files["doc"])
	}
	if r.getFiles.Load() != 2 {
		t.Fatal("one pass drained multiple batches")
	}
	if err := r.provider.KillSession("session-b"); err != nil {
		t.Fatal(err)
	}
	poll.reply <- nil
	nextMediaPoll(t, r)
	if len(mediaProviderReceipts(r)) != 1 || r.m.store.snapshot().MediaInputs["update:4"].State != "failed" {
		t.Fatal("stale recipient admitted file")
	}
}

func TestRunMediaRecipientChangeDuringDownloadNeverRetargets(t *testing.T) {
	for _, recipient := range []string{"brain", "session"} {
		t.Run(recipient, func(t *testing.T) {
			r := newRunningMediaFixture(t, "body", 0)
			poll := nextMediaPoll(t, r)
			topic := int64(101)
			if recipient == "brain" {
				topic = 76315
			}
			poll.reply <- []Update{r.document(2, topic, "doc", "original recipient only")}
			waitMediaSignal(t, r.blocked, "download did not start")
			poll = nextMediaPoll(t, r)
			if recipient == "brain" {
				if err := r.store.SetChatState(brain.ChatState{ThreadID: "new-current"}); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := r.provider.KillSession("session-a"); err != nil {
					t.Fatal(err)
				}
			}
			close(r.release)
			r.m.mediaMu.Lock()
			active := r.m.mediaActive
			r.m.mediaMu.Unlock()
			waitMediaSignal(t, active.done, "download did not finish")
			poll.reply <- nil
			nextMediaPoll(t, r)
			if len(mediaProviderReceipts(r)) != 0 || r.m.store.snapshot().MediaInputs["update:2"].State != "failed" {
				t.Fatal("file migrated after recipient changed")
			}
		})
	}
}
