## Summary

在对话中完成 `cloudgame` 仓库模块调用关系分析后，同一条 assistant 回复末尾硬接「7天无理由退货政策」法律条文与案例。根因不是 Memory Hub / wiki / Neo4j 污染，而是 **正题收束后 Agent loop 未停，模型自行开新意图并继续 tool_call**；本次碰巧用了 `web_search`，但关联网只能堵住「不该搜却搜了」，**关了 web 仍可能滥用 `rca_*` / knowledge / 本地读写等其它工具去填无关话题**——关联网是治标，防跑题才是治本。

另有配套缺陷：Agent `webToolsEnabled=false` 时进程级仍可能因 Bocha key 自动注册联网工具。

## Evidence

- Session: `fb9f9c75-dddd-4824-a571-bc0127e9764a`（agent `e8107fb3-e40a-4207-9d9a-6768847aaf79` / zone-4100-agent）
- UI: `http://localhost:5173/?agent=e8107fb3-e40a-4207-9d9a-6768847aaf79&session=fb9f9c75-dddd-4824-a571-bc0127e9764a`
- Timeline 工具计数（约）：`rca_read`×18、`rca_glob`×12、`web_search`×6；**无** `knowledge_search` / `memory_recall`
- 正文衔接：`…请告诉我。# 7天无理由退货政策 — 法律条文、官方解读与典型案例`
- 第一次 `web_search` query：`消费者权益保护法 第二十五条 七日无理由退货 原文`（此前正文无「退货/消费者」等字样）
- Agent `runtime_tools.webToolsEnabled=false`，但进程仍注册了 `web_search`

## Root cause (current understanding)

1. **（治本）话题漂移 / 工具滥用**：长 RCA 工具链后模型写完正题，Agent loop 未硬停；下一轮自行开新题并用工具填充「可查证」内容。关掉 `web_search` 并不能阻止同类行为换用其它工具。
2. **（治标 / 配套）注册旁路**：`internal/chat/runtime_tools.go` 在 `WebToolsEnabled` 为 false 时仍可能走 `WebToolsShouldRegister()`（有 Bocha/Tavily key 即 true）：

```go
if flags.WebToolsEnabled {
    registerWebTools(reg, true)
} else if WebToolsShouldRegister() {
    RegisterWebTools(reg)
}
```

3. `web_prompt.go` 中「多次 web_search 要按章节写完整」会放大跑题后的长文输出（仅在 web 已注册时生效）。

## Expected

- **治本**：用户未要求换题时，正题收束后不应擅自开新 topic，也不应继续发起无关 `tool_call`（不论是否联网工具）。
- **配套**：Agent `webToolsEnabled=false` 时：**绝不**注册 `web_search` / `web_extract`。

## Proposed fix

### P0 — 治本：收束后停止跑题与无关工具

- [ ] Agent loop：检测到「任务已完整收束 + 无用户新意图」时硬停 / 抑制后续无关 `tool_call`（需确认再继续亦可）
- [ ] 系统提示：未要求时禁止自行换题、禁止为无关章节继续调用任意工具

### P1 — 配套：联网能力 fail-closed

- [ ] `runtime_tools`：Agent 显式关闭 web 时 fail-closed，不再 fallback 到进程级 `WebToolsShouldRegister()`
- [ ] （可选）收敛 `web_prompt`：避免在已跑题场景鼓励「按章节写完整」

## Acceptance

- [ ] 复现同类型长 RCA 会话时：正题收束后**不再**擅自开启无关 topic（法律/政策等），也**不再**为此发起任意无关工具调用（不止 `web_search`）
- [ ] `webToolsEnabled=false` 的 Agent，registry 中无 `web_search` / `web_extract`

## Screenshots

代码分析收束后硬接「7天无理由退货」：

- 全貌：[topic-drift-full.png](https://github.com/sixath/portal/releases/download/untagged-edd8f1089578a956e957/topic-drift-full.png)
- 拼接处特写：[topic-drift-splice.png](https://github.com/sixath/portal/releases/download/untagged-edd8f1089578a956e957/topic-drift-splice.png)

> 私有仓库的 Release 资源无法被 GitHub camo 内嵌渲染；原图在 draft release `issue-7-screenshots`。本地副本：`d:\workspace\github\sixath\_neo4j_q\issue7_assets\`

