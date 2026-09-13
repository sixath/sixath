package chat

import (
	"strings"

	"github.com/sixath/framework/model"
)

// tokenCounters 按 "provider/model" 维护已校准的 token 计数器，进程内共享：
// 同一模型在不同会话/Agent 上的 token 特征一致，复用同一实例收敛更快。
//
// 背景见 docs/superpowers/plans/2026-09-12-maturity-hardening.md Task 4：
// 上下文压缩此前用固定系数（码点 × 1.35）估算 token，属开环估计；接入本注册表后，
// provider 返回的真实 usage 会自动回填校准，使压缩触发点与成本口径可收敛、可观测。
var tokenCounters = model.NewTokenCounterRegistry()

// TokenCounterKey 归一化计数器 key；provider 小写、去空白。两者都为空时返回 "default"。
func TokenCounterKey(provider, modelName string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	m := strings.TrimSpace(modelName)
	switch {
	case p == "" && m == "":
		return "default"
	case p == "":
		return m
	case m == "":
		return p
	default:
		return p + "/" + m
	}
}

// TokenCounterFor 返回该模型对应的校准计数器（首次访问时创建）。
// 通过 ReActOptionsFromAgent → agent.WithReActTokenCounter 注入后，
// 每次模型调用返回的 usage 都会回填校准（见 model.ObserveTokenUsage）。
func TokenCounterFor(provider, modelName string) model.TokenCounter {
	return tokenCounters.Get(TokenCounterKey(provider, modelName))
}

// TokenCounterStats 返回各模型的校准快照，用于运维/指标确认 alpha 是否收敛
// （samples 增长且 alpha 稳定即代表校准生效）。
func TokenCounterStats() map[string]model.TokenCounterStats {
	return tokenCounters.Snapshot()
}
