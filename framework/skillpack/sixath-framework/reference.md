# Sixath Framework · 包地图与参考

## 目录结构

```
framework/
├── agent/           ChatAgent, ReActAgent, PlanExecuteAgent
├── model/           Model 接口, OpenAI, 上下文压缩, tool calling
├── tool/            Registry, skill_tools, MCP, dataquery, ask_user
├── skills/          Index, LoadSkillBody（Go 库）
├── templates/       Handler 装配
├── config/          Config, SkillsConfig, ContextCompression
├── memory/          Buffer, vector, summary
├── middleware/      Chain, recovery, metrics, tracing, cache
├── events/          Bus, RunStarted, ToolExecuted, ...
├── obs/             metrics, health, tracer
├── datasource/      mysql, mongo, es, ...
├── executor/        SQL 执行、guard、mask
├── plugin/          编译期插件注册
├── cli/ + cmd/sath/ CLI: init, demo, serve
├── skillpack/       **可分发 Skill 包（本 skill）**
├── skills_examples/ 示例 Skill（python-data-helper, archive-move-ops, ...）
└── docs/            设计文档与 best-practices
```

## 上下文压缩文件

| 文件 | 作用 |
|------|------|
| `model/context_pipeline.go` | PrepareChatContextCtx 主编排 |
| `model/context_budget.go` | L0 rune/token 预算 |
| `model/message_sanitize.go` | L1 清洗 |
| `model/snip_compact.go` | Snip 压缩 |
| `model/l2_runtime.go` | L2 摘要与 cooldown |

设计：`docs/superpowers/specs/2026-05-26-session-context-compression-design.md`

## Skills 子系统（Go）

| 文件 | 作用 |
|------|------|
| `skills/index.go` | 扫描 SKILL.md frontmatter |
| `skills/loader.go` | LoadSkillBody / LoadSkillFile |
| `skills/meta.go` | SkillMeta 结构 |
| `tool/skill_tools.go` | load_skill, skill_view, execute_skill_script |

## 测试

```bash
cd framework
go test ./...                           # 全量
go test ./skills/... ./tool/... -v      # Skills 相关
go test ./model/... -v                  # 上下文压缩
go test ./agent/... -v                  # ReAct
```

或通过 skill 脚本：

```
execute_skill_script("sixath-framework", "scripts/verify.sh")
```

## CLI

```bash
go build -o sath ./cmd/sath
./sath init -d myapp
./sath demo          # 需 OPENAI_API_KEY
./sath serve -a :8080 -c config.yaml
```

HTTP：`POST /chat`、`POST /data/chat`、`GET /health`、`GET /metrics`

## 关键设计文档

| 文档 | 主题 |
|------|------|
| `docs/best-practices.md` | 配置、Agent、Skills、工具、部署 |
| `docs/skills-requirements.md` | Skills 知识层设计 |
| `docs/extending.md` | 插件与事件 |
| `docs/design-agent-runtime-hermes-inspired.md` | Agent 运行时 |
| `docs/design-memory-tools-hermes-parity.md` | 记忆与工具护栏 |
| `docs/superpowers/specs/2026-06-05-tool-discovery-design.md` | 工具发现 |
| `docs/toolsets-hermes-mapping.md` | toolset 映射 |

## 示例 Skill 参考

| 路径 | 说明 |
|------|------|
| `skills_examples/python-data-helper/` | 简单 Python 脚本 Skill |
| `skills_examples/skills/archive-move-ops/` | 复杂脚本 + references + tests |
| `skills_examples/skills/rca-investigation/` | RCA：jaeger → ES → code（E4） |
| `skills_examples/skills/harness-fix/` | G4：ERRORS.md → skill_manage 人审修复 |

## Portal 集成

上层仓库 `portal/` 通过 framework templates 装配 Agent；改 framework API 时需同步 portal 的 `agent_builder.go` 等。
