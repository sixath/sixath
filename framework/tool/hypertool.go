package tool

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const HyperToolName = "hypertool"

//go:embed hypertool_runner.py
var hyperToolRunnerSource []byte

// HyperToolOptions configures the hypertool meta-tool (v0 minimal prototype).
type HyperToolOptions struct {
	// Enabled gates registration; default false.
	Enabled bool
	// TimeoutSeconds caps block execution; <=0 defaults to 30, max 300.
	TimeoutSeconds int
	// MaxInternalCalls limits call_tool invocations per block; <=0 defaults to 20.
	MaxInternalCalls int
	// PythonCommand interpreter binary; empty defaults to "python".
	PythonCommand string
	// BlockedTools cannot be invoked from inside a block (defaults include hypertool itself).
	BlockedTools []string
}

func defaultHyperToolOptions(opts *HyperToolOptions) HyperToolOptions {
	out := HyperToolOptions{
		Enabled:          false,
		TimeoutSeconds:   30,
		MaxInternalCalls: 20,
		PythonCommand:    "python",
		BlockedTools: []string{
			HyperToolName,
			"execute_skill_script",
			"ssh_exec",
			"terminal",
			"scp",
		},
	}
	if opts == nil {
		return out
	}
	if opts.Enabled {
		out.Enabled = true
	}
	if opts.TimeoutSeconds > 0 {
		out.TimeoutSeconds = opts.TimeoutSeconds
		if out.TimeoutSeconds > 300 {
			out.TimeoutSeconds = 300
		}
	}
	if opts.MaxInternalCalls > 0 {
		out.MaxInternalCalls = opts.MaxInternalCalls
	}
	if strings.TrimSpace(opts.PythonCommand) != "" {
		out.PythonCommand = strings.TrimSpace(opts.PythonCommand)
	}
	if len(opts.BlockedTools) > 0 {
		out.BlockedTools = append([]string(nil), opts.BlockedTools...)
	}
	return out
}

// RegisterHyperTool registers the HyperTool meta-tool when Enabled is true.
// The model submits a Python code block; primitive tools are invoked via call_tool(name, arguments).
func RegisterHyperTool(reg *Registry, opts *HyperToolOptions) error {
	if reg == nil {
		return errors.New("hypertool: registry is nil")
	}
	cfg := defaultHyperToolOptions(opts)
	if !cfg.Enabled {
		return nil
	}

	blocked := make(map[string]struct{}, len(cfg.BlockedTools))
	for _, name := range cfg.BlockedTools {
		name = strings.TrimSpace(name)
		if name != "" {
			blocked[name] = struct{}{}
		}
	}

	return reg.Register(Tool{
		Name:        HyperToolName,
		Description: hyperToolDescription(),
		Toolset:     ToolsetCore,
		AlwaysLoad:  true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"code": map[string]any{
					"type":        "string",
					"description": "Python code block. Use call_tool(name, arguments) to invoke registered tools. Assign the final value to variable result (must be JSON-serializable).",
				},
			},
			"required": []string{"code"},
		},
		Execute: buildHyperToolExecute(reg, cfg, blocked),
	})
}

func hyperToolDescription() string {
	return strings.TrimSpace(`
Execute a locally deterministic tool workflow inside one code block.
Inside the block, call registered tools with call_tool(name, arguments) where arguments is an object.
Store intermediate values in local variables, filter or aggregate tool outputs, then assign the final payload to result.
Use hypertool when the next steps are predictable (chain, transform, aggregate); use direct tool calls when the plan depends on semantic interpretation of unknown outputs.
Do not call hypertool recursively. Do not use print for the final answer — set result.
`)
}

