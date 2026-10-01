package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/sixath/framework/investigate/cases"
)

// InvestigationToolName 是证据台账工具的注册名。
const InvestigationToolName = "investigation"

// Hypothesis 状态。
const (
	HypothesisOpen     = "open"
	HypothesisAccepted = "accepted"
	HypothesisRejected = "rejected"
)

// Hypothesis 层级：root 为系统之外的状态变化，trigger 为它造成的触发条件，mechanism 为系统自身的保护/重试/降级等表现机制。
const (
	HypothesisKindRoot      = "root"
	HypothesisKindTrigger   = "trigger"
	HypothesisKindMechanism = "mechanism"
)

func validHypothesisKind(k string) bool {
	return k == HypothesisKindRoot || k == HypothesisKindTrigger || k == HypothesisKindMechanism
}

// 观测需覆盖完整工具输出（含落盘文件），否则模型从翻阅结果里摘的原文会被误判为不存在。
const (
	maxObservationsPerSession     = 200
	maxObservationBytes           = 2 << 20
	maxSessionObservationBytes    = 16 << 20
	maxObservationSpillFileBytes  = 2 << 20
	maxObservationSpillFilesPerOp = 4
)

// EvidenceLink 把一段工具输出原文挂到某个假设上。
type EvidenceLink struct {
	ToolCallID string `json:"tool_call_id,omitempty"`
	Tool       string `json:"tool,omitempty"`
	Quote      string `json:"quote"`
	Note       string `json:"note,omitempty"`
	// Contrast 表示该证据来自对照组（正常对象、正常时段）。
	Contrast bool `json:"contrast,omitempty"`
	// SubjectQuote 为对照证据中故障对象一侧的原文（Quote 为正常对象一侧），两者须不同。
	SubjectQuote string `json:"subject_quote,omitempty"`
	// Change 表示该证据说明了"上次正常"到"开始出问题"之间发生的变化。
	Change bool `json:"change,omitempty"`
	// Verified 表示 Quote 已在本会话记录的工具输出中找到；未接入观测记录时为 false。
	Verified bool `json:"verified"`
	// Producer 表示该证据来自缺失信号的产生者自身（它的日志/状态），而不是等待该信号的一方。
	Producer bool `json:"producer,omitempty"`
	// Args 为产生该原文的工具调用参数，用于把结案证据沉淀为可复用的验证探针；不展示给模型。
	Args map[string]any `json:"-"`
}

