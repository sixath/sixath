# S38 收口：停 PortalSetting AutoMigrate

**日期**: 2026-09-05  
**状态**: 已确认（S37 之后用户继续；选 S36 leftover：表模型已无读写）  
**范围**: `model.PortalSetting` 与 `data.go` AutoMigrate。不 DROP `portal_settings` 表。不改 Channel / MaybeSpill / `growth.llm`。不合 assembler。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S36](./2026-09-05-remaining-dead-shell-off-design.md)；[S37](./2026-09-05-proto-dead-keys-off-design.md)

**一句话**: S36 删了 Get/Put；启动时还在给一张没人读写的全局配置表做 AutoMigrate。停掉，表留下。

---

## 1. 背景

S36 锁定：删 `portal_settings.go` 的 `Get/PutCodeModelSetting`；**表与 AutoMigrate 留下**。磁盘（`rg`，排除 `_neo4j_q`）：

| leftover | 现网 |
|----------|------|
| `portal/internal/data/portal_settings.go` | **不存在** |
| `model.PortalSetting` / `PortalSettingJSON` | 只出现在模型文件与 `data.go` AutoMigrate |
| HTTP `/settings/code-model` | **不存在** |
| Channel `auto_route_*` / `MaybeSpill` / `growth.llm` | **还在干活，不是洞** |

没有生产读写后，AutoMigrate 只是每次启动假装还有全局配置器官。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| `data.go` AutoMigrate 的 `&model.PortalSetting{}` | **删除** |
| `portal/internal/data/model/portal_setting.go` | **删除** |
| 已有 `portal_settings` 表 | **不 DROP**（与 S34/S36 历史表口径一致） |
| Channel / `MaybeSpill` / `growth.llm` / assembler | **不改 / 不合** |

---

## 3. 行为

```text
Portal 启动不再 AutoMigrate portal_settings
model.PortalSetting / PortalSettingJSON → 不存在
已有库里的 portal_settings 行原样留下，本刀不读不写不删
```

---

## 4. 非目标

- 不 DROP `portal_settings` / growth 历史表
- 不改 Channel / Gateway `auto_route_*`
- 不改 `MaybeSpill`
- 不改 `/route` / `growth.llm`
- 不合 assembler

---

## 5. 成功标准

1. `portal/internal/data/model/portal_setting.go` 不存在。
2. `portal/internal/data/data.go` 不含 `PortalSetting`。
3. `cd portal && go test ./internal/data -count=1` 绿（skip 预存 SQLITE_BUSY）。
