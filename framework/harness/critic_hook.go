package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

// CriticRulePrefix 为结案审查继续时的 RuleID 前缀，同时是它独立预算的计数键。
const CriticRulePrefix = "critic"

const (
	criticRuleIDRules = CriticRulePrefix + ":rules"
	criticRuleIDModel = CriticRulePrefix + ":model"

	defaultCriticMinToolCalls = 6
	defaultCriticMaxRounds    = 2

	criticArgRunes    = 200
	criticResultRunes = 400
	criticLedgerRunes = 6000
	criticMaxCalls    = 60
)

// 模型级审查的问题类型。
const (
	CriticIssueStoppedAtMechanism = "stopped_at_mechanism"
	CriticIssueUnsupportedClaim   = "unsupported_claim"
	CriticIssueMisreadOutput      = "misread_output"
	CriticIssueWeakContrast       = "weak_contrast"
	CriticIssueOnsetNotFirst      = "onset_not_first"
	CriticIssueUnexplainedChange  = "unexplained_change"
	CriticIssueFixTreatsSymptom   = "fix_treats_symptom"
	CriticIssuePriorCaseIgnored   = "prior_case_ignored"
	CriticIssueCaseNotProposed    = "case_not_proposed"
	CriticIssueMissingProducer    = "missing_producer_evidence"
	CriticIssueUnexplainedForm    = "unexplained_form_change"
	CriticIssueUnverifiedCode     = "unverified_code_meaning"
)

var criticIssueTypes = map[string]bool{
	CriticIssueStoppedAtMechanism: true, CriticIssueUnsupportedClaim: true, CriticIssueMisreadOutput: true,
	CriticIssueWeakContrast: true, CriticIssueOnsetNotFirst: true, CriticIssueUnexplainedChange: true,
	CriticIssueFixTreatsSymptom: true, CriticIssueMissingProducer: true, CriticIssueUnexplainedForm: true,
	CriticIssueUnverifiedCode: true,
}

// criticRuleIDCase 为"提醒沉淀案例"单独的 RuleID：只在没有其它问题时提醒，且整个回合只提醒一次。
const criticRuleIDCase = CriticRulePrefix + ":case"

// CriticConfig 是 hooks.yaml 的 critic 段。
//
//	critic:
//	  enabled: true
//	  model: ""          # 审查模型名，由装配方解析；为空复用当前 agent 的模型
//	  min_tool_calls: 6  # 少于该工具调用数的回合不审查（闲聊、简单问答）
//	  max_rounds: 2      # 审查打回的轮次上限，独立于 max_stop_nudges
type CriticConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Model        string `yaml:"model"`
	MinToolCalls int    `yaml:"min_tool_calls"`
	MaxRounds    int    `yaml:"max_rounds"`
}

// CriticIssue 是审查指出的一处问题。
type CriticIssue struct {
	Type      string `json:"type"`
	Detail    string `json:"detail"`
	NextProbe string `json:"next_probe,omitempty"`
}

// CriticVerdict 是一次审查的结论。
type CriticVerdict struct {
	Verdict string        `json:"verdict"`
	Issues  []CriticIssue `json:"issues,omitempty"`
}

// CriticHook 在排查类回合准备结案时做两级审查：
// 规则级（台账缺口、引用失败调用、未核对证据）零成本先行；规则通过后，配置了模型时再做模型级复核。
// 两级都打回时让模型带着问题清单继续；轮次预算独立（BudgetedStopHook）。
type CriticHook struct {
	cfg     CriticConfig
	ledgers tool.InvestigationStore
	model   model.Model
}

// NewCriticHook 构造结案审查；m 为 nil 时只做规则级审查。
func NewCriticHook(cfg CriticConfig, ledgers tool.InvestigationStore, m model.Model) *CriticHook {
	if cfg.MinToolCalls <= 0 {
		cfg.MinToolCalls = defaultCriticMinToolCalls
	}
	if cfg.MaxRounds <= 0 {
		cfg.MaxRounds = defaultCriticMaxRounds
	}
	return &CriticHook{cfg: cfg, ledgers: ledgers, model: m}
}

func (c *CriticHook) StopBudget() (string, int) { return CriticRulePrefix, c.cfg.MaxRounds }

