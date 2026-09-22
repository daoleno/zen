package work

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	zenattachment "github.com/daoleno/zen/daemon/attachment"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var dshAttachmentsTag = regexp.MustCompile(`(?s)<zen_attachments>\s*(.*?)\s*</zen_attachments>`)

// Only files already admitted to the canonical Zen upload directory may become
// native image bytes. Provider-authored paths never acquire filesystem authority.
func dshPromptContent(text string) ([]map[string]any, error) {
	content := []map[string]any{{"type": "text", "text": text}}
	match := dshAttachmentsTag.FindStringSubmatch(text)
	if len(match) != 2 {
		return content, nil
	}
	var envelope zenattachment.Envelope
	if err := json.Unmarshal([]byte(match[1]), &envelope); err != nil {
		return nil, fmt.Errorf("invalid attachment envelope")
	}
	if len(envelope.Files) > 8 {
		return nil, fmt.Errorf("at most eight attachments are supported")
	}
	root := filepath.Join(filepath.Dir(filepath.Dir(DSHRoot())), "uploads")
	total := 0
	for _, attachment := range envelope.Files {
		relative, err := filepath.Rel(root, attachment.Path)
		if err != nil || !filepath.IsLocal(relative) {
			return nil, fmt.Errorf("attachment is outside the Session upload owner")
		}
		file, err := os.OpenInRoot(root, relative)
		if err != nil {
			return nil, err
		}
		header := make([]byte, 512)
		n, _ := file.Read(header)
		mime := http.DetectContentType(header[:n])
		if !strings.HasPrefix(mime, "image/") {
			file.Close()
			continue
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > 5<<20 {
			file.Close()
			return nil, fmt.Errorf("DSH native image intake is limited to 5 MiB per image")
		}
		_, err = file.Seek(0, io.SeekStart)
		if err != nil {
			file.Close()
			return nil, err
		}
		bytes, err := io.ReadAll(io.LimitReader(file, (5<<20)+1))
		file.Close()
		if err != nil {
			return nil, err
		}
		total += len(bytes)
		if len(bytes) > 5<<20 || total > 40<<20 {
			return nil, fmt.Errorf("DSH image intake exceeds its byte limit")
		}
		content = append(content, map[string]any{"type": "image", "mediaType": mime, "data": base64.StdEncoding.EncodeToString(bytes), "name": attachment.Name})
	}
	return content, nil
}

// Reopened history uses the harness-owned immutable image, while native admission
// still hashes the exact original text accepted from Zen.
func dshUserContent(blocks []dshBlock) (display, exact string) {
	var texts []string
	for _, block := range blocks {
		if block.Type == "text" {
			texts = append(texts, block.Text)
		}
	}
	exact = strings.Join(texts, "\n")
	match := dshAttachmentsTag.FindStringSubmatch(exact)
	if len(match) != 2 {
		return dshContentText(blocks), exact
	}
	var envelope zenattachment.Envelope
	if json.Unmarshal([]byte(match[1]), &envelope) != nil {
		return dshContentText(blocks), exact
	}
	used := map[int]bool{}
	for index, file := range envelope.Files {
		for imageIndex, block := range blocks {
			if used[imageIndex] || block.Type != "image" || block.Attachment.ID == "" || block.Attachment.Name != file.Name {
				continue
			}
			envelope.Files[index].Path = "dsh-attachment:" + block.Attachment.ID
			envelope.Files[index].ContentType = block.Attachment.MediaType
			used[imageIndex] = true
			break
		}
	}
	data, _ := json.Marshal(envelope)
	display = strings.Replace(exact, match[0], "<zen_attachments>"+string(data)+"</zen_attachments>", 1)
	for index, block := range blocks {
		if block.Type == "image" && !used[index] {
			display += "\n" + dshContentText([]dshBlock{block})
		}
	}
	return display, exact
}
