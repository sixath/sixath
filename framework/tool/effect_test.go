package tool

import (
	"context"
	"strings"
	"testing"
)

func noopExec(context.Context, map[string]any) (any, error) { return nil, nil }

func TestRegisterFillsBuiltinEffect(t *testing.T) {
	reg := NewEmptyRegistry()
	if err := reg.Register(Tool{Name: "es_log_query", Execute: noopExec}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(Tool{Name: "custom_tool", Execute: noopExec}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(Tool{Name: "rca_grep", Effect: EffectWrite, Execute: noopExec}); err != nil {
		t.Fatal(err)
	}
	es, _ := reg.Get("es_log_query")
	if es.Effect != EffectRead {
		t.Fatalf("es_log_query effect=%q want read", es.Effect)
	}
	custom, _ := reg.Get("custom_tool")
	if custom.Effect != EffectUnknown {
		t.Fatalf("custom_tool effect=%q want unknown", custom.Effect)
	}
	explicit, _ := reg.Get("rca_grep")
	if explicit.Effect != EffectWrite {
		t.Fatalf("explicit effect must not be overridden, got %q", explicit.Effect)
	}
}

func TestEffectForHTTPMethod(t *testing.T) {
	reg := NewRegistry()
	h, ok := reg.Get("http_request")
	if !ok {
		t.Fatal("http_request missing")
	}
	cases := map[string]Effect{"GET": EffectRead, "head": EffectRead, "POST": EffectWrite, "": EffectRead}
	for method, want := range cases {
		if got := h.EffectFor(map[string]any{"method": method}); got != want {
			t.Fatalf("method %q effect=%q want %q", method, got, want)
		}
	}
	if got := HTTPMethodEffect(map[string]any{"url": "http://x"}); got != EffectRead {
		t.Fatalf("missing method effect=%q want read", got)
	}
	if got := HTTPMethodEffect(map[string]any{"url": "http://x", "body": `{"a":1}`}); got != EffectWrite {
		t.Fatalf("missing method with body effect=%q want write", got)
	}
	if got := HTTPMethodEffect(map[string]any{"method": "", "url": "http://x", "body": ""}); got != EffectRead {
		t.Fatalf("missing method with empty body effect=%q want read", got)
	}
}

func TestEffectForVMRunCmd(t *testing.T) {
	reg := NewEmptyRegistry()
	if err := RegisterVMRunCmd(reg, VMRunCmdConfig{}); err != nil {
		t.Fatal(err)
	}
	vm, _ := reg.Get("vm_run_cmd")
	if got := vm.EffectFor(map[string]any{"cmd": `dir /o-d G:\game`}); got != EffectRead {
		t.Fatalf("dir effect=%q want read", got)
	}
	if got := vm.EffectFor(map[string]any{"cmd": "taskkill /im game.exe /f"}); got != EffectWrite {
		t.Fatalf("taskkill effect=%q want write", got)
	}
	if got := vm.EffectFor(nil); got != EffectExec {
		t.Fatalf("empty cmd effect=%q want exec", got)
	}
}

func TestAppendDescription(t *testing.T) {
	reg := NewEmptyRegistry()
	if err := reg.Register(Tool{Name: "a", Description: "base", Execute: noopExec}); err != nil {
		t.Fatal(err)
	}
	if !reg.AppendDescription("a", "部署说明：端口 53000") {
		t.Fatal("append failed")
	}
	if !reg.AppendDescription("a", "部署说明：端口 53000") {
		t.Fatal("idempotent append should report true")
	}
	got, _ := reg.Get("a")
	if strings.Count(got.Description, "部署说明") != 1 || !strings.HasPrefix(got.Description, "base\n\n") {
		t.Fatalf("unexpected description %q", got.Description)
	}
	if reg.AppendDescription("missing", "x") || reg.AppendDescription("a", "  ") {
		t.Fatal("missing tool or blank extra must return false")
	}
}
