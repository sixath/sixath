package harness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// HarnessHooksFileRel is the workspace-relative path for declarative hooks (G4.1).
const HarnessHooksFileRel = "harness/hooks.yaml"

// HarnessHooksFile is the on-disk schema for workspace harness hooks.
type HarnessHooksFile struct {
	Version int               `yaml:"version"`
	Rules   []HarnessHookRule `yaml:"rules"`
}

// HarnessHookRule is one declarative Before rule (MVP: action=block only).
type HarnessHookRule struct {
	ID     string            `yaml:"id"`
	Tools  []string          `yaml:"tools"`
	Match  *HarnessHookMatch `yaml:"match"`
	Action string            `yaml:"action"`
	Reason string            `yaml:"reason"`
}

// HarnessHookMatch optionally constrains a rule to a param regex.
type HarnessHookMatch struct {
	Param string `yaml:"param"`
	Regex string `yaml:"regex"`
}

// LoadWorkspaceHarnessHooks loads harness/hooks.yaml from workspace (if present).
// Missing file → nil, nil. Invalid YAML / bad regex → error (caller may log and skip).
func LoadWorkspaceHarnessHooks(workspace string) ([]ToolHook, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil, nil
	}
	path := filepath.Join(workspace, filepath.FromSlash(HarnessHooksFileRel))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return ParseHarnessHooksYAML(data)
}

// ParseHarnessHooksYAML parses hooks YAML bytes into ToolHook implementations.
func ParseHarnessHooksYAML(data []byte) ([]ToolHook, error) {
	var file HarnessHooksFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("harness hooks: parse: %w", err)
	}
	if file.Version != 0 && file.Version != 1 {
		return nil, fmt.Errorf("harness hooks: unsupported version %d", file.Version)
	}
	var hooks []ToolHook
	for i, rule := range file.Rules {
		h, err := newDeclarativeBlockHook(rule)
		if err != nil {
			id := rule.ID
			if id == "" {
				id = fmt.Sprintf("rules[%d]", i)
			}
			return nil, fmt.Errorf("harness hooks: rule %s: %w", id, err)
		}
		if h != nil {
			hooks = append(hooks, h)
		}
	}
	return hooks, nil
}

type declarativeBlockHook struct {
	id     string
	tools  map[string]struct{} // empty map = all tools
	param  string
	re     *regexp.Regexp
	reason string
	always bool // no match clause → block whenever tool matches
}

func newDeclarativeBlockHook(rule HarnessHookRule) (*declarativeBlockHook, error) {
	action := strings.ToLower(strings.TrimSpace(rule.Action))
	if action == "" {
		action = "block"
	}
	if action != "block" {
		return nil, fmt.Errorf("unsupported action %q (MVP only block)", rule.Action)
	}
	h := &declarativeBlockHook{
		id:     strings.TrimSpace(rule.ID),
		reason: strings.TrimSpace(rule.Reason),
		tools:  make(map[string]struct{}),
	}
	for _, t := range rule.Tools {
		t = strings.TrimSpace(t)
		if t != "" {
			h.tools[t] = struct{}{}
		}
	}
	if rule.Match == nil {
		h.always = true
	} else {
		h.param = strings.TrimSpace(rule.Match.Param)
		pat := strings.TrimSpace(rule.Match.Regex)
		if pat == "" {
			return nil, fmt.Errorf("match.regex is required when match is set")
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("match.regex: %w", err)
		}
		h.re = re
	}
	if h.reason == "" {
		if h.id != "" {
			h.reason = "blocked by harness hook " + h.id
		} else {
			h.reason = "blocked by harness hook"
		}
	}
	return h, nil
}

func (h *declarativeBlockHook) Before(_ context.Context, name string, params map[string]any) (map[string]any, error) {
	if h == nil {
		return params, nil
	}
	if len(h.tools) > 0 {
		if _, ok := h.tools[name]; !ok {
			return params, nil
		}
	}
	if h.always {
		return params, fmt.Errorf("%s", h.reason)
	}
	val := ""
	if h.param != "" {
		if params != nil {
			if v, ok := params[h.param]; ok && v != nil {
				val = fmt.Sprint(v)
			}
		}
	} else {
		// No param: match regex against a stable serialization of all stringish args.
		val = flattenParams(params)
	}
	if h.re != nil && h.re.MatchString(val) {
		return params, fmt.Errorf("%s", h.reason)
	}
	return params, nil
}

func (h *declarativeBlockHook) After(_ context.Context, _ string, result any, err error) (any, error) {
	return result, err
}

func flattenParams(params map[string]any) string {
	if params == nil {
		return ""
	}
	parts := make([]string, 0, len(params))
	for k, v := range params {
		if v == nil {
			continue
		}
		parts = append(parts, k+"="+fmt.Sprint(v))
	}
	return strings.Join(parts, " ")
}
