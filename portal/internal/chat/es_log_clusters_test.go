package chat

import (
	"context"
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/datasource"
	"github.com/sixath/framework/executor"
	"github.com/sixath/framework/tool"
	"google.golang.org/protobuf/types/known/structpb"
)

// fakeESReader records Query's datasourceID without talking to Elasticsearch.
type fakeESReader struct {
	gotDatasource string
	gotIndex      string
	result        *executor.QueryResult
}

func (f *fakeESReader) Query(ctx context.Context, datasourceID string, dsl string, opts executor.QueryOptions) (*executor.QueryResult, error) {
	f.gotDatasource = datasourceID
	if opts.Extras != nil {
		if v, ok := opts.Extras["index"].(string); ok {
			f.gotIndex = v
		}
	}
	if f.result == nil {
		f.result = &executor.QueryResult{Columns: []string{"message"}, Rows: [][]any{{"ok"}}}
	}
	return f.result, nil
}

func esDatasourceMeta(t *testing.T, name string, fields map[string]any) *biz.ToolMeta {
	t.Helper()
	ds := map[string]any{"type": "elasticsearch", "dsn": "http://es.example:9200"}
	for k, v := range fields {
		ds[k] = v
	}
	st, err := structpb.NewStruct(map[string]any{"datasource": ds})
	if err != nil {
		t.Fatal(err)
	}
	return &biz.ToolMeta{Name: name, Type: biz.ToolTypeDatasource, Config: st}
}

func rcaESLogMeta(t *testing.T, name, desc string, extra map[string]any) *biz.ToolMeta {
	t.Helper()
	return &biz.ToolMeta{
		Name:        name,
		Description: desc,
		Type:        biz.ToolTypeRCA,
		Config:      mustRCAStruct(t, "es_log_query", extra),
	}
}

func clusterByID(clusters []tool.ESLogCluster, id string) (tool.ESLogCluster, bool) {
	for _, c := range clusters {
		if c.ID == id {
			return c, true
		}
	}
	return tool.ESLogCluster{}, false
}

