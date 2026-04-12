package terminal

import (
	"reflect"
	"testing"
)

func TestTmuxAttachCommandEnablesRGBClientFeatures(t *testing.T) {
	cmd := tmuxAttachCommand("zen-demo")

	want := []string{"tmux", "-T", "RGB,256", "attach-session", "-t", "zen-demo"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("tmuxAttachCommand args = %v, want %v", cmd.Args, want)
	}
}

func TestTmuxClientEnvOverridesTerminalCapabilities(t *testing.T) {
	env := tmuxClientEnv([]string{
		"TERM=screen-256color",
		"COLORTERM=24bit",
		"LANG=en_US.UTF-8",
	})

	got := map[string]string{}
	for _, entry := range env {
		for index := 0; index < len(entry); index += 1 {
			if entry[index] != '=' {
				continue
			}
			got[entry[:index]] = entry[index+1:]
			break
		}
	}

	if got["TERM"] != "xterm-256color" {
		t.Fatalf("TERM = %q, want %q", got["TERM"], "xterm-256color")
	}
	if got["COLORTERM"] != "truecolor" {
		t.Fatalf("COLORTERM = %q, want %q", got["COLORTERM"], "truecolor")
	}
	if got["LANG"] != "en_US.UTF-8" {
		t.Fatalf("LANG = %q, want %q", got["LANG"], "en_US.UTF-8")
	}
}

func TestTmuxResizeWindowCommandUsesLinkedSessionSize(t *testing.T) {
	cmd := tmuxResizeWindowCommand("zen-123-1", Size{Cols: 52, Rows: 41})

	want := []string{"tmux", "resize-window", "-t", "zen-123-1", "-x", "52", "-y", "41"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("resize-window args = %v, want %v", cmd.Args, want)
	}
}

func TestTmuxResizeWindowHookUsesFixedTargetAndSize(t *testing.T) {
	got := tmuxResizeWindowHook("zen-123-1", Size{Cols: 52, Rows: 41})
	want := "resize-window -t zen-123-1 -x 52 -y 41"
	if got != want {
		t.Fatalf("resize hook = %q, want %q", got, want)
	}
}

func TestTmuxHistoryCaptureRangeUsesOnlyScrollbackRegion(t *testing.T) {
	startLine, endLine := tmuxHistoryCaptureRange(10, 21)
	if startLine != -21 || endLine != -10 {
		t.Fatalf("history capture range = (%d, %d), want (%d, %d)", startLine, endLine, -21, -10)
	}
}

func TestTmuxHistoryCaptureRangeHandlesNoHistory(t *testing.T) {
	startLine, endLine := tmuxHistoryCaptureRange(10, 0)
	if startLine != 0 || endLine != -1 {
		t.Fatalf("history capture range for empty history = (%d, %d), want (%d, %d)", startLine, endLine, 0, -1)
	}
}
