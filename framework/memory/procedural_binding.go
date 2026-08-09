package memory

import (
	"fmt"
	"log/slog"
	"strings"
)

const (
	BindingActionSkill         = "skill"
	BindingActionToolSequence  = "tool_sequence"
	BindingModeSuggest         = "suggest"
	BindingModePrefer          = "prefer"
)

// ProceduralBinding is a hand-written or future auto-committed repair slot (P3-C).
type ProceduralBinding struct {
	TriggerCode  string   `json:"trigger_code" yaml:"trigger_code"`
	TriggerQuery string   `json:"trigger_query" yaml:"trigger_query"`
	ActionKind   string   `json:"action_kind" yaml:"action_kind"` // skill | tool_sequence
	SkillID      string   `json:"skill_id" yaml:"skill_id"`
	ToolNames    []string `json:"tool_names" yaml:"tool_names"`
	Mode         string   `json:"mode" yaml:"mode"` // suggest | prefer
	AgentID      string   `json:"agent_id" yaml:"agent_id"` // optional scope; empty = all
}

// ResolveTaskFamily returns Agent tag/label if non-empty, else agentID (umbrella §6.5).
func ResolveTaskFamily(agentID string, agentTagsOrLabels string) string {
	if s := strings.TrimSpace(agentTagsOrLabels); s != "" {
		return s
	}
	return strings.TrimSpace(agentID)
}

// ValidateProceduralBinding checks schema and that tool_names ⊆ registeredTools.
// registeredTools nil/empty skips tool membership check (still requires names non-empty for tool_sequence).
func ValidateProceduralBinding(b ProceduralBinding, registeredTools map[string]struct{}) (ProceduralBinding, error) {
	b.TriggerCode = strings.TrimSpace(b.TriggerCode)
	b.TriggerQuery = strings.TrimSpace(b.TriggerQuery)
	b.ActionKind = strings.ToLower(strings.TrimSpace(b.ActionKind))
	b.SkillID = strings.TrimSpace(b.SkillID)
	b.Mode = strings.ToLower(strings.TrimSpace(b.Mode))
	b.AgentID = strings.TrimSpace(b.AgentID)
	if b.Mode == "" {
		b.Mode = BindingModeSuggest
	}
	if b.Mode != BindingModeSuggest && b.Mode != BindingModePrefer {
		return b, fmt.Errorf("memory: binding mode %q invalid", b.Mode)
	}
	if b.TriggerCode == "" && b.TriggerQuery == "" {
		return b, fmt.Errorf("memory: binding needs trigger_code or trigger_query")
	}
	switch b.ActionKind {
	case BindingActionSkill:
		if b.SkillID == "" {
			return b, fmt.Errorf("memory: skill binding needs skill_id")
		}
	case BindingActionToolSequence:
		names := make([]string, 0, len(b.ToolNames))
		for _, n := range b.ToolNames {
			n = strings.TrimSpace(n)
			if n == "" {
				continue
			}
			if registeredTools != nil {
				if _, ok := registeredTools[n]; !ok {
					return b, fmt.Errorf("memory: unknown tool %q in binding", n)
				}
			}
			names = append(names, n)
		}
		if len(names) == 0 {
			return b, fmt.Errorf("memory: tool_sequence binding needs tool_names")
		}
		b.ToolNames = names
	default:
		return b, fmt.Errorf("memory: action_kind %q invalid", b.ActionKind)
	}
	return b, nil
}

// FilterValidBindings validates and keeps good entries; logs and drops invalid.
func FilterValidBindings(items []ProceduralBinding, registeredTools map[string]struct{}, log *slog.Logger) []ProceduralBinding {
	if log == nil {
		log = slog.Default()
	}
	out := make([]ProceduralBinding, 0, len(items))
	for _, raw := range items {
		b, err := ValidateProceduralBinding(raw, registeredTools)
		if err != nil {
			log.Warn("procedural_binding_rejected", "err", err.Error(), "skill_id", raw.SkillID, "trigger_code", raw.TriggerCode)
			continue
		}
		out = append(out, b)
	}
	return out
}

