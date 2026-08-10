package tool

import "context"

// ContextKeyToolDiscoveryExpand 挂载 ToolDiscoveryExpand，供 list_tools / tool_search
// 在目录未命中时按 Agent 绑定热装载更多工具（如 MCP）。
const ContextKeyToolDiscoveryExpand = "tool_discovery_expand"

// ToolDiscoveryExpand 在工具发现未命中时扩展可用工具面。
// 实现方应保证线程安全，并在 ExpandOnMiss 成功后更新 CurrentCatalog。
type ToolDiscoveryExpand interface {
	CurrentCatalog() ToolCatalog
	// ExpandOnMiss 尝试装载更多工具。query 为空表示装载全部尚未注册的绑定能力；
	// 非空时优先按元数据匹配，匹配不到则可回退为装载全部未注册绑定。
	// expanded 为新装载的资源标识（如 mcp server id）。
	ExpandOnMiss(ctx context.Context, query string) (expanded []string, err error)
}

// CatalogFromContext 优先取 Expand 的实时目录，其次取 ContextKeyToolCatalog 快照。
func CatalogFromContext(ctx context.Context) (ToolCatalog, bool) {
	if ctx == nil {
		return ToolCatalog{}, false
	}
	if exp, ok := ctx.Value(ContextKeyToolDiscoveryExpand).(ToolDiscoveryExpand); ok && exp != nil {
		return exp.CurrentCatalog(), true
	}
	raw := ctx.Value(ContextKeyToolCatalog)
	cat, ok := raw.(ToolCatalog)
	return cat, ok
}

// DiscoveryExpandFromContext 读取可选的发现扩展钩子。
func DiscoveryExpandFromContext(ctx context.Context) ToolDiscoveryExpand {
	if ctx == nil {
		return nil
	}
	exp, _ := ctx.Value(ContextKeyToolDiscoveryExpand).(ToolDiscoveryExpand)
	return exp
}
