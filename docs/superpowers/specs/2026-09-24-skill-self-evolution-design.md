# Agent Skill Self-Evolution Design

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 从对话中自动检测进化信号（用户纠正、模型试错、技能过时等），生成提案，经人工评审后自动创建/更新 SKILL.md 或 USER.md，形成技能自进化闭环。

**Architecture:** 对话结束后异步执行：规则粗筛 → LLM 分类 → 生成提议 → 冲突检查 + 去重 → 暂存到 proposals 表 → 用户通过独立评审页面审核 → 采纳后写入文件。

**Tech Stack:** Go (portal backend) + React/TypeScript (frontend) + MySQL (proposals 表) + 已有 `skill_manage` / `memory_remember` 工具。

---

## 1. 当前状态

### 已有基础设施（可用）

- `skill_manage(create/patch)` — 完整的技能创建/修改工具，有 confirm 流程，测试通过
- `memory_remember(scope=agent)` — 可写 USER.md / MEMORY.md
- `BuildSkillsIndex` — 自动加载 Agent 工作区 skills/ 目录
- `skill_view` / `skills_list` — 技能查询
- `FailureSignal` + `MultiFailureSink` — 唯一还在连接的组件：捕获 tool_failed 信号，记日志 + 内存 ring buffer

### 已移除（不重建）

- `NotifyMemoryExtractFromTurn` — Turn 后事实提取已完全移除
- ProceduralBinding（P3-C/D/E）— `CommitProceduralRepair` 和整个过程绑定栈已删除
- 事实提取 Prefetch 接线 — memory_units 的 facts 不再注入系统提示

### 设计原则

- **不重建已删除的组件** — 利用现有 `skill_manage` 和 `memory_remember` 作为唯一写入路径
- **不引入新存储类型** — 只新增一张 `evolution_proposals` 表
- **所有写入走人工评审** — 不做全自动写入
- **信号检测异步、低成本** — 不阻塞 turn，不放大 token 消耗

---

## 2. 进化信号类型

| # | 信号 | 触发方 | 检测方式 | 输出 |
|---|------|--------|----------|------|
| 1 | 风格/格式/语气纠正 | 用户 | 关键词粗筛→LLM分类 | USER.md patch |
| 2 | 工作流/步骤纠正 | 用户 | 关键词粗筛→LLM分类 | SKILL.md create |
| 3 | 调试技巧/修复/绕行 | 用户 | 关键词粗筛→LLM分类 | SKILL.md create |
| 4 | 技能过时/错误 | 用户/加载 | 关键词粗筛→LLM分类 | SKILL.md mark deprecated |
| 5 | 反复试错后成功 | 模型自身 | ToolFailed≥N次+成功+同类问题≥M次 | SKILL.md create |

---

## 3. 整体架构

```
每 turn 结束（异步，不阻塞响应）
    │
    ▼
┌─────────────────────────────────────┐
│ 信号检测层                            │
│ ┌──────────┐  ┌──────────┐          │
│ │ 规则粗筛   │→│ LLM 分类  │          │
│ │ (<5%命中) │  │ (haiku)  │          │
│ └──────────┘  └────┬─────┘          │
│                    │ confidence≥0.7  │
├────────────────────┼────────────────┤
│ 提议生成层          ▼                │
│ ┌──────────────────────────────┐    │
│ │ LLM 生成具体变更内容           │    │
│ │ 输入: turn全文 + signal_type  │    │
│ └──────────────┬───────────────┘    │
├────────────────┼────────────────────┤
│ 仲裁层           ▼                    │
│ ┌──────────────────────────────┐    │
│ │ ① 冲突检查 (LLM yes/no)       │    │
│ │ ② 去重 (embedding cosine)    │    │
│ └──────────────┬───────────────┘    │
├────────────────┼────────────────────┤
│ 暂存层           ▼                    │
│ ┌──────────────────────────────┐    │
│ │ 写入 evolution_proposals 表   │    │
│ │ status = pending             │    │
│ └──────────────────────────────┘    │
└─────────────────────────────────────┘
            │
            ▼
┌─────────────────────────────────────┐
│ 评审页面 (前端)                       │
│ /evolution-review                    │
│ ┌──────────────────────────────────┐ │
│ │ 提案列表 + diff预览 + 采纳/编辑/拒绝│ │
│ └──────────────────────────────────┘ │
│            │                         │
│     采纳   ▼                         │
│ ┌──────────────────────────────────┐ │
│ │ 执行写入:                          │ │
│ │ skill_manage(create/patch)        │ │
│ │ memory_remember(USER.md)          │ │
│ │ 追加 EVOLUTION.log               │ │
│ └──────────────────────────────────┘ │
└─────────────────────────────────────┘
```

