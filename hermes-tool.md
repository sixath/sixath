# Hermes Agent 工具完全参考

> 基于源码抽取。每个工具列出 `schema["description"]`（即模型看到的原文）、关键参数、实现要点、适用场景。涉及 76 个 `registry.register(...)` 调用 + Spotify 插件，覆盖 14 个领域。

## 注册与发现机制

[tools/registry.py](tools/registry.py) 是单例注册表，提供 `ToolEntry` dataclass 与 `discover_builtin_tools()`：扫描 [tools/](tools/) 下所有 `.py` 文件，仅当文件内含**模块级**的 `registry.register(...)` 调用时才动态导入，避免误把内部辅助模块（也调 register 但是在函数内）当成工具来注册。每个 `ToolEntry` 包含：

- `name` / `toolset` —— 工具名与所属工具组
- `schema` —— 给 LLM 的 JSON schema（即下文每节引用的 description）
- `handler` —— 实际执行函数
- `check_fn` —— 运行时门控（环境变量 / 平台 / 后端可达性）
- `requires_env` —— 提示性必需 env 列表
- `is_async` —— 是否在 `await` 路径中调度
- `max_result_size_chars` —— 输出截断阈值

工具组的组装与按平台分发由 [toolsets.py](toolsets.py) 完成（`_HERMES_CORE_TOOLS` + `TOOLSETS` 字典），见 [hermes-growth-architecture.md](hermes-growth-architecture.md) 中"工具分发"一节。

---

# 一、Agent 元工具（成长闭环之核）

这一组是支撑技能/记忆/召回闭环的"自我大脑"工具，几乎所有平台默认开启。

## `memory` — 持久化记忆

- **toolset**: `memory` · **门控**: 始终可用 · **文件**: [tools/memory_tool.py](tools/memory_tool.py)

```
Save durable information to persistent memory that survives across sessions. Memory is injected into future turns, so keep it compact and focused on facts that will still matter later.

WHEN TO SAVE (do this proactively, don't wait to be asked):
- User corrects you or says 'remember this' / 'don't do that again'
- User shares a preference, habit, or personal detail (name, role, timezone, coding style)
- You discover something about the environment (OS, installed tools, project structure)
- You learn a convention, API quirk, or workflow specific to this user's setup
- You identify a stable fact that will be useful again in future sessions

PRIORITY: User preferences and corrections > environment facts > procedural knowledge.

Do NOT save task progress, session outcomes, completed-work logs, or temporary TODO state to memory; use session_search to recall those from past transcripts. If you've discovered a new way to do something, solved a problem that could be necessary later, save it as a skill with the skill tool.

TWO TARGETS:
- 'user': who the user is -- name, role, preferences, communication style, pet peeves
- 'memory': your notes -- environment facts, project conventions, tool quirks, lessons learned

ACTIONS: add (new entry), replace (update existing -- old_text identifies it), remove (delete -- old_text identifies it).

SKIP: trivial/obvious info, things easily re-discovered, raw data dumps, and temporary task state.
```

- **参数**：`action` ∈ {add, replace, remove}；`target` ∈ {memory, user}；`content`（add/replace 必需）；`old_text`（replace/remove 必需，定位用的子串）。
- **实现**：写入 `MEMORY.md` / `USER.md`，临时文件 + 原子 rename，跨平台文件锁（POSIX 用 fcntl，Windows 用 msvcrt），写前过 prompt-injection 扫描，每文件容量上限可配。
- **适用**：用户纠正 / 偏好 / 环境事实——主动写，避免下次重提。任务进度类不要写。

## `todo` — 当前会话任务表

- **toolset**: `todo` · **门控**: 始终可用 · **文件**: [tools/todo_tool.py](tools/todo_tool.py)

```
Manage your task list for the current session. Use for complex tasks with 3+ steps or when the user provides multiple tasks. Call with no parameters to read the current list.

Writing:
- Provide 'todos' array to create/update items
- merge=false (default): replace the entire list with a fresh plan
- merge=true: update existing items by id, add any new ones

Each item: {id: string, content: string, status: pending|in_progress|completed|cancelled}
List order is priority. Only ONE item in_progress at a time.
Mark items completed immediately when done. If something fails, cancel it and add a revised item.

Always returns the full current list.
```

- **参数**：`todos`（对象数组，每项有 `id`/`content`/`status`）；`merge`（默认 false，整体替换；true 则按 id 合并）。
- **实现**：内存型 `TodoStore`，按 id 去重（后写胜出），上下文压缩后 `format_for_injection()` 仅注入 pending/in_progress 项。
- **适用**：≥3 步的多步任务规划与状态跟踪；不持久化，仅本会话有效。

## `session_search` — 跨会话召回

- **toolset**: `session_search` · **门控**: 状态 DB 父目录存在 · **文件**: [tools/session_search_tool.py](tools/session_search_tool.py)

```
Search your long-term memory of past conversations, or browse recent sessions. This is your recall -- every past session is searchable, and this tool summarizes what happened.

TWO MODES:
1. Recent sessions (no query): Call with no arguments to see what was worked on recently. Returns titles, previews, and timestamps. Zero LLM cost, instant.
2. Keyword search (with query): Search for specific topics across all past sessions. Returns LLM-generated summaries of matching sessions.

USE THIS PROACTIVELY when:
- The user says 'we did this before', 'remember when', 'last time', 'as I mentioned'
- The user asks about a topic you worked on before but don't have in current context
- The user references a project, person, or concept that seems familiar but isn't in memory
- You want to check if you've solved a similar problem before
- The user asks 'what did we do about X?' or 'how did we fix Y?'

Search syntax: keywords joined with OR for broad recall (elevenlabs OR baseten OR funding), phrases for exact match ("docker networking"), boolean (python NOT java), prefix (deploy*). IMPORTANT: Use OR between keywords for best results — FTS5 defaults to AND which misses sessions that only mention some terms.
```

- **参数**：`query`（省略则浏览最近会话）；`role_filter`（如 `"user,assistant"`）；`limit`（默认 3，最多 5）。
- **实现**：FTS5（CJK 走 trigram tokenizer）→ 沿 `parent_session_id` 折叠到根会话 → 排除当前会话 → 对每个命中会话截 ~100K 字符（围绕 match 位置）→ 通过 `asyncio.Semaphore` 限流并发调用辅助 LLM (Gemini Flash) 生成摘要。空查询直接返回元数据，零 LLM 成本。
- **适用**：用户说"上次/之前/记得吗"时主动调用，胜过让用户重述。

## `skills_list` / `skill_view` — 浏览与加载技能

- **toolset**: `skills` · **门控**: 始终可用 · **文件**: [tools/skills_tool.py](tools/skills_tool.py)

`skills_list`：

```
List available skills (name + description). Use skill_view(name) to load full content.
```

`skill_view`：

```
Skills allow for loading information about specific tasks and workflows, as well as scripts and templates. Load a skill's full content or access its linked files (references, templates, scripts). First call returns SKILL.md content plus a 'linked_files' dict showing available references/templates/scripts. To access those, call again with file_path parameter.
```

- **参数**：`skills_list` 接 `category` 过滤；`skill_view` 接 `name` (支持 `plugin:skill`) 与可选 `file_path`（取 `references/`、`templates/`、`scripts/` 子文件）。
- **实现**：`skill_view` 实际走 `_skill_view_with_bump` 包装——成功后调 `bump_view`/`bump_use` 写技能使用计数（这正是 [agent/curator.py](agent/curator.py) 自动 stale/archive 决策的输入）。加载内容会过 prompt-injection 扫描，并按 frontmatter 把声明的 env 注入会话。
- **适用**：开始任务前先 `skills_list` 找匹配，再 `skill_view` 读完整指引。

