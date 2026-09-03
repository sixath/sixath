package biz

import (
	"context"
	"errors"
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
	if !errors.Is(err, ErrRCAESLogUseDatasource) {
		t.Fatalf("create RCA es_log_query: %v", err)
	}

	err = ValidateCreateRCAESLog(ToolTypeRCA, mustStruct(t, map[string]any{
		"rca": map[string]any{"func_path": "  es_log_query  ", "endpoint": "http://es:9200"},
	}))
	if !errors.Is(err, ErrRCAESLogUseDatasource) {
		t.Fatalf("trimmed func_path must fail: %v", err)
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
	if !errors.Is(err, ErrESDatasourceMissingMeta) {
		t.Fatalf("missing default_index and purpose: %v", err)
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

	onlyIndex := ValidateElasticsearchDatasource(ToolTypeDatasource, mustStruct(t, map[string]any{
		"datasource": map[string]any{"type": "elasticsearch", "default_index": "app-*"},
	}))
	if !errors.Is(onlyIndex, ErrESDatasourceMissingMeta) {
		t.Fatalf("only default_index: %v", onlyIndex)
	}
	onlyPurpose := ValidateElasticsearchDatasource(ToolTypeDatasource, mustStruct(t, map[string]any{
		"datasource": map[string]any{"type": "elasticsearch", "purpose": "应用日志"},
	}))
	if !errors.Is(onlyPurpose, ErrESDatasourceMissingMeta) {
		t.Fatalf("only purpose: %v", onlyPurpose)
	}
	whitespace := ValidateElasticsearchDatasource(ToolTypeDatasource, mustStruct(t, map[string]any{
		"datasource": map[string]any{"type": "elasticsearch", "default_index": "  ", "purpose": "  "},
	}))
	if !errors.Is(whitespace, ErrESDatasourceMissingMeta) {
		t.Fatalf("whitespace-only: %v", whitespace)
	}
}

func seedOwnedTool(tools *fakeToolACLRepo, resources *fakeToolResourceRepo, tool *ToolMeta, owner string) {
	tools.tools[tool.ID] = tool
	resource := &Resource{
		ID: "resource-" + tool.ID, Type: ResourceTypeTool, Name: tool.Name,
		PayloadRef: tool.ID, OwnerUserID: owner, Visibility: VisibilityPrivate,
	}
	resources.resources[resource.ID] = resource
	resources.byPayload["tool:"+tool.ID] = resource
}

func TestToolUsecaseCreateRejectsRCAESLogQuery(t *testing.T) {
	uc, _, _ := newToolACLUsecase()
	ctx := WithCallerUserID(context.Background(), "user-1")
	_, err := uc.Create(ctx, "elk", "", string(ToolTypeRCA), mustStruct(t, map[string]any{
		"rca": map[string]any{"func_path": "es_log_query", "endpoint": "http://es:9200"},
	}))
	if !errors.Is(err, ErrRCAESLogUseDatasource) {
		t.Fatalf("Create RCA es_log_query: %v", err)
	}
}

func TestToolUsecaseCreateRejectsESDatasourceMissingMeta(t *testing.T) {
	uc, _, _ := newToolACLUsecase()
	ctx := WithCallerUserID(context.Background(), "user-1")
	_, err := uc.Create(ctx, "es", "", string(ToolTypeDatasource), mustStruct(t, map[string]any{
		"datasource": map[string]any{"type": "elasticsearch", "dsn": "http://es:9200"},
	}))
	if !errors.Is(err, ErrESDatasourceMissingMeta) {
		t.Fatalf("Create ES without index/purpose: %v", err)
	}
}

func TestToolUsecaseUpdateExistingRCAESLogQuery(t *testing.T) {
	uc, tools, resources := newToolACLUsecase()
	seedOwnedTool(tools, resources, &ToolMeta{
		ID: "legacy-es", Name: "legacy-es", Type: ToolTypeRCA,
		Config: mustStruct(t, map[string]any{
			"rca": map[string]any{"func_path": "es_log_query", "endpoint": "http://es:9200"},
		}),
	}, "user-1")
	ctx := WithCallerUserID(context.Background(), "user-1")
	_, err := uc.Update(ctx, "legacy-es", nil, nil, nil, mustStruct(t, map[string]any{
		"rca": map[string]any{"func_path": "es_log_query", "endpoint": "http://es:9200"},
	}))
	if err != nil {
		t.Fatalf("Update existing RCA es_log_query: %v", err)
	}
}

func TestToolUsecaseUpdateRejectsMintingRCAESLogQuery(t *testing.T) {
	uc, tools, resources := newToolACLUsecase()
	seedOwnedTool(tools, resources, &ToolMeta{
		ID: "jaeger", Name: "jaeger", Type: ToolTypeRCA,
		Config: mustStruct(t, map[string]any{
			"rca": map[string]any{"func_path": "jaeger_trace", "query_url": "http://jaeger:16686"},
		}),
	}, "user-1")
	ctx := WithCallerUserID(context.Background(), "user-1")
	_, err := uc.Update(ctx, "jaeger", nil, nil, nil, mustStruct(t, map[string]any{
		"rca": map[string]any{"func_path": "es_log_query", "endpoint": "http://es:9200"},
	}))
	if !errors.Is(err, ErrRCAESLogUseDatasource) {
		t.Fatalf("Update jaeger → es_log_query: %v, want ErrRCAESLogUseDatasource", err)
	}
}
