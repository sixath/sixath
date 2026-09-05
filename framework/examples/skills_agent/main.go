package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"

	"github.com/sixath/framework/config"
	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/middleware"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/skills"
	"github.com/sixath/framework/templates"
)

// 本示例演示如何使用 Skills-aware Chat Handler：从配置目录加载 Skills，
// 在 System Prompt 中注入摘要，并通过 load_skill 工具按需加载 Skill 正文。
//
// 前置条件：
//   - 设置 OPENAI_API_KEY（必填）；可选 OPENAI_BASE_URL、OPENAI_MODEL。
//   - 在仓库根目录运行，且存在 skills_examples 目录（或通过 -dirs 指定）。
//
// 运行示例（在仓库根目录）：
//
//	set OPENAI_API_KEY=your_key
//	go run ./examples/skills_agent -message "帮我设计一个产品介绍落地页"
//	go run ./examples/skills_agent -dirs skills_examples -message "分析一下技术部有多少人"
func main() {
	dirs := flag.String("dirs", "D:\\workspace\\github\\sixath\\framework\\skills_examples", "comma-separated skills directories to scan (relative to cwd)")
	message := flag.String("message", "分析vmid=29155预启动失败的原因?esUrl=http://10.137.212.70:29200", "user message")

	flag.Parse()
	/*	os.Setenv("OPENAI_API_KEY", "sk-DKPXsITvLkWVdtN_gTRPdTmv9ILx3VJ1FVy9AI8ur4R9P_3oM4INlil_Or8")
		os.Setenv("OPENAI_BASE_URL", "http://10.86.3.248:3000/v1")*/
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
	cfg.Skills.AllowScriptExecution = true
	var skillsDirs []string
	for _, d := range splitTrim(*dirs, ",") {
		if d != "" {
			skillsDirs = append(skillsDirs, d)
		}
	}
	if len(skillsDirs) == 0 {
		skillsDirs = []string{"skills_examples"}
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
		RequestID: "skills-example-1",
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
