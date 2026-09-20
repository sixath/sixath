package chat

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"backend/internal/biz"

	"github.com/sixath/framework/datasource"
	"github.com/sixath/framework/netx"
	"github.com/sixath/framework/tool"
)

// httpRequestClientTimeout matches framework/tool/http_tool.go default.
const httpRequestClientTimeout = 20 * time.Second

const outboundClientTimeout = 30 * time.Second

// LoadProxyCatalog loads proxy Specs for the agent default and each tool
// ModeProxy binding. It uses GetByID only — no resources/ACL re-check.
func LoadProxyCatalog(ctx context.Context, repo biz.ProxyRepo, agentProxyID string, tools []*biz.ToolMeta) map[string]netx.Spec {
	out := make(map[string]netx.Spec)
	if repo == nil {
		return out
	}
	for _, id := range collectProxyIDs(agentProxyID, tools) {
		meta, err := repo.GetByID(ctx, id)
		if err != nil || meta == nil {
			continue
		}
		out[id] = proxyMetaToSpec(meta)
	}
	return out
}

func collectProxyIDs(agentProxyID string, tools []*biz.ToolMeta) []string {
	seen := make(map[string]struct{})
	var ids []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	add(agentProxyID)
	for _, t := range tools {
		if t == nil {
			continue
		}
		b := toolEgressBinding(toolConfigToMap(t.Config))
		if netx.NormalizeMode(b.Mode) == netx.ModeProxy {
			add(b.ProxyID)
		}
	}
	return ids
}

func proxyMetaToSpec(m *biz.ProxyMeta) netx.Spec {
	if m == nil {
		return netx.Spec{}
	}
	np := m.NoProxy
	if np != nil {
		np = append([]string(nil), np...)
	}
	return netx.Spec{
		ID:       m.ID,
		Type:     m.Type,
		Host:     m.Host,
		Port:     m.Port,
		User:     m.User,
		Password: m.Password,
		NoProxy:  np,
	}
}

func toolEgressBinding(cfg map[string]interface{}) netx.Binding {
	if cfg == nil {
		return netx.Binding{Mode: netx.ModeInherit}
	}
	mode, _ := cfg["egress_mode"].(string)
	proxyID, _ := cfg["proxy_id"].(string)
	return netx.Binding{Mode: netx.NormalizeMode(mode), ProxyID: strings.TrimSpace(proxyID)}
}

func agentSpecFromOpts(o RegistryBuildOptions) *netx.Spec {
	id := strings.TrimSpace(o.AgentProxyID)
	if id == "" || o.Proxies == nil {
		return nil
	}
	if s, ok := o.Proxies[id]; ok {
		cp := s
		return &cp
	}
	return nil
}

func agentProxyCatalogErr(o RegistryBuildOptions) error {
	id := strings.TrimSpace(o.AgentProxyID)
	if id == "" {
		return nil
	}
	if o.Proxies != nil {
		if _, ok := o.Proxies[id]; ok {
			return nil
		}
	}
	return fmt.Errorf("proxy %q not found", id)
}

func resolveEffective(b netx.Binding, o RegistryBuildOptions, destHost string) (*netx.Effective, error) {
	if netx.NormalizeMode(b.Mode) == netx.ModeInherit {
		if err := agentProxyCatalogErr(o); err != nil {
			return nil, err
		}
	}
	return netx.Resolve(b, agentSpecFromOpts(o), o.Proxies, destHost)
}

func clientFromEffective(eff *netx.Effective, timeout time.Duration) *http.Client {
	if eff == nil {
		return nil
	}
	return netx.HTTPClientEffective(*eff, timeout)
}

func applyHTTPRequestOverlay(reg *tool.Registry, o RegistryBuildOptions) {
	if reg == nil {
		return
	}
	if err := agentProxyCatalogErr(o); err != nil {
		id := strings.TrimSpace(o.AgentProxyID)
		slog.Error("egress: agent proxy not in catalog, http_request fail-closed", "proxy_id", id)
		reg.SetHTTPClient(netx.UnavailableHTTPClient(id, httpRequestClientTimeout), netx.Spec{ID: id})
		return
	}
	spec := agentSpecFromOpts(o)
	if spec == nil {
		return
	}
	reg.SetHTTPClient(netx.HTTPClient(*spec, httpRequestClientTimeout), *spec)
}

func applyDatasourceEgress(cfg *datasource.Config, b netx.Binding, o RegistryBuildOptions) error {
	if cfg == nil {
		return nil
	}
	typ := strings.ToLower(strings.TrimSpace(cfg.Type))
	if typ == "hive" {
		return nil
	}
	dest := destHostFromMySQL(*cfg)
	if isElasticsearchType(typ) {
		dest = destHostFromURL(cfg.DSN)
		if dest == "" {
			dest = destHostFromHostPort(cfg.Host, cfg.Port)
		}
	}
	eff, err := resolveEffective(b, o, dest)
	if err != nil {
		return err
	}
	if eff == nil {
		return nil
	}
	switch typ {
	case "mysql", "mongo", "mongodb":
		label := typ
		if typ == "mongo" {
			label = "mongodb"
		}
		if strings.EqualFold(strings.TrimSpace(eff.Spec.Type), netx.TypeHTTP) {
			return netx.AnnotateError(eff.Spec, fmt.Errorf("%s cannot use an http proxy", label))
		}
		if strings.EqualFold(strings.TrimSpace(eff.Spec.Type), netx.TypeSOCKS5) {
			inner := netx.DialContext(eff.Spec)
			spec := eff.Spec
			cfg.ProxyNetKey = spec.ID
			cfg.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				c, err := inner(ctx, network, addr)
				return c, netx.AnnotateError(spec, err)
			}
		}
	case "elasticsearch", "es":
		cfg.HTTPClient = clientFromEffective(eff, outboundClientTimeout)
	}
	return nil
}

func applyMCPHTTPClient(mc *tool.McpConfig, b netx.Binding, o RegistryBuildOptions) error {
	if mc == nil || isStdioTransport(mc.Transport) {
		return nil
	}
	eff, err := resolveEffective(b, o, destHostFromURL(mc.Endpoint))
	if err != nil {
		return err
	}
	mc.HTTPClient = clientFromEffective(eff, outboundClientTimeout)
	return nil
}

func resolveJaegerClient(queryURL string, b netx.Binding, o RegistryBuildOptions) (*http.Client, error) {
	eff, err := resolveEffective(b, o, destHostFromURL(queryURL))
	if err != nil {
		return nil, err
	}
	return clientFromEffective(eff, outboundClientTimeout), nil
}

func isStdioTransport(transport string) bool {
	return strings.EqualFold(strings.TrimSpace(transport), "stdio")
}

func destHostFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		return destHostFromHostPort(raw, 0)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func destHostFromHostPort(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	if port > 0 && strings.Contains(host, ":") {
		if h, _, err := net.SplitHostPort(net.JoinHostPort(host, fmt.Sprintf("%d", port))); err == nil {
			return h
		}
	}
	return host
}

func destHostFromMySQL(cfg datasource.Config) string {
	if h := destHostFromHostPort(cfg.Host, cfg.Port); h != "" {
		return h
	}
	return destHostFromMySQLDSN(cfg.DSN)
}

func destHostFromMySQLDSN(dsn string) string {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return ""
	}
	if i := strings.Index(dsn, "tcp("); i >= 0 {
		rest := dsn[i+4:]
		if j := strings.Index(rest, ")"); j >= 0 {
			return destHostFromHostPort(rest[:j], 0)
		}
	}
	return destHostFromURL(dsn)
}
