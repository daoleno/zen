package telegram

import (
	"strings"
	"testing"
)

func TestRenderMarkdownTelegramRichText(t *testing.T) {
	source := "# Heading\n\n**bold and *nested*** plus ~~gone~~ and [Zen](https://zen.example).\n\n" +
		"`inline`\n\n> quoted **strong**\n\n- first\n- [x] done\n- [ ] pending\n\n" +
		"| Name | State |\n| --- | --- |\n| Zen | ready |\n\n```go\nfmt.Println(\"hi\")\n```\n\n" +
		"    indented code\n\n<span>visible &amp; safe</span>\n\nemoji 🧠"
	rendered := renderMarkdown(source)

	for _, visible := range []string{
		"Heading", "bold and nested", "gone", "Zen", "inline", "quoted strong",
		"- first", "- [x] done", "- [ ] pending", "Name | State", "Zen | ready",
		"fmt.Println", "indented code", "<span>visible & safe</span>", "emoji 🧠",
	} {
		if !strings.Contains(rendered.Text, visible) {
			t.Errorf("rendered text missing %q:\n%s", visible, rendered.Text)
		}
	}
	wantTypes := map[string]bool{"bold": false, "italic": false, "strikethrough": false, "text_link": false, "code": false, "pre": false, "blockquote": false}
	for _, entity := range rendered.Entities {
		if _, found := wantTypes[entity.Type]; found {
			wantTypes[entity.Type] = true
		}
		if entity.Offset < 0 || entity.Length <= 0 || entity.Offset+entity.Length > utf16Len(rendered.Text) {
			t.Fatalf("invalid entity %+v for %d UTF-16 units", entity, utf16Len(rendered.Text))
		}
		if entity.Type == "text_link" && entity.URL != "https://zen.example" {
			t.Fatalf("link=%+v", entity)
		}
		if entity.Type == "pre" && strings.Contains(rendered.Text, "fmt.Println") && entity.Language != "" && entity.Language != "go" {
			t.Fatalf("pre language=%q", entity.Language)
		}
	}
	for kind, found := range wantTypes {
		if !found {
			t.Errorf("missing %s entity: %+v", kind, rendered.Entities)
		}
	}
}

func TestRenderMarkdownMalformedIsReadable(t *testing.T) {
	rendered := renderMarkdown("Unclosed **bold and [link](\n\n<broken")
	if strings.TrimSpace(rendered.Text) == "" || !strings.Contains(rendered.Text, "Unclosed") || !strings.Contains(rendered.Text, "broken") {
		t.Fatalf("malformed Markdown was lost: %q", rendered.Text)
	}
}

func TestRenderMarkdownDoesNotCreateUnsafeLinkEntity(t *testing.T) {
	rendered := renderMarkdown("[visible](javascript:alert(1))")
	if rendered.Text != "visible" || len(rendered.Entities) != 0 {
		t.Fatalf("unsafe link was not reduced to plain visible text: %+v", rendered)
	}
}

func TestRenderMarkdownEntityOffsetsUseUTF16(t *testing.T) {
	rendered := renderMarkdown("🧠 **bold *deep***")
	var bold, italic *MessageEntity
	for index := range rendered.Entities {
		switch rendered.Entities[index].Type {
		case "bold":
			bold = &rendered.Entities[index]
		case "italic":
			italic = &rendered.Entities[index]
		}
	}
	if rendered.Text != "🧠 bold deep" || bold == nil || italic == nil {
		t.Fatalf("rendered=%+v", rendered)
	}
	if bold.Offset != 3 || bold.Length != 9 || italic.Offset != 8 || italic.Length != 4 {
		t.Fatalf("UTF-16 entities: bold=%+v italic=%+v", *bold, *italic)
	}
}

func TestChunkRichTextUsesUTF16AndClipsEntities(t *testing.T) {
	exact := richText{Text: strings.Repeat("🧠", 2048), Entities: []MessageEntity{{Type: "bold", Offset: 0, Length: 4096}}}
	chunks := chunkRichText(exact, 4096)
	if len(chunks) != 1 || utf16Len(chunks[0].Text) != 4096 || len(chunks[0].Entities) != 1 {
		t.Fatalf("exact chunks=%+v", chunks)
	}

	overflow := richText{Text: strings.Repeat("🧠", 2049), Entities: []MessageEntity{{Type: "bold", Offset: 0, Length: 4098}}}
	chunks = chunkRichText(overflow, 4096)
	if len(chunks) != 2 || utf16Len(chunks[0].Text) != 4096 || utf16Len(chunks[1].Text) != 2 {
		t.Fatalf("overflow chunk lengths=%d/%d chunks=%d", utf16Len(chunks[0].Text), utf16Len(chunks[1].Text), len(chunks))
	}
	if chunks[0].Entities[0].Offset != 0 || chunks[0].Entities[0].Length != 4096 ||
		chunks[1].Entities[0].Offset != 0 || chunks[1].Entities[0].Length != 2 {
		t.Fatalf("clipped entities=%+v", chunks)
	}
}

func TestChunkRichTextDuplicatesLinkAcrossReadableBoundary(t *testing.T) {
	value := richText{Text: "alpha beta gamma", Entities: []MessageEntity{{Type: "text_link", Offset: 0, Length: 16, URL: "https://zen.example"}}}
	chunks := chunkRichText(value, 10)
	if len(chunks) != 2 || chunks[0].Text != "alpha " || chunks[1].Text != "beta gamma" {
		t.Fatalf("chunks=%+v", chunks)
	}
	for _, chunk := range chunks {
		if len(chunk.Entities) != 1 || chunk.Entities[0].URL != "https://zen.example" || chunk.Entities[0].Length != utf16Len(chunk.Text) {
			t.Fatalf("link not duplicated/clipped: %+v", chunk)
		}
	}
}
