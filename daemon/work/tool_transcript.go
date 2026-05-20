package work

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

const (
	maxTranscriptEvents = 96
	maxTranscriptChars  = 12000
	maxTranscriptLine   = 240
	maxTranscriptAge    = 72 * time.Hour
)

type ToolTranscript struct {
	Source    string
	Path      string
	SessionID string
	Updated   time.Time
	Excerpt   string
}

func loadToolTranscript(agent classifier.Agent, now time.Time) ToolTranscript {
	tool := agentToolName(agent.Command, agent.Name)
	switch tool {
	case "codex":
		transcript, err := loadCodexTranscript(agent.Cwd, now)
		if err != nil {
			log.Printf("work transcript lookup failed for codex (%s): %v", agent.Cwd, err)
		}
		return transcript
	case "claude":
		transcript, err := loadClaudeTranscript(agent.Cwd, now)
		if err != nil {
			log.Printf("work transcript lookup failed for claude (%s): %v", agent.Cwd, err)
		}
		return transcript
	default:
		return ToolTranscript{}
	}
}

func agentToolName(command, name string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) > 0 {
		command = strings.ToLower(filepath.Base(fields[0]))
	}
	command = strings.TrimSuffix(command, ".exe")
	name = strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(command, "codex") || strings.Contains(name, "codex"):
		return "codex"
	case command == "cc" || strings.Contains(command, "claude") || strings.Contains(name, "claude"):
		return "claude"
	default:
		return ""
	}
}

func loadCodexTranscript(cwd string, now time.Time) (ToolTranscript, error) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ToolTranscript{}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ToolTranscript{}, err
	}
	dbPath := filepath.Join(home, ".codex", "state_5.sqlite")
	if _, err := os.Stat(dbPath); err != nil {
		return ToolTranscript{}, nil
	}
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		return ToolTranscript{}, nil
	}

	for _, candidateCWD := range transcriptCWDCandidates(cwd) {
		rows, err := queryCodexThreads(sqlite3, dbPath, candidateCWD)
		if err != nil {
			return ToolTranscript{}, err
		}
		for _, row := range rows {
			path := strings.TrimSpace(row.RolloutPath)
			if path == "" {
				continue
			}
			meta, err := readCodexMeta(path)
			if err != nil {
				continue
			}
			if meta.CWD != candidateCWD || strings.EqualFold(meta.Originator, "codex-exec") {
				continue
			}
			info, err := os.Stat(path)
			if err != nil || !isTranscriptFresh(info.ModTime(), now) {
				continue
			}
			excerpt, err := summarizeCodexTranscript(path)
			if err != nil || strings.TrimSpace(excerpt) == "" {
				continue
			}
			return ToolTranscript{
				Source:    "codex",
				Path:      path,
				SessionID: firstNonEmpty(meta.ID, row.ID),
				Updated:   info.ModTime(),
				Excerpt:   excerpt,
			}, nil
		}
	}
	return ToolTranscript{}, nil
}

type codexThreadRow struct {
	ID          string `json:"id"`
	RolloutPath string `json:"rollout_path"`
}

func queryCodexThreads(sqlite3, dbPath, cwd string) ([]codexThreadRow, error) {
	query := fmt.Sprintf(`SELECT id, rollout_path FROM threads WHERE archived = 0 AND cwd = %s ORDER BY coalesce(updated_at_ms, updated_at * 1000) DESC LIMIT 12`, sqlString(cwd))
	out, err := exec.Command(sqlite3, "-json", dbPath, query).Output()
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, nil
	}
	var rows []codexThreadRow
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

type codexMeta struct {
	ID         string
	CWD        string
	Originator string
}

func readCodexMeta(path string) (codexMeta, error) {
	file, err := os.Open(path)
	if err != nil {
		return codexMeta{}, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var envelope struct {
				Type    string `json:"type"`
				Payload struct {
					ID         string `json:"id"`
					CWD        string `json:"cwd"`
					Originator string `json:"originator"`
				} `json:"payload"`
			}
			if json.Unmarshal(line, &envelope) == nil && envelope.Type == "session_meta" {
				return codexMeta{
					ID:         strings.TrimSpace(envelope.Payload.ID),
					CWD:        strings.TrimSpace(envelope.Payload.CWD),
					Originator: strings.TrimSpace(envelope.Payload.Originator),
				}, nil
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return codexMeta{}, err
		}
	}
	return codexMeta{}, fmt.Errorf("missing codex session metadata")
}

