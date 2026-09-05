# S11 收口：Insights 退出产品外壳

**日期**: 2026-09-05  
**状态**: 已确认（父规格 §5.6 / §6.3；S1 leftover；2026-09-05 实施）  
**范围**: Web 路由与页面、Portal `GET .../insights`。不改 Rewind、不删 turn-trace、不改 Growth 包。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S1](./2026-09-05-dead-code-hub-off-design.md)、P4（已藏导航）

**一句话**: Insights 不再以隐藏路由留在外壳里；和 Hub 管理面一样拆掉。

---

## 1. 背景

父规格 §5.6：Web KEEP 不含 Insights（随 Growth 降级）。P4 只去掉详情导航；S1 明确 **不删** `App.tsx` 路由。直链 `/agents/:id/insights` 与 `GET /api/v1/agents/{id}/insights` 仍活着。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| Web | 删路由、`AgentInsightsPage`、`chatApi.getInsights`、`AgentInsights` 类型 |
| Portal HTTP | 删 `InsightsHandler` 与 `/insights` 注册 |
| 聚合实现 | 删 `GetInsights` / `AggregateInsights` 及测试 |
| Rewind | **保留** `POST .../rewind` 与 turn-trace 存储 |
| Growth / MEA / Hub 包 | **不删** |

---

## 3. 行为

```text
GET /api/v1/agents/{id}/insights → 不再注册
/agents/:id/insights → 无匹配路由
Rewind / turn_traces 不变
```

---

## 4. 非目标

- 不删 `framework/agent` 别名
- 不改 `NewChatStreamHandler`
- 不拆 `growth_chat.go` / procedural 包
- 不合 assembler

---

## 5. 成功标准

1. `portal/internal/server/http.go` 不含 `/insights`。
2. `web/src/App.tsx` 不含 Insights 路由；无 `AgentInsightsPage.tsx`。
3. Rewind 路由仍在。
4. `cd portal && go test ./internal/server ./internal/service -count=1` 绿（skip 预存 SQLITE_BUSY）。
