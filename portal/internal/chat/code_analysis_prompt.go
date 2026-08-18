package chat

import "strings"

const codeAnalysisSystemHint = `## 源码分析
- 源码以配置的 **code roots** 为准（跨仓用 rca_grep / rca_glob / rca_read / rca_symbol）。workspace 下的摘录、MEMORY、其它模型摘要不是源码证据。
- 不要用 load_skill 代替上述工具；不要把绑定工具的展示名当成 Skill 名。
- 找到 HTTP path / 函数 / topic / 错误码后，必须再 grep **入边（调用方）**，禁止把入口 handler 当成唯一源头。
- 同一中文名可能对应多套实现：先枚举候选再下钻。
- 先构图（仓、main、路由、消费者）再深读；不要在第一份命中上宣称完整。
- 事实必须带 path:line；推断必须标明。入边扫完或明确某仓不在 code roots 才许下结论。`

// AppendCodeAnalysisPrompt 追加与业务无关的源码分析协议。
func AppendCodeAnalysisPrompt(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return codeAnalysisSystemHint
	}
	return base + "\n\n---\n\n" + codeAnalysisSystemHint
}

// AppendCodeAnalysisPromptIf 在 code 族激活时追加；active==nil（工具面关闭/全量）视为应追加。
func AppendCodeAnalysisPromptIf(active map[string]struct{}, base string) string {
	if !FamilyActive(active, FamilyCode) {
		return base
	}
	return AppendCodeAnalysisPrompt(base)
}
