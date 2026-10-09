package handbook

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sixath/framework/skills"
)

func renderSample(t *testing.T) map[string]string {
	t.Helper()
	f, err := CollectFacts(context.Background(), sampleRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	return Render(RenderMeta{RelPath: "cloudgame/svc-a", Commit: "0123456789abcdef", GeneratedAt: time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)}, f)
}

func TestRender_PagesAndContent(t *testing.T) {
	pages := renderSample(t)
	for _, p := range []string{"SKILL.md", "references/overview.md", "references/index.md", "references/registers.md",
		"references/areas/root.md", "references/areas/cmd-server.md", "references/areas/internal-order.md"} {
		if _, ok := pages[p]; !ok {
			t.Fatalf("missing page %s; have %v", p, keysOf(pages))
		}
	}
	checks := map[string][]string{
		"references/overview.md":             {"cloudgame/svc-a 概览", "`0123456789ab`", "Go module `example.com/svc`", "`cmd/server`", "`github.com/a/b`", "数据表 1 个"},
		"references/index.md":                {"| internal/order | 2 | `references/areas/internal-order.md` |", "`internal/order`（package order，1 个文件）：Package order persists orders."},
		"references/registers.md":            {"### `orders`", "- 写 `internal/order/store.go:18`", "- 读 `internal/order/store.go:13`", "- 引用 `internal/order/store.go:10`"},
		"references/areas/internal-order.md": {"## `internal/order`（package order）", "- `internal/order/store.go`（go，20 行）", "  - method `(*Store).MarkPaid` L17-20", "，测试）"},
	}
	for page, needles := range checks {
		for _, n := range needles {
			if !strings.Contains(pages[page], n) {
				t.Fatalf("%s missing %q:\n%s", page, n, pages[page])
			}
		}
	}
}

func TestRender_SkillFrontmatterParses(t *testing.T) {
	pages := renderSample(t)
	dir := filepath.Join(t.TempDir(), "skill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(pages["SKILL.md"]), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := skills.NewIndex([]string{dir}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := idx.GetByName("handbook-cloudgame-svc-a")
	if !ok || !m.HiddenFromSummary || !strings.Contains(m.Description, "cloudgame/svc-a 的代码地图") {
		t.Fatalf("meta = %#v ok=%v", m, ok)
	}
}

func TestPaginate(t *testing.T) {
	big := strings.Repeat("x", MaxPageBytes/2+1)
	pages := paginate("references/areas/a", "# 分区 a", []string{big, big, big})
	if len(pages) != 3 {
		t.Fatalf("pages = %v", keysOf(pages))
	}
	first := pages["references/areas/a.md"]
	if !strings.Contains(first, "（第 1/3 页）") || !strings.Contains(first, "`references/areas/a.p2.md`、`references/areas/a.p3.md`") {
		t.Fatalf("first page header:\n%s", first[:200])
	}
	if empty := paginate("references/registers", "# 寄存器", nil); !strings.Contains(empty["references/registers.md"], "（无）") {
		t.Fatalf("empty = %v", empty)
	}
}

func TestSlugAndSkillName(t *testing.T) {
	cases := map[string]string{"cloudgame/svc-a": "handbook-cloudgame-svc-a", "Infra/Common_Lib": "handbook-infra-common-lib", "///": "handbook-root"}
	for in, want := range cases {
		if got := SkillName(in); got != want {
			t.Fatalf("SkillName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := areaOfDir("internal/order/sub"); got != "internal/order" {
		t.Fatalf("areaOfDir = %q", got)
	}
	if got := areaOfDir("."); got != "(root)" {
		t.Fatalf("areaOfDir(.) = %q", got)
	}
	if got := areaOfDir("docs/a"); got != "docs" {
		t.Fatalf("areaOfDir(docs/a) = %q", got)
	}
}

func keysOf(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
