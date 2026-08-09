package chat

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sixath/framework/tool"
)

const catalogSummaryThreshold = 15

var toolsetLabels = map[string]string{
	tool.ToolsetFile:          "数据",
	tool.ToolsetCore:          "出站",
	tool.ToolsetMCP:           "外部",
	tool.ToolsetWeb:           "联网",
	tool.ToolsetSkills:        "技能",
	tool.ToolsetMemory:        "记忆",
	tool.ToolsetSessionSearch: "会话",
	tool.ToolsetTerminal:      "终端",
	tool.ToolsetTodo:          "待办",
	tool.ToolsetCronjob:       "计划",
}

var toolsetOrder = []string{
	tool.ToolsetFile,
	tool.ToolsetCore,
	tool.ToolsetWeb,
	tool.ToolsetSkills,
	tool.ToolsetMCP,
	tool.ToolsetMemory,
	tool.ToolsetSessionSearch,
	tool.ToolsetTerminal,
	tool.ToolsetTodo,
	tool.ToolsetCronjob,
}

// FormatToolCatalogPrompt 生成置顶 system 块，反映当前 Agent 可用工具面。
func FormatToolCatalogPrompt(cat tool.ToolCatalog) string {
	available := filterAvailableEntries(cat.Entries)
	if len(available) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## 可用工具目录（共 %d 个，均已配置就绪，勿向用户索取已有凭据）\n", len(available))
	writePinnedBindingsSection(&b, available)

	if len(available) > catalogSummaryThreshold {
		writeToolsetSummary(&b, available)
		b.WriteString("\n可用工具较多，请用 list_tools 或 tool_search 查询详情。")
		b.WriteString("\n禁止在回复中向用户索取已在上方「已绑定能力」列出的数据库连接信息或企微 Webhook。")
		return strings.TrimSpace(b.String())
	}

	grouped := groupEntriesByToolset(available)
	for _, ts := range sortedToolsets(grouped) {
		writeToolsetHeader(&b, ts)
		writeToolsetEntries(&b, grouped[ts])
	}
	return strings.TrimSpace(b.String())
}

func writePinnedBindingsSection(b *strings.Builder, entries []tool.ToolCatalogEntry) {
	pinned := pinnedBindingEntries(entries)
	if len(pinned) == 0 {
		return
	}
	b.WriteString("\n### 已绑定能力（优先使用，禁止索要凭据）\n")
	for _, e := range pinned {
		line := "- " + e.Name
		if summary := formatBindingSummary(e.Bindings); summary != "" {
			line += " — " + summary
		}
		fmt.Fprintln(b, line)
	}
	if hasBoundTool(pinned, "execute_read") && hasBoundTool(pinned, "send_to_wecom") {
		b.WriteString("- 典型流程：execute_read(datasource_id=…) 查库统计 → send_to_wecom(content=…) 推送企微\n")
	}
	b.WriteString("- 禁止通过 ask_user 或纯文本向用户索取 host/端口/账号/密码/连接串/Webhook URL\n")
}

