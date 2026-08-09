package chat

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/skills"
)

// SkillRouteSettings 控制用户消息 → 技能自动匹配与预注入。
type SkillRouteSettings struct {
	Enabled      bool
	MinScore     int
	MaxBodyRunes int
}

// DefaultSkillRouteSettings 默认开启路由，阈值 5，正文最多 12000 runes。
var DefaultSkillRouteSettings = SkillRouteSettings{
	Enabled:      true,
	MinScore:     0,
	MaxBodyRunes: 12000,
}

var skillRouteSettings = DefaultSkillRouteSettings

// SetSkillRouteSettings 由 main 在加载 config 后调用。
func SetSkillRouteSettings(s SkillRouteSettings) {
	skillRouteSettings = s
}

// SkillRouteEnabled 是否启用自动路由。
func SkillRouteEnabled() bool {
	return skillRouteSettings.Enabled
}

// BuildEffectiveSystemPromptForTurn 在基础 system prompt 上，按本轮 userQuery 预注入最佳匹配 Skill 正文。
func BuildEffectiveSystemPromptForTurn(userPrompt string, skillsIdx *skills.Index, userQuery string) string {
	return BuildEffectiveSystemPromptForTurnScoped(userPrompt, skillsIdx, userQuery, "", "")
}

// BuildEffectiveSystemPromptForTurnScoped also merges persisted procedural units for the session (P3-E).
func BuildEffectiveSystemPromptForTurnScoped(userPrompt string, skillsIdx *skills.Index, userQuery, agentID, sessionID string) string {
	base := BuildEffectiveSystemPrompt(userPrompt, skillsIdx)
	userQuery = strings.TrimSpace(userQuery)
	base = appendProceduralBindingHints(base, userQuery, skillsIdx, agentID, sessionID)
	if !skillRouteSettings.Enabled || skillsIdx == nil {
		return base
	}
	if userQuery == "" {
		return base
	}
	match, ok := skills.RouteBest(userQuery, skillsIdx.All(), skills.RouteOptions{
		MinScore: skillRouteSettings.MinScore,
	})
	if !ok {
		return base
	}
	body, err := skillsIdx.LoadSkillBody(match.Name)
	if err != nil || strings.TrimSpace(body) == "" {
		return base
	}
	maxRunes := skillRouteSettings.MaxBodyRunes
	if maxRunes <= 0 {
		maxRunes = DefaultSkillRouteSettings.MaxBodyRunes
	}
	body = truncateRunes(body, maxRunes)
	block := fmt.Sprintf(
		"【已自动匹配 Skill: %s（得分 %d）】\n"+
			"本轮用户问题与该技能高度相关。以下内容来自 SKILL.md，请**优先遵循**此工作流；"+
			"无需再调用 load_skill 加载该技能（除非需要 read_skill_file 或 execute_skill_script）。\n\n%s",
		match.Name, match.Score, body,
	)
	if base == "" {
		return block
	}
	return base + "\n\n---\n\n" + block
}

func appendProceduralBindingHints(base, userQuery string, skillsIdx *skills.Index, agentID, sessionID string) string {
	var bindings []memory.ProceduralBinding
	var cat *memory.ProceduralCatalog
	if sessionID != "" {
		bindings, cat = ProceduralBindingsForSkillRouterTurn(agentID, sessionID)
	} else {
		bindings, cat = ProceduralBindingsForSkillRouter()
	}
	if len(bindings) == 0 || userQuery == "" {
		return base
	}
	matched := memory.MatchProceduralBindings(bindings, agentID, userQuery, nil)
	var blocks []string
	var hit []memory.ProceduralBinding
	for _, b := range matched {
		if b.ActionKind == memory.BindingActionSkill && b.SkillID != "" && skillsIdx != nil {
			if _, err := skillsIdx.LoadSkillBody(b.SkillID); err != nil {
				continue // skill missing — skip suggest
			}
		}
		blocks = append(blocks, memory.FormatBindingSuggest(b))
		hit = append(hit, b)
	}
	if cat != nil && len(hit) > 0 {
		cat.RecordHit(memory.ProceduralHitRouter, hit)
	}
	if len(blocks) == 0 {
		return base
	}
	hint := strings.Join(blocks, "\n")
	if base == "" {
		return hint
	}
	return base + "\n\n---\n\n" + hint
}

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "\n\n…(SKILL 正文已截断)"
}
