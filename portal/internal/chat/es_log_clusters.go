package chat

import (
	"log/slog"
	"strings"

	"backend/internal/biz"

	"github.com/sixath/framework/datasource"
	"github.com/sixath/framework/executor"
	"github.com/sixath/framework/tool"
)

// collectESLogClusters builds the ES cluster table from bound elasticsearch
// datasources, then merges transitional RCA es_log_query rows. Connections
// live on a dedicated datasource.Registry, not the data-trio registry.
func collectESLogClusters(tools []*biz.ToolMeta) (clusters []tool.ESLogCluster, esReg *datasource.Registry) {
	esReg = datasource.NewRegistry()
	datasource.RegisterElasticsearch(esReg)

	byID := make(map[string]*tool.ESLogCluster)
	var order []string
	upsert := func(id string) *tool.ESLogCluster {
		if c, ok := byID[id]; ok {
			return c
		}
		c := &tool.ESLogCluster{ID: id}
		byID[id] = c
		order = append(order, id)
		return c
	}

	for _, t := range tools {
		if t == nil || t.Type != biz.ToolTypeDatasource {
			continue
		}
		dsMap := datasourceMapFromTool(t)
		dsCfg := canonicalDatasourceConfig(t.Name, datasource.ConfigFromMap(dsMap))
		if !isElasticsearchType(dsCfg.Type) {
			continue
		}
		dsCfg.Type = datasource.TypeElasticsearch
		if _, err := esReg.Register(dsCfg); err != nil {
			slog.Warn("es_log: register elasticsearch datasource failed", "id", dsCfg.ID, "err", err)
			continue
		}
		c := upsert(dsCfg.ID)
		c.DefaultIndex = mapStringField(dsMap, "default_index", "defaultIndex")
		c.TraceIDField = mapStringField(dsMap, "trace_id_field", "traceIdField")
		c.Purpose = mapStringField(dsMap, "purpose")
	}

	for _, t := range tools {
		if t == nil || t.Type != biz.ToolTypeRCA {
			continue
		}
		cfg := toolConfigToMap(t.Config)
		rcaMap, _ := cfg["rca"].(map[string]interface{})
		if rcaMap == nil {
			continue
		}
		funcPath, _ := rcaMap["func_path"].(string)
		if strings.TrimSpace(funcPath) != "es_log_query" {
			continue
		}
		endpoint, _ := rcaMap["endpoint"].(string)
		dsID, _ := rcaMap["datasource_id"].(string)
		endpoint = strings.TrimSpace(endpoint)
		dsID = strings.TrimSpace(dsID)
		if (endpoint != "") == (dsID != "") {
			slog.Warn("rca: es_log_query need exactly one of endpoint or datasource_id, skip")
			continue
		}

		fillEmpty := func(c *tool.ESLogCluster) {
			if c.DefaultIndex == "" {
				c.DefaultIndex = mapStringField(rcaMap, "default_index", "defaultIndex")
			}
			if c.TraceIDField == "" {
				c.TraceIDField = mapStringField(rcaMap, "trace_id_field", "traceIdField")
			}
			if c.Purpose == "" {
				c.Purpose = strings.TrimSpace(t.Description)
			}
		}

		if dsID != "" {
			c, ok := byID[dsID]
			if !ok {
				slog.Warn("rca: es_log_query datasource not found among agent tools, skip", "datasource_id", dsID)
				continue
			}
			fillEmpty(c)
			continue
		}

		clusterID := strings.TrimSpace(t.Name)
		if clusterID == "" {
			slog.Warn("rca: inline es_log_query missing tool name, skip")
			continue
		}
		if existing, ok := byID[clusterID]; ok {
			slog.Warn("rca: inline es endpoint ignored; same-name datasource already registered", "id", clusterID)
			fillEmpty(existing)
			continue
		}

		dsCfg := datasource.Config{
			ID:   clusterID,
			Type: datasource.TypeElasticsearch,
			DSN:  endpoint,
		}
		if u, _ := rcaMap["user"].(string); strings.TrimSpace(u) != "" {
			dsCfg.User = u
			if p, _ := rcaMap["password"].(string); p != "" {
				dsCfg.Password = p
			}
		}
		if _, err := esReg.Register(dsCfg); err != nil {
			slog.Warn("rca: inline es register failed", "err", err)
			continue
		}
		fillEmpty(upsert(clusterID))
	}

	clusters = make([]tool.ESLogCluster, 0, len(order))
	for _, id := range order {
		clusters = append(clusters, *byID[id])
	}
	return clusters, esReg
}

// registerESLogFromAgentTools registers es_log_query once when the cluster table is nonempty.
func registerESLogFromAgentTools(reg *tool.Registry, tools []*biz.ToolMeta) {
	if reg == nil {
		return
	}
	clusters, esReg := collectESLogClusters(tools)
	if len(clusters) == 0 {
		return
	}
	if err := tool.RegisterESLogTool(reg, executor.NewESExecutor(esReg), tool.ESLogConfig{Clusters: clusters}); err != nil {
		slog.Warn("es_log_query: register failed", "err", err)
	}
}

func datasourceMapFromTool(t *biz.ToolMeta) map[string]interface{} {
	m := toolConfigToMap(t.Config)
	if nested, ok := m["datasource"].(map[string]interface{}); ok {
		return nested
	}
	return m
}

func mapStringField(m map[string]interface{}, keys ...string) string {
	if m == nil {
		return ""
	}
	for _, k := range keys {
		if v, ok := m[k].(string); ok {
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		}
	}
	return ""
}
