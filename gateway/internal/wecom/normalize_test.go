package wecom

import (
	"strings"
	"testing"
)

func TestStripBotMention(t *testing.T) {
	got := StripBotMention("@小天才 帮我查", []string{"小天才"})
	if got != "帮我查" {
		t.Fatalf("StripBotMention with bot_names=%q", got)
	}

	got = StripBotMention("@SomeBot hello", nil)
	if got != "hello" {
		t.Fatalf("StripBotMention empty bot_names=%q want hello", got)
	}
}

func TestPeerID(t *testing.T) {
	if PeerID("group", "C1", "U1") != "chat:C1" {
		t.Fatal()
	}
	if PeerID("single", "", "U1") != "user:U1" {
		t.Fatal()
	}
}

func TestNormalizeMsgCallbackText(t *testing.T) {
	body := []byte(`{"msgid":"M1","aibotid":"BOT","chatid":"C1","chattype":"group","from":{"userid":"alice"},"msgtype":"text","text":{"content":"@小天才 今天天气如何"}}`)
	n, err := NormalizeMsgBody(body, NormalizeOpts{BotNames: []string{"小天才"}, BotID: "BOT"})
	if err != nil {
		t.Fatal(err)
	}
	if n.QuestionText != "今天天气如何" {
		t.Fatalf("QuestionText=%q", n.QuestionText)
	}
	wantRuntime := "[企微] 发起人=alice(alice)\n问题：今天天气如何"
	if n.RuntimeContent != wantRuntime {
		t.Fatalf("RuntimeContent=%q want %q", n.RuntimeContent, wantRuntime)
	}
	if n.PeerID != "chat:C1" {
		t.Fatalf("PeerID=%q", n.PeerID)
	}
}

func TestNormalizeMsgBodyBotIDMismatch(t *testing.T) {
	body := []byte(`{"msgid":"M1","aibotid":"OTHER","chatid":"C1","chattype":"group","from":{"userid":"alice"},"msgtype":"text","text":{"content":"hi"}}`)
	_, err := NormalizeMsgBody(body, NormalizeOpts{BotID: "BOT"})
	if err == nil {
		t.Fatal("expected error for aibotid mismatch")
	}
}

func TestFormatReplyCard(t *testing.T) {
	s := FormatReplyCard("alice", "今天天气如何", "晴")
	for _, want := range []string{"发起人：alice", "问题：今天天气如何", "晴"} {
		if !strings.Contains(s, want) {
			t.Fatalf("FormatReplyCard missing %q in %q", want, s)
		}
	}
}

func TestFormatFailureCard(t *testing.T) {
	s := FormatFailureCard("alice", "今天天气如何", "turn failed")
	for _, want := range []string{"发起人：alice", "问题：今天天气如何", "turn failed"} {
		if !strings.Contains(s, want) {
			t.Fatalf("FormatFailureCard missing %q in %q", want, s)
		}
	}
}
