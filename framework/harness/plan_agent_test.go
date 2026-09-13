package harness

import (
	"context"
	"testing"

	"github.com/sixath/framework/model"
)

// fakePlanner 依次返回脚本化回复，耗尽后重复最后一个。
type fakePlanner struct {
	replies    []string
	calls      int
	lastPrompt string
}

func (f *fakePlanner) Generate(_ context.Context, prompt string, _ ...model.Option) (*model.Generation, error) {
	f.calls++
	f.lastPrompt = prompt
	return &model.Generation{Text: f.reply()}, nil
}

func (f *fakePlanner) Chat(_ context.Context, _ []model.Message, _ ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: f.reply()}, nil
}

func (f *fakePlanner) Embed(_ context.Context, _ []string, _ ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

func (f *fakePlanner) reply() string {
	if len(f.replies) == 0 {
		return ""
	}
	idx := f.calls - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(f.replies) {
		idx = len(f.replies) - 1
	}
	return f.replies[idx]
}

// fakeWorker 依次返回脚本化响应。
type fakeWorker struct {
	replies  []*Response
	calls    int
	lastReqs []*Request
}

func (w *fakeWorker) Run(_ context.Context, req *Request) (*Response, error) {
	w.calls++
	w.lastReqs = append(w.lastReqs, req)
	if len(w.replies) == 0 {
		return nil, nil
	}
	idx := w.calls - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(w.replies) {
		idx = len(w.replies) - 1
	}
	return w.replies[idx], nil
}

const validPlan = `{"steps":[{"id":"a","goal":"do A","success_criteria":"A done"},{"id":"b","goal":"do B","success_criteria":"B done"}]}`

func TestParsePlan_Valid(t *testing.T) {
	p, err := ParsePlan(validPlan)
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}
	if len(p.Steps) != 2 || p.Steps[0].Goal != "do A" {
		t.Fatalf("plan = %+v", p)
	}
}

func TestParsePlan_MarkdownFence(t *testing.T) {
	text := "Here is the plan:\n```json\n" + validPlan + "\n```\nHope it helps."
	p, err := ParsePlan(text)
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}
	if len(p.Steps) != 2 {
		t.Fatalf("steps = %d", len(p.Steps))
	}
}

func TestParsePlan_AssignsMissingIDs(t *testing.T) {
	p, err := ParsePlan(`{"steps":[{"goal":"one"},{"goal":"two"}]}`)
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}
	if p.Steps[0].ID != "step-1" || p.Steps[1].ID != "step-2" {
		t.Fatalf("ids = %q/%q", p.Steps[0].ID, p.Steps[1].ID)
	}
}

func TestParsePlan_Errors(t *testing.T) {
	for _, bad := range []string{
		"not json at all",
		`{"steps":[]}`,            // 无步骤
		`{"steps":[{"goal":""}]}`, // goal 为空
	} {
		if _, err := ParsePlan(bad); err == nil {
			t.Fatalf("ParsePlan(%q) expected error", bad)
		}
	}
}

func TestPlanExecuteAgent_PlannerOutputMustValidate(t *testing.T) {
	planner := &fakePlanner{replies: []string{"not json at all", validPlan}}
	worker := &fakeWorker{replies: []*Response{{Text: "done"}}}
	agent := NewPlanExecuteAgent(planner, worker)

	resp, err := agent.Run(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "task"}}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if planner.calls != 2 {
		t.Fatalf("planner calls = %d, want 2 (initial + 1 repair)", planner.calls)
	}
	if resp == nil || resp.Text != "done" {
		t.Fatalf("resp = %#v", resp)
	}
}

