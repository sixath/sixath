# ChatWithToolsStream 与 RunStream 流式改造方案

## 一、现状

| 场景 | RunStream 行为 |
|------|----------------|
| 无工具 | 若 model 实现 `StreamingModel`，则 `ChatStream` 逐 token 流式输出 |
| 有工具 | 同步执行完整 ReAct 循环，完成后**单块**发送 `resp.Text` |

**原因**：`ChatWithTools` 为非流式接口，ReAct 每步需完整响应才能解析 tool_call 并执行。

---

## 二、目标

有工具时，在**最终回答**阶段支持逐 token 流式输出；工具调用步骤仍同步执行（工具本身无法流式）。

---

## 三、技术要点

### 3.1 OpenAI 流式 + Tools 支持

`go-openai` 的 `CreateChatCompletionStream` 支持带 `Tools` 的请求。流式响应 `ChatCompletionStreamChoiceDelta` 包含：

```go
type ChatCompletionStreamChoiceDelta struct {
    Content      string        `json:"content,omitempty"`   // 文本 token
    ToolCalls    []ToolCall    `json:"tool_calls,omitempty"` // 增量 tool call
}
```

- **文本模式**：`Delta.Content` 有值 → 可立即转发给用户
- **工具模式**：`Delta.ToolCalls` 有值 → 需累积至 `finish_reason=tool_calls` 得到完整调用，再执行工具

### 3.2 单步决策流程

```
stream.Recv() 循环
├── Delta.Content != ""     → 转发到 textStream，继续
├── Delta.ToolCalls != nil  → 累积 tool call，停止转发文本
└── finish_reason
    ├── "stop"       → 无工具调用，返回 Generation{Used: false}
    └── "tool_calls" → 执行工具，返回 Generation{Used: true, Observation: ...}
```

---

## 四、接口设计

### 4.1 新增接口 `ToolCallingStreamingModel`

```go
// model/model.go

// ToolCallingStreamingModel 支持「带工具的流式」模型接口。
type ToolCallingStreamingModel interface {
    model.Model
    // ChatWithToolsStream 流式执行带工具的对话。
    // 返回 (textStream, finalGenCh, err)：
    //   - textStream：文本增量 channel
    //   - finalGenCh：stream 结束后收到 *Generation（含 ToolStep）
    ChatWithToolsStream(ctx context.Context, messages []model.Message, reg *tool.Registry, opts ...model.Option) (textStream <-chan string, finalGenCh <-chan *model.Generation, err error)
}
```

### 4.2 实现要点（OpenAIClient）

1. 复用 `openai_tools.go` 的 tools 构建逻辑，构建带 `Tools` 的 `ChatCompletionRequest`
2. 调用 `CreateChatCompletionStream` 替代 `CreateChatCompletion`
3. 循环 `stream.Recv()`：
   - `resp.Choices[0].Delta.Content` → 写入 `textStream`
   - `resp.Choices[0].Delta.ToolCalls` → 累积到 `toolCallsAccum`
   - `resp.Choices[0].FinishReason`：
     - `stop` → 关闭 textStream，返回 `Generation{Text: accumulated, Used: false}`
     - `tool_calls` → 关闭 textStream，从累积结果解析完整 tool call，执行工具，返回 `Generation{Text: observation, Used: true}`

**ToolCalls 累积**：流式 API 中 `tool_calls` 以 delta 形式返回，需按 `index` 合并多个 chunk 的 `id`、`function.name`、`function.arguments`。

---

## 五、ReActAgent.RunStream 改造

### 5.1 新逻辑

```go
// 有工具且 model 实现 ToolCallingStreamingModel
tsm, hasToolsStream := a.model.(model.ToolCallingStreamingModel)
if hasToolsStream && a.tools != nil {
    // ReAct 循环，每步用 ChatWithToolsStream
    for step := 0; step < a.maxSteps; step++ {
        textCh, gen, err := tsm.ChatWithToolsStream(ctx, messages, a.tools)
        if err != nil { ... }

        if !gen.Raw.(model.ToolStep).Used {
            // 本步为最终回答：流式输出 textCh，并收集完整文本
            out := make(chan string)
            go func() {
                defer close(out)
                var full strings.Builder
                for s := range textCh {
                    full.WriteString(s)
                    select { case out <- s: case <-ctx.Done(): return }
                }
                lastAnswer = full.String()
                // 可选：追加「请给出最终答案」再调一次 ChatStream...
            }()
            return out, nil
        }

        // 本步为 tool call：无流式输出，注入 observation，继续下一轮
        messages = append(messages, model.Message{Role: "tool", Content: gen.Text})
        // 下一轮可能又是 tool 或最终回答
    }
}

// 若 model 仅实现 ToolCallingModel 未实现 ToolCallingStreamingModel，则保持现有「同步后单块发送」
```

### 5.2 兼容性

| model 实现 | RunStream 行为 |
|------------|----------------|
| `ToolCallingStreamingModel` | 有工具时，最终回答步流式输出 |
| `ToolCallingModel`（仅） | 有工具时，同步执行后单块发送（与现有一致） |
| `StreamingModel`（无工具） | 无工具时流式（与现有一致） |

---

## 六、实现步骤

| 步骤 | 内容 |
|------|------|
| 1 | 在 `model/model.go` 中定义 `ToolCallingStreamingModel` 接口 |
| 2 | 在 `model/` 下新增 `openai_tools_stream.go`，实现 `OpenAIClient.ChatWithToolsStream` |
| 3 | 实现 tool_calls delta 累积逻辑（按 index 合并 id/name/arguments） |
| 4 | 修改 `agent/react_agent.go` 的 `RunStream`：检测 `ToolCallingStreamingModel`，有工具时走流式分支 |
| 5 | 补充单元测试与集成测试 |

---

## 七、注意事项

1. **ToolCalls 累积**：OpenAI 流式返回的 `tool_calls` 可能分多 chunk，`function.arguments` 为 JSON 字符串，需按 index 拼接。
2. **FinishReason**：需在 stream 结束的 chunk 中读取 `finish_reason`，以区分 `stop` 与 `tool_calls`。
3. **多 tool_call**：当前 `ChatWithTools` 仅处理首个 tool_call，流式实现可保持同样策略。
4. **最终回答步**：若 ReAct 在「请给出最终答案」时再调一次 `Chat`，该步也可改为 `ChatStream` 以保持流式体验。
