package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/model"
)

// ToolGuardrailsConfig 工具护栏（设计 §6.3）。Enabled=false 或未配置时行为与现网一致。
// HardHalt 为 false 时等价于 warnings_only：仅发 ToolGuardrailWarn，不中断 Run。
type ToolGuardrailsConfig struct {
	Enabled bool

	// HardHalt 为 true 时，达到 Same*FailureHalt 阈值将返回 ErrToolGuardrailHalt 并设置 trace.GuardrailHalt。
	HardHalt bool

	// SameArgsFailureWarn R1：连续同工具、同规范化错误、同 StableArgsKey 的失败次数达到阈值则告警；0 表示用默认 2。
	SameArgsFailureWarn int
	SameArgsFailureHalt int // 0 表示不因 R1 硬停
	// SameToolFailureWarn R2：连续同工具名且 Error 非空；0 表示用默认 3。
	SameToolFailureWarn int
	SameToolFailureHalt int // 0 表示不因 R2 硬停

	// IdempotentTools / MutatingTools 为 nil 时使用设计 §6.3 默认名单；空切片表示显式清空该侧名单。
	IdempotentTools []string
	MutatingTools   []string
	// IdempotentRelaxMultiplier 幂等工具 R1/R2 的 warn/halt 阈值倍率（>1 更宽松）；0 视为 2。
	IdempotentRelaxMultiplier int

	// NoProgressToolOnlyWarn R3（设计 §6.1）：连续多轮模型在 ChatWithTools 中仍选择「仅调工具」路径（无最终 assistant 文本）；0 关闭。
	NoProgressToolOnlyWarn int
	// NoProgressToolOnlyHalt R3 硬停阈值；0 表示不因 R3 硬停。
	NoProgressToolOnlyHalt int
}

var (
	defaultIdempotentToolNames = []string{"memory_search", "load_skill", "read_skill_file"}
	defaultMutatingToolNames   = []string{"ssh_exec", "execute_skill_script", "execute_read"}
)

// GuardrailHaltSystemMessage 返回带 Metadata[sixath.origin]=guardrail_halt 的 system 提示（设计 §3.1、§6.2）。
func GuardrailHaltSystemMessage() model.Message {
	return model.Message{
		Role:    "system",
		Content: "工具护栏已硬停：重复工具失败超过配置阈值，本回合结束。",
		Metadata: map[string]any{
			model.MetadataKeySixathOrigin: model.OriginGuardrailHalt,
		},
	}
}

// appendGuardrailHaltForTrace 写入 trace.GuardrailHaltMessage，并将同一条 system 消息追加到 messages（设计 §6.2）。
func appendGuardrailHaltForTrace(trace *RunTrace, messages []model.Message) []model.Message {
	if trace == nil {
		return messages
	}
	m := GuardrailHaltSystemMessage()
	copyMsg := m
	trace.GuardrailHaltMessage = &copyMsg
	return append(messages, m)
}

// StableArgsKey returns sha256hex(canonicalJSON(args)) for R1 same-args guardrail
// matching (design §6.1). Nil or empty maps canonicalize to "{}".
func StableArgsKey(args map[string]any) string {
	b, err := CanonicalJSON(args)
	if err != nil {
		sum := sha256.Sum256([]byte("__canonical_json_error__:" + err.Error()))
		return hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// CanonicalJSON returns deterministic JSON bytes: object keys sorted lexicographically,
// null object values and null array elements omitted, json.Number coerced like numeric
// literals, whole floats emitted as JSON integers when representable as int64.
func CanonicalJSON(args map[string]any) ([]byte, error) {
	n := normalizeMap(args)
	return json.Marshal(n)
}

func normalizeMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]any, len(keys))
	for _, k := range keys {
		if m[k] == nil {
			continue
		}
		nv := normalizeValue(m[k])
		if nv == nil {
			continue
		}
		out[k] = nv
	}
	return out
}

