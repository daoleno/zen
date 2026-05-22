package server

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const codexSlashCommandCacheTTL = 10 * time.Minute
const maxCodexBinaryScanBytes = 260 << 20
const maxSlashCommandNameLen = 48

var codexSlashCommandCache = struct {
	sync.Mutex
	snapshot  CodexSlashCommandSnapshot
	expiresAt time.Time
}{}

type CodexSlashCommand struct {
	Value             string                  `json:"value"`
	Name              string                  `json:"name"`
	Title             string                  `json:"title"`
	Description       string                  `json:"description"`
	Source            string                  `json:"source,omitempty"`
	Category          string                  `json:"category"`
	Execution         string                  `json:"execution"`
	Input             CodexSlashCommandInput  `json:"input"`
	Output            CodexSlashCommandOutput `json:"output"`
	Interactive       bool                    `json:"interactive"`
	ChatSupported     bool                    `json:"chat_supported"`
	TerminalSupported bool                    `json:"terminal_supported"`
}

type CodexSlashCommandInput struct {
	Kind        string `json:"kind"`
	Placeholder string `json:"placeholder,omitempty"`
	Picker      string `json:"picker,omitempty"`
}

type CodexSlashCommandOutput struct {
	Kind string `json:"kind"`
}

type CodexSlashCommandSnapshot struct {
	GeneratedAt time.Time           `json:"generated_at"`
	Source      string              `json:"source"`
	Version     string              `json:"version,omitempty"`
	Commands    []CodexSlashCommand `json:"commands"`
}

type codexSlashCommandSpec struct {
	name        string
	description string
}

type codexSlashCommandCapability struct {
	category          string
	execution         string
	input             CodexSlashCommandInput
	output            CodexSlashCommandOutput
	interactive       bool
	chatSupported     bool
	terminalSupported bool
}

type codexSlashCommandDiscovery struct {
	names        []string
	descriptions map[string]string
}

type binaryAddressMapper struct {
	segments []binaryAddressSegment
}

type binaryAddressSegment struct {
	offset uint64
	vaddr  uint64
	size   uint64
}

var codexSlashCommandSpecs = []codexSlashCommandSpec{
	{name: "model", description: "choose what model and reasoning effort to use"},
	{name: "fast", description: "1.5x speed, increased usage"},
	{name: "ide", description: "include current selection, open files, and other context from your IDE"},
	{name: "permissions", description: "choose what Codex is allowed to do"},
	{name: "keymap", description: "remap TUI shortcuts"},
	{name: "setup-default-sandbox", description: "set up elevated agent sandbox"},
	{name: "sandbox-add-read-dir", description: "let sandbox read a directory: /sandbox-add-read-dir <absolute_path>"},
	{name: "vim", description: "toggle Vim mode for the composer"},
	{name: "experimental", description: "toggle experimental features"},
	{name: "approve", description: "approve one retry of a recent auto-review denial"},
	{name: "memories", description: "configure memory use and generation"},
	{name: "skills", description: "use skills to improve how Codex performs specific tasks"},
	{name: "hooks", description: "view and manage lifecycle hooks"},
	{name: "review", description: "review my current changes and find issues"},
	{name: "rename", description: "rename the current thread"},
	{name: "new", description: "start a new chat during a conversation"},
	{name: "resume", description: "resume a saved chat"},
	{name: "fork", description: "fork the current chat"},
	{name: "init", description: "create an AGENTS.md file with instructions for Codex"},
	{name: "compact", description: "summarize conversation to prevent hitting the context limit"},
	{name: "plan", description: "switch to Plan mode"},
	{name: "goal", description: "set or view the goal for a long-running task"},
	{name: "side", description: "start a side conversation in an ephemeral fork"},
	{name: "copy", description: "copy last response as markdown"},
	{name: "raw", description: "toggle raw scrollback mode for copy-friendly terminal selection"},
	{name: "diff", description: "show git diff (including untracked files)"},
	{name: "mention", description: "mention a file"},
	{name: "status", description: "show current session configuration and token usage"},
	{name: "debug-config", description: "show config layers and requirement sources for debugging"},
	{name: "title", description: "configure which items appear in the terminal title"},
	{name: "statusline", description: "configure which items appear in the status line"},
	{name: "theme", description: "choose a syntax highlighting theme"},
	{name: "pets", description: "choose or hide the terminal pet"},
	{name: "mcp", description: "list configured MCP tools; use /mcp verbose for details"},
	{name: "apps", description: "manage apps"},
	{name: "plugins", description: "browse plugins"},
	{name: "logout", description: "log out of Codex"},
	{name: "quit", description: "exit Codex"},
	{name: "exit", description: "exit Codex"},
	{name: "feedback", description: "send logs to maintainers"},
	{name: "rollout", description: "print the rollout file path"},
	{name: "ps", description: "list background terminals"},
	{name: "stop", description: "stop all background terminals"},
	{name: "clear", description: "clear the terminal and start a new chat"},
	{name: "personality", description: "choose a communication style for Codex"},
	{name: "realtime", description: "toggle realtime voice mode (experimental)"},
	{name: "settings", description: "configure realtime microphone/speaker"},
	{name: "test-approval", description: "test approval request"},
	{name: "agent", description: "switch the active agent thread"},
	{name: "subagents", description: "switch the active agent thread"},
	{name: "btw", description: "start a side conversation in an ephemeral fork"},
	{name: "debug-m-drop", description: "DO NOT USE"},
	{name: "debug-m-update", description: "DO NOT USE"},
}

