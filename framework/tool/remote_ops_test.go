package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteOp_ParseAndCommands(t *testing.T) {
	if op, err := parseRemoteOp(map[string]any{}); op != nil || err != nil {
		t.Fatalf("no op: %v %v", op, err)
	}
	if _, err := parseRemoteOp(map[string]any{"op": "grep", "path": `D:\a.log`}); err == nil {
		t.Fatal("grep without pattern must fail")
	}
	if _, err := parseRemoteOp(map[string]any{"op": "tail"}); err == nil {
		t.Fatal("tail without path must fail")
	}
	if _, err := parseRemoteOp(map[string]any{"op": "rm", "path": "x"}); err == nil {
		t.Fatal("unknown op must fail")
	}

	op, _ := parseRemoteOp(map[string]any{"op": "grep", "path": `D:\logs\agent.log`, "pattern": "prestart failed", "lines": 5000.0})
	if op.Lines != remoteOpMaxLines {
		t.Fatalf("lines should be capped: %d", op.Lines)
	}
	cmd, err := op.windowsCmd(-1)
	if err != nil || cmd != `findstr /N /I /L /C:"prestart failed" "D:\logs\agent.log"` {
		t.Fatalf("grep cmd=%q err=%v", cmd, err)
	}
	if c, _ := op.posixCmd(); c != `grep -n -i -F -e 'prestart failed' 'D:\logs\agent.log'` {
		t.Fatalf("posix grep=%q", c)
	}

	bad, _ := parseRemoteOp(map[string]any{"op": "tail", "path": `D:\a.log" & del C:\x`})
	if _, err := bad.windowsCmd(10); err == nil {
		t.Fatal("cmd.exe metacharacters must be rejected")
	}
	quoted, _ := parseRemoteOp(map[string]any{"op": "tail", "path": `/var/log/it's.log`, "lines": 20.0})
	if c, _ := quoted.posixCmd(); c != `tail -n 20 '/var/log/it'\''s.log'` {
		t.Fatalf("posix quoting=%q", c)
	}

	tail, _ := parseRemoteOp(map[string]any{"op": "tail", "path": `D:\a.log`, "lines": 10.0})
	if c, _ := tail.windowsCmd(250); c != `more +240 "D:\a.log"` {
		t.Fatalf("tail skip=%q", c)
	}
	if c, _ := tail.windowsCmd(5); c != `type "D:\a.log"` {
		t.Fatalf("short file tail=%q", c)
	}

	ls, _ := parseRemoteOp(map[string]any{"op": "ls_recent", "path": `D:\svc\state`})
	if c, _ := ls.windowsCmd(-1); c != `dir /O-D /T:W /A-D "D:\svc\state"` {
		t.Fatalf("ls_recent default=%q", c)
	}
	lsAll, _ := parseRemoteOp(map[string]any{"op": "ls_recent", "path": `D:\svc\state`, "include_dirs": true, "recursive": true})
	if c, _ := lsAll.windowsCmd(-1); c != `dir /O-D /T:W /S "D:\svc\state"` {
		t.Fatalf("ls_recent dirs+recursive=%q", c)
	}
	if c, _ := lsAll.posixCmd(); c != `ls -ltR 'D:\svc\state'` {
		t.Fatalf("posix ls_recent recursive=%q", c)
	}
}

