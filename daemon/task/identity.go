package task

import (
	"fmt"
	"strings"
)

const DefaultIdentifierPrefix = "ZEN"

var projectKeyIgnoredTrailingTokens = map[string]struct{}{
	"API":      {},
	"APP":      {},
	"BACKEND":  {},
	"CLI":      {},
	"CLIENT":   {},
	"DAEMON":   {},
	"DESKTOP":  {},
	"FRONTEND": {},
	"MOBILE":   {},
	"SERVER":   {},
	"SERVICE":  {},
	"SERVICES": {},
	"WEB":      {},
}

func NormalizeIdentifierPrefix(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(value)) {
		isASCIIAlphaNum := (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !isASCIIAlphaNum {
			continue
		}
		builder.WriteRune(r)
	}

	normalized := builder.String()
	if normalized == "" {
		return DefaultIdentifierPrefix
	}
	return normalized
}

func FormatDisplayID(prefix string, number int) string {
	return fmt.Sprintf("%s-%d", NormalizeIdentifierPrefix(prefix), number)
}

func DisplayID(currentTask *Task) string {
	if currentTask == nil {
		return ""
	}
	return FormatDisplayID(currentTask.IdentifierPrefix, currentTask.Number)
}

func DeriveProjectKey(name string, isTaken func(string) bool) string {
	base := deriveProjectKeyBase(name)
	if base == "" {
		base = DefaultIdentifierPrefix
	}
	if isTaken == nil || !isTaken(base) {
		return base
	}

	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("%s%d", base, suffix)
		if !isTaken(candidate) {
			return candidate
		}
	}
}

func deriveProjectKeyBase(name string) string {
	tokens := projectKeyTokens(name)
	if len(tokens) == 0 {
		return DefaultIdentifierPrefix
	}

	trimmed := trimProjectKeyTokens(tokens)
	if len(trimmed) > 0 {
		tokens = trimmed
	}

	first := tokens[0]
	if len(first) >= 3 {
		return first[:3]
	}

	var builder strings.Builder
	builder.WriteString(first)
	for i := 1; i < len(tokens) && builder.Len() < 3; i++ {
		builder.WriteByte(tokens[i][0])
	}

	candidate := builder.String()
	if candidate != "" {
		return candidate
	}

	return DefaultIdentifierPrefix
}

func projectKeyTokens(name string) []string {
	var tokens []string
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		tokens = append(tokens, current.String())
		current.Reset()
	}

	for _, r := range strings.ToUpper(strings.TrimSpace(name)) {
		isASCIIAlphaNum := (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if isASCIIAlphaNum {
			current.WriteRune(r)
			continue
		}
		flush()
	}
	flush()

	return tokens
}

func trimProjectKeyTokens(tokens []string) []string {
	if len(tokens) <= 1 {
		return tokens
	}

	end := len(tokens)
	for end > 1 {
		if _, ok := projectKeyIgnoredTrailingTokens[tokens[end-1]]; !ok {
			break
		}
		end--
	}
	return tokens[:end]
}
