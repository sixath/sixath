package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sixath/framework/investigate/cases"
)

// CaseLibraryToolName 是案例库工具的注册名。
const CaseLibraryToolName = "case_library"

// CaseSaveConfirmKind 为案例草稿确认卡片的 kind。
const CaseSaveConfirmKind = "case_save"

const (
	maxRecalledCases     = 3
	maxCaseEvidence      = 10
	maxCaseProbes        = 5
	maxCaseProbeExpect   = 160
	maxCaseEvidencePerHy = 3
)

// CaseStoreResolver 按运行上下文返回案例库（通常是 agent 工作区下的 cases/）；返回 nil 表示不可用。
type CaseStoreResolver func(ctx context.Context) cases.Store

// WorkspaceCaseStore 使用上下文中的工作区根目录作为案例库位置。
func WorkspaceCaseStore(ctx context.Context) cases.Store {
	ws, _ := ctx.Value(ContextKeyWorkspaceRoot).(string)
	if strings.TrimSpace(ws) == "" {
		return nil
	}
	return cases.ForWorkspace(ws)
}

func caseSummary(h cases.Hit) map[string]any {
	c := h.Case
	out := map[string]any{"id": c.ID, "symptom": c.Symptom, "score": h.Score}
	if c.Title != "" {
		out["title"] = c.Title
	}
	if len(c.Chain) > 0 {
		out["chain"] = c.Chain
	}
	if len(c.VerifyProbes) > 0 {
		out["verify_probes"] = c.VerifyProbes
	}
	if c.Fix != "" {
		out["fix"] = c.Fix
	}
	return out
}

func containsID(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func mergeIDs(cur, add []string) []string {
	for _, id := range add {
		if !containsID(cur, id) {
			cur = append(cur, id)
		}
	}
	return cur
}

const caseLibraryDescription = `Library of confirmed past investigations (symptom -> root cause chain -> probes that verified it).

Actions:
- search {query}: find confirmed cases similar to a symptom. Found cases are added to the investigation ledger; verify each (run its verify_probes, add_hypothesis with from_case, accept or reject).
- propose {title?, signature?, fix?}: after the root cause is accepted in the investigation ledger, draft a case from the ledger (symptom, chain, onset, key evidence, probes). signature = short keywords that identify this kind of failure (component, log markers, failure form). fix = the remediation that addresses the root. The draft only takes effect after the user confirms it in the UI; you cannot confirm it yourself.`

// RegisterCaseLibraryTool 注册案例库工具；resolve 为 nil 时使用工作区下的 cases/。
func RegisterCaseLibraryTool(reg *Registry, store InvestigationStore, resolve CaseStoreResolver) error {
	if reg == nil {
		return errors.New("case_library: registry is nil")
	}
	if store == nil {
		store = DefaultInvestigationStore
	}
	if resolve == nil {
		resolve = WorkspaceCaseStore
	}
	return reg.Register(Tool{
		Name:        CaseLibraryToolName,
		Description: caseLibraryDescription,
		Effect:      EffectRead,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action":    map[string]any{"type": "string", "enum": []string{"search", "propose"}},
				"query":     map[string]any{"type": "string"},
				"title":     map[string]any{"type": "string"},
				"signature": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"fix":       map[string]any{"type": "string"},
			},
			"required": []string{"action"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			cs := resolve(ctx)
			if cs == nil {
				return map[string]any{"ok": false, "error": "case library is not available (no workspace)"}, nil
			}
			sessionID, _ := ctx.Value(ContextKeySessionID).(string)
			action, _ := params["action"].(string)
			switch strings.TrimSpace(action) {
			case "search":
				return caseSearch(cs, store, sessionID, params)
			case "propose":
				if sessionID == "" {
					return map[string]any{"ok": false, "error": "session_id is required"}, nil
				}
				out, err := caseProposeDraft(cs, store, sessionID, params)
				if err != nil {
					return map[string]any{"ok": false, "error": err.Error()}, nil
				}
				return out, nil
			default:
				return map[string]any{"ok": false, "error": fmt.Sprintf("unknown action %q", action)}, nil
			}
		},
	})
}

func caseSearch(cs cases.Store, store InvestigationStore, sessionID string, params map[string]any) (any, error) {
	q, _ := params["query"].(string)
	if strings.TrimSpace(q) == "" {
		q = store.Get(sessionID).Symptom
	}
	if strings.TrimSpace(q) == "" {
		return map[string]any{"ok": false, "error": "query is required"}, nil
	}
	hits, err := cs.Search(q, maxRecalledCases, false)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}, nil
	}
	out := make([]map[string]any, 0, len(hits))
	var ids []string
	for _, h := range hits {
		out = append(out, caseSummary(h))
		ids = append(ids, h.Case.ID)
	}
	if sessionID != "" {
		_, _ = store.Update(sessionID, func(l *InvestigationLedger) error {
			l.CasesEnabled = true
			l.RecalledCases = mergeIDs(l.RecalledCases, ids)
			return nil
		})
	}
	return map[string]any{"ok": true, "cases": out}, nil
}

