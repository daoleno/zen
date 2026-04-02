package push

import "testing"

func TestFormatNotificationAgentLabel(t *testing.T) {
	tests := []struct {
		name      string
		agentName string
		agentID   string
		want      string
	}{
		{name: "clean shell path and session suffix", agentName: "./bin/zen-daemon (main:7)", agentID: "main:7", want: "zen-daemon"},
		{name: "fallback to agent id", agentName: "", agentID: "main:7", want: "main:7"},
		{name: "keep simple project name", agentName: "backend-api", agentID: "main:7", want: "backend-api"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatNotificationAgentLabel(tt.agentName, tt.agentID); got != tt.want {
				t.Fatalf("formatNotificationAgentLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeNotificationSummary(t *testing.T) {
	summary := "2026/04/02 14:37:37   permission denied writing dist/index.js"
	want := "permission denied writing dist/index.js"

	if got := normalizeNotificationSummary(summary); got != want {
		t.Fatalf("normalizeNotificationSummary() = %q, want %q", got, want)
	}
}

func TestBuildNotificationBodyFallback(t *testing.T) {
	if got := buildNotificationBody("", "Session finished."); got != "Session finished." {
		t.Fatalf("buildNotificationBody() = %q, want %q", got, "Session finished.")
	}
}