func summarizeCodexTranscript(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	builder := newTranscriptBuilder("Codex")
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			builder.consumeCodexLine(line)
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}
	}
	return builder.String(), nil
}

func loadClaudeTranscript(cwd string, now time.Time) (ToolTranscript, error) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ToolTranscript{}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ToolTranscript{}, err
	}
	for _, candidateCWD := range transcriptCWDCandidates(cwd) {
		projectDir := filepath.Join(home, ".claude", "projects", encodeClaudeProjectDir(candidateCWD))
		entries, err := os.ReadDir(projectDir)
		if err != nil {
			continue
		}

		type candidate struct {
			path string
			info os.FileInfo
		}
		var candidates []candidate
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
				continue
			}
			info, err := entry.Info()
			if err != nil || !isTranscriptFresh(info.ModTime(), now) {
				continue
			}
			candidates = append(candidates, candidate{
				path: filepath.Join(projectDir, entry.Name()),
				info: info,
			})
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			return candidates[i].info.ModTime().After(candidates[j].info.ModTime())
		})

		for _, candidate := range candidates {
			meta, err := readClaudeMeta(candidate.path)
			if err != nil {
				continue
			}
			if meta.CWD != "" && meta.CWD != candidateCWD {
				continue
			}
			excerpt, err := summarizeClaudeTranscript(candidate.path)
			if err != nil || strings.TrimSpace(excerpt) == "" {
				continue
			}
			return ToolTranscript{
				Source:    "claude",
				Path:      candidate.path,
				SessionID: meta.SessionID,
				Updated:   candidate.info.ModTime(),
				Excerpt:   excerpt,
			}, nil
		}
	}
	return ToolTranscript{}, nil
}

type claudeMeta struct {
	CWD       string
	SessionID string
}