---

## 4. 配置

```yaml
# agent_extra.yaml 新增
evolution:
  enabled: true
  
  # 规则粗筛关键词
  rules:
    style_correction:
      - "太啰嗦"
      - "简洁"
      - "短一点"
      - "stop doing"
      - "记住"
      - "别这样"
    workflow_correction:
      - "应该先"
      - "顺序不对"
      - "下次记住"
      - "以后"
      - "步骤"
      - "流程"
    debugging_trick:
      - "原来是这样"
      - "找到原因"
      - "根因"
      - "绕过去"
      - "换个方式"
      - "解决了"
    stale_skill:
      - "这个技能"
      - "过时"
      - "不管用"
      - "不对"
      - "这个不对"
  
  # 模型试探错检测
  trial_and_error:
    min_tool_failures: 3
    min_occurrences: 2
    observation_window: 50
    similarity_threshold: 0.8
  
  # LLM 分类模型
  classifier:
    provider: openai
    model: gpt-4o-mini
    max_tokens: 200
  
  # 去重阈值
  dedup_threshold: 0.85

  # Embedding（去重 + 晋升检测）
  embedding:
    provider: openai
    model: text-embedding-3-small
    # provider 不可用时：去重 → 跳过（fail-open），晋升 → 跳过

  # 所有提案必须走评审页面（唯一选项）
  # 无 auto_promote: agent 的跳过评审选项

# 环境变量覆盖
# SATH_EVOLUTION_ENABLED=true|false
# SATH_EVOLUTION_TRIAL_MIN_FAILURES=3
# SATH_EVOLUTION_TRIAL_MIN_OCCURRENCES=2
```

---

## 5. 信号检测详细流程

### 5.1 规则粗筛

纯字符串匹配，不调用 LLM。五种信号的检测并行执行。

| 信号 | 检测逻辑 |
|------|----------|
| 风格纠正 | 用户消息命中关键词 → 标记 |
| 工作流纠正 | 用户消息命中关键词 → 标记 |
| 调试技巧 | 用户消息命中关键词 → 标记 |
| 过时技能 | 用户消息命中关键词 且 当前已加载技能 → 标记 |
| 反复试错 | ToolFailed 为事实性错误 ≥ min_tool_failures 且 最终成功 → 进试错缓冲（详见第6节） |

前四种走 LLM 分类，第五种走独立的缓冲累积检测。

**事实性错误定义：** 仅当 ToolFailed 的错误消息匹配以下模式之一时才计入试错统计：
- 参数类型不匹配（`type mismatch`、`cannot convert`、`cannot cast`、`column count`）
- 字段不存在（`column.*not found`、`field.*not found`、`unknown column`、`no such column`）
- 表不存在（`table.*not found`、`relation.*does not exist`、`no such table`）
- 视图/序列/函数不存在（`view.*not found`、`sequence.*not found`、`function.*not found`、`procedure.*not found`）
- 索引缺失（`index.*not found`、`no such index`）
- 数据库/Schema 不存在（`database.*not found`、`no such database`、`schema.*not found`）
- 语法/协议错误（`syntax error`、`parse error`、`protocol error`）
- 约束违反（`duplicate key`、`unique constraint`、`foreign key`、`cannot be null`、`check constraint`、`constraint`）
- 值域错误（`value out of range`、`overflow`、`division by zero`）

非事实性错误（网络超时、权限拒绝、限流、死锁、磁盘满、认证失败等）不进入试错缓冲。

### 5.2 LLM 分类

