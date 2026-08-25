package chat

import (
	"context"
	"time"

	"backend/internal/biz"

	"github.com/sixath/framework/model"
)

// PrepareTurnToolSurface resolves per-turn active tool families for registry/runtime/gate filtering.
// When SATH_TURN_TOOL_SURFACE is off, returns nil active (no filter) and Source "disabled".
func PrepareTurnToolSurface(
	ctx context.Context,
	userText string,
	tools []*biz.ToolMeta,
	servers []*biz.McpServerMeta,
	agentMeta *biz.AgentMeta,
	m model.Model,
) (active map[string]struct{}, result IntentResolveResult) {
	if !ToolSurfaceEnabled() {
		return nil, IntentResolveResult{
			Source: "disabled",
			Reason: "SATH_TURN_TOOL_SURFACE off",
		}
	}
	flags := RuntimeToolsForAgent(agentMeta)
	// Include knowledge in BoundFamilies when surface is on; RegisterAgentRuntimeTools still
	// gates actual registration via FamilyActive + ResolveForRuntimeTools.
	knowledgeOn := true
	bound := BoundFamiliesFrom(tools, servers, flags.WebToolsEnabled, knowledgeOn)
	if ToolFamilySplitEnabled() {
		bound = mergeFamilyIDs(bound, FamilySkills, FamilyMemory)
	}
	resolver := IntentResolver{
		Classifier: ModelFamilyClassifier{Model: m, Timeout: 3 * time.Second},
	}
	res := resolver.Resolve(ctx, IntentResolveInput{
		UserText:      userText,
		BoundFamilies: bound,
		Servers:       servers,
	})
	return familySet(res.ActiveFamilies), res
}
