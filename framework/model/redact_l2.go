package model

import "regexp"

var (
	reBearer   = regexp.MustCompile(`(?i)\b(bearer\s+[a-z0-9\-._~+/]+=*)\b`)
	reAPIKeyEq = regexp.MustCompile(`(?i)(api[_-]?key|secret|token|password)\s*=\s*\S+`)
)

// RedactForL2Context 对拟送入 L2 摘要模型的文本做轻量脱敏（设计 §5.2）。
func RedactForL2Context(s string) string {
	s = reBearer.ReplaceAllString(s, "Bearer [REDACTED]")
	s = reAPIKeyEq.ReplaceAllString(s, "$1=[REDACTED]")
	return s
}
