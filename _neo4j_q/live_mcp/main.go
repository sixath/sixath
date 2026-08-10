package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sixath/framework/tool"
)

func main() {
	cfg := &tool.McpConfig{
		Id: "confluence-live", Transport: "stdio", Command: "npx",
		Args:    []string{"-y", "@atlassian-dc-mcp/confluence"},
		Backend: "mark3labs",
		Env: map[string]string{
			"CONFLUENCE_HOST":      os.Getenv("CONFLUENCE_HOST"),
			"CONFLUENCE_API_TOKEN": os.Getenv("CONFLUENCE_API_TOKEN"),
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cli, release, err := tool.DefaultMcpProcessPool().Acquire(ctx, cfg)
	if err != nil {
		fmt.Println("ACQUIRE_ERR", err)
		os.Exit(1)
	}
	defer release()
	tools, err := cli.ListTools(ctx)
	if err != nil {
		fmt.Println("LIST_ERR", err)
		os.Exit(1)
	}
	fmt.Println("TOOLS", len(tools))
	for _, t := range tools {
		fmt.Println("TOOL", t.Name)
	}
	out, err := cli.CallTool(ctx, "confluence_searchSpace", map[string]any{"searchText": "a", "limit": 2})
	if err != nil {
		fmt.Println("CALL_ERR", err)
		os.Exit(2)
	}
	if len(out) > 400 {
		out = out[:400] + "..."
	}
	fmt.Println("CALL_OK", out)
}