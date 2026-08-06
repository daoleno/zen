package work

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderResumeTokenContracts(t *testing.T) {
	piPath := filepath.Join(t.TempDir(), "session.jsonl")
	tests := []struct {
		name     string
		provider string
		command  string
		want     string
		present  bool
		wantErr  error
	}{
		{name: "codex none", provider: AgentProviderCodex, command: "codex --dangerously-bypass-approvals-and-sandbox"},
		{name: "codex resume", provider: AgentProviderCodex, command: "codex resume thread-1", want: "thread-1", present: true},
		{name: "codex env resume", provider: AgentProviderCodex, command: `env PATH='/opt:$PATH' codex resume 'thread-1'`, want: "thread-1", present: true},
		{name: "codex config value not subcommand", provider: AgentProviderCodex, command: "codex --config resume"},
		{name: "codex unknown bare flag before resume", provider: AgentProviderCodex, command: "codex --experimental-foo resume 019fd717-589c-7a11-9966-917f43dc336a", wantErr: ErrResumeAmbiguous},
		{name: "codex unknown bare flag alone", provider: AgentProviderCodex, command: "codex --experimental-foo", wantErr: ErrResumeAmbiguous},
		{name: "codex known boolean then resume", provider: AgentProviderCodex, command: "codex --dangerously-bypass-approvals-and-sandbox resume thread-1", want: "thread-1", present: true},
		{name: "codex known value flag", provider: AgentProviderCodex, command: "codex --profile default"},
		{name: "codex equals unknown form", provider: AgentProviderCodex, command: "codex --experimental-foo=bar"},
		{name: "codex missing value", provider: AgentProviderCodex, command: "codex resume", present: true, wantErr: ErrResumeMissingValue},
		{name: "codex duplicate resume", provider: AgentProviderCodex, command: "codex resume a resume b", present: true, wantErr: ErrResumeAmbiguous},
		{name: "claude resume", provider: AgentProviderClaude, command: "claude --resume sess", want: "sess", present: true},
		{name: "claude duplicate resume", provider: AgentProviderClaude, command: "claude --resume a --resume b", present: true, wantErr: ErrResumeAmbiguous},
		{name: "claude resume and -r", provider: AgentProviderClaude, command: "claude --resume a -r b", present: true, wantErr: ErrResumeAmbiguous},
		{name: "claude missing value", provider: AgentProviderClaude, command: "claude --resume", present: true, wantErr: ErrResumeMissingValue},
		{name: "grok duplicate", provider: AgentProviderGrok, command: "grok --resume a --resume b", present: true, wantErr: ErrResumeAmbiguous},
		{name: "cursor duplicate", provider: AgentProviderCursor, command: "cursor-agent --resume a --resume b", present: true, wantErr: ErrResumeAmbiguous},
		{name: "opencode ses", provider: AgentProviderOpenCode, command: "opencode -s ses_abc", want: "ses_abc", present: true},
		{name: "opencode -s and --session", provider: AgentProviderOpenCode, command: "opencode -s ses_a --session ses_b", present: true, wantErr: ErrResumeAmbiguous},
		{name: "opencode duplicate -s", provider: AgentProviderOpenCode, command: "opencode -s ses_a -s ses_b", present: true, wantErr: ErrResumeAmbiguous},
		{name: "pi session", provider: AgentProviderPi, command: "pi --session " + piPath, want: piPath, present: true},
		{name: "pi duplicate session", provider: AgentProviderPi, command: "pi --session " + piPath + " --session " + piPath, present: true, wantErr: ErrResumeAmbiguous},
		{name: "pi session and session-dir", provider: AgentProviderPi, command: "pi --session " + piPath + " --session-dir /tmp/x", present: true, wantErr: ErrResumeAmbiguous},
		{name: "unparseable", provider: AgentProviderCodex, command: "codex && true", wantErr: ErrLaunchUnparseable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token, present, err := ProviderResumeToken(tc.provider, tc.command)
			if tc.wantErr != nil {
				if err == nil || !errors.Is(err, tc.wantErr) && !strings.Contains(err.Error(), tc.wantErr.Error()) {
					t.Fatalf("err=%v want %v", err, tc.wantErr)
				}
				if present != tc.present {
					t.Fatalf("present=%v want %v", present, tc.present)
				}
				return
			}
			if err != nil || present != tc.present || token != tc.want {
				t.Fatalf("got (%q,%v,%v) want (%q,%v,nil)", token, present, err, tc.want, tc.present)
			}
		})
	}
}

