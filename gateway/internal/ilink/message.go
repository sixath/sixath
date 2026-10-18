package ilink

import (
	"strings"
	"time"
)

const previewRunes = 40

// Inbound is a user message reduced to what the Gateway pipeline needs.
type Inbound struct {
	MessageID    string
	FromUserID   string
	GroupID      string
	ContextToken string
	// Text joins all text items; empty means the message carried no text (NonText).
	Text      string
	NonText   bool
	CreatedAt time.Time
}

// IsUserMessage filters out bot echoes (message_type 2).
func (m Message) IsUserMessage() bool { return m.MessageType == messageTypeUser }

// Normalize extracts text from item_list (type=1); messages without any non-blank text are NonText.
func Normalize(m Message) Inbound {
	var parts []string
	for _, it := range m.ItemList {
		if it.Type == itemTypeText && it.TextItem != nil && strings.TrimSpace(it.TextItem.Text) != "" {
			parts = append(parts, it.TextItem.Text)
		}
	}
	in := Inbound{
		MessageID:    messageIDString(m.MessageID),
		FromUserID:   strings.TrimSpace(m.FromUserID),
		GroupID:      strings.TrimSpace(m.GroupID),
		ContextToken: m.ContextToken,
		Text:         strings.TrimSpace(strings.Join(parts, "\n")),
	}
	in.NonText = in.Text == ""
	if m.CreateTimeMs > 0 {
		in.CreatedAt = time.UnixMilli(m.CreateTimeMs)
	}
	return in
}

// Preview is the roster preview: first 40 runes of the text, or a placeholder for non-text.
func (in Inbound) Preview() string {
	if in.NonText {
		return "[非文本消息]"
	}
	r := []rune(in.Text)
	if len(r) <= previewRunes {
		return in.Text
	}
	return string(r[:previewRunes])
}
