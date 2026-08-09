---
name: sixath-framework
description: >
  Operates and extends github.com/sixath/framework — Go AI Agent framework with ReAct,
  Skills (SKILL.md progressive disclosure), MCP tools, context compression (L0/L1/L2),
  multi-model providers, dataquery agents, and middleware. Use when developing framework
  code, creating framework-executable Skills, tuning config.yaml, debugging agent/tool/MCP
  flows, or running go test in the sixath framework repo. Install via skillpack or npx skills add.
tags: [sixath, framework, go, agent, react, skills, mcp, context-compression]
scope: [chat, portal, dataquery]
allowed_tools:
  - load_skill
  - read_skill_file
  - execute_skill_script
  - skills_list
  - skill_view
---

# sixath-framework

Sixath（sath）Go AI Agent 框架的操作手册：ReAct 循环、Skills 知识层、MCP 工具、上下文压缩、多模型、数据查询 Agent。

> 本 Skill 可被 **framework 运行时**加载（`skills_dirs` 含 `skillpack`），也可安装到 **Cursor/Claude Code** 供外部 Agent 改代码。

## 包内文档

| 文件 | 何时读 |
|------|--------|
| [README.md](README.md) | 安装（npx skills / install 脚本） |
| [agents.md](agents.md) | 跨 Agent 环境 |
| [reference.md](reference.md) | 包地图、CLI、测试、设计文档 |
| [authoring.md](authoring.md) | **编写 framework 可执行 Skill** |

## 定位仓库根

含 `go.mod`（`module github.com/sixath/framework`）的 `framework/` 目录。所有 `go` 命令在此执行。

Monorepo 中路径通常为 `<repo>/framework/`。

## 架构速览

```
应用 / Portal
  ↑ templates.New*HandlerFromConfig
Agent (ChatAgent / ReActAgent / PlanExecuteAgent)
  ↑ model.Model + memory + tool.Registry
Skills 层 (skills.Index + load_skill / skill_view)
  ↑ SKILL.md + scripts/
Tools (内置 + MCP + 数据查询)
Middleware → events.Bus → obs (metrics/tracing)
```

## 核心包边界

| 包 | 职责 | 禁止 |
|----|------|------|
| `agent/` | Agent 接口与 ReAct 循环 | 直接调 HTTP |
| `model/` | 模型抽象、OpenAI 工具调用、**上下文压缩 L0/L1/L2** | 注册业务工具 |
| `tool/` | Tool/Registry、load_skill、MCP、数据查询工具 | 解析 YAML 配置 |
| `skills/` | 扫描 SKILL.md、Index、LoadSkillBody | 执行 Agent |
| `templates/` | 从 config 装配 Handler | 业务逻辑 |
| `config/` | Config 结构与 Load | 运行时 Agent |
| `memory/` | 短期/向量/摘要记忆 | — |
| `middleware/` | Recovery/Logging/Metrics/Tracing 等 | — |
| `datasource/` + `executor/` | 数据源与 SQL/ES 执行 | — |

**依赖方向**：`templates` → `agent`/`tool`/`skills`；`model` 不依赖 `agent`（TraceSink 用回调避免循环引用）。

## 上下文压缩（改 model/ 时必读）

`model.PrepareChatContextCtx` 顺序（`context_pipeline.go`）：

1. L1 sanitize
2. Snip compact（可选）
3. L2 pre-prune tool bodies
4. L0 rune/token 预算压缩
5. strip orphan tools
6. L2 summarize（可选，需 ctx + L2 配置）

相关文件：`context_budget.go`、`snip_compact.go`、`l2_runtime.go`、`message_sanitize.go`。

## Skills 运行时（framework 执行本 Skill）

配置：

```yaml
skills:
  skills_dirs: [skillpack, skills_examples]
  allow_script_execution: true
  script_allowed_extensions: [".sh", ".ps1", ".py", ".js"]
```

| 工具 | 用途 |
|------|------|
| `skills_list` / `skill_view` | Hermes 对齐：列技能 / 读 SKILL.md + linked_files |
| `load_skill(name)` | 加载完整 SKILL.md；可按 Skill 声明注册 MCP |
| `read_skill_file(name, path)` | 读 docs/assets/scripts 下文件（不执行） |
| `execute_skill_script(name, path)` | 执行 `scripts/` 下脚本；path 必须以 `scripts/` 开头 |

**约束**：Skill 名称是 **参数**，不是工具名；必须先 `load_skill`/`skill_view` 再按正文操作。

## 常用命令

```bash
cd framework
go test ./...                    # 全量单测
go test ./model/... -v           # 改上下文压缩
go test ./tool/... -v            # 改工具/Skills
go build -o sath ./cmd/sath
./sath serve -a :8080 -c config.yaml
```

Framework 内验证（需 `allow_script_execution`）：

```
execute_skill_script("sixath-framework", "scripts/verify.ps1")
```

## 开发工作流

1. 读 [reference.md](reference.md) 确认改动落在哪个包
2. 小步修改，匹配现有命名与 Option 模式
3. `go test` 覆盖变更包；全量 `go test ./...` 收尾
4. 若改 Skill 工具或 frontmatter 解析 → 同步 `skills/index_test.go` / `tool/skill_tools` 相关测试
5. 若改上下文 pipeline → 跑 `model/*_test.go`

## 配置要点

```yaml
model: openai/gpt-4o
max_history: 10
middlewares: [logging, metrics, tracing]
skills:
  skills_dirs: [skillpack, skills_examples]
context_compression:   # 见 config.ContextCompression
  ...
```

密钥仅环境变量（`OPENAI_API_KEY` 等），禁止硬编码。

## Agent 选型

| 场景 | Handler |
|------|---------|
| 纯对话 | `NewChatAgentHandlerFromConfig` |
| Skills + 工具 | `NewSkillsAwareChatHandlerFromConfig` |
| 数据库查询 | `NewDataQueryHandlerFromConfig` |

## 禁止

- 在 `model/` 引入对 `agent/` 的 import（用 TraceSink 回调）
- 硬编码 API Key
- 未经配置开启 `execute_skill_script` 在生产环境跑不可信脚本
- 编造工具执行结果（缺信息时如实说明）
- 破坏 `skills/` Go 包与 `skillpack/` 目录的职责混淆（Go 包是索引代码，skillpack 是 SKILL.md 资产）

## 详细参考

- 包地图与测试：[reference.md](reference.md)
- 写新 Skill：[authoring.md](authoring.md)
- 最佳实践：`docs/best-practices.md`
- Skills 需求：`docs/skills-requirements.md`