## `skill_manage` — 技能 CRUD

- **toolset**: `skills` · **门控**: 默认开启 · **文件**: [tools/skill_manager_tool.py](tools/skill_manager_tool.py)

```
Manage skills (create, update, delete). Skills are your procedural memory — reusable approaches for recurring task types. New skills go to {hermes_home}/skills/; existing skills can be modified wherever they live.

Actions: create (full SKILL.md + optional category), patch (old_string/new_string — preferred for fixes), edit (full SKILL.md rewrite — major overhauls only), delete, write_file, remove_file.

Create when: complex task succeeded (5+ calls), errors overcome, user-corrected approach worked, non-trivial workflow discovered, or user asks you to remember a procedure.
Update when: instructions stale/wrong, OS-specific failures, missing steps or pitfalls found during use. If you used a skill and hit issues not covered by it, patch it immediately.

After difficult/iterative tasks, offer to save as a skill. Skip for simple one-offs. Confirm with user before creating/deleting.

Good skills: trigger conditions, numbered steps with exact commands, pitfalls section, verification steps.

Pinned skills are off-limits — all write actions refuse with a message pointing the user to `hermes curator unpin <name>`.
```

- **参数**：`action` ∈ {create, patch, edit, delete, write_file, remove_file}；`name`（必需）；`content`（create/edit）；`old_string`/`new_string`/`replace_all`（patch）；`category`（create）；`file_path`/`file_content`（write_file）。
- **实现**：pinned skill 拒写；所有写都临时文件 + rename；新增/修改内容跑 `_security_scan_skill`；create/delete/edit 触发 Curator 遥测（间接驱动后续技能合并）。
- **适用**：复杂任务成功后 codify 经验；技能用过发现遗漏立刻 patch。

## `clarify` — 主动向用户提问

- **toolset**: `clarify` · **门控**: 始终可用（但必须有 callback 注入） · **文件**: [tools/clarify_tool.py](tools/clarify_tool.py)

```
Ask the user a question when you need clarification, feedback, or a decision before proceeding. Supports two modes:

1. **Multiple choice** — provide up to 4 choices. The user picks one or types their own answer via a 5th 'Other' option.
2. **Open-ended** — omit choices entirely. The user types a free-form response.

Use this tool when:
- The task is ambiguous and you need the user to choose an approach
- You want post-task feedback ('How did that work out?')
- You want to offer to save a skill or update memory
- A decision has meaningful trade-offs the user should weigh in on

Do NOT use this tool for simple yes/no confirmation of dangerous commands (the terminal tool handles that). Prefer making a reasonable default choice yourself when the decision is low-stakes.
```

- **参数**：`question`（必需）；`choices`（≤4，省略即开放题）。
- **实现**：完全委托平台 callback——CLI 渲染箭头键选项菜单，messaging 平台渲染编号列表。子 agent 不能用（[delegate_tool.py](tools/delegate_tool.py) 屏蔽）。
- **适用**：两条同样合理的路径让用户拍板；任务后请求反馈。

---

# 二、编排与并行

## `delegate_task` — 派生子 agent

- **toolset**: `delegation` · **门控**: 始终可用 · **文件**: [tools/delegate_tool.py](tools/delegate_tool.py)

```
Spawn one or more subagents to work on tasks in isolated contexts. Each subagent gets its own conversation, terminal session, and toolset. Only the final summary is returned -- intermediate tool results never enter your context window.

TWO MODES (one of 'goal' or 'tasks' is required):
1. Single task: provide 'goal' (+ optional context, toolsets)
2. Batch (parallel): provide 'tasks' array with up to delegation.max_concurrent_children items (default 3, configurable via config.yaml, no hard ceiling). All run concurrently and results are returned together. Nested delegation requires role='orchestrator' and delegation.max_spawn_depth >= 2.

WHEN TO USE delegate_task:
- Reasoning-heavy subtasks (debugging, code review, research synthesis)
- Tasks that would flood your context with intermediate data
- Parallel independent workstreams (research A and B simultaneously)

WHEN NOT TO USE (use these instead):
- Mechanical multi-step work with no reasoning needed -> use execute_code
- Single tool call -> just call the tool directly
- Tasks needing user interaction -> subagents cannot use clarify
- Durable long-running work that must outlive the current turn -> use cronjob (action='create') or terminal(background=True, notify_on_complete=True)

IMPORTANT:
- Subagents have NO memory of your conversation. Pass all relevant info via the 'context' field.
- If user wrote in non-English, say so in 'context' or summaries default to English.
- Subagent summaries are SELF-REPORTS, not verified facts. Verify external side-effects yourself.
- Leaf subagents CANNOT call: delegate_task, clarify, memory, send_message, execute_code.
- Orchestrator subagents retain delegate_task; bounded by delegation.max_spawn_depth.
- Each subagent gets its own terminal session.
```

- **参数**：`goal` 或 `tasks[]`（互斥）；`context`、`toolsets`、`max_iterations`、`role` ∈ {leaf, orchestrator}、`acp_command`/`acp_args`（接 Claude Code 等 ACP 客户端）。
- **实现**：`ThreadPoolExecutor`（默认并发 3，配置 `delegation.max_concurrent_children`）跑子 `AIAgent`；自动屏蔽 `DELEGATE_BLOCKED_TOOLS`；非交互审批 callback 防 TUI 死锁；心跳监测 + `child_timeout_seconds`（默认 600s）。
- **适用**：把"会大量调用工具但只需返回最终结论"的子任务隔离掉，避免把 50 步的中间产物带回主上下文。

## `execute_code` — 在沙盒里跑 Python 调工具

- **toolset**: `code_execution` · **门控**: POSIX + 配置后端检查 · **文件**: [tools/code_execution_tool.py](tools/code_execution_tool.py)

```
Run a Python script that can call Hermes tools programmatically. Use this when you need 3+ tool calls with processing logic between them, need to filter/reduce large tool outputs before they enter your context, need conditional branching (if X then Y else Z), or need to loop (fetch N pages, process N files, retry on failure).

Use normal tool calls instead when: single tool call with no processing, you need to see the full result and apply complex reasoning, or the task requires interactive user input.

Available via `from hermes_tools import ...`: {tool_lines}

Limits: 5-minute timeout, 50KB stdout cap, max 50 tool calls per script. terminal() is foreground-only (no background or pty).

Also available (no import needed):
  json_parse(text: str) — json.loads with strict=False
  shell_quote(s: str) — shlex.quote()
  retry(fn, max_attempts=3, delay=2) — exponential backoff
```

- **参数**：`code`（Python 源码字符串，最终 `print()` 输出）。
- **实现**：subprocess 跑用户代码，注入合成 `hermes_tools` 模块 → 通过 Unix domain socket（本地）或文件 RPC（远程）回调主 agent；白名单：web_search、web_extract、read_file、write_file、search_files、patch、terminal；硬限：300s / 50KB stdout / 50 调用。
- **适用**：把 N 步机械流水线（如"读 50 个文件 → 过滤 → 调 web_search → 汇总"）压成 1 个 turn，省 round-trip 与上下文。

## `mixture_of_agents` — 多模型协同

- **toolset**: `moa` · **门控**: `OPENROUTER_API_KEY` · **文件**: [tools/mixture_of_agents_tool.py](tools/mixture_of_agents_tool.py)

```
Route a hard problem through multiple frontier LLMs collaboratively. Makes 5 API calls (4 reference models + 1 aggregator) with maximum reasoning effort — use sparingly for genuinely difficult problems. Best for: complex math, advanced algorithms, multi-step analytical reasoning, problems benefiting from diverse perspectives.
```

