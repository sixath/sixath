package tool

import (
	"context"
	"strings"
	"testing"
)

func TestTodoTool_ReadEmptyList(t *testing.T) {
	store := NewInMemoryTodoStore()
	reg := NewRegistry()
	if err := RegisterTodoTool(reg, store); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("todo")
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess-a")

	res, err := tl.Execute(ctx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	todos := m["todos"].([]map[string]any)
	if len(todos) != 0 {
		t.Fatalf("expected empty list, got %#v", todos)
	}
}

func TestTodoTool_ReplaceList(t *testing.T) {
	store := NewInMemoryTodoStore()
	reg := NewRegistry()
	_ = RegisterTodoTool(reg, store)
	tl, _ := reg.Get("todo")
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess-b")

	res, err := tl.Execute(ctx, map[string]any{
		"todos": []any{
			map[string]any{"id": "1", "content": "first", "status": "pending"},
			map[string]any{"id": "2", "content": "second", "status": "in_progress"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	todos := res.(map[string]any)["todos"].([]map[string]any)
	if len(todos) != 2 || todos[1]["id"] != "2" {
		t.Fatalf("unexpected replace result: %#v", todos)
	}
}

func TestTodoTool_MergeUpdatesByID(t *testing.T) {
	store := NewInMemoryTodoStore()
	reg := NewRegistry()
	_ = RegisterTodoTool(reg, store)
	tl, _ := reg.Get("todo")
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess-c")

	_, _ = tl.Execute(ctx, map[string]any{
		"todos": []any{
			map[string]any{"id": "a", "content": "alpha", "status": "pending"},
			map[string]any{"id": "b", "content": "beta", "status": "pending"},
		},
	})
	res, err := tl.Execute(ctx, map[string]any{
		"merge": true,
		"todos": []any{
			map[string]any{"id": "a", "content": "alpha done", "status": "completed"},
			map[string]any{"id": "c", "content": "gamma", "status": "pending"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	todos := res.(map[string]any)["todos"].([]map[string]any)
	if len(todos) != 3 {
		t.Fatalf("expected 3 todos after merge, got %#v", todos)
	}
	if todos[0]["id"] != "a" || todos[0]["content"] != "alpha done" || todos[0]["status"] != "completed" {
		t.Fatalf("merge update failed: %#v", todos[0])
	}
	if todos[2]["id"] != "c" {
		t.Fatalf("new item not appended: %#v", todos)
	}
}

func TestTodoTool_SingleInProgress(t *testing.T) {
	store := NewInMemoryTodoStore()
	reg := NewRegistry()
	_ = RegisterTodoTool(reg, store)
	tl, _ := reg.Get("todo")
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess-d")

	res, err := tl.Execute(ctx, map[string]any{
		"todos": []any{
			map[string]any{"id": "1", "content": "one", "status": "in_progress"},
			map[string]any{"id": "2", "content": "two", "status": "in_progress"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	todos := res.(map[string]any)["todos"].([]map[string]any)
	inProgress := 0
	for _, item := range todos {
		if item["status"] == "in_progress" {
			inProgress++
		}
	}
	if inProgress != 1 {
		t.Fatalf("expected exactly one in_progress, got %d: %#v", inProgress, todos)
	}
	if todos[1]["status"] != "in_progress" || todos[0]["status"] != "pending" {
		t.Fatalf("latest in_progress should win: %#v", todos)
	}
}

func TestTodoTool_SessionIsolation(t *testing.T) {
	store := NewInMemoryTodoStore()
	reg := NewRegistry()
	_ = RegisterTodoTool(reg, store)
	tl, _ := reg.Get("todo")

	ctxA := context.WithValue(context.Background(), ContextKeySessionID, "sess-1")
	ctxB := context.WithValue(context.Background(), ContextKeySessionID, "sess-2")

	_, _ = tl.Execute(ctxA, map[string]any{
		"todos": []any{
			map[string]any{"id": "x", "content": "only A", "status": "pending"},
		},
	})
	resB, _ := tl.Execute(ctxB, map[string]any{})
	todosB := resB.(map[string]any)["todos"].([]map[string]any)
	if len(todosB) != 0 {
		t.Fatalf("session B should be empty, got %#v", todosB)
	}
}

func TestFormatTodosForInjection(t *testing.T) {
	text := FormatTodosForInjection([]TodoItem{
		{ID: "1", Content: "do thing", Status: TodoStatusInProgress},
		{ID: "2", Content: "done", Status: TodoStatusCompleted},
		{ID: "3", Content: "later", Status: TodoStatusPending},
	})
	if text == "" {
		t.Fatal("expected injection text")
	}
	if !strings.Contains(text, "in_progress") || !strings.Contains(text, "pending") || !strings.Contains(text, "do thing") {
		t.Fatalf("missing expected lines: %q", text)
	}
	if strings.Contains(text, "done") {
		t.Fatalf("completed item should not inject: %q", text)
	}
}
