package chat

import (
	"context"
	"encoding/json"
	"strings"

	"backend/internal/biz"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/memory"
)

// DetectResult is the output of the evolution detection pipeline.
type DetectResult struct {
	IsSignal   bool    `json:"is_signal"`
	SignalType string  `json:"signal_type"`
	Confidence float64 `json:"confidence"`
	Summary    string  `json:"summary"`
}

// classResult is the LLM classification response.
type classResult struct {
	IsSignal   bool    `json:"is_signal"`
	SignalType string  `json:"signal_type"`
	Confidence float64 `json:"confidence"`
	Summary    string  `json:"summary"`
}

// DetectEvolutionSignals runs the full detection pipeline for a completed turn.
// Returns nil if no signal detected. Fully async-safe, fail-open.
func DetectEvolutionSignals(
	ctx context.Context,
	messages []*biz.ChatMessage,
	loadedSkills []string,
	failureSignals []memory.FailureSignal,
	trialBuf *TrialBuffer,
) *DetectResult {
	cfg := EvolutionConfig()
	if cfg == nil {
		return nil
	}

	userMsg := lastUserMessage(messages)
	if userMsg == "" {
		return nil
	}

	// Phase 1: Keyword rule filtering
	ruleHits := keywordMatch(userMsg, cfg)

	// Phase 1.5: Trial-and-error detection
	if len(failureSignals) > 0 && trialBuf != nil {
		factualFailures := filterFactualErrors(failureSignals)
		if len(factualFailures) >= cfg.TrialAndError.MinToolFailures {
			problem := buildTrialProblemSummary(factualFailures, messages)
			if problem != "" {
				met := trialBuf.Record(
					agentIDFromCtx(ctx),
					problem,
					len(factualFailures),
					extractSuccessSteps(messages),
					cfg.TrialAndError.MinToolFailures,
					cfg.TrialAndError.MinOccurrences,
				)
				if met {
					ruleHits = append(ruleHits, "trial_error")
				}
			}
		}
	}

	if len(ruleHits) == 0 {
		return nil
	}

	// Phase 2: LLM classification (only if rules hit)
	classifierCfg := resolveClassifier(cfg)
	if classifierCfg == nil {
		return nil
	}

	prompt := buildClassificationPrompt(messages, ruleHits)
	resp, err := callClassifier(ctx, classifierCfg, prompt, cfg.Classifier.MaxTokens)
	if err != nil {
		return nil // fail-open
	}

	var cr classResult
	if err := json.Unmarshal([]byte(resp), &cr); err != nil {
		return nil
	}

	if !cr.IsSignal || cr.Confidence < 0.5 {
		return nil
	}

	return &DetectResult{
		IsSignal:   true,
		SignalType: cr.SignalType,
		Confidence: cr.Confidence,
		Summary:    cr.Summary,
	}
}

func keywordMatch(userMsg string, cfg *config.EvolutionConfig) []string {
	lower := strings.ToLower(userMsg)
	var hits []string
	for _, kw := range cfg.Rules.StyleCorrection {
		if strings.Contains(lower, strings.ToLower(kw)) {
			hits = append(hits, "style_correction")
			break
		}
	}
	for _, kw := range cfg.Rules.WorkflowCorrection {
		if strings.Contains(lower, strings.ToLower(kw)) {
			hits = append(hits, "workflow_correction")
			break
		}
	}
	for _, kw := range cfg.Rules.DebuggingTrick {
		if strings.Contains(lower, strings.ToLower(kw)) {
			hits = append(hits, "debugging_trick")
			break
		}
	}
	for _, kw := range cfg.Rules.StaleSkill {
		if strings.Contains(lower, strings.ToLower(kw)) {
			hits = append(hits, "stale_skill")
			break
		}
	}
	return hits
}

func buildClassificationPrompt(messages []*biz.ChatMessage, ruleHits []string) string {
	var sb strings.Builder
	sb.WriteString("判断以下对话是否包含需要记录为技能进化信号的内容。\n\n")
	sb.WriteString("可能的信号类型: " + strings.Join(ruleHits, ", ") + "\n\n")
	sb.WriteString("对话内容:\n")
	for i := len(messages) - 1; i >= 0 && sb.Len() < 3000; i-- {
		if messages[i].Role == "user" {
			sb.WriteString("用户: " + truncateStr(messages[i].Content, 500) + "\n")
		}
	}
	sb.WriteString("\n输出JSON: {\"is_signal\":bool,\"signal_type\":\"string\",\"confidence\":0.0-1.0,\"summary\":\"一句话描述\"}")
	return sb.String()
}

func lastUserMessage(messages []*biz.ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i] != nil && messages[i].Role == "user" && strings.TrimSpace(messages[i].Content) != "" {
			return messages[i].Content
		}
	}
	return ""
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func resolveClassifier(cfg *config.EvolutionConfig) interface{} {
	return nil
}

func callClassifier(ctx context.Context, provider interface{}, prompt string, maxTokens int) (string, error) {
	return "", nil
}

// filterFactualErrors returns only tool failure signals whose error message matches factual error patterns.
func filterFactualErrors(signals []memory.FailureSignal) []memory.FailureSignal {
	var result []memory.FailureSignal
	for _, s := range signals {
		if s.Code == "tool_failed" && IsFactualError(s.Message) {
			result = append(result, s)
		}
	}
	return result
}

// buildTrialProblemSummary constructs a one-line problem description from factual failure messages.
func buildTrialProblemSummary(failures []memory.FailureSignal, messages []*biz.ChatMessage) string {
	if len(failures) == 0 {
		return ""
	}
	return strings.TrimSpace(failures[0].Message)
}

// extractSuccessSteps extracts the final successful tool calls from messages.
func extractSuccessSteps(messages []*biz.ChatMessage) []string {
	var steps []string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && strings.TrimSpace(messages[i].Content) != "" {
			steps = append(steps, messages[i].Content)
			if len(steps) >= 3 {
				break
			}
		}
	}
	for i, j := 0, len(steps)-1; i < j; i, j = i+1, j-1 {
		steps[i], steps[j] = steps[j], steps[i]
	}
	return steps
}

func agentIDFromCtx(ctx context.Context) string {
	if v := ctx.Value("agent_id"); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}