// MatchProceduralBindings returns bindings matching agent, optional failure codes, and/or query text.
func MatchProceduralBindings(items []ProceduralBinding, agentID, query string, failureCodes []string) []ProceduralBinding {
	agentID = strings.TrimSpace(agentID)
	queryLower := strings.ToLower(strings.TrimSpace(query))
	codeSet := map[string]struct{}{}
	for _, c := range failureCodes {
		c = strings.TrimSpace(c)
		if c != "" {
			codeSet[c] = struct{}{}
		}
	}
	var out []ProceduralBinding
	for _, b := range items {
		if b.AgentID != "" && agentID != "" && b.AgentID != agentID {
			continue
		}
		matched := false
		if b.TriggerCode != "" {
			if _, ok := codeSet[b.TriggerCode]; ok {
				matched = true
			}
		}
		if !matched && b.TriggerQuery != "" && queryLower != "" {
			if strings.Contains(queryLower, strings.ToLower(b.TriggerQuery)) {
				matched = true
			}
		}
		if matched {
			out = append(out, b)
		}
	}
	return out
}

// FormatBindingSuggest renders a short suggest/prefer hint for Prefetch or prompt injection.
func FormatBindingSuggest(b ProceduralBinding) string {
	mode := b.Mode
	if mode == "" {
		mode = BindingModeSuggest
	}
	var action string
	switch b.ActionKind {
	case BindingActionSkill:
		action = "Skill `" + b.SkillID + "`"
	case BindingActionToolSequence:
		action = "工具序列 [" + strings.Join(b.ToolNames, " → ") + "]"
	default:
		action = b.ActionKind
	}
	trig := b.TriggerCode
	if trig == "" {
		trig = b.TriggerQuery
	}
	verb := "建议"
	if mode == BindingModePrefer {
		verb = "优先"
	}
	return fmt.Sprintf("【过程修复 %s】条件 `%s` → %s 使用 %s", verb, trig, verb, action)
}

// BindingFromMetadata reconstructs a ProceduralBinding from a persisted procedural unit.
func BindingFromMetadata(meta map[string]any, contentFallback string) (ProceduralBinding, bool) {
	if UnitKindFromMetadata(meta) != KindProcedural {
		return ProceduralBinding{}, false
	}
	if st, _ := meta[MetaProceduralStatus].(string); st != "" && st != ProceduralStatusActive {
		return ProceduralBinding{}, false
	}
	b := ProceduralBinding{
		TriggerCode:  strings.TrimSpace(metaString(meta, "binding_trigger_code")),
		TriggerQuery: strings.TrimSpace(metaString(meta, "binding_trigger_query")),
		ActionKind:   strings.TrimSpace(metaString(meta, "binding_action_kind")),
		SkillID:      strings.TrimSpace(metaString(meta, "binding_skill_id")),
		Mode:         strings.TrimSpace(metaString(meta, "binding_mode")),
		AgentID:      strings.TrimSpace(metaString(meta, "agent_id")),
	}
	if b.AgentID == "" {
		b.AgentID = strings.TrimSpace(metaString(meta, "binding_agent_id"))
	}
	if names, ok := meta["binding_tool_names"].([]string); ok {
		b.ToolNames = names
	} else if raw, ok := meta["binding_tool_names"].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				b.ToolNames = append(b.ToolNames, s)
			}
		}
	}
	if b.TriggerCode == "" && b.TriggerQuery == "" {
		if code := strings.TrimSpace(metaString(meta, MetaFailureCode)); code != "" {
			b.TriggerCode = code
		}
	}
	if b.ActionKind == "" {
		return ProceduralBinding{}, false
	}
	vb, err := ValidateProceduralBinding(b, nil)
	if err != nil {
		_ = contentFallback
		return ProceduralBinding{}, false
	}
	return vb, true
}

// MergeProceduralBindings dedupes by EntryIDForBinding, preferring earlier entries.
func MergeProceduralBindings(sets ...[]ProceduralBinding) []ProceduralBinding {
	seen := map[string]struct{}{}
	var out []ProceduralBinding
	for _, set := range sets {
		for _, b := range set {
			id := EntryIDForBinding(b)
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, b)
		}
	}
	return out
}