- **参数**：`user_prompt`。
- **实现**：并行调 4 个参考模型（claude-opus-4.6、gemini-2.5-pro、gpt-5.4-pro、deepseek-v3.2）max reasoning，再用 claude-opus-4.6 聚合。
- **适用**：高难数学/算法/分析题，单模型见解不足时；用一次 ≈ 5 次 API 成本。

---

# 三、文件与终端

## `read_file` / `write_file` / `patch` / `search_files`

- **toolset**: `file` · **门控**: 文件后端可达 · **文件**: [tools/file_tools.py](tools/file_tools.py)

`read_file`：

```
Read a text file with line numbers and pagination. Use this instead of cat/head/tail in terminal. Output format: 'LINE_NUM|CONTENT'. Suggests similar filenames if not found. Use offset and limit for large files. Reads exceeding ~100K characters are rejected; use offset and limit to read specific sections of large files. NOTE: Cannot read images or binary files — use vision_analyze for images.
```

参数：`path` / `offset`（默认 1）/ `limit`（默认 500，上限 2000）。无硬截断。

`write_file`：

```
Write content to a file, completely replacing existing content. Use this instead of echo/cat heredoc in terminal. Creates parent directories automatically. OVERWRITES the entire file — use 'patch' for targeted edits.
```

参数：`path` / `content`。自动建父目录；更新跨 agent 的 `file_state` 去重表。

`patch`：

```
Targeted find-and-replace edits in files. Use this instead of sed/awk in terminal. Uses fuzzy matching (9 strategies) so minor whitespace/indentation differences won't break it. Returns a unified diff. Auto-runs syntax checks after editing.

Replace mode (default): find a unique string and replace it.
Patch mode: apply V4A multi-file patches for bulk changes.
```

参数：`mode` ∈ {replace, patch}；replace 用 `path`/`old_string`/`new_string`/`replace_all`；patch 用 `patch`（V4A 格式）。九种模糊匹配策略容忍空白/缩进差异；编辑后自动跑语法检查。

`search_files`：

```
Search file contents or find files by name. Use this instead of grep/rg/find/ls in terminal. Ripgrep-backed, faster than shell equivalents.

Content search (target='content'): Regex search inside files. Output modes: full matches with line numbers, file paths only, or match counts.

File search (target='files'): Find files by glob pattern (e.g., '*.py', '*config*'). Also use this instead of ls — results sorted by modification time.
```

参数：`pattern` / `target` ∈ {content, files} / `path` / `file_glob` / `output_mode` / `context` / `limit` / `offset`。底层 ripgrep。

- **适用**：所有"打开-改-搜"路径都应走这四件套，不要去 terminal 里 cat/grep/sed/find。

## `terminal` — Shell 执行

- **toolset**: `terminal` · **门控**: 选定后端可用 · **文件**: [tools/terminal_tool.py](tools/terminal_tool.py)

```
Execute shell commands on a Linux environment. Filesystem usually persists between calls.

Do NOT use cat/head/tail to read files — use read_file instead.
Do NOT use grep/rg/find to search — use search_files instead.
Do NOT use ls to list directories — use search_files(target='files') instead.
Do NOT use sed/awk to edit files — use patch instead.
Do NOT use echo/cat heredoc to create files — use write_file instead.
Reserve terminal for: builds, installs, git, processes, scripts, network, package managers, and anything that needs a shell.

Foreground (default): Commands return INSTANTLY when done, even if the timeout is high. Set timeout=300 for long builds/scripts — you'll still get the result in seconds if it's fast.
Background: Set background=true to get a session_id. Two patterns:
  (1) Long-lived processes that never exit (servers, watchers).
  (2) Long-running tasks with notify_on_complete=true — keep working on other things; system auto-notifies when done.
For servers/watchers, do NOT use shell-level background wrappers (nohup/disown/setsid/trailing '&') in foreground mode.
After starting a server, verify readiness with a health check or log signal, then run tests in a separate terminal() call.
Use process(action="poll") for progress checks, process(action="wait") to block until done.
PTY mode: Set pty=true for interactive CLI tools (Codex, Claude Code, Python REPL).

Do NOT use vim/nano/interactive tools without pty=true — they hang without a pseudo-terminal.
```

- **参数**：`command`、`background`、`timeout`、`workdir`、`pty`、`notify_on_complete`、`watch_patterns`。
- **实现**：6 后端切换（local / docker / ssh / modal / daytona / vercel sandbox）；前台 3 次重试瞬态故障；输出截断（head+tail）+ ANSI 剥离 + 密钥脱敏；后台委托 `process_registry`，模式监听限速 15s/次 + 三振出局熔断。
- **适用**：构建、git、包管理、网络、跑脚本——一切真正需要 shell 的事。

## `process` — 后台进程管理

- **toolset**: `terminal` · **门控**: 与 terminal 同后端 · **文件**: [tools/process_registry.py](tools/process_registry.py)

```
Manage background processes started with terminal(background=true). Actions: 'list' (show all), 'poll' (check status + new output), 'log' (full output with pagination), 'wait' (block until done or timeout), 'kill' (terminate), 'write' (send raw stdin data without newline), 'submit' (send data + Enter, for answering prompts), 'close' (close stdin/send EOF).
```

- **参数**：`action` ∈ {list, poll, log, wait, kill, write, submit, close}；`session_id`（除 list 外必需）；`data`/`timeout`/`offset`/`limit` 视 action 而定。
- **实现**：`ProcessRegistry` 单例，`_running`/`_finished` 字典 + JSON 检查点崩溃恢复；watch_patterns 15s 冷却 + 三振熔断 + 全局熔断。
- **适用**：长测试 / 服务器 / 监视器的状态轮询、日志读取、stdin 喂入（如对 `apt install` 的 yes/no）。

---

# 四、Web

## `web_search` — 搜索

- **toolset**: `web` · **门控**: `EXA_API_KEY` / `PARALLEL_API_KEY` / `TAVILY_API_KEY` / `FIRECRAWL_API_KEY` 任一或托管网关 · **文件**: [tools/web_tools.py](tools/web_tools.py)

```
Search the web for information. Returns up to 5 results by default with titles, URLs, and descriptions. The query is passed through to the configured backend, so operators such as site:domain, filetype:pdf, intitle:word, -term, and "exact phrase" may work when the backend supports them.
```

- **参数**：`query`、`limit`（1–100，默认 5）。
- **实现**：分发到 firecrawl / exa / parallel / tavily / 托管 firecrawl 网关之一。
- **适用**：现时事实查询、查文档、找 URL 喂给 web_extract 或 browser。

## `web_extract` — 页面/PDF 提取

- **toolset**: `web` · **门控**: 同 web_search · **is_async**: true

```
Extract content from web page URLs. Returns page content in markdown format. Also works with PDF URLs (arxiv papers, documents, etc.) — pass the PDF link directly and it converts to markdown text. Pages under 5000 chars return full markdown; larger pages are LLM-summarized and capped at ~5000 chars per page. Pages over 2M chars are refused. If a URL fails or times out, use the browser tool to access it instead.
```

- **参数**：`urls`（≤5）。
- **实现**：SSRF 防护（内网 IP 黑名单）；<5K 直接返回；>5K 走 Gemini Flash 摘要；最多 5 URL 并发；arxiv PDF 直转 markdown。
- **适用**：search 结果里挑 2–3 条精读；PDF 论文一键 markdown。

---

# 五、浏览器自动化

## 共享设施

