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
	vmRunCmdPowerShellRe   = regexp.MustCompile(`(?i)(?:^|[|&;\n])\s*(?:powershell(?:\.exe)?|pwsh(?:\.exe)?)\b|(?i)\b(?:Get|Set|Select|Where|ForEach|Out|Format|Measure|New|Remove|Stop|Start|Restart|Write|Add|Clear|Copy|Move|Rename|Test|ConvertTo|ConvertFrom|Import|Export|Invoke|Wait)-[A-Za-z]+\b`)
)

func isVMRunCmdPowerShell(cmd string) bool {
	return vmRunCmdPowerShellRe.MatchString(strings.TrimSpace(cmd))
}

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

// vmRunCmdEffect 复用危险命令分级：无需确认的命令视为只读，其余视为写。
func vmRunCmdEffect(args map[string]any) Effect {
	if remoteOpRequested(args, "cmd") {
		return EffectRead
	}
	cmd, _ := args["cmd"].(string)
	if strings.TrimSpace(cmd) == "" {
		return EffectUnknown
	}
	if classifyVMRunCmd(cmd) == vmRunCmdAllow {
		return EffectRead
	}
	return EffectWrite
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

// vmRunCmdDatasourceCandidates 返回按优先级排列的 MySQL 数据源：preferred 在前，其余按绑定顺序。
func vmRunCmdDatasourceCandidates(cfg VMRunCmdConfig) ([]string, error) {
	preferred := strings.TrimSpace(cfg.PreferredDatasourceID)
	if preferred != "" {
		found := false
		for _, id := range cfg.MySQLIDs {
			if id == preferred {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("preferred datasource %q is not bound; pass host or bind MySQL", preferred)
		}
	}
	if len(cfg.MySQLIDs) == 0 {
		return nil, errors.New("pass host or bind MySQL")
	}
	out := make([]string, 0, len(cfg.MySQLIDs))
	if preferred != "" {
		out = append(out, preferred)
	}
	for _, id := range cfg.MySQLIDs {
		if id != preferred {
			out = append(out, id)
		}
	}
	return out, nil
}

// lookupVMRunCmdHost 依次在候选数据源查 vmid，返回第一个查到的 IP；全部失败时返回最有信息量的错误。
func lookupVMRunCmdHost(ctx context.Context, cfg VMRunCmdConfig, vmid int64) (string, bool, error) {
	ids, err := vmRunCmdDatasourceCandidates(cfg)
	if err != nil {
		return "", false, err
	}
	if cfg.Lookup == nil {
		return "", false, errors.New("vmid lookup is not available; pass host or bind MySQL")
	}
	var firstErr error
	for _, id := range ids {
		host, ambiguous, err := cfg.Lookup(ctx, id, vmid)
		if err == nil && strings.TrimSpace(host) != "" {
			return host, ambiguous, nil
		}
		if err == nil {
			err = errVMRunCmdNoIP
		}
		if firstErr == nil || (errors.Is(firstErr, errVMRunCmdNoIP) && !errors.Is(err, errVMRunCmdNoIP)) {
			firstErr = err
		}
	}
	return "", false, firstErr
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
	vmRunCmdMaxReadBody    = 1 << 20

	// VMIPLookupSQL is the MySQL query Portal Task 6 will run via VMIPLookup.
	VMIPLookupSQL = "SELECT mgr_ipv4_address FROM t_game_virtual_machine_info WHERE vmid = ?"
)

var errVMRunCmdNoIP = errors.New("vm_run_cmd: no IP found for vmid")

// vmRunCmdTimeoutMsg：实例端 runCmd 有约 3 秒执行上限，超时时只回 "time out"，这不是命令输出。
const vmRunCmdTimeoutMsg = "the instance's runCmd hit its execution limit (~3s) and returned \"time out\"; this is NOT command output and NOT zero matches. Narrow the command: grep a more specific keyword (e.g. a timestamp prefix like 2026-09-27T21:1), target a single smaller file, or use op=ls_recent to pick the right file first"

func isRunCmdTimeout(text string) bool {
	return strings.EqualFold(strings.TrimSpace(text), "time out")
}

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
	props := map[string]any{
		"cmd":           map[string]any{"type": "string", "description": "cmd.exe command only. Examples: type D:\\path\\file.log, dir D:\\path, findstr keyword file, tasklist. Do not send PowerShell. Omit when using op."},
		"host":          map[string]any{"type": "string", "description": "Instance hostname or IPv4."},
		"vmid":          map[string]any{"description": "VM id (integer or decimal string)."},
		"port":          map[string]any{"type": "integer", "description": "runCmd port (default 53000)."},
		"timeout_sec":   map[string]any{"type": "integer", "description": "Request timeout seconds (default 30, max 120)."},
		"confirm_token": map[string]any{"type": "string", "description": "Confirmation token for dangerous commands."},
	}
	for k, v := range remoteOpSchema() {
		props[k] = v
	}
	return reg.Register(Tool{
		Name:        vmRunCmdName,
		Description: "Run a Windows cmd.exe command on a VM via POST /runCmd. The instance executes cmd only — do not send PowerShell cmdlets (Get-Content, Get-Process, Select-String) or powershell.exe. Use cmd equivalents: type, dir, findstr, tasklist. Address the instance by host (hostname or IPv4) or vmid — do not pass a url parameter. Do not use http_request to hit :53000/runCmd; use this tool instead. Empty stdout (output_empty) is not evidence of missing logs — the command may have produced no output or the log pipeline may not capture it.\nLarge stdout (>16KB) is saved under tmp/results/ with a head/tail preview (read it with read_file/search_files); without a workspace it is capped at 50KB from the START, so `type` on a large log only shows old lines. For logs prefer op instead of cmd: op=find (locate files by name or wildcard under a drive/directory, e.g. path=D:\\ pattern=<name>.log — install and log directories differ between instances, do not guess them), op=ls_recent (newest files in a directory), op=tail (last N lines of a file), op=grep (last N lines matching a keyword).",
		Toolset:     ToolsetRCA,
		Parameters: map[string]any{
			"type":       "object",
			"properties": props,
		},
		Effect:   EffectExec,
		EffectFn: vmRunCmdEffect,
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			if remoteOpRequested(params, "cmd") && params["confirm_token"] == nil {
				return executeVMRunCmdOp(ctx, cfg, params), nil
			}
			return executeVMRunCmd(ctx, cfg, params), nil
		},
	})
}

