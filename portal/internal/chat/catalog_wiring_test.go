package chat

import (
	"context"
	"slices"
	"testing"

	"github.com/sixath/framework/tool"
)

func TestCatalogWiring_BuildCatalogForAgent(t *testing.T) {
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	_ = RegisterCatalogTools(reg)

	cat := BuildCatalogForAgent(context.Background(), CatalogWiringInput{
		Reg: reg,
		DsBindings: []DatasourceBinding{{
			ID: "prod_mysql", Type: "mysql", DBName: "archive", Available: true,
		}},
		WecomChannelID: "ch-1",
		ChannelType:    "wecom",
	})

	names := make([]string, 0, len(cat.Entries))
	for _, e := range cat.Entries {
		names = append(names, e.Name)
	}
	if !slices.Contains(names, "list_tools") {
		t.Fatalf("catalog missing list_tools: %v", names)
	}
	if !slices.Contains(names, "calculator_add") {
		t.Fatalf("catalog missing calculator_add: %v", names)
	}

	var executeRead *tool.ToolCatalogEntry
	for i := range cat.Entries {
		if cat.Entries[i].Name == "execute_read" {
			executeRead = &cat.Entries[i]
			break
		}
	}
	if executeRead != nil && executeRead.Bindings["datasource_id"] != "prod_mysql" {
		t.Fatalf("execute_read datasource_id: got %q", executeRead.Bindings["datasource_id"])
	}
}