func (c *CriticHook) OnStop(ctx context.Context, in StopHookInput) StopDecision {
	if c == nil || strings.TrimSpace(in.Text) == "" {
		return StopDecision{}
	}
	// 只审查"调查"：用过台账，或工具调用达到 min_tool_calls；闲聊和简单查询不打扰。
	if len(in.ToolCalls) < c.cfg.MinToolCalls && !usedToolSuccessfully(in.ToolCalls, tool.InvestigationToolName) {
		return StopDecision{}
	}
	round := countPrefix(in.Continues, CriticRulePrefix) + 1
	if issues := c.ruleIssues(ctx, in); len(issues) > 0 {
		return c.revise(criticRuleIDRules, "rules", round, issues)
	}
	if is := c.caseReminder(ctx, in); is != nil {
		return c.revise(criticRuleIDCase, "rules", round, []CriticIssue{*is})
	}
	if c.model == nil {
		return StopDecision{Event: criticEvent("rules", CriticVerdict{Verdict: "pass"}, round, "")}
	}
	v, err := c.modelVerdict(ctx, in)
	if err != nil {
		return StopDecision{Event: criticEvent("model", CriticVerdict{Verdict: "pass"}, round, err.Error())}
	}
	if v.Verdict != "revise" || len(v.Issues) == 0 {
		return StopDecision{Event: criticEvent("model", CriticVerdict{Verdict: "pass"}, round, "")}
	}
	return c.revise(criticRuleIDModel, "model", round, v.Issues)
}

func (c *CriticHook) revise(ruleID, tier string, round int, issues []CriticIssue) StopDecision {
	var b strings.Builder
	b.WriteString("【结案审查未通过】结论还不能交付，请按下列问题继续取证后再给出结论：\n")
	for i, is := range issues {
		fmt.Fprintf(&b, "%d. [%s] %s", i+1, is.Type, is.Detail)
		if strings.TrimSpace(is.NextProbe) != "" {
			fmt.Fprintf(&b, "\n   下一步：%s", is.NextProbe)
		}
		b.WriteString("\n")
	}
	b.WriteString("如果某条确实无法取证，在结论中明确说明原因与不确定性，不要用推测填补。")
	return StopDecision{
		Continue: true,
		RuleID:   ruleID,
		Message:  b.String(),
		Event:    criticEvent(tier, CriticVerdict{Verdict: "revise", Issues: issues}, round, ""),
	}
}

func criticEvent(tier string, v CriticVerdict, round int, errMsg string) *StopHookEvent {
	issues := make([]map[string]any, 0, len(v.Issues))
	for _, is := range v.Issues {
		issues = append(issues, map[string]any{"type": is.Type, "detail": is.Detail, "next_probe": is.NextProbe})
	}
	p := map[string]any{"tier": tier, "verdict": v.Verdict, "issues": issues, "round": round}
	if errMsg != "" {
		p["error"] = errMsg
	}
	return &StopHookEvent{Kind: events.CriticVerdict, Payload: p}
}