[tools/browser_tool.py](tools/browser_tool.py) + [tools/browser_supervisor.py](tools/browser_supervisor.py)：每个 task_id 一个会话，后端依次降级 BrowserUse → Browserbase → 本地 headless Chromium，可被 `BROWSER_CDP_URL` env 或 `browser.cdp_url` config 覆盖到用户自带 Chrome。所有调用走 `agent-browser` CLI 子进程，stdout/stderr 写入 per-session 临时文件以避免 daemon 继承管道。CDP supervisor 维护 frame-tree 与对话框捕获，`browser_navigate` 时按需启动。**所有 10 个工具的 `check_fn` 都是 `check_browser_requirements`**。

## `browser_navigate`

```
Navigate to a URL in the browser. Initializes the session and loads the page. Must be called before other browser tools. For simple information retrieval, prefer web_search or web_extract (faster, cheaper). For plain-text endpoints — URLs ending in .md, .txt, .json, .yaml, .yml, .csv, .xml, raw.githubusercontent.com, or any documented API endpoint — prefer curl via the terminal tool or web_extract; the browser stack is overkill and much slower for these. Use browser tools when you need to interact with a page (click, fill forms, dynamic content). Returns a compact page snapshot with interactive elements and ref IDs — no need to call browser_snapshot separately after navigating.
```

参数 `url`。SSRF 检查云后端的私网 IP；导航成功立刻附返一份 compact 快照。**适用**：交互必需的第一步——表单、登录、动态内容。

## `browser_snapshot`

```
Get a text-based snapshot of the current page's accessibility tree. Returns interactive elements with ref IDs (like @e1, @e2) for browser_click and browser_type. full=false (default): compact view with interactive elements. full=true: complete page content. Snapshots over 8000 chars are truncated or LLM-summarized. Requires browser_navigate first. Note: browser_navigate already returns a compact snapshot — use this to refresh after interactions that change the page, or with full=true for complete content.
```

参数 `full`（默认 false）。>8K 字符走 Gemini Flash 摘要；CDP 在线时附 `pending_dialogs` 与 `frame_tree`。**适用**：点击/输入后页面变了，刷新视图。

## `browser_click`

```
Click on an element identified by its ref ID from the snapshot (e.g., '@e5'). The ref IDs are shown in square brackets in the snapshot output. Requires browser_navigate and browser_snapshot to be called first.
```

参数 `ref`。**适用**：按钮/链接/复选框点击。

## `browser_type`

```
Type text into an input field identified by its ref ID. Clears the field first, then types the new text. Requires browser_navigate and browser_snapshot to be called first.
```

参数 `ref` / `text`。先清后输。**适用**：搜索框、登录表单。

## `browser_scroll`

```
Scroll the page in a direction. Use this to reveal more content that may be below or above the current viewport. Requires browser_navigate to be called first.
```

参数 `direction` ∈ {up, down}。**适用**：露出 fold 之下的内容、无限滚动。

## `browser_back`

```
Navigate back to the previous page in browser history. Requires browser_navigate to be called first.
```

无参数。**适用**：误点跳走后回退。

## `browser_press`

```
Press a keyboard key. Useful for submitting forms (Enter), navigating (Tab), or keyboard shortcuts. Requires browser_navigate to be called first.
```

参数 `key`（如 Enter / Tab / Escape / ArrowDown）。**适用**：提交表单、关 modal、键盘导航 dropdown。

## `browser_get_images`

```
Get a list of all images on the current page with their URLs and alt text. Useful for finding images to analyze with the vision tool. Requires browser_navigate to be called first.
```

无参数。**适用**：枚举 `<img>` 后挑感兴趣的喂 `vision_analyze`。

## `browser_vision`

```
Take a screenshot of the current page and analyze it with vision AI. Use this when you need to visually understand what's on the page - especially useful for CAPTCHAs, visual verification challenges, complex layouts, or when the text snapshot doesn't capture important visual information. Returns both the AI analysis and a screenshot_path that you can share with the user by including MEDIA:<screenshot_path> in your response. Requires browser_navigate to be called first.
```

参数 `question`、`annotate`（true 时在交互元素上叠 `[N]` 编号映射 `@eN`）。截图 + `vision_analyze_tool`。**适用**：CAPTCHA、视觉验证、复杂布局核对。

## `browser_console`

```
Get browser console output and JavaScript errors from the current page. Returns console.log/warn/error/info messages and uncaught JS exceptions. Use this to detect silent JavaScript errors, failed API calls, and application warnings. Requires browser_navigate to be called first. When 'expression' is provided, evaluates JavaScript in the page context and returns the result — use this for DOM inspection, reading page state, or extracting data programmatically.
```

参数 `clear`（读后清空）、`expression`（在页面上下文 eval 任意 JS）。**适用**：调 web app 的静默 JS 错误；从 window 全局对象提数据。

---

# 六、CDP 进阶（仅 CDP 后端可用）

## `browser_cdp` — 原生 CDP 命令

- **toolset**: `browser-cdp` · **门控**: `_get_cdp_override()` 非空 + browser 通用门控 · **文件**: [tools/browser_cdp_tool.py](tools/browser_cdp_tool.py)

```
Send a raw Chrome DevTools Protocol (CDP) command. Escape hatch for browser operations not covered by browser_navigate, browser_click, browser_console, etc.

**Requires a reachable CDP endpoint.** Available when the user has run '/browser connect' to attach to a running Chrome, or when 'browser.cdp_url' is set in config.yaml. Not currently wired up for cloud backends (Browserbase, Browser Use, Firecrawl).

**CDP method reference:** https://chromedevtools.github.io/devtools-protocol/

**Common patterns:**
- List tabs: method='Target.getTargets'
- Handle a native JS dialog: method='Page.handleJavaScriptDialog'
- Get all cookies: method='Network.getAllCookies'
- Eval in a specific tab: method='Runtime.evaluate', target_id=<tabId>
- Set viewport: method='Emulation.setDeviceMetricsOverride', target_id=<tabId>

**Usage rules:**
- Browser-level methods (Target.*, Browser.*, Storage.*): omit target_id and frame_id.
- Page-level methods (Page.*, Runtime.*, DOM.*, Emulation.*, Network.* scoped to a tab): pass target_id from Target.getTargets.
- **Cross-origin iframe scope**: pass frame_id from browser_snapshot frame_tree output. Routes through the CDP supervisor's live connection — the only reliable way on Browserbase where stateless CDP calls hit signed-URL expiry.
```

- **参数**：`method`、`params`、`target_id`、`frame_id`、`timeout`（≤300）。
- **实现**：双路由——`frame_id` 给 supervisor 的现存 WebSocket session；否则新开 WS、可选 `Target.attachToTarget` 后发命令。
- **适用**：原生 JS 对话框、cookies、跨域 iframe 内 eval、低层 tab 管理。

## `browser_dialog`

- **toolset**: `browser-cdp` · **门控**: 同 `browser_cdp` · **文件**: [tools/browser_dialog_tool.py](tools/browser_dialog_tool.py)

```
Respond to a native JavaScript dialog (alert / confirm / prompt / beforeunload) that is currently blocking the page.

**Workflow:** call ``browser_snapshot`` first — if a dialog is open, it appears in the ``pending_dialogs`` field with ``id``, ``type``, and ``message``. Then call this tool with ``action='accept'`` or ``action='dismiss'``.

**Prompt dialogs:** pass ``prompt_text`` to supply the response string.

**Multiple dialogs:** if more than one dialog is queued, pass ``dialog_id`` from the snapshot to disambiguate.

**Availability:** only present when a CDP-capable backend is attached.
```

- **参数**：`action` ∈ {accept, dismiss}；`prompt_text`；`dialog_id`。
- **实现**：通过 supervisor 的活跃 CDP WebSocket 发 `Page.handleJavaScriptDialog`。
- **适用**：alert/confirm/prompt 阻塞页面时单击 OK/Cancel。

---

# 七、多模态

