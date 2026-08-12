package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sixath/framework/events"
	"strings"
	"time"

	markclient "github.com/mark3labs/mcp-go/client"
	marktransport "github.com/mark3labs/mcp-go/client/transport"
	markmcp "github.com/mark3labs/mcp-go/mcp"
	mcpmetoro "github.com/metoro-io/mcp-golang"
	mcphttp "github.com/metoro-io/mcp-golang/transport/http"
	"github.com/sixath/framework/obs"
)

// mcpClient 抽象 MCP 客户端能力，便于在 mcpTool 中解耦具体实现。
type mcpClient interface {
	Initialize(ctx context.Context) error
	ListTools(ctx context.Context) ([]Tool, error)
	CallTool(ctx context.Context, name string, args map[string]any) (string, error)
	Close(ctx context.Context) error
}

// mcpTool 仅依赖 mcpClient 接口，不关心底层使用哪个库。
type mcpTool struct {
	client mcpClient
}

// McpConfig MCP 工具运行时配置，与 portal API 的 mcp 嵌套结构对齐。
type McpConfig struct {
	Transport  string // ""|"http"|"stdio"；空且 Endpoint 非空 → http
	Endpoint   string
	Id         string
	Backend    string
	Command    string
	Args       []string
	Env        map[string]string
	TimeoutSec int
	mcpTool    *mcpTool
}

// McpConfigFromMap 从 map（如 portal 存储的 config）解析为 McpConfig。
// 支持 mcp 嵌套或扁平字段（mcp_endpoint、mcp_transport、mcp_command 等）。
func McpConfigFromMap(m map[string]interface{}) *McpConfig {
	if m == nil {
		return nil
	}
	c := &McpConfig{}
	if v, ok := m["mcp"]; ok {
		if nested, ok := v.(map[string]interface{}); ok {
			applyMcpNestedFields(c, nested)
		}
	}
	if c.Endpoint == "" {
		if s, ok := m["mcp_endpoint"].(string); ok {
			c.Endpoint = s
		}
	}
	if c.Id == "" {
		if s, ok := m["mcp_server_id"].(string); ok {
			c.Id = s
		}
	}
	if c.Backend == "" {
		if s, ok := m["mcp_backend"].(string); ok {
			c.Backend = s
		}
	}
	if c.Transport == "" {
		if s, ok := m["mcp_transport"].(string); ok {
			c.Transport = s
		}
	}
	if c.Command == "" {
		if s, ok := m["mcp_command"].(string); ok {
			c.Command = s
		}
	}
	if len(c.Args) == 0 {
		if v, ok := m["mcp_args"]; ok {
			c.Args = parseMcpStringSlice(v)
		}
	}
	if c.Env == nil {
		if v, ok := m["mcp_env"]; ok {
			c.Env = parseMcpStringMap(v)
		}
	}
	if c.TimeoutSec == 0 {
		if v, ok := m["mcp_timeout_sec"]; ok {
			c.TimeoutSec = parseMcpTimeoutSec(v)
		}
	}
	if c.Endpoint == "" && c.Id == "" && c.Backend == "" && c.Transport == "" && c.Command == "" {
		return nil
	}
	return c
}

func applyMcpNestedFields(c *McpConfig, nested map[string]interface{}) {
	if s, ok := nested["endpoint"].(string); ok {
		c.Endpoint = s
	}
	if s, ok := nested["id"].(string); ok {
		c.Id = s
	}
	if s, ok := nested["backend"].(string); ok {
		c.Backend = s
	}
	if s, ok := nested["transport"].(string); ok {
		c.Transport = s
	}
	if s, ok := nested["command"].(string); ok {
		c.Command = s
	}
	if v, ok := nested["args"]; ok {
		c.Args = parseMcpStringSlice(v)
	}
	if v, ok := nested["env"]; ok {
		c.Env = parseMcpStringMap(v)
	}
	if v, ok := nested["timeout_sec"]; ok {
		c.TimeoutSec = parseMcpTimeoutSec(v)
	}
}

