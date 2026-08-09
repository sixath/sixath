package tool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	terminalDefaultTimeoutSec = 300
	terminalDefaultMaxOutput  = 50 * 1024
)

// TerminalConfig configures the local terminal tool.
type TerminalConfig struct {
	Enabled           bool
	DefaultTimeoutSec int
	MaxOutputBytes    int
	DeniedPatterns    []string // hard deny — never execute
	DangerPatterns    []string // require confirm_token (execute_write-style)
	ConfirmTTLSeconds int
	PendingStore      TerminalPendingStore
	TokenGen          TokenGenerator
	Runner            TerminalRunner
	Processes         *ProcessRegistry // required for background=true
}

// TerminalRunner executes a local shell command (injectable for tests).
type TerminalRunner interface {
	Run(ctx context.Context, name string, args []string, dir string) TerminalRunResult
}

// TerminalRunResult is the raw outcome of a local shell invocation.
type TerminalRunResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Err      error
	TimedOut bool
	Duration time.Duration
}

type osTerminalRunner struct{}

func (osTerminalRunner) Run(ctx context.Context, name string, args []string, dir string) TerminalRunResult {
	start := time.Now()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := TerminalRunResult{
		ExitCode: 0,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Err:      err,
		TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded),
		Duration: time.Since(start),
	}
	if err != nil {
		res.ExitCode = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
		}
	}
	return res
}

// TerminalLocalEnabled is the process-wide default for local terminal (override via env in Portal).
var TerminalLocalEnabled = false

// RegisterTerminalTool registers the Hermes-aligned local terminal tool (foreground P0).
func RegisterTerminalTool(reg *Registry, cfg *TerminalConfig) error {
	if reg == nil {
		return errors.New("terminal: registry is nil")
	}
	c := terminalConfigOrDefault(cfg)
	checkFn := func(ctx context.Context) error {
		if !c.Enabled {
			return errors.New("terminal local is disabled (set TERMINAL_LOCAL_ENABLED=true)")
		}
		return nil
	}
	return reg.Register(Tool{
		Name: "terminal",
		Description: "Execute shell commands on the local machine. " +
			"Foreground by default; set background=true to get a session_id and manage via process tool. " +
			"Use read_file/write_file/patch/search_files for file ops; web_search/web_extract for web. " +
			"Reserve terminal for builds, git, package managers, and scripts. " +
			"Dangerous commands require user confirm via confirm_token (same flow as execute_write).",
		Toolset: ToolsetTerminal,
		CheckFn: checkFn,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Shell command to execute.",
				},
				"workdir": map[string]any{
					"type":        "string",
					"description": "Working directory relative to workspace_root (optional).",
				},
				"timeout": map[string]any{
					"type":        "integer",
					"description": "Timeout in seconds (default 300). Applies to foreground and background.",
				},
				"background": map[string]any{
					"type":        "boolean",
					"description": "If true, start in background and return session_id for process tool.",
				},
				"notify_on_complete": map[string]any{
					"type":        "boolean",
					"description": "If true with background, wake the agent when the process exits (and mark notify_pending on poll).",
				},
				"pty": map[string]any{
					"type":        "boolean",
					"description": "Attach a real PTY (Unix pty / Windows ConPTY). Use with process write/submit for interactive programs that need a TTY.",
				},
				"confirm_token": map[string]any{
					"type":        "string",
					"description": "Confirmation token from a previous danger-command proposal.",
				},
			},
			"required": []string{"command"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			if token, _ := params["confirm_token"].(string); strings.TrimSpace(token) != "" {
				return confirmTerminal(ctx, c, strings.TrimSpace(token), params)
			}

			command, _ := params["command"].(string)
			command = strings.TrimSpace(command)
			if command == "" {
				return map[string]any{"error": "command is required"}, nil
			}
			if denied, pattern := commandDenied(command, c.DeniedPatterns); denied {
				return map[string]any{
					"error":   "command_denied",
					"pattern": pattern,
					"hint":    "command matched terminal denylist",
				}, nil
			}
			if danger, pattern := commandDenied(command, c.DangerPatterns); danger {
				return proposeTerminal(ctx, c, command, pattern, params)
			}
			return runTerminal(ctx, c, command, params)
		},
	})
}

func proposeTerminal(ctx context.Context, c *TerminalConfig, command, pattern string, params map[string]any) (any, error) {
	if c.PendingStore == nil || c.TokenGen == nil {
		return map[string]any{
			"error": "confirm_required_but_unconfigured",
			"hint":  "command matched danger patterns but pending store is not configured",
			"pattern": pattern,
		}, nil
	}
	sessionID, _ := ctx.Value(ContextKeySessionID).(string)
	if sessionID == "" {
		return map[string]any{"error": "session_id is required for danger command confirm"}, nil
	}
	token, err := c.TokenGen.NewToken()
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("generate token: %v", err)}, nil
	}
	workdir, _ := params["workdir"].(string)
	timeout := intFromParam(params["timeout"], 0)
	ttl := c.ConfirmTTLSeconds
	if ttl <= 0 {
		ttl = 300
	}
	pending := PendingTerminal{
		Token:            token,
		Command:          command,
		Workdir:          strings.TrimSpace(workdir),
		Timeout:          timeout,
		Background:       boolFromParam(params["background"]),
		NotifyOnComplete: boolFromParam(params["notify_on_complete"]),
		Pattern:          pattern,
		CreatedAt:        time.Now(),
	}
	if err := c.PendingStore.SavePending(ctx, sessionID, pending); err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{
		"status":     "pending",
		"token":      token,
		"command":    command,
		"pattern":    pattern,
		"expires_in": ttl,
		"hint":       "user must confirm; re-call terminal with confirm_token to execute",
	}, nil
}