// ruleIssues 为规则级审查：只检查可机械判定的问题，避免误伤。
func (c *CriticHook) ruleIssues(ctx context.Context, in StopHookInput) []CriticIssue {
	var issues []CriticIssue
	issues = append(issues, failedCallCitations(in.Text, in.ToolCalls)...)
	issues = append(issues, unverifiedCodeMeanings(in.Text, in.ToolCalls)...)
	if c.ledgers == nil || !usedToolSuccessfully(in.ToolCalls, tool.InvestigationToolName) {
		return issues
	}
	sessionID, _ := ctx.Value(tool.ContextKeySessionID).(string)
	if sessionID == "" {
		return issues
	}
	l := c.ledgers.Get(sessionID)
	if l.OnsetMissing() || l.OnsetUnbounded() || l.OnsetUnverified() {
		issues = append(issues, CriticIssue{
			Type:      CriticIssueOnsetNotFirst,
			Detail:    "问题开始时间（onset）与最后一次正常（last_good）没有同时被工具原文支撑",
			NextProbe: "放宽时间窗（数天）找第一次失败和它之前最后一次成功，用 set_onset 附 onset_quote / last_good_quote",
		})
	}
	if hyps := l.MechanismOnly(); len(hyps) > 0 {
		issues = append(issues, CriticIssue{
			Type:      CriticIssueStoppedAtMechanism,
			Detail:    fmt.Sprintf("已采纳的 %s 不是系统之外的状态变化（root），结论停在了机制层", hypIDs(hyps)),
			NextProbe: "追问它为什么在 onset 开始：查 last_good 与 onset 之间的部署/配置/文件修改/进程中断/依赖变化",
		})
	}
	for _, h := range l.AcceptedWithoutChange() {
		issues = append(issues, CriticIssue{
			Type:      CriticIssueUnexplainedChange,
			Detail:    fmt.Sprintf("已采纳的 %s 没有说明 last_good 到 onset 之间发生了什么变化", h.ID),
			NextProbe: "在故障组件附近按修改时间倒序查看目录/配置/变更记录，找到变化后用 change=true 记录",
		})
	}
	for _, h := range l.Hypotheses {
		if h.Status == tool.HypothesisAccepted && len(h.Evidence) > 0 && !anyVerified(h.Evidence) && observationsRecorded(c.ledgers, sessionID) {
			issues = append(issues, CriticIssue{
				Type:   CriticIssueUnsupportedClaim,
				Detail: fmt.Sprintf("已采纳的 %s 的证据都没有在工具输出中核对到原文", h.ID),
			})
		}
	}
	if ids := l.IgnoredCases(); len(ids) > 0 {
		issues = append(issues, CriticIssue{
			Type:      CriticIssuePriorCaseIgnored,
			Detail:    fmt.Sprintf("召回了相似的已确认案例 %s，但没有验证或排除它", strings.Join(ids, "、")),
			NextProbe: "对本目标执行案例里的 verify_probes，add_hypothesis from_case=<案例ID>，再根据结果 accept 或 reject",
		})
	}
	for _, h := range l.AbsenceWithoutProducer() {
		issues = append(issues, CriticIssue{
			Type:      CriticIssueMissingProducer,
			Detail:    fmt.Sprintf("已采纳的 %s 说「%s」没有出现，但没有查看负责产生它的组件自己的日志/状态，原因是推测的", h.ID, h.Absence),
			NextProbe: "找到产生该信号的组件（谁写这个文件/打这行日志/发这个回调），读它自己的日志或状态目录，用 producer=true 记录证据",
		})
	}
	for _, h := range rootChangesAfterOnset(l) {
		issues = append(issues, CriticIssue{
			Type:      CriticIssueUnexplainedChange,
			Detail:    fmt.Sprintf("已采纳的根因 %s 的变化证据都发生在问题开始（%s）之后，不能解释问题为什么从那时开始；它最多解释之后的现象", h.ID, l.Onset),
			NextProbe: "在 last_good 与 onset 之间找更早的变化（补丁/修复、部署、配置、重启、中断），把晚于 onset 的变化改记为 trigger 或另一条假设",
		})
	}
	for _, fc := range unexplainedFormChanges(l, in.ToolCalls) {
		issues = append(issues, CriticIssue{
			Type:      CriticIssueUnexplainedForm,
			Detail:    fmt.Sprintf("问题开始之后失败形式在 %s 又变了（%s），已采纳的结论没有用证据解释这次变化", fc.Time.UTC().Format(time.RFC3339), fc.Detail),
			NextProbe: "查这个时间点附近的配置/参数/运维操作：timeline change_sources，或 field_history 跟踪相关参数的取值，找到后作为 change=true 证据记录",
		})
	}
	return issues
}

// criticOnsetSlack 容忍同一分钟内的先后（日志精度、批量写入）。
const criticOnsetSlack = time.Minute

