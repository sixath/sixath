package skills

import (
	"strings"
	"unicode"
)

// RouteMatch 表示一次技能路由命中结果。
type RouteMatch struct {
	Name  string
	Score int
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
	opts.MaxResults = 1
	matches := Route(userQuery, metas, opts)
	if len(matches) == 0 {
		return RouteMatch{}, false
	}
	return matches[0], true
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
		if tag == "" {
			continue
		}
		if strings.Contains(q, tag) || tagContainedInTokens(tag, qTokens) {
			score += 5
		}
	}
	return score
}

func tokenize(s string) map[string]struct{} {
	out := make(map[string]struct{})
	var b strings.Builder
	flush := func() {
		if b.Len() >= 3 {
			out[b.String()] = struct{}{}
		}
		b.Reset()
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
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
