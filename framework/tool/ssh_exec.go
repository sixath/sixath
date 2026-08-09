package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	SSHExecErrorHostKeyFailed   = "host_key_failed"
	SSHExecErrorAuthFailed      = "auth_failed"
	SSHExecErrorTimeout         = "timeout"
	SSHExecErrorNetworkFailed   = "network_failed"
	SSHExecErrorCommandFailed   = "command_failed"
	SSHExecErrorBlockedByPolicy = "blocked_by_policy"
	SSHExecErrorInternal        = "internal_error"
)

type SSHExecConfig struct {
	Runner  SSHExecRunner
	SSHPath string
	// MaxOutputBytes caps stdout/stderr returned to the model (0 = default 50 KiB).
	MaxOutputBytes int
	DefaultUser    string
	// DefaultPassword 页面配置的 SSH 密码；非空时走 golang.org/x/crypto/ssh（非交互），系统 ssh 无法用 BatchMode 输密码。
	DefaultPassword       string
	Port                  int
	KnownHostsPath        string
	DefaultTimeoutSec     int
	StrictHostKeyChecking string
	AllowedHosts          []string
	// DefaultHost 非空且调用未传 host 时使用（与「仅一条非通配 allowed_hosts」二选一或叠加）。
	DefaultHost            string
	AllowedUsers           []string
	AllowedCommandPrefixes []string
	DeniedCommandPatterns  []string
	// Native 非空时使用 golang.org/x/crypto/ssh 直连（私钥等）；可与 DefaultPassword 同时使用。
	Native *SSHExecNativeConfig
}

// usesCryptoSSH 为 true 时使用 x/crypto/ssh，不调用外部 ssh 可执行文件。
func usesCryptoSSH(cfg *SSHExecConfig) bool {
	if cfg == nil {
		return false
	}
	if strings.TrimSpace(cfg.DefaultPassword) != "" {
		return true
	}
	if cfg.Native != nil && len(cfg.Native.PrivateKeyPaths) > 0 {
		return true
	}
	return false
}

type SSHExecRunner interface {
	Run(ctx context.Context, name string, args []string, workingDir string) SSHExecRunResult
}

type SSHExecRunResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Err      error
	TimedOut bool
	Duration time.Duration
}

type SSHExecResult struct {
	OK            bool   `json:"ok"`
	Host          string `json:"host"`
	User          string `json:"user"`
	Command       string `json:"command"`
	ExitCode      int    `json:"exit_code"`
	Stdout        string `json:"stdout"`
	Stderr        string `json:"stderr"`
	ErrorCategory string `json:"error_category,omitempty"`
	DurationMS    int64  `json:"duration_ms"`
}

type osSSHExecRunner struct{}

func (osSSHExecRunner) Run(ctx context.Context, name string, args []string, workingDir string) SSHExecRunResult {
	start := time.Now()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = workingDir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := SSHExecRunResult{
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

func RegisterSSHExecTool(reg *Registry, cfg *SSHExecConfig, opts ...*RegisterToolOptions) error {
	if reg == nil {
		return errors.New("ssh_exec: registry is nil")
	}
	cfg = normalizeSSHExecConfig(cfg)
	if err := validateSSHExecCryptoConfig(cfg); err != nil {
		return err
	}
	desc := "Execute a read-only SSH command on an allowed host and return structured stdout, stderr, exit code, duration and error category."
	if len(opts) > 0 && opts[0] != nil && opts[0].Description != "" {
		desc = opts[0].Description
	}
	return reg.Register(Tool{
		Name:        "ssh_exec",
		Description: desc,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"host": map[string]any{
					"type":        "string",
					"description": "SSH target host or IP; must match allowed_hosts when configured. If the tool is configured with default_host or exactly one concrete allowed host, you may omit this and the server will apply that default.",
				},
				"user": map[string]any{
					"type":        "string",
					"description": "SSH login user. If omitted, default_user is used.",
				},
				"command": map[string]any{
					"type":        "string",
					"description": "Remote command to execute. Must match allowed_command_prefixes and denied_command_patterns policies when configured.",
				},
				"timeout_sec": map[string]any{
					"type":        "integer",
					"description": "Overall command timeout in seconds. Defaults to tool policy.",
				},
				"strict_host_key_checking": map[string]any{
					"type":        "string",
					"description": "OpenSSH StrictHostKeyChecking value: yes, accept-new, or no. Defaults to tool policy.",
				},
				"working_dir": map[string]any{
					"type":        "string",
					"description": "Optional remote working directory. The tool will cd into it before running command.",
				},
			},
			"required": []string{"host", "command"},
		},
		Execute: buildSSHExecExecute(cfg),
	})
}

