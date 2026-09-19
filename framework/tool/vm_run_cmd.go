package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type vmRunCmdPolicy int

const (
	vmRunCmdDeny vmRunCmdPolicy = iota
	vmRunCmdConfirm
	vmRunCmdAllow
)

var (
	vmRunCmdDenySubstrings = []string{
		"format ",
		"format.com",
		"shutdown /s",
		"stop-computer",
		"reset-computermachinepassword",
		"rm -rf /",
		"del /s /q c:",
	}
	vmRunCmdConfirmSubstrings = []string{
		"taskkill",
		"stop-process",
		"stop-service",
		"net stop",
		"sc stop",
		"sc delete",
		"restart-computer",
		"shutdown /r",
		"remove-item",
		"del ",
		"rmdir",
		"rd ",
	}
	vmRunCmdDriveRootRe    = regexp.MustCompile(`(?i)[a-z]:\\(?:\s|$)`)
	vmRunCmdDriveRootEndRe = regexp.MustCompile(`(?i)[a-z]:\\?\s*$`)
)

func classifyVMRunCmd(cmd string) vmRunCmdPolicy {
	lower := strings.ToLower(cmd)
	for _, s := range vmRunCmdDenySubstrings {
		if strings.Contains(lower, s) {
			return vmRunCmdDeny
		}
	}
	if strings.Contains(lower, "remove-item") && strings.Contains(lower, "-recurse") {
		if vmRunCmdDriveRootRe.MatchString(cmd) || vmRunCmdDriveRootEndRe.MatchString(strings.TrimSpace(cmd)) {
			return vmRunCmdDeny
		}
	}
	for _, s := range vmRunCmdConfirmSubstrings {
		if strings.Contains(lower, s) {
			return vmRunCmdConfirm
		}
	}
	return vmRunCmdAllow
}

