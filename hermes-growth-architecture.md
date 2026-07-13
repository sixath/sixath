# Hermes Agent 的"成长"：一个高度集成的、自动化的系统工程

> 基于源码的架构剖析。所有引用均指向仓库内具体文件与行号。

通读 [run_agent.py](run_agent.py)、[agent/](agent/)、[tools/](tools/)、[cron/](cron/) 与 [environments/](environments/) 之后，可以确认 Hermes 的"自我改进"并不是一个孤立的功能模块，而是把**触发—执行—持久化—回流**四个环节，沿着 agent 主循环逐层缝合进整个系统的工程结构。下面按五个支柱 + 一个横切层逐一拆解。

---

## 支柱一：技能的自创与自演化（Skill Loop）

**触发器嵌在主循环本身**。每次工具迭代和每次对话回合，[run_agent.py](run_agent.py) 都会维护两个计数器；当达到阈值就**在用户感知不到的地方**点燃一次"复盘"。

- 工具迭代计数：[run_agent.py:10751-10753](run_agent.py#L10751-L10753) 每个 LLM 调用后 +1，前提是 `skill_manage` 在 `valid_tool_names` 中。
- 回合阈值检查:[run_agent.py:13783-13788](run_agent.py#L13783-L13788) 在生成最终响应**之后**才置位 `_should_review_skills`,避免把复盘的开销加在用户的等待时间里。
- 默认每 10 个迭代触发一次([run_agent.py:1775-1781](run_agent.py#L1775-L1781)),可由 `skills.creation_nudge_interval` 配置。

**执行：fork 一个"瘦身版自己"做后台复盘**。命中后 [_spawn_background_review](run_agent.py#L3525-L3620) 在守护线程里实例化一个新的 `AIAgent`,仅启用 `["memory","skills"]` 两个 toolset,并把 `_skill_nudge_interval = 0`、`_memory_nudge_interval = 0`([run_agent.py:3590-3598](run_agent.py#L3590-L3598))以杜绝递归触发。它接收完整的 `messages_snapshot` 作为对话历史,按 [_SKILL_REVIEW_PROMPT](run_agent.py#L3330-L3404) 的优先级链路(patch 现有 → 加 reference → 新建 umbrella)改写技能库——并通过提示词显式禁止"PR 编号、错误字符串"这类一次性产物变成技能名。

**长期巩固:Curator**。一次性的复盘只能修补,长期的累积需要**周期性合并**。[agent/curator.py](agent/curator.py) 是一个**按"上次活动时间"门控**的二级清扫器(默认 7 天空闲 ≥2 小时才会启动,会话开始时由 `maybe_run_curator()` 检查),它做两件事:

1. **纯自动化的状态机迁移**([curator.py:266-274](agent/curator.py#L266-L274)):30 天未用 → `stale`,90 天未用 → `archived`(移入 `~/.hermes/skills/.archive/`,永远可恢复),用回了又自动复活。
2. **再 fork 一次 LLM 做"伞形合并"**:把同前缀的散件合并成 class-level umbrella,把兄弟节点降级为 `references/` `templates/` `scripts/`。

最关键的工程细节是——**当技能被合并/删名后,Curator 会反向重写 cron 作业里的技能引用**([curator.py:839-845](agent/curator.py#L839-L845) 调用 [cron.jobs.rewrite_skill_refs](cron/jobs.py)),这意味着定时任务不会因为技能演化而悄悄断链。这是把"成长"和"自动化执行"真正缝在一起的那一针。

---

## 支柱二:记忆的主动沉淀(Memory Nudge)

完全对称于技能 nudge:[run_agent.py:10470-10476](run_agent.py#L10470-L10476) 在每个回合开始时检查 `_turns_since_memory`,命中阈值置位 `_should_review_memory`。如果两个标志同时为真,agent 会用 [_COMBINED_REVIEW_PROMPT](run_agent.py) 让 fork 出去的复盘 agent 在**同一次 API 往返**里同时改写 `MEMORY.md` / `USER.md` 和技能库——这是为了在不破坏 prompt cache 的前提下省钱,也是为什么提示词里专门强调"大多数会话至少应该产出一项更新,nothing-pass 不是中性结果"。

存储抽象通过 [agent/memory_manager.py](agent/memory_manager.py) 实现"单 builtin + 至多一个 external provider"的契约([memory_manager.py:206-228](agent/memory_manager.py#L206-L228) 显式拒绝二次注册)。`MemoryProvider` ABC([agent/memory_provider.py](agent/memory_provider.py))不仅有 `prefetch`/`sync_turn`/`shutdown`,还暴露 `on_session_end`/`on_session_switch`/`on_pre_compress`/`on_memory_write`/`on_delegation` 等**生命周期钩子**——当 builtin 写入 `MEMORY.md`,`on_memory_write` 触发外部 provider(如 Honcho)镜像写入自家后端。换句话说,"长出第二条记忆通路"完全不需要改 core,只需新建一个插件目录。

---

## 支柱三:跨会话召回(FTS5 + LLM 摘要)

[hermes_state.py:103-136](hermes_state.py#L103-L136) 同时建了两张 FTS5 虚表:默认 tokenizer 的 `messages_fts` 与 trigram tokenizer 的 `messages_fts_trigram`(专为中日韩文本服务)。每条消息通过 SQL 触发器自动入库,对 agent 主流程是零侵入的。

[tools/session_search_tool.py](tools/session_search_tool.py) 把"搜索"包装成 agent 可主动调用的工具:FTS5 ranked hits → 沿 `parent_session_id` 链路把压缩分片折叠回根会话 → 排除当前会话 → **并发 LLM 摘要**([session_search_tool.py:451-464](tools/session_search_tool.py#L451-L464) 用 `asyncio.Semaphore` 限流)。工具 schema 的描述明确诱导 agent 在听到"上次"、"as I mentioned"时**自发**调用——召回不是用户的工单,而是 agent 推理链路里的一步。空查询时会跳过 LLM 直接返回最近会话目录,省钱省 latency。

特别值得注意:**cron 跑出的会话也写进同一个 SQLite**(按 `source='cron'` 打标),所以 agent 可以"记得"自己昨晚定时任务跑出来的结论,这是把后台自动化和前台对话拼在同一记忆基底上的关键。

---

## 支柱四:用户建模(Honcho 辩证模型 + USER.md)

builtin 层就是 `USER.md`,由 [_MEMORY_REVIEW_PROMPT](run_agent.py#L3321-L3327) 引导后台复盘 agent 写入两类条目:"用户透露的关于自己的事"与"用户表达的对你工作方式的期望"。

[plugins/memory/honcho/](plugins/memory/honcho/) 把它升级成一个**双 peer 的辩证模型**:每个会话有 user peer 和 AI peer 两个观察者,`observeMe`/`observeOthers` 控制各自从谁身上学。`honcho_reasoning` 工具在后端跑 1-3 轮 dialectic 推理(每轮提升 reasoning level),`per-directory`/`per-repo`/`per-session`/`global` 四种 session 策略决定哪条对话累积进哪个用户表征。多 profile 场景下 (`hermes profile create --clone`),多个 AI peer 共享同一 workspace 但维护独立身份——你可以并行让几个 Hermes 在不同任务上"认识同一个人"再对照其各自的用户模型。

---

## 支柱五:把闭环反哺给下一代模型(Training Data Loop)

[run_agent.py](run_agent.py) 的 `_save_trajectory()`([agent/trajectory.py:16-56](agent/trajectory.py#L16-L56))把每次对话转成 ShareGPT 格式,`<REASONING_SCRATCHPAD>` 自动改写为 `<think>` 标签,按 `completed` 分流写入 `trajectory_samples.jsonl` 或 `failed_trajectories.jsonl`。

[batch_runner.py](batch_runner.py) 用 `multiprocessing.Pool` 大规模复用同一台 `AIAgent`,配合 [toolset_distributions.py](toolset_distributions.py) 的分布采样实现 curriculum diversity;[trajectory_compressor.py](trajectory_compressor.py)(`compress_trajectory` 见 [trajectory_compressor.py:709-827](trajectory_compressor.py#L709-L827))保护首尾,把中段折叠成 `[CONTEXT SUMMARY]:` 单条以适配训练上下文窗口。最终 [environments/](environments/) 下的 Atropos RL envs 把这些轨迹接到强化学习训练管线——**今天的一次对话,就是明天模型权重里的一个梯度**。

---

## 横切层:把上述五条支柱"焊"在一起的自动化骨架

### Cron 调度器:把"自我执行"塞进同一个数据基底

[cron/scheduler.py](cron/scheduler.py) 的 `tick()` 每 60 秒由 gateway 后台线程驱动,**用文件锁**([scheduler.py:1300-1303](cron/scheduler.py#L1300-L1303) 在锁内 `advance_next_run`)实现跨进程 at-most-once。每个 job 启动一个全新 `AIAgent`,关键工程取舍:

- `skip_memory=True`——cron 上下文绝不污染用户表征。
- 用 `agent.get_activity_summary()` 做**活动检测的超时**(默认 600s),不是墙钟超时——agent 可以跑很久,只要它一直在产出。
- pre-check 脚本 (`~/.hermes/scripts/`) 可在最后一行 stdout 返回 `{"wakeAgent": false}` 直接 gate 掉 LLM 调用——把 token 成本控制权交给用户脚本。
- 输出走 `deliver` 多目的地(`telegram:-100123:17` 这种 channel:thread 寻址),且 `[SILENT]` marker 让 agent 自己判断"无事可报就别打扰人"。

### 钩子矩阵:插件不准改 core

[hermes_cli/plugins.py:78-113](hermes_cli/plugins.py#L78-L113) 的 `VALID_HOOKS` 集合定义了 13 个挂钩点(`pre/post_tool_call`、`pre/post_llm_call`、`pre/post_api_request`、`on_session_*`、`subagent_stop` 等),覆盖 agent 整个生命周期。Python 插件和 [agent/shell_hooks.py](agent/shell_hooks.py) 的 shell 脚本钩子走同一 dispatch 路径——后者通过 stdin JSON 收事件,stdout JSON 返回 `{"action": "block", "message": ...}` 即可否决一个工具调用([shell_hooks.py:43-45](agent/shell_hooks.py#L43-L45)),首次使用要写入 `~/.hermes/shell-hooks-allowlist.json` 取得用户许可。这正是 Teknium 在 PR #5295 立下的规矩:**"插件 MUST NOT 修改 core"**,需要新能力就扩展通用 hook 表面,绝不允许把插件特定逻辑硬编码进 [run_agent.py](run_agent.py) / [cli.py](cli.py)。

### Skills Hub:让"成长"流入社区

[tools/skills_hub.py](tools/skills_hub.py) 把 7 种 SkillSource(OptionalSkill、HermesIndex、GitHub、SkillsSh、ClawHub、ClaudeMarketplace、LobeHub、URL、WellKnown)抽象成同一 `search/fetch/install` 接口,进入隔离区(`~/.hermes/skills/.hub/quarantine/`)→ 由 [tools/skills_guard.py](tools/skills_guard.py) 用 `_INJECTION_PATTERNS` 扫 prompt injection → 写 lock.json(content hash + 出处) + audit.log。Curator 的本地合并和 Skills Hub 的对外发布走的是同一种 `SKILL.md` 格式(agentskills.io 兼容),从而**单机的成长可以以 PR/repo 的形态扩散到整个生态**。

---

## 这为什么是"系统工程"

把所有线索拼起来看:

1. **触发面**——nudge 计数器嵌在主循环的迭代/回合,cron tick 嵌在 gateway 后台线程,Curator gating 嵌在会话起点;三类时序信号没有公共调度器,但都被精心放在"恰好 agent 不忙"的窗口里。
2. **执行面**——技能复盘、Curator 合并、cron 任务统统**fork 出一个瘦身的 AIAgent 自身**,复用同一段对话循环代码、同一份 toolset 体系、同一个权限模型,递归保护通过把内层 nudge 设为 0 完成。
3. **持久化面**——所有产出(USER.md、MEMORY.md、SKILL.md、trajectory、cron 会话)都进入同一个 `HERMES_HOME` 目录与同一份 SQLite + FTS5 索引,因此**前台对话与后台自动化共享同一张记忆图**。
4. **演化面**——Curator 修改技能名后反写 cron 作业引用,`on_memory_write` hook 把 builtin 写入镜像到 Honcho,`save_trajectory` 把对话直接喂给 Atropos——任何一处"自我改进"都会被对应的下游通道接住。
5. **扩展面**——profile 隔离 (`get_hermes_home()`)、provider 单实例约束、hook 严禁改 core、Skills Hub 多源安装——保证这套闭环可以在多用户、多模型、多渠道的真实负载下被复制和扩展,而不是退化成一个只在作者笔电上工作的 demo。

简而言之:Hermes 的"成长"不是一段 `if learn(): improve()` 的代码,它是**触发器分散嵌入主流程、执行体复用 agent 自身、持久化共享同一基底、演化通过钩子级联、对外通过 Hub 扩散**——五层独立的工程决策被同一组数据形状(`SKILL.md` / `messages` 表 / SharedGPT trajectory)串成一条闭环。这就是把"自我改进"做成系统工程,而不是做成一个特性。

---

## 关键代码索引

| 关注点 | 位置 |
|---|---|
| 技能 nudge 计数 | [run_agent.py:10751-10753](run_agent.py#L10751-L10753) |
| 技能 nudge 阈值 + 重置 | [run_agent.py:13783-13788](run_agent.py#L13783-L13788) |
| 记忆 nudge 阈值 | [run_agent.py:10470-10476](run_agent.py#L10470-L10476) |
| 后台复盘 fork | [run_agent.py:3525-3620](run_agent.py#L3525-L3620) |
| `_SKILL_REVIEW_PROMPT` | [run_agent.py:3330-3404](run_agent.py#L3330-L3404) |
| Curator 自动状态迁移 | [agent/curator.py:238-278](agent/curator.py#L238-L278) |
| Curator LLM 复盘 fork | [agent/curator.py:1337-1467](agent/curator.py#L1337-L1467) |
| Curator 反写 cron 引用 | [agent/curator.py:839-845](agent/curator.py#L839-L845) |
| `MemoryProvider` 抽象契约 | [agent/memory_provider.py](agent/memory_provider.py) |
| `MemoryManager` 单实例约束 | [agent/memory_manager.py:206-228](agent/memory_manager.py#L206-L228) |
| FTS5 虚表 DDL | [hermes_state.py:103-136](hermes_state.py#L103-L136) |
| `search_messages` FTS5 路由 | [hermes_state.py:1669-1845](hermes_state.py#L1669-L1845) |
| `session_search` 并行摘要 | [tools/session_search_tool.py:451-464](tools/session_search_tool.py#L451-L464) |
| `save_trajectory` 调用点 | [run_agent.py:13652-13657](run_agent.py#L13652-L13657) |
| ShareGPT 格式 + scratchpad→think | [agent/trajectory.py:16-56](agent/trajectory.py#L16-L56) |
| `TrajectoryCompressor.compress_trajectory` | [trajectory_compressor.py:709-827](trajectory_compressor.py#L709-L827) |
| Cron `tick()` 文件锁 + 并行执行 | [cron/scheduler.py:1258-1416](cron/scheduler.py#L1258-L1416) |
| `VALID_HOOKS` 集合 | [hermes_cli/plugins.py:78-113](hermes_cli/plugins.py#L78-L113) |
| Shell hook stdin/stdout 契约 | [agent/shell_hooks.py:30-47](agent/shell_hooks.py#L30-L47) |
| Skills Hub 源注册 | [tools/skills_hub.py:3091-3110](tools/skills_hub.py#L3091-L3110) |
| Hub 隔离区 + lock 路径 | [tools/skills_hub.py:46-53](tools/skills_hub.py#L46-L53) |