func confirmTerminal(ctx context.Context, c *TerminalConfig, token string, _ map[string]any) (any, error) {
	if c.PendingStore == nil {
		return map[string]any{"error": "terminal: confirm store not configured"}, nil
	}
	sessionID, _ := ctx.Value(ContextKeySessionID).(string)
	if sessionID == "" {
		return map[string]any{"error": "session_id is required"}, nil
	}
	pending, err := c.PendingStore.GetPending(ctx, sessionID, token)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	if pending == nil {
		return ConfirmTokenError("not_found"), nil
	}
	ttl := c.ConfirmTTLSeconds
	if ttl <= 0 {
		ttl = 300
	}
	if time.Since(pending.CreatedAt) > time.Duration(ttl)*time.Second {
		_ = c.PendingStore.DeletePending(ctx, sessionID, token)
		return ConfirmTokenError("expired"), nil
	}
	// Trust pending payload only — ignore re-submitted command/workdir/timeout.
	runParams := map[string]any{}
	if pending.Workdir != "" {
		runParams["workdir"] = pending.Workdir
	}
	if pending.Timeout > 0 {
		runParams["timeout"] = pending.Timeout
	}
	if pending.Background {
		runParams["background"] = true
	}
	if pending.NotifyOnComplete {
		runParams["notify_on_complete"] = true
	}
	out, err := runTerminal(ctx, c, pending.Command, runParams)
	if err != nil {
		return out, err
	}
	if m, ok := out.(map[string]any); ok {
		if ev, has := m["error"]; has && ev != nil && fmt.Sprint(ev) != "" {
			return out, nil
		}
	}
	_ = c.PendingStore.DeletePending(ctx, sessionID, token)
	return out, nil
}

func runTerminal(ctx context.Context, c *TerminalConfig, command string, params map[string]any) (any, error) {
	if boolFromParam(params["background"]) {
		return spawnBackgroundTerminal(ctx, c, command, params)
	}
	timeoutSec := c.DefaultTimeoutSec
	if v := intFromParam(params["timeout"], 0); v > 0 {
		timeoutSec = v
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	workDir, err := terminalWorkdir(ctx, params)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	usePTY := boolFromParam(params["pty"])
	var run TerminalRunResult
	if usePTY {
		run = runCommandWithPTY(runCtx, command, workDir, c.MaxOutputBytes)
	} else {
		shell, shellArgs := localShellInvocation(command)
		run = c.Runner.Run(runCtx, shell, shellArgs, workDir)
	}
	stdout := stripANSI(truncateOutput(run.Stdout, c.MaxOutputBytes))
	stderr := stripANSI(truncateOutput(run.Stderr, c.MaxOutputBytes))
	out := map[string]any{
		"exit_code":   run.ExitCode,
		"stdout":      stdout,
		"stderr":      stderr,
		"duration_ms": run.Duration.Milliseconds(),
		"timed_out":   run.TimedOut,
	}
	if usePTY {
		out["pty"] = true
	}
	if workDir != "" {
		out["workdir"] = workDir
	}
	if run.ExitCode != 0 {
		out["status"] = "error"
		if run.Err != nil && stdout == "" && stderr == "" {
			out["error"] = run.Err.Error()
		}
	} else {
		out["status"] = "ok"
	}
	return out, nil
}

func spawnBackgroundTerminal(ctx context.Context, c *TerminalConfig, command string, params map[string]any) (any, error) {
	if c.Processes == nil {
		return map[string]any{
			"error": "background_unconfigured",
			"hint":  "terminal background requires ProcessRegistry wiring",
		}, nil
	}
	timeoutSec := c.DefaultTimeoutSec
	if v := intFromParam(params["timeout"], 0); v > 0 {
		timeoutSec = v
	}
	workDir, err := terminalWorkdir(ctx, params)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	chatSID, _ := ctx.Value(ContextKeySessionID).(string)
	usePTY := boolFromParam(params["pty"])
	id, err := c.Processes.Start(processStartRequest{
		ChatSessionID:    chatSID,
		Command:          command,
		Workdir:          workDir,
		TimeoutSec:       timeoutSec,
		NotifyOnComplete: boolFromParam(params["notify_on_complete"]),
		MaxOutputBytes:   c.MaxOutputBytes,
		PTY:              usePTY,
	})
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	out := map[string]any{
		"status":     "running",
		"session_id": id,
		"command":    command,
		"hint":       "use process(action=poll|wait|kill|write|submit, session_id=...) to manage",
	}
	if usePTY {
		out["pty"] = true
	}
	if workDir != "" {
		out["workdir"] = workDir
	}
	if boolFromParam(params["notify_on_complete"]) {
		out["notify_on_complete"] = true
	}
	return out, nil
}

func boolFromParam(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		s := strings.ToLower(strings.TrimSpace(b))
		return s == "1" || s == "true" || s == "yes"
	default:
		return false
	}
}

