package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// DefaultGateThreshold 完成率相对 baseline 允许的最大下降（pt）。下降超过 2pt 判为回归。
const DefaultGateThreshold = 0.02

// ShapeGateThreshold answer_shape 端到端通过率允许的最大下降；高于真实环境约 5pt 的自然波动。
const ShapeGateThreshold = 0.08

// ShapeMaxLostRate infra_error + judge_error 占比超过该值时本次运行无效。
const ShapeMaxLostRate = 0.20

// Diff baseline 对比结果。
type Diff struct {
	CompletionRateDelta float64    `json:"completion_rate_delta"`
	ToolF1Delta         float64    `json:"tool_selection_f1_delta"`
	GatePassed          bool       `json:"gate_passed"`
	Threshold           float64    `json:"threshold"`
	AnswerShape         *ShapeDiff `json:"answer_shape,omitempty"`
}

// ShapeDiff answer_shape 门禁结果。
type ShapeDiff struct {
	PassRateDelta float64  `json:"e2e_pass_rate_delta"`
	Threshold     float64  `json:"threshold"`
	LostRate      float64  `json:"lost_rate"`
	Invalid       bool     `json:"invalid,omitempty"`
	Regressed     []string `json:"regressed,omitempty"`
	GatePassed    bool     `json:"gate_passed"`
}

// EvaluateGate 对比 baseline 与当前 summary。
// 旧类别：baseline 为 nil 或 Total==0、或本次没有旧类别任务时不设门禁。
// answer_shape：丢失运行占比超限即失败（无需基线）；有基线时比较 e2e_pass_rate。
func EvaluateGate(summary Summary, baseline *Summary) *Diff {
	d := &Diff{Threshold: DefaultGateThreshold, GatePassed: true}
	d.AnswerShape = evaluateShapeGate(summary.AnswerShape, baseline)
	if baseline != nil && baseline.Total > 0 && summary.Total > 0 {
		d.CompletionRateDelta = summary.CompletionRate - baseline.CompletionRate
		d.ToolF1Delta = summary.ToolF1 - baseline.ToolF1
		d.GatePassed = d.CompletionRateDelta >= -DefaultGateThreshold
	}
	if d.AnswerShape != nil && !d.AnswerShape.GatePassed {
		d.GatePassed = false
	}
	return d
}

func evaluateShapeGate(cur *ShapeSummary, baseline *Summary) *ShapeDiff {
	if cur == nil {
		return nil
	}
	sd := &ShapeDiff{Threshold: ShapeGateThreshold, LostRate: cur.LostRate, GatePassed: true}
	if cur.LostRate > ShapeMaxLostRate {
		sd.Invalid = true
		sd.GatePassed = false
		return sd
	}
	if baseline == nil || baseline.AnswerShape == nil || baseline.AnswerShape.Runs == 0 {
		return sd
	}
	sd.PassRateDelta = cur.PassRate - baseline.AnswerShape.PassRate
	sd.GatePassed = sd.PassRateDelta >= -ShapeGateThreshold
	return sd
}

// TaskRegressions 返回基线中通过、本次未通过的任务 ID（CI 单题门禁）。本次没跑到或全部运行丢失的任务不算回归，后者由丢失率门禁约束。
func TaskRegressions(cur, base []TaskResult) []string {
	curByID := make(map[string]TaskResult, len(cur))
	for _, r := range cur {
		curByID[r.TaskID] = r
	}
	var out []string
	for _, b := range base {
		if !b.Passed {
			continue
		}
		if c, ok := curByID[b.TaskID]; ok && !c.Passed && !isLostRun(c) {
			out = append(out, b.TaskID)
		}
	}
	return out
}

func (d *Diff) applyRegressions(ids []string) {
	if len(ids) == 0 {
		return
	}
	if d.AnswerShape == nil {
		d.AnswerShape = &ShapeDiff{Threshold: ShapeGateThreshold}
	}
	d.AnswerShape.Regressed = ids
	d.AnswerShape.GatePassed = false
	d.GatePassed = false
}

// gateError 把未通过的门禁转成退出错误；通过时返回 nil。
func gateError(d *Diff) error {
	if d == nil || d.GatePassed {
		return nil
	}
	var parts []string
	if d.CompletionRateDelta < -d.Threshold {
		parts = append(parts, fmt.Sprintf("completion_rate dropped %.1fpt (threshold %.1fpt)", -d.CompletionRateDelta*100, d.Threshold*100))
	}
	if a := d.AnswerShape; a != nil && !a.GatePassed {
		if a.Invalid {
			parts = append(parts, fmt.Sprintf("answer_shape run invalid: %.0f%% runs lost to infra/judge errors (max %.0f%%)", a.LostRate*100, ShapeMaxLostRate*100))
		}
		if a.PassRateDelta < -a.Threshold {
			parts = append(parts, fmt.Sprintf("e2e_pass_rate dropped %.1fpt (threshold %.1fpt)", -a.PassRateDelta*100, a.Threshold*100))
		}
		if len(a.Regressed) > 0 {
			parts = append(parts, "answer_shape regressed tasks: "+strings.Join(a.Regressed, ", "))
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "gate failed")
	}
	return fmt.Errorf("regression gate failed: %s", strings.Join(parts, "; "))
}

// LoadBaseline 读取 baseline Summary JSON；path 为空返回 nil（无基线）。
func LoadBaseline(path string) (*Summary, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Summary
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