var codexSlashCommandCapabilities = map[string]codexSlashCommandCapability{
	"model":                 pickerCommand("settings", "model", "management-screen"),
	"fast":                  terminalCommand("settings", inlineArgs("optional speed mode"), "terminal", true),
	"ide":                   terminalCommand("settings", inputNone(), "terminal", true),
	"permissions":           pickerCommand("settings", "permissions", "management-screen"),
	"keymap":                terminalCommand("settings", inputNone(), "terminal", true),
	"setup-default-sandbox": terminalCommand("settings", inputNone(), "terminal", true),
	"sandbox-add-read-dir":  terminalCommand("settings", inlineArgs("<absolute_path>"), "terminal", true),
	"vim":                   terminalCommand("settings", inputNone(), "terminal", true),
	"experimental":          terminalCommand("settings", inputNone(), "terminal", true),
	"approve":               terminalCommand("session", inputNone(), "terminal", false),
	"memories":              terminalCommand("management", inputNone(), "management-screen", true),
	"skills":                terminalCommand("management", inputNone(), "management-screen", true),
	"hooks":                 terminalCommand("management", inputNone(), "management-screen", true),
	"review":                terminalCommand("tools", inputNone(), "terminal", false),
	"rename":                terminalCommand("session", freeformInput("new thread title"), "terminal", false),
	"new":                   terminalCommand("session", inputNone(), "terminal", true),
	"resume":                terminalCommand("navigation", pickerInput("conversation"), "management-screen", true),
	"fork":                  terminalCommand("navigation", pickerInput("conversation"), "management-screen", true),
	"init":                  terminalCommand("tools", inputNone(), "terminal", false),
	"compact":               terminalCommand("session", inputNone(), "terminal", false),
	"plan":                  terminalCommand("session", inputNone(), "terminal", false),
	"goal":                  terminalCommand("session", freeformInput("goal text"), "terminal", false),
	"side":                  terminalCommand("navigation", freeformInput("side conversation prompt"), "terminal", true),
	"copy":                  chatCommand("tools", inputNone(), "none"),
	"raw":                   terminalCommand("tools", inputNone(), "terminal", true),
	"diff":                  chatCommand("tools", inputNone(), "diff"),
	"mention":               terminalCommand("tools", pickerInput("file"), "terminal", true),
	"status":                chatCommand("session", inputNone(), "status-card"),
	"debug-config":          terminalCommand("debug", inputNone(), "terminal", false),
	"title":                 terminalCommand("settings", inputNone(), "management-screen", true),
	"statusline":            terminalCommand("settings", inputNone(), "management-screen", true),
	"theme":                 terminalCommand("settings", pickerInput("theme"), "management-screen", true),
	"pets":                  terminalCommand("settings", pickerInput("pet"), "management-screen", true),
	"mcp":                   terminalCommand("management", inlineArgs("verbose"), "management-screen", true),
	"apps":                  terminalCommand("management", inputNone(), "management-screen", true),
	"plugins":               terminalCommand("management", inputNone(), "management-screen", true),
	"logout":                terminalCommand("danger", inputNone(), "terminal", true),
	"quit":                  terminalCommand("danger", inputNone(), "terminal", true),
	"exit":                  terminalCommand("danger", inputNone(), "terminal", true),
	"feedback":              terminalCommand("management", inputNone(), "terminal", true),
	"rollout":               terminalCommand("tools", inputNone(), "terminal", false),
	"ps":                    terminalCommand("tools", inputNone(), "terminal", false),
	"stop":                  terminalCommand("tools", inputNone(), "terminal", false),
	"clear":                 terminalCommand("session", inputNone(), "terminal", true),
	"personality":           terminalCommand("settings", pickerInput("personality"), "management-screen", true),
	"realtime":              terminalCommand("settings", inputNone(), "terminal", true),
	"settings":              terminalCommand("settings", inputNone(), "management-screen", true),
	"test-approval":         terminalCommand("debug", inputNone(), "terminal", true),
	"agent":                 terminalCommand("navigation", pickerInput("agent"), "management-screen", true),
	"subagents":             terminalCommand("navigation", pickerInput("agent"), "management-screen", true),
	"btw":                   terminalCommand("navigation", freeformInput("side conversation prompt"), "terminal", true),
	"debug-m-drop":          unsupportedCommand("debug"),
	"debug-m-update":        unsupportedCommand("debug"),
}