// executeVMRunCmdOp 把结构化 op 翻译成 cmd.exe 命令执行；tail 先统计行数再用 more +K 跳到末尾。
func executeVMRunCmdOp(ctx context.Context, cfg VMRunCmdConfig, params map[string]any) map[string]any {
	op, err := parseRemoteOp(params)
	if err != nil {
		return rcaErr(vmRunCmdName, err.Error(), ErrorPermanent)
	}
	// 先拿完整输出按 op 语义截取，再决定落盘或截断，避免 50KB 头部截断吃掉最新的匹配行。
	fullCtx := context.WithValue(ctx, vmRunCmdKeepFullOutputKey{}, true)
	call := func(cmd string) map[string]any {
		p := make(map[string]any, len(params))
		for k, v := range params {
			switch k {
			case "op", "path", "pattern", "patterns", "lines", "include_dirs", "recursive":
				continue
			}
			p[k] = v
		}
		p["cmd"] = cmd
		return executeVMRunCmd(fullCtx, cfg, p)
	}

	total := -1
	if op.Op == RemoteOpTail {
		countCmd, err := op.windowsLineCountCmd()
		if err != nil {
			return rcaErr(vmRunCmdName, err.Error(), ErrorPermanent)
		}
		res := call(countCmd)
		if res["ok"] != true {
			return res
		}
		stdout, _ := res["stdout"].(string)
		n, ok := parseLineCount(stdout)
		if !ok {
			out := rcaErr(vmRunCmdName, "op=tail: could not count lines (file missing or unreadable?)", ErrorPermanent)
			out["stdout"] = stdout
			return out
		}
		total = n
	}
	cmd, err := op.windowsCmd(total)
	if err != nil {
		return rcaErr(vmRunCmdName, err.Error(), ErrorPermanent)
	}
	return finishVMRunCmdOp(ctx, op, cmd, total, call(cmd))
}

func finishVMRunCmdOp(ctx context.Context, op *remoteOp, cmd string, total int, res map[string]any) map[string]any {
	res["op"] = op.Op
	res["command"] = cmd
	if total >= 0 {
		res["total_lines"] = total
	}
	if stdout, ok := res["stdout"].(string); ok {
		if res["ok"] == true {
			trimmed, omitted := op.trimOutput(stdout)
			if omitted > 0 {
				res["omitted_lines"] = omitted
			}
			stdout = trimmed
		}
		truncated, _ := res["truncated"].(bool)
		out, spill := spillOrCapVMRunCmd(ctx, stdout, &truncated)
		res["stdout"] = out
		res["truncated"] = truncated
		if spill != nil {
			res["spill"] = spill
		}
	}
	return res
}

type vmRunCmdKeepFullOutputKey struct{}

// spillOrCapVMRunCmd 超过阈值时落盘并返回头尾预览；无法落盘则按 50KB 截断头部。
func spillOrCapVMRunCmd(ctx context.Context, text string, truncated *bool) (string, *TextSpill) {
	if out, spill := MaybeSpillText(ctx, vmRunCmdName, text); spill != nil {
		return out, spill
	}
	out, cut := truncateVMRunCmdBody([]byte(text))
	if cut {
		*truncated = true
	}
	return out, nil
}

