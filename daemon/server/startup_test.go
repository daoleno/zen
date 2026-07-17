package server

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestRunWithReadyCallsBackAfterListenAndShutsDownCompatibly(t *testing.T) {
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve address: %v", err)
	}
	addr := reserved.Addr().String()
	if err := reserved.Close(); err != nil {
		t.Fatalf("release address: %v", err)
	}

	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("new auth manager: %v", err)
	}
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	ready := false
	err = srv.RunWithReady(ctx, addr, func() {
		ready = true
		second, listenErr := net.Listen("tcp", addr)
		if listenErr == nil {
			_ = second.Close()
			t.Errorf("ready callback ran before address was acquired")
		}
		cancel()
	})
	if !ready {
		t.Fatal("ready callback was not called")
	}
	if !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("RunWithReady error = %v, want http.ErrServerClosed", err)
	}
}

func TestRunWithReadyDoesNotCallBackWhenListenFails(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy address: %v", err)
	}
	defer occupied.Close()

	called := false
	err = (&Server{}).RunWithReady(context.Background(), occupied.Addr().String(), func() {
		called = true
	})
	if err == nil {
		t.Fatal("RunWithReady unexpectedly succeeded")
	}
	if called {
		t.Fatal("ready callback ran after listen failure")
	}
}

func TestServerStartupIgnoresExistingSchema2CanonicalLedger(t *testing.T) {
	root := t.TempDir()
	authManager, err := auth.NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "canonical-chat", "chatthread-v2.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	schema2 := []byte(`{"schema_version":2,"threads":{"legacy":"must remain untouched"}}`)
	if err := os.WriteFile(path, schema2, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil); srv == nil {
		t.Fatal("server.New returned nil with an existing schema2 file")
	}
	afterBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterBytes, schema2) || after.Mode().Perm() != 0o600 ||
		!after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("server.New touched the retired Ledger: bytes=%t mode=%#o mtime_changed=%t",
			bytes.Equal(afterBytes, schema2), after.Mode().Perm(), !after.ModTime().Equal(before.ModTime()))
	}
}
