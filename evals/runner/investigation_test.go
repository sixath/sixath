package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/investigate/cases"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

var prematureForbid = []string{
	`(让我|我将|我会|接下来)[^。\n]{0,30}(查|看|检查|确认|分析)[^。\n]{0,30}[。.…:：]?\s*$`,
	`(是否需要我|要不要我|需要我[^。\n]{0,20}吗)[^\n]*$`,
}

func investigationTask() Task {
	return Task{
		ID:       "inv",
		Category: "investigation",
		Input:    "x",
		MaxSteps: 8,
		Expect: Expectation{
			RootCause:    []string{"repair|修复"},
			MinToolCalls: 2,
			MustContrast: true,
			ForbidOutput: prematureForbid,
		},
		Fixtures: []ToolFixture{
			{Tool: "vm_run_cmd", Match: "good", Contrast: true, Response: "normal dir"},
			{Tool: "vm_run_cmd", Match: "bad", Response: "repair.flag"},
		},
	}
}

func TestInvestigationValidate(t *testing.T) {
	if err := investigationTask().Validate(); err != nil {
		t.Fatalf("valid task rejected: %v", err)
	}
	noRoot := investigationTask()
	noRoot.Expect.RootCause = nil
	noFixtures := investigationTask()
	noFixtures.Fixtures = nil
	noContrast := investigationTask()
	noContrast.Fixtures = noContrast.Fixtures[1:]
	badRegex := investigationTask()
	badRegex.Expect.ForbidOutput = []string{"("}
	for name, task := range map[string]Task{"no root": noRoot, "no fixtures": noFixtures, "no contrast fixture": noContrast, "bad regex": badRegex} {
		if err := task.Validate(); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestEvaluateInvestigation(t *testing.T) {
	task := investigationTask()
	cases := []struct {
		name   string
		ev     investigationEvidence
		reason string
	}{
		{"pass", investigationEvidence{Output: "根因：游戏目录修复中断，文件被锁。", ToolCalls: 3, ContrastHits: 1}, ""},
		{"intent ending", investigationEvidence{Output: "SDK 初始化较慢。让我进一步查看目录。", ToolCalls: 3, ContrastHits: 1}, "premature_stop"},
		{"readonly permission", investigationEvidence{Output: "发现可疑点，是否需要我继续查看日志？", ToolCalls: 3, ContrastHits: 1}, "premature_stop"},
		{"too few calls", investigationEvidence{Output: "repair 失败", ToolCalls: 1, ContrastHits: 1}, "too_few_tool_calls"},
		{"no contrast", investigationEvidence{Output: "repair 失败", ToolCalls: 3}, "no_contrast"},
		{"red herring", investigationEvidence{Output: "根因是 SDK 初始化超时。", ToolCalls: 3, ContrastHits: 1}, "missing_root_cause"},
	}
	for _, c := range cases {
		ok, reason := evaluateInvestigation(task, c.ev)
		if ok != (c.reason == "") || reason != c.reason {
			t.Errorf("%s: ok=%v reason=%q want %q", c.name, ok, reason, c.reason)
		}
	}
}

func TestStoppedAtMechanism(t *testing.T) {
	task := investigationTask()
	task.Expect.ForbidRoot = []string{"错误计数|counter"}
	cases := []struct {
		name   string
		out    string
		reason string
	}{
		{"mechanism as root", "根因：错误计数达到上限，保护机制拦截了启动。", "stopped_at_mechanism"},
		{"chain to root", "根因：补丁 repair 中断导致文件被锁，随后错误计数达到上限触发保护。", ""},
		{"mechanism mentioned outside root sentence", "错误计数达到上限只是表现。\n根因：repair 被中断。", ""},
		{"no root sentence", "错误计数达到上限；repair 被中断。", ""},
		{"heading is not a conclusion", "# 错误计数保护根因分析\n根因链条：repair 被中断导致文件被锁。", ""},
	}
	for _, c := range cases {
		ok, reason := evaluateInvestigation(task, investigationEvidence{Output: c.out, ToolCalls: 3, ContrastHits: 1})
		if ok != (c.reason == "") || reason != c.reason {
			t.Errorf("%s: ok=%v reason=%q want %q", c.name, ok, reason, c.reason)
		}
	}
	if investigationAttribution("stopped_at_mechanism") != "prompt" {
		t.Error("stopped_at_mechanism is a method problem")
	}
}

func TestInvestigationValidate_LedgerExpectations(t *testing.T) {
	good := investigationTask()
	good.Expect.ExpectOnset = &OnsetWindow{After: "2026-09-20 22:30:00", Before: "2026-09-20 22:45"}
	good.Expect.ExpectKinds = []string{"root", "trigger"}
	if err := good.Validate(); err != nil {
		t.Fatalf("valid: %v", err)
	}
	badKind := good
	badKind.Expect.ExpectKinds = []string{"cause"}
	reversed := good
	reversed.Expect.ExpectOnset = &OnsetWindow{After: "2026-09-21 00:00", Before: "2026-09-20 00:00"}
	noTime := good
	noTime.Expect.ExpectOnset = &OnsetWindow{After: "yesterday", Before: "2026-09-20 00:00"}
	for name, task := range map[string]Task{"bad kind": badKind, "reversed": reversed, "not a time": noTime} {
		if err := task.Validate(); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestScoreLedger(t *testing.T) {
	task := investigationTask()
	task.Expect.ExpectOnset = &OnsetWindow{After: "2026-09-20 22:30:00", Before: "2026-09-20 22:45:00"}
	task.Expect.ExpectKinds = []string{"root"}
	full := tool.InvestigationLedger{
		Symptom:          "prestart failed",
		Onset:            "2026-09-20 22:41:10",
		OnsetEvidence:    &tool.EvidenceLink{Quote: "x", Verified: true},
		LastGood:         "2026-09-20 21:03:55",
		LastGoodEvidence: &tool.EvidenceLink{Quote: "y", Verified: true},
		Hypotheses: []tool.Hypothesis{
			{ID: "h1", Status: tool.HypothesisAccepted, Kind: tool.HypothesisKindRoot},
			{ID: "h2", Status: tool.HypothesisAccepted, Kind: tool.HypothesisKindMechanism, CausedBy: "h1"},
		},
	}
	if q := scoreLedger(task, full); q.Score != 1 {
		t.Fatalf("complete ledger must score 1: %+v", q)
	}
	shallow := tool.InvestigationLedger{
		Symptom: "prestart failed",
		Onset:   "2026-09-21 08:10:02",
		Hypotheses: []tool.Hypothesis{
			{ID: "h1", Status: tool.HypothesisAccepted, Kind: tool.HypothesisKindMechanism},
		},
	}
	q := scoreLedger(task, shallow)
	for _, k := range []string{"onset_verified", "onset_in_window", "chain_reaches_root", "kind_root"} {
		if q.Checks[k] {
			t.Errorf("%s must fail for a mechanism-only ledger with the latest failure as onset", k)
		}
	}
	if q.Score >= 0.5 {
		t.Fatalf("shallow ledger score=%v", q.Score)
	}
	if q := scoreLedger(task, tool.InvestigationLedger{}); q.Checks["ledger_used"] || q.Score != 0 {
		t.Fatalf("unused ledger: %+v", q)
	}
}

func TestCriticRoundsAndSummary(t *testing.T) {
	if n := criticRounds([]string{"gaps", "critic:rules", "critic:model"}); n != 2 {
		t.Fatalf("critic rounds=%d", n)
	}
	one, half := 1.0, 0.5
	s := ComputeSummary([]TaskResult{
		{TaskID: "a", Category: "investigation", LedgerScore: &one, CriticRounds: 2},
		{TaskID: "b", Category: "investigation", LedgerScore: &half},
		{TaskID: "c", Category: "investigation"},
	}, nil)
	if s.LedgerScored != 2 || s.AvgLedgerScore != 0.75 || s.AvgCriticRounds != 2.0/3 {
		t.Fatalf("summary=%+v", s)
	}
}

func TestRunLiveTask_TimelineLedgerCriticOnDistractorTask(t *testing.T) {
	tasks, err := LoadTasks(filepath.Join("..", "tasks", "investigation.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var task Task
	for _, tk := range tasks {
		if tk.ID == "investigation-prestart-counter-guard" {
			task = tk
		}
	}
	if task.ID == "" {
		t.Fatal("distractor task missing")
	}
	store := tool.NewInMemoryInvestigationStore()
	steps := []model.ToolStep{
		{Used: true, ToolCallID: "c1", ToolName: tool.TimelineToolName, Arguments: map[string]any{
			"window": "3d",
			"series": []any{
				map[string]any{"label": "success", "args": map[string]any{"query": "vm=300112 prestart ok"}},
				map[string]any{"label": "failure", "args": map[string]any{"query": "vm=300112 failed"}},
			},
			"events": []any{map[string]any{"label": "patch", "args": map[string]any{"query": "vm=300112 patch"}}},
		}},
		{Used: true, ToolCallID: "c2", ToolName: "es_log_query", Arguments: map[string]any{"query": "vm=300140"}},
		{Used: true, ToolCallID: "c3", ToolName: tool.InvestigationToolName, Arguments: map[string]any{
			"action": "set_onset", "onset": "2026-09-20 22:41:10",
			"onset_quote":     "2026-09-20T22:41:10.000Z ERROR vm=300112 prestart game launch failed",
			"last_good":       "2026-09-20 21:03:55",
			"last_good_quote": "2026-09-20T21:03:55.000Z INFO vm=300112 prestart ok",
		}},
	}
	final := "根因：22:36 下发的 hotfix-0920 以 repair 模式校验文件时锁住 bin/engine.dll，导致预启动失败；错误计数保护只是后续表现。"
	res := runLiveTask(task, buildMockRegistry(), &scriptedModel{steps: steps, final: final},
		liveOptions{Ledger: true, LedgerStore: store, Timeline: true, Critic: &agent.CriticConfig{Enabled: true, MinToolCalls: 99}})
	if !res.Passed {
		t.Fatalf("expected pass, got %+v", res)
	}
	l := store.Get("eval-" + task.ID)
	if l.OnsetEvidence == nil || !l.OnsetEvidence.Verified || l.LastGoodEvidence == nil || !l.LastGoodEvidence.Verified {
		t.Fatalf("timeline quotes must verify in the ledger: %+v", l)
	}
	if res.LedgerScore == nil || !res.LedgerChecks["onset_in_window"] || !res.LedgerChecks["onset_verified"] {
		t.Fatalf("ledger checks=%v", res.LedgerChecks)
	}
	if res.LedgerChecks["chain_reaches_root"] {
		t.Fatal("no root hypothesis was recorded")
	}
}

func TestMatchFixture(t *testing.T) {
	list := []ToolFixture{
		{Tool: "t", Match: "Repair|patch_log", Response: "a"},
		{Tool: "t", Match: "225781", Response: "b"},
		{Tool: "t", Response: "fallback"},
	}
	for args, want := range map[string]string{
		`{"cmd":"type patch_log\\repair.log"}`: "a",
		`{"vm":"225781","cmd":"dir"}`:           "b",
		`{"vm":"1"}`:                            "fallback",
	} {
		f, ok := matchFixture(list, args)
		if !ok || f.Response != want {
			t.Errorf("args %s: got %q ok=%v want %q", args, f.Response, ok, want)
		}
	}
	if _, ok := matchFixture(list[:2], `{"vm":"1"}`); ok {
		t.Error("expected miss without catch-all fixture")
	}
}

func TestRunLiveTask_InvestigationWithFixtures(t *testing.T) {
	task := investigationTask()
	steps := []model.ToolStep{
		{Used: true, ToolCallID: "c1", ToolName: "vm_run_cmd", Arguments: map[string]any{"vm": "bad", "cmd": "dir"}},
		{Used: true, ToolCallID: "c2", ToolName: "vm_run_cmd", Arguments: map[string]any{"vm": "good", "cmd": "dir"}},
	}
	pass := runLiveTask(task, buildMockRegistry(), &scriptedModel{steps: steps, final: "对照正常机无 repair.flag，根因为修复中断。"}, liveOptions{})
	if !pass.Passed {
		t.Fatalf("expected pass, got %+v", pass)
	}

	noContrast := runLiveTask(task, buildMockRegistry(), &scriptedModel{steps: steps[:1], final: "根因为 repair 中断。"}, liveOptions{})
	if noContrast.Passed || noContrast.FailureReason != "too_few_tool_calls" {
		t.Fatalf("expected too_few_tool_calls, got %+v", noContrast)
	}
	task.Expect.MinToolCalls = 1
	noContrast = runLiveTask(task, buildMockRegistry(), &scriptedModel{steps: steps[:1], final: "根因为 repair 中断。"}, liveOptions{})
	if noContrast.FailureReason != "no_contrast" || noContrast.Attribution != "prompt" {
		t.Fatalf("expected no_contrast/prompt, got %+v", noContrast)
	}

	premature := runLiveTask(task, buildMockRegistry(), &scriptedModel{steps: steps, final: "让我再检查一下目录"}, liveOptions{})
	if premature.FailureReason != "premature_stop" || premature.Attribution != "harness" {
		t.Fatalf("expected premature_stop/harness, got %+v", premature)
	}
}

func TestRunLiveTask_LedgerVerifiesQuotesAgainstFixtures(t *testing.T) {
	task := investigationTask()
	store := tool.NewInMemoryInvestigationStore()
	steps := []model.ToolStep{
		{Used: true, ToolCallID: "c1", ToolName: "vm_run_cmd", Arguments: map[string]any{"vm": "bad", "cmd": "dir"}},
		{Used: true, ToolCallID: "c2", ToolName: "vm_run_cmd", Arguments: map[string]any{"vm": "good", "cmd": "dir"}},
		{Used: true, ToolCallID: "c3", ToolName: tool.InvestigationToolName, Arguments: map[string]any{"action": "add_hypothesis", "statement": "修复中断"}},
		{Used: true, ToolCallID: "c4", ToolName: tool.InvestigationToolName, Arguments: map[string]any{"action": "add_evidence", "hypothesis_id": "h1", "quote": "repair.flag"}},
		{Used: true, ToolCallID: "c5", ToolName: tool.InvestigationToolName, Arguments: map[string]any{"action": "add_evidence", "hypothesis_id": "h1", "quote": "编造的日志"}},
	}
	res := runLiveTask(task, buildMockRegistry(), &scriptedModel{steps: steps, final: "对照正常机无 repair.flag，根因为修复中断。"}, liveOptions{Ledger: true, LedgerStore: store})
	if !res.Passed {
		t.Fatalf("expected pass, got %+v", res)
	}
	ledger := store.Get("eval-" + task.ID)
	if len(ledger.Hypotheses) != 1 || len(ledger.Hypotheses[0].Evidence) != 1 {
		t.Fatalf("only the verbatim quote may be linked: %+v", ledger)
	}
	if ev := ledger.Hypotheses[0].Evidence[0]; !ev.Verified || ev.Tool != "vm_run_cmd" {
		t.Fatalf("evidence must be verified against fixture output: %+v", ev)
	}
}

func TestRunLiveTask_CasesRecalledFromDir(t *testing.T) {
	src := filepath.Join("..", "cases", "case-prestart-repair-aborted.md")
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Skip("eval case not present")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, filepath.Base(src)), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	task := investigationTask()
	store := tool.NewInMemoryInvestigationStore()
	steps := []model.ToolStep{
		{Used: true, ToolCallID: "c1", ToolName: tool.InvestigationToolName, Arguments: map[string]any{"action": "set_symptom", "symptom": "vm 410233 预启动失败 prestart game launch failed exit_code=1"}},
		{Used: true, ToolCallID: "c2", ToolName: "vm_run_cmd", Arguments: map[string]any{"vm": "bad", "cmd": "dir"}},
		{Used: true, ToolCallID: "c3", ToolName: "vm_run_cmd", Arguments: map[string]any{"vm": "good", "cmd": "dir"}},
	}
	res := runLiveTask(task, buildMockRegistry(), &scriptedModel{steps: steps, final: "对照正常机无 repair.flag，根因为修复中断。"},
		liveOptions{Ledger: true, LedgerStore: store, Cases: cases.NewFileStore(dir)})
	if !res.Passed {
		t.Fatalf("expected pass, got %+v", res)
	}
	if got := store.Get("eval-" + task.ID).RecalledCases; len(got) != 1 || got[0] != "case-prestart-repair-aborted" {
		t.Fatalf("confirmed case must be recalled by set_symptom: %v", got)
	}
}

func TestInvestigationTasksFileLoads(t *testing.T) {
	path := filepath.Join("..", "tasks", "investigation.jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Skip("tasks file not present")
	}
	tasks, err := LoadTasks(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(tasks) < 7 {
		t.Fatalf("want >=7 investigation tasks, got %d", len(tasks))
	}
	distractors := 0
	for _, task := range tasks {
		if len(task.Expect.ForbidRoot) > 0 && task.Expect.ExpectOnset != nil && len(task.Expect.ExpectKinds) > 0 {
			distractors++
		}
	}
	if distractors < 3 {
		t.Fatalf("want >=3 tasks with mechanism distractors and ledger expectations, got %d", distractors)
	}
	for _, task := range tasks {
		if task.Category != "investigation" || !strings.HasPrefix(task.ID, "investigation-") {
			t.Errorf("unexpected task %s/%s", task.ID, task.Category)
		}
	}
}
