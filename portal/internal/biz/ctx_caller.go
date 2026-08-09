package biz

import "context"

type ctxKey int

const (
	ctxCallerUserID ctxKey = iota + 1
	ctxOrgID
)

func WithCallerUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, ctxCallerUserID, userID)
}

func CallerUserID(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxCallerUserID).(string)
	return v, ok && v != ""
}

func WithOrgID(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, ctxOrgID, orgID)
}

func OrgID(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxOrgID).(string)
	return v, ok && v != ""
}

// DetachCallerContext returns a non-cancellable context that keeps caller/org
// identity from parent. Use for async indexing / dirty-notify goroutines so
// GetSession / agent ACL checks still see the request principal after the
// HTTP request context is cancelled.
func DetachCallerContext(parent context.Context) context.Context {
	bg := context.Background()
	if parent == nil {
		return bg
	}
	if uid, ok := CallerUserID(parent); ok {
		bg = WithCallerUserID(bg, uid)
	}
	if org, ok := OrgID(parent); ok {
		bg = WithOrgID(bg, org)
	}
	return bg
}
