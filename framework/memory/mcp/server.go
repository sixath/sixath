package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/tool"
	toolmem "github.com/sixath/framework/tool/memory"
)

const (
	defaultServerName    = "sixath-memory"
	defaultServerVersion = "0.1.0"
)

// Options configures the Memory MCP server.
type Options struct {
	AgentWriteEnabled bool
	ServerName        string
	ServerVersion     string
	ExtraPaths        []string
}

// NewDefaultStore returns an in-memory MemoryStore (session units only).
func NewDefaultStore() memory.MemoryStore {
	return memory.NewFacade(memory.FacadeConfig{
		Session: memory.NewSessionMemory(),
	})
}

// NewServer builds an MCP server exposing memory_remember / memory_recall / memory_get.
func NewServer(store memory.MemoryStore, opts Options) (*server.MCPServer, error) {
	if store == nil {
		return nil, fmt.Errorf("memory mcp: store is nil")
	}
	name := strings.TrimSpace(opts.ServerName)
	if name == "" {
		name = defaultServerName
	}
	version := strings.TrimSpace(opts.ServerVersion)
	if version == "" {
		version = defaultServerVersion
	}

	reg := tool.NewRegistry()
	if err := toolmem.RegisterMemoryStoreTools(reg, store, toolmem.StoreToolsOptions{
		AgentWriteEnabled: opts.AgentWriteEnabled,
		ExtraPaths:        opts.ExtraPaths,
	}); err != nil {
		return nil, err
	}

	s := server.NewMCPServer(name, version, server.WithToolCapabilities(false))
	for _, name := range []string{"memory_remember", "memory_recall", "memory_get"} {
		tl, ok := reg.Get(name)
		if !ok {
			return nil, fmt.Errorf("memory mcp: missing tool %q", name)
		}
		mcpTool, handler := wrapTool(tl)
		s.AddTool(mcpTool, handler)
	}
	return s, nil
}

// ServeStdio serves the MCP server over stdin/stdout.
func ServeStdio(s *server.MCPServer) error {
	if s == nil {
		return fmt.Errorf("memory mcp: server is nil")
	}
	return server.ServeStdio(s)
}

// ListenAndServeHTTP serves the MCP server via Streamable HTTP on addr (e.g. ":8765").
func ListenAndServeHTTP(s *server.MCPServer, addr string) error {
	if s == nil {
		return fmt.Errorf("memory mcp: server is nil")
	}
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = ":8765"
	}
	httpServer := server.NewStreamableHTTPServer(s)
	return httpServer.Start(addr)
}

// Handler returns an http.Handler for embedding Streamable HTTP in another mux.
func Handler(s *server.MCPServer) http.Handler {
	return server.NewStreamableHTTPServer(s)
}

func wrapTool(tl tool.Tool) (mcplib.Tool, server.ToolHandlerFunc) {
	schema := toolInputSchema(tl)
	mcpTool := mcplib.Tool{
		Name:        tl.Name,
		Description: tl.Description + " MCP clients should pass user_id/session_id/agent_id/workspace_root as needed for scope.",
		InputSchema: schema,
	}
	handler := func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		args := req.GetArguments()
		if args == nil {
			args = map[string]any{}
		}
		ctx = withIdentity(ctx, args)
		result, err := tl.Execute(ctx, args)
		if err != nil {
			return mcplib.NewToolResultError(err.Error()), nil
		}
		return resultToMCP(result)
	}
	return mcpTool, handler
}

func withIdentity(ctx context.Context, args map[string]any) context.Context {
	if v := anyString(args["user_id"]); v != "" {
		ctx = context.WithValue(ctx, tool.ContextKeyUserID, v)
	}
	if v := anyString(args["session_id"]); v != "" {
		ctx = context.WithValue(ctx, tool.ContextKeySessionID, v)
	}
	if v := anyString(args["agent_id"]); v != "" {
		ctx = context.WithValue(ctx, tool.ContextKeyAgentID, v)
	}
	if v := anyString(args["workspace_root"]); v != "" {
		ctx = context.WithValue(ctx, tool.ContextKeyWorkspaceRoot, v)
	}
	return ctx
}

func anyString(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func toolInputSchema(tl tool.Tool) mcplib.ToolInputSchema {
	schema := mcplib.ToolInputSchema{Type: "object", Properties: map[string]any{}}
	params, _ := tl.Parameters.(map[string]any)
	if params == nil {
		params = map[string]any{}
	}
	if raw, ok := params["properties"].(map[string]any); ok {
		for k, v := range raw {
			schema.Properties[k] = v
		}
	}
	if req, ok := params["required"].([]string); ok {
		schema.Required = append([]string{}, req...)
	} else if reqAny, ok := params["required"].([]any); ok {
		for _, r := range reqAny {
			if s, ok := r.(string); ok {
				schema.Required = append(schema.Required, s)
			}
		}
	}
	// MCP identity fields (Agent path uses context instead).
	schema.Properties["user_id"] = map[string]any{"type": "string", "description": "User id for scope=user."}
	schema.Properties["session_id"] = map[string]any{"type": "string", "description": "Session id for scope=session."}
	schema.Properties["agent_id"] = map[string]any{"type": "string", "description": "Agent id (optional metadata)."}
	schema.Properties["workspace_root"] = map[string]any{"type": "string", "description": "Workspace root for scope=agent."}
	return schema
}

func resultToMCP(result any) (*mcplib.CallToolResult, error) {
	if result == nil {
		return mcplib.NewToolResultText("null"), nil
	}
	if s, ok := result.(string); ok {
		return mcplib.NewToolResultText(s), nil
	}
	b, err := json.Marshal(result)
	if err != nil {
		return mcplib.NewToolResultError(err.Error()), nil
	}
	return mcplib.NewToolResultText(string(b)), nil
}
