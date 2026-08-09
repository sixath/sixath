package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func sleepCommand(sec int) string {
	if runtime.GOOS == "windows" {
		// ping roughly 1s per count-1 on Windows
		return "ping -n " + itoa(sec+1) + " 127.0.0.1 >NUL"
	}
	return "sleep " + itoa(sec)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func TestProcessRegistry_StartPollWait(t *testing.T) {
	reg := NewProcessRegistry()
	id, err := reg.Start(processStartRequest{
		ChatSessionID: "chat-1",
		Command:       "echo hello-bg",
		TimeoutSec:    30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("empty id")
	}

	deadline := time.Now().Add(5 * time.Second)
	var poll map[string]any
	for time.Now().Before(deadline) {
		poll, err = reg.Poll(id, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if poll["status"] != processStatusRunning {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if poll["status"] != processStatusExited {
		// wait explicitly
		poll, err = reg.Wait(id, 5)
		if err != nil {
			t.Fatal(err)
		}
	}
	if poll["status"] != processStatusExited {
		t.Fatalf("status=%v poll=%#v", poll["status"], poll)
	}
	stdout, _ := poll["stdout"].(string)
	if !strings.Contains(stdout, "hello-bg") {
		t.Fatalf("stdout=%q", stdout)
	}
}

func TestProcessRegistry_Kill(t *testing.T) {
	reg := NewProcessRegistry()
	id, err := reg.Start(processStartRequest{
		ChatSessionID: "chat-1",
		Command:       sleepCommand(30),
		TimeoutSec:    60,
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	m, err := reg.Kill(id)
	if err != nil {
		t.Fatal(err)
	}
	if m["status"] != processStatusKilled {
		t.Fatalf("%#v", m)
	}
	poll, err := reg.Poll(id, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if poll["status"] != processStatusKilled {
		t.Fatalf("poll after kill: %#v", poll)
	}
}

func TestProcessRegistry_PollIncremental(t *testing.T) {
	reg := NewProcessRegistry()
	id, err := reg.Start(processStartRequest{
		Command:    "echo first && echo second",
		TimeoutSec: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = reg.Wait(id, 5)
	p1, err := reg.Poll(id, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	off, _ := p1["stdout_offset"].(int)
	p2, err := reg.Poll(id, off, 0)
	if err != nil {
		t.Fatal(err)
	}
	if p2["stdout"].(string) != "" {
		t.Fatalf("expected empty incremental stdout, got %q", p2["stdout"])
	}
}

func TestTerminalBackground_AndProcessTool(t *testing.T) {
	procs := NewProcessRegistry()
	toolReg := NewRegistry()
	cfg := &TerminalConfig{
		Enabled:      true,
		PendingStore: NewInMemoryTerminalPendingStore(),
		TokenGen:     &fakeTokenGen{next: "t"},
		Processes:    procs,
		Runner:       &fakeTerminalRunner{result: TerminalRunResult{ExitCode: 0}},
	}
	if err := RegisterTerminalTool(toolReg, cfg); err != nil {
		t.Fatal(err)
	}
	if err := RegisterProcessTool(toolReg, procs, true); err != nil {
		t.Fatal(err)
	}
	term, _ := toolReg.Get("terminal")
	proc, _ := toolReg.Get("process")
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "chat-bg")
	ctx = context.WithValue(ctx, ContextKeyWorkspaceRoot, t.TempDir())

	res, err := term.Execute(ctx, map[string]any{
		"command":    "echo bg-ok",
		"background": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["status"] != "running" {
		t.Fatalf("%#v", m)
	}
	sid, _ := m["session_id"].(string)
	if sid == "" {
		t.Fatal("missing session_id")
	}

	waitRes, err := proc.Execute(ctx, map[string]any{"action": "wait", "session_id": sid, "timeout": 5})
	if err != nil {
		t.Fatal(err)
	}
	wm := waitRes.(map[string]any)
	if wm["status"] != processStatusExited {
		t.Fatalf("%#v", wm)
	}
	if !strings.Contains(wm["stdout"].(string), "bg-ok") {
		t.Fatalf("stdout=%v", wm["stdout"])
	}

	listRes, err := proc.Execute(ctx, map[string]any{"action": "list"})
	if err != nil {
		t.Fatal(err)
	}
	procsList := listRes.(map[string]any)["processes"].([]map[string]any)
	if len(procsList) < 1 {
		t.Fatalf("expected listed process, got %#v", listRes)
	}
}

func TestProcessRegistry_SubmitAndClose(t *testing.T) {
	reg := NewProcessRegistry()
	helper := buildStdinEchoHelper(t)
	id, err := reg.Start(processStartRequest{
		RawArgs:    []string{helper},
		TimeoutSec: 15,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Submit(id, "hello-stdin"); err != nil {
		t.Fatal(err)
	}
	_, _ = reg.CloseStdin(id)
	poll, err := reg.Wait(id, 10)
	if err != nil {
		t.Fatal(err)
	}
	stdout, _ := poll["stdout"].(string)
	if !strings.Contains(stdout, "got:hello-stdin") {
		t.Fatalf("stdout=%q poll=%#v", stdout, poll)
	}
}

func TestProcessTool_WriteSubmitClose(t *testing.T) {
	procs := NewProcessRegistry()
	reg := NewRegistry()
	if err := RegisterProcessTool(reg, procs, true); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("process")
	helper := buildStdinEchoHelper(t)
	id, err := procs.Start(processStartRequest{RawArgs: []string{helper}, TimeoutSec: 15})
	if err != nil {
		t.Fatal(err)
	}
	res, err := tl.Execute(context.Background(), map[string]any{
		"action":     "submit",
		"session_id": id,
		"data":       "from-tool",
	})
	if err != nil {
		t.Fatal(err)
	}
	if errMsg, ok := res.(map[string]any)["error"]; ok && errMsg != nil && fmt.Sprint(errMsg) != "" {
		t.Fatalf("%#v", res)
	}
	_, _ = tl.Execute(context.Background(), map[string]any{"action": "close", "session_id": id})
	wait, err := tl.Execute(context.Background(), map[string]any{"action": "wait", "session_id": id, "timeout": 10})
	if err != nil {
		t.Fatal(err)
	}
	stdout, _ := wait.(map[string]any)["stdout"].(string)
	if !strings.Contains(stdout, "got:from-tool") {
		t.Fatalf("stdout=%q", stdout)
	}
}

// buildStdinEchoHelper compiles a tiny helper that prints got:<line> after reading one stdin line.
func buildStdinEchoHelper(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "echo_stdin.go")
	code := "package main\n" +
		"import (\n\t\"bufio\"\n\t\"fmt\"\n\t\"os\"\n)\n" +
		"func main() {\n" +
		"\ts, _ := bufio.NewReader(os.Stdin).ReadString('\\n')\n" +
		"\tfmt.Print(\"got:\" + s)\n" +
		"}\n"
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "echo_stdin.exe")
	if runtime.GOOS != "windows" {
		out = filepath.Join(dir, "echo_stdin")
	}
	cmd := exec.Command("go", "build", "-o", out, src)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build helper: %v\n%s", err, b)
	}
	return out
}

func TestProcessRegistry_NotifyHandler(t *testing.T) {
	reg := NewProcessRegistry()
	ch := make(chan ProcessNotifyEvent, 1)
	reg.SetNotifyHandler(func(ev ProcessNotifyEvent) {
		ch <- ev
	})
	id, err := reg.Start(processStartRequest{
		ChatSessionID:    "chat-notify",
		Command:          "echo done-notify",
		TimeoutSec:       30,
		NotifyOnComplete: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-ch:
		if ev.ProcessID != id || ev.ChatSessionID != "chat-notify" {
			t.Fatalf("%#v", ev)
		}
		if ev.Status != processStatusExited {
			t.Fatalf("status=%s", ev.Status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for notify")
	}
	if !reg.AcknowledgeNotify(id) {
		// may already be consumed by Poll elsewhere; try once from pending
		p, _ := reg.Poll(id, 0, 0)
		_ = p
	}
}

func TestProcessTool_WriteToMissing(t *testing.T) {
	procs := NewProcessRegistry()
	reg := NewRegistry()
	if err := RegisterProcessTool(reg, procs, true); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("process")
	res, err := tl.Execute(context.Background(), map[string]any{"action": "write", "session_id": "missing", "data": "y"})
	if err != nil {
		t.Fatal(err)
	}
	if res.(map[string]any)["error"] == nil {
		t.Fatalf("expected error, got %#v", res)
	}
}
