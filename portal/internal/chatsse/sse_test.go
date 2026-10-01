package chatsse

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	portalchat "backend/internal/chat"
	"backend/internal/service"

	"github.com/sixath/framework/config"
)

func TestAggregateFinal_ScrubsMemoryFence(t *testing.T) {
	portalchat.SetPortalAgentExtra(&config.PortalAgentExtra{
		MemoryOrchestratorPrefetch: &config.MemoryOrchestratorPrefetch{
			StreamScrub: true,
			FenceTag:    "sixath-memory-context",
		},
	})
	t.Cleanup(func() {
		portalchat.SetPortalAgentExtra(&config.PortalAgentExtra{})
	})

	tag := "sixath-memory-context"
	open := `<` + tag + ` id="abc12345">`
	closeTag := `</` + tag + `>`
	ch := make(chan service.ChatStreamEvent, 8)
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventChunk, Content: "Hello "}
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventChunk, Content: open}
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventChunk, Content: "SECRET"}
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventChunk, Content: closeTag}
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventChunk, Content: " world"}
	close(ch)

	got := AggregateFinal(ch)
	if got.Failed {
		t.Fatalf("unexpected failed: %+v", got)
	}
	if !strings.Contains(got.Content, "Hello") || !strings.Contains(got.Content, "world") {
		t.Fatalf("expected outside text preserved, got %q", got.Content)
	}
	if strings.Contains(got.Content, "SECRET") {
		t.Fatalf("persist leaked fence body: %q", got.Content)
	}
}

func TestWriteStream_PersistsFinalTextNotIntermediateDeltas(t *testing.T) {
	final := "结论：根因是 prelaunch 目录下的补丁文件在 09:58 被替换，导致校验失败。"
	ch := make(chan service.ChatStreamEvent, 8)
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventChunk, Content: "我先查一下日志。"}
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventChunk, Content: "草稿结论：计数器保护。"}
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventChunk, Content: final}
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventFinal, Content: final}
	close(ch)

	var persisted string
	rec := httptest.NewRecorder()
	WriteStream(context.Background(), rec, ch, "s1", func(_ context.Context, _, content string, _ map[string]any) error {
		persisted = content
		return nil
	})
	if persisted != final {
		t.Fatalf("persisted = %q, want final text only", persisted)
	}
	if strings.Contains(rec.Body.String(), "event: final") {
		t.Fatalf("final event must not be emitted to the client: %s", rec.Body.String())
	}
}

func TestPersistableContent(t *testing.T) {
	long := strings.Repeat("报告正文", 100)
	if got := persistableContent(long+"完成。", "完成。", false); got != long+"完成。" {
		t.Fatal("a tiny closing line must not replace a long streamed report")
	}
	report := strings.Repeat("最终结论", 100)
	drafts := strings.Repeat("草稿结论", 500) + report
	if got := persistableContent(drafts, report, false); got != report {
		t.Fatal("a full final report must replace superseded drafts even when much shorter")
	}
	if got := persistableContent("partial", "full final", true); got != "partial" {
		t.Fatal("canceled turns keep streamed text")
	}
	if got := persistableContent("a b c", "", false); got != "a b c" {
		t.Fatal("no final falls back to stream")
	}
}

func TestAggregateFinal_DeadlineErrorNotSuppressed(t *testing.T) {
	ch := make(chan service.ChatStreamEvent, 4)
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventChunk, Content: "partial"}
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventError, Error: "context deadline exceeded"}
	close(ch)

	got := AggregateFinal(ch)
	if !got.Failed {
		t.Fatalf("expected Failed after deadline error, got %+v", got)
	}
	if got.Content != "partial" {
		t.Fatalf("content = %q, want partial", got.Content)
	}
	if got.Error == "" {
		t.Fatal("expected error message")
	}
}

func TestWriteStream_PersistsTimelineOnFailedError(t *testing.T) {
	ch := make(chan service.ChatStreamEvent, 8)
	ch <- service.ChatStreamEvent{
		Type:      service.ChatStreamEventModelCall,
		ModelCall: &service.ModelCallPayload{Phase: "invoked", Step: 0, Model: "deepseek-v4-pro"},
	}
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventError, Error: "model stream reset"}
	close(ch)

	var persisted struct {
		content string
		meta    map[string]any
		called  bool
	}
	rec := httptest.NewRecorder()
	res := WriteStream(context.Background(), rec, ch, "sess-1", func(_ context.Context, _, content string, meta map[string]any) error {
		persisted.called = true
		persisted.content = content
		persisted.meta = meta
		return nil
	})
	if !res.Failed {
		t.Fatalf("expected Failed, got %+v", res)
	}
	if !persisted.called {
		t.Fatal("failed stream must persist assistant so refresh is not an orphan user turn")
	}
	if !strings.Contains(persisted.content, "model stream reset") {
		t.Fatalf("persist content=%q", persisted.content)
	}
	tl, _ := persisted.meta["timeline"].([]any)
	if len(tl) == 0 {
		t.Fatalf("expected timeline in metadata, meta=%#v", persisted.meta)
	}
}

