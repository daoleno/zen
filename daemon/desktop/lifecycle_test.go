package desktop

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestHelperLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("X11 helper process fixture requires a POSIX shell")
	}
	for _, scenario := range []string{"stop", "disconnect", "view-input", "repeat-start", "invalid-batch", "before-grant"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			var packet bytes.Buffer
			payload := []byte(`{"state":"streaming"}`)
			kind := byte(1)
			if scenario == "before-grant" {
				payload, kind = []byte{0, 0, 1, 101}, 2
			}
			_ = binary.Write(&packet, binary.BigEndian, uint32(len(payload)+1))
			packet.WriteByte(kind)
			packet.Write(payload)
			var escaped strings.Builder
			for _, b := range packet.Bytes() {
				fmt.Fprintf(&escaped, "\\%03o", b)
			}
			helper := filepath.Join(dir, "helper")
			// A deterministic pipe peer, not a display/capture or consent bypass.
			script := "#!/bin/sh\nprintf '" + escaped.String() + "'\ncat > \"$ZEN_DESKTOP_TEST_INPUT\"\nprintf closed > \"$ZEN_DESKTOP_TEST_CLOSED\"\n"
			if err := os.WriteFile(helper, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("ZEN_DESKTOP_HELPER", helper)
			t.Setenv("ZEN_DESKTOP_DISPLAY", ":not-a-real-display")
			t.Setenv("ZEN_DESKTOP_TEST_INPUT", filepath.Join(dir, "input"))
			t.Setenv("ZEN_DESKTOP_TEST_CLOSED", filepath.Join(dir, "closed"))
			var manager Manager
			finished := make(chan struct{})
			host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				u := websocket.Upgrader{}
				conn, err := u.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer close(finished)
				manager.Serve(conn, "fixture", "Fixture", func() bool { return true })
			}))
			defer host.Close()
			defer manager.Close()
			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(host.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			if _, _, err := conn.ReadMessage(); err != nil {
				t.Fatal(err)
			}
			if err := conn.WriteJSON(Command{Type: "start", Source: "x11", Control: scenario != "view-input"}); err != nil {
				t.Fatal(err)
			}
			if scenario != "before-grant" {
				if _, _, err := conn.ReadMessage(); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "stop":
				_ = conn.WriteJSON(Command{Type: "key", Code: 97, Down: true})
				_ = conn.WriteJSON(Command{Type: "stop"})
			case "disconnect":
				_ = conn.Close()
			case "view-input":
				_ = conn.WriteJSON(Command{Type: "key", Code: 97, Down: true})
			case "repeat-start":
				_ = conn.WriteJSON(Command{Type: "start", Source: "x11"})
			case "invalid-batch":
				_ = conn.WriteJSON(Command{Type: "batch", Events: []Command{{Type: "key", Code: 97, Down: true}, {Type: "invalid"}}})
			}
			if scenario != "disconnect" {
				if _, _, err := conn.ReadMessage(); err == nil {
					t.Fatal("invalid lifecycle retained media ownership")
				}
			}
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				t.Fatal("helper did not terminate")
			}
			if _, err := os.Stat(filepath.Join(dir, "closed")); err != nil {
				t.Fatal("helper did not receive graceful EOF", err)
			}
			input, err := os.ReadFile(filepath.Join(dir, "input"))
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "stop" {
				if !bytes.Contains(input, []byte("key ")) {
					t.Fatal("granted input was not forwarded")
				}
			} else if len(input) != 0 {
				t.Fatalf("unexpected input: %s", input)
			}
		})
	}
}
