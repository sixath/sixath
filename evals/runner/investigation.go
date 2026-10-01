package main

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/tool"
)

const fixtureMiss = `{"ok":true,"hits":[],"note":"no matching data for these arguments"}`

// fixtureRecorder 记录 fixture 命中情况，用于判定是否查过对照组。
type fixtureRecorder struct {
	mu            sync.Mutex
	contrastHits  int
	fixtureHits   int
	fixtureMisses int
}

func (r *fixtureRecorder) hit(f ToolFixture) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fixtureHits++
	if f.Contrast {
		r.contrastHits++
	}
}

func (r *fixtureRecorder) miss() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fixtureMisses++
}

// buildFixtureRegistry 在 mock 工具集之上注册 fixture 工具；同名时 fixture 优先。
func buildFixtureRegistry(fixtures []ToolFixture, rec *fixtureRecorder) *tool.Registry {
	reg := tool.NewRegistry()
	byTool := map[string][]ToolFixture{}
	var order []string
	for _, f := range fixtures {
		if _, ok := byTool[f.Tool]; !ok {
			order = append(order, f.Tool)
		}
		byTool[f.Tool] = append(byTool[f.Tool], f)
	}
	for _, name := range order {
		list := byTool[name]
		_ = reg.Register(tool.Tool{
			Name:        name,
			Description: "fixture " + name,
			Effect:      tool.EffectRead,
			Execute: func(_ context.Context, args map[string]any) (any, error) {
				raw, _ := json.Marshal(args)
				if f, ok := matchFixture(list, string(raw)); ok {
					rec.hit(f)
					return f.Response, nil
				}
				rec.miss()
				return fixtureMiss, nil
			},
		})
	}
	for _, n := range mockToolNames {
		if _, exists := byTool[n]; exists {
			continue
		}
		name := n
		_ = reg.Register(tool.Tool{
			Name:        name,
			Description: "mock " + name,
			Execute: func(_ context.Context, _ map[string]any) (any, error) {
				return "ok", nil
			},
		})
	}
	return reg
}

func matchFixture(list []ToolFixture, argsJSON string) (ToolFixture, bool) {
	lower := strings.ToLower(argsJSON)
	for _, f := range list {
		if strings.TrimSpace(f.Match) == "" || containsAny(lower, f.Match) {
			return f, true
		}
	}
	return ToolFixture{}, false
}

// containsAny 判断 text（已小写）是否包含 alternatives 中任一项（| 分隔，忽略大小写）。
func containsAny(text, alternatives string) bool {
	for _, alt := range strings.Split(alternatives, "|") {
		alt = strings.ToLower(strings.TrimSpace(alt))
		if alt != "" && strings.Contains(text, alt) {
			return true
		}
	}
	return false
}

// investigationEvidence 是判定调查类任务所需的运行事实。
type investigationEvidence struct {
	Output       string
	ToolCalls    int
	ContrastHits int
}

// evaluateInvestigation 按「未提前收尾 → 有足够取证 → 查过对照 → 结论命中根因」顺序判定，
// 失败原因对应不同的改进方向（harness / 方法 / 工具 / 模型）。
func evaluateInvestigation(task Task, ev investigationEvidence) (bool, string) {
	for _, p := range task.Expect.ForbidOutput {
		if re, err := regexp.Compile(p); err == nil && re.MatchString(ev.Output) {
			return false, "premature_stop"
		}
	}
	if task.Expect.MinToolCalls > 0 && ev.ToolCalls < task.Expect.MinToolCalls {
		return false, "too_few_tool_calls"
	}
	if task.Expect.MustContrast && ev.ContrastHits == 0 {
		return false, "no_contrast"
	}
	if stoppedAtMechanism(task, ev.Output) {
		return false, "stopped_at_mechanism"
	}
	lower := strings.ToLower(ev.Output)
	for _, kw := range task.Expect.RootCause {
		if !containsAny(lower, kw) {
			return false, "missing_root_cause"
		}
	}
	return true, ""
}