func TestRemoteOp_Find(t *testing.T) {
	op, err := parseRemoteOp(map[string]any{"op": "find", "path": `D:\`, "patterns": []any{"cgvmagent.log", "xagent.log"}})
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := op.windowsCmd(-1); c != `dir /S /B /A-D "D:\cgvmagent.log" "D:\xagent.log"` {
		t.Fatalf("windows find=%q", c)
	}
	noSlash, _ := parseRemoteOp(map[string]any{"op": "find", "path": `D:`, "pattern": "*.log"})
	if c, _ := noSlash.windowsCmd(-1); c != `dir /S /B /A-D "D:\*.log"` {
		t.Fatalf("windows find drive without slash=%q", c)
	}
	lin, _ := parseRemoteOp(map[string]any{"op": "find", "path": "/data", "patterns": []any{"a.log", "b.log"}})
	if c, _ := lin.posixCmd(); c != `find '/data' -type f \( -name 'a.log' -o -name 'b.log' \)` {
		t.Fatalf("posix find=%q", c)
	}
	if _, err := parseRemoteOp(map[string]any{"op": "find", "path": `D:\`}); err == nil {
		t.Fatal("find without pattern must fail")
	}
	if _, err := parseRemoteOp(map[string]any{"op": "find", "path": `D:\`, "pattern": `logs\cgvmagent.log`}); err == nil {
		t.Fatal("find pattern with directory must fail")
	}
	bad, _ := parseRemoteOp(map[string]any{"op": "find", "path": `D:\`, "pattern": `a.log" & del x`})
	if _, err := bad.windowsCmd(-1); err == nil {
		t.Fatal("cmd.exe metacharacters must be rejected")
	}
	var b strings.Builder
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&b, "D:\\p%d\\cgvmagent.log\r\n", i)
	}
	out, omitted := (&remoteOp{Op: RemoteOpFind, Lines: 2}).trimOutput(b.String())
	if !strings.HasPrefix(out, `D:\p1\`) || strings.Contains(out, `p3`) || omitted != 3 {
		t.Fatalf("find keeps first paths: %q omitted=%d", out, omitted)
	}
}

func TestRemoteOp_TrimOutput(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&b, "line%d\n", i)
	}
	grep := &remoteOp{Op: RemoteOpGrep, Lines: 3}
	out, omitted := grep.trimOutput(b.String())
	if out != "line28\nline29\nline30" || omitted != 27 {
		t.Fatalf("grep keeps last lines: %q omitted=%d", out, omitted)
	}
	ls := &remoteOp{Op: RemoteOpListRecent, Lines: 2}
	out, omitted = ls.trimOutput(b.String())
	if !strings.HasPrefix(out, "line1\n") || strings.Contains(out, "line11") || omitted != 20 {
		t.Fatalf("ls keeps newest (first) lines: %q omitted=%d", out, omitted)
	}
	if n, ok := parseLineCount("  1234\r\n"); !ok || n != 1234 {
		t.Fatalf("count=%d ok=%v", n, ok)
	}
	if _, ok := parseLineCount("File not found"); ok {
		t.Fatal("non-numeric count must fail")
	}
	noisy := "FINDSTR: 行 3 太长。\r\n12:err a\r\nFINDSTR: 行 9 太长。\r\n15:err b\r\n"
	out, omitted = (&remoteOp{Op: RemoteOpGrep, Lines: 10}).trimOutput(noisy)
	if strings.Contains(out, "FINDSTR") || !strings.Contains(out, "12:err a") || !strings.Contains(out, "15:err b") || omitted != 0 {
		t.Fatalf("findstr diagnostics must be dropped: %q omitted=%d", out, omitted)
	}
}

func TestRemoteOp_MultiplePatterns(t *testing.T) {
	op, err := parseRemoteOp(map[string]any{"op": "grep", "path": `D:\a.log`, "pattern": "crash", "patterns": []any{"timeout", "crash", "repair"}})
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := op.windowsCmd(-1); c != `findstr /N /I /L /C:"crash" /C:"timeout" /C:"repair" "D:\a.log"` {
		t.Fatalf("windows multi grep=%q", c)
	}
	if c, _ := op.posixCmd(); c != `grep -n -i -F -e 'crash' -e 'timeout' -e 'repair' 'D:\a.log'` {
		t.Fatalf("posix multi grep=%q", c)
	}
	if _, err := parseRemoteOp(map[string]any{"op": "grep", "path": "x", "patterns": []any{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"}}); err == nil {
		t.Fatal("too many patterns must fail")
	}
}

// 输出超过读取上限时 JSON 包装被截断，仍需拆出 codeDesc 的真实多行文本，且不能谎报 ret_code。
func TestUnwrapRunCmdEnvelope_Truncated(t *testing.T) {
	full, _ := json.Marshal(map[string]any{"codeDesc": "l1\r\nl2 \"q\" \u4e2d\r\nl3 tail", "retCode": 0})
	for _, cut := range []int{len(full) - 20, len(full) - 25, len(full) - 30} {
		text, code, ok := unwrapRunCmdEnvelope(string(full[:cut]))
		if !ok || code != runCmdRetCodeUnknown || !strings.HasPrefix(text, "l1\r\nl2 ") {
			t.Fatalf("cut=%d text=%q code=%d ok=%v", cut, text, code, ok)
		}
	}
	if _, _, ok := unwrapRunCmdEnvelope(`{"codeDesc": "abc\u4e`); !ok {
		t.Fatal("dangling unicode escape must be trimmed, not fail")
	}
	if text, code, ok := unwrapRunCmdEnvelope(`{"retCode":3,"codeDesc":"x"}`); !ok || text != "x" || code != 3 {
		t.Fatalf("complete envelope: %q %d %v", text, code, ok)
	}
	if _, _, ok := unwrapRunCmdEnvelope(`{"other":"x"`); ok {
		t.Fatal("non-envelope must stay raw")
	}
}

func TestVMRunCmd_OpTailCountsThenSkips(t *testing.T) {
	var cmds []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		cmds = append(cmds, body["cmd"])
		if strings.Contains(body["cmd"], `find /c /v ""`) {
			_, _ = w.Write([]byte("\r\n---------- D:\\LOGS\\AGENT.LOG: 500\r\n"))
			return
		}
		_, _ = w.Write([]byte("l498\r\nl499\r\nl500\r\n"))
	}))
	t.Cleanup(srv.Close)
	host, port := hostPort(t, srv.URL)
	reg := NewRegistry()
	if err := RegisterVMRunCmd(reg, VMRunCmdConfig{HTTPClient: srv.Client()}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("vm_run_cmd")
	args := map[string]any{"host": host, "port": port, "op": "tail", "path": `D:\logs\agent.log`, "lines": 3}
	if eff := tl.EffectFor(args); eff != EffectRead {
		t.Fatalf("op call should be read-only, got %q", eff)
	}
	out, err := tl.Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["ok"] != true || m["total_lines"] != 500 || m["op"] != "tail" {
		t.Fatalf("%#v", m)
	}
	if len(cmds) != 2 || cmds[0] != `find /c /v "" "D:\logs\agent.log"` || cmds[1] != `more +497 "D:\logs\agent.log"` {
		t.Fatalf("cmds=%q", cmds)
	}
}

