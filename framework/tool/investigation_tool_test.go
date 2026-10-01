package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func investigationCall(t *testing.T, reg *Registry, ctx context.Context, args map[string]any) map[string]any {
	t.Helper()
	tl, ok := reg.Get(InvestigationToolName)
	if !ok {
		t.Fatal("investigation tool not registered")
	}
	out, err := tl.Execute(ctx, args)
	if err != nil {
		t.Fatalf("execute %v: %v", args, err)
	}
	return out.(map[string]any)
}

func newInvestigationFixture(t *testing.T) (*Registry, *InMemoryInvestigationStore, context.Context) {
	t.Helper()
	reg := NewRegistry()
	store := NewInMemoryInvestigationStore()
	if err := RegisterInvestigationTool(reg, store); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "s1")
	return reg, store, ctx
}

func TestInvestigationTool_OnsetBoundsAndChange(t *testing.T) {
	reg, store, ctx := newInvestigationFixture(t)
	store.RecordObservation("s1", ToolObservation{ToolCallID: "c1", Tool: "vm_run_cmd", Output: "2026/09/26  04:52   0 repair\nhealthy: no repair"})

	investigationCall(t, reg, ctx, map[string]any{"action": "set_onset", "onset": "2026-09-27 21:10 最早可见失败"})
	if l := store.Get("s1"); !l.OnsetUnbounded() {
		t.Fatalf("onset without last_good must be unbounded: %+v", l)
	}
	investigationCall(t, reg, ctx, map[string]any{"action": "set_onset", "onset": "2026-09-26 04:52", "last_good": "2026-09-26 01:17 启动成功"})
	if l := store.Get("s1"); l.OnsetUnbounded() || l.LastGood == "" {
		t.Fatalf("last_good must bound onset: %+v", l)
	}

	investigationCall(t, reg, ctx, map[string]any{"action": "add_hypothesis", "statement": "标志文件缺失导致检测失败"})
	investigationCall(t, reg, ctx, map[string]any{"action": "add_evidence", "hypothesis_id": "h1", "quote": "healthy: no repair", "subject_quote": "04:52 0 repair", "contrast": true})
	investigationCall(t, reg, ctx, map[string]any{"action": "set_status", "hypothesis_id": "h1", "status": "accepted"})
	if got := store.Get("s1").AcceptedWithoutChange(); len(got) != 1 {
		t.Fatalf("accepted without change evidence must be reported: %+v", got)
	}
	investigationCall(t, reg, ctx, map[string]any{"action": "add_evidence", "hypothesis_id": "h1", "quote": "04:52 0 repair", "change": true})
	if got := store.Get("s1").AcceptedWithoutChange(); len(got) != 0 {
		t.Fatalf("change evidence must satisfy: %+v", got)
	}

	investigationCall(t, reg, ctx, map[string]any{"action": "add_hypothesis", "statement": "x"})
	investigationCall(t, reg, ctx, map[string]any{"action": "add_evidence", "hypothesis_id": "h2", "quote": "healthy: no repair"})
	out := investigationCall(t, reg, ctx, map[string]any{"action": "set_status", "hypothesis_id": "h2", "status": "accepted", "contrast_unavailable": "单机", "change_unavailable": "无历史记录"})
	if out["ok"] != true {
		t.Fatalf("accept: %v", out)
	}
	if got := store.Get("s1").AcceptedWithoutChange(); len(got) != 0 {
		t.Fatalf("change_unavailable must satisfy: %+v", got)
	}
}