func discoverCodexSlashCommands(now time.Time) CodexSlashCommandSnapshot {
	if now.IsZero() {
		now = time.Now()
	}

	codexSlashCommandCache.Lock()
	if now.Before(codexSlashCommandCache.expiresAt) && len(codexSlashCommandCache.snapshot.Commands) > 0 {
		snapshot := codexSlashCommandCache.snapshot
		codexSlashCommandCache.Unlock()
		return snapshot
	}
	codexSlashCommandCache.Unlock()

	version := codexVersion()
	commands, source := discoverCodexSlashCommandsUncached()
	if len(commands) == 0 {
		commands = defaultCodexSlashCommands("fallback")
		source = "fallback"
	}
	snapshot := CodexSlashCommandSnapshot{
		GeneratedAt: now.UTC(),
		Source:      source,
		Version:     version,
		Commands:    commands,
	}

	codexSlashCommandCache.Lock()
	codexSlashCommandCache.snapshot = snapshot
	codexSlashCommandCache.expiresAt = now.Add(codexSlashCommandCacheTTL)
	codexSlashCommandCache.Unlock()

	return snapshot
}

func discoverCodexSlashCommandsUncached() ([]CodexSlashCommand, string) {
	binaryPath, ok := resolveCodexNativeBinary()
	if !ok {
		return defaultCodexSlashCommands("fallback"), "fallback"
	}
	info, err := os.Stat(binaryPath)
	if err != nil || info.IsDir() || info.Size() <= 0 || info.Size() > maxCodexBinaryScanBytes {
		return defaultCodexSlashCommands("fallback"), "fallback"
	}
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		return defaultCodexSlashCommands("fallback"), "fallback"
	}
	discovery := parseCodexSlashCommandDiscovery(data)
	if len(discovery.names) == 0 && len(discovery.descriptions) == 0 {
		return defaultCodexSlashCommands("fallback"), "fallback"
	}
	return commandsFromDiscovery(discovery), "codex-binary"
}

func defaultCodexSlashCommands(source string) []CodexSlashCommand {
	commands := make([]CodexSlashCommand, 0, len(codexSlashCommandSpecs))
	for _, spec := range codexSlashCommandSpecs {
		commands = append(commands, codexSlashCommandFromSpec(spec, spec.description, source))
	}
	return commands
}