// rootChangesAfterOnset 返回已采纳的 root 假设中，带时间的变化证据全部晚于 onset 的那些。
// onset 与证据时间都取自日志原文，按相同方式解析。
func rootChangesAfterOnset(l tool.InvestigationLedger) []tool.Hypothesis {
	onset, ok := time.Time{}, false
	if l.OnsetEvidence != nil {
		onset, ok = tool.QuoteTime(l.OnsetEvidence.Quote)
	}
	if !ok {
		onset, ok = tool.QuoteTime(l.Onset)
	}
	if !ok {
		return nil
	}
	var out []tool.Hypothesis
	for _, h := range l.Hypotheses {
		if h.Status != tool.HypothesisAccepted || h.Kind != tool.HypothesisKindRoot {
			continue
		}
		dated, before := 0, false
		for _, e := range h.Evidence {
			if !e.Change {
				continue
			}
			if t, ok := tool.QuoteTime(e.Quote); ok {
				dated++
				before = before || !t.After(onset.Add(criticOnsetSlack))
			}
		}
		if dated > 0 && !before {
			out = append(out, h)
		}
	}
	return out
}

const (
	criticMaxFormIssues = 2
	criticMinMatchRunes = 8
)

// unexplainedFormChanges 返回 timeline 发现的 onset 之后的失败形式变化中，
// 附近的变更记录（changes_near / field_history 取值变化）都没有被任何已采纳假设引为证据的那些。
func unexplainedFormChanges(l tool.InvestigationLedger, calls []ToolCallRecord) []tool.FormChange {
	var quotes []string
	for _, h := range l.Hypotheses {
		if h.Status != tool.HypothesisAccepted {
			continue
		}
		for _, e := range h.Evidence {
			if q := strings.TrimSpace(e.Quote); utf8.RuneCountInString(q) >= criticMinMatchRunes {
				quotes = append(quotes, q)
			}
		}
	}
	if len(quotes) == 0 {
		return nil
	}
	var transitions []tool.FieldTransition
	var forms []tool.FormChange
	for _, c := range calls {
		if callFailed(c) {
			continue
		}
		switch c.ToolName {
		case tool.FieldHistoryToolName:
			transitions = append(transitions, tool.FieldHistoryTransitions(c.Result)...)
		case tool.TimelineToolName:
			forms = append(forms, tool.TimelineFormChanges(c.Result)...)
		}
	}
	cited := func(texts []string) bool {
		for _, t := range texts {
			for _, q := range quotes {
				if strings.Contains(t, q) || (utf8.RuneCountInString(t) >= criticMinMatchRunes && strings.Contains(q, t)) {
					return true
				}
			}
		}
		return false
	}
	seen := map[time.Time]bool{}
	var out []tool.FormChange
	for _, fc := range forms {
		if seen[fc.Time] {
			continue
		}
		seen[fc.Time] = true
		texts := append([]string(nil), fc.Changes...)
		for _, tr := range transitions {
			d := tr.Time.Sub(fc.Time)
			if d < 0 {
				d = -d
			}
			if d <= fc.Margin {
				texts = append(texts, tr.Quotes...)
			}
		}
		if !cited(texts) {
			out = append(out, fc)
		}
		if len(out) >= criticMaxFormIssues {
			break
		}
	}
	return out
}

// caseReminder 在结论已追到 root、可用案例库但尚未生成草稿时提醒一次。
func (c *CriticHook) caseReminder(ctx context.Context, in StopHookInput) *CriticIssue {
	if c.ledgers == nil || containsString(in.Continues, criticRuleIDCase) {
		return nil
	}
	sessionID, _ := ctx.Value(tool.ContextKeySessionID).(string)
	if sessionID == "" {
		return nil
	}
	l := c.ledgers.Get(sessionID)
	if !l.CasesEnabled || l.CaseProposed != "" || !l.HasAcceptedRoot() {
		return nil
	}
	return &CriticIssue{
		Type:      CriticIssueCaseNotProposed,
		Detail:    "根因已确认，但还没有把本次调查沉淀为案例",
		NextProbe: "调用 case_library action=propose（附 signature 与针对根因的 fix），草稿经用户确认后才会用于以后的调查；然后给出最终结论",
	}
}

