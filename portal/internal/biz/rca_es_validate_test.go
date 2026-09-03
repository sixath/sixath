package biz

import (
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
)

func mustStruct(t *testing.T, m map[string]any) *structpb.Struct {
	t.Helper()
	s, err := structpb.NewStruct(m)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestValidateRCAESLogConfig(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, endpoint, dsID string
		wantErr              bool
	}{
		{"both", "http://es:9200", "es-logs", true},
		{"neither", "", "", true},
		{"endpoint_only", "http://es:9200", "", false},
		{"ds_only", "", "es-logs", false},
		{"trim_spaces_both", "  ", "  ", true},
		{"endpoint_trim", "  http://es:9200  ", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRCAESLogConfig(tc.endpoint, tc.dsID)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected: %v", err)
			}
		})
	}
}

func TestRejectCreateRCAESLogQuery(t *testing.T) {
	err := ValidateCreateRCAESLog(ToolTypeRCA, mustStruct(t, map[string]any{
		"rca": map[string]any{"func_path": "es_log_query", "endpoint": "http://es:9200"},
	}))
	if err == nil {
		t.Fatal("create RCA es_log_query must fail")
	}

	if err := ValidateCreateRCAESLog(ToolTypeRCA, mustStruct(t, map[string]any{
		"rca": map[string]any{"func_path": "jaeger_trace", "query_url": "http://jaeger:16686"},
	})); err != nil {
		t.Fatalf("other RCA create must pass: %v", err)
	}
	if err := ValidateCreateRCAESLog(ToolTypeDatasource, mustStruct(t, map[string]any{
		"datasource": map[string]any{"type": "elasticsearch", "dsn": "http://es:9200"},
	})); err != nil {
		t.Fatalf("non-RCA create must pass: %v", err)
	}
}

func TestValidateElasticsearchDatasource(t *testing.T) {
	err := ValidateElasticsearchDatasource(ToolTypeDatasource, mustStruct(t, map[string]any{
		"datasource": map[string]any{"type": "elasticsearch", "dsn": "http://es:9200"},
	}))
	if err == nil {
		t.Fatal("missing default_index and purpose must fail")
	}
	err = ValidateElasticsearchDatasource(ToolTypeDatasource, mustStruct(t, map[string]any{
		"datasource": map[string]any{
			"type": "elasticsearch", "dsn": "http://es:9200",
			"default_index": "app-*", "purpose": "应用日志",
		},
	}))
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}

	if err := ValidateElasticsearchDatasource(ToolTypeDatasource, mustStruct(t, map[string]any{
		"datasource": map[string]any{"type": "mysql", "dsn": "mysql://localhost/db"},
	})); err != nil {
		t.Fatalf("mysql datasource must pass: %v", err)
	}
	if err := ValidateElasticsearchDatasource(ToolTypeDatasource, mustStruct(t, map[string]any{
		"datasource": map[string]any{"type": "es", "dsn": "http://es:9200"},
	})); err == nil {
		t.Fatal("type es missing default_index and purpose must fail")
	}
	if err := ValidateElasticsearchDatasource(ToolTypeBuiltin, mustStruct(t, map[string]any{})); err != nil {
		t.Fatalf("non-datasource must pass: %v", err)
	}
}
