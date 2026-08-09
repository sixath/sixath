# Growth 远期路线图 R1–R3 可行性调研

**关联**：[growth-system-design §9](./2026-05-10-growth-system-design.md) · [phase2 计划](../plans/2026-05-10-growth-system-phase2.md) §1.5 · [Hermes 对照](../../../../hermes-growth-architecture.md)（仓库根）  
**状态**：调研稿（2026-05-18）  
**范围**：评估 R1 / R2 / R3 **是否适合**与已交付 Growth phase2（2.1–2.4）同里程碑合并；给出依赖、风险与立项前 checklist。

---

## 1. 结论摘要

| ID | 一句话 | 与当前 Growth PR 关系 | 建议 |
|----|--------|----------------------|------|
| **R1** | 跨会话 FTS/向量 + `session_search` 类工具 | **正交**（检索数据面 + agent 工具） | **R1a/R1b 已落地**（见 [session-search-r1](./2026-05-19-session-search-r1.md)）；R1c 向量未做 |
| **R2** | Curator 周期合并 + cron 技能引用反写 | **部分相关**（技能写回后的长期治理） | 先做 **引用链调研**（§4），再立项 |
| **R3** | 轨迹导出/压缩 → RL | **解耦**（训练管线） | **独立 training/observability epic** |

**当前不存在「R1–R3 实现有 bug」**：代码库未实现这三项；Growth phase2 已交付部分与 R1–R3 **无运行时冲突**，但文档上勿将 R1–R3 计入 phase2 验收。

---

## 2. 已交付基线（Growth phase2 vs 远期）

### 2.1 已有（phase2）

- 触发：工具/assistant 计数、`growthwake`、空闲扫描（C1）、可选会话结束记忆轻检（C2）。
- 技能环：`SkillReviewRunner` + patch 写盘 + `skills_index_generation` bump。
- 记忆环：`NotifyMemorySessionDirty` + `MemoryState` 注入复盘 prompt（B1）。
- 多实例：workspace 租约、重试/退避、metrics。

### 2.2 已有但与 R1–R3 易混淆

| 能力 | 位置 | 不是 R1/R2/R3 的原因 |
|------|------|----------------------|
| 工作区内 memory FTS/vector | `framework/memorysearch`（`builtin.go` 等） | **单工作区/会话记忆索引**，非跨会话 `session_search` |
| LLM prompt「Skill Curator」 | `framework/growth/runner_llm.go` | **文案角色**，非 Hermes `curator.py` 周期作业 |
| Cron `skill_execute` | `portal` `cron_tasks.payload_kind` | 有 payload 类型，**无**技能名反写 / Curator 合并 |
| Growth 事件 | `framework/events` | 可观测，**非** ShareGPT 训练轨迹 |

---

## 3. R1 — 跨会话 FTS / 向量检索 + 工具

### 3.1 Hermes 参照

- FTS5 双表（默认 + trigram CJK）、消息触发器入库。
- `session_search` 工具：FTS 命中 → `parent_session_id` 折叠 → 排除当前会话 → 辅助 LLM 摘要（限流）。
- 见仓库根 `hermes-tool.md` § `session_search`、`hermes-growth-architecture.md` 支柱三。

### 3.2 sixath 缺口

- 无跨会话 FTS 虚表 / 与 `chat_messages` 同步机制。
- 无 agent 工具 `session_search`（或等价）注册。
- 无「沿 parent 链折叠 + 并发摘要」编排层。
- `memorysearch` 仅服务 **当前 agent workspace** 下的 memory 文件/chunk，索引范围不同。

### 3.3 主要风险与阻塞

| 风险 | 说明 | 缓解 |
|------|------|------|
| 数据面未定 | MySQL vs SQLite sidecar vs 独立检索服务 | 先写 **R1-spec**：schema、同步、删除策略 |
| 与首期承诺冲突 | design §1 明确首期不做 FTS | 单独版本线 / feature flag，不 retroactive 改 growth v1 验收 |
| 成本 | 每命中会话一次辅助 LLM | 默认 max 摘要数、空查询只返元数据（对齐 Hermes） |
| 隐私 / 多租户 | 跨会话 = 跨用户边界需鉴权 | session 归属 agent/user 过滤必须在检索 SQL/API 层 |