func TestWriteStream_EmptyReplyGetsFallbackNotice(t *testing.T) {
	ch := make(chan service.ChatStreamEvent, 8)
	ch <- service.ChatStreamEvent{
		Type:      service.ChatStreamEventModelCall,
		ModelCall: &service.ModelCallPayload{Phase: "responded", Step: 0, Model: "kimi-k3"},
	}
	close(ch)

	var persisted struct {
		content string
		meta    map[string]any
		called  bool
	}
	rec := httptest.NewRecorder()
	res := WriteStream(context.Background(), rec, ch, "sess-empty", func(_ context.Context, _, content string, meta map[string]any) error {
		persisted.called = true
		persisted.content = content
		persisted.meta = meta
		return nil
	})
	if res.Failed || res.Canceled {
		t.Fatalf("empty success turn must not fail/cancel, got %+v", res)
	}
	if !persisted.called {
		t.Fatal("must persist empty-reply fallback so UI is not blank")
	}
	if persisted.content != EmptyReplyNotice {
		t.Fatalf("persist content=%q want %q", persisted.content, EmptyReplyNotice)
	}
	if persisted.meta["empty_reply"] != true {
		t.Fatalf("expected empty_reply=true, meta=%#v", persisted.meta)
	}
	if body := rec.Body.String(); !strings.Contains(body, "empty_reply") && !strings.Contains(body, EmptyReplyNotice) {
		// SSE still ends with done; fallback is persist-facing. Ensure done was written.
		if !strings.Contains(body, "event: done") {
			t.Fatalf("expected done event, body=%q", body)
		}
	}
}

func TestWriteStream_WhitespaceOnlyGetsFallbackNotice(t *testing.T) {
	ch := make(chan service.ChatStreamEvent, 4)
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventChunk, Content: "  \n\t"}
	close(ch)

	var got string
	rec := httptest.NewRecorder()
	_ = WriteStream(context.Background(), rec, ch, "sess-ws", func(_ context.Context, _, content string, _ map[string]any) error {
		got = content
		return nil
	})
	if got != EmptyReplyNotice {
		t.Fatalf("whitespace-only content=%q want fallback", got)
	}
}

func TestWriteStream_CanceledEmptyGetsCancelNotice(t *testing.T) {
	ch := make(chan service.ChatStreamEvent, 4)
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventCancelled}
	close(ch)

	var persisted struct {
		content string
		meta    map[string]any
	}
	rec := httptest.NewRecorder()
	res := WriteStream(context.Background(), rec, ch, "sess-cancel", func(_ context.Context, _, content string, meta map[string]any) error {
		persisted.content = content
		persisted.meta = meta
		return nil
	})
	if !res.Canceled {
		t.Fatalf("expected Canceled, got %+v", res)
	}
	if persisted.content != EmptyCancelNotice {
		t.Fatalf("content=%q want %q", persisted.content, EmptyCancelNotice)
	}
	if persisted.meta["interrupted"] != true {
		t.Fatalf("expected interrupted, meta=%#v", persisted.meta)
	}
	if persisted.meta["empty_reply"] != true {
		t.Fatalf("expected empty_reply, meta=%#v", persisted.meta)
	}
}

func TestWriteStream_NonEmptyContentUnchanged(t *testing.T) {
	ch := make(chan service.ChatStreamEvent, 4)
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventChunk, Content: "正常答复"}
	close(ch)

	var got string
	var meta map[string]any
	rec := httptest.NewRecorder()
	_ = WriteStream(context.Background(), rec, ch, "sess-ok", func(_ context.Context, _, content string, m map[string]any) error {
		got = content
		meta = m
		return nil
	})
	if got != "正常答复" {
		t.Fatalf("content=%q", got)
	}
	if meta["empty_reply"] == true {
		t.Fatalf("must not mark empty_reply for real content, meta=%#v", meta)
	}
}

func TestAggregateFinal_OmitsDebugAndToolEvents(t *testing.T) {
	ch := make(chan service.ChatStreamEvent, 8)
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventDebug, Content: "agent.tool.started[{\"tool\":\"list_tools\"}]\r\n"}
	ch <- service.ChatStreamEvent{
		Type:     service.ChatStreamEventToolCall,
		Content:  "should-not-appear",
		ToolCall: &service.ToolCallPayload{ToolName: "list_tools", Phase: "started"},
	}
	ch <- service.ChatStreamEvent{
		Type:      service.ChatStreamEventModelCall,
		Content:   "should-not-appear",
		ModelCall: &service.ModelCallPayload{Phase: "invoked"},
	}
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventChunk, Content: "最终答复"}
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventDebug, Content: "agent.tool.completed[{}]\r\n"}
	close(ch)

	got := AggregateFinal(ch)
	if got.Failed {
		t.Fatalf("unexpected failed: %+v", got)
	}
	if got.Content != "最终答复" {
		t.Fatalf("content = %q want only assistant chunk", got.Content)
	}
	if strings.Contains(got.Content, "agent.tool") || strings.Contains(got.Content, "list_tools") {
		t.Fatalf("debug/tool leaked into final content: %q", got.Content)
	}
}
