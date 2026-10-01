package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

type stubStopHook struct {
	calls int
	fn    func(in StopHookInput) StopDecision
}

func (s *stubStopHook) OnStop(_ context.Context, in StopHookInput) StopDecision {
	s.calls++
	return s.fn(in)
}

func TestStopHook_ContinuesThenFinishes(t *testing.T) {
	fake := &fakeOpenAIClient{
		plainReplies: []string{"让我进一步查找日志", "结论：磁盘满"},
	}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	hook := &stubStopHook{fn: func(in StopHookInput) StopDecision {
		if strings.HasPrefix(in.Text, "让我") {
			return StopDecision{Continue: true, RuleID: "intent", Message: "请直接调用工具"}
		}
		return StopDecision{}
	}}
	a := NewReActAgent(fake, memory.NewBufferMemory(5), reg, WithReActMaxSteps(5), WithReActStopHooks(hook))
	resp, err := a.Run(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "为什么失败"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "结论：磁盘满" {
		t.Fatalf("text=%q", resp.Text)
	}
	tr := resp.Metadata["trace"].(*RunTrace)
	if len(tr.StopHookContinues) != 1 || tr.StopHookContinues[0] != "intent" {
		t.Fatalf("StopHookContinues=%v", tr.StopHookContinues)
	}
	var sawNudge, sawPremature bool
	for _, m := range fake.lastToolMessages {
		if m.Role == "user" && m.Content == "请直接调用工具" && m.Metadata[model.MetadataKeySixathOrigin] == model.OriginStopHook {
			sawNudge = true
		}
		if m.Role == "assistant" && m.Content == "让我进一步查找日志" {
			sawPremature = true
		}
	}
	if !sawNudge || !sawPremature {
		t.Fatalf("second model call must see premature answer and nudge: %+v", fake.lastToolMessages)
	}
}

func TestStopHook_RespectsMaxNudges(t *testing.T) {
	fake := &fakeOpenAIClient{finalReply: "让我继续查"}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	hook := &stubStopHook{fn: func(StopHookInput) StopDecision {
		return StopDecision{Continue: true, RuleID: "always", Message: "继续"}
	}}
	a := NewReActAgent(fake, nil, reg, WithReActMaxSteps(10), WithReActStopHooks(hook), WithReActMaxStopNudges(2))
	resp, err := a.Run(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "q"}}})
	if err != nil {
		t.Fatal(err)
	}
	tr := resp.Metadata["trace"].(*RunTrace)
	if len(tr.StopHookContinues) != 2 || fake.toolCalls != 3 {
		t.Fatalf("continues=%v modelCalls=%d, want 2 continues and 3 model calls", tr.StopHookContinues, fake.toolCalls)
	}
}

func TestStopHook_StreamSeparatesPrematureText(t *testing.T) {
	fake := &fakeOpenAIClient{plainReplies: []string{"让我查一下", "最终结论"}}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	hook := &stubStopHook{fn: func(in StopHookInput) StopDecision {
		if in.Text == "让我查一下" {
			return StopDecision{Continue: true, RuleID: "intent", Message: "继续"}
		}
		return StopDecision{}
	}}
	a := NewReActAgent(fake, nil, reg, WithReActMaxSteps(5), WithReActStopHooks(hook))
	ch, err := a.RunEvents(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "q"}}})
	if err != nil {
		t.Fatal(err)
	}
	var deltas strings.Builder
	var done *StreamEvent
	for ev := range ch {
		switch ev.Type {
		case StreamEventDelta:
			deltas.WriteString(ev.Text)
		case StreamEventDone:
			e := ev
			done = &e
		case StreamEventError:
			t.Fatalf("stream error: %s", ev.Error)
		}
	}
	if done == nil || done.Text != "最终结论" {
		t.Fatalf("done=%+v", done)
	}
	if got := deltas.String(); !strings.Contains(got, "最终结论") {
		t.Fatalf("deltas=%q", got)
	}
	if len(done.Trace.StopHookContinues) != 1 {
		t.Fatalf("StopHookContinues=%v", done.Trace.StopHookContinues)
	}
}

