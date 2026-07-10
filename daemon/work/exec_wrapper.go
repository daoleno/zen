package work

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// nestedCodexToolCall is one tools.* invocation recovered from a Codex
// functions.exec / custom_tool_call "exec" wrapper script.
// The parser never evaluates JavaScript.
type nestedCodexToolCall struct {
	Name    string
	RawArgs string
	Object  map[string]json.RawMessage
	Text    string
}

var (
	codexExecConstStrRe = regexp.MustCompile(`(?m)(?:const|let|var)\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*("(?:\\.|[^"\\])*")`)
)

func isCodexExecWrapperTool(name string) bool {
	normalized := strings.TrimSpace(name)
	normalized = strings.TrimPrefix(normalized, "functions.")
	return normalized == "exec"
}

func parseCodexExecWrapper(input string) []nestedCodexToolCall {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	input = strings.ReplaceAll(input, "\r", "\n")
	if strings.TrimSpace(input) == "" {
		return nil
	}

	assignments := map[string]string{}
	for _, match := range codexExecConstStrRe.FindAllStringSubmatch(input, -1) {
		if len(match) < 3 {
			continue
		}
		name := match[1]
		if decoded, ok := decodeJSStringLiteral(match[2]); ok {
			assignments[name] = decoded
		}
	}

	var calls []nestedCodexToolCall
	for _, site := range findCodexExecToolCallSites(input) {
		args, ok := extractBalancedArgs(input, site.openParen)
		if !ok {
			continue
		}
		call := nestedCodexToolCall{
			Name:    site.name,
			RawArgs: strings.TrimSpace(args),
		}
		call.Object, call.Text = parseExecWrapperArgs(call.RawArgs, assignments)
		calls = append(calls, call)
	}
	return calls
}

type codexExecToolCallSite struct {
	name      string
	openParen int
}

// findCodexExecToolCallSites walks executable code only, skipping string bodies
// and line/block comments so tool-like text inside commands cannot fake calls.
func findCodexExecToolCallSites(source string) []codexExecToolCallSite {
	var sites []codexExecToolCallSite
	n := len(source)
	i := 0
	for i < n {
		ch := source[i]
		switch {
		case ch == '"' || ch == '\'':
			i = skipJSQuotedString(source, i)
		case ch == '`':
			i = skipJSTemplateLiteral(source, i)
		case ch == '/' && i+1 < n && source[i+1] == '/':
			i = skipJSLineComment(source, i)
		case ch == '/' && i+1 < n && source[i+1] == '*':
			i = skipJSBlockComment(source, i)
		case hasJSIdentPrefix(source, i, "tools") &&
			!isJSIdentPartAt(source, i-1) &&
			i+5 < n && source[i+5] == '.':
			nameStart := i + 6
			nameEnd := nameStart
			for nameEnd < n && isIdentPart(source[nameEnd]) {
				nameEnd++
			}
			if nameEnd > nameStart {
				j := nameEnd
				for j < n && (source[j] == ' ' || source[j] == '\t' || source[j] == '\n' || source[j] == '\r') {
					j++
				}
				if j < n && source[j] == '(' {
					sites = append(sites, codexExecToolCallSite{
						name:      source[nameStart:nameEnd],
						openParen: j,
					})
					i = j
					continue
				}
			}
			i++
		default:
			i++
		}
	}
	return sites
}

