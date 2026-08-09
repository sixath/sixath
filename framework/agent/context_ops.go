package agent

import "github.com/sixath/framework/model"

func ensureContextOps(trace *RunTrace) {
	if trace.ContextOps == nil {
		trace.ContextOps = &ContextOpsTrace{}
	}
}

// lastContextOpsInvocation 返回当前 Run 内最近一次 beginModelInvocation 对应的记录（供 PrepareChatContext 回调写入）。
func lastContextOpsInvocation(trace *RunTrace) *ContextOpsInvocation {
	if trace == nil || trace.ContextOps == nil {
		return nil
	}
	n := len(trace.ContextOps.Invocations)
	if n == 0 {
		return nil
	}
	return &trace.ContextOps.Invocations[n-1]
}

// beginModelInvocation 在每次调用底层 Model 前记录一次 invocation（设计 §5.3.2）。
func beginModelInvocation(trace *RunTrace, mode string) {
	if trace == nil {
		return
	}
	ensureContextOps(trace)
	idx := trace.invocationSeq
	trace.invocationSeq++
	trace.ContextOps.Invocations = append(trace.ContextOps.Invocations, ContextOpsInvocation{
		Index: idx,
		Mode:  mode,
	})
}

func contextTraceMerge(trace *RunTrace) model.ContextTraceFunc {
	return func(kind string, detail map[string]any) {
		if trace == nil {
			return
		}
		ensureContextOps(trace)
		inv := lastContextOpsInvocation(trace)
		switch kind {
		case "l1_sanitize":
			if n, ok := detail["messages_touched"].(int); ok && n > 0 {
				trace.ContextOps.SanitizeApplied = true
				if inv != nil {
					inv.SanitizeApplied = true
				}
			}
		case "l0_compress":
			if n, ok := detail["messages_removed"].(int); ok {
				trace.ContextOps.L0DroppedMessages += n
				if inv != nil {
					inv.L0DroppedMessages += n
				}
			}
		case "l0_compress_tokens":
			if n, ok := detail["messages_removed"].(int); ok {
				trace.ContextOps.L0DroppedMessages += n
				if inv != nil {
					inv.L0DroppedMessages += n
				}
			}
		case "strip_orphan_tools":
			if n, ok := detail["messages_removed"].(int); ok {
				trace.ContextOps.StripOrphanTools += n
				if inv != nil {
					inv.StripOrphanTools += n
				}
			}
		case "snip_compact":
			if n, ok := detail["messages_removed"].(int); ok {
				trace.ContextOps.SnipCompactRemoved += n
				if inv != nil {
					inv.SnipCompactRemoved += n
				}
			}
		case "l2_pre_prune_tool":
			if n, ok := detail["runes_removed"].(int); ok && n > 0 {
				trace.ContextOps.L2PrePruneRunesRemoved += n
				if inv != nil {
					inv.L2ToolPrePruneRunesRemoved += n
				}
			}
		case "l2_summarize":
			trace.ContextOps.L2Used = true
			trace.ContextOps.L2InvocationCount++
			if h, ok := detail["summary_hash"].(string); ok && h != "" {
				trace.ContextOps.L2SummaryHash = h
				trace.ContextOps.L2SummaryHashes = append(trace.ContextOps.L2SummaryHashes, h)
				if inv != nil {
					inv.L2SummaryHash = h
				}
			}
			if txt, ok := detail["summary_text"].(string); ok && txt != "" {
				trace.LastL2Summary = txt
			}
			if n, ok := detail["middle_removed"].(int); ok && n > 0 {
				trace.LastL2MiddleRemoved = n
			}
			if inv != nil {
				inv.L2Used = true
			}
		case "l2_cooldown_skip":
			trace.ContextOps.L2CooldownActive = true
		case "l2_cooldown_enter":
			trace.ContextOps.L2CooldownActive = true
		}
	}
}
