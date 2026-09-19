package templates

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/datasource"
	"github.com/sixath/framework/executor"
	"github.com/sixath/framework/netx"
	"github.com/sixath/framework/tool"
	fwws "github.com/sixath/framework/workspace"
)

// registerRCATools 按配置条件注册 RCA 工具链的三组工具。
// 各子节缺省时对应工具跳过(记 log),不返回错误——缺配置不应阻断整个 handler。
func registerRCATools(reg *tool.Registry, cfg config.Config) error {
	if reg == nil {
		return nil
	}

	// 1) 多仓库代码检索:只认 workspace/code（忽略 rca.repos.roots）。
	if code := fwws.ResolveCodeMount(cfg.Workspace); code != "" {
		if err := tool.RegisterRCACodeTools(reg, []string{code}); err != nil {
			return err
		}
	} else {
		slog.Info("rca: workspace/code missing, skip rca_grep/rca_glob/rca_read")
	}

	// 2) Jaeger:有 query_url 才注册。
	if cfg.RCA.Jaeger.QueryURL != "" {
		if err := tool.RegisterJaegerTool(reg, cfg.RCA.Jaeger.QueryURL, yamlEgressClient(cfg, destHostFromURL(cfg.RCA.Jaeger.QueryURL))); err != nil {
			return err
		}
	} else {
		slog.Info("rca: jaeger.query_url empty, skip jaeger_trace")
	}

	// 3) ES 日志: endpoint 与 datasource_id 恰好其一。
	ep := strings.TrimSpace(cfg.RCA.ES.Endpoint)
	dsID := strings.TrimSpace(cfg.RCA.ES.DatasourceID)
	switch {
	case ep != "" && dsID != "":
		slog.Warn("rca: es endpoint and datasource_id both set, skip es_log_query")
	case ep == "" && dsID == "":
		slog.Info("rca: es endpoint and datasource_id empty, skip es_log_query")
	case ep != "":
		const inlineID = "rca-es"
		dsCfg := datasource.Config{
			ID:         inlineID,
			Type:       datasource.TypeElasticsearch,
			DSN:        ep,
			User:       cfg.RCA.ES.User,
			Password:   cfg.RCA.ES.Password,
			HTTPClient: yamlEgressClient(cfg, destHostFromURL(ep)),
		}
		dsReg := datasource.NewRegistry()
		datasource.RegisterElasticsearch(dsReg)
		if _, err := dsReg.Register(dsCfg); err != nil {
			slog.Warn("rca: inline es register failed", "err", err)
			break
		}
		if err := tool.RegisterESLogTool(reg, executor.NewESExecutor(dsReg), tool.ESLogConfig{
			DatasourceID: inlineID,
			DefaultIndex: cfg.RCA.ES.DefaultIndex,
			TraceIDField: cfg.RCA.ES.TraceIDField,
			BodyField:    cfg.RCA.ES.BodyField,
		}); err != nil {
			return err
		}
	default:
		reader, ok := buildRCAESReader(cfg)
		if !ok {
			slog.Warn("rca: es datasource not found in data_sources, skip es_log_query",
				"datasource_id", cfg.RCA.ES.DatasourceID)
		} else {
			if err := tool.RegisterESLogTool(reg, reader, tool.ESLogConfig{
				DatasourceID: cfg.RCA.ES.DatasourceID,
				DefaultIndex: cfg.RCA.ES.DefaultIndex,
				TraceIDField: cfg.RCA.ES.TraceIDField,
				BodyField:    cfg.RCA.ES.BodyField,
			}); err != nil {
				return err
			}
		}
	}

	return nil
}

// buildRCAESReader 依据 cfg.RCA.ES.DatasourceID 从 cfg.DataSources 找到对应 ES 数据源,
// 构建一个只含该数据源的 registry 与只读 Reader。找不到则返回 ok=false。
func buildRCAESReader(cfg config.Config) (executor.Reader, bool) {
	var dsCfg *datasource.Config
	for i := range cfg.DataSources {
		if cfg.DataSources[i].ID == cfg.RCA.ES.DatasourceID {
			dsCfg = &cfg.DataSources[i]
			break
		}
	}
	if dsCfg == nil {
		return nil, false
	}
	injected := *dsCfg
	injected.HTTPClient = yamlEgressClient(cfg, destHostFromURL(injected.DSN))
	if injected.HTTPClient == nil && injected.Host != "" {
		injected.HTTPClient = yamlEgressClient(cfg, injected.Host)
	}
	dsReg := datasource.NewRegistry()
	datasource.RegisterElasticsearch(dsReg)
	if _, err := dsReg.Register(injected); err != nil {
		slog.Warn("rca: register es datasource failed", "err", err)
		return nil, false
	}
	return executor.NewESExecutor(dsReg), true
}

// rcaEgressClientTimeout matches framework/tool/http_tool.go http_request overlay.
const rcaEgressClientTimeout = 20 * time.Second

func yamlEgressClient(cfg config.Config, destHost string) *http.Client {
	cat := make(map[string]netx.Spec, len(cfg.Proxies))
	for _, s := range cfg.Proxies {
		id := strings.TrimSpace(s.ID)
		if id == "" {
			continue
		}
		cat[id] = s
	}
	var agent *netx.Spec
	if id := strings.TrimSpace(cfg.ProxyID); id != "" {
		if s, ok := cat[id]; ok {
			cp := s
			agent = &cp
		}
	}
	eff, err := netx.Resolve(netx.Binding{Mode: netx.ModeInherit}, agent, cat, destHost)
	if err != nil || eff == nil {
		return nil
	}
	// ModeInherit never sets Force; HTTPClient is enough.
	return netx.HTTPClient(eff.Spec, rcaEgressClientTimeout)
}

func destHostFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