func skipJSQuotedString(source string, start int) int {
	if start >= len(source) {
		return start
	}
	quote := source[start]
	escaped := false
	for i := start + 1; i < len(source); i++ {
		ch := source[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == quote {
			return i + 1
		}
		if ch == '\n' && quote != '`' {
			return i
		}
	}
	return len(source)
}

func skipJSTemplateLiteral(source string, start int) int {
	if start >= len(source) || source[start] != '`' {
		return start
	}
	escaped := false
	for i := start + 1; i < len(source); {
		ch := source[i]
		if escaped {
			escaped = false
			i++
			continue
		}
		if ch == '\\' {
			escaped = true
			i++
			continue
		}
		if ch == '`' {
			return i + 1
		}
		if ch == '$' && i+1 < len(source) && source[i+1] == '{' {
			i = skipJSTemplateExpression(source, i+2)
			continue
		}
		i++
	}
	return len(source)
}

func skipJSTemplateExpression(source string, start int) int {
	depth := 1
	i := start
	for i < len(source) && depth > 0 {
		ch := source[i]
		switch {
		case ch == '"' || ch == '\'':
			i = skipJSQuotedString(source, i)
		case ch == '`':
			i = skipJSTemplateLiteral(source, i)
		case ch == '/' && i+1 < len(source) && source[i+1] == '/':
			i = skipJSLineComment(source, i)
		case ch == '/' && i+1 < len(source) && source[i+1] == '*':
			i = skipJSBlockComment(source, i)
		case ch == '{':
			depth++
			i++
		case ch == '}':
			depth--
			i++
		default:
			i++
		}
	}
	return i
}

func skipJSLineComment(source string, start int) int {
	i := start + 2
	for i < len(source) && source[i] != '\n' {
		i++
	}
	if i < len(source) {
		return i + 1
	}
	return i
}

func skipJSBlockComment(source string, start int) int {
	i := start + 2
	for i+1 < len(source) {
		if source[i] == '*' && source[i+1] == '/' {
			return i + 2
		}
		i++
	}
	return len(source)
}

func hasJSIdentPrefix(source string, index int, ident string) bool {
	if index < 0 || index+len(ident) > len(source) {
		return false
	}
	if source[index:index+len(ident)] != ident {
		return false
	}
	return !isJSIdentPartAt(source, index+len(ident))
}

func isJSIdentPartAt(source string, index int) bool {
	if index < 0 || index >= len(source) {
		return false
	}
	return isIdentPart(source[index])
}

func parseExecWrapperArgs(raw string, assignments map[string]string) (map[string]json.RawMessage, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ""
	}
	if strings.HasPrefix(raw, "\"") || strings.HasPrefix(raw, "'") || strings.HasPrefix(raw, "`") {
		if decoded, ok := decodeJSStringLiteral(raw); ok {
			return nil, decoded
		}
		return nil, ""
	}
	if ident := strings.TrimSpace(raw); isSimpleJSIdent(ident) {
		if value, ok := assignments[ident]; ok {
			return nil, value
		}
		return nil, ""
	}
	if strings.HasPrefix(raw, "{") {
		normalized := normalizeJSObjectLiteral(raw)
		var object map[string]json.RawMessage
		if json.Unmarshal([]byte(normalized), &object) == nil {
			return object, ""
		}
		// Fall back to extracting a few known string fields without eval.
		return extractLooseObjectFields(raw), ""
	}
	return nil, ""
}

func extractBalancedArgs(source string, openParen int) (string, bool) {
	if openParen < 0 || openParen >= len(source) || source[openParen] != '(' {
		return "", false
	}
	depth := 0
	inString := byte(0)
	escaped := false
	for index := openParen; index < len(source); index++ {
		ch := source[index]
		if inString != 0 {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' && inString != '`' {
				escaped = true
				continue
			}
			if ch == inString {
				inString = 0
			}
			continue
		}
		switch ch {
		case '"', '\'', '`':
			inString = ch
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return source[openParen+1 : index], true
			}
		}
	}
	return "", false
}

func decodeJSStringLiteral(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if len(value) < 2 {
		return "", false
	}
	quote := value[0]
	if (quote != '"' && quote != '\'' && quote != '`') || value[len(value)-1] != quote {
		return "", false
	}
	inner := value[1 : len(value)-1]
	var b strings.Builder
	b.Grow(len(inner))
	escaped := false
	for index := 0; index < len(inner); {
		ch := inner[index]
		if escaped {
			switch ch {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case '\\', '"', '\'', '`':
				b.WriteByte(ch)
			case 'u':
				if index+4 < len(inner) {
					hex := inner[index+1 : index+5]
					if r, err := parseHexRune(hex); err == nil {
						b.WriteRune(r)
						index += 5
						escaped = false
						continue
					}
				}
				b.WriteByte('u')
			default:
				b.WriteByte(ch)
			}
			escaped = false
			index++
			continue
		}
		if ch == '\\' {
			escaped = true
			index++
			continue
		}
		b.WriteByte(ch)
		index++
	}
	if escaped {
		return "", false
	}
	return b.String(), true
}

func parseHexRune(value string) (rune, error) {
	var n int
	for i := 0; i < len(value); i++ {
		ch := value[i]
		n <<= 4
		switch {
		case ch >= '0' && ch <= '9':
			n |= int(ch - '0')
		case ch >= 'a' && ch <= 'f':
			n |= int(ch-'a') + 10
		case ch >= 'A' && ch <= 'F':
			n |= int(ch-'A') + 10
		default:
			return 0, errInvalidHex
		}
	}
	if n > utf8.MaxRune {
		return 0, errInvalidHex
	}
	return rune(n), nil
}

var errInvalidHex = errors.New("invalid hex")

func isSimpleJSIdent(value string) bool {
	if value == "" {
		return false
	}
	for index, r := range value {
		if index == 0 {
			if !(unicode.IsLetter(r) || r == '_' || r == '$') {
				return false
			}
			continue
		}
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$') {
			return false
		}
	}
	return true
}

