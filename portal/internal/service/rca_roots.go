package service

import (
	"context"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/sixath/framework/tool"
)

// RCARootResolver returns the RCA roots of an agent's bound repositories. A nil result
// means the agent has no repo bindings and the legacy workspace/code link applies; a
// non-nil (possibly empty) result is authoritative and must be passed through unchanged.
type RCARootResolver interface {
	RCARootsForAgent(ctx context.Context, agentID string) ([]tool.RCARoot, error)
}

// resolveRCARoots fails open: lookup errors fall back to the legacy roots.
func resolveRCARoots(ctx context.Context, r RCARootResolver, agentID string, logger *log.Helper) []tool.RCARoot {
	if r == nil || agentID == "" {
		return nil
	}
	roots, err := r.RCARootsForAgent(ctx, agentID)
	if err != nil {
		logger.Warnf("resolve rca roots for agent %s: %v", agentID, err)
		return nil
	}
	return roots
}
