package issue

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	frontmatterDelim = []byte("---")
	mentionRe        = regexp.MustCompile(`(?m)(?:^|\s)@([a-z][a-z0-9-]*)(?:#([a-z0-9-]+))?\b`)
)

// ParseFile parses a Markdown file's bytes into an Issue.
func ParseFile(path string, data []byte, mtime time.Time) (*Issue, error) {
	fm, body, err := splitFrontmatter(data)
	if err != nil {
		return nil, err
	}

	parsed, extra, err := decodeFrontmatter(fm)
	if err != nil {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}
	parsed.Extra = extra

	project := projectFromPath(path)
	title := extractTitle(body)
	mentions := ExtractMentions(body)

	return &Issue{
		ID:          parsed.ID,
		Path:        path,
		Project:     project,
		Title:       title,
		Body:        body,
		Frontmatter: parsed,
		Mentions:    mentions,
		Mtime:       mtime,
	}, nil
}

func splitFrontmatter(data []byte) (fm, body string, err error) {
	data = bytes.TrimLeft(data, "\ufeff")
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))

	if !bytes.HasPrefix(data, append(frontmatterDelim, '\n')) {
		return "", "", fmt.Errorf("missing --- frontmatter delimiter")
	}

	rest := data[len(frontmatterDelim)+1:]
	end := bytes.Index(rest, append([]byte("\n"), frontmatterDelim...))
	if end < 0 {
		return "", "", fmt.Errorf("missing closing frontmatter delimiter")
	}

	fm = string(rest[:end])
	bodyStart := end + 1 + len(frontmatterDelim)
	if bodyStart >= len(rest) {
		return fm, "", nil
	}
	if rest[bodyStart] == '\n' {
		bodyStart++
	}
	return fm, string(rest[bodyStart:]), nil
}

func decodeFrontmatter(fm string) (Frontmatter, map[string]interface{}, error) {
	var typed Frontmatter
	if err := yaml.Unmarshal([]byte(fm), &typed); err != nil {
		return Frontmatter{}, nil, err
	}

	raw := map[string]interface{}{}
	if err := yaml.Unmarshal([]byte(fm), &raw); err != nil {
		return Frontmatter{}, nil, err
	}

	extra := map[string]interface{}{}
	known := map[string]struct{}{
		"id":            {},
		"created":       {},
		"done":          {},
		"dispatched":    {},
		"agent_session": {},
	}
	for key, value := range raw {
		if _, ok := known[key]; ok {
			continue
		}
		extra[key] = value
	}
	if len(extra) == 0 {
		extra = nil
	}

	return typed, extra, nil
}

func projectFromPath(path string) string {
	return filepath.Base(filepath.Dir(path))
}

func extractTitle(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(trimmed[2:])
		}
		return trimmed
	}
	return ""
}

// ExtractMentions returns all @role / @role#session mentions in document order.
// Indexes point to the '@' character in the body.
func ExtractMentions(body string) []Mention {
	matches := mentionRe.FindAllStringSubmatchIndex(body, -1)
	out := make([]Mention, 0, len(matches))
	for _, match := range matches {
		role := body[match[2]:match[3]]
		session := ""
		if len(match) >= 6 && match[4] >= 0 {
			session = body[match[4]:match[5]]
		}
		out = append(out, Mention{
			Role:    role,
			Session: session,
			Index:   match[2] - 1,
		})
	}
	return out
}

// SerializeIssue renders an Issue back to Markdown bytes suitable for atomic write.
// Unknown frontmatter fields from iss.Frontmatter.Extra are preserved.
func SerializeIssue(iss *Issue) ([]byte, error) {
	out := map[string]interface{}{}
	out["id"] = iss.Frontmatter.ID
	out["created"] = iss.Frontmatter.Created.Format(time.RFC3339)
	if iss.Frontmatter.Done != nil {
		out["done"] = iss.Frontmatter.Done.Format(time.RFC3339)
	} else {
		out["done"] = nil
	}
	if iss.Frontmatter.Dispatched != nil {
		out["dispatched"] = iss.Frontmatter.Dispatched.Format(time.RFC3339)
	}
	if iss.Frontmatter.AgentSession != "" {
		out["agent_session"] = iss.Frontmatter.AgentSession
	}
	for key, value := range iss.Frontmatter.Extra {
		out[key] = value
	}

	buf := &bytes.Buffer{}
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(buf)
	enc.SetIndent(2)
	if err := enc.Encode(out); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	buf.WriteString("---\n")
	buf.WriteString(iss.Body)
	if !strings.HasSuffix(iss.Body, "\n") {
		buf.WriteString("\n")
	}
	return buf.Bytes(), nil
}
