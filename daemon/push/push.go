package push

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

const expoPushURL = "https://exp.host/--/api/v2/push/send"

// Client sends push notifications via Expo Push API.
type Client struct {
	httpClient *http.Client
	token      string // Expo push token from mobile app
	serverRef  string // Opaque client-side server identifier for notification routing
}

// New creates a push notification client.
func New() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// SetRegistration sets the Expo push token and client-provided server reference.
func (c *Client) SetRegistration(token, serverRef string) {
	c.token = token
	c.serverRef = serverRef
	log.Printf("push token registered: %s", token[:20]+"...")
}

// HasToken returns true if a push token is registered.
func (c *Client) HasToken() bool {
	return c.token != ""
}

// Message represents a push notification.
type Message struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Data     any    `json:"data,omitempty"`
	Priority string `json:"priority,omitempty"` // "high" or "default"
	Sound    string `json:"sound,omitempty"`
}

// Send sends a push notification to the registered device.
func (c *Client) Send(msg Message) error {
	if c.token == "" {
		return fmt.Errorf("no push token registered")
	}

	payload := map[string]any{
		"to":    c.token,
		"title": msg.Title,
		"body":  msg.Body,
		"sound": "default",
	}
	if msg.Priority != "" {
		payload["priority"] = msg.Priority
	}
	if msg.Data != nil {
		payload["data"] = msg.Data
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal push: %w", err)
	}

	req, err := http.NewRequest("POST", expoPushURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send push: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("push API returned %d", resp.StatusCode)
	}

	log.Printf("push sent: %s - %s", msg.Title, msg.Body)
	return nil
}

// NotifyAgentBlocked sends a high-priority notification for a blocked agent.
func (c *Client) NotifyAgentBlocked(agentID, agentName, summary string) {
	if !c.HasToken() {
		return
	}
	c.Send(Message{
		Title:    fmt.Sprintf("%s needs you", agentName),
		Body:     summary,
		Priority: "high",
		Data: map[string]string{
			"agent_id":  agentID,
			"screen":    "terminal",
			"server_id": c.serverRef,
		},
	})
}

// NotifyAgentFailed sends a high-priority notification for a failed agent.
func (c *Client) NotifyAgentFailed(agentID, agentName, summary string) {
	if !c.HasToken() {
		return
	}
	c.Send(Message{
		Title:    fmt.Sprintf("%s failed", agentName),
		Body:     summary,
		Priority: "high",
		Data: map[string]string{
			"agent_id":  agentID,
			"screen":    "terminal",
			"server_id": c.serverRef,
		},
	})
}

// NotifyAgentDone sends a normal-priority notification for a completed agent.
func (c *Client) NotifyAgentDone(agentID, agentName string) {
	if !c.HasToken() {
		return
	}
	c.Send(Message{
		Title:    fmt.Sprintf("%s done", agentName),
		Body:     "Task completed successfully",
		Priority: "default",
		Data: map[string]string{
			"agent_id":  agentID,
			"screen":    "terminal",
			"server_id": c.serverRef,
		},
	})
}
