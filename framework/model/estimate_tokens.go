package model

import (
	"unicode/utf8"
)

// DefaultTokenEstimateAlpha 对「码点→token」粗估的保守系数（设计 §5.4：中文等宽字符 tokenizer 往往高于 len/utf8/4）。
// 与 MaxContextRunes（码点预算）并存；L2 软阈值用本估算判断是否触发摘要。
const DefaultTokenEstimateAlpha = 1.35

// EstimateTokensConservative 对 messages 做保守 token 粗估：sum(rune_count(plain)*alpha)。
// plain 口径与 CompressMessagesByRunesBudget 一致（见 plainTextForBudget）。
func EstimateTokensConservative(msgs []Message, alpha float64) int {
	if alpha <= 0 {
		alpha = DefaultTokenEstimateAlpha
	}
	var n float64
	for i := range msgs {
		rc := utf8.RuneCountInString(plainTextForBudget(msgs[i]))
		n += float64(rc) * alpha
	}
	if n < 1 && len(msgs) > 0 {
		return 1
	}
	return int(n + 0.999) // ceil small totals
}
