package middleware

import (
	"context"
	"strings"

	"github.com/sixath/framework/errs"
	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/model"
)

// ContentFilter 定义输入/输出内容检查（兼容 CheckInput/CheckOutput；可实现 Check）。
type ContentFilter interface {
	CheckInput(text string) error
	CheckOutput(text string) error
}

// RoleAwareFilter 支持按 role 检查。
type RoleAwareFilter interface {
	Check(role, text string) FilterResult
}

// SimpleBlocklistFilter 基于关键字黑名单的简单实现，用于演示与开发环境。
type SimpleBlocklistFilter struct {
	Blocked []string
}

func (f *SimpleBlocklistFilter) CheckInput(text string) error {
	for _, w := range f.Blocked {
		if w == "" {
			continue
		}
		if strings.Contains(text, w) {
			return errs.ErrContentBlocked
		}
	}
	return nil
}

func (f *SimpleBlocklistFilter) CheckOutput(text string) error {
	return f.CheckInput(text)
}

func checkMessage(filter ContentFilter, m model.Message, role string) error {
	if filter == nil {
		return nil
	}
	if rf, ok := filter.(RoleAwareFilter); ok {
		if r := rf.Check(role, m.Content); r.Decision == FilterBlock {
			return errs.ErrContentBlocked
		}
		for _, p := range m.Parts {
			if p.Text != "" {
				if r := rf.Check(role, p.Text); r.Decision == FilterBlock {
					return errs.ErrContentBlocked
				}
			}
		}
		return nil
	}
	if role == "user" || role == "assistant" {
		if err := filter.CheckInput(m.Content); err != nil {
			return err
		}
		for _, p := range m.Parts {
			if p.Text != "" {
				if err := filter.CheckInput(p.Text); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// ContentSafetyMiddleware 在请求前后进行内容安全检查（输入/输出）。
func ContentSafetyMiddleware(filter ContentFilter) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
			if filter == nil || req == nil {
				return next(ctx, req)
			}
			for _, m := range req.Messages {
				role := m.Role
				if role == "user" {
					if err := checkMessage(filter, m, role); err != nil {
						if ac := agent.ContextFrom(ctx); ac != nil {
							ac.BlockReason = "input_filter"
						}
						return nil, errs.ErrContentBlocked
					}
				}
			}

			resp, err := next(ctx, req)
			if err != nil || resp == nil {
				return resp, err
			}

			if err := filter.CheckOutput(resp.Text); err != nil {
				if ac := agent.ContextFrom(ctx); ac != nil {
					ac.BlockReason = "output_filter"
				}
				return nil, errs.ErrContentBlocked
			}
			return resp, nil
		}
	}
}
