package harness

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/sixath/framework/tool"
	"gopkg.in/yaml.v3"
)

// defaultStopTailRunes 为 stop_rules 匹配回复末尾的默认字符数。
const defaultStopTailRunes = 200

// openTodosPlaceholder 在 message 中会被替换为未完成的 todo 列表。
const openTodosPlaceholder = "{{open_todos}}"

// investigationGapsPlaceholder 在 message 中会被替换为台账缺口说明。
const investigationGapsPlaceholder = "{{investigation_gaps}}"

// 台账缺口类型（when_investigation_gaps 可选值）。
const (
	GapOnsetMissing   = "onset_missing"
	GapOpenHypotheses = "open_hypotheses"
	GapLedgerUnused   = "ledger_unused"
	// GapOnsetUnbounded 给了开始时间但没有上次正常的时间点。
	GapOnsetUnbounded = "onset_unbounded"
	// GapAcceptedWithoutChange 已采纳的假设没有变化证据。
	GapAcceptedWithoutChange = "accepted_without_change"
	// GapMechanismOnly 已采纳的假设都不是 root（系统之外的变化），也未说明无法追到根因的原因。
	GapMechanismOnly = "mechanism_only"
	// GapOnsetUnverified onset / last_good 没有工具输出原文支撑。
	GapOnsetUnverified = "onset_unverified"
)

var knownInvestigationGaps = map[string]bool{
	GapOnsetMissing: true, GapOpenHypotheses: true, GapLedgerUnused: true,
	GapOnsetUnbounded: true, GapAcceptedWithoutChange: true,
	GapMechanismOnly: true, GapOnsetUnverified: true,
}

// HarnessStopRule 是一条声明式 stop 规则：条件全部满足时让模型带着 Message 继续。
//
//	stop_rules:
//	  - id: intent-without-action
//	    match: "(让我|我将)(进一步)?(查询|查看)"   # 对回复末尾 tail_runes 个字符做正则匹配
//	    unless: "(总结如下)"                     # 可选：末尾命中则不触发
//	    unless_scope: match_sentence             # 可选：tail（默认，整段末尾）| match_sentence（仅 match 所在句）
//	    message: "你说要继续查，但没有调用工具……"
//	  - id: open-todos
//	    when_open_todos: true                    # 本 Run 用过 todo 且仍有未完成项
//	    message: "清单未完成：\n{{open_todos}}"
//	  - id: ledger-gaps
//	    when_investigation_gaps: [onset_missing, open_hypotheses]  # 本 Run 用过台账且存在任一缺口
//	    message: "台账未闭环：\n{{investigation_gaps}}"
type HarnessStopRule struct {
	ID                    string   `yaml:"id"`
	TailRunes             int      `yaml:"tail_runes"`
	Match                 string   `yaml:"match"`
	Unless                string   `yaml:"unless"`
	UnlessScope           string   `yaml:"unless_scope"`
	WhenOpenTodos         bool     `yaml:"when_open_todos"`
	WhenInvestigationGaps []string `yaml:"when_investigation_gaps"`
	MinToolCalls          int      `yaml:"min_tool_calls"`
	Message               string   `yaml:"message"`
}

// StopHookOptions 为声明式 stop 规则提供运行时依赖。
type StopHookOptions struct {
	// Todos 为 when_open_todos 读取会话 todo 的存储；nil 时该条件永不满足。
	Todos tool.TodoStore
	// Investigations 为 when_investigation_gaps 读取会话台账；nil 时该条件永不满足。
	Investigations tool.InvestigationStore
}

// LoadWorkspaceStopHooks 从 workspace/harness/hooks.yaml 加载 stop_rules。
// 文件不存在 → nil, 0, nil；返回的 int 为 max_stop_nudges（0 表示用默认值）。
func LoadWorkspaceStopHooks(workspace string, opts StopHookOptions) ([]StopHook, int, error) {
	data, err := readWorkspaceHooksFile(workspace)
	if err != nil || data == nil {
		return nil, 0, err
	}
	return ParseHarnessStopHooksYAML(data, opts)
}

// ParseHarnessStopHooksYAML 解析 hooks YAML 中的 stop_rules。
func ParseHarnessStopHooksYAML(data []byte, opts StopHookOptions) ([]StopHook, int, error) {
	var file HarnessHooksFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, 0, fmt.Errorf("harness hooks: parse: %w", err)
	}
	if file.Version != 0 && file.Version != 1 {
		return nil, 0, fmt.Errorf("harness hooks: unsupported version %d", file.Version)
	}
	var hooks []StopHook
	for i, rule := range file.StopRules {
		h, err := newDeclarativeStopHook(rule, opts)
		if err != nil {
			id := rule.ID
			if id == "" {
				id = fmt.Sprintf("stop_rules[%d]", i)
			}
			return nil, 0, fmt.Errorf("harness hooks: stop rule %s: %w", id, err)
		}
		hooks = append(hooks, h)
	}
	return hooks, file.MaxStopNudges, nil
}

