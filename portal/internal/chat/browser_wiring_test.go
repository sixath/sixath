package chat

import (
	"testing"

	"github.com/sixath/framework/tool"
	"github.com/sixath/framework/tool/browser"
)

func TestRegisterAgentRuntimeTools_BrowserFlagOff_NoTools(t *testing.T) {
	reg := tool.NewRegistry()
	old := DefaultHermesP0ToolFlags
	SetHermesP0ToolFlags(HermesP0ToolFlags{SkillManageConfirmCreateDelete: true})
	t.Cleanup(func() { SetHermesP0ToolFlags(old) })

	if err := RegisterAgentRuntimeTools(reg, AgentRuntimeToolsOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"browser_navigate", "browser_snapshot", "browser_click", "browser_type"} {
		if _, ok := reg.Get(name); ok {
			t.Fatalf("expected %q absent when BrowserEnabled=false", name)
		}
	}
}

func TestRegisterAgentRuntimeTools_BrowserFlagOn_FakeFactory(t *testing.T) {
	reg := tool.NewRegistry()
	store := browser.NewSessionStore()
	factory := func() (browser.Backend, error) {
		return browser.NewFakeBackend(), nil
	}
	flags := HermesP0ToolFlags{BrowserEnabled: true}
	if err := RegisterAgentRuntimeTools(reg, AgentRuntimeToolsOptions{
		Flags:          &flags,
		BrowserStore:   store,
		BrowserFactory: factory,
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"browser_navigate", "browser_snapshot", "browser_click", "browser_type"} {
		if _, ok := reg.Get(name); !ok {
			t.Fatalf("expected %q registered when BrowserEnabled=true", name)
		}
	}
}

func TestEnrichHermesP0FromEnv_BrowserEnabled(t *testing.T) {
	t.Setenv("SATH_BROWSER_ENABLED", "true")
	flags := HermesP0ToolFlags{}
	EnrichHermesP0FromEnv(&flags)
	if !flags.BrowserEnabled {
		t.Fatalf("expected BrowserEnabled from SATH_BROWSER_ENABLED: %+v", flags)
	}
}
