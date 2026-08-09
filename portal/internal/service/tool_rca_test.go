package service

import (
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
)

func TestToolConfigRoundTrip_RCA(t *testing.T) {
	// struct (as stored/received) -> proto -> struct, rca must survive both directions
	in, err := structpb.NewStruct(map[string]any{
		"rca": map[string]any{
			"func_path":      "es_log_query",
			"roots":          []any{"/repos/a"},
			"query_url":      "http://j:16686",
			"datasource_id":  "es-logs",
			"default_index":  "app-*",
			"trace_id_field": "trace_id",
		},
	})
	if err != nil {
		t.Fatalf("structpb: %v", err)
	}
	proto := structToToolConfig(in)
	if proto.Rca == nil {
		t.Fatal("proto.Rca must be populated from struct")
	}
	if proto.Rca.FuncPath != "es_log_query" || proto.Rca.DatasourceId != "es-logs" ||
		proto.Rca.DefaultIndex != "app-*" || proto.Rca.TraceIdField != "trace_id" ||
		proto.Rca.QueryUrl != "http://j:16686" {
		t.Fatalf("proto.Rca fields wrong: %+v", proto.Rca)
	}
	if len(proto.Rca.Roots) != 1 || proto.Rca.Roots[0] != "/repos/a" {
		t.Fatalf("proto.Rca.Roots wrong: %v", proto.Rca.Roots)
	}
	back := protoToolConfigToStruct(proto)
	rcaVal, ok := back.Fields["rca"]
	if !ok || rcaVal.GetStructValue() == nil {
		t.Fatal("round-trip back to struct must contain rca")
	}
	f := rcaVal.GetStructValue().GetFields()
	if f["func_path"].GetStringValue() != "es_log_query" {
		t.Fatalf("round-trip func_path wrong: %v", f["func_path"])
	}
}
