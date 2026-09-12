package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/attachment"
)

func TestFileDownloadRejectsTraversalRedirectOversizeAndSecrets(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch filepath.Base(r.URL.Path) {
		case "redirect":
			w.Header().Set("Location", "http://127.0.0.1:1/secret")
			w.WriteHeader(302)
		case "large":
			w.Header().Set("Content-Length", fmt.Sprint(maxTelegramFileBytes+1))
		default:
			w.WriteHeader(503)
		}
	}))
	defer server.Close()
	client := NewClient(server.URL, server.Client())
	for _, path := range []string{"../secret", "a/../../secret", "/etc/passwd", "http://127.0.0.1/x", "a/%2e%2e/x", "a\\x", "a?token=x", "a#x", "a//x"} {
		if body, err := client.DownloadFile(t.Context(), "fixture-secret", File{FilePath: path}); err == nil {
			body.Close()
			t.Fatalf("unsafe path allowed: %s", path)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("invalid path performed network IO")
	}
	for _, path := range []string{"redirect", "large", "error"} {
		_, err := client.DownloadFile(t.Context(), "fixture-secret", File{FilePath: path})
		if err == nil || strings.Contains(err.Error(), "fixture-secret") || strings.Contains(err.Error(), server.URL) {
			t.Fatalf("unsafe error: %v", err)
		}
	}
	if requests.Load() != 3 {
		t.Fatal("redirect followed or expected request missing")
	}
}

func TestMediaDownloadTimeoutSizeMismatchAndStreamingLimitLeaveNoFile(t *testing.T) {
	for _, mode := range []string{"timeout", "mismatch", "unknown-length", "image-mismatch"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/getFile") {
					size := int64(0)
					if mode == "mismatch" {
						size = 9
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": File{FileID: "owned", FilePath: "owned", FileSize: size}})
					return
				}
				if mode == "timeout" {
					<-r.Context().Done()
					return
				}
				w.Header().Set("Content-Type", "application/octet-stream")
				w.(http.Flusher).Flush()
				_, _ = io.WriteString(w, strings.Repeat("x", 32))
			}))
			defer server.Close()
			m := &Manager{api: NewClient(server.URL, server.Client()), attachments: &attachment.Store{Dir: filepath.Join(t.TempDir(), "uploads")}, mediaTimeout: 50 * time.Millisecond}
			part := mediaPart{Remote: File{FileID: "owned"}, File: attachment.File{Name: "file.dat", ContentType: "application/octet-stream", Size: -1}}
			limit := int64(8)
			if mode == "mismatch" {
				part.File.Size = 8
				limit = 32
			}
			if mode == "image-mismatch" {
				part.File.Kind = "image"
				part.File.ContentType = "image/jpeg"
				limit = 32
			}
			started := time.Now()
			if _, err := m.downloadMedia(t.Context(), "fixture-token", part, limit); err == nil {
				t.Fatal("invalid download stored")
			}
			if time.Since(started) > time.Second {
				t.Fatal("download exceeded deadline")
			}
			entries, err := os.ReadDir(m.attachments.Dir)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("partial data retained: %v", entries)
			}
		})
	}
}

func TestMediaSelectionKeepsSafeNameMIMEAndHonestStickerAudioLabels(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		message                 Message
		kind, mime, description string
	}{
		{"static", Message{Sticker: &Sticker{File: File{FileID: "x"}, Emoji: "\U0001f44d"}}, "image", "image/webp", "Static sticker"},
		{"animated", Message{Sticker: &Sticker{File: File{FileID: "x"}, Emoji: "\U0001f44d", IsAnimated: true}}, "file", "application/x-tgsticker", "not interpreted"},
		{"video-sticker", Message{Sticker: &Sticker{File: File{FileID: "x"}, Emoji: "\U0001f44d", IsVideo: true}}, "file", "video/webm", "not interpreted"},
		{"voice", Message{Voice: &MediaFile{File: File{FileID: "x"}, MIMEType: "audio/ogg"}}, "file", "audio/ogg", "no transcription"},
		{"document", Message{Document: &MediaFile{File: File{FileID: "x", FileSize: 4}, FileName: "\u62a5\u544a\U0001f4ce.pdf", MIMEType: "application/pdf"}}, "file", "application/pdf", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			part, err := mediaSelection(tc.message)
			if err != nil || part.File.Kind != tc.kind || part.File.ContentType != tc.mime || !strings.Contains(part.File.Description, tc.description) {
				t.Fatalf("selection=%+v error=%v", part, err)
			}
			if tc.message.Sticker != nil && !strings.Contains(part.File.Description, tc.message.Sticker.Emoji) {
				t.Fatal("sticker emoji lost")
			}
			if tc.message.Document != nil && part.File.Name != tc.message.Document.FileName {
				t.Fatal("safe display name changed")
			}
		})
	}
}

func TestClientInteractionRequestsAreTypedAndMessageScoped(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
	}))
	defer server.Close()
	c := NewClient(server.URL, server.Client())
	for _, reactions := range [][]ReactionType{{{Type: "emoji", Emoji: "\U0001f44d"}}, {}} {
		if err := c.SetMessageReaction(context.Background(), "fixture-token", ReactionRequest{ChatID: 10, MessageID: 20, Reaction: reactions}); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.PinChatMessage(t.Context(), "fixture-token", 10, 20); err != nil {
		t.Fatal(err)
	}
	for _, body := range bodies {
		if body["chat_id"] != float64(10) || body["message_id"] != float64(20) || body["message_thread_id"] != nil {
			t.Fatalf("wrong scope: %v", body)
		}
	}
	if len(bodies[1]["reaction"].([]any)) != 0 || bodies[2]["disable_notification"] != true {
		t.Fatal("removal/pin encoding changed")
	}
}
