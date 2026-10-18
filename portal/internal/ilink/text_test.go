package ilink

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPlainText(t *testing.T) {
	in := "# 标题\n\n**加粗** 与 __下划线__\n\n```go\nfmt.Println(\"**keep**\")\n```\n## 小节\n正文"
	want := "标题\n\n加粗 与 下划线\n\nfmt.Println(\"**keep**\")\n小节\n正文"
	if got := PlainText(in); got != want {
		t.Fatalf("PlainText =\n%q\nwant\n%q", got, want)
	}
}

func TestPlainText_KeepsPlainAndCRLF(t *testing.T) {
	if got := PlainText("a_b c#d\r\n#nospace"); got != "a_b c#d\n#nospace" {
		t.Fatalf("PlainText = %q", got)
	}
}

func TestSplitText_ShortAndBlank(t *testing.T) {
	if got := SplitText("  hi  ", 10); len(got) != 1 || got[0] != "hi" {
		t.Fatalf("short = %q", got)
	}
	if got := SplitText(" \n ", 10); len(got) != 0 {
		t.Fatalf("blank = %q", got)
	}
}

func TestSplitText_HardCutByRunes(t *testing.T) {
	s := strings.Repeat("字", 9)
	got := SplitText(s, 4)
	if len(got) != 3 || got[0] != "字字字字" || got[2] != "字" {
		t.Fatalf("parts = %q", got)
	}
}

func TestSplitText_PrefersNewline(t *testing.T) {
	got := SplitText("aaaa\nbbbbbb", 8)
	if len(got) != 2 || got[0] != "aaaa" || got[1] != "bbbbbb" {
		t.Fatalf("parts = %q", got)
	}
}

func TestSplitText_DefaultLimit(t *testing.T) {
	s := strings.Repeat("中", DefaultSplitRunes+1)
	got := SplitText(s, 0)
	if len(got) != 2 || utf8.RuneCountInString(got[0]) != DefaultSplitRunes || got[1] != "中" {
		t.Fatalf("parts lens = %d", len(got))
	}
}
