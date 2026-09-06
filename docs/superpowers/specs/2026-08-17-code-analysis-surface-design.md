# 源码分析工具面（Code Analysis Surface）设计

**日期**: 2026-08-17  
**状态**: 待确认  
**动机**: migu-agent 会话 c7aa 在「根据代码分析存档迁移整体流程」上失败——不是缺少跨仓 grep，而是把代码检索做成了 RCA 排障工具，再被 Turn Tool Surface 收窄掉。模型只能看见 workspace 的 `read_file` / `search_files`，读到工作区根目录的 txt 摘录，把触发源猜成 vm-manager。

**关联**:
- [Turn Tool Surface](./2026-08-09-turn-tool-surface-design.md)
- [RCA 工具链](./2026-07-07-rca-toolchain-design.md)（代码三件套的实现仍复用，**族与品牌**要拆开）
- 对照会话：`c7aa3b9b-fa8e-416e-862d-bfbac36e23dc`，agent `a3af7bc6-6888-4dde-b782-ef2bfcb04df1`

**非本设计**:
- 为某个业务写「存档迁移」专用 Skill 并强制路由
- 重命名 `rca_grep` → `code_grep`（二期别名，避免破坏已有绑定/证据格式）
- 新建图数据库 / 通用 call-graph 引擎
- 改 MEA 外环、Gateway、鉴权

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 根因 | 代码检索工具挂在 `FamilyRCA`，关键词是 jaeger/日志；「流程梳理」fail-narrow 到 `core`，跨仓工具根本不进 registry |
| 主路径 | **拆族**：`code`（源码导航）与 `rca`（trace/日志）分离；工具名一期不变 |
| 意图 | 「代码 / 源码 / 流程 / 调用链 / 模块」→ `code`；「trace / 日志 / jaeger / es」→ `rca`，且 **rca 自动并上 code**（排障闭环） |
| 策略层 | `code` 族激活时追加通用源码分析系统提示（工作集、入边、消歧、构图、证据、停机） |
| 派生语料 | 一期只改工具描述 + 系统提示；不在 `search_files` 里写死排除 `*.txt`（那是 workspace 草稿的合法用途） |
| 领域 Skill | 仍可选；平台不以 `rca-sync-archive-migrate` 为分析能力 |

---

## 1. 目标与非目标

### 目标（一期）

1. 用户说「根据代码分析 / 梳理流程 / 谁调用了 / 模块关系」时，本轮 registry **必须出现** `rca_grep` / `rca_glob` / `rca_read`（若 Agent 已绑定 `rca_code` / `rca_symbol`）。
2. 用户只问 GitLab / 只闲聊时，行为与现网一致：不无故摊开代码检索或 jaeger。
3. 用户问线上 trace/日志时，仍能看到 `jaeger_trace` / `es_log_query`，并**同时**看到代码三件套（trace → 代码）。
4. `code` 族激活时，系统提示给出与业务无关的分析协议，降低「读 workspace txt 当源码」「在第一模块停住」的概率。
5. 工具描述去掉「仅用于 RCA」语义，写明：优先于 workspace `search_files`，根是配置的 code roots。

### 非目标（一期不做）

- 破坏性重命名工具
- 新工具 `code_refs` / 多跳图执行器（协议用提示覆盖；二期再工具化）
- 自动加载某个业务 SKILL.md
- 把 workspace 草稿目录从 Agent 里删掉
- 语言无关 LSP 平台（已有 `rca_symbol` 保持原族映射到 `code`）

### 成功标准（可用会话题回归）

对绑定了 `rca_code`（roots=`D:\workspace\migu`）的 agent，用户发送：

> 根据代码分析 存档迁移整体流程梳理

期望：

| 检查 | 通过条件 |
|------|----------|
| 工具面 | `ActiveFamilies` 含 `code`（及 `core`）；`rca_grep` 在 registry |
| 首轮工具 | 允许 `rca_grep` / `rca_glob`；**不**把 `load_skill(migu_rca)` 当代码检索 |
| 证据 | 结论能引用 `*.go` 路径，而不是 workspace 根 `dispatch_*.txt` |
| 对照负例 | 「帮我查 GitLab 项目」仍不激活 `code`/`rca` |

一期可用单测锁工具面；端到端「不读 txt」用提示 + 描述约束，不强制 LLM 单测。

---

## 2. 现状（为何会收窄掉）

```
用户: 根据代码分析存档迁移整体流程
  → IntentResolver 规则关键词 FamilyRCA = jaeger/trace/日志排查/链路
  → 「流程」≠「链路」，无高分
  → fail_narrow → Active = {core}
  → BuildRegistry 过滤掉全部 ToolTypeRCA
  → 模型只剩 read_file / search_files / terminal / load_skill
  → 检索 E:\sixath\workspace\migu 根目录 txt
```

已存在且应复用：

| 能力 | 位置 | 说明 |
|------|------|------|
| 跨仓 grep/glob/read | `framework/tool/rca_code_tools.go` | roots 白名单已是 `D:\workspace\migu` |
| 符号/引用 | `framework/tool/rca_symbol_tool.go` | 映射到 `code` 族即可 |
| 每轮收窄 | `IntentResolver` + `filterToolsForSurface` | 缺 `code` 族 |
| workspace 单根文件工具 | `read_file` / `search_files` | 继续服务草稿/skill，不当源码根 |

---

## 3. 族模型