func normalizeValue(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case bool, string:
		return x
	case json.Number:
		return normalizeJSONNumber(x)
	case float32:
		return normalizeFloat(float64(x))
	case float64:
		return normalizeFloat(x)
	case int:
		return normalizeFloat(float64(x))
	case int8:
		return normalizeFloat(float64(x))
	case int16:
		return normalizeFloat(float64(x))
	case int32:
		return normalizeFloat(float64(x))
	case int64:
		return normalizeFloat(float64(x))
	case uint:
		return normalizeFloat(float64(x))
	case uint8:
		return normalizeFloat(float64(x))
	case uint16:
		return normalizeFloat(float64(x))
	case uint32:
		return normalizeFloat(float64(x))
	case uint64:
		if x <= uint64(math.MaxInt64) {
			return normalizeFloat(float64(x))
		}
		return fmt.Sprintf("%d", x)
	case map[string]any:
		return normalizeMap(x)
	case []any:
		out := make([]any, 0, len(x))
		for _, e := range x {
			if e == nil {
				continue
			}
			nv := normalizeValue(e)
			if nv == nil {
				continue
			}
			out = append(out, nv)
		}
		return out
	case json.RawMessage:
		var inner any
		if err := json.Unmarshal(x, &inner); err != nil {
			return string(x)
		}
		return normalizeValue(inner)
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprint(x)
		}
		var w any
		if err := json.Unmarshal(b, &w); err != nil {
			return fmt.Sprint(x)
		}
		return normalizeValue(w)
	}
}

func normalizeJSONNumber(n json.Number) any {
	if i, err := n.Int64(); err == nil {
		return i
	}
	f, err := n.Float64()
	if err != nil {
		return n.String()
	}
	return normalizeFloat(f)
}

func normalizeFloat(f float64) any {
	if math.IsNaN(f) {
		return "__NaN__"
	}
	if math.IsInf(f, 1) {
		return "__Inf__"
	}
	if math.IsInf(f, -1) {
		return "__NegInf__"
	}
	tr := math.Trunc(f)
	if tr == f && f >= float64(math.MinInt64) && f <= float64(math.MaxInt64) {
		return int64(f)
	}
	return f
}

func toolGuardrailsEffective(c *ToolGuardrailsConfig) *ToolGuardrailsConfig {
	if c == nil || !c.Enabled {
		return nil
	}
	out := *c
	if c.IdempotentTools == nil {
		out.IdempotentTools = append([]string(nil), defaultIdempotentToolNames...)
	} else {
		out.IdempotentTools = append([]string(nil), c.IdempotentTools...)
	}
	if c.MutatingTools == nil {
		out.MutatingTools = append([]string(nil), defaultMutatingToolNames...)
	} else {
		out.MutatingTools = append([]string(nil), c.MutatingTools...)
	}
	if out.SameArgsFailureWarn <= 0 {
		out.SameArgsFailureWarn = 2
	}
	if out.SameToolFailureWarn <= 0 {
		out.SameToolFailureWarn = 3
	}
	return &out
}

func guardrailTierMultiplier(cfg *ToolGuardrailsConfig, toolName string) int {
	for _, s := range cfg.MutatingTools {
		if s == toolName {
			return 1
		}
	}
	for _, s := range cfg.IdempotentTools {
		if s == toolName {
			if cfg.IdempotentRelaxMultiplier <= 0 {
				return 2
			}
			return cfg.IdempotentRelaxMultiplier
		}
	}
	return 1
}

func effectiveR1WarnThreshold(cfg *ToolGuardrailsConfig, toolName string) int {
	return cfg.SameArgsFailureWarn * guardrailTierMultiplier(cfg, toolName)
}

func effectiveR1HaltThreshold(cfg *ToolGuardrailsConfig, toolName string) int {
	if cfg.SameArgsFailureHalt <= 0 {
		return 0
	}
	return cfg.SameArgsFailureHalt * guardrailTierMultiplier(cfg, toolName)
}

func effectiveR2WarnThreshold(cfg *ToolGuardrailsConfig, toolName string) int {
	return cfg.SameToolFailureWarn * guardrailTierMultiplier(cfg, toolName)
}

