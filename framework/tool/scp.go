package tool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const scpDefaultMaxFileBytes = 100 * 1024 * 1024

// SCPConfig configures the scp file transfer tool (upload/download over SSH).
// Embeds SSHExecConfig for host/user/auth policy shared with ssh_exec.
type SCPConfig struct {
	SSHExecConfig
	SCPPath                   string
	AllowedLocalPathPrefixes  []string
	AllowedRemotePathPrefixes []string
	MaxFileBytes              int
}

type scpRequest struct {
	host                  string
	user                  string
	direction             string
	localPath             string
	remotePath            string
	timeoutSec            int
	strictHostKeyChecking string
}

type SCPResult struct {
	OK               bool   `json:"ok"`
	Host             string `json:"host"`
	User             string `json:"user"`
	Direction        string `json:"direction"`
	LocalPath        string `json:"local_path"`
	RemotePath       string `json:"remote_path"`
	BytesTransferred int64  `json:"bytes_transferred"`
	Stderr           string `json:"stderr,omitempty"`
	ErrorCategory    string `json:"error_category,omitempty"`
	DurationMS       int64  `json:"duration_ms"`
}

func RegisterSCPTool(reg *Registry, cfg *SCPConfig, opts ...*RegisterToolOptions) error {
	if reg == nil {
		return errors.New("scp: registry is nil")
	}
	cfg = normalizeSCPConfig(cfg)
	if err := validateSSHExecCryptoConfig(&cfg.SSHExecConfig); err != nil {
		return err
	}
	desc := "Upload or download a file over SCP/SFTP to an allowed host. Use direction=upload (local_path -> remote_path) or direction=download (remote_path -> local_path)."
	if len(opts) > 0 && opts[0] != nil && opts[0].Description != "" {
		desc = opts[0].Description
	}
	return reg.Register(Tool{
		Name:        "scp",
		Description: desc,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"host": map[string]any{
					"type":        "string",
					"description": "SSH target host or IP; must match allowed_hosts when configured.",
				},
				"user": map[string]any{
					"type":        "string",
					"description": "SSH login user. If omitted, default_user is used.",
				},
				"direction": map[string]any{
					"type":        "string",
					"enum":        []string{"upload", "download"},
					"description": "upload copies local_path to remote_path; download copies remote_path to local_path.",
				},
				"local_path": map[string]any{
					"type":        "string",
					"description": "Local filesystem path (source for upload, destination for download).",
				},
				"remote_path": map[string]any{
					"type":        "string",
					"description": "Remote filesystem path (destination for upload, source for download).",
				},
				"timeout_sec": map[string]any{
					"type":        "integer",
					"description": "Overall transfer timeout in seconds. Defaults to tool policy.",
				},
				"strict_host_key_checking": map[string]any{
					"type":        "string",
					"description": "OpenSSH StrictHostKeyChecking value: yes, accept-new, or no.",
				},
			},
			"required": []string{"host", "direction", "local_path", "remote_path"},
		},
		Execute: buildSCPExecute(cfg),
	})
}

func buildSCPExecute(cfg *SCPConfig) ExecuteFunc {
	return func(ctx context.Context, params map[string]any) (any, error) {
		effective := normalizeSCPConfig(scpEffectiveConfig(ctx, cfg))
		req, err := scpRequestFromParams(params, effective)
		if err != nil {
			return nil, err
		}
		if reason := validateSCPRequest(req, effective); reason != "" {
			return SCPResult{
				OK:            false,
				Host:          req.host,
				User:          req.user,
				Direction:     req.direction,
				LocalPath:     req.localPath,
				RemotePath:    req.remotePath,
				Stderr:        reason,
				ErrorCategory: SSHExecErrorBlockedByPolicy,
			}, nil
		}

		runCtx, cancel := context.WithTimeout(ctx, time.Duration(req.timeoutSec)*time.Second)
		defer cancel()

		var run scpRunResult
		if usesCryptoSSH(&effective.SSHExecConfig) {
			run = runNativeSCP(runCtx, req, effective)
		} else {
			args := buildSCPArgs(req, effective)
			extRun := effective.Runner.Run(runCtx, effective.SCPPath, args, "")
			run = scpRunResult{
				ExitCode: extRun.ExitCode,
				Stderr:   extRun.Stderr,
				Err:      extRun.Err,
				TimedOut: extRun.TimedOut,
				Duration: extRun.Duration,
			}
			if req.direction == "upload" {
				if info, statErr := os.Stat(req.localPath); statErr == nil && run.Err == nil && run.ExitCode == 0 {
					run.BytesTransferred = info.Size()
				}
			} else if run.Err == nil && run.ExitCode == 0 {
				if info, statErr := os.Stat(req.localPath); statErr == nil {
					run.BytesTransferred = info.Size()
				}
			}
		}

		cat := classifySCPRun(run)
		return SCPResult{
			OK:               cat == "",
			Host:             req.host,
			User:             req.user,
			Direction:        req.direction,
			LocalPath:        req.localPath,
			RemotePath:       req.remotePath,
			BytesTransferred: run.BytesTransferred,
			Stderr:           truncateOutput(run.Stderr, effective.MaxOutputBytes),
			ErrorCategory:    cat,
			DurationMS:       run.Duration.Milliseconds(),
		}, nil
	}
}

