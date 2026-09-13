package tool

import (
	"fmt"
	"math"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// 参数校验（docs/superpowers/plans/2026-09-12-maturity-hardening.md Task 5）
//
// 目标：把「参数缺失/类型不对」这类错误从「工具内部各自手写判断（或未定义行为）」
// 提前为「结构化、模型可读的拒绝」，模型下一轮可自行修正。
//
// 两条硬性设计约束：
//  1. **fail-open**：只校验我们确认支持的 JSON Schema 子集；schema 若使用了不支持
//     的关键字（oneOf/anyOf/$ref/pattern/format…），整个工具跳过校验，绝不误杀。
//  2. **可关闭**：SATH_TOOL_ARG_VALIDATION=0/off/false 时完全不注册校验包装，
//     线上可秒级回退。

// EnvToolArgValidation 控制是否启用工具入参校验（默认启用）。
const EnvToolArgValidation = "SATH_TOOL_ARG_VALIDATION"

// SchemaError 描述单个参数校验失败。
type SchemaError struct {
	// Path 为参数路径，如 "limit"、"items[2].url"；根对象为空串。
	Path string `json:"path"`
	// Keyword 为命中的 schema 关键字：required|type|enum|minimum|maximum|additionalProperties。
	Keyword string `json:"keyword"`
	// Message 为面向模型的完整说明。
	Message string `json:"message"`
}

// InvalidArgumentsError 表示入参未通过工具声明的 schema 校验。
// 与执行期错误同等对待：Agent 会把它作为可恢复的 tool error 交回模型重试。
type InvalidArgumentsError struct {
	Tool   string
	Errors []SchemaError
}

const invalidArgsMaxShown = 5

func (e *InvalidArgumentsError) Error() string {
	if e == nil {
		return "invalid tool arguments"
	}
	shown := e.Errors
	suffix := ""
	if len(shown) > invalidArgsMaxShown {
		shown = shown[:invalidArgsMaxShown]
		suffix = fmt.Sprintf(" (+%d more)", len(e.Errors)-invalidArgsMaxShown)
	}
	parts := make([]string, 0, len(shown))
	for _, se := range shown {
		parts = append(parts, se.Message)
	}
	if len(parts) == 0 {
		return fmt.Sprintf("tool %q: invalid arguments", e.Tool)
	}
	return fmt.Sprintf("tool %q: invalid arguments: %s%s", e.Tool, strings.Join(parts, "; "), suffix)
}

// toolArgValidationEnabled 在调用时读取环境变量，便于测试与线上热切换。
func toolArgValidationEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvToolArgValidation))) {
	case "0", "false", "off", "no", "disable", "disabled":
		return false
	default:
		return true
	}
}

// 支持的关键字集合。校验类关键字之外的注解（description 等）不影响判定。
var (
	supportedSchemaKeywords = map[string]struct{}{
		"type":                 {},
		"properties":           {},
		"required":             {},
		"enum":                 {},
		"items":                {},
		"minimum":              {},
		"maximum":              {},
		"additionalProperties": {},
	}
	annotationSchemaKeywords = map[string]struct{}{
		"description": {},
		"title":       {},
		"default":     {},
		"examples":    {},
		"deprecated":  {},
		"readOnly":    {},
		"writeOnly":   {},
		"$comment":    {},
		"$schema":     {},
		"$id":         {},
	}
)

// SchemaSupported 报告 schema 是否完全落在受支持的子集内。
// 返回 false 时 ValidateArguments 会跳过校验（fail-open）。
func SchemaSupported(schema any) bool {
	root, ok := schemaObject(schema)
	if !ok || len(root) == 0 {
		return false
	}
	return schemaSupported(root)
}

