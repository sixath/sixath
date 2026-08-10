package chat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/tool"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestBuildSkillsIndexMergesWorkspaceAndExtraDirectories(t *testing.T) {
	workspace := t.TempDir()
	workspaceSkill := filepath.Join(workspace, "skills", "workspace-skill", "SKILL.md")
	sharedSkillDir := filepath.Join(t.TempDir(), "shared-skill")
	sharedSkill := filepath.Join(sharedSkillDir, "SKILL.md")

	for path, content := range map[string]string{
		workspaceSkill: "---\nname: workspace-skill\ndescription: workspace\n---\n",
		sharedSkill:    "---\nname: shared-skill\ndescription: shared\n---\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	index, err := BuildSkillsIndex(workspace, []string{sharedSkillDir})
	if err != nil {
		t.Fatalf("BuildSkillsIndex: %v", err)
	}
	if index == nil {
		t.Fatal("BuildSkillsIndex returned nil index")
	}
	for _, name := range []string{"workspace-skill", "shared-skill"} {
		if _, ok := index.GetByName(name); !ok {
			t.Errorf("skill %q missing from index", name)
		}
	}
}

func TestBuildRegistry_NoAgentToolsKeepsRegistryDefaults(t *testing.T) {
	reg := tool.NewRegistry()
	_, err := BuildRegistry(nil, nil, reg)
	if err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	if _, ok := reg.Get("http_request"); !ok {
		t.Fatal("expected default http_request from tool.NewRegistry when agent has no tools")
	}
}

func TestBuildRegistry_AllDatasourcesFailIncludesDetail(t *testing.T) {
	cfg, err := structpb.NewStruct(map[string]interface{}{
		"datasource": map[string]interface{}{
			"type": "hive",
		},
	})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}
	reg := tool.NewRegistry()
	_, err = BuildRegistry([]*biz.ToolMeta{{
		ID:     "ds-1",
		Name:   "bad-hive",
		Type:   biz.ToolTypeDatasource,
		Config: cfg,
	}}, nil, reg)
	if err == nil {
		t.Fatal("expected error when datasource cannot register")
	}
	if !containsAll(err.Error(), "所有数据源均注册失败", "bad-hive", "incomplete") {
		t.Fatalf("error should include datasource detail, got: %v", err)
	}
}

func TestBuildRegistry_ElasticsearchOnly_NoDataTrio(t *testing.T) {
	cfg, err := structpb.NewStruct(map[string]interface{}{
		"datasource": map[string]interface{}{
			"id":   "zj-es",
			"type": "elasticsearch",
			"dsn":  "http://127.0.0.1:9200",
		},
	})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}
	reg := tool.NewRegistry()
	res, err := BuildRegistry([]*biz.ToolMeta{{
		ID:     "es-1",
		Name:   "zj-es",
		Type:   biz.ToolTypeDatasource,
		Config: cfg,
	}}, nil, reg)
	if err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	for _, name := range []string{"list_tables", "describe_table", "execute_read"} {
		if _, ok := reg.Get(name); ok {
			t.Fatalf("ES-only agent must not register %s", name)
		}
	}
	if res == nil || !strings.Contains(res.DatasourcePrompt, "es_log_query") {
		t.Fatalf("prompt=%q", res.DatasourcePrompt)
	}
	if len(res.DsBindings) != 1 || !res.DsBindings[0].SkipDataTools {
		t.Fatalf("bindings=%+v", res.DsBindings)
	}
}

func TestBuildRegistry_MySQLPlusES_RegistersDataTrioForMySQLOnly(t *testing.T) {
	mysqlCfg, err := structpb.NewStruct(map[string]interface{}{
		"datasource": map[string]interface{}{
			"id":     "cgarchive",
			"type":   "mysql",
			"dsn":    "user:pass@tcp(127.0.0.1:3306)/db",
			"dbname": "db",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	esCfg, err := structpb.NewStruct(map[string]interface{}{
		"datasource": map[string]interface{}{
			"id":   "zj-es",
			"type": "elasticsearch",
			"dsn":  "http://127.0.0.1:9200",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	res, err := BuildRegistry([]*biz.ToolMeta{
		{ID: "m1", Name: "cgarchive", Type: biz.ToolTypeDatasource, Config: mysqlCfg},
		{ID: "e1", Name: "zj-es", Type: biz.ToolTypeDatasource, Config: esCfg},
	}, nil, reg)
	if err != nil {
		// MySQL DSN may fail ping/register depending on factory — if so skip trio assert
		if strings.Contains(err.Error(), "所有数据源均注册失败") {
			t.Skipf("mysql register failed in env: %v", err)
		}
		t.Fatalf("BuildRegistry: %v", err)
	}
	if _, ok := reg.Get("execute_read"); !ok {
		t.Fatal("expected execute_read when mysql registers")
	}
	if strings.Contains(res.DatasourcePrompt, "**zj-es**") {
		t.Fatalf("ES must not be in data prompt list: %s", res.DatasourcePrompt)
	}
	if !strings.Contains(res.DatasourcePrompt, "es_log_query") {
		t.Fatalf("missing ES hint: %s", res.DatasourcePrompt)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

func TestBuildRegistry_RegisterSSHExecBuiltin(t *testing.T) {
	cfg, err := structpb.NewStruct(map[string]interface{}{
		"func_path": "ssh_exec",
		"parameters": map[string]interface{}{
			"default_user":             "vrviu",
			"allowed_hosts":            []interface{}{"10.18.240.0/24"},
			"allowed_users":            []interface{}{"vrviu"},
			"allowed_command_prefixes": []interface{}{"journalctl -u archive-manager"},
			"strict_host_key_checking": "yes",
			"timeout_sec":              3,
		},
	})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}
	reg := tool.NewRegistry()
	_, err = BuildRegistry([]*biz.ToolMeta{{
		ID:     "ssh-tool",
		Name:   "SSH Exec",
		Type:   biz.ToolTypeBuiltin,
		Config: cfg,
	}}, nil, reg)
	if err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	tl, ok := reg.Get("ssh_exec")
	if !ok {
		t.Fatal("ssh_exec not registered")
	}

	out, err := tl.Execute(context.Background(), map[string]any{
		"host":    "10.18.241.1",
		"user":    "root",
		"command": "rm -rf /",
	})
	if err != nil {
		t.Fatalf("Execute should return policy result: %v", err)
	}
	res := out.(tool.SSHExecResult)
	if res.OK || res.ErrorCategory != tool.SSHExecErrorBlockedByPolicy {
		t.Fatalf("unexpected result: %+v", res)
	}
}
