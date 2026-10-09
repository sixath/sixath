package handbook

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sixath/framework/skills"
)

func renderSample(t *testing.T) map[string]string {
	t.Helper()
	f, err := CollectFacts(context.Background(), sampleRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	return Render(RenderMeta{RelPath: "cloudgame/svc-a", Commit: "0123456789abcdef", GeneratedAt: time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)}, f, nil)
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
	m, ok := idx.GetByName("handbook-cloudgame-svc-a-97d4e7ac")
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
	cases := map[string]string{
		"svc-a":            "handbook-svc-a",
		"root":             "handbook-root",
		"cloudgame/svc-a":  "handbook-cloudgame-svc-a-97d4e7ac",
		"Infra/Common_Lib": "handbook-infra-common-lib-70d5397e",
		"///":              "handbook-root-732c4e97",
		"a/b-c":            "handbook-a-b-c-b88f83c8",
		"a-b/c":            "handbook-a-b-c-4e84717d",
	}
	for in, want := range cases {
		if got := SkillName(in); got != want {
			t.Fatalf("SkillName(%q) = %q, want %q", in, got, want)
		}
	}
	long1, long2 := strings.Repeat("a", 70)+"1", strings.Repeat("a", 70)+"2"
	n1, n2 := SkillName(long1), SkillName(long2)
	if n1 == n2 || len(n1) > len("handbook-")+60 || len(n2) > len("handbook-")+60 {
		t.Fatalf("long names: %q %q", n1, n2)
	}
	if exact := strings.Repeat("b", 60); SkillName(exact) != "handbook-"+exact {
		t.Fatalf("60-char slug = %q", SkillName(exact))
	}
	for in, want := range map[string]string{"aux": "aux-dir", "CON": "con-dir", "lpt1": "lpt1-dir", "auxiliary": "auxiliary"} {
		if got := slug(in); got != want {
			t.Fatalf("slug(%q) = %q, want %q", in, got, want)
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

func TestBuildAreas_DedupeDoesNotCollide(t *testing.T) {
	f := &Facts{Files: []File{{Path: "README.md"}, {Path: "root/a.go"}, {Path: "root-2/b.go"}}}
	seen := map[string]string{}
	for _, a := range buildAreas(f) {
		if prev, ok := seen[a.ID]; ok {
			t.Fatalf("areas %q and %q share id %q", prev, a.Name, a.ID)
		}
		seen[a.ID] = a.Name
	}
	if len(seen) != 3 {
		t.Fatalf("areas = %v", seen)
	}
}

func TestPaginate_TruncatesOversizedSection(t *testing.T) {
	huge := strings.Repeat("长", MaxPageBytes)
	pages := paginate("references/areas/a", "# 分区 a", []string{"## head\n\n", huge, "## tail\n\n"})
	for p, c := range pages {
		if len(c) > MaxPageBytes {
			t.Fatalf("%s is %d bytes", p, len(c))
		}
		if !utf8.ValidString(c) {
			t.Fatalf("%s is not valid UTF-8", p)
		}
	}
	all := strings.Join(mapValues(pages), "")
	if !strings.Contains(all, "…（已截断）") || !strings.Contains(all, "## head") || !strings.Contains(all, "## tail") {
		t.Fatalf("pages = %v", keysOf(pages))
	}
}

func TestRender_LargeRepoPagesWithinCap(t *testing.T) {
	f := &Facts{Symbols: map[string][]Symbol{}}
	doc := strings.Repeat("很长的包说明", 20)
	for i := 0; i < 400; i++ {
		dir := fmt.Sprintf("module%03d/pkg", i)
		p := dir + "/x.go"
		f.Files = append(f.Files, File{Path: p, Lang: "go", Lines: 100})
		f.Packages = append(f.Packages, GoPackage{Dir: dir, Name: "pkg", Doc: doc, Files: 1, Main: true})
		for j := 0; j < 60; j++ {
			f.Symbols[p] = append(f.Symbols[p], Symbol{Name: fmt.Sprintf("Func%02d", j), Kind: "func", Line: j, EndLine: j + 1})
		}
		f.Registers = append(f.Registers, RegisterHit{Kind: RegTable, Name: fmt.Sprintf("table_%03d", i), Access: AccessRead, Path: p, Line: 1})
	}
	pages := Render(RenderMeta{RelPath: "big/repo", Commit: "c", GeneratedAt: time.Unix(0, 0)}, f, nil)
	for p, c := range pages {
		if len(c) > MaxPageBytes {
			t.Fatalf("%s is %d bytes > %d", p, len(c), MaxPageBytes)
		}
	}
	if _, ok := pages["references/index.p2.md"]; !ok {
		t.Fatalf("index should be paginated; pages = %v", keysOf(pages))
	}
	if !strings.Contains(pages["references/index.md"], "`references/index.p2.md`") {
		t.Fatalf("index first page lacks continuation list:\n%.300s", pages["references/index.md"])
	}
}

func TestRender_TimestampsUTC(t *testing.T) {
	f := &Facts{Symbols: map[string][]Symbol{}}
	at := time.Date(2026, 10, 9, 16, 30, 0, 0, time.FixedZone("CST", 8*3600))
	pages := Render(RenderMeta{RelPath: "svc", Commit: "c", GeneratedAt: at}, f, nil)
	for _, p := range []string{"SKILL.md", "references/overview.md"} {
		if !strings.Contains(pages[p], "2026-10-09 08:30") {
			t.Fatalf("%s timestamp not UTC:\n%s", p, pages[p])
		}
	}
}

func mapValues(m map[string]string) []string {
	var out []string
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

func keysOf(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