func readClaudeMeta(path string) (claudeMeta, error) {
	file, err := os.Open(path)
	if err != nil {
		return claudeMeta{}, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	lineCount := 0
	meta := claudeMeta{}
	for {
		line, err := reader.ReadBytes('\n')
		lineCount++
		if len(bytes.TrimSpace(line)) > 0 {
			var envelope struct {
				CWD       string `json:"cwd"`
				SessionID string `json:"sessionId"`
			}
			if json.Unmarshal(line, &envelope) == nil {
				if cwd := strings.TrimSpace(envelope.CWD); cwd != "" {
					meta.CWD = cwd
				}
				if sessionID := strings.TrimSpace(envelope.SessionID); sessionID != "" {
					meta.SessionID = sessionID
				}
				if meta.CWD != "" && meta.SessionID != "" {
					return meta, nil
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return claudeMeta{}, err
		}
		if lineCount >= 80 && (meta.CWD != "" || meta.SessionID != "") {
			return meta, nil
		}
	}
	return meta, nil
}

func summarizeClaudeTranscript(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	builder := newTranscriptBuilder("Claude Code")
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			builder.consumeClaudeLine(line)
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}
	}
	return builder.String(), nil
}

type transcriptBuilder struct {
	source             string
	userTurns          int
	assistantTurns     int
	toolCalls          int
	toolFailures       int
	planCreates        int
	planUpdates        int
	skillUses          int
	permissionModes    int
	fileSnapshots      int
	hookEvents         int
	userClarifications int
	edits              int
	testRuns           int
	searches           int
	reads              int
	events             []string
	seenToolCalls      map[string]string
	surfaces           map[string]int
	userCorrections    int
	lastPrompt         string
	lastPromptCount    int
	lastUserMessage    string
	lastAssistantMsg   string
}

func newTranscriptBuilder(source string) *transcriptBuilder {
	return &transcriptBuilder{
		source:        source,
		seenToolCalls: map[string]string{},
		surfaces:      map[string]int{},
	}
}

func (b *transcriptBuilder) consumeCodexLine(line []byte) {
	var envelope struct {
		Type      string          `json:"type"`
		Timestamp string          `json:"timestamp"`
		Payload   json.RawMessage `json:"payload"`
	}
	if json.Unmarshal(line, &envelope) != nil {
		return
	}

	switch envelope.Type {
	case "event_msg":
		b.consumeCodexEvent(envelope.Payload)
	case "response_item":
		b.consumeCodexResponseItem(envelope.Payload)
	}
}

func (b *transcriptBuilder) consumeClaudeAttachment(raw json.RawMessage) {
	var attachment struct {
		Type      string          `json:"type"`
		HookName  string          `json:"hookName"`
		HookEvent string          `json:"hookEvent"`
		Content   json.RawMessage `json:"content"`
		Stdout    string          `json:"stdout"`
		Stderr    string          `json:"stderr"`
	}
	if json.Unmarshal(raw, &attachment) != nil {
		return
	}
	switch attachment.Type {
	case "hook_success", "hook_additional_context", "command_permissions", "task_reminder":
		b.hookEvents++
		if attachment.Type == "command_permissions" {
			b.permissionModes++
		}
		if attachment.Type == "task_reminder" {
			b.addEvent("Task reminder")
		}
		if attachment.Type == "hook_success" {
			if text := cleanTranscriptText(attachment.Stdout); text != "" {
				b.addEvent("Hook: " + excerptText(text))
			}
		}
	case "mcp_instructions_delta":
		b.hookEvents++
		b.addEvent("MCP instructions updated")
	case "skill_listing":
		b.hookEvents++
	}
}

func (b *transcriptBuilder) consumeCodexEvent(raw json.RawMessage) {
	var payload struct {
		Type             string   `json:"type"`
		Message          string   `json:"message"`
		Phase            string   `json:"phase"`
		CallID           string   `json:"call_id"`
		ExitCode         *int     `json:"exit_code"`
		Status           string   `json:"status"`
		Command          []string `json:"command"`
		AggregatedOutput string   `json:"aggregated_output"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return
	}

	switch payload.Type {
	case "user_message":
		b.addUser(payload.Message)
	case "agent_message":
		b.addAssistant(payload.Message)
	case "exec_command_end":
		command := shellCommandLabel(payload.Command)
		if command == "" {
			command = b.seenToolCalls[payload.CallID]
		}
		exitCode := 0
		if payload.ExitCode != nil {
			exitCode = *payload.ExitCode
		}
		b.addCommand(command, exitCode, payload.AggregatedOutput)
	case "patch_apply_end":
		b.addEvent("Patch: apply_patch completed")
	}
}

func (b *transcriptBuilder) consumeCodexResponseItem(raw json.RawMessage) {
	var payload struct {
		Type      string          `json:"type"`
		Role      string          `json:"role"`
		Content   json.RawMessage `json:"content"`
		Name      string          `json:"name"`
		Arguments string          `json:"arguments"`
		CallID    string          `json:"call_id"`
		Status    string          `json:"status"`
		Input     string          `json:"input"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return
	}

	switch payload.Type {
	case "message":
		text := codexContentText(payload.Content)
		switch payload.Role {
		case "user":
			b.addUser(text)
		case "assistant":
			b.addAssistant(text)
		}
	case "function_call":
		if payload.Name == "exec_command" {
			command := codexExecCommand(payload.Arguments)
			if command != "" {
				b.seenToolCalls[payload.CallID] = command
			}
		}
	case "custom_tool_call":
		if payload.Name == "apply_patch" {
			b.toolCalls++
			b.edits++
			surfaces := patchSurfaces(payload.Input)
			for _, surface := range surfaces {
				b.addSurface(surface)
			}
			if len(surfaces) > 0 {
				b.addEvent("Tool: apply_patch " + strings.Join(surfaces, ", "))
			} else {
				b.addEvent("Tool: apply_patch")
			}
		}
	}
}

func (b *transcriptBuilder) consumeClaudeLine(line []byte) {
	var envelope struct {
		Type    string `json:"type"`
		Message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		Attachment     json.RawMessage `json:"attachment"`
		LastPrompt     string          `json:"lastPrompt"`
		PermissionMode string          `json:"permissionMode"`
	}
	if json.Unmarshal(line, &envelope) != nil {
		return
	}

	switch envelope.Type {
	case "user":
		b.consumeClaudeUser(envelope.Message.Content)
	case "assistant":
		b.consumeClaudeAssistant(envelope.Message.Content)
	case "attachment":
		b.consumeClaudeAttachment(envelope.Attachment)
	case "last-prompt":
		b.consumeClaudeLastPrompt(envelope.LastPrompt)
	case "permission-mode":
		b.permissionModes++
		if mode := strings.TrimSpace(envelope.PermissionMode); mode != "" {
			b.addEvent("Permission mode: " + mode)
		}
	case "file-history-snapshot":
		b.fileSnapshots++
	}
}

func (b *transcriptBuilder) consumeClaudeLastPrompt(prompt string) {
	prompt = cleanTranscriptText(prompt)
	if prompt == "" {
		return
	}
	if b.lastPrompt == prompt {
		b.lastPromptCount++
		return
	}
	b.lastPrompt = prompt
	b.lastPromptCount = 1
	b.addEvent("Prompt: " + excerptText(prompt))
}

func (b *transcriptBuilder) consumeClaudeUser(raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	if raw[0] == '"' {
		var text string
		if json.Unmarshal(raw, &text) == nil {
			b.addUser(text)
		}
		return
	}
	var items []map[string]json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return
	}
	for _, item := range items {
		itemType := jsonString(item["type"])
		switch itemType {
		case "text":
			text := jsonString(item["text"])
			b.addUser(text)
			if looksLikeUserClarification(text) {
				b.userClarifications++
			}
		case "tool_result":
			isError := jsonBool(item["is_error"])
			if isError {
				b.toolFailures++
				b.addEvent("Tool result failed: " + excerptText(claudeContentText(item["content"])))
			}
		}
	}
}