输入：当前 turn + 上 2 轮用户消息（最多 3000 tokens）  
模型：`evolution.classifier.model`（缺省 gpt-4o-mini）  
输出格式：

```json
{
  "is_signal": true,
  "signal_type": "style_correction",
  "confidence": 0.85,
  "summary": "用户要求回复更简洁，不要啰嗦"
}
```

confidence < 0.5 直接丢弃；≥ 0.5 且 < 0.7 标记 `review_level=low_confidence`（评审页面用黄色标注）；≥ 0.7 正常进入提议生成。

### 5.3 提议生成

根据信号类型，用不同模板：

| 信号 | 模板 | 目标 |
|------|------|------|
| 风格纠正 | "根据以下对话，生成 USER.md 的修改建议" | USER.md patch |
| 工作流纠正 | "根据以下对话，生成一个 SKILL.md" | SKILL.md create |
| 调试技巧 | "根据以下对话，生成一个 troubleshooting SKILL.md" | SKILL.md create |
| 过时技能 | "判断哪个已加载技能过时，生成废弃说明" | SKILL.md mark deprecated |
| 反复试错 | "根据失败路径和最终成功步骤，生成 SKILL.md" | SKILL.md create |

---

## 6. 试错检测缓冲

### 6.1 缓冲区结构

进程内 LRU map，key = `SHA256(agentID + 问题摘要)[:12]`，容量 256 条。

```
┌──────────────────────────────────────────┐
│ key: "a1b2c3d4e5f6"                      │
│ agent_id: "zone-4100-agent"              │
│ problem: "排查VM端口不通问题"               │
│ fail_counts: [3, 2, 4]                   │
│ success_steps: ["telnet IP port", ...]   │
│ first_seen: 2026-09-24T10:00             │
│ last_seen: 2026-09-24T14:30              │
│ occurrence: 3                            │
│ state: observing | proposed               │
└──────────────────────────────────────────┘
```

### 6.2 生命周期

- `state=observing` + `observation_window` 轮内未再次出现 → 自动淘汰
- `state=proposed` + 提案被审核（采纳/拒绝）→ 移除
- 进程重启 → 全部清空（可接受，重新观察）

### 6.3 问题摘要生成

仅当 ToolFailed ≥ `min_tool_failures` 且 turn 最终成功时，调 LLM 生成一句话摘要（不计入常规 token 统计）：

```
输入: 对话中所有 ToolFailed 的错误消息 + 最终成功的步骤
输出: 一句话问题描述，如 "排查VM端口不通时需要先telnet确认可达再netstat检查监听"
```

### 6.4 成功步骤累积

同一问题每次出现可能有不同的解法。缓冲中的 `success_steps` 使用**最长路径保留**策略：

- 每次新的成功 solve，比较新旧步骤数
- 步骤更多的方案覆盖步骤更少的（假设探索更充分）
- 步骤数相同时，保留最新的（假设时间更近的方案更优）

提案生成时，以缓冲中当前的 `success_steps` 作为技能的步骤来源。

---

## 7. 仲裁

### 7.1 冲突检查

输入：[所有已加载技能的 description] + [新内容]  
LLM 一句话判断：

```
新指令和已有指令有语义矛盾吗？
是 → 输出矛盾点
否 → 放行
```

冲突时提案照常入库，但标记 `conflict=true`，评审页面显示冲突详情。

### 7.2 去重

新技能摘要 → embedding → cosine_sim(同目录已有技能.description_embedding)  
max > `dedup_threshold`（默认 0.85）→ 拒绝提案，记录 `duplicate_of: "已有技能名"`

已有技能的 description embedding 在技能创建/修改时预计算缓存。

Embedding 模型不可用时（网关无 `/embeddings` 或超时）：
- **去重** → fail-open，跳过去重，提案照常入库（标记 `dedup_skipped=true`）
- **晋升检测** → 跳过，本次不触发晋升提案

### 7.3 冲突检查降级

冲突检查 LLM 调用失败（超时/报错）→ fail-open，提案照常入库（标记 `conflict_check_failed=true`），由人工在评审页面判断。

