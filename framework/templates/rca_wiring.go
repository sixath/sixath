package templates

import (
	"log/slog"
	"strings"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/datasource"
	"github.com/sixath/framework/executor"
	"github.com/sixath/framework/tool"
)

// registerRCATools 按配置条件注册 RCA 工具链的三组工具。
// 各子节缺省时对应工具跳过(记 log),不返回错误——缺配置不应阻断整个 handler。
func registerRCATools(reg *tool.Registry, cfg config.Config) error {
	if reg == nil {
		return nil
	}

	// 1) 多仓库代码检索:有 roots 才注册。
	if len(cfg.RCA.Repos.Roots) > 0 {
		if err := tool.RegisterRCACodeTools(reg, cfg.RCA.Repos.Roots); err != nil {
			return err
		}
	} else {
		slog.Info("rca: repos.roots empty, skip rca_grep/rca_glob/rca_read")
	}

	// 2) Jaeger:有 query_url 才注册。
	if cfg.RCA.Jaeger.QueryURL != "" {
		if err := tool.RegisterJaegerTool(reg, cfg.RCA.Jaeger.QueryURL); err != nil {
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
			ID:       inlineID,
			Type:     datasource.TypeElasticsearch,
			DSN:      ep,
			User:     cfg.RCA.ES.User,
			Password: cfg.RCA.ES.Password,
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
	dsReg := datasource.NewRegistry()
	datasource.RegisterElasticsearch(dsReg)
	if _, err := dsReg.Register(*dsCfg); err != nil {
		slog.Warn("rca: register es datasource failed", "err", err)
		return nil, false
	}
	bundle := executor.NewBundle(dsReg)
	return bundle.Reader, true
}