var (
	criticCodeNumber = regexp.MustCompile(`(?i)(错误码|返回码|状态码|退出码|结果码|原因码|error[ _]?code|exit[ _]?code|return[ _]?code|status[ _]?code|errno|code)[\s=:：|*]*(?:为|是)?[\s*]*[（(\[]?\s*(0x[0-9a-f]+|-?\d{2,})`)
	// criticCodeInterprets 命中时说明结论在解释该数字的含义，而不只是引用它。
	criticCodeInterprets = regexp.MustCompile(`(?i)(表示|代表|意味|含义|意思|即|对应|说明|指|means?|indicates?|stands? for)`)
	// criticCodeGloss 为紧跟在数字后的括号释义，如「1603 (安装程序初始化失败)」。
	criticCodeGloss = regexp.MustCompile(`^[\s*]*[（(][^）)0-9]{2,}[）)]`)
	criticCodeReaders    = map[string]bool{
		"rca_grep": true, "rca_read": true, "rca_symbol": true, "rca_glob": true,
		"read_file": true, "search_files": true, "web_search": true, "web_extract": true, "read_skill_file": true,
	}
	criticSourceCommand = regexp.MustCompile(`(?i)(\.(go|py|java|cs|c|cc|cpp|h|hpp|js|ts|rs|md|txt|conf|ini|ya?ml|xml|ps1|sh|bat|cmd)\b|findstr|select-string|\bgrep\b|\btype\b|\bcat\b)`)
	criticMaxCodeIssues = 2
)

// unverifiedCodeMeanings 找出结论里被解释了含义、但调查中没有任何读代码/文档的调用输出过的数字码。
func unverifiedCodeMeanings(text string, calls []ToolCallRecord) []CriticIssue {
	var issues []CriticIssue
	seen := map[string]bool{}
	for _, m := range criticCodeNumber.FindAllStringSubmatchIndex(text, -1) {
		num := text[m[4]:m[5]]
		if seen[num] {
			continue
		}
		if !criticCodeGloss.MatchString(text[m[1]:]) && !criticCodeInterprets.MatchString(enclosingSentence(text, m[0], m[1])) {
			continue
		}
		seen[num] = true
		if codeReadInCalls(num, calls) {
			continue
		}
		issues = append(issues, CriticIssue{
			Type:      CriticIssueUnverifiedCode,
			Detail:    fmt.Sprintf("结论解释了 %s 的含义，但调查过程中没有读过定义它的代码或文档", text[m[0]:m[1]]),
			NextProbe: fmt.Sprintf("在相关组件的源码/文档里搜索 %s（rca_grep / search_files），读定义处确认含义后再下结论", num),
		})
		if len(issues) >= criticMaxCodeIssues {
			break
		}
	}
	return issues
}