type declarativeStopHook struct {
	id            string
	tailRunes     int
	match         *regexp.Regexp
	unless        *regexp.Regexp
	unlessInMatch bool
	whenOpenTodos bool
	gaps          []string
	minToolCalls  int
	message       string
	todos         tool.TodoStore
	ledgers       tool.InvestigationStore
}

func newDeclarativeStopHook(rule HarnessStopRule, opts StopHookOptions) (*declarativeStopHook, error) {
	h := &declarativeStopHook{
		id:            strings.TrimSpace(rule.ID),
		tailRunes:     rule.TailRunes,
		whenOpenTodos: rule.WhenOpenTodos,
		minToolCalls:  rule.MinToolCalls,
		message:       strings.TrimSpace(rule.Message),
		todos:         opts.Todos,
		ledgers:       opts.Investigations,
	}
	for _, g := range rule.WhenInvestigationGaps {
		g = strings.TrimSpace(g)
		if !knownInvestigationGaps[g] {
			return nil, fmt.Errorf("unknown when_investigation_gaps value %q", g)
		}
		h.gaps = append(h.gaps, g)
	}
	if h.message == "" {
		return nil, fmt.Errorf("message is required")
	}
	if h.tailRunes <= 0 {
		h.tailRunes = defaultStopTailRunes
	}
	var err error
	if p := strings.TrimSpace(rule.Match); p != "" {
		if h.match, err = regexp.Compile(p); err != nil {
			return nil, fmt.Errorf("match: %w", err)
		}
	}
	if p := strings.TrimSpace(rule.Unless); p != "" {
		if h.unless, err = regexp.Compile(p); err != nil {
			return nil, fmt.Errorf("unless: %w", err)
		}
	}
	switch strings.TrimSpace(rule.UnlessScope) {
	case "", UnlessScopeTail:
	case UnlessScopeMatchSentence:
		if h.match == nil {
			return nil, fmt.Errorf("unless_scope %q requires match", UnlessScopeMatchSentence)
		}
		h.unlessInMatch = true
	default:
		return nil, fmt.Errorf("unknown unless_scope %q (use %s or %s)", rule.UnlessScope, UnlessScopeTail, UnlessScopeMatchSentence)
	}
	if h.match == nil && !h.whenOpenTodos && len(h.gaps) == 0 {
		return nil, fmt.Errorf("match, when_open_todos or when_investigation_gaps is required")
	}
	return h, nil
}

func (h *declarativeStopHook) OnStop(ctx context.Context, in StopHookInput) StopDecision {
	if len(in.ToolCalls) < h.minToolCalls {
		return StopDecision{}
	}
	tail := tailRunes(strings.TrimSpace(in.Text), h.tailRunes)
	if h.match != nil && !h.match.MatchString(tail) {
		return StopDecision{}
	}
	if h.unless != nil {
		if h.unlessInMatch {
			if !h.hasUnexemptedMatch(tail) {
				return StopDecision{}
			}
		} else if h.unless.MatchString(tail) {
			return StopDecision{}
		}
	}
	msg := h.message
	if h.whenOpenTodos {
		open := h.openTodos(ctx, in.ToolCalls)
		if len(open) == 0 {
			return StopDecision{}
		}
		msg = strings.ReplaceAll(msg, openTodosPlaceholder, strings.Join(open, "\n"))
	}
	if len(h.gaps) > 0 {
		gaps := h.investigationGaps(ctx, in)
		if len(gaps) == 0 {
			return StopDecision{}
		}
		msg = strings.ReplaceAll(msg, investigationGapsPlaceholder, strings.Join(gaps, "\n"))
	}
	return StopDecision{Continue: true, RuleID: h.id, Message: msg}
}

