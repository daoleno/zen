package work

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type DSHInteraction struct {
	RPCID   string          `json:"rpcId"`
	Payload json.RawMessage `json:"payload"`
}
type dshInteractionOwner struct {
	mu        sync.Mutex
	sessionID string
	epoch     string
	connected bool
	pending   map[string]DSHInteraction
}

func newDSHInteractionOwner(id string) *dshInteractionOwner {
	return &dshInteractionOwner{sessionID: id, pending: map[string]DSHInteraction{}}
}
func (o *dshInteractionOwner) reset(connected bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.epoch = uuid.NewString()
	o.connected = connected
	o.pending = map[string]DSHInteraction{}
}
func (o *dshInteractionOwner) consume(line []byte) {
	var frame struct {
		Type    string          `json:"type"`
		RPCID   string          `json:"rpcId"`
		Payload json.RawMessage `json:"payload"`
	}
	if json.Unmarshal(line, &frame) != nil || frame.Type != "server-request" || frame.RPCID == "" {
		return
	}
	var payload struct {
		Type          string `json:"type"`
		SessionID     string `json:"sessionId"`
		ApprovalID    string `json:"approvalId"`
		QuestionRPCID string `json:"questionRpcId"`
	}
	if json.Unmarshal(frame.Payload, &payload) != nil || payload.SessionID != o.sessionID {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	switch payload.Type {
	case "approval/requested", "question/requested":
		if len(o.pending) < 32 {
			o.pending[frame.RPCID] = DSHInteraction{RPCID: frame.RPCID, Payload: append(json.RawMessage(nil), frame.Payload...)}
		}
	case "question/resolved":
		delete(o.pending, payload.QuestionRPCID)
	case "approval/resolved":
		for id, request := range o.pending {
			var existing struct {
				ApprovalID string `json:"approvalId"`
			}
			if json.Unmarshal(request.Payload, &existing) == nil && existing.ApprovalID == payload.ApprovalID {
				delete(o.pending, id)
			}
		}
	}
}
func (o *dshInteractionOwner) snapshot() any {
	o.mu.Lock()
	defer o.mu.Unlock()
	items := make([]DSHInteraction, 0, len(o.pending))
	for _, item := range o.pending {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].RPCID < items[j].RPCID })
	return struct {
		Epoch     string           `json:"epoch"`
		Connected bool             `json:"connected"`
		Items     []DSHInteraction `json:"items"`
	}{o.epoch, o.connected, items}
}
func (o *dshInteractionOwner) run(ctx context.Context, address string) {
	for ctx.Err() == nil {
		o.reset(false)
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, address+"/api/events.mux", nil)
		response, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(request)
		if err == nil {
			if response.StatusCode == http.StatusOK {
				o.reset(true)
				scanner := bufio.NewScanner(response.Body)
				scanner.Buffer(make([]byte, 4096), 2<<20)
				for scanner.Scan() {
					line := scanner.Bytes()
					if bytes.HasPrefix(line, []byte("data: ")) {
						o.consume(line[6:])
					}
				}
			}
			response.Body.Close()
		}
		o.reset(false)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}
func (o *dshInteractionOwner) answer(ctx context.Context, address string, payload map[string]any) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	epoch, _ := payload["epoch"].(string)
	rpcID, _ := payload["rpcId"].(string)
	pending, ok := o.pending[rpcID]
	if !o.connected || epoch != o.epoch || !ok {
		return fmt.Errorf("This request is no longer pending. Refresh the Session.")
	}
	var request struct {
		Type       string `json:"type"`
		ApprovalID string `json:"approvalId"`
	}
	if json.Unmarshal(pending.Payload, &request) != nil {
		return fmt.Errorf("invalid pending request")
	}
	value := map[string]any{"sessionId": o.sessionID}
	result := map[string]any{"ok": true, "value": value}
	switch request.Type {
	case "approval/requested":
		outcome, _ := payload["outcome"].(string)
		if request.ApprovalID == "" || (outcome != "allowed-once" && outcome != "rejected") {
			return fmt.Errorf("choose Allow once or Reject")
		}
		value["approvalId"] = request.ApprovalID
		value["outcome"] = outcome
	case "question/requested":
		if payload["cancel"] == true {
			result = map[string]any{"ok": false, "error": map[string]any{"code": "cancelled", "message": "Cancelled by user", "details": map[string]any{}}}
		} else {
			if _, ok := payload["answer"].(map[string]any); !ok {
				return fmt.Errorf("question answer is required")
			}
			value["answer"] = payload["answer"]
		}
	default:
		return fmt.Errorf("unsupported interaction")
	}
	body, _ := json.Marshal(map[string]any{"type": "client-response", "rpcId": rpcID, "result": result})
	httpRequest, _ := http.NewRequestWithContext(ctx, http.MethodPost, address+"/api/respond", bytes.NewReader(body))
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(httpRequest)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var receipt struct {
		Accepted bool   `json:"accepted"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&receipt); err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK || !receipt.Accepted {
		if receipt.Reason == "not-pending" {
			delete(o.pending, rpcID)
		}
		return fmt.Errorf("Native DSH rejected the answer: %s", strings.TrimSpace(receipt.Reason))
	}
	delete(o.pending, rpcID)
	return nil
}
