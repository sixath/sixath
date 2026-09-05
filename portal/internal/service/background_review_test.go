package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/growth"
	"github.com/sixath/framework/model"
)

func TestAfterTurnBackgroundReview_FinalizeTrueSpawnsOnce(t *testing.T) {
	repo := &fakeGrowthRepoForService{state: &biz.ChatGrowthState{SessionID: "bg-spawn-1"}}
	uc := biz.NewGrowthUsecase(repo)
	uc.SetBackgroundReviewEnabled(true)
	uc.SetNudgeConfig(growth.NudgeConfig{Enabled: true, SkillToolInterval: 1, MemoryTurnInterval: 99})

	var calls atomic.Int32
	s := &ChatService{
		growthUC: uc,
		log:      log.NewHelper(log.DefaultLogger),
		bgReviewSpawnHook: func(p BackgroundReviewParams) {
			calls.Add(1)
			if !p.SpawnSkill {
				t.Errorf("expected SpawnSkill true")
			}
			if len(p.Messages) == 0 {
				t.Errorf("expected non-empty snapshot messages")
			}
			_ = uc.SetBgReviewInFlight(context.Background(), p.SessionID, false)
		},
	}

	tr := &agent.RunTrace{
		RequestID: "req-bg-1",
		ToolCalls: []agent.ToolCallRecord{
			{ToolName: "echo", Error: ""},
		},
	}
	msgs := []model.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "ok"},
		{Role: "tool", Content: `{"tool":"echo","result":"ok"}`, Metadata: map[string]any{"tool_name": "echo"}},
	}

	s.afterTurnBackgroundReview(context.Background(), "bg-spawn-1", "agent-1", "/ws", msgs, tr)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if calls.Load() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("spawn calls want 1 got %d", got)
	}

	st, err := uc.GetState(context.Background(), "bg-spawn-1")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if st.BgReviewInFlight {
		t.Fatal("in_flight should be cleared by spy finish")
	}
}

func TestAfterTurnBackgroundReview_MissingMessagesUsesFallback(t *testing.T) {
	repo := &fakeGrowthRepoForService{state: &biz.ChatGrowthState{SessionID: "bg-fb-1"}}
	uc := biz.NewGrowthUsecase(repo)
	uc.SetBackgroundReviewEnabled(true)
	uc.SetNudgeConfig(growth.NudgeConfig{Enabled: true, SkillToolInterval: 1, MemoryTurnInterval: 99})

	var gotMsgs []model.Message
	done := make(chan struct{})
	s := &ChatService{
		growthUC: uc,
		log:      log.NewHelper(log.DefaultLogger),
		bgReviewSpawnHook: func(p BackgroundReviewParams) {
			gotMsgs = append([]model.Message(nil), p.Messages...)
			_ = uc.SetBgReviewInFlight(context.Background(), p.SessionID, false)
			close(done)
		},
	}

	tr := &agent.RunTrace{
		RequestID: "req-fb",
		ToolCalls: []agent.ToolCallRecord{
			{ToolCallID: "c1", ToolName: "search", Arguments: map[string]any{"q": "x"}, Result: "hit", Error: "boom"},
			{ToolCallID: "c2", ToolName: "ok_tool", Result: "fine", Error: ""},
		},
	}
	// Missing Messages → fallback from RunTrace synthetic tools (chatUC nil → no history).
	s.afterTurnBackgroundReview(context.Background(), "bg-fb-1", "a1", "/ws", nil, tr)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("spawn not called")
	}
	if len(gotMsgs) == 0 {
		t.Fatal("fallback snapshot must be non-empty when Messages missing but RunTrace has tools")
	}
	hasTool := false
	for _, m := range gotMsgs {
		if m.Role == "tool" {
			hasTool = true
			break
		}
	}
	if !hasTool {
		t.Fatalf("fallback should include synthetic tool messages, got %#v", gotMsgs)
	}
}

func TestTruncateSnapshotMessages_PreferFailedTools(t *testing.T) {
	msgs := make([]model.Message, 0, 10)
	for i := 0; i < 5; i++ {
		msgs = append(msgs, model.Message{Role: "user", Content: "u"})
	}
	msgs = append(msgs, model.Message{
		Role:     "tool",
		Content:  `{"tool":"x","error":"fail"}`,
		Metadata: map[string]any{"error": "fail"},
	})
	for i := 0; i < 4; i++ {
		msgs = append(msgs, model.Message{Role: "assistant", Content: "a"})
	}
	out := truncateSnapshotMessages(msgs, 3, true)
	if len(out) != 3 {
		t.Fatalf("len want 3 got %d", len(out))
	}
	foundFailed := false
	for _, m := range out {
		if isFailedToolMessage(m) {
			foundFailed = true
		}
	}
	if !foundFailed {
		t.Fatal("prefer-failed must keep the failed tool message")
	}
}

func TestSyntheticToolMessagesFromTrace_NonEmpty(t *testing.T) {
	tr := &agent.RunTrace{
		ToolCalls: []agent.ToolCallRecord{
			{ToolName: "t1", Result: "r1", Error: "e1"},
		},
	}
	msgs := syntheticToolMessagesFromTrace(tr)
	if len(msgs) != 1 || msgs[0].Role != "tool" {
		t.Fatalf("got %#v", msgs)
	}
	if msgs[0].Metadata["error"] != "e1" {
		t.Fatalf("error metadata: %#v", msgs[0].Metadata)
	}
}

func TestSetBgReviewInFlight(t *testing.T) {
	repo := &fakeGrowthRepoForService{}
	uc := biz.NewGrowthUsecase(repo)
	if err := uc.SetBgReviewInFlight(context.Background(), "inf-1", true); err != nil {
		t.Fatal(err)
	}
	st, err := uc.GetState(context.Background(), "inf-1")
	if err != nil {
		t.Fatal(err)
	}
	if !st.BgReviewInFlight || st.BgReviewInFlightSince == nil {
		t.Fatalf("want in_flight set, got %+v", st)
	}
	if err := uc.SetBgReviewInFlight(context.Background(), "inf-1", false); err != nil {
		t.Fatal(err)
	}
	st, _ = uc.GetState(context.Background(), "inf-1")
	if st.BgReviewInFlight || st.BgReviewInFlightSince != nil {
		t.Fatalf("want cleared, got %+v", st)
	}
}