// investigationGaps 除 ledger_unused 外仅在本 Run 用过台账时检查，未参与调查的对话不受影响。
// ledger_unused 要求台账工具已注册但本 Run 未使用；通常配合 min_tool_calls，只约束多步排查。
func (h *declarativeStopHook) investigationGaps(ctx context.Context, in StopHookInput) []string {
	if h.ledgers == nil {
		return nil
	}
	if !usedToolSuccessfully(in.ToolCalls, tool.InvestigationToolName) {
		for _, g := range h.gaps {
			if g == GapLedgerUnused && in.HasTool != nil && in.HasTool(tool.InvestigationToolName) {
				return []string{"- 本轮做了多步排查，但没有用 investigation 台账记录症状、开始时间、候选假设和证据"}
			}
		}
		return nil
	}
	sessionID, _ := ctx.Value(tool.ContextKeySessionID).(string)
	if sessionID == "" {
		return nil
	}
	ledger := h.ledgers.Get(sessionID)
	var out []string
	for _, g := range h.gaps {
		switch g {
		case GapOnsetMissing:
			if ledger.OnsetMissing() {
				out = append(out, "- 问题开始时间未确认（set_onset），也未说明无法确认的原因（unknown_reason）")
			}
		case GapOpenHypotheses:
			for _, hyp := range ledger.OpenHypotheses() {
				out = append(out, fmt.Sprintf("- 假设 %s 尚未采纳或排除：%s", hyp.ID, hyp.Statement))
			}
		case GapOnsetUnbounded:
			if ledger.OnsetUnbounded() {
				out = append(out, fmt.Sprintf("- 开始时间「%s」缺少上次正常的时间点（last_good）：它可能只是你读到的日志最早位置，需往前查到最后一次正常记录", ledger.Onset))
			}
		case GapAcceptedWithoutChange:
			for _, hyp := range ledger.AcceptedWithoutChange() {
				out = append(out, fmt.Sprintf("- 已采纳的 %s 没有说明开始前后发生了什么变化（change 证据），可能只是在复述症状：%s", hyp.ID, hyp.Statement))
			}
		case GapMechanismOnly:
			if hyps := ledger.MechanismOnly(); len(hyps) > 0 {
				ids := make([]string, 0, len(hyps))
				for _, hyp := range hyps {
					ids = append(ids, hyp.ID)
				}
				out = append(out, fmt.Sprintf("- 已采纳的 %s 都不是 root（系统之外的状态变化）：系统自身的保护/重试/降级只是机制。继续追问「它为什么在 onset 开始」，找到外部变化后 add_hypothesis kind=root 并用 caused_by 串起链条；确实追不到时用 set_root_unknown 说明原因", strings.Join(ids, "、")))
			}
		case GapOnsetUnverified:
			if ledger.OnsetUnverified() {
				out = append(out, "- onset / last_good 没有原文支撑：用 set_onset 的 onset_quote、last_good_quote 附上显示第一次失败和最后一次成功的工具输出原文（timeline 工具可直接给出）")
			}
		}
	}
	return out
}

// InvestigationObserver 把每次成功的工具输出登记到台账存储，供 add_evidence 核对原文。
// 用作 WithReActToolSuccessHook；台账工具自身的输出不登记。
func InvestigationObserver(store tool.InvestigationStore) func(ctx context.Context, req *Request, rec ToolCallRecord) {
	return func(ctx context.Context, _ *Request, rec ToolCallRecord) {
		if store == nil || rec.ToolName == tool.InvestigationToolName {
			return
		}
		sessionID, _ := ctx.Value(tool.ContextKeySessionID).(string)
		store.RecordObservation(sessionID, tool.ToolObservation{
			ToolCallID: rec.ToolCallID,
			Tool:       rec.ToolName,
			Output:     tool.ObservationTextWithSpills(ctx, rec.Result),
			Args:       rec.Arguments,
		})
	}
}

// openTodos 仅在本 Run 调用过 todo 时返回未完成项，避免旧 turn 遗留的清单卡住新问题。
func (h *declarativeStopHook) openTodos(ctx context.Context, calls []ToolCallRecord) []string {
	if h.todos == nil || !usedToolSuccessfully(calls, "todo") {
		return nil
	}
	sessionID, _ := ctx.Value(tool.ContextKeySessionID).(string)
	if sessionID == "" {
		return nil
	}
	var out []string
	for _, item := range h.todos.List(sessionID) {
		if item.Status == tool.TodoStatusPending || item.Status == tool.TodoStatusInProgress {
			out = append(out, fmt.Sprintf("- [%s] %s", item.Status, item.Content))
		}
	}
	return out
}

func usedToolSuccessfully(calls []ToolCallRecord, name string) bool {
	for _, c := range calls {
		if c.ToolName == name && c.Allowed && strings.TrimSpace(c.Error) == "" {
			return true
		}
	}
	return false
}

// unless_scope 可选值。
const (
	UnlessScopeTail          = "tail"
	UnlessScopeMatchSentence = "match_sentence"
)

// hasUnexemptedMatch 判断是否存在一处 match，其所在句子不命中 unless。
func (h *declarativeStopHook) hasUnexemptedMatch(text string) bool {
	for _, loc := range h.match.FindAllStringIndex(text, -1) {
		if !h.unless.MatchString(enclosingSentence(text, loc[0], loc[1])) {
			return true
		}
	}
	return false
}

const sentenceBreaks = "。！？!?\n"

// enclosingSentence 返回覆盖 [start,end) 的句子（以中英文句末标点或换行分隔）。
func enclosingSentence(s string, start, end int) string {
	from := strings.LastIndexAny(s[:start], sentenceBreaks)
	if from < 0 {
		from = 0
	} else {
		_, size := utf8.DecodeRuneInString(s[from:])
		from += size
	}
	to := strings.IndexAny(s[end:], sentenceBreaks)
	if to < 0 {
		to = len(s)
	} else {
		_, size := utf8.DecodeRuneInString(s[end+to:])
		to = end + to + size
	}
	return s[from:to]
}

func tailRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}