func normalizeJSObjectLiteral(value string) string {
	var b strings.Builder
	b.Grow(len(value) + 16)
	inString := byte(0)
	escaped := false
	for index := 0; index < len(value); {
		ch := value[index]
		if inString != 0 {
			b.WriteByte(ch)
			if escaped {
				escaped = false
				index++
				continue
			}
			if ch == '\\' {
				escaped = true
				index++
				continue
			}
			if ch == inString {
				inString = 0
			}
			index++
			continue
		}
		switch ch {
		case '"', '\'':
			inString = ch
			b.WriteByte(ch)
			index++
		case '`':
			// Keep template literals opaque; JSON parse will fail and loose extraction may help.
			inString = ch
			b.WriteByte(ch)
			index++
		default:
			if isIdentStart(ch) {
				start := index
				index++
				for index < len(value) && isIdentPart(value[index]) {
					index++
				}
				ident := value[start:index]
				rest := strings.TrimLeft(value[index:], " \t\n\r")
				if strings.HasPrefix(rest, ":") && !isJSKeyword(ident) {
					b.WriteByte('"')
					b.WriteString(ident)
					b.WriteByte('"')
					continue
				}
				b.WriteString(ident)
				continue
			}
			b.WriteByte(ch)
			index++
		}
	}
	return b.String()
}

func extractLooseObjectFields(raw string) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	re := regexp.MustCompile(`(?s)(?:"([^"]+)"|([A-Za-z_][A-Za-z0-9_]*))\s*:\s*("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*')`)
	for _, match := range re.FindAllStringSubmatch(raw, -1) {
		key := match[1]
		if key == "" {
			key = match[2]
		}
		if key == "" {
			continue
		}
		if decoded, ok := decodeJSStringLiteral(match[3]); ok {
			encoded, err := json.Marshal(decoded)
			if err == nil {
				out[key] = encoded
			}
		}
	}
	return out
}

func isIdentStart(ch byte) bool {
	return ch == '_' || ch == '$' || (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z')
}

func isIdentPart(ch byte) bool {
	return isIdentStart(ch) || (ch >= '0' && ch <= '9')
}

func isJSKeyword(value string) bool {
	switch value {
	case "true", "false", "null", "undefined", "new", "await", "async", "function", "return", "const", "let", "var":
		return true
	default:
		return false
	}
}

func nestedCallCommand(call nestedCodexToolCall) string {
	if call.Object != nil {
		if cmd := strings.TrimSpace(jsonString(call.Object["cmd"])); cmd != "" {
			return cmd
		}
		if command := jsonString(call.Object["command"]); command != "" {
			return command
		}
	}
	if strings.TrimSpace(call.RawArgs) == "" {
		return ""
	}
	return codexExecCommand(call.RawArgs)
}

func nestedCallPatchText(call nestedCodexToolCall) string {
	if text := strings.TrimSpace(call.Text); text != "" {
		return text
	}
	if call.Object != nil {
		if patch := strings.TrimSpace(jsonString(call.Object["patch"])); patch != "" {
			return patch
		}
		if input := strings.TrimSpace(jsonString(call.Object["input"])); input != "" {
			return input
		}
	}
	return ""
}

func nestedCallPlan(call nestedCodexToolCall) (string, []CodexPlanStep) {
	if call.Object == nil {
		return codexPlanToolArguments(call.RawArgs)
	}
	explanation := strings.TrimSpace(jsonString(call.Object["explanation"]))
	rawPlan := call.Object["plan"]
	if len(rawPlan) == 0 {
		return explanation, nil
	}
	var steps []CodexPlanStep
	if json.Unmarshal(rawPlan, &steps) != nil {
		// JS object literals may leave plan as a non-JSON array; recover step/status pairs.
		steps = extractLoosePlanSteps(string(rawPlan))
	}
	for index := range steps {
		steps[index].Step = cleanConversationText(steps[index].Step)
		steps[index].Status = normalizePlanStepStatus(steps[index].Status)
	}
	return explanation, steps
}

func extractLoosePlanSteps(raw string) []CodexPlanStep {
	re := regexp.MustCompile(`(?s)\{\s*(?:step|"step")\s*:\s*("(?:\\.|[^"\\])*")\s*,\s*(?:status|"status")\s*:\s*("(?:\\.|[^"\\])*")\s*\}`)
	var steps []CodexPlanStep
	for _, match := range re.FindAllStringSubmatch(raw, -1) {
		step, okStep := decodeJSStringLiteral(match[1])
		status, okStatus := decodeJSStringLiteral(match[2])
		if !okStep || strings.TrimSpace(step) == "" {
			continue
		}
		if !okStatus {
			status = "pending"
		}
		steps = append(steps, CodexPlanStep{
			Step:   step,
			Status: normalizePlanStepStatus(status),
		})
	}
	return steps
}

func nestedCallViewPath(call nestedCodexToolCall) string {
	if call.Object != nil {
		if path := strings.TrimSpace(jsonString(call.Object["path"])); path != "" {
			return path
		}
		if url := strings.TrimSpace(jsonString(call.Object["image_url"])); url != "" {
			return url
		}
	}
	return ""
}