## `vision_analyze`

- **toolset**: `vision` · **门控**: 视觉 LLM client 可解析 · **文件**: [tools/vision_tools.py](tools/vision_tools.py)

```
Inspect an image from a URL, file path, or tool output when you need closer detail than what's visible in the conversation. If the user's image is already attached to the conversation and you can see it, just answer directly — only call this tool for images referenced by URL/path, images returned inside other tool results (browser screenshots, search thumbnails), or when you need a deeper look at a specific region the main model's vision may have missed.
```

- **参数**：`image_url`（URL 或路径）、`question`。
- **实现**：URL 下载到 `$HERMES_HOME/cache/vision/`（带 SSRF 防护），magic bytes 验 MIME，>20MB Pillow 缩；base64 data URL → `async_call_llm(task="vision")` 走配置的视觉 provider。
- **适用**：分析 `browser_vision` 截图、URL 图片、tool 返回里的图片。

## `image_generate`

- **toolset**: `image_gen` · **门控**: `FAL_KEY` 或托管 Nous 网关 · **文件**: [tools/image_generation_tool.py](tools/image_generation_tool.py)

```
Generate high-quality images from text prompts. The underlying backend (FAL, OpenAI, etc.) and model are user-configured and not selectable by the agent. Returns either a URL or an absolute file path in the `image` field; display it with markdown ![description](url-or-path) and the gateway will deliver it.
```

- **参数**：`prompt`、`aspect_ratio` ∈ {landscape, portrait, square}。
- **实现**：默认模型 `fal-ai/flux-2/klein/9b`；`_build_fal_payload` 把 aspect_ratio 翻成模型私有 size；可链式 `flux-2-pro` → `clarity-upscaler`；插件 provider（如 OpenAI）由 `image_gen.provider` 切换。
- **适用**：插画/概念图/Mockup 按文本生成。

## `text_to_speech`

- **toolset**: `tts` · **门控**: 任一 provider 可用 · **文件**: [tools/tts_tool.py](tools/tts_tool.py)

```
Convert text to speech audio. Returns a MEDIA: path that the platform delivers as native audio. Compatible providers render as a voice bubble on Telegram; otherwise audio is sent as a regular attachment. In CLI mode, saves to ~/voice-memos/. Voice and provider are user-configured (built-in providers like edge/openai or custom command providers under tts.providers.<name>), not model-selected.
```

- **参数**：`text`、`output_path`。
- **实现**：10 后端轮选——edge-tts（默认免费）、ElevenLabs、OpenAI、MiniMax、xAI、Mistral、Gemini、本地 NeuTTS、KittenTTS、Piper，外加用户 shell-command provider；Telegram 会话试 ffmpeg 转 Opus 拿 voice-bubble 渲染；返 `MEDIA:<path>` token 让 gateway 派发。
- **适用**：Telegram/Discord 上的语音回复；长回复转语音备忘。

---

# 八、调度与多 Agent

## `cronjob` — 定时任务

- **toolset**: `cronjob` · **门控**: gateway / 交互 session / `HERMES_EXEC_ASK` env 任一 · **文件**: [tools/cronjob_tools.py](tools/cronjob_tools.py)

```
Manage scheduled cron jobs with a single compressed tool.

Use action='create' to schedule a new job from a prompt or one or more skills.
Use action='list' to inspect jobs.
Use action='update', 'pause', 'resume', 'remove', or 'run' to manage an existing job.

To stop a job the user no longer wants: first action='list' to find the job_id, then action='remove' with that job_id. Never guess job IDs — always list first.

Jobs run in a fresh session with no current-chat context, so prompts must be self-contained.
If skills are provided on create, the future cron run loads those skills in order, then follows the prompt as the task instruction.
On update, passing skills=[] clears attached skills.

NOTE: The agent's final response is auto-delivered to the target. Put the primary user-facing content in the final response. Cron jobs run autonomously with no user present — they cannot ask questions or request clarification.

Important safety rule: cron-run sessions should not recursively schedule more cron jobs.
```

- **参数**：`action` + `job_id` + `prompt` + `schedule`（"30m" / "every 2h" / cron / ISO）+ `name` + `repeat` + `deliver`（多目的，可 `platform:chat:thread`）+ `skills` + `model` + `script`（前置 hook 路径）+ `context_from`（其他 job 输出注入）+ `enabled_toolsets` + `workdir`。
- **实现**：prompt 过 injection 扫描；`script` 路径必须在 `$HERMES_HOME/scripts/` 内；委托 [cron/jobs.py](cron/jobs.py)。
- **适用**：日报、夜间备份、定期审计、N 小时后提醒——一切非交互的周期/一次性自动化。

## Kanban（7 个）—— 多 worker 协调

[tools/kanban_tools.py](tools/kanban_tools.py)。**全 7 个工具的门控都是 `HERMES_KANBAN_TASK` env 存在**；普通 chat 看不到。背后是 `~/.hermes/kanban.db`，`task_id` 默认从 env 取。

### `kanban_show`

```
Read a task's full state — title, body, assignee, parent task handoffs, your prior attempts on this task if any, comments, and recent events. Use this to (re)orient yourself before starting work, especially on retries. The response includes a pre-formatted ``worker_context`` string suitable for inclusion verbatim in your reasoning.
```

参数 `task_id`（默认 env）。汇总 task / comments / 最近 50 events / runs / parents / children + 预格式化 `worker_context`。**适用**：worker 启动第一步重新定向。

### `kanban_complete`

```
Mark your current task done with a structured handoff for downstream workers and humans. Prefer ``summary`` for a human-readable 1-3 sentence description of what you did; put machine-readable facts in ``metadata`` (changed_files, tests_run, decisions, findings, etc). At least one of ``summary`` or ``result`` is required.
```

参数 `task_id` / `summary` / `metadata` / `result`。状态置 done，触发依赖此任务的子任务自动 promote。**适用**：worker 收尾。

### `kanban_block`

```
Transition the task to blocked because you need human input to proceed. ``reason`` will be shown to the human on the board and included in context when someone unblocks you. Use for genuine blockers only — don't block on things you can resolve yourself.
```

参数 `reason`。状态置 blocked，dashboard 通知人。**适用**：缺凭证、需求模糊等真阻塞。

### `kanban_heartbeat`

```
Signal that you're still alive during a long operation (training, encoding, large crawls). Call every few minutes so humans see liveness separately from PID checks. Pure side effect — no work changes.
```

参数 `note`。仅写 event log，不改状态。**适用**：长跑期间向 dashboard 表明存活。

### `kanban_comment`

```
Append a comment to a task's thread. Use for durable notes that should outlive this run (questions for the next worker, partial findings, rationale). Ephemeral reasoning doesn't belong here — use your normal response instead.
```

参数 `task_id` / `body` / `author`。**适用**：留给下个 worker 的问题、部分发现、决策依据。

### `kanban_create`

```
Create a new kanban task, optionally as a child of the current one (pass the current task id in ``parents``). Used by orchestrator workers to fan out — decompose work into child tasks with specific assignees, link them into a pipeline, then complete your own task. The dispatcher picks up the new tasks on its next tick and spawns the assigned profiles.
```

参数 `title` / `assignee` / `body` / `parents` / `tenant` / `priority` / `workspace_kind` ∈ {scratch, dir, worktree} / `workspace_path` / `triage` / `idempotency_key` / `max_runtime_seconds` / `skills`。**适用**：orchestrator 把大任务拆给一组 specialist profile 并接成 fan-out/fan-in 管线。

### `kanban_link`

```
Add a parent→child dependency edge after both tasks already exist. The child won't promote to 'ready' until all parents are 'done'. Cycles and self-links are rejected.
```

