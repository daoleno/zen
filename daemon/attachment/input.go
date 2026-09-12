package attachment

import (
	"encoding/json"
	"strings"
)

// Entity offsets are UTF-16 units in Text, never in the serialized envelope.
type Entity struct {
	Type          string      `json:"type"`
	Offset        int         `json:"offset"`
	Length        int         `json:"length"`
	URL           string      `json:"url,omitempty"`
	Language      string      `json:"language,omitempty"`
	CustomEmojiID string      `json:"custom_emoji_id,omitempty"`
	User          *EntityUser `json:"user,omitempty"`
}

type EntityUser struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
}

type Caption struct {
	Text     string   `json:"text"`
	Entities []Entity `json:"entities,omitempty"`
}

// Envelope extends the existing mobile zen_attachments contract. Providers
// receive local file references through their normal receipt-owned input.
type Envelope struct {
	Files    []File    `json:"files"`
	Captions []Caption `json:"captions,omitempty"`
}

func Input(body string, files []File, captions []Caption) string {
	if len(files) == 0 && len(captions) == 0 {
		return body
	}
	raw, _ := json.Marshal(Envelope{Files: files, Captions: captions})
	return strings.TrimSpace(body) + "\n\n<zen_attachments>" + string(raw) + "</zen_attachments>"
}
