# S2 血肉包：`framework/context` + PromptBuilder

**日期**: 2026-09-05  
**状态**: 已确认（设计评审，2026-09-05）  
**范围**: 新建 `framework/context`；迁入 L0/L1/L2；PromptBuilder；Model 变为傻 Provider。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S1](./2026-09-05-dead-code-hub-off-design.md)  
**后续**: [S3](./2026-09-05-harness-workspace-rename-design.md)

**一句话**: Context 是血肉。Harness 在调 Model **之前**做 PromptBuilder 与压缩管道；`model` 只编码、发请求、解析 tool_calls。

本 spec **废止**父规格「Context 管道留在 `framework/model`」及「P1–P4 不引入 PromptBuilder」（P1–P4 已结束）。

---

## 1. 背景

现网 `PrepareChatContextCtx` 在 `openai.go` / `openai_tools.go` / `openai_tools_stream.go` **内部**运行，Provider 吞掉血肉。Portal 用 `BuildEffectiveSystemPrompt` 把 Agent 文案与 skills 索引拼成一条 `ReActConfig.SystemPrompt`，没有 Stable/Ephemeral，也没有 `prompt_stable_hash`。

对齐 [design-agent-runtime-hermes-inspired](../../../framework/docs/design-agent-runtime-hermes-inspired.md) G-O3，但不对接厂商 prompt cache，也不恢复已裁的 catalog/web/datasource/任务锁叠层。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| 新包 | `github.com/sixath/framework/context`：PromptBuilder + L0/L1/L2 |
| Provider | `model` 不再调用 `PrepareChatContextCtx` |
| Builder 输入 | 调用方传入字符串 / 工具名列表；Builder **不** import portal |
| Stable | Agent `systemPrompt` + skills **索引**（`templates.BuildSkillsAwarePrompt`，非 SKILL 全文）+ 可选 `MEMORY.md` / `USER.md` + 当前 registry **短工具名列表** |
| Ephemeral | 本轮一次性说明（预算告警、护栏 banner）；可空 |
| Encode | 默认一条 system：`Stable` 在前；有 Ephemeral 时中间 `\n\n---\n\n` |
| Hash | `prompt_stable_hash` = SHA256(Stable) 的十六进制前 16 字符 |
| 管道配置 | `context.PipelineConfig`（预算、snip、L2、Trace）；`model.CallConfig` 只留温度 / max tokens / model 名 |
| 禁止 | 旧 `FormatToolCatalogPrompt` 置顶块；厂商 cache breakpoint |

---

## 3. 包与依赖

```text
portal → (现) agent/harness → context / tool / workspace(S3)
context → model          （Message 仍是 canonical）
model 不得 → context / harness / agent
```

`[]model.Message` 仍是编排层消息类型。管道文件从 `framework/model/context_*.go`、`snip_compact.go`、`l2_*.go`、`context_budget*.go` 等迁到 `framework/context`（实施计划列精确文件）。单测随迁。

`model` 内 **不** 保留 `PrepareChatContext` 转发包装（否则 model 必须 import context）。所有调用者改为 `context.Prepare` / `PrepareCtx`。

---

## 4. 每步模型调用顺序

1. `PromptBuilder.Build(in) → {Stable, Ephemeral, StableHash}`
2. `Encode(Stable, Ephemeral)` → 一条 system 字符串
3. 写入或替换 messages 中的 system 条
4. `context.PrepareCtx(ctx, messages, PipelineConfig)`（顺序与现网一致：L1 → snip → L2 预剪枝 → code pin → L0 → strip → L2 摘要）
5. `model.Chat` / `ChatWithTools` / stream 等价入口（内部不再压缩）

Harness（现 `framework/agent`）在 `beginModelInvocation` 路径执行 1–4。直接调用 Model 的测试不得再假设 Provider 会压缩。

### PromptBuilder 输入（概念 API）

```text
AgentSystem   string
SkillsIndex   string      // 已渲染的索引，不是 *skills.Index
MemoryMD      string      // 可选；文件不存在则空
UserMD        string      // 可选
ToolNames     []string    // 当时 registry 的 List 名；MCP 热加载后可变（Stable 合法变化）
Ephemeral     string      // 本轮一次性；无则空
```

Portal 装配器负责读 workspace 的 MEMORY.md/USER.md、调用 `BuildSkillsAwarePrompt`、把 Agent 文案交给 Harness。Harness 每步从 **当时** registry 取 ToolNames。

Trace：Run / invocation 记录 `prompt_stable_hash`（可选字段即可）。

---

## 5. 非目标

- 不改 `framework/agent` → `harness` 包名（S3）
- 不抽出 `framework/workspace`（S3）
- 不对接 Anthropic/OpenAI prompt cache
- 不把工具 schema/描述全文塞进 Stable（只要短名列表）
- 不恢复 Skill 全文预注入

---

## 6. 成功标准

1. `grep PrepareChatContext framework/model/*.go` 无生产调用（测试已迁出）。
2. 只改 Ephemeral、Stable 字节不变 → `prompt_stable_hash` 不变（单测）。
3. Encode 顺序固定：Stable 在前；无 Ephemeral 时无 `---` 分隔符。
4. 默认装配仍能：假 Model + 计算器或无工具跑通 SSE。
5. 管道行为与迁出前一致：现有 L0/L1/L2 单测换包后绿。
6. `go test ./framework/context ./framework/model ./framework/agent ./framework/tool -count=1` 绿。

禁止把 `_neo4j_q/` 当夹具。
