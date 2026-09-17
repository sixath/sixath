package runtime

import (
	"context"
	"errors"
)

// streamRunContext derives a run context for reply_mode=stream.
//
// Kratos wraps every request with server.http.timeout (DeadlineExceeded). That
// bound is for JSON APIs; a ReAct tool loop is capped by MaxSteps (and the
// client disconnecting). Ignore the HTTP deadline, but still cancel when the
// caller context is Canceled (browser/Gateway hang-up).
func streamRunContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	go func() {
		select {
		case <-parent.Done():
			if errors.Is(parent.Err(), context.DeadlineExceeded) {
				<-ctx.Done()
				return
			}
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}
