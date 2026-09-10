package watcher

import (
	"errors"
	"testing"
)

// TestResolveDelegatedAbsenceUsesAuthoritativeInventory covers the restart /
// reconcile decision boundary. The decision is based on one authoritative exact
// inventory: a transport failure is Unknown, while a successful inventory is
// authoritative for both presence and ownership of the exact window. A
// transient target-probe failure followed by a recovered server that still
// owns the target must stay live, never gone.
func TestResolveDelegatedAbsenceUsesAuthoritativeInventory(t *testing.T) {
	tests := []struct {
		name     string
		script   string
		wantGone bool
		wantErr  bool
	}{
		{
			name: "unreachable server is unknown not absence",
			script: `echo "failed to connect to server" >&2
exit 1`,
			wantErr: true,
		},
		{
			name: "reachable inventory without target proves absence",
			script: `cmd=
for arg in "$@"; do
  case "$arg" in list-windows|list-panes|list-sessions) cmd=$arg ;; esac
done
if [ "$cmd" = "list-windows" ]; then printf 'other:@7\t1\n'; exit 0; fi
if [ "$cmd" = "list-panes" ]; then echo "can't find session: main:@1" >&2; exit 1; fi
echo "unexpected tmux invocation: $*" >&2
exit 1`,
			wantGone: true,
		},
		{
			name: "owned live target is not absent",
			script: `cmd=
for arg in "$@"; do
  case "$arg" in list-windows|list-panes|list-sessions) cmd=$arg ;; esac
done
if [ "$cmd" = "list-windows" ]; then printf 'main:@1\t1\n'; exit 0; fi
echo "unexpected tmux invocation: $*" >&2
exit 1`,
			wantGone: false,
		},
		{
			name: "present foreign target is positive replacement",
			script: `cmd=
for arg in "$@"; do
  case "$arg" in list-windows|list-panes|list-sessions) cmd=$arg ;; esac
done
if [ "$cmd" = "list-windows" ]; then printf 'main:@1\t0\n'; exit 0; fi
echo "unexpected tmux invocation: $*" >&2
exit 1`,
			wantGone: true,
		},
		{
			// Regression: the legacy two-call probe saw no-server from
			// list-panes and then declared the target gone because a later
			// list-sessions reached a recovered server. The single exact
			// inventory must stay live.
			name: "recovered server with owned target stays live",
			script: `cmd=
for arg in "$@"; do
  case "$arg" in list-windows|list-panes|list-sessions) cmd=$arg ;; esac
done
if [ "$cmd" = "list-windows" ]; then printf 'main:@1\t1\n'; exit 0; fi
if [ "$cmd" = "list-panes" ]; then echo "failed to connect to server" >&2; exit 1; fi
if [ "$cmd" = "list-sessions" ]; then echo main; exit 0; fi
echo "unexpected tmux invocation: $*" >&2
exit 1`,
			wantGone: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("PATH", writeFakeTmux(t, test.script))
			w := New(0)
			gone, err := w.ResolveDelegatedAbsence("main:@1")
			if test.wantErr {
				if err == nil || !errors.Is(err, ErrOwnershipProbeUnavailable) {
					t.Fatalf("absence gone=%v err=%v, want ErrOwnershipProbeUnavailable", gone, err)
				}
				return
			}
			if err != nil || gone != test.wantGone {
				t.Fatalf("absence gone=%v err=%v, want gone=%v", gone, err, test.wantGone)
			}
		})
	}
}
