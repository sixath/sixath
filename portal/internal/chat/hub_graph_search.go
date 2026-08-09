package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/memory/hub/local"
	"github.com/sixath/framework/tool"
)

// neo4jHubGraphSearcher adapts the shared Neo4j GraphStore to Hub knowledge_search (source=graph).
// Resolves the store lazily so it works even when InitLocalMemoryHub ran before SetMemoryGraphConfig.
type neo4jHubGraphSearcher struct{}

func (neo4jHubGraphSearcher) Search(ctx context.Context, query string, limit int) ([]local.KnowledgeHit, error) {
	if !memoryGraphEnabled() || memoryGraphProvider() != "neo4j" {
		return nil, nil
	}
	g := sharedNeo4jGraphStore()
	if g == nil {
		return nil, nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}

	sessionID := contextString(ctx, tool.ContextKeySessionID)
	userID := contextString(ctx, tool.ContextKeyUserID)
	agentID := contextString(ctx, tool.ContextKeyAgentID)

	type scopeAttempt struct {
		scope   memory.Scope
		scopeID string
	}
	var attempts []scopeAttempt
	if sessionID != "" {
		attempts = append(attempts, scopeAttempt{memory.ScopeSession, sessionID})
	}
	if userID != "" {
		attempts = append(attempts, scopeAttempt{memory.ScopeUser, userID})
	}
	if agentID != "" {
		attempts = append(attempts, scopeAttempt{memory.ScopeAgent, agentID})
	}
	if len(attempts) == 0 {
		return nil, nil
	}

	hops := memoryGraphMaxHops()
	seen := map[string]struct{}{}
	var hits []local.KnowledgeHit
	for _, a := range attempts {
		seeds, err := g.MatchSeeds(ctx, a.scope, a.scopeID, query, limit)
		if err != nil {
			return nil, err
		}
		if len(seeds) == 0 {
			continue
		}
		expanded, err := g.Expand(ctx, memory.GraphExpandQuery{
			Scope:         a.scope,
			ScopeID:       a.scopeID,
			SeedEntityIDs: seeds,
			Hops:          hops,
			Limit:         limit,
		})
		if err != nil {
			return nil, err
		}
		// Include seed names even when Expand is empty (isolated nodes).
		if len(expanded) == 0 {
			for _, sid := range seeds {
				if _, ok := seen[sid]; ok {
					continue
				}
				seen[sid] = struct{}{}
				hits = append(hits, local.KnowledgeHit{
					ID:      sid,
					Source:  "graph",
					Content: fmt.Sprintf("seed %s scope=%s/%s", sid, a.scope, a.scopeID),
					Score:   0.5,
				})
				if len(hits) >= limit {
					return hits, nil
				}
			}
			continue
		}
		for _, gh := range expanded {
			if gh.EntityID == "" {
				continue
			}
			if _, ok := seen[gh.EntityID]; ok {
				continue
			}
			seen[gh.EntityID] = struct{}{}
			unit := ""
			if len(gh.RelatedUnitIDs) > 0 {
				unit = gh.RelatedUnitIDs[0]
			}
			content := gh.Name
			if content == "" {
				content = gh.EntityID
			}
			if unit != "" {
				content = fmt.Sprintf("%s (unit=%s)", content, unit)
			}
			content = fmt.Sprintf("%s [scope=%s/%s]", content, a.scope, a.scopeID)
			hits = append(hits, local.KnowledgeHit{
				ID:      gh.EntityID,
				Source:  "graph",
				Content: content,
				Score:   gh.Score,
			})
			if len(hits) >= limit {
				return hits, nil
			}
		}
	}
	return hits, nil
}

var _ local.GraphSearcher = neo4jHubGraphSearcher{}