func buildSSHExecExecute(cfg *SSHExecConfig) ExecuteFunc {
	return func(ctx context.Context, params map[string]any) (any, error) {
		effectiveCfg := sshExecEffectiveConfig(ctx, cfg)
		req, err := sshExecRequestFromParams(params, effectiveCfg)
		if err != nil {
			return nil, err
		}
		if reason := validateSSHExecRequest(req, effectiveCfg); reason != "" {
			return SSHExecResult{
				OK:            false,
				Host:          req.host,
				User:          req.user,
				Command:       req.command,
				ExitCode:      -1,
				Stderr:        reason,
				ErrorCategory: SSHExecErrorBlockedByPolicy,
			}, nil
		}

		runCtx, cancel := context.WithTimeout(ctx, time.Duration(req.timeoutSec)*time.Second)
		defer cancel()

		var run SSHExecRunResult
		if usesCryptoSSH(effectiveCfg) {
			run = runNativeSSH(runCtx, req, effectiveCfg)
		} else {
			args := buildSSHExecArgs(req, effectiveCfg)
			run = effectiveCfg.Runner.Run(runCtx, effectiveCfg.SSHPath, args, "")
		}
		cat := classifySSHExecRun(run)
		maxOut := effectiveCfg.MaxOutputBytes
		if maxOut <= 0 {
			maxOut = terminalDefaultMaxOutput
		}
		return SSHExecResult{
			OK:            cat == "",
			Host:          req.host,
			User:          req.user,
			Command:       req.command,
			ExitCode:      run.ExitCode,
			Stdout:        truncateOutput(run.Stdout, maxOut),
			Stderr:        truncateOutput(run.Stderr, maxOut),
			ErrorCategory: cat,
			DurationMS:    run.Duration.Milliseconds(),
		}, nil
	}
}

