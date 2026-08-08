//go:build linux

package watcher

import (
	"os/exec"
	"testing"
	"time"
)

// TestParseProcStatStartTicks pins the /proc/<pid>/stat field-22 extraction,
// including comm fields containing spaces and parentheses.
func TestParseProcStatStartTicks(t *testing.T) {
	cases := []struct {
		name string
		stat string
		want int64
		ok   bool
	}{
		{
			name: "plain comm",
			stat: "123 (pi) S 1 123 123 0 -1 4194560 100 0 0 0 50 0 0 0 20 0 1 0 424242 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0",
			want: 424242,
			ok:   true,
		},
		{
			name: "spaced and parenthesized comm",
			stat: "9 (pi (node) child) S 1 9 9 0 -1 4194560 0 0 0 0 0 0 0 0 20 0 1 0 777 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0",
			want: 777,
			ok:   true,
		},
		{
			name: "missing starttime field",
			stat: "123 (pi) S 1 123 123 0 -1 0",
			ok:   false,
		},
		{
			name: "no closing paren",
			stat: "123 pi S 1 123",
			ok:   false,
		},
		{
			name: "non-numeric starttime",
			stat: "123 (pi) S 1 123 123 0 -1 4194560 0 0 0 0 50 0 0 0 20 0 1 0 x 0",
			ok:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseProcStatStartTicks([]byte(tc.stat))
			if ok != tc.ok || (tc.ok && got != tc.want) {
				t.Fatalf("parseProcStatStartTicks(%q) = %d, %v; want %d, %v", tc.stat, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestProcessStartTimeFromProcRealProcess derives the precise start of a real
// spawned process and proves it is stable, lies within the same second window
// below the ps lstart rounding, and refines through refineProcessStartedAt.
func TestProcessStartTimeFromProcRealProcess(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	pid := cmd.Process.Pid

	precise, ok := processStartTimeFromProc(pid)
	if !ok {
		t.Fatalf("processStartTimeFromProc(%d) failed for a live process", pid)
	}
	now := time.Now().UTC()
	if precise.After(now) || now.Sub(precise) > time.Minute {
		t.Fatalf("derived start %v is implausible for a just-spawned process (now %v)", precise, now)
	}
	// Stability: repeated reads return the identical instant.
	again, ok := processStartTimeFromProc(pid)
	if !ok || !again.Equal(precise) {
		t.Fatalf("derived start not stable: %v then %v (ok=%v)", precise, again, ok)
	}

	// The ps lstart value truncates to whole seconds, so the precise start
	// of the same process lies in [rounded, rounded+1s) and the guard must
	// accept it.
	rounded := precise.Truncate(time.Second)
	if refined := refineProcessStartedAt(rounded, pid); !refined.Equal(precise) {
		t.Fatalf("refineProcessStartedAt(%v, %d) = %v, want precise %v", rounded, pid, refined, precise)
	}

	// Unknown pids and zero observations keep the observed value unchanged.
	observed := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	if got := refineProcessStartedAt(observed, 2147483647); !got.Equal(observed) {
		t.Fatalf("unknown pid must keep the observed start, got %v", got)
	}
	if got := refineProcessStartedAt(time.Time{}, pid); !got.IsZero() {
		t.Fatalf("zero observation must stay zero, got %v", got)
	}
	// A contradictory observation (wrong pid's second window) is kept.
	if got := refineProcessStartedAt(observed, pid); !got.Equal(observed) {
		t.Fatalf("inconsistent observation must be kept, got %v", got)
	}
}
