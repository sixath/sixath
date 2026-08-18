package chat

import (
	"fmt"
	"strings"

	"github.com/sixath/framework/datasource"
)

// DatasourceBinding 描述 Agent 绑定的一个数据源工具及其运行时 ID。
type DatasourceBinding struct {
	ToolName  string
	ID        string
	Type      string
	DBName    string
	Available bool
	Err       string
	// SkipDataTools 为 true 时不进入 list_tables / describe_table / execute_read（如 elasticsearch）。
	SkipDataTools bool
}

const esDataToolsRoutingHint = "Elasticsearch / 日志检索请使用已绑定的 es_log_query 或 http_request，禁止对 ES 使用 list_tables、describe_table、execute_read。"

// isElasticsearchType reports whether a datasource type is Elasticsearch.
func isElasticsearchType(typ string) bool {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "elasticsearch", "es":
		return true
	default:
		return false
	}
}

// FormatDatasourcePrompt 生成注入 system prompt 的多数据源说明。
// SkipDataTools 绑定不列入 data 三件套清单，仅触发 ES 路由提示。
func FormatDatasourcePrompt(bindings []DatasourceBinding, defaultID string) string {
	if len(bindings) == 0 {
		return ""
	}
	var data []DatasourceBinding
	hasESSkip := false
	for _, ds := range bindings {
		if ds.SkipDataTools || isElasticsearchType(ds.Type) {
			hasESSkip = true
			continue
		}
		data = append(data, ds)
	}

	var b strings.Builder
	if len(data) > 0 {
		b.WriteString("## 已绑定数据源\n")
		b.WriteString("调用 list_tables、describe_table、execute_read 时必须显式传 datasource_id（与下列 ID 完全一致）。这些工具不支持 Elasticsearch。\n")
		hasAvailable := false
		for _, ds := range data {
			if ds.Available {
				hasAvailable = true
			}
			line := fmt.Sprintf("- **%s**（类型 %s", ds.ID, ds.Type)
			if ds.DBName != "" {
				line += fmt.Sprintf("，库 %s", ds.DBName)
			}
			if ds.ToolName != "" && ds.ToolName != ds.ID {
				line += fmt.Sprintf("；工具名 %s", ds.ToolName)
			}
			if !ds.Available {
				line += "；**当前不可用**"
				if ds.Err != "" {
					line += "：" + ds.Err
				}
			}
			if ds.ID == defaultID {
				line += "；**默认**"
			}
			line += "）\n"
			b.WriteString(line)
		}
		if defaultID != "" {
			b.WriteString(fmt.Sprintf("\n未指定 datasource_id 时使用默认：**%s**。\n", defaultID))
		}
		if hasAvailable {
			b.WriteString("\n至少有一个数据源**已可用**：禁止通过 ask_user 向用户索取数据库 host/端口/账号/密码或连接串；请直接用 list_tables → describe_table → execute_read。\n")
			b.WriteString("查库/查记录在 execute_read 返回结果后立即作答结束；不要再调 es_log_query 或历史会话中的排障流程，除非本轮用户明确要求查日志/链路。\n")
			b.WriteString("若表不在已绑定库中，说明库名/类型不匹配，应提示用户在 Agent 绑定正确的数据源工具，而不是让用户手填连接信息。\n")
		}
	}
	if hasESSkip {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(esDataToolsRoutingHint)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// AppendDatasourcePrompt 将多数据源说明追加到 system prompt。
func AppendDatasourcePrompt(base, datasourcePrompt string) string {
	base = strings.TrimSpace(base)
	datasourcePrompt = strings.TrimSpace(datasourcePrompt)
	if datasourcePrompt == "" {
		return base
	}
	if base == "" {
		return datasourcePrompt
	}
	return base + "\n\n---\n\n" + datasourcePrompt
}

func bindingFromConfig(toolName string, cfg datasource.Config, registerErr error) DatasourceBinding {
	b := DatasourceBinding{
		ToolName:  toolName,
		ID:        cfg.ID,
		Type:      cfg.Type,
		DBName:    cfg.DBName,
		Available: registerErr == nil,
	}
	if registerErr != nil {
		b.Err = registerErr.Error()
	}
	if isElasticsearchType(cfg.Type) {
		b.SkipDataTools = true
	}
	return b
}
