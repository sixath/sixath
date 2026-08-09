package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/config"
	"github.com/sixath/framework/middleware"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/skills"
	"github.com/sixath/framework/templates"
)

// 本示例演示如何在使用 Skills-aware Chat Handler 时加载并使用 k8s-ops Skill。
//
// 会从 -dirs 指定目录扫描 SKILL.md（默认包含 examples/k8s-skill），模型在收到 k8s 相关问题时
// 会先调用 load_skill("k8s-ops") 获取操作指南，再按技能中的工作流回答或调用 MCP 工具。
//
// 若配置了 mcp-k8s（通过 -mcp-endpoint），加载 k8s-ops 后该 MCP 的工具会注册到当前上下文。
//
// 前置条件：
//   - 设置 OPENAI_API_KEY（必填）；可选 OPENAI_BASE_URL、OPENAI_MODEL。
//
// 运行示例（在仓库根目录）：
//
//	set OPENAI_API_KEY=your_key
//	go run ./examples/k8s_agent
//	go run ./examples/k8s_agent -message "default 命名空间下有哪些 Pod？请先加载 k8s-ops 技能。"
//	go run ./examples/k8s_agent -dirs examples/k8s-skill -mcp-endpoint http://localhost:8080/mcp -message "列出 default 的 Pod"
func main() {
	// 默认 skills 目录：优先当前目录下的 examples/k8s-skill，便于在仓库根目录运行
	defaultDirs := "D:\\workspace\\github\\sath\\skills_examples\\k8s-skill"
	if wd, err := os.Getwd(); err == nil {
		// 若当前目录已是 examples/k8s_agent，则上级的 k8s-skill
		if strings.HasSuffix(wd, "k8s_agent") {
			defaultDirs = filepath.Join("..", "k8s-skill")
		}
	}

	dirs := flag.String("dirs", defaultDirs, "comma-separated skills directories to scan (e.g. examples/k8s-skill)")
	message := flag.String("message", "请先加载 k8s-ops 技能，然后告诉 zone 命名空间下的 access-service 的Pod有什么问题常。", "user message about k8s")
	mcpEndpoint := flag.String("mcp-endpoint", "http://10.141.194.104:30750/mcp", "optional: MCP server endpoint for mcp-k8s (id mcp-k8s); when set, loading k8s-ops will register this MCP")
	mcpBackend := flag.String("mcp-backend", "mark3labs", "optional: MCP backend when mcp-endpoint is set (metoro or mark3labs)")
	flag.Parse()

	if os.Getenv("OPENAI_API_KEY") == "" {
		log.Fatal("OPENAI_API_KEY is required")
	}

	cfg := config.FromEnv()
	if cfg.ModelName == "" {
		cfg.ModelName = "openai/gpt-3.5-turbo"
	}
	if cfg.MaxHistory <= 0 {
		cfg.MaxHistory = 30
	}

	var skillsDirs []string
	for _, d := range splitTrim(*dirs, ",") {
		if d != "" {
			skillsDirs = append(skillsDirs, d)
		}
	}
	if len(skillsDirs) == 0 {
		skillsDirs = []string{"examples/k8s-skill"}
	}

	// 可选：配置 mcp-k8s，便于加载 k8s-ops 后使用 MCP 工具
	if *mcpEndpoint != "" {
		cfg.Skills.MCPServers = append(cfg.Skills.MCPServers, config.MCPServerEntry{
			Endpoint: *mcpEndpoint,
			ID:       "mcp-k8s",
			Backend:  *mcpBackend,
		})
	}

	idx, err := skills.NewIndex(skillsDirs, nil, nil)
	if err != nil {
		log.Fatalf("build skills index: %v", err)
	}
	log.Printf("loaded %d skill(s) from %v", len(idx.All()), skillsDirs)

	middlewareByName := map[string]middleware.Middleware{}
	handler, err := templates.NewSkillsAwareChatHandlerFromConfig(cfg, idx, middlewareByName)
	if err != nil {
		log.Fatalf("create handler: %v", err)
	}

	req := &agent.Request{
		Messages: []model.Message{
			{Role: "user", Content: *message},
		},
		RequestID: "k8s-agent-example-1",
	}

	resp, err := handler(context.Background(), req)
	if err != nil {
		log.Fatalf("handler error: %v", err)
	}
	if resp == nil {
		log.Fatal("handler returned nil response")
	}

	log.Printf("reply: %s", resp.Text)
}

func splitTrim(s, sep string) []string {
	var out []string
	for _, p := range strings.Split(s, sep) {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}
