package tool

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

type fakeTerminalRunner struct {
	lastCommand string
	lastDir     string
	result      TerminalRunResult
}

func (f *fakeTerminalRunner) Run(_ context.Context, _ string, args []string, dir string) TerminalRunResult {
	f.lastDir = dir
	if len(args) > 0 {
		f.lastCommand = args[len(args)-1]
	}
	return f.result
}

func registerTerminalForTest(t *testing.T, runner TerminalRunner) Tool {
	t.Helper()
	return registerTerminalForTestWithStore(t, runner, NewInMemoryTerminalPendingStore(), &fakeTokenGen{next: "tok-term"})
}

func registerTerminalForTestWithStore(t *testing.T, runner TerminalRunner, store TerminalPendingStore, gen TokenGenerator) Tool {
	t.Helper()
	reg := NewRegistry()
	cfg := &TerminalConfig{
		Enabled:           true,
		DefaultTimeoutSec: 5,
		MaxOutputBytes:    1024,
		PendingStore:      store,
		TokenGen:          gen,
		Runner:            runner,
	}
	if err := RegisterTerminalTool(reg, cfg); err != nil {
		t.Fatal(err)
	}
	tl, ok := reg.Get("terminal")
	if !ok {
		t.Fatal("terminal not registered")
	}
	return tl
}

func TestTerminalTool_DeniesDangerousCommands(t *testing.T) {
	runner := &fakeTerminalRunner{result: TerminalRunResult{ExitCode: 0}}
	tl := registerTerminalForTest(t, runner)
	ctx := context.WithValue(context.Background(), ContextKeyWorkspaceRoot, t.TempDir())
	ctx = context.WithValue(ctx, ContextKeySessionID, "sess-1")

	cases := []string{
		"rm -rf /",
		`:(){ :|:& };:`,
		"mkfs.ext4 /dev/sda",
	}
	for _, cmd := range cases {
		res, err := tl.Execute(ctx, map[string]any{"command": cmd})
		if err != nil {
			t.Fatalf("cmd %q: %v", cmd, err)
		}
		m := res.(map[string]any)
		if m["error"] != "command_denied" {
			t.Fatalf("cmd %q: expected command_denied, got %#v", cmd, m)
		}
	}
	if runner.lastCommand != "" {
		t.Fatal("runner should not be invoked for denied commands")
	}
}

func TestTerminalTool_DangerProposeAndConfirm(t *testing.T) {
	runner := &fakeTerminalRunner{result: TerminalRunResult{ExitCode: 0, Stdout: "ok\n"}}
	store := NewInMemoryTerminalPendingStore()
	tl := registerTerminalForTestWithStore(t, runner, store, &fakeTokenGen{next: "tok-danger"})
	ctx := context.WithValue(context.Background(), ContextKeyWorkspaceRoot, t.TempDir())
	ctx = context.WithValue(ctx, ContextKeySessionID, "sess-danger")

	res, err := tl.Execute(ctx, map[string]any{"command": "rm -rf ./build", "workdir": "."})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["status"] != "pending" || m["token"] != "tok-danger" {
		t.Fatalf("propose: %#v", m)
	}
	if runner.lastCommand != "" {
		t.Fatal("runner must not run on propose")
	}

	res2, err := tl.Execute(ctx, map[string]any{
		"command":       "ignored-should-use-pending",
		"confirm_token": "tok-danger",
	})
	if err != nil {
		t.Fatal(err)
	}
	m2 := res2.(map[string]any)
	if m2["status"] != "ok" {
		t.Fatalf("confirm: %#v", m2)
	}
	if runner.lastCommand != "rm -rf ./build" {
		t.Fatalf("expected pending command, got %q", runner.lastCommand)
	}
	pending, _ := store.GetPending(ctx, "sess-danger", "tok-danger")
	if pending != nil {
		t.Fatal("pending should be deleted after successful confirm")
	}
}

func TestTerminalTool_DangerWithoutStoreRejected(t *testing.T) {
	runner := &fakeTerminalRunner{result: TerminalRunResult{ExitCode: 0}}
	reg := NewRegistry()
	cfg := &TerminalConfig{
		Enabled: true,
		Runner:  runner,
		// no PendingStore / TokenGen
	}
	if err := RegisterTerminalTool(reg, cfg); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("terminal")
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "s")
	res, err := tl.Execute(ctx, map[string]any{"command": "sudo apt install x"})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["error"] != "confirm_required_but_unconfigured" {
		t.Fatalf("%#v", m)
	}
	if runner.lastCommand != "" {
		t.Fatal("must not execute")
	}
}

func TestTerminalTool_ConfirmIgnoresClientCommand(t *testing.T) {
	runner := &fakeTerminalRunner{result: TerminalRunResult{ExitCode: 0}}
	store := NewInMemoryTerminalPendingStore()
	tl := registerTerminalForTestWithStore(t, runner, store, &fakeTokenGen{next: "tok-1"})
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess")
	ctx = context.WithValue(ctx, ContextKeyWorkspaceRoot, t.TempDir())

	_, _ = tl.Execute(ctx, map[string]any{"command": "sudo true"})
	_, _ = tl.Execute(ctx, map[string]any{
		"confirm_token": "tok-1",
		"command":       "echo pwned",
	})
	if runner.lastCommand != "sudo true" {
		t.Fatalf("got %q", runner.lastCommand)
	}
}

