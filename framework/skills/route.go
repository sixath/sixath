package skills

import (
	"strings"
	"unicode"
)

// RouteMatch 表示一次技能路由命中结果。
type RouteMatch struct {
	Name          string
	Score         int
	RunnerUpScore int
}

// RouteOptions 控制关键词路由行为。
type RouteOptions struct {
	// MinScore 最低得分；<=0 时使用默认 5。
	MinScore int
	// MaxResults 返回条数上限；<=0 时仅取最佳一条。
	MaxResults int
}

const defaultRouteMinScore = 5

// Route 按用户问题对技能元数据打分并降序返回（不含未达阈值的项）。
func Route(userQuery string, metas []SkillMeta, opts RouteOptions) []RouteMatch {
	if len(metas) == 0 || strings.TrimSpace(userQuery) == "" {
		return nil
	}
	minScore := opts.MinScore
	if minScore <= 0 {
		minScore = defaultRouteMinScore
	}
	q := strings.ToLower(strings.TrimSpace(userQuery))
	qTokens := tokenize(q)

	var out []RouteMatch
	for _, m := range metas {
		sc := scoreSkill(q, qTokens, m)
		if sc >= minScore {
			out = append(out, RouteMatch{Name: m.Name, Score: sc})
		}
	}
	sortMatchesDesc(out)
	max := opts.MaxResults
	if max <= 0 {
		max = len(out)
	}
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// RouteBest 返回得分最高且达阈值的技能；无命中时 ok=false。
func RouteBest(userQuery string, metas []SkillMeta, opts RouteOptions) (RouteMatch, bool) {
	opts.MaxResults = 2
	matches := Route(userQuery, metas, opts)
	if len(matches) == 0 {
		return RouteMatch{}, false
	}
	m := matches[0]
	if len(matches) >= 2 {
		m.RunnerUpScore = matches[1].Score
	}
	return m, true
}

func scoreSkill(q string, qTokens map[string]struct{}, m SkillMeta) int {
	score := 0
	name := strings.ToLower(strings.TrimSpace(m.Name))
	if name == "" {
		return 0
	}
	nameSpaced := strings.ReplaceAll(name, "-", " ")
	if strings.Contains(q, name) || strings.Contains(q, nameSpaced) {
		score += 12
	}
	for _, part := range strings.Split(name, "-") {
		part = strings.TrimSpace(part)
		if len(part) < 3 {
			continue
		}
		if _, ok := qTokens[part]; ok {
			score += 4
		}
	}
	desc := strings.ToLower(m.Description)
	for tok := range qTokens {
		if len(tok) < 3 {
			continue
		}
		if strings.Contains(desc, tok) {
			score += 2
		}
	}
	for _, tag := range m.Tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" || len(tag) < 3 {
			continue
		}
		hit := tagContainedInTokens(tag, qTokens)
		if !hit && len(tag) >= 4 && strings.Contains(q, tag) {
			hit = true
		}
		if hit {
			score += 5
		}
	}
	return score
}

func tokenize(s string) map[string]struct{} {
	out := make(map[string]struct{})
	var b strings.Builder
	lastClass := 0
	flush := func() {
		if b.Len() >= 3 {
			out[b.String()] = struct{}{}
		}
		b.Reset()
		lastClass = 0
	}
	for _, r := range s {
		c := tokenClass(r)
		if c == 0 {
			flush()
			continue
		}
		if lastClass != 0 && c != lastClass {
			flush()
		}
		b.WriteRune(r)
		lastClass = c
	}
	flush()
	expandCJKNGrams(out)
	return out
}

// expandCJKNGrams adds 2-grams and 3-grams for CJK tokens so Chinese queries
// can match skill description/tags (a whole sentence is otherwise one token).
func expandCJKNGrams(out map[string]struct{}) {
	var extra []string
	for tok := range out {
		rs := []rune(tok)
		if len(rs) < 2 {
			continue
		}
		allCJK := true
		for _, r := range rs {
			if tokenClass(r) != 2 {
				allCJK = false
				break
			}
		}
		if !allCJK {
			continue
		}
		for n := 2; n <= 3 && n <= len(rs); n++ {
			for i := 0; i+n <= len(rs); i++ {
				extra = append(extra, string(rs[i:i+n]))
			}
		}
	}
	for _, g := range extra {
		out[g] = struct{}{}
	}
}

func tokenClass(r rune) int {
	if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
		return 1
	}
	if unicode.IsLetter(r) {
		return 2
	}
	return 0
}

func tagContainedInTokens(tag string, tokens map[string]struct{}) bool {
	for _, part := range strings.Fields(strings.ReplaceAll(tag, "-", " ")) {
		part = strings.ToLower(part)
		if len(part) < 3 {
			continue
		}
		if _, ok := tokens[part]; ok {
			return true
		}
	}
	return false
}

func sortMatchesDesc(matches []RouteMatch) {
	for i := 0; i < len(matches); i++ {
		for j := i + 1; j < len(matches); j++ {
			if matches[j].Score > matches[i].Score {
				matches[i], matches[j] = matches[j], matches[i]
			}
		}
	}
}
