package model

import "context"

// ContextTraceFunc 在上下文变换（L0/L1/L2、strip 孤儿 tool 等）发生时回调，供 agent 聚合到 RunTrace。
// kind 取值示例："l1_sanitize"、"snip_compact"、"l2_pre_prune_tool"、"l0_compress"、"strip_orphan_tools"、"l2_summarize"、"l2_cooldown_skip"、"l2_cooldown_enter"。
type ContextTraceFunc func(kind string, detail map[string]any)

// TraceSink 为产品 §6.5 O2 / 设计 §8.1 命名的可观测回调面，与 ContextTraceFunc 等价。
// 实现应放在 agent 等上层包；model 不依赖 agent，避免循环引用。
type TraceSink = ContextTraceFunc

// PrepareChatContext 在进入网关请求前对 messages 执行与 OpenAIClient 一致的变换（无 ctx 时使用 Background）。
func PrepareChatContext(messages []Message, callCfg *CallConfig) []Message {
	return PrepareChatContextCtx(context.Background(), messages, callCfg)
}

// PrepareChatContextCtx 与设计 §5.3 顺序对齐：L1 → L2 预剪枝（可选）→ code pin → L0 → strip → L2 摘要（可选）。
func PrepareChatContextCtx(ctx context.Context, messages []Message, callCfg *CallConfig) []Message {
	out := messages
	var tracef ContextTraceFunc
	if callCfg != nil {
		tracef = callCfg.ContextTrace
	}
	if len(out) == 0 {
		return out
	}
	out, nL1 := ApplyL1SanitizeToMessages(out)
	if tracef != nil && nL1 > 0 {
		tracef("l1_sanitize", map[string]any{"messages_touched": nL1})
	}
	if callCfg != nil && callCfg.SnipCompactEnabled {
		beforeLen := len(out)
		out2, snipRemoved := SnipCompactMessages(out)
		if snipRemoved > 0 {
			out = out2
			if tracef != nil {
				tracef("snip_compact", map[string]any{"messages_removed": beforeLen - len(out)})
			}
		}
	}
	prePrune := 0
	if callCfg != nil && callCfg.L2 != nil {
		prePrune = callCfg.L2.PrePruneToolRunes()
	}
	if prePrune > 0 {
		br := totalMessageRunes(out)
		out2 := pruneToolMessageBodies(out, prePrune)
		ar := totalMessageRunes(out2)
		if tracef != nil && ar < br {
			tracef("l2_pre_prune_tool", map[string]any{"max_runes_each": prePrune, "runes_removed": br - ar})
		}
		out = out2
	}
	out = ensureCodePinMessages(out)
	if callCfg != nil && callCfg.MaxContextRunes > 0 {
		before := len(out)
		out = CompressMessagesByRunesBudget(out, callCfg.MaxContextRunes)
		if tracef != nil && len(out) < before {
			tracef("l0_compress", map[string]any{"messages_removed": before - len(out)})
		}
	}
	if callCfg != nil && callCfg.MaxContextTokensSoft > 0 {
		alpha := callCfg.TokenEstimateAlpha
		if alpha <= 0 {
			alpha = DefaultTokenEstimateAlpha
		}
		est := EstimateTokensConservative(out, alpha)
		if est > callCfg.MaxContextTokensSoft {
			// 将 token 阈值映射到近似 rune 预算，复用既有 L0 裁剪策略（按 user block + tool 原子链）。
			budgetRunes := int(float64(callCfg.MaxContextTokensSoft) / alpha)
			if budgetRunes <= 0 {
				budgetRunes = 1
			}
			before := len(out)
			out2 := CompressMessagesByRunesBudget(out, budgetRunes)
			if len(out2) < before {
				out = out2
				if tracef != nil {
					tracef("l0_compress_tokens", map[string]any{
						"messages_removed": before - len(out),
						"token_estimate":   est,
						"soft_limit":       callCfg.MaxContextTokensSoft,
						"alpha":            alpha,
					})
				}
			}
		}
	}
	beforeStrip := len(out)
	out = stripLeadingOrphanToolsAfterSystem(out)
	if tracef != nil && len(out) < beforeStrip {
		tracef("strip_orphan_tools", map[string]any{"messages_removed": beforeStrip - len(out)})
	}
	if callCfg != nil && callCfg.L2 != nil && ctx != nil {
		out = callCfg.L2.MaybeSummarize(ctx, out, tracef)
	}
	return out
}
