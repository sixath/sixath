package middleware

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sixath/framework/errs"
	agent "github.com/sixath/framework/harness"
	"golang.org/x/sync/singleflight"
)

// StreamHandler 流式 Agent 处理函数。
type StreamHandler func(ctx context.Context, req *agent.Request) (<-chan agent.ResponseChunk, error)

// StreamMiddleware 包装 StreamHandler。
type StreamMiddleware func(StreamHandler) StreamHandler

// StreamChain 组合流式中间件（自动注入 AgentContext）。
func StreamChain(final StreamHandler, mws ...StreamMiddleware) StreamHandler {
	all := make([]StreamMiddleware, 0, len(mws)+1)
	all = append(all, streamAgentContextMiddleware())
	all = append(all, mws...)
	h := final
	for i := len(all) - 1; i >= 0; i-- {
		h = all[i](h)
	}
	return h
}

func streamAgentContextMiddleware() StreamMiddleware {
	return func(next StreamHandler) StreamHandler {
		return func(ctx context.Context, req *agent.Request) (<-chan agent.ResponseChunk, error) {
			ctx, ac := agent.EnsureContext(ctx)
			if req != nil {
				req.Normalize()
				if ac != nil {
					if ac.StartTime.IsZero() {
						ac.StartTime = time.Now()
					}
					ac.AgentName = req.EffectiveAgentName()
					ac.UserID = req.UserID
					ac.ModelName = req.ModelName
				}
			}
			return next(ctx, req)
		}
	}
}

// LiftStreamMiddleware 将非流式中间件提升为流式：先收集全文再经 mw 处理（适用于 Logging 等）。
func LiftStreamMiddleware(mw Middleware) StreamMiddleware {
	if mw == nil {
		return func(next StreamHandler) StreamHandler { return next }
	}
	return func(next StreamHandler) StreamHandler {
		return func(ctx context.Context, req *agent.Request) (<-chan agent.ResponseChunk, error) {
			chIn, err := next(ctx, req)
			if err != nil {
				return nil, err
			}
			collected, err := collectChunks(chIn)
			if err != nil {
				return errorChunkChan(err), nil
			}
			h := mw(func(c context.Context, r *agent.Request) (*agent.Response, error) {
				return &agent.Response{Text: collected}, nil
			})
			if _, err := h(ctx, req); err != nil {
				return errorChunkChan(err), nil
			}
			return singleChunkChan(collected), nil
		}
	}
}

func collectChunks(ch <-chan agent.ResponseChunk) (string, error) {
	var sb strings.Builder
	for c := range ch {
		if c.Err != nil {
			return sb.String(), c.Err
		}
		sb.WriteString(c.Delta)
	}
	return sb.String(), nil
}

func singleChunkChan(text string) <-chan agent.ResponseChunk {
	ch := make(chan agent.ResponseChunk, 1)
	ch <- agent.ResponseChunk{Delta: text, Done: true}
	close(ch)
	return ch
}

func errorChunkChan(err error) <-chan agent.ResponseChunk {
	ch := make(chan agent.ResponseChunk, 1)
	ch <- agent.ResponseChunk{Err: err, Done: true}
	close(ch)
	return ch
}

// StreamMetricsMiddleware 流式 metrics（末 chunk 计时；source 含 cache/blocked）。
func StreamMetricsMiddleware(next StreamHandler) StreamHandler {
	return func(ctx context.Context, req *agent.Request) (<-chan agent.ResponseChunk, error) {
		start := time.Now()
		ch, err := next(ctx, req)
		if err != nil {
			name := "default"
			if req != nil {
				name = req.EffectiveAgentName()
			}
			observeAgentRequest(name, "error", time.Since(start))
			return nil, err
		}
		out := make(chan agent.ResponseChunk, 16)
		go func() {
			defer close(out)
			for c := range ch {
				out <- c
				if c.Done {
					name := "default"
					if req != nil {
						name = req.EffectiveAgentName()
					}
					observeAgentRequest(name, agent.RequestSource(agent.ContextFrom(ctx), c.Err), time.Since(start))
				}
			}
		}()
		return out, nil
	}
}