### 3.4 建议 epic 切分

1. **R1a**：消息 FTS 索引 + 增量同步（不含工具）。
2. **R1b**：`session_search` 工具 + portal 注册 + 集成测试（golden queries）。
3. **R1c**（可选）：向量混合检索与 FTS 融合（可复用 `memorysearch` hybrid 思路，但索引域仍不同）。

### 3.5 R1 立项前 checklist

- [x] 写入 [session-search-r1](./2026-05-19-session-search-r1.md)（2026-05-19 MVP）。
- [ ] 确认存储：`chat_messages` 来源表与触发器/批同步方案。
- [ ] 定义工具 schema（参数、max_results、摘要模型、超时）。
- [ ] 安全：agent_id / user_id 隔离策略与审计日志。
- [ ] 与 Growth：**不**在 `GrowthWorker` 内调用 R1；agent 运行时按需调工具即可。

---

## 4. R2 — Curator 类合并 + cron 技能引用反写

### 4.1 Hermes 参照

- `maybe_run_curator()`：空闲门控（如 7 天 + 2h）→ 合并/删技能 → `rewrite_skill_refs` 更新 cron。
- 见 `hermes-growth-architecture.md`「长期巩固: Curator」。

### 4.2 sixath 缺口

- 无 Go 版 Curator 调度器（周期、门控、合并策略）。
- 无 `rewrite_skill_refs` 或等价逻辑。
- Growth `ApplyPatchBatch` 是 **阈值触发的单次复盘**，不是空闲批量合并。

### 4.3 引用链调研表（**R2 前置**）

**调研状态**：代码库静态调研 **已完成**（2026-05-18）；**生产库 `cron_tasks` 行样本仍待填**（见 §4.3.2）。

**调研人**：代码扫读（agent）；**生产 SQL 复核**：____

| # | 引用来源 | 存储位置 | 引用形式 | 代码库示例 / 约定 | 合并/重命名后需反写？ | R2c 优先级 |
|---|----------|----------|----------|-------------------|----------------------|------------|
| 1 | Cron **`skill_execute`** | `cron_tasks.payload_content` | **纯文本相对路径**，非 JSON：`{skillName}/scripts/{file}` | `daily-report/scripts/run.sh`（见 `portal/internal/cron/executor.go` L143–151） | **是** — 第一段须与 `skills.Index.GetByName` 的 **frontmatter `name`** 一致 | **P0** |
| 2 | Cron **`agent_turn`** | 同上 | **自然语言**用户消息，无结构化 skill 字段 | `"总结昨天的待办与完成情况"`（`portal/docs/architecture_design.md` §5.6） | **否**（机器反写不可行；最多依赖 agent 自行 `load_skill`） | — |
| 3 | Agent 配置 | `agents` 表 | `workspace` 目录路径；`system_prompt` 自由文本 | 无技能名列表字段（`portal/internal/data/model/agent.go`） | **否**（仅路径级；技能在 workspace 子树） | — |
| 4 | ReAct 工具调用 | 运行时 / `chat_messages` 等 | `execute_skill_script` 参数 `name` + `path` | `name` = frontmatter `name`；`path` = `scripts/...`（`framework/tool/skill_tools.go`） | **否**（历史消息；R1 检索可覆盖） | P2 可选 |
| 5 | Growth patch | workspace 文件 | 相对路径 `skills/.../SKILL.md` | `skills/test/SKILL.md`（`framework/growth` 测试与 LLM prompt） | **是**（Curator 合并即改此处；与 cron 无自动联动） | **P0**（filesystem） |
| 6 | Skills 索引 | `workspace/skills/**/SKILL.md` | 权威 ID = frontmatter **`name`**（kebab-case）；**目录名可不同** | `Index.byName[meta.Name]`（`framework/skills/index.go`） | **是**（删/并/改名必须同步 #1、#5） | **P0** |

#### 4.3.1 关键结论（避免 Hermes 假设直接照搬）

