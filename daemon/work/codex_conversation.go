package work

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

const (
	maxCodexConversationEvents = 240
	maxCodexConversationBody   = 8000
	maxCodexConversationRead   = 4 << 20
)

type cachedCodexConversation struct {
	size         int64
	modTime      time.Time
	conversation CodexConversation
}

var codexConversationCache = struct {
	sync.Mutex
	byPath map[string]cachedCodexConversation
}{
	byPath: map[string]cachedCodexConversation{},
}

type CodexConversation struct {
	Available bool                     `json:"available"`
	Reason    string                   `json:"reason,omitempty"`
	Source    string                   `json:"source,omitempty"`
	Path      string                   `json:"path,omitempty"`
	SessionID string                   `json:"session_id,omitempty"`
	CWD       string                   `json:"cwd,omitempty"`
	Updated   *time.Time               `json:"updated_at,omitempty"`
	Events    []CodexConversationEvent `json:"events"`
}

type CodexConversationEvent struct {
	ID          string          `json:"id"`
	Seq         int             `json:"seq"`
	Timestamp   string          `json:"timestamp,omitempty"`
	Kind        string          `json:"kind"`
	Role        string          `json:"role,omitempty"`
	Title       string          `json:"title,omitempty"`
	Body        string          `json:"body,omitempty"`
	Command     string          `json:"command,omitempty"`
	ToolName    string          `json:"tool_name,omitempty"`
	Input       string          `json:"input,omitempty"`
	Output      string          `json:"output,omitempty"`
	CallID      string          `json:"call_id,omitempty"`
	ExitCode    *int            `json:"exit_code,omitempty"`
	Status      string          `json:"status,omitempty"`
	Files       []string        `json:"files,omitempty"`
	Explanation string          `json:"explanation,omitempty"`
	Plan        []CodexPlanStep `json:"plan,omitempty"`
	Source      string          `json:"source,omitempty"`
}

type CodexPlanStep struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}

func LoadCodexConversationForAgent(agent classifier.Agent, now time.Time) (CodexConversation, error) {
	if agentToolName(agent.Command, agent.Name) != "codex" {
		return CodexConversation{
			Available: false,
			Reason:    "not_codex",
			Events:    []CodexConversationEvent{},
		}, nil
	}
	if strings.TrimSpace(agent.Cwd) == "" {
		return CodexConversation{
			Available: false,
			Reason:    "missing_cwd",
			Events:    []CodexConversationEvent{},
		}, nil
	}

	candidate, ok, err := findCodexTranscript(agent, now)
	if err != nil {
		return CodexConversation{}, err
	}
	if !ok {
		return CodexConversation{
			Available: false,
			Reason:    "transcript_not_found",
			Events:    []CodexConversationEvent{},
		}, nil
	}

	conversation, err := loadCachedCodexConversation(candidate.Path)
	if err != nil {
		return CodexConversation{}, err
	}
	conversation.Available = true
	conversation.Source = "codex_rollout"
	conversation.Path = candidate.Path
	conversation.SessionID = firstNonEmpty(conversation.SessionID, candidate.Meta.ID, candidate.Row.ID)
	conversation.CWD = firstNonEmpty(conversation.CWD, candidate.Meta.CWD)
	conversation.Updated = &candidate.Updated
	if conversation.Events == nil {
		conversation.Events = []CodexConversationEvent{}
	}
	return conversation, nil
}

func loadCachedCodexConversation(path string) (CodexConversation, error) {
	info, err := os.Stat(path)
	if err != nil {
		return CodexConversation{}, err
	}

	codexConversationCache.Lock()
	if cached, ok := codexConversationCache.byPath[path]; ok &&
		cached.size == info.Size() &&
		cached.modTime.Equal(info.ModTime()) {
		conversation := cached.conversation
		codexConversationCache.Unlock()
		return conversation, nil
	}
	codexConversationCache.Unlock()

	conversation, err := parseCodexConversation(path)
	if err != nil {
		return CodexConversation{}, err
	}

	codexConversationCache.Lock()
	codexConversationCache.byPath[path] = cachedCodexConversation{
		size:         info.Size(),
		modTime:      info.ModTime(),
		conversation: conversation,
	}
	codexConversationCache.Unlock()
	return conversation, nil
}

