# 仓库管理 + 分组绑定 + RCA Handbook 设计

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 把"代码仓库"从 agent 工作区里的一个软链接提升为 portal 的一等实体；agent 通过**仓库组**批量绑定仓库；每个仓库（以及每个组）维护一份共享的、以行为为中心的 **Handbook**，让 RCA Agent 先按 handbook 路由、再读真实源码核实，提升根因定位的召回与精度。

**Architecture:** 仓库注册表（扫描 `code_roots` 发现 git 根）→ 分组（目录组/标签组/手工组）→ agent 绑定组或仓库（独立表 `agent_repo_bindings`，不与 Loadout 共用）→ 展开为 agent 有效仓库集 → 运行时据此计算 RCA roots 与可见 handbook。Handbook 按"仓库 + commit"生成、多 agent 共享，cron 增量刷新，运行时懒校验，人工/RCA 经验以 overlay 形式独立沉淀。

**Tech Stack:** Go (portal backend + framework skills) + React/TypeScript (web) + MySQL + 现有 `cron.Scheduler`、`evolution_proposals` 流水线、Skills 渐进披露（`skills_list` / `skill_view` / `read_skill_file`）。

**参考论文:** Harness Handbook: Making Evolving Agent Harnesses Readable, Navigable, and Editable（[arXiv 2607.13285](https://arxiv.org/abs/2607.13285)）。本设计借鉴其 L1–L3 行为文档树、状态寄存器视图、BGPD（行为引导的渐进披露）、定位符校验与冻结、diff 驱动增量同步；不照搬其 function-as-leaf 全量 actor–critic 生成。

---

## 1. 当前状态

### 1.1 代码挂载

- 业务代码由宿主机只读挂载：`docker-compose.yml` `${HOST_CODE_ROOT:-./codes}:/mnt/codes:ro`；portal 配置 `data.code_roots: [/mnt/codes]`。
- `codes/` 被 gitignore，仓库内**没有任何代码同步机制**，更新取决于宿主机上的 `git pull`。
- Agent 绑定代码 = `POST /api/v1/agents/{id}/workspace-link` → `fwws.LinkCode` 创建 `workspace/code → 目标目录` 软链接（`framework/workspace/code_link.go`）。

### 1.2 存在的问题

| # | 问题 | 影响 |
|---|------|------|
| 1 | 绑定关系只存在于文件系统，DB 无记录 | 无法回答"哪些 agent 用了某仓库"；handbook 无法确定归属与可见性 |
| 2 | 每个 agent 只有一个 `workspace/code` | 要看多个仓库只能链父目录，粒度失控 |
| 3 | 目标可以是任意深度目录 | 没有"仓库"边界，无法按 commit/哈希管理 |
| 4 | 20–30 个仓库的 agent 只能挂父目录 | 无法排除个别仓库，无法细粒度授权 |
| 5 | RCA 只靠 `rca_grep` / `rca_glob` / `rca_read` 探索 | 正是论文的 Baseline：冷路径、跨模块、跨服务问题召回差 |

### 1.3 可复用的基础设施

- `MergeRCARoots(workspace, configured) []string`（`portal/internal/chat/code_roots.go:166`）：**已支持多根目录**，只是被单个软链接限制。调用点：`portal/internal/chat/rca_builder.go:41,48`。
- `sharedSkillDirs(ctx, agentID)` → `chat.BuildSkillsIndex(workspace, extraSkillDirs)`（`portal/internal/service/chat.go:438-442`）：agent 额外 Skill 目录的注入点。
- `cron.Scheduler`（`portal/internal/cron/scheduler.go`）：已有周期循环（evolution 清理），可加仓库巡检循环。
- `evolution_proposals` 流水线（`portal/internal/chat/evolution_pipeline.go`、`portal/internal/biz/evolution.go`）：提案 → 去重 → 人工评审，可承载 overlay 提案。
- Turn trace 存储（`NewTurnTraceStoreFromData`）：可离线统计 handbook 漏召回，无需改 framework。
- `framework/tool/call_graph.go`：基于 `go/ast` 的调用图（同文件/同目录），可作为 Go 仓库 Phase I 的起点。

### 1.4 设计原则

- **仓库是事实来源，handbook 只是索引缓存**：Agent 必须读源码核实；handbook 过期只降低召回，不产生错误结论。
- **handbook 归属仓库，不归属 agent**：同一仓库全局一份，多 agent 共享。
- **组是一等可绑定对象**：成员动态变化，绑定组的 agent 自动跟随。
- **确定性事实与 LLM 描述分离**：路径、行号、符号、调用边、哈希由静态分析产生，LLM 不可改写；归不了类的显式记录。
- **handbook 不是绑定的前提**：无 handbook 时 RCA 照常 grep/read。
- **不引入新的外部依赖**：存储走 MySQL + `data_root` 文件。

---

## 2. 目标与非目标

**目标**

1. 仓库注册表：自动发现 `code_roots` 下的 git 根，记录 commit 与同步状态。
2. 分组：目录组自动生成；支持标签组、手工组。
3. 绑定：agent 绑定仓库 / 组（后期支持规则），支持排除；展开为有效仓库集。
4. 运行时：RCA roots 与 handbook 可见性由有效仓库集决定，兼容旧软链接。
5. 仓库级 handbook（file-as-leaf）+ 组级 handbook（跨服务 L0）。
6. 维护：三层更新（校验 / 增量 / 重建）、冻结、overlay、漏召回反馈。
7. 评测：RCA 定位 A/B（有/无 handbook）。

**非目标（本期）**

- portal 负责 clone / pull 代码（`sync_mode=managed`，留字段，后期做）。
- 按线上部署版本切换 handbook 快照。
- function-as-leaf 的逐函数 actor–critic 生成。
- framework 自身的 handbook（另立设计）。

---

## 3. 概念模型

```
code_roots (/mnt/codes, 只读)
   │  扫描 git 根
   ▼
Repository ──< RepoGroupMember >── RepoGroup        (目录组 / 标签组 / 手工组)
   │                                   │
   │ 1:1                               │ 1:1
   ▼                                   ▼
Repo Handbook (L1–L3 + registers)    Group Handbook (L0：服务清单 + 跨服务关系)

Agent ──< agent_repo_bindings >── {repo | repo_group | repo_selector}
   │        (含排除项)
   ▼  展开
agent_effective_repos  ──►  RCA roots  +  可见 handbook  +  code-map 入口 Skill
```

---

## 4. 数据模型

### 4.1 新增表（migration `019_repo_registry.sql`）

**`repositories`**

| 列 | 类型 | 说明 |
|----|------|------|
| id | VARCHAR(36) PK | UUID |
| code_root | VARCHAR(255) | 所属 code root 绝对路径（如 `/mnt/codes`）；255 是为了让 `UNIQUE(code_root, rel_path)` 在 utf8mb4 下不超过 InnoDB 3072 字节上限 |
| rel_path | VARCHAR(512) | 相对 code root 的路径，必须是 git 根；`UNIQUE(code_root, rel_path)` |
| name | VARCHAR(256) | 展示名，默认 = rel_path 最后一段 |
| description | TEXT | 可选；为空时取 handbook L1 摘要首句 |
| tags | JSON | 标签数组 |
| git_remote | VARCHAR(512) | 读取 `.git/config`，仅展示 |
| git_branch | VARCHAR(256) | 当前分支 |
| head_commit | VARCHAR(64) | 最近扫描到的 HEAD |
| sync_mode | VARCHAR(16) | `registry_only`（本期）/ `managed`（预留） |
| status | VARCHAR(16) | `active` / `missing`（目录消失）/ `archived` |
| handbook_status | VARCHAR(16) | `none` / `building` / `ready` / `stale` / `failed` |
| handbook_commit | VARCHAR(64) | handbook 对应 commit |
| handbook_version | INT | 原子切换用的版本号 |
| handbook_stats | JSON | 冻结比例、未归类文件比例、文件数、stage 数、register 数；P2a 实际为 `generator_version`、`built_at`、文件/Go 文件/包/分区/符号/寄存器数、`truncated`、`duration_ms`，失败时含 `last_error`、`failed_commit`；P2b 增加 `cards`（有当前卡片的文件数）、`stale_cards`、`stages`、`llm_rev`（本次渲染所用的 LLM 内容修订号） |
| handbook_lease_until / handbook_lease_token | DATETIME(3) / VARCHAR(36) | P2a 构建租约（migration `020_repo_handbook.sql`，§8.2） |
| handbook_model | VARCHAR(255) NOT NULL DEFAULT '' | P2b：仓库级 handbook 模型覆盖。空 = 继承全局 `handbook.model`；`off`（不区分大小写，存为小写）= 该仓库禁用 LLM 层；其余为模型名或 `<provider 名称或 ID>/<模型名>`（migration `021_repo_handbook_llm.sql`） |
| handbook_llm | JSON | P2b：LLM 层状态，字段见 §7.2"P2b 实现"的 `handbook_llm` 表 |
| handbook_llm_lease_until / handbook_llm_lease_token | DATETIME(3) / VARCHAR(36) | P2b：LLM 运行租约，独立于构建租约（§8.2） |
| owner_id | VARCHAR(36) | 负责人 |
| last_scanned_at / created_at / updated_at | DATETIME(3) | |

**`repo_groups`**

| 列 | 类型 | 说明 |
|----|------|------|
| id | VARCHAR(36) PK | UUID |
| name | VARCHAR(256) | 展示名 |
| kind | VARCHAR(16) | `dir` / `tag` / `manual` |
| rule | JSON | `dir`: `{"code_root","rel_prefix"}`；`tag`: `{"all_of":[],"any_of":[]}`；`manual`: null |
| auto_apply_new | BOOLEAN | 新成员是否自动对已绑定 agent 生效（默认 dir/tag=true，manual 无意义） |
| handbook_status / handbook_version | | 组级 handbook 状态 |
| owner_id / created_at / updated_at | | |

**`repo_group_members`**

| 列 | 类型 | 说明 |
|----|------|------|
| group_id | VARCHAR(36) | PK 之一 |
| repo_id | VARCHAR(36) | PK 之一 |
| source | VARCHAR(16) | `manual`（手工组成员）/ `rule`（dir/tag 展开结果） |
| state | VARCHAR(16) | `active` / `pending_confirm`（`auto_apply_new=false` 时的新成员） |
| created_at | DATETIME(3) | |

**`agent_effective_repos`**（系统维护，禁止手工改）

| 列 | 类型 | 说明 |
|----|------|------|
| agent_id | VARCHAR(36) | PK 之一 |
| repo_id | VARCHAR(36) | PK 之一 |
| via | JSON | 来源绑定列表，如 `[{"kind":"repo_group","id":"..."}]` |
| sub_paths | JSON | 可选：只关注的子路径（迁移旧子目录软链接时产生） |
| computed_at | DATETIME(3) | |

**`agent_repo_bindings`**

| 列 | 类型 | 说明 |
|----|------|------|
| agent_id | VARCHAR(36) | PK 之一 |
| target_kind | VARCHAR(16) | `repo` / `repo_group` / `repo_selector`（P4）；PK 之一 |
| target_id | VARCHAR(256) | repository.id / repo_groups.id / 规则 hash；PK 之一 |
| mode | VARCHAR(16) | `include` / `exclude`（`exclude` 仅允许 `target_kind=repo`） |
| sub_paths | JSON | 可选，仅 `repo` + `include` |
| rule | JSON | 仅 `repo_selector` |
| priority | INT | code-map 展示顺序 |
| created_by / created_at / updated_at | | |

### 4.2 为什么不复用 `agent_asset_bindings`

`agent_asset_bindings`（migration 012）是 Loadout 的绑定表，Loadout 仍在推进（`docs/superpowers/specs/2026-08-07-memory-hub-governance-knowledge-plugins-design.md`）。与仓库绑定共用会产生三处冲突：

1. **来源不同**：Loadout 来自"该 agent 解析到的治理面"（`Resolve(agent).Governance`，§9.1）。agent 覆盖到外部 Hub（如 tencent）时 Loadout 由外部 Hub 的 `ResolveLoadout` 给出，**不读本地表**——仓库绑定若放在这里，会随治理面切换而整体消失。仓库是本地挂载的资源，绑定必须恒走本地。
2. **状态机不同**：Loadout 的 `status` 是资产生命周期（`draft` / `active` / `stale` / `superseded` / `archived`），仓库绑定需要的是 `include` / `exclude` 语义，混用会让任一方的过滤逻辑误判。
3. **语义不同**：Loadout 绑定的资产会直接进入运行时（Skill 来源优先级高于 `skills_dirs`）；仓库绑定只决定可见范围，其 handbook 经 code-map 渐进披露。`ResolveLoadout` 若读到 `repo*` 行会被当成资产处理。

因此使用独立表 `agent_repo_bindings`，两套绑定在 UI 上可以放在同一个"配装"页的不同分区。

**与 Loadout 的唯一交点——Skill 名冲突**：Loadout 规则是"已绑定名称 N → 忽略 `skills_dirs` 中同名 Skill"。handbook 与 code-map 通过 `sharedSkillDirs` 注入，属于 `skills_dirs` 一侧，可能被 Loadout 同名资产覆盖。约定：

- 保留名前缀 `code-map`、`handbook-`，Loadout 绑定与 Skill 创建（`skill_manage`、evolution 提案）拒绝使用该前缀；
- 在 `skills/index.go` 合并阶段若仍检测到冲突，记 error 并以 handbook 为准（保留名优先于 Loadout 同名规则，作为唯一例外，写入 Loadout 设计 §9.1 的例外清单）。

### 4.3 展开规则

```
effective(agent) =
    ⋃ { target_id       | binding(kind=repo, mode=include) }
  ∪ ⋃ { members(group)  | binding(kind=repo_group), member.state=active }
  ∪ ⋃ { match(rule)     | binding(kind=repo_selector) }
  − { target_id | binding(kind=repo, mode=exclude) }
  − { repo | repo.status != active }
```

重算触发（均为同一事务后异步执行，按 agent 粒度幂等重算）：

- agent 绑定增删改；
- 组成员变化（扫描发现新仓库、标签变更、手工增删、确认 pending 成员）；
- 仓库状态变化（`missing` / `archived`）。

---

## 5. 仓库发现与同步

### 5.1 扫描（`RepoScanner`）

- 遍历每个 code root，深度上限沿用 `MaxCodeBrowseDepth`；遇到含 `.git` 的目录即记为仓库并**不再下钻**（不支持嵌套仓库，submodule 归属父仓库）。
- 读取 HEAD（直接读 `.git/HEAD` + refs，避免依赖 git 可执行文件；packed-refs 兜底）。
- 新仓库 → 插入 `repositories`；消失 → `status=missing`（不删，保留绑定与 handbook，便于恢复）。
- 目录组自动维护：对每个仓库的父目录链生成/更新 `kind=dir` 的组（仅生成**直接父目录**一级，如 `cloudgame/`；更深层级不自动建组，避免组爆炸）。

### 5.2 调度

在 `cron.Scheduler` 增加 `repoScanLoop`：

- 间隔 10 分钟。P1 为常量 `cron.DefaultRepoScanInterval`，启动时先扫一次；配置项 `repo_registry.scan_interval` 后续再加。
- 每轮：扫描 → 对 `head_commit` 变化的仓库投递 handbook 增量刷新任务（§7.2）→ 更新组成员 → 触发有效仓库集重算。
- 提供 `POST /api/v1/repos/scan` 手动触发；宿主机可在 `post-merge` hook 里调用（可选）。

### 5.3 `sync_mode=managed`（预留，不在本期）

portal 负责 clone/pull 到可写卷，需改 compose 挂载、管理 git 凭据（参考 `secrets/`）、磁盘配额与失败重试。字段先留，行为为 no-op。

---

## 6. 运行时接入

### 6.1 RCA roots

**绑定是权威来源**，以"有没有绑定"而不是"有效仓库集是否为空"作为分界：

1. agent **没有任何绑定** → `RepoRegistryUsecase.RCARootsForAgent` 返回 `nil`，沿用旧逻辑：`workspace/code` 软链接存在则用它，否则用工具配置里的 `roots`（`MergeRCARoots` 不变）；
2. agent **有绑定** → 返回非 nil 切片，**即使为空也是权威结果**：每项 `Name = rel_path`（含 `sub_paths` 时为 `rel_path/sub`），`Path` 为绝对路径；为空时不注册 `rca_code` / `rca_symbol` 工具，也**不回退**到 `workspace/code`；
3. 查询出错 → 记 warn，按"没有绑定"处理（fail-open 到旧逻辑）。

传递机制：service 层通过 `RCARootResolver`（`service/rca_roots.go`）查询，结果放入 `chat.RegistryBuildOptions.RCARoots []tool.RCARoot`；`rca_builder.go` 在 `RCARoots != nil` 时走 `RegisterRCACodeToolsNamed` / `RegisterRCASymbolToolNamed`。`chat` 包不直接访问 DB。

返回前在使用时再校验：仓库所在 code root 已不在配置中、路径已不存在、或解析软链接后逃出 code root 的条目会被丢弃并记 warn。

### 6.1.1 仓库逻辑名（P1 必改）

`rca_grep` / `rca_glob` / `rca_read` 已支持多 root，输出带 `repo` 字段，`rca_read` 要求传 `repo`。但仓库名取自 root 的 basename（`framework/tool/rca_repos.go` `repoNameFromRoot` = `filepath.Base`），`selectRoots` 按名字匹配时**遇到重名静默返回第一个**：

- 现状下 agent 多挂父目录（root = `/mnt/codes/cloudgame`，repo 名 = `cloudgame`，文件路径 = `svc-a/internal/x.go`），问题未暴露；
- 改为按仓库挂载后，`cloudgame/gateway` 与 `migu/gateway` 这类重名会让 `rca_read` 读错仓库，且不报错。

改动：

- `RegisterRCACodeTools(reg, roots []string)` 改为接收 `[]RCARoot{Name, Path}`；portal 侧 `Name = repositories.rel_path`（如 `cloudgame/svc-a`），全局唯一。
- 保留 `[]string` 入口做兼容：名字仍取 basename，但**构造时检测重名，重名则改用相对公共父目录的路径并记 warn**，不再静默选第一个。
- `repoCheck` 的候选列表、错误提示随之使用逻辑名。
- handbook 定位符、code-map、turn trace 证据统计统一使用该逻辑名，三者才能对齐（§9.4 依赖此点）。
- 测试：两个 basename 相同的 root，`rca_read(repo=...)` 必须读到正确文件；旧单 root 配置行为不变。

### 6.2 Handbook 注入

`sharedSkillDirs(ctx, agentID)` 末尾追加：

- 生成（或复用缓存）该 agent 的 **`code-map` 入口 Skill** 目录；
- 每个有效仓库的 handbook skill 目录（只读，指向 `data_root/handbooks/repos/<repo_id>/current/skill`）；
- 每个相关组的组级 handbook skill 目录。

为避免挤占系统提示中的 Skill 摘要配额（`BuildSkillsSummary` 按顺序截取前 8 条，超出即被丢弃），**仓库/组 handbook skill 的 frontmatter 标记 `hidden_from_summary: true`**（需在 `skills/index.go` / `prompt.go` 增加该字段支持），只有 `code-map` 进摘要。Agent 通过 `skill_view("code-map")` → 组 → 仓库逐级下钻。

同一标记还需让 handbook **不参与自动路由**（`skills/route.go`、`skills/embed_route.go`）：自动命中会把 SKILL.md 正文注入系统提示（`harness/prompt_prepare.go`），30 个仓库的 handbook 被关键词误命中会直接占用上下文，违背渐进披露。

### 6.3 兼容旧软链接

- 旧接口 `POST /agents/{id}/workspace-link` 保留，**只创建软链接，不再自动写绑定**（P1 实施时改定：隐式绑定会让 agent 悄悄切到"绑定权威"模式，且目录组只含直接子仓库，可能比旧链接少看到仓库）。
- 若 agent 已有绑定，响应额外带 `"repo_bindings_override": true` 和 `"warning": "agent has repo bindings; RCA tools use bound repositories, not this link"`，提示该链接对 RCA 工具不生效；查询绑定出错时忽略，不影响链接结果。
- 旧链接到绑定的转换统一走迁移接口（§14）。
- 兼容期结束后，`workspace/code` 由有效仓库集反向生成（单仓库时链接仓库；多仓库时不再创建），旧接口返回 deprecation 提示。

---

## 7. Handbook

### 7.1 存储布局

```
/data/portal/handbooks/
  repos/<repo_id>/
    current.json                      # 原子切换的指针 {"version":N}（不依赖 symlink）
    v<N>/
      manifest.json                   # commit、generator_version、leaf_mode=file、生成时间、stats
      facts/
        files.json                    # P2a：path、语言、大小、sha256
        symbols.json                  # P2a：Go 符号（含 body 指纹）
        registers.json                # P2a：寄存器候选（kind、name、access、path:line）
        packages.json                 # P2a：Go 包目录、包名、包注释、文件数、是否 main
        module.json                   # P2b：go.mod 信息（有 go.mod 时），供 LLM 层读取 facts
        graph.json                    # 调用边（P2a/P2b 均未做，coverage.graph = "none"）
      coverage.json                   # 未归类文件、冻结条目、未解析调用；P2a 记录跳过统计与截断
      skill/                          # 对 Agent 暴露（渲染产物）
        SKILL.md
        references/overview.md
        references/index.md           # 超过 48KB 时分页：index.p2.md、index.p3.md …
        references/registers.md       # 同上分页
        references/areas/<id>.md      # P2a：目录分区（同上分页）
        references/stages/<id>.md     # P2b（已实现）：LLM 行为阶段
    llm/                              # P2b：LLM 缓存，不在版本目录里、不随版本切换
      cards/<hash 前 2 位>/<hash>-<prompt 版本>.json   # 文件卡片，按文件内容 sha256 寻址
      skeleton.json                   # 阶段骨架、文件归属、总览、寄存器用途
      failed-<prompt 版本>.json       # 生成失败的内容哈希与传输错误轮数（无失败时删除）
    overlay/                          # 不随版本切换，永不被生成覆盖
      notes.jsonl
  groups/<group_id>/
    current.json, v<N>/ ...           # 组级 handbook，结构同上（无 cards）
  agents/<agent_id>/code-map/SKILL.md # 入口 Skill，有效仓库集变化时重渲染
```

P2b 实现：原设想的版本内 `generated/` 改为仓库级 `llm/` 缓存。LLM 层只写 `llm/`，确定性构建（§7.2 P2a）在渲染时读取它，把卡片、阶段、总览、寄存器用途渲染进新版本的 `skill/`；`llm/` 本身不进版本目录，也不随 `current.json` 切换。卡片按内容哈希寻址，换模型不作废卡片；`LLMPromptVersion`（当前 `p2b-1`）变化时旧卡片、旧骨架被忽略并在清理时删除。

### 7.2 仓库级 handbook 构建（file-as-leaf）

**Phase I：确定性事实（无 LLM）**

- 文件清单 + content hash；忽略规则：`.gitignore`、vendor、生成代码、测试数据、二进制、超大文件（阈值可配）。
- 语言适配器产出符号与调用边（业务仓库约 95% 为 Go）：
  - Go：扩展 `framework/tool/call_graph.go` 至包级（或 `golang.org/x/tools/go/packages`）；函数节点记录 body 指纹，供 §8.1 增量刷新与 §9.2 修复提交信号使用。
  - 非 Go（约 5%）：只做文件级（清单、哈希、寄存器候选的字面量匹配），不产出符号与调用边，`coverage.json` 标注 `graph: none`。**不引入 tree-sitter 等多语言解析**，避免 cgo/外部依赖。
- 未解析调用写入 `coverage.json`，不猜测目标。
- 抽取"寄存器候选"：DB 表名（SQL 字面量 / ORM tag）、缓存 key 前缀、MQ topic、HTTP/RPC 路由与客户端调用、配置键。均为字面量匹配，带 `file:line`。

**P2a 实现（确定性，无 LLM）**：

- 不解析 `.gitignore`；固定跳过隐藏目录（`.` 开头）与 `vendor`、`node_modules`、`third_party`、`testdata`、`dist`、`build`、`target`、`out`；跳过锁文件（`go.sum`、`package-lock.json` 等）、生成代码（`Code generated … DO NOT EDIT`、`*.pb.go` 等）、二进制、> 512KB 的文件；单仓库最多 20000 个文件，超出在 `coverage.truncated` 标注。
- Go 用 `go/parser` 产出包、包注释、导出与非导出符号（类型、函数、方法）及函数 body 指纹（sha256 前 12 位），读取 `go.mod` 直接依赖；**不做调用图**（`coverage.graph = "none"`），解析失败的文件记入 `coverage.go_parse_errors`。
- 寄存器候选四类，均为正则：
  - 表：`INSERT INTO` / `UPDATE … SET` / `DELETE FROM`（写）与 `FROM` / `JOIN`（读）、GORM `.Table("x")`；`.sql` 文件大小写不敏感，**源码只匹配大写 SQL 关键字**（英文字符串中的 "from" 太常见）；`TableName()` 返回的字符串字面量记为表引用；
  - 路由：`.GET/.POST/…/.Handle/.HandleFunc("/…")` 与 proto `get: "/…"` 等注解；
  - topic：名字含 `topic` 的变量/字段/YAML 键的字符串赋值；
  - 缓存键：含 `:` 的字符串字面量，且同一行出现 redis/cache/rdb/key 上下文。
- 名字超过 200 字节的寄存器候选丢弃。

**Phase II：行为组织（LLM）**

- 为每个文件生成 file card（论文 D.1.3 deep 模式 prompt 精简版）：purpose、description、关键函数说明、role、lifecycle。函数清单与行号作为**固定事实**输入，输出中不得出现清单外的符号（校验后丢弃违规条目）。
- 由 cards + 目录结构 + 入口点推断 stage 骨架，文件归入一个主 stage（可选 1–2 个次 stage）。默认 `oneshot`，不做多轮 doctor。
- 无法归类 → `coverage.json`，渲染时列在 index 末尾"未归类文件"。

**Phase III：合成与校验**

- stage 摘要（L2）、系统总览（L1）自底向上单次生成。
- 寄存器视图：以 Phase I 候选为骨架，LLM 只写用途说明，**读写位置列表完全来自静态匹配**。
- 校验所有 L3 定位符（文件存在 + 哈希一致），失败即 `frozen`。
- 渲染 `skill/`，写 `manifest.json`，原子切换 `current.json`。

**P2b 实现（LLM 层，计划 `docs/superpowers/plans/2026-10-09-repo-handbook-p2b.md`）**：

两层：确定性层（P2a 的 `Build`）每次 HEAD 变化立即发布；LLM 层是独立的后台运行 `handbook.Enrich`（biz 侧 `EnrichPending` / `RequestEnrich`），持有独立租约，读已发布版本的 facts 与仓库检出，只写 `llm/` 缓存；运行改变了缓存时生成新的 `handbook_llm.rev`，触发一次确定性重建把新内容渲染发布。模型通过 `framework/model.Model` 注入，所有调用 `temperature = 0.2`（OpenAI 兼容实现会把 0 替换成默认值）；回复取第一个能解码的 JSON 对象，`finish_reason = length`、无 JSON、解码失败都算"回复不可用"（区别于传输错误）。

- **卡片资格**：非测试、非空、语言属于源码类（go、proto、sql、python、java、javascript、typescript、shell、lua、c、cpp、rust、php、ruby、kotlin、csharp、scala、vue）。文档、配置、测试只出现在清单与分区页。
- **卡片生成**：提示词含仓库、路径、语言、行数、符号清单（最多 80 个，`functions.name` 只能从中原样选取）与文件内容（按 `max_file_kb` 截断在行边界并标注；代码围栏比内容中最长的反引号串更长）。输出 `purpose / description / role / lifecycle / functions(≤8)`，`max_tokens = 900`。校验：清单外或重复的函数丢弃，`role` 不在枚举内改为 `other`，各字段按 rune 截断（purpose 120、description 600、lifecycle 120、函数说明 160），`purpose` 为空视为回复不可用。文件经 `os.OpenInRoot` 读取（仅普通文件、≤ 512KB），内容哈希与 facts 不一致的文件本轮跳过。
- **按内容去重**：内容相同的文件共用一张卡片和一次模型调用；缓存键为内容 sha256 + 提示词版本。Go 文件优先，其次按路径排序。缓存中解码失败的 JSON 文件被删除并视为缺失；Windows 下同一哈希并发写入冲突时，读到另一写入者留下的有效卡片即视为成功。所有缓存写入（卡片、失败集及其删除、骨架）失败时线性退避重试 3 次（10ms、20ms），仍失败返回 `ErrCacheWrite`；读取失败（非解码失败）返回 `ErrCacheRead`。
- **预算**：每轮最多 `max_cards_per_run` 次**卡片模型调用**（不是文件数，默认 600）、并发 `concurrency`（默认 4）、时长 `max_run_minutes`（默认 15）。被预算截断、时间到或父 context 取消时状态为 `partial`，已生成的卡片保留，下轮继续。
- **卡片失败**：回复不可用 → 该内容哈希进入失败集 `llm/failed-<prompt 版本>.json`，不再重试，直到内容变化或手动"重新生成"（`full` 运行清空失败集重试）。传输错误 → 本轮计入 `card_transport_errors`，状态 `partial`，下轮重试；同一哈希累计 3 轮传输错误后转入失败集，避免一直被拒的文件永远卡住合成。连续 5 次调用传输失败 → 本轮失败（`ErrModelUnavailable`）。
- **合成触发**：本轮没有预算截断、没有取消、没有待重试的传输错误时才合成；只允许失败集中的文件缺卡片（计入 `card_errors`）。
- **骨架推断（按目录）**：输入为每个含卡片资格文件的目录（文件数 + 最多 3 条"文件名：职责"）及 `package main` 入口，要求划分 3–15 个执行阶段（按请求/任务处理流程，不按技术分层），每个目录归入一个阶段，文件继承目录的阶段。提示词逐级降级直到满足"≤ 120KB 且目录数 ≤ 359"：每目录职责行 3 → 1 → 0，再把目录合并到前 4、3、2、1 级（每级先 1 行职责再 0 行）。回复上限 `max_tokens = min(1000 + 20 × 目录数, 8192)`（许多 OpenAI 兼容服务拒绝更大的 `max_tokens`，目录数上限 359 由此而来）。阶段 id 须匹配 `^[a-z0-9][a-z0-9-]{0,39}$` 且去重；回复漏掉的目录继承最近的已分配祖先目录，仍未分配的文件取最近目录链上的多数阶段；没有文件的阶段丢弃，超过 15 个时保留文件最多的 15 个。
- **退化（按目录分区作阶段）**：`fallback_reason` 为 `too_large`（降级后仍放不下）、`bad_reply`（回复不可用）、`few_stages`（有效阶段 < 2）、`unassigned`（未归类 > 10%）、`no_model`。退化用 P2a 的目录分区作阶段，超过 15 个分区时保留最大的 14 个，其余并入"其他"。骨架推断的传输错误不退化，按合成失败处理。`bad_reply` 退化的骨架在 24 小时后重试模型（重建原因 `fallback_retry`）。仓库没有任何卡片资格文件时得到空骨架（0 个阶段）。
- **重建判定**：`none`（无骨架）/ `prompt`（提示词版本变化）/ `age`（距上次重建 > `skeleton_rebuild_days`，默认 30）/ `fallback_retry` / `changes`（自上次重建累计改动的**不同**路径 × 5 > 重建时文件数，即 > 20%）/ `topdir`（出现新顶层目录，仓库根 `.` 除外）/ `unassigned`（增量归类后未归类 > 10%）/ `manual`（`full` 运行）。不依赖增量结果的条件先判断，避免对马上要重建的骨架白做增量；`unassigned` 在增量更新之后判断。
- **增量更新**：删除的文件移出骨架；内容变化的文件保留阶段；新文件取同目录或最近上级目录的多数阶段；变得没有文件的阶段被删除；改动路径累计到 `ChangedPaths`（去重排序）。每个文件的归属 `FileAssign` 记 `Stage`、`Hash`（上次归类时的内容哈希）与 `CardHash`（展示的卡片）：当前内容有卡片时 `CardHash = Hash`，否则保留旧 `CardHash`，渲染为"已过期"。
- **合成**：受影响阶段（文件集变化、文件内容变化、获得了当前卡片，或尚无说明；重建时为全部阶段）重写说明（提示词 ≤ 40KB，`max_tokens = 700`，≤ 1500 rune）；有阶段且有说明被重写、总览为空或阶段集变化时重写总览（`max_tokens = 1500`，≤ 4000 rune），**0 个阶段时不写总览**；寄存器用途只为引用次数最多的 60 个寄存器生成，每批 20 个（`max_tokens = 1500`，每条 ≤ 160 rune），已有用途的寄存器只有在读写位置所在文件改动时才重写（重建时全部重写，提示词版本未变则先沿用旧用途）。合成调用的传输错误重试 2 次（线性退避 1s、2s），仍失败则本轮失败且不写骨架；回复不可用时保留旧文本。所有步骤成功后，骨架有变化或 commit 变化才写入 `skeleton.json`。
- **清理**：合成成功后删除当前 facts 与骨架 `CardHash` 都不再引用的卡片、其他提示词版本的卡片与失败集文件、超过 1 小时的临时文件；清理失败只记日志，不影响本轮结果。
- **渲染**（`GeneratorVersion = p2b-1`，变化会触发全部仓库确定性重建一次）：`LoadLLMLayer` 读取卡片资格文件的当前卡片、骨架中旧 `CardHash` 对应的过期卡片，以及当前提示词版本的骨架。有阶段时：`SKILL.md` 换成按阶段组织的模板（§7.2 模板）；`references/index.md` 标题改为"索引"，先列"执行阶段"表（阶段、文件数、说明首句、页面），再列"未归类文件""已过期文件"（各最多 100 个），之后是目录分区；`references/stages/<id>.md` 为阶段说明 + 每个文件的卡片（职责与 role、说明、执行时机、关键函数及 facts 中的行号；过期卡片标"已过期，以源码为准，改用 rca_grep"）；`references/overview.md` 增加"系统总览（LLM 生成）"与阶段列表；`references/registers.md` 每个寄存器加"用途："；分区页每个文件加"职责"行（过期标注）。没有阶段但有卡片时仍按 P2a 模板渲染并带卡片职责。
- **渲染净化**：LLM 文本在渲染时再次按上限截断；单行字段折叠空白，表格单元格转义 `|`，行首的 `#`、`>`、`-`、`+`、`*`、`=`、`|`、反引号、`~`、`<` 与有序列表标记被转义；多行 Markdown（总览、阶段说明）的标题降级到 `####` 以下，setext 下划线与 HTML 块起始被转义，未闭合的代码围栏补闭合；非法或重复的阶段 id 跳过，函数名去掉反引号。
- **缓存出错时降级**：确定性构建读取 `llm/` 出错时不阻塞发布，按无 LLM 内容渲染，且 `handbook_stats.llm_rev` 留空——与期望的 rev 不符，下一轮扫描会再重建。单张卡片读取失败按缺失处理。

`handbook_llm` 字段：

| 字段 | 含义 |
|---|---|
| `state` | `partial` / `complete` / `failed`；只记录过基础设施错误时可能没有 |
| `model` / `commit` / `prompt_version` | 本轮使用的模型名（配置值）、基于的 handbook commit、`LLMPromptVersion` |
| `cards_total` / `cards_done` / `cards_new` | 需要卡片的文件数 / 有当前卡片的文件数 / 本轮新生成卡片覆盖的文件数（均按文件计，同内容文件共享卡片） |
| `card_errors` / `card_transport_errors` | 失败集中的文件数（等内容变化或手动重新生成）/ 本轮调用出错、下轮重试的文件数 |
| `stages` / `fallback` / `fallback_reason` / `skeleton_rebuilt` / `rebuild_reason` / `skeleton_built_at` | 骨架信息；未合成的轮次沿用上一轮的 `stages`、`fallback`、`fallback_reason`、`skeleton_built_at` |
| `tokens_in` / `tokens_out` | 本轮 token |
| `run_at` / `duration_ms` | 本轮开始时间与耗时 |
| `rev` | 内容修订号；变化即触发确定性重渲染（确定性构建把它写进 `handbook_stats.llm_rev`） |
| `last_error` / `failed_commit` | 错误信息；`last_error` 也用于记录非失败轮次的最后一次传输错误和基础设施错误，只有 `state = failed` 才是失败 |

**SKILL.md 模板**（对齐论文 Figure 6，RCA 化）：

```markdown
---
name: handbook-<repo-slug>
description: >-
  <repo> 的行为地图。RCA 时用它找出与故障相关的所有代码位置：按执行阶段组织文件，
  并列出每个共享状态（表/缓存键/topic/接口）的全部读写位置。定位后必须用 rca_read 核实源码。
hidden_from_summary: true
---

# <repo> Handbook

## 文件
- references/overview.md —— 系统总览：主流程、阶段划分、外部依赖。先读。
- references/index.md —— 每个阶段（id、做什么、包含的文件），每个状态寄存器；末尾列出已过期/未归类文件。
- references/registers.md —— 每个寄存器的用途与**全部**读写位置（path:line）。
- references/stages/<id>.md —— 阶段说明 + 每个文件的卡片（定位符、职责、关键函数、关系）。

## 用法（RCA）
1. 读 overview，再读 index，确定与现象相关的阶段与寄存器；不要过早收窄到单一阶段。
2. 对涉及的每个寄存器读 registers.md，记下所有写入点——异常状态往往在远处被写坏。
3. 打开相关 stages/<id>.md。
4. 对每个候选位置用 rca_read 读真实源码确认；标注"已过期"的条目以源码为准，改用 rca_grep。
5. 结论只能基于你读到的源码，不能基于 handbook 的描述。
```

**P2a 渲染（`generator_version = p2a-2`）**：

- 页面为 `SKILL.md`、`references/overview.md`、`references/index.md`、`references/registers.md`、`references/areas/<id>.md`；"阶段"由目录分区代替（按首级目录分区，`internal`/`pkg`/`cmd`/`app(s)`/`src`/`service(s)`/`api` 下取两级）。
- **Skill 名**：`handbook-<slug>`，slug 为 `rel_path` 小写后把 `[a-z0-9]` 以外的连续字符替换为 `-`。slug 有损（`rel_path` 含 `[a-z0-9-]` 以外的字符，如嵌套路径 `group/svc`、大写、下划线）或超长被截断时，追加 `-<sha256(rel_path) 前 8 位十六进制>`，slug 部分总长 ≤ 60 字符，保证不同仓库的 skill 名不冲突。
- **页面大小**：每页（含 `index.md`）不超过 `MaxPageBytes` = 48KB，能被一次 `read_skill_file` 读完；超出时按节分页，首页列出续页 `<base>.p2.md`、`<base>.p3.md` …；单节超过一页时在 rune 边界截断并标注 `…（已截断）`。每文件符号、每寄存器位置、index 表格行数另有上限。
- `generator_version` 变化会触发所有仓库重建。P2b 起为 `p2b-1`，渲染时合入 LLM 层（见上文"P2b 实现"）。

### 7.3 组级 handbook（L0）

- **服务清单**：每个成员仓库一行（名称、角色、L1 首句）。
- **跨服务关系**：由各仓库 Phase I 寄存器候选做确定性 join：
  - 同一 HTTP/RPC 路由：提供方（路由注册）↔ 调用方（客户端调用）；
  - 同一 MQ topic：生产者 ↔ 消费者；
  - 同一表名 / 缓存键前缀：所有读写服务。
- **跨服务寄存器**：上述共享资源的全部读写位置（`repo:path:line`）。
- LLM 仅写组概述与每类关系的一句话说明。
- 刷新：任一成员仓库 handbook 版本变化 → 组级增量重算（join 部分为纯计算，成本低）。

### 7.4 code-map 入口 Skill

```markdown
---
name: code-map
description: 你可访问的代码仓库目录。排查与代码相关的问题时先读它，按组 → 仓库逐级查看 handbook。
---
## cloudgame（12 个服务）—— skill_view("handbook-group-cloudgame")
- svc-a：<一句话> —— skill_view("handbook-cloudgame-svc-a")
...
## 单独绑定
- infra/common-lib：<一句话> —— skill_view("handbook-infra-common-lib")
（无 handbook 的仓库标注"暂无 handbook，直接用 rca_grep"）
```

P2a 实现：每个 agent 一份 `handbooks/agents/<agent_id>/code-map/SKILL.md`（`summary_pinned: true`），按 `path.Dir(rel_path)` 分组列出有效仓库（无组级 handbook）；内容不变时不重写。实际 skill 名按 §7.2 规则生成，嵌套路径会带哈希后缀，例如 `cloudgame/svc-a` → `handbook-cloudgame-svc-a-<8 位十六进制>`，以 code-map 中列出的名字为准。

有效仓库集或任一 handbook 状态变化时重渲染。

---

## 8. 维护

### 8.1 三层更新

| 层 | 内容 | LLM | 触发 |
|----|------|-----|------|
| 校验 | 对比 `files.json` 哈希与当前文件；失配条目在内存视图中标记 frozen | 否 | 每次加载 handbook（带进程内缓存，按 `head_commit` 失效） |
| 增量刷新 | Phase I 全量重算（廉价）→ 只为新增/改动文件重生成 card 并重新归 stage → 受影响 stage 的 L2、L1、寄存器说明重写 → 删除文件移除 | 是，按改动量 | §5.2 巡检发现 HEAD 变化 |
| 骨架重建 | 重跑 Phase II 骨架推断 + 全量合成，复用未变文件的 card | 是，全量摘要 | 阈值或定期 |

骨架重建阈值（任一满足，配置可调）：

- 自上次重建以来改动文件 > 20%；
- 新增顶层目录；
- 未归类文件比例 > 10%；
- 冻结条目比例 > 15%；
- 距上次重建 > 30 天。

**P2b 实现**：

| 层 | 实际做法 |
|----|----------|
| 校验 / 冻结 | 不做运行时校验；"冻结"落实为渲染时的"已过期"标注：文件当前内容没有卡片、但骨架里有旧卡片时显示旧卡片并标"已过期，以源码为准"（§8.3）。设计中"冻结比例 > 15% 触发重建"不做——每轮增量会补上新卡片 |
| 确定性层 | 每次 HEAD 变化（或 `generator_version`、期望的 `llm_rev` 变化）由 `RebuildStale` 全量重算 Phase I 并发布，不调用 LLM |
| LLM 增量 | 同一次扫描里 `RebuildStale` 之后运行 `EnrichPending`：只为缺卡片的内容哈希生成卡片（预算内），新文件按目录多数阶段归类，改动文件保留阶段，删除文件移除，受影响阶段重写说明，总览与相关寄存器用途随之重写 |
| 骨架重建 | §7.2"重建判定"中的任一条件：无骨架、提示词版本变化、> `skeleton_rebuild_days`、改动路径 > 20%、新顶层目录、未归类 > 10%、`bad_reply` 退化满 24 小时、手动"重新生成"（`full`）；复用未变文件的卡片 |

`needsEnrich`（自动运行的条件）：仓库 `active`、未在构建、已发布的 handbook 由 HEAD 和当前生成器构建（`handbook_version > 0`、`handbook_commit = head_commit`、`generator_version` 一致）、LLM 租约空闲、该仓库的模型非空；并且上一轮未完成（`partial` 或无状态）、基于其他 commit 或提示词版本、骨架超龄、或 `bad_reply` 退化满 24 小时。`failed` 只在 HEAD 或使用的模型变化后自动重试。

### 8.2 并发与一致性

- 每个仓库同一时刻只允许一个构建任务：DB 行级租约（`handbook_status=building` + `handbook_lease_until` + `handbook_lease_token`），过期可抢占。
- 新版本写入 `v<N+1>/`，完成后更新 `current.json` 与 `handbook_version`；正在运行的 RCA 继续读已打开的旧版本。保留最近 2 个版本，其余由清理循环删除。
- 构建失败 → `handbook_status=failed`，保留旧版本继续服务。

**P2a 实现**：

- **租约**：列 `handbook_lease_until` + `handbook_lease_token`（migration `020_repo_handbook.sql`）。`ClaimHandbookBuild` 在未 building 或租约已过期时置 `building`、租约 30 分钟并生成新 token；`FinishHandbookBuild` / `ReleaseHandbookBuild` 只在 token 仍匹配时生效，否则返回 `ErrHandbookLeaseLost`，结果被丢弃（另一个构建已接管）。
- **超时**：单次构建上限 25 分钟（短于租约）；超时按失败记录（`failed`，`handbook_stats.failed_commit = HEAD`，`last_error` 注明"构建超时"），不会每轮扫描反复重试。
- **取消**：父 context 取消（进程关闭）、仓库读取失败时释放租约并恢复认领前的状态（原状态为卡住的 `building` 时恢复为 `ready`/`none`），不记失败；认领后发现仓库已不是 `active`（如已归档）同样释放跳过。
- **重试**：失败的 commit 不自动重试，HEAD 变化或手动重建才重试；`generator_version` 变化触发全部重建；租约过期仍停在 `building` 的仓库由 `RebuildStale` 重新认领。
- **并发**：全局最多 2 个构建同时运行（`RebuildStale` 与手动重建共用槽位）；手动重建在仓库正在构建或没有空闲槽位时返回 409 `HANDBOOK_BUILDING`。`RebuildStale` 在每次仓库扫描成功后由 cron 触发，同一时刻只跑一轮。
- **发布**：先写入 `v<N>.tmp-*` 唯一临时目录再 rename 为 `v<N>`，**从不覆盖已存在的版本目录**（`ErrVersionExists`）；遇到失去租约的构建遗留的目录时版本号顺延，最多尝试 3 次；成功后原子写 `current.json`，保留最近 2 版，超过 1 小时的临时目录由清理删除。
- **读取**：`ReadSkillFile` 校验 repo id（`[A-Za-z0-9_-]`）与路径每一段（白名单字符、不以 `.` 结尾），再经 `filepath.IsLocal`（拒绝 Windows 设备名）与 `os.OpenInRoot` 打开，拒绝目录，单次最多 256KB。

**P2b 实现（LLM 运行）**：

- **租约**：列 `handbook_llm_lease_until` + `handbook_llm_lease_token`（migration `021_repo_handbook_llm.sql`），与构建租约互相独立，不改 `handbook_status`。`ClaimHandbookEnrich` 在租约为空或已过期时认领，时长 = 运行超时（`max_run_minutes`）+ 10 分钟；`FinishHandbookEnrich` / `ReleaseHandbookEnrich` 只在 token 匹配时生效，否则结果被丢弃（`ErrHandbookLeaseLost`）。
- **并发**：全局同时最多 1 个 LLM 运行（`enrichSlots` 容量 1，`EnrichPending` 与 `RequestEnrich` 共用）。`EnrichPending` 由 cron 在每次扫描成功、`RebuildStale` 之后于同一 goroutine 调用，`TryLock` 保证同一时刻只有一轮；待运行仓库按上次 `run_at` 升序（从未运行过的最先），逐个同步运行，本次调用已用时超过运行超时后不再启动新的运行，剩余仓库留给下一次扫描。
- **手动**：`RequestEnrich(id, full)` 不论状态都可触发（含 `failed`）；仓库非 `active` → `ErrInvalidRepo`，模型为空 → `ErrHandbookLLMDisabled`，handbook 不是当前 HEAD + 当前生成器 → `ErrHandbookNotReady`，该仓库 LLM 租约被占 → `ErrHandbookBuilding`，其他仓库正在运行（槽位满）→ `ErrHandbookLLMBusy`，已开始关闭 → `ErrHandbookShutdown`。认领成功后异步运行，context 派生自 usecase 的基础 context（不受 HTTP 请求结束影响，关闭时取消）；`RequestRebuild` 同理。
- **关闭**：`kratos` 传给 cron Server 的 context 不会在停止时取消，cron `Server.Stop` 自行取消调度器并等待其循环与扫描后启动的 handbook 任务（`RebuildStale` + `EnrichPending`）退出，最多 15 秒。之后 wire cleanup 先调用 `HandbookUsecase.Shutdown`（拒绝新的异步运行、取消基础 context、最多等 15 秒让手动构建/LLM 运行退出），再关闭数据库。`RebuildStale` / `EnrichPending` 的 context 也随基础 context 取消。被取消的运行释放租约；写结果与释放租约使用脱离取消、10 秒超时的 context。
- **配置**：`handbook:` 段（§11），`concurrency` 上限 16、`max_file_kb` 上限 256、`max_run_minutes` 上限 20，0 或不填取默认。模型名优先级：仓库 `handbook_model`（`off` 禁用）> 全局 `handbook.model`（环境变量 `SATH_HANDBOOK_MODEL` 覆盖）。按模型目录解析（与 critic 相同：精确模型名，或 `<provider 名称或 ID>/<模型名>`），每次解析有超时。只要有模型目录就安装解析器，因此即使没有全局模型，仓库级覆盖也能运行。
- **失败与重试**：解析模型失败或 `Enrich` 返回模型相关错误（连续调用失败、合成传输错误）→ `state = failed`、`failed_commit` = 当前 commit、`model` = 所用模型，HEAD 或模型变化、或手动重新生成前不自动重试；失败轮次若已改变缓存（如生成了部分卡片）仍会换新 rev 触发重渲染。读取 facts、解析仓库路径、打开缓存目录、缓存读写失败（`ErrCacheRead` / `ErrCacheWrite`，如 Windows 下读者占用导致 rename 失败）等**基础设施错误**不记失败，只写 `last_error`、`run_at`、`duration_ms`（`state` 不变；本轮已改变缓存时换新 rev），下一次扫描重试。
- **取消**：父 context 取消（进程关闭）、仓库读取失败、认领后发现仓库已非 `active` / handbook 不再是当前版本 / 模型被禁用时，释放租约，不记录任何状态；context 已取消时出现的错误（如解析模型失败）也按取消处理，不记失败。
- **重渲染**：运行改变了缓存（新卡片或骨架变化）或此前没有 rev 时生成新 `rev`（`now` 的 36 进制纳秒），写库后调用 `RequestRebuild`；`RebuildStale` 也会把 `handbook_stats.llm_rev` 与期望值（LLM 层禁用时为空）不一致的仓库视为过期，保证重渲染最终发生。确定性构建只在仓库的模型非空时读取 `llm/`。

### 8.3 冻结条目的呈现

论文中冻结条目不参与定位；RCA 场景放宽：仍出现在 index，但标注"已过期，以源码为准，改用 rca_grep"。保证 handbook 过期时退化为现状（Baseline），不会更差。

P2b 实现：文件内容变化后、新卡片生成前，阶段页显示旧卡片并在文件标题后标"已过期，以源码为准，改用 rca_grep"，分区页的职责行标"已过期，以源码为准"且不附卡片中的函数说明；`references/index.md` 末尾列出"已过期文件"；新卡片生成并重渲染后标注消失。

### 8.4 版本对齐

- `manifest.json` 记录 commit；RCA turn trace 记录本次使用的 `handbook_commit` 与仓库 `head_commit`。
- 本期只维护 HEAD 一份；行为结构变化慢，具体行号以 Agent 实际读取的挂载代码为准。
- 线上版本 ≠ 挂载版本是既有问题，本设计只将其显式化，不解决。

---

## 9. Overlay 与反馈闭环

### 9.1 Overlay 分层原则

| 内容 | 归属 | 例 |
|------|------|----|
| 关于**代码的事实** | 仓库/组 overlay（共享） | "`order_status` 还在 `refund/compensate.go:88` 被写" |
| 关于**排查策略** | agent 自己的 Skill | "卡顿先看网关 p99"、"本团队先查 ES 日志" |

判据：换一个 agent 来问是否仍成立。

`overlay/notes.jsonl` 每行：`{id, scope: stage|register|file, anchor, locator, text, source: {agent_id, session_id, turn}, status: active|stale, created_at, reviewed_by}`。渲染时合并到对应 stage/register 页面，标注"经验"。overlay 定位符参与 §8.1 校验，失效标 `stale` 并提示人工复核，不自动删除。

### 9.2 RCA 确认信号

portal 与 web 当前**没有任何用户反馈机制**（无点赞、无"已解决"）。确认信号按强度分三档，组合使用：

| 档 | 信号 | 获取方式 | 噪声 |
|----|------|----------|------|
| 强 | 用户显式确认："已解决 / 根因正确"按钮，可选勾选或纠正根因位置 | 新增反馈 API + 消息气泡按钮 | 低，但覆盖率低 |
| 强 | **修复提交命中**：RCA 引用的证据文件/函数在之后 N 天（默认 7）内被修改 | 仓库扫描增量刷新时已算出改动文件集与函数 body 指纹变化（§8.1），与近期 RCA 证据求交，**无需 git 命令** | 中：改动不一定是修这个 bug；函数级命中比文件级可靠 |
| 中 | 用户后续文本的正/负向表达（"对，就是这里"、"修好了" / "不对"、"不是这个"） | 复用 evolution 规则粗筛 + LLM 分类，新增 `rca_confirm` / `rca_reject` 两类 | 中 |
| 弱 | 无追问：回答后同一会话无纠正、同一 agent N 小时内无相同问题再问 | turn trace 离线统计 | 高，只作统计权重，不单独触发任何动作 |

用途分级：

- **overlay 提案**：仅强档或中档正向信号触发，且仍走人工评审；
- **漏召回统计**（§9.4）：只用强档 + 中档正向样本，避免错误结论污染"证据集合"；
- **负向信号**（显式否定、"不对"）：该次 RCA 的证据不计入统计，且若其路由集合命中 handbook 条目，记一次"误导"，累计到 stage/register 级别供复核；
- 修复提交命中时，在仓库管理页的 RCA 记录上展示"可能的修复提交"，由用户一键确认，转为强档显式确认。

#### 9.2.1 反馈入口

两个渠道写入同一张表 `rca_feedback`（`id, agent_id, session_id, turn_index, channel, verdict: resolved|root_cause_ok|wrong, corrected_locators JSON, user_id, created_at`），同一 `(session_id, turn_index, user_id)` 只保留最新一条。

- **web**：回答气泡下"已解决 / 根因正确 / 不对"按钮；选"不对"可选填正确位置（仓库 + 文件，带自动补全，补全数据来自有效仓库集）。
- **企微**：gateway 当前只处理文本消息回调（`gateway/internal/wecom/frame.go` 仅 `aibot_msg_callback` / `aibot_respond_msg`，`normalize.go` 拒绝非 text 消息），回复只有 stream 一种，需新增：
  1. 最终 stream 回复完成后，**另发一张模板卡片**（按钮："已解决"、"根因不对"），不改动现有 stream 回复路径，失败不影响主回复；
  2. 处理卡片按钮的事件回调：新增事件帧的 cmd 常量与解析（具体 cmd 名与帧格式以企微智能机器人长连接文档为准），按钮 key 携带一次性 token；
  3. token → `(agent_id, session_id, turn_index)` 映射存于 gateway 已有的 idempotency store 同类存储，TTL 7 天；回调后调 portal 反馈 API；
  4. 点击后更新卡片为"已记录，谢谢"，防重复点击；"根因不对"只记录 verdict，纠正位置引导到 web 填写（卡片附 web 会话链接）。
- 仅当本轮调用过 `rca_*` 工具时才展示反馈入口，避免普通问答打扰用户。

#### 9.2.2 修复提交命中的校准（上线前回放）

阈值不拍脑袋，P3 上线前用历史数据回放：

1. **样本**：最近 1–3 个月调用过 `rca_read` 的 turn（turn trace 已持久化），抽取证据集合 E（仓库 + 文件 + 若为 Go 则所在函数）。
2. **改动序列**：对涉及仓库在宿主机上一次性导出该时间段的 commit 序列与每个 commit 的改动文件/函数（离线脚本可用 git 命令，运行时仍不依赖 git）。
3. **网格**：时间窗 {1, 3, 7, 14} 天 × 粒度 {文件, 函数} × 是否要求"同一 commit 改动多个证据位置"。
4. **人工标注**：每个网格点随机抽 50 条命中，由熟悉业务的人判断"这次改动是否确实是在修这次 RCA 的问题"，得到精确率；同时统计每个网格点的命中覆盖率。
5. **选点**：取精确率 ≥ 80% 前提下覆盖率最高的组合，写入配置 `rca_fix_match.{window_days, granularity, min_hits}`。
6. **持续监控**：上线后修复命中的"一键确认 / 驳回"比例即在线精确率；连续 2 周低于 70% 自动告警并回退到更严格的组合。

精确率达不到 80% 的组合不启用，修复提交命中只作为"待确认"提示展示，不计入强档。

### 9.3 Overlay 提案

在 `evolution_proposals` 增加信号类型 `handbook_fact`、`target_action=overlay_add`、`target_path=handbooks/repos/<repo_id>/overlay`：

- 来源：RCA 被确认（§9.2 强档或中档正向）后，从 turn 中抽取"源码证据 + 结论"中的代码事实。
- 落库前确定性校验：引用的路径存在、行号范围合法、符号在 `graph.json`/文件中可找到；不通过直接丢弃。
- 复用已有的冲突检查、embedding 去重、评审页面；评审页按仓库聚合，记录来源 agent。

### 9.4 漏召回检测（离线，无需改 framework）

从 turn trace 的工具调用离线计算，仅针对 §9.2 强档或中档正向确认的 RCA：

- **路由集合** R：本轮 `skill_view` / `read_skill_file` 读到的 handbook 页面中出现的文件定位符；
- **证据集合** E：本轮 `rca_read` 读取且在最终回答中被引用的文件（`repo` 为 §6.1.1 的逻辑名）；
- **漏召回** = E \ R，按仓库汇总。

每日 cron 汇总：漏召回率写入 `repositories.handbook_stats`；同一文件反复漏召回 → 加入下次增量刷新的"重新归类"队列，并在 coverage 中标注。

---

## 10. 可见性与权限

- Agent 只能看到 `agent_effective_repos` 中仓库的 handbook 与其所在组的组级 handbook；组级 handbook 渲染时**按 agent 可见仓库裁剪**（跨服务关系只列可见仓库，对不可见端显示"外部服务"）。因此组级 handbook 的 `skill/` 需按 agent 渲染一份视图（存于 `agents/<agent_id>/`，与 code-map 同时重渲染）。
- `auto_apply_new=false` 的组，新成员进入 `pending_confirm`，组负责人确认后才生效。
- 仓库/组的增删改、绑定变更走现有 `user_resource_acl` 权限体系（`portal/migrations/007_user_resource_acl.sql`），资源类型新增 `repo`、`repo_group`。
- **P1 暂缓 ACL 资源类型**：全局的仓库/组接口只要求登录；agent 绑定接口要求对目标 agent 有编辑权限（`copy-from` 另需对源 agent 有查看权限）；迁移接口只报告和处理调用者有编辑权限的 agent。`repo` / `repo_group` 资源类型在后续阶段补上。

---

## 11. API

P1 已实现的接口（列表接口不分页，返回全量）：

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/repos` | 列仓库，过滤参数 `status` / `q` / `group_id` / `code_root`；返回 `{items, total}` |
| GET | `/api/v1/repos/{id}` | 详情 + 有效仓库集中包含该仓库的 agent |
| PATCH | `/api/v1/repos/{id}` | 改名称、描述、标签、负责人；`status` 只允许 `active` / `archived` |
| POST | `/api/v1/repos/scan` | 手动触发扫描；扫描进行中返回 409 `REPO_SCAN_RUNNING` |
| POST | `/api/v1/repos/migrate-legacy-links` | 旧 `workspace/code` 迁移；默认只出报告，`?apply=true` 才写入（§14） |
| GET | `/api/v1/repo-groups` | 列组，`?kind=` 过滤；返回 `{items}`，每项含 `repo_ids` |
| POST | `/api/v1/repo-groups` | 新建 manual 组 `{"name","repo_ids"}` |
| PUT | `/api/v1/repo-groups/{id}/members` | 整体替换 manual 组成员 `{"repo_ids"}` |
| DELETE | `/api/v1/repo-groups/{id}` | 删除 manual 组；仍被 agent 绑定时返回 409 `REPO_GROUP_IN_USE` |
| GET | `/api/v1/agents/{agent_id}/repo-bindings` | 绑定列表 + 有效仓库集（含 via） |
| PUT | `/api/v1/agents/{agent_id}/repo-bindings` | 整体替换绑定（含排除），`{"bindings":[]}` 表示清空；返回新的有效仓库集 |
| POST | `/api/v1/agents/{agent_id}/repo-bindings/copy-from/{other_id}` | 复制其他 agent 的绑定 |

P2a 已实现的 handbook 接口：

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/repos/{id}/handbook` | handbook 状态、commit、HEAD、版本、stats 与已发布版本的页面列表 |
| GET | `/api/v1/repos/{id}/handbook/page?path=` | 读取已发布版本的一个页面（相对 skill 目录，如 `references/index.md`）；路径非法 400 |
| POST | `/api/v1/repos/{id}/handbook/rebuild` | 异步重建（不论是否过期）；仓库非 `active` 400；正在构建或全局构建槽位已满 409 `HANDBOOK_BUILDING` |

错误码：参数非法 400 `INVALID_ARGUMENT`；仓库/组不存在 404 `NOT_FOUND`；agent 不存在沿用 `AGENT_NOT_FOUND`；409 见上表；handbook 未发布或页面不存在 404 `HANDBOOK_NOT_FOUND`；未配置 handbook 503 `HANDBOOK_DISABLED`。

P2b 新增与变更的接口：

| 方法 | 路径 | 说明 |
|------|------|------|
| PATCH | `/api/v1/repos/{id}` | 新增可选字段 `handbook_model`：去首尾空白，≤ 255 字符且不含空白/控制字符（否则 400 `INVALID_ARGUMENT`）；`off` 不区分大小写，存为 `off`；`""` 清除覆盖 |
| POST | `/api/v1/repos/{id}/handbook/enrich?full=` | 异步启动该仓库的 LLM 运行，成功 200 `{"accepted":true}`。`full` 用 `strconv.ParseBool` 解析（`1`/`true`/`0`/`false` 等），非法 400 `INVALID_ARGUMENT`；`full=true` 重建骨架并重试失败集。错误：仓库非 `active` 400 `INVALID_ARGUMENT`；LLM 层禁用 400 `HANDBOOK_LLM_DISABLED`；handbook 不是当前 HEAD/生成器 409 `HANDBOOK_NOT_READY`；该仓库 LLM 运行中 409 `HANDBOOK_BUILDING`；其他仓库 LLM 运行中 409 `HANDBOOK_LLM_BUSY`；仓库不存在 404 `NOT_FOUND`；未配置 handbook 503 `HANDBOOK_DISABLED` |
| GET | `/api/v1/handbook/config` | 全局 LLM 配置（已套默认值与上限）：`model`、`concurrency`、`max_cards_per_run`、`max_file_kb`、`max_run_minutes`、`skeleton_rebuild_days`，以及 `available`（已安装模型解析器；否则任何仓库包括覆盖都不运行）与 `enabled`（`available` 且全局模型非空） |
| GET | `/api/v1/repos/{id}/handbook` | 新增 `llm`（`handbook_llm`）、`llm_model`（该仓库实际使用的模型，`""` = LLM 层关闭）、`llm_running`（LLM 租约未过期） |

`GET /api/v1/repos`、`/repos/{id}` 的仓库对象新增 `handbook_model`、`handbook_llm`、`handbook_llm_lease_until`。

配置（`portal/configs/config.yaml`）：

```yaml
handbook:
  model: ""                 # 空 = 只生成静态 handbook（仓库可单独覆盖）；环境变量 SATH_HANDBOOK_MODEL 覆盖
  concurrency: 4            # 上限 16
  max_cards_per_run: 600    # 每轮卡片模型调用数
  max_file_kb: 24           # 上限 256
  max_run_minutes: 15       # 上限 20；租约 = 该值 + 10 分钟
  skeleton_rebuild_days: 30
```

配置只在进程启动时读取；仓库级覆盖经 PATCH 立即生效。

后续阶段的接口：`POST /api/v1/repos/batch`、`POST /api/v1/repo-groups/{id}/members/confirm`，以及 tag 组的创建与修改。

---

## 12. 前端

P1b 已实现（侧栏「代码仓库」入口，页内标签切换）：

- **仓库页** `/repos`：按状态、code root、分组、关键字筛选；列出路径、所属分组、标签、分支与提交、状态、最近扫描时间；手动扫描并显示报告；编辑名称、描述、标签并查看使用该仓库的 agent；归档与恢复。
- **分组页** `/repo-groups`：目录组只读（随扫描更新）；手工组新建、编辑成员、删除（仍被绑定时提示 409 信息）。
- **迁移页** `/repos/migration`：生成迁移报告（不写入），展示方案、迁移后可见的逻辑名、原因；确认后应用自动迁移。
- **Agent 详情页「代码仓库」区块**：勾选分组；展开分组逐个排除；单独添加仓库（可填子目录）；本地预览"保存后可见 N 个仓库"及每个仓库的来源；清空绑定（回到旧链接）；从其他 agent 复制。绑定需要已有 agent id，因此放在详情页而不是编辑表单。
- **Agent 编辑页**：已有绑定时，在 workspace/code 选择处提示"RCA 代码工具使用绑定的仓库，链接只影响工作区文件浏览"。

P1b 的限制：仓库表没有"使用 agent 数"列（列表接口不返回，改在编辑弹窗列出使用该仓库的 agent）；"从其他 agent 复制"只列前 100 个 agent；读取绑定需要 agent 编辑权限，只有查看权限时区块显示无法加载。

P2a 已实现：

- 仓库表新增 **Handbook 列**：未生成 / 生成中 / 最新 / 待更新 / 失败（悬停显示 `last_error`）。
- **重建按钮**：仓库为 `active` 即可点击，构建中也可以点（后端返回 409 时在列表上方内联显示"重建 … 失败：…"）；列表中存在租约未过期的构建时每 3 秒静默刷新一次，没有活跃构建（含租约已过期、卡住的 `building`）时停止轮询。
- **查看弹窗** `HandbookDialog`：状态、版本、commit 与 HEAD 取自弹窗内获取的 handbook 视图（不依赖列表行的旧数据）；左侧页面列表（SKILL.md → references → areas）、右侧页面内容；视图与页面加载时显示"加载中…"，失败时显示错误；Esc 关闭。

P2b 已实现：

- **LLM 徽章**（仓库表 Handbook 列下方）：未启用 / 增强中 / 待增强 / 部分完成 x/y / 已完成 x/y / 失败，悬停显示 `last_error`。前端按 `/handbook/config` 与仓库字段计算：服务端无模型解析器（`available=false`）、仓库设为 `off`、或既无覆盖也无全局模型 → 未启用；LLM 租约未过期 → 增强中；`failed` 只在 `failed_commit` 仍等于 `handbook_commit` 且模型未变时显示为失败（否则后端会重试，显示待增强）；无状态或 `commit` 不是当前 handbook commit → 待增强。进度 x/y 只在部分完成与已完成时显示。列表在有活跃构建**或**活跃 LLM 运行时每 3 秒静默刷新。
- **编辑弹窗**新增"Handbook 模型"输入框，候选来自模型目录（仅启用且有 API key 的 provider、非隐藏条目，取值 `<provider 名称或 ID>/<模型名>`，名称含空白或 `/` 时用 ID）以及 `off`；占位提示显示继承的全局模型或"服务端未启用 LLM 增强"。
- **HandbookDialog 的 LLM 区块**：状态徽章、所用模型、卡片 x/y（本轮新增、生成失败、调用出错待重试）、阶段数（退化时注明原因）、最近一轮时间/耗时/token；`last_error` 在失败时显示为错误、否则显示为"最近一轮有错误"警告。"重新生成 LLM 内容"按钮调用 `enrich?full=1`（LLM 关闭或运行中时禁用），失败按服务端 `reason`（`ApiError` 携带 HTTP 状态与 reason）映射成中文提示；运行期间每 3 秒刷新视图，运行结束后自动重新加载当前页面内容；触发成功后通知列表刷新。

后续阶段：落后天数、冻结比例、漏召回率列（P3）；仓库批量操作；tag 组与 pending 成员确认（P4）。

---

## 13. 监控与评测

### 13.1 指标

按仓库：handbook 落后 HEAD 天数、冻结比例、未归类比例、漏召回率、构建耗时与 token。按 agent：有效仓库数、无 handbook 的仓库数。

P2b 起，构建 token 与卡片指标的来源是 `repositories.handbook_llm`（`tokens_in` / `tokens_out` / `duration_ms` / `cards_*` / `card_errors`，每轮覆盖）；过期比例可由 `handbook_stats.stale_cards / cards` 得出。暂无单独的指标上报。

### 13.2 RCA 定位 A/B（`evals/`）

- 新任务集 `evals/tasks/rca_localization.jsonl`：故障描述 + 标注的根因位置（文件、符号）。
- 两组：`handbook=off`（现状）/ `handbook=on`，其余完全一致。
- 指标（借鉴论文）：
  - 文件级、符号级 Recall / Precision / F1；`Wrong`（与标注零重叠的比例）；
  - judge 先基于故障描述 + 原始代码构建**防泄漏答案键**，再分维度打分：定位 0.5、范围控制 0.25、推理有据 0.25；胜负阈值 δ=3；
  - token 消耗。
- 进入 nightly；`handbook=on` 相对 `off` 的优势消失视为 handbook 腐化信号。

---

## 14. 迁移

P1 以接口 `POST /api/v1/repos/migrate-legacy-links` 实现（默认 dry-run，`?apply=true` 写入），可重复执行：

1. 执行首次扫描，建立 `repositories` 与 dir 组。
2. 遍历调用者有编辑权限的 agent，解析 `workspace/code` 目标（软链接解析到 code root 的真实路径时，映射回配置的 code root 再比较；Windows 下路径比较不区分大小写）：
   - 目标是 git 根 → `bind_repo`，可自动写入；
   - 目标恰为某 dir 组目录，且目标下所有仓库都是该组的**直接**成员 → `bind_group`，可自动写入；
   - 目标恰为某 dir 组目录但其下还有更深层的仓库 → 降级为 `manual_multi`，`reason` 列出嵌套仓库（dir 组只含直接子仓库，自动绑定会让 agent 少看到这些仓库）；
   - 目标是包含多个 git 根的其他目录 → `manual_multi`，候选为其下所有仓库的逐个 `repo` 绑定；
   - 目标是仓库内子目录 → `manual_subdir`，候选为所属仓库 + `sub_paths`；
   - 其余 → `unresolved`；已有绑定的 agent → `skip_has_bindings`。
3. `apply=true` 只写入 `bind_repo` / `bind_group`，写入后重算该 agent 的有效仓库集；`manual_*` 只出报告，这些 agent 保持旧软链接逻辑直到人工处理。
4. 报告每项包含 `action`、候选 `bindings`、`reason`，以及 `after_roots`：按候选绑定展开后 RCA 工具将看到的逻辑名列表，用于和旧链接对比。

workspace-link 接口不再自动绑定（§6.3）。

---

## 15. 分期

| 期 | 内容 | 验收 |
|----|------|------|
| P1 | 仓库表、扫描、dir 组、绑定（repo/group/排除）、有效仓库集、**RCA 仓库逻辑名（§6.1.1）**、运行时 RCA roots、迁移、仓库与绑定 UI | 30 仓库 agent 一次勾选完成绑定；basename 重名的两个仓库 `rca_read` 读取正确；旧 agent 行为不变 |
| P2a | 确定性 handbook（Phase I 事实 + 目录分区渲染，无 LLM）、code-map、`hidden_from_summary` / `summary_pinned`、保留名、租约与版本化发布、HEAD 变化自动全量重建、仓库页 Handbook 列/查看/重建（计划 `docs/superpowers/plans/2026-10-09-repo-handbook-p2a.md`） | 任选 1 个业务仓库生成 handbook；agent 绑定后 `code-map` 出现在摘要首位、handbook 不进摘要；HEAD 变化后下一次扫描内重建 |
| P2b | LLM 文件卡片（按内容去重）、行为阶段（`references/stages/`）、总览、寄存器用途、预算分轮的增量刷新、骨架重建与退化、"已过期"标注、仓库级模型覆盖、LLM 状态 UI 与手动重新生成（计划 `docs/superpowers/plans/2026-10-09-repo-handbook-p2b.md`，已实现；真实模型冒烟待人工执行，步骤见计划末尾） | 选 1 个业务仓库跑通；HEAD 变化后的下一次扫描（默认 10 分钟）内完成确定性重建与增量 LLM 刷新（改动在单轮预算内时） |
| P3 | RCA 确认信号（反馈按钮 + 文本分类 + 修复提交命中）、RCA 定位 A/B 评测、漏召回统计 | 反馈可在 web 提交；修复提交命中可在仓库页展示；A/B 报告进 nightly |
| P4 | 组级 handbook、按 agent 裁剪视图、tag 组、规则绑定 | 跨服务问题样例中组级 handbook 被路由命中 |
| P5 | overlay 提案接入 evolution 流水线 | 确认的 RCA 能产生可评审 overlay 提案 |

确认信号放在 P3 而非 P5：漏召回统计和 overlay 都依赖它，且反馈按钮本身不依赖 handbook，可与 P1/P2 并行开发。
| 后续 | `sync_mode=managed`、版本快照 | — |

P1 独立有价值（解决多仓库绑定与可观测性），不依赖 handbook。

---

## 16. 文件变更清单

| 操作 | 文件 | 说明 |
|------|------|------|
| CREATE | `portal/migrations/019_repo_registry.sql` | repositories / repo_groups / repo_group_members / agent_repo_bindings / agent_effective_repos |
| CREATE | `portal/internal/data/model/repository.go` 等 | GORM model |
| CREATE | `portal/internal/data/repository_repo.go` | Repo 实现 |
| CREATE | `portal/internal/biz/repo_registry.go` | 扫描、分组、绑定展开 Usecase |
| CREATE | `portal/internal/biz/handbook.go` | handbook 构建/刷新/校验编排 |
| CREATE | `portal/internal/handbook/`（P2a 已实现：`facts.go`、`gosyms.go`、`registers.go`、`render.go`、`codemap.go`、`store.go`、`builder.go`；P2b 已实现：`llmcache.go`、`llmcall.go`、`cards.go`、`organize.go`、`synthesize.go`、`enrich.go`；以后：validate、group_join） | handbook 生成管线 |
| CREATE | `portal/migrations/020_repo_handbook.sql` | P2a：`handbook_lease_until`、`handbook_lease_token` |
| CREATE | `portal/migrations/021_repo_handbook_llm.sql` | P2b：`handbook_model`、`handbook_llm`、`handbook_llm_lease_until`、`handbook_llm_lease_token` |
| CREATE | `portal/internal/biz/handbook_llm.go` | P2b：`HandbookLLMConfig`、`SetLLM`、`EnrichPending`、`RequestEnrich`、LLM 租约与状态记录 |
| CREATE | `portal/internal/conf/handbook_config.go` | P2b：`handbook:` 配置段与 `SATH_HANDBOOK_MODEL` |
| CREATE | `portal/internal/service/handbook_llm.go`；MODIFY `service/critic_model.go` | P2b：抽出 `catalogModelResolver`，`ConfigureHandbookLLM` 接线 |
| MODIFY | `portal/internal/biz/handbook.go`、`repo_registry.go`、`repo_registry_usecase.go`，`portal/internal/data/repo_registry.go`、`data/model/repo_registry.go`，`portal/internal/server/repo_registry.go`、`http.go`，`portal/internal/cron/scheduler.go`，`portal/cmd/backend/main.go`、`wire.go`、`wire_gen.go`，`portal/configs/config.yaml` | P2b：重建判定加 `llm_rev`、LLM 租约方法、`handbook_model` 校验与更新、enrich/config 接口、扫描后运行 `EnrichPending`、配置加载 |
| MODIFY | `web/src/api/client.ts`（`ApiError`）、`api/repoRegistry.ts`、`api/repoRegistryTypes.ts`、`utils/repoRegistry.ts`、`components/HandbookDialog.tsx`、`pages/RepoListPage.tsx`、`pages/RepoRegistry.css` | P2b：LLM 状态、模型覆盖、重新生成 |
| CREATE | `portal/internal/service/handbook_dirs.go` | P2a：`HandbookSkillDirResolver`，`sharedSkillDirs` 末尾追加 code-map 与 handbook 目录（fail-open） |
| CREATE | `web/src/components/HandbookDialog.tsx` | P2a：handbook 查看弹窗 |
| CREATE | `portal/internal/server/repos.go` | HTTP handlers |
| CREATE | `portal/cmd/migrate-repo-bindings/main.go` | 迁移 |
| MODIFY | `portal/internal/chat/code_roots.go` | `MergeRCARoots` 增加 effective roots 优先级 |
| MODIFY | `portal/internal/chat/rca_builder.go` | 传入 effective roots |
| MODIFY | `portal/internal/service/chat.go`、`agent.go` | `sharedSkillDirs` 注入 code-map 与 handbook 目录；查询有效仓库集 |
| MODIFY | `portal/internal/server/code_roots.go` | 旧 workspace-link 接口映射为绑定 |
| MODIFY | `portal/internal/cron/scheduler.go` | `repoScanLoop`、handbook 刷新队列、漏召回日汇总、旧版本清理 |
| MODIFY | `portal/internal/data/data.go` | AutoMigrate + ProviderSet |
| MODIFY | `portal/cmd/backend/wire.go`、`wire_gen.go` | DI |
| MODIFY | `portal/internal/biz/evolution.go`、`chat/evolution_pipeline.go` | `handbook_fact` 信号 + 确定性校验 |
| MODIFY | `framework/skills/index.go`、`skills/prompt.go`、`skills/route.go`、`skills/embed_route.go` | 支持 `hidden_from_summary`（不进摘要、不参与自动路由） |
| MODIFY | `framework/tool/call_graph.go` | 包级调用图（Go 仓库 Phase I） |
| MODIFY | `framework/tool/rca_repos.go`、`rca_code_tools.go`、`rca_symbol_tool.go` | 命名 root（`RCARoot{Name, Path}`）；`[]string` 入口检测 basename 重名 |
| CREATE | `portal/migrations/022_rca_feedback.sql`（021 已被 P2b 占用）、`portal/internal/biz/feedback.go`、`server/feedback.go` | RCA 确认反馈（已解决 / 根因正确 / 纠正根因位置） |
| MODIFY | `gateway/internal/wecom/frame.go`、`wsclient.go` | 卡片按钮事件回调帧、发送/更新模板卡片 |
| CREATE | `gateway/internal/wecom/feedback_card.go` | 反馈卡片构造、token 映射 |
| MODIFY | `gateway/internal/adapter/wecom_bot.go` | 最终回复后发反馈卡片（仅本轮用过 `rca_*`）；事件回调转 portal 反馈 API |
| MODIFY | `gateway/internal/runtimeclient/client.go` | 调 portal 反馈 API；turn 结果需带回"是否用过 rca 工具"与 turn 标识 |
| CREATE | `scripts/rca_fix_match_replay/` | §9.2.2 离线回放与标注抽样脚本 |
| MODIFY | Loadout 绑定写入校验（随 Loadout 实现落点）、`framework/tool/skillops`（`skill_manage`）、evolution 提案生成 | 拒绝保留名前缀 `code-map` / `handbook-` |
| MODIFY | `portal/internal/chat/evolution_pipeline.go` | 新增 `rca_confirm` / `rca_reject` 文本信号分类 |
| CREATE | `portal/internal/biz/rca_fix_match.go` | 增量刷新改动集 × 近期 RCA 证据 → 修复提交命中 |
| MODIFY | 消息气泡组件（web） | 反馈按钮 |
| CREATE | `web/src/pages/ReposPage.tsx`、`RepoGroupsPage.tsx`、`web/src/api/repos.ts` | 前端 |
| MODIFY | Agent 编辑页组件、`web/src/App.tsx` | 绑定 UI + 路由 |
| CREATE | `evals/tasks/rca_localization.jsonl`、`evals/runner` 指标扩展 | A/B 评测 |

---

## 17. 不包含的内容

- 不做 portal 侧 clone/pull（`managed` 仅预留字段）。
- 不做按部署版本切换 handbook。
- 不做逐函数 actor–critic 生成与多轮 doctor 精修。
- 不把排查策略写进 handbook。
- 不让 handbook 成为绑定或 RCA 的前置条件。
- 不为 framework 本身生成 handbook（另立设计）。

---

## 18. 待决问题

已决：

- ~~业务仓库语言分布~~：约 95% Go → Go 做函数级调用图，非 Go 只做文件级，不引入多语言解析（§7.2）。
- ~~多 root 路径前缀~~：已核实输出带 `repo` 字段，但 basename 重名会静默选错 → P1 改为仓库逻辑名（§6.1.1）。
- ~~RCA 确认信号~~：三档信号组合（§9.2）。

- ~~`agent_asset_bindings` 归属~~：Loadout 仍在推进 → 仓库绑定改用独立表 `agent_repo_bindings`；与 Loadout 的唯一交点是 Skill 保留名（§4.2）。
- ~~企微渠道反馈~~：支持交互卡片 → 最终回复后另发反馈卡片（§9.2.1）。
- ~~修复提交命中的时间窗与粒度~~：上线前历史回放 + 人工标注校准（§9.2.2）。
- ~~handbook 生成模型与预算~~：handbook 生成模型（P2b）从模型目录按 `provider/model` 选择（同 critic 模型解析方式）；预算在 P2b 计划中定。P2a 不使用 LLM。

待决：

1. ~~handbook 生成模型与预算~~（已决，见上）。
2. **组级 handbook 按 agent 裁剪**的渲染成本：agent 数 × 组数较大时是否改为运行时过滤而非预渲染。
3. **Loadout 保留名例外**：需 Loadout 设计 owner 确认在其 §9.1 加入"`code-map` / `handbook-` 前缀不受同名覆盖规则约束"的例外。
4. **回放标注人力**：§9.2.2 每个网格点 50 条，共 16 个网格点约 800 条，需要业务方安排标注人。
