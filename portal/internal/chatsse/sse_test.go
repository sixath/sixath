package chatsse

import (
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
