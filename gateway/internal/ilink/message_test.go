package ilink

import (
	"encoding/json"
	"strings"
	"testing"
)

func decodeMessage(t *testing.T, raw string) Message {
	t.Helper()
	var m Message
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestNormalizeTextItems(t *testing.T) {
	m := decodeMessage(t, `{"message_id":"m-1","from_user_id":" u@im.wechat ","message_type":1,"context_token":"ctx","item_list":[{"type":1,"text_item":{"text":"第一段"}},{"type":2},{"type":1,"text_item":{"text":"第二段"}}]}`)
	in := Normalize(m)
	if in.MessageID != "m-1" || in.FromUserID != "u@im.wechat" || in.Text != "第一段\n第二段" || in.NonText {
		t.Fatalf("inbound = %+v", in)
	}
	if !in.CreatedAt.IsZero() {
		t.Fatalf("created_at without create_time_ms = %v", in.CreatedAt)
	}
}

func TestNormalizeNonText(t *testing.T) {
	m := decodeMessage(t, `{"message_id":1,"from_user_id":"u","message_type":1,"item_list":[{"type":2}]}`)
	in := Normalize(m)
	if !in.NonText || in.Text != "" || in.Preview() != "[非文本消息]" {
		t.Fatalf("inbound = %+v preview=%q", in, in.Preview())
	}
	blank := Normalize(decodeMessage(t, `{"message_id":2,"from_user_id":"u","message_type":1,"item_list":[{"type":1,"text_item":{"text":"  "}}]}`))
	if !blank.NonText {
		t.Fatalf("blank text should be NonText: %+v", blank)
	}
}

func TestNormalizeGroupAndBotMessage(t *testing.T) {
	m := decodeMessage(t, `{"message_id":3,"from_user_id":"u","group_id":"g1","message_type":2,"item_list":[]}`)
	if m.IsUserMessage() {
		t.Fatal("message_type 2 is a bot message")
	}
	if Normalize(m).GroupID != "g1" {
		t.Fatal("group id lost")
	}
}

func TestPreviewTruncatesTo40Runes(t *testing.T) {
	in := Inbound{Text: strings.Repeat("字", 50)}
	if got := []rune(in.Preview()); len(got) != 40 {
		t.Fatalf("preview runes = %d", len(got))
	}
}
