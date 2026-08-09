package chat

import "strings"

const turnIntentSystemHint = `## 任务边界
- 只回答**本轮用户提出的问题**；正题完成后停止，不要自行开启新话题或额外章节。
- 用户未要求时，不要为「延伸阅读 / 相关政策 / 法律条文 / 其它案例」等调用任何工具。
- 若想确认是否继续相关话题，先用自然语言询问用户，**不要先调用工具**。`

// AppendTurnIntentPrompt 追加防跑题 / 防工具滥用的系统约束。
func AppendTurnIntentPrompt(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return turnIntentSystemHint
	}
	return base + "\n\n---\n\n" + turnIntentSystemHint
}