// StreamRecoveryMiddleware 捕获 goroutine 内 panic 并写入末 chunk Err。
func StreamRecoveryMiddleware(next StreamHandler) StreamHandler {
	return func(ctx context.Context, req *agent.Request) (<-chan agent.ResponseChunk, error) {
		ch, err := next(ctx, req)
		if err != nil {
			return nil, err
		}
		out := make(chan agent.ResponseChunk, 16)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					out <- agent.ResponseChunk{Err: panicError(r), Done: true}
				}
				close(out)
			}()
			for c := range ch {
				out <- c
			}
		}()
		return out, nil
	}
}

func panicError(r any) error {
	return fmt.Errorf("%w: %v", errs.ErrInternal, r)
}

// StreamCacheMiddleware 流式缓存：命中单 chunk 回放；miss 收集后写入。
func StreamCacheMiddleware(store *CacheStore, builder CacheKeyBuilder) StreamMiddleware {
	if builder == nil {
		builder = &DefaultCacheKey{Version: 1}
	}
	var sf singleflight.Group
	return func(next StreamHandler) StreamHandler {
		return func(ctx context.Context, req *agent.Request) (<-chan agent.ResponseChunk, error) {
			if req == nil || store == nil {
				return next(ctx, req)
			}
			key := builder.BuildKey(req)
			if cached, ok := store.Get(key); ok && cached != nil {
				if ac := agent.ContextFrom(ctx); ac != nil {
					ac.CacheHit = true
				}
				return singleChunkChan(cached.Text), nil
			}
			v, err, _ := sf.Do(key, func() (any, error) {
				ch, err := next(ctx, req)
				if err != nil {
					return nil, err
				}
				text, err := collectChunks(ch)
				if err != nil {
					return nil, err
				}
				store.Set(key, &agent.Response{Text: text})
				return text, nil
			})
			if err != nil {
				return nil, err
			}
			return singleChunkChan(v.(string)), nil
		}
	}
}

// StreamContentSafetyMiddleware 输入整段检查；输出按 chunk 检查。
func StreamContentSafetyMiddleware(filter ContentFilter) StreamMiddleware {
	return func(next StreamHandler) StreamHandler {
		return func(ctx context.Context, req *agent.Request) (<-chan agent.ResponseChunk, error) {
			if filter == nil || req == nil {
				return next(ctx, req)
			}
			for _, m := range req.Messages {
				if m.Role == "user" {
					if err := checkMessage(filter, m, m.Role); err != nil {
						if ac := agent.ContextFrom(ctx); ac != nil {
							ac.BlockReason = "input_filter"
						}
						return errorChunkChan(err), nil
					}
				}
			}
			ch, err := next(ctx, req)
			if err != nil {
				return nil, err
			}
			out := make(chan agent.ResponseChunk, 16)
			go func() {
				defer close(out)
				for c := range ch {
					if c.Delta != "" {
						if err := filter.CheckOutput(c.Delta); err != nil {
							if ac := agent.ContextFrom(ctx); ac != nil {
								ac.BlockReason = "output_filter"
							}
							out <- agent.ResponseChunk{Err: err, Done: true}
							return
						}
					}
					out <- c
				}
			}()
			return out, nil
		}
	}
}

// StringStreamAdapter 将 StreamableAgent 的 string channel 转为 ResponseChunk。
func StringStreamAdapter(run func(context.Context, *agent.Request) (<-chan string, error)) StreamHandler {
	return func(ctx context.Context, req *agent.Request) (<-chan agent.ResponseChunk, error) {
		ch, err := run(ctx, req)
		if err != nil {
			return nil, err
		}
		out := make(chan agent.ResponseChunk, 16)
		go func() {
			defer close(out)
			for s := range ch {
				out <- agent.ResponseChunk{Delta: s}
			}
			out <- agent.ResponseChunk{Done: true}
		}()
		return out, nil
	}
}

// DrainStringChunks 消费 chunk 流并拼接文本（测试/适配用）。
func DrainStringChunks(ch <-chan agent.ResponseChunk) (string, error) {
	return collectChunks(ch)
}
