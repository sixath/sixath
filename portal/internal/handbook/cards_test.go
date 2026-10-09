package handbook

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sixath/framework/model"
)

func TestExtractJSONObject(t *testing.T) {
	cases := map[string]string{
		"```json\n{\"a\":1}\n```": `{"a":1}`,
		"好的：{\"a\":{\"b\":2}} 以上": `{"a":{"b":2}}`,
		"没有":                      "",
		`{"a":1} 注：{x}`:           `{"a":1}`,
		`前置 {说明} 然后 {"a":1}`:      `{"a":1}`,
		"说明：\n```\n{\"a\":[1,{\"b\":\"}\"}]}\n```\n完": `{"a":[1,{"b":"}"}]}`,
		"{ 未闭合": "",
		`[1,2]`: "",
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
	noNL := clipContent([]byte(strings.Repeat("中", 20)), 10)
	body := strings.TrimSuffix(noNL, clippedMark)
	if !utf8.ValidString(body) || body != strings.Repeat("中", 3) {
		t.Fatalf("no-newline clip must cut at a UTF-8 boundary: %q", noNL)
	}
	for _, n := range []int{0, -5} {
		if got := clipContent([]byte("abc"), n); got != "a"+clippedMark {
			t.Fatalf("maxBytes %d: %q", n, got)
		}
	}
}

func TestCodeFence(t *testing.T) {
	cases := map[string]string{"plain": "```", "a ``` b": "````", "x ````` y `": "``````"}
	for in, want := range cases {
		if got := codeFence(in); got != want {
			t.Errorf("%q: got %q", in, got)
		}
	}
	p := cardPrompt("r", File{Path: "a.md", Lang: "go"}, nil, "s := \"```\"")
	if !strings.Contains(p, "````go\n") || !strings.Contains(p, "\n````\n") {
		t.Fatalf("prompt fence: %s", p)
	}
}

func TestSanitizeCard_TrimsBackticksFromNames(t *testing.T) {
	c := sanitizeCard(cardReply{Purpose: "x", Functions: []CardFunc{{Name: " `(*Store).Get` ", Summary: "s"}}},
		[]Symbol{{Name: "(*Store).Get"}})
	if len(c.Functions) != 1 || c.Functions[0].Name != "(*Store).Get" {
		t.Fatalf("functions %#v", c.Functions)
	}
}

func TestSanitizeCard_CollapsesWhitespace(t *testing.T) {
	c := sanitizeCard(cardReply{Purpose: "订单\n  存储", Description: "a\n\nb", Lifecycle: " 请求\t时 ",
		Functions: []CardFunc{{Name: "F", Summary: "读\r\n写"}}}, []Symbol{{Name: "F"}})
	if c.Purpose != "订单 存储" || c.Description != "a b" || c.Lifecycle != "请求 时" || c.Functions[0].Summary != "读 写" {
		t.Fatalf("%#v", c)
	}
}

type stubModel struct {
	fakeModel
	gen *model.Generation
	err error
}

func (s *stubModel) Chat(context.Context, []model.Message, ...model.Option) (*model.Generation, error) {
	return s.gen, s.err
}

func TestLLMJSON_ErrorKinds(t *testing.T) {
	var out map[string]any
	var u usage
	truncated := &stubModel{gen: &model.Generation{Text: `{"a":1,"b":{"c":2},"d":`, FinishReason: "length"}}
	if err := llmJSON(context.Background(), truncated, "s", "u", 10, &out, &u); !errors.Is(err, errBadReply) {
		t.Fatalf("length: %v", err)
	}
	transport := &stubModel{err: errors.New("connection reset")}
	if err := llmJSON(context.Background(), transport, "s", "u", 10, &out, &u); err == nil || errors.Is(err, errBadReply) {
		t.Fatalf("transport error must not be errBadReply: %v", err)
	}
	empty := &stubModel{}
	if err := llmJSON(context.Background(), empty, "s", "u", 10, &out, nil); !errors.Is(err, errBadReply) {
		t.Fatalf("nil generation: %v", err)
	}
	ok := &stubModel{gen: &model.Generation{Text: `{"a":1}`, TokenUsage: &model.TokenUsage{InputTokens: 3}}}
	if err := llmJSON(context.Background(), ok, "s", "u", 10, &out, nil); err != nil || out["a"] != float64(1) {
		t.Fatalf("nil usage: %v %#v", err, out)
	}
}

type seqModel struct {
	fakeModel
	gens []*model.Generation
}

func (s *seqModel) Chat(ctx context.Context, msgs []model.Message, opts ...model.Option) (*model.Generation, error) {
	var cfg model.CallConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	s.maxTokens = append(s.maxTokens, cfg.MaxTokens)
	g := s.gens[0]
	if len(s.gens) > 1 {
		s.gens = s.gens[1:]
	}
	return g, nil
}

func TestLLMJSON_RetriesTruncatedReplyWithLargerBudget(t *testing.T) {
	var out map[string]any
	var u usage
	m := &seqModel{gens: []*model.Generation{
		{Text: "", FinishReason: "length", TokenUsage: &model.TokenUsage{OutputTokens: 900}},
		{Text: `{"a":1}`, FinishReason: "stop", TokenUsage: &model.TokenUsage{OutputTokens: 1200}},
	}}
	if err := llmJSON(context.Background(), m, "s", "u", 900, &out, &u); err != nil || out["a"] != float64(1) {
		t.Fatalf("retry: %v %#v", err, out)
	}
	if len(m.maxTokens) != 2 || m.maxTokens[0] != 900 || m.maxTokens[1] != llmMaxTokensCap {
		t.Fatalf("max tokens %v", m.maxTokens)
	}
	if u.out.Load() != 2100 {
		t.Fatalf("usage must count both calls: %d", u.out.Load())
	}

	always := &seqModel{gens: []*model.Generation{{Text: "", FinishReason: "length"}}}
	if err := llmJSON(context.Background(), always, "s", "u", 4000, &out, nil); !errors.Is(err, errBadReply) {
		t.Fatalf("still truncated: %v", err)
	}
	if len(always.maxTokens) != 2 || always.maxTokens[1] != llmMaxTokensCap {
		t.Fatalf("retry once at the cap: %v", always.maxTokens)
	}

	capped := &seqModel{gens: []*model.Generation{{Text: "", FinishReason: "length"}}}
	if err := llmJSON(context.Background(), capped, "s", "u", llmMaxTokensCap, &out, nil); !errors.Is(err, errBadReply) || len(capped.maxTokens) != 1 {
		t.Fatalf("no retry at cap: %v %v", err, capped.maxTokens)
	}
}

func TestLLMJSON_TruncatedButCompleteObjectIsAccepted(t *testing.T) {
	var out map[string]any
	complete := &seqModel{gens: []*model.Generation{{Text: "```json\n{\"a\":1}\n```", FinishReason: "length"}}}
	if err := llmJSON(context.Background(), complete, "s", "u", 900, &out, nil); err != nil || out["a"] != float64(1) || len(complete.maxTokens) != 1 {
		t.Fatalf("complete object under length: %v %#v %v", err, out, complete.maxTokens)
	}

	out = nil
	inner := &seqModel{gens: []*model.Generation{{Text: `{"purpose":"x","functions":[{"name":"F","summary":"s"},{"na`, FinishReason: "length"}}}
	if err := llmJSON(context.Background(), inner, "s", "u", 900, &out, nil); !errors.Is(err, errBadReply) || out != nil {
		t.Fatalf("inner object of a cut-off reply must not pass: %v %#v", err, out)
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
