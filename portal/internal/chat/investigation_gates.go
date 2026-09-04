package chat

import (
	"os"
	"strings"

	"backend/internal/conf"
)

const taskLockEnv = "SATH_TASK_LOCK"

var taskLockOverride *bool

func SetTaskLockEnabled(enabled bool) {
	v := enabled
	taskLockOverride = &v
}

func resetTaskLockOverride() { taskLockOverride = nil }

func TaskLockEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(taskLockEnv)))
	if v != "" {
		return !(v == "0" || v == "false" || v == "off" || v == "no")
	}
	if taskLockOverride != nil {
		return *taskLockOverride
	}
	return true
}

func envSet(key string) bool {
	return strings.TrimSpace(os.Getenv(key)) != ""
}

// ApplyInvestigationGates sets process overrides. Caller must invoke at process start.
// 单层 env 已设置时不要 Set*：ToolSurfaceEnabled / TurnIntentGateEnabled / TaskLockEnabled 会先读 env。
// 禁止在 env 已设置时再套用 YAML turn_tool_surface_enabled.
func ApplyInvestigationGates(cfg *conf.ChatConfig) {
	on := cfg != nil && cfg.InvestigationGatesOn()
	if !envSet(turnToolSurfaceEnv) {
		if on {
			if cfg != nil && cfg.TurnToolSurfaceEnabled != nil {
				SetTurnToolSurfaceEnabled(*cfg.TurnToolSurfaceEnabled)
			} else {
				SetTurnToolSurfaceEnabled(true)
			}
		} else {
			SetTurnToolSurfaceEnabled(false)
		}
	}
	if !envSet(turnIntentGateEnv) {
		SetTurnIntentGateEnabled(on)
	}
	if !envSet(taskLockEnv) {
		SetTaskLockEnabled(on)
	}
}

func MaybeApplyTaskLock(prompt string, lock TurnTaskLock) string {
	if !TaskLockEnabled() {
		return prompt
	}
	return AppendTaskLock(prompt, lock)
}

func MaybeMergeTaskLockMetadata(md map[string]any, lock TurnTaskLock) map[string]any {
	if !TaskLockEnabled() {
		return md
	}
	return MergeTaskLockMetadata(md, lock)
}
