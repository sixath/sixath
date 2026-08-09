package chat

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sixath/framework/tool"
)

func TestFormatToolCatalogPrompt_GroupsByToolset(t *testing.T) {
	cat := tool.ToolCatalog{Entries: []tool.ToolCatalogEntry{
		{Name: "execute_read", Toolset: tool.ToolsetFile, Available: true,
			Bindings: map[string]string{"datasource_id": "prod_mysql", "type": "mysql", "db_name": "archive"}},
		{Name: "send_to_wecom", Toolset: tool.ToolsetCore, Available: true,
			Bindings: map[string]string{"channel_type": "wecom", "channel_id": "ops-alerts"}},
		{Name: "hidden_tool", Toolset: tool.ToolsetWeb, Available: false},
	}}
	p := FormatToolCatalogPrompt(cat)
	if !strings.Contains(p, "prod_mysql") || !strings.Contains(p, "send_to_wecom") {
		t.Fatalf("prompt missing bindings: %s", p)
	}
	if strings.Contains(p, "hidden_tool") {
		t.Fatalf("unavailable tool should be omitted: %s", p)
	}
	if !strings.Contains(p, "均已配置就绪，勿向用户索取已有凭据") {
		t.Fatalf("prompt missing readiness theme: %s", p)
	}
	if !strings.Contains(p, "数据 [file]") || !strings.Contains(p, "出站 [core]") {
		t.Fatalf("prompt should group by toolset with Chinese labels: %s", p)
	}
}

func TestFormatToolCatalogPrompt_SummaryWhenMany(t *testing.T) {
	entries := make([]tool.ToolCatalogEntry, 20)
	for i := range entries {
		entries[i] = tool.ToolCatalogEntry{Name: fmt.Sprintf("tool_%d", i), Toolset: "mcp", Available: true}
	}
	p := FormatToolCatalogPrompt(tool.ToolCatalog{Entries: entries})
	if !strings.Contains(p, "list_tools") {
		t.Fatal("large catalog should reference list_tools")
	}
	if !strings.Contains(p, "tool_search") {
		t.Fatal("large catalog should reference tool_search")
	}
	if strings.Contains(p, "tool_0") {
		t.Fatalf("summary mode should not list individual tools: %s", p)
	}
	if !strings.Contains(p, "外部 [mcp] — 20 个") {
		t.Fatalf("summary mode should show toolset counts: %s", p)
	}
}

func TestFormatToolCatalogPrompt_SummaryPinsBindings(t *testing.T) {
	entries := make([]tool.ToolCatalogEntry, 18)
	for i := range entries {
		entries[i] = tool.ToolCatalogEntry{Name: fmt.Sprintf("mcp__tool_%d", i), Toolset: tool.ToolsetMCP, Available: true}
	}
	entries = append(entries,
		tool.ToolCatalogEntry{Name: "execute_read", Toolset: tool.ToolsetFile, Available: true,
			Bindings: map[string]string{"datasource_id": "archive_mysql", "type": "mysql", "db_name": "archive"}},
		tool.ToolCatalogEntry{Name: "send_to_wecom", Toolset: tool.ToolsetCore, Available: true,
			Bindings: map[string]string{"channel_id": "ch-ops", "channel_type": "wecom"}},
	)
	p := FormatToolCatalogPrompt(tool.ToolCatalog{Entries: entries})
	for _, want := range []string{"已绑定能力", "archive_mysql", "send_to_wecom", "execute_read", "禁止索要凭据"} {
		if !strings.Contains(p, want) {
			t.Fatalf("summary mode should pin bindings, missing %q in:\n%s", want, p)
		}
	}
	if strings.Contains(p, "mcp__tool_0") {
		t.Fatalf("summary mode should not list bulk MCP tools: %s", p)
	}
}