type scpRunResult struct {
	ExitCode         int
	Stderr           string
	Err              error
	TimedOut         bool
	Duration         time.Duration
	BytesTransferred int64
}

func scpEffectiveConfig(ctx context.Context, cfg *SCPConfig) *SCPConfig {
	if cfg == nil {
		return normalizeSCPConfig(nil)
	}
	base := normalizeSCPConfig(cfg)
	sshCfg := sshExecEffectiveConfig(ctx, &base.SSHExecConfig)
	cp := *base
	cp.SSHExecConfig = *sshCfg
	return normalizeSCPConfig(&cp)
}

func scpRequestFromParams(params map[string]any, cfg *SCPConfig) (scpRequest, error) {
	host := hostFromParams(params, &cfg.SSHExecConfig)
	if host == "" {
		return scpRequest{}, errors.New("scp: host is required (pass host, or set default_host / exactly one concrete allowed_hosts in tool config)")
	}
	direction := strings.ToLower(strings.TrimSpace(firstString(params, "direction")))
	switch direction {
	case "upload", "download":
	default:
		return scpRequest{}, errors.New("scp: direction must be upload or download")
	}
	localPath := strings.TrimSpace(firstString(params, "local_path", "local", "src", "source"))
	if localPath == "" {
		return scpRequest{}, errors.New("scp: local_path is required")
	}
	localPath = filepath.Clean(localPath)
	remotePath := cleanRemotePath(firstString(params, "remote_path", "remote", "dest", "destination"))
	if remotePath == "" {
		return scpRequest{}, errors.New("scp: remote_path is required")
	}

	user := firstString(params, "user", "username", "login_user")
	user = strings.TrimSpace(user)
	if user == "" {
		user = cfg.DefaultUser
	}
	if user == "" {
		return scpRequest{}, errors.New("scp: user is required (or configure default_user)")
	}

	timeoutSec := cfg.DefaultTimeoutSec
	if v, ok := params["timeout_sec"]; ok {
		n, ok := ToIntNonNegative(v)
		if !ok || n == 0 {
			return scpRequest{}, errors.New("scp: timeout_sec must be a positive number")
		}
		timeoutSec = n
	}

	strict := normalizeStrictHostKeyChecking(firstString(params, "strict_host_key_checking"))
	if strict == "" {
		strict = cfg.StrictHostKeyChecking
	}
	if strict == "" {
		strict = "accept-new"
	}
	if !isAllowedStrictHostKeyChecking(strict) {
		return scpRequest{}, errors.New("scp: strict_host_key_checking must be yes, accept-new, or no")
	}

	return scpRequest{
		host:                  host,
		user:                  user,
		direction:             direction,
		localPath:             localPath,
		remotePath:            remotePath,
		timeoutSec:            timeoutSec,
		strictHostKeyChecking: strict,
	}, nil
}

