package growth

import "context"

// ReviewJob 描述一次待执行的成长复盘任务（portal worker 在抢租约后组装）。
type ReviewJob struct {
	SessionID     string
	WorkspaceKey  string // 与租约表 workspace_key 一致；当前实现下等于 agents.workspace 路径
	// WorkspaceRoot 为 agent 工作区根目录（用于 BuildSkillsIndex / ApplyPatchBatch）；通常与 WorkspaceKey 相同。
	WorkspaceRoot string
	PendingSkill  bool
	PendingMemory bool
	// LearningsSummary 来自 workspace/.learnings 或上级目录（portal 在启用 learnings_review 时填充）。
	LearningsSummary string
}

// Runner 执行单次复盘（首期可为 Stub，Task11 再换 LLM 实现）。
type Runner interface {
	Run(ctx context.Context, job ReviewJob) error
}