1. **`skill_execute` 第一段不是「目录名」而是逻辑名**  
   `Executor` → `AgentService.ExecuteSkill` 将 `payload_content` 按 `/` 拆分，第一段作为 `skillName` 传入 `ExecuteSkillScript` → `idx.GetByName(skillName)`。  
   因此 Curator **合并/改名 frontmatter `name`** 时，必须重写 cron 行首段（或整条 path），不能只改磁盘目录名。

2. **`agent_turn` 不构成可反写引用链**  
   任务内容是指令文本；技能调用发生在当次 ReAct 内，与 Hermes `rewrite_skill_refs` 场景 **不对等**。R2c 可 **仅覆盖 `skill_execute`**。

3. **R2 反写范围可收敛**  
   - **必做**：`cron_tasks` 中 `payload_kind = 'skill_execute'` 的 `payload_content`（按 `agent_id` join `agents.workspace` 限定范围）。  
   - **必做**：workspace 内 `skills/**` 树（与 Growth `ApplyPatchBatch` / 未来 Curator 同一写盘模型）。  
   - **不做**：`agent_turn` 文本、agent `system_prompt` 全文替换。

4. **与 Growth phase2 关系**  
   Growth 已能 `create/patch/delete` `skills/*/SKILL.md`，但 **不会** 扫描/更新 `cron_tasks`；这正是 R2c 的缺口。

#### 4.3.2 生产样本 SQL（待运维执行）

```sql
-- 按 agent 查看 skill_execute 任务（替换 LIMIT）
SELECT id, name, agent_id, payload_kind, LEFT(payload_content, 120) AS payload_preview, enabled
FROM cron_tasks
WHERE payload_kind = 'skill_execute'
ORDER BY updated_at DESC
LIMIT 10;

-- 统计两类 payload 占比
SELECT payload_kind, COUNT(*) AS n FROM cron_tasks GROUP BY payload_kind;
```

将结果填入下表（留空表示尚未拉生产）：

| task_id | task_name | payload_content（完整） | 第一段 name | 与磁盘目录/frontmatter 是否一致 |
|---------|-----------|-------------------------|-------------|--------------------------------|
| _生产待填_ | | | | |
| _生产待填_ | | | | |
| _生产待填_ | | | | |

#### 4.3.3 `payload_content` 契约（建议写入 R2-spec）

| `payload_kind` | 类型 | 必填 | 格式 | 校验入口 |
|----------------|------|------|------|----------|
| `skill_execute` | string | 是 | `{name}/scripts/{filename}`；`name` 匹配 `^[a-zA-Z0-9_-]+$`（与 `AgentService.skillNamePattern` 一致）；`scripts/` 前缀强制 | `portal/internal/service/agent.go` `ExecuteSkill` |
| `agent_turn` | string | 是 | 任意 UTF-8 文本，作为单次 `ChatRequest.Content` | `portal/internal/cron/executor.go` `runAgentTurn` |

**文档示例（非生产数据）**：

```json
{
  "payload_kind": "skill_execute",
  "payload_content": "daily-report/scripts/run.sh"
}
```

```json
{
  "payload_kind": "agent_turn",
  "payload_content": "总结昨天的待办与完成情况"
}
```

### 4.4 主要风险

| 风险 | 说明 |
|------|------|
| 无引用链 | 做了 Curator 合并却无法反写 cron → 定时任务静默失败 |
| 与 Growth 租约 | Curator 与 `GrowthWorker` 同写 workspace 需 **共享 workspace 租约** 或串行化 |
| 合并策略 | 删/并 SKILL 的产品规则（pinned 技能拒写等）需单独 spec |
| 与 R1 | 合并后旧 skill 名可能只存在于历史会话 FTS → R1 与 R2 发布顺序需协调 |

### 4.5 建议 epic 切分

