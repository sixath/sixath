package tooldata

import (
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