func TestInvestigationTool_AcceptRequiresEvidenceAndContrast(t *testing.T) {
	reg, store, ctx := newInvestigationFixture(t)
	store.RecordObservation("s1", ToolObservation{ToolCallID: "c1", Tool: "vm_run_cmd", Output: "2026-09-20 09:58  <DIR> patch_log\n2026-09-20 09:58        repair.flag"})
	store.RecordObservation("s1", ToolObservation{ToolCallID: "c2", Tool: "vm_run_cmd", Output: `{"output":"2026-09-10 03:12  <DIR> data\n2026-09-10 03:12        game.exe"}`})

	out := investigationCall(t, reg, ctx, map[string]any{"action": "add_hypothesis", "statement": "游戏目录修复中断"})
	id, _ := out["hypothesis_id"].(string)
	if id != "h1" {
		t.Fatalf("hypothesis_id=%v", out["hypothesis_id"])
	}

	out = investigationCall(t, reg, ctx, map[string]any{"action": "set_status", "hypothesis_id": id, "status": "accepted"})
	if out["ok"] != false || !strings.Contains(out["error"].(string), "without evidence") {
		t.Fatalf("accept without evidence must fail: %v", out)
	}

	out = investigationCall(t, reg, ctx, map[string]any{"action": "add_evidence", "hypothesis_id": id, "quote": "SDK init timeout"})
	if out["ok"] != false || !strings.Contains(out["error"].(string), "not found") {
		t.Fatalf("fabricated quote must be rejected: %v", out)
	}

	out = investigationCall(t, reg, ctx, map[string]any{"action": "add_evidence", "hypothesis_id": id, "quote": "09:58        REPAIR.flag"})
	if out["ok"] != true {
		t.Fatalf("verbatim quote (whitespace/case-insensitive) must be accepted: %v", out)
	}
	out = investigationCall(t, reg, ctx, map[string]any{"action": "set_status", "hypothesis_id": id, "status": "accepted"})
	if out["ok"] != false || !strings.Contains(out["error"].(string), "contrast") {
		t.Fatalf("accept without contrast must fail: %v", out)
	}

	out = investigationCall(t, reg, ctx, map[string]any{"action": "add_evidence", "hypothesis_id": id, "quote": "03:12  <DIR> data", "subject_quote": "09:58 repair.flag", "contrast": true, "tool_call_id": "c2"})
	if out["ok"] != true {
		t.Fatalf("contrast evidence from JSON-escaped output must verify: %v", out)
	}
	out = investigationCall(t, reg, ctx, map[string]any{"action": "set_status", "hypothesis_id": id, "status": "accepted", "reason": "对照机无 repair 痕迹"})
	if out["ok"] != true {
		t.Fatalf("accept with contrast: %v", out)
	}
	ledger := store.Get("s1")
	h := ledger.Hypotheses[0]
	if h.Status != HypothesisAccepted || len(h.Evidence) != 2 || !h.Evidence[0].Verified || h.Evidence[0].ToolCallID != "c1" || h.Evidence[0].Tool != "vm_run_cmd" {
		t.Fatalf("ledger=%+v", ledger)
	}
}

func TestInvestigationTool_ContrastNeedsTwoDifferentQuotes(t *testing.T) {
	reg, store, ctx := newInvestigationFixture(t)
	store.RecordObservation("s1", ToolObservation{ToolCallID: "c1", Tool: "compare", Output: "bad: cfg max_retry=50\ngood: cfg max_retry=3\nboth: counter protect on"})
	investigationCall(t, reg, ctx, map[string]any{"action": "add_hypothesis", "statement": "重试配置被改"})
	out := investigationCall(t, reg, ctx, map[string]any{"action": "add_evidence", "hypothesis_id": "h1", "quote": "good: cfg max_retry=3", "contrast": true})
	if out["ok"] != false || !strings.Contains(out["error"].(string), "subject_quote") {
		t.Fatalf("contrast without subject_quote must fail: %v", out)
	}
	out = investigationCall(t, reg, ctx, map[string]any{"action": "add_evidence", "hypothesis_id": "h1", "quote": "counter protect on", "subject_quote": "Counter  protect on", "contrast": true})
	if out["ok"] != false || !strings.Contains(out["error"].(string), "identical") {
		t.Fatalf("identical contrast quotes must fail: %v", out)
	}
	out = investigationCall(t, reg, ctx, map[string]any{"action": "add_evidence", "hypothesis_id": "h1", "quote": "good: cfg max_retry=3", "subject_quote": "made up", "contrast": true})
	if out["ok"] != false || !strings.Contains(out["error"].(string), "subject_quote") {
		t.Fatalf("fabricated subject_quote must fail: %v", out)
	}
	out = investigationCall(t, reg, ctx, map[string]any{"action": "add_evidence", "hypothesis_id": "h1", "quote": "good: cfg max_retry=3", "subject_quote": "bad: cfg max_retry=50", "contrast": true})
	if out["ok"] != true {
		t.Fatalf("valid contrast: %v", out)
	}
	if e := store.Get("s1").Hypotheses[0].Evidence[0]; !e.Verified || e.SubjectQuote == "" {
		t.Fatalf("evidence=%+v", e)
	}
}

