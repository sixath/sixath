package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestLoadStateMissingFileReturnsEmpty(t *testing.T) {
	st, err := loadState(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if st.Peers == nil || st.BotToken != "" {
		t.Fatalf("got %+v", st)
	}
}

func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "a.json")
	st := &State{BotToken: "bt", BotID: "b@im.bot", Cursor: "c1", Peers: map[string]*PeerState{}}
	st.recordInbound(Message{
		FromUserID:   "u@im.wechat",
		MessageType:  messageTypeUser,
		ContextToken: "ctx-1",
		ItemList:     []MessageItem{{Type: itemTypeText, TextItem: &TextItem{Text: "hello"}}},
	}, time.Unix(100, 0))
	if err := st.save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := loadState(path)
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	p := got.Peers["u@im.wechat"]
	if got.Cursor != "c1" || p == nil || p.ContextToken != "ctx-1" || p.LastText != "hello" || p.BotIDAtToken != "b@im.bot" {
		t.Fatalf("got %+v peer %+v", got, p)
	}
}

func TestRecordInboundIgnoresBotMessagesAndKeepsTokenWhenMissing(t *testing.T) {
	st := &State{BotID: "b", Peers: map[string]*PeerState{}}
	st.recordInbound(Message{FromUserID: "u", MessageType: messageTypeUser, ContextToken: "ctx-1"}, time.Unix(1, 0))
	st.recordInbound(Message{FromUserID: "u", MessageType: messageTypeUser}, time.Unix(2, 0))
	st.recordInbound(Message{FromUserID: "b", MessageType: messageTypeBot, ContextToken: "x"}, time.Unix(3, 0))
	if st.Peers["u"].ContextToken != "ctx-1" {
		t.Fatalf("token overwritten: %+v", st.Peers["u"])
	}
	if _, ok := st.Peers["b"]; ok {
		t.Fatal("bot message must not be recorded as peer")
	}
}

func TestBuildLengthTextASCII(t *testing.T) {
	for _, n := range []int{10, 500, 1800, 4000} {
		s := buildLengthText(n, false)
		if got := utf8.RuneCountInString(s); got != n {
			t.Fatalf("n=%d rune count = %d", n, got)
		}
		if n >= 100 && !strings.HasSuffix(s, "【END】") {
			t.Fatalf("n=%d missing END marker", n)
		}
	}
}

func TestBuildLengthTextChinese(t *testing.T) {
	s := buildLengthText(1000, true)
	if got := utf8.RuneCountInString(s); got != 1000 {
		t.Fatalf("rune count = %d", got)
	}
	if len(s) < 2500 {
		t.Fatalf("byte length = %d, want ~3 bytes per rune", len(s))
	}
	if !strings.Contains(s, "测") || !strings.HasSuffix(s, "【END】") {
		t.Fatalf("unexpected content: %q", s[:60])
	}
}