func validateSCPRequest(req scpRequest, cfg *SCPConfig) string {
	if len(cfg.AllowedHosts) > 0 && !sshExecHostAllowed(req.host, cfg.AllowedHosts) {
		return fmt.Sprintf("host %q is not allowed by scp policy", req.host)
	}
	if len(cfg.AllowedUsers) > 0 && !stringInList(req.user, cfg.AllowedUsers) {
		return fmt.Sprintf("user %q is not allowed by scp policy", req.user)
	}
	if !localPathAllowed(req.localPath, cfg.AllowedLocalPathPrefixes) {
		return fmt.Sprintf("local_path %q is not allowed by scp policy", req.localPath)
	}
	if !remotePathAllowed(req.remotePath, cfg.AllowedRemotePathPrefixes) {
		return fmt.Sprintf("remote_path %q is not allowed by scp policy", req.remotePath)
	}
	if strings.Contains(req.remotePath, "..") {
		return fmt.Sprintf("remote_path %q must not contain ..", req.remotePath)
	}

	maxBytes := cfg.MaxFileBytes
	if maxBytes <= 0 {
		maxBytes = scpDefaultMaxFileBytes
	}

	switch req.direction {
	case "upload":
		info, err := os.Stat(req.localPath)
		if err != nil {
			return fmt.Sprintf("local_path %q is not accessible: %v", req.localPath, err)
		}
		if info.IsDir() {
			return fmt.Sprintf("local_path %q is a directory; scp only supports single files", req.localPath)
		}
		if info.Size() > int64(maxBytes) {
			return fmt.Sprintf("local file size %d exceeds scp max_file_bytes limit %d", info.Size(), maxBytes)
		}
	case "download":
		dir := filepath.Dir(req.localPath)
		if dir != "" {
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				return fmt.Sprintf("local destination directory %q is not accessible", dir)
			}
		}
	}
	return ""
}

func buildSCPArgs(req scpRequest, cfg *SCPConfig) []string {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=" + req.strictHostKeyChecking,
		"-o", fmt.Sprintf("ConnectTimeout=%d", req.timeoutSec),
	}
	if cfg.Port > 0 {
		args = append(args, "-P", strconv.Itoa(cfg.Port))
	}
	target := req.user + "@" + req.host + ":" + req.remotePath
	if req.direction == "upload" {
		args = append(args, req.localPath, target)
	} else {
		args = append(args, target, req.localPath)
	}
	return args
}

func classifySCPRun(run scpRunResult) string {
	wrapped := SSHExecRunResult{
		ExitCode: run.ExitCode,
		Stderr:   run.Stderr,
		Err:      run.Err,
		TimedOut: run.TimedOut,
	}
	return classifySSHExecRun(wrapped)
}

func normalizeSCPConfig(cfg *SCPConfig) *SCPConfig {
	if cfg == nil {
		cfg = &SCPConfig{}
	}
	cp := *cfg
	base := normalizeSSHExecConfig(&cp.SSHExecConfig)
	cp.SSHExecConfig = *base
	if cp.SCPPath == "" {
		cp.SCPPath = "scp"
	}
	if cp.MaxFileBytes <= 0 {
		cp.MaxFileBytes = scpDefaultMaxFileBytes
	}
	return &cp
}

func SCPConfigFromMap(cfg map[string]interface{}) *SCPConfig {
	if cfg == nil {
		return &SCPConfig{}
	}
	if nested, ok := cfg["parameters"].(map[string]interface{}); ok {
		cfg = mergeSSHExecConfigMaps(cfg, nested)
	}
	out := &SCPConfig{SSHExecConfig: *SSHExecConfigFromMap(cfg)}
	out.SCPPath = strings.TrimSpace(firstStringFromMap(cfg, "scp_path", "scp_executable"))
	out.AllowedLocalPathPrefixes = stringSliceFromAny(cfg["allowed_local_path_prefixes"])
	out.AllowedRemotePathPrefixes = stringSliceFromAny(cfg["allowed_remote_path_prefixes"])
	if v, ok := cfg["max_file_bytes"]; ok {
		if n, ok := ToIntNonNegative(v); ok && n > 0 {
			out.MaxFileBytes = n
		}
	}
	return normalizeSCPConfig(out)
}

func firstStringFromMap(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key].(string); ok {
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		}
	}
	return ""
}

func cleanRemotePath(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.ReplaceAll(raw, "\\", "/")
	return path.Clean(raw)
}

func localPathAllowed(p string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	clean := filepath.Clean(p)
	for _, prefix := range prefixes {
		prefix = filepath.Clean(strings.TrimSpace(prefix))
		if prefix == "" {
			continue
		}
		if clean == prefix || strings.HasPrefix(clean, prefix+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func remotePathAllowed(p string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	clean := cleanRemotePath(p)
	for _, prefix := range prefixes {
		prefix = cleanRemotePath(prefix)
		if prefix == "" {
			continue
		}
		if clean == prefix || strings.HasPrefix(clean, prefix+"/") {
			return true
		}
	}
	return false
}
