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
		t.Fatalf("ParseFile: %v", err)
	}
	if iss.Frontmatter.ID != "01HZ5K8J9X" {
		t.Fatalf("id = %q", iss.Frontmatter.ID)
	}
	if iss.Frontmatter.Done != nil {
		t.Fatalf("done = %v, want nil", iss.Frontmatter.Done)
	}
	if iss.Title != "Hello" {
		t.Fatalf("title = %q", iss.Title)
	}
	if !strings.Contains(iss.Body, "Body line.") {
		t.Fatalf("body = %q", iss.Body)
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
		t.Fatalf("ParseFile: %v", err)
	}
	if iss.Frontmatter.Done == nil {
		t.Fatal("done should be set")
	}
	want := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
	if !iss.Frontmatter.Done.Equal(want) {
		t.Fatalf("done = %v, want %v", iss.Frontmatter.Done, want)
	}
}

func TestParseFile_DoneMissingIsActive(t *testing.T) {
	src := `---
id: a
created: 2026-04-21T00:00:00Z
---
Body
`

	iss, err := ParseFile("/tmp/x.md", []byte(src), time.Now())
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if iss.Frontmatter.Done != nil {
		t.Fatalf("done = %v, want nil", iss.Frontmatter.Done)
	}
}

func TestParseFile_NoFrontmatterError(t *testing.T) {
	if _, err := ParseFile("/tmp/x.md", []byte("just a body"), time.Now()); err == nil {
		t.Fatal("expected error for missing frontmatter")
	}
}

func TestParseFile_TitleFallbackFirstNonEmptyLine(t *testing.T) {
	src := `---
id: a
created: 2026-04-21T00:00:00Z
---

Plain text first line, no heading.
`

	iss, err := ParseFile("/tmp/x.md", []byte(src), time.Now())
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if iss.Title != "Plain text first line, no heading." {
		t.Fatalf("title = %q", iss.Title)
	}
}

func TestParseFile_ExtraFieldsPreserved(t *testing.T) {
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
		t.Fatalf("ParseFile: %v", err)
	}
	if iss.Frontmatter.Dispatched == nil {
		t.Fatal("dispatched should be set")
	}
	if iss.Frontmatter.AgentSession != "zen-claude-3" {
		t.Fatalf("agent_session = %q", iss.Frontmatter.AgentSession)
	}
	if _, ok := iss.Frontmatter.Extra["labels"]; !ok {
		t.Fatalf("extra = %#v, want labels", iss.Frontmatter.Extra)
	}
}

func TestExtractMentions_RoleOnly(t *testing.T) {
	got := ExtractMentions("Hey @claude please look at this")
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Role != "claude" || got[0].Session != "" {
		t.Fatalf("mention = %+v", got[0])
	}
}

func TestExtractMentions_RoleAndSession(t *testing.T) {
	got := ExtractMentions("Try @claude#zen-claude-3 for this one")
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Role != "claude" || got[0].Session != "zen-claude-3" {
		t.Fatalf("mention = %+v", got[0])
	}
}

func TestExtractMentions_IgnoresEmail(t *testing.T) {
	got := ExtractMentions("ping me at user@host.com, but actually @codex handle it")
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Role != "codex" {
		t.Fatalf("mention = %+v", got[0])
	}
}

func TestExtractMentions_MultiplePreservesOrder(t *testing.T) {
	got := ExtractMentions("First @claude then @codex")
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Role != "claude" || got[1].Role != "codex" {
		t.Fatalf("mentions = %+v", got)
	}
	if got[0].Index >= got[1].Index {
		t.Fatalf("indices = %d, %d", got[0].Index, got[1].Index)
	}
}

func TestExtractMentions_StartOfLine(t *testing.T) {
	got := ExtractMentions("@claude fix this")
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Role != "claude" || got[0].Index != 0 {
		t.Fatalf("mention = %+v", got[0])
	}
}

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
		t.Fatalf("ParseFile: %v", err)
	}
	out, err := SerializeIssue(iss)
	if err != nil {
		t.Fatalf("SerializeIssue: %v", err)
	}
	reparsed, err := ParseFile("/tmp/x.md", out, time.Now())
	if err != nil {
		t.Fatalf("ParseFile(reparsed): %v", err)
	}
	if reparsed.Frontmatter.ID != iss.Frontmatter.ID {
		t.Fatal("id lost")
	}
	if reparsed.Frontmatter.Done == nil || !reparsed.Frontmatter.Done.Equal(*iss.Frontmatter.Done) {
		t.Fatalf("done lost: %#v", reparsed.Frontmatter.Done)
	}
	if reparsed.Frontmatter.AgentSession != iss.Frontmatter.AgentSession {
		t.Fatalf("agent_session = %q", reparsed.Frontmatter.AgentSession)
	}
	if reparsed.Body != iss.Body {
		t.Fatalf("body = %q, want %q", reparsed.Body, iss.Body)
	}
}

func TestSerializeIssue_EmitsEmptyDoneField(t *testing.T) {
	iss := &Issue{
		Frontmatter: Frontmatter{
			ID:      "a",
			Created: time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC),
		},
		Body: "body",
	}
	out, err := SerializeIssue(iss)
	if err != nil {
		t.Fatalf("SerializeIssue: %v", err)
	}
	reparsed, err := ParseFile("/tmp/x.md", out, time.Now())
	if err != nil {
		t.Fatalf("ParseFile(reparsed): %v", err)
	}
	if reparsed.Frontmatter.Done != nil {
		t.Fatalf("done = %v, want nil", reparsed.Frontmatter.Done)
	}
}