func codeReadInCalls(num string, calls []ToolCallRecord) bool {
	word := regexp.MustCompile(`(^|[^0-9A-Za-z])` + regexp.QuoteMeta(num) + `($|[^0-9A-Za-z])`)
	for _, c := range calls {
		if callFailed(c) {
			continue
		}
		if !criticCodeReaders[c.ToolName] {
			raw, _ := json.Marshal(c.Arguments)
			if !criticSourceCommand.Match(raw) {
				continue
			}
		}
		if word.MatchString(tool.ObservationText(c.Result)) {
			return true
		}
	}
	return false
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func anyVerified(ev []tool.EvidenceLink) bool {
	for _, e := range ev {
		if e.Verified {
			return true
		}
	}
	return false
}

func observationsRecorded(store tool.InvestigationStore, sessionID string) bool {
	_, _, recorded := store.FindQuote(sessionID, "", "\x00")
	return recorded
}

func hypIDs(hs []tool.Hypothesis) string {
	ids := make([]string, 0, len(hs))
	for _, h := range hs {
		ids = append(ids, h.ID)
	}
	return strings.Join(ids, "、")
}

// criticProbeArgKeys 为探测类工具里标识"查了什么"的参数；结论提到这些值而对应调用失败/超时，即引用了无效结果。
var criticProbeArgKeys = []string{"path", "pattern", "patterns", "query", "cmd", "command", "file", "keyword", "grep"}

// criticAcknowledgesFailure 命中时说明结论已如实交代该探测失败，不算误读。
var criticAcknowledgesFailure = regexp.MustCompile(`(?i)(超时|失败|未能|无法|报错|出错|没有返回|未返回|timed? ?out|failed|error)`)

// failedCallCitations 找出结论中提到、但对应调用失败或超时且之后没有成功重试的探测对象。
func failedCallCitations(text string, calls []ToolCallRecord) []CriticIssue {
	succeeded := map[string]bool{}
	for _, c := range calls {
		if !callFailed(c) {
			for _, v := range probeArgValues(c.Arguments) {
				succeeded[c.ToolName+"\x00"+v] = true
			}
		}
	}
	seen := map[string]bool{}
	var issues []CriticIssue
	for _, c := range calls {
		if !callFailed(c) {
			continue
		}
		for _, v := range probeArgValues(c.Arguments) {
			key := c.ToolName + "\x00" + v
			if succeeded[key] || seen[key] {
				continue
			}
			idx := strings.Index(text, v)
			if idx < 0 || criticAcknowledgesFailure.MatchString(enclosingSentence(text, idx, idx+len(v))) {
				continue
			}
			seen[key] = true
			issues = append(issues, CriticIssue{
				Type:      CriticIssueMisreadOutput,
				Detail:    fmt.Sprintf("结论引用了 %s 对「%s」的结果，但该调用失败或超时（%s），没有有效输出，不能当作「没有/不存在」的证据", c.ToolName, v, failureReason(c)),
				NextProbe: "缩小范围（更窄的目录/文件/时间窗）重跑该探测，拿到有效输出后再下结论",
			})
		}
	}
	return issues
}

func callFailed(c ToolCallRecord) bool {
	if !c.Allowed || strings.TrimSpace(c.Error) != "" {
		return true
	}
	m, ok := c.Result.(map[string]any)
	if !ok {
		return false
	}
	if v, ok := m["ok"].(bool); ok && !v {
		return true
	}
	t, _ := m["timed_out"].(bool)
	return t
}

func failureReason(c ToolCallRecord) string {
	if strings.TrimSpace(c.Error) != "" {
		return truncRunes(c.Error, 80)
	}
	if m, ok := c.Result.(map[string]any); ok {
		if t, _ := m["timed_out"].(bool); t {
			return "timed_out"
		}
		if e, ok := m["error"].(string); ok && e != "" {
			return truncRunes(e, 80)
		}
	}
	return "ok:false"
}

func probeArgValues(args map[string]any) []string {
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if utf8.RuneCountInString(s) >= 4 {
			out = append(out, s)
		}
	}
	for _, k := range criticProbeArgKeys {
		switch v := args[k].(type) {
		case string:
			add(v)
		case []any:
			for _, x := range v {
				if s, ok := x.(string); ok {
					add(s)
				}
			}
		case []string:
			for _, s := range v {
				add(s)
			}
		}
	}
	return out
}

const criticSystemPrompt = `你是排障结论的审查员。你会拿到用户问题、调查过程（工具调用及结果摘要）、调查台账和结论草稿。
只审查，不重新调查。逐条检查以下问题，只报告有明确依据的问题：
- stopped_at_mechanism：根因停在系统自身的保护/重试/降级/计数器/熔断等机制上，没有追到系统之外的状态变化（部署、配置或文件被改、进程中断、依赖到期、资源耗尽、人为操作）。
- unsupported_claim：结论中的关键断言在工具结果里找不到依据（包括编造的日志、文件、时间、数字）。
- misread_output：把失败/超时/截断/空结果当成了"没有"或"正常"，或读错了输出含义。
- weak_contrast：对照比较的是症状本身（坏对象当然报错），而不是能区分正常与故障的那一层（同一配置/文件/版本/参数在两边是否不同）；或对照两边其实相同。
- onset_not_first：认定的开始时间只是读到的最早一条，没有证明之前是正常的（缺少 last_good），或时间窗太短。
- unexplained_change：没有说明最后一次正常到第一次失败之间发生了什么变化。
- fix_treats_symptom：建议的处置只消除症状（重启、清标志、重试），没有针对根因。
- missing_producer_evidence：结论说某个信号（文件、日志行、回调、就绪标记）没有出现，却只看了等待它的一方，没有查看负责产生它的组件自己的日志/状态，就推测了原因。
- unexplained_form_change：问题时间窗内失败形式变了（报错不同、超时时长不同、从失败回调变成卡住），结论却说"根因相同"而没有解释这次变化（配置/参数/操作变更）。
- unverified_code_meaning：结论把某个数字解释为错误码/状态码/含义，但调查过程中没有读过定义它的代码或文档。
没有问题时 verdict=pass。有问题时 verdict=revise，每条给出 type、detail（具体指出哪句结论/哪次调用）、next_probe（下一步应执行的具体查询）。
只输出一个 JSON 对象，不要输出其他内容：{"verdict":"pass|revise","issues":[{"type":"...","detail":"...","next_probe":"..."}]}`

