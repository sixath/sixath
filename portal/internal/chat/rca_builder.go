package chat

import (
	"log/slog"
	"strings"
	"time"

	"backend/internal/biz"

	"github.com/sixath/framework/datasource"
	"github.com/sixath/framework/executor"
	"github.com/sixath/framework/tool"
)

// registerRCATool 按 cfg["func_path"] 构造并注册 RCA 工具。
// agentTools 为该 Agent 绑定的全部工具,供 es_log_query 查找其依赖的 datasource 工具。
// 缺配置/依赖缺失时跳过并记 warn,绝不 panic 或阻断整体构建。
func registerRCATool(reg *tool.Registry, cfg map[string]interface{}, agentTools []*biz.ToolMeta) {
	if reg == nil {
		return
	}
	rcaMap, _ := cfg["rca"].(map[string]interface{})
	if rcaMap == nil {
		slog.Warn("rca: tool config missing 'rca' section, skip")
		return
	}
	funcPath, _ := rcaMap["func_path"].(string)
	switch funcPath {
	case "rca_code":
		roots := stringSliceFromAny(rcaMap["roots"])
		if len(roots) == 0 {
			slog.Warn("rca: rca_code has no roots, skip")
			return
		}
		_ = tool.RegisterRCACodeTools(reg, roots)
	case "rca_symbol":
		roots := stringSliceFromAny(rcaMap["roots"])
		if len(roots) == 0 {
			slog.Warn("rca: rca_symbol has no roots, skip")
			return
		}
		goplsPath, _ := rcaMap["gopls_path"].(string)
		opts := tool.RCASymbolOpts{GoplsPath: goplsPath}
		if readyTimeout, ok := rcaTimeoutSeconds(rcaMap["ready_timeout_sec"]); ok {
			opts.ReadyTimeout = readyTimeout
		}
		if requestTimeout, ok := rcaTimeoutSeconds(rcaMap["request_timeout_sec"]); ok {
			opts.RequestTimeout = requestTimeout
		}
		_ = tool.RegisterRCASymbolTool(reg, roots, opts)
	case "jaeger_trace":
		queryURL, _ := rcaMap["query_url"].(string)
		if queryURL == "" {
			slog.Warn("rca: jaeger_trace has no query_url, skip")
			return
		}
		_ = tool.RegisterJaegerTool(reg, queryURL)
	case "es_log_query":
		endpoint, _ := rcaMap["endpoint"].(string)
		dsID, _ := rcaMap["datasource_id"].(string)
		endpoint = strings.TrimSpace(endpoint)
		dsID = strings.TrimSpace(dsID)
		if (endpoint != "") == (dsID != "") {
			slog.Warn("rca: es_log_query need exactly one of endpoint or datasource_id, skip")
			return
		}
		var reader executor.Reader
		var ok bool
		queryDSID := dsID
		if endpoint != "" {
			const inlineID = "rca-es"
			dsCfg := datasource.Config{
				ID:   inlineID,
				Type: datasource.TypeElasticsearch,
				DSN:  endpoint,
			}
			if u, _ := rcaMap["user"].(string); strings.TrimSpace(u) != "" {
				dsCfg.User = u
				if p, _ := rcaMap["password"].(string); p != "" {
					dsCfg.Password = p
				}
			}
			dsReg := datasource.NewRegistry()
			datasource.RegisterElasticsearch(dsReg)
			if _, err := dsReg.Register(dsCfg); err != nil {
				slog.Warn("rca: inline es register failed", "err", err)
				return
			}
			reader = executor.NewESExecutor(dsReg)
			ok = true
			queryDSID = inlineID
		} else {
			reader, ok = buildESReaderFromAgentTools(agentTools, dsID)
			if !ok {
				slog.Warn("rca: es_log_query datasource not found among agent tools, skip", "datasource_id", dsID)
				return
			}
		}
		defaultIndex, _ := rcaMap["default_index"].(string)
		traceIDField, _ := rcaMap["trace_id_field"].(string)
		_ = tool.RegisterESLogTool(reg, reader, tool.ESLogConfig{
			DatasourceID: queryDSID,
			DefaultIndex: defaultIndex,
			TraceIDField: traceIDField,
		})
	default:
		slog.Warn("rca: unknown func_path, skip", "func_path", funcPath)
	}
}

// rcaTimeoutSeconds converts config values decoded from structpb to durations.
func rcaTimeoutSeconds(v interface{}) (time.Duration, bool) {
	switch seconds := v.(type) {
	case float64:
		return time.Duration(seconds * float64(time.Second)), true
	case int:
		return time.Duration(seconds) * time.Second, true
	default:
		return 0, false
	}
}

// buildESReaderFromAgentTools 在 agentTools 中找到 id==dsID 的 datasource 工具,
// 构建只含该数据源的 ES executor(实现 executor.Reader)。找不到/构建失败返回 ok=false。
func buildESReaderFromAgentTools(agentTools []*biz.ToolMeta, dsID string) (executor.Reader, bool) {
	for _, t := range agentTools {
		if t.Type != biz.ToolTypeDatasource {
			continue
		}
		m := toolConfigToMap(t.Config)
		dsMap := m
		if nested, ok := m["datasource"].(map[string]interface{}); ok {
			dsMap = nested
		}
		dsCfg := datasource.ConfigFromMap(dsMap)
		// 运行时数据源 ID 对齐工具名(与 canonicalDatasourceConfig 一致)。
		id := dsCfg.ID
		if t.Name != "" {
			id = t.Name
		}
		if id != dsID {
			continue
		}
		dsCfg.ID = dsID
		dsReg := datasource.NewRegistry()
		datasource.RegisterElasticsearch(dsReg)
		if _, err := dsReg.Register(dsCfg); err != nil {
			slog.Warn("rca: register es datasource failed", "err", err)
			return nil, false
		}
		return executor.NewESExecutor(dsReg), true
	}
	return nil, false
}

// stringSliceFromAny 把 structpb 解出的 []any / []string 归一化为 []string。
func stringSliceFromAny(v interface{}) []string {
	switch xs := v.(type) {
	case []string:
		return xs
	case []interface{}:
		out := make([]string, 0, len(xs))
		for _, e := range xs {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
