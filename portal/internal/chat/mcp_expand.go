package chat

import (
	"context"
	"os"
	"strings"
	"sync"

	"backend/internal/biz"

	"github.com/sixath/framework/tool"
)

const mcpExpandOnMissEnv = "SATH_MCP_EXPAND_ON_MISS"

// McpExpandOnMissEnabled reports whether bound-MCP hot-load on discovery miss is on (default on).
func McpExpandOnMissEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(mcpExpandOnMissEnv)))
	return !(v == "0" || v == "false" || v == "off" || v == "no")
}

// McpExpandOnMissOptions wires a per-turn expander for list_tools / tool_search.
type McpExpandOnMissOptions struct {
	Reg            *tool.Registry
	BoundServers   []*biz.McpServerMeta // full agent bindings (not surface-filtered)
	ActiveFamilies map[string]struct{} // shared with TurnIntentGate; nil = no family gate
	ToolFamily     map[string]string   // shared tool→family index; mutated on expand
	Wiring         CatalogWiringInput
	Catalog        tool.ToolCatalog
}

// McpExpandOnMiss hot-registers bound MCP servers when discovery misses.
type McpExpandOnMiss struct {
	mu             sync.Mutex
	reg            *tool.Registry
	bound          []*biz.McpServerMeta
	activeFamilies map[string]struct{}
	toolFamily     map[string]string
	wiring         CatalogWiringInput
	catalog        tool.ToolCatalog
}

// NewMcpExpandOnMiss builds a controller. Returns nil when disabled or inputs incomplete.
func NewMcpExpandOnMiss(opts McpExpandOnMissOptions) *McpExpandOnMiss {
	if !McpExpandOnMissEnabled() {
		return nil
	}
	if opts.Reg == nil || len(opts.BoundServers) == 0 {
		return nil
	}
	tf := opts.ToolFamily
	if tf == nil {
		tf = map[string]string{}
	}
	return &McpExpandOnMiss{
		reg:            opts.Reg,
		bound:          opts.BoundServers,
		activeFamilies: opts.ActiveFamilies,
		toolFamily:     tf,
		wiring:         opts.Wiring,
		catalog:        opts.Catalog,
	}
}

// CurrentCatalog implements tool.ToolDiscoveryExpand.
func (e *McpExpandOnMiss) CurrentCatalog() tool.ToolCatalog {
	if e == nil {
		return tool.ToolCatalog{}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.catalog
}

// ExpandOnMiss implements tool.ToolDiscoveryExpand.
func (e *McpExpandOnMiss) ExpandOnMiss(ctx context.Context, query string) ([]string, error) {
	if e == nil {
		return nil, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	candidates := matchBoundMCPServers(query, e.bound, e.reg)
	if len(candidates) == 0 {
		return nil, nil
	}

	var expanded []string
	for _, s := range candidates {
		if s == nil || s.ID == "" {
			continue
		}
		if e.reg.HasMcpServer(s.ID) {
			continue
		}
		mc := biz.McpServerToConfig(s)
		if mc == nil {
			continue
		}
		tool.RegisterMcpTool(e.reg, mc)
		if !e.reg.HasMcpServer(mc.Id) {
			continue
		}
		expanded = append(expanded, s.ID)
		if e.activeFamilies != nil {
			e.activeFamilies[MCPFamilyID(s.ID)] = struct{}{}
		}
	}
	if len(expanded) == 0 {
		return nil, nil
	}

	// Refresh tool→family index for newly registered tools.
	for name, fam := range BuildToolFamilyIndex(e.reg) {
		e.toolFamily[name] = fam
	}

	e.wiring.Reg = e.reg
	e.catalog = BuildCatalogForAgent(ctx, e.wiring)
	return expanded, nil
}

// matchBoundMCPServers picks unbound MCP metas for hot-load.
// Empty query → all not-yet-registered (list_tools 全量浏览场景).
// Non-empty → only servers whose id/name/description overlap the query.
// 不在「query 与 MCP 元数据无关」时回退装载全部，避免 lightstreamer 之类业务词误触发 Confluence。
func matchBoundMCPServers(query string, servers []*biz.McpServerMeta, reg *tool.Registry) []*biz.McpServerMeta {
	unbound := make([]*biz.McpServerMeta, 0, len(servers))
	for _, s := range servers {
		if s == nil || strings.TrimSpace(s.ID) == "" {
			continue
		}
		if reg != nil && reg.HasMcpServer(s.ID) {
			continue
		}
		unbound = append(unbound, s)
	}
	if len(unbound) == 0 {
		return nil
	}
	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		return unbound
	}

	var matched []*biz.McpServerMeta
	for _, s := range unbound {
		if mcpMetaMatchesQuery(q, s) {
			matched = append(matched, s)
		}
	}
	return matched
}

func mcpMetaMatchesQuery(queryLower string, s *biz.McpServerMeta) bool {
	if s == nil || queryLower == "" {
		return false
	}
	for _, tip := range []string{s.ID, s.Name, s.Description} {
		tip = strings.ToLower(strings.TrimSpace(tip))
		if tip == "" {
			continue
		}
		if strings.Contains(queryLower, tip) || strings.Contains(tip, queryLower) {
			return true
		}
		// token overlap for multi-word queries
		for _, tok := range strings.FieldsFunc(queryLower, func(r rune) bool {
			return r == ' ' || r == ',' || r == ';' || r == '/' || r == '|'
		}) {
			tok = strings.TrimSpace(tok)
			if len(tok) < 2 {
				continue
			}
			if strings.Contains(tip, tok) {
				return true
			}
		}
	}
	return false
}

// WithDiscoveryExpand attaches expander to ctx when non-nil.
func WithDiscoveryExpand(ctx context.Context, exp *McpExpandOnMiss) context.Context {
	if ctx == nil || exp == nil {
		return ctx
	}
	return context.WithValue(ctx, tool.ContextKeyToolDiscoveryExpand, exp)
}
