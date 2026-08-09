package metadata

import (
	"context"
	"testing"

	"github.com/sixath/framework/datasource"
)

type stubFetcherDS struct {
	id string
}

func (d *stubFetcherDS) ID() string                     { return d.id }
func (d *stubFetcherDS) Type() string                   { return "stub" }
func (d *stubFetcherDS) Ping(ctx context.Context) error { return nil }
func (d *stubFetcherDS) Close() error                   { return nil }

func TestRefreshWithRegistry_TracksDatasourceID(t *testing.T) {
	reg := datasource.NewRegistry()
	reg.RegisterType("stub", func(cfg datasource.Config) (datasource.DataSource, error) {
		return &stubFetcherDS{id: cfg.ID}, nil
	})
	_, _ = reg.Register(datasource.Config{ID: "a", Type: "stub"})
	_, _ = reg.Register(datasource.Config{ID: "b", Type: "stub"})

	metaReg := NewRegistry(reg)
	metaReg.Register("stub", func(ds datasource.DataSource) (func(context.Context) (*Schema, error), error) {
		s := ds.(*stubFetcherDS)
		return func(ctx context.Context) (*Schema, error) {
			return &Schema{Name: s.id + "_db", Tables: []Table{{Name: "only_" + s.id}}}, nil
		}, nil
	})

	store := NewInMemoryStore(nil)
	ctx := context.Background()

	if _, err := RefreshWithRegistry(ctx, metaReg, store, "a"); err != nil {
		t.Fatal(err)
	}
	if store.CachedDatasourceID() != "a" {
		t.Fatalf("cached=%q", store.CachedDatasourceID())
	}
	s1, _ := store.GetSchema(ctx)
	if s1.Tables[0].Name != "only_a" {
		t.Fatalf("a tables: %+v", s1.Tables)
	}

	if _, err := RefreshWithRegistry(ctx, metaReg, store, "b"); err != nil {
		t.Fatal(err)
	}
	if store.CachedDatasourceID() != "b" {
		t.Fatalf("cached=%q want b", store.CachedDatasourceID())
	}
	s2, _ := store.GetSchema(ctx)
	if s2.Tables[0].Name != "only_b" {
		t.Fatalf("b tables: %+v", s2.Tables)
	}
}
