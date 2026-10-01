package tool

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func bigLog(lines int) string {
	var b strings.Builder
	for i := 1; i <= lines; i++ {
		fmt.Fprintf(&b, "2026-09-27 10:%02d:%02d INFO line %d 涓枃\n", (i/60)%60, i%60, i)
	}
	return b.String()
}

func textSpillCtx(t *testing.T) (context.Context, string) {
	t.Helper()
	ws := t.TempDir()
	ctx := context.WithValue(context.Background(), ContextKeyWorkspaceRoot, ws)
	ctx = context.WithValue(ctx, ContextKeySessionID, "sess-1")
	return ctx, ws
}

func TestMaybeSpillText(t *testing.T) {
	ctx, ws := textSpillCtx(t)
	if out, info := MaybeSpillText(ctx, "vm_run_cmd", "small"); out != "small" || info != nil {
		t.Fatalf("small text must not spill: %q %v", out, info)
	}
	text := bigLog(2000)
	out, info := MaybeSpillText(ctx, "vm_run_cmd", text)
	if info == nil {
		t.Fatal("expected spill")
	}
	if !strings.HasPrefix(info.Path, "tmp/results/sess-1/") || !strings.HasSuffix(info.Path, ".txt") {
		t.Fatalf("path=%q", info.Path)
	}
	saved, err := os.ReadFile(filepath.Join(ws, filepath.FromSlash(info.Path)))
	if err != nil || string(saved) != text {
		t.Fatalf("saved file mismatch err=%v len=%d", err, len(saved))
	}
	if len(out) > textSpillHeadBytes+textSpillTailBytes+300 {
		t.Fatalf("preview too large: %d", len(out))
	}
	if !strings.Contains(out, "line 1 涓枃") || !strings.Contains(out, "line 2000 涓枃") || !strings.Contains(out, info.Path) {
		t.Fatalf("preview must keep head, tail and path: %q", out)
	}
	if !strings.Contains(out, "read_file") {
		t.Fatal("preview should tell the model how to read the file")
	}

	if out, info := MaybeSpillText(context.Background(), "vm_run_cmd", text); info != nil || out != text {
		t.Fatal("without workspace text must pass through unchanged")
	}
}

func TestVMRunCmd_LargeStdoutSpills(t *testing.T) {
	text := bigLog(3000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(text))
	}))
	t.Cleanup(srv.Close)
	host, port := hostPort(t, srv.URL)
	reg := NewRegistry()
	if err := RegisterVMRunCmd(reg, VMRunCmdConfig{HTTPClient: srv.Client()}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("vm_run_cmd")
	args := map[string]any{"host": host, "port": port, "cmd": `type D:\a.log`}

	ctx, _ := textSpillCtx(t)
	m := mustMap(t, tl, ctx, args)
	if m["spill"] == nil || m["truncated"] != false {
		t.Fatalf("expected spill without truncation: %#v", m["spill"])
	}
	if !strings.Contains(m["stdout"].(string), "line 3000") {
		t.Fatal("preview should include the newest lines")
	}

	m = mustMap(t, tl, context.Background(), args)
	if m["spill"] != nil || m["truncated"] != true || len(m["stdout"].(string)) != vmRunCmdMaxBody {
		t.Fatalf("without workspace keep the 50KB head cap: truncated=%v len=%d", m["truncated"], len(m["stdout"].(string)))
	}
}

func mustMap(t *testing.T, tl Tool, ctx context.Context, args map[string]any) map[string]any {
	t.Helper()
	out, err := tl.Execute(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	return out.(map[string]any)
}
