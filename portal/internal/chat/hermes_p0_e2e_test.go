package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/memorysearch"
	"github.com/sixath/framework/tool"
	toolskill "github.com/sixath/framework/tool/skillops"
	"github.com/sixath/framework/tool/web"
)

// TestHermesP0E2E_Checklist automates Task 16 manual Chat checklist via direct tool execution.
func TestHermesP0E2E_Checklist(t *testing.T) {
	root := t.TempDir()
	agentID := "agent-e2e"
	sessionID := "sess-e2e"

	if err := os.MkdirAll(filepath.Join(root, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	demoDir := filepath.Join(root, "skills", "demo-e2e")
	if err := os.MkdirAll(demoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	demoSkill := "---\nname: demo-e2e\ndescription: e2e demo skill\n---\n# Demo\n\nUse tab indent.\n"
	if err := os.WriteFile(filepath.Join(demoDir, "SKILL.md"), []byte(demoSkill), 0o644); err != nil {
		t.Fatal(err)
	}

	skillsIdx, err := BuildSkillsIndex(root, nil)
	if err != nil {
		t.Fatal(err)
	}

	cronStub := &e2eCronClient{}
	SetCronClient(cronStub)
	t.Cleanup(func() { SetCronClient(nil) })

	oldFlags := DefaultHermesP0ToolFlags
	SetHermesP0ToolFlags(HermesP0ToolFlags{
		MemoryWriteEnabled:             true,
		SkillRuntimeManageEnabled:      true,
		SkillManageConfirmCreateDelete: true,
		TodoEnabled:                    true,
		WorkspaceFilesEnabled:          true,
		WebToolsEnabled:                false,
		TerminalLocalEnabled:           true,
		CronjobToolEnabled:             true,
	})
	t.Cleanup(func() { SetHermesP0ToolFlags(oldFlags) })

	memCfg := e2eMemoryConfig(root)
	reg := tool.NewRegistry()
	if err := RegisterAgentRuntimeTools(reg, AgentRuntimeToolsOptions{
		SkillsIdx:   skillsIdx,
		AllowScript: true,
		MemoryCfg:   &memCfg,
	}); err != nil {
		t.Fatal(err)
	}

	mockWeb := e2eMockBochaBackend(t)
	if err := tool.RegisterWebTools(reg, &tool.WebToolsConfig{SearchBackend: mockWeb}); err != nil {
		t.Fatal(err)
	}

	ctx := e2eCtx(root, agentID, sessionID)
	t.Cleanup(func() {
		cfg := config.Config{Memory: memCfg}
		if mgr, err := memorysearch.GetMemorySearchManager(cfg, agentID, root, nil, nil); err == nil {
			if c, ok := mgr.(interface{ Close() error }); ok {
				_ = c.Close()
			}
		}
	})

	t.Run("1_memory_remember_then_recall", func(t *testing.T) {
		memTL := mustGetTool(t, reg, "memory_remember")
		res, err := memTL.Execute(ctx, map[string]any{
			"scope":   "session",
			"action":  "add",
			"content": "User prefers tab indent for code.",
		})
		if err != nil {
			t.Fatal(err)
		}
		if m, _ := res.(map[string]any); m["error"] != nil {
			t.Fatalf("memory_remember add: %#v", res)
		}

		searchTL := mustGetTool(t, reg, "memory_recall")
		out, err := searchTL.Execute(ctx, map[string]any{"scope": "session", "query": "tab indent", "limit": 5})
		if err != nil {
			t.Fatal(err)
		}
		sm, _ := out.(map[string]any)
		if errMsg, _ := sm["error"].(string); errMsg != "" {
			t.Fatalf("memory_recall error: %s", errMsg)
		}
		if !e2eHasResults(sm["hits"]) {
			t.Fatalf("memory_recall expected hits for tab indent: %#v", sm)
		}
	})

	t.Run("2_skills_list_view_patch", func(t *testing.T) {
		listTL := mustGetTool(t, reg, "skills_list")
		listOut, err := listTL.Execute(ctx, map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		lm, _ := listOut.(map[string]any)
		skills, _ := lm["skills"].([]map[string]any)
		if len(skills) == 0 {
			// []any fallback
			if arr, ok := lm["skills"].([]any); ok && len(arr) > 0 {
				skills = nil // present
			} else {
				t.Fatalf("skills_list empty: %#v", listOut)
			}
		}

		viewTL := mustGetTool(t, reg, "skill_view")
		viewOut, err := viewTL.Execute(ctx, map[string]any{"name": "demo-e2e"})
		if err != nil {
			t.Fatal(err)
		}
		vm, _ := viewOut.(map[string]any)
		if vm["error"] != nil {
			t.Fatalf("skill_view: %#v", viewOut)
		}

		manageTL := mustGetTool(t, reg, "skill_manage")
		patchOut, err := manageTL.Execute(ctx, map[string]any{
			"action":     "patch",
			"name":       "demo-e2e",
			"old_string": "Use tab indent.",
			"new_string": "Use tab indent always.",
		})
		if err != nil {
			t.Fatal(err)
		}
		var patchToken string
		switch v := patchOut.(type) {
		case toolskill.SkillManagePendingResponse:
			if v.Status != "pending" {
				t.Fatalf("expected patch pending, got %#v", patchOut)
			}
			patchToken = v.Token
		case map[string]any:
			if v["status"] == "ok" {
				// patch confirm disabled
			} else if v["status"] == "pending" {
				patchToken, _ = v["token"].(string)
			} else {
				t.Fatalf("skill_manage patch: %#v", patchOut)
			}
		default:
			t.Fatalf("unexpected patch type %T: %#v", patchOut, patchOut)
		}
		if patchToken != "" {
			confirm, err := manageTL.Execute(tool.WithSkillManageUIConfirm(ctx), map[string]any{
				"action":        "patch",
				"name":          "demo-e2e",
				"confirm_token": patchToken,
			})
			if err != nil {
				t.Fatal(err)
			}
			pm, _ := confirm.(map[string]any)
			if pm["status"] != "ok" {
				t.Fatalf("patch confirm: %#v", confirm)
			}
		}
		body, err := os.ReadFile(filepath.Join(root, "skills", "demo-e2e", "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "tab indent always") {
			t.Fatalf("SKILL.md not patched: %q", string(body))
		}
	})

	t.Run("3_skill_create_confirm_disk", func(t *testing.T) {
		manageTL := mustGetTool(t, reg, "skill_manage")
		content := "---\nname: created-e2e\ndescription: created in e2e\n---\n# Created\n"
		propose, err := manageTL.Execute(ctx, map[string]any{
			"action":  "create",
			"name":    "created-e2e",
			"content": content,
		})
		if err != nil {
			t.Fatal(err)
		}
		var token string
		switch v := propose.(type) {
		case map[string]any:
			st, _ := v["status"].(string)
			if st != "confirm_required" && st != "pending" {
				t.Fatalf("expected pending confirm, got %#v", propose)
			}
			token, _ = v["token"].(string)
		case toolskill.SkillManagePendingResponse:
			if v.Status != "pending" && v.Status != "confirm_required" {
				t.Fatalf("expected pending confirm, got %#v", propose)
			}
			token = v.Token
		default:
			t.Fatalf("unexpected propose type %T: %#v", propose, propose)
		}
		if token == "" {
			t.Fatal("missing confirm token")
		}

		confirm, err := manageTL.Execute(tool.WithSkillManageUIConfirm(ctx), map[string]any{
			"action":        "create",
			"name":          "created-e2e",
			"content":       content,
			"confirm_token": token,
		})
		if err != nil {
			t.Fatal(err)
		}
		cm, _ := confirm.(map[string]any)
		if cm["status"] != "ok" {
			t.Fatalf("confirm create: %#v", confirm)
		}
		path := filepath.Join(root, "skills", "created-e2e", "SKILL.md")
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("skill not on disk: %v", err)
		}
	})

	t.Run("4_read_file_and_patch", func(t *testing.T) {
		notePath := filepath.Join(root, "notes", "hello.txt")
		if err := os.MkdirAll(filepath.Dir(notePath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(notePath, []byte("hello world\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		readTL := mustGetTool(t, reg, "read_file")
		readOut, err := readTL.Execute(ctx, map[string]any{"path": "notes/hello.txt"})
		if err != nil {
			t.Fatal(err)
		}
		rm, _ := readOut.(map[string]any)
		if !strings.Contains(rm["content"].(string), "hello") {
			t.Fatalf("read_file: %#v", readOut)
		}

		patchTL := mustGetTool(t, reg, "patch")
		patchOut, err := patchTL.Execute(ctx, map[string]any{
			"path":       "notes/hello.txt",
			"old_string": "world",
			"new_string": "Hermes",
		})
		if err != nil {
			t.Fatal(err)
		}
		if pm, _ := patchOut.(map[string]any); pm["error"] != nil {
			t.Fatalf("patch: %#v", patchOut)
		}
		b, _ := os.ReadFile(notePath)
		if !strings.Contains(string(b), "Hermes") {
			t.Fatalf("file not patched: %q", string(b))
		}
	})

	t.Run("5_web_search", func(t *testing.T) {
		webTL := mustGetTool(t, reg, "web_search")
		if err := webTL.CheckFn(ctx); err != nil {
			t.Fatal(err)
		}
		out, err := webTL.Execute(ctx, map[string]any{"query": "AI news"})
		if err != nil {
			t.Fatal(err)
		}
		resp, ok := out.(*web.SearchResponse)
		if !ok || len(resp.Results) == 0 || resp.Results[0].Title != "E2E News" {
			t.Fatalf("web_search: %#v", out)
		}
	})

	t.Run("6_terminal_git_status", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not in PATH")
		}
		gitRoot := filepath.Join(root, "git-repo")
		if err := os.MkdirAll(gitRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("git", "init")
		cmd.Dir = gitRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git init failed: %v %s", err, out)
		}

		termTL := mustGetTool(t, reg, "terminal")
		if err := termTL.CheckFn(ctx); err != nil {
			t.Fatal(err)
		}
		out, err := termTL.Execute(ctx, map[string]any{
			"command": "git status",
			"workdir": ".",
		})
		if err != nil {
			t.Fatal(err)
		}
		m, _ := out.(map[string]any)
		if m["error"] != nil {
			t.Fatalf("terminal: %#v", out)
		}
		combined, _ := m["stdout"].(string)
		combined += m["stderr"].(string)
		if !strings.Contains(strings.ToLower(combined), "git") && m["exit_code"].(int) != 0 {
			t.Fatalf("git status unexpected: %#v", out)
		}
	})

	t.Run("7_cronjob_create_and_list", func(t *testing.T) {
		cronTL := mustGetTool(t, reg, "cronjob")
		if err := cronTL.CheckFn(ctx); err != nil {
			t.Fatal(err)
		}
		createOut, err := cronTL.Execute(ctx, map[string]any{
			"action":          "create",
			"name":            "daily-summary",
			"schedule_kind":   "cron",
			"schedule_expr":   "0 9 * * *",
			"payload_kind":    "agent_turn",
			"payload_content": "Summarize inbox",
		})
		if err != nil {
			t.Fatal(err)
		}
		cm, _ := createOut.(map[string]any)
		if cm["ok"] != true {
			t.Fatalf("cronjob create: %#v", createOut)
		}

		listOut, err := cronTL.Execute(ctx, map[string]any{"action": "list"})
		if err != nil {
			t.Fatal(err)
		}
		lm, _ := listOut.(map[string]any)
		if lm["total"].(int) != 1 {
			t.Fatalf("cronjob list: %#v", listOut)
		}
	})

	t.Run("nested_cron_create_forbidden", func(t *testing.T) {
		cronTL := mustGetTool(t, reg, "cronjob")
		cronCtx := WithCronSessionContext(ctx)
		out, err := cronTL.Execute(cronCtx, map[string]any{
			"action":          "create",
			"name":            "nested",
			"schedule_kind":   "cron",
			"schedule_expr":   "0 9 * * *",
			"payload_kind":    "agent_turn",
			"payload_content": "x",
		})
		if err != nil {
			t.Fatal(err)
		}
		m, _ := out.(map[string]any)
		if m["error"] != "cron_nested_forbidden" {
			t.Fatalf("expected cron_nested_forbidden, got %#v", out)
		}
	})
}

type e2eCronClient struct {
	tasks []tool.CronJobSummary
}

func (c *e2eCronClient) Create(_ context.Context, agentID string, in tool.CronJobCreateInput) (tool.CronJobSummary, error) {
	s := tool.CronJobSummary{
		ID:             "cron-e2e-1",
		Name:           in.Name,
		AgentID:        agentID,
		ScheduleKind:   in.ScheduleKind,
		ScheduleExpr:   in.ScheduleExpr,
		PayloadKind:    in.PayloadKind,
		PayloadContent: in.PayloadContent,
		Enabled:        in.Enabled,
	}
	c.tasks = append(c.tasks, s)
	return s, nil
}

func (c *e2eCronClient) List(_ context.Context, _ string, _, _ int, _ *bool) ([]tool.CronJobSummary, int, error) {
	return c.tasks, len(c.tasks), nil
}

func (c *e2eCronClient) Update(_ context.Context, taskID string, updates map[string]any) (tool.CronJobSummary, error) {
	for i := range c.tasks {
		if c.tasks[i].ID == taskID {
			if v, ok := updates["enabled"].(bool); ok {
				c.tasks[i].Enabled = v
			}
			return c.tasks[i], nil
		}
	}
	return tool.CronJobSummary{}, nil
}

func (c *e2eCronClient) Delete(_ context.Context, taskID string) error {
	for i, task := range c.tasks {
		if task.ID == taskID {
			c.tasks = append(c.tasks[:i], c.tasks[i+1:]...)
			return nil
		}
	}
	return nil
}

func (c *e2eCronClient) RunAdHoc(_ context.Context, _ string) error { return nil }

func e2eMemoryConfig(root string) config.MemoryConfig {
	return config.MemoryConfig{
		Backend: "builtin",
		Defaults: config.MemorySearchConfig{
			Enabled: true,
			Sources: []string{"memory"},
			Store:   config.MemoryStoreConfig{Path: filepath.Join(root, ".idx.db")},
		},
	}
}

func e2eMockBochaBackend(t *testing.T) web.WebSearchBackend {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"webPages":{"value":[{"name":"E2E News","url":"https://example.com","snippet":"test"}]}}`))
	}))
	t.Cleanup(srv.Close)
	return web.NewBochaBackend(web.BochaConfig{
		APIKey:     "e2e-key",
		Endpoint:   srv.URL,
		HTTPClient: srv.Client(),
	})
}

func e2eCtx(root, agentID, sessionID string) context.Context {
	ctx := context.WithValue(context.Background(), tool.ContextKeyWorkspaceRoot, root)
	ctx = context.WithValue(ctx, tool.ContextKeyAgentID, agentID)
	ctx = context.WithValue(ctx, tool.ContextKeySessionID, sessionID)
	return ctx
}

func mustGetTool(t *testing.T, reg *tool.Registry, name string) tool.Tool {
	t.Helper()
	tl, ok := reg.Get(name)
	if !ok {
		t.Fatalf("tool %q not registered", name)
	}
	return tl
}

func e2eHasResults(v any) bool {
	switch r := v.(type) {
	case []any:
		return len(r) > 0
	case []map[string]any:
		return len(r) > 0
	default:
		if b, err := json.Marshal(v); err == nil {
			return strings.Contains(string(b), "tab") || strings.Contains(string(b), "indent")
		}
	}
	return false
}
