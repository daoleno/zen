package terminal

import (
	"reflect"
	"testing"
)

func TestTmuxNewViewSessionCommandCreatesIndependentProjection(t *testing.T) {
	cmd := tmuxNewViewSessionCommand("zen-view")

	want := []string{
		"tmux",
		"new-session",
		"-d",
		"-P",
		"-F",
		"#{window_id}",
		"-s",
		"zen-view",
		"sleep 86400",
	}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("tmuxNewViewSessionCommand args = %v, want %v", cmd.Args, want)
	}
}

func TestTmuxLinkViewWindowCommandReplacesOnlyBootstrapWindow(t *testing.T) {
	cmd := tmuxLinkViewWindowCommand("source:@12", "@99")

	want := []string{"tmux", "link-window", "-k", "-s", "source:@12", "-t", "@99"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("tmuxLinkViewWindowCommand args = %v, want %v", cmd.Args, want)
	}
}

func TestTmuxAttachCommandIgnoresViewSizeAndKeepsInputEnabled(t *testing.T) {
	cmd := tmuxAttachCommand("zen-demo")

	want := []string{
		"tmux",
		"-T",
		"RGB,256",
		"attach-session",
		"-f",
		"ignore-size",
		"-t",
		"zen-demo",
	}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("tmuxAttachCommand args = %v, want %v", cmd.Args, want)
	}
}

func TestTmuxStatusLines(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    int
		wantErr bool
	}{
		{name: "off", value: "off", want: 0},
		{name: "zero", value: "0", want: 0},
		{name: "on", value: "on", want: 1},
		{name: "multiple", value: "3", want: 3},
		{name: "empty", value: "", wantErr: true},
		{name: "too large", value: "6", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := tmuxStatusLines(test.value)
			if test.wantErr {
				if err == nil {
					t.Fatalf("tmuxStatusLines(%q) = %d, want error", test.value, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("tmuxStatusLines(%q): %v", test.value, err)
			}
			if got != test.want {
				t.Fatalf("tmuxStatusLines(%q) = %d, want %d", test.value, got, test.want)
			}
		})
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

func TestTmuxWindowRefExtractsWindowIndex(t *testing.T) {
	got := tmuxWindowRef("main:12")
	if got != "12" {
		t.Fatalf("tmuxWindowRef() = %q, want %q", got, "12")
	}
}

func TestTmuxWindowRefStripsPaneSuffix(t *testing.T) {
	got := tmuxWindowRef("main:12.1")
	if got != "12" {
		t.Fatalf("tmuxWindowRef() with pane suffix = %q, want %q", got, "12")
	}
}

func TestTmuxWindowRefHandlesInvalidTarget(t *testing.T) {
	got := tmuxWindowRef("main")
	if got != "" {
		t.Fatalf("tmuxWindowRef() for invalid target = %q, want empty string", got)
	}
}

func TestTmuxSourceWindowTarget(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		want    string
		wantErr bool
	}{
		{name: "session current window", target: "main", want: "main"},
		{name: "stable window id", target: "main:@12", want: "main:@12"},
		{name: "numeric window and pane", target: "main:12.1", want: "main:12"},
		{name: "empty", target: " ", wantErr: true},
		{name: "missing session", target: ":12", wantErr: true},
		{name: "missing window", target: "main:", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := tmuxSourceWindowTarget(test.target)
			if test.wantErr {
				if err == nil {
					t.Fatalf("tmuxSourceWindowTarget(%q) = %q, want error", test.target, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("tmuxSourceWindowTarget(%q): %v", test.target, err)
			}
			if got != test.want {
				t.Fatalf("tmuxSourceWindowTarget(%q) = %q, want %q", test.target, got, test.want)
			}
		})
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
