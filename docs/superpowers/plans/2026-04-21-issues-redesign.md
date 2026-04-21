# Issues Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current Linear-style task tracker with a file-first, Markdown-per-issue system rooted at `~/.zen/issues/<project>/*.md`. Spec: `docs/issues-redesign.md`.

**Architecture:** Daemon owns `~/.zen/issues/` via fsnotify; an `issue/` package parses, stores, and dispatches. Server exposes new WebSocket messages (`list_issues`, `write_issue`, `send_issue`, `redispatch_issue`, `delete_issue`, `list_executors`) and broadcasts `issue_changed` / `issue_deleted` / `issues_snapshot`. App uses a new `store/issues.tsx` Context+reducer, rewrites the list and detail screens, and adds a Markdown editor with `@role` mention picker. Old `daemon/task/` package, old WebSocket handlers, `app/store/tasks.tsx`, and all current `app/components/issue/*` files are removed at the end.

**Tech Stack:** Go 1.22+, `github.com/fsnotify/fsnotify` (already in go.mod), `github.com/BurntSushi/toml` (new), `gopkg.in/yaml.v3` (new), ULID via `github.com/oklog/ulid/v2` (new). React Native + Expo + TypeScript, React Context + `useReducer`.

---

## File Structure

### Daemon — new files

- `daemon/issue/types.go` — `Issue`, `Frontmatter`, `Mention`, `Project`, `Executor`, `AgentState` helpers.
- `daemon/issue/parser.go` — frontmatter + body + mention parsing and serialization.
- `daemon/issue/parser_test.go`
- `daemon/issue/project.go` — `project.toml` loader.
- `daemon/issue/project_test.go`
- `daemon/issue/executors.go` — `~/.zen/executors.toml` loader with built-in defaults.
- `daemon/issue/executors_test.go`
- `daemon/issue/store.go` — directory scan, fsnotify watcher with 200ms debounce, atomic writes, mtime conflict, event channel.
- `daemon/issue/store_test.go`
- `daemon/issue/dispatch.go` — pick idle session / spawn new, write initial prompt, stamp `dispatched` + `agent_session`.
- `daemon/issue/dispatch_test.go`
- `daemon/issue/paths.go` — resolve `~/.zen/issues` and helpers.

### Daemon — modified files

- `daemon/cmd/zen-daemon/main.go` — initialize `issue.Store` + `issue.Dispatcher`, pass to `server.New`, drop task store wiring.
- `daemon/server/server.go` — remove old task/run/comment message handlers and broadcasts; add new issue handlers; change `server.New` signature.
- `daemon/go.mod` / `daemon/go.sum` — add new deps.

### Daemon — deleted files

- `daemon/task/` entire directory.

### App — new files

- `app/store/issues.tsx` — Context + reducer for issues state.
- `app/components/issue/MarkdownEditor.tsx`
- `app/components/issue/MentionPicker.tsx`
- `app/components/issue/IssueRow.tsx` (new, replaces the deleted one)
- `app/components/issue/__tests__/MentionPicker.test.tsx`
- `app/components/issue/__tests__/MarkdownEditor.test.tsx`
- `app/store/__tests__/issues.test.ts`

### App — rewritten files

- `app/app/(tabs)/issues.tsx` — list grouped by project, two sections per project (Active / Done), top-right "+".
- `app/app/issue/[id].tsx` — markdown editor + mention picker + Send.
- `app/app/_layout.tsx` — register new WebSocket listeners, remove old task listeners.

### App — deleted files

- `app/store/tasks.tsx`
- `app/components/issue/AssignIssueSheet.tsx`
- `app/components/issue/AttachmentStack.tsx`
- `app/components/issue/CreateIssueSheet.tsx`
- `app/components/issue/DelegateRunSheet.tsx`
- `app/components/issue/DueDatePicker.tsx`
- `app/components/issue/IssueRow.tsx` (old; replaced by new one above)
- `app/components/issue/IssueStatusIcon.tsx`
- `app/components/issue/PriorityBar.tsx`
- `app/components/issue/PriorityPicker.tsx`
- `app/components/issue/ProjectEditorSheet.tsx`
- `app/components/issue/ProjectRow.tsx`
- `app/components/issue/StatusFilterBar.tsx`
- `app/components/issue/StatusPicker.tsx`
- `app/components/issue/TaskPickerSheet.tsx`

### Commit Rhythm

- Phase 1 (daemon additions): commit per task. Daemon still has old task package present — it compiles alongside new code until Phase 3.
- Phase 2 (app rewrite): commit per task. App may temporarily have both old and new listeners registered; final commit in Phase 2 removes old listener registrations.
- Phase 3 (daemon cleanup): commit per task. Final commit deletes `daemon/task/`.
- End-to-end verification gate before declaring done.

---

## Phase 1 — Daemon: New issue package

### Task 1: Add Go dependencies and bootstrap package

**Files:**
- Modify: `daemon/go.mod`
- Create: `daemon/issue/doc.go`

- [ ] **Step 1: Add dependencies**

Run:
```bash
cd /home/daoleno/workspace/zen/daemon
go get github.com/BurntSushi/toml@latest
go get gopkg.in/yaml.v3@latest
go get github.com/oklog/ulid/v2@latest
go mod tidy
```

Expected: `go.mod` updated with three new require lines. `go.sum` populated.

- [ ] **Step 2: Create package doc file**

Create `daemon/issue/doc.go`:

```go
// Package issue implements a file-first issue system rooted at ~/.zen/issues/<project>/*.md.
//
// Issues are Markdown files with minimal YAML frontmatter (id, created, done).
// The daemon watches the issues root via fsnotify, broadcasts changes over
// WebSocket, and dispatches tmux-backed agents to edit the files directly.
package issue
```

- [ ] **Step 3: Verify build**

Run:
```bash
cd /home/daoleno/workspace/zen/daemon && go build ./...
```

Expected: success, no output.

- [ ] **Step 4: Commit**

```bash
cd /home/daoleno/workspace/zen/daemon
git add go.mod go.sum issue/doc.go
git commit -m "Scaffold daemon/issue package and add deps"
```

---

### Task 2: Core types

**Files:**
- Create: `daemon/issue/types.go`

- [ ] **Step 1: Write the types**

```go
package issue

import "time"

// Issue is the canonical in-memory representation of one Markdown file.
type Issue struct {
	ID          string    `json:"id"`
	Path        string    `json:"path"`        // absolute path on disk
	Project     string    `json:"project"`     // directory name under ~/.zen/issues
	Title       string    `json:"title"`       // first "# heading" or first non-empty line
	Body        string    `json:"body"`        // raw Markdown body (after frontmatter)
	Frontmatter Frontmatter `json:"frontmatter"`
	Mentions    []Mention `json:"mentions"`
	Mtime       time.Time `json:"mtime"`       // filesystem mtime at last read
}

// Frontmatter holds the structured fields we read/write in the YAML block.
// Unknown fields are preserved via Extra so agent-written metadata survives round-trips.
type Frontmatter struct {
	ID            string                 `yaml:"id" json:"id"`
	Created       time.Time              `yaml:"created" json:"created"`
	Done          *time.Time             `yaml:"done,omitempty" json:"done,omitempty"`
	Dispatched    *time.Time             `yaml:"dispatched,omitempty" json:"dispatched,omitempty"`
	AgentSession  string                 `yaml:"agent_session,omitempty" json:"agentSession,omitempty"`
	Extra         map[string]interface{} `yaml:"-" json:"extra,omitempty"`
}

// Mention is one @role or @role#session reference in the body.
type Mention struct {
	Role    string `json:"role"`
	Session string `json:"session,omitempty"` // empty unless @role#session form was used
	Index   int    `json:"index"`             // byte offset of the '@' in body
}

// Executor is one configured agent kind (claude, codex, ...).
type Executor struct {
	Name    string `json:"name" toml:"name"`
	Command string `json:"command" toml:"command"`
}

// Project holds the content of one project.toml.
type Project struct {
	Name     string `toml:"name" json:"name"`
	Cwd      string `toml:"cwd" json:"cwd"`
	Executor string `toml:"executor" json:"executor"`
}
```

- [ ] **Step 2: Verify build**

```bash
cd /home/daoleno/workspace/zen/daemon && go build ./issue/...
```

Expected: success.

- [ ] **Step 3: Commit**

```bash
git add daemon/issue/types.go
git commit -m "Define issue package types"
```

---

### Task 3: Frontmatter parser (TDD)

**Files:**
- Create: `daemon/issue/parser.go`
- Create: `daemon/issue/parser_test.go`

- [ ] **Step 1: Write failing tests for frontmatter parse**

```go
package issue

import (
	"strings"
	"testing"
	"time"
)

func TestParseFile_Minimal(t *testing.T) {
	src := `---
id: 01HZ5K8J9X
created: 2026-04-21T14:32:15+08:00
done:
---
# Hello

Body line.
`
	iss, err := ParseFile("/tmp/x.md", []byte(src), time.Now())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if iss.Frontmatter.ID != "01HZ5K8J9X" {
		t.Errorf("id: got %q", iss.Frontmatter.ID)
	}
	if iss.Frontmatter.Done != nil {
		t.Errorf("done should be nil, got %v", iss.Frontmatter.Done)
	}
	if iss.Title != "Hello" {
		t.Errorf("title: got %q", iss.Title)
	}
	if !strings.Contains(iss.Body, "Body line.") {
		t.Errorf("body missing content: %q", iss.Body)
	}
}

func TestParseFile_DoneTimestamp(t *testing.T) {
	src := `---
id: a
created: 2026-04-21T00:00:00Z
done: 2026-04-22T12:00:00Z
---
Body
`
	iss, err := ParseFile("/tmp/x.md", []byte(src), time.Now())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if iss.Frontmatter.Done == nil {
		t.Fatal("done should be set")
	}
	if !iss.Frontmatter.Done.Equal(time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("done: got %v", iss.Frontmatter.Done)
	}
}

func TestParseFile_DoneMissing_IsActive(t *testing.T) {
	src := `---
id: a
created: 2026-04-21T00:00:00Z
---
Body
`
	iss, err := ParseFile("/tmp/x.md", []byte(src), time.Now())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if iss.Frontmatter.Done != nil {
		t.Errorf("missing done should parse as nil")
	}
}

func TestParseFile_NoFrontmatter_Error(t *testing.T) {
	src := "just a body with no frontmatter"
	if _, err := ParseFile("/tmp/x.md", []byte(src), time.Now()); err == nil {
		t.Fatal("expected error for missing frontmatter")
	}
}

func TestParseFile_TitleFallback_FirstNonEmptyLine(t *testing.T) {
	src := `---
id: a
created: 2026-04-21T00:00:00Z
---

Plain text first line, no heading.
`
	iss, err := ParseFile("/tmp/x.md", []byte(src), time.Now())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if iss.Title != "Plain text first line, no heading." {
		t.Errorf("title fallback: got %q", iss.Title)
	}
}

func TestParseFile_ExtraFields_Preserved(t *testing.T) {
	src := `---
id: a
created: 2026-04-21T00:00:00Z
dispatched: 2026-04-21T01:00:00Z
agent_session: zen-claude-3
labels: [keep, me]
---
Body
`
	iss, err := ParseFile("/tmp/x.md", []byte(src), time.Now())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if iss.Frontmatter.Dispatched == nil {
		t.Fatal("dispatched should be set")
	}
	if iss.Frontmatter.AgentSession != "zen-claude-3" {
		t.Errorf("agent_session: got %q", iss.Frontmatter.AgentSession)
	}
	if _, ok := iss.Frontmatter.Extra["labels"]; !ok {
		t.Errorf("extra fields should preserve unknown keys, got %v", iss.Frontmatter.Extra)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

```bash
cd /home/daoleno/workspace/zen/daemon && go test ./issue/ -run TestParseFile -v
```

Expected: FAIL — `ParseFile` not defined.

- [ ] **Step 3: Implement `parser.go` (parse half only)**

```go
package issue

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var frontmatterDelim = []byte("---")

// ParseFile parses a Markdown file's bytes into an Issue.
// The path and mtime are taken from the caller (store reads them from disk).
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
	data = bytes.TrimLeft(data, "\ufeff") // strip BOM if present
	if !bytes.HasPrefix(data, frontmatterDelim) {
		return "", "", fmt.Errorf("missing --- frontmatter delimiter")
	}
	rest := data[len(frontmatterDelim):]
	// expect newline after opening ---
	nl := bytes.IndexByte(rest, '\n')
	if nl < 0 {
		return "", "", fmt.Errorf("unterminated opening frontmatter delimiter")
	}
	rest = rest[nl+1:]
	end := bytes.Index(rest, append([]byte("\n"), frontmatterDelim...))
	if end < 0 {
		return "", "", fmt.Errorf("missing closing frontmatter delimiter")
	}
	fm = string(rest[:end])
	// skip \n---
	bodyStart := end + 1 + len(frontmatterDelim)
	if bodyStart >= len(rest) {
		return fm, "", nil
	}
	// skip trailing newline after closing ---
	if rest[bodyStart] == '\n' {
		bodyStart++
	}
	return fm, string(rest[bodyStart:]), nil
}

func decodeFrontmatter(fm string) (Frontmatter, map[string]interface{}, error) {
	// First pass: typed decode for known fields.
	var typed Frontmatter
	if err := yaml.Unmarshal([]byte(fm), &typed); err != nil {
		return Frontmatter{}, nil, err
	}
	// Second pass: generic decode to capture unknown fields.
	raw := map[string]interface{}{}
	if err := yaml.Unmarshal([]byte(fm), &raw); err != nil {
		return Frontmatter{}, nil, err
	}
	extra := map[string]interface{}{}
	known := map[string]struct{}{
		"id": {}, "created": {}, "done": {}, "dispatched": {}, "agent_session": {},
	}
	for k, v := range raw {
		if _, ok := known[k]; ok {
			continue
		}
		extra[k] = v
	}
	if len(extra) == 0 {
		extra = nil
	}
	return typed, extra, nil
}

func projectFromPath(p string) string {
	// ~/.zen/issues/<project>/<file>.md → <project>
	dir := filepath.Dir(p)
	return filepath.Base(dir)
}

