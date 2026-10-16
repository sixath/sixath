# Obsidian 经验知识库（experience knowledge vault）设计

> 状态：spec（brainstorming 产出，待 review）
> 日期：2026-10-14

## 目标（Goal）

给 agent 加一层** handbook 之外的经验知识**：由人 + agent 沉淀「怎么排查 / 踩过的坑 / 工具用法 / 案例」，用 Obsidian 作为编辑界面、文件系统作为存储、`framework/memory` 的语义索引作为检索层。**第一优先级、可独立验收的闭环是**：agent 下次排查时能「检索命中」这些经验，不再每轮现场 rediscover（对比上一轮诊断出的 `list_tools → load_skill → http_request` 反复找工具的痛点）。

## 背景与动机（Why）

- **handbook 只覆盖「代码长什么样」**（自动从仓库生成 symbols/areas/stages），覆盖不到「这件事该怎么干、上次怎么干的」——后者正是 agent 每次「找半天」的缺口。
- 上轮诊断确认：agent 每轮现场 rediscover 工具用法、检索路径，因为正确用法没有被前置成稳定知识。
- 三个使用场景（agent 沉淀排查知识 / 个人第二大脑 / 组织共享知识库）共用**同一个 vault**，用目录 + frontmatter 分层，本 spec 只把「agent 沉淀 + 检索命中」做成最小闭环，另两个场景作为同 vault 的自然外溢，不阻塞主闭环。

## 架构（Architecture）

```
文件系统 vault（Obsidian 打开、编辑）
   ├── _agent_notes/   【隔离区】agent 写，status=pending，不进检索
   ├── ops/            【正式经验】人 review 后晋升，进检索
   ├── mine/           【个人第二大脑】仅本人
   └── shared/         【组织共享】多人协作
        │
        ▼ 扫描（复用 skills.watcher 的目录监听思路）
framework/memory 语义索引（vector + graph，片段级命中）
        │
        ▼ 检索 + 权限过滤
agent 排查上下文命中经验 → 不再现场 rediscover
```

- **写入**：复用现有 workspace 文件工具（`framework/tool/file_tools.go` → `RegisterWorkspaceFileTools`），不新造专用工具；用「模板 + system prompt 规范」约束 agent 只写 `_agent_notes/`、只写 pending。
- **检索**：vault 笔记 embed 成向量 + 用 `[[双向链接]]`/tag 建图，走 `framework/memory` 的混合检索（semantic + graph RRF），片段级命中 + 溯源，而非整篇糊进上下文。

## 数据模型（Data model）

每条笔记 = 一个 markdown 文件，用 **Obsidian frontmatter** 承载结构化元数据，正文为可检索的自然语言。

```markdown
---
type: experience          # experience | case | tool-usage | workflow | note
agent_id: <uuid>          # 沉淀它的 agent（隔离区笔记才有）
status: pending           # pending(隔离区) | promoted(正式)
tags: [es, 流水, 8110008]
source: session:<uuid>    # 溯源到哪次排查
created: 2026-10-14
---
# 存档卸载失败流水怎么查
...
```

字段约定：
- `status`：`pending` 在 `_agent_notes/`，`promoted` 在正式目录。**晋升 = 人移动文件 + 改 status**，agent 无权晋升。
- `type`：区分经验 / 案例 / 工具用法 / 流程 / 一般笔记，检索和过滤用。
- `source`：溯源到产生它的 session，检索命中时带上，便于判断时效。

## 目录与权限

| 目录 | 谁写 | 谁读（检索命中） | status |
|------|------|------------------|--------|
| `_agent_notes/` | agent | 不命中（pending 不进检索） | pending |
| `ops/` | 人（review 后） | 所有 agent | promoted |
| `mine/` | 本人 | 仅本人 | — |
| `shared/` | 组织成员 | 组织内 agent | — |

## 组件与数据流

1. **沉淀（写）**：agent 排查结束，被 system prompt 引导——若本次有「下次还用得上」的经验，用 workspace 文件工具写 `_agent_notes/xxx.md`，frontmatter 带 `status: pending` + `type` + `tags` + `source`。**先只做显式触发**（agent 自行判断沉淀），自动触发（turn 结束 LLM 判断）留待闭环跑通后。
2. **Review 与晋升**：人用 Obsidian 打开 vault，review `_agent_notes/` 里的 pending 笔记，认可则拖入 `ops/`（或对应正式目录）并把 `status` 改为 `promoted`。
3. **索引（读）**：vault 笔记作为经验来源，扫描进 memory 语义索引（vector + graph）。pending 笔记**不进索引**；promoted 笔记进索引。
4. **检索命中**：agent 排查时，排查上下文做 query，混合检索命中经验，返回「片段 + 出处（source/created/type）」，注入上下文。按 agent 身份做权限过滤（`mine/` 仅本人、`shared/` 组织内、`ops/` 所有人）。

## 错误处理与风险

- **防污染**：agent 只能写 `_agent_notes/`（模板 + system prompt 硬约束）；pending 不进检索，防止「agent 自己刚写、未 review 的笔记」被自己或他人当真理命中（自证循环）。
- **命中质量**：命中结果带 `source`/`created`/`type`，agent 可判断时效与相关性；返回片段而非整篇，避免上下文爆炸。
- **存储后端**：语义索引依赖 Qdrant（或 SQLite 降级）做向量、Neo4j（或降级）做图。先以 SQLite 起步跑通闭环，Qdrant/Neo4j sidecar 作为可选升级（docker-compose 已有 `compose.neo4j.yml`）。

## 测试与验收

- 复用 `evals` 的 `rca_handbook` A/B 骨架：同一批排查题，A/B 对比「有经验库」vs「无」，衡量 agent 是否更少「现场找半天」、更快定位根因。
- 主闭环验收标准：agent 在第二次遇到同类问题时，检索命中库里已晋升的经验，调用链里不再出现「多次 list_tools + load_skill 才动手查」的 rediscover 行为。

## 范围（YAGNI，先不做）

- 自动触发沉淀（turn 结束 LLM 判断）——闭环跑通后。
- 组织级 ACL / Git 协作冲突处理——先按「共享目录 + 约定」。
- Qdrant / Neo4j 高可用与生产化——先 SQLite。
- 新造专用「写知识库」工具——复用 workspace 文件工具。