func parseMcpStringSlice(v interface{}) []string {
	switch a := v.(type) {
	case []string:
		return a
	case []interface{}:
		out := make([]string, 0, len(a))
		for _, item := range a {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func parseMcpStringMap(v interface{}) map[string]string {
	switch m := v.(type) {
	case map[string]string:
		return m
	case map[string]interface{}:
		out := make(map[string]string, len(m))
		for k, val := range m {
			if s, ok := val.(string); ok {
				out[k] = s
			}
		}
		return out
	default:
		return nil
	}
}

func parseMcpTimeoutSec(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func isStdioMcpTransport(cfg *McpConfig) bool {
	return cfg != nil && strings.EqualFold(strings.TrimSpace(cfg.Transport), "stdio")
}

// NewMcpTool 根据配置创建底层 MCP 客户端，并包装为 mcpTool。
// stdio 走进程池（Acquire on Initialize/ListTools/CallTool）；HTTP 保持直连客户端。
func NewMcpTool(cfg *McpConfig) (*mcpTool, error) {
	if cfg == nil {
		return nil, fmt.Errorf("mcp: config is nil")
	}
	if isStdioMcpTransport(cfg) {
		if err := ValidateStdioMcp(cfg.Command, cfg.Args, cfg.Env); err != nil {
			return nil, err
		}
		backend := strings.ToLower(strings.TrimSpace(cfg.Backend))
		if backend == "" {
			backend = "mark3labs"
		}
		if backend != "mark3labs" {
			return nil, fmt.Errorf("mcp: stdio transport requires mark3labs backend, got %q", cfg.Backend)
		}
		return &mcpTool{client: &stdioPoolClientAdapter{cfg: cfg}}, nil
	}
	backend := strings.ToLower(strings.TrimSpace(cfg.Backend))
	if backend == "" {
		backend = "metoro"
	}
	var cli mcpClient
	var err error
	switch backend {
	case "mark3labs":
		cli, err = newMark3labsClient(cfg.Endpoint)
	default:
		cli, err = newMetoroClient(cfg.Endpoint)
	}
	if err != nil {
		return nil, err
	}
	return &mcpTool{client: cli}, nil
}

// Initialize 初始化 MCP 会话
func (c *mcpTool) Initialize(ctx context.Context) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("mcp: client is nil")
	}
	return c.client.Initialize(ctx)
}

// ListTools 获取工具列表
func (c *mcpTool) ListTools(ctx context.Context) ([]Tool, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("mcp: client is nil")
	}
	return c.client.ListTools(ctx)
}

// Close 关闭客户端
func (c *mcpTool) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close(context.Background())
}

func RegisterMcpTool(r *Registry, cfg *McpConfig, opts ...*RegisterToolOptions) {
	if r == nil || cfg == nil {
		return
	}
	if r.HasMcpServer(cfg.Id) {
		return
	}
	if isStdioMcpTransport(cfg) {
		registerStdioMcpTool(r, cfg)
		return
	}
	mcpTool, err := NewMcpTool(cfg)
	if err != nil {
		return
	}
	err = mcpTool.Initialize(context.Background())
	if err != nil {
		return
	}
	tools, err := mcpTool.ListTools(context.Background())
	if err != nil {
		return
	}
	cfg.mcpTool = mcpTool
	for _, tool := range tools {
		tool.Execute = buildMcpExecute(cfg, tool.Name)
		if tool.Toolset == "" {
			tool.Toolset = ToolsetMCP
		}
		tool.Bindings = map[string]string{"mcp_server": cfg.Id}
		tool.SearchHints = splitMcpServerHints(cfg.Id, tool.Name)
		r.Register(tool)
	}
	r.MarkMcpServer(cfg.Id)
}

func registerStdioMcpTool(r *Registry, cfg *McpConfig) {
	if err := ValidateStdioMcp(cfg.Command, cfg.Args, cfg.Env); err != nil {
		return
	}
	backend := strings.ToLower(strings.TrimSpace(cfg.Backend))
	if backend == "" {
		backend = "mark3labs"
	}
	if backend != "mark3labs" {
		return
	}
	pool := DefaultMcpProcessPool()
	cli, release, err := pool.Acquire(context.Background(), cfg)
	if err != nil {
		return
	}
	tools, ok := pool.CachedTools(cfg.Id)
	if !ok {
		tools, err = cli.ListTools(context.Background())
		if err != nil {
			release()
			return
		}
		pool.StoreTools(cfg.Id, tools)
	}
	// Execute 走池；mcpTool 可为空，CallTool 不依赖长连接句柄。
	cfg.mcpTool = &mcpTool{client: &stdioPoolClientAdapter{cfg: cfg}}
	for _, t := range tools {
		t.Execute = buildStdioMcpExecute(cfg, t.Name)
		if t.Toolset == "" {
			t.Toolset = ToolsetMCP
		}
		t.Bindings = map[string]string{"mcp_server": cfg.Id}
		t.SearchHints = splitMcpServerHints(cfg.Id, t.Name)
		_ = r.Register(t)
	}
	r.MarkMcpServer(cfg.Id)
	release()
}

