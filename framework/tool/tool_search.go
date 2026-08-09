package tool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
)

const (
	ToolSearchName   = "tool_search"
	ToolDescribeName = "tool_describe"
	ToolCallName     = "tool_call"
)

// ToolSearchConfig 控制 tool_search 渐进披露激活策略。
type ToolSearchConfig struct {
	Mode               string  // auto|on|off from env SATH_TOOL_SEARCH
	ThresholdPct       float64 // default 10
	HardTokenThreshold int     // default 20000 when context unknown
}

// ToolSearchConfigFromEnv 从环境变量读取 tool_search 配置。
func ToolSearchConfigFromEnv() ToolSearchConfig {
	cfg := ToolSearchConfig{
		Mode:               "auto",
		ThresholdPct:       10,
		HardTokenThreshold: 20000,
	}
	if v := strings.TrimSpace(os.Getenv("SATH_TOOL_SEARCH")); v != "" {
		cfg.Mode = strings.ToLower(v)
	}
	return cfg
}

// ShouldActivateToolSearch 判断是否应启用 tool_search 桥接三件套。
func ShouldActivateToolSearch(cat ToolCatalog, cfg ToolSearchConfig) bool {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode == "" {
		mode = "auto"
	}
	switch mode {
	case "off":
		return false
	case "on":
		return hasDeferredAvailable(cat)
	case "auto":
		thresholdPct := cfg.ThresholdPct
		if thresholdPct <= 0 {
			thresholdPct = 10
		}
		hardThreshold := cfg.HardTokenThreshold
		if hardThreshold <= 0 {
			hardThreshold = 20000
		}
		tokenThreshold := int(float64(hardThreshold) * thresholdPct / 100.0)
		return estimateDeferredSchemaTokens(cat) >= tokenThreshold
	default:
		return false
	}
}

func hasDeferredAvailable(cat ToolCatalog) bool {
	for _, e := range cat.Entries {
		if e.Deferred && e.Available {
			return true
		}
	}
	return false
}

func estimateDeferredSchemaTokens(cat ToolCatalog) int {
	var chars int
	for _, e := range cat.Entries {
		if !e.Deferred || !e.Available {
			continue
		}
		chars += len(buildDoc(e))
	}
	return chars / 4
}

// ToolSearchRegisterConfig 注册桥接工具所需依赖。
type ToolSearchRegisterConfig struct {
	Registry *Registry
	Catalog  ToolCatalog
}

type toolSearchResult struct {
	Name        string  `json:"name"`
	Toolset     string  `json:"toolset"`
	Description string  `json:"description"`
	Score       float64 `json:"score"`
}

type toolDescribeResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

// RegisterToolSearchTools 向注册表注册 tool_search、tool_describe、tool_call。
func RegisterToolSearchTools(reg *Registry, cfg ToolSearchRegisterConfig) error {
	if reg == nil {
		reg = cfg.Registry
	}
	if reg == nil {
		return errors.New("tool_search: registry is nil")
	}
	cat := cfg.Catalog

	if err := reg.Register(Tool{
		Name:        ToolSearchName,
		Description: "Search deferred tools by natural language query. Returns matching tool names and brief descriptions.",
		Toolset:     ToolsetCore,
		AlwaysLoad:  true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Search query to find deferred tools.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Max results (default 5, max 20).",
				},
			},
			"required": []string{"query"},
		},
		Execute: buildToolSearchExecute(cat),
	}); err != nil {
		return err
	}

	if err := reg.Register(Tool{
		Name:        ToolDescribeName,
		Description: "Return the full JSON schema for a deferred tool by name.",
		Toolset:     ToolsetCore,
		AlwaysLoad:  true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Tool name to describe.",
				},
			},
			"required": []string{"name"},
		},
		Execute: buildToolDescribeExecute(reg),
	}); err != nil {
		return err
	}

	return reg.Register(Tool{
		Name:        ToolCallName,
		Description: "Invoke a deferred tool by name with the given arguments.",
		Toolset:     ToolsetCore,
		AlwaysLoad:  true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Tool name to invoke.",
				},
				"arguments": map[string]any{
					"type":        "object",
					"description": "Arguments to pass to the tool.",
				},
			},
			"required": []string{"name", "arguments"},
		},
		Execute: buildToolCallExecute(reg),
	})
}

func buildToolSearchExecute(cat ToolCatalog) ExecuteFunc {
	return func(ctx context.Context, params map[string]any) (any, error) {
		_ = ctx
		query, _ := params["query"].(string)
		query = strings.TrimSpace(query)
		if query == "" {
			return nil, errors.New("tool_search: query is required")
		}

		limit := 5
		if v, ok := params["limit"]; ok {
			if n, ok := toInt(v); ok && n > 0 {
				limit = n
			}
		}
		if limit > 20 {
			limit = 20
		}

		deferred := filterDeferredAvailable(cat.Entries)
		ranked := rankCatalog(ToolCatalog{Entries: deferred}, query)
		if limit > len(ranked) {
			limit = len(ranked)
		}

		results := make([]toolSearchResult, 0, limit)
		for i := 0; i < limit; i++ {
			e := ranked[i].entry
			results = append(results, toolSearchResult{
				Name:        e.Name,
				Toolset:     e.Toolset,
				Description: e.Description,
				Score:       ranked[i].score,
			})
		}
		b, err := json.Marshal(results)
		if err != nil {
			return nil, err
		}
		return string(b), nil
	}
}

func buildToolDescribeExecute(reg *Registry) ExecuteFunc {
	return func(ctx context.Context, params map[string]any) (any, error) {
		_ = ctx
		name, _ := params["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, errors.New("tool_describe: name is required")
		}
		t, ok := reg.Get(name)
		if !ok {
			return nil, errors.New("tool_describe: tool not found: " + name)
		}
		out := toolDescribeResult{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		}
		b, err := json.Marshal(out)
		if err != nil {
			return nil, err
		}
		return string(b), nil
	}
}

func buildToolCallExecute(reg *Registry) ExecuteFunc {
	return func(ctx context.Context, params map[string]any) (any, error) {
		name, _ := params["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, errors.New("tool_call: name is required")
		}
		args, ok := params["arguments"].(map[string]any)
		if !ok {
			if params["arguments"] == nil {
				args = map[string]any{}
			} else {
				return nil, errors.New("tool_call: arguments must be an object")
			}
		}
		t, ok := reg.Get(name)
		if !ok {
			return nil, errors.New("tool_call: tool not found: " + name)
		}
		return t.Execute(ctx, args)
	}
}

func filterDeferredAvailable(entries []ToolCatalogEntry) []ToolCatalogEntry {
	out := make([]ToolCatalogEntry, 0)
	for _, e := range entries {
		if e.Deferred && e.Available {
			out = append(out, e)
		}
	}
	return out
}

func toInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case json.Number:
		n, err := x.Int64()
		if err != nil {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}