| Family ID | 成员 | 规则关键词（示例） |
|-----------|------|-------------------|
| `core` | memory / todo / skills / session / **workspace 文件工具** / terminal | （常开） |
| `code` | `rca_grep` `rca_glob` `rca_read` `rca_symbol` | 源码、代码、仓库、调用链、模块关系、流程梳理、谁调用、grep、go.mod、入口 |
| `rca` | `jaeger_trace` `es_log_query` | jaeger、trace、span、otel、elasticsearch、日志排查 |
| `web` / `knowledge` / `mcp:*` | 不变 | 不变 |

**绑定 → BoundFamilies**：

- `rca.func_path ∈ {rca_code, rca_symbol}` → 计入 `code`
- `rca.func_path ∈ {jaeger_trace, es_log_query}` → 计入 `rca`
- 同一 Agent 可同时绑两者（migu-agent 即是）

**过滤 → filterToolsForSurface**：

- `rca_code` / `rca_symbol` 工具：`FamilyCode` 激活才注册
- `jaeger_trace` / `es_log_query`：`FamilyRCA` 激活才注册
- **Resolve 后处理**：若 `rca` ∈ Active 且 `code` ∈ Bound，则 Active 并上 `code`（单向）
- `code` 激活 **不**自动并上 `rca`

兼容：`SATH_TURN_TOOL_SURFACE=0` 时 `active==nil`，全量绑定，行为与现网一致。

---

## 4. 系统提示（仅 code 族激活时追加）

与 `AppendTurnIntentPrompt` 并列，新增 `AppendCodeAnalysisPrompt`，在 `PrepareTurnToolSurface` 之后、`BuildReActAgent` 之前，当 `FamilyActive(active, FamilyCode)` 时追加。要点（短、可测字符串）：

1. **工作集**：源码以配置的 code roots（`rca_grep` 的 roots）为准；workspace 下 `*.txt` / MEMORY / 其它模型摘要不是源码证据。
2. **优先工具**：跨仓检索用 `rca_grep` / `rca_glob` / `rca_read` / `rca_symbol`；不要用 `load_skill` 代替；不要把绑定工具的展示名当成 Skill 名。
3. **入边**：找到 HTTP path / 函数 / topic / 错误码后，必须再 grep 调用方，禁止把入口 handler 当成唯一源头。
4. **消歧**：同一中文名可能对应多套实现，先枚举候选再下钻。
5. **构图再深读**：先列仓/入口（main、路由、消费者），沿边走，不要在第一份命中上宣称完整。
6. **证据与停机**：事实带 `path:line`；推断标明；入边扫完或明确某仓不在 roots 才许下结论。

禁止在提示里写咪咕/存档迁移/union-archiver 等业务名。

---

## 5. 工具描述（framework）

`rca_grep` / `rca_glob` / `rca_read` 的 Description：

- 删除或弱化 “RCA repository roots / inside RCA”
- 改为 “configured code roots (multi-repo)”
- 明确：源码/调用链/模块分析优先用本组；workspace `search_files` 只用于 Agent 工作区草稿
- `search_files` / `read_file` 的 hint 反向补一句：若问题是源码分析且已有 `rca_*`，不要把工作区摘录当唯一依据

工具 **Name 不变**，证据 `kind: rca_grep` 不变。

---

## 6. 错误处理与测试

| 场景 | 行为 |
|------|------|
| 只绑了 jaeger，没绑 rca_code | Bound 无 `code`；并族逻辑不发明 `code` |
| 只绑了 rca_code | 「查 jaeger」规则不命中 `code`；除非用户同时说代码/流程 |
| 分类器超时 | 若规则已命中 `code` 关键词则走 unique_rule_hit，不依赖分类器 |
| 旧单测 GitLab-only 不激活 RCA | 继续成立，且不激活 `code` |

必加单测：

1. `TestIntentResolver_CodeAnalysisActivatesCodeFamily`：用户「根据代码分析存档迁移整体流程」+ bound 含 code/rca/gitlab → Active 含 `code`，不含 `rca`、不含 gitlab。
2. `TestIntentResolver_RCAIncludesCodeWhenBound`：用户「看下 Jaeger trace」→ Active 含 `rca` 且含 `code`。
3. `TestFilterToolsForSurface_CodeVsRCA`：code 激活只放行 rca_code 工具；rca 激活放行 es/jaeger。
4. `TestAppendCodeAnalysisPrompt_Generic`：提示含「code roots」「rca_grep」，不含「存档迁移」。
5. framework：`rca_grep` Description 含 `code roots`，不再把 RCA 当唯一用途（字符串断言从宽）。

---

## 7. 分期

| 期 | 内容 |
|----|------|
| **一期（本文）** | 拆族、意图、并族、系统提示、描述去品牌、单测 |
| 二期 | `code_grep` 别名；`code_refs`（字面量/符号入边）；grep 默认跳过 vendor 已有、可加重生成物 |
| 三期 | 多跳工作流工具化、完备性清单结构化输出 |

领域 Skill（如 `rca-sync-archive-migrate`）留在 Agent workspace，**不**进入本期平台代码。

---

## 8. 风险

- 关键词「流程」「链路」可能误伤：`链路` 现属 RCA。保留在 RCA；代码族用「调用链」「流程梳理」「代码流程」。若误激活 code，代价是多暴露 grep，比漏掉轻。
- 中文「流程」单独出现（「审批流程」）可能打开 code 族。可接受；Fail-narrow 以前更差。
- 提示变长：仅 code 族激活时追加，GitLab 轮次不加。
