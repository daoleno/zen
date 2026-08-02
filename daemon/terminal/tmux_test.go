package terminal

import (
	"context"
	"reflect"
	"testing"
)

func TestTmuxNewViewSessionCommandCreatesIndependentWindowSelector(t *testing.T) {
	cmd := tmuxNewViewSessionCommand(context.Background(), "zen-view")

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
	cmd := tmuxLinkViewWindowCommand(
		context.Background(),
		"source:@12",
		"@99",
	)

	want := []string{"tmux", "link-window", "-k", "-s", "source:@12", "-t", "@99"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("tmuxLinkViewWindowCommand args = %v, want %v", cmd.Args, want)
	}
}

func TestTmuxAttachCommandIsANormalSizingClient(t *testing.T) {
	cmd := tmuxAttachCommand(context.Background(), "zen-demo")

	want := []string{
		"tmux",
		"-T",
		"RGB,256",
		"attach-session",
		"-t",
		"zen-demo",
	}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("tmuxAttachCommand args = %v, want %v", cmd.Args, want)
	}
}

func TestTmuxSessionSizeIsAlwaysThePhonePTYGrid(t *testing.T) {
	session := &tmuxSession{size: Size{Cols: 44, Rows: 18}}
	if got := session.Size(); got != (Size{Cols: 44, Rows: 18}) {
		t.Fatalf("tmuxSession.Size() = %+v, want phone grid 44x18", got)
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