func buildMcpExecute(cfg *McpConfig, toolName string) ExecuteFunc {
	return func(ctx context.Context, params map[string]any) (any, error) {
		start := time.Now()
		status := "ok"
		defer func() {
			obs.ObserveDataQueryTool(toolName, status, time.Since(start))
		}()

		if cfg == nil {
			status = "error"
			return nil, errors.New("mcp: not configured ")
		}

		if cfg.mcpTool == nil || cfg.mcpTool.client == nil {
			status = "error"
			return nil, fmt.Errorf("mcp: client not initialized")
		}

		responseText, err := cfg.mcpTool.client.CallTool(ctx, toolName, params)
		if err != nil {
			status = "error"
			return "", fmt.Errorf("failed to call MCP tool %s: %w", toolName, err)
		}
		return responseText, nil
	}
}

func buildStdioMcpExecute(cfg *McpConfig, toolName string) ExecuteFunc {
	return func(ctx context.Context, params map[string]any) (any, error) {
		start := time.Now()
		status := "ok"
		defer func() {
			obs.ObserveDataQueryTool(toolName, status, time.Since(start))
		}()
		if cfg == nil {
			status = "error"
			return nil, errors.New("mcp: not configured ")
		}
		cli, release, err := DefaultMcpProcessPool().Acquire(ctx, cfg)
		if err != nil {
			status = "error"
			return nil, err
		}
		defer release()
		responseText, err := cli.CallTool(ctx, toolName, params)
		if err != nil {
			status = "error"
			return "", fmt.Errorf("failed to call MCP tool %s: %w", toolName, err)
		}
		return responseText, nil
	}
}

// stdioPoolClientAdapter 通过 DefaultMcpProcessPool 完成 Initialize/List/Call。
type stdioPoolClientAdapter struct {
	cfg *McpConfig
}

func (a *stdioPoolClientAdapter) Initialize(ctx context.Context) error {
	cli, release, err := DefaultMcpProcessPool().Acquire(ctx, a.cfg)
	if err != nil {
		return err
	}
	_ = cli
	release()
	return nil
}

func (a *stdioPoolClientAdapter) ListTools(ctx context.Context) ([]Tool, error) {
	pool := DefaultMcpProcessPool()
	if tools, ok := pool.CachedTools(a.cfg.Id); ok {
		return tools, nil
	}
	cli, release, err := pool.Acquire(ctx, a.cfg)
	if err != nil {
		return nil, err
	}
	defer release()
	tools, err := cli.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	pool.StoreTools(a.cfg.Id, tools)
	return tools, nil
}

func (a *stdioPoolClientAdapter) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	cli, release, err := DefaultMcpProcessPool().Acquire(ctx, a.cfg)
	if err != nil {
		return "", err
	}
	defer release()
	return cli.CallTool(ctx, name, args)
}

func (a *stdioPoolClientAdapter) Close(ctx context.Context) error {
	return nil
}

// metoroClientAdapter 基于 github.com/metoro-io/mcp-golang 的客户端实现 mcpClient。
type metoroClientAdapter struct {
	cli *mcpmetoro.Client
}

func newMetoroClient(endpoint string) (mcpClient, error) {

	transport := mcphttp.NewHTTPClientTransport(endpoint)
	transport.WithHeader("Accept", "application/json, text/event-stream")

	// 创建 MCP 客户端
	cli := mcpmetoro.NewClient(transport)

	adapter := &metoroClientAdapter{
		cli: cli,
	}

	return adapter, nil
	/*httpTransport := mcphttp.NewHTTPClientTransport("/mcp")
	httpTransport.WithBaseURL(endpoint)
		cli := mcpmetoro.NewClient(httpTransport)
		return &metoroClientAdapter{cli: cli}, nil*/
}

func (a *metoroClientAdapter) Initialize(ctx context.Context) error {
	if a.cli == nil {
		return fmt.Errorf("metoro client is nil")
	}
	_, err := a.cli.Initialize(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialize metoro MCP client: %w", err)
	}
	return nil
}

func (a *metoroClientAdapter) ListTools(ctx context.Context) ([]Tool, error) {
	if a.cli == nil {
		return nil, fmt.Errorf("metoro client is nil")
	}
	res, err := a.cli.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list MCP tools (metoro): %w", err)
	}
	out := make([]Tool, 0, len(res.Tools))
	for _, t := range res.Tools {
		out = append(out, Tool{
			Name:        t.Name,
			Description: derefString(t.Description),
			Parameters:  t.InputSchema,
		})
	}
	return out, nil
}

