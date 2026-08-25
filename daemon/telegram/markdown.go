package telegram

import (
	"fmt"
	stdhtml "html"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extensionast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
	goldmarktext "github.com/yuin/goldmark/text"
)

type richText struct {
	Text     string
	Entities []MessageEntity
}

type richTextBuilder struct {
	source   []byte
	text     strings.Builder
	units    int
	entities []MessageEntity
	list     []*ast.List
}

var telegramMarkdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
)

func renderMarkdown(source string) richText {
	b := &richTextBuilder{source: []byte(source)}
	document := telegramMarkdown.Parser().Parse(goldmarktext.NewReader([]byte(source)))
	b.renderChildren(document)
	text := strings.TrimRight(b.text.String(), "\n")
	units := utf16Len(text)
	entities := b.entities[:0]
	for _, entity := range b.entities {
		if entity.Offset < units && entity.Length > 0 {
			if entity.Offset+entity.Length > units {
				entity.Length = units - entity.Offset
			}
			entities = append(entities, entity)
		}
	}
	return richText{Text: text, Entities: entities}
}

func (b *richTextBuilder) renderChildren(parent ast.Node) {
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		b.render(child)
	}
}

func (b *richTextBuilder) render(node ast.Node) {
	switch n := node.(type) {
	case *ast.Text:
		b.write(stdhtml.UnescapeString(string(n.Segment.Value(b.source))))
		if n.SoftLineBreak() || n.HardLineBreak() {
			b.write("\n")
		}
	case *ast.String:
		b.write(stdhtml.UnescapeString(string(n.Value)))
	case *ast.Emphasis:
		kind := "italic"
		if n.Level == 2 {
			kind = "bold"
		}
		b.withEntity(MessageEntity{Type: kind}, func() { b.renderChildren(n) })
	case *extensionast.Strikethrough:
		b.withEntity(MessageEntity{Type: "strikethrough"}, func() { b.renderChildren(n) })
	case *ast.CodeSpan:
		b.withEntity(MessageEntity{Type: "code"}, func() { b.renderChildren(n) })
	case *ast.Link:
		if goldmarkhtml.IsDangerousURL(n.Destination) {
			b.renderChildren(n)
		} else {
			b.withEntity(MessageEntity{Type: "text_link", URL: string(n.Destination)}, func() { b.renderChildren(n) })
		}
	case *ast.AutoLink:
		value := string(n.URL(b.source))
		b.withEntity(MessageEntity{Type: "text_link", URL: value}, func() { b.write(string(n.Label(b.source))) })
	case *ast.RawHTML:
		b.write(stdhtml.UnescapeString(string(n.Segments.Value(b.source))))
	case *ast.HTMLBlock:
		b.ensureBlockGap()
		b.write(stdhtml.UnescapeString(string(n.Text(b.source))))
		b.ensureLine()
	case *ast.CodeBlock:
		b.renderCodeBlock(n.Text(b.source), "")
	case *ast.FencedCodeBlock:
		language := ""
		if n.Info != nil {
			if fields := strings.Fields(string(n.Info.Text(b.source))); len(fields) > 0 {
				language = fields[0]
			}
		}
		b.renderCodeBlock(n.Text(b.source), language)
	case *ast.Heading:
		b.ensureBlockGap()
		b.withEntity(MessageEntity{Type: "bold"}, func() { b.renderChildren(n) })
		b.ensureBlockGap()
	case *ast.Paragraph:
		b.renderChildren(n)
		b.ensureBlockGap()
	case *ast.Blockquote:
		b.ensureBlockGap()
		b.withEntity(MessageEntity{Type: "blockquote"}, func() { b.renderChildren(n) })
		b.ensureBlockGap()
	case *ast.List:
		b.ensureLine()
		b.list = append(b.list, n)
		b.renderChildren(n)
		b.list = b.list[:len(b.list)-1]
		b.ensureBlockGap()
	case *ast.ListItem:
		marker := "- "
		if len(b.list) > 0 && b.list[len(b.list)-1].IsOrdered() {
			index := 0
			for sibling := n.PreviousSibling(); sibling != nil; sibling = sibling.PreviousSibling() {
				index++
			}
			marker = fmt.Sprintf("%d. ", b.list[len(b.list)-1].Start+index)
		}
		b.write(marker)
		b.renderChildren(n)
		b.ensureLine()
	case *extensionast.TaskCheckBox:
		if n.IsChecked {
			b.write("[x] ")
		} else {
			b.write("[ ] ")
		}
	case *extensionast.Table:
		b.ensureBlockGap()
		b.renderChildren(n)
		b.ensureBlockGap()
	case *extensionast.TableHeader, *extensionast.TableRow:
		b.renderChildren(n)
		b.ensureLine()
	case *extensionast.TableCell:
		if n.PreviousSibling() != nil {
			b.write(" | ")
		}
		b.renderChildren(n)
	case *ast.ThematicBreak:
		b.ensureBlockGap()
		b.write("---")
		b.ensureBlockGap()
	default:
		b.renderChildren(node)
	}
}

