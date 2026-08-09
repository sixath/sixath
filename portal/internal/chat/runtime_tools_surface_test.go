package chat

import (
	"testing"

	"github.com/sixath/framework/tool"
)

func TestRegisterAgentRuntimeTools_SkipsWebWhenInactive(t *testing.T) {
	reg := tool.NewRegistry()
	flags := HermesP0ToolFlags{WebToolsEnabled: true, TodoEnabled: true}
	err := RegisterAgentRuntimeTools(reg, AgentRuntimeToolsOptions{
		Flags:          &flags,
		ActiveFamilies: familySet([]string{FamilyCore, "mcp:gitlab"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("web_search"); ok {
		t.Fatal("web_search should be skipped")
	}
	if _, ok := reg.Get("todo"); !ok {
		t.Fatal("todo should register as core runtime tool")
	}
}
