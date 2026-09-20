package main

import (
	"encoding/json"
	"os"
)

// DefaultGateThreshold 完成率相对 baseline 允许的最大下降（pt）。下降超过 2pt 判为回归。
const DefaultGateThreshold = 0.02

// Diff baseline 对比结果。
type Diff struct {
	CompletionRateDelta float64 `json:"completion_rate_delta"`
	ToolF1Delta         float64 `json:"tool_selection_f1_delta"`
	GatePassed          bool    `json:"gate_passed"`
	Threshold           float64 `json:"threshold"`
}

// EvaluateGate 对比 baseline 与当前 summary。
// baseline 为 nil 或 Total==0 时视为无基线：GatePassed=true（不设门禁，等待首次基线）。
func EvaluateGate(summary Summary, baseline *Summary) *Diff {
	d := &Diff{Threshold: DefaultGateThreshold, GatePassed: true}
	if baseline == nil || baseline.Total == 0 {
		return d
	}
	d.CompletionRateDelta = summary.CompletionRate - baseline.CompletionRate
	d.ToolF1Delta = summary.ToolF1 - baseline.ToolF1
	d.GatePassed = d.CompletionRateDelta >= -DefaultGateThreshold
	return d
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