func buildHyperToolExecute(reg *Registry, cfg HyperToolOptions, blocked map[string]struct{}) ExecuteFunc {
	return func(ctx context.Context, params map[string]any) (any, error) {
		code, _ := params["code"].(string)
		code = strings.TrimSpace(code)
		if code == "" {
			return nil, errors.New("hypertool: code is required")
		}

		runnerPath, cleanup, err := materializeHyperToolRunner()
		if err != nil {
			return nil, err
		}
		defer cleanup()

		timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
		runCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		cmd := exec.CommandContext(runCtx, cfg.PythonCommand, runnerPath)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, fmt.Errorf("hypertool: stdin pipe: %w", err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, fmt.Errorf("hypertool: stdout pipe: %w", err)
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return nil, fmt.Errorf("hypertool: stderr pipe: %w", err)
		}

		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("hypertool: start python (%s): %w", cfg.PythonCommand, err)
		}

		var (
			stdinMu  sync.Mutex
			calls    int
			result   any
			runErr   error
			wg       sync.WaitGroup
			reader   = bufio.NewReader(stdout)
			errBytes []byte
		)

		wg.Add(1)
		go func() {
			defer wg.Done()
			errBytes, _ = io.ReadAll(stderr)
		}()

		runPayload, err := json.Marshal(map[string]any{"type": "run", "code": code})
		if err != nil {
			_ = cmd.Process.Kill()
			return nil, err
		}
		if _, err := stdin.Write(append(runPayload, '\n')); err != nil {
			_ = cmd.Process.Kill()
			return nil, fmt.Errorf("hypertool: write run payload: %w", err)
		}

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if runCtx.Err() != nil {
					_ = cmd.Process.Kill()
					return nil, fmt.Errorf("hypertool: timeout after %ds", cfg.TimeoutSeconds)
				}
				runErr = fmt.Errorf("hypertool: read runner output: %w", err)
				break
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			var msg hyperToolRunnerMsg
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				runErr = fmt.Errorf("hypertool: invalid runner json: %w", err)
				break
			}

			switch msg.Type {
			case "call":
				calls++
				if calls > cfg.MaxInternalCalls {
					runErr = fmt.Errorf("hypertool: exceeded max internal calls (%d)", cfg.MaxInternalCalls)
					_ = writeHyperToolHostMsg(stdin, &stdinMu, hyperToolHostMsg{Type: "error", Message: runErr.Error()})
					break
				}
				toolResult, callErr := hyperToolInvoke(reg, blocked, ctx, msg.Name, msg.Arguments)
				if callErr != nil {
					_ = writeHyperToolHostMsg(stdin, &stdinMu, hyperToolHostMsg{Type: "error", Message: callErr.Error()})
				} else {
					_ = writeHyperToolHostMsg(stdin, &stdinMu, hyperToolHostMsg{Type: "result", Result: toolResult})
				}
			case "done":
				result = msg.Result
			case "error":
				runErr = errors.New(strings.TrimSpace(msg.Message))
				if runErr.Error() == "" {
					runErr = errors.New("hypertool: block execution failed")
				}
			default:
				runErr = fmt.Errorf("hypertool: unknown runner message type %q", msg.Type)
			}

			if msg.Type == "done" || msg.Type == "error" || runErr != nil {
				break
			}
		}

		_ = stdin.Close()
		_ = cmd.Wait()
		wg.Wait()

		if runErr != nil {
			if len(errBytes) > 0 {
				return nil, fmt.Errorf("%w; stderr: %s", runErr, strings.TrimSpace(string(errBytes)))
			}
			return nil, runErr
		}
		if result == nil {
			return nil, errors.New("hypertool: block finished without result")
		}
		return hyperToolFormatResult(result)
	}
}

type hyperToolRunnerMsg struct {
	Type      string         `json:"type"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
	Result    any            `json:"result"`
	Message   string         `json:"message"`
}

type hyperToolHostMsg struct {
	Type    string `json:"type"`
	Result  any    `json:"result,omitempty"`
	Message string `json:"message,omitempty"`
}

func writeHyperToolHostMsg(w io.Writer, mu *sync.Mutex, msg hyperToolHostMsg) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	_, err = w.Write(append(b, '\n'))
	return err
}

func hyperToolInvoke(reg *Registry, blocked map[string]struct{}, ctx context.Context, name string, args map[string]any) (any, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("call_tool: name is required")
	}
	if _, ok := blocked[name]; ok {
		return nil, fmt.Errorf("call_tool: tool %q is blocked inside hypertool", name)
	}
	if args == nil {
		args = map[string]any{}
	}

	tl, ok := reg.Get(name)
	if !ok {
		return nil, fmt.Errorf("call_tool: tool not found: %s", name)
	}
	return tl.Execute(ctx, args)
}

func hyperToolFormatResult(v any) (any, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("hypertool: result marshal: %w", err)
		}
		return string(b), nil
	}
}

func materializeHyperToolRunner() (path string, cleanup func(), err error) {
	f, err := os.CreateTemp("", "sixath-hypertool-runner-*.py")
	if err != nil {
		return "", func() {}, err
	}
	path = f.Name()
	if _, err := f.Write(hyperToolRunnerSource); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", func() {}, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", func() {}, err
	}
	return path, func() { _ = os.Remove(path) }, nil
}

// HyperToolPromptSnippet returns system-prompt guidance when HyperTool is enabled.
func HyperToolPromptSnippet() string {
	return strings.TrimSpace(`
【HyperTool 执行策略】
当子任务可由确定的工具调用链完成（检索→解析→过滤→聚合）时，优先调用 hypertool 工具，在单个 Python 代码块内用 call_tool(name, arguments) 组合多个工具，并将最终结果赋给 result。
当下一步依赖对未知结构输出的语义理解、或需要根据中间结果大幅调整计划时，仍应逐步直接调用工具。
hypertool 块内的变量不能跨块复用；不要把推理文字写进代码块（含 # 注释）。
`)
}
