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
}

// FormatDatasourcePrompt 生成注入 system prompt 的多数据源说明。
func FormatDatasourcePrompt(bindings []DatasourceBinding, defaultID string) string {
	if len(bindings) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## 已绑定数据源\n")
	b.WriteString("调用 list_tables、describe_table、execute_read 时必须显式传 datasource_id（与下列 ID 完全一致）。\n")
	hasAvailable := false
	for _, ds := range bindings {
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
		b.WriteString("若表不在已绑定库中，说明库名/类型不匹配，应提示用户在 Agent 绑定正确的数据源工具，而不是让用户手填连接信息。\n")
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
	return b
}
