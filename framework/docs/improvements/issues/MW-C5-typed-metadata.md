# [MW-C5] Metadata 高频字段 typed 化

| 字段 | 值 |
|------|-----|
| **优先级** | P1 |
| **模块** | framework/agent、framework/middleware、各 model adapter |
| **状态** | 已完成 |
| **关联报告** | [03-middleware.md C5](../03-middleware.md) |
| **预估工作量** | 1 天 |
| **依赖** | 与 [MW-A4](MW-A4-metrics-token-parsing.md) 一起做 |

## 问题位置

- `framework/agent/types.go: Request.Metadata` / `Response.Metadata`
- 中间件调用点: 凡是 `req.Metadata["..."]` / `resp.Metadata["..."]`

## 现状

中间件强依赖 string key,但**没 const 定义**:
- `req.Metadata["agent_name"]` / `["user_id"]` / `["model"]`
- `resp.Metadata["token_input"]` / `["token_output"]`

随处出现散字符串,容易拼错(`"AgentName"` / `"agent-name"` / `"agentName"`),**测试无法发现**(默认值 fallback)。

## 改进方案

### Step 1 — 集中 const 定义

```go
// framework/agent/metadata.go (新)
package agent

// Well-known metadata keys
const (
    MetaAgentName  = "agent_name"
    MetaUserID     = "user_id"
    MetaModelName  = "model"
    MetaSystem     = "system_prompt"
    MetaTemperature = "temperature"

    MetaTokenInput  = "token_input"
    MetaTokenOutput = "token_output"
)
```

所有中间件 / model adapter / portal 调用点改用常量。

### Step 2 — 高频字段提升为 typed 字段

```go
type Request struct {
    Messages    []model.Message
    Metadata    map[string]any   // 保留作为扩展点

    // 高频字段 typed 化
    AgentName   string
    UserID      string
    ModelName   string
    Temperature float32
    SystemPrompt string
}

type Usage struct {
    InputTokens  int64
    OutputTokens int64
}

type Response struct {
    Text     string
    Parts    []Part
    Usage    Usage              // 新增
    Metadata map[string]any
}
```

中间件读 `req.AgentName` 而非 `req.Metadata["agent_name"]`。

### Step 3 — 兼容期

旧代码读 `req.Metadata["agent_name"]` 仍能拿到值(在 `Request` 的 getter 兜底),给一个 deprecation 期。

## 验收标准

- [ ] `framework/agent/metadata.go` 定义全部 well-known key 常量
- [ ] 所有 middleware / portal 调用点改用常量(grep `Metadata["` 应为 0)
- [ ] `Request` / `Response` 提升 5-7 个高频字段为 typed
- [ ] 各 model adapter 在解析响应时填充 `Usage` 字段
- [ ] CHANGELOG 记录 typed 字段优先级 > metadata map(若两者都有)

## 测试要求

- `TestRequest_TypedFieldsTakesPrecedence`: 同时设 typed 字段和 metadata map,中间件读到 typed
- `TestUsage_FilledByOpenAIAdapter`: mock OpenAI 响应,断言 `resp.Usage.InputTokens > 0`
- 所有现有测试用例的 `Metadata` 字符串改为常量,0 回归

## 风险

- `Request` / `Response` 加字段是**软 breaking**,用 struct literal 构造的代码会有 `unkeyed field` warning(但不破)
- portal 端需要同步升级 → 本 issue 完成前 portal 不会自动受益
- 各 adapter 填充 `Usage` 是后续工作,本 issue 主聚焦 framework 内部
