package chat

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/tool"
	"google.golang.org/protobuf/types/known/structpb"
)

// E5 RCA agent binding acceptance (Phase 1).
// Checklist: ToolTypeRCA valid; ValidRCAFuncPath allowlist; BuildRegistry registers
// jaeger_trace / es_log_query / rca_*; List / ListForAPI expose those names when configured.
// Unit coverage also lives in rca_builder_test.go and biz/tool_test.go.
// Web UI already supports type=rca in web/src/pages/ToolForm.tsx (no frontend work here).

func registryNameSet(tools []tool.Tool) map[string]bool {
	m := make(map[string]bool, len(tools))
	for _, tl := range tools {
		m[tl.Name] = true
	}
	return m
}

func TestE5_RCABindingAcceptance(t *testing.T) {
	t.Run("1_ToolTypeRCA_is_valid", func(t *testing.T) {
		if biz.ToolTypeRCA != "rca" {
			t.Fatalf("ToolTypeRCA=%q want %q", biz.ToolTypeRCA, "rca")
		}
		if !biz.IsValidToolType(string(biz.ToolTypeRCA)) {
			t.Fatal("ToolTypeRCA must be a valid tool type")
		}
	})

	t.Run("2_ValidRCAFuncPath_allowlist", func(t *testing.T) {
		for _, fp := range []string{"rca_code", "rca_symbol", "jaeger_trace", "es_log_query"} {
			if !biz.ValidRCAFuncPath(fp) {
				t.Fatalf("%q must be accepted", fp)
			}
		}
		for _, fp := range []string{"", "rca_grep", "rca_glob", "rca_read", "unknown", "nope"} {
			if biz.ValidRCAFuncPath(fp) {
				t.Fatalf("%q must be rejected", fp)
			}
		}
	})

	t.Run("3_BuildRegistry_registers_expected_tools", func(t *testing.T) {
		ws := t.TempDir()
		if err := os.Mkdir(filepath.Join(ws, WorkspaceCodeLink), 0o755); err != nil {
			t.Fatal(err)
		}
		jaeger, _ := structpb.NewStruct(map[string]any{
			"rca": map[string]any{"func_path": "jaeger_trace", "query_url": "http://j:16686"},
		})
		code, _ := structpb.NewStruct(map[string]any{
			"rca": map[string]any{"func_path": "rca_code", "roots": []any{"/repos/a"}},
		})
		esDS, _ := structpb.NewStruct(map[string]any{
			"datasource": map[string]any{"id": "es-logs", "type": "elasticsearch", "dsn": "http://localhost:9200"},
		})
		esLog, _ := structpb.NewStruct(map[string]any{
			"rca": map[string]any{
				"func_path":      "es_log_query",
				"datasource_id":  "es-logs",
				"default_index":  "app-*",
				"trace_id_field": "trace_id",
			},
		})
		tools := []*biz.ToolMeta{
			{Name: "rca-jaeger", Type: biz.ToolTypeRCA, Config: jaeger},
			{Name: "rca-code", Type: biz.ToolTypeRCA, Config: code},
			{Name: "es-logs", Type: biz.ToolTypeDatasource, Config: esDS},
			{Name: "rca-es", Type: biz.ToolTypeRCA, Config: esLog},
		}
		reg := tool.NewRegistry()
		if _, err := BuildRegistry(tools, nil, reg, RegistryBuildOptions{Workspace: ws}); err != nil {
			t.Fatalf("BuildRegistry: %v", err)
		}
		want := []string{"jaeger_trace", "es_log_query", "rca_grep", "rca_glob", "rca_read"}
		for _, n := range want {
			if !rcaHas(reg, n) {
				t.Fatalf("expected %s registered after BuildRegistry", n)
			}
		}
	})

	t.Run("4_List_and_ListForAPI_expose_names", func(t *testing.T) {
		ws := t.TempDir()
		if err := os.Mkdir(filepath.Join(ws, WorkspaceCodeLink), 0o755); err != nil {
			t.Fatal(err)
		}
		jaeger, _ := structpb.NewStruct(map[string]any{
			"rca": map[string]any{"func_path": "jaeger_trace", "query_url": "http://j:16686"},
		})
		code, _ := structpb.NewStruct(map[string]any{
			"rca": map[string]any{"func_path": "rca_code", "roots": []any{"/repos/a"}},
		})
		esDS, _ := structpb.NewStruct(map[string]any{
			"datasource": map[string]any{"id": "es-logs", "type": "elasticsearch", "dsn": "http://localhost:9200"},
		})
		esLog, _ := structpb.NewStruct(map[string]any{
			"rca": map[string]any{
				"func_path":      "es_log_query",
				"datasource_id":  "es-logs",
				"default_index":  "app-*",
				"trace_id_field": "trace_id",
			},
		})
		tools := []*biz.ToolMeta{
			{Name: "rca-jaeger", Type: biz.ToolTypeRCA, Config: jaeger},
			{Name: "rca-code", Type: biz.ToolTypeRCA, Config: code},
			{Name: "es-logs", Type: biz.ToolTypeDatasource, Config: esDS},
			{Name: "rca-es", Type: biz.ToolTypeRCA, Config: esLog},
		}
		reg := tool.NewRegistry()
		if _, err := BuildRegistry(tools, nil, reg, RegistryBuildOptions{Workspace: ws}); err != nil {
			t.Fatalf("BuildRegistry: %v", err)
		}

		want := []string{"jaeger_trace", "es_log_query", "rca_grep", "rca_glob", "rca_read"}
		listNames := registryNameSet(reg.List())
		apiNames := registryNameSet(reg.ListForAPI(context.Background(), nil))
		for _, n := range want {
			if !listNames[n] {
				t.Fatalf("List missing %q", n)
			}
			if !apiNames[n] {
				t.Fatalf("ListForAPI missing %q", n)
			}
		}
	})
}
