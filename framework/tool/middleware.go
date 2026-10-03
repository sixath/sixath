package tool

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

const argCheckTimeout = 2 * time.Second

func applyArgAliases(params map[string]any, aliases map[string]string) map[string]any {
	if len(aliases) == 0 || len(params) == 0 {
		return params
	}
	out := make(map[string]any, len(params))
	for k, v := range params {
		out[k] = v
	}
	for alias, canonical := range aliases {
		v, ok := out[alias]
		if !ok {
			continue
		}
		if _, exists := out[canonical]; !exists {
			out[canonical] = v
		}
		delete(out, alias)
	}
	return out
}

// invalidArgsResult 沿用仓库既有失败约定 {"ok":false,"error":…,"error_code":permanent}，
// 保证依赖结果结构的调用方（RCA evidence 契约、UI 时间线）仍能解析；同时调用方仍返回 error。
func invalidArgsResult(name string, err error) map[string]any {
	result := map[string]any{
		"ok":         false,
		"tool":       name,
		"error":      err.Error(),
		"error_code": ErrorPermanent,
	}
	var iae *InvalidArgumentsError
	if errors.As(err, &iae) {
		result["invalid_arguments"] = iae.Errors
	}
	return result
}

// runArgChecks 依次执行规则并收集错误；规则自身失败或超时则跳过（fail-open），返回被跳过的规则名。
func runArgChecks(ctx context.Context, toolName string, checks []ArgCheck, params map[string]any) ([]SchemaError, []string) {
	var errs []SchemaError
	var skipped []string
	for _, c := range checks {
		if c == nil {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, argCheckTimeout)
		found, err := c.Check(cctx, params)
		cancel()
		if err != nil {
			skipped = append(skipped, c.Name())
			slog.InfoContext(ctx, "tool arg check skipped", "tool", toolName, "check", c.Name(), "reason", err.Error())
			continue
		}
		errs = append(errs, found...)
	}
	return errs, skipped
}

// wrapChecks 构造校验中间件：别名 → JSON Schema → ArgChecks → Execute → EmptyProbe。
// 开关每次调用读取，便于线上即时回退。
func wrapChecks(t Tool) ExecuteFunc {
	name, schema, aliases, checks, probe, inner := t.Name, t.Parameters, t.ArgAliases, t.ArgChecks, t.EmptyProbe, t.Execute
	return func(ctx context.Context, params map[string]any) (any, error) {
		params = applyArgAliases(params, aliases)
		var skipped []string
		if toolArgValidationEnabled() {
			if err := ValidateArguments(name, schema, params); err != nil {
				return invalidArgsResult(name, err), err
			}
			var errs []SchemaError
			errs, skipped = runArgChecks(ctx, name, checks, params)
			if len(errs) > 0 {
				err := &InvalidArgumentsError{Tool: name, Errors: errs}
				return invalidArgsResult(name, err), err
			}
		}
		result, err := inner(ctx, params)
		if err != nil {
			return result, err
		}
		if probe != nil {
			result = runEmptyProbe(ctx, probe, params, result)
		}
		if len(skipped) > 0 {
			if st, _, _ := HitContractFromResult(result); st == HitStatusEmpty {
				result = MarkSuspect(result, nil)
			}
			if m, ok := result.(map[string]any); ok {
				m["check_skipped"] = skipped
			}
		}
		return result, nil
	}
}
