package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	runResultScriptMaxCodeBytes = 65536
	runResultScriptName         = "run_result_script"
)

var (
	lookPathFn    = exec.LookPath
	scriptTimeout = 15 * time.Second
)

func pythonInterpreter() (string, error) {
	if p, err := lookPathFn("python"); err == nil {
		return p, nil
	}
	return lookPathFn("python3")
}

func RegisterRunResultScriptTool(reg *Registry) error {
	if reg == nil {
		return errors.New("run_result_script: registry is nil")
	}
	return reg.Register(Tool{
		Name: runResultScriptName,
		Description: "Last-resort Python 3 over a spilled file under tmp/results/. " +
			"Prefer query tools, then result_stats (count/group_by/unique), then this tool. " +
			"Open the data file via sys.argv[1]; do not read_file the whole jsonl.",
		Toolset: ToolsetFile,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Workspace-relative data file under tmp/results/.",
				},
				"code": map[string]any{
					"type":        "string",
					"description": "Inline Python (mutually exclusive with script_path).",
				},
				"script_path": map[string]any{
					"type":        "string",
					"description": "Existing .py under tmp/results/ (mutually exclusive with code).",
				},
			},
			"required": []string{"path"},
		},
		Execute: executeRunResultScript,
	})
}

func executeRunResultScript(ctx context.Context, params map[string]any) (any, error) {
	scriptAbs, dataAbs, dataRel, err := prepareRunResultScript(ctx, params)
	if err != nil {
		return nil, err
	}
	_ = scriptAbs
	_ = dataAbs
	_ = dataRel
	return nil, fmt.Errorf("run_result_script: not implemented")
}

func prepareRunResultScript(ctx context.Context, params map[string]any) (scriptAbs, dataAbs, dataRel string, err error) {
	ws, err := workspaceRootFromCtx(ctx)
	if err != nil {
		return "", "", "", fmt.Errorf("run_result_script: workspace_root_missing")
	}
	rel, _ := params["path"].(string)
	if strings.TrimSpace(rel) == "" {
		return "", "", "", fmt.Errorf("run_result_script: path is required")
	}
	dataAbs, dataRel, err = resolveResultsPath(ws, rel)
	if err != nil {
		return "", "", "", fmt.Errorf("run_result_script: %w", err)
	}
	st, err := os.Stat(dataAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", "", fmt.Errorf("run_result_script: file not found")
		}
		return "", "", "", fmt.Errorf("run_result_script: %w", err)
	}
	if st.IsDir() {
		return "", "", "", fmt.Errorf("run_result_script: path is not a file")
	}

	code, _ := params["code"].(string)
	scriptPath, _ := params["script_path"].(string)
	hasCode := strings.TrimSpace(code) != ""
	hasScript := strings.TrimSpace(scriptPath) != ""
	if hasCode == hasScript {
		return "", "", "", fmt.Errorf("run_result_script: provide exactly one of code or script_path")
	}
	if hasCode && len(code) > runResultScriptMaxCodeBytes {
		return "", "", "", fmt.Errorf("run_result_script: code exceeds 64KiB")
	}

	if hasScript {
		scriptAbs, _, err = resolveResultsPath(ws, scriptPath)
		if err != nil {
			return "", "", "", fmt.Errorf("run_result_script: %w", err)
		}
		if strings.ToLower(filepath.Ext(scriptAbs)) != ".py" {
			return "", "", "", fmt.Errorf("run_result_script: script_path must be .py")
		}
		st, err := os.Stat(scriptAbs)
		if err != nil {
			if os.IsNotExist(err) {
				return "", "", "", fmt.Errorf("run_result_script: script not found")
			}
			return "", "", "", fmt.Errorf("run_result_script: %w", err)
		}
		if st.IsDir() {
			return "", "", "", fmt.Errorf("run_result_script: script_path is not a file")
		}
		return scriptAbs, dataAbs, dataRel, nil
	}

	sess, _ := ctx.Value(ContextKeySessionID).(string)
	pyRel, scriptAbs, err := newSpillNamedFile(ws, sess, runResultScriptName, ".py")
	if err != nil {
		return "", "", "", fmt.Errorf("run_result_script: %w", err)
	}
	_ = pyRel
	if filepath.Clean(scriptAbs) == filepath.Clean(dataAbs) {
		pyRel, scriptAbs, err = newSpillNamedFile(ws, sess, runResultScriptName, ".py")
		if err != nil {
			return "", "", "", fmt.Errorf("run_result_script: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(scriptAbs), 0o755); err != nil {
		return "", "", "", fmt.Errorf("run_result_script: %w", err)
	}
	if err := os.WriteFile(scriptAbs, []byte(code), 0o644); err != nil {
		return "", "", "", fmt.Errorf("run_result_script: write script: %w", err)
	}
	expireSessionResults(filepath.Dir(scriptAbs), time.Now())
	return scriptAbs, dataAbs, dataRel, nil
}

func splitScriptOutput(raw string) []string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	parts := strings.Split(raw, "\n")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func rowsFromScriptLines(lines []string) []map[string]any {
	if len(lines) == 0 {
		return nil
	}
	objs := make([]map[string]any, 0, len(lines))
	allObj := true
	for _, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil || m == nil {
			allObj = false
			break
		}
		objs = append(objs, m)
	}
	if allObj {
		return objs
	}
	out := make([]map[string]any, len(lines))
	for i, line := range lines {
		out[i] = map[string]any{"line": line}
	}
	return out
}

func truncateUTF8Bytes(s string, n int) string {
	if n < 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
