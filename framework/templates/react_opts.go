package templates

import (
	"strings"

	agent "github.com/sixath/framework/harness"
)

// appendWorkspaceOpt 非空 workspace 时给 ReAct 加上可写根（PromptBuilder / 文件器官）。
func appendWorkspaceOpt(opts []agent.ReActOption, workspace string) []agent.ReActOption {
	if ws := strings.TrimSpace(workspace); ws != "" {
		return append(opts, agent.WithReActWorkspace(ws))
	}
	return opts
}
