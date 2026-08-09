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
}

func TestAppendDatasourcePrompt(t *testing.T) {
	out := AppendDatasourcePrompt("base", "## 已绑定数据源\n- **a**")
	if !strings.HasPrefix(out, "base") || !strings.Contains(out, "已绑定数据源") {
		t.Fatalf("out: %q", out)
	}
}
