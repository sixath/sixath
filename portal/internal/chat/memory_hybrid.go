package chat

import (
	"context"
	"strings"
)

// hybridRecallGate resolves the per-agent hybrid_recall runtime tool flag.
// Fail-open: nil getter / empty agentID / lookup error / unset field → true.
// When the captured agents is nil, falls back to the live globalMemoryAgentGetter
// (same pattern as dynamicUnitEmbedder.Embed) so a store built before
// SetMemoryAgentGetter still observes a later-injected getter.
func hybridRecallGate(agents AgentGetter) func(context.Context, string) bool {
	return func(ctx context.Context, agentID string) bool {
		if strings.TrimSpace(agentID) == "" {
			return true
		}
		getter := globalMemoryAgentGetter
		if agents != nil {
			getter = agents
		}
		if getter == nil {
			return true
		}
		meta, err := getter.Get(ctx, agentID)
		if err != nil || meta == nil {
			return true
		}
		if meta.RuntimeTools.HybridRecall == nil {
			return true
		}
		return *meta.RuntimeTools.HybridRecall
	}
}
