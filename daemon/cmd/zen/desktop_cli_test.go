package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/daoleno/zen/daemon/desktop"
)

func TestMainHelpListsDesktopHost(t *testing.T) {
	var stderr bytes.Buffer
	_ = run([]string{"--help"}, &stderr)
	if !strings.Contains(stderr.String(), "desktop-host") || !strings.Contains(stderr.String(), "desktop-identity") {
		t.Fatalf("help missing desktop roles:\n%s", stderr.String())
	}
}

func TestDesktopIdentityJSON(t *testing.T) {
	stdout, err := captureStdout(func() error {
		return runDesktopIdentityCommand(os.Stderr)
	})
	if err != nil {
		t.Fatal(err)
	}
	var id desktop.Identity
	if json.Unmarshal(stdout, &id) != nil || id.SHA256 == "" || id.Executable == "" {
		t.Fatalf("identity=%s", stdout)
	}
	if len(id.Roles) != 3 {
		t.Fatalf("roles=%v", id.Roles)
	}
}

func captureStdout(fn func() error) ([]byte, error) {
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	os.Stdout = w
	runErr := fn()
	_ = w.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	_ = r.Close()
	return buf.Bytes(), runErr
}
