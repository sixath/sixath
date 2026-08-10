package main

import (
	"context"
	"fmt"
	"strings"

	"backend/internal/chat"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/model"
)

func main() {
	gate := chat.NewTurnIntentGate()
	if gate == nil {
		panic("TurnIntentGate disabled")
	}
	text := strings.Repeat("cloudgame 模块调用关系：A 依赖 B，B 依赖 C。详细说明架构分层。", 3) + "如有需要请告诉我。"
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req: &agent.Request{Messages: []model.Message{{
			Role: "user", Content: "分析 cloudgame 仓库模块调用关系",
		}}},
		AssistantText: text,
		ToolStep: model.ToolStep{
			Used: true,
			ToolCalls: []model.ToolCall{{
				ID: "1", Name: "web_search",
				Arguments: map[string]any{"query": "消费者权益保护法 七日无理由退货"},
			}},
		},
	})
	fmt.Printf("decision=%v reason=%q\n", res.Decision, res.Reason)
	if res.Decision != agent.PostModelFinish {
		panic("expected finish")
	}
	fmt.Println("SMOKE_OK")
}