func (a *metoroClientAdapter) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	if a.cli == nil {
		return "", fmt.Errorf("metoro client is nil")
	}
	resp, err := a.cli.CallTool(ctx, name, args)
	rid, _ := ctx.Value(ContextKeyRequestID).(string)
	invokedPayload := map[string]any{
		"mcpTooName": name,
	}
	events.DefaultBus().Publish(ctx, events.Event{
		Kind:      events.ToolExecuted,
		RequestID: rid,
		Payload:   invokedPayload,
	})
	if err != nil {
		return "", err
	}
	if resp == nil || len(resp.Content) == 0 {
		return "工具执行完成", nil
	}
	var b strings.Builder
	for _, c := range resp.Content {
		if c.TextContent != nil {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(c.TextContent.Text)
			continue
		}
		if data, err := json.Marshal(c); err == nil {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(string(data))
		}
	}
	if b.Len() == 0 {
		return "工具执行完成", nil
	}
	return b.String(), nil
}

func (a *metoroClientAdapter) Close(ctx context.Context) error {
	// 当前 mcp-golang 客户端未暴露 Close 方法，这里直接返回 nil。
	return nil
}

// mark3labsClientAdapter 基于 github.com/mark3labs/mcp-go 的客户端实现 mcpClient。
type mark3labsClientAdapter struct {
	cli *markclient.Client
}

func newMark3labsClient(endpoint string) (mcpClient, error) {
	httpTransport, err := marktransport.NewStreamableHTTP(
		endpoint,
		marktransport.WithHTTPHeaders(map[string]string{
			"Accept": "application/json, text/event-stream",
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create mark3labs HTTP transport: %w", err)
	}
	cli := markclient.NewClient(httpTransport)
	if err := cli.Start(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to start mark3labs MCP client: %w", err)
	}
	return &mark3labsClientAdapter{cli: cli}, nil
}

func newMark3labsStdioClient(cfg *McpConfig) (mcpClient, error) {
	if cfg == nil {
		return nil, fmt.Errorf("mcp: config is nil")
	}
	envSlice := make([]string, 0, len(cfg.Env))
	for k, v := range cfg.Env {
		envSlice = append(envSlice, k+"="+v)
	}
	cmd, err := ResolveStdioMcpCommand(cfg.Command)
	if err != nil {
		return nil, err
	}
	cli, err := markclient.NewStdioMCPClient(cmd, envSlice, cfg.Args...)
	if err != nil {
		return nil, fmt.Errorf("failed to create mark3labs stdio client: %w", err)
	}
	if err := cli.Start(context.Background()); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("failed to start mark3labs stdio client: %w", err)
	}
	return &mark3labsClientAdapter{cli: cli}, nil
}

func (a *mark3labsClientAdapter) Initialize(ctx context.Context) error {
	if a.cli == nil {
		return fmt.Errorf("mark3labs client is nil")
	}
	// 初始化 MCP 会话（必须调用，否则客户端未初始化）
	initRequest := markmcp.InitializeRequest{
		Params: markmcp.InitializeParams{
			ProtocolVersion: markmcp.LATEST_PROTOCOL_VERSION,
			Capabilities:    markmcp.ClientCapabilities{},
			ClientInfo: markmcp.Implementation{
				Name:    "sath",
				Version: "1.0.0",
			},
		},
	}
	if _, err := a.cli.Initialize(ctx, initRequest); err != nil {
		return fmt.Errorf("failed to initialize mark3labs MCP client: %w", err)
	}
	return nil
}

func (a *mark3labsClientAdapter) ListTools(ctx context.Context) ([]Tool, error) {
	if a.cli == nil {
		return nil, fmt.Errorf("mark3labs client is nil")
	}
	res, err := a.cli.ListTools(ctx, markmcp.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to list MCP tools (mark3labs): %w", err)
	}
	out := make([]Tool, 0, len(res.Tools))
	for _, t := range res.Tools {
		out = append(out, Tool{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.InputSchema,
		})
	}
	return out, nil
}

func (a *mark3labsClientAdapter) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	if a.cli == nil {
		return "", fmt.Errorf("mark3labs client is nil")
	}
	req := markmcp.CallToolRequest{
		Params: markmcp.CallToolParams{
			Name:      name,
			Arguments: args,
		},
	}
	resp, err := a.cli.CallTool(ctx, req)
	if err != nil {
		return "", err
	}
	if resp == nil || len(resp.Content) == 0 {
		return "工具执行完成", nil
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return "工具执行完成", nil
	}
	return string(data), nil
}

func (a *mark3labsClientAdapter) Close(ctx context.Context) error {
	if a.cli == nil {
		return nil
	}
	return a.cli.Close()
}

// derefString 将 *string 转为 string，nil 时返回空串。
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