// sshPasswordFromContext 读取 ask_user 履约后的 SSH 密码；仅当工具配置未设置 DefaultPassword 时使用。
func sshPasswordFromContext(ctx context.Context) string {
	for _, field := range []string{"ssh_password", "password"} {
		if v, ok := SecretFromContext(ctx, field); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// sshExecEffectiveConfig 合并运行时 context secret，使 ask_user 收集的密码可用于 crypto SSH。
func sshExecEffectiveConfig(ctx context.Context, cfg *SSHExecConfig) *SSHExecConfig {
	base := normalizeSSHExecConfig(cfg)
	if strings.TrimSpace(base.DefaultPassword) != "" {
		return base
	}
	if pw := sshPasswordFromContext(ctx); pw != "" {
		cp := *base
		cp.DefaultPassword = pw
		return normalizeSSHExecConfig(&cp)
	}
	return base
}

type sshExecRequest struct {
	host                  string
	user                  string
	command               string
	timeoutSec            int
	strictHostKeyChecking string
	workingDir            string
}

func sshExecRequestFromParams(params map[string]any, cfg *SSHExecConfig) (sshExecRequest, error) {
	host := hostFromParams(params, cfg)
	if host == "" {
		return sshExecRequest{}, errors.New("ssh_exec: host is required (pass host, or set default_host / exactly one concrete allowed_hosts in tool config)")
	}
	command := firstString(params, "command", "cmd", "shell", "script")
	command = strings.TrimSpace(command)
	if command == "" {
		return sshExecRequest{}, errors.New("ssh_exec: command is required")
	}
	user := firstString(params, "user", "username", "login_user")
	user = strings.TrimSpace(user)
	if user == "" {
		user = cfg.DefaultUser
	}
	if user == "" {
		return sshExecRequest{}, errors.New("ssh_exec: user is required (or configure default_user)")
	}

	timeoutSec := cfg.DefaultTimeoutSec
	if v, ok := params["timeout_sec"]; ok {
		n, ok := ToIntNonNegative(v)
		if !ok || n == 0 {
			return sshExecRequest{}, errors.New("ssh_exec: timeout_sec must be a positive number")
		}
		timeoutSec = n
	}

	strict, _ := params["strict_host_key_checking"].(string)
	strict = normalizeStrictHostKeyChecking(strict)
	if strict == "" {
		strict = cfg.StrictHostKeyChecking
	}
	if strict == "" {
		strict = "accept-new"
	}
	if !isAllowedStrictHostKeyChecking(strict) {
		return sshExecRequest{}, errors.New("ssh_exec: strict_host_key_checking must be yes, accept-new, or no")
	}

	workingDir, _ := params["working_dir"].(string)
	return sshExecRequest{
		host:                  host,
		user:                  user,
		command:               command,
		timeoutSec:            timeoutSec,
		strictHostKeyChecking: strict,
		workingDir:            strings.TrimSpace(workingDir),
	}, nil
}

// hostFromParams 解析 host：支持多别名、列表型参数、非字符串 JSON 标量，以及配置中的 default_host / 单条明确 allowed_hosts。
func hostFromParams(params map[string]any, cfg *SSHExecConfig) string {
	host := firstString(params, "host", "ip", "hostname", "target_host", "target", "server", "machine", "remote_host", "ssh_host", "address")
	if host == "" {
		host = firstHostFromList(params, "hosts", "host_list", "targets", "ips")
	}
	if host == "" {
		host = scalarStringFromParam(params["host"])
	}
	host = strings.TrimSpace(host)
	if host != "" {
		return host
	}
	if cfg == nil {
		return ""
	}
	if h := strings.TrimSpace(cfg.DefaultHost); h != "" {
		return h
	}
	return singleConcreteAllowedHost(cfg.AllowedHosts)
}

// singleConcreteAllowedHost 在 allowed_hosts 仅有一条且为可直连主机名/IP（非 CIDR、非通配）时作为隐式默认 host。
func singleConcreteAllowedHost(allowed []string) string {
	var one string
	for _, h := range allowed {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if one != "" {
			return ""
		}
		one = h
	}
	if one == "" {
		return ""
	}
	if strings.ContainsAny(one, "*?") {
		return ""
	}
	if strings.Contains(one, "/") {
		return ""
	}
	return one
}

func scalarStringFromParam(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strings.TrimSpace(fmt.Sprint(t))
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case json.Number:
		return strings.TrimSpace(t.String())
	case bool:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func firstString(params map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := params[key]; ok {
			if s, ok := v.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func firstHostFromList(params map[string]any, keys ...string) string {
	for _, key := range keys {
		v, ok := params[key]
		if !ok {
			continue
		}
		switch vv := v.(type) {
		case []string:
			for _, item := range vv {
				if s := strings.TrimSpace(item); s != "" {
					return s
				}
			}
		case []any:
			for _, item := range vv {
				if s, ok := item.(string); ok {
					s = strings.TrimSpace(s)
					if s != "" {
						return s
					}
				}
			}
		}
	}
	return ""
}

func validateSSHExecRequest(req sshExecRequest, cfg *SSHExecConfig) string {
	if len(cfg.AllowedHosts) > 0 && !sshExecHostAllowed(req.host, cfg.AllowedHosts) {
		return fmt.Sprintf("host %q is not allowed by ssh_exec policy", req.host)
	}
	if len(cfg.AllowedUsers) > 0 && !stringInList(req.user, cfg.AllowedUsers) {
		return fmt.Sprintf("user %q is not allowed by ssh_exec policy", req.user)
	}
	if len(cfg.AllowedCommandPrefixes) > 0 && !commandHasAllowedPrefix(req.command, cfg.AllowedCommandPrefixes) {
		return fmt.Sprintf("command %q is not allowed by ssh_exec policy", req.command)
	}
	if denied, pattern := commandDenied(req.command, cfg.DeniedCommandPatterns); denied {
		return fmt.Sprintf("command %q is denied by ssh_exec policy pattern %q", req.command, pattern)
	}
	return ""
}

func buildSSHExecRemoteCommand(req sshExecRequest) string {
	remoteCommand := req.command
	if req.workingDir != "" {
		remoteCommand = "cd " + shellQuote(req.workingDir) + " && " + req.command
	}
	return remoteCommand
}

func buildSSHExecArgs(req sshExecRequest, cfg *SSHExecConfig) []string {
	remoteCommand := buildSSHExecRemoteCommand(req)
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=" + req.strictHostKeyChecking,
		"-o", fmt.Sprintf("ConnectTimeout=%d", req.timeoutSec),
	}
	if cfg != nil && cfg.Port > 0 {
		args = append(args, "-p", strconv.Itoa(cfg.Port))
	}
	args = append(args, req.user+"@"+req.host, remoteCommand)
	return args
}

func classifySSHExecRun(run SSHExecRunResult) string {
	if run.ExitCode == 0 && !run.TimedOut && run.Err == nil {
		return ""
	}
	text := strings.ToLower(run.Stdout + "\n" + run.Stderr)
	if run.TimedOut || strings.Contains(text, "context deadline exceeded") {
		return SSHExecErrorTimeout
	}
	if strings.Contains(text, "host key verification failed") ||
		strings.Contains(text, "remote host identification has changed") ||
		strings.Contains(text, "strict host key checking") {
		return SSHExecErrorHostKeyFailed
	}
	if strings.Contains(text, "permission denied") ||
		strings.Contains(text, "authentication failed") ||
		strings.Contains(text, "too many authentication failures") {
		return SSHExecErrorAuthFailed
	}
	if strings.Contains(text, "could not resolve hostname") ||
		strings.Contains(text, "name or service not known") ||
		strings.Contains(text, "no route to host") ||
		strings.Contains(text, "connection timed out") ||
		strings.Contains(text, "connection refused") ||
		strings.Contains(text, "operation timed out") ||
		strings.Contains(text, "kex_exchange_identification") {
		return SSHExecErrorNetworkFailed
	}
	if run.ExitCode != 0 {
		return SSHExecErrorCommandFailed
	}
	return SSHExecErrorInternal
}

func normalizeSSHExecConfig(cfg *SSHExecConfig) *SSHExecConfig {
	if cfg == nil {
		cfg = &SSHExecConfig{}
	}
	cp := *cfg
	if !usesCryptoSSH(&cp) {
		if cp.Runner == nil {
			cp.Runner = osSSHExecRunner{}
		}
		if cp.SSHPath == "" {
			cp.SSHPath = "ssh"
		}
	} else {
		if cp.Runner == nil {
			cp.Runner = osSSHExecRunner{}
		}
	}
	if cp.DefaultTimeoutSec <= 0 {
		cp.DefaultTimeoutSec = 30
	}
	cp.StrictHostKeyChecking = normalizeStrictHostKeyChecking(cp.StrictHostKeyChecking)
	if cp.StrictHostKeyChecking == "" {
		cp.StrictHostKeyChecking = "accept-new"
	}
	// 密码 / 原生 SSH、默认 accept-new 且无 known_hosts 时，降为不校验主机密钥（页面仅填用户/密码/端口时的默认行为）。
	kh := strings.TrimSpace(cp.KnownHostsPath)
	if cp.Native != nil && kh == "" {
		kh = strings.TrimSpace(cp.Native.KnownHostsPath)
	}
	if usesCryptoSSH(&cp) && kh == "" && cp.StrictHostKeyChecking == "accept-new" {
		cp.StrictHostKeyChecking = "no"
	}
	if len(cp.DeniedCommandPatterns) == 0 {
		cp.DeniedCommandPatterns = defaultSSHExecDeniedCommandPatterns()
	}
	return &cp
}

func SSHExecConfigFromMap(cfg map[string]interface{}) *SSHExecConfig {
	if cfg == nil {
		return &SSHExecConfig{}
	}
	if nested, ok := cfg["parameters"].(map[string]interface{}); ok {
		cfg = mergeSSHExecConfigMaps(cfg, nested)
	}
	out := &SSHExecConfig{}
	if v, ok := cfg["ssh_path"].(string); ok {
		out.SSHPath = strings.TrimSpace(v)
	}
	if out.SSHPath == "" {
		if v, ok := cfg["ssh_executable"].(string); ok {
			out.SSHPath = strings.TrimSpace(v)
		}
	}
	if v, ok := cfg["default_user"].(string); ok {
		out.DefaultUser = strings.TrimSpace(v)
	}
	if v, ok := cfg["default_host"].(string); ok {
		out.DefaultHost = strings.TrimSpace(v)
	}
	if v, ok := cfg["password"].(string); ok {
		out.DefaultPassword = v
	}
	if v, ok := cfg["default_password"].(string); ok && out.DefaultPassword == "" {
		out.DefaultPassword = v
	}
	if v, ok := cfg["port"]; ok {
		if pi, ok := ToIntNonNegative(v); ok && pi > 0 {
			out.Port = pi
		}
	}
	if v, ok := cfg["known_hosts_path"].(string); ok {
		out.KnownHostsPath = strings.TrimSpace(v)
	}
	if v, ok := cfg["timeout_sec"]; ok {
		if n, ok := ToIntNonNegative(v); ok {
			out.DefaultTimeoutSec = n
		}
	}
	if v, ok := cfg["default_timeout_sec"]; ok {
		if n, ok := ToIntNonNegative(v); ok {
			out.DefaultTimeoutSec = n
		}
	}
	if v, ok := cfg["max_output_bytes"]; ok {
		if n, ok := ToIntNonNegative(v); ok {
			out.MaxOutputBytes = n
		}
	}
	if v, ok := cfg["strict_host_key_checking"].(string); ok {
		out.StrictHostKeyChecking = normalizeStrictHostKeyChecking(v)
	}
	out.AllowedHosts = stringSliceFromAny(cfg["allowed_hosts"])
	out.AllowedUsers = stringSliceFromAny(cfg["allowed_users"])
	out.AllowedCommandPrefixes = stringSliceFromAny(cfg["allowed_command_prefixes"])
	out.DeniedCommandPatterns = stringSliceFromAny(cfg["denied_command_patterns"])
	if raw, ok := cfg["native"].(map[string]interface{}); ok {
		n := &SSHExecNativeConfig{}
		n.PrivateKeyPaths = stringSliceFromAny(raw["private_key_paths"])
		if len(n.PrivateKeyPaths) == 0 {
			if p, ok := raw["private_key_path"].(string); ok && strings.TrimSpace(p) != "" {
				n.PrivateKeyPaths = []string{strings.TrimSpace(p)}
			}
		}
		if v, ok := raw["known_hosts_path"].(string); ok {
			n.KnownHostsPath = strings.TrimSpace(v)
		}
		if v, ok := raw["port"]; ok {
			if pi, ok := ToIntNonNegative(v); ok && pi > 0 {
				n.Port = pi
			}
		}
		out.Native = n
	}
	// 仅有 native.port、无密钥与密码时，只使用顶层 Port + 系统 ssh，避免误判为 crypto 路径。
	if out.Native != nil &&
		len(out.Native.PrivateKeyPaths) == 0 &&
		strings.TrimSpace(out.Native.KnownHostsPath) == "" &&
		strings.TrimSpace(out.DefaultPassword) == "" {
		if out.Native.Port > 0 && out.Port == 0 {
			out.Port = out.Native.Port
		}
		out.Native = nil
	}
	return out
}

func mergeSSHExecConfigMaps(base, nested map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(base)+len(nested))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range nested {
		out[k] = v
	}
	return out
}

func stringSliceFromAny(v any) []string {
	switch x := v.(type) {
	case []string:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s := strings.TrimSpace(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, s)
				}
			}
		}
		return out
	default:
		return nil
	}
}