func TestWithProviderResumeTokenContracts(t *testing.T) {
	piPath := filepath.Join(t.TempDir(), "owned.jsonl")
	tests := []struct {
		name     string
		provider string
		command  string
		token    string
		wantErr  error
		wantSub  string
	}{
		{name: "codex env inject", provider: AgentProviderCodex, command: `env PATH='/opt:$PATH' codex --dangerously-bypass-approvals-and-sandbox`, token: "thread-1", wantSub: "resume"},
		{name: "codex unknown bare flag tail inject", provider: AgentProviderCodex, command: "codex --experimental-foo", token: "thread-1", wantErr: ErrResumeAmbiguous},
		{name: "codex unknown bare flag before resume inject", provider: AgentProviderCodex, command: "codex --experimental-foo resume 019fd717-589c-7a11-9966-917f43dc336a", token: "019fd717-589c-7a11-9966-917f43dc336a", wantErr: ErrResumeAmbiguous},
		{name: "codex equals unknown inject ok", provider: AgentProviderCodex, command: "codex --experimental-foo=bar", token: "thread-1", wantSub: "resume"},
		{name: "codex known value then inject", provider: AgentProviderCodex, command: "codex --profile default", token: "thread-1", wantSub: "resume"},
		{name: "codex conflicting", provider: AgentProviderCodex, command: `env PATH='/opt:$PATH' codex resume other`, token: "thread-1", wantErr: errors.New("already resumes")},
		{name: "codex matching", provider: AgentProviderCodex, command: "codex resume thread-1", token: "thread-1", wantSub: "codex resume thread-1"},
		{name: "codex after terminator", provider: AgentProviderCodex, command: "codex -- --help", token: "thread-1", wantErr: ErrResumeTerminated},
		{name: "claude inject", provider: AgentProviderClaude, command: "claude", token: "s1", wantSub: "--resume"},
		{name: "opencode non-ses", provider: AgentProviderOpenCode, command: "opencode --auto", token: "not-ses", wantErr: errors.New("ses_*")},
		{name: "opencode inject", provider: AgentProviderOpenCode, command: "opencode --auto", token: "ses_ok", wantSub: "-s"},
		{name: "pi inject", provider: AgentProviderPi, command: "pi", token: piPath, wantSub: "--session"},
		{name: "unparseable", provider: AgentProviderCodex, command: "codex; rm -rf /", token: "t", wantErr: ErrLaunchUnparseable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := WithProviderResumeToken(tc.provider, tc.command, tc.token)
			if tc.wantErr != nil {
				if err == nil || (!errors.Is(err, tc.wantErr) && !strings.Contains(err.Error(), tc.wantErr.Error())) {
					t.Fatalf("err=%v want containing %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got, tc.wantSub) || !strings.Contains(got, tc.token) {
				t.Fatalf("got %q", got)
			}
			token, present, err := ProviderResumeToken(tc.provider, got)
			if err != nil || !present || token != tc.token {
				t.Fatalf("round-trip (%q,%v,%v)", token, present, err)
			}
		})
	}
}

func TestCodexSessionIDFromRolloutPath(t *testing.T) {
	id := "019fd717-589c-7a11-9966-917f43dc336a"
	path := "/home/daoleno/.codex/sessions/2026/08/06/rollout-2026-08-06T20-40-59-" + id + ".jsonl"
	if got := CodexSessionIDFromRolloutPath(path); got != id {
		t.Fatalf("got %q", got)
	}
}