参数 `parent_id` / `child_id`。**适用**：补建依赖边，例如发现"合成任务"应等"采集任务"。

## RL 训练（10 个）

[tools/rl_training_tool.py](tools/rl_training_tool.py)。**全 10 个的门控都是 `check_rl_api_keys()`**：Python ≥3.11 + `TINKER_API_KEY` + `WANDB_API_KEY`。底层是 Atropos + Tinker + sglang。

### `rl_list_environments`

```
List all available RL environments. Returns environment names, paths, and descriptions. TIP: Read the file_path with file tools to understand how each environment works (verifiers, data loading, rewards).
```

无参数。AST 扫描 `tinker-atropos/tinker_atropos/environments/` 找 `BaseEnv` 子类。

### `rl_select_environment`

```
Select an RL environment for training. Loads the environment's default configuration. After selecting, use rl_get_current_config() to see settings and rl_edit_config() to modify them.
```

参数 `name`。动态导入 env 模块、调 `config_init()` 反射 Pydantic 字段。

### `rl_get_current_config`

```
Get the current environment configuration. Returns only fields that can be modified: group_size, max_token_length, total_steps, steps_per_eval, use_wandb, wandb_name, max_num_workers.
```

无参数。区分 `configurable_fields` 与 `locked_fields`（基础设施级别不让改）。

### `rl_edit_config`

```
Update a configuration field. Use rl_get_current_config() first to see all available fields for the selected environment. Each environment has different configurable options. Infrastructure settings (tokenizer, URLs, lora_rank, learning_rate) are locked.
```

参数 `field` / `value`。锁定字段拒写。

### `rl_start_training`

```
Start a new RL training run with the current environment and config. Most training parameters (lora_rank, learning_rate, etc.) are fixed. Use rl_edit_config() to set group_size, batch_size, wandb_project before starting. WARNING: Training takes hours.
```

无参数。生成 yaml config，`asyncio.create_task(_spawn_training_run)` 顺序起 3 个子进程：run-api → launch_training.py（trainer + sglang）→ env serve；间歇 5s/30s/90s 等待 + liveness check。

### `rl_check_status`

```
Get status and metrics for a training run. RATE LIMITED: enforces 30-minute minimum between checks for the same run. Returns WandB metrics: step, state, reward_mean, loss, percent_correct.
```

参数 `run_id`。30 分钟限速；poll 三个子进程；可选 `WANDB_ENTITY` 拉指标。

### `rl_stop_training`

```
Stop a running training job. Use if metrics look bad, training is stagnant, or you want to try different settings.
```

参数 `run_id`。SIGTERM 倒序终结三进程，10s 后 SIGKILL 兜底。

### `rl_get_results`

```
Get final results and metrics for a completed training run. Returns final metrics and path to trained weights.
```

参数 `run_id`。WandB 拉 summary + 10 sample history。

### `rl_list_runs`

```
List all training runs (active and completed) with their status.
```

无参数。仅当前进程内存里的 `_active_runs`，进程重启就丢。

### `rl_test_inference`

```
Quick inference test for any environment. Runs a few steps of inference + scoring using OpenRouter. Default: 3 steps x 16 completions = 48 rollouts per model, testing 3 models = 144 total. Tests environment loading, prompt construction, inference parsing, and verifier logic. Use BEFORE training to catch issues.
```

参数 `num_steps` / `group_size` / `models[]`。每模型一个子进程跑 env 的 `process` 模式（OpenRouter inference），10 分钟超时；产 JSONL 算 per-step 准确率。**适用**：训练前花 ~ 144 rollout 的小钱验证 env 不出 bug。

---

# 九、消息发送

## `send_message` — 跨平台统一发送

- **toolset**: `messaging` · **门控**: 当前 session 在非本地平台或 gateway 在跑 · **文件**: [tools/send_message_tool.py](tools/send_message_tool.py)

```
Send a message to a connected messaging platform, or list available targets.

IMPORTANT: When the user asks to send to a specific channel or person (not just a bare platform name), call send_message(action='list') FIRST to see available targets, then send to the correct one.
If the user just says a platform name like 'send to telegram', send directly to the home channel without listing first.
```

- **参数**：`action` ∈ {send, list}；`target` 形如 `telegram` / `telegram:#channel` / `telegram:chat_id` / `telegram:chat_id:thread_id`；`message`（支持 `MEDIA:<local_path>` token 触发原生媒体附件）。
- **实现**：解析 target → 解析人类友好别名 → 派发到 17 个 per-platform sender 之一（telegram / discord / slack / signal / matrix / yuanbao / feishu / weixin / sms / email / mattermost / whatsapp / dingtalk / wecom / bluebubbles / qqbot / 插件 adapter）；超长按平台 max-length 自动分片；发送后回写 gateway session。
- **适用**：任何主动通知 / 跨平台转发 / cron 自动投递。

---

# 十、Discord 原生

## `discord` — 读与参与

- **toolset**: `discord` · **门控**: `DISCORD_BOT_TOKEN` · **文件**: [tools/discord_tool.py](tools/discord_tool.py)

```
Read and participate in a Discord server.

Available actions:
  fetch_messages(channel_id)  — recent messages; optional before/after snowflakes
  search_members(guild_id, query)  — find members by name prefix
  create_thread(channel_id, name)  — create a public thread; optional message_id anchor

Use the channel_id from the current conversation context. Use search_members to look up user IDs by name prefix.
```

> Schema 在加载时按 bot 实际 intents 动态裁剪：缺 MESSAGE_CONTENT 会附 `content` 为空的提示；缺 GUILD_MEMBERS 则隐藏 `search_members`。

- **参数**：`action` ∈ {fetch_messages, search_members, create_thread}；`guild_id` / `channel_id` / `message_id` / `query` / `name` / `limit` / `before` / `after` / `auto_archive_duration`。
- **实现**：纯 stdlib `urllib.request` 打 `discord.com/api/v10`；config 级 `discord.server_actions` allowlist 二次门控；403 错误增强为可操作提示。
- **适用**：bot 直接读所在频道、查成员 ID、建讨论 thread。

## `discord_admin` — 服务端管理

- **toolset**: `discord_admin` · **门控**: 同上

```
Manage a Discord server via the REST API.

Available actions:
  list_guilds()  — list servers the bot is in
  server_info(guild_id)  — server details + member counts
  list_channels(guild_id)  — all channels grouped by category
  channel_info(channel_id)  — single channel details
  list_roles(guild_id)  — roles sorted by position
  member_info(guild_id, user_id)  — lookup a specific member
  search_members(guild_id, query)  — find members by name prefix
  list_pins(channel_id)  — pinned messages in a channel
  pin_message(channel_id, message_id)  — pin a message
  unpin_message(channel_id, message_id)  — unpin a message
  add_role(guild_id, user_id, role_id)  — assign a role
  remove_role(guild_id, user_id, role_id)  — remove a role

Call list_guilds first to discover guild_ids, then list_channels for channel_ids. Runtime errors will tell you if the bot lacks a specific per-guild permission (e.g. MANAGE_ROLES for add_role).
```

- **参数**：同 `discord` 的字段集合，`action` 覆盖管理操作。
- **实现**：`_detect_capabilities` 通过 `GET /applications/@me` 懒加载缓存；intent 与 user-config allowlist 双向门控。
- **适用**：列频道/管角色/钉消息等服务管理操作。

---

# 十一、飞书

## `feishu_doc_read`

- **toolset**: `feishu_doc` · **门控**: `lark_oapi` 可导入 · **文件**: [tools/feishu_doc_tool.py](tools/feishu_doc_tool.py)

```
Read the full content of a Feishu/Lark document as plain text. Useful when you need more context beyond the quoted text in a comment.
```

