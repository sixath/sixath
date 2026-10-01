package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/investigate/cases"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

// CLI 支持四种模式：
//   - report：读结果 JSONL，算汇总 + 报告，可选对比 baseline 做回归门禁。
//   - mock：用脚本化模型确定性驱动 ReActAgent 回放任务集（CI 回归门禁）。
//   - live：用真实模型驱动 ReActAgent 跑任务集，产出结果 JSONL + 报告（nightly 回填 baseline）。
//   - portal：通过 Portal/Gateway 对话接口驱动真实 agent 跑 answer_shape 任务，judge 打分（nightly 真实环境）。
func main() {
	mode := flag.String("mode", "report", "runner mode (report|mock|live|portal)")
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
	stopRulesPath := flag.String("stop-rules", "", "workspace hooks.yaml with stop_rules to attach in live mode (optional, for A/B)")
	ledger := flag.Bool("ledger", false, "register the investigation ledger tool in live mode (optional, for A/B)")
	critic := flag.Bool("critic", false, "attach the closing critic (rule tier + model tier) in live mode (optional, for A/B); settings come from the critic section of -stop-rules when present")
	timeline := flag.Bool("timeline", false, "register the timeline tool in live mode (optional, for A/B)")
	extraSteps := flag.Int("extra-steps", 0, "added to every task's max_steps in live mode; use the same value for both A/B arms")
	maxOutputTokens := flag.Int("max-output-tokens", 0, "max tokens per model reply in live mode (0 = framework default 1024; long conclusions get cut off)")
	casesDir := flag.String("cases", "", "case library directory for live mode (requires -ledger); confirmed cases in it are recalled by set_symptom")
	judgeProvider := flag.String("judge-provider", "", "judge model provider (default: -provider)")
	judgeModel := flag.String("judge-model", "", "judge model name; required when tasks include answer_shape")
	judgeBaseURL := flag.String("judge-base-url", "", "judge model base URL (default: -base-url)")
	judgeAPIKey := flag.String("judge-api-key", os.Getenv("SATH_EVAL_JUDGE_API_KEY"), "judge API key (or env SATH_EVAL_JUDGE_API_KEY; default: -api-key)")
	sutModel := flag.String("sut-model", "", "model under test, used to refuse judging with the same model (live mode default: -model)")
	repeat := flag.Int("repeat", 1, "runs per answer_shape task; results are merged by majority")
	baselineResults := flag.String("baseline-results", "", "results JSONL used as per-task baseline (answer_shape CI gate)")
	portalURL := flag.String("portal-url", "", "Portal or Gateway base URL (portal mode)")
	agentID := flag.String("agent", "", "agent id to evaluate (portal mode)")
	portalToken := flag.String("token", os.Getenv("SATH_EVAL_PORTAL_TOKEN"), "bearer token (portal mode; or env SATH_EVAL_PORTAL_TOKEN)")
	orgID := flag.String("org", "default", "X-Org-Id header (portal mode)")
	concurrency := flag.Int("concurrency", 2, "parallel tasks (portal mode)")
	turnTimeout := flag.Duration("turn-timeout", 10*time.Minute, "per-turn timeout from send to final reply (portal mode)")
	gracePeriod := flag.Duration("grace-period", 2*time.Minute, "extra wait for the final reply after -turn-timeout (portal mode)")
	judgeTimeout := flag.Duration("judge-timeout", 2*time.Minute, "per judge call timeout (live/portal mode)")
	flag.Parse()
	jf := judgeFlags{Provider: *judgeProvider, Model: *judgeModel, BaseURL: *judgeBaseURL, APIKey: *judgeAPIKey, SUTModel: *sutModel}

	var err error
	switch *mode {
	case "mock":
		err = runMockMode(*tasksPath, *baselinePath, *outPath, *baselineOutPath)
	case "live":
		err = runLiveMode(*tasksPath, *baselinePath, *outPath, *resultsOutPath, *baselineOutPath, *provider, *modelName, *baseURL, *apiKey, *stopRulesPath, liveFlags{Ledger: *ledger, Critic: *critic, Timeline: *timeline, ExtraSteps: *extraSteps, CasesDir: *casesDir, MaxOutputTokens: *maxOutputTokens, Repeat: *repeat, BaselineResults: *baselineResults, Judge: jf, JudgeTimeout: *judgeTimeout})
	case "portal":
		var j *Judge
		j, err = buildJudge(jf, *provider, *baseURL, *apiKey)
		if err == nil {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			err = runPortalMode(portalModeConfig{
				Ctx:       ctx,
				TasksPath: *tasksPath, PortalURL: *portalURL, AgentID: *agentID, Token: *portalToken, OrgID: *orgID,
				Repeat: *repeat, Concurrency: *concurrency, TurnTimeout: *turnTimeout, GracePeriod: *gracePeriod,
				JudgeTimeout: *judgeTimeout, Judge: j,
				Finish: finishOpts{ResultsOut: *resultsOutPath, BaselineOut: *baselineOutPath, Baseline: *baselinePath, BaselineResults: *baselineResults, Out: *outPath},
			})
			stop()
		}
	default:
		err = runReport(*resultsPath, *tasksPath, *baselinePath, *baselineResults, *outPath, *baselineOutPath, *mode)
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
	return finishRun("mock", tasks, results, finishOpts{BaselineOut: baselineOutPath, Baseline: baselinePath, Out: outPath})
}

// liveFlags 为 live 模式 A/B 开关。
type liveFlags struct {
	Ledger, Critic, Timeline bool
	ExtraSteps               int
	CasesDir                 string
	MaxOutputTokens          int
	Repeat                   int
	BaselineResults          string
	Judge                    judgeFlags
	JudgeTimeout             time.Duration
}

// judgeFlags 为 answer_shape 打分器配置；空字段回落到被测模型的同名配置。
type judgeFlags struct {
	Provider, Model, BaseURL, APIKey, SUTModel string
}

func buildJudge(jf judgeFlags, fallbackProvider, fallbackBaseURL, fallbackAPIKey string) (*Judge, error) {
	name := strings.TrimSpace(jf.Model)
	if name == "" {
		return nil, nil
	}
	if sut := strings.TrimSpace(jf.SUTModel); sut != "" && strings.EqualFold(sut, name) {
		return nil, fmt.Errorf("-judge-model %q must differ from the model under test", name)
	}
	cfg := model.ModelConfig{Provider: jf.Provider, Model: name, BaseURL: jf.BaseURL, APIKey: jf.APIKey}
	if cfg.Provider == "" {
		cfg.Provider = fallbackProvider
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = fallbackBaseURL
	}
	if cfg.APIKey == "" {
		cfg.APIKey = fallbackAPIKey
	}
	m, err := model.NewModelFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("judge model: %w", err)
	}
	return &Judge{Model: m}, nil
}

