package host

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBrokerErrorFrameIsAdditive(t *testing.T) {
	// A challenge must never be mistaken for an error frame.
	challenge, err := json.Marshal(Challenge{Nonce: strings.Repeat("a", 64), Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	if code := brokerErrorCode(challenge); code != "" {
		t.Fatalf("challenge decoded as error %q", code)
	}
	errorFrame, err := json.Marshal(brokerErrorFrame{Error: "wayland_display_ambiguous"})
	if err != nil {
		t.Fatal(err)
	}
	if code := brokerErrorCode(errorFrame); code != "wayland_display_ambiguous" {
		t.Fatalf("code=%q", code)
	}
	if code := brokerErrorCode([]byte("not json")); code != "" {
		t.Fatalf("garbage decoded as error %q", code)
	}
	if code := brokerErrorCode(nil); code != "" {
		t.Fatalf("nil decoded as error %q", code)
	}
}

func TestBrokerUnavailableReasonsAreActionableEnglish(t *testing.T) {
	cases := map[string]string{
		"wayland_runtime_unavailable": "runtime directory",
		"wayland_bus_unavailable":     "session bus",
		"wayland_display_unavailable": "compositor",
		"wayland_display_ambiguous":   "will not guess",
		"wayland_greeter_unsupported": "SDDM X11",
		"wayland_session_locked":      "Unlock the computer",
		"unknown_code":                "unavailable",
	}
	for code, want := range cases {
		reason := brokerUnavailableReason(code)
		if !strings.Contains(reason, want) {
			t.Fatalf("code %s reason %q missing %q", code, reason, want)
		}
		if strings.ContainsRune(reason, '\n') || len(reason) > 240 {
			t.Fatalf("code %s reason is not a bounded single line", code)
		}
	}
}
