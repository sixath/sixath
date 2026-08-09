package config

// ContextCompression 全局上下文压缩 / L2 配置（设计 §5.4、§5.6）；默认全零等价关闭 L2。
// 实际装配 auxiliary 模型需由上层用 model.NewFromIdentifier(L2AuxiliaryModel) 等创建后传入 agent.WithReActContextCompression。
type ContextCompression struct {
	L2Enabled                bool    `json:"l2_enabled" yaml:"l2_enabled"`
	L2AuxiliaryModel         string  `json:"l2_auxiliary_model" yaml:"l2_auxiliary_model"`
	SoftTokenEstimate        int     `json:"soft_token_estimate" yaml:"soft_token_estimate"`
	MaxConsecutiveFailures   int     `json:"max_consecutive_failures" yaml:"max_consecutive_failures"`
	CooldownSec              int     `json:"cooldown_sec" yaml:"cooldown_sec"`
	EstimateAlpha            float64 `json:"estimate_alpha" yaml:"estimate_alpha"`
	ToolContentPrePruneRunes int     `json:"tool_content_pre_prune_runes" yaml:"tool_content_pre_prune_runes"`
}