// Hypothesis 一个候选根因。
type Hypothesis struct {
	ID        string         `json:"id"`
	Statement string         `json:"statement"`
	Status    string         `json:"status"`
	Kind      string         `json:"kind,omitempty"`
	// CausedBy 为造成本假设的上游假设 ID（mechanism ← trigger ← root）。
	CausedBy string `json:"caused_by,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Evidence  []EvidenceLink `json:"evidence,omitempty"`
	// ContrastUnavailable 采纳时无法获得对照证据的原因。
	ContrastUnavailable string `json:"contrast_unavailable,omitempty"`
	// ChangeUnavailable 采纳时无法找到变化证据的原因。
	ChangeUnavailable string `json:"change_unavailable,omitempty"`
	// FromCase 为该假设来源的历史案例 ID。
	FromCase string `json:"from_case,omitempty"`
	// Absence 为该假设描述的缺失信号（某文件/日志/回调没有出现）。
	Absence string `json:"absence,omitempty"`
}

// InvestigationLedger 会话级调查台账。
type InvestigationLedger struct {
	Symptom            string `json:"symptom,omitempty"`
	Onset              string `json:"onset,omitempty"`
	OnsetUnknownReason string `json:"onset_unknown_reason,omitempty"`
	// LastGood 为已确认正常的最近时间点，与 Onset 一起界定问题开始的区间。
	LastGood string `json:"last_good,omitempty"`
	// OnsetEvidence / LastGoodEvidence 为支撑两个时间点的工具输出原文。
	OnsetEvidence    *EvidenceLink `json:"onset_evidence,omitempty"`
	LastGoodEvidence *EvidenceLink `json:"last_good_evidence,omitempty"`
	// RootUnknownReason 说明为何无法追到系统之外的根因（如缺少对应时段的数据）。
	RootUnknownReason string       `json:"root_unknown_reason,omitempty"`
	Hypotheses        []Hypothesis `json:"hypotheses,omitempty"`
	// RecalledCases 为本次调查召回的已确认历史案例 ID。
	RecalledCases []string `json:"recalled_cases,omitempty"`
	// CasesEnabled 表示本会话可用案例库；CaseProposed 为已生成的案例草稿 ID。
	CasesEnabled bool   `json:"-"`
	CaseProposed string `json:"case_proposed,omitempty"`
}

// IgnoredCases 返回召回了但没有任何来源于它的假设被验证（accepted/rejected）的案例。
func (l InvestigationLedger) IgnoredCases() []string {
	tested := map[string]bool{}
	for _, h := range l.Hypotheses {
		if h.FromCase != "" && h.Status != HypothesisOpen {
			tested[h.FromCase] = true
		}
	}
	var out []string
	for _, id := range l.RecalledCases {
		if !tested[id] {
			out = append(out, id)
		}
	}
	return out
}

// AbsenceWithoutProducer 返回声明了缺失信号、但因果链上没有产生者证据的已采纳假设。
func (l InvestigationLedger) AbsenceWithoutProducer() []Hypothesis {
	hasProducer := false
	for _, h := range l.Hypotheses {
		if h.Status != HypothesisAccepted {
			continue
		}
		for _, e := range h.Evidence {
			hasProducer = hasProducer || e.Producer
		}
	}
	if hasProducer {
		return nil
	}
	var out []Hypothesis
	for _, h := range l.Hypotheses {
		if h.Status == HypothesisAccepted && strings.TrimSpace(h.Absence) != "" {
			out = append(out, h)
		}
	}
	return out
}

// HasAcceptedRoot 是否已采纳 kind=root 的假设。
func (l InvestigationLedger) HasAcceptedRoot() bool {
	for _, h := range l.Hypotheses {
		if h.Status == HypothesisAccepted && h.Kind == HypothesisKindRoot {
			return true
		}
	}
	return false
}

// OnsetUnverified 给了开始时间/上次正常时间，但缺少对应的工具输出原文。
func (l InvestigationLedger) OnsetUnverified() bool {
	if strings.TrimSpace(l.OnsetUnknownReason) != "" || strings.TrimSpace(l.Onset) == "" {
		return false
	}
	if l.OnsetEvidence == nil {
		return true
	}
	return strings.TrimSpace(l.LastGood) != "" && l.LastGoodEvidence == nil
}

// MechanismOnly 已有采纳的假设，但没有一个是 root（系统之外的变化），也未说明无法追到根因的原因。
func (l InvestigationLedger) MechanismOnly() []Hypothesis {
	if strings.TrimSpace(l.RootUnknownReason) != "" {
		return nil
	}
	var accepted []Hypothesis
	for _, h := range l.Hypotheses {
		if h.Status != HypothesisAccepted {
			continue
		}
		if h.Kind == HypothesisKindRoot {
			return nil
		}
		accepted = append(accepted, h)
	}
	return accepted
}

// OnsetMissing 问题开始时间既未确认也未说明原因。
func (l InvestigationLedger) OnsetMissing() bool {
	return strings.TrimSpace(l.Onset) == "" && strings.TrimSpace(l.OnsetUnknownReason) == ""
}

// OnsetUnbounded 给了开始时间但没有"上次正常"作为下界：可能只是现有日志的最早位置。
func (l InvestigationLedger) OnsetUnbounded() bool {
	return strings.TrimSpace(l.Onset) != "" && strings.TrimSpace(l.LastGood) == "" && strings.TrimSpace(l.OnsetUnknownReason) == ""
}

// AcceptedWithoutChange 返回已采纳但既无变化证据也未说明原因的假设。
func (l InvestigationLedger) AcceptedWithoutChange() []Hypothesis {
	var out []Hypothesis
	for _, h := range l.Hypotheses {
		if h.Status != HypothesisAccepted || strings.TrimSpace(h.ChangeUnavailable) != "" {
			continue
		}
		hasChange := false
		for _, e := range h.Evidence {
			hasChange = hasChange || e.Change
		}
		if !hasChange {
			out = append(out, h)
		}
	}
	return out
}

// OpenHypotheses 返回仍未采纳或排除的假设。
func (l InvestigationLedger) OpenHypotheses() []Hypothesis {
	var out []Hypothesis
	for _, h := range l.Hypotheses {
		if h.Status == HypothesisOpen {
			out = append(out, h)
		}
	}
	return out
}

// ToolObservation 是一次成功工具调用的输出快照，用于核对证据原文。
type ToolObservation struct {
	ToolCallID string
	Tool       string
	Output     string
	Args       map[string]any
}

// InvestigationStore 保存会话台账与工具输出观测。
type InvestigationStore interface {
	Get(sessionID string) InvestigationLedger
	Update(sessionID string, fn func(*InvestigationLedger) error) (InvestigationLedger, error)
	RecordObservation(sessionID string, obs ToolObservation)
	// FindQuote 在观测中查找 quote；toolCallID 非空时只查该次调用。
	// recorded 表示该会话是否有任何观测（没有则无法核对）。
	FindQuote(sessionID, toolCallID, quote string) (obs ToolObservation, found, recorded bool)
}

// InMemoryInvestigationStore 进程内实现。
type InMemoryInvestigationStore struct {
	mu       sync.Mutex
	ledgers  map[string]*InvestigationLedger
	observed map[string][]ToolObservation
}

// DefaultInvestigationStore 供运行时共享。
var DefaultInvestigationStore = NewInMemoryInvestigationStore()

func NewInMemoryInvestigationStore() *InMemoryInvestigationStore {
	return &InMemoryInvestigationStore{
		ledgers:  map[string]*InvestigationLedger{},
		observed: map[string][]ToolObservation{},
	}
}

func (s *InMemoryInvestigationStore) Get(sessionID string) InvestigationLedger {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l := s.ledgers[sessionID]; l != nil {
		return cloneLedger(*l)
	}
	return InvestigationLedger{}
}

func (s *InMemoryInvestigationStore) Update(sessionID string, fn func(*InvestigationLedger) error) (InvestigationLedger, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := InvestigationLedger{}
	if l := s.ledgers[sessionID]; l != nil {
		cur = cloneLedger(*l)
	}
	if err := fn(&cur); err != nil {
		return InvestigationLedger{}, err
	}
	stored := cloneLedger(cur)
	s.ledgers[sessionID] = &stored
	return cur, nil
}

// RecordObservation 保存归一化后的输出（Output 字段即归一化文本），按条数与总字节淘汰最旧的观测。
func (s *InMemoryInvestigationStore) RecordObservation(sessionID string, obs ToolObservation) {
	if sessionID == "" || strings.TrimSpace(obs.Output) == "" {
		return
	}
	obs.Output = normalizeQuote(cutUTF8Prefix(obs.Output, maxObservationBytes))
	s.mu.Lock()
	defer s.mu.Unlock()
	list := append(s.observed[sessionID], obs)
	total := 0
	start := len(list)
	for start > 0 && len(list)-start < maxObservationsPerSession {
		if total+len(list[start-1].Output) > maxSessionObservationBytes && start < len(list) {
			break
		}
		total += len(list[start-1].Output)
		start--
	}
	s.observed[sessionID] = append([]ToolObservation(nil), list[start:]...)
}

func (s *InMemoryInvestigationStore) FindQuote(sessionID, toolCallID, quote string) (ToolObservation, bool, bool) {
	needle := normalizeQuote(quote)
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.observed[sessionID]
	if len(list) == 0 || needle == "" {
		return ToolObservation{}, false, len(list) > 0
	}
	for i := len(list) - 1; i >= 0; i-- {
		obs := list[i]
		if toolCallID != "" && obs.ToolCallID != toolCallID {
			continue
		}
		if strings.Contains(obs.Output, needle) {
			return obs, true, true
		}
	}
	return ToolObservation{}, false, true
}

var quoteUnescaper = strings.NewReplacer(
	`\\`, `\`, `\"`, `"`, `\n`, " ", `\r`, " ", `\t`, " ",
	`\u003c`, "<", `\u003e`, ">", `\u0026`, "&",
)

// normalizeQuote 反复还原 JSON 转义（工具结果常被多层编码），并压缩空白、转小写。
func normalizeQuote(s string) string {
	for i := 0; i < 3; i++ {
		next := quoteUnescaper.Replace(s)
		if next == s {
			break
		}
		s = next
	}
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

var spillPathRe = regexp.MustCompile(`tmp/results/[A-Za-z0-9._-]+/[A-Za-z0-9._-]+\.(?:txt|jsonl|json|csv)`)

// ObservationTextWithSpills 在 ObservationText 基础上附加结果中引用的本会话落盘文件内容，
// 使从 read_file / search_files 之外的完整输出里摘出的原文也能被核对。
func ObservationTextWithSpills(ctx context.Context, result any) string {
	text := ObservationText(result)
	if ctx == nil {
		return text
	}
	ws, _ := ctx.Value(ContextKeyWorkspaceRoot).(string)
	sess, _ := ctx.Value(ContextKeySessionID).(string)
	if strings.TrimSpace(ws) == "" || strings.TrimSpace(sess) == "" {
		return text
	}
	prefix := "tmp/results/" + sess + "/"
	seen := map[string]bool{}
	var b strings.Builder
	b.WriteString(text)
	for _, rel := range spillPathRe.FindAllString(text, -1) {
		if seen[rel] || !strings.HasPrefix(rel, prefix) || len(seen) >= maxObservationSpillFilesPerOp {
			continue
		}
		seen[rel] = true
		f, err := os.Open(filepath.Join(ws, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(f, maxObservationSpillFileBytes))
		_ = f.Close()
		b.WriteString("\n")
		b.Write(data)
	}
	return b.String()
}

func cloneLedger(l InvestigationLedger) InvestigationLedger {
	out := l
	if l.OnsetEvidence != nil {
		e := *l.OnsetEvidence
		out.OnsetEvidence = &e
	}
	if l.LastGoodEvidence != nil {
		e := *l.LastGoodEvidence
		out.LastGoodEvidence = &e
	}
	out.RecalledCases = append([]string(nil), l.RecalledCases...)
	out.Hypotheses = make([]Hypothesis, len(l.Hypotheses))
	for i, h := range l.Hypotheses {
		h.Evidence = append([]EvidenceLink(nil), h.Evidence...)
		out.Hypotheses[i] = h
	}
	return out
}

// ObservationText 把工具结果转为可检索的文本。
func ObservationText(result any) string {
	switch v := result.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(raw)
	}
}

const investigationToolDescription = `Investigation ledger for diagnosing problems. Keeps the symptom, when the problem started, candidate root causes (hypotheses) and the evidence for each, so conclusions stay grounded.

Actions:
- view: show the ledger.
- set_symptom {symptom}: when a case library is available, returns similar_cases (confirmed past investigations). Verify each one FIRST: run its verify_probes against this target, add_hypothesis with from_case=<case id>, then accept or reject it with evidence, before open-ended exploration.
- set_onset {onset, onset_quote, last_good, last_good_quote} when the problem started, and the latest time it was confirmed working (e.g. the last success record). Each time needs a verbatim quote from a tool output showing it (the first failure line / the last success line). The earliest failure in the logs you happened to read is not the onset unless you also found the last good point before it; look further back if needed (the timeline tool finds both). Or {unknown_reason} if it cannot be determined.
- add_hypothesis {statement, kind?, caused_by?, from_case?, absence?} -> returns hypothesis_id. Record every plausible cause, including ones you plan to rule out. kind: root = a state change outside the system (deploy, edited config/file, interrupted process, expired dependency...); trigger = the condition that change created; mechanism = the system's own protection/retry/fallback/counter behaviour that makes it visible. caused_by = id of the upstream hypothesis (mechanism -> trigger -> root). Restating the symptom is not a root cause. from_case = id of the past case this hypothesis comes from. absence = the signal this hypothesis says is missing (a file/log line/callback that never appears).
- classify {hypothesis_id, kind, caused_by?, absence?}: set or change a hypothesis' kind / upstream link / missing signal.
- add_evidence {hypothesis_id, quote, tool_call_id?, note?, contrast?, subject_quote?, change?, producer?}: quote must be copied verbatim from a tool output in this session. contrast=true when the evidence compares the failing target with a healthy one (another instance, a normal time window) using the same probe: then quote is what the healthy side shows and subject_quote is what the failing side shows; both must be verbatim and must differ. change=true when the evidence shows what changed between last_good and onset. producer=true when the quote comes from the component that is supposed to produce a missing signal (its own log/state), not from the side waiting for it.
- set_status {hypothesis_id, status: accepted|rejected|open, reason?, contrast_unavailable?, change_unavailable?}: accepting requires at least one piece of evidence AND either contrast evidence or contrast_unavailable explaining why no comparison was possible. An accepted cause should also have change evidence, or change_unavailable explaining why none could be found. Reject hypotheses that the comparison target also shows, or that are contradicted by evidence.
- set_root_unknown {unknown_reason}: when you honestly cannot trace past a mechanism/trigger to an outside change, say why (e.g. no data for that period).

Method (domain-independent):
1. Keep asking "why" until the answer is a state change outside the system (deploy/upgrade, edited config or file, interrupted process, expired or changed dependency, exhausted resource, manual action). The system's own protection/retry/fallback/counter/breaker logic is a mechanism: it explains how the problem shows, not why it started.
2. Widen the window (days) to find the first failure and the last success before it, both backed by quotes; then ask what changed between them.
3. Contrast at the discriminating layer (the same config/file/version/dependency/argument on a healthy vs failing target), not the symptom itself.
4. Look at what was modified recently near the failing component. A failed or timed-out probe is not evidence of absence.
5. When the finding is "X never happens", find the component that should produce X and read its own logs/state; do not guess why from the waiting side.
6. If the failure changes form inside the window (different error, different timeout), something changed again: look for the config/parameter/operation change at that moment.
7. Do not interpret a number as an error/status code without reading the code or docs that define it.

Read-only bookkeeping; never needs user confirmation.`

// InvestigationToolOptions 台账工具的可选依赖。
type InvestigationToolOptions struct {
	// Cases 解析本次运行可用的案例库；为 nil 或返回 nil 时不召回历史案例。
	Cases CaseStoreResolver
}

// RegisterInvestigationTool 注册证据台账工具。
func RegisterInvestigationTool(reg *Registry, store InvestigationStore) error {
	return RegisterInvestigationToolWithOptions(reg, store, InvestigationToolOptions{})
}

// RegisterInvestigationToolWithOptions 注册证据台账工具，并按 opts 接入案例召回。
func RegisterInvestigationToolWithOptions(reg *Registry, store InvestigationStore, opts InvestigationToolOptions) error {
	if reg == nil {
		return errors.New("investigation: registry is nil")
	}
	if store == nil {
		store = DefaultInvestigationStore
	}
	return reg.Register(Tool{
		Name:        InvestigationToolName,
		Description: investigationToolDescription,
		Effect:      EffectRead,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type": "string",
					"enum": []string{"view", "set_symptom", "set_onset", "add_hypothesis", "classify", "add_evidence", "set_status", "set_root_unknown"},
				},
				"symptom":              map[string]any{"type": "string"},
				"onset":                map[string]any{"type": "string", "description": "When the problem started (timestamp or time range)."},
				"onset_quote":          map[string]any{"type": "string", "description": "Verbatim tool-output line showing the first failure."},
				"last_good":            map[string]any{"type": "string", "description": "Latest time the target was confirmed working, and what shows it."},
				"last_good_quote":      map[string]any{"type": "string", "description": "Verbatim tool-output line showing the last success before onset."},
				"kind":                 map[string]any{"type": "string", "enum": []string{HypothesisKindRoot, HypothesisKindTrigger, HypothesisKindMechanism}},
				"caused_by":            map[string]any{"type": "string", "description": "Upstream hypothesis id."},
				"subject_quote":        map[string]any{"type": "string", "description": "contrast=true: verbatim output of the same probe on the failing target."},
				"change":               map[string]any{"type": "boolean"},
				"change_unavailable":   map[string]any{"type": "string"},
				"unknown_reason":       map[string]any{"type": "string"},
				"statement":            map[string]any{"type": "string"},
				"hypothesis_id":        map[string]any{"type": "string"},
				"quote":                map[string]any{"type": "string", "description": "Verbatim excerpt from a tool output."},
				"tool_call_id":         map[string]any{"type": "string"},
				"note":                 map[string]any{"type": "string"},
				"contrast":             map[string]any{"type": "boolean"},
				"status":               map[string]any{"type": "string", "enum": []string{HypothesisAccepted, HypothesisRejected, HypothesisOpen}},
				"reason":               map[string]any{"type": "string"},
				"contrast_unavailable": map[string]any{"type": "string"},
				"from_case":            map[string]any{"type": "string", "description": "Id of the past case (similar_cases) this hypothesis comes from."},
				"absence":              map[string]any{"type": "string", "description": "The signal this hypothesis says never appears."},
				"producer":             map[string]any{"type": "boolean", "description": "The quote comes from the producer of the missing signal."},
			},
			"required": []string{"action"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			sessionID, _ := ctx.Value(ContextKeySessionID).(string)
			if sessionID == "" {
				return map[string]any{"ok": false, "error": "session_id is required"}, nil
			}
			var caseStore cases.Store
			if opts.Cases != nil {
				caseStore = opts.Cases(ctx)
			}
			out, err := runInvestigationAction(store, sessionID, params, caseStore)
			if err != nil {
				return map[string]any{"ok": false, "error": err.Error(), "ledger": store.Get(sessionID)}, nil
			}
			return out, nil
		},
	})
}

func runInvestigationAction(store InvestigationStore, sessionID string, p map[string]any, caseStore cases.Store) (map[string]any, error) {
	str := func(k string) string { v, _ := p[k].(string); return strings.TrimSpace(v) }
	action := str("action")
	result := map[string]any{"ok": true}
	var ledger InvestigationLedger
	var err error
	switch action {
	case "", "view":
		ledger = store.Get(sessionID)
	case "set_symptom":
		if str("symptom") == "" {
			return nil, errors.New("symptom is required")
		}
		var similar []map[string]any
		var ids []string
		if caseStore != nil {
			hits, serr := caseStore.Search(str("symptom"), maxRecalledCases, false)
			if serr != nil {
				result["case_search_error"] = serr.Error()
			}
			for _, h := range hits {
				similar = append(similar, caseSummary(h))
				ids = append(ids, h.Case.ID)
			}
		}
		ledger, err = store.Update(sessionID, func(l *InvestigationLedger) error {
			l.Symptom = str("symptom")
			l.CasesEnabled = l.CasesEnabled || caseStore != nil
			l.RecalledCases = mergeIDs(l.RecalledCases, ids)
			return nil
		})
		if len(similar) > 0 {
			result["similar_cases"] = similar
			result["next"] = "verify each similar case first: run its verify_probes on this target, add_hypothesis with from_case, then accept or reject it with evidence"
		}
	case "set_onset":
		onset, reason := str("onset"), str("unknown_reason")
		if onset == "" && reason == "" {
			return nil, errors.New("onset or unknown_reason is required")
		}
		var onsetEv, lastGoodEv *EvidenceLink
		if q := str("onset_quote"); q != "" {
			link, lerr := buildEvidenceLink(store, sessionID, "", q, "onset", false)
			if lerr != nil {
				return nil, fmt.Errorf("onset_quote: %w", lerr)
			}
			onsetEv = &link
		}
		if q := str("last_good_quote"); q != "" {
			link, lerr := buildEvidenceLink(store, sessionID, "", q, "last_good", false)
			if lerr != nil {
				return nil, fmt.Errorf("last_good_quote: %w", lerr)
			}
			lastGoodEv = &link
		}
		ledger, err = store.Update(sessionID, func(l *InvestigationLedger) error {
			if onset != l.Onset {
				l.OnsetEvidence = nil
			}
			l.Onset, l.OnsetUnknownReason = onset, reason
			if lg := str("last_good"); lg != "" || onset == "" {
				if lg != l.LastGood {
					l.LastGoodEvidence = nil
				}
				l.LastGood = lg
			}
			if onsetEv != nil {
				l.OnsetEvidence = onsetEv
			}
			if lastGoodEv != nil {
				l.LastGoodEvidence = lastGoodEv
			}
			return nil
		})
		if err == nil && onset != "" && (ledger.OnsetEvidence == nil || (ledger.LastGood != "" && ledger.LastGoodEvidence == nil)) {
			result["warning"] = "onset/last_good recorded without a verbatim quote; add onset_quote and last_good_quote from the tool output that shows them"
		}
	case "add_hypothesis":
		stmt := str("statement")
		if stmt == "" {
			return nil, errors.New("statement is required")
		}
		kind := str("kind")
		if kind != "" && !validHypothesisKind(kind) {
			return nil, fmt.Errorf("invalid kind %q (use root, trigger or mechanism)", kind)
		}
		var id string
		ledger, err = store.Update(sessionID, func(l *InvestigationLedger) error {
			cb := str("caused_by")
			if cb != "" && findHypothesis(l, cb) == nil {
				return fmt.Errorf("caused_by %q not found", cb)
			}
			fc := str("from_case")
			if fc != "" && !containsID(l.RecalledCases, fc) {
				return fmt.Errorf("from_case %q was not recalled in this investigation (see similar_cases from set_symptom or case_library search)", fc)
			}
			id = fmt.Sprintf("h%d", len(l.Hypotheses)+1)
			l.Hypotheses = append(l.Hypotheses, Hypothesis{ID: id, Statement: stmt, Status: HypothesisOpen, Kind: kind, CausedBy: cb, FromCase: fc, Absence: str("absence")})
			return nil
		})
		result["hypothesis_id"] = id
	case "classify":
		kind := str("kind")
		if !validHypothesisKind(kind) {
			return nil, fmt.Errorf("invalid kind %q (use root, trigger or mechanism)", kind)
		}
		ledger, err = store.Update(sessionID, func(l *InvestigationLedger) error {
			h := findHypothesis(l, str("hypothesis_id"))
			if h == nil {
				return fmt.Errorf("hypothesis %q not found", str("hypothesis_id"))
			}
			if cb := str("caused_by"); cb != "" {
				if cb == h.ID || findHypothesis(l, cb) == nil {
					return fmt.Errorf("caused_by %q must be another existing hypothesis", cb)
				}
				h.CausedBy = cb
			}
			if a := str("absence"); a != "" {
				h.Absence = a
			}
			h.Kind = kind
			return nil
		})
	case "set_root_unknown":
		reason := str("unknown_reason")
		if reason == "" {
			return nil, errors.New("unknown_reason is required")
		}
		ledger, err = store.Update(sessionID, func(l *InvestigationLedger) error {
			l.RootUnknownReason = reason
			return nil
		})
	case "add_evidence":
		contrast := p["contrast"] == true
		link, lerr := buildEvidenceLink(store, sessionID, str("tool_call_id"), str("quote"), str("note"), contrast)
		if lerr != nil {
			return nil, lerr
		}
		if contrast {
			subj := str("subject_quote")
			if subj == "" {
				return nil, errors.New("contrast evidence needs subject_quote (what the failing target shows for the same probe) in addition to quote (what the healthy target shows)")
			}
			if normalizeQuote(subj) == normalizeQuote(link.Quote) {
				return nil, errors.New("contrast quotes are identical: both targets show the same thing, so this is not a discriminating difference (it argues for rejecting the hypothesis)")
			}
			subjLink, serr := buildEvidenceLink(store, sessionID, "", subj, "", false)
			if serr != nil {
				return nil, fmt.Errorf("subject_quote: %w", serr)
			}
			link.SubjectQuote = subjLink.Quote
			link.Verified = link.Verified && subjLink.Verified
		}
		link.Change = p["change"] == true
		link.Producer = p["producer"] == true
		ledger, err = store.Update(sessionID, func(l *InvestigationLedger) error {
			h := findHypothesis(l, str("hypothesis_id"))
			if h == nil {
				return fmt.Errorf("hypothesis %q not found", str("hypothesis_id"))
			}
			h.Evidence = append(h.Evidence, link)
			return nil
		})
		if !link.Verified {
			result["warning"] = "no tool outputs recorded in this session; quote could not be verified"
		}
	case "set_status":
		ledger, err = store.Update(sessionID, func(l *InvestigationLedger) error {
			h := findHypothesis(l, str("hypothesis_id"))
			if h == nil {
				return fmt.Errorf("hypothesis %q not found", str("hypothesis_id"))
			}
			if err := applyHypothesisStatus(h, str("status"), str("reason"), str("contrast_unavailable")); err != nil {
				return err
			}
			if cu := str("change_unavailable"); cu != "" {
				h.ChangeUnavailable = cu
			}
			return nil
		})
	default:
		return nil, fmt.Errorf("unknown action %q", action)
	}
	if err != nil {
		return nil, err
	}
	result["ledger"] = ledger
	return result, nil
}

func buildEvidenceLink(store InvestigationStore, sessionID, callID, quote, note string, contrast bool) (EvidenceLink, error) {
	if quote == "" {
		return EvidenceLink{}, errors.New("quote is required: copy the relevant line verbatim from a tool output")
	}
	link := EvidenceLink{ToolCallID: callID, Quote: quote, Note: note, Contrast: contrast}
	obs, found, recorded := store.FindQuote(sessionID, callID, quote)
	if !recorded {
		return link, nil
	}
	if !found && callID != "" {
		obs, found, _ = store.FindQuote(sessionID, "", quote)
	}
	if !found {
		return EvidenceLink{}, errors.New("quote not found in any tool output of this session; copy it verbatim or query the data first")
	}
	link.ToolCallID, link.Tool, link.Verified, link.Args = obs.ToolCallID, obs.Tool, true, obs.Args
	return link, nil
}

func applyHypothesisStatus(h *Hypothesis, status, reason, contrastUnavailable string) error {
	switch status {
	case HypothesisOpen, HypothesisRejected:
		h.Status, h.Reason = status, reason
		return nil
	case HypothesisAccepted:
		if len(h.Evidence) == 0 {
			return errors.New("cannot accept without evidence: add_evidence first")
		}
		hasContrast := false
		for _, e := range h.Evidence {
			hasContrast = hasContrast || e.Contrast
		}
		if !hasContrast && contrastUnavailable == "" && h.ContrastUnavailable == "" {
			return errors.New("cannot accept without contrast: run the same query against a healthy target or normal time window and add it with contrast=true, or explain in contrast_unavailable why no comparison is possible")
		}
		h.Status, h.Reason = status, reason
		if contrastUnavailable != "" {
			h.ContrastUnavailable = contrastUnavailable
		}
		return nil
	default:
		return fmt.Errorf("invalid status %q", status)
	}
}

func findHypothesis(l *InvestigationLedger, id string) *Hypothesis {
	for i := range l.Hypotheses {
		if l.Hypotheses[i].ID == id {
			return &l.Hypotheses[i]
		}
	}
	return nil
}