func defaultSSHExecDeniedCommandPatterns() []string {
	return []string{
		`(?i)\brm\s+-rf\b`,
		`(?i)\bmkfs\b`,
		`(?i)\bshutdown\b`,
		`(?i)\breboot\b`,
		`(?i)\bpoweroff\b`,
		`(?i)\bhalt\b`,
		`(?i)\bdd\s+if=`,
		`(?i)>\s*/etc/`,
		`(?i)\bchmod\s+-R\s+777\b`,
	}
}

func sshExecHostAllowed(host string, allowed []string) bool {
	ip := net.ParseIP(host)
	for _, pattern := range allowed {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if _, cidr, err := net.ParseCIDR(pattern); err == nil && ip != nil && cidr.Contains(ip) {
			return true
		}
		if strings.EqualFold(pattern, host) {
			return true
		}
		if strings.ContainsAny(pattern, "*?") {
			if ok, _ := path.Match(strings.ToLower(pattern), strings.ToLower(host)); ok {
				return true
			}
		}
	}
	return false
}

func stringInList(value string, allowed []string) bool {
	for _, item := range allowed {
		if strings.EqualFold(strings.TrimSpace(item), value) {
			return true
		}
	}
	return false
}

func commandHasAllowedPrefix(command string, prefixes []string) bool {
	command = strings.TrimSpace(command)
	for _, prefix := range prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix != "" && strings.HasPrefix(command, prefix) {
			return true
		}
	}
	return false
}

func commandDenied(command string, patterns []string) (bool, string) {
	for _, pattern := range patterns {
		if pattern = strings.TrimSpace(pattern); pattern == "" {
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			if strings.Contains(command, pattern) {
				return true, pattern
			}
			continue
		}
		if re.MatchString(command) {
			return true, pattern
		}
	}
	return false, ""
}

func normalizeStrictHostKeyChecking(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isAllowedStrictHostKeyChecking(value string) bool {
	switch value {
	case "yes", "accept-new", "no":
		return true
	default:
		return false
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
