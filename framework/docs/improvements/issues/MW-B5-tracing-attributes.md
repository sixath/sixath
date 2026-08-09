# [MW-B5] Tracing span 加 agent.name 与丰富 attribute

| 字段 | 值 |
|------|-----|
| **优先级** | P1 |
| **模块** | framework/middleware、framework/obs |
| **状态** | 已完成 |
| **关联报告** | [03-middleware.md B5](../03-middleware.md) |
| **预估工作量** | 0.5 天 |
| **依赖** | 无 |

## 问题位置

- `framework/middleware/tracing.go: TracingMiddleware`

## 现状

```go
func TracingMiddleware(next Handler) Handler {
    tracer := otel.Tracer("github.com/sixath/framework/agent")
    return func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
        ctx, span := tracer.Start(ctx, "Agent.Run", trace.WithSpanKind(trace.SpanKindServer))
        if req != nil && req.Metadata != nil {
            if uid, ok := req.Metadata["user_id"].(string); ok && uid != "" {
                span.SetAttributes(attribute.String("user.id", uid))
            }
        }
        defer span.End()
        resp, err := next(ctx, req)
        if err != nil { span.RecordError(err) }
        return resp, err
    }
}
```

## 问题分析

1. **span 名固定** = `"Agent.Run"`,APM 工具无法按 agent 分组聚合
2. **attribute 极薄**: 只取 user_id,缺 agent_name / messages.count / model / token / status
3. **没设 status**: error 时没 `span.SetStatus(codes.Error)`,APM 上颜色不会变红

## 改进方案

```go
func TracingMiddleware(next Handler) Handler {
    tracer := otel.Tracer("github.com/sixath/framework/agent")
    return func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
        agentName := "default"
        if req != nil && req.Metadata != nil {
            if v, ok := req.Metadata["agent_name"].(string); ok && v != "" { agentName = v }
        }

        ctx, span := tracer.Start(ctx, "Agent.Run/"+agentName,
            trace.WithSpanKind(trace.SpanKindServer))
        defer span.End()

        // 入口 attribute
        if req != nil {
            span.SetAttributes(
                attribute.String("agent.name", agentName),
                attribute.Int("agent.messages_count", len(req.Messages)),
                attribute.String("agent.request_id", req.RequestID),
            )
            if v, ok := req.Metadata["user_id"].(string); ok && v != "" {
                span.SetAttributes(attribute.String("enduser.id", v))
            }
            if v, ok := req.Metadata["model"].(string); ok && v != "" {
                span.SetAttributes(attribute.String("agent.model", v))
            }
        }

        resp, err := next(ctx, req)

        // 出口 attribute
        if err != nil {
            span.RecordError(err)
            span.SetStatus(codes.Error, err.Error())
        } else {
            span.SetStatus(codes.Ok, "")
            if resp != nil {
                span.SetAttributes(attribute.Int("agent.response_length", len(resp.Text)))
                if in, ok := anyx.Int64FromAny(resp.Metadata["token_input"]); ok {
                    span.SetAttributes(attribute.Int64("agent.token.input", in))
                }
                if out, ok := anyx.Int64FromAny(resp.Metadata["token_output"]); ok {
                    span.SetAttributes(attribute.Int64("agent.token.output", out))
                }
            }
        }
        return resp, err
    }
}
```

## 验收标准

- [ ] span 名包含 agent name(`Agent.Run/<name>`)
- [ ] 入口 attribute 至少包含 agent.name / agent.messages_count / agent.request_id
- [ ] 用户 id 用 OTel 标准命名 `enduser.id`(opentelemetry semantic conventions)
- [ ] 错误路径设置 `span.SetStatus(codes.Error, ...)`,Jaeger / Tempo 上正确变红
- [ ] token 字段使用 [MW-A4](MW-A4-metrics-token-parsing.md) 抽出的 `Int64FromAny` 解析

## 测试要求

- `TestTracingMiddleware_SpanName`: 用 `tracetest.NewInMemoryExporter`,断言 span name 含 agent_name
- `TestTracingMiddleware_Attributes`: 断言所有期望 attribute 存在
- `TestTracingMiddleware_ErrorStatus`: 断言 error 时 span status = Error

## 风险

- span name 含动态字符串可能在 cardinality 上有限制(Datadog APM 有要求)。Mitigation: 把 agent.name 放 attribute 而非 span name(可选,看 APM 偏好)
- 依赖 [MW-A4](MW-A4-metrics-token-parsing.md) 的 `Int64FromAny` helper,合并到同 PR 或先做 A4

## 关联 issue

- [MW-A4](MW-A4-metrics-token-parsing.md): 共用 Int64FromAny
- [DS-C4 / EX-C2](DS-C4-observability.md): 观测性整改总入口
