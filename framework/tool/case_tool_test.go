package tool

import (
	"context"
	"testing"

	"github.com/sixath/framework/investigate/cases"
)

func caseToolFixture(t *testing.T) (*Registry, *InMemoryInvestigationStore, *cases.FileStore, context.Context) {
	t.Helper()
	reg := NewRegistry()
	store := NewInMemoryInvestigationStore()
	cs := cases.NewFileStore(t.TempDir())
	resolve := func(context.Context) cases.Store { return cs }
	if err := RegisterInvestigationToolWithOptions(reg, store, InvestigationToolOptions{Cases: resolve}); err != nil {
		t.Fatal(err)
	}
	if err := RegisterCaseLibraryTool(reg, store, resolve); err != nil {
		t.Fatal(err)
	}
	return reg, store, cs, context.WithValue(context.Background(), ContextKeySessionID, "s1")
}

func caseCall(t *testing.T, reg *Registry, ctx context.Context, args map[string]any) map[string]any {
	t.Helper()
	tl, _ := reg.Get(CaseLibraryToolName)
	out, err := tl.Execute(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	return out.(map[string]any)
}

func TestCaseLibrary_ProposeRecallAndIgnoredRule(t *testing.T) {
	reg, store, cs, ctx := caseToolFixture(t)
	store.RecordObservation("s1", ToolObservation{ToolCallID: "c1", Tool: "vm_run_cmd", Output: "2026/09/25 04:48 0 repair", Args: map[string]any{"op": "ls_recent", "path": `G:\yysls\LocalData\Patch`}})
	store.RecordObservation("s1", ToolObservation{ToolCallID: "c2", Tool: "vm_run_cmd", Output: "healthy: patch updated 12:00"})

	if out := caseCall(t, reg, ctx, map[string]any{"action": "propose"}); out["ok"] != false {
		t.Fatalf("propose without a ledger must fail: %+v", out)
	}
	investigationCall(t, reg, ctx, map[string]any{"action": "set_symptom", "symptom": "vm 225781 预启动失败 cloud_game_init_finish 不出现 check.bat 超时"})
	investigationCall(t, reg, ctx, map[string]any{"action": "add_hypothesis", "statement": "宿主机重启打断补丁，repair 标记残留", "kind": "root"})
	investigationCall(t, reg, ctx, map[string]any{"action": "add_evidence", "hypothesis_id": "h1", "quote": "04:48 0 repair", "subject_quote": "04:48 0 repair", "contrast": false, "change": true})
	investigationCall(t, reg, ctx, map[string]any{"action": "set_status", "hypothesis_id": "h1", "status": "accepted", "contrast_unavailable": "test"})

	out := caseCall(t, reg, ctx, map[string]any{"action": "propose", "signature": []any{"cloud_game_init_finish", "check.bat", "预启动超时"}, "fix": "手动跑完修复"})
	if out["status"] != "pending" || out["kind"] != CaseSaveConfirmKind || out["token"] == "" {
		t.Fatalf("propose = %+v", out)
	}
	id := out["token"].(string)
	draft, err := cs.Get(id)
	if err != nil || draft.Status != cases.StatusDraft || draft.Root() == "" || len(draft.VerifyProbes) != 1 || draft.VerifyProbes[0].Args["op"] != "ls_recent" {
		t.Fatalf("draft = %+v err=%v", draft, err)
	}
	if again := caseCall(t, reg, ctx, map[string]any{"action": "propose"}); again["token"] != id {
		t.Fatalf("re-propose must update the same draft: %+v", again)
	}

	ctx2 := context.WithValue(context.Background(), ContextKeySessionID, "s2")
	res := investigationCall(t, reg, ctx2, map[string]any{"action": "set_symptom", "symptom": "vm 255266 预启动一直失败 cloud_game_init_finish 没出现 check.bat"})
	if res["similar_cases"] != nil {
		t.Fatalf("drafts must not be recalled: %+v", res["similar_cases"])
	}
	if _, err := cs.Confirm(id, "u1"); err != nil {
		t.Fatal(err)
	}
	res = investigationCall(t, reg, ctx2, map[string]any{"action": "set_symptom", "symptom": "vm 255266 预启动一直失败 cloud_game_init_finish 没出现 check.bat"})
	if res["similar_cases"] == nil {
		t.Fatalf("confirmed case must be recalled: %+v", res)
	}
	l := store.Get("s2")
	if got := l.IgnoredCases(); len(got) != 1 || got[0] != id {
		t.Fatalf("recalled but untested case must be reported: %+v", got)
	}
	if r := investigationCall(t, reg, ctx2, map[string]any{"action": "add_hypothesis", "statement": "x", "from_case": "nope"}); r["ok"] != false {
		t.Fatal("from_case must reference a recalled case")
	}
	investigationCall(t, reg, ctx2, map[string]any{"action": "add_hypothesis", "statement": "同案例：repair 标记残留", "from_case": id})
	investigationCall(t, reg, ctx2, map[string]any{"action": "set_status", "hypothesis_id": "h1", "status": "rejected", "reason": "no repair file"})
	if got := store.Get("s2").IgnoredCases(); len(got) != 0 {
		t.Fatalf("tested case must not be reported: %+v", got)
	}
}

func TestInvestigation_AbsenceWithoutProducer(t *testing.T) {
	reg, store, ctx := newInvestigationFixture(t)
	store.RecordObservation("s1", ToolObservation{ToolCallID: "c1", Tool: "vm_run_cmd", Output: "check.bat ret=false\ngame patch_log: verify mpk=12 reason=repairing"})
	investigationCall(t, reg, ctx, map[string]any{"action": "add_hypothesis", "statement": "游戏没写出就绪标记", "kind": "trigger", "absence": "cloud_game_init_finish"})
	investigationCall(t, reg, ctx, map[string]any{"action": "add_evidence", "hypothesis_id": "h1", "quote": "check.bat ret=false"})
	investigationCall(t, reg, ctx, map[string]any{"action": "set_status", "hypothesis_id": "h1", "status": "accepted", "contrast_unavailable": "test"})
	if got := store.Get("s1").AbsenceWithoutProducer(); len(got) != 1 {
		t.Fatalf("absence without producer evidence must be reported: %+v", got)
	}
	investigationCall(t, reg, ctx, map[string]any{"action": "add_evidence", "hypothesis_id": "h1", "quote": "verify mpk=12 reason=repairing", "producer": true})
	if got := store.Get("s1").AbsenceWithoutProducer(); len(got) != 0 {
		t.Fatalf("producer evidence must satisfy: %+v", got)
	}
}