func parseVMRunCmdHost(host string) (string, error) {
	h := strings.TrimSpace(host)
	if h == "" {
		return "", fmt.Errorf("vm_run_cmd: host is empty")
	}
	for _, bad := range []string{"://", "/", `\`, " ", "@"} {
		if strings.Contains(h, bad) {
			return "", fmt.Errorf("vm_run_cmd: invalid host %q", host)
		}
	}
	return h, nil
}

func selectVMRunCmdDatasource(cfg VMRunCmdConfig) (string, error) {
	preferred := strings.TrimSpace(cfg.PreferredDatasourceID)
	if preferred != "" {
		for _, id := range cfg.MySQLIDs {
			if id == preferred {
				return preferred, nil
			}
		}
		return "", fmt.Errorf("preferred datasource %q is not bound; pass host or bind MySQL", preferred)
	}
	switch len(cfg.MySQLIDs) {
	case 1:
		return cfg.MySQLIDs[0], nil
	case 0:
		return "", errors.New("pass host or bind MySQL")
	default:
		return "", fmt.Errorf("multiple MySQL datasources bound (%s); pass host or set preferred", strings.Join(cfg.MySQLIDs, ", "))
	}
}

func classifyVMRunCmdLookupErr(err error) string {
	if errors.Is(err, errVMRunCmdNoIP) {
		return ErrorPermanent
	}
	if errors.Is(err, context.DeadlineExceeded) || classifyRCAError(err) == ErrorTransient {
		return ErrorTransient
	}
	return ErrorPermanent
}

func parseVMRunCmdVMID(raw string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("vm_run_cmd: invalid vmid %q: %w", raw, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("vm_run_cmd: vmid must be positive")
	}
	return n, nil
}

const (
	vmRunCmdName           = "vm_run_cmd"
	vmRunCmdDefaultPort    = 53000
	vmRunCmdDefaultTimeout = 30
	vmRunCmdMaxTimeout     = 120
	vmRunCmdMaxBody        = 50 * 1024

	// VMIPLookupSQL is the MySQL query Portal Task 6 will run via VMIPLookup.
	VMIPLookupSQL = "SELECT mgr_ipv4_address FROM t_game_virtual_machine_info WHERE vmid = ?"
)

var errVMRunCmdNoIP = errors.New("vm_run_cmd: no IP found for vmid")

type VMIPLookup func(ctx context.Context, datasourceID string, vmid int64) (host string, ambiguous bool, err error)

type VMRunCmdConfig struct {
	HTTPClient            *http.Client
	Lookup                VMIPLookup
	MySQLIDs              []string
	PreferredDatasourceID string
	TokenGen              TokenGenerator
	PendingStore          VMRunCmdPendingStore
	ConfirmTTLSeconds     int
	ClientForHost         func(host string, port int) (*http.Client, error)
}

func RegisterVMRunCmd(reg *Registry, cfg VMRunCmdConfig) error {
	if reg == nil {
		return errors.New("vm_run_cmd: registry is nil")
	}
	return reg.Register(Tool{
		Name:        vmRunCmdName,
		Description: "Run a Windows PowerShell command on a VM via POST /runCmd. Address the instance by host (hostname or IPv4) or vmid — do not pass a url parameter. Do not use http_request to hit :53000/runCmd; use this tool instead. Empty stdout (output_empty) is not evidence of missing logs — the command may have produced no output or the log pipeline may not capture it.",
		Toolset:     ToolsetRCA,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"cmd":           map[string]any{"type": "string", "description": "Command to run on the instance."},
				"host":          map[string]any{"type": "string", "description": "Instance hostname or IPv4."},
				"vmid":          map[string]any{"description": "VM id (integer or decimal string)."},
				"port":          map[string]any{"type": "integer", "description": "runCmd port (default 53000)."},
				"timeout_sec":   map[string]any{"type": "integer", "description": "Request timeout seconds (default 30, max 120)."},
				"confirm_token": map[string]any{"type": "string", "description": "Confirmation token for dangerous commands."},
			},
			"required": []string{"cmd"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			return executeVMRunCmd(ctx, cfg, params), nil
		},
	})
}

func executeVMRunCmd(ctx context.Context, cfg VMRunCmdConfig, params map[string]any) map[string]any {
	if params == nil {
		params = map[string]any{}
	}
	if _, ok := params["url"]; ok {
		return rcaErr(vmRunCmdName, "url parameter is not allowed; pass host or vmid", ErrorPermanent)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	confirmed := false
	confirmToken := ""
	if token, _ := params["confirm_token"].(string); strings.TrimSpace(token) != "" {
		confirmToken = strings.TrimSpace(token)
		pending, errMap := loadVMRunCmdPending(ctx, cfg, confirmToken)
		if errMap != nil {
			return errMap
		}
		overlayVMRunCmdPending(params, pending)
		confirmed = true
	}

	cmd, _ := params["cmd"].(string)
	if strings.TrimSpace(cmd) == "" {
		return rcaErr(vmRunCmdName, "cmd is required", ErrorPermanent)
	}

	policy := classifyVMRunCmd(cmd)
	if policy == vmRunCmdDeny {
		return rcaErr(vmRunCmdName, "blocked_by_policy", ErrorPermanent)
	}

	hostRaw, _ := params["host"].(string)
	hostRaw = strings.TrimSpace(hostRaw)
	vmid, hasVMID, vmidErr := parseVMRunCmdVMIDParam(params["vmid"])
	if vmidErr != nil {
		return rcaErr(vmRunCmdName, vmidErr.Error(), ErrorPermanent)
	}

	port := vmRunCmdDefaultPort
	if _, ok := params["port"]; ok {
		port = intFromParam(params["port"], 0)
		if port < 1 || port > 65535 {
			return rcaErr(vmRunCmdName, "port must be 1-65535", ErrorPermanent)
		}
	}

	timeoutSec := intFromParam(params["timeout_sec"], vmRunCmdDefaultTimeout)
	if timeoutSec <= 0 {
		timeoutSec = vmRunCmdDefaultTimeout
	}
	if timeoutSec > vmRunCmdMaxTimeout {
		timeoutSec = vmRunCmdMaxTimeout
	}

	var host string
	if hostRaw != "" {
		parsed, err := parseVMRunCmdHost(hostRaw)
		if err != nil {
			return rcaErr(vmRunCmdName, err.Error(), ErrorPermanent)
		}
		host = parsed
	}

	if policy == vmRunCmdConfirm && !confirmed {
		if host == "" && !hasVMID {
			return rcaErr(vmRunCmdName, "host or vmid is required", ErrorPermanent)
		}
		return proposeVMRunCmd(ctx, cfg, cmd, host, vmid, port, timeoutSec)
	}

	if host == "" && !hasVMID {
		return rcaErr(vmRunCmdName, "host or vmid is required", ErrorPermanent)
	}
	var ipAmbiguous bool
	if host == "" {
		dsID, err := selectVMRunCmdDatasource(cfg)
		if err != nil {
			return rcaErr(vmRunCmdName, err.Error(), ErrorPermanent)
		}
		if cfg.Lookup == nil {
			return rcaErr(vmRunCmdName, "vmid lookup is not available; pass host or bind MySQL", ErrorPermanent)
		}
		looked, ambiguous, err := cfg.Lookup(ctx, dsID, vmid)
		if err != nil {
			return rcaErr(vmRunCmdName, err.Error(), classifyVMRunCmdLookupErr(err))
		}
		parsed, err := parseVMRunCmdHost(looked)
		if err != nil {
			return rcaErr(vmRunCmdName, err.Error(), ErrorPermanent)
		}
		host = parsed
		ipAmbiguous = ambiguous
	}

	client, err := selectVMRunCmdClient(cfg, host, port, timeoutSec)
	if err != nil {
		return rcaErr(vmRunCmdName, err.Error(), ErrorTransient)
	}

	payload, err := json.Marshal(map[string]string{"cmd": cmd})
	if err != nil {
		return rcaErr(vmRunCmdName, err.Error(), ErrorPermanent)
	}

	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	endpoint := fmt.Sprintf("http://%s:%d/runCmd", host, port)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return rcaErr(vmRunCmdName, err.Error(), ErrorPermanent)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return rcaErr(vmRunCmdName, err.Error(), ErrorTransient)
	}
	defer resp.Body.Close()

	stdout, truncated := readVMRunCmdBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := fmt.Sprintf("runCmd returned %d", resp.StatusCode)
		if stdout != "" {
			msg = msg + ": " + stdout
		}
		out := rcaErr(vmRunCmdName, msg, runCmdHTTPErrorCode(resp.StatusCode))
		out["http_status"] = resp.StatusCode
		if stdout != "" {
			out["stdout"] = stdout
		}
		if truncated {
			out["truncated"] = true
		}
		return out
	}

	result := map[string]any{
		"stdout":       stdout,
		"output_empty": stdout == "",
		"truncated":    truncated,
		"host":         host,
		"port":         port,
	}
	if ipAmbiguous {
		result["ip_ambiguous"] = true
	}
	ref := map[string]any{"kind": vmRunCmdName, "host": host}
	if hasVMID {
		result["vmid"] = vmid
		ref["vmid"] = vmid
	}
	result["evidence_refs"] = []map[string]any{ref}
	out := rcaOK(vmRunCmdName, result)
	if confirmed && out["ok"] == true {
		sessionID, _ := ctx.Value(ContextKeySessionID).(string)
		_ = cfg.PendingStore.DeletePending(ctx, sessionID, confirmToken)
	}
	return out
}

func vmRunCmdConfirmTTL(cfg VMRunCmdConfig) int {
	if cfg.ConfirmTTLSeconds > 0 {
		return cfg.ConfirmTTLSeconds
	}
	return 300
}

func proposeVMRunCmd(ctx context.Context, cfg VMRunCmdConfig, cmd, host string, vmid int64, port, timeoutSec int) map[string]any {
	if cfg.PendingStore == nil || cfg.TokenGen == nil {
		return rcaErr(vmRunCmdName, "confirm_required_but_unconfigured", ErrorPermanent)
	}
	sessionID, _ := ctx.Value(ContextKeySessionID).(string)
	if sessionID == "" {
		return rcaErr(vmRunCmdName, "session_id is required for danger command confirm", ErrorPermanent)
	}
	token, err := cfg.TokenGen.NewToken()
	if err != nil {
		return rcaErr(vmRunCmdName, fmt.Sprintf("generate token: %v", err), ErrorPermanent)
	}
	ttl := vmRunCmdConfirmTTL(cfg)
	pending := PendingVMRunCmd{
		Token:      token,
		Command:    cmd,
		Host:       host,
		VMID:       vmid,
		Port:       port,
		TimeoutSec: timeoutSec,
		CreatedAt:  time.Now(),
	}
	if err := cfg.PendingStore.SavePending(ctx, sessionID, pending); err != nil {
		return rcaErr(vmRunCmdName, err.Error(), ErrorPermanent)
	}
	return map[string]any{
		"status":     "pending",
		"token":      token,
		"command":    cmd,
		"expires_in": ttl,
		"hint":       "user must confirm; re-call vm_run_cmd with confirm_token to execute",
	}
}

func loadVMRunCmdPending(ctx context.Context, cfg VMRunCmdConfig, token string) (*PendingVMRunCmd, map[string]any) {
	if cfg.PendingStore == nil {
		return nil, rcaErr(vmRunCmdName, "vm_run_cmd: confirm store not configured", ErrorPermanent)
	}
	sessionID, _ := ctx.Value(ContextKeySessionID).(string)
	if sessionID == "" {
		return nil, rcaErr(vmRunCmdName, "session_id is required", ErrorPermanent)
	}
	pending, err := cfg.PendingStore.GetPending(ctx, sessionID, token)
	if err != nil {
		return nil, rcaErr(vmRunCmdName, err.Error(), ErrorPermanent)
	}
	if pending == nil {
		return nil, ConfirmTokenError("not_found")
	}
	ttl := vmRunCmdConfirmTTL(cfg)
	if time.Since(pending.CreatedAt) > time.Duration(ttl)*time.Second {
		_ = cfg.PendingStore.DeletePending(ctx, sessionID, token)
		return nil, ConfirmTokenError("expired")
	}
	return pending, nil
}

func overlayVMRunCmdPending(params map[string]any, p *PendingVMRunCmd) {
	params["cmd"] = p.Command
	params["host"] = p.Host
	if p.VMID != 0 {
		params["vmid"] = p.VMID
	} else {
		delete(params, "vmid")
	}
	if p.Port != 0 {
		params["port"] = p.Port
	}
	if p.TimeoutSec != 0 {
		params["timeout_sec"] = p.TimeoutSec
	}
}

func selectVMRunCmdClient(cfg VMRunCmdConfig, host string, port, timeoutSec int) (*http.Client, error) {
	var client *http.Client
	if cfg.ClientForHost != nil {
		c, err := cfg.ClientForHost(host, port)
		if err != nil {
			return nil, err
		}
		client = c
	} else if cfg.HTTPClient != nil {
		client = cfg.HTTPClient
	}
	if client == nil {
		return &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}, nil
	}
	cp := *client
	cp.Timeout = time.Duration(timeoutSec) * time.Second
	return &cp, nil
}

func parseVMRunCmdVMIDParam(v any) (int64, bool, error) {
	if v == nil {
		return 0, false, nil
	}
	switch n := v.(type) {
	case string:
		if strings.TrimSpace(n) == "" {
			return 0, false, nil
		}
		id, err := parseVMRunCmdVMID(n)
		return id, err == nil, err
	case int:
		if n <= 0 {
			return 0, true, fmt.Errorf("vm_run_cmd: vmid must be positive")
		}
		return int64(n), true, nil
	case int32:
		if n <= 0 {
			return 0, true, fmt.Errorf("vm_run_cmd: vmid must be positive")
		}
		return int64(n), true, nil
	case int64:
		if n <= 0 {
			return 0, true, fmt.Errorf("vm_run_cmd: vmid must be positive")
		}
		return n, true, nil
	case float64:
		id := int64(n)
		if id <= 0 {
			return 0, true, fmt.Errorf("vm_run_cmd: vmid must be positive")
		}
		return id, true, nil
	default:
		return 0, true, fmt.Errorf("vm_run_cmd: invalid vmid %v", v)
	}
}

func runCmdHTTPErrorCode(status int) string {
	if status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500 {
		return ErrorTransient
	}
	return ErrorPermanent
}

func readVMRunCmdBody(r io.Reader) (string, bool) {
	limited := http.MaxBytesReader(nil, io.NopCloser(r), int64(vmRunCmdMaxBody)+1)
	raw, err := io.ReadAll(limited)
	stdout, truncated := truncateVMRunCmdBody(raw)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			truncated = true
		}
	}
	return stdout, truncated
}

func truncateVMRunCmdBody(b []byte) (string, bool) {
	if len(b) > vmRunCmdMaxBody {
		return string(b[:vmRunCmdMaxBody]), true
	}
	return string(b), false
}