func executeESLog(t *testing.T, clusters []tool.ESLogCluster, fr *fakeESReader, clusterID string) {
	t.Helper()
	reg := tool.NewRegistry()
	if err := tool.RegisterESLogTool(reg, fr, tool.ESLogConfig{Clusters: clusters}); err != nil {
		t.Fatalf("RegisterESLogTool: %v", err)
	}
	tl, ok := reg.Get("es_log_query")
	if !ok {
		t.Fatal("es_log_query not registered")
	}
	if _, err := tl.Execute(context.Background(), map[string]any{"cluster": clusterID, "query": "x:1"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestCollectESLog_TwoDatasourcesNoRCA(t *testing.T) {
	tools := []*biz.ToolMeta{
		esDatasourceMeta(t, "a", map[string]any{"dsn": "http://a:9200", "default_index": "a-*", "purpose": "cluster a"}),
		esDatasourceMeta(t, "b", map[string]any{"dsn": "http://b:9200", "default_index": "b-*", "purpose": "cluster b"}),
	}
	clusters, esReg := collectESLogClusters(tools)
	if esReg == nil {
		t.Fatal("expected dedicated ES registry")
	}
	if len(clusters) != 2 {
		t.Fatalf("clusters=%v want 2", clusters)
	}
	if _, ok := clusterByID(clusters, "a"); !ok {
		t.Fatal("missing cluster a")
	}
	if _, ok := clusterByID(clusters, "b"); !ok {
		t.Fatal("missing cluster b")
	}

	reg := tool.NewRegistry()
	registerESLogFromAgentTools(reg, tools)
	if _, ok := reg.Get("es_log_query"); !ok {
		t.Fatal("es_log_query should be registered from ES datasources without an RCA ELK tool")
	}

	fr := &fakeESReader{}
	executeESLog(t, clusters, fr, "b")
	if fr.gotDatasource != "b" {
		t.Fatalf("Query datasourceID=%q want b", fr.gotDatasource)
	}
}

func TestCollectESLog_ZeroES_ToolAbsent(t *testing.T) {
	mysql, err := structpb.NewStruct(map[string]any{
		"datasource": map[string]any{"type": "mysql", "dsn": "u:p@tcp(h:3306)/db"},
	})
	if err != nil {
		t.Fatal(err)
	}
	tools := []*biz.ToolMeta{{Name: "db", Type: biz.ToolTypeDatasource, Config: mysql}}
	clusters, _ := collectESLogClusters(tools)
	if len(clusters) != 0 {
		t.Fatalf("clusters=%v want empty", clusters)
	}
	reg := tool.NewRegistry()
	registerESLogFromAgentTools(reg, tools)
	if _, ok := reg.Get("es_log_query"); ok {
		t.Fatal("es_log_query must be absent with zero ES")
	}

	reg2 := tool.NewRegistry()
	if _, err := BuildRegistry(nil, nil, reg2); err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	if _, ok := reg2.Get("es_log_query"); ok {
		t.Fatal("empty tools must not register es_log_query")
	}
}

func TestCollectESLog_InlineEndpointUsesToolName(t *testing.T) {
	tools := []*biz.ToolMeta{
		rcaESLogMeta(t, "zj-elk", "zj logs", map[string]any{"endpoint": "http://zj:9200", "default_index": "app-*"}),
		rcaESLogMeta(t, "other-elk", "other logs", map[string]any{"endpoint": "http://other:9200", "default_index": "other-*"}),
	}
	clusters, esReg := collectESLogClusters(tools)
	if len(clusters) != 2 {
		t.Fatalf("want 2 inline clusters, got %v", clusters)
	}
	if _, ok := clusterByID(clusters, "rca-es"); ok {
		t.Fatal("inline cluster id must be the RCA tool name, not rca-es")
	}
	for _, id := range []string{"zj-elk", "other-elk"} {
		c, ok := clusterByID(clusters, id)
		if !ok {
			t.Fatalf("missing cluster %s", id)
		}
		if _, err := esReg.Get(id); err != nil {
			t.Fatalf("inline connection %s: %v", id, err)
		}
		if c.ID == "rca-es" {
			t.Fatal("must not use fixed rca-es")
		}
	}
	if _, err := esReg.Get("rca-es"); err == nil {
		t.Fatal("rca-es must not be registered")
	}

	zj, _ := clusterByID(clusters, "zj-elk")
	if zj.Purpose != "zj logs" {
		t.Fatalf("purpose=%q want RCA description", zj.Purpose)
	}

	fr := &fakeESReader{}
	executeESLog(t, clusters, fr, "zj-elk")
	if fr.gotDatasource != "zj-elk" {
		t.Fatalf("Query datasourceID=%q want zj-elk", fr.gotDatasource)
	}
}

func TestCollectESLog_SameNameKeepsDatasourceMergesIndex(t *testing.T) {
	tools := []*biz.ToolMeta{
		esDatasourceMeta(t, "zj-elk", map[string]any{"dsn": "http://ds:9200"}),
		rcaESLogMeta(t, "zj-elk", "from rca", map[string]any{
			"endpoint":      "http://inline:9200",
			"default_index": "app-*",
		}),
	}
	clusters, esReg := collectESLogClusters(tools)
	if len(clusters) != 1 {
		t.Fatalf("want 1 merged cluster, got %v", clusters)
	}
	c, ok := clusterByID(clusters, "zj-elk")
	if !ok {
		t.Fatal("missing zj-elk")
	}
	if c.DefaultIndex != "app-*" {
		t.Fatalf("default_index=%q want app-* from RCA", c.DefaultIndex)
	}
	ds, err := esReg.Get("zj-elk")
	if err != nil {
		t.Fatalf("Get zj-elk: %v", err)
	}
	type esHTTPProvider interface{ ESHTTP() *datasource.ESHTTP }
	if p, ok := ds.(esHTTPProvider); ok {
		if p.ESHTTP().BaseURL != "http://ds:9200" {
			t.Fatalf("connection BaseURL=%q want datasource DSN, not inline", p.ESHTTP().BaseURL)
		}
	} else {
		t.Fatal("expected elasticsearch HTTP provider")
	}
	if _, err := esReg.Get("rca-es"); err == nil {
		t.Fatal("must not register rca-es")
	}

	fr := &fakeESReader{}
	executeESLog(t, clusters, fr, "zj-elk")
	if fr.gotDatasource != "zj-elk" {
		t.Fatalf("Query datasourceID=%q want zj-elk (datasource id)", fr.gotDatasource)
	}
	if fr.gotIndex != "app-*" {
		t.Fatalf("index=%q want app-* merged from RCA", fr.gotIndex)
	}
}

func TestCollectESLog_DatasourceIDMergesIndex(t *testing.T) {
	tools := []*biz.ToolMeta{
		esDatasourceMeta(t, "es-logs", map[string]any{"dsn": "http://es:9200"}),
		rcaESLogMeta(t, "rca-es", "prod indexes", map[string]any{
			"datasource_id":  "es-logs",
			"default_index":  "app-*",
			"trace_id_field": "trace_id",
		}),
	}
	clusters, _ := collectESLogClusters(tools)
	if len(clusters) != 1 {
		t.Fatalf("want 1 cluster, got %v", clusters)
	}
	c, ok := clusterByID(clusters, "es-logs")
	if !ok {
		t.Fatal("cluster id must be the datasource id")
	}
	if c.DefaultIndex != "app-*" {
		t.Fatalf("default_index=%q want app-* merged from RCA", c.DefaultIndex)
	}
	if c.TraceIDField != "trace_id" {
		t.Fatalf("trace_id_field=%q want trace_id", c.TraceIDField)
	}
	if c.Purpose != "prod indexes" {
		t.Fatalf("purpose=%q want RCA description", c.Purpose)
	}
}

func TestCollectESLog_UnboundDatasourceIDSkipped(t *testing.T) {
	tools := []*biz.ToolMeta{
		esDatasourceMeta(t, "es-logs", map[string]any{"dsn": "http://es:9200", "default_index": "a-*"}),
		rcaESLogMeta(t, "orphan", "", map[string]any{"datasource_id": "missing"}),
	}
	clusters, _ := collectESLogClusters(tools)
	if _, ok := clusterByID(clusters, "missing"); ok {
		t.Fatal("unbound datasource_id must not add a cluster")
	}
	if len(clusters) != 1 || clusters[0].ID != "es-logs" {
		t.Fatalf("want only bound es-logs, got %v", clusters)
	}
}

func TestCollectESLog_FlatDatasourceConfig(t *testing.T) {
	flat, err := structpb.NewStruct(map[string]any{
		"id": "es-logs", "type": "elasticsearch", "dsn": "http://localhost:9200",
		"defaultIndex": "flat-*", "traceIdField": "tid", "purpose": "flat logs",
	})
	if err != nil {
		t.Fatal(err)
	}
	tools := []*biz.ToolMeta{{Name: "es-logs", Type: biz.ToolTypeDatasource, Config: flat}}
	clusters, _ := collectESLogClusters(tools)
	c, ok := clusterByID(clusters, "es-logs")
	if !ok {
		t.Fatalf("missing es-logs, got %v", clusters)
	}
	if c.DefaultIndex != "flat-*" || c.TraceIDField != "tid" || c.Purpose != "flat logs" {
		t.Fatalf("flat camelCase fields not read: %+v", c)
	}
}

func TestCollectESLog_BothEndpointAndDatasourceIDSkipped(t *testing.T) {
	tools := []*biz.ToolMeta{
		rcaESLogMeta(t, "bad", "", map[string]any{
			"endpoint": "http://es:9200", "datasource_id": "es-logs",
		}),
	}
	clusters, _ := collectESLogClusters(tools)
	if len(clusters) != 0 {
		t.Fatalf("both endpoint and datasource_id must skip, got %v", clusters)
	}
}
