package main

import (
	"encoding/json"
	"os"
	"time"
)

// Report 一次评测的完整报告。
type Report struct {
	Timestamp    string    `json:"timestamp"`
	Mode         string    `json:"mode"`
	Summary      Summary   `json:"summary"`
	Failures     []Failure `json:"failures"`
	BaselineDiff *Diff     `json:"baseline_diff,omitempty"`
}

// Failure 单条失败任务的归因。
type Failure struct {
	TaskID      string `json:"id"`
	Reason      string `json:"reason"`
	Attribution string `json:"attribution"`
}

// BuildReport 由汇总与结果构造报告。
func BuildReport(mode string, summary Summary, results []TaskResult, diff *Diff) Report {
	rep := Report{
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		Mode:         mode,
		Summary:      summary,
		Failures:     []Failure{},
		BaselineDiff: diff,
	}
	for _, r := range results {
		if r.Passed {
			continue
		}
		rep.Failures = append(rep.Failures, Failure{
			TaskID:      r.TaskID,
			Reason:      r.FailureReason,
			Attribution: r.Attribution,
		})
	}
	return rep
}

func writeReport(path string, rep Report) error {
	raw, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if path == "" || path == "-" {
		_, err = os.Stdout.Write(raw)
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}