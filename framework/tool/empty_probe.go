package tool

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sixath/framework/executor"
)

// EnvToolEmptyProbe=off/0/false 全局关闭零结果探测（默认开启）。
const EnvToolEmptyProbe = "SATH_TOOL_EMPTY_PROBE"

const (
	emptyProbeMaxVariants = 3
	emptyProbeBudget      = 3 * time.Second
	// 给工具自身收尾（序列化、spill）留出的余量，避免探测把成功的查询拖成超时。
	emptyProbeDeadlineMargin = 200 * time.Millisecond
)

// EmptyProbe 在结果为 0 条（hit_status=empty）时，按 Relax 给出的放宽变体逐个计数。
// 预算（变体数、超时）由中间件统一控制，工具只负责生成变体与计数。
type EmptyProbe struct {
	Relax func(params map[string]any) []ProbeVariant
	Count func(ctx context.Context, v ProbeVariant) (int64, error)
}

type ProbeVariant struct {
	Label  string
	Params map[string]any
}

func emptyProbeEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvToolEmptyProbe))) {
	case "0", "false", "off", "no", "disable", "disabled":
		return false
	}
	return true
}

func runEmptyProbe(ctx context.Context, p *EmptyProbe, params map[string]any, result any) any {
	if p == nil || p.Relax == nil || p.Count == nil || !emptyProbeEnabled() {
		return result
	}
	if st, _, _ := HitContractFromResult(result); st != HitStatusEmpty {
		return result
	}
	variants := p.Relax(params)
	if len(variants) == 0 {
		return result
	}
	budget := emptyProbeBudget
	if dl, ok := ctx.Deadline(); ok {
		if left := time.Until(dl) - emptyProbeDeadlineMargin; left < budget {
			budget = left
		}
	}
	if budget <= 0 {
		return result
	}
	pctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	d := &executor.Diagnosis{}
	if len(variants) > emptyProbeMaxVariants {
		variants = variants[:emptyProbeMaxVariants]
		d.Truncated = true
	}
	var best *executor.ProbeCount
	for _, v := range variants {
		if pctx.Err() != nil {
			d.Truncated = true
			break
		}
		n, err := p.Count(pctx, v)
		if err != nil {
			d.Errors = append(d.Errors, fmt.Sprintf("%s: %v", v.Label, err))
			continue
		}
		d.Probes = append(d.Probes, executor.ProbeCount{Label: v.Label, Count: n})
		if n > 0 && best == nil {
			pc := d.Probes[len(d.Probes)-1]
			best = &pc
		}
	}
	if len(d.Probes) == 0 && len(d.Errors) == 0 {
		return result
	}
	if best == nil {
		d.Hint = "relaxed variants also returned 0; the data likely does not exist in this range"
		return AttachDiagnosis(result, d)
	}
	d.Hint = fmt.Sprintf("original query returned 0 but %s returned %d; a condition is probably wrong (field/value/time range) — fix it before concluding there is no data", best.Label, best.Count)
	return MarkSuspect(result, d)
}