func terminalConfigOrDefault(cfg *TerminalConfig) *TerminalConfig {
	c := &TerminalConfig{
		Enabled:           TerminalLocalEnabled,
		DefaultTimeoutSec: terminalDefaultTimeoutSec,
		MaxOutputBytes:    terminalDefaultMaxOutput,
		DeniedPatterns:    defaultTerminalDeniedPatterns(),
		DangerPatterns:    defaultTerminalDangerPatterns(),
		ConfirmTTLSeconds: 300,
		Runner:            osTerminalRunner{},
	}
	if cfg != nil {
		c.Enabled = cfg.Enabled
		if cfg.DefaultTimeoutSec > 0 {
			c.DefaultTimeoutSec = cfg.DefaultTimeoutSec
		}
		if cfg.MaxOutputBytes > 0 {
			c.MaxOutputBytes = cfg.MaxOutputBytes
		}
		if len(cfg.DeniedPatterns) > 0 {
			c.DeniedPatterns = cfg.DeniedPatterns
		}
		if len(cfg.DangerPatterns) > 0 {
			c.DangerPatterns = cfg.DangerPatterns
		}
		if cfg.ConfirmTTLSeconds > 0 {
			c.ConfirmTTLSeconds = cfg.ConfirmTTLSeconds
		}
		if cfg.PendingStore != nil {
			c.PendingStore = cfg.PendingStore
		}
		if cfg.TokenGen != nil {
			c.TokenGen = cfg.TokenGen
		}
		if cfg.Runner != nil {
			c.Runner = cfg.Runner
		}
		if cfg.Processes != nil {
			c.Processes = cfg.Processes
		}
	}
	return c
}

func terminalWorkdir(ctx context.Context, params map[string]any) (string, error) {
	ws, _ := ctx.Value(ContextKeyWorkspaceRoot).(string)
	workdir, _ := params["workdir"].(string)
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		if ws != "" {
			return filepath.Clean(ws), nil
		}
		return "", nil
	}
	if ws == "" {
		return "", fmt.Errorf("workspace_root not set for workdir %q", workdir)
	}
	full, err := ResolveWorkspacePath(ws, workdir)
	if err != nil {
		return "", err
	}
	return full, nil
}

func localShellInvocation(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", command}
	}
	return "sh", []string{"-c", command}
}

// defaultTerminalDeniedPatterns are catastrophic hard-denies (never confirmable).
// Softer risks (e.g. rm -rf ./build) live in defaultTerminalDangerPatterns.
func defaultTerminalDeniedPatterns() []string {
	return []string{
		`(?i):\(\)\s*\{`,
		`(?i):\(\)\s*\{\s*:\|\:\s*&\s*\}`,
		`(?i)\brm\s+-rf\s+/`,
		`(?i)\brm\s+-rf\s+/\*`,
		`(?i)\bmkfs\b`,
		`(?i)\bshutdown\b`,
		`(?i)\breboot\b`,
		`(?i)\bpoweroff\b`,
		`(?i)\bhalt\b`,
		`(?i)>\s*/etc/`,
		`(?i)\bformat\s+[a-z]:`,
		`(?i)\bdel\s+/[fq\s].*[a-z]:\\`,
		`(?i)\brd\s+/s\s+/q\s+[a-z]:\\`,
	}
}

// defaultTerminalDangerPatterns require confirm_token before execution.
func defaultTerminalDangerPatterns() []string {
	return []string{
		`(?i)\brm\s+-rf\b`,
		`(?i)\bdd\s+if=`,
		`(?i)\bchmod\s+-R\s+777\b`,
		`(?i)\bsudo\b`,
		`(?i)\bgit\s+push\b.*--force`,
		`(?i)\bcurl\b.*\|\s*(ba)?sh\b`,
		`(?i)\bwget\b.*\|\s*(ba)?sh\b`,
		`(?i)\bDROP\s+(DATABASE|TABLE)\b`,
		`(?i)\bRemove-Item\b.*-Recurse`,
		`(?i)\bdel\s+/[sq]`,
		`(?i)\bkubectl\s+delete\b`,
		`(?i)\bdocker\s+system\s+prune\b`,
	}
}

var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func stripANSI(s string) string {
	return ansiEscapeRe.ReplaceAllString(s, "")
}

func truncateOutput(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	head := maxBytes * 2 / 3
	tail := maxBytes - head
	if tail < 0 {
		tail = 0
	}
	return s[:head] + "\n...[output truncated]...\n" + s[len(s)-tail:]
}

// TerminalEnabledFromEnv reports whether TERMINAL_LOCAL_ENABLED is truthy.
func TerminalEnabledFromEnv() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("TERMINAL_LOCAL_ENABLED")))
	return v == "1" || v == "true" || v == "yes"
}