func hasAnswerShape(tasks []Task) bool {
	for _, t := range tasks {
		if t.Category == "answer_shape" {
			return true
		}
	}
	return false
}

func runLiveMode(tasksPath, baselinePath, outPath, resultsOutPath, baselineOutPath, provider, modelName, baseURL, apiKey, stopRulesPath string, flags liveFlags) error {
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
	lo := liveOptions{Ledger: flags.Ledger, Timeline: flags.Timeline, ExtraSteps: flags.ExtraSteps, MaxOutputTokens: flags.MaxOutputTokens, LedgerStore: tool.NewInMemoryInvestigationStore()}
	if dir := strings.TrimSpace(flags.CasesDir); dir != "" {
		if !flags.Ledger {
			return fmt.Errorf("-cases requires -ledger")
		}
		lo.Cases = cases.NewFileStore(dir)
	}
	var criticCfg *agent.CriticConfig
	if stopRulesPath != "" {
		data, err := os.ReadFile(stopRulesPath)
		if err != nil {
			return err
		}
		lo.StopHooks, lo.MaxStopNudges, err = agent.ParseHarnessStopHooksYAML(data, agent.StopHookOptions{
			Todos:          tool.NewInMemoryTodoStore(),
			Investigations: lo.LedgerStore,
		})
		if err != nil {
			return err
		}
		if cfg, ok := agent.ParseCriticConfigYAML(data); ok {
			criticCfg = &cfg
		}
	}
	if flags.Critic {
		if criticCfg == nil {
			criticCfg = &agent.CriticConfig{}
		}
		cfg := *criticCfg
		cfg.Enabled = true
		lo.Critic = &cfg
		if name := strings.TrimSpace(cfg.Model); name != "" {
			cm, err := model.NewModelFromConfig(model.ModelConfig{Provider: provider, Model: name, APIKey: apiKey, BaseURL: baseURL})
			if err != nil {
				return fmt.Errorf("critic model: %w", err)
			}
			lo.CriticModel = cm
		}
	}
	lo.Repeat = flags.Repeat
	lo.JudgeTimeout = flags.JudgeTimeout
	if flags.Judge.SUTModel == "" {
		flags.Judge.SUTModel = modelName
	}
	lo.Judge, err = buildJudge(flags.Judge, provider, baseURL, apiKey)
	if err != nil {
		return err
	}
	if lo.Judge == nil && hasAnswerShape(tasks) {
		return fmt.Errorf("-judge-model is required for answer_shape tasks")
	}
	results := runLive(tasks, m, lo)
	return finishRun("live", tasks, results, finishOpts{
		ResultsOut: resultsOutPath, BaselineOut: baselineOutPath, Baseline: baselinePath,
		BaselineResults: flags.BaselineResults, Out: outPath,
	})
}

