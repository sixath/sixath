package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/config"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/templates"
)

// 本示例演示如何基于 MongoDB 配置并调用 database_agent 做一次数据查询（集合列举 / 结构采样 / find 只读）。
//
// 需设置环境变量：OPENAI_API_KEY（必填）；可选 OPENAI_BASE_URL。
//
// 运行（在 framework 模块根目录 sixath/framework）：
//
//	set OPENAI_API_KEY=your_key
//	go run ./examples/database_agent/mongodb -config ./examples/database_agent/mongodb/config.mongodb.yaml
//
// 或进入示例目录后：
//
//	cd examples/database_agent/mongodb
//	go run . -config config.mongodb.yaml
func main() {
	cfgPath := flag.String("config", "D:\\workspace\\github\\sixath\\framework\\examples\\database_agent\\mongodb\\config.mongodb.yaml", "path to config file")
	message := flag.String("message", "当前数据库有哪些集合？若存在 plan_config 集合，请描述其字段并查询前 3 条文档示例。", "plan_config message to send to data query agent")
	flag.Parse()

	if os.Getenv("OPENAI_API_KEY") == "" {
		log.Fatal("OPENAI_API_KEY is required")
	}

	cfg, err := config.LoadWithEnv(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	mwMap := templates.DefaultMiddlewareMap()
	dataHandler, err := templates.NewDataQueryHandlerFromConfig(cfg, mwMap)
	if err != nil {
		log.Fatalf("build data query handler: %v", err)
	}

	req := &agent.Request{
		Messages: []model.Message{
			{Role: "user", Content: *message},
		},
		RequestID: "example-mongodb-1",
		Metadata: map[string]any{
			"session_id":    "demo",
			"user_id":       "tester",
			"datasource_id": "main",
		},
	}

	resp, err := dataHandler(context.Background(), req)
	if err != nil {
		log.Fatalf("data query: %v", err)
	}
	if resp == nil {
		log.Fatal("data query: nil response")
	}
	log.Printf("reply: %s", resp.Text)
}