1. **R2a**：引用链调研 + `payload_content` schema 文档化（§4.3 代码库已预填）。
2. **R2b**：**已落地** — `framework/growth/curator*.go`、`portal` `CuratorWorker`、迁移 `004`、`portal/docs/growth-curator-r2b.md`。
3. **R2c**：**已落地** — `framework/growth` 提取 rename + `RewriteSkillExecutePayload`；`portal` `CronRefRewriteUsecase` 在 Growth/Curator patch 成功后反写 `skill_execute`（见 `portal/docs/growth-cron-rewrite-r2c.md`）。生产样本表 §4.3.2 仍可用于验收真实 payload 形态。

### 4.6 R2 立项前 checklist

- [x] 完成 §4.3 引用链表（**代码库静态调研**，2026-05-18）。
- [ ] 执行 §4.3.2 SQL，填入 ≥3 条真实 `skill_execute` 样本并核对 name/目录/frontmatter 一致性。
- [ ] 定义合并 API：输入/输出、与 `growth.ValidatePatchBatch` 关系（复用 vs 独立）。
- [ ] 与 Growth worker 的锁策略（同 `growth_workspace_leases`）。
- [ ] 失败策略：合并一半失败是否回滚（建议对齐 `ApplyPatchBatch` 整批语义）。

---

## 5. R3 — 轨迹导出与压缩

### 5.1 Hermes 参照

- `_save_trajectory` → ShareGPT JSONL；scratchpad 脱敏；`trajectory_compressor` 压中段。
- 对接 `batch_runner` / RL envs（见 `hermes-growth-architecture.md` 支柱五）。

### 5.2 sixath 缺口

- Go 代码库 **无** trajectory 包或导出钩子。
- `framework/events` 的 growth 事件 **不是** 训练样本格式。

### 5.3 主要风险

| 风险 | 说明 |
|------|------|
| 范围膨胀 | 导出 + 压缩 + RL 训练是三件事 |
| 合规 | 全量对话落盘需 retention / PII / 用户同意 |
| 与 Growth 混淆 | 复盘写技能 ≠ 训练数据；钩子应挂在 **chat 完成路径**，非 worker |

### 5.4 建议 epic 切分

1. **R3a**：只读导出 JSONL（`completed` / `failed` 分流），无压缩、无训练。
2. **R3b**：`compress_trajectory` 等价（首尾保留 + 中段 summary）。
3. **R3c**：对接外部训练管线（若需要）。

### 5.5 R3 立项前 checklist

- [ ] 定义 ShareGPT（或 OpenAI messages）字段映射与 `redacted_thinking` 规则。
- [ ] 挂载点：`ChatService` 流式/非流式 onDone vs 独立 batch job。
- [ ] 存储路径、轮转、默认 **关闭** 开关。
- [ ] 明确 **不** 依赖 Growth pending/租约。

---

## 6. 推荐实施顺序

```text
R2a 引用链调研 ──┬──► R2b Curator（workspace only）
                 └──► R2c cron 反写（若表格证明需要）

R1a 索引 ──► R1b session_search 工具
                │
                └──► 可与 R2 并行，但发布时注意 skill 重命名与历史检索一致性

R3a 导出 ──► R3b 压缩 ──► R3c RL（可选，独立团队）
```

**不建议**：在 `feat/growth-system` / PR #1 中附带 R1–R3 任一完整交付。

---

## 7. 与 Growth phase2 验收的边界

| 验收项 | 包含 R1–R3？ |
|--------|--------------|
| [growth-system-acceptance](./2026-05-10-growth-system-acceptance.md) §9 | **否**（不适用） |
| phase2 里程碑 2.1–2.4 | **否** |
| phase2 里程碑 2.5 原文「R1 专项另立 epic」 | **R1 独立**；E1/E2/T2 属生产化，已部分完成 |

---

## 8. 修订记录

| 日期 | 变更 |
|------|------|
| 2026-05-18 | 初版：R1–R3 可行性、风险、checklist、引用链调研表。 |
| 2026-05-18 | §4.3 预填：cron/agent/skills/growth 引用形式；`skill_execute` 与 frontmatter `name` 对齐说明；生产 SQL 模板。 |
| 2026-05-18 | **R2b 实现**：CuratorRunner + CuratorWorker + `growth_curator_states`；默认 `curator_enabled=false`。 |
