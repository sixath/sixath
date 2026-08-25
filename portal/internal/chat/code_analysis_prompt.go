package chat

import "strings"

const codeAnalysisSystemHint = `## 源码分析
- 源码以配置的 **code roots** 为准（跨仓用 rca_grep / rca_glob / rca_read / rca_symbol）。workspace 下的摘录、MEMORY、其它模型摘要不是源码证据；禁止用 MEMORY.md / *.txt 顶替 rca_read。
- 不要用 load_skill / skill_view 代替上述工具；不要把绑定工具的展示名当成 Skill 名。本轮未要求查库或加载技能时，不要调用 list_tables / execute_read / memory_recall。
- 找到 HTTP path / 函数 / topic / 错误码后，必须用 **rca_symbol action=references** 扫入边（调用方）。gopls 失败时看返回里的 symbol_ok=false 与 grep 回退，仍算已扫。多仓时 callers 含其它 roots（看 repos_scanned）。禁止把入口 handler 当成唯一源头。
- 入边为空或调用方全在 code roots 外（inbound_empty）才许宣称「整体流程 / 唯一源头」；否则必须先读 callers。
- 同一中文名可能对应多套实现：先枚举候选再下钻。
- 先构图（仓、main、路由、消费者）再深读；不要在第一份命中上宣称完整。
- grep 只定位。下结论前 rca_read 必须盖住**整个函数**；Go 文件会返回 **control_flow** 路径表（when / calls），即使窗口很窄也覆盖整函数。路径表给自己核对用，不要整表贴进终答。
- 声称「会调用 X / 会写库」前必须自己对着 control_flow 的 path id 或 when 核对。填结构体字段 ≠ 落库。
- 用户问题里的错误码/分支（如 1105、已有用户）要对着 control_flow 当前路径：该路径走不到的调用/写库，不要说成发生。
- Go 的 rca_read 还会给 call_graph（同文件/同目录 callee）。跨函数下钻读图上的 file，不要猜路径。非 Go 没有 control_flow/call_graph 时照常读原文，不要编 AST。

## 怎么写给用户
- 先写 **结论**（3–8 句中文）：直接回答「会发生 / 不会发生什么」。不要用 P1/P2、when、path id、control_flow 当正文。
- 结论之后才加「依据」：每条一行，path:line + 一句话。事实必须带 path:line；推断必须标明。
- 不要把 rca_read 的路径表、ASCII 调用链、辅助函数全文贴进终答。用户没要看实现时，不要大段 fenced 代码。
- 必须引用代码时，fenced 块必须从 rca_read 原文连续摘抄，禁止把相邻语句拼成伪源码。对用户用「区域返回已存在则不当失败、且不写映射」这类说法，不要写「走 P5」。`

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