func TestPlanExecuteAgent_FallsBackToReActOnPlannerFailure(t *testing.T) {
	planner := &fakePlanner{replies: []string{"bad"}} // 永远返回非法输出
	worker := &fakeWorker{replies: []*Response{{Text: "react-done"}}}
	agent := NewPlanExecuteAgent(planner, worker)

	req := &Request{Messages: []model.Message{{Role: "user", Content: "task"}, {Role: "user", Content: "more"}}}
	resp, err := agent.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp == nil || resp.Text != "react-done" {
		t.Fatalf("resp = %#v", resp)
	}
	if worker.calls != 1 {
		t.Fatalf("worker calls = %d, want 1 (fallback to ReAct)", worker.calls)
	}
	if len(worker.lastReqs[0].Messages) != 2 {
		t.Fatalf("fallback request should carry original messages, got %d", len(worker.lastReqs[0].Messages))
	}
}

func TestPlanExecuteAgent_ReplansOnStepFailure(t *testing.T) {
	planner := &fakePlanner{replies: []string{
		`{"steps":[{"id":"a","goal":"A"},{"id":"b","goal":"B"}]}`,
		`{"steps":[{"id":"b2","goal":"B2"}]}`, // replan 出的新计划
	}}
	worker := &fakeWorker{replies: []*Response{{Text: ""}, {Text: "final"}}} // A 失败（空输出），B2 成功
	agent := NewPlanExecuteAgent(planner, worker)

	resp, err := agent.Run(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "task"}}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp == nil || resp.Text != "final" {
		t.Fatalf("resp = %#v", resp)
	}
	if worker.calls != 2 {
		t.Fatalf("worker calls = %d, want 2 (A fail + B2 success)", worker.calls)
	}
	if planner.calls != 2 {
		t.Fatalf("planner calls = %d, want 2 (initial + 1 replan)", planner.calls)
	}
}

func TestPlanExecuteAgent_ReadonlyStepSkipsApproval(t *testing.T) {
	planner := &fakePlanner{replies: []string{`{"steps":[{"id":"r","goal":"read only","readonly":true}]}`}}
	worker := &fakeWorker{replies: []*Response{{Text: "ok"}}}
	agent := NewPlanExecuteAgent(planner, worker)

	_, err := agent.Run(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "task"}}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(worker.lastReqs) != 1 {
		t.Fatalf("worker calls = %d", len(worker.lastReqs))
	}
	ro, _ := worker.lastReqs[0].Metadata["step_readonly"].(bool)
	if !ro {
		t.Fatalf("step_readonly should be true, metadata = %#v", worker.lastReqs[0].Metadata)
	}
}

func TestPlanExecuteAgent_ExecutesStepsInOrder(t *testing.T) {
	planner := &fakePlanner{replies: []string{`{"steps":[{"id":"1","goal":"one"},{"id":"2","goal":"two"},{"id":"3","goal":"three"}]}`}}
	worker := &fakeWorker{replies: []*Response{{Text: "r1"}, {Text: "r2"}, {Text: "r3"}}}
	agent := NewPlanExecuteAgent(planner, worker)

	resp, err := agent.Run(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "task"}}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp == nil || resp.Text != "r3" {
		t.Fatalf("final resp = %#v, want r3", resp)
	}
	if worker.calls != 3 {
		t.Fatalf("worker calls = %d, want 3", worker.calls)
	}
	for i, want := range []string{"one", "two", "three"} {
		if got := worker.lastReqs[i].Messages[0].Content; got != want {
			t.Fatalf("step %d goal = %q, want %q", i, got, want)
		}
	}
}

// fakeVerifier 模型自评：依次返回脚本化判定（YES/NO），耗尽后重复最后一个。
type fakeVerifier struct {
	replies []string
	calls   int
}