func commandsFromDiscovery(discovery codexSlashCommandDiscovery) []CodexSlashCommand {
	commands := make([]CodexSlashCommand, 0, len(codexSlashCommandSpecs))
	seen := make(map[string]bool, len(codexSlashCommandSpecs)+len(discovery.names))
	for _, spec := range codexSlashCommandSpecs {
		description := spec.description
		if discovered := strings.TrimSpace(discovery.descriptions[spec.name]); discovered != "" {
			description = discovered
		}
		commands = append(commands, codexSlashCommandFromSpec(spec, description, "codex-binary"))
		seen[spec.name] = true
	}
	for _, name := range discovery.names {
		if seen[name] {
			continue
		}
		description := strings.TrimSpace(discovery.descriptions[name])
		if description == "" {
			description = "Codex slash command"
		}
		commands = append(commands, codexSlashCommandFromName(name, description, "codex-binary"))
		seen[name] = true
	}
	return commands
}

func codexSlashCommandFromSpec(spec codexSlashCommandSpec, description, source string) CodexSlashCommand {
	return codexSlashCommandFromName(spec.name, description, source)
}

func codexSlashCommandFromName(name, description, source string) CodexSlashCommand {
	capability := slashCommandCapability(name)
	return CodexSlashCommand{
		Value:             "/" + name,
		Name:              name,
		Title:             slashCommandTitle(name),
		Description:       description,
		Source:            source,
		Category:          capability.category,
		Execution:         capability.execution,
		Input:             capability.input,
		Output:            capability.output,
		Interactive:       capability.interactive,
		ChatSupported:     capability.chatSupported,
		TerminalSupported: capability.terminalSupported,
	}
}

func slashCommandCapability(name string) codexSlashCommandCapability {
	if capability, ok := codexSlashCommandCapabilities[name]; ok {
		return capability.withDefaults()
	}
	if strings.HasPrefix(name, "debug-") || strings.HasPrefix(name, "test-") {
		return terminalCommand("debug", inputNone(), "terminal", true).withDefaults()
	}
	return terminalCommand("unknown", inputNone(), "terminal", true).withDefaults()
}

func (capability codexSlashCommandCapability) withDefaults() codexSlashCommandCapability {
	if capability.category == "" {
		capability.category = "unknown"
	}
	if capability.execution == "" {
		capability.execution = "terminal-required"
	}
	if capability.input.Kind == "" {
		capability.input.Kind = "none"
	}
	if capability.output.Kind == "" {
		capability.output.Kind = "terminal"
	}
	if capability.execution != "unsupported" {
		capability.terminalSupported = true
	}
	return capability
}

func chatCommand(category string, input CodexSlashCommandInput, outputKind string) codexSlashCommandCapability {
	return codexSlashCommandCapability{
		category:          category,
		execution:         "chat-native",
		input:             input,
		output:            CodexSlashCommandOutput{Kind: outputKind},
		interactive:       input.Kind == "picker" || input.Kind == "form",
		chatSupported:     true,
		terminalSupported: true,
	}
}

func terminalCommand(category string, input CodexSlashCommandInput, outputKind string, interactive bool) codexSlashCommandCapability {
	return codexSlashCommandCapability{
		category:          category,
		execution:         "terminal-required",
		input:             input,
		output:            CodexSlashCommandOutput{Kind: outputKind},
		interactive:       interactive,
		chatSupported:     false,
		terminalSupported: true,
	}
}

func pickerCommand(category, picker, outputKind string) codexSlashCommandCapability {
	return terminalCommand(category, pickerInput(picker), outputKind, true)
}

func unsupportedCommand(category string) codexSlashCommandCapability {
	return codexSlashCommandCapability{
		category:          category,
		execution:         "unsupported",
		input:             inputNone(),
		output:            CodexSlashCommandOutput{Kind: "terminal"},
		interactive:       true,
		chatSupported:     false,
		terminalSupported: false,
	}
}