---

## 8. 数据模型

### 8.1 数据库

**新增表：`evolution_proposals`**

| 列 | 类型 | 说明 |
|----|------|------|
| id | VARCHAR(36) PK | UUID |
| agent_id | VARCHAR(36) | 来源 Agent |
| session_id | VARCHAR(36) | 来源会话 |
| turn_index | INT | 来源轮次 |
| signal_type | VARCHAR(32) | style/workflow/debugging/stale/trial_error |
| confidence | DECIMAL(3,2) | 分类置信度 |
| problem_summary | TEXT | 问题摘要 |
| proposed_content | TEXT | 提案具体内容 |
| target_path | VARCHAR(512) | 目标文件路径 |
| target_action | VARCHAR(32) | create/patch/deprecate |
| conflict | BOOLEAN | 是否与已有指令冲突 |
| conflict_detail | TEXT | 冲突详情 |
| conflict_check_failed | BOOLEAN | 冲突检查 LLM 调用失败 |
| dedup_skipped | BOOLEAN | 去重因 embedding 不可用而跳过 |
| status | VARCHAR(16) | pending/approved/rejected/expired |
| review_comment | TEXT | 评审意见 |
| created_at | DATETIME | 创建时间 |
| reviewed_at | DATETIME | 评审时间 |
| reviewed_by | VARCHAR(64) | 评审人 |

### 8.2 代理层

- `EvolutionProposalRepo` — MySQL CRUD
- `EvolutionUsecase` — 业务逻辑（冲突检查、去重、写入执行）
- `EvolutionService` — HTTP handler 适配

---

## 9. API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/evolution/proposals` | 列提案（支持 status 过滤、分页） |
| GET | `/api/v1/evolution/proposals/{id}` | 单个提案详情 + diff 预览 |
| POST | `/api/v1/evolution/proposals/{id}/approve` | 采纳 → 执行写入 |
| POST | `/api/v1/evolution/proposals/{id}/reject` | 拒绝 → 记录原因 |
| PATCH | `/api/v1/evolution/proposals/{id}` | 编辑后重新提交 |
| GET | `/api/v1/evolution/proposals/count` | 待审数量（侧边栏红点） |

---

## 10. 评审页面

当 `pending` 提案数量 > 0，侧边栏显示红点数字。

页面路由：`/evolution-review`

每个提案卡片展示：
- 信号类型图标 + 描述
- 来源（Agent 名 + 会话轮次，可点击跳转）
- 问题摘要
- 目标路径和操作类型（create/patch/deprecate）
- diff 预览（展开/折叠）
- 冲突标记（如有）
- 操作按钮：[采纳] [编辑] [拒绝]

编辑模式：打开文本编辑器，允许修改 proposed_content，修改后保存相当于"拒绝旧版 + 创建新版"。

拒绝：弹出理由输入框（可选），提交后状态变为 rejected。

采纳流程：
1. 后端调 `skill_manage(create/patch)` 或 `memory_remember` 写入文件
2. 追加 EVOLUTION.log
3. 更新提案状态为 approved

---

## 11. 审计日志

Agent 工作区的 `EVOLUTION.log`，每行一条记录：

```log
[2026-09-24 10:05] approved  proposal=uuid  type=workflow  target=skills/port-debug.md:create  turn=47  reviewer=jas.jin
[2026-09-24 11:20] rejected  proposal=uuid  type=style     reason="我自己控制语气就行"           turn=52  reviewer=jas.jin
[2026-09-24 14:30] approved  proposal=uuid  type=trial_error target=skills/es-mapping-fix.md:create  turn=63  reviewer=jas.jin
```

格式固定，人类和 AI 都能解析。与 `evolution_proposals` 表通过 proposal uuid 双向追溯。

---

## 12. 技能生命周期

### 12.1 状态机

