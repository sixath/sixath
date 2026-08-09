package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/sixath/framework/datasource"
)

type noopDSForTest struct {
	id, typ string
}

func (n *noopDSForTest) ID() string                 { return n.id }
func (n *noopDSForTest) Type() string               { return n.typ }
func (n *noopDSForTest) Ping(context.Context) error { return nil }
func (n *noopDSForTest) Close() error               { return nil }

type stubExecutor struct {
	typ string
}

func (s *stubExecutor) Execute(ctx context.Context, datasourceID string, dsl string, opts ExecuteOptions) (*Result, error) {
	return &Result{Columns: []string{s.typ}}, nil
}

func TestExecutorRegistry_RoutesByType(t *testing.T) {
	reg := datasource.NewRegistry()
	datasource.RegisterNoop(reg)
	reg.RegisterType("clickhouse", func(cfg datasource.Config) (datasource.DataSource, error) {
		return &noopDSForTest{id: cfg.ID, typ: "clickhouse"}, nil
	})
	execReg := NewExecutorRegistry(reg)
	execReg.Register("clickhouse", &stubExecutor{typ: "clickhouse"})
	if _, err := reg.Register(datasource.Config{ID: "ds1", Type: "clickhouse"}); err != nil {
		t.Fatal(err)
	}
	res, err := execReg.Execute(context.Background(), "ds1", "SELECT 1", ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Columns) != 1 || res.Columns[0] != "clickhouse" {
		t.Fatalf("expected clickhouse executor, got %+v", res)
	}
}

func TestExecutorRegistry_UnsupportedType(t *testing.T) {
	reg := datasource.NewRegistry()
	datasource.RegisterNoop(reg)
	execReg := NewExecutorRegistry(reg)
	if _, err := reg.Register(datasource.Config{ID: "ds1", Type: "noop"}); err != nil {
		t.Fatal(err)
	}
	_, err := execReg.Execute(context.Background(), "ds1", "SELECT 1", ExecuteOptions{})
	if !errors.Is(err, ErrUnsupportedDataSource) {
		t.Fatalf("expected ErrUnsupportedDataSource, got %v", err)
	}
}

func TestExecutorRegistry_NotFound(t *testing.T) {
	reg := datasource.NewRegistry()
	execReg := NewExecutorRegistry(reg)
	_, err := execReg.Execute(context.Background(), "missing", "SELECT 1", ExecuteOptions{})
	if !errors.Is(err, datasource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestBundle_MySQLWriterOnly(t *testing.T) {
	reg := datasource.NewRegistry()
	b := NewBundle(reg)
	if b.Writer == nil || b.Reader == nil {
		t.Fatal("bundle should expose reader and writer")
	}
}
