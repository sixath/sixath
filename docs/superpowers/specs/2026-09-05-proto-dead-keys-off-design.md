# S37 收口：regen proto 去掉死字段

**日期**: 2026-09-05  
**状态**: 已确认（S36 之后用户继续；选 proto regen，不改 Channel / MaybeSpill）  
**范围**: `agent.proto` 的 `code_*` / `hub_*` / `mea_enabled`；`conf.proto` 里已无 worker 的 Growth 字段与 Skills 预注入死键。保留 `growth.llm`（含 auxiliary）。不合 assembler。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S36](./2026-09-05-remaining-dead-shell-off-design.md)

**一句话**: Web 和 yaml 已经不写这些键；proto / biz / JSON round-trip 还在假装它们是 API。regen 掉。

---

## 1. 背景

S25–S36 拆了切模、Hub 管理面、MEA 勾选、Growth worker yaml。磁盘 leftover：

| leftover | 现网 |
|----------|------|
| `ModelConfig.code_*` proto/biz/data | Update 仍 round-trip 进 `model_config` JSON |
| `RuntimeToolsConfig.mea_enabled` / `hub_*` | Update 仍 merge / 写 JSON |
| `conf.Growth` worker/curator/learnings 字段 | 发货 yaml 已无这些键；proto 仍解析 |
| `conf.Skills.auto_route_enabled` 等 | 发货 yaml 仍写 false；P3 后无调用者 |
| `conf.Growth.llm` + `auxiliary` | **仍**给 `/route` |
| Channel `auto_route_*` / `MaybeSpill` | **还在干活** |

下次 Agent 保存会按新 struct marshal：库里的死 JSON 键会被丢掉。本刀**不**做独立 SQL 迁移，也**不** DROP 表。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| `ModelConfig` `code_provider`–`code_base_url` | **删除**（reserved 6–9） |
| `RuntimeToolsConfig` `hub_*` / `mea_enabled` | **删除**（reserved 10–13）；**保留** `hybrid_recall` |
| `conf.Growth` | **只留** `llm = 5`；其余 reserved |
| `GrowthLLM` | 保留 provider/model/api_key/base_url/`max_transcript_runes`/`auxiliary`；删复盘 prompt 死键 |
| `conf.Skills` 预注入键 | **删除** `auto_route_enabled` / `route_min_score` / `route_max_body_runes`；保留 `allow_script_execution` |
| Channel proto / `MaybeSpill` / assembler | **不改 / 不合** |

---

## 3. 行为

```text
Create/Update Agent 不再接受或回写 code_* / mea_enabled / hub_*
GET Agent 也不再带这些字段
/route 仍从 growth.llm（auxiliary 优先）装配
发货 yaml skills 只留 allow_script_execution
已有行里的死 JSON 键：下次 Update 该对象时丢掉，不做批量擦库
```

---

## 4. 非目标

- 不改 Channel / Gateway `auto_route_*`
- 不改 `MaybeSpill` / `hybrid_recall`
- 不 DROP 表、不改历史 SQL migration
- 不合 assembler

---

## 5. 成功标准

1. `portal/api/agent/v1/agent.proto` 不含 `code_provider` / `mea_enabled` / `hub_governance`。
2. `portal/internal/conf/conf.proto` 的 `Growth` 不含 `worker_enabled`；`Skills` 不含 `auto_route_enabled`。
3. `cd portal && make config && make api` 后 `go build ./...` 通过。
4. `cd portal && go test ./internal/biz ./internal/data ./internal/service ./internal/conf ./internal/runtime -count=1` 绿（skip 预存 SQLITE_BUSY）。