var rootSentenceSplit = regexp.MustCompile(`[。\n；;]`)

var rootSentenceMarker = regexp.MustCompile(`(?i)根因|根本原因|root cause|直接原因|原因是|原因为`)

// stoppedAtMechanism 结论中点名根因的句子命中 forbid_root（内部机制），且这些句子都没有命中 root_cause。
func stoppedAtMechanism(task Task, output string) bool {
	if len(task.Expect.ForbidRoot) == 0 {
		return false
	}
	var rootSentences []string
	for _, s := range rootSentenceSplit.Split(output, -1) {
		// Markdown 标题只是章节名（如「限流过载根因分析」），不是结论句。
		if strings.HasPrefix(strings.TrimSpace(s), "#") {
			continue
		}
		if rootSentenceMarker.MatchString(s) {
			rootSentences = append(rootSentences, strings.ToLower(s))
		}
	}
	if len(rootSentences) == 0 {
		return false
	}
	namesMechanism := false
	for _, s := range rootSentences {
		for _, kw := range task.Expect.ForbidRoot {
			namesMechanism = namesMechanism || containsAny(s, kw)
		}
		allRoot := len(task.Expect.RootCause) > 0
		for _, kw := range task.Expect.RootCause {
			allRoot = allRoot && containsAny(s, kw)
		}
		if allRoot {
			return false
		}
	}
	return namesMechanism
}

// ledgerQuality 台账质量：每项检查独立计分，Score 为通过项占比。
type ledgerQuality struct {
	Score  float64
	Checks map[string]bool
}

// scoreLedger 评估台账是否把调查做完整：onset 有原文且落在期望窗口、因果链到达 root、期望的层级都在。
func scoreLedger(task Task, l tool.InvestigationLedger) ledgerQuality {
	checks := map[string]bool{}
	used := strings.TrimSpace(l.Symptom) != "" || strings.TrimSpace(l.Onset) != "" || len(l.Hypotheses) > 0
	checks["ledger_used"] = used
	checks["onset_verified"] = l.OnsetEvidence != nil && l.OnsetEvidence.Verified &&
		(strings.TrimSpace(l.LastGood) == "" || (l.LastGoodEvidence != nil && l.LastGoodEvidence.Verified))
	if w := task.Expect.ExpectOnset; w != nil {
		ok := false
		if after, before, err := w.bounds(); err == nil {
			if t, parsed := parseEvalTime(l.Onset); parsed {
				ok = !t.Before(after) && !t.After(before)
			}
		}
		checks["onset_in_window"] = ok
	}
	reachesRoot := false
	for _, h := range l.Hypotheses {
		if h.Status == tool.HypothesisAccepted && h.Kind == tool.HypothesisKindRoot {
			reachesRoot = true
		}
	}
	checks["chain_reaches_root"] = reachesRoot
	for _, k := range task.Expect.ExpectKinds {
		found := false
		for _, h := range l.Hypotheses {
			if h.Kind == k && h.Status != tool.HypothesisRejected {
				found = true
			}
		}
		checks["kind_"+k] = found
	}
	passed := 0
	for _, ok := range checks {
		if ok {
			passed++
		}
	}
	return ledgerQuality{Score: float64(passed) / float64(len(checks)), Checks: checks}
}

// criticRounds 统计本次运行被结案审查打回的次数。
func criticRounds(continues []string) int {
	n := 0
	for _, id := range continues {
		if strings.HasPrefix(id, agent.CriticRulePrefix) {
			n++
		}
	}
	return n
}

// investigationAttribution 把失败原因映射到改进方向，便于汇总时看出短板在哪一层。
func investigationAttribution(reason string) string {
	switch reason {
	case "premature_stop":
		return "harness"
	case "too_few_tool_calls", "no_contrast", "stopped_at_mechanism":
		return "prompt"
	default:
		return "model"
	}
}
