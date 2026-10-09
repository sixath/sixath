package service

import (
	"context"

	"github.com/go-kratos/kratos/v2/log"
)

// HandbookSkillDirResolver returns the code-map and handbook skill dirs of an agent;
// nil when the agent has no repo bindings.
type HandbookSkillDirResolver interface {
	SkillDirsForAgent(ctx context.Context, agentID string) ([]string, error)
}

// appendHandbookDirs fails open: on error it still appends whatever dirs were resolved.
func appendHandbookDirs(ctx context.Context, r HandbookSkillDirResolver, agentID string, dirs []string, logger *log.Helper) []string {
	if r == nil || agentID == "" {
		return dirs
	}
	extra, err := r.SkillDirsForAgent(ctx, agentID)
	if err != nil {
		logger.Warnf("resolve handbook skill dirs for agent %s: %v", agentID, err)
	}
	return append(dirs, extra...)
}
