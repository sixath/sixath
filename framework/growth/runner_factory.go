package growth

import "context"

// RunnerDeps 注入 Stub / noop-LLM / 技能复盘路径的 portal 回调。
type RunnerDeps struct {
	MemoryNotify func(ctx context.Context, sessionID string)
	// ClearGrowthPending 按位清除 pending；stub 路径传 (true,true)。
	ClearGrowthPending func(ctx context.Context, sessionID string, clearSkill, clearMemory bool) error
	// Transcript 拉取会话 Markdown（与 portal ChatTranscriptProvider 对齐）；SkillReviewRunner 在 PendingSkill 时使用。
	Transcript func(ctx context.Context, sessionID string) (string, error)
	// MemoryState 拉取记忆子系统状态摘要（B1）；返回字符串注入复盘 prompt；nil/error 时跳过且不阻断 run。
	// 典型实现：portal 适配 memorysearch.Manager 输出最近一次同步时间、dirty 计数、可选 top-N 关键词。
	MemoryState func(ctx context.Context, sessionID string) (string, error)
	// ProposeSkillPatches 产出待应用补丁；为 nil 且 llm_review_enabled 时走 NoopLLMRunner。
	ProposeSkillPatches func(ctx context.Context, job ReviewJob, transcript, skillsSummary string) ([]Patch, error)
	// ProposeCombinedReview 当 PendingSkill+PendingMemory 同时存在时，合并为单次 LLM 调用，产出补丁 + 是否触发记忆通知。
	// 为 nil 时回退到顺序调用 ProposeSkillPatches → MemoryNotify（两次独立调用）。
	ProposeCombinedReview func(ctx context.Context, job ReviewJob, transcript, skillsSummary string) (patches []Patch, notifyMemory bool, err error)
	// InvalidateSkillsCache 在 ApplyPatchBatch 成功后调用，用于通知 portal 层刷新技能索引缓存（当前 BuildSkillsIndex 每次重新扫描，预留此钩子供未来缓存层使用）。
	InvalidateSkillsCache func(ctx context.Context, workspace string)
	// RewriteCronSkillRefs 在技能 frontmatter 改名后反写 cron skill_execute（R2c）；nil 时跳过。
	RewriteCronSkillRefs CronSkillRefRewriter
	// SpawnReviewAgent 在 workspace 内 fork 一个瘦身复盘 agent，用 skillops 工具自主演化技能库
	// （fork-agent 路径，spec §4.1）。由 portal 注入（唯一同时依赖 framework/agent 的层）。
	// 实现负责：构造 ReActAgent（瘦身工具集 + 递归保护：不设 ToolSuccessHook）、注入 workspace_root 到 ctx、
	// Run，以及 Run 成功后的 InvalidateSkillsCache / ClearGrowthPending 收尾。
	// 为 nil 时 AgentReviewRunner 直接降级到 SkillReviewRunner。
	SpawnReviewAgent func(ctx context.Context, job ReviewJob, transcript, skillsSummary string) error
}

// NoopLLMRunner LLM 复盘骨架：无 ProposeSkillPatches 时行为与 StubRunner 一致。
type NoopLLMRunner struct {
	stub *StubRunner
}

// Run 委托 StubRunner；后续可插入真 LLM。
func (n *NoopLLMRunner) Run(ctx context.Context, job ReviewJob) error {
	if n == nil || n.stub == nil {
		return nil
	}
	return n.stub.Run(ctx, job)
}

// RunnerSelect 控制 NewRunner 的路径选择。
type RunnerSelect struct {
	// LLMReviewEnabled 关闭时一律返回 StubRunner。
	LLMReviewEnabled bool
	// AgentReviewEnabled 为 true 且 deps.SpawnReviewAgent!=nil 时选 fork-agent 路径。
	AgentReviewEnabled bool
}

// NewRunner 按配置选择 Runner：
// - LLMReviewEnabled=false → StubRunner；
// - AgentReviewEnabled 且 SpawnReviewAgent!=nil → AgentReviewRunner（fork-agent，失败降级 SkillReviewRunner）；
// - ProposeSkillPatches!=nil → SkillReviewRunner（单次 LLM patch）；
// - 否则 → NoopLLMRunner。
func NewRunner(sel RunnerSelect, deps RunnerDeps) Runner {
	if !sel.LLMReviewEnabled {
		return &StubRunner{
			MemoryNotify:       deps.MemoryNotify,
			ClearGrowthPending: deps.ClearGrowthPending,
		}
	}
	if sel.AgentReviewEnabled && deps.SpawnReviewAgent != nil {
		return &AgentReviewRunner{
			deps:     deps,
			fallback: &SkillReviewRunner{deps: deps},
		}
	}
	if deps.ProposeSkillPatches != nil {
		return &SkillReviewRunner{deps: deps}
	}
	return &NoopLLMRunner{stub: &StubRunner{
		MemoryNotify:       deps.MemoryNotify,
		ClearGrowthPending: deps.ClearGrowthPending,
	}}
}
