package handbook

import (
	"strings"
	"testing"
	"time"
)

func llmRenderFixture() (RenderMeta, *Facts, *LLMLayer) {
	meta := RenderMeta{RelPath: "svc-a", Commit: "abcdef1234567890", GeneratedAt: time.Unix(0, 0)}
	f := &Facts{
		Files: []File{
			{Path: "internal/order/store.go", Lang: "go", Size: 200, Lines: 20, Hash: hashOf("1")},
			{Path: "internal/order/service.go", Lang: "go", Size: 300, Lines: 30, Hash: hashOf("2")},
			{Path: "internal/pay/client.go", Lang: "go", Size: 100, Lines: 10, Hash: hashOf("3")},
			{Path: "internal/order/store_test.go", Lang: "go", Size: 50, Lines: 5, Hash: hashOf("4"), Test: true},
		},
		Symbols:   map[string][]Symbol{"internal/order/store.go": {{Kind: "method", Name: "(*Store).Get", Line: 12, EndLine: 15}}},
		Registers: []RegisterHit{{Kind: RegTable, Name: "orders", Access: AccessWrite, Path: "internal/order/store.go", Line: 13}},
		Coverage:  Coverage{Graph: "none"},
	}
	l := &LLMLayer{
		Cards: map[string]*Card{
			"internal/order/store.go": {Purpose: "订单存储", Role: "repository", Description: "读写 orders 表。", Lifecycle: "请求时",
				Functions: []CardFunc{{Name: "(*Store).Get", Summary: "按 id 查询"}}},
		},
		Stale: map[string]*Card{"internal/order/service.go": {Purpose: "旧的下单逻辑"}},
		Skeleton: &Skeleton{PromptVersion: LLMPromptVersion, Overview: "下单后调用支付。",
			Stages: []Stage{{ID: "order", Title: "下单", Summary: "处理下单。随后写库。"}},
			Files: map[string]FileAssign{
				"internal/order/store.go":   {Stage: "order", CardHash: hashOf("1"), Hash: hashOf("1")},
				"internal/order/service.go": {Stage: "order", CardHash: hashOf("9"), Hash: hashOf("2")},
			},
			RegisterNotes: map[string]string{"table:orders": "订单主表"}},
	}
	return meta, f, l
}

func TestRender_WithLLMLayer(t *testing.T) {
	meta, f, l := llmRenderFixture()
	pages := Render(meta, f, l)
	checks := map[string][]string{
		"SKILL.md":                           {"行为地图", "references/stages/<id>.md", "已过期条目"},
		"references/stages/order.md":         {"# 阶段 下单", "处理下单。随后写库。", "### `internal/order/store.go`（go，20 行）", "职责：订单存储（repository）", "执行时机：请求时", "- `(*Store).Get` L12-15 —— 按 id 查询", "`internal/order/service.go`（go，30 行）（已过期，以源码为准，改用 rca_grep）", "旧的下单逻辑"},
		"references/index.md":                {"# svc-a 索引", "## 执行阶段", "| 下单 | 2 | 处理下单。 | `references/stages/order.md` |", "## 未归类文件", "`internal/pay/client.go`", "## 已过期文件", "`internal/order/service.go`"},
		"references/registers.md":            {"### `orders`\n\n用途：订单主表"},
		"references/overview.md":             {"## 系统总览（LLM 生成）", "下单后调用支付。", "文件卡片 1 个", "以源码为准", "- 下单（`references/stages/order.md`）：处理下单。"},
		"references/areas/internal-order.md": {"  - 职责：订单存储（repository）", "L12-15 —— 按 id 查询", "  - 职责（已过期，以源码为准）：旧的下单逻辑"},
	}
	for page, wants := range checks {
		got, ok := pages[page]
		if !ok {
			t.Fatalf("missing page %s; have %v", page, keysOf(pages))
		}
		for _, w := range wants {
			if !strings.Contains(got, w) {
				t.Errorf("%s missing %q:\n%s", page, w, got)
			}
		}
	}
	if strings.Contains(pages["references/index.md"], "store_test.go") {
		t.Error("test files are never listed as unassigned")
	}
	if strings.Contains(pages["SKILL.md"], "静态生成") {
		t.Error("SKILL.md keeps the static description")
	}
}