func (c *CriticHook) modelVerdict(ctx context.Context, in StopHookInput) (CriticVerdict, error) {
	var b strings.Builder
	if q := lastUserQuestion(in.Messages); q != "" {
		fmt.Fprintf(&b, "## 用户问题\n%s\n\n", truncRunes(q, 2000))
	}
	b.WriteString("## 调查过程\n")
	calls := in.ToolCalls
	if len(calls) > criticMaxCalls {
		calls = calls[len(calls)-criticMaxCalls:]
	}
	for i, call := range calls {
		status := "ok"
		if callFailed(call) {
			status = "FAILED(" + failureReason(call) + ")"
		}
		args, _ := json.Marshal(call.Arguments)
		fmt.Fprintf(&b, "%d. %s %s -> %s\n   %s\n", i+1, call.ToolName, truncRunes(string(args), criticArgRunes), status,
			truncRunes(strings.Join(strings.Fields(tool.ObservationText(call.Result)), " "), criticResultRunes))
	}
	if c.ledgers != nil {
		if sessionID, _ := ctx.Value(tool.ContextKeySessionID).(string); sessionID != "" {
			if raw, err := json.Marshal(c.ledgers.Get(sessionID)); err == nil && string(raw) != "{}" {
				fmt.Fprintf(&b, "\n## 调查台账\n%s\n", truncRunes(string(raw), criticLedgerRunes))
			}
		}
	}
	fmt.Fprintf(&b, "\n## 结论草稿\n%s\n", truncRunes(in.Text, 6000))
	gen, err := c.model.Chat(ctx, []model.Message{
		{Role: "system", Content: criticSystemPrompt},
		{Role: "user", Content: b.String()},
	})
	if err != nil {
		return CriticVerdict{}, err
	}
	if gen == nil {
		return CriticVerdict{}, fmt.Errorf("critic: empty generation")
	}
	return parseCriticVerdict(gen.Text)
}

// parseCriticVerdict 从模型输出中取第一个 JSON 对象；未知问题类型丢弃，无有效问题视为 pass。
func parseCriticVerdict(text string) (CriticVerdict, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return CriticVerdict{}, fmt.Errorf("critic: no JSON object in output")
	}
	var v CriticVerdict
	if err := json.Unmarshal([]byte(text[start:end+1]), &v); err != nil {
		return CriticVerdict{}, fmt.Errorf("critic: parse verdict: %w", err)
	}
	v.Verdict = strings.ToLower(strings.TrimSpace(v.Verdict))
	kept := v.Issues[:0]
	for _, is := range v.Issues {
		is.Type = strings.TrimSpace(is.Type)
		if criticIssueTypes[is.Type] && strings.TrimSpace(is.Detail) != "" {
			kept = append(kept, is)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return criticIssueRank(kept[i].Type) < criticIssueRank(kept[j].Type) })
	v.Issues = kept
	if len(v.Issues) == 0 {
		v.Verdict = "pass"
	}
	return v, nil
}

// criticIssueRank 让最影响结论方向的问题排在前面。
func criticIssueRank(t string) int {
	switch t {
	case CriticIssueMisreadOutput, CriticIssueUnsupportedClaim, CriticIssueUnverifiedCode:
		return 0
	case CriticIssueStoppedAtMechanism, CriticIssueOnsetNotFirst, CriticIssueUnexplainedChange,
		CriticIssuePriorCaseIgnored, CriticIssueMissingProducer, CriticIssueUnexplainedForm:
		return 1
	default:
		return 2
	}
}

func lastUserQuestion(msgs []model.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if !strings.EqualFold(m.Role, "user") || strings.TrimSpace(m.Content) == "" {
			continue
		}
		if origin, _ := m.Metadata[model.MetadataKeySixathOrigin].(string); origin != "" {
			continue
		}
		return m.Content
	}
	return ""
}

func truncRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}