func schemaSupported(schema map[string]any) bool {
	for key := range schema {
		if _, ok := supportedSchemaKeywords[key]; ok {
			continue
		}
		if _, ok := annotationSchemaKeywords[key]; ok {
			continue
		}
		// 未知关键字：保守起见整体跳过（可能是 oneOf/$ref/pattern 等收窄语义）。
		return false
	}
	if props, ok := schemaObject(schema["properties"]); ok {
		for _, sub := range props {
			if subSchema, ok := schemaObject(sub); ok {
				if !schemaSupported(subSchema) {
					return false
				}
			}
		}
	}
	if items, ok := schemaObject(schema["items"]); ok {
		if !schemaSupported(items) {
			return false
		}
	}
	if ap, ok := schemaObject(schema["additionalProperties"]); ok {
		if !schemaSupported(ap) {
			return false
		}
	}
	return true
}

// ValidateArguments 按工具声明的 schema 校验入参。
// 返回 nil 表示通过，或「无可校验内容」（未声明 schema / schema 超出支持子集）。
func ValidateArguments(toolName string, schema any, params map[string]any) error {
	if !toolArgValidationEnabled() {
		return nil
	}
	root, ok := schemaObject(schema)
	if !ok || len(root) == 0 {
		return nil
	}
	if !schemaSupported(root) {
		return nil
	}
	errs := validateSchema(root, params, "")
	if len(errs) == 0 {
		return nil
	}
	return &InvalidArgumentsError{Tool: toolName, Errors: errs}
}

func validateSchema(schema map[string]any, value any, path string) []SchemaError {
	var errs []SchemaError

	if enum, ok := schema["enum"]; ok {
		if !valueInEnum(enum, value) {
			errs = append(errs, SchemaError{
				Path:    path,
				Keyword: "enum",
				Message: fmt.Sprintf("%s must be one of %v (got %s)", nameOf(path), enumValues(enum), describeValue(value)),
			})
		}
	}

	if want, ok := schema["type"].(string); ok && want != "" {
		if !typeMatches(want, value) {
			return append(errs, SchemaError{
				Path:    path,
				Keyword: "type",
				Message: fmt.Sprintf("%s must be %s (got %s)", nameOf(path), want, jsonKind(value)),
			})
		}
	}

	if min, ok := numberOf(schema["minimum"]); ok {
		if n, isNum := numberOf(value); isNum && n < min {
			errs = append(errs, SchemaError{
				Path:    path,
				Keyword: "minimum",
				Message: fmt.Sprintf("%s must be >= %v (got %v)", nameOf(path), min, n),
			})
		}
	}
	if max, ok := numberOf(schema["maximum"]); ok {
		if n, isNum := numberOf(value); isNum && n > max {
			errs = append(errs, SchemaError{
				Path:    path,
				Keyword: "maximum",
				Message: fmt.Sprintf("%s must be <= %v (got %v)", nameOf(path), max, n),
			})
		}
	}

	props, hasProps := schemaObject(schema["properties"])
	obj, isObj := value.(map[string]any)
	if hasProps && isObj {
		for _, name := range stringSlice(schema["required"]) {
			if _, present := obj[name]; !present {
				child := joinPath(path, name)
				errs = append(errs, SchemaError{
					Path:    child,
					Keyword: "required",
					Message: fmt.Sprintf("missing required argument %q", child),
				})
			}
		}
		for _, key := range sortedKeys(props) {
			v, present := obj[key]
			if !present {
				continue
			}
			if sub, ok := schemaObject(props[key]); ok {
				errs = append(errs, validateSchema(sub, v, joinPath(path, key))...)
			}
		}
		if ap, exists := schema["additionalProperties"]; exists {
			switch extra := ap.(type) {
			case bool:
				if !extra {
					for _, key := range sortedKeys(obj) {
						if _, declared := props[key]; declared {
							continue
						}
						child := joinPath(path, key)
						errs = append(errs, SchemaError{
							Path:    child,
							Keyword: "additionalProperties",
							Message: fmt.Sprintf("argument %q is not allowed", child),
						})
					}
				}
			case map[string]any:
				for _, key := range sortedKeys(obj) {
					if _, declared := props[key]; declared {
						continue
					}
					errs = append(errs, validateSchema(extra, obj[key], joinPath(path, key))...)
				}
			}
		}
	}

	if items, ok := schemaObject(schema["items"]); ok {
		if kind := jsonKind(value); kind == "array" {
			arr := reflect.ValueOf(value)
			for i := 0; i < arr.Len(); i++ {
				errs = append(errs, validateSchema(items, arr.Index(i).Interface(), fmt.Sprintf("%s[%d]", path, i))...)
			}
		}
	}

	return errs
}

