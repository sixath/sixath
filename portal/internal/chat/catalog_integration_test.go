package chat

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

// discoveryFakeModel drives ReAct integration tests without a real LLM.
type discoveryFakeModel struct {
	toolSteps  []model.ToolStep
	finalReply string
	toolCalls  int
}

func (f *discoveryFakeModel) Generate(context.Context, string, ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: f.finalReply}, nil
}

func (f *discoveryFakeModel) Chat(context.Context, []model.Message, ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: f.finalReply}, nil
}

func (f *discoveryFakeModel) Embed(context.Context, []string, ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

func (f *discoveryFakeModel) ChatWithTools(ctx context.Context, messages []model.Message, reg *tool.Registry, opts ...model.Option) (*model.Generation, error) {
	_ = ctx
	_ = messages
	_ = reg
	_ = opts
	f.toolCalls++
	if len(f.toolSteps) > 0 {
		step := f.toolSteps[0]
		f.toolSteps = f.toolSteps[1:]
		return &model.Generation{Text: f.finalReply, Raw: step}, nil
	}
	return &model.Generation{Text: f.finalReply, Raw: model.ToolStep{Used: false}}, nil
}

func registerStubDatasourceTools(reg *tool.Registry) error {
	return registerTestDatasourceTools(reg, false)
}

// registerDiscoveryDatasourceTools registers datasource tools with a realistic execute_read response.
func registerDiscoveryDatasourceTools(reg *tool.Registry) error {
	return registerTestDatasourceTools(reg, true)
}

func registerTestDatasourceTools(reg *tool.Registry, realisticRead bool) error {
	for _, name := range []string{"list_tables", "describe_table"} {
		n := name
		if err := reg.Register(tool.Tool{
			Name:        n,
			Description: "Datasource tool " + n + " for integration tests",
			Toolset:     tool.ToolsetFile,
			Parameters:  map[string]any{"type": "object"},
			Execute: func(ctx context.Context, params map[string]any) (any, error) {
				_ = ctx
				_ = params
				return map[string]any{"ok": true, "tool": n}, nil
			},
		}); err != nil {
			return err
		}
	}
	readExecute := func(ctx context.Context, params map[string]any) (any, error) {
		_ = ctx
		if !realisticRead {
			return map[string]any{"ok": true, "tool": "execute_read"}, nil
		}
		dsID, _ := params["datasource_id"].(string)
		if dsID != "" && dsID != "prod_mysql" {
			return map[string]any{"error": "unknown datasource_id"}, nil
		}
		return map[string]any{
			"rows": []map[string]any{
				{"status": "active", "cnt": 42},
				{"status": "inactive", "cnt": 7},
			},
		}, nil
	}
	return reg.Register(tool.Tool{
		Name:        "execute_read",
		Description: "Run read-only SQL against bound MySQL datasource prod_mysql (archive)",
		Toolset:     tool.ToolsetFile,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"datasource_id": map[string]any{"type": "string"},
				"sql":           map[string]any{"type": "string"},
			},
		},
		Execute: readExecute,
	})
}

type discoveryAgentFixture struct {
	Reg      *tool.Registry
	Catalog  tool.ToolCatalog
	Wecom    *httptest.Server
	LastPost string
}