func (b *richTextBuilder) renderCodeBlock(value []byte, language string) {
	b.ensureBlockGap()
	b.withEntity(MessageEntity{Type: "pre", Language: language}, func() {
		b.write(strings.TrimSuffix(string(value), "\n"))
	})
	b.ensureBlockGap()
}

func (b *richTextBuilder) withEntity(entity MessageEntity, render func()) {
	entity.Offset = b.units
	render()
	entity.Length = b.units - entity.Offset
	if entity.Length > 0 {
		b.entities = append(b.entities, entity)
	}
}

func (b *richTextBuilder) write(value string) {
	b.text.WriteString(value)
	b.units += utf16Len(value)
}

func (b *richTextBuilder) ensureLine() {
	if b.text.Len() > 0 && !strings.HasSuffix(b.text.String(), "\n") {
		b.write("\n")
	}
}

func (b *richTextBuilder) ensureBlockGap() {
	if b.text.Len() == 0 {
		return
	}
	if strings.HasSuffix(b.text.String(), "\n\n") {
		return
	}
	if strings.HasSuffix(b.text.String(), "\n") {
		b.write("\n")
	} else {
		b.write("\n\n")
	}
}

func utf16Len(value string) int {
	units := 0
	for _, r := range value {
		units++
		if r > 0xffff {
			units++
		}
	}
	return units
}

func chunkRichText(value richText, maximum int) []richText {
	if maximum <= 0 || utf16Len(value.Text) <= maximum {
		return []richText{value}
	}
	type boundary struct {
		byteIndex int
		units     int
		r         rune
	}
	boundaries := []boundary{{}}
	units := 0
	for index, r := range value.Text {
		if index != 0 {
			boundaries = append(boundaries, boundary{byteIndex: index, units: units, r: r})
		}
		units += len(utf16.Encode([]rune{r}))
	}
	boundaries = append(boundaries, boundary{byteIndex: len(value.Text), units: units})

	var chunks []richText
	start := 0
	for start < len(boundaries)-1 {
		end := start + 1
		for end < len(boundaries) && boundaries[end].units-boundaries[start].units <= maximum {
			end++
		}
		end--
		preferred := end
		for i := end; end < len(boundaries)-1 && i > start; i-- {
			previous := runeBefore(value.Text, boundaries[i].byteIndex)
			if previous == '\n' {
				preferred = i
				if i > start+1 && runeBefore(value.Text, boundaries[i-1].byteIndex) == '\n' {
					break
				}
			} else if preferred == end && (previous == ' ' || previous == '\t') {
				preferred = i
			}
		}
		if preferred > start {
			end = preferred
		}
		startUnits, endUnits := boundaries[start].units, boundaries[end].units
		chunk := richText{Text: value.Text[boundaries[start].byteIndex:boundaries[end].byteIndex]}
		for _, entity := range value.Entities {
			entityEnd := entity.Offset + entity.Length
			overlapStart := max(entity.Offset, startUnits)
			overlapEnd := min(entityEnd, endUnits)
			if overlapStart < overlapEnd {
				copy := entity
				copy.Offset = overlapStart - startUnits
				copy.Length = overlapEnd - overlapStart
				chunk.Entities = append(chunk.Entities, copy)
			}
		}
		chunks = append(chunks, chunk)
		start = end
	}
	return chunks
}

func runeBefore(value string, byteIndex int) rune {
	if byteIndex <= 0 {
		return 0
	}
	r, _ := utf8.DecodeLastRuneInString(value[:byteIndex])
	return r
}
