package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	"github.com/sixath/framework/tool"
)

// ChatWithTools 是 ReAct 主路径；它此前丢弃了 resp.Usage，导致 turn_trace / SSE 的
// token 恒为 0，token 计数器也永远拿不到样本。本用例锁住「主路径必须计量并校准」。
func TestOpenAIClient_ChatWithTools_ReportsUsageAndCalibrates(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleAssistant,
					Content: "done",
				},
			}},
			Usage: openai.Usage{PromptTokens: 120, CompletionTokens: 30},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := openAITestClient(ts)
	reg := tool.NewRegistry()
	if err := tool.RegisterCalculatorTool(reg); err != nil {
		t.Fatalf("register calculator tool error: %v", err)
	}

	counter := NewCalibratedCounter(nil)
	gen, err := client.ChatWithTools(context.Background(),
		[]Message{{Role: "user", Content: strings.Repeat("x", 100)}},
		reg, WithTokenCounter(counter))
	if err != nil {
		t.Fatalf("ChatWithTools: %v", err)
	}
	if gen.TokenUsage == nil {
		t.Fatal("expected TokenUsage on the tool path")
	}
	if gen.TokenUsage.InputTokens != 120 || gen.TokenUsage.OutputTokens != 30 {
		t.Fatalf("usage=%+v want input=120 output=30", gen.TokenUsage)
	}
	if !counter.Observed() {
		t.Fatal("counter must be calibrated from tool-path usage")
	}
	st := counter.Stats()
	// est = 100 runes × 1.35 = 135，actual(prompt) = 120 → alpha ≈ 1.2
	if st.Alpha >= DefaultTokenEstimateAlpha {
		t.Fatalf("alpha should have been corrected downwards, got %+v", st)
	}
}

func TestOpenAIClient_ChatWithTools_NoUsageLeavesNil(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleAssistant,
					Content: "done",
				},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := openAITestClient(ts)
	reg := tool.NewRegistry()
	if err := tool.RegisterCalculatorTool(reg); err != nil {
		t.Fatalf("register calculator tool error: %v", err)
	}

	gen, err := client.ChatWithTools(context.Background(), []Message{{Role: "user", Content: "hi"}}, reg)
	if err != nil {
		t.Fatalf("ChatWithTools: %v", err)
	}
	if gen.TokenUsage != nil {
		t.Fatalf("expected nil TokenUsage when provider omits usage, got %+v", gen.TokenUsage)
	}
}
