# S35 收口：Agent 表单退出 MEA 假入口

**日期**: 2026-09-05  
**状态**: 已确认（用户继续清货架；S27 leftover；2026-09-05 实施）  
**范围**: Web Agent 表单/详情的 `mea_enabled` 勾选。不 regen proto，不改 Channel，不合 assembler。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S27](./2026-09-05-mea-off-design.md)；[S34](./2026-09-05-remaining-growth-off-design.md)

**一句话**: `framework/mea` 和 Portal `mea_run` 已不存在；设置页还在勾「MEA 长程验收」。拆掉。

---

## 1. 背景

S27 删了 `framework/mea`。磁盘（`Test-Path`）：

| leftover | 现网 |
|----------|------|
| `portal/internal/chat/mea_*.go` / `mea_stream.go` | **不存在**；Chat 不旁路 ReAct |
| Web `RUNTIME_TOOL_FIELDS` | 仍有 `mea_enabled` 勾选与详情 badge |
| proto / biz / DB `mea_enabled` | 死键，Update 仍 round-trip |

父规格 §6.3：MEA 移出默认、不重写。外壳不得再假装能开。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| Web `mea_enabled` 勾选 / 详情展示 | **删除** |
| `normalizeRuntimeTools` / `serializeRuntimeTools` | **不再读写** `mea_enabled` |
| proto / biz / data `MEAEnabled` | **保留死键**（不 regen proto；本刀不擦库里的 JSON） |
| `MaybeSpill` / Channel `auto_route_*` / `config.HyperTool` | **不改** |
| assembler | **不合** |

---

## 3. 行为

```text
Agent 新建/编辑页不再出现「MEA 长程验收」勾选
Agent 详情不再把 mea_enabled 列成已启用工具
默认 Chat 仍不跑 MEA
已有 Agent 的 runtime_tools.mea_enabled JSON 不被本刀迁移擦除
```

---

## 4. 非目标

- 不 regen proto
- 不改 Channel / Gateway `auto_route_*`
- 不改 `MaybeSpill`
- 不删 `config.HyperTool` yaml 死键
- 不合 assembler

---

## 5. 成功标准

1. `web/src/api/client.ts` 的 `RUNTIME_TOOL_FIELDS` 不含 `mea_enabled`。
2. Agent 表单无 `runtime-tool-mea_enabled`。
3. `portal/internal/service/chat.go` 不含 `streamWithRulesMEA` / `MEAEnabledForAgent`。
4. `cd portal && go test ./internal/service ./internal/chat -count=1` 绿（skip 预存 SQLITE_BUSY）。
5. Web e2e：`agent-runtime-tools.spec.ts` 断言 MEA 勾选不出现。