func extractTitle(body string) string {
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "# ") {
			return strings.TrimSpace(t[2:])
		}
		return t
	}
	return ""
}
```

**Important instruction to the implementer:** `ExtractMentions` is deliberately a stub in this task. Task 4 implements the real regex. **Do not implement the real logic here**, even if it seems trivial — keeping the boundary makes Task 4's TDD cycle meaningful. Paste exactly:

```go
// ExtractMentions is implemented in parser.go (mentions section) in Task 4.
// Intentional stub — do not implement here.
func ExtractMentions(body string) []Mention { return nil }
```

Place the stub at the bottom of `parser.go` so the package compiles. Task 4 replaces it.

- [ ] **Step 4: Run tests to verify pass**

```bash
cd /home/daoleno/workspace/zen/daemon && go test ./issue/ -run TestParseFile -v
```

Expected: all 6 subtests PASS.

- [ ] **Step 5: Commit**

```bash
git add daemon/issue/parser.go daemon/issue/parser_test.go
git commit -m "Parse issue frontmatter + title, preserve unknown fields"
```

---

### Task 4: Mention extractor (TDD)

**Files:**
- Modify: `daemon/issue/parser.go` (replace `ExtractMentions` stub)
- Modify: `daemon/issue/parser_test.go` (add tests)

- [ ] **Step 1: Write failing tests**

Append to `parser_test.go`:

```go
func TestExtractMentions_RoleOnly(t *testing.T) {
	body := "Hey @claude please look at this"
	got := ExtractMentions(body)
	if len(got) != 1 {
		t.Fatalf("want 1 mention, got %d: %+v", len(got), got)
	}
	if got[0].Role != "claude" || got[0].Session != "" {
		t.Errorf("unexpected: %+v", got[0])
	}
}

func TestExtractMentions_RoleAndSession(t *testing.T) {
	body := "Try @claude#zen-claude-3 for this one"
	got := ExtractMentions(body)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].Role != "claude" || got[0].Session != "zen-claude-3" {
		t.Errorf("unexpected: %+v", got[0])
	}
}

func TestExtractMentions_IgnoresEmail(t *testing.T) {
	body := "ping me at user@host.com, but actually @codex handle it"
	got := ExtractMentions(body)
	if len(got) != 1 {
		t.Fatalf("want 1 mention (codex only), got %d: %+v", len(got), got)
	}
	if got[0].Role != "codex" {
		t.Errorf("unexpected: %+v", got[0])
	}
}

func TestExtractMentions_MultiplePreservesOrder(t *testing.T) {
	body := "First @claude then @codex"
	got := ExtractMentions(body)
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	if got[0].Role != "claude" || got[1].Role != "codex" {
		t.Errorf("order: %+v", got)
	}
	if got[0].Index >= got[1].Index {
		t.Errorf("indices not increasing: %d, %d", got[0].Index, got[1].Index)
	}
}

func TestExtractMentions_StartOfLine(t *testing.T) {
	body := "@claude fix this"
	got := ExtractMentions(body)
	if len(got) != 1 || got[0].Role != "claude" {
		t.Fatalf("want single claude mention at start, got %+v", got)
	}
	if got[0].Index != 0 {
		t.Errorf("index: want 0, got %d", got[0].Index)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

```bash
cd /home/daoleno/workspace/zen/daemon && go test ./issue/ -run TestExtractMentions -v
```

Expected: FAIL (stub returns nil).

- [ ] **Step 3: Replace the stub with real implementation**

Edit `parser.go`, replace the `ExtractMentions` stub with:

```go
import "regexp"

// mentionRe matches @<role> or @<role>#<session>, requiring the '@' to be
// preceded by start-of-string or whitespace so emails like user@host do not match.
var mentionRe = regexp.MustCompile(`(?m)(?:^|\s)@([a-z][a-z0-9-]*)(?:#([a-z0-9-]+))?\b`)

// ExtractMentions returns all @role / @role#session mentions in document order.
// Indexes point to the '@' character in the body.
func ExtractMentions(body string) []Mention {
	matches := mentionRe.FindAllStringSubmatchIndex(body, -1)
	out := make([]Mention, 0, len(matches))
	for _, m := range matches {
		// m: [full_start, full_end, role_start, role_end, sess_start, sess_end]
		// Index of '@' is role_start - 1.
		role := body[m[2]:m[3]]
		var session string
		if m[4] >= 0 {
			session = body[m[4]:m[5]]
		}
		out = append(out, Mention{
			Role:    role,
			Session: session,
			Index:   m[2] - 1,
		})
	}
	return out
}
```

Add `"regexp"` to the imports block if not already present.

- [ ] **Step 4: Run tests to verify pass**

```bash
cd /home/daoleno/workspace/zen/daemon && go test ./issue/ -v
```

Expected: all tests (parse + extract) pass.

- [ ] **Step 5: Commit**

```bash
git add daemon/issue/parser.go daemon/issue/parser_test.go
git commit -m "Extract @role and @role#session mentions"
```

---

### Task 5: Issue serialization (write back) (TDD)

**Files:**
- Modify: `daemon/issue/parser.go`
- Modify: `daemon/issue/parser_test.go`

- [ ] **Step 1: Write failing tests for `SerializeIssue`**

Append to `parser_test.go`:

```go
func TestSerializeIssue_RoundTrip(t *testing.T) {
	src := `---
id: 01HZ5K8J9X
created: 2026-04-21T14:32:15Z
done: 2026-04-22T00:00:00Z
dispatched: 2026-04-21T15:00:00Z
agent_session: zen-claude-3
---
# Hello

Body line.
`
	iss, err := ParseFile("/tmp/x.md", []byte(src), time.Now())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := SerializeIssue(iss)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	reparsed, err := ParseFile("/tmp/x.md", out, time.Now())
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if reparsed.Frontmatter.ID != iss.Frontmatter.ID {
		t.Errorf("id lost")
	}
	if reparsed.Frontmatter.Done == nil || !reparsed.Frontmatter.Done.Equal(*iss.Frontmatter.Done) {
		t.Errorf("done lost")
	}
	if reparsed.Frontmatter.AgentSession != iss.Frontmatter.AgentSession {
		t.Errorf("agent_session lost")
	}
	if reparsed.Body != iss.Body {
		t.Errorf("body lost:\nwant: %q\ngot:  %q", iss.Body, reparsed.Body)
	}
}

func TestSerializeIssue_EmitsEmptyDoneField(t *testing.T) {
	// When done is nil, serialized YAML should still contain `done:` (empty)
	// so the author knows the field exists. This is optional polish; if we
	// choose to omit entirely that's also fine. Either way, re-parse yields nil.
	iss := &Issue{
		Frontmatter: Frontmatter{
			ID:      "a",
			Created: time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC),
		},
		Body: "body",
	}
	out, err := SerializeIssue(iss)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	reparsed, err := ParseFile("/tmp/x.md", out, time.Now())
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if reparsed.Frontmatter.Done != nil {
		t.Errorf("done should be nil after round-trip")
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

```bash
cd /home/daoleno/workspace/zen/daemon && go test ./issue/ -run TestSerializeIssue -v
```

Expected: FAIL — `SerializeIssue` not defined.

- [ ] **Step 3: Implement `SerializeIssue`**

Append to `parser.go`:

```go
// SerializeIssue renders an Issue back to Markdown bytes suitable for atomic write.
// Preserves unknown frontmatter keys from iss.Frontmatter.Extra.
func SerializeIssue(iss *Issue) ([]byte, error) {
	// Build a map to control key order and include Extra.
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
	for k, v := range iss.Frontmatter.Extra {
		out[k] = v
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
	// Ensure exactly one blank line isn't added; use body as-is.
	buf.WriteString(iss.Body)
	if !strings.HasSuffix(iss.Body, "\n") {
		buf.WriteString("\n")
	}
	return buf.Bytes(), nil
}
```

- [ ] **Step 4: Run tests to verify pass**

```bash
cd /home/daoleno/workspace/zen/daemon && go test ./issue/ -v
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add daemon/issue/parser.go daemon/issue/parser_test.go
git commit -m "Serialize issue back to Markdown, preserving unknown fields"
```

---

### Task 6: Project.toml loader (TDD)

**Files:**
- Create: `daemon/issue/project.go`
- Create: `daemon/issue/project_test.go`

- [ ] **Step 1: Write failing tests**

```go
package issue

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProject_Explicit(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "project.toml"), []byte(`
name = "zen"
cwd = "/home/x/code/zen"
executor = "codex"
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	p, err := LoadProject(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if p.Name != "zen" || p.Cwd != "/home/x/code/zen" || p.Executor != "codex" {
		t.Errorf("got %+v", p)
	}
}

func TestLoadProject_MissingFile_Inbox(t *testing.T) {
	dir := t.TempDir()
	// simulate "inbox" directory (no project.toml)
	os.Mkdir(filepath.Join(dir, "inbox"), 0o700)
	p, err := LoadProject(filepath.Join(dir, "inbox"))
	if err != nil {
		t.Fatalf("inbox should not error: %v", err)
	}
	if p.Name != "inbox" {
		t.Errorf("default name should be dir basename, got %q", p.Name)
	}
	if p.Cwd != "" {
		t.Errorf("cwd should default empty for inbox, got %q", p.Cwd)
	}
}

func TestLoadProject_MalformedTOML_Error(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "project.toml"), []byte("this is not = toml ]]]"), 0o600)
	if _, err := LoadProject(dir); err == nil {
		t.Fatal("expected error for malformed toml")
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

```bash
cd /home/daoleno/workspace/zen/daemon && go test ./issue/ -run TestLoadProject -v
```

Expected: FAIL — `LoadProject` not defined.

- [ ] **Step 3: Implement `project.go`**

```go
package issue

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// LoadProject reads <projectDir>/project.toml and returns the Project.
// If the file is absent, returns a default Project with Name = basename(projectDir).
func LoadProject(projectDir string) (Project, error) {
	p := Project{Name: filepath.Base(projectDir)}
	cfg := filepath.Join(projectDir, "project.toml")
	raw, err := os.ReadFile(cfg)
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return Project{}, err
	}
	if err := toml.Unmarshal(raw, &p); err != nil {
		return Project{}, err
	}
	if p.Name == "" {
		p.Name = filepath.Base(projectDir)
	}
	return p, nil
}
```

- [ ] **Step 4: Run tests to verify pass**

```bash
cd /home/daoleno/workspace/zen/daemon && go test ./issue/ -run TestLoadProject -v
```

Expected: 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add daemon/issue/project.go daemon/issue/project_test.go
git commit -m "Load project.toml with basename fallback"
```

---

### Task 7: Executor config (TDD)

**Files:**
- Create: `daemon/issue/executors.go`
- Create: `daemon/issue/executors_test.go`

- [ ] **Step 1: Write failing tests**

```go
package issue

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadExecutors_Defaults(t *testing.T) {
	cfg, err := LoadExecutors("/nonexistent/path")
	if err != nil {
		t.Fatalf("missing file should fall back silently: %v", err)
	}
	if cfg.Default != "claude" {
		t.Errorf("default executor: got %q", cfg.Default)
	}
	if _, ok := cfg.ByName["claude"]; !ok {
		t.Error("claude missing from defaults")
	}
	if _, ok := cfg.ByName["codex"]; !ok {
		t.Error("codex missing from defaults")
	}
}

func TestLoadExecutors_CustomFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "executors.toml")
	os.WriteFile(path, []byte(`
default_executor = "codex"

[[executors]]
name = "claude"
command = "/opt/claude"

[[executors]]
name = "codex"
command = "/opt/codex"

[[executors]]
name = "gpt5"
command = "/opt/gpt5"
`), 0o600)
	cfg, err := LoadExecutors(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Default != "codex" {
		t.Errorf("default: got %q", cfg.Default)
	}
	if cfg.ByName["claude"].Command != "/opt/claude" {
		t.Errorf("claude: got %+v", cfg.ByName["claude"])
	}
	if _, ok := cfg.ByName["gpt5"]; !ok {
		t.Error("gpt5 missing")
	}
}

func TestLoadExecutors_Roles(t *testing.T) {
	cfg, _ := LoadExecutors("/nonexistent")
	names := cfg.Roles()
	if len(names) < 2 {
		t.Errorf("expected >=2 roles, got %v", names)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

```bash
cd /home/daoleno/workspace/zen/daemon && go test ./issue/ -run TestLoadExecutors -v
```

Expected: FAIL — undefined.

- [ ] **Step 3: Implement `executors.go`**

```go
package issue

import (
	"errors"
	"os"
	"sort"

	"github.com/BurntSushi/toml"
)

// ExecutorConfig holds the result of parsing ~/.zen/executors.toml.
type ExecutorConfig struct {
	Default string
	ByName  map[string]Executor
}

// Roles returns executor names sorted alphabetically.
func (c *ExecutorConfig) Roles() []string {
	out := make([]string, 0, len(c.ByName))
	for n := range c.ByName {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

type executorFile struct {
	DefaultExecutor string     `toml:"default_executor"`
	Executors       []Executor `toml:"executors"`
}

// LoadExecutors reads the file at path. If the file does not exist, returns
// a built-in default (claude, codex).
func LoadExecutors(path string) (*ExecutorConfig, error) {
	cfg := &ExecutorConfig{
		Default: "claude",
		ByName: map[string]Executor{
			"claude": {Name: "claude", Command: "claude"},
			"codex":  {Name: "codex", Command: "codex"},
		},
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	var f executorFile
	if err := toml.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	if f.DefaultExecutor != "" {
		cfg.Default = f.DefaultExecutor
	}
	for _, e := range f.Executors {
		if e.Name == "" {
			continue
		}
		cfg.ByName[e.Name] = e
	}
	return cfg, nil
}
```

- [ ] **Step 4: Run tests to verify pass**

```bash
cd /home/daoleno/workspace/zen/daemon && go test ./issue/ -run TestLoadExecutors -v
```

Expected: 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add daemon/issue/executors.go daemon/issue/executors_test.go
git commit -m "Load executors.toml with built-in defaults"
```

---

### Task 8: Paths helper

**Files:**
- Create: `daemon/issue/paths.go`

- [ ] **Step 1: Implement**

```go
package issue

import (
	"os"
	"path/filepath"
)

// DefaultRoot returns ~/.zen/issues.
func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".zen", "issues"), nil
}

// DefaultExecutorsPath returns ~/.zen/executors.toml.
func DefaultExecutorsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".zen", "executors.toml"), nil
}

// EnsureDir creates dir with mode 0o700 if it doesn't exist.
func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0o700)
}
```

- [ ] **Step 2: Verify build**

```bash
cd /home/daoleno/workspace/zen/daemon && go build ./issue/...
```

Expected: success.

- [ ] **Step 3: Commit**

```bash
git add daemon/issue/paths.go
git commit -m "Add default path helpers for issues root and executors.toml"
```

---

### Task 9: Store — scan + in-memory snapshot (TDD)

**Files:**
- Create: `daemon/issue/store.go`
- Create: `daemon/issue/store_test.go`

- [ ] **Step 1: Write failing test**

```go
package issue

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeIssue(t *testing.T, path, id string) {
	t.Helper()
	content := `---
id: ` + id + `
created: 2026-04-21T00:00:00Z
---
# Issue ` + id + `

Body.
`
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStore_Scan(t *testing.T) {
	root := t.TempDir()
	writeIssue(t, filepath.Join(root, "zen", "a.md"), "A")
	writeIssue(t, filepath.Join(root, "zen", "b.md"), "B")
	writeIssue(t, filepath.Join(root, "inbox", "c.md"), "C")

	s, err := NewStore(root)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()

	all := s.List()
	if len(all) != 3 {
		t.Fatalf("want 3, got %d", len(all))
	}
}

func TestStore_GetByID(t *testing.T) {
	root := t.TempDir()
	writeIssue(t, filepath.Join(root, "zen", "a.md"), "A")

	s, _ := NewStore(root)
	defer s.Close()

	iss, ok := s.GetByID("A")
	if !ok {
		t.Fatal("A not found")
	}
	if iss.Project != "zen" {
		t.Errorf("project: %q", iss.Project)
	}
}

func TestStore_WriteAndRead(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "zen"), 0o700)
	s, _ := NewStore(root)
	defer s.Close()

	now := time.Now().UTC()
	iss := &Issue{
		Path:    filepath.Join(root, "zen", "new.md"),
		Project: "zen",
		Body:    "# New\n\nBody.\n",
		Frontmatter: Frontmatter{
			ID:      "NEW",
			Created: now,
		},
	}
	if _, err := s.Write(iss, time.Time{}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, ok := s.GetByID("NEW")
	if !ok {
		t.Fatal("not found after write")
	}
	if got.Title != "New" {
		t.Errorf("title: %q", got.Title)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd /home/daoleno/workspace/zen/daemon && go test ./issue/ -run TestStore -v
```

Expected: FAIL — undefined.

- [ ] **Step 3: Implement `store.go` (scan + write, no watcher yet)**

```go
package issue

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Event describes an issue change emitted by the store.
type Event struct {
	Type    EventType
	Path    string
	Issue   *Issue // nil when Type == EventDeleted
}

type EventType string

const (
	EventChanged  EventType = "changed"
	EventDeleted  EventType = "deleted"
)

// ErrConflict is returned by Write when the base mtime does not match disk.
var ErrConflict = errors.New("issue write conflict: file changed on disk")

// Store is an in-memory index of Markdown issue files under Root.
type Store struct {
	Root string

	mu       sync.RWMutex
	byPath   map[string]*Issue
	byID     map[string]*Issue
	subs     map[int]chan Event
	nextSub  int

	// watcher bits (populated in Task 10)
	stopCh chan struct{}
}

// NewStore creates the store and performs an initial scan.
func NewStore(root string) (*Store, error) {
	if err := EnsureDir(root); err != nil {
		return nil, err
	}
	s := &Store{
		Root:   root,
		byPath: map[string]*Issue{},
		byID:   map[string]*Issue{},
		subs:   map[int]chan Event{},
		stopCh: make(chan struct{}),
	}
	if err := s.scanAll(); err != nil {
		return nil, err
	}
	return s, nil
}

// Close stops the watcher (if started) and releases subscriptions.
func (s *Store) Close() error {
	close(s.stopCh)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.subs {
		close(ch)
	}
	s.subs = nil
	return nil
}

// Subscribe returns a channel that receives Events. Caller must drain it.
func (s *Store) Subscribe() (int, <-chan Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextSub
	s.nextSub++
	ch := make(chan Event, 64)
	s.subs[id] = ch
	return id, ch
}

// Unsubscribe removes a subscriber by id.
func (s *Store) Unsubscribe(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, ok := s.subs[id]; ok {
		close(ch)
		delete(s.subs, id)
	}
}

func (s *Store) broadcast(ev Event) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, ch := range s.subs {
		select {
		case ch <- ev:
		default:
			// drop for slow subscribers
		}
	}
}

// List returns a snapshot of all issues.
func (s *Store) List() []*Issue {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Issue, 0, len(s.byPath))
	for _, v := range s.byPath {
		out = append(out, v)
	}
	return out
}

