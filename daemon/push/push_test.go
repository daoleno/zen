package push

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestFormatNotificationAgentLabel(t *testing.T) {
	tests := []struct {
		name      string
		agentName string
		agentID   string
		want      string
	}{
		{name: "clean shell path and session suffix", agentName: "./bin/zen (main:7)", agentID: "main:7", want: "zen"},
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

func TestShortPushRegistrationNeverPanicsOrLogsCredential(t *testing.T) {
	var logs bytes.Buffer
	previousWriter := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousWriter)

	client := New()
	client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return pushResponse(http.StatusOK, `{"data":{"status":"ok","id":"ticket-1"}}`), nil
	})
	client.SetRegistration("x", "server-1")
	if err := client.NotifyAgentDone("agent-1", "Agent", "finished"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "x") {
		t.Fatalf("push credential leaked to logs: %q", logs.String())
	}
}

func TestNotifyWithoutRegistrationDoesNotAttemptHTTP(t *testing.T) {
	client := New()
	var requests atomic.Int64
	client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return pushResponse(http.StatusOK, `{"data":{"status":"ok"}}`), nil
	})

	err := client.NotifyAgentBlocked("agent-1", "Agent", "waiting")
	if !errors.Is(err, ErrNoRegistration) {
		t.Fatalf("error = %v, want ErrNoRegistration", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("HTTP attempts = %d, want 0", requests.Load())
	}
}

func TestNotifyFailuresAreSingleAttempts(t *testing.T) {
	tests := []struct {
		name      string
		transport roundTripFunc
	}{
		{
			name: "transport",
			transport: func(*http.Request) (*http.Response, error) {
				return nil, errors.New("network unavailable")
			},
		},
		{
			name: "HTTP",
			transport: func(*http.Request) (*http.Response, error) {
				return pushResponse(http.StatusServiceUnavailable, `{}`), nil
			},
		},
		{
			name: "ticket",
			transport: func(*http.Request) (*http.Response, error) {
				return pushResponse(http.StatusOK, `{"data":{"status":"error","message":"rejected"}}`), nil
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := New()
			client.SetRegistration("credential-token", "server-1")
			var attempts atomic.Int64
			client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				attempts.Add(1)
				return test.transport(request)
			})
			if err := client.NotifyAgentFailed("agent-1", "Agent", "failed"); err == nil {
				t.Fatal("expected notification failure")
			} else if strings.Contains(err.Error(), "credential-token") {
				t.Fatalf("credential leaked in error: %v", err)
			}
			if attempts.Load() != 1 {
				t.Fatalf("attempts = %d, want 1", attempts.Load())
			}
		})
	}
}

func TestScheduledResultPushUsesOnlyRoutingAndGenericCopy(t *testing.T) {
	tests := []struct {
		status, wantTitle, wantBody, wantPriority string
	}{
		{
			status: "completed", wantTitle: "Frozen report is ready",
			wantBody: "Open Brain for the scheduled result.", wantPriority: "default",
		},
		{
			status: "failed", wantTitle: "Frozen report failed",
			wantBody: "Open Brain for the failure outcome.", wantPriority: "high",
		},
	}
	for _, test := range tests {
		t.Run(test.status, func(t *testing.T) {
			client := New()
			client.SetRegistration("token-1", "server-1")
			var payload map[string]any
			client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				return pushResponse(http.StatusOK, `{"data":{"status":"ok","id":"ticket-1"}}`), nil
			})

			if err := client.NotifyScheduledResult(
				"Frozen report", test.status, "thread-frozen", "calendar_result:item:run",
			); err != nil {
				t.Fatal(err)
			}
			if payload["to"] != "token-1" || payload["title"] != test.wantTitle ||
				payload["body"] != test.wantBody || payload["priority"] != test.wantPriority {
				t.Fatalf("payload = %#v", payload)
			}
			if len(payload) != 6 {
				t.Fatalf("unexpected scheduled payload fields: %#v", payload)
			}
			data, ok := payload["data"].(map[string]any)
			if !ok {
				t.Fatalf("data = %#v", payload["data"])
			}
			wantData := map[string]string{
				"screen": "brain", "server_id": "server-1",
				"brain_thread_id":  "thread-frozen",
				"brain_message_id": "calendar_result:item:run",
			}
			if len(data) != len(wantData) {
				t.Fatalf("unexpected scheduled routing fields: %#v", data)
			}
			for key, want := range wantData {
				if data[key] != want {
					t.Fatalf("data[%q] = %#v, want %q", key, data[key], want)
				}
			}
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "private deliverable") || strings.Contains(string(encoded), "private failure") {
				t.Fatalf("terminal contents leaked: %s", encoded)
			}
		})
	}
}

func TestConcurrentRegistrationAndNotificationUseCoherentSnapshot(t *testing.T) {
	client := New()
	pairs := map[string]string{
		"token-a": "server-a",
		"token-b": "server-b",
	}
	client.SetRegistration("token-a", "server-a")
	var attempts atomic.Int64
	errCh := make(chan error, 512)
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var payload struct {
			To   string            `json:"to"`
			Data map[string]string `json:"data"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			errCh <- err
		} else if pairs[payload.To] != payload.Data["server_id"] {
			errCh <- errors.New("token and server_ref came from different registrations")
		}
		attempts.Add(1)
		return pushResponse(http.StatusOK, `{"data":{"status":"ok","id":"ticket"}}`), nil
	})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for index := range 250 {
			if index%2 == 0 {
				client.SetRegistration("token-a", "server-a")
			} else {
				client.SetRegistration("token-b", "server-b")
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 250 {
			if err := client.NotifyAgentDone("agent-1", "Agent", "done"); err != nil {
				errCh <- err
			}
		}
	}()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	if attempts.Load() != 250 {
		t.Fatalf("attempts = %d, want 250", attempts.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func pushResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
