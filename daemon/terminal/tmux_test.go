package terminal

import (
	"reflect"
	"testing"
)

func TestTmuxWindowLinkedHookCommandConfiguresWindowSizing(t *testing.T) {
	got := tmuxWindowLinkedHookCommand()
	want := `run-shell "tmux set-window-option -t #{hook_window} window-size latest; tmux set-window-option -t #{hook_window} aggressive-resize on"`
	if got != want {
		t.Fatalf("tmuxWindowLinkedHookCommand() = %q, want %q", got, want)
	}
}

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

func TestTmuxHistoryCaptureRangeCapsLargeHistoryToStartupBudget(t *testing.T) {
	startLine, endLine := tmuxHistoryCaptureRange(36, 5000)
	if startLine != -144 || endLine != -36 {
		t.Fatalf("history capture range for large history = (%d, %d), want (%d, %d)", startLine, endLine, -144, -36)
	}
}

func TestTmuxHistoryCaptureRangeRespectsMaximumLineBudget(t *testing.T) {
	startLine, endLine := tmuxHistoryCaptureRange(80, 5000)
	if startLine != -240 || endLine != -80 {
		t.Fatalf("history capture range for tall pane = (%d, %d), want (%d, %d)", startLine, endLine, -240, -80)
	}
}
