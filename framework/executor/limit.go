package executor

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// ErrLimitPushdownUnsupported 表示无法安全自动包裹 LIMIT，需由调用方在 SQL 中显式添加。
var ErrLimitPushdownUnsupported = errors.New("executor: cannot auto-apply LIMIT to this query; add LIMIT explicitly")

var (
	limitSuffixRe = regexp.MustCompile(`(?i)\bLIMIT\s+\d+(\s*,\s*\d+)?(\s+OFFSET\s+\d+)?\s*$`)
	limitWrapBlock = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bFOR\s+UPDATE\b`),
		regexp.MustCompile(`(?i)\bLOCK\s+IN\s+SHARE\s+MODE\b`),
		regexp.MustCompile(`(?i)\bINTO\s+(OUTFILE|DUMPFILE)\b`),
	}
)

// hasLimitClause 检测 SQL 末尾是否已有 LIMIT（不解析字符串字面量，仅看尾部）。
func hasLimitClause(cleaned string) bool {
	s := strings.TrimSpace(cleaned)
	s = strings.TrimSuffix(s, ";")
	s = strings.TrimSpace(s)
	return limitSuffixRe.MatchString(s)
}

func limitWrapBlocked(cleaned string) string {
	for _, re := range limitWrapBlock {
		if m := re.FindString(cleaned); m != "" {
			return m
		}
	}
	return ""
}

// applyMaxRowsToSQL 在无 LIMIT 时将查询包裹为子查询并施加 LIMIT；已有 LIMIT 时原样返回。
func applyMaxRowsToSQL(cleaned string, maxRows int) (string, error) {
	if maxRows <= 0 || cleaned == "" {
		return cleaned, nil
	}
	if hasLimitClause(cleaned) {
		return cleaned, nil
	}
	if reason := limitWrapBlocked(cleaned); reason != "" {
		return "", fmt.Errorf("%w: matched %s", ErrLimitPushdownUnsupported, reason)
	}
	return fmt.Sprintf("SELECT * FROM (%s) AS _limited LIMIT %d", cleaned, maxRows), nil
}

// injectESSearchSize 将 MaxRows 下推到 ES search body 的 size 字段（仅 clamp 上限，不覆盖更小的 size）。
func injectESSearchSize(body string, maxRows int) (string, error) {
	if maxRows <= 0 {
		return body, nil
	}
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		trimmed = "{}"
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(trimmed), &m); err != nil {
		return "", fmt.Errorf("executor: parse es search body: %w", err)
	}
	cur, ok := jsonNumberAsInt(m["size"])
	if !ok || cur > maxRows {
		m["size"] = maxRows
	}
	out, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("executor: marshal es search body: %w", err)
	}
	return string(out), nil
}

func jsonNumberAsInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if n < 0 || n > math.MaxInt || n != math.Trunc(n) {
			return 0, false
		}
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}
