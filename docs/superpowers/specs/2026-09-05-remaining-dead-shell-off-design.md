# S36 收口：一次性拆掉剩余死壳

**日期**: 2026-09-05  
**状态**: 已确认（用户选「不 regen proto」的 leftover 清扫；2026-09-05 实施）  
**范围**: Web `code_*` 死映射、`config.HyperTool`、发货 yaml 里已无 worker 的 Growth 开关、Portal `portal_settings` 的全局 code 模型读写。不 regen proto，不改 Channel / MaybeSpill / `growth.llm`，不合 assembler。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S35](./2026-09-05-mea-shell-off-design.md)；[S26](./2026-09-05-hypertool-off-design.md)；[S25](./2026-09-05-shelf-family-code-model-off-design.md)；[S34](./2026-09-05-remaining-growth-off-design.md)

**一句话**: 器官和假入口已经拆完；yaml / client / config 还在假装能开 HyperTool、Growth worker、源码切模。清掉死壳，活着的 `/route`、Channel、MaybeSpill 不动。

---

## 1. 背景

S25–S35 拆了切模 UI、HyperTool 实现、Growth OS、MEA 勾选。磁盘 leftover（`Test-Path` / `rg`，排除 `_neo4j_q`）：

| leftover | 现网 |
|----------|------|
| Web `ModelConfig.code_*` / `normalizeModelConfig` | 设置页已无切模；PUT 仍可能 round-trip |
| `config.HyperTool` | `hypertool.go` **不存在**；字段只解析 yaml |
| 发货 yaml `worker_enabled` / curator / learnings / `llm_review_*` | worker 已删；键仍写 false |
| `portal_settings.go` `Get/PutCodeModelSetting` | HTTP `/settings/code-model` **不存在** |
| proto / biz `mea_enabled` / `hub_*` / Agent `code_*` / `conf.Growth` 布尔 | **本刀不 regen** |
| `conf.Growth.llm` | **仍**给 `/route` 分类器 |
| Channel `auto_route_*` / `MaybeSpill` | **还在干活，不是洞** |

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| Web `code_*` | **不再读写** |
| `HyperToolConfig` / `Config.HyperTool` | **删除**（未知 yaml 键忽略） |
| 发货 yaml Growth worker/curator/learnings/review 开关 | **删除**；保留 `growth:` + `llm` 注释/环境变量说明 |
| `GetCodeModelSetting` / `PutCodeModelSetting` / `portal_settings.go` | **删除**；`PortalSetting` 表与 AutoMigrate **留下** |
| proto / biz / DB 死字段 | **保留**（不 regen） |
| `conf.Growth.llm` / `EnrichGrowthFromEnv` | **保留** |
| Channel `auto_route_*` / `MaybeSpill` / assembler | **不改 / 不合** |

---

## 3. 行为

```text
Agent 保存不再把 code_* 写进 model_config JSON（Web 侧）
framework Config 不再有 HyperTool 字段
发货 yaml 不再列出 worker_enabled / curator / learnings 开关
全局 code 模型不再有 data 层读写 API
/route 仍可从 growth.llm 或 SATH_GROWTH_LLM_* 装配
```

---

## 4. 非目标

- 不 regen proto（`mea_enabled`、`hub_*`、Agent `code_*`、`conf.Growth` 布尔留下）
- 不改 Channel / Gateway `auto_route_*`
- 不改 `MaybeSpill`
- 不 DROP `portal_settings` / growth 历史表
- 不合 assembler

---

## 5. 成功标准

1. `web/src/api/client.ts` 的 `ModelConfig` / `normalizeModelConfig` 不含 `code_provider` / `code_model` / `code_api_key` / `code_base_url`。
2. `framework/config/config.go` 不含 `HyperTool` / `HyperToolConfig`。
3. `portal/configs/config.yaml` 与 `config.docker.yaml` 不含 `worker_enabled`、`curator_enabled`、`learnings_review_enabled`、`llm_review_enabled`。
4. `portal/internal/data/portal_settings.go` 不存在。
5. `cd framework && go test ./config ./tool ./templates -count=1` 绿。
6. `cd portal && go test ./internal/conf ./internal/data ./internal/service ./internal/server -count=1` 绿（skip 预存 SQLITE_BUSY）。
