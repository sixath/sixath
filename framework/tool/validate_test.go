package tool

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func fileSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":  map[string]any{"type": "string", "description": "relative path"},
			"limit": map[string]any{"type": "integer", "minimum": 1},
			"kind":  map[string]any{"type": "string", "enum": []string{"text", "select"}},
			"tags": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"opts": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"mode": map[string]any{"type": "string"},
				},
				"required":             []string{"mode"},
				"additionalProperties": false,
			},
		},
		"required": []string{"path"},
	}
}

func TestValidateArguments_AcceptsValidCall(t *testing.T) {
	err := ValidateArguments("read_file", fileSchema(), map[string]any{
		"path":  "/tmp/a.txt",
		"limit": 5,
		"kind":  "text",
		"tags":  []any{"a", "b"},
		"opts":  map[string]any{"mode": "fast"},
	})
	if err != nil {
		t.Fatalf("valid call rejected: %v", err)
	}
}

func TestValidateArguments_MissingRequired(t *testing.T) {
	err := ValidateArguments("read_file", fileSchema(), map[string]any{"limit": 5})
	if err == nil {
		t.Fatal("expected error for missing required argument")
	}
	var iae *InvalidArgumentsError
	if !errors.As(err, &iae) {
		t.Fatalf("error type=%T want *InvalidArgumentsError", err)
	}
	if len(iae.Errors) != 1 || iae.Errors[0].Keyword != "required" || iae.Errors[0].Path != "path" {
		t.Fatalf("errors=%+v", iae.Errors)
	}
	if !strings.Contains(err.Error(), `missing required argument "path"`) {
		t.Fatalf("message not model-readable: %s", err.Error())
	}
}

func TestValidateArguments_WrongType(t *testing.T) {
	err := ValidateArguments("read_file", fileSchema(), map[string]any{"path": 123})
	if err == nil {
		t.Fatal("expected type error")
	}
	if !strings.Contains(err.Error(), "must be string") || !strings.Contains(err.Error(), "got integer") {
		t.Fatalf("message should name expected/actual type: %s", err.Error())
	}
}

func TestValidateArguments_RejectsObjectWhereStringExpected(t *testing.T) {
	err := ValidateArguments("read_file", fileSchema(), map[string]any{"path": map[string]any{"a": 1}})
	if err == nil {
		t.Fatal("expected type error")
	}
	if !strings.Contains(err.Error(), "got object") {
		t.Fatalf("message=%s", err.Error())
	}
}

func TestValidateArguments_EnumViolation(t *testing.T) {
	err := ValidateArguments("read_file", fileSchema(), map[string]any{"path": "p", "kind": "bogus"})
	if err == nil {
		t.Fatal("expected enum error")
	}
	if !strings.Contains(err.Error(), "must be one of") || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("message=%s", err.Error())
	}
}

func TestValidateArguments_MinimumViolation(t *testing.T) {
	err := ValidateArguments("read_file", fileSchema(), map[string]any{"path": "p", "limit": 0})
	if err == nil {
		t.Fatal("expected minimum error")
	}
	if !strings.Contains(err.Error(), "must be >= 1") {
		t.Fatalf("message=%s", err.Error())
	}
}

func TestValidateArguments_NestedPathsAreReported(t *testing.T) {
	err := ValidateArguments("read_file", fileSchema(), map[string]any{
		"path": "p",
		"tags": []any{"ok", 42},
		"opts": map[string]any{"other": true},
	})
	if err == nil {
		t.Fatal("expected nested errors")
	}
	msg := err.Error()
	if !strings.Contains(msg, "tags[1]") {
		t.Fatalf("array index missing in %s", msg)
	}
	if !strings.Contains(msg, `"opts.mode" is required`) && !strings.Contains(msg, "opts.mode") {
		t.Fatalf("nested required missing in %s", msg)
	}
	if !strings.Contains(msg, "opts.other") {
		t.Fatalf("additionalProperties violation missing in %s", msg)
	}
}