// unwrapRunCmdEnvelope 拆开 runCmd 的 {"codeDesc": "<输出>", "retCode": n} 包装，使 stdout 为真实多行文本；
const runCmdRetCodeUnknown = -1 << 62

// 响应体被读取上限截断时仍尽力解出 codeDesc，此时返回 runCmdRetCodeUnknown；非该格式时原样返回。
func unwrapRunCmdEnvelope(raw string) (string, int, bool) {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "{") {
		return raw, 0, false
	}
	var env struct {
		CodeDesc *string `json:"codeDesc"`
		RetCode  int     `json:"retCode"`
	}
	if err := json.Unmarshal([]byte(s), &env); err == nil && env.CodeDesc != nil {
		return *env.CodeDesc, env.RetCode, true
	}
	if text, ok := decodeTruncatedJSONStringField(s, "codeDesc"); ok {
		return text, runCmdRetCodeUnknown, true
	}
	return raw, 0, false
}

// decodeTruncatedJSONStringField 解码 JSON 对象中某字符串字段，允许字符串在末尾被截断。
func decodeTruncatedJSONStringField(s, field string) (string, bool) {
	i := strings.Index(s, `"`+field+`"`)
	if i < 0 {
		return "", false
	}
	rest := strings.TrimLeft(s[i+len(field)+2:], " \t\r\n")
	if !strings.HasPrefix(rest, ":") {
		return "", false
	}
	rest = strings.TrimLeft(rest[1:], " \t\r\n")
	if !strings.HasPrefix(rest, `"`) {
		return "", false
	}
	body := rest[1:]
	end := len(body)
	for j := 0; j < len(body); j++ {
		if body[j] == '\\' {
			j++
			continue
		}
		if body[j] == '"' {
			end = j
			break
		}
	}
	body = trimIncompleteJSONEscape(body[:end])
	var out string
	if err := json.Unmarshal([]byte(`"`+body+`"`), &out); err != nil {
		return "", false
	}
	return out, true
}

func trimIncompleteJSONEscape(s string) string {
	k := strings.LastIndexByte(s, '\\')
	if k < 0 {
		return s
	}
	n := 0
	for p := k; p >= 0 && s[p] == '\\'; p-- {
		n++
	}
	if n%2 == 0 {
		return s
	}
	tail := s[k:]
	if len(tail) == 1 || (tail[1] == 'u' && len(tail) < 6) {
		return s[:k]
	}
	return s
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
	if isVMRunCmdPowerShell(cmd) {
		return rcaErr(vmRunCmdName, "vm_run_cmd only runs cmd.exe, not PowerShell. Use type/dir/findstr/tasklist instead of Get-Content/Get-ChildItem/Select-String/Get-Process.", ErrorPermanent)
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
		looked, ambiguous, err := lookupVMRunCmdHost(ctx, cfg, vmid)
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

	raw, truncated := readVMRunCmdBody(resp.Body)
	text, retCode, unwrapped := unwrapRunCmdEnvelope(raw)
	var (
		stdout string
		spill  *TextSpill
	)
	if keep, _ := ctx.Value(vmRunCmdKeepFullOutputKey{}).(bool); keep {
		stdout = text
	} else {
		stdout, spill = spillOrCapVMRunCmd(ctx, text, &truncated)
	}
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
	if isRunCmdTimeout(text) {
		out := rcaErr(vmRunCmdName, vmRunCmdTimeoutMsg, ErrorTransient)
		out["timed_out"] = true
		out["host"] = host
		out["port"] = port
		return out
	}

	result := map[string]any{
		"stdout":       stdout,
		"output_empty": stdout == "",
		"truncated":    truncated,
		"host":         host,
		"port":         port,
	}
	if spill != nil {
		result["spill"] = spill
	}
	if unwrapped && retCode != runCmdRetCodeUnknown {
		result["ret_code"] = retCode
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

// readVMRunCmdBody 读取至多 vmRunCmdMaxReadBody 字节；是否再截到 vmRunCmdMaxBody 取决于能否落盘。
func readVMRunCmdBody(r io.Reader) (string, bool) {
	limited := http.MaxBytesReader(nil, io.NopCloser(r), int64(vmRunCmdMaxReadBody)+1)
	raw, err := io.ReadAll(limited)
	truncated := false
	if len(raw) > vmRunCmdMaxReadBody {
		raw = raw[:vmRunCmdMaxReadBody]
		truncated = true
	}
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			truncated = true
		}
	}
	return string(raw), truncated
}

func truncateVMRunCmdBody(b []byte) (string, bool) {
	if len(b) > vmRunCmdMaxBody {
		return string(b[:vmRunCmdMaxBody]), true
	}
	return string(b), false
}
