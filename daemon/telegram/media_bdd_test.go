package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/attachment"
	"github.com/daoleno/zen/daemon/brain"
)

type mediaFixtureAPI struct {
	*fakeAPI
	client *Client
}

func (a *mediaFixtureAPI) GetFile(ctx context.Context, token, id string) (File, error) {
	return a.client.GetFile(ctx, token, id)
}
func (a *mediaFixtureAPI) DownloadFile(ctx context.Context, token string, file File) (io.ReadCloser, error) {
	return a.client.DownloadFile(ctx, token, file)
}

// Bot HTTP and provider IO are synthetic. Bytes, upload storage, Telegram
// receipts, Brain admission, canonical timelines and Session routing are real.
type mediaFixture struct {
	m        *Manager
	store    *brain.Store
	service  *brain.Service
	provider *conversationProvider
	api      *mediaFixtureAPI
	root     string
	now      time.Time
	mu       sync.Mutex
	files    map[string][]byte
	fail     map[string]int
	download map[string]int
}

func newMediaFixture(t *testing.T) *mediaFixture {
	t.Helper()
	m, s, service, provider, api, root := realConversationFixture(t)
	f := &mediaFixture{m: m, store: s, service: service, provider: provider, root: root, now: time.Now().UTC(),
		files: map[string][]byte{"doc": []byte("owned document bytes\n"), "other": []byte("second file\n")}, fail: map[string]int{}, download: map[string]int{}}
	var photo bytes.Buffer
	if err := jpeg.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	f.files["photo"] = photo.Bytes()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path == "/botfixture-token/getFile" {
			var request struct {
				FileID string `json:"file_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": File{FileID: request.FileID, FileSize: int64(len(f.files[request.FileID])), FilePath: "fixtures/" + request.FileID}})
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/file/botfixture-token/fixtures/")
		f.download[id]++
		if f.fail[id] > 0 {
			f.fail[id]--
			w.WriteHeader(503)
			return
		}
		data, ok := f.files[id]
		if !ok {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)
	f.api = &mediaFixtureAPI{fakeAPI: api, client: NewClient(server.URL, server.Client())}
	m.api, m.attachments = f.api, &attachment.Store{Dir: filepath.Join(root, "uploads")}
	m.now = func() time.Time { return f.now }
	if err := m.store.mutate(func(state *durableState) error {
		state.BrainTopicID, state.BrainTopics = 76315, []int64{76315}
		state.Topics = []topicMapping{{ChatID: 10, SessionID: "session-a", MessageThreadID: 101, State: topicStateActive}, {ChatID: 10, SessionID: "session-b", MessageThreadID: 102, State: topicStateActive}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *mediaFixture) document(id, topic int64, fileID, caption string) Update {
	u := topicUpdate(id, topic, "")
	u.Message.Caption = caption
	u.Message.Document = &MediaFile{File: File{FileID: fileID, FileSize: int64(len(f.files[fileID]))}, FileName: "notes.txt", MIMEType: "text/plain"}
	return u
}

func (f *mediaFixture) apply(t *testing.T, updates ...Update) {
	t.Helper()
	for _, update := range updates {
		if err := f.m.handleUpdate(t.Context(), "fixture-token", update); err != nil {
			t.Fatal(err)
		}
	}
	f.drain(t)
}

func (f *mediaFixture) advance(t *testing.T, by time.Duration) {
	t.Helper()
	f.now = f.now.Add(by)
	f.drain(t)
}

// Deterministically drive the same one-step boundary as Run, joining fixture
// IO before changing the test clock or inspecting actual file/provider bytes.
func (f *mediaFixture) drain(t *testing.T) {
	t.Helper()
	for range maxMediaInputs * 2 {
		before := f.m.store.snapshot()
		if err := f.m.advanceMedia(t.Context(), "fixture-token"); err != nil {
			t.Fatal(err)
		}
		f.m.mediaMu.Lock()
		active := f.m.mediaActive
		f.m.mediaMu.Unlock()
		if active != nil {
			select {
			case <-active.done:
			case <-time.After(5 * time.Second):
				t.Fatal("fixture download timed out")
			}
			continue
		}
		if reflect.DeepEqual(before.MediaInputs, f.m.store.snapshot().MediaInputs) {
			return
		}
	}
	t.Fatal("fixture media did not settle")
}

func (f *mediaFixture) reopen(t *testing.T) {
	t.Helper()
	m, err := NewManagerWithOptions(f.root, f.service, Options{API: f.api, Attachments: f.m.attachments, Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	f.m = m
	t.Cleanup(m.stopTyping)
}

func decodeAttachment(t *testing.T, body string) attachment.Envelope {
	t.Helper()
	_, raw, ok := strings.Cut(body, "<zen_attachments>")
	if !ok {
		t.Fatalf("missing canonical attachment envelope: %q", body)
	}
	raw, _, _ = strings.Cut(raw, "</zen_attachments>")
	var result attachment.Envelope
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertAttachment(t *testing.T, store *attachment.Store, file attachment.File, want []byte) {
	t.Helper()
	if !store.Exists(file) || filepath.Dir(file.Path) != store.Dir || file.Size != int64(len(want)) {
		t.Fatalf("invalid attachment: %+v", file)
	}
	data, err := os.ReadFile(file.Path)
	if err != nil || !bytes.Equal(data, want) {
		t.Fatalf("stored bytes mismatch: %v", err)
	}
	info, _ := os.Stat(file.Path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("unsafe mode: %v", info.Mode())
	}
}

func TestMediaBDDPhotoDocumentCaptionCanonicalRoutingAndRestart(t *testing.T) {
	f := newMediaFixture(t)
	caption := "Compare \U0001f469\u200d\U0001f4bb \u2764\ufe0f"
	u := topicUpdate(2, 76315, "")
	u.Message.Caption = caption
	u.Message.CaptionEntities = []MessageEntity{{Type: "custom_emoji", Offset: 8, Length: 5, CustomEmojiID: "owned-emoji"}, {Type: "text_mention", Offset: 0, Length: 7, User: &attachment.EntityUser{ID: 10, FirstName: "Fixture"}}}
	u.Message.Photo = []PhotoSize{{File: File{FileID: "thumb", FileSize: 5}, Width: 1, Height: 1}, {File: File{FileID: "photo", FileSize: int64(len(f.files["photo"]))}, Width: 2, Height: 2}}
	f.apply(t, u, f.document(3, 101, "doc", "Session A caption"), f.document(4, 102, "other", "Session B caption"))
	rows := f.provider.receipts()
	for _, tc := range []struct {
		id                       int
		session, remote, caption string
	}{{2, "host:@1", "photo", caption}, {3, "session-a", "doc", "Session A caption"}, {4, "session-b", "other", "Session B caption"}} {
		row := rows[fmt.Sprintf("telegram:update:7001:%d", tc.id)]
		if row.SessionID != tc.session || !strings.Contains(row.Body, tc.caption) {
			t.Fatalf("wrong route/caption: %+v", row)
		}
		envelope := decodeAttachment(t, row.Body)
		if len(envelope.Files) != 1 || envelope.Captions[0].Text != tc.caption {
			t.Fatalf("metadata=%+v", envelope)
		}
		assertAttachment(t, f.m.attachments, envelope.Files[0], f.files[tc.remote])
		if tc.id == 2 && !reflect.DeepEqual(envelope.Captions[0].Entities, u.Message.CaptionEntities) {
			t.Fatal("UTF16/custom emoji metadata lost")
		}
	}
	items, err := f.store.ThreadTimeline("brain-current", 0)
	if err != nil || len(items) != 1 || !items[0].BrainAdmission {
		t.Fatalf("not real canonical admission: %v %v", items, err)
	}
	f.reopen(t)
	f.apply(t, u)
	if !reflect.DeepEqual(rows, f.provider.receipts()) || f.download["photo"] != 1 {
		t.Fatal("restart/replay downloaded or admitted twice")
	}
	if f.m.store.snapshot().BrainTopicID != 76315 {
		t.Fatal("primary Brain replaced")
	}
}

func TestMediaBDDAlbumPartialRetryRestartAndLateItem(t *testing.T) {
	f := newMediaFixture(t)
	one, two := f.document(2, 101, "doc", "first caption"), f.document(3, 101, "other", "second caption")
	one.Message.MediaGroupID, two.Message.MediaGroupID = "album-owned", "album-owned"
	f.fail["other"] = 1
	f.apply(t, one, two, two)
	if len(f.provider.receipts()) != 0 {
		t.Fatal("album admitted before quiet boundary")
	}
	f.advance(t, 3*time.Second)
	if len(f.provider.receipts()) != 0 || f.download["doc"] != 1 {
		t.Fatal("partial batch admitted or first file missing")
	}
	f.reopen(t)
	f.advance(t, 3*time.Second)
	rows := f.provider.receipts()
	if len(rows) != 1 || f.download["doc"] != 1 || f.download["other"] != 2 {
		t.Fatalf("retry lost dedupe: rows=%v downloads=%v", rows, f.download)
	}
	envelope := decodeAttachment(t, rows["telegram:update:7001:2"].Body)
	if len(envelope.Files) != 2 || len(envelope.Captions) != 2 || envelope.Captions[1].Text != "second caption" {
		t.Fatalf("album lost content: %+v", envelope)
	}
	assertAttachment(t, f.m.attachments, envelope.Files[0], f.files["doc"])
	assertAttachment(t, f.m.attachments, envelope.Files[1], f.files["other"])
	late := f.document(4, 101, "doc", "late caption")
	late.Message.MediaGroupID = "album-owned"
	f.apply(t, late)
	if len(f.provider.receipts()) != 1 || f.m.store.snapshot().Processed["4"].Disposition != "media_late" {
		t.Fatal("late album item caused another turn")
	}
}

func TestMediaBDDRejectMetadataOwnerAndExactStaleRecipient(t *testing.T) {
	for _, name := range []string{"path", "windows", "control", "mime", "huge", "caption", "entity", "foreign", "group", "edited", "unknown", "stale"} {
		t.Run(name, func(t *testing.T) {
			f := newMediaFixture(t)
			u := f.document(2, 101, "doc", "caption")
			switch name {
			case "path":
				u.Message.Document.FileName = "../escape.txt"
			case "windows":
				u.Message.Document.FileName = "C:\\escape.txt"
			case "control":
				u.Message.Document.FileName = "unsafe\nfile"
			case "mime":
				u.Message.Document.MIMEType = "invalid\r\ncontent-type"
			case "huge":
				u.Message.Document.FileSize = maxTelegramFileBytes + 1
			case "caption":
				u.Message.Caption = strings.Repeat("a", 4097)
			case "entity":
				u.Message.Caption = "\U0001f44d"
				u.Message.CaptionEntities = []MessageEntity{{Type: "bold", Offset: 1, Length: 1}}
			case "foreign":
				u.Message.From.ID = 11
			case "group":
				u.Message.Chat.Type = "group"
			case "edited":
				u.EditedMessage, u.Message = u.Message, nil
			case "unknown":
				u.Message.MessageThreadID = 999
			case "stale":
				f.provider.KillSession("session-a")
			}
			f.apply(t, u)
			if len(f.provider.receipts()) != 0 || len(f.download) != 0 {
				t.Fatal("invalid input reached file/provider IO")
			}
		})
	}
}

func TestMediaBDDFrozenRecipientsAndSourceTopic(t *testing.T) {
	f := newMediaFixture(t)
	if err := f.m.store.mutate(func(s *durableState) error { s.ReplySessions[900] = "session-a"; return nil }); err != nil {
		t.Fatal(err)
	}
	u := f.document(2, 76315, "doc", "reply to A from Brain topic")
	u.Message.ReplyToMessage = &Message{MessageID: 900, From: &User{ID: 7001, IsBot: true}}
	f.apply(t, u)
	if f.provider.receipts()["telegram:update:7001:2"].SessionID != "session-a" {
		t.Fatal("source reply fell into Brain")
	}
	found := false
	for _, row := range f.m.store.snapshot().Outbox {
		if row.ID == "media-ack:update:2" {
			found = row.MessageThreadID == 76315 && row.ReplyMessageID == 2
		}
	}
	if !found {
		t.Fatal("ack left its source topic")
	}
	if err := f.m.deliverPending(t.Context(), "fixture-token", 8); err != nil {
		t.Fatal(err)
	}
	for _, row := range f.m.store.snapshot().Outbox {
		if row.ID == "media-ack:update:2" && f.m.store.snapshot().ReplySessions[row.MessageID] != "session-a" {
			t.Fatal("reply to media receipt lost exact Session attribution")
		}
	}
	album := f.document(3, 76315, "doc", "must not migrate")
	album.Message.MediaGroupID = "brain-before-switch"
	f.apply(t, album)
	if err := f.store.SetChatState(brain.ChatState{ThreadID: "new-current"}); err != nil {
		t.Fatal(err)
	}
	f.advance(t, 3*time.Second)
	if len(f.provider.receipts()) != 1 {
		t.Fatal("staged Brain files migrated to a new conversation")
	}
	if err := f.m.store.mutate(func(s *durableState) error { s.TopicsAvailable = false; s.FallbackSessionID = "session-a"; return nil }); err != nil {
		t.Fatal(err)
	}
	a := f.document(4, 0, "other", "stay on selected A")
	a.Message.MediaGroupID = "fallback"
	f.apply(t, a)
	if err := f.m.store.mutate(func(s *durableState) error { s.FallbackSessionID = "session-b"; return nil }); err != nil {
		t.Fatal(err)
	}
	f.advance(t, 3*time.Second)
	if f.provider.receipts()["telegram:update:7001:4"].SessionID != "session-a" {
		t.Fatal("queued file retargeted to B")
	}
}

func TestMediaBDDAdmissionCrashNeverReplaysAndDownloadFailureIsAtomic(t *testing.T) {
	f := newMediaFixture(t)
	u := f.document(2, 101, "doc", "crash boundary")
	if err := f.m.handleUpdate(t.Context(), "fixture-token", u); err != nil {
		t.Fatal(err)
	}
	input := f.m.store.snapshot().MediaInputs["update:2"]
	input.State = "admitting"
	if err := f.m.saveMedia(input); err != nil {
		t.Fatal(err)
	}
	f.reopen(t)
	f.advance(t, time.Second)
	if len(f.provider.receipts()) != 0 || f.m.store.snapshot().MediaInputs[input.ID].State != "uncertain" {
		t.Fatal("indeterminate admission replayed")
	}
	f.fail["other"] = 9
	f.apply(t, f.document(3, 102, "other", "must not submit caption alone"))
	f.advance(t, 3*time.Second)
	f.advance(t, 5*time.Second)
	if len(f.provider.receipts()) != 0 || f.m.store.snapshot().MediaInputs["update:3"].State != "failed" {
		t.Fatal("failed transfer forwarded caption or did not terminate")
	}
}

func TestMediaBDDPersistenceFailureDoesNotAcknowledgeOrLoseInput(t *testing.T) {
	f := newMediaFixture(t)
	u := f.document(2, 101, "doc", "retry same receipt")
	statePath := f.m.store.statePath
	f.m.store.statePath = filepath.Join(f.root, "missing-parent", "state.json")
	if err := f.m.handleUpdate(t.Context(), "fixture-token", u); err == nil {
		t.Fatal("failed staging was acknowledged")
	}
	if f.m.store.snapshot().NextOffset != 2 || len(f.provider.receipts()) != 0 || len(f.download) != 0 {
		t.Fatal("failed persistence gained authority")
	}
	f.m.store.statePath = statePath
	f.apply(t, u)
	f.reopen(t)
	f.apply(t, u)
	if len(f.provider.receipts()) != 1 || f.download["doc"] != 1 {
		t.Fatal("retry/restart lost receipt idempotency")
	}
}

func TestMediaBDDReceiptCapacityRejectsWithoutBlockingCommands(t *testing.T) {
	f := newMediaFixture(t)
	if err := f.m.store.mutate(func(s *durableState) error {
		for i := range maxMediaInputs {
			s.MediaInputs[fmt.Sprint(i)] = mediaInput{State: "accepted", CreatedAt: f.now}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.apply(t, f.document(2, 101, "doc", "quota"), topicUpdate(3, 101, "/status"))
	s := f.m.store.snapshot()
	if s.Processed["2"].Disposition != "media_rejected" || s.Processed["3"].Disposition != "command" || len(f.provider.receipts()) != 0 {
		t.Fatal("media capacity blocked unrelated commands")
	}
}