// GetByID returns the issue with the given frontmatter id.
func (s *Store) GetByID(id string) (*Issue, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	iss, ok := s.byID[id]
	return iss, ok
}

// Write persists the issue to disk atomically. If baseMtime is non-zero and
// the current file mtime does not match, returns ErrConflict.
// Returns the new on-disk mtime.
func (s *Store) Write(iss *Issue, baseMtime time.Time) (time.Time, error) {
	if iss.Path == "" {
		return time.Time{}, fmt.Errorf("issue path required")
	}
	if !baseMtime.IsZero() {
		st, err := os.Stat(iss.Path)
		if err == nil {
			if !st.ModTime().Equal(baseMtime) {
				return time.Time{}, ErrConflict
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return time.Time{}, err
		}
	}

	data, err := SerializeIssue(iss)
	if err != nil {
		return time.Time{}, err
	}
	if err := os.MkdirAll(filepath.Dir(iss.Path), 0o700); err != nil {
		return time.Time{}, err
	}
	if err := writeAtomic(iss.Path, data, 0o600); err != nil {
		return time.Time{}, err
	}
	st, err := os.Stat(iss.Path)
	if err != nil {
		return time.Time{}, err
	}
	iss.Mtime = st.ModTime()

	s.mu.Lock()
	s.byPath[iss.Path] = iss
	s.byID[iss.Frontmatter.ID] = iss
	s.mu.Unlock()
	s.broadcast(Event{Type: EventChanged, Path: iss.Path, Issue: iss})
	return iss.Mtime, nil
}

// Delete removes the file and evicts from the index.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	iss, ok := s.byID[id]
	if !ok {
		s.mu.Unlock()
		return os.ErrNotExist
	}
	path := iss.Path
	delete(s.byID, id)
	delete(s.byPath, path)
	s.mu.Unlock()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s.broadcast(Event{Type: EventDeleted, Path: path})
	return nil
}

func (s *Store) scanAll() error {
	return filepath.Walk(s.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		if err := s.reloadPath(path); err != nil {
			// skip files we can't parse; log and continue
			fmt.Fprintf(os.Stderr, "issue: skip %s: %v\n", path, err)
			return nil
		}
		return nil
	})
}

// reloadPath reads a file from disk and upserts it into the index.
func (s *Store) reloadPath(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	iss, err := ParseFile(path, data, st.ModTime())
	if err != nil {
		return err
	}
	s.mu.Lock()
	// evict previous id mapping if path changed ids
	if old, ok := s.byPath[path]; ok && old.Frontmatter.ID != iss.Frontmatter.ID {
		delete(s.byID, old.Frontmatter.ID)
	}
	s.byPath[path] = iss
	s.byID[iss.Frontmatter.ID] = iss
	s.mu.Unlock()
	return nil
}

// writeAtomic is the issue package's local atomic write.
func writeAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".zen-issue-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}
```

- [ ] **Step 4: Run tests to verify pass**

```bash
cd /home/daoleno/workspace/zen/daemon && go test ./issue/ -run TestStore -v
```

Expected: 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add daemon/issue/store.go daemon/issue/store_test.go
git commit -m "Add issue store: scan + atomic write + mtime conflict check"
```

---

### Task 10: Store — fsnotify watcher + debounce (TDD)

**Files:**
- Modify: `daemon/issue/store.go`
- Modify: `daemon/issue/store_test.go`

- [ ] **Step 1: Write failing test**

Append to `store_test.go`:

```go
func TestStore_Watch_NotifiesOnChange(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "zen"), 0o700)
	s, _ := NewStore(root)
	defer s.Close()

	if err := s.StartWatcher(); err != nil {
		t.Fatalf("watch: %v", err)
	}
	_, ch := s.Subscribe()

	// write a new file
	go writeIssue(t, filepath.Join(root, "zen", "live.md"), "LIVE")

	select {
	case ev := <-ch:
		if ev.Type != EventChanged {
			t.Errorf("type: %s", ev.Type)
		}
		if ev.Issue == nil || ev.Issue.Frontmatter.ID != "LIVE" {
			t.Errorf("unexpected: %+v", ev.Issue)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for change event")
	}
}

func TestStore_Watch_DebouncesMultipleWrites(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "zen"), 0o700)
	s, _ := NewStore(root)
	defer s.Close()

	s.StartWatcher()
	_, ch := s.Subscribe()

	p := filepath.Join(root, "zen", "hot.md")
	writeIssue(t, p, "HOT")
	// immediately rewrite a few times within the debounce window
	for i := 0; i < 3; i++ {
		time.Sleep(50 * time.Millisecond)
		writeIssue(t, p, "HOT")
	}

	count := 0
	timeout := time.After(600 * time.Millisecond)
loop:
	for {
		select {
		case <-ch:
			count++
		case <-timeout:
			break loop
		}
	}
	if count == 0 {
		t.Fatal("no events")
	}
	if count > 2 {
		t.Errorf("expected debounced (<=2 events), got %d", count)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd /home/daoleno/workspace/zen/daemon && go test ./issue/ -run TestStore_Watch -v
```

Expected: FAIL — `StartWatcher` not defined.

- [ ] **Step 3: Add watcher to `store.go`**

Append to `store.go`:

```go
import "github.com/fsnotify/fsnotify"

// StartWatcher begins recursive fsnotify watching of s.Root. Events are
// debounced per-path with a 200ms window. Safe to call once.
func (s *Store) StartWatcher() error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := addRecursive(w, s.Root); err != nil {
		w.Close()
		return err
	}
	pending := map[string]*time.Timer{}
	var mu sync.Mutex

	flush := func(path string) {
		mu.Lock()
		delete(pending, path)
		mu.Unlock()
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			s.mu.Lock()
			iss, ok := s.byPath[path]
			if ok {
				delete(s.byPath, path)
				delete(s.byID, iss.Frontmatter.ID)
			}
			s.mu.Unlock()
			if ok {
				s.broadcast(Event{Type: EventDeleted, Path: path})
			}
			return
		}
		if !strings.HasSuffix(path, ".md") {
			return
		}
		if err := s.reloadPath(path); err != nil {
			fmt.Fprintf(os.Stderr, "issue: reload %s: %v\n", path, err)
			return
		}
		s.mu.RLock()
		iss := s.byPath[path]
		s.mu.RUnlock()
		if iss != nil {
			s.broadcast(Event{Type: EventChanged, Path: path, Issue: iss})
		}
	}

	go func() {
		defer w.Close()
		for {
			select {
			case <-s.stopCh:
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				// if a new dir was created, watch it recursively
				if ev.Op&fsnotify.Create != 0 {
					st, err := os.Stat(ev.Name)
					if err == nil && st.IsDir() {
						addRecursive(w, ev.Name)
					}
				}
				mu.Lock()
				if t, ok := pending[ev.Name]; ok {
					t.Stop()
				}
				path := ev.Name
				pending[path] = time.AfterFunc(200*time.Millisecond, func() {
					flush(path)
				})
				mu.Unlock()
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				fmt.Fprintf(os.Stderr, "issue: watch error: %v\n", err)
			}
		}
	}()
	return nil
}

func addRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // best-effort
		}
		if info.IsDir() {
			return w.Add(path)
		}
		return nil
	})
}
```

- [ ] **Step 4: Run tests to verify pass**

```bash
cd /home/daoleno/workspace/zen/daemon && go test ./issue/ -run TestStore -v
```

Expected: all Store tests pass.

- [ ] **Step 5: Commit**

```bash
git add daemon/issue/store.go daemon/issue/store_test.go
git commit -m "Watch issues root with fsnotify + 200ms debounce"
```

---

### Task 11: Dispatcher (TDD with interface-based fakes)

**Files:**
- Create: `daemon/issue/dispatch.go`
- Create: `daemon/issue/dispatch_test.go`

This task introduces two small interfaces (`SessionRegistry`, `SessionRunner`) so the dispatcher is testable without real tmux. Concrete implementations are provided later in Task 12 (wiring) as thin adapters around `watcher` and `terminal`.

- [ ] **Step 1: Write failing tests**

