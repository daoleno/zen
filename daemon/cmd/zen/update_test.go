package main

import "testing"

func TestStartupNoticeSuppression(t *testing.T) {
	tests := []struct {
		name        string
		interactive bool
		term        string
		ci          string
		json        bool
		want        bool
	}{
		{name: "interactive", interactive: true, term: "xterm-256color", want: true},
		{name: "noninteractive", term: "xterm-256color"},
		{name: "dumb terminal", interactive: true, term: "dumb"},
		{name: "CI", interactive: true, term: "xterm", ci: "true"},
		{name: "JSON", interactive: true, term: "xterm", json: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := startupNoticeAllowed(test.interactive, test.term, test.ci, test.json); got != test.want {
				t.Fatalf("startupNoticeAllowed() = %v, want %v", got, test.want)
			}
		})
	}
}
