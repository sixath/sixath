package chat

import "strings"

const codeAnalysisSystemHint = `## 源码分析
- 源码以配置的 **code roots** 为准（跨仓用 rca_grep / rca_glob / rca_read / rca_symbol）。workspace 下的摘录、MEMORY、其它模型摘要不是源码证据。
- 不要用 load_skill 代替上述工具；不要把绑定工具的展示名当成 Skill 名。
- 找到 HTTP path / 函数 / topic / 错误码后，必须再 grep **入边（调用方）**，禁止把入口 handler 当成唯一源头。
- 同一中文名可能对应多套实现：先枚举候选再下钻。
- 先构图（仓、main、路由、消费者）再深读；不要在第一份命中上宣称完整。
- 事实必须带 path:line；推断必须标明。入边扫完或明确某仓不在 code roots 才许下结论。
- grep 只定位。下结论前 rca_read 必须盖住**整个函数**（含所有 if/else/return），不要只截命中行附近几句。
- 贴出的代码必须从 rca_read 原文摘抄，禁止把相邻语句拼成伪源码；if / else / return / errcode 判断不得省略。
- 声称「会调用 X / 会写库」前，按当前场景对包围它的每个条件求值。填结构体字段 ≠ 落库。`

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
