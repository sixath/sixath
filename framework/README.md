# Sixath Agent Framework

Go 语言 AI Agent 运行时框架，为 Portal（管理面 + runtime）与 Gateway（入站）提供统一的多轮 ReAct 执行内核、工具生态、技能（Skill）、MCP、多层记忆、上下文工程与可观测性。

> 本 README 描述**实际代码能力**（不再是早期 "V0.1 MVP 骨架" 的自述）。历史演进与治理决策见 [docs/](../docs/)。

## 核心能力

- **多轮 ReAct 执行内核**（`agent/`）：工具循环、并行工具（信号量 + `RequiresSequential` 串行回退）、三条流式路径、`forceFinalSummary` 兜底、证据门（EvidenceGate）、后置模型策略（PostModelPolicy）、RunTrace / OTel span。
- **工具生态**（`tool/`）：30+ 内置工具（文件/补丁/终端 PTY/进程/SSH/SCP/浏览器 chromedp/网页搜索/抽取/数据查询 list_tables+execute_read+write/rca_*/memory_*/cronjob/todo/ask_user/vision/jaeger/es_log），以及渐进披露的 `tool_search`/`tool_describe`/`tool_call` + BM25 目录检索。
- **工具治理**：Registry 前置 JSON Schema 参数校验、统一超时、`ApprovalPolicy`（read/write/destructive/network 风险分级 + 会话级授权缓存）、SSRF / pathguard / SQL guard 防护。
- **技能（Skill）**（`skills/`）：`SKILL.md` frontmatter 索引、关键词路由、校验、按需加载正文、fsnotify 热重载（`Watcher`）。
- **MCP**（`tool/mcp.go`）：双后端（mark3labs / metoro）、stdio + Streamable HTTP、进程池 + 引用计数 + idle TTL、stdio 白名单、幂等注册；另可反向充当 MCP Server（`memory/mcp/server.go`）。
- **多层记忆**（`memory/`）：多 scope（user/session/agent）、向量（SQLite/Qdrant）、图（Neo4j）、RRF 混合检索、语义冲突、procedural、embedding 缓存 + 熔断、跨会话 SQLite FTS5。
- **上下文工程**（`model/context_pipeline.go`）：L1 清洗 → snip → L0 预算裁剪 → 清理孤儿 tool → L2 LLM 摘要（含失败冷却）；`TokenCounter` 抽象 + usage 回填自校准（`CalibratedCounter`）。
- **模型接入**（`model/`）：OpenAI / DashScope / Ollama + `MultiModel`；出口统一包**重试/退避装饰器**（`WrapResilient`，流式仅首 token 前重试）。
- **规划（Plan-Execute）**（`agent/plan_agent.go`）：结构化 `Plan` 解析/校验、逐步执行、失败 replan（上限 2 次）、规划失败回退 ReAct。
- **可观测**（`obs/`）：Prometheus 指标 + OTel span；`turntrace/` 落 RunTrace。

## 包结构

```text
agent/          ReActAgent / ChatAgent / PlanExecuteAgent、护栏、RunTrace
model/          Model 接口、provider 适配器、重试装饰器、token 计数与上下文管线
tool/           工具注册表、30+ 内置工具、参数校验、ApprovalPolicy、MCP client
skills/         SKILL.md 索引与热重载
memory/         记忆 Facade（向量/图/RRF）、embedding 缓存、MCP server
turntrace/      RunTrace 持久化
obs/            Prometheus 指标
events/         事件总线
growth/         Growth 复盘/策展运行模型
sessionsearch/  会话检索
memorysearch/   记忆索引（fsnotify 增量）
```

## 快速开始

框架不自带独立服务，典型消费方是 Portal（`../portal`，module `backend`，通过 `replace ../framework` 引用）与 Gateway（`../gateway`）。

最小模型调用：

```go
m, err := model.NewFromIdentifier("openai/gpt-4o") // 返回已包重试装饰器的 Model
gen, err := m.Generate(ctx, "你好")
```

构造 ReAct Agent：

```go
reg := tool.NewRegistry()
// ... 注册工具 ...
a := agent.NewReActAgent(m, mem, reg, agent.WithMaxSteps(20))
resp, err := a.Run(ctx, &agent.Request{Messages: []model.Message{{Role: "user", Content: "..."}}})
```

## 与 Portal / Gateway 的衔接

- Portal `internal/chat/agent_builder.go` 用 `BuildReActAgent` + `RegisterAgentRuntimeTools` 组装 agent，并注入 `TokenCounter`、`L2Runtime`、ApprovalPolicy 等。
- Portal 的 runtime 接口（`/runtime/v1`）SSE 流式下发模型/工具事件，消费 `RunTrace`。
- Gateway 通过 Portal runtime 调用 agent；trace 经 W3C `traceparent` 透传贯通（Gateway → Portal → framework 同一 trace）。

## 测试与评测

- `framework/` 单测数百个，覆盖 ReAct 循环、工具、记忆、上下文管线、重试、token 校准、Skill 热重载、Plan-Execute 等核心路径。
- 回归评测集与 runner 见 [`../evals/`](../evals/)。

## 相关文档

- 架构/设计：`docs/` 下 `design-*.md`、`product-spec-*.md` 等。
- 成熟度补齐计划与 ADR：`../docs/`（`superpowers/plans/`、`adr/`）。
