package growth

import "context"

// StubRunner v1 无 LLM：可选触发记忆脏通知并清 pending 标志。字段由 portal 注入。
// 若未来在本 runner 内再调 ReAct，须对 agent.Request.Metadata 使用 MetaGrowthReview，避免递归计数（spec §4.3）。
type StubRunner struct {
	MemoryNotify       func(ctx context.Context, sessionID string)
	ClearGrowthPending func(ctx context.Context, sessionID string, clearSkill, clearMemory bool) error
}

// Run 若 PendingMemory 且 MemoryNotify 非空则调用；随后清除全部 pending（与一期 stub 语义一致）。
func (s *StubRunner) Run(ctx context.Context, job ReviewJob) error {
	if s == nil {
		return nil
	}
	if job.PendingMemory && s.MemoryNotify != nil {
		s.MemoryNotify(ctx, job.SessionID)
	}
	if s.ClearGrowthPending != nil {
		return s.ClearGrowthPending(ctx, job.SessionID, true, true)
	}
	return nil
}
