package tool

import "context"

// EnabledToolsetsFromContext 读取 context 中的 enabled_toolsets 白名单；无则返回 nil（表示不过滤）。
func EnabledToolsetsFromContext(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	v, ok := ctx.Value(ContextKeyEnabledToolsets).([]string)
	if !ok || len(v) == 0 {
		return nil
	}
	out := make([]string, len(v))
	copy(out, v)
	return out
}

// ListForAPI 返回应暴露给 LLM API schema 的工具：先按 toolsets 过滤，再应用 CheckFn。
// toolsets 为 nil 时从 ctx 读取 ContextKeyEnabledToolsets；仍为空则等价 List()。
func (r *Registry) ListForAPI(ctx context.Context, toolsets []string) []Tool {
	if r == nil {
		return nil
	}
	if toolsets == nil {
		toolsets = EnabledToolsetsFromContext(ctx)
	}
	base := r.ListByToolsets(toolsets)
	if len(base) == 0 {
		return base
	}
	out := make([]Tool, 0, len(base))
	for _, t := range base {
		if t.CheckFn != nil {
			if err := t.CheckFn(ctx); err != nil {
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

// ListDeferred 返回 ListForAPI 结果中应延迟加载（deferred）的工具子集。
func (r *Registry) ListDeferred(ctx context.Context) []Tool {
	base := r.ListForAPI(ctx, nil)
	cfg := DefaultDeferConfig()
	out := make([]Tool, 0)
	for _, t := range base {
		if ShouldDefer(t, cfg) {
			out = append(out, t)
		}
	}
	return out
}

// ListForAPIWithDefer 在 ListForAPI 基础上，当 deferActive 为 true 时从 schema 中排除 deferred 工具。
func (r *Registry) ListForAPIWithDefer(ctx context.Context, toolsets []string, deferActive bool) []Tool {
	base := r.ListForAPI(ctx, toolsets)
	if !deferActive {
		return base
	}
	cfg := DefaultDeferConfig()
	out := make([]Tool, 0, len(base))
	for _, t := range base {
		if ShouldDefer(t, cfg) {
			continue
		}
		out = append(out, t)
	}
	return out
}
