package metadata

import (
	"context"
	"testing"

	"github.com/sixath/framework/datasource"
)

func TestRegistry_ResolveFetcher_noopUnsupported(t *testing.T) {
	reg := datasource.NewRegistry()
	datasource.RegisterNoop(reg)
	meta := NewRegistry(reg)
	if _, err := reg.Register(datasource.Config{ID: "ds1", Type: "noop"}); err != nil {
		t.Fatal(err)
	}
	_, err := meta.ResolveFetcher("ds1")
	if err != ErrUnsupportedDataSource {
		t.Fatalf("noop should be unsupported for metadata: %v", err)
	}
}

func TestRefreshWithRegistry_Unsupported(t *testing.T) {
	reg := datasource.NewRegistry()
	datasource.RegisterNoop(reg)
	store := NewInMemoryStore(nil)
	if _, err := reg.Register(datasource.Config{ID: "ds1", Type: "noop"}); err != nil {
		t.Fatal(err)
	}
	_, err := RefreshWithRegistry(context.Background(), NewRegistry(reg), store, "ds1")
	if err != ErrUnsupportedDataSource {
		t.Fatalf("got %v", err)
	}
}
