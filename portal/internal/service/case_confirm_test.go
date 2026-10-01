package service

import (
	"context"
	"testing"

	"backend/internal/chat"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/investigate/cases"
	"github.com/sixath/framework/tool"
)

func TestCaseSaveConfirmationAndApply(t *testing.T) {
	ws := t.TempDir()
	draft, err := cases.ForWorkspace(ws).SaveDraft(cases.Case{Symptom: "预启动失败"})
	if err != nil {
		t.Fatal(err)
	}
	call := agent.ToolCallRecord{ToolCallID: "c1", ToolName: tool.CaseLibraryToolName,
		Result: map[string]any{"status": "pending", "token": draft.ID, "name": "预启动失败", "preview": "# x"}}
	req := caseSaveConfirmationFromCall(call)
	if req == nil || req.Kind != tool.CaseSaveConfirmKind || req.Token != draft.ID || req.DSL != "# x" {
		t.Fatalf("req = %+v", req)
	}
	if caseSaveConfirmationFromCall(agent.ToolCallRecord{ToolName: tool.CaseLibraryToolName, Result: map[string]any{"ok": true, "cases": []any{}}}) != nil {
		t.Fatal("search results must not produce a card")
	}
	out, err := chat.ApplyCaseConfirm(context.Background(), ws, draft.ID)
	if err != nil || out["status"] != "confirmed" {
		t.Fatalf("out=%v err=%v", out, err)
	}
	if got, _ := cases.ForWorkspace(ws).Get(draft.ID); got.Status != cases.StatusConfirmed {
		t.Fatalf("case not confirmed: %+v", got)
	}
}
