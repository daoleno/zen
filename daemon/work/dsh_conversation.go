package work

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/klauspost/compress/zstd"
)

const dshConversationSource = "dsh_session_jsonl"

type dshEvent struct {
	Type  string          `json:"type"`
	Seq   int             `json:"seq"`
	Time  int64           `json:"time"`
	Seq0  int             `json:"seq0"`
	Time0 int64           `json:"time0"`
	Data  json.RawMessage `json:"data"`
}

type dshBlock struct {
	Type         string `json:"type"`
	Text         string `json:"text"`
	AttachmentID string `json:"attachmentId"`
	Name         string `json:"name"`
	MediaType    string `json:"mediaType"`
	Attachment   struct {
		ID        string `json:"attachmentId"`
		Name      string `json:"name"`
		MediaType string `json:"mediaType"`
	} `json:"attachment"`
	ToolCallID string     `json:"toolCallId"`
	Content    []dshBlock `json:"content"`
	IsError    bool       `json:"isError"`
}
type dshMessage struct {
	ID      string     `json:"id"`
	Content []dshBlock `json:"content"`
	Source  struct {
		Kind string `json:"kind"`
	} `json:"source"`
}

func (r *ProviderConversationReader) loadDSHConversationForWorker(worker classifier.Worker, now time.Time) (CodexConversation, error) {
	id := DSHSessionID(worker.Command)
	if id == "" {
		return CodexConversation{Reason: "session_not_ready", Events: []CodexConversationEvent{}}, nil
	}
	paths, err := filepath.Glob(filepath.Join(DSHRoot(), "logs", "*", id, "session.jsonl.zstd"))
	if err != nil {
		return CodexConversation{}, err
	}
	if len(paths) != 1 {
		return CodexConversation{Reason: "transcript_not_found", SessionID: id, Events: []CodexConversationEvent{}}, nil
	}
	return r.loadDSHConversation(paths[0])
}

func (r *ProviderConversationReader) loadDSHConversation(path string) (CodexConversation, error) {
	return r.loadFileConversation(WorkerProviderDSH, path, parseDSHConversation)
}