```go
package issue

import (
	"errors"
	"testing"
	"time"
)

// --- fakes ---

type fakeSession struct {
	id       string
	project  string
	cwd      string
	role     string
	state    string
}

type fakeRegistry struct {
	sessions []fakeSession
}

func (f *fakeRegistry) IdleSessions(role, cwd string) []SessionInfo {
	out := []SessionInfo{}
	for _, s := range f.sessions {
		if s.role == role && s.cwd == cwd && s.state == "idle" {
			out = append(out, SessionInfo{ID: s.id, Project: s.project, Cwd: s.cwd, Role: s.role})
		}
	}
	return out
}

type fakeRunner struct {
	spawnCalls int
	sendCalls  []string
	spawnErr   error
	newID      string
}

func (f *fakeRunner) Spawn(role, cwd, command string) (string, error) {
	f.spawnCalls++
	if f.spawnErr != nil {
		return "", f.spawnErr
	}
	return f.newID, nil
}
func (f *fakeRunner) Send(sessionID, text string) error {
	f.sendCalls = append(f.sendCalls, sessionID+"|"+text)
	return nil
}

// --- tests ---

func TestDispatch_UsesIdleSession(t *testing.T) {
	reg := &fakeRegistry{sessions: []fakeSession{{id: "claude-1", cwd: "/p", role: "claude", state: "idle"}}}
	run := &fakeRunner{}
	execs := &ExecutorConfig{Default: "claude", ByName: map[string]Executor{"claude": {Name: "claude", Command: "claude"}}}

	iss := &Issue{
		Path: "/tmp/t.md", Project: "p",
		Body: "@claude do it",
		Mentions: []Mention{{Role: "claude"}},
		Frontmatter: Frontmatter{ID: "T", Created: time.Now()},
	}
	proj := Project{Name: "p", Cwd: "/p", Executor: "claude"}
	d := NewDispatcher(reg, run, execs)
	updated, err := d.Dispatch(iss, proj)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if run.spawnCalls != 0 {
		t.Errorf("should reuse idle, not spawn; spawns=%d", run.spawnCalls)
	}
	if len(run.sendCalls) != 1 {
		t.Fatalf("expected 1 send, got %d", len(run.sendCalls))
	}
	if updated.Frontmatter.AgentSession != "claude-1" {
		t.Errorf("agent_session: %q", updated.Frontmatter.AgentSession)
	}
	if updated.Frontmatter.Dispatched == nil {
		t.Error("dispatched not set")
	}
}

func TestDispatch_SpawnsWhenNoIdle(t *testing.T) {
	reg := &fakeRegistry{}
	run := &fakeRunner{newID: "claude-new"}
	execs := &ExecutorConfig{Default: "claude", ByName: map[string]Executor{"claude": {Name: "claude", Command: "claude"}}}
	iss := &Issue{Path: "/tmp/t.md", Project: "p", Mentions: []Mention{{Role: "claude"}},
		Frontmatter: Frontmatter{ID: "T", Created: time.Now()}}
	proj := Project{Name: "p", Cwd: "/p", Executor: "claude"}
	d := NewDispatcher(reg, run, execs)
	updated, err := d.Dispatch(iss, proj)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if run.spawnCalls != 1 {
		t.Errorf("expected spawn, got %d", run.spawnCalls)
	}
	if updated.Frontmatter.AgentSession != "claude-new" {
		t.Errorf("session: %q", updated.Frontmatter.AgentSession)
	}
}

func TestDispatch_AlreadyDispatched_Error(t *testing.T) {
	now := time.Now()
	iss := &Issue{Frontmatter: Frontmatter{ID: "T", Dispatched: &now}}
	d := NewDispatcher(&fakeRegistry{}, &fakeRunner{}, &ExecutorConfig{Default: "claude"})
	if _, err := d.Dispatch(iss, Project{Cwd: "/p"}); !errors.Is(err, ErrAlreadyDispatched) {
		t.Fatalf("want ErrAlreadyDispatched, got %v", err)
	}
}

func TestDispatch_NoExecutor_Error(t *testing.T) {
	execs := &ExecutorConfig{Default: "missing", ByName: map[string]Executor{}}
	iss := &Issue{Frontmatter: Frontmatter{ID: "T"}, Mentions: []Mention{{Role: "missing"}}}
	d := NewDispatcher(&fakeRegistry{}, &fakeRunner{}, execs)
	if _, err := d.Dispatch(iss, Project{Cwd: "/p"}); !errors.Is(err, ErrExecutorNotConfigured) {
		t.Fatalf("want ErrExecutorNotConfigured, got %v", err)
	}
}

func TestDispatch_RespectsSessionMention(t *testing.T) {
	reg := &fakeRegistry{sessions: []fakeSession{
		{id: "claude-1", cwd: "/p", role: "claude", state: "idle"},
		{id: "claude-2", cwd: "/p", role: "claude", state: "idle"},
	}}
	run := &fakeRunner{}
	execs := &ExecutorConfig{Default: "claude", ByName: map[string]Executor{"claude": {Name: "claude", Command: "claude"}}}
	iss := &Issue{
		Path: "/tmp/t.md", Project: "p",
		Mentions: []Mention{{Role: "claude", Session: "claude-2"}},
		Frontmatter: Frontmatter{ID: "T"},
	}
	proj := Project{Name: "p", Cwd: "/p", Executor: "claude"}
	d := NewDispatcher(reg, run, execs)
	updated, err := d.Dispatch(iss, proj)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if updated.Frontmatter.AgentSession != "claude-2" {
		t.Errorf("want claude-2, got %q", updated.Frontmatter.AgentSession)
	}
}

func TestDispatch_Redispatch_ClearsFields(t *testing.T) {
	now := time.Now()
	reg := &fakeRegistry{sessions: []fakeSession{{id: "claude-1", cwd: "/p", role: "claude", state: "idle"}}}
	run := &fakeRunner{}
	execs := &ExecutorConfig{Default: "claude", ByName: map[string]Executor{"claude": {Name: "claude", Command: "claude"}}}
	iss := &Issue{
		Path: "/tmp/t.md", Project: "p",
		Mentions: []Mention{{Role: "claude"}},
		Frontmatter: Frontmatter{ID: "T", Dispatched: &now, AgentSession: "old"},
	}
	proj := Project{Name: "p", Cwd: "/p", Executor: "claude"}
	d := NewDispatcher(reg, run, execs)
	updated, err := d.Redispatch(iss, proj)
	if err != nil {
		t.Fatalf("redispatch: %v", err)
	}
	if updated.Frontmatter.AgentSession != "claude-1" {
		t.Errorf("session should be refreshed, got %q", updated.Frontmatter.AgentSession)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd /home/daoleno/workspace/zen/daemon && go test ./issue/ -run TestDispatch -v
```

Expected: FAIL — undefined.

- [ ] **Step 3: Implement `dispatch.go`**

```go
package issue

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// SessionInfo describes a candidate tmux session.
type SessionInfo struct {
	ID      string
	Project string
	Cwd     string
	Role    string
}

// SessionRegistry returns running sessions the dispatcher can target.
// Implementations wrap the watcher + classifier.
type SessionRegistry interface {
	// IdleSessions returns sessions with matching role + cwd currently idle
	// (classifier state allows sending new input).
	IdleSessions(role, cwd string) []SessionInfo
}

// SessionRunner is the tmux side of the world: spawn new sessions and deliver
// text to existing ones.
type SessionRunner interface {
	// Spawn creates a new tmux session under cwd running command.
	// Returns the session id.
	Spawn(role, cwd, command string) (string, error)
	// Send writes text into an existing session's stdin (tmux send-keys).
	Send(sessionID, text string) error
}

// Sentinel errors returned by Dispatch.
var (
	ErrAlreadyDispatched       = errors.New("issue already dispatched")
	ErrExecutorNotConfigured   = errors.New("executor not configured")
	ErrSpawnFailed             = errors.New("spawn failed")
)

// Dispatcher picks an agent and hands it an initial prompt for the issue.
type Dispatcher struct {
	reg   SessionRegistry
	run   SessionRunner
	execs *ExecutorConfig
	Now   func() time.Time
}

func NewDispatcher(reg SessionRegistry, run SessionRunner, execs *ExecutorConfig) *Dispatcher {
	return &Dispatcher{reg: reg, run: run, execs: execs, Now: time.Now}
}

// Dispatch sends iss to an agent and returns the updated issue (with
// Frontmatter.Dispatched + AgentSession populated). Caller persists it.
func (d *Dispatcher) Dispatch(iss *Issue, proj Project) (*Issue, error) {
	if iss.Frontmatter.Dispatched != nil {
		return iss, ErrAlreadyDispatched
	}
	return d.dispatchInternal(iss, proj)
}

// Redispatch clears the dispatched markers and dispatches again.
func (d *Dispatcher) Redispatch(iss *Issue, proj Project) (*Issue, error) {
	iss.Frontmatter.Dispatched = nil
	iss.Frontmatter.AgentSession = ""
	return d.dispatchInternal(iss, proj)
}

func (d *Dispatcher) dispatchInternal(iss *Issue, proj Project) (*Issue, error) {
	role, targetSession := d.primaryMention(iss)
	if role == "" {
		role = proj.Executor
		if role == "" {
			role = d.execs.Default
		}
	}
	exec, ok := d.execs.ByName[role]
	if !ok {
		return iss, fmt.Errorf("%w: %s", ErrExecutorNotConfigured, role)
	}

	cwd := proj.Cwd
	if cwd == "" {
		return iss, fmt.Errorf("project %q has no cwd set", proj.Name)
	}

	var sessionID string
	if targetSession != "" {
		// user asked for a specific existing session — trust it
		sessionID = targetSession
	} else {
		candidates := d.reg.IdleSessions(role, cwd)
		if len(candidates) > 0 {
			sessionID = candidates[0].ID
		} else {
			newID, err := d.run.Spawn(role, cwd, exec.Command)
			if err != nil {
				return iss, fmt.Errorf("%w: %v", ErrSpawnFailed, err)
			}
			sessionID = newID
		}
	}

	prompt := buildInitialPrompt(iss.Path)
	if err := d.run.Send(sessionID, prompt); err != nil {
		return iss, fmt.Errorf("%w: send prompt: %v", ErrSpawnFailed, err)
	}
	now := d.Now()
	iss.Frontmatter.Dispatched = &now
	iss.Frontmatter.AgentSession = sessionID
	return iss, nil
}

func (d *Dispatcher) primaryMention(iss *Issue) (role, session string) {
	if len(iss.Mentions) == 0 {
		return "", ""
	}
	m := iss.Mentions[0]
	return m.Role, m.Session
}

func buildInitialPrompt(path string) string {
	return strings.TrimSpace(fmt.Sprintf(`
Your task is described in this file: %s
Read it, do the work, and edit the file as you progress.
When finished, set `+"`done: <ISO8601 timestamp>`"+` in the frontmatter.
`, path)) + "\n"
}
```

- [ ] **Step 4: Run tests to verify pass**

```bash
cd /home/daoleno/workspace/zen/daemon && go test ./issue/ -run TestDispatch -v
```

Expected: 6 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add daemon/issue/dispatch.go daemon/issue/dispatch_test.go
git commit -m "Dispatch issues to idle or freshly spawned sessions"
```

---

### Task 12: Wire registry and runner adapters

**Files:**
- Create: `daemon/issue/adapters.go`

The dispatcher's `SessionRegistry` and `SessionRunner` interfaces need concrete implementations using the existing `watcher` package (agent discovery) and `terminal` package (tmux).

- [ ] **Step 1: Confirm watcher types (already verified)**

Verified facts (from reading the code):
- `watcher.Watcher.Agents() []*classifier.Agent` (watcher/watcher.go:55).
- `classifier.Agent` fields include `ID`, `Cwd`, `State`, `Command`, `Project` — access via pointer (the slice is `[]*classifier.Agent`).
- `classifier.StateRunning` is the "idle and accepting input" state.
- The watcher polls via `tmux list-sessions` every 500ms and strips sessions whose name starts with `zen-` (watcher/watcher.go:486). Spawned agent sessions must NOT use the `zen-` prefix.

- [ ] **Step 2: Implement adapters**

**Critical constraint (verified in source):** `daemon/watcher/watcher.go:486` explicitly excludes tmux sessions whose name starts with `zen-` because those are terminal-streaming proxies, not user agents. Therefore the dispatcher must NOT use the `zen-` prefix, or the watcher will never track the spawned session and `agent_session` in the frontmatter won't resolve to anything in the app.

Also important: the watcher's agent ID is `<session_name>:<window_id>` (comment at `watcher.go:471`), not the bare session name. After spawning, we look up the window id and return the full target so later `Send` calls and the app's agent lookup line up with the watcher's view.

Create `daemon/issue/adapters.go`:

```go
package issue

import (
	"fmt"
	"math/rand"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
)

// WatcherRegistry adapts watcher.Watcher to SessionRegistry.
type WatcherRegistry struct {
	W *watcher.Watcher
}

func (r *WatcherRegistry) IdleSessions(role, cwd string) []SessionInfo {
	out := []SessionInfo{}
	for _, a := range r.W.Agents() {
		if a.Cwd != cwd {
			continue
		}
		if !roleMatches(a, role) {
			continue
		}
		if a.State != classifier.StateRunning {
			continue
		}
		out = append(out, SessionInfo{
			ID:      a.ID,
			Project: a.Project,
			Cwd:     a.Cwd,
			Role:    role,
		})
	}
	return out
}

// roleMatches decides whether an Agent corresponds to the requested executor role.
// Convention: the first word of a.Command (e.g., "claude", "codex") is the role name.
func roleMatches(a *classifier.Agent, role string) bool {
	if a.Command == "" {
		return false
	}
	first := firstWord(a.Command)
	return first == role
}

func firstWord(s string) string {
	for i, r := range s {
		if r == ' ' || r == '\t' {
			return s[:i]
		}
	}
	return s
}

// TmuxRunner adapts tmux CLI calls to SessionRunner.
type TmuxRunner struct{}

var tmuxCounter atomic.Uint64
var tmuxRand = rand.New(rand.NewSource(time.Now().UnixNano()))

// sessionName builds a tmux session name that:
//   - Does NOT start with "zen-" (the watcher excludes those).
//   - Is short, readable, unique enough for practical use.
// Format: "<role>-<date>-<4 hex chars>", e.g. "claude-260421-3a1f".
func sessionName(role string) string {
	n := tmuxCounter.Add(1)
	// small daily suffix + random to avoid collisions across daemon restarts
	return fmt.Sprintf("%s-%s-%04x%x",
		role,
		time.Now().Format("060102"),
		tmuxRand.Intn(0xffff),
		n%0xf,
	)
}