func (f *fakeVerifier) Generate(_ context.Context, _ string, _ ...model.Option) (*model.Generation, error) {
	f.calls++
	idx := f.calls - 1
	if idx >= len(f.replies) {
		idx = len(f.replies) - 1
	}
	return &model.Generation{Text: f.replies[idx]}, nil
}
func (f *fakeVerifier) Chat(_ context.Context, _ []model.Message, _ ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: f.replies[0]}, nil
}
func (f *fakeVerifier) Embed(_ context.Context, _ []string, _ ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

func TestPlanExecuteAgent_VerifierRejectsThenReplans(t *testing.T) {
	planner := &fakePlanner{replies: []string{
		`{"steps":[{"id":"a","goal":"A","success_criteria":"A done"}]}`,
		`{"steps":[{"id":"b","goal":"B","success_criteria":"B done"}]}`,
	}}
	worker := &fakeWorker{replies: []*Response{{Text: "partial"}, {Text: "done"}}}
	// verifier 对第一步返回 NO，第二步返回 YES。
	verifier := &fakeVerifier{replies: []string{"NO", "YES"}}
	agent := NewPlanExecuteAgent(planner, worker, WithPlanVerifier(verifier), WithPlanMaxReplans(1))

	resp, err := agent.Run(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "task"}}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp == nil || resp.Text != "done" {
		t.Fatalf("resp = %#v", resp)
	}
	if worker.calls != 2 {
		t.Fatalf("worker calls = %d, want 2", worker.calls)
	}
}

func TestPlanExecuteAgent_ImplementsEventStreamable(t *testing.T) {
	planner := &fakePlanner{replies: []string{`{"steps":[{"id":"a","goal":"A"}]}`}}
	worker := &fakeWorker{replies: []*Response{{Text: "ok"}}}
	var a any = NewPlanExecuteAgent(planner, worker)
	if _, ok := a.(EventStreamableAgent); !ok {
		t.Fatal("PlanExecuteAgent should implement EventStreamableAgent")
	}
	if _, ok := a.(StreamableAgent); !ok {
		t.Fatal("PlanExecuteAgent should implement StreamableAgent")
	}
}

func TestPlanExecuteAgent_RunEvents_EmitsPlanAndSteps(t *testing.T) {
	planner := &fakePlanner{replies: []string{`{"steps":[{"id":"a","goal":"A"},{"id":"b","goal":"B"}]}`}}
	worker := &fakeWorker{replies: []*Response{{Text: "r1"}, {Text: "r2"}}}
	agent := NewPlanExecuteAgent(planner, worker)

	ch, err := agent.RunEvents(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "task"}}})
	if err != nil {
		t.Fatalf("RunEvents: %v", err)
	}
	var kinds []StreamEventType
	var finalText string
	var planEv, stepEv bool
	for ev := range ch {
		kinds = append(kinds, ev.Type)
		switch ev.Type {
		case StreamEventDelta:
			finalText = ev.Text
		case StreamEventPlan:
			planEv = true
			if _, ok := ev.Metadata["plan"].(*Plan); !ok {
				t.Fatal("plan event should carry *Plan in Metadata")
			}
		case StreamEventPlanStep:
			stepEv = true
			if _, ok := ev.Metadata["step"].(PlanStep); !ok {
				t.Fatal("plan_step event should carry PlanStep in Metadata")
			}
		}
	}
	if !planEv || !stepEv {
		t.Fatalf("expected plan + step events, kinds=%v", kinds)
	}
	want := []StreamEventType{StreamEventPlan, StreamEventPlanStep, StreamEventPlanStep, StreamEventDelta, StreamEventDone}
	if len(kinds) != len(want) {
		t.Fatalf("events = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("event[%d] = %v, want %v (all=%v)", i, kinds[i], want[i], kinds)
		}
	}
	if finalText != "r2" {
		t.Fatalf("final text = %q, want r2", finalText)
	}
}

func TestPlanExecuteAgent_RunEvents_FallsBackToRunOnPlanFailure(t *testing.T) {
	planner := &fakePlanner{replies: []string{"bad"}}
	worker := &fakeWorker{replies: []*Response{{Text: "react-done"}}}
	agent := NewPlanExecuteAgent(planner, worker)

	ch, _ := agent.RunEvents(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "task"}}})
	var kinds []StreamEventType
	var text string
	for ev := range ch {
		kinds = append(kinds, ev.Type)
		if ev.Type == StreamEventDelta {
			text = ev.Text
		}
	}
	if text != "react-done" {
		t.Fatalf("fallback text = %q, want react-done (events=%v)", text, kinds)
	}
	if len(kinds) == 0 || kinds[len(kinds)-1] != StreamEventDone {
		t.Fatalf("fallback should end with done, events=%v", kinds)
	}
}