func parseDSHConversation(path string) (CodexConversation, error) {
	file, err := os.Open(path)
	if err != nil {
		return CodexConversation{}, err
	}
	defer file.Close()
	var reader io.Reader = file
	if strings.HasSuffix(path, ".zstd") {
		decoder, err := zstd.NewReader(file, zstd.WithDecoderMaxMemory(64<<20), zstd.WithDecoderConcurrency(1))
		if err != nil {
			return CodexConversation{}, err
		}
		defer decoder.Close()
		reader = decoder
	}
	limit := &io.LimitedReader{R: reader, N: 64 << 20}
	scanner := bufio.NewScanner(limit)
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if index := bytes.IndexByte(data, '\n'); index >= 0 {
			return index + 1, data[:index], nil
		}
		if atEOF {
			return len(data), nil, nil
		}
		return 0, nil, nil
	})
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	if !scanner.Scan() {
		return CodexConversation{}, fmt.Errorf("DSH session header missing")
	}
	var header struct {
		Type    string `json:"type"`
		Version int    `json:"version"`
		ID      string `json:"id"`
		CWD     string `json:"cwd"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil || header.Type != "session" || header.Version != 0 || header.ID == "" {
		return CodexConversation{}, fmt.Errorf("unsupported DSH session header")
	}
	var events []dshEvent
	for scanner.Scan() {
		var event dshEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return CodexConversation{}, err
		}
		events = append(events, event)
	}
	// A live writer may still be appending the last frame. Never mutate the native log.
	if err := scanner.Err(); err != nil && err != io.ErrUnexpectedEOF {
		return CodexConversation{}, err
	}
	if limit.N <= 0 {
		return CodexConversation{}, fmt.Errorf("DSH history exceeds the 64 MiB read limit")
	}
	return projectDSHConversation(header.ID, header.CWD, path, events), nil
}

func projectDSHConversation(id, cwd, path string, events []dshEvent) CodexConversation {
	conversation := CodexConversation{Available: true, Source: dshConversationSource, SessionID: id, CWD: cwd, Path: path, Events: []CodexConversationEvent{}}
	positions := map[string]int{}
	lifecycle := providerActivityLifecycle{}
	put := func(event CodexConversationEvent) {
		if index, ok := positions[event.ID]; ok {
			event.Seq = conversation.Events[index].Seq
			conversation.Events[index] = event
		} else {
			positions[event.ID] = len(conversation.Events)
			conversation.Events = append(conversation.Events, event)
		}
	}
	for _, event := range events {
		var data struct {
			Turn      int        `json:"turn"`
			Step      int        `json:"step"`
			CallID    string     `json:"callId"`
			Name      string     `json:"name"`
			Arguments string     `json:"arguments"`
			Content   []dshBlock `json:"content"`
			Message   dshMessage `json:"message"`
			Source    struct {
				Kind string `json:"kind"`
			} `json:"source"`
			ID     string `json:"id"`
			Reason struct {
				Kind string `json:"kind"`
			} `json:"reason"`
			Result json.RawMessage `json:"result"`
			Texts  []string        `json:"texts"`
			Chunk  struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"chunk"`
		}
		if json.Unmarshal(event.Data, &data) != nil {
			continue
		}
		seq, clock := event.Seq, event.Time
		if strings.HasSuffix(event.Type, "-chunks") {
			seq = event.Seq0
			clock = event.Time0
		}
		timestamp := time.UnixMilli(clock).UTC().Format(time.RFC3339Nano)
		activityID := fmt.Sprintf("%s:turn:%d", id, data.Turn)
		assistantID := fmt.Sprintf("%s:assistant:%d:%d", id, data.Turn, data.Step)
		base := CodexConversationEvent{ID: fmt.Sprintf("%s:%d", id, seq), Seq: seq + 1, Timestamp: timestamp, Source: dshConversationSource}
		switch event.Type {
		case "turn/start":
			lifecycle.start(activityID, timestamp)
		case "turn/end":
			status := ProviderActivityCompleted
			if data.Reason.Kind == "cancelled" || data.Reason.Kind == "aborted" || data.Reason.Kind == "interrupted" {
				status = ProviderActivityInterrupted
			}
			if data.Reason.Kind == "error" || data.Reason.Kind == "blocked" {
				status = ProviderActivityFailed
			}
			lifecycle.settle(activityID, status, timestamp)
		case "user/message":
			if data.Source.Kind != "user" {
				continue
			}
			base.ID = id + ":user:" + data.ID
			base.Kind = "user_message"
			base.Role = "user"
			var exact string
			base.Body, exact = dshUserContent(data.Content)
			base.AdmissionSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(exact)))
			put(base)
		case "assistant/chunk", "text-chunks", "reasoning-chunks":
			kind := data.Chunk.Type
			text := data.Chunk.Text
			if event.Type == "text-chunks" {
				kind = "text-delta"
				text = strings.Join(data.Texts, "")
			}
			if event.Type == "reasoning-chunks" {
				kind = "reasoning-delta"
				text = strings.Join(data.Texts, "")
			}
			if kind != "text-delta" && kind != "reasoning-delta" {
				continue
			}
			base.ID = assistantID
			base.Kind = "assistant_message"
			base.Role = "assistant"
			if kind == "reasoning-delta" {
				base.ID += ":reasoning"
				base.Kind = "reasoning"
			}
			if index, ok := positions[base.ID]; ok {
				base.Body = conversation.Events[index].Body
			}
			base.Body += text
			base.Partial = true
			put(base)
		case "assistant/message":
			base.ID = assistantID
			base.Kind = "assistant_message"
			base.Role = "assistant"
			base.Body = dshContentText(data.Message.Content)
			put(base)
			if index, ok := positions[assistantID+":reasoning"]; ok {
				conversation.Events[index].Partial = false
			}
		case "tool/call":
			base.ID = id + ":tool:" + data.CallID
			base.Kind = "tool_call"
			base.CallID = data.CallID
			base.ToolName = data.Name
			base.Input = data.Arguments
			base.Status = "running"
			base.Partial = true
			put(base)
		case "tool/result":
			if len(data.Message.Content) == 0 {
				continue
			}
			block := data.Message.Content[0]
			data.CallID = block.ToolCallID
			base.ID = id + ":result:" + data.CallID
			base.Kind = "tool_result"
			base.CallID = data.CallID
			base.Output = dshContentText(block.Content)
			base.Status = "completed"
			if block.IsError {
				base.Status = "failed"
			}
			if index, ok := positions[id+":tool:"+data.CallID]; ok {
				call := conversation.Events[index]
				call.Output = base.Output
				call.Status = base.Status
				call.Partial = false
				put(call)
			} else {
				base.Kind = "tool"
				put(base)
			}
		}
	}
	result := conversationWithActivity(conversation, &lifecycle)
	return result
}

func dshContentText(blocks []dshBlock) string {
	var parts []string
	for _, block := range blocks {
		if block.Type == "text" {
			parts = append(parts, block.Text)
		} else if block.Attachment.ID != "" {
			parts = append(parts, "![Image](dsh-attachment:"+block.Attachment.ID+")")
		}
	}
	return strings.Join(parts, "\n")
}