// Spawn creates a detached tmux session running command in cwd and returns
// the watcher-compatible agent ID "<session>:<window_id>".
func (TmuxRunner) Spawn(role, cwd, command string) (string, error) {
	name := sessionName(role)
	create := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", cwd, command)
	if out, err := create.CombinedOutput(); err != nil {
		return "", fmt.Errorf("tmux new-session: %w: %s", err, strings.TrimSpace(string(out)))
	}
	// Ask tmux for the window id so the returned id matches how the watcher
	// stores agents (session:window_id).
	lw := exec.Command("tmux", "list-windows", "-t", name, "-F", "#{window_id}")
	out, err := lw.Output()
	if err != nil {
		_ = exec.Command("tmux", "kill-session", "-t", name).Run()
		return "", fmt.Errorf("tmux list-windows: %w", err)
	}
	windowID := strings.TrimSpace(string(out))
	if windowID == "" {
		_ = exec.Command("tmux", "kill-session", "-t", name).Run()
		return "", fmt.Errorf("tmux list-windows: no window id returned")
	}
	return name + ":" + windowID, nil
}

// Send writes text followed by Enter to the session's pane.
// agentID is expected in "session:window_id" form (what Spawn returns and
// what the watcher stores).
func (TmuxRunner) Send(agentID, text string) error {
	cmd := exec.Command("tmux", "send-keys", "-t", agentID, text, "C-m")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux send-keys: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
```

No changes required in the `terminal` package.

- [ ] **Step 3: Verify build**

```bash
cd /home/daoleno/workspace/zen/daemon && go build ./...
```

If `watcher.Agent`, `classifier.StateRunning`, or `terminal.GenerateSessionName` don't match the real API exactly, fix the imports and field names now. All such references in adapters.go must resolve before continuing.

Expected: success.

- [ ] **Step 4: Commit**

```bash
git add daemon/issue/adapters.go daemon/terminal/names.go
git commit -m "Adapt watcher + tmux to issue dispatcher interfaces"
```

---

### Task 13: Server — new message types and skeleton handler

**Files:**
- Modify: `daemon/server/server.go`

Goal: add new issue message types to the existing dispatch switch and a new constructor field for the issue store. Do NOT remove old handlers yet — that happens in Phase 3.

- [ ] **Step 1: Read existing handler pattern**

```bash
grep -n "case \"" /home/daoleno/workspace/zen/daemon/server/server.go | head -30
grep -n "func (s \*Server)" /home/daoleno/workspace/zen/daemon/server/server.go | head -10
grep -n "func New" /home/daoleno/workspace/zen/daemon/server/server.go
```

Expected: confirm `server.New(...)` signature and the existing `switch raw.Type` location.

- [ ] **Step 2: Extend `server.New` to take issue store + dispatcher**

Find the existing `func New(...)` signature (around line 60-90) and add two new parameters at the end:

```go
func New(
    authManager *auth.Manager,
    w *watcher.Watcher,
    pusher *push.Pusher,
    sc *stats.Collector,
    tasks *task.Store,        // existing, will be removed in Phase 3
    runs *task.RunStore,      // existing, will be removed in Phase 3
    guidance *task.GuidanceStore, // existing
    projects *task.ProjectStore,  // existing
    issues *issue.Store,         // NEW
    dispatcher *issue.Dispatcher, // NEW
    execs *issue.ExecutorConfig,  // NEW
) *Server {
    // ... existing body ...
    s.issues = issues
    s.dispatcher = dispatcher
    s.execs = execs
    // Subscribe to issue events for broadcast (Task 15 will add broadcast logic)
    return s
}
```

Add corresponding fields to the `Server` struct (near the other store fields):

```go
issues     *issue.Store
dispatcher *issue.Dispatcher
execs      *issue.ExecutorConfig
```

Import at the top:

```go
import "github.com/daoleno/zen/daemon/issue"
```

- [ ] **Step 3: Add handler skeletons**

Find the main dispatch switch (search `case "list_tasks":`). Add these cases alongside (do not remove old cases):

```go
case "list_issues":
    s.handleListIssues(conn, raw)
case "get_issue":
    s.handleGetIssue(conn, raw)
case "write_issue":
    s.handleWriteIssue(conn, raw)
case "send_issue":
    s.handleSendIssue(conn, raw)
case "redispatch_issue":
    s.handleRedispatchIssue(conn, raw)
case "delete_issue":
    s.handleDeleteIssue(conn, raw)
case "list_executors":
    s.handleListExecutors(conn, raw)
```

Create stub handler methods at the bottom of the file (implementations fleshed out in Tasks 14 + 15):

```go
func (s *Server) handleListIssues(conn *websocket.Conn, raw rawMessage) {
    s.sendJSON(conn, map[string]any{"type": "issue_list", "issues": s.issues.List()})
}

func (s *Server) handleGetIssue(conn *websocket.Conn, raw rawMessage) {
    var req struct{ ID string `json:"id"` }
    if err := json.Unmarshal(raw.Body, &req); err != nil {
        s.sendErrorWithRequestID(conn, raw.RequestID, err.Error())
        return
    }
    iss, ok := s.issues.GetByID(req.ID)
    if !ok {
        s.sendErrorWithRequestID(conn, raw.RequestID, "issue not found")
        return
    }
    s.sendJSON(conn, map[string]any{"type": "issue", "requestId": raw.RequestID, "issue": iss})
}

func (s *Server) handleListExecutors(conn *websocket.Conn, raw rawMessage) {
    s.sendJSON(conn, map[string]any{"type": "executor_list", "executors": s.execs.Roles()})
}

func (s *Server) handleWriteIssue(conn *websocket.Conn, raw rawMessage) {
    s.sendErrorWithRequestID(conn, raw.RequestID, "write_issue: not implemented")
}
func (s *Server) handleSendIssue(conn *websocket.Conn, raw rawMessage) {
    s.sendErrorWithRequestID(conn, raw.RequestID, "send_issue: not implemented")
}
func (s *Server) handleRedispatchIssue(conn *websocket.Conn, raw rawMessage) {
    s.sendErrorWithRequestID(conn, raw.RequestID, "redispatch_issue: not implemented")
}
func (s *Server) handleDeleteIssue(conn *websocket.Conn, raw rawMessage) {
    s.sendErrorWithRequestID(conn, raw.RequestID, "delete_issue: not implemented")
}
```

**Note:** `rawMessage`, `sendJSON`, `sendErrorWithRequestID`, and the WebSocket conn type name may differ slightly in the codebase (e.g., `Raw` / `Conn` / `sendError`). Adjust the signatures above to match the existing pattern in `server.go`.

- [ ] **Step 4: Update the daemon main to supply the new args**

Edit `daemon/cmd/zen-daemon/main.go`:

```go
// Add imports
import "github.com/daoleno/zen/daemon/issue"

// Replace the server.New call around line 121:

issuesRoot, err := issue.DefaultRoot()
if err != nil {
    return fmt.Errorf("resolve issues root: %w", err)
}
issueStore, err := issue.NewStore(issuesRoot)
if err != nil {
    return fmt.Errorf("initialize issue store: %w", err)
}
if err := issueStore.StartWatcher(); err != nil {
    return fmt.Errorf("start issue watcher: %w", err)
}
defer issueStore.Close()

executorsPath, _ := issue.DefaultExecutorsPath()
execs, err := issue.LoadExecutors(executorsPath)
if err != nil {
    return fmt.Errorf("load executors: %w", err)
}

dispatcher := issue.NewDispatcher(
    &issue.WatcherRegistry{W: w},
    issue.TmuxRunner{},
    execs,
)

srv := server.New(
    authManager, w, pusher, sc,
    taskStore, runStore, guidanceStore, projectStore,
    issueStore, dispatcher, execs,
)
```

- [ ] **Step 5: Verify build**

```bash
cd /home/daoleno/workspace/zen/daemon && go build ./...
```

Expected: success. If signatures differ slightly, adjust minor naming to compile.

- [ ] **Step 6: Commit**

```bash
git add daemon/server/server.go daemon/cmd/zen-daemon/main.go
git commit -m "Wire issue store, dispatcher, and executor list into server"
```

---

### Task 14: Server — implement write/send/redispatch/delete handlers

**Files:**
- Modify: `daemon/server/server.go`

- [ ] **Step 1: Implement `handleWriteIssue`**

Replace the stub from Task 13 with:

```go
func (s *Server) handleWriteIssue(conn *websocket.Conn, raw rawMessage) {
    var req struct {
        ID          string                 `json:"id"`
        Project     string                 `json:"project"`
        Path        string                 `json:"path,omitempty"`
        Body        string                 `json:"body"`
        Frontmatter map[string]interface{} `json:"frontmatter"`
        BaseMtime   *time.Time             `json:"baseMtime,omitempty"`
    }
    if err := json.Unmarshal(raw.Body, &req); err != nil {
        s.sendErrorWithRequestID(conn, raw.RequestID, err.Error())
        return
    }

    // Build the Issue. If ID is empty, generate a ULID and fresh created timestamp.
    now := time.Now().UTC()
    if req.ID == "" {
        req.ID = ulid.Make().String()
    }

    // Build path if not given.
    path := req.Path
    if path == "" {
        root, _ := issue.DefaultRoot()
        slug := slugify(firstLine(req.Body), req.ID)
        path = filepath.Join(root, req.Project, now.Format("2006-01-02")+"-"+slug+".md")
    }

    fm := issue.Frontmatter{
        ID:      req.ID,
        Created: now,
    }
    if v, ok := req.Frontmatter["created"].(string); ok {
        if t, err := time.Parse(time.RFC3339, v); err == nil {
            fm.Created = t
        }
    }
    if v, ok := req.Frontmatter["done"].(string); ok && v != "" {
        if t, err := time.Parse(time.RFC3339, v); err == nil {
            fm.Done = &t
        }
    }
    if v, ok := req.Frontmatter["dispatched"].(string); ok && v != "" {
        if t, err := time.Parse(time.RFC3339, v); err == nil {
            fm.Dispatched = &t
        }
    }
    if v, ok := req.Frontmatter["agent_session"].(string); ok {
        fm.AgentSession = v
    }

    iss := &issue.Issue{
        Path:        path,
        Project:     req.Project,
        Body:        req.Body,
        Frontmatter: fm,
        Mentions:    issue.ExtractMentions(req.Body),
        Title:       titleFromBody(req.Body),
    }

    var base time.Time
    if req.BaseMtime != nil {
        base = *req.BaseMtime
    }
    mtime, err := s.issues.Write(iss, base)
    if err != nil {
        if errors.Is(err, issue.ErrConflict) {
            current, _ := s.issues.GetByID(req.ID)
            s.sendJSON(conn, map[string]any{
                "type": "error", "requestId": raw.RequestID,
                "error": "conflict", "current": current,
            })
            return
        }
        s.sendErrorWithRequestID(conn, raw.RequestID, err.Error())
        return
    }
    iss.Mtime = mtime
    s.sendJSON(conn, map[string]any{"type": "issue_written", "requestId": raw.RequestID, "issue": iss})
}

func slugify(line, fallback string) string {
    line = strings.TrimSpace(strings.TrimPrefix(line, "#"))
    line = strings.TrimSpace(line)
    if line == "" {
        return strings.ToLower(fallback)
    }
    out := make([]rune, 0, len(line))
    for _, r := range strings.ToLower(line) {
        switch {
        case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
            out = append(out, r)
        case r == ' ' || r == '-' || r == '_':
            out = append(out, '-')
        }
    }
    s := strings.Trim(string(out), "-")
    if s == "" {
        return strings.ToLower(fallback)
    }
    if len(s) > 60 {
        s = s[:60]
    }
    return s
}

func firstLine(s string) string {
    if i := strings.IndexByte(s, '\n'); i >= 0 {
        return s[:i]
    }
    return s
}

func titleFromBody(body string) string {
    for _, line := range strings.Split(body, "\n") {
        t := strings.TrimSpace(line)
        if t == "" { continue }
        if strings.HasPrefix(t, "# ") { return strings.TrimSpace(t[2:]) }
        return t
    }
    return ""
}
```

Add imports at the top if not present:

```go
import (
    "errors"
    "path/filepath"
    "strings"
    "time"

    "github.com/daoleno/zen/daemon/issue"
    "github.com/oklog/ulid/v2"
)
```

- [ ] **Step 2: Implement `handleSendIssue`**

```go
func (s *Server) handleSendIssue(conn *websocket.Conn, raw rawMessage) {
    var req struct{ ID string `json:"id"` }
    if err := json.Unmarshal(raw.Body, &req); err != nil {
        s.sendErrorWithRequestID(conn, raw.RequestID, err.Error())
        return
    }
    iss, ok := s.issues.GetByID(req.ID)
    if !ok {
        s.sendErrorWithRequestID(conn, raw.RequestID, "issue not found")
        return
    }
    proj, err := issue.LoadProject(filepath.Dir(iss.Path))
    if err != nil {
        s.sendErrorWithRequestID(conn, raw.RequestID, err.Error())
        return
    }
    updated, err := s.dispatcher.Dispatch(iss, proj)
    if err != nil {
        s.sendErrorWithRequestID(conn, raw.RequestID, err.Error())
        return
    }
    if _, err := s.issues.Write(updated, time.Time{}); err != nil {
        s.sendErrorWithRequestID(conn, raw.RequestID, err.Error())
        return
    }
    s.sendJSON(conn, map[string]any{"type": "issue_dispatched", "requestId": raw.RequestID, "issue": updated})
}
```

- [ ] **Step 3: Implement `handleRedispatchIssue`**

Same as above but uses `s.dispatcher.Redispatch` instead of `Dispatch`.

```go
func (s *Server) handleRedispatchIssue(conn *websocket.Conn, raw rawMessage) {
    var req struct{ ID string `json:"id"` }
    if err := json.Unmarshal(raw.Body, &req); err != nil {
        s.sendErrorWithRequestID(conn, raw.RequestID, err.Error())
        return
    }
    iss, ok := s.issues.GetByID(req.ID)
    if !ok {
        s.sendErrorWithRequestID(conn, raw.RequestID, "issue not found")
        return
    }
    proj, err := issue.LoadProject(filepath.Dir(iss.Path))
    if err != nil {
        s.sendErrorWithRequestID(conn, raw.RequestID, err.Error())
        return
    }
    updated, err := s.dispatcher.Redispatch(iss, proj)
    if err != nil {
        s.sendErrorWithRequestID(conn, raw.RequestID, err.Error())
        return
    }
    if _, err := s.issues.Write(updated, time.Time{}); err != nil {
        s.sendErrorWithRequestID(conn, raw.RequestID, err.Error())
        return
    }
    s.sendJSON(conn, map[string]any{"type": "issue_redispatched", "requestId": raw.RequestID, "issue": updated})
}
```

- [ ] **Step 4: Implement `handleDeleteIssue`**

```go
func (s *Server) handleDeleteIssue(conn *websocket.Conn, raw rawMessage) {
    var req struct{ ID string `json:"id"` }
    if err := json.Unmarshal(raw.Body, &req); err != nil {
        s.sendErrorWithRequestID(conn, raw.RequestID, err.Error())
        return
    }
    if err := s.issues.Delete(req.ID); err != nil {
        s.sendErrorWithRequestID(conn, raw.RequestID, err.Error())
        return
    }
    s.sendJSON(conn, map[string]any{"type": "issue_deleted_ack", "requestId": raw.RequestID, "id": req.ID})
}
```

- [ ] **Step 5: Verify build**

```bash
cd /home/daoleno/workspace/zen/daemon && go build ./...
```

Expected: success.

- [ ] **Step 6: Commit**

```bash
git add daemon/server/server.go
git commit -m "Implement issue write/send/redispatch/delete handlers"
```

---

### Task 15: Server — broadcast issue events and initial snapshot

**Files:**
- Modify: `daemon/server/server.go`

- [ ] **Step 1: Subscribe to store events in `server.New`**

At the end of `server.New`, add:

```go
// Fan out issue store events to all connected clients.
go func() {
    _, ch := issues.Subscribe()
    for ev := range ch {
        switch ev.Type {
        case issue.EventChanged:
            s.broadcastAll(map[string]any{"type": "issue_changed", "path": ev.Path, "issue": ev.Issue})
        case issue.EventDeleted:
            s.broadcastAll(map[string]any{"type": "issue_deleted", "path": ev.Path})
        }
    }
}()
```

If the existing server has a helper that writes to all connected conns, use it; otherwise add one near the other helpers:

```go
func (s *Server) broadcastAll(msg map[string]any) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    for conn := range s.active {
        s.sendJSON(conn, msg)
    }
}
```

- [ ] **Step 2: Push initial snapshot on new connection**

Find where the server handles a new WebSocket connection (look for `upgrader.Upgrade` or `s.active[conn] =`). After adding the conn to `s.active`, push:

```go
s.sendJSON(conn, map[string]any{
    "type": "issues_snapshot",
    "issues": s.issues.List(),
    "executors": s.execs.Roles(),
})
```

- [ ] **Step 3: Verify build + quick smoke test**

```bash
cd /home/daoleno/workspace/zen/daemon && go build ./... && go test ./issue/... -v
```

Expected: success, all issue package tests pass.

- [ ] **Step 4: Commit**

```bash
git add daemon/server/server.go
git commit -m "Broadcast issue events and push snapshot on connect"
```

---

## Phase 2 — App: new editor and store

### Task 16: Add `store/issues.tsx` (Context + reducer)

**Files:**
- Create: `app/store/issues.tsx`
- Create: `app/store/__tests__/issues.test.ts`

- [ ] **Step 1: Write failing reducer test**

```ts
// app/store/__tests__/issues.test.ts
import { issuesReducer, initialIssuesState, Issue } from "../issues";

