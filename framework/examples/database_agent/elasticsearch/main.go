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

// 本示例演示如何基于 Elasticsearch 配置并调用 database_agent 做一次数据查询。
//
// 需设置环境变量：OPENAI_API_KEY（必填）；可选 OPENAI_BASE_URL。
//
// 运行示例（在仓库根目录）：
//
//	set OPENAI_API_KEY=your_key
//	go run ./examples/database_agent/elasticsearch -config ./examples/database_agent/elasticsearch/config.elasticsearch.yaml
//
// 或进入示例目录后：
//
//	cd examples/database_agent/elasticsearch
//	go run . -config config.elasticsearch.yaml
func main() {
	cfgPath := flag.String("config", "D:\\workspace\\github\\sath\\examples\\database_agent\\elasticsearch\\config.elasticsearch.yaml", "path to config file")
	message := flag.String("message", "查询backend-vm_manager索引下M字段4103_3mrtug0l92h7数据", "user message to send to data query agent")
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
		RequestID: "example-es-1",
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