// ---------- 值判定 ----------

// jsonKind 把 Go 值归类为 JSON 类型名（模型中转来的数字为 float64）。
func jsonKind(v any) string {
	if v == nil {
		return "null"
	}
	switch rv := reflect.ValueOf(v); rv.Kind() {
	case reflect.Ptr, reflect.Interface:
		if rv.IsNil() {
			return "null"
		}
		return jsonKind(rv.Elem().Interface())
	case reflect.String:
		// 注意：数字样式的字符串仍是 string（"1" 必须满足 type=string）。
		// 对 integer/number 的宽松接受放在 typeMatches 里，不能在这里改变类型判定，
		// 否则会误杀 A 类字段（该回归由 todo id="1" 的用例暴露）。
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		if math.Trunc(f) == f {
			return "integer"
		}
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map:
		return "object"
	default:
		return "unknown"
	}
}

// typeMatches 判定值是否满足 schema 声明的 type。
// 宽松点：integer/number 同时接受整值 float 与数字字符串（模型常把数字写成字符串）。
func typeMatches(want string, v any) bool {
	got := jsonKind(v)
	switch want {
	case "object", "array", "string", "boolean":
		return got == want
	case "integer":
		if got == "integer" {
			return true
		}
		if s, ok := v.(string); ok {
			return isNumericString(s) && math.Trunc(parseNumeric(s)) == parseNumeric(s)
		}
		return false
	case "number":
		// integer 视为 number 的子集；数字字符串同样宽松接受。
		if got == "integer" || got == "number" {
			return true
		}
		if s, ok := v.(string); ok {
			return isNumericString(s)
		}
		return false
	case "":
		return true
	default:
		// 未知 type 声明：不判定（避免误杀）。
		return true
	}
}

func numberOf(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case int32:
		return float64(t), true
	case string:
		if isNumericString(t) {
			return parseNumeric(t), true
		}
		return 0, false
	default:
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return float64(rv.Int()), true
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return float64(rv.Uint()), true
		case reflect.Float32, reflect.Float64:
			return rv.Float(), true
		}
		return 0, false
	}
}

func isNumericString(s string) bool {
	if s == "" {
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

func parseNumeric(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return math.NaN()
	}
	return f
}

func valueInEnum(enum any, v any) bool {
	list := enumValues(enum)
	for _, candidate := range list {
		if reflect.DeepEqual(candidate, v) {
			return true
		}
		if a, ok := numberOf(candidate); ok {
			if b, ok2 := numberOf(v); ok2 && a == b {
				return true
			}
		}
		if as, ok := candidate.(string); ok {
			if bs, ok2 := v.(string); ok2 && as == bs {
				return true
			}
		}
	}
	return false
}

func enumValues(enum any) []any {
	rv := reflect.ValueOf(enum)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil
	}
	out := make([]any, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out = append(out, rv.Index(i).Interface())
	}
	return out
}

func describeValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		if len(t) > 40 {
			t = t[:40] + "…"
		}
		return strconv.Quote(t)
	}
	s := fmt.Sprintf("%v", v)
	if len(s) > 40 {
		s = s[:40] + "…"
	}
	return fmt.Sprintf("%s (%s)", s, jsonKind(v))
}

// nameOf 构造面向模型的参数名：根对象为 arguments，其余为 "a.b[0].c"。
func nameOf(path string) string {
	if path == "" {
		return "arguments"
	}
	return fmt.Sprintf("argument %q", path)
}

func joinPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

func schemaObject(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	if !ok || m == nil {
		return nil, false
	}
	return m, true
}

func stringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