func (b *transcriptBuilder) consumeClaudeAssistant(raw json.RawMessage) {
	var items []map[string]json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		if len(raw) > 0 && raw[0] == '"' {
			var text string
			if json.Unmarshal(raw, &text) == nil {
				b.addAssistant(text)
			}
		}
		return
	}
	for _, item := range items {
		itemType := jsonString(item["type"])
		switch itemType {
		case "text":
			b.addAssistant(jsonString(item["text"]))
		case "tool_use":
			name := jsonString(item["name"])
			b.toolCalls++
			if name == "Edit" || name == "MultiEdit" || name == "Write" {
				b.edits++
			}
			if name == "Read" {
				b.reads++
			}
			if name == "Grep" || name == "Glob" || name == "LS" {
				b.searches++
			}
			label := "Tool: " + name
			if input := claudeToolInputSummary(name, item["input"]); input != "" {
				label += " " + input
			}
			if surface := claudeToolSurface(name, item["input"]); surface != "" {
				b.addSurface(surface)
			}
			isValidation := isTestCommand(label)
			switch name {
			case "TaskCreate":
				b.planCreates++
			case "TaskUpdate":
				b.planUpdates++
			case "Skill":
				b.skillUses++
			case "AskUserQuestion":
				b.userClarifications++
			case "Bash":
				if looksLikeValidationCommand(label) {
					isValidation = true
				}
			}
			if isValidation {
				b.testRuns++
			}
			b.addEvent(label)
		}
	}
}

func (b *transcriptBuilder) addUser(text string) {
	text = cleanTranscriptText(text)
	if text == "" || isTranscriptBoilerplate(text) || text == b.lastUserMessage {
		return
	}
	b.userTurns++
	if looksLikeUserCorrection(text) {
		b.userCorrections++
	}
	b.lastUserMessage = text
	b.addEvent("User: " + excerptText(text))
}

func (b *transcriptBuilder) addAssistant(text string) {
	text = cleanTranscriptText(text)
	if text == "" || isTranscriptBoilerplate(text) || text == b.lastAssistantMsg {
		return
	}
	b.assistantTurns++
	b.lastAssistantMsg = text
	b.addEvent("Assistant: " + excerptText(text))
}

