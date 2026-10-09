package handbook

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestExtractJSONObject(t *testing.T) {
	cases := map[string]string{
		"```json\n{\"a\":1}\n```": `{"a":1}`,
		"好的：{\"a\":{\"b\":2}} 以上": `{"a":{"b":2}}`,
		"没有":                      "",
	}
	for in, want := range cases {
		if got := extractJSONObject(in); got != want {
			t.Errorf("%q: got %q", in, got)
		}
	}
}

func TestClipContent(t *testing.T) {
	src := strings.Repeat("一行中文内容\n", 100)
	got := clipContent([]byte(src), 50)
	if len(got) > 50+len(clippedMark) || !strings.HasSuffix(got, clippedMark) || !strings.HasSuffix(strings.TrimSuffix(got, clippedMark), "\n") {
		t.Fatalf("clip: %q", got)
	}
	if clipContent([]byte("short"), 50) != "short" {
		t.Fatal("short content must be kept")
	}
}

func TestGenerateCard_ValidatesAgainstSymbols(t *testing.T) {
	m := (&fakeModel{}).on("文件卡片", func(string) string {
		return "```json\n" + `{"purpose":"订单存储","description":"读写 orders 表。","role":"Repository","lifecycle":"请求时",` +
			`"functions":[{"name":"(*Store).Get","summary":"按 id 查询"},{"name":"Invented","summary":"不存在"},{"name":"(*Store).Get","summary":"重复"}]}` + "\n```"
	})
	f := File{Path: "internal/order/store.go", Lang: "go", Lines: 20, Hash: hashOf("a")}
	syms := []Symbol{{Kind: "method", Name: "(*Store).Get", Line: 12, EndLine: 15}, {Kind: "method", Name: "(*Store).MarkPaid", Line: 17, EndLine: 20}}
	var u usage
	card, err := generateCard(context.Background(), m, "p/m", "svc-a", f, syms, []byte("package order"), 1024, &u)
	if err != nil {
		t.Fatal(err)
	}
	if card.Purpose != "订单存储" || card.Role != "repository" || card.Hash != f.Hash || card.PromptVersion != LLMPromptVersion || card.Model != "p/m" {
		t.Fatalf("card %#v", card)
	}
	if len(card.Functions) != 1 || card.Functions[0].Name != "(*Store).Get" {
		t.Fatalf("functions must be limited to known symbols without duplicates: %#v", card.Functions)
	}
	if u.in.Load() != 10 || u.out.Load() != 5 {
		t.Fatalf("usage %d/%d", u.in.Load(), u.out.Load())
	}
	if !strings.Contains(m.calls[0], "(*Store).MarkPaid") || !strings.Contains(m.calls[0], "internal/order/store.go") {
		t.Fatalf("prompt must list the file path and symbols: %s", m.calls[0])
	}
}

func TestGenerateCard_NonGoDropsFunctionsAndUnknownRole(t *testing.T) {
	m := (&fakeModel{}).on("文件卡片", func(string) string {
		return `{"purpose":"建表","role":"weird","functions":[{"name":"x","summary":"y"}]}`
	})
	var u usage
	card, err := generateCard(context.Background(), m, "m", "r", File{Path: "s.sql", Lang: "sql", Hash: hashOf("b")}, nil, []byte("CREATE TABLE t"), 1024, &u)
	if err != nil || card.Role != "other" || len(card.Functions) != 0 {
		t.Fatalf("card %#v %v", card, err)
	}
}

func TestGenerateCard_BadReplies(t *testing.T) {
	for name, reply := range map[string]string{"no json": "抱歉", "no purpose": `{"purpose":"  "}`, "broken": `{"purpose": }`} {
		m := (&fakeModel{}).on("文件卡片", func(string) string { return reply })
		var u usage
		_, err := generateCard(context.Background(), m, "m", "r", File{Path: "a.go", Lang: "go", Hash: hashOf("c")}, nil, []byte("x"), 1024, &u)
		if !errors.Is(err, errBadReply) {
			t.Errorf("%s: want errBadReply, got %v", name, err)
		}
	}
}