func parseCodexConversation(path string) (CodexConversation, error) {
	file, err := os.Open(path)
	if err != nil {
		return CodexConversation{}, err
	}
	defer file.Close()

	builder := newCodexConversationBuilder(filepath.Base(path))
	reader := bufio.NewReader(file)
	lineNumber := 0
	for {
		line, err := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			lineNumber++
			builder.consumeLine(lineNumber, line)
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return CodexConversation{}, err
		}
	}
	return builder.conversation(), nil
}

func seekCodexConversationTail(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() <= maxCodexConversationRead {
		_, err = file.Seek(0, io.SeekStart)
		return err
	}
	if _, err := file.Seek(info.Size()-maxCodexConversationRead, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReader(file)
	if _, err := reader.ReadBytes('\n'); err != nil && err != io.EOF {
		return err
	}
	offset, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if _, err := file.Seek(offset-int64(reader.Buffered()), io.SeekStart); err != nil {
		return err
	}
	return nil
}

type codexConversationBuilder struct {
	sourceID               string
	sessionID              string
	cwd                    string
	events                 []CodexConversationEvent
	commandByCall          map[string]string
	commandCallBySession   map[string]string
	eventByCall            map[string]int
	sessionByCall          map[string]string
	recentMessageLineByKey map[string]int
	seenStatusKeys         map[string]struct{}
	patchEventSeen         bool
}

func newCodexConversationBuilder(sourceID string) *codexConversationBuilder {
	return &codexConversationBuilder{
		sourceID:               sourceID,
		commandByCall:          map[string]string{},
		commandCallBySession:   map[string]string{},
		eventByCall:            map[string]int{},
		sessionByCall:          map[string]string{},
		recentMessageLineByKey: map[string]int{},
		seenStatusKeys:         map[string]struct{}{},
	}
}

func (b *codexConversationBuilder) consumeLine(lineNumber int, line []byte) {
	var envelope struct {
		Type      string          `json:"type"`
		Timestamp string          `json:"timestamp"`
		Payload   json.RawMessage `json:"payload"`
	}
	if json.Unmarshal(line, &envelope) != nil {
		return
	}

	timestamp := normalizeCodexTimestamp(envelope.Timestamp)
	switch envelope.Type {
	case "session_meta":
		b.consumeSessionMeta(envelope.Payload)
	case "event_msg":
		b.consumeEvent(lineNumber, timestamp, envelope.Payload)
	case "response_item":
		b.consumeResponseItem(lineNumber, timestamp, envelope.Payload)
	}
}

func (b *codexConversationBuilder) consumeSessionMeta(raw json.RawMessage) {
	var payload struct {
		ID  string `json:"id"`
		CWD string `json:"cwd"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return
	}
	if id := strings.TrimSpace(payload.ID); id != "" {
		b.sessionID = id
	}
	if cwd := strings.TrimSpace(payload.CWD); cwd != "" {
		b.cwd = cwd
	}
}

func (b *codexConversationBuilder) consumeEvent(lineNumber int, timestamp string, raw json.RawMessage) {
	var payload struct {
		Type             string          `json:"type"`
		Message          string          `json:"message"`
		Phase            string          `json:"phase"`
		CallID           string          `json:"call_id"`
		ExitCode         *int            `json:"exit_code"`
		Status           string          `json:"status"`
		Command          []string        `json:"command"`
		AggregatedOutput string          `json:"aggregated_output"`
		Goal             json.RawMessage `json:"goal"`
		Explanation      string          `json:"explanation"`
		Plan             []CodexPlanStep `json:"plan"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return
	}

	switch payload.Type {
	case "task_started":
		b.addStatus(lineNumber, timestamp, "Task started", "")
	case "user_message":
		b.addMessage(lineNumber, timestamp, "user", payload.Message)
	case "agent_message":
		title := ""
		if strings.TrimSpace(payload.Phase) != "" {
			title = strings.TrimSpace(payload.Phase)
		}
		b.addMessageWithTitle(lineNumber, timestamp, "assistant", payload.Message, title)
	case "exec_command_end":
		command := shellCommandLabel(payload.Command)
		if command == "" {
			command = b.commandByCall[payload.CallID]
		}
		if command == "" {
			command = "command"
		}
		exitCode := 0
		if payload.ExitCode != nil {
			exitCode = *payload.ExitCode
		}
		b.upsertCommandEnd(lineNumber, timestamp, payload.CallID, command, exitCode, payload.AggregatedOutput)
	case "patch_apply_end":
		status := strings.TrimSpace(payload.Status)
		if (status == "" || status == "success") && b.patchEventSeen {
			return
		}
		if status == "" || status == "success" {
			b.addStatus(lineNumber, timestamp, "Patch applied", "")
		} else {
			b.addStatus(lineNumber, timestamp, "Patch "+status, "")
		}
	case "thread_goal_updated":
		body := codexGoalText(payload.Goal)
		b.addStatus(lineNumber, timestamp, "Goal updated", body)
	case "plan_update":
		b.addPlanUpdate(lineNumber, timestamp, "", payload.Explanation, payload.Plan)
	}
}

func (b *codexConversationBuilder) consumeResponseItem(lineNumber int, timestamp string, raw json.RawMessage) {
	var payload struct {
		Type      string          `json:"type"`
		Role      string          `json:"role"`
		Content   json.RawMessage `json:"content"`
		Name      string          `json:"name"`
		Arguments string          `json:"arguments"`
		CallID    string          `json:"call_id"`
		Status    string          `json:"status"`
		Input     string          `json:"input"`
		Output    json.RawMessage `json:"output"`
		Summary   json.RawMessage `json:"summary"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return
	}

	switch payload.Type {
	case "message":
		text := codexConversationContentText(payload.Content)
		switch payload.Role {
		case "user", "assistant":
			b.addMessage(lineNumber, timestamp, payload.Role, text)
		}
	case "function_call":
		if isCodexPlanTool(payload.Name) {
			explanation, plan := codexPlanToolArguments(payload.Arguments)
			b.addPlanUpdate(lineNumber, timestamp, payload.CallID, explanation, plan)
		} else if isCodexCommandTool(payload.Name) {
			command := codexExecCommand(payload.Arguments)
			if command != "" {
				b.commandByCall[payload.CallID] = command
				b.addCommandStart(lineNumber, timestamp, payload.CallID, command)
			}
		} else {
			callID := strings.TrimSpace(payload.CallID)
			command := ""
			if sessionID := codexToolSessionID(payload.Arguments); sessionID != "" && callID != "" {
				b.sessionByCall[callID] = sessionID
				if commandCallID := b.commandCallBySession[sessionID]; commandCallID != "" {
					command = b.commandByCall[commandCallID]
				}
			}
			b.addToolStart(lineNumber, timestamp, payload.CallID, payload.Name, payload.Arguments, "running", command)
		}
	case "function_call_output":
		output := codexFunctionOutputText(payload.Output, payload.Content)
		if sessionID := b.sessionByCall[strings.TrimSpace(payload.CallID)]; sessionID != "" {
			b.updateSessionCommandOutput(lineNumber, timestamp, sessionID, output)
		}
		b.updateCallOutput(lineNumber, timestamp, payload.CallID, output)
	case "custom_tool_call":
		if payload.Name == "apply_patch" {
			callID := strings.TrimSpace(payload.CallID)
			files := patchSurfaces(payload.Input)
			title := "Patch"
			if len(files) > 0 {
				title = fmt.Sprintf("Patch %d file", len(files))
				if len(files) > 1 {
					title += "s"
				}
			}
			b.addEvent(CodexConversationEvent{
				ID:        b.eventID(lineNumber),
				Timestamp: timestamp,
				Kind:      "patch",
				Title:     title,
				Body:      truncateConversationBody(payload.Input),
				Files:     files,
				CallID:    callID,
				Source:    "codex_rollout",
			})
			b.patchEventSeen = true
		} else {
			b.addToolStart(lineNumber, timestamp, payload.CallID, payload.Name, payload.Input, "done", "")
		}
	case "custom_tool_call_output":
		output := codexFunctionOutputText(payload.Output, payload.Content)
		if sessionID := b.sessionByCall[strings.TrimSpace(payload.CallID)]; sessionID != "" {
			b.updateSessionCommandOutput(lineNumber, timestamp, sessionID, output)
		}
		b.updateCallOutput(lineNumber, timestamp, payload.CallID, output)
	case "reasoning":
		if summary := codexConversationContentText(payload.Summary); summary != "" {
			b.addEvent(CodexConversationEvent{
				ID:        b.eventID(lineNumber),
				Timestamp: timestamp,
				Kind:      "commentary",
				Title:     "Reasoning",
				Body:      summary,
				Source:    "codex_rollout",
			})
		}
	}
}

func (b *codexConversationBuilder) addMessage(lineNumber int, timestamp, role, text string) {
	b.addMessageWithTitle(lineNumber, timestamp, role, text, "")
}

func (b *codexConversationBuilder) addMessageWithTitle(lineNumber int, timestamp, role, text, title string) {
	text = cleanConversationText(text)
	if text == "" || isTranscriptBoilerplate(text) {
		return
	}
	key := role + ":" + text
	if previousLine, exists := b.recentMessageLineByKey[key]; exists && lineNumber-previousLine <= 12 {
		b.recentMessageLineByKey[key] = lineNumber
		return
	}
	b.recentMessageLineByKey[key] = lineNumber

	kind := "assistant_message"
	if role == "user" {
		kind = "user_message"
	}
	b.addEvent(CodexConversationEvent{
		ID:        b.eventID(lineNumber),
		Timestamp: timestamp,
		Kind:      kind,
		Role:      role,
		Title:     cleanConversationText(title),
		Body:      text,
		Source:    "codex_rollout",
	})
}

func (b *codexConversationBuilder) addCommandStart(lineNumber int, timestamp, callID, command string) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		b.addEvent(CodexConversationEvent{
			ID:        b.eventID(lineNumber),
			Timestamp: timestamp,
			Kind:      "command",
			Title:     "Command",
			Command:   cleanConversationText(command),
			Status:    "running",
			Source:    "codex_rollout",
		})
		return
	}
	if _, exists := b.eventByCall[callID]; exists {
		return
	}
	event := CodexConversationEvent{
		ID:        b.eventID(lineNumber),
		Timestamp: timestamp,
		Kind:      "command",
		Title:     "Command",
		Command:   cleanConversationText(command),
		CallID:    callID,
		Status:    "running",
		Source:    "codex_rollout",
	}
	if b.addEvent(event) {
		b.eventByCall[callID] = len(b.events) - 1
	}
}

func (b *codexConversationBuilder) upsertCommandEnd(lineNumber int, timestamp, callID, command string, exitCode int, output string) {
	status := "done"
	title := "Command finished"
	if exitCode != 0 {
		status = "failed"
		title = "Command failed"
	}
	body := codexCommandOutputBody(output)
	if body != "" {
		body = truncateConversationBody(body)
	}
	if index, exists := b.eventByCall[strings.TrimSpace(callID)]; exists && index >= 0 && index < len(b.events) {
		if b.events[index].Kind != "command" {
			return
		}
		b.events[index].Title = title
		b.events[index].Command = cleanConversationText(command)
		b.events[index].ExitCode = &exitCode
		b.events[index].Status = status
		b.events[index].Body = body
		if timestamp != "" {
			b.events[index].Timestamp = timestamp
		}
		return
	}
	b.addEvent(CodexConversationEvent{
		ID:        b.eventID(lineNumber),
		Timestamp: timestamp,
		Kind:      "command",
		Title:     title,
		Command:   cleanConversationText(command),
		CallID:    strings.TrimSpace(callID),
		ExitCode:  &exitCode,
		Status:    status,
		Body:      body,
		Source:    "codex_rollout",
	})
}

func (b *codexConversationBuilder) updateCommandOutput(lineNumber int, timestamp, callID, output string) {
	callID = strings.TrimSpace(callID)
	if index, exists := b.eventByCall[callID]; exists && index >= 0 && index < len(b.events) {
		if b.events[index].Kind != "command" {
			return
		}
		body := cleanConversationText(output)
		if sessionID := codexProcessSessionID(body); sessionID != "" {
			b.commandCallBySession[sessionID] = callID
			b.events[index].Status = "running"
			b.events[index].Title = "Command"
			body = codexCommandOutputBody(body)
		} else if exitCode := codexFunctionOutputExitCode(body); exitCode != nil {
			b.events[index].ExitCode = exitCode
			b.events[index].Status = "done"
			b.events[index].Title = "Command finished"
			if *exitCode != 0 {
				b.events[index].Status = "failed"
				b.events[index].Title = "Command failed"
			}
			body = codexCommandOutputBody(body)
			if timestamp != "" {
				b.events[index].Timestamp = timestamp
			}
		} else if body != "" {
			status := codexToolOutputStatus(body)
			b.events[index].Status = status
			if status == "failed" {
				b.events[index].Title = "Command failed"
			} else {
				b.events[index].Title = "Command finished"
			}
			if timestamp != "" {
				b.events[index].Timestamp = timestamp
			}
		}
		b.events[index].Body = truncateConversationBody(body)
	}
}

func (b *codexConversationBuilder) updateSessionCommandOutput(lineNumber int, timestamp, sessionID, output string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	callID := b.commandCallBySession[sessionID]
	if callID == "" {
		return
	}
	b.updateCommandOutput(lineNumber, timestamp, callID, output)
}

func (b *codexConversationBuilder) addToolStart(lineNumber int, timestamp, callID, name, input, status, command string) {
	callID = strings.TrimSpace(callID)
	name = cleanToolName(name)
	if name == "" {
		name = "tool"
	}
	input = codexToolPayloadText(input)
	status = cleanConversationText(status)
	if status == "" {
		status = "running"
	}
	event := CodexConversationEvent{
		ID:        b.eventID(lineNumber),
		Timestamp: timestamp,
		Kind:      "tool",
		Title:     "Tool",
		ToolName:  name,
		Input:     input,
		Command:   cleanConversationText(command),
		CallID:    callID,
		Status:    status,
		Source:    "codex_rollout",
	}
	if callID != "" {
		if index, exists := b.eventByCall[callID]; exists && index >= 0 && index < len(b.events) {
			b.events[index].Title = event.Title
			b.events[index].ToolName = event.ToolName
			b.events[index].Input = event.Input
			if event.Command != "" {
				b.events[index].Command = event.Command
			}
			b.events[index].Status = event.Status
			if timestamp != "" {
				b.events[index].Timestamp = timestamp
			}
			return
		}
	}
	if b.addEvent(event) && callID != "" {
		b.eventByCall[callID] = len(b.events) - 1
	}
}

func (b *codexConversationBuilder) updateCallOutput(lineNumber int, timestamp, callID, output string) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return
	}
	if index, exists := b.eventByCall[callID]; exists && index >= 0 && index < len(b.events) {
		switch b.events[index].Kind {
		case "command":
			b.updateCommandOutput(lineNumber, timestamp, callID, output)
		case "tool":
			b.events[index].Output = codexToolPayloadText(output)
			b.events[index].Status = codexToolOutputStatus(output)
			if timestamp != "" {
				b.events[index].Timestamp = timestamp
			}
		case "patch":
			// Codex renders apply_patch as a file-change cell. The paired
			// custom_tool_call_output is only protocol acknowledgement.
			if timestamp != "" {
				b.events[index].Timestamp = timestamp
			}
		}
		return
	}
	output = codexToolPayloadText(output)
	if output == "" {
		return
	}
	b.addEvent(CodexConversationEvent{
		ID:        b.eventID(lineNumber),
		Timestamp: timestamp,
		Kind:      "tool",
		Title:     "Tool output",
		ToolName:  "tool",
		Output:    output,
		CallID:    callID,
		Status:    codexToolOutputStatus(output),
		Source:    "codex_rollout",
	})
}

func (b *codexConversationBuilder) addPlanUpdate(lineNumber int, timestamp, callID, explanation string, plan []CodexPlanStep) {
	explanation = cleanConversationText(explanation)
	steps := make([]CodexPlanStep, 0, len(plan))
	for _, item := range plan {
		step := cleanConversationText(item.Step)
		if step == "" {
			continue
		}
		steps = append(steps, CodexPlanStep{
			Step:   step,
			Status: normalizePlanStepStatus(item.Status),
		})
	}
	if explanation == "" && len(steps) == 0 {
		return
	}

	b.addEvent(CodexConversationEvent{
		ID:          b.eventID(lineNumber),
		Timestamp:   timestamp,
		Kind:        "plan",
		Title:       "Updated Plan",
		Body:        explanation,
		Explanation: explanation,
		Plan:        steps,
		CallID:      strings.TrimSpace(callID),
		Status:      "done",
		Source:      "codex_rollout",
	})
}

func (b *codexConversationBuilder) addStatus(lineNumber int, timestamp, title, body string) {
	title = cleanConversationText(title)
	body = cleanConversationText(body)
	if isLowSignalCodexStatus(title, body) {
		return
	}
	key := title + "\x00" + body
	if _, exists := b.seenStatusKeys[key]; exists {
		return
	}
	b.seenStatusKeys[key] = struct{}{}
	b.addEvent(CodexConversationEvent{
		ID:        b.eventID(lineNumber),
		Timestamp: timestamp,
		Kind:      "status",
		Title:     title,
		Body:      body,
		Source:    "codex_rollout",
	})
}

func (b *codexConversationBuilder) addEvent(event CodexConversationEvent) bool {
	event.Body = truncateConversationBody(event.Body)
	event.Command = truncateRunes(cleanConversationText(event.Command), 800)
	event.ToolName = truncateRunes(cleanToolName(event.ToolName), 120)
	event.Input = truncateConversationBody(event.Input)
	event.Output = truncateConversationBody(event.Output)
	event.Explanation = truncateConversationBody(event.Explanation)
	for index := range event.Plan {
		event.Plan[index].Step = truncateRunes(cleanConversationText(event.Plan[index].Step), 240)
		event.Plan[index].Status = normalizePlanStepStatus(event.Plan[index].Status)
	}
	if event.Kind == "" || (event.Body == "" && event.Title == "" && event.Command == "" && event.ToolName == "" && event.Input == "" && event.Output == "" && len(event.Files) == 0 && event.Explanation == "" && len(event.Plan) == 0) {
		return false
	}
	if event.ID == "" {
		event.ID = b.eventID(len(b.events) + 1)
	}
	b.events = append(b.events, event)
	if len(b.events) > maxCodexConversationEvents {
		copy(b.events, b.events[len(b.events)-maxCodexConversationEvents:])
		b.events = b.events[:maxCodexConversationEvents]
	}
	b.reindexEvents()
	return true
}

func (b *codexConversationBuilder) reindexEvents() {
	b.eventByCall = map[string]int{}
	for index := range b.events {
		b.events[index].Seq = index + 1
		if callID := strings.TrimSpace(b.events[index].CallID); callID != "" {
			b.eventByCall[callID] = index
		}
	}
}

func (b *codexConversationBuilder) eventID(lineNumber int) string {
	if b.sessionID != "" {
		return fmt.Sprintf("%s:%d", b.sessionID, lineNumber)
	}
	return fmt.Sprintf("%s:%d", b.sourceID, lineNumber)
}

func (b *codexConversationBuilder) conversation() CodexConversation {
	if b.events == nil {
		b.events = []CodexConversationEvent{}
	}
	b.reindexEvents()
	return CodexConversation{
		Available: true,
		Source:    "codex_rollout",
		SessionID: b.sessionID,
		CWD:       b.cwd,
		Events:    b.events,
	}
}

func codexConversationContentText(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	if raw[0] == '"' {
		return cleanConversationText(jsonString(raw))
	}
	var items []map[string]json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return ""
	}
	var parts []string
	for _, item := range items {
		itemType := jsonString(item["type"])
		switch itemType {
		case "input_text", "output_text", "text", "summary_text":
			if text := cleanConversationText(jsonString(item["text"])); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func codexFunctionOutputText(rawOutput, rawContent json.RawMessage) string {
	if text := codexConversationContentText(rawOutput); text != "" {
		return text
	}
	if text := codexConversationContentText(rawContent); text != "" {
		return text
	}
	if text := codexJSONPayloadText(rawOutput); text != "" {
		return text
	}
	if text := codexJSONPayloadText(rawContent); text != "" {
		return text
	}
	return ""
}

func codexJSONPayloadText(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	if raw[0] == '"' {
		return cleanConversationText(jsonString(raw))
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, bytes.TrimSpace(raw), "", "  ") == nil {
		return cleanConversationText(pretty.String())
	}
	return cleanConversationText(string(raw))
}

func codexToolPayloadText(value string) string {
	value = cleanConversationText(value)
	if value == "" {
		return ""
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, []byte(value), "", "  ") == nil {
		return cleanConversationText(pretty.String())
	}
	return value
}

func codexFunctionOutputExitCode(output string) *int {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var value string
		switch {
		case strings.HasPrefix(trimmed, "Exit code:"):
			value = strings.TrimSpace(strings.TrimPrefix(trimmed, "Exit code:"))
		case strings.HasPrefix(trimmed, "Process exited with code "):
			value = strings.TrimSpace(strings.TrimPrefix(trimmed, "Process exited with code "))
		default:
			continue
		}
		if fields := strings.Fields(value); len(fields) > 0 {
			value = fields[0]
		}
		exitCode, err := strconv.Atoi(value)
		if err != nil {
			return nil
		}
		return &exitCode
	}
	return nil
}

func codexProcessSessionID(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		const prefix = "Process running with session ID "
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		if fields := strings.Fields(value); len(fields) > 0 {
			return fields[0]
		}
	}
	return ""
}

func codexToolSessionID(arguments string) string {
	var payload map[string]json.RawMessage
	if json.Unmarshal([]byte(arguments), &payload) != nil {
		return ""
	}
	raw := bytes.TrimSpace(payload["session_id"])
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	if raw[0] == '"' {
		return strings.TrimSpace(jsonString(raw))
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return ""
	}
	switch typed := value.(type) {
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func codexCommandOutputBody(output string) string {
	lines := strings.Split(cleanConversationText(output), "\n")
	bodyLines := lines
	for index, line := range lines {
		if strings.TrimSpace(line) == "Output:" {
			bodyLines = lines[index+1:]
			break
		}
	}
	var kept []string
	for _, line := range bodyLines {
		if isCodexCommandMetadataLine(line) {
			continue
		}
		kept = append(kept, line)
	}
	return cleanConversationText(strings.Join(kept, "\n"))
}

func isCodexCommandMetadataLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "Chunk ID:") ||
		strings.HasPrefix(trimmed, "Wall time:") ||
		strings.HasPrefix(trimmed, "Process exited with code ") ||
		strings.HasPrefix(trimmed, "Process running with session ID ") ||
		strings.HasPrefix(trimmed, "Original token count:") ||
		strings.HasPrefix(trimmed, "Total output lines:")
}

func codexToolOutputStatus(output string) string {
	normalized := strings.ToLower(cleanConversationText(output))
	switch {
	case normalized == "":
		return "done"
	case strings.Contains(normalized, "failed to parse function arguments"):
		return "failed"
	case strings.HasPrefix(normalized, "error:"):
		return "failed"
	case strings.Contains(normalized, "\nerror:"):
		return "failed"
	case strings.Contains(normalized, "toolcallerror"):
		return "failed"
	default:
		return "done"
	}
}

func isCodexCommandTool(name string) bool {
	normalized := strings.TrimSpace(name)
	normalized = strings.TrimPrefix(normalized, "functions.")
	switch normalized {
	case "exec_command", "shell_command":
		return true
	default:
		return false
	}
}

func isCodexPlanTool(name string) bool {
	normalized := strings.TrimSpace(name)
	normalized = strings.TrimPrefix(normalized, "functions.")
	return normalized == "update_plan"
}

func codexPlanToolArguments(arguments string) (string, []CodexPlanStep) {
	var payload struct {
		Explanation string          `json:"explanation"`
		Plan        []CodexPlanStep `json:"plan"`
	}
	if json.Unmarshal([]byte(arguments), &payload) != nil {
		return "", nil
	}
	return payload.Explanation, payload.Plan
}

func normalizePlanStepStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "completed":
		return "completed"
	case "in_progress", "in-progress", "inprogress":
		return "in_progress"
	default:
		return "pending"
	}
}

func cleanToolName(name string) string {
	return truncateRunes(cleanConversationText(name), 120)
}

func codexGoalText(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	if text := codexConversationContentText(raw); text != "" {
		return text
	}
	var payload struct {
		Objective string `json:"objective"`
		Status    string `json:"status"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	return strings.TrimSpace(strings.Join([]string{payload.Status, payload.Objective}, " "))
}

func normalizeCodexTimestamp(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Format(time.RFC3339Nano)
		}
	}
	return value
}

func cleanConversationText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	lines := strings.Split(value, "\n")
	out := make([]string, 0, len(lines))
	blankRun := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			blankRun++
			if blankRun <= 1 {
				out = append(out, "")
			}
			continue
		}
		blankRun = 0
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func truncateConversationBody(value string) string {
	value = cleanConversationText(value)
	if value == "" {
		return ""
	}
	return truncateRunes(value, maxCodexConversationBody)
}

func isLowSignalCodexStatus(title, body string) bool {
	switch strings.TrimSpace(title) {
	case "Goal updated":
		return true
	}
	return false
}