const mkIssue = (overrides: Partial<Issue> = {}): Issue => ({
  id: "A",
  path: "/root/zen/a.md",
  project: "zen",
  title: "A",
  body: "# A",
  frontmatter: { id: "A", created: "2026-04-21T00:00:00Z" },
  mentions: [],
  mtime: "2026-04-21T00:00:00Z",
  ...overrides,
});

describe("issuesReducer", () => {
  it("applies ISSUES_SNAPSHOT", () => {
    const next = issuesReducer(initialIssuesState, {
      type: "ISSUES_SNAPSHOT",
      issues: [mkIssue(), mkIssue({ id: "B", project: "zen" })],
      executors: ["claude", "codex"],
    });
    expect(Object.keys(next.byId)).toEqual(["A", "B"]);
    expect(next.byProject["zen"]).toEqual(["A", "B"]);
    expect(next.executors).toEqual(["claude", "codex"]);
  });

  it("applies ISSUE_CHANGED upsert", () => {
    const state = issuesReducer(initialIssuesState, {
      type: "ISSUES_SNAPSHOT", issues: [mkIssue()], executors: [],
    });
    const next = issuesReducer(state, {
      type: "ISSUE_CHANGED",
      issue: mkIssue({ title: "Updated" }),
    });
    expect(next.byId["A"].title).toBe("Updated");
  });

  it("applies ISSUE_DELETED", () => {
    const state = issuesReducer(initialIssuesState, {
      type: "ISSUES_SNAPSHOT", issues: [mkIssue(), mkIssue({ id: "B" })], executors: [],
    });
    const next = issuesReducer(state, { type: "ISSUE_DELETED", id: "A" });
    expect(next.byId["A"]).toBeUndefined();
    expect(next.byProject["zen"]).toEqual(["B"]);
  });
});
```

- [ ] **Step 2: Run tests to verify failure**

```bash
cd /home/daoleno/workspace/zen/app && npx jest store/__tests__/issues.test.ts
```

Expected: FAIL — module not found.

- [ ] **Step 3: Implement `store/issues.tsx`**

```tsx
import React, { createContext, useContext, useReducer } from "react";

export type Frontmatter = {
  id: string;
  created: string; // RFC3339
  done?: string | null;
  dispatched?: string | null;
  agent_session?: string;
  [k: string]: any;
};

export type Mention = { role: string; session?: string; index: number };

export type Issue = {
  id: string;
  path: string;
  project: string;
  title: string;
  body: string;
  frontmatter: Frontmatter;
  mentions: Mention[];
  mtime: string;
};

export type IssuesState = {
  byId: Record<string, Issue>;
  byProject: Record<string, string[]>; // project -> ids (insertion order)
  executors: string[];
};

export const initialIssuesState: IssuesState = {
  byId: {},
  byProject: {},
  executors: [],
};

export type Action =
  | { type: "ISSUES_SNAPSHOT"; issues: Issue[]; executors: string[] }
  | { type: "ISSUE_CHANGED"; issue: Issue }
  | { type: "ISSUE_DELETED"; id?: string; path?: string }
  | { type: "EXECUTORS_LOADED"; executors: string[] };

function groupByProject(byId: Record<string, Issue>): Record<string, string[]> {
  const out: Record<string, string[]> = {};
  for (const iss of Object.values(byId)) {
    if (!out[iss.project]) out[iss.project] = [];
    out[iss.project].push(iss.id);
  }
  // sort each group by created desc
  for (const p of Object.keys(out)) {
    out[p].sort((a, b) =>
      byId[b].frontmatter.created.localeCompare(byId[a].frontmatter.created)
    );
  }
  return out;
}

export function issuesReducer(state: IssuesState, action: Action): IssuesState {
  switch (action.type) {
    case "ISSUES_SNAPSHOT": {
      const byId: Record<string, Issue> = {};
      for (const iss of action.issues) byId[iss.id] = iss;
      return { byId, byProject: groupByProject(byId), executors: action.executors };
    }
    case "ISSUE_CHANGED": {
      const byId = { ...state.byId, [action.issue.id]: action.issue };
      return { ...state, byId, byProject: groupByProject(byId) };
    }
    case "ISSUE_DELETED": {
      const byId = { ...state.byId };
      if (action.id) delete byId[action.id];
      else if (action.path) {
        for (const k of Object.keys(byId)) {
          if (byId[k].path === action.path) delete byId[k];
        }
      }
      return { ...state, byId, byProject: groupByProject(byId) };
    }
    case "EXECUTORS_LOADED":
      return { ...state, executors: action.executors };
  }
}

type Ctx = { state: IssuesState; dispatch: React.Dispatch<Action> };
const IssuesContext = createContext<Ctx | null>(null);

export function IssuesProvider({ children }: { children: React.ReactNode }) {
  const [state, dispatch] = useReducer(issuesReducer, initialIssuesState);
  return (
    <IssuesContext.Provider value={{ state, dispatch }}>{children}</IssuesContext.Provider>
  );
}

export function useIssues(): Ctx {
  const v = useContext(IssuesContext);
  if (!v) throw new Error("useIssues must be used within IssuesProvider");
  return v;
}
```

- [ ] **Step 4: Run tests to verify pass**

```bash
cd /home/daoleno/workspace/zen/app && npx jest store/__tests__/issues.test.ts
```

Expected: 3 tests PASS.

- [ ] **Step 5: Register provider in `_layout.tsx`**

Add `<IssuesProvider>` around the app content, nested inside existing providers. Then register WebSocket listeners (place next to existing `wsClient.on("task_list", ...)` registrations):

```tsx
import { IssuesProvider, useIssues } from "@/store/issues";

// inside AppContent component:
const { dispatch: issuesDispatch } = useIssues();

useEffect(() => {
  const onSnapshot = (msg: any) => issuesDispatch({ type: "ISSUES_SNAPSHOT", issues: msg.issues, executors: msg.executors });
  const onChanged = (msg: any) => issuesDispatch({ type: "ISSUE_CHANGED", issue: msg.issue });
  const onDeleted = (msg: any) => issuesDispatch({ type: "ISSUE_DELETED", id: msg.id, path: msg.path });
  const onExecutors = (msg: any) => issuesDispatch({ type: "EXECUTORS_LOADED", executors: msg.executors });

  wsClient.on("issues_snapshot", onSnapshot);
  wsClient.on("issue_changed", onChanged);
  wsClient.on("issue_deleted", onDeleted);
  wsClient.on("executor_list", onExecutors);

  return () => {
    wsClient.off("issues_snapshot", onSnapshot);
    wsClient.off("issue_changed", onChanged);
    wsClient.off("issue_deleted", onDeleted);
    wsClient.off("executor_list", onExecutors);
  };
}, [issuesDispatch]);
```

If `wsClient.off` isn't present, use whatever deregistration call the existing code uses.

- [ ] **Step 6: Verify build**

```bash
cd /home/daoleno/workspace/zen/app && npx tsc --noEmit
```

Expected: success (or the same pre-existing errors as before — no new issues).

- [ ] **Step 7: Commit**

```bash
git add app/store/issues.tsx app/store/__tests__/issues.test.ts app/app/_layout.tsx
git commit -m "Add issues Context store and wire WebSocket listeners"
```

---

### Task 17: `IssueRow` component

**Files:**
- Create: `app/components/issue/IssueRowNew.tsx` (temp name to avoid clashing with the old file until Task 22)

- [ ] **Step 1: Implement**

```tsx
import React from "react";
import { View, Text, Pressable, StyleSheet } from "react-native";
import { useRouter } from "expo-router";
import { Issue } from "@/store/issues";

export function statusGlyph(iss: Issue): string {
  if (iss.frontmatter.done) return "✓";
  if (iss.frontmatter.dispatched) return "▶";
  return "●";
}