func caseProposeDraft(cs cases.Store, store InvestigationStore, sessionID string, params map[string]any) (map[string]any, error) {
	ledger := store.Get(sessionID)
	c, err := CaseFromLedger(ledger)
	if err != nil {
		return nil, err
	}
	if t, _ := params["title"].(string); strings.TrimSpace(t) != "" {
		c.Title = strings.TrimSpace(t)
	}
	if f, _ := params["fix"].(string); strings.TrimSpace(f) != "" {
		c.Fix = strings.TrimSpace(f)
	}
	c.Signature = stringList(params["signature"])
	c.SessionID = sessionID
	if ledger.CaseProposed != "" {
		if old, gerr := cs.Get(ledger.CaseProposed); gerr == nil && old.Status == cases.StatusDraft {
			c.ID, c.CreatedAt = old.ID, old.CreatedAt
		}
	}
	saved, err := cs.SaveDraft(c)
	if err != nil {
		return nil, err
	}
	_, _ = store.Update(sessionID, func(l *InvestigationLedger) error {
		l.CaseProposed = saved.ID
		return nil
	})
	name := saved.Title
	if name == "" {
		name = saved.Symptom
	}
	return map[string]any{
		"ok":      true,
		"status":  "pending",
		"kind":    CaseSaveConfirmKind,
		"action":  "save",
		"token":   saved.ID,
		"name":    name,
		"preview": cases.Render(saved),
		"note":    "draft saved; it is used in future investigations only after the user confirms it",
	}, nil
}

// CaseFromLedger 用台账中已采纳的因果链生成案例草稿（不含 ID/签名/修复）。
func CaseFromLedger(l InvestigationLedger) (cases.Case, error) {
	if strings.TrimSpace(l.Symptom) == "" {
		return cases.Case{}, errors.New("ledger has no symptom: set_symptom first")
	}
	var accepted []Hypothesis
	for _, h := range l.Hypotheses {
		if h.Status == HypothesisAccepted {
			accepted = append(accepted, h)
		}
	}
	if len(accepted) == 0 {
		return cases.Case{}, errors.New("no accepted hypothesis in the ledger: accept the root cause first")
	}
	sort.SliceStable(accepted, func(i, j int) bool { return kindRank(accepted[i].Kind) < kindRank(accepted[j].Kind) })
	c := cases.Case{Symptom: l.Symptom, Onset: l.Onset, LastGood: l.LastGood}
	seenProbe := map[string]bool{}
	addEvidence := func(e EvidenceLink) {
		if len(c.KeyEvidence) < maxCaseEvidence {
			c.KeyEvidence = append(c.KeyEvidence, cases.Evidence{Tool: e.Tool, Quote: e.Quote, Note: e.Note})
		}
		if e.Tool == "" || len(e.Args) == 0 || len(c.VerifyProbes) >= maxCaseProbes || !reusableProbeTool(e.Tool) {
			return
		}
		key, _ := json.Marshal(map[string]any{"t": e.Tool, "a": e.Args})
		if seenProbe[string(key)] {
			return
		}
		seenProbe[string(key)] = true
		expect := e.Quote
		if e.Contrast && e.SubjectQuote != "" {
			expect = e.SubjectQuote
		}
		c.VerifyProbes = append(c.VerifyProbes, cases.Probe{Tool: e.Tool, Args: e.Args, Expect: cutRunes(expect, maxCaseProbeExpect)})
	}
	if l.OnsetEvidence != nil {
		addEvidence(*l.OnsetEvidence)
	}
	for _, h := range accepted {
		kind := h.Kind
		if kind == "" {
			kind = HypothesisKindMechanism
		}
		c.Chain = append(c.Chain, cases.ChainLink{Kind: kind, Statement: h.Statement})
		n := 0
		for _, e := range h.Evidence {
			if n >= maxCaseEvidencePerHy {
				break
			}
			if e.Verified || len(h.Evidence) == 1 {
				addEvidence(e)
				n++
			}
		}
	}
	return c, nil
}

func reusableProbeTool(name string) bool {
	switch name {
	case InvestigationToolName, CaseLibraryToolName, "todo", "ask_user":
		return false
	}
	return true
}

func kindRank(k string) int {
	switch k {
	case HypothesisKindRoot:
		return 0
	case HypothesisKindTrigger:
		return 1
	case HypothesisKindMechanism:
		return 2
	}
	return 3
}

func stringList(v any) []string {
	var out []string
	switch x := v.(type) {
	case []string:
		out = append(out, x...)
	case []any:
		for _, s := range x {
			if str, ok := s.(string); ok {
				out = append(out, str)
			}
		}
	case string:
		out = strings.Split(x, ",")
	}
	clean := out[:0]
	for _, s := range out {
		if s = strings.TrimSpace(s); s != "" {
			clean = append(clean, s)
		}
	}
	return clean
}

func cutRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