func TestValidateArguments_NumberStringsAccepted(t *testing.T) {
	// 模型常把数字写成字符串；这类用法不构成参数错误，不应产生额外往返。
	if err := ValidateArguments("read_file", fileSchema(), map[string]any{"path": "p", "limit": "5"}); err != nil {
		t.Fatalf("numeric string should be accepted: %v", err)
	}
}

// 回归：数字样式的字符串必须满足 type=string（todo 的 id="1" 曾被误判为 number）。
func TestValidateArguments_NumericLookingStringSatisfiesStringType(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":      map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
		},
		"required": []string{"id", "content"},
	}
	if err := ValidateArguments("todo", schema, map[string]any{"id": "1", "content": "2"}); err != nil {
		t.Fatalf("numeric-looking strings must satisfy string type: %v", err)
	}
}

func TestValidateArguments_SkipsUnsupportedSchema(t *testing.T) {
	schemas := []map[string]any{
		{"type": "object", "properties": map[string]any{"a": map[string]any{"oneOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "number"}}}}, "required": []string{"a"}},
		{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string", "pattern": "^x$"}}},
		{"type": "object", "$ref": "#/definitions/x"},
		{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string", "format": "email"}}},
	}
	for i, s := range schemas {
		if SchemaSupported(s) {
			t.Fatalf("schema %d should be outside the supported subset", i)
		}
		if err := ValidateArguments("t", s, map[string]any{}); err != nil {
			t.Fatalf("schema %d must fail-open, got %v", i, err)
		}
	}
}

func TestValidateArguments_SkipsWhenNoSchema(t *testing.T) {
	for _, schema := range []any{nil, map[string]any{}, "not-a-schema", 42} {
		if err := ValidateArguments("t", schema, map[string]any{"whatever": 1}); err != nil {
			t.Fatalf("schema %v must be skipped, got %v", schema, err)
		}
	}
}

func TestValidateArguments_DisabledByEnv(t *testing.T) {
	t.Setenv(EnvToolArgValidation, "0")
	if err := ValidateArguments("read_file", fileSchema(), map[string]any{}); err != nil {
		t.Fatalf("validation should be disabled by env: %v", err)
	}
}

// ---------- Registry 集成 ----------

func registeringTool(t *testing.T, reg *Registry, name string, schema any, calls *int) {
	t.Helper()
	if err := reg.Register(Tool{
		Name:       name,
		Parameters: schema,
		Execute: func(_ context.Context, _ map[string]any) (any, error) {
			*calls++
			return "ok", nil
		},
	}); err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
}

func TestRegistryExecute_RejectsInvalidArgumentsWithoutRunningTool(t *testing.T) {
	t.Setenv(EnvToolArgValidation, "1")
	reg := NewRegistry()
	calls := 0
	registeringTool(t, reg, "demo_tool", fileSchema(), &calls)

	tl, ok := reg.Get("demo_tool")
	if !ok {
		t.Fatal("tool missing")
	}
	res, err := tl.Execute(context.Background(), map[string]any{"limit": 3})
	if err == nil {
		t.Fatal("expected validation error")
	}
	var iae *InvalidArgumentsError
	if !errors.As(err, &iae) {
		t.Fatalf("error=%T want *InvalidArgumentsError", err)
	}
	if calls != 0 {
		t.Fatalf("tool must not run on invalid arguments, calls=%d", calls)
	}
	// 结果结构沿用仓库既有约定（vision.go / rcaErr）：ok=false + error + error_code。
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("result=%T want map[string]any (callers rely on the structured payload)", res)
	}
	if m["ok"] != false || m["error_code"] != ErrorPermanent {
		t.Fatalf("result=%#v", m)
	}
	if _, has := m["invalid_arguments"]; !has {
		t.Fatalf("expected invalid_arguments detail, got %#v", m)
	}
}

func TestRegistryExecute_RunsToolWhenArgumentsValid(t *testing.T) {
	t.Setenv(EnvToolArgValidation, "1")
	reg := NewRegistry()
	calls := 0
	registeringTool(t, reg, "demo_tool", fileSchema(), &calls)

	tl, _ := reg.Get("demo_tool")
	if _, err := tl.Execute(context.Background(), map[string]any{"path": "p"}); err != nil {
		t.Fatalf("valid call failed: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d want 1", calls)
	}
}

func TestRegistryExecute_ValidationDisabledByEnv(t *testing.T) {
	t.Setenv(EnvToolArgValidation, "off")
	reg := NewRegistry()
	calls := 0
	registeringTool(t, reg, "demo_tool", fileSchema(), &calls)

	tl, _ := reg.Get("demo_tool")
	if _, err := tl.Execute(context.Background(), map[string]any{}); err != nil {
		t.Fatalf("validation should be off: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d want 1", calls)
	}
}

func TestRegistryExecute_ToolTimeoutProducesClearError(t *testing.T) {
	t.Setenv(EnvToolArgValidation, "0")
	reg := NewRegistry()
	if err := reg.Register(Tool{
		Name:    "slow_tool",
		Timeout: 20 * time.Millisecond,
		Execute: func(ctx context.Context, _ map[string]any) (any, error) {
			select {
			case <-time.After(500 * time.Millisecond):
				return "late", nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	tl, _ := reg.Get("slow_tool")
	_, err := tl.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timeout after") {
		t.Fatalf("error should be explicit about the timeout: %v", err)
	}
}

func TestRegistryExecute_ToolTimeoutOptOut(t *testing.T) {
	t.Setenv(EnvToolArgValidation, "0")
	reg := NewRegistry()
	if err := reg.Register(Tool{
		Name:    "self_managed_tool",
		Timeout: -1, // 显式声明由工具自行管理（例如配置为「无限制」的数据源查询）
		Execute: func(ctx context.Context, _ map[string]any) (any, error) {
			select {
			case <-time.After(200 * time.Millisecond):
				return "done", nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	tl, _ := reg.Get("self_managed_tool")
	res, err := tl.Execute(context.Background(), nil)
	if err != nil || res != "done" {
		t.Fatalf("opt-out tool must not be cut: res=%v err=%v", res, err)
	}
}

func TestRegistryExecute_TimeoutEnvOverride(t *testing.T) {
	t.Setenv(EnvToolArgValidation, "0")
	t.Setenv(EnvToolTimeoutSec, "1")
	reg := NewRegistry()
	if err := reg.Register(Tool{
		Name: "slow_tool",
		Execute: func(ctx context.Context, _ map[string]any) (any, error) {
			select {
			case <-time.After(3 * time.Second):
				return "late", nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	tl, _ := reg.Get("slow_tool")
	start := time.Now()
	if _, err := tl.Execute(context.Background(), nil); err == nil {
		t.Fatal("expected timeout")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("env timeout not applied, elapsed=%s", elapsed)
	}
}

func TestRegistryExecute_TimeoutDisabledByEnvZero(t *testing.T) {
	t.Setenv(EnvToolArgValidation, "0")
	t.Setenv(EnvToolTimeoutSec, "0")
	reg := NewRegistry()
	if err := reg.Register(Tool{
		Name: "slow_tool",
		Execute: func(ctx context.Context, _ map[string]any) (any, error) {
			select {
			case <-time.After(200 * time.Millisecond):
				return "done", nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	tl, _ := reg.Get("slow_tool")
	if _, err := tl.Execute(context.Background(), nil); err != nil {
		t.Fatalf("timeout wrapper should be disabled: %v", err)
	}
}

func TestRegistryExecute_ParentCancellationIsNotRewrittenAsToolTimeout(t *testing.T) {
	t.Setenv(EnvToolArgValidation, "0")
	reg := NewRegistry()
	if err := reg.Register(Tool{
		Name:    "slow_tool",
		Timeout: time.Minute,
		Execute: func(ctx context.Context, _ map[string]any) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tl, _ := reg.Get("slow_tool")
	_, err := tl.Execute(ctx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("parent cancellation must propagate as-is, got %v", err)
	}
}
