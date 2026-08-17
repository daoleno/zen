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
	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/calendar"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

func TestRunWithReadyCallsBackAfterListenAndShutsDownCompatibly(t *testing.T) {
	addr := availableAddress(t)
	srv := newEventSubscriptionServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	ready := false
	err := srv.RunWithReady(ctx, addr, func() {
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
	assertServerEventSubscriptionsClosed(t, srv)
}

func TestRunWithReadyDoesNotCallBackWhenListenFails(t *testing.T) {
	occupied := testListener(t)
	srv := newEventSubscriptionServer(t)
	called := false
	err := srv.RunWithReady(context.Background(), occupied.Addr().String(), func() {
		called = true
	})
	if err == nil {
		t.Fatal("RunWithReady unexpectedly succeeded")
	}
	if called {
		t.Fatal("ready callback ran after listen failure")
	}
	assertServerEventSubscriptionsClosed(t, srv)
}

func testListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func availableAddress(t *testing.T) string {
	t.Helper()
	listener := testListener(t)
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func newEventSubscriptionServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	w := watcher.New(time.Second)
	workStore, err := work.NewStore(filepath.Join(root, "work"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workStore.Close() })
	brainStore, err := brain.NewStore(filepath.Join(root, "brain"))
	if err != nil {
		t.Fatal(err)
	}
	brainService := brain.NewService(brainStore, w, nil)
	calendarStore, err := calendar.NewStore(filepath.Join(root, "calendar"))
	if err != nil {
		t.Fatal(err)
	}
	srv := New(nil, w, nil, nil, workStore, nil, brainService)
	srv.SetCalendar(calendarStore, nil)
	return srv
}

func assertServerEventSubscriptionsClosed(t *testing.T, srv *Server) {
	t.Helper()
	assertSubscriptionClosed(t, "Work", srv.workSub)
	assertSubscriptionClosed(t, "Calendar", srv.calendarSub)
	assertSubscriptionClosed(t, "Brain Work", srv.brainWorkSub)
}

func assertSubscriptionClosed[T any](t *testing.T, name string, subscription <-chan T) {
	t.Helper()
	select {
	case _, ok := <-subscription:
		if ok {
			t.Fatalf("%s subscription remained open", name)
		}
	default:
		t.Fatalf("%s subscription remained open", name)
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
