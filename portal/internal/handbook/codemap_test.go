package handbook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sixath/framework/skills"
)

func TestRenderCodeMap(t *testing.T) {
	out := RenderCodeMap([]CodeMapEntry{
		{RelPath: "cloudgame/svc-a", Description: "网关\n第二行", SkillName: "handbook-cloudgame-svc-a"},
		{RelPath: "cloudgame/svc-b", SubPaths: []string{"internal/x"}},
		{RelPath: "infra"},
	})
	for _, n := range []string{
		"## cloudgame（2 个）",
		"- `cloudgame/svc-a`：网关 —— skill_view(\"handbook-cloudgame-svc-a\")",
		"- `cloudgame/svc-b`（仅子目录，repo 参数用 `cloudgame/svc-b/internal/x`） —— 暂无 handbook，直接用 rca_grep",
		"## （顶层）（1 个）",
	} {
		if !strings.Contains(out, n) {
			t.Fatalf("missing %q:\n%s", n, out)
		}
	}
	dir := filepath.Join(t.TempDir(), "code-map")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := skills.NewIndex([]string{dir}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := idx.GetByName(CodeMapSkillName)
	if !ok || !m.SummaryPinned || m.HiddenFromSummary || !strings.Contains(m.Description, "3 个代码仓库") {
		t.Fatalf("meta = %#v ok=%v", m, ok)
	}
	if empty := RenderCodeMap(nil); !strings.Contains(empty, "当前没有可访问的仓库") {
		t.Fatalf("empty code-map:\n%s", empty)
	}
}
