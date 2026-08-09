package chat

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sixath/framework/skills"
	"github.com/sixath/framework/tool"
)

func TestDatasourceCatalogProvider_EnrichesBindings(t *testing.T) {
	p := &DatasourceCatalogProvider{Bindings: []DatasourceBinding{{
		ID: "prod_mysql", Type: "mysql", DBName: "archive", Available: true,
	}}}
	entries := []tool.ToolCatalogEntry{{Name: "execute_read", Toolset: tool.ToolsetFile, Available: true}}
	out := p.Enrich(context.Background(), entries)
	if len(out) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(out))
	}
	e := out[0]
	if e.Bindings["datasource_id"] != "prod_mysql" {
		t.Fatalf("datasource_id: got %q", e.Bindings["datasource_id"])
	}
	if e.Bindings["type"] != "mysql" {
		t.Fatalf("type: got %q", e.Bindings["type"])
	}
	if e.Bindings["db_name"] != "archive" {
		t.Fatalf("db_name: got %q", e.Bindings["db_name"])
	}
	for _, want := range []string{"mysql", "archive", "数据库", "SQL"} {
		if !slices.Contains(e.SearchHints, want) {
			t.Fatalf("SearchHints missing %q: %v", want, e.SearchHints)
		}
	}
}

func TestChannelCatalogProvider_EnrichesWecom(t *testing.T) {
	p := &ChannelCatalogProvider{ChannelID: "ch-123", ChannelType: "wecom"}
	entries := []tool.ToolCatalogEntry{{Name: "send_to_wecom", Toolset: tool.ToolsetCore, Available: true}}
	out := p.Enrich(context.Background(), entries)
	if len(out) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(out))
	}
	e := out[0]
	if e.Bindings["channel_id"] != "ch-123" {
		t.Fatalf("channel_id: got %q", e.Bindings["channel_id"])
	}
	if e.Bindings["channel_type"] != "wecom" {
		t.Fatalf("channel_type: got %q", e.Bindings["channel_type"])
	}
	for _, want := range []string{"企微", "企业微信", "推送", "webhook"} {
		if !slices.Contains(e.SearchHints, want) {
			t.Fatalf("SearchHints missing %q: %v", want, e.SearchHints)
		}
	}
}

func TestSkillsCatalogProvider_EnrichesSkillNames(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "demo-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: demo-skill\ndescription: Demo skill for catalog\n---\n# Demo\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := skills.NewIndex([]string{dir}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	p := &SkillsCatalogProvider{Index: idx}
	entries := []tool.ToolCatalogEntry{{Name: "load_skill", Toolset: tool.ToolsetSkills, Available: true}}
	out := p.Enrich(context.Background(), entries)
	if len(out) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(out))
	}
	e := out[0]
	for _, want := range []string{"demo-skill", "Demo skill for catalog"} {
		if !slices.Contains(e.SearchHints, want) {
			t.Fatalf("SearchHints missing %q: %v", want, e.SearchHints)
		}
	}
}