func TestStopHook_ToolsStreamPath(t *testing.T) {
	fake := &fakeSequencedStreamingToolClient{texts: []string{"让我查一下", "最终结论"}}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	hook := &stubStopHook{fn: func(in StopHookInput) StopDecision {
		if in.Text == "让我查一下" {
			return StopDecision{Continue: true, RuleID: "intent", Message: "继续"}
		}
		return StopDecision{}
	}}
	a := NewReActAgent(fake, nil, reg, WithReActMaxSteps(5), WithReActStopHooks(hook))
	ch, err := a.RunEvents(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "q"}}})
	if err != nil {
		t.Fatal(err)
	}
	var deltas strings.Builder
	var done *StreamEvent
	for ev := range ch {
		switch ev.Type {
		case StreamEventDelta:
			deltas.WriteString(ev.Text)
		case StreamEventDone:
			e := ev
			done = &e
		case StreamEventError:
			t.Fatalf("stream error: %s", ev.Error)
		}
	}
	if fake.calls != 2 {
		t.Fatalf("model calls=%d want 2", fake.calls)
	}
	if got := deltas.String(); got != "让我查一下\n\n最终结论" {
		t.Fatalf("deltas=%q", got)
	}
	if done == nil || done.Text != "最终结论" || len(done.Trace.StopHookContinues) != 1 {
		t.Fatalf("done=%+v", done)
	}
}

func loadExampleStopHooks(t *testing.T, todos tool.TodoStore) []StopHook {
	t.Helper()
	return loadExampleStopHooksWith(t, StopHookOptions{Todos: todos})
}

func loadExampleStopHooksWith(t *testing.T, opts StopHookOptions) []StopHook {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "stop_rules_example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	hooks, maxNudges, err := ParseHarnessStopHooksYAML(data, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks) != 5 || maxNudges != 2 {
		t.Fatalf("hooks=%d max=%d", len(hooks), maxNudges)
	}
	return hooks
}

func firstStop(ctx context.Context, hooks []StopHook, in StopHookInput) StopDecision {
	for _, h := range hooks {
		if d := h.OnStop(ctx, in); d.Continue {
			return d
		}
	}
	return StopDecision{}
}

func TestExampleStopRules_Classification(t *testing.T) {
	hooks := loadExampleStopHooks(t, nil)
	cases := []struct {
		name string
		text string
		want string
	}{
		{"intent from real session", "日志中有大量重复的 `checkVmIfExitPrelaunchInfoMatched` 警告。让我进一步查找失败相关的关键日志，包括失败回调、错误码等。", "intent-without-action"},
		{"intent english", "The index looks empty. Let me check the other cluster.", "intent-without-action"},
		{"readonly permission from real session", "要精确定位，需要进入 VM `10.30.16.24` 查看 cgvmagent 的详细日志。是否需要我登入该 VM 查看？", "readonly-permission"},
		{"readonly permission variant", "建议查看 NtUniSdk.log 中的崩溃堆栈。需要我进一步查看这些日志文件吗？", "readonly-permission"},
		{"mutating ask is legit", "根因已确认。是否需要我重启该实例的 cgvmagent 进程？", ""},
		{"clarification is legit", "是否需要我查询生产环境还是测试环境？", ""},
		{"recommendation is not intent", "## 建议\n1. 下一步检查游戏补丁日志\n2. 联系厂商确认版本", ""},
		{"plain conclusion", "根因：repair 标记导致每次启动全量校验，超过 180 秒被判超时。", ""},
		{"polite closing", "如有问题请让我知道。", ""},
		// 以下三条来自 glm-5.1 真实评测：前文的应急建议 / 信息索取不应豁免末尾的只读请示。
		{"ask after info request (live eval)", "## 需要您提供更多信息\n\n为了更精准定位，请提供：\n- 故障发生的具体时间\n- 是否有监控告警记录\n\n是否需要我进一步排查特定方向？", "readonly-permission"},
		{"ask after restart advice (live eval)", "### 应急处理建议\n\n如果影响业务，可考虑：\n- **重启 order-svc-1**（释放泄露的连接）\n- **临时增加数据库最大连接数**\n\n需要我帮你执行具体的数据库查询或日志分析吗？", "readonly-permission"},
		{"ask after cleanup advice (live eval)", "4. **手动触发重试**\n   - 如果需要，可以清理空文件后手动触发提取任务\n\n需要我帮你进一步检查上游提取任务的日志吗？", "readonly-permission"},
		{"intent with modifiers (live eval)", "在等待您反馈的同时，我将先进行初步调查，重点排查以下方向：\n- 近期变更\n\n请提供上述信息，我将立即开始深入分析。", "intent-without-action"},
		{"ask with other verb (live eval)", "**建议后续操作：**\n1. 检查数据源\n\n需要我协助检查上游数据生成逻辑或定时任务状态吗？", "readonly-permission"},
		{"statement is not ask", "这一步需要我方运维配合完成。", ""},
		{"restart ask after readonly findings", "日志显示连接池耗尽。\n\n需要我重启 order-svc-1 吗？", ""},
	}
	for _, tc := range cases {
		d := firstStop(context.Background(), hooks, StopHookInput{Text: tc.text})
		if d.RuleID != tc.want {
			t.Errorf("%s: rule=%q want %q", tc.name, d.RuleID, tc.want)
		}
	}
}

