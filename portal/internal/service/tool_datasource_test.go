package service

import (
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
)

func TestToolConfigRoundTrip_DatasourceESMetadata(t *testing.T) {
	in, err := structpb.NewStruct(map[string]any{
		"datasource": map[string]any{
			"id":             "zj-elk",
			"type":           "elasticsearch",
			"dsn":            "http://es:9200",
			"default_index":  "app-*",
			"trace_id_field": "trace_id",
			"purpose":        "应用日志",
		},
	})
	if err != nil {
		t.Fatalf("structpb: %v", err)
	}
	proto := structToToolConfig(in)
	if proto.Datasource == nil {
		t.Fatal("proto.Datasource must be populated from struct")
	}
	if proto.Datasource.Id != "zj-elk" || proto.Datasource.Type != "elasticsearch" ||
		proto.Datasource.Dsn != "http://es:9200" || proto.Datasource.DefaultIndex != "app-*" ||
		proto.Datasource.TraceIdField != "trace_id" || proto.Datasource.Purpose != "应用日志" {
		t.Fatalf("proto.Datasource fields wrong: %+v", proto.Datasource)
	}
	back := protoToolConfigToStruct(proto)
	dsVal, ok := back.Fields["datasource"]
	if !ok || dsVal.GetStructValue() == nil {
		t.Fatal("round-trip back to struct must contain datasource")
	}
	f := dsVal.GetStructValue().GetFields()
	if f["default_index"].GetStringValue() != "app-*" {
		t.Fatalf("round-trip default_index wrong: %v", f["default_index"])
	}
	if f["trace_id_field"].GetStringValue() != "trace_id" {
		t.Fatalf("round-trip trace_id_field wrong: %v", f["trace_id_field"])
	}
	if f["purpose"].GetStringValue() != "应用日志" {
		t.Fatalf("round-trip purpose wrong: %v", f["purpose"])
	}
}

func TestToolConfigRoundTrip_DatasourceESCamelCase(t *testing.T) {
	in, err := structpb.NewStruct(map[string]any{
		"datasource": map[string]any{
			"type":         "elasticsearch",
			"defaultIndex": "app-*",
			"traceIdField": "trace_id",
			"purpose":      "应用日志",
		},
	})
	if err != nil {
		t.Fatalf("structpb: %v", err)
	}
	proto := structToToolConfig(in)
	if proto.Datasource == nil {
		t.Fatal("proto.Datasource must be populated from camelCase keys")
	}
	if proto.Datasource.DefaultIndex != "app-*" || proto.Datasource.TraceIdField != "trace_id" ||
		proto.Datasource.Purpose != "应用日志" {
		t.Fatalf("camelCase decode wrong: %+v", proto.Datasource)
	}
	back := protoToolConfigToStruct(proto)
	f := back.Fields["datasource"].GetStructValue().GetFields()
	if f["default_index"].GetStringValue() != "app-*" || f["trace_id_field"].GetStringValue() != "trace_id" {
		t.Fatalf("camelCase round-trip must write snake_case keys: %v", f)
	}
}

func TestToolConfigRoundTrip_DatasourceESWithoutDSN(t *testing.T) {
	in, err := structpb.NewStruct(map[string]any{
		"datasource": map[string]any{
			"type":          "elasticsearch",
			"default_index": "app-*",
			"purpose":       "应用日志",
		},
	})
	if err != nil {
		t.Fatalf("structpb: %v", err)
	}
	proto := structToToolConfig(in)
	if proto.Datasource == nil {
		t.Fatal("type+purpose+index without dsn must decode")
	}
	if proto.Datasource.Dsn != "" {
		t.Fatalf("dsn should stay empty: %q", proto.Datasource.Dsn)
	}
	back := protoToolConfigToStruct(proto)
	dsVal, ok := back.Fields["datasource"]
	if !ok || dsVal.GetStructValue() == nil {
		t.Fatal("type+purpose+index without dsn must persist")
	}
	f := dsVal.GetStructValue().GetFields()
	if f["type"].GetStringValue() != "elasticsearch" || f["default_index"].GetStringValue() != "app-*" ||
		f["purpose"].GetStringValue() != "应用日志" {
		t.Fatalf("persisted fields wrong: %v", f)
	}
}
