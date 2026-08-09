package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/sixath/framework/events"
	"log"
	"os"
	"strings"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/config"
	"github.com/sixath/framework/middleware"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/skills"
	"github.com/sixath/framework/templates"
)

// 本示例演示如何使用带 Python 脚本的 Skill（python-data-helper）。
//
// 从 -dirs 指定目录扫描 Skills（默认 skills_examples），并开启脚本执行与 .py 扩展名，
// 模型可先 load_skill("python-data-helper")，再通过 execute_skill_script 执行 scripts/*.py。
//
// 前置条件：
//   - 设置 OPENAI_API_KEY（必填）；可选 OPENAI_BASE_URL、OPENAI_MODEL。
//   - 本机已安装 python3（用于执行 .py 脚本）。
//
// 运行示例（在仓库根目录）：
//
//	set OPENAI_API_KEY=your_key
//	go run ./examples/python_skill_agent
//	go run ./examples/python_skill_agent -message "请加载 python-data-helper 并执行 scripts/version.py 验证脚本能力"
//	go run ./examples/python_skill_agent -dirs skills_examples -message "用 python-data-helper 做一次 JSON 格式化的说明"
func main() {

	dirs := flag.String("dirs", "D:\\workspace\\github\\sath\\skills_examples", "comma-separated skills directories (e.g. skills_examples)")
	message := flag.String("message", "请先加载 python-data-helper 技能，然后执行 scripts/version.py 并告诉我输出结果。", "user message")

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

	// 开启脚本执行并允许 .sh / .py，以便执行 python-data-helper 下的 Python 脚本
	cfg.Skills.AllowScriptExecution = true
	if len(cfg.Skills.ScriptAllowedExtensions) == 0 {
		cfg.Skills.ScriptAllowedExtensions = []string{".sh", ".py"}
	}

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
	bus := events.NewBus()
	events.SetDefaultBus(bus)
	bus.Subscribe(false, func(ctx context.Context, e events.Event) {
		fmt.Println(fmt.Sprintf("listen event [%#v]", e))
	})
	handler, err := templates.NewSkillsAwareChatHandlerFromConfig(cfg, idx, middlewareByName)
	if err != nil {
		log.Fatalf("create handler: %v", err)
	}

	req := &agent.Request{
		Messages: []model.Message{
			{Role: "user", Content: *message},
		},
		RequestID: "python-skill-example-1",
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
