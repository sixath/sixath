package tool

import "context"

type secretProviderKey struct{}

// WithSecretProvider 将会话 secret 存储绑定到 context，供 SecretFromContext 与后续工具使用。
func WithSecretProvider(ctx context.Context, store AskUserFulfillmentStore) context.Context {
	if store == nil {
		return ctx
	}
	return context.WithValue(ctx, secretProviderKey{}, store)
}

// SecretFromContext 读取 ask_user 已履约的 password 类字段；明文不进 LLM 上下文。
func SecretFromContext(ctx context.Context, field string) (string, bool) {
	store, _ := ctx.Value(secretProviderKey{}).(AskUserFulfillmentStore)
	if store == nil || field == "" {
		return "", false
	}
	sessionID, _ := ctx.Value(ContextKeySessionID).(string)
	if sessionID == "" {
		return "", false
	}
	v, err := store.GetSecret(ctx, sessionID, field)
	if err != nil {
		return "", false
	}
	return v, true
}
