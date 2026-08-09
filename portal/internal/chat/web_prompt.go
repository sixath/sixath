package chat

import "strings"

const webToolsSystemHint = `## 联网搜索（web_search / web_extract）
当需要**实时、可查证**的公开网页信息，且该信息**服务于本轮用户问题**时，优先调用 **web_search**（返回标题、URL、摘要）；单页全文再用 **web_extract**。
在回复中引用检索结果时请附上 URL，不要仅用 http_request 代替 web_search。
仅当用户明确要求多章节/完整条目，且进行了多次 web_search 时，最终回答才须综合全部检索结果写完整。
不要在用户未要求时自行扩展新主题、额外政策/法律章节或「延伸阅读」。
仅当 web_search 不可用或明确失败时，再说明限制并改用其它手段。`

// AppendWebToolsPrompt 在调用方确认已启用 web 工具时追加 system 说明。
// 调用方应仅在 Agent WebToolsEnabled（且通常已配置 key）时调用，避免「提示有工具但未注册」。
func AppendWebToolsPrompt(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return webToolsSystemHint
	}
	return base + "\n\n---\n\n" + webToolsSystemHint
}

// ShouldAppendWebToolsPrompt reports whether the web tools system hint should be added
// for this agent turn (explicit agent/env enable + configured search backend).
func ShouldAppendWebToolsPrompt(flags HermesP0ToolFlags) bool {
	return flags.WebToolsEnabled && WebToolsConfigured()
}
