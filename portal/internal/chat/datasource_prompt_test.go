package chat

import (
	"strings"
	"testing"

	"github.com/sixath/framework/datasource"
)

func TestCanonicalDatasourceConfig_UsesToolName(t *testing.T) {
	cfg := canonicalDatasourceConfig("cgarchive", datasource.Config{ID: "mysql", Type: "mysql", DBName: "d_cgarchive"})
	if cfg.ID != "cgarchive" {
		t.Fatalf("id = %q, want cgarchive", cfg.ID)
	}
}

func TestIsElasticsearchType(t *testing.T) {
	if !isElasticsearchType("elasticsearch") || !isElasticsearchType("ES") || !isElasticsearchType("es") {
		t.Fatal("expected elasticsearch aliases")
	}
	if isElasticsearchType("mysql") {
		t.Fatal("mysql must not be ES")
	}
}

func TestFormatDatasourcePrompt(t *testing.T) {
	p := FormatDatasourcePrompt([]DatasourceBinding{
		{ID: "cgarchive", Type: "mysql", DBName: "d_cgarchive", Available: true},
		{ID: "pro_mysql_vm_tool", Type: "mysql", DBName: "d_4103", Available: false, Err: "access denied"},
	}, "cgarchive")
	if !strings.Contains(p, "datasource_id") {
		t.Fatal("missing datasource_id hint")
	}
	if !strings.Contains(p, "**cgarchive**") || !strings.Contains(p, "默认") {
		t.Fatal("missing default marker for cgarchive")
	}
	if !strings.Contains(p, "不可用") {
		t.Fatal("missing unavailable marker")
	}
	if !strings.Contains(p, "禁止通过 ask_user") {
		t.Fatal("missing ask_user prohibition when datasource available")
	}
	if !strings.Contains(p, "立即作答结束") {
		t.Fatal("missing stop-after-execute_read hint")
	}
}

func TestFormatDatasourcePrompt_SkipsESAndAddsRoutingHint(t *testing.T) {
	p := FormatDatasourcePrompt([]DatasourceBinding{
		{ID: "cgarchive", Type: "mysql", DBName: "d_cgarchive", Available: true},
		{ID: "zj-es", Type: "elasticsearch", SkipDataTools: true, Available: false, Purpose: "应用日志", DefaultIndex: "app-*"},
	}, "cgarchive")
	if strings.Contains(p, "**zj-es**") {
		t.Fatalf("ES id must not appear in data list: %s", p)
	}
	if !strings.Contains(p, "**cgarchive**") {
		t.Fatal("mysql binding missing")
	}
	if !strings.Contains(p, "es_log_query") || !strings.Contains(p, "http_request") {
		t.Fatalf("missing ES routing hint: %s", p)
	}
	if !strings.Contains(p, "cluster=") {
		t.Fatalf("missing cluster= routing: %s", p)
	}
	if !strings.Contains(p, "es_log_query(cluster=zj-es)") {
		t.Fatalf("missing ES cluster listing: %s", p)
	}
	if !strings.Contains(p, "应用日志") || !strings.Contains(p, "app-*") {
		t.Fatalf("missing purpose/default_index: %s", p)
	}
}

func TestFormatDatasourcePrompt_ESOnly(t *testing.T) {
	p := FormatDatasourcePrompt([]DatasourceBinding{
		{ID: "zj-es", Type: "elasticsearch", SkipDataTools: true, Purpose: "应用日志", DefaultIndex: "app-*"},
	}, "")
	if strings.Contains(p, "## 已绑定数据源") {
		t.Fatalf("should not list data section for ES-only: %s", p)
	}
	if !strings.Contains(p, "es_log_query") {
		t.Fatalf("want routing hint, got %s", p)
	}
	if !strings.Contains(p, "cluster=") {
		t.Fatalf("missing cluster=: %s", p)
	}
	if !strings.Contains(p, "es_log_query(cluster=zj-es)") {
		t.Fatalf("missing cluster listing: %s", p)
	}
	if !strings.Contains(p, "应用日志") || !strings.Contains(p, "app-*") {
		t.Fatalf("missing purpose/default_index: %s", p)
	}
}

func TestAppendDatasourcePrompt(t *testing.T) {
	out := AppendDatasourcePrompt("base", "## 已绑定数据源\n- **a**")
	if !strings.HasPrefix(out, "base") || !strings.Contains(out, "已绑定数据源") {
		t.Fatalf("out: %q", out)
	}
}

func TestBindingFromConfig_MarksESSkip(t *testing.T) {
	b := bindingFromConfig("zj-es", datasource.Config{ID: "x", Type: "elasticsearch"}, nil)
	if !b.SkipDataTools {
		t.Fatal("expected SkipDataTools")
	}
}
