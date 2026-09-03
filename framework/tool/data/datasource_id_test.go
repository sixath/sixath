package tooldata

import (
	"strings"
	"testing"

	"github.com/sixath/framework/datasource"
)

func TestResolveDatasourceID_ModelLiteralDefaultRemapsToConfigured(t *testing.T) {
	reg := datasource.NewRegistry()
	datasource.RegisterNoop(reg)
	if _, err := reg.Register(datasource.Config{ID: "main", Type: "noop"}); err != nil {
		t.Fatal(err)
	}
	got := ResolveDatasourceID(map[string]any{"datasource_id": "default"}, "main", reg)
	if got != "main" {
		t.Fatalf("got %q want main", got)
	}
}

func TestResolveDatasourceID_KeepWhenDefaultIdExistsInRegistry(t *testing.T) {
	reg := datasource.NewRegistry()
	datasource.RegisterNoop(reg)
	if _, err := reg.Register(datasource.Config{ID: "default", Type: "noop"}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Register(datasource.Config{ID: "main", Type: "noop"}); err != nil {
		t.Fatal(err)
	}
	got := ResolveDatasourceID(map[string]any{"datasource_id": "default"}, "main", reg)
	if got != "default" {
		t.Fatalf("got %q want default (literal id registered)", got)
	}
}

func TestResolveDatasourceID_NoRegistryFallback(t *testing.T) {
	got := ResolveDatasourceID(map[string]any{"datasource_id": "default"}, "main", nil)
	if got != "main" {
		t.Fatalf("got %q want main", got)
	}
}

func TestRejectElasticsearchDatasource(t *testing.T) {
	reg := datasource.NewRegistry()
	datasource.RegisterElasticsearch(reg)
	if _, err := reg.Register(datasource.Config{ID: "es1", Type: "elasticsearch", DSN: "http://127.0.0.1:9200"}); err != nil {
		t.Fatal(err)
	}
	err := RejectElasticsearchDatasource(reg, "es1", "execute_read")
	if err == nil || !strings.Contains(err.Error(), "不支持 Elasticsearch") {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(err.Error(), "cluster=") {
		t.Fatalf("reject message should mention cluster=: %v", err)
	}
	datasource.RegisterNoop(reg)
	if _, err := reg.Register(datasource.Config{ID: "n1", Type: "noop"}); err != nil {
		t.Fatal(err)
	}
	if err := RejectElasticsearchDatasource(reg, "n1", "execute_read"); err != nil {
		t.Fatalf("noop should pass: %v", err)
	}
}