export function relativeTime(iso: string): string {
  const now = Date.now();
  const then = new Date(iso).getTime();
  const diff = Math.floor((now - then) / 1000);
  if (diff < 60) return "just now";
  if (diff < 3600) return `${Math.floor(diff / 60)}m`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h`;
  return `${Math.floor(diff / 86400)}d`;
}

export function IssueRow({ issue }: { issue: Issue }) {
  const router = useRouter();
  return (
    <Pressable
      onPress={() => router.push(`/issue/${issue.id}`)}
      style={({ pressed }) => [styles.row, pressed && styles.pressed]}
    >
      <Text style={styles.glyph}>{statusGlyph(issue)}</Text>
      <View style={styles.body}>
        <Text style={styles.title} numberOfLines={1}>
          {issue.title || "(untitled)"}
        </Text>
        <Text style={styles.meta}>
          {issue.project} · {relativeTime(issue.frontmatter.created)}
        </Text>
      </View>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  row: { flexDirection: "row", padding: 12, borderBottomWidth: StyleSheet.hairlineWidth, borderColor: "#333" },
  pressed: { opacity: 0.6 },
  glyph: { width: 20, textAlign: "center", color: "#888" },
  body: { flex: 1, marginLeft: 8 },
  title: { fontSize: 16, color: "#eee" },
  meta: { fontSize: 12, color: "#888", marginTop: 2 },
});
```

- [ ] **Step 2: Verify build**

```bash
cd /home/daoleno/workspace/zen/app && npx tsc --noEmit
```

Expected: success.

- [ ] **Step 3: Commit**

```bash
git add app/components/issue/IssueRowNew.tsx
git commit -m "Add new IssueRow with frontmatter-derived status glyph"
```

---

### Task 18: Rewrite `(tabs)/issues.tsx` list screen

**Files:**
- Modify: `app/app/(tabs)/issues.tsx`

- [ ] **Step 1: Replace file contents**

```tsx
import React, { useMemo, useState } from "react";
import { View, Text, SectionList, Pressable, StyleSheet, TextInput, Modal, Alert } from "react-native";
import { useRouter } from "expo-router";
import { useIssues } from "@/store/issues";
import { IssueRow } from "@/components/issue/IssueRowNew";
import { wsClient } from "@/services/websocket";

type Section = { title: string; data: string[] };

export default function IssuesScreen() {
  const router = useRouter();
  const { state } = useIssues();
  const [creating, setCreating] = useState(false);
  const [newProject, setNewProject] = useState("inbox");

  const sections = useMemo(() => {
    const out: Section[] = [];
    for (const project of Object.keys(state.byProject).sort()) {
      const ids = state.byProject[project];
      const active = ids.filter((id) => !state.byId[id].frontmatter.done);
      const done = ids.filter((id) => !!state.byId[id].frontmatter.done);
      if (active.length) out.push({ title: `${project} · active`, data: active });
      if (done.length) out.push({ title: `${project} · done`, data: done });
    }
    return out;
  }, [state]);

  const createIssue = async () => {
    const req = {
      type: "write_issue",
      project: newProject,
      body: "# New issue\n\n",
      frontmatter: {},
      requestId: `new-${Date.now()}`,
    };
    wsClient.send(req);
    setCreating(false);
    // The server will respond with issue_written; we navigate after the reducer upserts it.
    // Simplest: route by requestId match once issue_written arrives. For now, rely on ISSUE_CHANGED.
  };

  return (
    <View style={styles.screen}>
      <View style={styles.header}>
        <Text style={styles.h1}>Issues</Text>
        <Pressable onPress={() => setCreating(true)} style={styles.addBtn}>
          <Text style={styles.addBtnText}>＋</Text>
        </Pressable>
      </View>

      <SectionList
        sections={sections}
        keyExtractor={(id) => id}
        renderItem={({ item }) => <IssueRow issue={state.byId[item]} />}
        renderSectionHeader={({ section }) => (
          <Text style={styles.sectionHeader}>{section.title}</Text>
        )}
        ListEmptyComponent={<Text style={styles.empty}>No issues yet. Tap ＋ to create one.</Text>}
      />

      <Modal visible={creating} animationType="slide" transparent>
        <View style={styles.modalBackdrop}>
          <View style={styles.modal}>
            <Text style={styles.modalTitle}>New issue</Text>
            <TextInput
              value={newProject}
              onChangeText={setNewProject}
              placeholder="Project (e.g., inbox, zen)"
              placeholderTextColor="#666"
              style={styles.input}
            />
            <View style={{ flexDirection: "row", justifyContent: "flex-end" }}>
              <Pressable onPress={() => setCreating(false)} style={styles.btn}>
                <Text style={styles.btnText}>Cancel</Text>
              </Pressable>
              <Pressable onPress={createIssue} style={[styles.btn, styles.btnPrimary]}>
                <Text style={[styles.btnText, styles.btnPrimaryText]}>Create</Text>
              </Pressable>
            </View>
          </View>
        </View>
      </Modal>
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: "#111" },
  header: { flexDirection: "row", alignItems: "center", padding: 16 },
  h1: { flex: 1, color: "#eee", fontSize: 22, fontWeight: "600" },
  addBtn: { width: 36, height: 36, borderRadius: 18, backgroundColor: "#222", alignItems: "center", justifyContent: "center" },
  addBtnText: { color: "#eee", fontSize: 22, lineHeight: 24 },
  sectionHeader: { padding: 8, backgroundColor: "#1a1a1a", color: "#888", fontSize: 12, textTransform: "uppercase" },
  empty: { color: "#666", textAlign: "center", marginTop: 48 },
  modalBackdrop: { flex: 1, backgroundColor: "rgba(0,0,0,0.6)", justifyContent: "center", padding: 16 },
  modal: { backgroundColor: "#1a1a1a", borderRadius: 12, padding: 16 },
  modalTitle: { color: "#eee", fontSize: 18, fontWeight: "600", marginBottom: 12 },
  input: { backgroundColor: "#222", color: "#eee", padding: 10, borderRadius: 8, marginBottom: 16 },
  btn: { padding: 10, marginLeft: 8 },
  btnText: { color: "#ccc" },
  btnPrimary: { backgroundColor: "#4a90e2", borderRadius: 8 },
  btnPrimaryText: { color: "#fff", fontWeight: "600" },
});
```

- [ ] **Step 2: Verify build**

```bash
cd /home/daoleno/workspace/zen/app && npx tsc --noEmit
```

Expected: success. (If the old `components/issue/IssueRow.tsx` is still being imported somewhere, rename the import or mark it for cleanup in Task 22.)

- [ ] **Step 3: Commit**

```bash
git add app/app/\(tabs\)/issues.tsx
git commit -m "Rewrite issues list screen as project-sectioned view"
```

---

### Task 19: `MentionPicker` component (TDD)

**Files:**
- Create: `app/components/issue/MentionPicker.tsx`
- Create: `app/components/issue/__tests__/MentionPicker.test.tsx`

- [ ] **Step 1: Write failing test**

```tsx
// MentionPicker.test.tsx
import React from "react";
import { render, fireEvent } from "@testing-library/react-native";
import { MentionPicker } from "../MentionPicker";

const candidates = [
  { kind: "role" as const, name: "claude" },
  { kind: "role" as const, name: "codex" },
  { kind: "session" as const, role: "claude", sessionId: "zen-claude-3", project: "zen" },
];

it("filters by prefix", () => {
  const onSelect = jest.fn();
  const { queryByText, getByText } = render(
    <MentionPicker candidates={candidates} query="cl" onSelect={onSelect} onDismiss={() => {}} />
  );
  expect(queryByText("@codex")).toBeNull();
  expect(getByText("@claude")).toBeTruthy();
});

it("calls onSelect with chosen candidate", () => {
  const onSelect = jest.fn();
  const { getByText } = render(
    <MentionPicker candidates={candidates} query="" onSelect={onSelect} onDismiss={() => {}} />
  );
  fireEvent.press(getByText("@codex"));
  expect(onSelect).toHaveBeenCalledWith({ kind: "role", name: "codex" });
});
```

- [ ] **Step 2: Run test to verify failure**

```bash
cd /home/daoleno/workspace/zen/app && npx jest components/issue/__tests__/MentionPicker.test.tsx
```

Expected: FAIL (module not found).

- [ ] **Step 3: Implement**

```tsx
import React from "react";
import { View, Text, Pressable, StyleSheet, FlatList } from "react-native";

export type MentionCandidate =
  | { kind: "role"; name: string }
  | { kind: "session"; role: string; sessionId: string; project: string };

export function MentionPicker({
  candidates,
  query,
  onSelect,
  onDismiss,
}: {
  candidates: MentionCandidate[];
  query: string;
  onSelect: (c: MentionCandidate) => void;
  onDismiss: () => void;
}) {
  const q = query.toLowerCase();
  const filtered = candidates.filter((c) => {
    const label = c.kind === "role" ? c.name : c.sessionId;
    return label.toLowerCase().startsWith(q);
  });

  if (filtered.length === 0) return null;

  return (
    <View style={styles.container}>
      <FlatList
        data={filtered}
        keyExtractor={(c) => (c.kind === "role" ? `r:${c.name}` : `s:${c.sessionId}`)}
        renderItem={({ item }) => (
          <Pressable onPress={() => onSelect(item)} style={styles.row}>
            <Text style={styles.label}>
              {item.kind === "role" ? `@${item.name}` : `@${item.role}#${item.sessionId}`}
            </Text>
            {item.kind === "session" && (
              <Text style={styles.subtext}>· {item.project}</Text>
            )}
          </Pressable>
        )}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: { maxHeight: 200, backgroundColor: "#1a1a1a", borderTopWidth: StyleSheet.hairlineWidth, borderColor: "#333" },
  row: { flexDirection: "row", padding: 10, alignItems: "center" },
  label: { color: "#eee", fontSize: 14 },
  subtext: { color: "#888", fontSize: 12, marginLeft: 8 },
});
```

- [ ] **Step 4: Run tests to verify pass**

```bash
cd /home/daoleno/workspace/zen/app && npx jest components/issue/__tests__/MentionPicker.test.tsx
```

Expected: 2 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add app/components/issue/MentionPicker.tsx app/components/issue/__tests__/MentionPicker.test.tsx
git commit -m "Add MentionPicker with prefix filter"
```

---

### Task 20: `MarkdownEditor` with mention detection (TDD)

**Files:**
- Create: `app/components/issue/MarkdownEditor.tsx`
- Create: `app/components/issue/__tests__/MarkdownEditor.test.tsx`

- [ ] **Step 1: Write failing test for `activeMention`**

```tsx
// MarkdownEditor.test.tsx
import { activeMention } from "../MarkdownEditor";

describe("activeMention", () => {
  it("detects @ at cursor", () => {
    expect(activeMention("hello @cl", 9)).toEqual({ query: "cl", start: 6 });
  });
  it("returns null when cursor not in a mention", () => {
    expect(activeMention("hello world", 5)).toBeNull();
  });
  it("ignores email-like @ usage", () => {
    expect(activeMention("ping user@host", 14)).toBeNull();
  });
});
```

- [ ] **Step 2: Run to verify failure**

```bash
cd /home/daoleno/workspace/zen/app && npx jest components/issue/__tests__/MarkdownEditor.test.tsx
```

Expected: FAIL.

- [ ] **Step 3: Implement `MarkdownEditor.tsx`**

```tsx
import React, { useState, useCallback, useRef } from "react";
import { View, TextInput, StyleSheet, NativeSyntheticEvent, TextInputSelectionChangeEventData } from "react-native";
import { MentionPicker, MentionCandidate } from "./MentionPicker";

/**
 * Inspect `value` at cursor position `pos` and return either an active mention
 * query (with the byte offset of the leading '@') or null. Used to drive the
 * picker.
 *
 * Rules:
 *   - The '@' must be at start of string or preceded by whitespace.
 *   - The query is the characters after '@' up to `pos`.
 *   - Only lowercase letters/digits/hyphen are allowed in the query; anything
 *     else before `pos` aborts the mention.
 */
export function activeMention(value: string, pos: number): { query: string; start: number } | null {
  let i = pos - 1;
  while (i >= 0) {
    const c = value[i];
    if (c === "@") {
      if (i === 0 || /\s/.test(value[i - 1])) {
        return { query: value.slice(i + 1, pos), start: i };
      }
      return null;
    }
    if (!/[a-z0-9-]/.test(c)) return null;
    i--;
  }
  return null;
}

export type MarkdownEditorProps = {
  value: string;
  onChange: (text: string) => void;
  candidates: MentionCandidate[];
  autoFocus?: boolean;
};

export function MarkdownEditor({ value, onChange, candidates, autoFocus }: MarkdownEditorProps) {
  const [selection, setSelection] = useState({ start: 0, end: 0 });
  const ref = useRef<TextInput>(null);

  const onSelectionChange = useCallback(
    (e: NativeSyntheticEvent<TextInputSelectionChangeEventData>) => setSelection(e.nativeEvent.selection),
    []
  );

  const mention = activeMention(value, selection.start);

  const onPickMention = (c: MentionCandidate) => {
    if (!mention) return;
    const insert = c.kind === "role" ? `@${c.name}` : `@${c.role}#${c.sessionId}`;
    const before = value.slice(0, mention.start);
    const after = value.slice(selection.start);
    const next = before + insert + after;
    onChange(next);
    // re-focus after state settles
    setTimeout(() => ref.current?.focus(), 10);
  };

  return (
    <View style={styles.container}>
      <TextInput
        ref={ref}
        value={value}
        onChangeText={onChange}
        onSelectionChange={onSelectionChange}
        multiline
        autoFocus={autoFocus}
        style={styles.input}
        placeholder="# Title\n\nWrite your issue..."
        placeholderTextColor="#555"
      />
      {mention && (
        <MentionPicker
          candidates={candidates}
          query={mention.query}
          onSelect={onPickMention}
          onDismiss={() => {}}
        />
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: "#111" },
  input: { flex: 1, color: "#eee", padding: 12, fontSize: 15, fontFamily: "Menlo", textAlignVertical: "top" },
});
```

- [ ] **Step 4: Run tests to verify pass**

```bash
cd /home/daoleno/workspace/zen/app && npx jest components/issue/__tests__/MarkdownEditor.test.tsx
```

Expected: 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add app/components/issue/MarkdownEditor.tsx app/components/issue/__tests__/MarkdownEditor.test.tsx
git commit -m "Add MarkdownEditor with inline mention picker"
```

---

### Task 21: Rewrite `app/issue/[id].tsx` detail screen

**Files:**
- Modify: `app/app/issue/[id].tsx`

- [ ] **Step 1: Replace file contents**

