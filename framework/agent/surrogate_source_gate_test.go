package agent

import "testing"

func TestEvaluateSurrogateSourceGate_txtWithoutGoRejects(t *testing.T) {
	records := []ToolCallRecord{{
		ToolName:  "read_file",
		Arguments: map[string]any{"path": "notes/dump.txt"},
		Result:    map[string]any{"path": "notes/dump.txt", "content": "func Handle() {}"},
	}}
	got := EvaluateSurrogateSourceGate(records, "根据源码，整体流程会调用 InsertUnionUserAreaInfo。")
	if got.Allow {
		t.Fatal("txt stand-in must not allow")
	}
	if got.Action != "inject" {
		t.Fatalf("got %#v", got)
	}
}

func TestEvaluateSurrogateSourceGate_memoryWithoutGoRejects(t *testing.T) {
	records := []ToolCallRecord{{
		ToolName:  "memory_get",
		Arguments: map[string]any{"path": "MEMORY.md"},
		Result:    map[string]any{"path": "MEMORY.md"},
	}}
	got := EvaluateSurrogateSourceGate(records, "代码里会写入本地映射。")
	if got.Allow {
		t.Fatal("MEMORY.md stand-in must not allow")
	}
}

func TestEvaluateSurrogateSourceGate_goEvidenceAllows(t *testing.T) {
	records := []ToolCallRecord{
		{ToolName: "read_file", Arguments: map[string]any{"path": "MEMORY.md"}},
		{ToolName: "rca_read", Result: map[string]any{"ok": true, "file": "helper.go", "content": "1|x"}},
	}
	if got := EvaluateSurrogateSourceGate(records, "errcode==0 时才会 Insert。"); !got.Allow {
		t.Fatalf("rca_read .go should allow: %#v", got)
	}
}

func TestEvaluateSurrogateSourceGate_citesTxtDespiteGoRejects(t *testing.T) {
	records := []ToolCallRecord{
		{ToolName: "read_file", Arguments: map[string]any{"path": "session.txt"}},
		{ToolName: "rca_read", Result: map[string]any{"ok": true, "file": "helper.go", "content": "1|x"}},
	}
	got := EvaluateSurrogateSourceGate(records, "依据 session.txt：会写入映射。")
	if got.Allow {
		t.Fatal("citing .txt as evidence must not allow")
	}
}

func TestEvaluateSurrogateSourceGate_memoryOnlyNoCodeClaimAllows(t *testing.T) {
	records := []ToolCallRecord{{
		ToolName:  "memory_get",
		Arguments: map[string]any{"path": "MEMORY.md"},
	}}
	if got := EvaluateSurrogateSourceGate(records, "上次约定用中文回复。"); !got.Allow {
		t.Fatalf("non-code memory recall should allow: %#v", got)
	}
}

func TestEvaluateSurrogateSourceGate_noSurrogateAllows(t *testing.T) {
	records := []ToolCallRecord{{ToolName: "rca_read", Result: map[string]any{"file": "a.go"}}}
	if got := EvaluateSurrogateSourceGate(records, "会调用 Foo。"); !got.Allow {
		t.Fatalf("no surrogate should allow: %#v", got)
	}
}