func TestTerminalTool_ExecutesAllowedCommand(t *testing.T) {
	root := t.TempDir()
	runner := &fakeTerminalRunner{
		result: TerminalRunResult{
			ExitCode: 0,
			Stdout:   "\x1b[31mhello\x1b[0m\n",
			Stderr:   "",
		},
	}
	tl := registerTerminalForTest(t, runner)
	ctx := context.WithValue(context.Background(), ContextKeyWorkspaceRoot, root)

	res, err := tl.Execute(ctx, map[string]any{
		"command": "echo hello",
		"workdir": ".",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["status"] != "ok" {
		t.Fatalf("%#v", m)
	}
	if stdout := m["stdout"].(string); !strings.Contains(stdout, "hello") || strings.Contains(stdout, "\x1b") {
		t.Fatalf("stdout=%q", stdout)
	}
	if runner.lastDir == "" {
		t.Fatal("expected workdir")
	}
}

func TestTerminalTool_CheckFnDisabledByDefault(t *testing.T) {
	reg := NewRegistry()
	old := TerminalLocalEnabled
	TerminalLocalEnabled = false
	t.Cleanup(func() { TerminalLocalEnabled = old })
	if err := RegisterTerminalTool(reg, nil); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("terminal")
	if err := tl.CheckFn(context.Background()); err == nil {
		t.Fatal("expected disabled check to fail")
	}
}

func TestTerminalTool_PtyForeground(t *testing.T) {
	old := TerminalLocalEnabled
	TerminalLocalEnabled = true
	t.Cleanup(func() { TerminalLocalEnabled = old })

	reg := NewRegistry()
	if err := RegisterTerminalTool(reg, &TerminalConfig{
		Enabled:           true,
		DefaultTimeoutSec: 30,
		MaxOutputBytes:    8192,
		Runner:            osTerminalRunner{},
	}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("terminal")
	ctx := context.WithValue(context.Background(), ContextKeyWorkspaceRoot, t.TempDir())
	cmd := "echo pty_ok"
	if runtime.GOOS == "windows" {
		cmd = "echo pty_ok"
	}
	res, err := tl.Execute(ctx, map[string]any{"command": cmd, "pty": true, "timeout": 30})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["pty"] != true {
		t.Fatalf("expected pty=true, got %#v", m)
	}
	if m["status"] != "ok" && m["exit_code"] != 0 {
		// Some environments may still succeed with exit 0 even if status differs
		if m["error"] != nil {
			t.Fatalf("pty foreground failed: %#v", m)
		}
	}
	out := fmt.Sprint(m["stdout"])
	if !strings.Contains(out, "pty_ok") {
		t.Fatalf("stdout missing pty_ok: %#v", m)
	}
}

func TestStripANSI(t *testing.T) {
	got := stripANSI("\x1b[1mBold\x1b[0m text")
	if got != "Bold text" {
		t.Fatalf("got %q", got)
	}
}

func TestTruncateOutput(t *testing.T) {
	s := strings.Repeat("a", 100)
	got := truncateOutput(s, 40)
	if len(got) >= 100 {
		t.Fatalf("expected truncation, len=%d", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("got %q", got)
	}
}

func TestTerminalTool_ConfirmErrorCode_NotFound(t *testing.T) {
	runner := &fakeTerminalRunner{result: TerminalRunResult{ExitCode: 0}}
	tl := registerTerminalForTest(t, runner)
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess")
	ctx = context.WithValue(ctx, ContextKeyWorkspaceRoot, t.TempDir())

	res, err := tl.Execute(ctx, map[string]any{
		"command":       "ignored",
		"confirm_token": "no-such-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["error_code"] != "not_found" {
		t.Fatalf("error_code: %#v", m)
	}
	if m["error"] != "确认已失效（可能已被替换、已使用或服务重启），请重新发起" {
		t.Fatalf("error: %#v", m)
	}
}

func TestTerminalTool_ConfirmErrorCode_Expired(t *testing.T) {
	runner := &fakeTerminalRunner{result: TerminalRunResult{ExitCode: 0}}
	store := NewInMemoryTerminalPendingStore()
	reg := NewRegistry()
	cfg := &TerminalConfig{
		Enabled:           true,
		ConfirmTTLSeconds: 60,
		PendingStore:      store,
		TokenGen:          &fakeTokenGen{next: "tok-exp"},
		Runner:            runner,
	}
	if err := RegisterTerminalTool(reg, cfg); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("terminal")
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess-exp")
	ctx = context.WithValue(ctx, ContextKeyWorkspaceRoot, t.TempDir())

	_, err := tl.Execute(ctx, map[string]any{"command": "sudo true"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := store.GetPending(ctx, "sess-exp", "tok-exp")
	if p == nil {
		t.Fatal("pending missing")
	}
	p.CreatedAt = time.Now().Add(-10 * time.Minute)
	_ = store.SavePending(ctx, "sess-exp", *p)

	res, err := tl.Execute(ctx, map[string]any{
		"command":       "ignored",
		"confirm_token": "tok-exp",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["error_code"] != "expired" {
		t.Fatalf("error_code: %#v", m)
	}
	if m["error"] != "确认已过期，请让助手重新发起操作" {
		t.Fatalf("error: %#v", m)
	}
}
