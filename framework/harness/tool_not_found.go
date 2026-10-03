package harness

import (
	"sort"
	"strings"

	"github.com/sixath/framework/tool"
)

const maxToolNameSuggestions = 3

// toolNotFoundMessage 生成回传给模型的错误文本：附带编辑距离最近的已注册工具名，便于模型自行纠正。
func toolNotFoundMessage(name string, available []string) string {
	msg := ErrToolNotFound.Error() + ": " + name
	if s := suggestToolNames(name, available); len(s) > 0 {
		msg += "; did you mean: " + strings.Join(s, ", ") + "? Retry with an exact registered tool name."
	}
	return msg
}

func suggestToolNames(name string, available []string) []string {
	target := strings.ToLower(name)
	if target == "" || len(available) == 0 {
		return nil
	}
	type cand struct {
		name string
		dist int
	}
	threshold := len([]rune(target))/3 + 1
	var cands []cand
	for _, n := range available {
		lower := strings.ToLower(n)
		d := tool.Levenshtein(target, lower)
		if d <= threshold || strings.Contains(lower, target) || strings.Contains(target, lower) {
			cands = append(cands, cand{name: n, dist: d})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].dist != cands[j].dist {
			return cands[i].dist < cands[j].dist
		}
		return cands[i].name < cands[j].name
	})
	if len(cands) > maxToolNameSuggestions {
		cands = cands[:maxToolNameSuggestions]
	}
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.name)
	}
	return out
}