// portalModeConfig portal 模式参数。Ctx 为 nil 时用 context.Background()；取消后停止派发，已有结果照常收尾。
type portalModeConfig struct {
	Ctx                                         context.Context
	TasksPath, PortalURL, AgentID, Token, OrgID string
	Repeat, Concurrency                         int
	TurnTimeout, GracePeriod, JudgeTimeout      time.Duration
	Judge                                       *Judge
	Finish                                      finishOpts
}

func runPortalMode(cfg portalModeConfig) error {
	if cfg.TasksPath == "" {
		return fmt.Errorf("-tasks is required for portal mode")
	}
	if strings.TrimSpace(cfg.PortalURL) == "" {
		return fmt.Errorf("-portal-url is required for portal mode")
	}
	if strings.TrimSpace(cfg.AgentID) == "" {
		return fmt.Errorf("-agent is required for portal mode")
	}
	if cfg.Judge == nil {
		return fmt.Errorf("-judge-model is required for portal mode")
	}
	all, err := LoadTasksFromPath(cfg.TasksPath)
	if err != nil {
		return err
	}
	var tasks []Task
	for _, t := range all {
		if t.Category == "answer_shape" {
			tasks = append(tasks, t)
		}
	}
	if len(tasks) == 0 {
		return fmt.Errorf("portal mode runs answer_shape tasks only; none found in %s", cfg.TasksPath)
	}
	if skipped := len(all) - len(tasks); skipped > 0 {
		fmt.Fprintf(os.Stderr, "eval runner: portal mode skips %d non-answer_shape task(s)\n", skipped)
	}
	timeout := cfg.TurnTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	client := &PortalClient{
		BaseURL:      strings.TrimRight(strings.TrimSpace(cfg.PortalURL), "/"),
		Token:        cfg.Token,
		OrgID:        cfg.OrgID,
		HTTP:         &http.Client{},
		Timeout:      timeout,
		PollInterval: 3 * time.Second,
		GracePeriod:  cfg.GracePeriod,
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	runID := time.Now().UTC().Format("20060102T150405")
	results := runPortal(ctx, tasks, client, portalRunOptions{
		AgentID: cfg.AgentID, RunID: runID, Judge: cfg.Judge, JudgeTimeout: cfg.JudgeTimeout,
		Repeat: cfg.Repeat, Concurrency: cfg.Concurrency,
	})
	return finishRun("portal", tasks, results, cfg.Finish)
}

// finishOpts 为一次运行收尾时的输出与门禁配置。
type finishOpts struct {
	ResultsOut, BaselineOut, Baseline, BaselineResults, Out string
}

// finishRun 写结果，计算门禁（含 answer_shape 单题回归），输出报告；
// 门禁失败时不写基线，避免无效或回归的运行覆盖基线、静默关闭下次门禁。
func finishRun(mode string, tasks []Task, results []TaskResult, o finishOpts) error {
	summary := ComputeSummary(results, tasks)
	if o.ResultsOut != "" {
		if err := writeResults(o.ResultsOut, results); err != nil {
			return err
		}
	}
	baseline, err := LoadBaseline(o.Baseline)
	if err != nil {
		return err
	}
	var diff *Diff
	if baseline != nil || summary.AnswerShape != nil || o.BaselineResults != "" {
		diff = EvaluateGate(summary, baseline)
	}
	if o.BaselineResults != "" {
		base, err := LoadResults(o.BaselineResults)
		if err != nil {
			return err
		}
		diff.applyRegressions(TaskRegressions(results, base))
	}
	if err := writeReport(o.Out, BuildReport(mode, summary, results, diff)); err != nil {
		return err
	}
	gateErr := gateError(diff)
	if o.BaselineOut != "" {
		if gateErr != nil {
			fmt.Fprintf(os.Stderr, "eval runner: gate failed, baseline not written to %s\n", o.BaselineOut)
		} else if err := writeBaseline(o.BaselineOut, &summary); err != nil {
			return err
		}
	}
	return gateErr
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

func runReport(resultsPath, tasksPath, baselinePath, baselineResultsPath, outPath, baselineOutPath, mode string) error {
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
	return finishRun(mode, tasks, results, finishOpts{
		BaselineOut: baselineOutPath, Baseline: baselinePath, BaselineResults: baselineResultsPath, Out: outPath,
	})
}

// writeBaseline 把 Summary 写为 baseline JSON。
func writeBaseline(path string, summary *Summary) error {
	raw, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}