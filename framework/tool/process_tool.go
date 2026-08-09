package tool

import (
	"context"
	"errors"
	"strings"
)

// RegisterProcessTool registers the Hermes-aligned process manager tool.
func RegisterProcessTool(reg *Registry, processes *ProcessRegistry, enabled bool) error {
	if reg == nil {
		return errors.New("process: registry is nil")
	}
	if processes == nil {
		return errors.New("process: ProcessRegistry is nil")
	}
	checkFn := func(ctx context.Context) error {
		if !enabled {
			return errors.New("terminal/process local is disabled (set TERMINAL_LOCAL_ENABLED=true)")
		}
		return nil
	}
	return reg.Register(Tool{
		Name: "process",
		Description: "Manage background processes started with terminal(background=true). " +
			"Actions: list, poll (status + new output), log (paginated), wait, kill, " +
			"write (raw stdin), submit (stdin + Enter), close (stdin EOF).",
		Toolset: ToolsetTerminal,
		CheckFn: checkFn,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type": "string",
					"enum": []string{"list", "poll", "log", "wait", "kill", "write", "submit", "close"},
					"description": "Process management action.",
				},
				"session_id": map[string]any{
					"type":        "string",
					"description": "Background process id returned by terminal(background=true). Required except for list.",
				},
				"timeout": map[string]any{
					"type":        "integer",
					"description": "Wait timeout in seconds (wait action).",
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "Byte offset for log pagination.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Max bytes to return for log (default 4000).",
				},
				"stdout_offset": map[string]any{
					"type":        "integer",
					"description": "Poll: previous stdout_offset cursor.",
				},
				"stderr_offset": map[string]any{
					"type":        "integer",
					"description": "Poll: previous stderr_offset cursor.",
				},
				"data": map[string]any{
					"type":        "string",
					"description": "Stdin payload for write (raw) or submit (with newline).",
				},
			},
			"required": []string{"action"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			action, _ := params["action"].(string)
			action = strings.TrimSpace(strings.ToLower(action))
			procID, _ := params["session_id"].(string)
			procID = strings.TrimSpace(procID)

			switch action {
			case "list":
				chatSID, _ := ctx.Value(ContextKeySessionID).(string)
				return map[string]any{
					"status":    "ok",
					"processes": processes.List(chatSID),
				}, nil
			case "poll":
				if procID == "" {
					return map[string]any{"error": "session_id is required"}, nil
				}
				m, err := processes.Poll(procID, intFromParam(params["stdout_offset"], 0), intFromParam(params["stderr_offset"], 0))
				if err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				return m, nil
			case "log":
				if procID == "" {
					return map[string]any{"error": "session_id is required"}, nil
				}
				m, err := processes.Log(procID, intFromParam(params["offset"], 0), intFromParam(params["limit"], 4000))
				if err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				return m, nil
			case "wait":
				if procID == "" {
					return map[string]any{"error": "session_id is required"}, nil
				}
				m, err := processes.Wait(procID, intFromParam(params["timeout"], 0))
				if err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				return m, nil
			case "kill":
				if procID == "" {
					return map[string]any{"error": "session_id is required"}, nil
				}
				m, err := processes.Kill(procID)
				if err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				return m, nil
			case "write":
				if procID == "" {
					return map[string]any{"error": "session_id is required"}, nil
				}
				data, _ := params["data"].(string)
				m, err := processes.Write(procID, data)
				if err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				return m, nil
			case "submit":
				if procID == "" {
					return map[string]any{"error": "session_id is required"}, nil
				}
				data, _ := params["data"].(string)
				m, err := processes.Submit(procID, data)
				if err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				return m, nil
			case "close":
				if procID == "" {
					return map[string]any{"error": "session_id is required"}, nil
				}
				m, err := processes.CloseStdin(procID)
				if err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				return m, nil
			default:
				return map[string]any{"error": "unknown action: " + action}, nil
			}
		},
	})
}
