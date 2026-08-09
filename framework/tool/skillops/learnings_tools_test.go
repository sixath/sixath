package toolskill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	core "github.com/sixath/framework/tool"
)

func TestAppendLearningTool_writesUnderWorkspace(t *testing.T) {
	root := t.TempDir()
	reg := core.NewRegistry()
	if err := RegisterAppendLearningTool(reg); err != nil {
		t.Fatal(err)
	}
	tool, ok := reg.Get("append_learning")
	if !ok {
		t.Fatal("tool not registered")
	}
	ctx := context.WithValue(context.Background(), core.ContextKeyWorkspaceRoot, root)
	_, err := tool.Execute(ctx, map[string]any{
		"summary":  "always run grep from local host",
		"category": "correction",
		"area":     "ops",
	})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, ".learnings", "LEARNINGS.md")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "always run grep from local host") {
		t.Fatalf("missing summary: %s", body)
	}
}
