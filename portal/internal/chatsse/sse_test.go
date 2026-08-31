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
