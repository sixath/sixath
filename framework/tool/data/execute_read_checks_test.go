package tooldata

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/sixath/framework/datasource"
	"github.com/sixath/framework/executor"
	"github.com/sixath/framework/metadata"
	core "github.com/sixath/framework/tool"
)

type stubDS struct{ id, typ string }

func (s *stubDS) ID() string                 { return s.id }
func (s *stubDS) Type() string               { return s.typ }
func (s *stubDS) Ping(context.Context) error { return nil }
func (s *stubDS) Close() error               { return nil }

func newStubDSRegistry(t *testing.T, ids map[string]string) *datasource.Registry {
	t.Helper()
	reg := datasource.NewRegistry()
	for _, typ := range []string{datasource.TypeMySQL, datasource.TypeHive, datasource.TypeMongoDB, datasource.TypeElasticsearch} {
		typ := typ
		reg.RegisterType(typ, func(cfg datasource.Config) (datasource.DataSource, error) {
			return &stubDS{id: cfg.ID, typ: typ}, nil
		})
	}
	for id, typ := range ids {
		if _, err := reg.Register(datasource.Config{ID: id, Type: typ}); err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

// primedStore 让 store 缓存 dsID 的 schema（stub 数据源没有 DB()，内置 MySQL 拉取器无法刷新）。
func primedStore(t *testing.T, reg *datasource.Registry, dsID string, schema *metadata.Schema) *metadata.InMemoryStore {
	t.Helper()
	store := metadata.NewInMemoryStore(nil)
	metaReg := metadata.NewRegistry(reg)
	metaReg.Register(datasource.TypeMySQL, func(datasource.DataSource) (func(context.Context) (*metadata.Schema, error), error) {
		return func(context.Context) (*metadata.Schema, error) { return schema, nil }, nil
	})
	if _, err := metadata.RefreshWithRegistry(context.Background(), metaReg, store, dsID); err != nil {
		t.Fatal(err)
	}
	return store
}

func withSchemaRefresh(t *testing.T, fn func(context.Context, *datasource.Registry, *metadata.InMemoryStore, string) (*metadata.Schema, error)) {
	t.Helper()
	prev := sqlSchemaRefresh
	sqlSchemaRefresh = fn
	t.Cleanup(func() { sqlSchemaRefresh = prev })
}

type scriptReader struct {
	calls []string
	fn    func(dsl string) (*executor.QueryResult, error)
}

func (s *scriptReader) Query(_ context.Context, _ string, dsl string, _ executor.QueryOptions) (*executor.QueryResult, error) {
	s.calls = append(s.calls, dsl)
	return s.fn(dsl)
}

func oneRowReader() *scriptReader {
	return &scriptReader{fn: func(string) (*executor.QueryResult, error) {
		return &executor.QueryResult{Columns: []string{"x"}, Rows: [][]any{{int64(1)}}}, nil
	}}
}

func vmAssignSchema() *metadata.Schema {
	return &metadata.Schema{Tables: []metadata.Table{
		{Name: "vm_assign", Columns: []metadata.Column{{Name: "id"}, {Name: "state"}}},
		{Name: "pool_config", Columns: []metadata.Column{{Name: "id"}}},
	}}
}

func registerExecuteRead(t *testing.T, cfg *ExecuteReadConfig) core.Tool {
	t.Helper()
	r := core.NewRegistry()
	if err := RegisterExecuteReadTool(r, cfg); err != nil {
		t.Fatal(err)
	}
	tl, ok := r.Get("execute_read")
	if !ok {
		t.Fatal("execute_read not registered")
	}
	return tl
}

func wantInvalidArg(t *testing.T, err error, keyword string) core.SchemaError {
	t.Helper()
	var iae *core.InvalidArgumentsError
	if !errors.As(err, &iae) {
		t.Fatalf("want InvalidArgumentsError, got %v", err)
	}
	for _, e := range iae.Errors {
		if e.Keyword == keyword {
			return e
		}
	}
	t.Fatalf("want keyword %q, got %+v", keyword, iae.Errors)
	return core.SchemaError{}
}

func TestExecuteReadChecks(t *testing.T) {
	dsReg := newStubDSRegistry(t, map[string]string{"mysql1": "mysql", "mongo1": "mongodb", "hive1": "hive"})
	newCfg := func(r executor.Reader) *ExecuteReadConfig {
		return &ExecuteReadConfig{Reader: r, Registry: dsReg, DefaultDatasourceID: "mysql1"}
	}

	t.Run("query alias executes", func(t *testing.T) {
		r := oneRowReader()
		tl := registerExecuteRead(t, newCfg(r))
		if _, err := tl.Execute(context.Background(), map[string]any{"query": "SELECT 1", "datasource_id": "mysql1"}); err != nil {
			t.Fatal(err)
		}
		if len(r.calls) != 1 || r.calls[0] != "SELECT 1" {
			t.Fatalf("calls=%v", r.calls)
		}
	})

	t.Run("dsl required", func(t *testing.T) {
		r := oneRowReader()
		_, err := registerExecuteRead(t, newCfg(r)).Execute(context.Background(), map[string]any{"datasource_id": "mysql1"})
		wantInvalidArg(t, err, "required")
		if len(r.calls) != 0 {
			t.Fatal("must not execute")
		}
	})

	t.Run("lucene rejected on mysql", func(t *testing.T) {
		r := oneRowReader()
		_, err := registerExecuteRead(t, newCfg(r)).Execute(context.Background(), map[string]any{"dsl": "level:ERROR AND x:y", "datasource_id": "mysql1"})
		e := wantInvalidArg(t, err, core.KeywordReject)
		if !strings.Contains(e.Hint, "es_log_query") {
			t.Fatalf("hint=%q", e.Hint)
		}
		if len(r.calls) != 0 {
			t.Fatal("must not execute")
		}
	})

	t.Run("lucene rejected on hive", func(t *testing.T) {
		_, err := registerExecuteRead(t, newCfg(oneRowReader())).Execute(context.Background(), map[string]any{"dsl": "service:foo", "datasource_id": "hive1"})
		wantInvalidArg(t, err, core.KeywordReject)
	})

	t.Run("index rejected on mysql", func(t *testing.T) {
		_, err := registerExecuteRead(t, newCfg(oneRowReader())).Execute(context.Background(), map[string]any{"dsl": "SELECT 1", "index": "logs-*"})
		e := wantInvalidArg(t, err, core.KeywordReject)
		if e.Path != "index" {
			t.Fatalf("path=%q", e.Path)
		}
	})

	t.Run("mongodb json not rejected", func(t *testing.T) {
		r := oneRowReader()
		if _, err := registerExecuteRead(t, newCfg(r)).Execute(context.Background(), map[string]any{"dsl": `{"find":"c"}`, "datasource_id": "mongo1"}); err != nil {
			t.Fatal(err)
		}
		if len(r.calls) != 1 {
			t.Fatalf("calls=%v", r.calls)
		}
	})

	t.Run("datasource id candidates", func(t *testing.T) {
		r := oneRowReader()
		tl := registerExecuteRead(t, newCfg(r))
		_, err := tl.Execute(context.Background(), map[string]any{"dsl": "SELECT 1", "datasource_id": "mysq1"})
		e := wantInvalidArg(t, err, core.KeywordOneOf)
		if !slices.Contains(e.Candidates, "mysql1") {
			t.Fatalf("candidates=%v", e.Candidates)
		}
		if _, err := tl.Execute(context.Background(), map[string]any{"dsl": "SELECT 1", "datasource_id": "default"}); err != nil {
			t.Fatalf("default: %v", err)
		}
		if len(r.calls) != 1 {
			t.Fatalf("calls=%v", r.calls)
		}
	})

	t.Run("reader only executes", func(t *testing.T) {
		r := oneRowReader()
		cfg := &ExecuteReadConfig{Reader: r, DefaultDatasourceID: "ds1"}
		if _, err := registerExecuteRead(t, cfg).Execute(context.Background(), map[string]any{"dsl": "SELECT 1"}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("unknown column and table", func(t *testing.T) {
		r := oneRowReader()
		cfg := newCfg(r)
		cfg.Store = primedStore(t, dsReg, "mysql1", vmAssignSchema())
		tl := registerExecuteRead(t, cfg)
		refreshed := 0
		withSchemaRefresh(t, func(context.Context, *datasource.Registry, *metadata.InMemoryStore, string) (*metadata.Schema, error) {
			refreshed++
			s := vmAssignSchema()
			s.Tables[0].Columns = append(s.Tables[0].Columns, metadata.Column{Name: "created_at"})
			return s, nil
		})

		_, err := tl.Execute(context.Background(), map[string]any{"dsl": "SELECT id, stat FROM vm_assign"})
		e := wantInvalidArg(t, err, core.KeywordUnknownField)
		if e.Path != "dsl" || !slices.Contains(e.Candidates, "state") {
			t.Fatalf("column err=%+v", e)
		}

		_, err = tl.Execute(context.Background(), map[string]any{"dsl": "SELECT id FROM vm_asign"})
		e = wantInvalidArg(t, err, core.KeywordUnknownField)
		if e.Path != "dsl" || !slices.Contains(e.Candidates, "vm_assign") {
			t.Fatalf("table err=%+v", e)
		}

		if refreshed != 2 {
			t.Fatalf("unknown refs must force one refresh each, got %d", refreshed)
		}

		for _, ok := range []string{
			"SELECT id, state FROM vm_assign WHERE state = 3",
			"SELECT id FROM vm_assign ORDER BY created_at DESC",
			"SELECT name FROM otherdb.users",
			"SELECT a.id FROM vm_assign a JOIN pool_config p ON a.id = p.id",
		} {
			if _, err := tl.Execute(context.Background(), map[string]any{"dsl": ok}); err != nil {
				t.Fatalf("%q: %v", ok, err)
			}
		}
		if len(r.calls) != 4 {
			t.Fatalf("calls=%v", r.calls)
		}
	})

	t.Run("schema refresh failure fails open", func(t *testing.T) {
		r := oneRowReader()
		cfg := newCfg(r)
		cfg.Store = primedStore(t, dsReg, "mysql1", vmAssignSchema())
		withSchemaRefresh(t, func(context.Context, *datasource.Registry, *metadata.InMemoryStore, string) (*metadata.Schema, error) {
			return nil, errors.New("db down")
		})
		out, err := registerExecuteRead(t, cfg).Execute(context.Background(), map[string]any{"dsl": "SELECT id, stat FROM vm_assign"})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.calls) != 1 {
			t.Fatalf("calls=%v", r.calls)
		}
		if _, ok := out.(*executor.QueryResult); !ok {
			t.Fatalf("type %T", out)
		}
	})

	t.Run("schema check skipped on non-mysql", func(t *testing.T) {
		r := oneRowReader()
		cfg := newCfg(r)
		cfg.Store = primedStore(t, dsReg, "mysql1", vmAssignSchema())
		if _, err := registerExecuteRead(t, cfg).Execute(context.Background(), map[string]any{"dsl": "SELECT nope FROM vm_asign", "datasource_id": "hive1"}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("empty result probes table total", func(t *testing.T) {
		r := &scriptReader{fn: func(dsl string) (*executor.QueryResult, error) {
			if strings.HasPrefix(dsl, "SELECT COUNT(*)") {
				return &executor.QueryResult{Columns: []string{"COUNT(*)"}, Rows: [][]any{{int64(120)}}}, nil
			}
			return &executor.QueryResult{Columns: []string{"id"}}, nil
		}}
		out, err := registerExecuteRead(t, newCfg(r)).Execute(context.Background(), map[string]any{"dsl": "SELECT * FROM vm_assign WHERE state = 9"})
		if err != nil {
			t.Fatal(err)
		}
		res, ok := out.(*executor.QueryResult)
		if !ok {
			t.Fatalf("type %T", out)
		}
		if res.HitStatus != core.HitStatusSuspect {
			t.Fatalf("hit_status=%q", res.HitStatus)
		}
		if res.Diagnosis == nil || len(res.Diagnosis.Probes) == 0 || res.Diagnosis.Probes[0].Label != "table_total" || res.Diagnosis.Probes[0].Count != 120 {
			t.Fatalf("diagnosis=%+v", res.Diagnosis)
		}
		if len(r.calls) != 2 || r.calls[1] != "SELECT COUNT(*) FROM `vm_assign`" {
			t.Fatalf("calls=%v", r.calls)
		}
	})

	t.Run("no probe without condition", func(t *testing.T) {
		r := &scriptReader{fn: func(string) (*executor.QueryResult, error) {
			return &executor.QueryResult{Columns: []string{"id"}}, nil
		}}
		out, err := registerExecuteRead(t, newCfg(r)).Execute(context.Background(), map[string]any{"dsl": "SELECT * FROM vm_assign"})
		if err != nil {
			t.Fatal(err)
		}
		if res := out.(*executor.QueryResult); res.HitStatus != core.HitStatusEmpty || len(r.calls) != 1 {
			t.Fatalf("hit=%q calls=%v", res.HitStatus, r.calls)
		}
	})
}