func inputNone() CodexSlashCommandInput {
	return CodexSlashCommandInput{Kind: "none"}
}

func inlineArgs(placeholder string) CodexSlashCommandInput {
	return CodexSlashCommandInput{Kind: "inline-args", Placeholder: placeholder}
}

func freeformInput(placeholder string) CodexSlashCommandInput {
	return CodexSlashCommandInput{Kind: "freeform", Placeholder: placeholder}
}

func pickerInput(picker string) CodexSlashCommandInput {
	return CodexSlashCommandInput{Kind: "picker", Picker: picker}
}

func parseCodexSlashCommandDiscovery(data []byte) codexSlashCommandDiscovery {
	descriptions := parseCodexSlashDescriptions(data)
	if descriptions == nil {
		descriptions = map[string]string{}
	}
	if fastDescription := parseCodexFastSlashDescription(data); fastDescription != "" {
		descriptions["fast"] = fastDescription
	}
	return codexSlashCommandDiscovery{
		names:        parseCodexSlashCommandNames(data),
		descriptions: descriptions,
	}
}

func parseCodexSlashDescriptions(data []byte) map[string]string {
	const marker = "choose what model and reasoning effort to use"
	const terminator = "DO NOT USE"

	start := bytes.Index(data, []byte(marker))
	if start < 0 {
		return nil
	}
	end := bytes.Index(data[start:], []byte(terminator))
	if end < 0 {
		return nil
	}
	block := string(data[start : start+end])
	result := make(map[string]string)
	cursor := 0
	for _, spec := range codexSlashCommandSpecs {
		if spec.name == "fast" || spec.name == "vim" || spec.name == "logout" || spec.name == "quit" || spec.name == "subagents" || spec.name == "btw" || spec.name == "debug-m-drop" || spec.name == "debug-m-update" {
			continue
		}
		index := strings.Index(block[cursor:], spec.description)
		if index < 0 {
			continue
		}
		cursor += index + len(spec.description)
		result[spec.name] = spec.description
	}
	return result
}

func parseCodexFastSlashDescription(data []byte) string {
	const marker = `"id": "priority"`
	const descriptionKey = `"description": `

	start := bytes.Index(data, []byte(marker))
	if start < 0 {
		return ""
	}
	windowEnd := min(len(data), start+1024)
	window := data[start:windowEnd]
	descriptionIndex := bytes.Index(window, []byte(descriptionKey))
	if descriptionIndex < 0 {
		return ""
	}
	raw := window[descriptionIndex+len(descriptionKey):]
	if len(raw) == 0 || raw[0] != '"' {
		return ""
	}
	end := 1
	for end < len(raw) {
		if raw[end] == '"' && raw[end-1] != '\\' {
			break
		}
		end++
	}
	if end >= len(raw) {
		return ""
	}
	unquoted, err := strconv.Unquote(string(raw[:end+1]))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(unquoted)
}

func parseCodexSlashCommandNames(data []byte) []string {
	if len(data) < 16 {
		return nil
	}

	mapper := newBinaryAddressMapper(data)
	startSet := map[int]bool{}
	for offset := 0; offset+8 <= len(data); offset += 8 {
		pointer, ok := mapper.fileOffset(binary.LittleEndian.Uint64(data[offset : offset+8]))
		if !ok || pointer < 0 || pointer >= len(data) || !isSlashCommandNameStart(data[pointer]) {
			continue
		}
		startSet[pointer] = true
	}
	if len(startSet) == 0 {
		return nil
	}

	starts := make([]int, 0, len(startSet))
	for start := range startSet {
		starts = append(starts, start)
	}
	sort.Ints(starts)

	fallbackNames := fallbackSlashCommandNameSet()
	seen := map[string]bool{}
	var discovered []string
	for i := 0; i < len(starts); {
		cluster := []int{starts[i]}
		i++
		for i < len(starts) {
			previous := cluster[len(cluster)-1]
			next := starts[i]
			if next <= previous || next-previous > maxSlashCommandNameLen || !isSlashCommandNameBytes(data[previous:next]) {
				break
			}
			cluster = append(cluster, next)
			i++
		}
		if len(cluster) < 2 {
			continue
		}

		names := namesFromSlashCommandCluster(data, cluster, fallbackNames)
		knownCount := 0
		hasAnchor := false
		for _, name := range names {
			if fallbackNames[name] {
				knownCount++
			}
			if isSlashCommandClusterAnchor(name) {
				hasAnchor = true
			}
		}
		if knownCount < 3 && !hasAnchor {
			continue
		}

		for _, name := range names {
			if seen[name] {
				continue
			}
			if fallbackNames[name] || hasAnchor || (knownCount >= 3 && strings.Contains(name, "-")) {
				discovered = append(discovered, name)
				seen[name] = true
			}
		}
	}
	return discovered
}