```
         ┌──────────┐
         │  active  │◄──────────────人工恢复──────────┐
         └────┬─────┘                                  │
              │                                        │
   ┌──────────┼──────────┐                             │
   ▼          ▼          ▼                             │
30天未用   人工标记   晋升到共享                          │
   │          │          │                             │
   ▼          ▼          ▼                             │
┌────────┐ ┌──────┐ ┌─────────┐                        │
│deprecat│ │delete│ │ shared  │                        │
│  ed    │ │  d   │ └─────────┘                        │
└───┬────┘ └──────┘                                    │
    │ 7天后                                             │
    ▼                                                  │
.archive/skills/ ──────────────────────────────────────┘
```

### 12.2 自动弃用规则

- `created_by=evolution` 且 `modified_by≠human` 且 `last_used > 30天` → 自动标记 deprecated
- deprecated 7 天后 → 移动到 `.archive/skills/`
- `.archive/skills/` 中 90 天以上的条目 → 系统通知提醒清理（不自动删除）
- 人工创建或编辑过的技能永不自动弃用

**执行位置：** 生命周期清理（deprecated 过期归档 + 90 天提醒）通过 Portal 现有 `cron` 服务每周执行一次，复用 `internal/cron` 调度器。

### 12.3 使用追踪

SKILL.md frontmatter，由框架自动维护：

```yaml
---
name: port-debug
description: 排查端口连通性问题
created: 2026-09-24T10:05:00Z
created_by: evolution
last_used: 2026-09-25T14:30:00Z
use_count: 7
last_modified: 2026-09-24T10:05:00Z
modified_by: human
deprecated: false
source_agent: zone-4100-agent
---
```

`last_used` 和 `use_count` 在每次 `load_skill` 时自动更新。

### 12.4 分层晋升

```
agent skills/              project skills/           user skills/
(Agent 级)                  (项目级)                  (用户级)
     │                          │                        │
     │ 3个Agent独立创建          │                        │
     │ 同一技能                  │                        │
     ▼                          │                        │
  晋升提案 ──────────────────────►│                        │
     (需评审)                    │ 3个项目独立创建          │
                                 │ 同一技能                │
                                 ▼                        │
                              晋升提案 ────────────────────►│
                               (需评审)                     │
```

晋升检测：新技能写入时，embedding 相似度 > 0.85 的已有技能 ≥ 2 个（来自不同 Agent/项目）→ 自动生成晋升提案到 `evolution_proposals`。

---

## 13. 文件变更清单

| 操作 | 文件 | 说明 |
|------|------|------|
| CREATE | `portal/migrations/014_evolution_proposals.sql` | 建表 |
| CREATE | `portal/internal/server/evolution.go` | HTTP handlers |
| CREATE | `portal/internal/biz/evolution.go` | Usecase + Proposal 模型 |
| CREATE | `portal/internal/data/evolution_proposal.go` | Repo 实现 |
| CREATE | `portal/internal/chat/evolution_detect.go` | 信号检测 + 提议生成 |
| CREATE | `portal/internal/chat/evolution_trial_buffer.go` | 试错检测缓冲 |
| CREATE | `portal/internal/chat/evolution_arbiter.go` | 冲突检查 + 去重 |
| CREATE | `web/src/pages/EvolutionReviewPage.tsx` | 评审页面 |
| CREATE | `web/src/api/evolution.ts` | API 客户端 |
| MODIFY | `portal/internal/server/http.go` | 加路由 |
| MODIFY | `portal/internal/service/chat.go` | turn 结束后触发检测 |
| MODIFY | `portal/cmd/backend/wire_gen.go` | DI 接线 |
| MODIFY | `portal/cmd/backend/wire.go` | DI 声明 |
| MODIFY | `portal/configs/agent_extra.yaml` | 默认配置 |
| MODIFY | `web/src/App.tsx` | 加路由 + 侧边栏 |
| MODIFY | `web/src/api/client.ts` | 基础路径 |

---

## 14. 不包含的内容

- 不重建 `NotifyMemoryExtractFromTurn` 事实提取
- 不重建 ProceduralBinding（P3-C/D/E）
- 不重连 memory_units Prefetch
- 不做全自动写入（全部走评审页面）
- 不做 Agent 间实时技能同步（只在晋升时触发提案）
- 不引入 Qdrant/Neo4j 等外部依赖（embedding 走轻量 text-embedding-3-small）