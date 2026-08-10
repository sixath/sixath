## Summary

在对话中完成 `cloudgame` 仓库模块调用关系分析后，同一条 assistant 回复末尾硬接「7天无理由退货政策」法律条文与案例。根因不是 Memory Hub / wiki / Neo4j 污染，而是 **Agent 循环在正题收束后继续跑**，并调用了 `web_search`；同时 Agent `webToolsEnabled=false` 时进程级仍可能因 Bocha key 自动注册联网工具。

## Evidence

- Session: `fb9f9c75-dddd-4824-a571-bc0127e9764a`（agent `e8107fb3-e40a-4207-9d9a-6768847aaf79` / zone-4100-agent）
- UI: `http://localhost:5173/?agent=e8107fb3-e40a-4207-9d9a-6768847aaf79&session=fb9f9c75-dddd-4824-a571-bc0127e9764a`
- Timeline 工具计数（约）：`rca_read`×18、`rca_glob`×12、`web_search`×6；**无** `knowledge_search` / `memory_recall`
- 正文衔接：`…请告诉我。# 7天无理由退货政策 — 法律条文、官方解读与典型案例`
- 第一次 `web_search` query：`消费者权益保护法 第二十五条 七日无理由退货 原文`（此前正文无「退货/消费者」等字样）
- Agent `runtime_tools.webToolsEnabled=false`，但进程仍注册了 `web_search`

## Root cause (current understanding)

1. **话题漂移**：长 RCA 工具链后模型写完正题，Agent loop 未硬停，下一轮自行开新题并用 `web_search` 填充「可查证」法律章节。
2. **注册旁路**：`internal/chat/runtime_tools.go` 在 `WebToolsEnabled` 为 false 时仍可能走 `WebToolsShouldRegister()`（有 Bocha/Tavily key 即 true）：

```go
if flags.WebToolsEnabled {
    registerWebTools(reg, true)
} else if WebToolsShouldRegister() {
    RegisterWebTools(reg)
}
```

3. `web_prompt.go` 中「多次 web_search 要按章节写完整」会放大跑题后的长文输出。

## Expected

- Agent `webToolsEnabled=false` 时：**绝不**注册 `web_search` / `web_extract`。
- 用户未要求联网/换题时，正题收束后不应擅自 `web_search` 无关 topic。

## Proposed fix

- [ ] `runtime_tools`：Agent 显式关闭 web 时 fail-closed，不再 fallback 到进程级 `WebToolsShouldRegister()`
- [ ] （可选）系统提示：未要求时禁止换题 / 禁止擅自 web_search
- [ ] （可选）Agent loop：检测到「任务已完整收束 + 无用户新意图」时抑制后续无关 tool_call

## Acceptance

- [ ] `webToolsEnabled=false` 的 Agent，registry 中无 `web_search`
- [ ] 复现同类型长 RCA 会话时，不应再出现无关法律/政策 web_search 段落（至少工具层不可用）

## Screenshots

（截图见下方评论；若未显示请打开 draft release: https://github.com/sixath/portal/releases/tag/untagged-edd8f1089578a956e957 ）
