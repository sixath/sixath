# Chat 执行时间线持久化设计

- 日期：2026-07-09
- 状态：已批准，实现中
- 范围：仅 `metadata.timeline`（sources 另开）

## 问题

工具/模型调用时间线仅存在于本轮 SSE 的前端内存（`messageTimelines`）。`SaveAssistantMessage` 只落库 `content`，刷新后 `listMessages` 无法回放时间线。

## 方案

在 `chat_messages.metadata`（JSON）中存储 finalize 后的 `TimelineNode[]`，与前端 reducer 输出同构（camelCase）。

```json
{ "timeline": [ { "kind": "tool", "id": "...", "step": 1, ... }, { "kind": "model", "step": 1, ... } ] }
```

## 数据流

1. `chat_sse` 累积本轮 `tool_call` / `model_call`
2. 流结束 `finalize` → `SaveAssistantMessage(session, content, metadata)`
3. `ListMessages` 返回 `MessageReply.metadata`
4. 前端：`messageTimelines[key] ?? m.metadata?.timeline ?? []`

## 非目标

- 不持久化 `sources`
- 不靠 `RunTrace` 重建（缺模型节点）
- 不做未截断结果按需回拉
