package chat

import (
	"context"
	"strings"
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/tool"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestApplyPortalToolDescriptions(t *testing.T) {
	reg := tool.NewEmptyRegistry()
	noop := func(context.Context, map[string]any) (any, error) { return nil, nil }
	for _, name := range []string{"vm_run_cmd", "rca_grep", "rca_glob", "rca_read"} {
		if err := reg.Register(tool.Tool{Name: name, Description: "base " + name, Execute: noop}); err != nil {
			t.Fatal(err)
		}
	}
	vmCfg, _ := structpb.NewStruct(map[string]any{"rca": map[string]any{"func_path": "vm_run_cmd"}})
	codeCfg, _ := structpb.NewStruct(map[string]any{"rca": map[string]any{"func_path": "rca_code"}})
	tools := []*biz.ToolMeta{
		{Name: "vm_tool", Type: biz.ToolTypeRCA, Description: "实例 runCmd 端口 53000，日志在 D:\\logs", Config: vmCfg},
		{Name: "code", Type: biz.ToolTypeRCA, Description: "cloudgame 源码", Config: codeCfg},
		{Name: "empty", Type: biz.ToolTypeRCA, Description: "  ", Config: vmCfg},
		{Name: "ds", Type: biz.ToolTypeDatasource, Description: "should be ignored", Config: vmCfg},
	}
	applyPortalToolDescriptions(reg, tools)

	vm, _ := reg.Get("vm_run_cmd")
	if !strings.HasPrefix(vm.Description, "base vm_run_cmd") || !strings.Contains(vm.Description, "部署说明（vm_tool）：实例 runCmd 端口 53000") {
		t.Fatalf("vm_run_cmd description=%q", vm.Description)
	}
	if strings.Contains(vm.Description, "should be ignored") {
		t.Fatal("non-RCA tool description must not be applied")
	}
	for _, name := range []string{"rca_grep", "rca_glob", "rca_read"} {
		got, _ := reg.Get(name)
		if !strings.Contains(got.Description, "cloudgame 源码") {
			t.Fatalf("%s description=%q", name, got.Description)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("你好世界", 2); got != "你好…" {
		t.Fatalf("got %q", got)
	}
	if got := truncateRunes("abc", 5); got != "abc" {
		t.Fatalf("got %q", got)
	}
}