func newBinaryAddressMapper(data []byte) binaryAddressMapper {
	mapper := binaryAddressMapper{
		segments: []binaryAddressSegment{{offset: 0, vaddr: 0, size: uint64(len(data))}},
	}
	file, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return mapper
	}
	defer file.Close()

	var segments []binaryAddressSegment
	for _, program := range file.Progs {
		if program.Type != elf.PT_LOAD || program.Filesz == 0 {
			continue
		}
		segments = append(segments, binaryAddressSegment{
			offset: program.Off,
			vaddr:  program.Vaddr,
			size:   program.Filesz,
		})
	}
	if len(segments) == 0 {
		return mapper
	}
	return binaryAddressMapper{segments: segments}
}

func (mapper binaryAddressMapper) fileOffset(address uint64) (int, bool) {
	for _, segment := range mapper.segments {
		if address < segment.vaddr || address >= segment.vaddr+segment.size {
			continue
		}
		offset := segment.offset + (address - segment.vaddr)
		if offset > uint64(^uint(0)>>1) {
			return 0, false
		}
		return int(offset), true
	}
	return 0, false
}

func namesFromSlashCommandCluster(data []byte, cluster []int, fallbackNames map[string]bool) []string {
	var names []string
	for index, start := range cluster {
		end := start
		if index+1 < len(cluster) {
			end = cluster[index+1]
		} else {
			end = slashCommandNameEnd(data, start)
		}
		name := strings.TrimSpace(string(data[start:end]))
		if index+1 == len(cluster) {
			name = longestKnownSlashCommandPrefix(name, fallbackNames)
		}
		if isSlashCommandName(name) {
			names = append(names, name)
		}
	}
	return names
}

func slashCommandNameEnd(data []byte, start int) int {
	end := start
	for end < len(data) && end-start <= maxSlashCommandNameLen && isSlashCommandNameByte(data[end]) {
		end++
	}
	return end
}

func longestKnownSlashCommandPrefix(name string, fallbackNames map[string]bool) string {
	best := ""
	for candidate := range fallbackNames {
		if strings.HasPrefix(name, candidate) && len(candidate) > len(best) {
			best = candidate
		}
	}
	if best != "" {
		return best
	}
	return name
}

func isSlashCommandClusterAnchor(name string) bool {
	switch name {
	case "setup-default-sandbox", "sandbox-add-read-dir", "debug-config", "statusline", "test-approval", "debug-m-drop", "debug-m-update":
		return true
	default:
		return false
	}
}

func fallbackSlashCommandNameSet() map[string]bool {
	result := make(map[string]bool, len(codexSlashCommandSpecs))
	for _, spec := range codexSlashCommandSpecs {
		result[spec.name] = true
	}
	return result
}

func isSlashCommandName(name string) bool {
	if len(name) < 2 || len(name) > maxSlashCommandNameLen || !isSlashCommandNameStart(name[0]) {
		return false
	}
	for i := 1; i < len(name); i++ {
		if !isSlashCommandNameByte(name[i]) {
			return false
		}
	}
	return true
}

func isSlashCommandNameBytes(data []byte) bool {
	if len(data) == 0 || len(data) > maxSlashCommandNameLen {
		return false
	}
	for _, value := range data {
		if !isSlashCommandNameByte(value) {
			return false
		}
	}
	return true
}

