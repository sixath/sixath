package harness

import "context"

// contextKeyRequestMetadata carries Request.Metadata into tool hooks.
type contextKeyRequestMetadata struct{}

// WithRequestMetadata stores req metadata on ctx for ToolHook.After / Before.
func WithRequestMetadata(ctx context.Context, md map[string]any) context.Context {
	if ctx == nil || md == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKeyRequestMetadata{}, md)
}

// RequestMetadataFromContext returns metadata injected via WithRequestMetadata.
func RequestMetadataFromContext(ctx context.Context) map[string]any {
	if ctx == nil {
		return nil
	}
	md, _ := ctx.Value(contextKeyRequestMetadata{}).(map[string]any)
	return md
}