func TestStopRule_UnlessScopeValidation(t *testing.T) {
	if _, _, err := ParseHarnessStopHooksYAML([]byte("stop_rules:\n  - id: x\n    match: a\n    unless: b\n    unless_scope: nope\n    message: m\n"), StopHookOptions{}); err == nil {
		t.Fatal("unknown unless_scope must fail")
	}
	if _, _, err := ParseHarnessStopHooksYAML([]byte("stop_rules:\n  - id: x\n    when_open_todos: true\n    unless: b\n    unless_scope: match_sentence\n    message: m\n"), StopHookOptions{}); err == nil {
		t.Fatal("match_sentence without match must fail")
	}
	if got := enclosingSentence("前文。请提供时间！需要我查吗？尾巴", len("前文。请提供时间！"), len("前文。请提供时间！需要我")); got != "需要我查吗？" {
		t.Fatalf("sentence=%q", got)
	}
}

func TestExampleStopRules_OpenTodos(t *testing.T) {
	store := tool.NewInMemoryTodoStore()
	store.Replace("s1", []tool.TodoItem{
		{ID: "1", Content: "查 planner 日志", Status: tool.TodoStatusCompleted},
		{ID: "2", Content: "对照正常实例", Status: tool.TodoStatusPending},
	})
	hooks := loadExampleStopHooks(t, store)
	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "s1")
	todoCall := []ToolCallRecord{{ToolName: "todo", Allowed: true}}

	d := firstStop(ctx, hooks, StopHookInput{Text: "结论如下……", ToolCalls: todoCall})
	if d.RuleID != "open-todos" || !strings.Contains(d.Message, "- [pending] 对照正常实例") || strings.Contains(d.Message, "查 planner 日志") {
		t.Fatalf("decision=%+v", d)
	}
	if d := firstStop(ctx, hooks, StopHookInput{Text: "结论如下……"}); d.Continue {
		t.Fatalf("todo not used this run must not trigger: %+v", d)
	}
}

func TestExampleStopRules_LedgerGaps(t *testing.T) {
	store := tool.NewInMemoryInvestigationStore()
	hooks := loadExampleStopHooksWith(t, StopHookOptions{Investigations: store})
	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "s1")

	observe := InvestigationObserver(store)
	observe(ctx, nil, ToolCallRecord{ToolCallID: "c1", ToolName: "es_log_query", Result: map[string]any{"hits": []string{"WARN sdk init slow"}}, Allowed: true})
	observe(ctx, nil, ToolCallRecord{ToolCallID: "c2", ToolName: tool.InvestigationToolName, Result: "ledger"})
	if _, found, _ := store.FindQuote("s1", "", "sdk init slow"); !found {
		t.Fatal("observer must record tool output")
	}
	if _, found, _ := store.FindQuote("s1", "", "ledger"); found {
		t.Fatal("ledger tool's own output must not be recorded")
	}

	_, _ = store.Update("s1", func(l *tool.InvestigationLedger) error {
		l.Hypotheses = []tool.Hypothesis{
			{ID: "h1", Statement: "SDK 初始化慢", Status: tool.HypothesisRejected},
			{ID: "h2", Statement: "目录修复中断", Status: tool.HypothesisOpen},
		}
		return nil
	})
	ledgerCall := []ToolCallRecord{{ToolName: tool.InvestigationToolName, Allowed: true}}

	d := firstStop(ctx, hooks, StopHookInput{Text: "结论：SDK 慢。", ToolCalls: ledgerCall})
	if d.RuleID != "ledger-gaps" || !strings.Contains(d.Message, "问题开始时间未确认") || !strings.Contains(d.Message, "h2") || strings.Contains(d.Message, "h1") {
		t.Fatalf("decision=%+v", d)
	}
	if d := firstStop(ctx, hooks, StopHookInput{Text: "结论：SDK 慢。"}); d.Continue {
		t.Fatalf("ledger not used this run must not trigger: %+v", d)
	}

	_, _ = store.Update("s1", func(l *tool.InvestigationLedger) error {
		l.Onset = "2026-09-20 09:58"
		l.Hypotheses[1].Status = tool.HypothesisAccepted
		return nil
	})
	d = firstStop(ctx, hooks, StopHookInput{Text: "根因：目录修复中断。", ToolCalls: ledgerCall})
	if d.RuleID != "ledger-gaps" || !strings.Contains(d.Message, "last_good") || !strings.Contains(d.Message, "h2 没有说明开始前后发生了什么变化") {
		t.Fatalf("unbounded onset and accepted cause without change must trigger: %+v", d)
	}

	_, _ = store.Update("s1", func(l *tool.InvestigationLedger) error {
		l.LastGood = "2026-09-20 01:17 最后一次启动成功"
		l.Hypotheses[1].Evidence = append(l.Hypotheses[1].Evidence, tool.EvidenceLink{Quote: "04:52 0 repair", Change: true})
		return nil
	})
	d = firstStop(ctx, hooks, StopHookInput{Text: "根因：目录修复中断。", ToolCalls: ledgerCall})
	if d.RuleID != "ledger-gaps" || !strings.Contains(d.Message, "没有原文支撑") || !strings.Contains(d.Message, "都不是 root") {
		t.Fatalf("unquoted onset and unclassified accepted cause must trigger: %+v", d)
	}

	_, _ = store.Update("s1", func(l *tool.InvestigationLedger) error {
		l.OnsetEvidence = &tool.EvidenceLink{Quote: "09:58 prelaunch failed", Verified: true}
		l.LastGoodEvidence = &tool.EvidenceLink{Quote: "01:17 prelaunch ok", Verified: true}
		l.Hypotheses[1].Kind = tool.HypothesisKindRoot
		return nil
	})
	if d := firstStop(ctx, hooks, StopHookInput{Text: "根因：目录修复中断。", ToolCalls: ledgerCall}); d.Continue {
		t.Fatalf("closed ledger must not trigger: %+v", d)
	}
}

