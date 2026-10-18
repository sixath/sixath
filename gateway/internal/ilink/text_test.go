package ilink

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSimplifyMarkdown(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"heading", "# 标题\n## 小节\n正文", "标题\n小节\n正文"},
		{"bold italic", "**加粗** 和 *斜体* 以及 __下划线__ 和 _强调_", "加粗 和 斜体 以及 下划线 和 强调"},
		{"snake_case kept", "变量 user_id_name 不变", "变量 user_id_name 不变"},
		{"link", "见 [文档](https://example.com/a) 和 ![图](https://x/y.png)", "见 文档 (https://example.com/a) 和 图 (https://x/y.png)"},
		{"inline code", "运行 `go test **./...**` 即可", "运行 go test **./...** 即可"},
		{"strike", "~~旧~~新", "旧新"},
		{"code block kept", "前\n```go\n# not heading\nx := **y**\n```\n后", "前\n# not heading\nx := **y**\n后"},
		{"list kept", "- 一\n- 二", "- 一\n- 二"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SimplifyMarkdown(tc.in); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestSplitTextShortUnchanged(t *testing.T) {
	got := SplitText("你好", 10)
	if len(got) != 1 || got[0] != "你好" {
		t.Fatalf("got %q", got)
	}
	if SplitText("  \n ", 10) != nil {
		t.Fatal("blank input should yield no chunks")
	}
}

func TestSplitTextPrefersNewline(t *testing.T) {
	got := SplitText("第一行内容\n第二行内容\n第三行", 12)
	want := []string{"第一行内容\n第二行内容", "第三行"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSplitTextHardCutWithoutNewline(t *testing.T) {
	s := strings.Repeat("字", 25)
	got := SplitText(s, 10)
	if len(got) != 3 || strings.Join(got, "") != s {
		t.Fatalf("got %q", got)
	}
	for _, c := range got {
		if utf8.RuneCountInString(c) > 10 {
			t.Fatalf("chunk too long: %q", c)
		}
	}
}

func TestSplitTextDefaultLimit(t *testing.T) {
	s := strings.Repeat("a", DefaultSplitRunes+1)
	got := SplitText(s, 0)
	if len(got) != 2 || utf8.RuneCountInString(got[0]) != DefaultSplitRunes {
		t.Fatalf("chunks=%d first=%d", len(got), utf8.RuneCountInString(got[0]))
	}
}

func TestShapeReply(t *testing.T) {
	got := ShapeReply("# 结论\n**是**", 100)
	if len(got) != 1 || got[0] != "结论\n是" {
		t.Fatalf("got %q", got)
	}
}
