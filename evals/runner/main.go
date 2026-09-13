package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/sixath/framework/model"
)

// CLI 支持三种模式：
//   - report：读结果 JSONL，算汇总 + 报告，可选对比 baseline 做回归门禁。
//   - mock：用脚本化模型确定性驱动 ReActAgent 回放任务集（CI 回归门禁）。
//   - live：用真实模型驱动 ReActAgent 跑任务集，产出结果 JSONL + 报告（nightly 回填 baseline）。
func main() {
	mode := flag.String("mode", "report", "runner mode (report|mock|live)")
	resultsPath := flag.String("results", "", "path to results JSONL (report mode)")
	tasksPath := flag.String("tasks", "", "path to tasks JSONL file or directory")
	baselinePath := flag.String("baseline", "", "path to baseline Summary JSON (optional)")
	outPath := flag.String("out", "-", "report output path (default stdout)")
	resultsOutPath := flag.String("results-out", "", "path to write results JSONL (live mode)")
	baselineOutPath := flag.String("baseline-out", "", "path to write baseline Summary JSON")
	// live 模式模型配置
	provider := flag.String("provider", "openai", "model provider (openai|dashscope|ollama)")
	modelName := flag.String("model", "", "model name (live mode)")
	baseURL := flag.String("base-url", "", "model base URL (live mode)")
	apiKey := flag.String("api-key", os.Getenv("SATH_EVAL_API_KEY"), "model API key (live mode; or env SATH_EVAL_API_KEY)")
	flag.Parse()

	var err error
	switch *mode {
	case "mock":
		err = runMockMode(*tasksPath, *baselinePath, *outPath, *baselineOutPath)
	case "live":
		err = runLiveMode(*tasksPath, *baselinePath, *outPath, *resultsOutPath, *baselineOutPath, *provider, *modelName, *baseURL, *apiKey)
	default:
		err = runReport(*resultsPath, *tasksPath, *baselinePath, *outPath, *baselineOutPath, *mode)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval runner:", err)
		os.Exit(1)
	}
}

func runMockMode(tasksPath, baselinePath, outPath, baselineOutPath string) error {
	if tasksPath == "" {
		return fmt.Errorf("-tasks is required for mock mode")
	}
	tasks, err := LoadTasksFromPath(tasksPath)
	if err != nil {
		return err
	}
	results := runMock(tasks)
	summary := ComputeSummary(results, tasks)

	if baselineOutPath != "" {
		if err := writeBaseline(baselineOutPath, &summary); err != nil {
			return err
		}
	}

	var diff *Diff
	if baselinePath != "" {
		baseline, err := LoadBaseline(baselinePath)
		if err != nil {
			return err
		}
		diff = EvaluateGate(summary, baseline)
	}
	rep := BuildReport("mock", summary, results, diff)
	if err := writeReport(outPath, rep); err != nil {
		return err
	}
	if diff != nil && !diff.GatePassed {
		return fmt.Errorf("regression gate failed: completion_rate dropped %.1fpt (threshold %.1fpt)",
			diff.CompletionRateDelta*100, diff.Threshold*100)
	}
	return nil
}

func runLiveMode(tasksPath, baselinePath, outPath, resultsOutPath, baselineOutPath, provider, modelName, baseURL, apiKey string) error {
	if tasksPath == "" {
		return fmt.Errorf("-tasks is required for live mode")
	}
	if modelName == "" {
		return fmt.Errorf("-model is required for live mode")
	}
	if apiKey == "" {
		return fmt.Errorf("-api-key (or env SATH_EVAL_API_KEY) is required for live mode")
	}
	tasks, err := LoadTasksFromPath(tasksPath)
	if err != nil {
		return err
	}
	m, err := model.NewModelFromConfig(model.ModelConfig{
		Provider: provider,
		Model:    modelName,
		APIKey:   apiKey,
		BaseURL:  baseURL,
	})
	if err != nil {
		return err
	}
	results := runLive(tasks, m)
	summary := ComputeSummary(results, tasks)

	if resultsOutPath != "" {
		if err := writeResults(resultsOutPath, results); err != nil {
			return err
		}
	}
	if baselineOutPath != "" {
		if err := writeBaseline(baselineOutPath, &summary); err != nil {
			return err
		}
	}

	var diff *Diff
	if baselinePath != "" {
		baseline, err := LoadBaseline(baselinePath)
		if err != nil {
			return err
		}
		diff = EvaluateGate(summary, baseline)
	}
	rep := BuildReport("live", summary, results, diff)
	if err := writeReport(outPath, rep); err != nil {
		return err
	}
	if diff != nil && !diff.GatePassed {
		return fmt.Errorf("regression gate failed: completion_rate dropped %.1fpt (threshold %.1fpt)",
			diff.CompletionRateDelta*100, diff.Threshold*100)
	}
	return nil
}

// writeResults 把结果写为 JSONL（每行一个 TaskResult），供 report 回放与 baseline 生成。
func writeResults(path string, results []TaskResult) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, r := range results {
		raw, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if _, err := w.Write(raw); err != nil {
			return err
		}
		if err := w.WriteByte('\n'); err != nil {
			return err
		}
	}
	return w.Flush()
}

func runReport(resultsPath, tasksPath, baselinePath, outPath, baselineOutPath, mode string) error {
	if resultsPath == "" {
		return fmt.Errorf("-results is required")
	}
	results, err := LoadResults(resultsPath)
	if err != nil {
		return err
	}
	var tasks []Task
	if tasksPath != "" {
		tasks, err = LoadTasksFromPath(tasksPath)
		if err != nil {
			return err
		}
	}
	summary := ComputeSummary(results, tasks)

	if baselineOutPath != "" {
		if err := writeBaseline(baselineOutPath, &summary); err != nil {
			return err
		}
	}

	var diff *Diff
	if baselinePath != "" {
		baseline, err := LoadBaseline(baselinePath)
		if err != nil {
			return err
		}
		diff = EvaluateGate(summary, baseline)
	}

	rep := BuildReport(mode, summary, results, diff)
	if err := writeReport(outPath, rep); err != nil {
		return err
	}
	if diff != nil && !diff.GatePassed {
		return fmt.Errorf("regression gate failed: completion_rate dropped %.1fpt (threshold %.1fpt)",
			diff.CompletionRateDelta*100, diff.Threshold*100)
	}
	return nil
}

// writeBaseline 把 Summary 写为 baseline JSON。
func writeBaseline(path string, summary *Summary) error {
	raw, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}