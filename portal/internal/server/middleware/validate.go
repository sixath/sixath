package middleware

import (
	"context"
	"fmt"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/middleware"
)

type validator interface {
	Validate() error
}

// Validator is a validator middleware.
func Validator() middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (reply interface{}, err error) {
			if v, ok := req.(validator); ok {
				if err := v.Validate(); err != nil {
					errMsg := err.Error()
					// 判断是protobuf的校验错误
					if ve, ok := err.(interface {
						Field() string
						Reason() string
						Cause() error
					}); ok {
						// 使用 field, reason, cause
						errMsg = fmt.Sprintf("字段 '%s' 的值不符合规则: %s, 原因: %s", ve.Field(), ve.Reason(), ve.Cause())
					}
					return nil, errors.BadRequest("VALIDATOR", errMsg).WithCause(err)
				}
			}
			return handler(ctx, req)
		}
	}
}