- **参数**：`doc_token`。
- **实现**：每线程通过 `set_client()` 注入 lark client，`GET /open-apis/docx/v1/documents/:document_id/raw_content` 用 tenant token。
- **适用**：处理飞书文档评论事件时拉全文上下文。

## `feishu_drive_list_comments` / `feishu_drive_list_comment_replies` / `feishu_drive_reply_comment` / `feishu_drive_add_comment`

- **toolset**: `feishu_drive` · **门控**: 同上 · **文件**: [tools/feishu_drive_tool.py](tools/feishu_drive_tool.py)

`list_comments`：

```
List comments on a Feishu document. Use is_whole=true to list whole-document comments only.
```

参数：`file_token` / `file_type`（默认 docx）/ `is_whole` / `page_size`（≤100）/ `page_token`。

`list_comment_replies`：

```
List all replies in a comment thread on a Feishu document.
```

参数：`file_token` / `comment_id` / `file_type` / `page_size` / `page_token`。

`reply_comment`：

```
Reply to a local comment thread on a Feishu document. Use this for local (quoted-text) comments. For whole-document comments, use feishu_drive_add_comment instead.
```

参数：`file_token` / `comment_id` / `content`（纯文本）。出现 1069302 错误码提示降级到 `add_comment`。

`add_comment`：

```
Add a new whole-document comment on a Feishu document. Use this for whole-document comments or as a fallback when reply_comment fails with code 1069302.
```

参数：`file_token` / `content`。Body 用 `reply_elements` 包 `text_run`。

- **适用**：飞书文档审阅自动化——读评论、回复、追加全文评论。

---

# 十二、元宝（5 个）

[tools/yuanbao_tools.py](tools/yuanbao_tools.py)。门控全是 `_check_yuanbao()`：session.platform == "yuanbao" 或 active adapter 存在。

## `yb_query_group_info`

```
Query basic info about a group (called '派/Pai' in the app), including group name, owner, and member count.
```

参数 `group_code`。

## `yb_query_group_members`

```
Query members of a group (called '派/Pai' in the app). Use this tool when you need to @mention someone, find a user by name, list bots (including Yuanbao AI), or list all members. IMPORTANT: You MUST call this tool before @mentioning any user, because you need the exact nickname to construct the @mention format.
```

参数 `group_code` / `action` ∈ {find, list_bots, list_all} / `name`（find 必需）/ `mention`。`mention=true` 时返回完整 @ 格式提示。

## `yb_send_dm`

```
Send a private/direct message (DM) to a user in a group, with optional media files. This tool automatically looks up the user by name in the group member list and sends the message. Use this when someone asks to privately message / 私信 / DM a user. Supports text, images, and file attachments. You can also provide user_id directly if already known.
```

参数 `group_code` / `name`（任一）/ `user_id` / `message` / `media_files[{path, is_voice}]`。多 match 返回候选错误。

## `yb_search_sticker`

```
Search the built-in Yuanbao sticker (TIM face / 表情包) catalogue by keyword. Returns the top matching candidates with sticker_id, name, and description. Use this BEFORE yb_send_sticker to discover the right sticker_id. Sticker = 贴纸 = TIM face — NOT a message reaction. Prefer sending a sticker over bare Unicode emoji when reacting/expressing emotion.
```

参数 `query` / `limit`（≤50）。纯本地静态目录查找。

## `yb_send_sticker`

```
Send a built-in sticker (TIMFaceElem / 贴纸表情) to the current Yuanbao chat. Call yb_search_sticker first if you don't know the sticker_id/name. Sticker = 贴纸 = TIM face — NOT a message reaction. CRITICAL: Whenever the user asks you to send a sticker / 贴纸 / 表情包, you MUST use this tool. DO NOT draw a PNG via execute_code / Pillow / matplotlib and then call send_image_file — that produces a fake 'sticker' image instead of a real TIM face and is the WRONG path.
```

参数 `sticker`（id 或 name，空=随机）/ `chat_id` / `reply_to`。

- **适用**：元宝（腾讯派/Pai）IM 平台的成员查询、私信、表情贴。

---

# 十三、Home Assistant（4 个）

[tools/homeassistant_tool.py](tools/homeassistant_tool.py)。门控全是 `HASS_TOKEN` env，URL 默认 `http://homeassistant.local:8123`。

## `ha_list_entities`

```
List Home Assistant entities. Optionally filter by domain (light, switch, climate, sensor, binary_sensor, cover, fan, etc.) or by area name (living room, kitchen, bedroom, etc.).
```

参数 `domain` / `area`（友好名子串）。`GET /api/states` 后过滤。

## `ha_get_state`

```
Get the detailed state of a single Home Assistant entity, including all attributes (brightness, color, temperature setpoint, sensor readings, etc.).
```

参数 `entity_id`（`domain.name` 格式，正则校验防注入）。`GET /api/states/{entity_id}`。

## `ha_list_services`

```
List available Home Assistant services (actions) for device control. Shows what actions can be performed on each device type and what parameters they accept. Use this to discover how to control devices found via ha_list_entities.
```

参数 `domain` 过滤。`GET /api/services`。

## `ha_call_service`

```
Call a Home Assistant service to control a device. Use ha_list_services to discover available services and their parameters for each domain.
```

参数 `domain` / `service` / `entity_id` / `data`（JSON 字符串）。**禁用域**：`shell_command`、`command_line`、`python_script`、`pyscript`、`hassio`、`rest_command`（防 RCE）；正则校验 domain/service/entity_id 防路径穿越。`POST /api/services/{domain}/{service}`。

- **适用**：开关灯、调温、操控智能门窗等家居控制；先 list 后 call。

---

# 十四、MCP 通用接入

[tools/mcp_tool.py](tools/mcp_tool.py) 实现**动态注册**：启动时读 `~/.hermes/config.yaml::mcp_servers`，每个 server 起一个 `MCPServerTask`（stdio subprocess 或 HTTP/StreamableHTTP），调 `session.list_tools()` 发现工具，按发现结果在主注册表里 emit `registry.register(...)`——**没有静态调用**。每 server 触发三类注册：

## 类别 1：工具代理 `mcp_<server>_<tool>`

- **toolset**: `mcp-<server>` · **门控**: 该 server session 活跃
- **schema description**: 取自 server 自报告的 `Tool.description`，过 prompt-injection 扫描。
- **handler**: `_make_tool_handler(server_name, tool_name, tool_timeout)` 在专属事件循环线程里 `run_coroutine_threadsafe(session.call_tool(...))`；**3 次连续失败 → 60s 熔断**；遇到 `mcp.client.auth` 401 调 `MCPOAuthManager.handle_401` 重连一次；transport 过期同样自动重连一次；错误信息脱敏；per-server `_rpc_lock` 防 stdio 流锁死。
- **适用**：任何外部 MCP server 暴露的工具——文件系统、GitHub、自定义服务等。

## 类别 2：资源工具

- `mcp_<server>_list_resources` —— `"List available resources from MCP server '<name>'"`，无参数。
- `mcp_<server>_read_resource` —— `"Read a resource by URI from MCP server '<name>'"`，参数 `uri`。
- 仅当 server 实现且 `tools.resources` 不为 false 时注册。**适用**：MCP server 暴露文件/数据源时。

## 类别 3：提示工具

- `mcp_<server>_list_prompts` —— `"List available prompts from MCP server '<name>'"`，无参数。
- `mcp_<server>_get_prompt` —— `"Get a prompt by name from MCP server '<name>'"`，参数 `name` + 可选 `arguments`。
- **适用**：取 server 内置 prompt template 注入推理。

