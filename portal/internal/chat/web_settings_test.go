package chat

import (
	"testing"

	fwconfig "github.com/sixath/framework/config"
	"github.com/sixath/framework/tool"
)

func TestWebToolsShouldRegister_autoWhenBochaKey(t *testing.T) {
	SetWebSettings(WebSettings{BochaAPIKey: "sk-x", DefaultCount: 8, DefaultSummary: true})
	if !WebToolsShouldRegister() {
		t.Fatal("expected auto register when bocha key set")
	}
}

func TestWebToolsShouldRegister_explicitOff(t *testing.T) {
	off := false
	SetWebSettings(WebSettings{ToolsEnabledExplicit: &off, BochaAPIKey: "sk-x"})
	if WebToolsShouldRegister() {
		t.Fatal("expected off when tools_enabled=false")
	}
}

func TestRegisterWebTools_registersWhenConfigured(t *testing.T) {
	SetWebSettings(WebSettings{BochaAPIKey: "sk-test", DefaultCount: 8, DefaultSummary: true})
	reg := tool.NewRegistry()
	if err := RegisterWebTools(reg); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("web_search"); !ok {
		t.Fatal("web_search not registered")
	}
}

func TestMergeWebSettings(t *testing.T) {
	on := true
	s := MergeWebSettings(WebSettings{}, &fwconfig.WebTools{
		ToolsEnabled:  &on,
		BochaAPIKey:   "k",
		SearchBackend: "bocha",
	})
	if s.ToolsEnabledExplicit == nil || !*s.ToolsEnabledExplicit {
		t.Fatalf("explicit: %#v", s.ToolsEnabledExplicit)
	}
	if s.BochaAPIKey != "k" {
		t.Fatalf("key=%q", s.BochaAPIKey)
	}
}