// 真实 runCmd 返回 {"codeDesc": "<输出>", "retCode": 0}，工具需拆包后再计数 / 截取 / 落盘。
func TestVMRunCmd_UnwrapsRunCmdEnvelope(t *testing.T) {
	var grepOut strings.Builder
	for i := 1; i <= 2000; i++ {
		fmt.Fprintf(&grepOut, "%d:{\"L\":\"info\",\"M\":\"CheckGameStatus run bat check.bat failed seq=%d\"}\r\n", 100000+i, i)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		desc := "plain output\r\nline2\r\n"
		retCode := 0
		switch {
		case strings.Contains(body["cmd"], `find /c /v ""`):
			desc = "14009\r\n"
		case strings.HasPrefix(body["cmd"], "more +"):
			desc = "t1\r\nt2\r\n"
		case strings.HasPrefix(body["cmd"], "findstr"):
			desc = grepOut.String()
		}
		b, _ := json.Marshal(map[string]any{"codeDesc": desc, "retCode": retCode})
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	host, port := hostPort(t, srv.URL)
	reg := NewRegistry()
	if err := RegisterVMRunCmd(reg, VMRunCmdConfig{HTTPClient: srv.Client()}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("vm_run_cmd")
	ctx, ws := textSpillCtx(t)

	m := mustMap(t, tl, ctx, map[string]any{"host": host, "port": port, "cmd": "tasklist"})
	if m["stdout"] != "plain output\r\nline2\r\n" || m["ret_code"] != 0 {
		t.Fatalf("envelope should be unwrapped: %#v", m)
	}

	m = mustMap(t, tl, ctx, map[string]any{"host": host, "port": port, "op": "tail", "path": `G:\x\NtUniSdk.log`, "lines": 2})
	if m["ok"] != true || m["total_lines"] != 14009 || m["command"] != `more +14007 "G:\x\NtUniSdk.log"` {
		t.Fatalf("tail should parse unwrapped count: %#v", m)
	}

	m = mustMap(t, tl, ctx, map[string]any{"host": host, "port": port, "op": "grep", "path": `D:\a.log`, "pattern": "check.bat", "lines": 300})
	if m["omitted_lines"] != 1700 {
		t.Fatalf("grep should keep last 300 real lines, omitted=%v", m["omitted_lines"])
	}
	spill, ok := m["spill"].(*TextSpill)
	if !ok {
		t.Fatalf("300 long lines should spill: %#v", m["spill"])
	}
	saved, err := os.ReadFile(filepath.Join(ws, filepath.FromSlash(spill.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(saved), "\n") < 299 || !strings.Contains(string(saved), "seq=2000") || strings.Contains(string(saved), "seq=1700\"") {
		t.Fatalf("spill file should hold the newest 300 matches as real lines (len=%d)", len(saved))
	}
	if !strings.Contains(m["stdout"].(string), "seq=2000") {
		t.Fatal("preview tail should show the newest match")
	}
}

func TestVMRunCmd_OpFindPostsDirSearch(t *testing.T) {
	var cmds []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		cmds = append(cmds, body["cmd"])
		_, _ = w.Write([]byte("D:\\CloudGameBundle\\logs\\cgvmagent\\cgvmagent.log\r\n"))
	}))
	t.Cleanup(srv.Close)
	host, port := hostPort(t, srv.URL)
	reg := NewRegistry()
	if err := RegisterVMRunCmd(reg, VMRunCmdConfig{HTTPClient: srv.Client()}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("vm_run_cmd")
	args := map[string]any{"host": host, "port": port, "op": "find", "path": `D:\`, "patterns": []any{"cgvmagent.log", "xagent.log"}}
	if eff := tl.EffectFor(args); eff != EffectRead {
		t.Fatalf("op=find should be read-only, got %q", eff)
	}
	m := mustMap(t, tl, context.Background(), args)
	if m["ok"] != true || m["op"] != "find" || !strings.Contains(m["stdout"].(string), `logs\cgvmagent\cgvmagent.log`) {
		t.Fatalf("%#v", m)
	}
	if len(cmds) != 1 || cmds[0] != `dir /S /B /A-D "D:\cgvmagent.log" "D:\xagent.log"` {
		t.Fatalf("cmds=%q", cmds)
	}
}

func TestVMRunCmd_OpGrepRejectsUnsafePathWithoutPosting(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	t.Cleanup(srv.Close)
	host, port := hostPort(t, srv.URL)
	reg := NewRegistry()
	if err := RegisterVMRunCmd(reg, VMRunCmdConfig{HTTPClient: srv.Client()}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("vm_run_cmd")
	out, _ := tl.Execute(context.Background(), map[string]any{
		"host": host, "port": port, "op": "grep", "path": `D:\a.log`, "pattern": `x" & taskkill /IM a.exe & echo "`,
	})
	if m := out.(map[string]any); m["ok"] != false || hits != 0 {
		t.Fatalf("unsafe pattern must be rejected before POST: %#v hits=%d", m, hits)
	}
}

func TestSSHExec_OpBuildsPosixCommand(t *testing.T) {
	runner := &fakeSSHRunner{result: SSHExecRunResult{ExitCode: 0, Stdout: "a\nb\nc\nd\n"}}
	reg := NewEmptyRegistry()
	if err := RegisterSSHExecTool(reg, &SSHExecConfig{Runner: runner, DefaultUser: "ops", AllowedHosts: []string{"10.0.0.1"}}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("ssh_exec")
	args := map[string]any{"host": "10.0.0.1", "op": "grep", "path": "/var/log/app.log", "pattern": "error", "lines": 2}
	if eff := tl.EffectFor(args); eff != EffectRead {
		t.Fatalf("op call should be read-only, got %q", eff)
	}
	if eff := tl.EffectFor(map[string]any{"command": "rm -rf /tmp/x"}); eff != EffectExec {
		t.Fatalf("raw command keeps exec effect, got %q", eff)
	}
	out, err := tl.Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	res := out.(SSHExecResult)
	if res.Command != `grep -n -i -F -e 'error' '/var/log/app.log'` {
		t.Fatalf("command=%q", res.Command)
	}
	if res.Stdout != "c\nd" {
		t.Fatalf("stdout should keep last 2 lines, got %q", res.Stdout)
	}
}