func TestExampleStopRules_LedgerUnused(t *testing.T) {
	store := tool.NewInMemoryInvestigationStore()
	hooks := loadExampleStopHooksWith(t, StopHookOptions{Investigations: store})
	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "s1")
	has := func(name string) bool { return name == tool.InvestigationToolName }
	calls := func(n int) []ToolCallRecord {
		out := make([]ToolCallRecord, n)
		for i := range out {
			out[i] = ToolCallRecord{ToolName: "vm_run_cmd", Allowed: true}
		}
		return out
	}
	text := "根因：游戏进程启动后崩溃。"

	d := firstStop(ctx, hooks, StopHookInput{Text: text, ToolCalls: calls(8), HasTool: has})
	if d.RuleID != "ledger-unused" || !strings.Contains(d.Message, "没有用 investigation 台账") {
		t.Fatalf("decision=%+v", d)
	}
	if d := firstStop(ctx, hooks, StopHookInput{Text: text, ToolCalls: calls(3), HasTool: has}); d.Continue {
		t.Fatalf("short run must not trigger: %+v", d)
	}
	if d := firstStop(ctx, hooks, StopHookInput{Text: text, ToolCalls: calls(8)}); d.Continue {
		t.Fatalf("ledger tool not registered must not trigger: %+v", d)
	}
}

func TestWorkspaceInvestigationLedgerEnabled(t *testing.T) {
	root := t.TempDir()
	if WorkspaceInvestigationLedgerEnabled(root) {
		t.Fatal("missing file must be disabled")
	}
	if err := os.MkdirAll(filepath.Join(root, "harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "harness", "hooks.yaml"), []byte("investigation_ledger: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !WorkspaceInvestigationLedgerEnabled(root) {
		t.Fatal("expected enabled")
	}
}

func TestParseHarnessStopHooksYAML_Validation(t *testing.T) {
	bad := []string{
		"stop_rules:\n  - id: x\n    match: 'a'\n",
		"stop_rules:\n  - id: x\n    message: m\n",
		"stop_rules:\n  - id: x\n    match: '('\n    message: m\n",
		"stop_rules:\n  - id: x\n    when_investigation_gaps: [nope]\n    message: m\n",
	}
	for _, y := range bad {
		if _, _, err := ParseHarnessStopHooksYAML([]byte(y), StopHookOptions{}); err == nil {
			t.Errorf("expected error for %q", y)
		}
	}
	hooks, _, err := ParseHarnessStopHooksYAML([]byte("rules: []\n"), StopHookOptions{})
	if err != nil || len(hooks) != 0 {
		t.Fatalf("no stop_rules must be ok: hooks=%v err=%v", hooks, err)
	}
}

func TestLoadWorkspaceStopHooks_MissingFile(t *testing.T) {
	hooks, n, err := LoadWorkspaceStopHooks(t.TempDir(), StopHookOptions{})
	if err != nil || hooks != nil || n != 0 {
		t.Fatalf("hooks=%v n=%d err=%v", hooks, n, err)
	}
}