func effectiveR2HaltThreshold(cfg *ToolGuardrailsConfig, toolName string) int {
	if cfg.SameToolFailureHalt <= 0 {
		return 0
	}
	return cfg.SameToolFailureHalt * guardrailTierMultiplier(cfg, toolName)
}

func applyToolGuardrails(cfg *ToolGuardrailsConfig, history []ToolCallRecord, emit func(events.Kind, map[string]any), consecutiveToolOnlyModelRounds int) (halt bool) {
	cfg = toolGuardrailsEffective(cfg)
	if cfg == nil || emit == nil {
		return false
	}
	// R3 仅依赖连续「仅工具」轮次，可在尚无失败 tool 记录时独立触发。
	if cfg.NoProgressToolOnlyWarn > 0 && consecutiveToolOnlyModelRounds >= cfg.NoProgressToolOnlyWarn {
		emit(events.ToolGuardrailWarn, map[string]any{
			"rule":             "no_progress",
			"streak":           consecutiveToolOnlyModelRounds,
			"threshold_warn":   cfg.NoProgressToolOnlyWarn,
			"threshold_halt":   cfg.NoProgressToolOnlyHalt,
		})
	}
	if cfg.HardHalt && cfg.NoProgressToolOnlyHalt > 0 && consecutiveToolOnlyModelRounds >= cfg.NoProgressToolOnlyHalt {
		return true
	}
	if len(history) == 0 {
		return false
	}
	last := history[len(history)-1]
	if strings.TrimSpace(last.Error) == "" {
		return false
	}
	tool := last.ToolName
	r1 := tailR1FailureStreak(history)
	warn1 := effectiveR1WarnThreshold(cfg, tool)
	if r1 >= warn1 {
		emit(events.ToolGuardrailWarn, map[string]any{
			"rule":             "same_args_failure",
			"tool":             tool,
			"step":             last.Step,
			"streak":           r1,
			"threshold_warn":   warn1,
			"tier_multiplier":  guardrailTierMultiplier(cfg, tool),
			"stable_args_key":  StableArgsKey(last.Arguments),
		})
	}
	halt1 := effectiveR1HaltThreshold(cfg, tool)
	if cfg.HardHalt && halt1 > 0 && r1 >= halt1 {
		return true
	}
	r2 := tailR2FailureStreak(history)
	warn2 := effectiveR2WarnThreshold(cfg, tool)
	if r2 >= warn2 {
		emit(events.ToolGuardrailWarn, map[string]any{
			"rule":             "same_tool_failure",
			"tool":             tool,
			"step":             last.Step,
			"streak":           r2,
			"threshold_warn":   warn2,
			"tier_multiplier":  guardrailTierMultiplier(cfg, tool),
		})
	}
	halt2 := effectiveR2HaltThreshold(cfg, tool)
	if cfg.HardHalt && halt2 > 0 && r2 >= halt2 {
		return true
	}
	return false
}

func tailR1FailureStreak(history []ToolCallRecord) int {
	if len(history) == 0 {
		return 0
	}
	last := history[len(history)-1]
	if strings.TrimSpace(last.Error) == "" {
		return 0
	}
	key := r1MatchKey(last)
	n := 1
	for i := len(history) - 2; i >= 0; i-- {
		r := history[i]
		if strings.TrimSpace(r.Error) == "" {
			break
		}
		if r1MatchKey(r) != key {
			break
		}
		n++
	}
	return n
}

func r1MatchKey(r ToolCallRecord) string {
	return r.ToolName + "\x00" + strings.TrimSpace(r.Error) + "\x00" + StableArgsKey(r.Arguments)
}

func tailR2FailureStreak(history []ToolCallRecord) int {
	if len(history) == 0 {
		return 0
	}
	last := history[len(history)-1]
	if strings.TrimSpace(last.Error) == "" {
		return 0
	}
	name := last.ToolName
	n := 1
	for i := len(history) - 2; i >= 0; i-- {
		r := history[i]
		if strings.TrimSpace(r.Error) == "" || r.ToolName != name {
			break
		}
		n++
	}
	return n
}
