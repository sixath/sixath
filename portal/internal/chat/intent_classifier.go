package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sixath/framework/model"
)

type ModelFamilyClassifier struct {
	Model   model.Model
	Timeout time.Duration // default 3s
}

func (c ModelFamilyClassifier) Classify(ctx context.Context, userText string, bound, candidates []string) ([]string, string, error) {
	if c.Model == nil {
		return nil, "", fmt.Errorf("nil model")
	}
	to := c.Timeout
	if to <= 0 {
		to = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()
	prompt := buildFamilyClassifyPrompt(userText, bound, candidates)
	gen, err := c.Model.Generate(ctx, prompt, model.WithTemperature(0), model.WithMaxTokens(256))
	if err != nil {
		return nil, "", err
	}
	return parseClassifierJSON(gen.Text)
}

func buildFamilyClassifyPrompt(user string, bound, candidates []string) string {
	return fmt.Sprintf(`You classify which tool families are needed for this user message.
Reply with ONLY JSON: {"families":["..."],"confidence":"high"|"low"}
Rules:
- families must be a subset of bound: %s
- prefer candidates when relevant: %s
- include multiple families only for explicit multi-intent
- confidence=high only when sure

User message:
%s`, strings.Join(bound, ", "), strings.Join(candidates, ", "), user)
}

func parseClassifierJSON(text string) ([]string, string, error) {
	text = strings.TrimSpace(text)
	if i := strings.Index(text, "{"); i >= 0 {
		if j := strings.LastIndex(text, "}"); j > i {
			text = text[i : j+1]
		}
	}
	var raw struct {
		Families   []string `json:"families"`
		Confidence string   `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, "", err
	}
	conf := strings.ToLower(strings.TrimSpace(raw.Confidence))
	if conf != "high" && conf != "low" {
		conf = "low"
	}
	return raw.Families, conf, nil
}
