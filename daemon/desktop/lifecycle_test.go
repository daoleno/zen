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
	for _, scenario := range []string{"stop", "disconnect", "revoke", "shutdown", "view-input", "repeat-start", "invalid-batch", "before-grant", "wayland-stop", "wayland-revoke", "wayland-wrong-source"} {
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
			script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ZEN_DESKTOP_TEST_ARGS\"\nprintf '" + escaped.String() + "'\ncat > \"$ZEN_DESKTOP_TEST_INPUT\"\nprintf closed > \"$ZEN_DESKTOP_TEST_CLOSED\"\n"
			if err := os.WriteFile(helper, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("ZEN_DESKTOP_HELPER", helper)
			backend := "x11"
			if strings.HasPrefix(scenario, "wayland-") {
				backend = "wayland"
			}
			t.Setenv("ZEN_DESKTOP_BACKEND", backend)
			t.Setenv("ZEN_DESKTOP_BUS_ADDRESS", "unix:path=/owned-fixture-never-opened")
			t.Setenv("ZEN_DESKTOP_TEST_ARGS", filepath.Join(dir, "args"))
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
			var inventory struct {
				Sources []struct{ ID string } `json:"sources"`
			}
			if err := conn.ReadJSON(&inventory); err != nil || len(inventory.Sources) != 1 || inventory.Sources[0].ID != backend {
				t.Fatal("incorrect configured source inventory", inventory, err)
			}
			selected := backend
			if scenario == "wayland-wrong-source" {
				selected = "x11"
			}
			if err := conn.WriteJSON(Command{Type: "start", Source: selected, Control: scenario != "view-input"}); err != nil {
				t.Fatal(err)
			}
			if scenario == "wayland-wrong-source" {
				var status map[string]any
				if err := conn.ReadJSON(&status); err != nil || status["state"] != "disconnected" {
					t.Fatal("stale source was not rejected", status, err)
				}
				select {
				case <-finished:
				case <-time.After(time.Second):
					t.Fatal("stale source retained ownership")
				}
				if _, err := os.Stat(filepath.Join(dir, "args")); !os.IsNotExist(err) {
					t.Fatal("stale source started a helper", err)
				}
				return
			}
			if scenario != "before-grant" {
				if _, _, err := conn.ReadMessage(); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "stop", "wayland-stop":
				_ = conn.WriteJSON(Command{Type: "key", Code: 97, Down: true})
				_ = conn.WriteJSON(Command{Type: "stop"})
			case "disconnect":
				_ = conn.Close()
			case "revoke", "wayland-revoke":
				manager.Revoke("fixture")
			case "shutdown":
				manager.Close()
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
			if strings.HasSuffix(scenario, "stop") {
				if !bytes.Contains(input, []byte("key ")) {
					t.Fatal("granted input was not forwarded")
				}
			} else if len(input) != 0 {
				t.Fatalf("unexpected input: %s", input)
			}
			args, err := os.ReadFile(filepath.Join(dir, "args"))
			if err != nil || (strings.Contains(string(args), "--wayland\n--bus-address\nunix:path=/owned-fixture-never-opened") != (backend == "wayland")) {
				t.Fatal("helper backend escaped configured source", string(args), err)
			}
		})
	}
}
