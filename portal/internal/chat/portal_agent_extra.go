package chat

import (
	"errors"
	"strings"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/config"
)

var (
	globalToolGuardrails   *agent.ToolGuardrailsConfig
	guardrailHaltDisplay   = "banner"
	guardrailHaltPersist   bool
	defaultGuardrailBanner = "对话已因工具护栏中止：重复失败超过阈值。请换一个问法或稍后再试。"

	streamMemoryFenceScrub bool
	streamMemoryFenceTag   string
)

// SetGlobalToolGuardrails 设置全局 ReAct 护栏（可由 framework config 或 agent_extra 加载）；nil 关闭。
func SetGlobalToolGuardrails(c *agent.ToolGuardrailsConfig) {
	globalToolGuardrails = c
}

// SetPortalAgentExtra 从 agent_extra.yaml 应用 tool_guardrails、记忆预取 Orchestrator 与 Portal 展示策略。
func SetPortalAgentExtra(extra *config.PortalAgentExtra) {
	if extra == nil {
		return
	}
	config.NormalizePortalAgentExtra(extra)
	if extra.ToolGuardrails != nil {
		SetGlobalToolGuardrails(agent.ToolGuardrailsFromConfig(extra.ToolGuardrails))
	}
	if extra.MemoryStore != nil && extra.MemoryStore.AgentWorkspace != nil && extra.MemoryStore.AgentWorkspace.WriteEnabled {
		f := DefaultHermesP0ToolFlags
		f.MemoryWriteEnabled = true
		SetHermesP0ToolFlags(f)
	}
	if extra.MemoryOrchestratorPrefetch != nil {
		cp := *extra.MemoryOrchestratorPrefetch
		storedPrefetchYAML = &cp
		streamMemoryFenceScrub = cp.StreamScrub
		streamMemoryFenceTag = cp.FenceTag
	} else {
		storedPrefetchYAML = nil
		streamMemoryFenceScrub = false
		streamMemoryFenceTag = ""
	}
	if extra.MemoryExtraction != nil {
		SetMemoryExtractionConfig(extra.MemoryExtraction)
	} else {
		SetMemoryExtractionConfig(nil)
	}
	if extra.MemoryConflict != nil {
		SetMemoryConflictConfig(extra.MemoryConflict)
	} else {
		SetMemoryConflictConfig(nil)
	}
	if extra.MemoryVector != nil {
		SetMemoryVectorConfig(extra.MemoryVector)
	} else {
		SetMemoryVectorConfig(nil)
	}
	if extra.MemoryGraph != nil {
		SetMemoryGraphConfig(extra.MemoryGraph)
	} else {
		SetMemoryGraphConfig(nil)
	}
	if extra.MemoryProceduralRepair != nil {
		SetProceduralRepairConfig(extra.MemoryProceduralRepair)
	} else {
		SetProceduralRepairConfig(nil)
	}
	RebuildPrefetchMemoryOrchestrator()
	if extra.Portal != nil && extra.Portal.GuardrailHalt != nil {
		g := extra.Portal.GuardrailHalt
		if s := strings.TrimSpace(strings.ToLower(g.Display)); s != "" {
			guardrailHaltDisplay = s
		}
		guardrailHaltPersist = g.PersistSystemMessage
	}
}

// DecomposeGuardrailRunError 解析护栏硬停错误与 Portal 展示策略。
// 若 returnRawErr 为 true，调用方应继续返回原始 err（含 none 策略与非护栏错误）。
func DecomposeGuardrailRunError(err error) (isGuardrailHalt bool, userVisible string, persist bool, returnRawErr bool) {
	var re *agent.RunError
	if err == nil || !errors.As(err, &re) || re.Trace == nil || !re.Trace.GuardrailHalt {
		return false, "", false, true
	}
	switch guardrailHaltDisplay {
	case "none":
		return true, "", false, true
	case "full":
		if re.Trace.GuardrailHaltMessage != nil && strings.TrimSpace(re.Trace.GuardrailHaltMessage.Content) != "" {
			return true, re.Trace.GuardrailHaltMessage.Content, guardrailHaltPersist, false
		}
		fallthrough
	case "brief":
		return true, "[护栏] 本回合已中止。", guardrailHaltPersist, false
	default:
		return true, defaultGuardrailBanner, guardrailHaltPersist, false
	}
}

// StreamMemoryFenceScrubEnabled 表示 agent_extra 中 memory_orchestrator_prefetch.stream_scrub 已启用。
func StreamMemoryFenceScrubEnabled() bool { return streamMemoryFenceScrub }

// StreamMemoryFenceScrubTag 返回围栏标签（空则 scrub 器使用默认 sixath-memory-context）。
func StreamMemoryFenceScrubTag() string { return streamMemoryFenceTag }
