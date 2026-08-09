// Package middleware 提供 Agent 中间件链。
//
// # 中间件执行顺序
//
// Chain(final, mws...) 中 mws[0] 在最外层（最先看到请求、最后看到响应）。
// ChainBuilder 按 Order 降序排列：Order 越大越靠外（越先执行），Order 越小越靠近 Handler（越后执行）。
//
// 推荐 Order 约定（数值越大越靠外）:
//
//	Recovery=100, Tracing=80, Logging=70, Metrics=60,
//	RateLimit=50, ContentSafety=40, Cache=20
//
// MergeGlobalLocal 先 global 后 local，再到达 final Handler。
package middleware
