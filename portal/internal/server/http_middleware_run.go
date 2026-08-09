package server

import (
	"context"

	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
)

// runWithMiddleware runs fn inside the HTTP server middleware chain.
// Custom Route handlers do not automatically receive Auth/caller context unless
// they invoke ctx.Middleware (unlike protobuf-registered services).
func runWithMiddleware(ctx kratoshttp.Context, fn func(context.Context) (any, error)) (any, error) {
	h := ctx.Middleware(func(c context.Context, _ any) (any, error) {
		return fn(c)
	})
	return h(ctx, nil)
}
