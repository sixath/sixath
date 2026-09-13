package chatsse

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/internal/service"
)

// 取消语义（Task 12）：取消不是失败——已产生的正文必须照常落库并标记 interrupted。
func TestWriteStream_CanceledPersistsPartialContent(t *testing.T) {
	ch := make(chan service.ChatStreamEvent, 3)
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventChunk, Content: "partial "}
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventChunk, Content: "answer"}
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventCancelled}
	close(ch)

	rec := httptest.NewRecorder()
	var gotContent string
	var gotMeta map[string]any
	res := WriteStream(context.Background(), rec, ch, "sess-cancel", func(_ context.Context, _, content string, meta map[string]any) error {
		gotContent = content
		gotMeta = meta
		return nil
	})

	if !res.Canceled {
		t.Fatal("result must be marked canceled")
	}
	if res.Failed {
		t.Fatal("cancel must not be treated as a failure (otherwise partial content is dropped)")
	}
	if gotContent != "partial answer" {
		t.Fatalf("persisted content=%q want %q", gotContent, "partial answer")
	}
	if gotMeta["interrupted"] != true {
		t.Fatalf("persisted metadata must mark interruption: %#v", gotMeta)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "event: cancelled") {
		t.Fatalf("expected cancelled SSE event, body=%s", body)
	}
	if strings.Contains(body, "event: error") {
		t.Fatalf("cancel must not emit an error event, body=%s", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Fatalf("stream should still terminate with done, body=%s", body)
	}
}

func TestWriteStream_CancelWithoutContentStillPersists(t *testing.T) {
	ch := make(chan service.ChatStreamEvent, 1)
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventCancelled}
	close(ch)

	rec := httptest.NewRecorder()
	saved := false
	res := WriteStream(context.Background(), rec, ch, "sess-2", func(context.Context, string, string, map[string]any) error {
		saved = true
		return nil
	})
	if !res.Canceled || res.Failed {
		t.Fatalf("res=%+v", res)
	}
	if !saved {
		t.Fatal("persist hook should still be called (content may be empty)")
	}
}

// final 模式（Gateway / 企微）没有可交互面：取消按终态失败上报，但部分内容仍返回。
func TestAggregateFinal_CanceledIsTerminalFailure(t *testing.T) {
	ch := make(chan service.ChatStreamEvent, 2)
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventChunk, Content: "half"}
	ch <- service.ChatStreamEvent{Type: service.ChatStreamEventCancelled}
	close(ch)

	res := AggregateFinal(ch)
	if !res.Canceled {
		t.Fatal("result must be marked canceled")
	}
	if !res.Failed || res.Error == "" {
		t.Fatalf("final mode must surface a terminal failure: %+v", res)
	}
	if res.Content != "half" {
		t.Fatalf("partial content=%q want half", res.Content)
	}
}