func pinnedBindingEntries(entries []tool.ToolCatalogEntry) []tool.ToolCatalogEntry {
	out := make([]tool.ToolCatalogEntry, 0)
	for _, e := range entries {
		if len(e.Bindings) > 0 {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func hasBoundTool(entries []tool.ToolCatalogEntry, name string) bool {
	for _, e := range entries {
		if e.Name == name {
			return true
		}
	}
	return false
}

func filterAvailableEntries(entries []tool.ToolCatalogEntry) []tool.ToolCatalogEntry {
	out := make([]tool.ToolCatalogEntry, 0, len(entries))
	for _, e := range entries {
		if e.Available {
			out = append(out, e)
		}
	}
	return out
}

func groupEntriesByToolset(entries []tool.ToolCatalogEntry) map[string][]tool.ToolCatalogEntry {
	grouped := make(map[string][]tool.ToolCatalogEntry)
	for _, e := range entries {
		ts := e.Toolset
		if ts == "" {
			ts = "_ungrouped"
		}
		grouped[ts] = append(grouped[ts], e)
	}
	return grouped
}

func sortedToolsets(grouped map[string][]tool.ToolCatalogEntry) []string {
	known := make(map[string]struct{}, len(toolsetOrder))
	ordered := make([]string, 0, len(grouped))
	for _, ts := range toolsetOrder {
		if _, ok := grouped[ts]; ok {
			ordered = append(ordered, ts)
			known[ts] = struct{}{}
		}
	}
	rest := make([]string, 0)
	for ts := range grouped {
		if _, ok := known[ts]; !ok {
			rest = append(rest, ts)
		}
	}
	sort.Strings(rest)
	return append(ordered, rest...)
}

func writeToolsetHeader(b *strings.Builder, toolset string) {
	label := toolsetLabel(toolset)
	if toolset == "_ungrouped" {
		fmt.Fprintf(b, "### %s\n", label)
		return
	}
	fmt.Fprintf(b, "### %s [%s]\n", label, toolset)
}

func toolsetLabel(toolset string) string {
	if label, ok := toolsetLabels[toolset]; ok {
		return label
	}
	if toolset == "_ungrouped" {
		return "其它"
	}
	return toolset
}

func writeToolsetSummary(b *strings.Builder, entries []tool.ToolCatalogEntry) {
	grouped := groupEntriesByToolset(entries)
	for _, ts := range sortedToolsets(grouped) {
		label := toolsetLabel(ts)
		if ts == "_ungrouped" {
			fmt.Fprintf(b, "### %s — %d 个\n", label, len(grouped[ts]))
			continue
		}
		fmt.Fprintf(b, "### %s [%s] — %d 个\n", label, ts, len(grouped[ts]))
	}
}

func writeToolsetEntries(b *strings.Builder, entries []tool.ToolCatalogEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	type lineGroup struct {
		names    []string
		binding  string
		deferred bool
	}
	byBinding := make(map[string]*lineGroup)
	order := make([]string, 0)

	for _, e := range entries {
		binding := formatBindingSummary(e.Bindings)
		key := binding
		if e.Deferred {
			key += "|deferred"
		}
		g, ok := byBinding[key]
		if !ok {
			g = &lineGroup{binding: binding, deferred: e.Deferred}
			byBinding[key] = g
			order = append(order, key)
		}
		g.names = append(g.names, e.Name)
	}

	for _, key := range order {
		g := byBinding[key]
		sort.Strings(g.names)
		line := "- " + strings.Join(g.names, " / ")
		if g.binding != "" {
			line += " — " + g.binding
		}
		if g.deferred {
			line += "（延迟加载，先用 tool_search）"
		}
		fmt.Fprintln(b, line)
	}
}

func formatBindingSummary(bindings map[string]string) string {
	if len(bindings) == 0 {
		return ""
	}

	if id := bindings["datasource_id"]; id != "" {
		details := datasourceBindingDetails(bindings)
		if details != "" {
			return fmt.Sprintf("已绑定 %s (%s)", id, details)
		}
		return fmt.Sprintf("已绑定 %s", id)
	}

	if channelType := bindings["channel_type"]; channelType != "" {
		channelID := bindings["channel_id"]
		if channelType == "wecom" && channelID != "" {
			return fmt.Sprintf("已绑定企微群 %s", channelID)
		}
		if channelID != "" {
			return fmt.Sprintf("已绑定 %s (%s)", channelID, channelType)
		}
	}

	if server := bindings["mcp_server"]; server != "" {
		return fmt.Sprintf("服务器 %s", server)
	}

	vals := make([]string, 0, len(bindings))
	for _, v := range bindings {
		if v != "" {
			vals = append(vals, v)
		}
	}
	sort.Strings(vals)
	if len(vals) == 0 {
		return ""
	}
	return "已绑定 " + strings.Join(vals, ", ")
}

func datasourceBindingDetails(bindings map[string]string) string {
	typ := bindings["type"]
	db := bindings["db_name"]
	switch {
	case typ != "" && db != "":
		return typ + "/" + db
	case typ != "":
		return typ
	case db != "":
		return db
	default:
		return ""
	}
}