func (b *transcriptBuilder) addCommand(command string, exitCode int, output string) {
	command = cleanTranscriptText(command)
	if command == "" {
		return
	}
	b.toolCalls++
	b.classifyCommand(command)
	label := fmt.Sprintf("Command exit=%d: %s", exitCode, excerptText(command))
	if exitCode != 0 {
		b.toolFailures++
		if detail := importantOutputLine(output); detail != "" {
			label += " | " + excerptText(detail)
		}
	}
	b.addEvent(label)
}

func (b *transcriptBuilder) classifyCommand(command string) {
	lower := strings.ToLower(command)
	if strings.Contains(lower, "apply_patch") {
		b.edits++
	}
	if strings.Contains(lower, "go test") ||
		strings.Contains(lower, "bun x tsc") ||
		strings.Contains(lower, "npm test") ||
		strings.Contains(lower, "pytest") ||
		strings.Contains(lower, "gradlew") {
		b.testRuns++
	}
	if strings.Contains(lower, "rg ") ||
		strings.HasPrefix(lower, "rg ") ||
		strings.Contains(lower, "find ") ||
		strings.Contains(lower, "grep ") {
		b.searches++
	}
	if strings.Contains(lower, "sed ") ||
		strings.Contains(lower, "nl ") ||
		strings.Contains(lower, "cat ") {
		b.reads++
	}
}

func (b *transcriptBuilder) addSurface(surface string) {
	surface = cleanTranscriptText(surface)
	if surface == "" {
		return
	}
	b.surfaces[surface]++
}

func (b *transcriptBuilder) addEvent(event string) {
	event = excerptText(event)
	if event == "" {
		return
	}
	if len(b.events) > 0 && b.events[len(b.events)-1] == event {
		return
	}
	b.events = append(b.events, event)
	if len(b.events) > maxTranscriptEvents {
		copy(b.events, b.events[len(b.events)-maxTranscriptEvents:])
		b.events = b.events[:maxTranscriptEvents]
	}
}

func (b *transcriptBuilder) String() string {
	if len(b.events) == 0 {
		return ""
	}
	lines := []string{
		fmt.Sprintf(
			"Transcript summary: user_turns=%d assistant_notes=%d tool_calls=%d failures=%d edits=%d test_runs=%d searches=%d reads=%d user_corrections=%d user_clarifications=%d plan_creates=%d plan_updates=%d skill_uses=%d permission_modes=%d file_snapshots=%d hook_events=%d",
			b.userTurns,
			b.assistantTurns,
			b.toolCalls,
			b.toolFailures,
			b.edits,
			b.testRuns,
			b.searches,
			b.reads,
			b.userCorrections,
			b.userClarifications,
			b.planCreates,
			b.planUpdates,
			b.skillUses,
			b.permissionModes,
			b.fileSnapshots,
			b.hookEvents,
		),
	}
	if repeated := b.repeatedSurfaces(); len(repeated) > 0 {
		lines = append(lines, "Repeated work surfaces: "+strings.Join(repeated, ", "))
	}
	if repeatedPrompt := b.repeatedLastPrompt(); repeatedPrompt != "" {
		lines = append(lines, "Repeated user prompt: "+repeatedPrompt)
	}
	lines = append(lines, "Recent meaningful events:")
	for _, event := range b.events {
		lines = append(lines, "- "+event)
	}
	out := strings.Join(lines, "\n")
	if len(out) > maxTranscriptChars {
		return out[len(out)-maxTranscriptChars:]
	}
	return out
}

func (b *transcriptBuilder) repeatedSurfaces() []string {
	if len(b.surfaces) == 0 {
		return nil
	}
	type entry struct {
		name  string
		count int
	}
	var entries []entry
	for name, count := range b.surfaces {
		if count < 2 {
			continue
		}
		entries = append(entries, entry{name: name, count: count})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		return entries[i].name < entries[j].name
	})
	out := make([]string, 0, len(entries))
	for _, item := range entries {
		out = append(out, fmt.Sprintf("%s x%d", item.name, item.count))
		if len(out) >= 5 {
			break
		}
	}
	return out
}

func (b *transcriptBuilder) repeatedLastPrompt() string {
	if b.lastPrompt == "" || b.lastPromptCount < 1 {
		return ""
	}
	return fmt.Sprintf("%s x%d", excerptText(b.lastPrompt), b.lastPromptCount)
}

