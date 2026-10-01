package tool

import "context"

// NestedToolGate 让「在工具内部再调用其它工具」的组合工具（如 compare）复用 harness 的控制面：
// Before 依次执行工具钩子与权限策略，返回改写后的参数或拒绝原因；After 执行工具钩子的后处理。
// 由 harness 在执行每个工具前注入 ctx；未注入时组合工具按自身约束直接执行。
type NestedToolGate struct {
	Before func(ctx context.Context, name string, args map[string]any) (map[string]any, error)
	After  func(ctx context.Context, name string, result any, err error) (any, error)
}

type nestedToolGateKey struct{}

// WithNestedToolGate 把 gate 绑定到 ctx。
func WithNestedToolGate(ctx context.Context, g *NestedToolGate) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, nestedToolGateKey{}, g)
}

// NestedToolGateFrom 读取 ctx 中的 gate；没有时返回 nil。
func NestedToolGateFrom(ctx context.Context) *NestedToolGate {
	if ctx == nil {
		return nil
	}
	g, _ := ctx.Value(nestedToolGateKey{}).(*NestedToolGate)
	return g
}