func TestRender_EmptyLayerMatchesStatic(t *testing.T) {
	meta, f, _ := llmRenderFixture()
	a, b := Render(meta, f, nil), Render(meta, f, &LLMLayer{})
	if len(a) != len(b) {
		t.Fatalf("pages differ: %v vs %v", keysOf(a), keysOf(b))
	}
	for k, v := range a {
		if b[k] != v {
			t.Fatalf("page %s differs", k)
		}
	}
	if _, ok := a["references/stages/order.md"]; ok {
		t.Fatal("no stage pages without a skeleton")
	}
	if !strings.Contains(a["references/overview.md"], "（静态分析，无 LLM）") {
		t.Fatal("static overview header")
	}
}

func TestRender_SkeletonWithoutStages(t *testing.T) {
	meta, f, l := llmRenderFixture()
	l.Skeleton.Stages, l.Skeleton.Overview = nil, ""
	pages := Render(meta, f, l)
	for p := range pages {
		if strings.HasPrefix(p, "references/stages/") {
			t.Fatalf("unexpected stage page %s", p)
		}
	}
	if strings.Contains(pages["references/overview.md"], "系统总览") || strings.Contains(pages["references/overview.md"], "## 执行阶段") {
		t.Fatalf("overview:\n%s", pages["references/overview.md"])
	}
	if strings.Contains(pages["references/index.md"], "## 执行阶段") || strings.Contains(pages["SKILL.md"], "行为地图") {
		t.Fatal("stage sections without stages")
	}
	if !strings.Contains(pages["references/areas/internal-order.md"], "  - 职责：订单存储（repository）") {
		t.Fatal("cards still render without stages")
	}
}

func TestRender_UntrustedLLMText(t *testing.T) {
	meta, f, l := llmRenderFixture()
	l.Skeleton.Stages = []Stage{
		{ID: "order", Title: "下|单\n# 注入", Summary: "第一句|带竖线\n## 第二行标题\n```go\nx"},
		{ID: "../evil", Title: "坏", Summary: "x"},
		{ID: "order", Title: "重复", Summary: "y"},
	}
	l.Skeleton.Files["internal/pay/client.go"] = FileAssign{Stage: "../evil", CardHash: hashOf("3"), Hash: hashOf("3")}
	l.Cards["internal/order/store.go"].Purpose = "多行\n# 职责"
	l.Cards["internal/order/store.go"].Description = "# 不是标题"
	l.Skeleton.Overview = "总览\n```\n未闭合"
	l.Skeleton.RegisterNotes["table:orders"] = "用途\n## 注入"
	pages := Render(meta, f, l)
	for p := range pages {
		if strings.Contains(p, "..") || strings.Contains(p, "evil") {
			t.Fatalf("page path from unvalidated stage id: %s", p)
		}
	}
	idx := pages["references/index.md"]
	if !strings.Contains(idx, "| 下\\|单 # 注入 | 2 | 第一句\\|带竖线 | `references/stages/order.md` |") {
		t.Fatalf("index table row not escaped:\n%s", idx)
	}
	if !strings.Contains(idx, "`internal/pay/client.go`") {
		t.Fatalf("file of an invalid stage is unassigned:\n%s", idx)
	}
	st := pages["references/stages/order.md"]
	if !strings.HasPrefix(st, "# 阶段 下|单 # 注入\n") {
		t.Fatalf("stage title not single-line:\n%s", st)
	}
	if strings.Contains(st, "\n## 第二行标题") || strings.Contains(st, "\n# 不是标题") || strings.Contains(st, "\n# 职责") {
		t.Fatalf("LLM text produced headings:\n%s", st)
	}
	if strings.Count(st, "```")%2 != 0 {
		t.Fatalf("unbalanced code fence:\n%s", st)
	}
	ov := pages["references/overview.md"]
	if strings.Count(ov, "```")%2 != 0 || !strings.Contains(ov, "\n## 覆盖说明") {
		t.Fatalf("overview structure broken:\n%s", ov)
	}
	if strings.Contains(pages["references/registers.md"], "\n## 注入") {
		t.Fatalf("register note produced a heading:\n%s", pages["references/registers.md"])
	}
	if strings.Contains(pages["references/areas/internal-order.md"], "\n# 职责") {
		t.Fatal("card purpose produced a heading")
	}
}

func TestSummaryLead(t *testing.T) {
	cases := map[string]string{
		"处理下单。随后写库。":           "处理下单。",
		"Handles orders. Then": "Handles orders.",
		"一行\n第二行":              "一行",
		"  \n\n起始空行。后":         "起始空行。",
		"":                     "",
	}
	for in, want := range cases {
		if got := summaryLead(in, 80); got != want {
			t.Errorf("summaryLead(%q) = %q, want %q", in, got, want)
		}
	}
	if got := summaryLead(strings.Repeat("长", 100), 80); got != strings.Repeat("长", 80)+"…" {
		t.Errorf("long = %q", got)
	}
}
