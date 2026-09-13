package tool

import "testing"

// 风险登记表与串行表的交叉审计。
//
// 目的：让「新增一个会改机器/远端状态的工具」这件事无法悄悄溜过——
// 要么进 builtinRequiresSequential，要么显式写进下面的例外表并说明理由。
func TestRiskLevels_CrossAuditWithSequentialTable(t *testing.T) {
	// 例外 A：写操作但天然并发安全（各自有存储级并发控制，并行不会交错）。
	writeButParallelSafe := map[string]bool{
		"memory_remember": true, // MemoryStore facade 自身处理并发
	}
	// 例外 B：外呼类风险但不共享会话态，因此不需要串行。
	networkParallelSafe := map[string]bool{
		"http_request": true, "web_search": true, "web_extract": true,
		"jaeger_trace": true, "es_log_query": true,
	}

	for name, level := range builtinRiskLevels {
		if level == RiskUnknown {
			continue
		}
		switch level {
		case RiskDestructive:
			// 破坏性工具执行任意命令、共享机器状态：必须串行。
			if !builtinRequiresSequential[name] {
				t.Fatalf("destructive tool %q must be in builtinRequiresSequential", name)
			}
		case RiskWrite:
			if writeButParallelSafe[name] {
				continue
			}
			if !builtinRequiresSequential[name] {
				t.Fatalf("write tool %q must be serialized (or be added to writeButParallelSafe with a reason)", name)
			}
		case RiskNetwork:
			if networkParallelSafe[name] {
				continue
			}
			if !builtinRequiresSequential[name] {
				t.Fatalf("network tool %q shares session state; serialize it or add it to networkParallelSafe", name)
			}
		case RiskRead:
			if builtinRequiresSequential[name] {
				// 交互型只读工具（ask_user）需要串行以保证会话态一致；其它只读工具不应被串行化。
				if name != "ask_user" {
					t.Fatalf("read-only tool %q should not be serialized (hurts parallel throughput)", name)
				}
			}
		}
	}

	// 反向：串行表里的每个工具都必须有明确的风险登记（不能是 unknown）。
	for name := range builtinRequiresSequential {
		if RiskLevelOf(name) == RiskUnknown {
			t.Fatalf("serialized tool %q has no risk classification; add it to builtinRiskLevels", name)
		}
	}
}

func TestRiskLevelOf_UnknownToolsAreUnknown(t *testing.T) {
	if got := RiskLevelOf("some_mcp_dynamic_tool"); got != RiskUnknown {
		t.Fatalf("unregistered tool risk=%q want %q", got, RiskUnknown)
	}
	if got := RiskLevelOf("terminal"); got != RiskDestructive {
		t.Fatalf("terminal risk=%q want destructive", got)
	}
	if got := RiskLevelOf("read_file"); got != RiskRead {
		t.Fatalf("read_file risk=%q want read", got)
	}
}
