// Package investigate 提供冷启动兜底调查工具 deep_investigate：
// agent 同时具备代码仓库工具与日志类工具时对模型可见；模型在无线索的
// 定位类问题中主动调用，工具内部跑子 ReAct 循环（代码优先 → 日志验证）
// 并返回调查结论。详见 docs/superpowers/specs/2026-09-24-skill-self-evolution-design.md。
package investigate

import (
	"context"
	"errors"
	"time"

	"github.com/sixath/framework/harness"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

// ToolName 是兜底调查工具的注册名。
const ToolName = "deep_investigate"

// codeToolNames 代码仓库工具组：任一存在即满足代码侧门控。
var codeToolNames = []string{"rca_grep", "rca_glob", "rca_read", "rca_symbol"}

// logToolNames 日志/链路工具组：任一存在即满足日志侧门控。
var logToolNames = []string{"es_log_query", "jaeger_trace", "vm_run_cmd"}

// Config 为 deep_investigate 的运行配置。
type Config struct {
	// Model 子 agent 使用的模型；必须实现 harness.ToolCallingModel（通常复用父 agent 的 chat 模型）。
	Model model.Model
	// MaxSteps 子 ReAct 循环步数上限；<=0 时默认 15。
	MaxSteps int
	// Timeout 整轮子调查超时；<=0 时默认 15 分钟。
	Timeout time.Duration
}

func (c Config) maxSteps() int {
	if c.MaxSteps > 0 {
		return c.MaxSteps
	}
	return 15
}

func (c Config) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return 15 * time.Minute
}

// Register 把 deep_investigate 注册进 reg。可见性由 CheckFn 门控决定：
// 代码工具组与日志工具组各至少一个「可见」（存在、CheckFn 通过、未被
// enabled_toolsets 白名单过滤）时才出现在模型 schema 中。
func Register(reg *tool.Registry, cfg Config) error {
	if reg == nil {
		return errors.New("investigate: registry is nil")
	}
	if cfg.Model == nil {
		return errors.New("investigate: model is nil")
	}
	if _, ok := cfg.Model.(harness.ToolCallingModel); !ok {
		return errors.New("investigate: model does not support tool calling")
	}
	return reg.Register(tool.Tool{
		Name:               ToolName,
		Description:        toolDescription,
		Toolset:            tool.ToolsetRCA,
		AlwaysLoad:         true, // 兜底工具必须扛住 defer 过滤，始终可见
		RequiresSequential: true, // 重操作，不与其他工具并行
		CheckFn:            gate(reg),
		Timeout:            cfg.timeout(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question": map[string]any{
					"type":        "string",
					"description": "The user's problem to investigate, restated faithfully (do not paraphrase away specifics like names/ids).",
				},
				"hints": map[string]any{
					"type":        "string",
					"description": "Optional known hints: service name, time window, vmid, interface name, etc.",
				},
			},
			"required": []string{"question"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			return nil, errors.New("not implemented") // Task 2 实现 buildExecute 后替换
		},
	})
}

// gate 返回 CheckFn：代码组与日志组各至少一个工具可见时放行。
func gate(reg *tool.Registry) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		if !anyVisible(ctx, reg, codeToolNames) {
			return errors.New("deep_investigate: no code repository tools available")
		}
		if !anyVisible(ctx, reg, logToolNames) {
			return errors.New("deep_investigate: no log query tools available")
		}
		return nil
	}
}

// anyVisible 判断组内是否有至少一个工具对当前 ctx 可见：
// 已注册、自身 CheckFn 通过、未被 enabled_toolsets 白名单排除。
func anyVisible(ctx context.Context, reg *tool.Registry, names []string) bool {
	for _, name := range names {
		t, ok := reg.Get(name)
		if !ok {
			continue
		}
		if t.CheckFn != nil && t.CheckFn(ctx) != nil {
			continue
		}
		if allowed := tool.EnabledToolsetsFromContext(ctx); allowed != nil {
			found := false
			for _, ts := range allowed {
				if ts == t.Toolset {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		return true
	}
	return false
}