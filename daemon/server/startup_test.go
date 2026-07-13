package server

import (
	"context"
	"errors"
	"net"
	"net/http"
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
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil, nil)
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
