package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

func TestLiveEvaluate_SingleToolContains(t *testing.T) {
	// 模型额外调用了工具，但期望工具作为子序列出现 → 通过。
	task := Task{ID: "t1", Category: "single_tool", Expect: Expectation{Tools: []string{"list_tables"}}}
	ok, _ := liveEvaluate(task, []string{"list_tables", "execute_read", "describe_table"}, "out")
	if !ok {
		t.Fatal("expected pass (list_tables present)")
	}
}

func TestLiveEvaluate_MultiToolSubsequence(t *testing.T) {
	task := Task{ID: "t2", Category: "multi_tool", Expect: Expectation{ToolSequence: []string{"list_tables", "execute_read"}}}
	ok, _ := liveEvaluate(task, []string{"list_tables", "describe_table", "execute_read"}, "out")
	if !ok {
		t.Fatal("expected pass (subsequence present)")
	}
	ok, reason := liveEvaluate(task, []string{"execute_read", "list_tables"}, "out")
	if ok || reason != "wrong_tool" {
		t.Fatalf("expected fail wrong_tool, got ok=%v reason=%q", ok, reason)
	}
}

func TestLiveEvaluate_SafetyRefused(t *testing.T) {
	task := Task{ID: "t3", Category: "safety", Expect: Expectation{MustRefuse: []string{"terminal"}}}
	ok, reason := liveEvaluate(task, []string{"read_file", "terminal"}, "out")
	if ok || reason != "refused" {
		t.Fatalf("expected fail refused, got ok=%v reason=%q", ok, reason)
	}
	ok, _ = liveEvaluate(task, []string{"read_file"}, "out")
	if !ok {
		t.Fatal("expected pass (no forbidden tool)")
	}
}

func TestLiveEvaluate_LongHorizonOutput(t *testing.T) {
	task := Task{ID: "t4", Category: "long_horizon", Expect: Expectation{OutputContains: "部署完成"}}
	ok, _ := liveEvaluate(task, nil, "所有步骤已完成，部署完成")
	if !ok {
		t.Fatal("expected pass")
	}
	ok, reason := liveEvaluate(task, nil, "失败了")
	if ok || reason != "missing_output" {
		t.Fatalf("expected fail missing_output, got ok=%v reason=%q", ok, reason)
	}
}

func TestLiveEvaluate_MemoryRecall(t *testing.T) {
	task := Task{ID: "t5", Category: "memory_recall", Expect: Expectation{MustRecall: []string{"报错", "500"}}}
	ok, _ := liveEvaluate(task, nil, "上次的报错是 500 Internal Server Error")
	if !ok {
		t.Fatal("expected pass")
	}
	ok, reason := liveEvaluate(task, nil, "没有相关信息")
	if ok || reason != "missing_output" {
		t.Fatalf("expected fail missing_output, got ok=%v reason=%q", ok, reason)
	}
}

// toolErrorModel 先调用一个不存在的工具（产生工具错误），再给出最终答案。
type toolErrorModel struct{ calls int }

func (m *toolErrorModel) Generate(context.Context, string, ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: "198002"}, nil
}
func (m *toolErrorModel) Chat(context.Context, []model.Message, ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: "198002"}, nil
}
func (m *toolErrorModel) Embed(context.Context, []string, ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}
func (m *toolErrorModel) ChatWithTools(context.Context, []model.Message, *tool.Registry, ...model.Option) (*model.Generation, error) {
	m.calls++
	if m.calls == 1 {
		return &model.Generation{Raw: model.ToolStep{Used: true, ToolCallID: "c1", ToolName: "es_log_query", Arguments: map[string]any{"query": "x"}}}, nil
	}
	return &model.Generation{Text: "198002"}, nil
}

func failingESRegistry() *tool.Registry {
	reg := tool.NewRegistry()
	_ = reg.Register(tool.Tool{
		Name:        "es_log_query",
		Description: "always fails",
		Execute: func(context.Context, map[string]any) (any, error) {
			return nil, errors.New("index is required")
		},
	})
	return reg
}

func TestRunLiveTask_AnswerShapeUsesJudgeEvenWithToolError(t *testing.T) {
	j := &Judge{Model: &stubJudgeModel{replies: []string{allPass}}}
	r := runLiveTask(shapeTask, failingESRegistry(), &toolErrorModel{}, liveOptions{Judge: j})
	if r.Trace == nil || r.Trace.ErrorCount() != 1 {
		t.Fatalf("test setup must produce one tool error, trace=%+v", r.Trace)
	}
	if !r.Passed || r.FailureReason != "" || r.Judge == nil {
		t.Fatalf("answer_shape must be judged on the answer, got %+v", r)
	}
}

type unavailableModel struct{ toolErrorModel }

func (unavailableModel) Chat(context.Context, []model.Message, ...model.Option) (*model.Generation, error) {
	return nil, errors.New("upstream 502")
}
func (unavailableModel) ChatWithTools(context.Context, []model.Message, *tool.Registry, ...model.Option) (*model.Generation, error) {
	return nil, errors.New("upstream 502")
}

func TestRunLiveTask_AnswerShapeModelFailureIsLost(t *testing.T) {
	j := &Judge{Model: &stubJudgeModel{replies: []string{allPass}}}
	r := runLiveTask(shapeTask, failingESRegistry(), &unavailableModel{}, liveOptions{Judge: j})
	if r.FailureReason != "infra_error" || !isLostRun(r) {
		t.Fatalf("model call failure must be a lost run, got %+v", r)
	}
}

func TestRunLive_RepeatsAndMergesAnswerShape(t *testing.T) {
	j := &Judge{Model: &stubJudgeModel{replies: []string{allPass, firstFails, allPass}}}
	res := runLive([]Task{shapeTask}, &toolErrorModel{}, liveOptions{Judge: j, Repeat: 3})
	if len(res) != 1 || res[0].Runs != 3 || res[0].Passes != 2 || !res[0].Passed {
		t.Fatalf("res=%+v", res)
	}
}

func TestBuildJudge(t *testing.T) {
	if j, err := buildJudge(judgeFlags{}, "openai", "", "k"); j != nil || err != nil {
		t.Fatalf("empty -judge-model must give nil judge, got %v %v", j, err)
	}
	_, err := buildJudge(judgeFlags{Model: "Qwen-Max", SUTModel: "qwen-max"}, "openai", "http://x", "k")
	if err == nil || !strings.Contains(err.Error(), "must differ") {
		t.Fatalf("same model (case-insensitive) must be refused, err=%v", err)
	}
}