```tsx
import React, { useEffect, useMemo, useState } from "react";
import { View, Text, Pressable, StyleSheet, Alert } from "react-native";
import { useLocalSearchParams, useRouter } from "expo-router";
import { useIssues, Issue } from "@/store/issues";
import { MarkdownEditor } from "@/components/issue/MarkdownEditor";
import { MentionCandidate } from "@/components/issue/MentionPicker";
import { useAgents } from "@/store/agents";
import { wsClient } from "@/services/websocket";

export default function IssueDetail() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const router = useRouter();
  const { state } = useIssues();
  const { state: agentsState } = useAgents();
  const issue = id ? state.byId[id] : undefined;

  const [body, setBody] = useState(issue?.body ?? "");
  const [baseMtime, setBaseMtime] = useState(issue?.mtime ?? "");
  const [remoteBanner, setRemoteBanner] = useState(false);
  const dispatched = !!issue?.frontmatter.dispatched;
  const done = !!issue?.frontmatter.done;

  useEffect(() => {
    if (issue && issue.mtime !== baseMtime && issue.body !== body) {
      setRemoteBanner(true);
    }
  }, [issue?.mtime]);

  const candidates: MentionCandidate[] = useMemo(() => {
    const roles = state.executors.map<MentionCandidate>((name) => ({ kind: "role", name }));
    const sessions = agentsState.agents
      .filter((a) => a.project === issue?.project)
      .map<MentionCandidate>((a) => ({
        kind: "session",
        role: (a.command?.split(" ")[0]) ?? "agent",
        sessionId: a.id,
        project: a.project ?? "",
      }));
    return [...roles, ...sessions];
  }, [state.executors, agentsState.agents, issue?.project]);

  if (!issue) {
    return (
      <View style={styles.center}>
        <Text style={styles.muted}>Issue not found.</Text>
      </View>
    );
  }

  const save = () => {
    wsClient.send({
      type: "write_issue",
      id: issue.id,
      project: issue.project,
      path: issue.path,
      body,
      frontmatter: issue.frontmatter,
      baseMtime,
      requestId: `save-${issue.id}-${Date.now()}`,
    });
    setBaseMtime(new Date().toISOString());
  };

  const send = () => {
    save();
    wsClient.send({ type: "send_issue", id: issue.id, requestId: `send-${issue.id}` });
  };

  const redispatch = () => {
    Alert.alert("Redispatch", "Clear dispatch state and send again?", [
      { text: "Cancel", style: "cancel" },
      {
        text: "Redispatch",
        onPress: () => wsClient.send({ type: "redispatch_issue", id: issue.id, requestId: `redis-${issue.id}` }),
      },
    ]);
  };

  const toggleDone = () => {
    const next = done ? { ...issue.frontmatter, done: null } : { ...issue.frontmatter, done: new Date().toISOString() };
    wsClient.send({
      type: "write_issue",
      id: issue.id,
      project: issue.project,
      path: issue.path,
      body,
      frontmatter: next,
      baseMtime,
      requestId: `done-${issue.id}-${Date.now()}`,
    });
  };

  return (
    <View style={styles.screen}>
      <View style={styles.header}>
        <Pressable onPress={() => router.back()} style={styles.backBtn}>
          <Text style={styles.backText}>‹</Text>
        </Pressable>
        <Text style={styles.project}>{issue.project}</Text>
        <Pressable onPress={toggleDone} style={styles.doneBtn}>
          <Text style={styles.doneText}>{done ? "Reopen" : "Mark done"}</Text>
        </Pressable>
      </View>

      {remoteBanner && (
        <Pressable
          onPress={() => {
            setBody(issue.body);
            setBaseMtime(issue.mtime);
            setRemoteBanner(false);
          }}
          style={styles.banner}
        >
          <Text style={styles.bannerText}>Remote updated · tap to load changes</Text>
        </Pressable>
      )}

      <MarkdownEditor value={body} onChange={setBody} candidates={candidates} autoFocus />

      <View style={styles.footer}>
        <Pressable onPress={save} style={[styles.btn, styles.btnSecondary]}>
          <Text style={styles.btnText}>Save</Text>
        </Pressable>
        {dispatched ? (
          <Pressable onPress={redispatch} style={[styles.btn, styles.btnPrimary]}>
            <Text style={[styles.btnText, styles.btnPrimaryText]}>Redispatch</Text>
          </Pressable>
        ) : (
          <Pressable onPress={send} style={[styles.btn, styles.btnPrimary]}>
            <Text style={[styles.btnText, styles.btnPrimaryText]}>Send</Text>
          </Pressable>
        )}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: "#111" },
  center: { flex: 1, alignItems: "center", justifyContent: "center" },
  muted: { color: "#888" },
  header: { flexDirection: "row", alignItems: "center", padding: 12, borderBottomWidth: StyleSheet.hairlineWidth, borderColor: "#333" },
  backBtn: { width: 32 },
  backText: { color: "#eee", fontSize: 28, lineHeight: 28 },
  project: { color: "#aaa", flex: 1, textAlign: "center" },
  doneBtn: { padding: 6 },
  doneText: { color: "#4a90e2" },
  banner: { padding: 10, backgroundColor: "#2a3a4a" },
  bannerText: { color: "#eee", textAlign: "center" },
  footer: { flexDirection: "row", justifyContent: "flex-end", padding: 12, borderTopWidth: StyleSheet.hairlineWidth, borderColor: "#333" },
  btn: { padding: 12, marginLeft: 8, borderRadius: 8 },
  btnSecondary: { backgroundColor: "#222" },
  btnPrimary: { backgroundColor: "#4a90e2" },
  btnText: { color: "#ccc" },
  btnPrimaryText: { color: "#fff", fontWeight: "600" },
});
```

- [ ] **Step 2: Verify build**

```bash
cd /home/daoleno/workspace/zen/app && npx tsc --noEmit
```

Expected: success. If the `useAgents` hook has a different shape than assumed, adjust the `candidates` construction to match the actual agents store.

- [ ] **Step 3: Commit**

```bash
git add app/app/issue/\[id\].tsx
git commit -m "Rewrite issue detail screen: markdown editor, Send, Mark done"
```

---

### Task 22: Delete old app components and old tasks store

**Files:**
- Delete: all of `app/components/issue/*` except the new ones (`IssueRowNew.tsx`, `MentionPicker.tsx`, `MarkdownEditor.tsx`) and their tests.
- Delete: `app/store/tasks.tsx`
- Modify: `app/app/_layout.tsx` to remove old task listener registrations and the `TasksProvider`.
- Rename: `app/components/issue/IssueRowNew.tsx` → `app/components/issue/IssueRow.tsx`

- [ ] **Step 1: Delete old component files**

```bash
cd /home/daoleno/workspace/zen/app
rm components/issue/AssignIssueSheet.tsx
rm components/issue/AttachmentStack.tsx
rm components/issue/CreateIssueSheet.tsx
rm components/issue/DelegateRunSheet.tsx
rm components/issue/DueDatePicker.tsx
rm components/issue/IssueRow.tsx
rm components/issue/IssueStatusIcon.tsx
rm components/issue/PriorityBar.tsx
rm components/issue/PriorityPicker.tsx
rm components/issue/ProjectEditorSheet.tsx
rm components/issue/ProjectRow.tsx
rm components/issue/StatusFilterBar.tsx
rm components/issue/StatusPicker.tsx
rm components/issue/TaskPickerSheet.tsx
mv components/issue/IssueRowNew.tsx components/issue/IssueRow.tsx
```

- [ ] **Step 2: Update import in `(tabs)/issues.tsx`**

Edit `app/app/(tabs)/issues.tsx` and change `import { IssueRow } from "@/components/issue/IssueRowNew";` back to `import { IssueRow } from "@/components/issue/IssueRow";`.

- [ ] **Step 3: Delete `store/tasks.tsx`**

```bash
rm app/store/tasks.tsx
```

- [ ] **Step 4: Strip old listeners from `_layout.tsx`**

Remove every `wsClient.on("task_list", ...)`, `wsClient.on("task_created", ...)`, etc., and remove `<TasksProvider>` from the component tree. Any component still importing `useTasks` must be updated or removed.

- [ ] **Step 5: Verify build**

```bash
cd /home/daoleno/workspace/zen/app && npx tsc --noEmit
```

If there are lingering imports of deleted files, the compiler surfaces them — remove them one at a time until clean.

- [ ] **Step 6: Run app tests**

```bash
cd /home/daoleno/workspace/zen/app && npx jest
```

Expected: all tests pass (old tests for deleted components will also be gone if they lived next to the components; if any snapshot tests remain, delete them).

- [ ] **Step 7: Commit**

```bash
cd /home/daoleno/workspace/zen/app
git add -A components/issue store app/\(tabs\)/issues.tsx app/_layout.tsx
git commit -m "Delete old task components and store"
```

---

## Phase 3 — Daemon: remove the old task system

### Task 23: Strip old task message handlers from server

**Files:**
- Modify: `daemon/server/server.go`

- [ ] **Step 1: Remove old handler cases**

Delete these cases from the dispatch switch:
- `create_task`, `update_task`, `delete_task`
- `list_tasks`, `list_runs`, `create_run`
- `delegate_task`, `add_task_comment`
- Any other `task_*` or `run_*` cases.

And delete the handler method bodies (e.g., `handleCreateTask`, `handleListTasks`, etc.).

- [ ] **Step 2: Remove old broadcast calls**

Search for `task_created`, `task_updated`, `run_created`, `run_updated` and delete the code that emits them (usually fan-out goroutines at the end of `server.New`).

- [ ] **Step 3: Drop old store fields from Server struct and `server.New`**

Remove `tasks`, `runs`, `guidance`, `projects` fields from the `Server` struct.

Update `server.New` signature:

```go
func New(
    authManager *auth.Manager,
    w *watcher.Watcher,
    pusher *push.Pusher,
    sc *stats.Collector,
    issues *issue.Store,
    dispatcher *issue.Dispatcher,
    execs *issue.ExecutorConfig,
) *Server {
    // ...
}
```

- [ ] **Step 4: Verify daemon still builds**

```bash
cd /home/daoleno/workspace/zen/daemon && go build ./...
```

Will fail because `cmd/zen-daemon/main.go` still passes old stores. Next task.

- [ ] **Step 5: Commit (WIP — build will fail until Task 24)**

Do NOT commit yet — Task 24 is the matching main.go change. Continue to Task 24 immediately, then commit both together at the end of Task 24.

---

### Task 24: Strip old task init from daemon main + delete `daemon/task` package

**Files:**
- Modify: `daemon/cmd/zen-daemon/main.go`
- Delete: `daemon/task/`

- [ ] **Step 1: Remove task store initialization from main.go**

Delete these blocks in `runDaemon`:
- `taskStore, err := task.NewStore(stateDir)` block
- `runStore, err := task.NewRunStore(stateDir)` block
- `guidanceStore, err := task.NewGuidanceStore(stateDir)` block
- `projectStore, err := task.NewProjectStore(stateDir)` block

And remove the `task` import from the `import (...)` block:

```go
// remove this line:
"github.com/daoleno/zen/daemon/task"
```

Update the `server.New` call to match the new signature (only issues + dispatcher + execs):

```go
srv := server.New(
    authManager, w, pusher, sc,
    issueStore, dispatcher, execs,
)
```

- [ ] **Step 2: Delete the task package**

```bash
cd /home/daoleno/workspace/zen/daemon
rm -rf task/
```

- [ ] **Step 3: Verify build + tests**

```bash
cd /home/daoleno/workspace/zen/daemon && go build ./... && go test ./...
```

Expected: success, all remaining tests pass (issue package tests plus any untouched server tests that no longer reference task types).

If server tests still reference task types, delete the failing test file or rewrite it against the new message types.

- [ ] **Step 4: Commit**

```bash
cd /home/daoleno/workspace/zen
git add daemon/
git commit -m "Remove daemon/task package and old WebSocket handlers"
```

---

### Task 25: Clean up leftover state files on startup

**Files:**
- Modify: `daemon/cmd/zen-daemon/main.go`

- [ ] **Step 1: Add cleanup block**

In `runDaemon`, after resolving `stateDir`, add:

```go
// Remove stale files from the old task system. Non-fatal if absent.
for _, name := range []string{"tasks.json", "runs.json", "meta.json"} {
    _ = os.Remove(filepath.Join(stateDir, name))
}
```

Add imports if needed:

```go
import (
    "os"
    "path/filepath"
)
```

- [ ] **Step 2: Verify build**

```bash
cd /home/daoleno/workspace/zen/daemon && go build ./...
```

Expected: success.

- [ ] **Step 3: Commit**

```bash
git add daemon/cmd/zen-daemon/main.go
git commit -m "Clean up legacy task state files on daemon startup"
```

---

## Phase 4 — Verification

### Task 26: Final verification pass

- [ ] **Step 1: Daemon test suite**

```bash
cd /home/daoleno/workspace/zen/daemon && go test ./... -count=1
```

Expected: all tests pass, no skipped-or-broken test files referencing `task`.

- [ ] **Step 2: App type check + test suite**

```bash
cd /home/daoleno/workspace/zen/app && npx tsc --noEmit && npx jest
```

Expected: both succeed.

- [ ] **Step 3: App Android export build**

```bash
cd /home/daoleno/workspace/zen/app && npx expo export --platform android
```

Expected: build completes without errors referencing tasks, old components, or missing types.

- [ ] **Step 4: Manual end-to-end smoke test**

From a shell on the daemon host:

1. Start the daemon:
   ```bash
   cd /home/daoleno/workspace/zen/daemon && go run ./cmd/zen-daemon -addr 127.0.0.1:9876
   ```
2. Point the app at `ws://127.0.0.1:9876` (or whatever the existing connection flow uses).
3. In the app, tap `＋`, enter project `zen`, create the issue. The file should appear at `~/.zen/issues/zen/<slug>.md`.
4. Open the issue; type:
   ```
   # Smoke test

   @claude please print "hello from zen" and set done.
   ```
5. Tap **Send**. The issue row should show `▶` status glyph.
6. Verify `~/.zen/issues/zen/<slug>.md` now contains `dispatched:` and `agent_session:` in the frontmatter, and that a `tmux ls` shows a new session starting with `zen-claude-`.
7. The agent (claude) should open the file, read the task, respond in-file, then add `done: <ts>`.
8. Back in the app, the issue moves from Active to Done.
9. Manually clear the `done` line in the file — the issue returns to Active.
10. Delete the file with `rm` — the issue disappears from the app.

If all steps succeed, the redesign is complete.

- [ ] **Step 5: Write manual E2E script as a doc**

Capture the steps above into `docs/e2e-issues.md` as a living runbook.

```bash
git add docs/e2e-issues.md
git commit -m "Document issues E2E smoke test"
```

- [ ] **Step 6: Final sanity commit of any lingering cleanup**

Run `git status` — ensure the tree is clean and no orphan files remain. If any TypeScript unused-import warnings were silenced by `// @ts-ignore` during rewrites, revisit and remove them.

---

## Self-Review Checklist (filled in by author)

- **Spec coverage**
  - File layout — Task 8 (paths), Task 9 (scan).
  - Frontmatter shape — Tasks 3, 5.
  - Mentions + roles — Tasks 4, 11, 19.
  - Completion via `done` field — parser (Task 3), list grouping (Task 18), detail toggle (Task 21).
  - Project config — Task 6.
  - Executor config — Task 7, adapters (Task 12).
  - Dispatch semantics incl. redispatch — Task 11 + Task 14.
  - Watcher + debounce — Task 10.
  - Broadcast + snapshot — Task 15.
  - Mention picker UX — Tasks 19, 20.
  - Conflict handling — Task 9 (`ErrConflict`), Task 21 (banner).
  - Deletion plan — Phase 3.
  - Testing strategy — TDD in Tasks 3–11, UI tests in Tasks 19–20, manual script in Task 26.
  - No coverage for `IssueRow` unit test (simple presentational). Acceptable omission.
- **Placeholder scan**
  - No "TBD" / "TODO" / "similar to" references left.
  - Adapter types (Task 12) explicitly flag that real signatures must be verified — the plan gives the `grep` commands to confirm.
- **Type consistency**
  - `Frontmatter` fields (id, created, done, dispatched, agent_session) match between parser (Task 3), serializer (Task 5), store write (Task 9), and server handler (Task 14).
  - Message type names (`write_issue`, `issue_written`, `send_issue`, `issue_dispatched`, `redispatch_issue`, `issue_redispatched`, `delete_issue`, `issue_deleted_ack`, `issues_snapshot`, `issue_changed`, `issue_deleted`, `list_executors`, `executor_list`) are consistent between daemon server and app listener (Tasks 13–16).
  - `MentionCandidate` discriminator `kind: "role" | "session"` is consistent between `MentionPicker` (Task 19) and its consumer in `[id].tsx` (Task 21).
