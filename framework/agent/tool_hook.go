package agent

import (
	"context"
	"errors"
	"fmt"
)

// ErrToolHookBlocked Before 拒绝执行工具（与 PermissionDenied 区分：回写模型 tool 消息，不中断整步为 RunError）。
var ErrToolHookBlocked = errors.New("tool hook blocked")

// ToolHook 工具生命周期钩子（harness 控制面；设计 §3.2 / runtime §6.3）。
// Middleware 负责 HTTP/Agent 外围；本接口只服务单次 tool 调用。
type ToolHook interface {
	Before(ctx context.Context, name string, params map[string]any) (map[string]any, error)
	After(ctx context.Context, name string, result any, err error) (any, error)
}

func runToolHooksBefore(ctx context.Context, hooks []ToolHook, name string, params map[string]any) (map[string]any, error) {
	out := params
	if out == nil {
		out = map[string]any{}
	}
	for _, h := range hooks {
		if h == nil {
			continue
		}
		next, err := h.Before(ctx, name, out)
		if err != nil {
			return out, fmt.Errorf("%w: %v", ErrToolHookBlocked, err)
		}
		if next != nil {
			out = next
		}
	}
	return out, nil
}

// runToolHooksAfter After 与 Before **同序**（规格写死）。
func runToolHooksAfter(ctx context.Context, hooks []ToolHook, name string, result any, execErr error) (any, error) {
	out := result
	err := execErr
	for _, h := range hooks {
		if h == nil {
			continue
		}
		var nextErr error
		out, nextErr = h.After(ctx, name, out, err)
		err = nextErr
	}
	return out, err
}
