package main

import (
	"context"
	"flag"
	"log"
	"os"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/middleware"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/templates"
)

// 本示例演示如何基于 MCPAgent 调用一个 MCP 服务提供的工具。
//
// 前置条件：
//   - 已部署或本地运行一个兼容 mark3labs/mcp-go 的 MCP 服务器，并提供至少一个工具。
//   - 设置 OPENAI_API_KEY（必填）；可选 OPENAI_BASE_URL。
//
// 运行示例（在仓库根目录）：
//
//	set OPENAI_API_KEY=your_key
//	go run ./examples/mcp_agent -endpoint http://localhost:8080/mcp -message "请帮我调用合适的 MCP 工具完成任务"
func main() {
	endpoint := flag.String("endpoint", "http://10.141.194.104:30750/mcp", "MCP server endpoint for StreamableHTTP client")
	message := flag.String("message", "请查询zone下有哪些pod", "user message")

	/*endpoint := flag.String("endpoint", "http://10.86.1.21:8081/mcp", "MCP server endpoint for StreamableHTTP client")
	message := flag.String("message", "区域4103，镜像126的资源分配情况", "user message")
	*/
	modelID := flag.String("model", "openai/gpt-3.5-turbo", "model identifier")

	flag.Parse()

	if os.Getenv("OPENAI_API_KEY") == "" {
		log.Fatal("OPENAI_API_KEY is required")
	}

	m, err := model.NewFromIdentifier(*modelID)
	if err != nil {
		log.Fatalf("create model: %v", err)
	}
	mem := memory.NewBufferMemory(10)

	// 简单中间件链：恢复 + 日志
	mws := []middleware.Middleware{
		middleware.RecoveryMiddleware,
		middleware.LoggingMiddleware,
	}

	handler := templates.NewMCPAgentHandler(m, mem, templates.Config{
		Endpoint:      *endpoint,
		ID:            "main-mcp",
		MaxReActSteps: 10,
		MaxHistory:    10,
		Backend:       "mark3labs",
	}, mws...)

	req := &agent.Request{
		Messages: []model.Message{
			{Role: "user", Content: *message},
		},
		RequestID: "mcp-example-1",
	}

	resp, err := handler(context.Background(), req)
	if err != nil {
		log.Fatalf("mcp agent error: %v", err)
	}
	if resp == nil {
		log.Fatal("mcp agent: nil response")
	}

	log.Printf("reply: %s", resp.Text)
}
