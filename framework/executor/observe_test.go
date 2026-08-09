package executor

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/sixath/framework/datasource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestClassifyErrorKind_FiniteLabels(t *testing.T) {
	allowed := map[string]bool{
		ErrorKindSchema: true, ErrorKindReadonly: true, ErrorKindTimeout: true,
		ErrorKindDriver: true, ErrorKindUnsupported: true, ErrorKindOther: true, "": true,
	}
	cases := []error{
		nil,
		ErrReadOnlyViolation,
		ErrUnsupportedDataSource,
		context.DeadlineExceeded,
		&SchemaRelatedError{Err: errors.New("schema")},
		fmt.Errorf("driver: %w", errors.New("boom")),
	}
	for _, err := range cases {
		k := ClassifyErrorKind(err)
		if !allowed[k] {
			t.Fatalf("unexpected error_kind %q for %v", k, err)
		}
	}
}

func TestBeginOp_SpanAttributes(t *testing.T) {
	sink := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(sink))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	otel.SetTracerProvider(tp)

	reg := datasource.NewRegistry()
	reg.RegisterType(datasource.TypeMySQL, func(cfg datasource.Config) (datasource.DataSource, error) {
		return &stubDS{id: cfg.ID, typ: datasource.TypeMySQL}, nil
	})
	if _, err := reg.Register(datasource.Config{ID: "ds1", Type: datasource.TypeMySQL}); err != nil {
		t.Fatalf("register: %v", err)
	}

	_, rec := beginOp(context.Background(), reg, "ds1", "query", 100, false)
	rec.finish(&Result{Rows: [][]any{{1}}, Truncated: true}, nil)

	spans := sink.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	got := map[string]bool{}
	for _, a := range spans[0].Attributes {
		got[string(a.Key)] = true
	}
	for _, key := range []string{
		"db.system", "db.datasource_id", "executor.op",
		"executor.max_rows", "executor.allow_write",
		"executor.rows_returned", "executor.truncated",
	} {
		if !got[key] {
			t.Errorf("missing attribute %q", key)
		}
	}
	if spans[0].Name != "Executor.query" {
		t.Errorf("span name = %q, want Executor.query", spans[0].Name)
	}
}

type stubDS struct {
	id, typ string
}

func (s *stubDS) ID() string                     { return s.id }
func (s *stubDS) Type() string                   { return s.typ }
func (s *stubDS) Ping(ctx context.Context) error { return nil }
func (s *stubDS) Close() error                   { return nil }
