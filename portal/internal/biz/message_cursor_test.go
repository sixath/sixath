package biz

import (
	"testing"
	"time"
)

func TestMessageCursor_RoundTrip(t *testing.T) {
	created := time.Date(2026, 9, 12, 10, 30, 15, 123456789, time.UTC)
	original := MessageCursor{CreatedAt: created, ID: "msg-abc-123"}

	encoded := original.Encode()
	if encoded == "" {
		t.Fatal("non-empty cursor must encode to a non-empty string")
	}

	decoded, err := DecodeMessageCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeMessageCursor(%q): %v", encoded, err)
	}
	if !decoded.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt=%s want %s", decoded.CreatedAt, created)
	}
	if decoded.ID != original.ID {
		t.Fatalf("ID=%q want %q", decoded.ID, original.ID)
	}
}

func TestMessageCursor_LocalTimezoneNormalised(t *testing.T) {
	shanghai := time.FixedZone("CST", 8*3600)
	created := time.Date(2026, 9, 12, 18, 0, 0, 0, shanghai)

	decoded, err := DecodeMessageCursor(MessageCursor{CreatedAt: created, ID: "m1"}.Encode())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !decoded.CreatedAt.Equal(created) {
		t.Fatalf("instant changed: got %s want %s", decoded.CreatedAt, created)
	}
}

func TestMessageCursor_EmptyMeansLatestPage(t *testing.T) {
	if got := (MessageCursor{}).Encode(); got != "" {
		t.Fatalf("zero cursor must encode to empty string, got %q", got)
	}
	decoded, err := DecodeMessageCursor("")
	if err != nil {
		t.Fatalf("empty cursor must be accepted: %v", err)
	}
	if decoded.ID != "" || !decoded.CreatedAt.IsZero() {
		t.Fatalf("empty cursor decoded to %+v", decoded)
	}
	if decoded, err := DecodeMessageCursor("   "); err != nil || decoded.ID != "" {
		t.Fatalf("whitespace cursor must be treated as empty: %+v err=%v", decoded, err)
	}
}

func TestDecodeMessageCursor_RejectsGarbage(t *testing.T) {
	cases := []string{
		"!!!not-base64!!!",
		"aGVsbG8",                      // base64 of "hello"：缺少分隔符
		"bm90LWEtdGltZXxtMQ",           // "not-a-time|m1"：时间不可解析
		"MjAyNi0wOS0xMlQxMDowMDowMFp8", // 有分隔符但 id 为空
	}
	for _, raw := range cases {
		if _, err := DecodeMessageCursor(raw); err == nil {
			t.Fatalf("DecodeMessageCursor(%q) must fail", raw)
		}
	}
}

func TestNormalizeMessagePageSize(t *testing.T) {
	tests := []struct {
		in, want int
	}{
		{0, DefaultMessagePageSize},
		{-5, DefaultMessagePageSize},
		{10, 10},
		{MaxMessagePageSize, MaxMessagePageSize},
		{MaxMessagePageSize + 1, MaxMessagePageSize},
		{100000, MaxMessagePageSize},
	}
	for _, tt := range tests {
		if got := NormalizeMessagePageSize(tt.in); got != tt.want {
			t.Fatalf("NormalizeMessagePageSize(%d)=%d want %d", tt.in, got, tt.want)
		}
	}
}
