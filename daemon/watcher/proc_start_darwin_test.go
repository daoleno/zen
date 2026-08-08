//go:build darwin

package watcher

import (
	"os"
	"testing"
	"time"
)

func TestProcessStartTimeFromDarwinCurrentProcess(t *testing.T) {
	started, ok := processStartTimeFromProc(os.Getpid())
	if !ok {
		t.Fatal("processStartTimeFromProc did not return Darwin kernel starttime")
	}
	if started.IsZero() {
		t.Fatal("Darwin process starttime is zero")
	}
	again, ok := processStartTimeFromProc(os.Getpid())
	if !ok || !again.Equal(started) {
		t.Fatalf("Darwin process starttime is not stable: %v then %v", started, again)
	}

	// The kernel evidence must pass the same consistency guard the watcher
	// applies on every platform: ps lstart truncates to whole seconds, so
	// the precise starttime lies in [rounded, rounded+1s) and must refine
	// the rounded observation instead of being rejected.
	rounded := started.Truncate(time.Second)
	if refined := refineProcessStartedAt(rounded, os.Getpid()); !refined.Equal(started) {
		t.Fatalf("refineProcessStartedAt(%v, %d) = %v, want precise %v", rounded, os.Getpid(), refined, started)
	}
	// A contradictory observation (start rounded two seconds down) lies
	// outside the guard and must be kept unchanged, never widened.
	observed := rounded.Add(-2 * time.Second)
	if got := refineProcessStartedAt(observed, os.Getpid()); !got.Equal(observed) {
		t.Fatalf("inconsistent observation must be kept, got %v", got)
	}
	// Zero observations are never refined.
	if got := refineProcessStartedAt(time.Time{}, os.Getpid()); !got.IsZero() {
		t.Fatalf("zero observation must stay zero, got %v", got)
	}
	// Invalid and unknown pids yield no evidence.
	if _, ok := processStartTimeFromProc(0); ok {
		t.Fatal("zero pid must not yield starttime evidence")
	}
	if _, ok := processStartTimeFromProc(2147483647); ok {
		t.Fatal("unknown pid must not yield starttime evidence")
	}
}
