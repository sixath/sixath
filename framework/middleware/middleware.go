package middleware

import (
	"context"
	"sort"

	"github.com/sixath/framework/agent"
)

// Handler 表示 Agent 的最终处理函数。
type Handler func(ctx context.Context, req *agent.Request) (*agent.Response, error)

// Middleware 用于包装 Handler，形成中间件链。
type Middleware func(Handler) Handler

// Chain 将多个中间件按顺序与最终 Handler 组合。
// 自动在最外层注入 AgentContextMiddleware（若调用方未重复添加）。
func Chain(final Handler, mws ...Middleware) Handler {
	all := make([]Middleware, 0, len(mws)+1)
	all = append(all, AgentContextMiddleware())
	all = append(all, mws...)
	if len(all) == 0 {
		return final
	}
	h := final
	for i := len(all) - 1; i >= 0; i-- {
		h = all[i](h)
	}
	return h
}

// OrderedMiddleware 带优先级的中间件。Order 越大越靠外（越先执行），越小越靠近 Handler（越后执行）。
type OrderedMiddleware struct {
	Order int
	Mw    Middleware
}

// ChainBuilder 按 Order 降序排列后与 final 组合成 Handler。
func ChainBuilder(final Handler, ordered ...OrderedMiddleware) Handler {
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Order > ordered[j].Order })
	mws := make([]Middleware, len(ordered))
	for i := range ordered {
		mws[i] = ordered[i].Mw
	}
	return Chain(final, mws...)
}

// MergeGlobalLocal 将全局中间件与局部中间件合并：先执行全局，再执行局部，最后执行 final（B.5.2）。
// 即请求先经过 global...，再经过 local...，最后到达 core Handler。
func MergeGlobalLocal(final Handler, global, local []Middleware) Handler {
	all := make([]Middleware, 0, len(global)+len(local))
	all = append(all, global...)
	all = append(all, local...)
	return Chain(final, all...)
}