func TestInvestigationTool_CausalChainAndOnsetQuotes(t *testing.T) {
	reg, store, ctx := newInvestigationFixture(t)
	store.RecordObservation("s1", ToolObservation{ToolCallID: "c1", Tool: "timeline", Output: "first failure 2026-09-26T04:52 prelaunch failed\nlast success 2026-09-26T01:17 prelaunch ok"})

	out := investigationCall(t, reg, ctx, map[string]any{"action": "set_onset", "onset": "04:52", "onset_quote": "invented line"})
	if out["ok"] != false {
		t.Fatalf("unverifiable onset_quote must fail: %v", out)
	}
	out = investigationCall(t, reg, ctx, map[string]any{"action": "set_onset", "onset": "04:52", "last_good": "01:17"})
	if out["warning"] == nil || !store.Get("s1").OnsetUnverified() {
		t.Fatalf("onset without quotes must be flagged: %v", out)
	}
	investigationCall(t, reg, ctx, map[string]any{"action": "set_onset", "onset": "04:52", "onset_quote": "2026-09-26T04:52 prelaunch failed", "last_good": "01:17", "last_good_quote": "2026-09-26T01:17 prelaunch ok"})
	if store.Get("s1").OnsetUnverified() {
		t.Fatalf("quoted onset must be verified: %+v", store.Get("s1"))
	}

	investigationCall(t, reg, ctx, map[string]any{"action": "add_hypothesis", "statement": "计数器保护拒绝启动", "kind": "mechanism"})
	tl, _ := reg.Get(InvestigationToolName)
	if res, err := tl.Execute(ctx, map[string]any{"action": "add_hypothesis", "statement": "x", "kind": "cause"}); err == nil && res.(map[string]any)["ok"] != false {
		t.Fatalf("invalid kind must fail: %v", res)
	}
	investigationCall(t, reg, ctx, map[string]any{"action": "add_evidence", "hypothesis_id": "h1", "quote": "prelaunch failed"})
	investigationCall(t, reg, ctx, map[string]any{"action": "set_status", "hypothesis_id": "h1", "status": "accepted", "contrast_unavailable": "单机"})
	if got := store.Get("s1").MechanismOnly(); len(got) != 1 {
		t.Fatalf("accepted mechanism without root must be reported: %+v", got)
	}

	investigationCall(t, reg, ctx, map[string]any{"action": "add_hypothesis", "statement": "01:30 补丁替换了启动配置", "kind": "root"})
	out = investigationCall(t, reg, ctx, map[string]any{"action": "classify", "hypothesis_id": "h1", "kind": "mechanism", "caused_by": "h2"})
	if out["ok"] != true || store.Get("s1").Hypotheses[0].CausedBy != "h2" {
		t.Fatalf("classify: %v", out)
	}
	investigationCall(t, reg, ctx, map[string]any{"action": "add_evidence", "hypothesis_id": "h2", "quote": "prelaunch ok"})
	investigationCall(t, reg, ctx, map[string]any{"action": "set_status", "hypothesis_id": "h2", "status": "accepted", "contrast_unavailable": "单机"})
	if got := store.Get("s1").MechanismOnly(); len(got) != 0 {
		t.Fatalf("accepted root must satisfy: %+v", got)
	}

	reg2, store2, ctx2 := newInvestigationFixture(t)
	store2.RecordObservation("s1", ToolObservation{ToolCallID: "c1", Output: "retry storm"})
	investigationCall(t, reg2, ctx2, map[string]any{"action": "add_hypothesis", "statement": "重试风暴", "kind": "mechanism"})
	investigationCall(t, reg2, ctx2, map[string]any{"action": "add_evidence", "hypothesis_id": "h1", "quote": "retry storm"})
	investigationCall(t, reg2, ctx2, map[string]any{"action": "set_status", "hypothesis_id": "h1", "status": "accepted", "contrast_unavailable": "x"})
	investigationCall(t, reg2, ctx2, map[string]any{"action": "set_root_unknown", "unknown_reason": "上游日志已过期"})
	if got := store2.Get("s1").MechanismOnly(); len(got) != 0 {
		t.Fatalf("root_unknown_reason must satisfy: %+v", got)
	}
}

func TestInvestigationTool_ContrastUnavailableAndOnset(t *testing.T) {
	reg, store, ctx := newInvestigationFixture(t)
	store.RecordObservation("s1", ToolObservation{ToolCallID: "c1", Tool: "es_log_query", Output: "ERROR all instances failed"})
	investigationCall(t, reg, ctx, map[string]any{"action": "add_hypothesis", "statement": "全局配置错误"})
	investigationCall(t, reg, ctx, map[string]any{"action": "add_evidence", "hypothesis_id": "h1", "quote": "all instances failed"})
	out := investigationCall(t, reg, ctx, map[string]any{"action": "set_status", "hypothesis_id": "h1", "status": "accepted", "contrast_unavailable": "所有实例都故障，没有正常对象"})
	if out["ok"] != true {
		t.Fatalf("contrast_unavailable must allow accept: %v", out)
	}

	if !store.Get("s1").OnsetMissing() {
		t.Fatal("onset should be missing initially")
	}
	out = investigationCall(t, reg, ctx, map[string]any{"action": "set_onset"})
	if out["ok"] != false {
		t.Fatalf("empty set_onset must fail: %v", out)
	}
	investigationCall(t, reg, ctx, map[string]any{"action": "set_onset", "unknown_reason": "日志只保留 1 天"})
	if store.Get("s1").OnsetMissing() {
		t.Fatal("unknown_reason satisfies onset")
	}
}

