package model

import (
	"strings"
	"unicode/utf8"
)

// SanitizeMessageContent 清理单条 message.content（L1 / OpenAI 兼容网关）：非法 UTF-8、NUL、除 \n\r\t 外的 ASCII 控制符、
// U+2028/U+2029。与 openAI 路径 patch 前一致（设计 §5.1 L1）。
func SanitizeMessageContent(s string) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == 0:
			continue
		case r < 32 && r != '\n' && r != '\r' && r != '\t':
			b.WriteRune(' ')
		case r == '\u2028' || r == '\u2029':
			b.WriteRune('\n')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ApplyL1SanitizeToMessages 对每条消息的文本与多模态 text 部分做 L1 清理（设计 §5.3 顺序第 1 步）。changed 为被改写过的消息条数。
func ApplyL1SanitizeToMessages(msgs []Message) ([]Message, int) {
	if len(msgs) == 0 {
		return msgs, 0
	}
	changed := 0
	out := make([]Message, len(msgs))
	for i := range msgs {
		m := msgs[i]
		before := m.Content
		m.Content = SanitizeMessageContent(m.Content)
		if before != m.Content {
			changed++
		}
		if len(m.Parts) > 0 {
			parts := make([]ContentPart, len(m.Parts))
			copy(parts, m.Parts)
			for j := range parts {
				if parts[j].Type == ContentTypeText {
					bt := parts[j].Text
					parts[j].Text = SanitizeMessageContent(parts[j].Text)
					if bt != parts[j].Text {
						changed++
					}
				}
			}
			m.Parts = parts
		}
		if strings.EqualFold(m.Role, "assistant") && hasToolCallsMeta(m) && strings.TrimSpace(m.Content) == "" {
			m.Content = " "
		}
		if strings.EqualFold(m.Role, "tool") && strings.TrimSpace(m.Content) == "" {
			m.Content = `{"error":"empty tool message"}`
		}
		out[i] = m
	}
	return out, changed
}

// TruncateMessageRunes 将 content 截断到至多 maxRunes 个 Unicode 码点（suffix 不计入预算）；maxRunes<=0 时不修改。
func TruncateMessageRunes(content string, maxRunes int, suffix string) string {
	if maxRunes <= 0 {
		return content
	}
	sufN := utf8.RuneCountInString(suffix)
	budget := maxRunes - sufN
	if budget < 1 {
		budget = 1
	}
	runes := []rune(content)
	if len(runes) <= budget {
		return content
	}
	return string(runes[:budget]) + suffix
}