func codexContentText(raw json.RawMessage) string {
	var items []map[string]json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return ""
	}
	var parts []string
	for _, item := range items {
		itemType := jsonString(item["type"])
		switch itemType {
		case "input_text", "output_text":
			parts = append(parts, jsonString(item["text"]))
		}
	}
	return strings.Join(parts, "\n")
}

func codexExecCommand(arguments string) string {
	var payload struct {
		Cmd string `json:"cmd"`
	}
	if json.Unmarshal([]byte(arguments), &payload) != nil {
		return ""
	}
	return payload.Cmd
}

func claudeContentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		return jsonString(raw)
	}
	var items []map[string]json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return ""
	}
	var parts []string
	for _, item := range items {
		if text := jsonString(item["text"]); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func claudeToolInputSummary(name string, raw json.RawMessage) string {
	var input map[string]json.RawMessage
	if json.Unmarshal(raw, &input) != nil {
		return ""
	}
	switch name {
	case "Bash":
		return excerptText(jsonString(input["command"]))
	case "TaskCreate":
		if subject := jsonString(input["subject"]); subject != "" {
			return subject
		}
	case "TaskUpdate":
		parts := []string{}
		if taskID := jsonString(input["taskId"]); taskID != "" {
			parts = append(parts, taskID)
		}
		if status := jsonString(input["status"]); status != "" {
			parts = append(parts, status)
		}
		return strings.Join(parts, " ")
	case "AskUserQuestion":
		if questions, ok := input["questions"]; ok {
			return summarizeAskUserQuestions(questions)
		}
	case "Skill":
		if skill := jsonString(input["skill"]); skill != "" {
			return skill
		}
	case "Read", "Edit", "MultiEdit", "Write":
		if path := jsonString(input["file_path"]); path != "" {
			return path
		}
	case "Grep":
		parts := []string{}
		if pattern := jsonString(input["pattern"]); pattern != "" {
			parts = append(parts, pattern)
		}
		if path := jsonString(input["path"]); path != "" {
			parts = append(parts, path)
		}
		return strings.Join(parts, " ")
	case "Glob":
		return jsonString(input["pattern"])
	case "LS":
		return jsonString(input["path"])
	}
	return ""
}

func claudeToolSurface(name string, raw json.RawMessage) string {
	if name != "Read" && name != "Edit" && name != "MultiEdit" && name != "Write" {
		return ""
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(raw, &input) != nil {
		return ""
	}
	return jsonString(input["file_path"])
}

func summarizeAskUserQuestions(raw json.RawMessage) string {
	type question struct {
		Header   string `json:"header"`
		Question string `json:"question"`
	}
	var questions []question
	if json.Unmarshal(raw, &questions) != nil {
		return "questions"
	}
	if len(questions) == 0 {
		return "questions=0"
	}
	parts := make([]string, 0, len(questions))
	for _, q := range questions {
		label := strings.TrimSpace(q.Header)
		if label == "" {
			label = strings.TrimSpace(q.Question)
		}
		if label != "" {
			parts = append(parts, label)
		}
		if len(parts) >= 3 {
			break
		}
	}
	if len(parts) == 0 {
		return "questions"
	}
	return "questions: " + strings.Join(parts, ", ")
}

func looksLikeUserClarification(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{
		"不对",
		"不是",
		"还是",
		"没看到",
		"为什么",
		"再",
		"重新",
		"更",
		"遮挡",
		"not",
		"still",
		"again",
		"why",
		"missing",
		"wrong",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func looksLikeValidationCommand(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "test") ||
		strings.Contains(lower, "tsc") ||
		strings.Contains(lower, "lint") ||
		strings.Contains(lower, "build") ||
		strings.Contains(lower, "export") ||
		strings.Contains(lower, "verify") ||
		strings.Contains(lower, "check") ||
		strings.Contains(lower, "git diff") ||
		strings.Contains(lower, "git status")
}

func patchSurfaces(patch string) []string {
	if strings.TrimSpace(patch) == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(patch, "\n") {
		line = strings.TrimSpace(line)
		var path string
		switch {
		case strings.HasPrefix(line, "*** Update File: "):
			path = strings.TrimSpace(strings.TrimPrefix(line, "*** Update File: "))
		case strings.HasPrefix(line, "*** Add File: "):
			path = strings.TrimSpace(strings.TrimPrefix(line, "*** Add File: "))
		case strings.HasPrefix(line, "*** Delete File: "):
			path = strings.TrimSpace(strings.TrimPrefix(line, "*** Delete File: "))
		}
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func shellCommandLabel(command []string) string {
	if len(command) == 0 {
		return ""
	}
	if len(command) >= 3 && (strings.HasSuffix(command[0], "sh") || strings.HasSuffix(command[0], "zsh") || strings.HasSuffix(command[0], "bash")) && command[1] == "-lc" {
		return command[2]
	}
	return strings.Join(command, " ")
}

func importantOutputLine(output string) string {
	output = strings.ReplaceAll(output, "\r\n", "\n")
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error") ||
			strings.Contains(lower, "failed") ||
			strings.Contains(lower, "panic") ||
			strings.Contains(lower, "not found") ||
			strings.Contains(lower, "permission denied") ||
			strings.Contains(lower, "exit status") {
			return line
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

func isTestCommand(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "go test") ||
		strings.Contains(lower, "bun x tsc") ||
		strings.Contains(lower, "npm test") ||
		strings.Contains(lower, "pytest") ||
		strings.Contains(lower, "gradlew")
}

func looksLikeUserCorrection(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{
		"不对",
		"搞错",
		"不是",
		"不应该",
		"还是",
		"重新",
		"浅",
		"没看到",
		"为什么",
		"wrong",
		"not what",
		"actually",
		"instead",
		"still",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func cleanTranscriptText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.TrimSpace(value)
	value = strings.Join(strings.Fields(value), " ")
	return value
}

func isTranscriptBoilerplate(value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(trimmed, "<environment_context>") ||
		strings.HasPrefix(trimmed, "<permissions instructions>") ||
		strings.HasPrefix(trimmed, "<collaboration_mode>") ||
		strings.HasPrefix(trimmed, "<skills_instructions>") ||
		strings.HasPrefix(trimmed, "<local-command-caveat>") ||
		strings.HasPrefix(trimmed, "<command-name>") ||
		strings.HasPrefix(trimmed, "<local-command-stdout>") ||
		strings.Contains(lower, "base directory for this skill") ||
		strings.Contains(lower, "you are codex")
}

func excerptText(value string) string {
	value = cleanTranscriptText(value)
	if len(value) <= maxTranscriptLine {
		return value
	}
	return strings.TrimSpace(value[:maxTranscriptLine-3]) + "..."
}

func encodeClaudeProjectDir(cwd string) string {
	clean := filepath.Clean(cwd)
	return strings.ReplaceAll(clean, string(filepath.Separator), "-")
}

func transcriptCWDCandidates(cwd string) []string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return nil
	}
	clean := filepath.Clean(cwd)
	if clean == "." {
		return nil
	}

	var out []string
	seen := map[string]bool{}
	add := func(path string) {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "" || path == "." || seen[path] {
			return
		}
		seen[path] = true
		out = append(out, path)
	}

	add(clean)
	if gitRoot := nearestGitRoot(clean); gitRoot != "" {
		add(gitRoot)
		return out
	}
	for parent := filepath.Dir(clean); parent != clean && parent != "." && parent != string(filepath.Separator); parent = filepath.Dir(parent) {
		add(parent)
		if len(out) >= 3 {
			break
		}
		clean = parent
	}
	return out
}

func nearestGitRoot(cwd string) string {
	for dir := filepath.Clean(cwd); dir != "." && dir != string(filepath.Separator); dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
	}
	return ""
}

func isTranscriptFresh(updated, now time.Time) bool {
	if updated.IsZero() || now.IsZero() {
		return true
	}
	if updated.After(now.Add(10 * time.Minute)) {
		return true
	}
	return now.Sub(updated) <= maxTranscriptAge
}

func sqlString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func jsonString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var out string
	if err := json.Unmarshal(raw, &out); err == nil {
		return strings.TrimSpace(out)
	}
	return ""
}

func jsonBool(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var out bool
	if err := json.Unmarshal(raw, &out); err == nil {
		return out
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		parsed, _ := strconv.ParseBool(text)
		return parsed
	}
	return false
}