> **MCP 基础设施**：单后台 daemon 线程跑 asyncio 循环；最多 5 次指数退避自动重连；OAuth 2.1 PKCE for HTTP；动态 `tools/list_changed` 通知；per-server include/exclude 过滤；spawn stdio subprocess 前 OSV 恶意包数据库检查。

---

# 十五、Spotify 插件（7 个）

`plugins/spotify/` 里通过 plugin loader 的 `ctx.register_tool()` 注册（仍走主注册表）。**全 7 个 toolset 都是 `spotify`**，**门控 `_check_spotify_available()`**——必须先 `hermes auth spotify`。

## `spotify_playback`

```
Control Spotify playback, inspect the active playback state, or fetch recently played tracks.
```

`action` ∈ {get_state, get_currently_playing, play, pause, next, previous, seek, set_repeat, set_shuffle, set_volume, recently_played}；其余字段视 action 而定（`device_id`, `context_uri`, `uris[]`, `position_ms`, `state`, `volume_percent`, `limit`, `after`, `before`）。

## `spotify_devices`

```
List Spotify Connect devices or transfer playback to a different device.
```

`action` ∈ {list, transfer}；`device_id` / `play`。

## `spotify_queue`

```
Inspect the user's Spotify queue or add an item to it.
```

`action` ∈ {get, add}；`uri` / `device_id`。

## `spotify_search`

```
Search the Spotify catalog for tracks, albums, artists, playlists, shows, or episodes.
```

`query`、`type`/`types`（默认 `["track"]`）、`limit`（≤50）、`offset`、`market`。

## `spotify_playlists`

```
List, inspect, create, update, and modify Spotify playlists.
```

`action` ∈ {list, get, create, add_items, remove_items, update_details}；通用字段如 `playlist_id` / `uris[]` / `name` / `description` / `public` / `collaborative` / `position` / `snapshot_id`。

## `spotify_albums`

```
Fetch Spotify album metadata or album tracks.
```

`action` ∈ {get, tracks}；`album_id` / `market` / `limit` / `offset`。

## `spotify_library`

```
List, save, or remove the user's saved Spotify tracks or albums. Use `kind` to select which.
```

`kind` ∈ {tracks, albums}；`action` ∈ {list, save, remove}；`uris[]` / `ids[]`。

- **适用**：声控 Spotify、查看正在播放、加入队列、改播放列表等。

---

# 工具组（Toolset）→ 平台映射速查

[toolsets.py](toolsets.py) 根据平台/场景把工具收紧：

| 平台 / 场景 | 含工具组 | 备注 |
|---|---|---|
| `hermes-cli` / `hermes-cron` | `_HERMES_CORE_TOOLS`（27 个） | 完整核心；`hermes tools` 进一步过滤 |
| `hermes-acp` | core 减消息/音频/clarify | 编辑器集成（VS Code/Zed/JetBrains） |
| `hermes-api-server` | core 减交互/messaging | OpenAI 兼容 HTTP API |
| `hermes-{telegram,whatsapp,slack,signal,...}` | core | 聊天平台 |
| `hermes-discord` | core + `discord` + `discord_admin` |  |
| `hermes-feishu` | core + `feishu_doc_read` + 4 个 `feishu_drive_*` |  |
| `hermes-yuanbao` | core + 5 个 `yb_*` |  |
| `debugging` | web + file + terminal | 故障排查场景 |
| `safe` | web + vision + image_gen | 无 terminal 的安全模式 |

**默认关闭**（除非 `hermes tools` 显式打开）：`moa`、`homeassistant`、`rl`。

---

# 安全与门控总结

1. **运行时 check_fn**：每个工具注册时携带 `check_fn` —— gateway 是否在跑（`send_message`）、env 是否齐（`HASS_TOKEN`、`DISCORD_BOT_TOKEN`、`OPENROUTER_API_KEY`、`FAL_KEY`、`TINKER_API_KEY` 等）、后端是否可达（terminal 后端、browser CLI、CDP endpoint）、特定 env 是否被设置（`HERMES_KANBAN_TASK`）。**未通过的工具不会进 schema**，模型看不见，杜绝幻觉调用。
2. **静态 allowlist**：`discord.server_actions`、`hermes_state.config` 等用户级 allowlist 在 schema 生成阶段做二次过滤。
3. **路径/命令注入防护**：`ha_call_service` 黑名单 `shell_command`/`pyscript` 等域；`cronjob` 的 `script` 必须在 `$HERMES_HOME/scripts/` 内；`feishu` / `home assistant` / `discord` 的 entity/path 全部正则校验。
4. **Prompt injection 扫描**：`memory`、`skill_manage`、MCP 工具 description——所有用户/外部内容写入或注入前都过 [tools/skills_guard.py](tools/skills_guard.py) 的 `_INJECTION_PATTERNS`。
5. **熔断与限速**：MCP 3 次失败 → 60s；watch_patterns 15s 冷却 + 三振；`rl_check_status` 30 分钟最低间隔；`web_search` 限 100 结果；`web_extract` 限 5 URL；`session_search` 默认 3 摘要、上限 5。
6. **沙盒**：`execute_code` 在 subprocess + UDS RPC，仅 7 个白名单工具；`terminal` 危险命令走审批；`delegate_task` 子 agent 屏蔽 5 个工具。
7. **OSV 检查**：MCP stdio subprocess 启动前先查恶意包数据库。

---

# 实现风格观察

- **schema 即文档即提示**：工具的"用法"、"什么时候用"、"什么时候别用"全写在 description 里——这是模型唯一能看到的指引。Hermes 的 description 普遍偏长（如 `delegate_task` 上千字），且喜欢列**反例**（"DO NOT use cat to read files"），把易错路径直接堵死。
- **统一返回 JSON 字符串**：所有 handler 返回 `tool_result()` 包出的 JSON 字符串，便于模型一致解析。
- **大输出走辅助 LLM**：`web_extract` / `session_search` / `browser_snapshot` 等大体量工具会就地调 Gemini Flash 等廉价模型摘要后再返主上下文，省 token 也省时间。
- **门控 = "工具不存在"**：宁可不进 schema 也不要让模型看见用不了的工具——这避免了"幻觉调用 + 错误返回"的恶性循环。
- **跨工具引用要动态注入**：[AGENTS.md](AGENTS.md) 明确禁止在 schema description 里硬编码"用 X 工具"——因为 X 可能因 check_fn 不在，转而用 [model_tools.py](model_tools.py) 的 `get_tool_definitions()` 后处理动态拼接。

---

# 总览

| 类别 | 数量 | 文件 |
|---|---|---|
| Agent 元工具 | 7 | memory_tool / todo_tool / session_search_tool / skills_tool×2 / skill_manager_tool / clarify_tool |
| 编排 | 3 | delegate_tool / code_execution_tool / mixture_of_agents_tool |
| 文件 + 终端 | 6 | file_tools×4 / terminal_tool / process_registry |
| Web | 2 | web_tools×2 |
| 浏览器 + CDP | 12 | browser_tool×10 / browser_cdp_tool / browser_dialog_tool |
| 多模态 | 3 | vision_tools / image_generation_tool / tts_tool |
| 调度 + 多 Agent | 18 | cronjob_tools / kanban_tools×7 / rl_training_tool×10 |
| 消息 | 1 | send_message_tool |
| 平台原生 | 11 | discord×2 / feishu_doc / feishu_drive×4 / yuanbao×5（注意 yuanbao 是 5 个，feishu 共 5 个）|
| Home Assistant | 4 | homeassistant_tool×4 |
| MCP（动态） | 3 类/server | mcp_tool |
| Spotify 插件 | 7 | plugins/spotify/ |
| **合计（不含 MCP 动态注册）** | **~76** | 30 个文件 |
