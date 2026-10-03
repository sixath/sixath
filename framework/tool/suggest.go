package tool

import (
	"sort"
	"strings"
)

// Suggest 返回与 input 最接近的至多 n 个候选：精确（忽略大小写）> 前缀 > 子串 > 编辑距离。
// 编辑距离阈值 max(2, len(input)/3)（按 rune 计）；无匹配返回 nil。
func Suggest(input string, candidates []string, n int) []string {
	in := strings.ToLower(strings.TrimSpace(input))
	if in == "" || n <= 0 || len(candidates) == 0 {
		return nil
	}
	limit := len([]rune(in)) / 3
	if limit < 2 {
		limit = 2
	}
	type scored struct {
		s    string
		rank int
		dist int
		idx  int
	}
	var picks []scored
	seen := map[string]struct{}{}
	for i, c := range candidates {
		if _, dup := seen[c]; dup || strings.TrimSpace(c) == "" {
			continue
		}
		seen[c] = struct{}{}
		lc := strings.ToLower(c)
		switch {
		case lc == in:
			picks = append(picks, scored{c, 0, 0, i})
		case strings.HasPrefix(lc, in):
			picks = append(picks, scored{c, 1, len(lc) - len(in), i})
		case strings.Contains(lc, in) || strings.Contains(in, lc):
			picks = append(picks, scored{c, 2, Levenshtein(lc, in), i})
		default:
			if d := Levenshtein(lc, in); d <= limit {
				picks = append(picks, scored{c, 3, d, i})
			}
		}
	}
	if len(picks) == 0 {
		return nil
	}
	sort.SliceStable(picks, func(a, b int) bool {
		if picks[a].rank != picks[b].rank {
			return picks[a].rank < picks[b].rank
		}
		if picks[a].dist != picks[b].dist {
			return picks[a].dist < picks[b].dist
		}
		return picks[a].idx < picks[b].idx
	})
	if picks[0].rank == 0 {
		return []string{picks[0].s}
	}
	if len(picks) > n {
		picks = picks[:n]
	}
	out := make([]string, len(picks))
	for i, p := range picks {
		out[i] = p.s
	}
	return out
}

// Levenshtein 计算 rune 级编辑距离。
func Levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
