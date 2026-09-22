package work

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestDSHNativeLogStreamingAndReloadIdentity(t *testing.T) {
	rows := []string{
		`{"type":"session","version":0,"id":"session-fixture","cwd":"/fixture"}`,
		`{"type":"turn/start","seq":0,"time":1000,"data":{"turn":1}}`,
		`{"type":"user/message","seq":1,"time":1001,"data":{"id":"user-1","source":{"kind":"user"},"content":[{"type":"text","text":"inspect"}]}}`,
		`{"type":"user/message","seq":2,"time":1002,"data":{"id":"system-1","source":{"kind":"plugin"},"content":[{"type":"text","text":"private runtime context"}]}}`,
		`{"type":"text-chunks","seq0":3,"time0":1003,"data":{"turn":1,"step":1,"index":0,"dt":[1,1],"texts":["hel","lo","!"]}}`,
	}
	path := filepath.Join(t.TempDir(), "session.jsonl.zstd")
	write := func() {
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		encoder, _ := zstd.NewWriter(file)
		for _, row := range rows {
			encoder.Write([]byte(row + "\n"))
		}
		encoder.Close()
		file.Close()
	}
	write()
	partial, err := parseDSHConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(partial.Events) != 2 || partial.Events[1].Body != "hello!" || !partial.Events[1].Partial {
		t.Fatalf("partial=%+v", partial.Events)
	}
	rows = append(rows,
		`{"type":"assistant/message","seq":6,"time":1006,"data":{"turn":1,"step":1,"message":{"id":"native-1","content":[{"type":"text","text":"hello!"}]}}}`,
		`{"type":"tool/call","seq":7,"time":1007,"data":{"turn":1,"step":1,"callId":"call-1","name":"read_file","arguments":"{}"}}`,
		`{"type":"tool/result","seq":8,"time":1008,"data":{"turn":1,"step":1,"message":{"content":[{"type":"tool-result","toolCallId":"call-1","content":[{"type":"text","text":"result"},{"type":"image","attachment":{"attachmentId":"image-1"}}]}]}}}`,
		`{"type":"turn/end","seq":9,"time":1009,"data":{"turn":1,"reason":{"kind":"completed"}}}`)
	write()
	complete, err := parseDSHConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	if complete.Events[1].ID != partial.Events[1].ID || complete.Events[1].Seq != partial.Events[1].Seq || complete.Events[1].Partial {
		t.Fatal("stream/reload identity changed")
	}
	if len(complete.Events) != 3 || complete.Events[2].CallID != "call-1" || complete.Events[2].Output != "result\n![Image](dsh-attachment:image-1)" || complete.Events[2].Partial {
		t.Fatalf("tool correlation=%+v", complete.Events)
	}

	if complete.Activity == nil || complete.Activity.Status != ProviderActivityCompleted {
		t.Fatalf("activity=%+v", complete.Activity)
	}
	_, _ = json.Marshal(complete)
}

func TestDSHLaunchAndLockOwnership(t *testing.T) {
	t.Setenv("ZEN_STATE_DIR", t.TempDir())
	command, err := EnsureDSHSessionLaunchCommand("dsh")
	if err != nil {
		t.Fatal(err)
	}
	if InferWorkerProvider(command) != WorkerProviderDSH || DSHSessionID(command) == "" {
		t.Fatalf("command=%s", command)
	}
	again, err := EnsureDSHSessionLaunchCommand(command)
	if err != nil || again != command {
		t.Fatal("launch ownership changed")
	}
	path := filepath.Join(t.TempDir(), "lock")
	unlock, err := lockDSHSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if release, err := lockDSHSession(path); err == nil {
		release()
		t.Fatal("duplicate owner admitted")
	}
	unlock()
	release, err := lockDSHSession(path)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestDSHNativeImageIntakeRejectsForeignPaths(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ZEN_STATE_DIR", root)
	if err := os.MkdirAll(filepath.Join(root, "uploads"), 0700); err != nil {
		t.Fatal(err)
	}
	image, err := os.ReadFile("../../app/assets/reading-fixture/normal.png")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "uploads", "photo.png")
	if err := os.WriteFile(path, image, 0600); err != nil {
		t.Fatal(err)
	}
	body := "<zen_attachments>{\"files\":[{\"name\":\"photo.png\",\"path\":" + strconv.Quote(path) + "}]}</zen_attachments>"
	blocks, err := dshPromptContent(body)
	if err != nil || len(blocks) != 2 || blocks[1]["type"] != "image" {
		t.Fatalf("native intake: %v %d", err, len(blocks))
	}
	outside := filepath.Join(t.TempDir(), "foreign.png")
	if err := os.WriteFile(outside, image, 0600); err != nil {
		t.Fatal(err)
	}
	foreign := strings.Replace(body, path, outside, 1)
	if _, err := dshPromptContent(foreign); err == nil {
		t.Fatal("foreign path acquired image authority")
	}
	link := filepath.Join(root, "uploads", "escape.png")
	if err := os.Symlink(outside, link); err == nil {
		if _, err := dshPromptContent(strings.Replace(body, path, link, 1)); err == nil {
			t.Fatal("symlink escape accepted")
		}
	}
}

func TestDSHPersistedImageReplacesPhoneUploadReference(t *testing.T) {
	text := `photo <zen_attachments>{"files":[{"name":"photo","path":"/uploads/temporary","content_type":"image/png"}]}</zen_attachments>`
	var blocks []dshBlock
	encoded := `[{"type":"text","text":` + strconv.Quote(text) + `},{"type":"image","attachment":{"attachmentId":"durable-image","mediaType":"image/png","name":"photo"}}]`
	if err := json.Unmarshal([]byte(encoded), &blocks); err != nil {
		t.Fatal(err)
	}
	display, exact := dshUserContent(blocks)
	if exact != text || strings.Contains(display, "/uploads/temporary") || !strings.Contains(display, "dsh-attachment:durable-image") || strings.Contains(display, "![Image]") {
		t.Fatalf("unexpected projection %q", display)
	}
}
