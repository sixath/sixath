package chat

import (
	"github.com/sixath/framework/tool"
)

// RegisterTodoTools registers the session todo tool (opt-in via HermesP0ToolFlags.TodoEnabled).
func RegisterTodoTools(reg *tool.Registry) error {
	if reg == nil {
		return nil
	}
	return tool.RegisterTodoTool(reg, tool.DefaultTodoStore)
}