func TestInvestigationTool_UnverifiedWithoutObservations(t *testing.T) {
	reg, store, ctx := newInvestigationFixture(t)
	investigationCall(t, reg, ctx, map[string]any{"action": "add_hypothesis", "statement": "x"})
	out := investigationCall(t, reg, ctx, map[string]any{"action": "add_evidence", "hypothesis_id": "h1", "quote": "anything"})
	if out["ok"] != true || out["warning"] == nil {
		t.Fatalf("without observer wiring evidence is accepted but flagged: %v", out)
	}
	if store.Get("s1").Hypotheses[0].Evidence[0].Verified {
		t.Fatal("must not be marked verified")
	}
}

func TestInvestigationTool_SessionRequiredAndEffect(t *testing.T) {
	reg, _, _ := newInvestigationFixture(t)
	out := investigationCall(t, reg, context.Background(), map[string]any{"action": "view"})
	if out["ok"] != false {
		t.Fatalf("missing session must fail: %v", out)
	}
	tl, _ := reg.Get(InvestigationToolName)
	if tl.EffectFor(nil) != EffectRead {
		t.Fatalf("effect=%q", tl.EffectFor(nil))
	}
}

func TestInMemoryInvestigationStore_ObservationCap(t *testing.T) {
	s := NewInMemoryInvestigationStore()
	for i := 0; i < maxObservationsPerSession+5; i++ {
		s.RecordObservation("s", ToolObservation{ToolCallID: "c", Output: "line"})
	}
	if n := len(s.observed["s"]); n != maxObservationsPerSession {
		t.Fatalf("observations=%d", n)
	}
	s.RecordObservation("s", ToolObservation{Output: strings.Repeat("x", maxObservationBytes+10)})
	list := s.observed["s"]
	if len(list[len(list)-1].Output) != maxObservationBytes {
		t.Fatal("observation output must be truncated")
	}
	big := strings.Repeat("y", maxObservationBytes)
	for i := 0; i < maxSessionObservationBytes/maxObservationBytes+3; i++ {
		s.RecordObservation("s", ToolObservation{Output: big})
	}
	total := 0
	for _, o := range s.observed["s"] {
		total += len(o.Output)
	}
	if total > maxSessionObservationBytes {
		t.Fatalf("session observations exceed budget: %d", total)
	}
}

// 引用常取自 ES 结果深处、多层 JSON 转义内容或落盘文件，都应能核对到。
func TestFindQuote_DeepEscapedAndSpilled(t *testing.T) {
	s := NewInMemoryInvestigationStore()
	var hits []map[string]any
	for i := 0; i < 60; i++ {
		hits = append(hits, map[string]any{"M": fmt.Sprintf("noise line %d %s", i, strings.Repeat("z", 200))})
	}
	hits = append(hits, map[string]any{"M": `release failed: 当前dal vm, vm被占用中：assignState = 300, mgrState = 1. flow <prelaunch_x> & "q"`})
	inner, _ := json.Marshal(map[string]any{"hits": hits})
	s.RecordObservation("s1", ToolObservation{ToolCallID: "c1", Tool: "es_log_query", Output: ObservationText(string(inner))})
	for _, q := range []string{"vm被占用中：assignState = 300, mgrState = 1.", `flow <prelaunch_x> & "q"`} {
		if _, found, _ := s.FindQuote("s1", "", q); !found {
			t.Fatalf("quote %q should be found deep in doubly-encoded output", q)
		}
	}

	ws := t.TempDir()
	ctx := context.WithValue(context.WithValue(context.Background(), ContextKeyWorkspaceRoot, ws), ContextKeySessionID, "sess1")
	dir := filepath.Join(ws, "tmp", "results", "sess1")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "1_vm_run_cmd_2.txt"), []byte("head\r\n2026/09/26  04:52                 0 repair\r\n"), 0o644)
	_ = os.WriteFile(filepath.Join(ws, "tmp", "results", "other.txt"), []byte("secret-other-session"), 0o644)
	result := map[string]any{"stdout": "head ...[omitted]...", "spill": &TextSpill{Path: "tmp/results/sess1/1_vm_run_cmd_2.txt"}, "x": "tmp/results/other/other.txt"}
	s.RecordObservation("sess1", ToolObservation{ToolCallID: "c2", Tool: "vm_run_cmd", Output: ObservationTextWithSpills(ctx, result)})
	if _, found, _ := s.FindQuote("sess1", "c2", "2026/09/26 04:52 0 repair"); !found {
		t.Fatal("quote from spilled file should be found")
	}
	if _, found, _ := s.FindQuote("sess1", "", "secret-other-session"); found {
		t.Fatal("files of other sessions must not be read")
	}
}
