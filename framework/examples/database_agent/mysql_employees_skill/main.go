package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/sixath/framework/config"
	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/templates"
)

// 本示例演示在 DataQuery Agent 中使用 mysql-employees-analysis Skill：
// 配置中指定 skills_dirs 后，Agent 会在系统提示中看到该 Skill 摘要，并可调用 load_skill 加载完整指南，
// 再按 Skill 中的工作流（list_tables → describe_table → execute_read）分析 MySQL employees 示例库。
//
// 前置条件：
//   - 已安装 MySQL employees 示例库（https://github.com/datacharmer/test_db）；
//   - 修改 config.yaml 中的 data_sources 连接信息；
//   - 设置 OPENAI_API_KEY（必填）；可选 OPENAI_BASE_URL。
//
// 运行示例（在仓库根目录）：
//
//	export OPENAI_API_KEY=your_key
//	go run ./examples/database_agent/mysql_employees_skill -config ./examples/database_agent/mysql_employees_skill/config.yaml
//	go run ./examples/database_agent/mysql_employees_skill -config ./examples/database_agent/mysql_employees_skill/config.yaml -message "技术部有多少人？平均薪资多少？"
//
// 或进入示例目录后：
//
//	cd examples/database_agent/mysql_employees_skill
//	go run . -config config.yaml -message "统计各部门当前员工人数"
func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config file (with data_sources + skills.skills_dirs)")
	message := flag.String("message", "请先加载 mysql-employees-analysis 技能，然后统计各部门当前员工人数。", "user message")
	flag.Parse()

	if os.Getenv("OPENAI_API_KEY") == "" {
		log.Fatal("OPENAI_API_KEY is required")
	}

	cfg, err := config.LoadWithEnv(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	if len(cfg.Skills.Dirs) == 0 {
		log.Print("warning: no skills_dirs in config; add skills.skills_dirs (e.g. [\"../../../skills_examples\"]) to use mysql-employees-analysis")
	}

	mwMap := templates.DefaultMiddlewareMap()
	handler, err := templates.NewDataQueryHandlerFromConfig(cfg, mwMap)
	if err != nil {
		log.Fatalf("build data query handler: %v", err)
	}

	req := &agent.Request{
		Messages: []model.Message{
			{Role: "user", Content: *message},
		},
		RequestID: "mysql-employees-skill-1",
		Metadata: map[string]any{
			"session_id":    "demo",
			"user_id":       "tester",
			"datasource_id": "main",
		},
	}

	resp, err := handler(context.Background(), req)
	if err != nil {
		log.Fatalf("data query: %v", err)
	}
	if resp == nil {
		log.Fatal("data query: nil response")
	}
	log.Printf("reply: %s", resp.Text)
}