func isSlashCommandNameStart(value byte) bool {
	return value >= 'a' && value <= 'z'
}

func isSlashCommandNameByte(value byte) bool {
	return (value >= 'a' && value <= 'z') || (value >= '0' && value <= '9') || value == '-'
}

func slashCommandTitle(name string) string {
	parts := strings.Split(name, "-")
	for i, part := range parts {
		if part == "" {
			continue
		}
		switch strings.ToLower(part) {
		case "ide":
			parts[i] = "IDE"
		case "mcp":
			parts[i] = "MCP"
		default:
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, " ")
}

func codexVersion() string {
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return ""
	}
	out, err := exec.Command(codexPath, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func resolveCodexNativeBinary() (string, bool) {
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return "", false
	}
	if isExecutableFile(codexPath) && !strings.HasSuffix(codexPath, ".js") {
		if !looksLikeNodeWrapper(codexPath) {
			return codexPath, true
		}
	}

	realPath, err := filepath.EvalSymlinks(codexPath)
	if err == nil && realPath != codexPath {
		if isExecutableFile(realPath) && !strings.HasSuffix(realPath, ".js") && !looksLikeNodeWrapper(realPath) {
			return realPath, true
		}
		codexPath = realPath
	}

	root := codexPackageRoot(codexPath)
	if root == "" {
		return "", false
	}
	targetTriple, packageName, ok := codexPlatformPackage()
	if !ok {
		return "", false
	}
	binaryName := "codex"
	if runtime.GOOS == "windows" {
		binaryName = "codex.exe"
	}
	candidates := []string{
		filepath.Join(root, "node_modules", packageName, "vendor", targetTriple, "codex", binaryName),
		filepath.Join(root, "vendor", targetTriple, "codex", binaryName),
	}
	for _, candidate := range candidates {
		if isExecutableFile(candidate) {
			return candidate, true
		}
	}
	return "", false
}

func codexPackageRoot(path string) string {
	clean := filepath.Clean(path)
	if filepath.Base(clean) == "codex.js" && filepath.Base(filepath.Dir(clean)) == "bin" {
		return filepath.Dir(filepath.Dir(clean))
	}
	dir := filepath.Dir(clean)
	for i := 0; i < 8 && dir != "." && dir != string(filepath.Separator); i++ {
		if filepath.Base(dir) == "@openai" && filepath.Base(filepath.Dir(dir)) == "node_modules" {
			candidate := filepath.Join(dir, "codex")
			if stat, err := os.Stat(candidate); err == nil && stat.IsDir() {
				return candidate
			}
		}
		if filepath.Base(dir) == "codex" && filepath.Base(filepath.Dir(dir)) == "@openai" {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
		dir = next
	}
	return ""
}

func codexPlatformPackage() (string, string, bool) {
	switch runtime.GOOS {
	case "linux", "android":
		switch runtime.GOARCH {
		case "amd64":
			return "x86_64-unknown-linux-musl", "@openai/codex-linux-x64", true
		case "arm64":
			return "aarch64-unknown-linux-musl", "@openai/codex-linux-arm64", true
		}
	case "darwin":
		switch runtime.GOARCH {
		case "amd64":
			return "x86_64-apple-darwin", "@openai/codex-darwin-x64", true
		case "arm64":
			return "aarch64-apple-darwin", "@openai/codex-darwin-arm64", true
		}
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			return "x86_64-pc-windows-msvc", "@openai/codex-win32-x64", true
		case "arm64":
			return "aarch64-pc-windows-msvc", "@openai/codex-win32-arm64", true
		}
	}
	return "", "", false
}

func looksLikeNodeWrapper(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	data := make([]byte, 4096)
	n, err := file.Read(data)
	if err != nil && n == 0 {
		return false
	}
	data = data[:n]
	return bytes.HasPrefix(data, []byte("#!/usr/bin/env node")) || bytes.Contains(data, []byte("node:child_process"))
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}
