package wecom

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// NormalizeOpts configures parsing of aibot_msg_callback body JSON.
type NormalizeOpts struct {
	BotNames []string
	BotID    string
}

// Normalized is the Gateway-internal view of an inbound text message.
type Normalized struct {
	MsgID          string
	PeerID         string
	AskerID        string
	AskerName      string
	QuestionText   string
	RuntimeContent string
	ChatID         string
	ChatType       string
}

type msgCallbackBody struct {
	MsgID    string `json:"msgid"`
	AibotID  string `json:"aibotid"`
	ChatID   string `json:"chatid"`
	ChatType string `json:"chattype"`
	From     struct {
		UserID string `json:"userid"`
	} `json:"from"`
	MsgType string `json:"msgtype"`
	Text    struct {
		Content string `json:"content"`
	} `json:"text"`
}

var genericMentionPrefix = regexp.MustCompile(`^@\S+\s*`)

// StripBotMention removes @bot prefixes from message text.
func StripBotMention(content string, botNames []string) string {
	s := strings.TrimSpace(content)
	if len(botNames) == 0 {
		s = genericMentionPrefix.ReplaceAllString(s, "")
		return strings.TrimSpace(s)
	}
	for _, name := range botNames {
		prefix := "@" + name
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimSpace(s[len(prefix):])
		}
	}
	return strings.TrimSpace(s)
}

// PeerID returns the session peer for group or single chat.
func PeerID(chatType, chatID, userID string) string {
	if chatType == "group" {
		return "chat:" + chatID
	}
	return "user:" + userID
}

// FormatRuntimeContent builds the text injected into Runtime turns.
func FormatRuntimeContent(askerName, askerID, question string) string {
	return fmt.Sprintf("[企微] 发起人=%s(%s)\n问题：%s", askerName, askerID, question)
}

// NormalizeMsgBody parses aibot_msg_callback body JSON into Normalized fields.
func NormalizeMsgBody(body []byte, opts NormalizeOpts) (Normalized, error) {
	var raw msgCallbackBody
	if err := json.Unmarshal(body, &raw); err != nil {
		return Normalized{}, fmt.Errorf("parse msg body: %w", err)
	}
	if opts.BotID != "" && raw.AibotID != opts.BotID {
		return Normalized{}, fmt.Errorf("aibotid mismatch: got %q want %q", raw.AibotID, opts.BotID)
	}
	if raw.MsgType != "text" {
		return Normalized{}, fmt.Errorf("unsupported msgtype %q", raw.MsgType)
	}

	askerID := raw.From.UserID
	askerName := askerID
	question := StripBotMention(raw.Text.Content, opts.BotNames)

	n := Normalized{
		MsgID:          raw.MsgID,
		PeerID:         PeerID(raw.ChatType, raw.ChatID, askerID),
		AskerID:        askerID,
		AskerName:      askerName,
		QuestionText:   question,
		RuntimeContent: FormatRuntimeContent(askerName, askerID, question),
		ChatID:         raw.ChatID,
		ChatType:       raw.ChatType,
	}
	return n, nil
}
