package sessionsearch

import (
	"strings"
	"unicode"
)

// BuildFTSMatchExpr 将自然语言查询转为 FTS5 OR 表达式（对齐 Hermes：默认 AND 易漏命中）。
func BuildFTSMatchExpr(query string) string {
	keywords := extractKeywords(query)
	if len(keywords) == 0 {
		q := strings.TrimSpace(query)
		if q == "" {
			return ""
		}
		return quoteFTSToken(q)
	}
	parts := make([]string, 0, len(keywords))
	for _, kw := range keywords {
		parts = append(parts, quoteFTSToken(kw))
	}
	return strings.Join(parts, " OR ")
}

func extractKeywords(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var words []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		w := cur.String()
		if len([]rune(w)) >= 2 {
			words = append(words, w)
		}
		cur.Reset()
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			cur.WriteRune(unicode.ToLower(r))
		} else {
			flush()
		}
	}
	flush()
	if len(words) > 12 {
		words = words[:12]
	}
	return words
}

func quoteFTSToken(tok string) string {
	tok = strings.ReplaceAll(tok, `"`, "")
	if tok == "" {
		return ""
	}
	return `"` + tok + `"`
}
