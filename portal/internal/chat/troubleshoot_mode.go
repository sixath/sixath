package chat

import "context"

type troubleshootModeContextKey struct{}

// WithTroubleshootMode 绑定本轮是否处于排障模式（聊天页开关）。
func WithTroubleshootMode(ctx context.Context, on bool) context.Context {
	return context.WithValue(ctx, troubleshootModeContextKey{}, on)
}

// TroubleshootModeFromContext 读取排障模式；未绑定时为关闭。
func TroubleshootModeFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	on, _ := ctx.Value(troubleshootModeContextKey{}).(bool)
	return on
}