func setupMysqlWecomDiscoveryFixture(t *testing.T, realisticDS bool) *discoveryAgentFixture {
	t.Helper()
	fix := &discoveryAgentFixture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		fix.LastPost = string(b)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	fix.Wecom = srv

	reg := tool.NewRegistry()
	var err error
	if realisticDS {
		err = registerDiscoveryDatasourceTools(reg)
	} else {
		err = registerStubDatasourceTools(reg)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := RegisterSendToWeComTool(reg, SendToWeComOptions{
		ResolveWebhook: func(context.Context) (string, error) { return srv.URL, nil },
	}); err != nil {
		t.Fatal(err)
	}
	if err := RegisterAskUserTools(reg); err != nil {
		t.Fatal(err)
	}
	fix.Reg = reg
	fix.Catalog, _ = wireDiscoveryAgent(t, reg)
	return fix
}

func discoveryWiringInput(reg *tool.Registry) CatalogWiringInput {
	return CatalogWiringInput{
		Reg: reg,
		DsBindings: []DatasourceBinding{{
			ID: "prod_mysql", Type: "mysql", DBName: "archive", Available: true,
		}},
		WecomChannelID: "ch-ops",
		ChannelType:    "wecom",
	}
}

func wireDiscoveryAgent(t *testing.T, reg *tool.Registry) (tool.ToolCatalog, bool) {
	t.Helper()
	catalog, active, err := WireCatalogAndToolSearch(context.Background(), discoveryWiringInput(reg))
	if err != nil {
		t.Fatalf("WireCatalogAndToolSearch: %v", err)
	}
	return catalog, active
}

// --- Layer 2: WireCatalogAndToolSearch + prompt injection ---

func TestWireCatalogAndToolSearch_MysqlWecomBindingsInCatalog(t *testing.T) {
	reg := tool.NewRegistry()
	if err := registerStubDatasourceTools(reg); err != nil {
		t.Fatal(err)
	}
	if err := RegisterSendToWeComTool(reg, SendToWeComOptions{
		ResolveWebhook: func(context.Context) (string, error) { return "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=test", nil },
	}); err != nil {
		t.Fatal(err)
	}

	catalog, _ := wireDiscoveryAgent(t, reg)

	var executeRead, sendWecom *tool.ToolCatalogEntry
	for i := range catalog.Entries {
		switch catalog.Entries[i].Name {
		case "execute_read":
			executeRead = &catalog.Entries[i]
		case "send_to_wecom":
			sendWecom = &catalog.Entries[i]
		}
	}
	if executeRead == nil {
		t.Fatal("catalog missing execute_read")
	}
	if executeRead.Bindings["datasource_id"] != "prod_mysql" {
		t.Fatalf("execute_read binding: %#v", executeRead.Bindings)
	}
	if sendWecom == nil {
		t.Fatal("catalog missing send_to_wecom")
	}
	if sendWecom.Bindings["channel_id"] != "ch-ops" {
		t.Fatalf("send_to_wecom binding: %#v", sendWecom.Bindings)
	}
}

func registerManyMcpTools(t *testing.T, reg *tool.Registry, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("mcp__bulk__tool_%03d", i)
		desc := fmt.Sprintf("Deferred MCP tool for progressive disclosure threshold testing #%d with extended catalog metadata", i)
		if err := reg.Register(tool.Tool{
			Name:        name,
			Description: desc,
			Toolset:     tool.ToolsetMCP,
			Parameters:  map[string]any{"type": "object"},
			Execute:     func(context.Context, map[string]any) (any, error) { return "ok", nil },
		}); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
}

func TestWireCatalogAndToolSearch_Mcp50ActivatesBridgeInAutoMode(t *testing.T) {
	os.Unsetenv("SATH_TOOL_SEARCH") // default auto

	reg := tool.NewRegistry()
	registerManyMcpTools(t, reg, 75)
	if err := registerStubDatasourceTools(reg); err != nil {
		t.Fatal(err)
	}

	catalog, active := wireDiscoveryAgent(t, reg)
	if !active {
		t.Fatal("expected tool_search active in auto mode with 75 deferred MCP tools")
	}
	names := catalogEntryNames(catalog)
	for _, want := range []string{tool.ToolSearchName, tool.ToolDescribeName, tool.ToolCallName} {
		if !containsString(names, want) {
			t.Fatalf("rebuilt catalog missing bridge tool %q", want)
		}
	}

	ctx := context.WithValue(context.Background(), tool.ContextKeyToolSearchActive, true)
	inline := reg.ListForAPI(ctx, nil)
	deferred := reg.ListForAPIWithDefer(ctx, nil, true)
	if len(deferred) >= len(inline) {
		t.Fatalf("deferred schema should be smaller than full inline: inline=%d deferred=%d", len(inline), len(deferred))
	}
	for _, tl := range deferred {
		if strings.HasPrefix(tl.Name, "mcp__bulk__") {
			t.Fatalf("deferred MCP tool %q should not appear in active schema", tl.Name)
		}
	}
}

func TestWireCatalogAndToolSearch_ActivatesBridgeAndRebuildsCatalog(t *testing.T) {
	t.Setenv("SATH_TOOL_SEARCH", "on")

	reg := tool.NewRegistry()
	if err := registerStubDatasourceTools(reg); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(tool.Tool{
		Name:        "mcp__jira__create_issue",
		Description: "Create Jira issue",
		Toolset:     tool.ToolsetMCP,
		Parameters:  map[string]any{"type": "object"},
		Execute:     func(context.Context, map[string]any) (any, error) { return "ok", nil },
	}); err != nil {
		t.Fatal(err)
	}

	catalog, active := wireDiscoveryAgent(t, reg)
	if !active {
		t.Fatal("expected tool_search active with deferred MCP tool")
	}
	names := catalogEntryNames(catalog)
	for _, want := range []string{"list_tools", tool.ToolSearchName, tool.ToolDescribeName, tool.ToolCallName} {
		if !containsString(names, want) {
			t.Fatalf("rebuilt catalog missing %q: %v", want, names)
		}
	}
}

func TestBuildAgentSystemPrompt_NoCatalogPrepend(t *testing.T) {
	effectivePrompt := BuildEffectiveSystemPrompt("You are a helpful assistant.", nil)
	effectivePrompt = AppendAskUserToolPrompt(effectivePrompt)
	dsPrompt := FormatDatasourcePrompt([]DatasourceBinding{{
		ID: "prod_mysql", Type: "mysql", DBName: "archive", Available: true,
	}}, "prod_mysql")
	if dsPrompt == "" || !strings.Contains(dsPrompt, "prod_mysql") {
		t.Fatalf("datasource catalog text missing prod_mysql: %s", dsPrompt)
	}
	if strings.Contains(effectivePrompt, "本轮任务锁") {
		t.Fatal(effectivePrompt)
	}
	if strings.HasPrefix(strings.TrimSpace(effectivePrompt), "## 可用工具目录") {
		t.Fatal("catalog block must not be prepended onto system prompt")
	}
	if !strings.Contains(effectivePrompt, "You are a helpful assistant") {
		t.Fatalf("agent system prompt missing:\n%s", effectivePrompt)
	}
}

// --- Layer 3: ask_user guard via full ReAct wiring ---

func TestToolDiscoveryIntegration_AskUserBlockedForMysqlCredentials(t *testing.T) {
	reg := tool.NewRegistry()
	if err := registerStubDatasourceTools(reg); err != nil {
		t.Fatal(err)
	}
	if err := RegisterAskUserTools(reg); err != nil {
		t.Fatal(err)
	}
	catalog, _ := wireDiscoveryAgent(t, reg)

	fake := &discoveryFakeModel{
		toolSteps: []model.ToolStep{{
			Used:     true,
			ToolName: "ask_user",
			Arguments: map[string]any{
				"prompt": "请提供 MySQL 数据库 host、port、用户名和密码",
				"kind":   "password",
				"field":  "mysql_credentials",
			},
		}},
		finalReply: "已查询完成",
	}
	mem := memory.NewBufferMemory(5)
	react := agent.NewReActAgent(fake, mem, reg)

	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "sess-discovery-1")
	ctx = context.WithValue(ctx, tool.ContextKeyToolCatalog, catalog)

	resp, err := react.Run(ctx, &agent.Request{
		Messages: []model.Message{{Role: "user", Content: "查一下 archive 库 users 表按 status 分组"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	trace, ok := resp.Metadata["trace"].(*agent.RunTrace)
	if !ok || len(trace.ToolCalls) != 1 {
		t.Fatalf("expected one tool call in trace, got %#v", resp.Metadata)
	}
	rec := trace.ToolCalls[0]
	if rec.ToolName != "ask_user" {
		t.Fatalf("expected ask_user call, got %q", rec.ToolName)
	}
	result := toString(rec.Result)
	if !strings.Contains(result, "ask_user blocked") {
		t.Fatalf("expected guard block message, got: %q", result)
	}
	redirected := strings.Contains(result, "execute_read") ||
		strings.Contains(result, "list_tables") ||
		strings.Contains(result, "describe_table")
	if !redirected || !strings.Contains(result, "prod_mysql") {
		t.Fatalf("expected guard redirect to datasource tool with prod_mysql, got: %q", result)
	}
}

func TestToolDiscoveryIntegration_AskUserAllowedForConfirm(t *testing.T) {
	reg := tool.NewRegistry()
	if err := registerStubDatasourceTools(reg); err != nil {
		t.Fatal(err)
	}
	if err := RegisterAskUserTools(reg); err != nil {
		t.Fatal(err)
	}
	catalog, _ := wireDiscoveryAgent(t, reg)

	fake := &discoveryFakeModel{
		toolSteps: []model.ToolStep{{
			Used:     true,
			ToolName: "ask_user",
			Arguments: map[string]any{
				"prompt": "是否继续执行删除操作？",
				"kind":   "confirm",
				"field":  "confirm_delete",
			},
		}},
		finalReply: "done",
	}
	mem := memory.NewBufferMemory(5)
	react := agent.NewReActAgent(fake, mem, reg)

	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "sess-discovery-2")
	ctx = context.WithValue(ctx, tool.ContextKeyToolCatalog, catalog)

	resp, err := react.Run(ctx, &agent.Request{Messages: []model.Message{{Role: "user", Content: "删除临时表"}}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	trace, ok := resp.Metadata["trace"].(*agent.RunTrace)
	if !ok || len(trace.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %#v", resp.Metadata)
	}
	rec := trace.ToolCalls[0]
	if strings.Contains(toString(rec.Result), "ask_user blocked") {
		t.Fatalf("confirm kind should not be blocked, got %q", rec.Result)
	}
	m, ok := rec.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected pending map result, got %T %v", rec.Result, rec.Result)
	}
	if m["status"] != "pending" || m["token"] == "" {
		t.Fatalf("expected pending ask_user with token, got %#v", m)
	}
}

// --- Spec §11.2: mysql + wecom multi-step scenario ---

func TestToolDiscoveryIntegration_MysqlStatusGroupByThenWecomPush(t *testing.T) {
	fix := setupMysqlWecomDiscoveryFixture(t, true)

	fake := &discoveryFakeModel{
		toolSteps: []model.ToolStep{
			{
				Used:     true,
				ToolName: "execute_read",
				Arguments: map[string]any{
					"datasource_id": "prod_mysql",
					"sql":           "SELECT status, COUNT(*) AS cnt FROM users GROUP BY status",
				},
			},
			{
				Used:     true,
				ToolName: "send_to_wecom",
				Arguments: map[string]any{
					"content": "users 按 status 分布：active=42, inactive=7",
					"msg_type": "text",
				},
			},
		},
		finalReply: "已查询 archive 库并推送到企微群。",
	}
	mem := memory.NewBufferMemory(8)
	react := agent.NewReActAgent(fake, mem, fix.Reg)

	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "sess-e2e-mysql-wecom")
	ctx = context.WithValue(ctx, tool.ContextKeyToolCatalog, fix.Catalog)

	resp, err := react.Run(ctx, &agent.Request{
		Messages: []model.Message{{
			Role:    "user",
			Content: "帮我查 archive 库 users 表按 status 分组统计，并把结果推到企微群",
		}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp == nil || !strings.Contains(resp.Text, "企微") {
		t.Fatalf("unexpected final reply: %#v", resp)
	}

	trace, ok := resp.Metadata["trace"].(*agent.RunTrace)
	if !ok || len(trace.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls (execute_read + send_to_wecom), got %#v", trace)
	}
	if trace.ToolCalls[0].ToolName != "execute_read" || trace.ToolCalls[0].Error != "" {
		t.Fatalf("execute_read failed: %#v", trace.ToolCalls[0])
	}
	rows, ok := trace.ToolCalls[0].Result.(map[string]any)["rows"].([]map[string]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("unexpected query rows: %#v", trace.ToolCalls[0].Result)
	}
	if trace.ToolCalls[1].ToolName != "send_to_wecom" || trace.ToolCalls[1].Error != "" {
		t.Fatalf("send_to_wecom failed: %#v", trace.ToolCalls[1])
	}
	if toString(trace.ToolCalls[1].Result) != "已发送到企业微信群" {
		t.Fatalf("unexpected wecom result: %v", trace.ToolCalls[1].Result)
	}
	if !strings.Contains(fix.LastPost, "active=42") {
		t.Fatalf("wecom webhook body missing query summary: %q", fix.LastPost)
	}
	for _, rec := range trace.ToolCalls {
		if rec.ToolName == "ask_user" {
			t.Fatal("should not call ask_user when mysql and wecom are bound")
		}
	}
	if fake.toolCalls < 3 {
		t.Fatalf("expected >=3 model rounds (2 tools + final), got %d", fake.toolCalls)
	}
}

func TestToolDiscoveryIntegration_JiraIssueViaToolSearchBridge(t *testing.T) {
	t.Setenv("SATH_TOOL_SEARCH", "on")

	reg := tool.NewRegistry()
	issueCreated := false
	if err := reg.Register(tool.Tool{
		Name:        "mcp__jira__create_issue",
		Description: "Create a Jira issue in the bound project",
		Toolset:     tool.ToolsetMCP,
		SearchHints: []string{"jira", "issue", "ticket"},
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"summary": map[string]any{"type": "string"},
			},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			_ = ctx
			issueCreated = true
			summary, _ := params["summary"].(string)
			return map[string]any{"key": "PROJ-99", "summary": summary}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	catalog, _ := wireDiscoveryAgent(t, reg)

	fake := &discoveryFakeModel{
		toolSteps: []model.ToolStep{
			{
				Used:     true,
				ToolName: tool.ToolSearchName,
				Arguments: map[string]any{
					"query": "jira create issue",
					"limit": 3,
				},
			},
			{
				Used:     true,
				ToolName: tool.ToolCallName,
				Arguments: map[string]any{
					"name": "mcp__jira__create_issue",
					"arguments": map[string]any{
						"summary": "Fix login bug",
					},
				},
			},
		},
		finalReply: "已在 Jira 创建 PROJ-99。",
	}
	mem := memory.NewBufferMemory(8)
	react := agent.NewReActAgent(fake, mem, reg)

	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "sess-e2e-jira")
	ctx = context.WithValue(ctx, tool.ContextKeyToolCatalog, catalog)
	ctx = context.WithValue(ctx, tool.ContextKeyToolSearchActive, true)

	resp, err := react.Run(ctx, &agent.Request{
		Messages: []model.Message{{Role: "user", Content: "帮我在 Jira 建个 issue，标题 Fix login bug"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !issueCreated {
		t.Fatal("mcp__jira__create_issue should have executed via tool_call bridge")
	}
	trace, ok := resp.Metadata["trace"].(*agent.RunTrace)
	if !ok || len(trace.ToolCalls) != 2 {
		t.Fatalf("expected tool_search + create_issue, got %#v", trace)
	}
	if trace.ToolCalls[0].ToolName != tool.ToolSearchName || trace.ToolCalls[0].Error != "" {
		t.Fatalf("tool_search failed: %#v", trace.ToolCalls[0])
	}
	if !strings.Contains(toString(trace.ToolCalls[0].Result), "mcp__jira__create_issue") {
		t.Fatalf("tool_search should surface jira tool, got %v", trace.ToolCalls[0].Result)
	}
	if trace.ToolCalls[1].ToolName != "mcp__jira__create_issue" || trace.ToolCalls[1].Error != "" {
		t.Fatalf("tool_call unwrap failed: %#v", trace.ToolCalls[1])
	}
}

// credentialThenToolFakeModel 首轮返回索凭文本，后续按 toolSteps 驱动工具调用。
type credentialThenToolFakeModel struct {
	credentialAsk string
	toolSteps     []model.ToolStep
	finalReply    string
	calls         int
}

func (f *credentialThenToolFakeModel) Generate(context.Context, string, ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: f.finalReply}, nil
}

func (f *credentialThenToolFakeModel) Chat(context.Context, []model.Message, ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: f.finalReply}, nil
}

func (f *credentialThenToolFakeModel) Embed(context.Context, []string, ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

func (f *credentialThenToolFakeModel) ChatWithTools(ctx context.Context, messages []model.Message, reg *tool.Registry, opts ...model.Option) (*model.Generation, error) {
	_ = ctx
	_ = messages
	_ = reg
	_ = opts
	f.calls++
	if f.calls == 1 {
		return &model.Generation{Text: f.credentialAsk, Raw: model.ToolStep{Used: false}}, nil
	}
	if len(f.toolSteps) > 0 {
		step := f.toolSteps[0]
		f.toolSteps = f.toolSteps[1:]
		return &model.Generation{Text: f.finalReply, Raw: step}, nil
	}
	return &model.Generation{Text: f.finalReply, Raw: model.ToolStep{Used: false}}, nil
}

func TestToolDiscoveryIntegration_PlainTextCredentialAskGetsRedirected(t *testing.T) {
	fix := setupMysqlWecomDiscoveryFixture(t, true)

	fake := &credentialThenToolFakeModel{
		credentialAsk: "请提供 MySQL Host、Port、数据库名、用户名、密码，以及企微 Webhook URL",
		toolSteps: []model.ToolStep{{
			Used:     true,
			ToolName: "execute_read",
			Arguments: map[string]any{
				"datasource_id": "prod_mysql",
				"sql":           "SELECT status, COUNT(*) AS cnt FROM t_archive_clean_task_detail GROUP BY status",
			},
		}},
		finalReply: "统计完成。",
	}
	mem := memory.NewBufferMemory(8)
	react := agent.NewReActAgent(fake, mem, fix.Reg)

	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "sess-e2e-text-guard")
	ctx = context.WithValue(ctx, tool.ContextKeyToolCatalog, fix.Catalog)

	resp, err := react.Run(ctx, &agent.Request{
		Messages: []model.Message{{
			Role:    "user",
			Content: "查 archive 库 t_archive_clean_task_detail 按 status 分组统计",
		}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	trace, ok := resp.Metadata["trace"].(*agent.RunTrace)
	if !ok {
		t.Fatalf("missing trace: %#v", resp.Metadata)
	}
	redirected := false
	for _, e := range trace.Errors {
		if strings.Contains(e, "credential_solicitation_redirect") {
			redirected = true
		}
	}
	if !redirected {
		t.Fatalf("expected credential redirect in trace errors, got %#v", trace.Errors)
	}
	if len(trace.ToolCalls) == 0 || trace.ToolCalls[0].ToolName != "execute_read" {
		t.Fatalf("expected execute_read after redirect, got %#v", trace.ToolCalls)
	}
}

func TestToolDiscoveryIntegration_AskUserBlockedForWecomWebhook(t *testing.T) {
	fix := setupMysqlWecomDiscoveryFixture(t, true)

	fake := &discoveryFakeModel{
		toolSteps: []model.ToolStep{{
			Used:     true,
			ToolName: "ask_user",
			Arguments: map[string]any{
				"prompt": "请提供企业微信群机器人 Webhook URL（qyapi.weixin.qq.com）",
				"kind":   "text",
				"field":  "wecom_webhook",
			},
		}},
		finalReply: "将改用 send_to_wecom",
	}
	mem := memory.NewBufferMemory(5)
	react := agent.NewReActAgent(fake, mem, fix.Reg)

	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "sess-e2e-wecom-guard")
	ctx = context.WithValue(ctx, tool.ContextKeyToolCatalog, fix.Catalog)

	resp, err := react.Run(ctx, &agent.Request{
		Messages: []model.Message{{Role: "user", Content: "把结论推到企微群"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	trace, ok := resp.Metadata["trace"].(*agent.RunTrace)
	if !ok || len(trace.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %#v", resp.Metadata)
	}
	result := toString(trace.ToolCalls[0].Result)
	if !strings.Contains(result, "ask_user blocked") || !strings.Contains(result, "send_to_wecom") {
		t.Fatalf("expected guard redirect to send_to_wecom, got: %q", result)
	}
}

func catalogEntryNames(cat tool.ToolCatalog) []string {
	out := make([]string, 0, len(cat.Entries))
	for _, e := range cat.Entries {
		out = append(out, e.Name)
	}
	return out
}

func containsString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
